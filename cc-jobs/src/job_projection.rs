//! Purpose-limited metadata for two already assigned recovery jobs.
use crate::*;
use std::{collections::BTreeSet, fs::File, io::Read, path::Path};

const KARAOKE_JOB: &str = "20261008-100555-orch-todo-1157";
const FOUR_JOB: &str = "20261112-093913-4431-fourbros-pr1245-normal-job-prd-qa";
const FOUR_SESSION: &str = "01a1bdce-c2db-7792-a036-79044f46d9fb";
const FOUR_LAUNCH: &str = "7e5809fa41664dc988379e7163284acc";

fn snowflake(value: &Value) -> Value {
    value
        .as_str()
        .filter(|s| s.len() >= 16 && s.len() <= 20 && s.bytes().all(|b| b.is_ascii_digit()))
        .map_or(Value::Null, |s| json!(s))
}

pub(crate) fn project(
    job: &Value,
    id: &str,
    purpose: &str,
    result: Option<&Path>,
    _associations: Option<&Value>,
    stderr: &[u8],
) -> Result<Value> {
    if s(job, "id") != id {
        return Err(Error::new("job projection identity mismatch"));
    }
    match purpose {
        "karaoke-1157-locators" if id == KARAOKE_JOB && job["todo_id"].as_u64() == Some(1157) => {
            let cwd = job["cwd"]
                .as_str()
                .filter(|p| Path::new(p).is_absolute() && !p.contains(['\r', '\n', '\0']))
                .map_or(Value::Null, |p| json!(p));
            // A route records a posted question, not its original-question
            // status. Only that explicit stored attribute is evidence here.
            let question = snowflake(&job["original_question_id"]);
            Ok(
                json!({"metadata_only":true,"purpose":purpose,"id":id,"cwd":cwd,"resultLocator":result.map(|p|p.to_string_lossy().to_string()),"originalQuestionId":question}),
            )
        }
        "fourbros-1245-launch-failure"
            if id == FOUR_JOB
                && job["status"] == "failed"
                && job["resume_session"] == FOUR_SESSION
                && job["launch_key"] == FOUR_LAUNCH =>
        {
            let message = &stderr[..stderr.len().min(16384)];
            // The bounded reader retains one lookahead byte. An alphanumeric
            // continuation means the final token has no observed right edge;
            // a delimiter (or actual EOF) still completes a short numeric token.
            let complete = if stderr.get(16384).is_some_and(u8::is_ascii_alphanumeric) {
                message
                    .iter()
                    .rposition(|b| !b.is_ascii_alphanumeric())
                    .map_or(&message[..0], |at| &message[..=at])
            } else {
                message
            };
            let has = |needle: &[u8]| {
                message
                    .windows(needle.len())
                    .any(|part| part.eq_ignore_ascii_case(needle))
            };
            let reason = if has(b"unexpected argument") || has(b"unrecognized option") {
                "cli_arguments"
            } else if has(b"session")
                && (has(b"locked") || has(b"in use") || has(b"already running"))
            {
                "session_in_use"
            } else if has(b"session") && (has(b"not found") || has(b"no session")) {
                "session_unavailable"
            } else if has(b"authentication") || has(b"unauthorized") || http_401(complete) {
                "authentication"
            } else if has(b"config") && (has(b"invalid") || has(b"unknown") || has(b"parse")) {
                "configuration"
            } else if has(b"usage limit") || has(b"rate limit") {
                "usage_limit"
            } else if has(b"network") || has(b"connection") {
                "transport"
            } else {
                "unknown"
            };
            let signals: Vec<_> = [
                ("error", b"error".as_slice()),
                ("warning", b"warn".as_slice()),
                ("panic", b"panic".as_slice()),
                ("configuration", b"config".as_slice()),
                ("filesystem", b"os error".as_slice()),
                ("permission", b"permission".as_slice()),
                ("database", b"database".as_slice()),
                ("provider", b"provider".as_slice()),
                ("model", b"model".as_slice()),
                ("session", b"session".as_slice()),
                ("transport", b"connect".as_slice()),
                ("tls", b"certificate".as_slice()),
                ("sandbox", b"sandbox".as_slice()),
                ("policy", b"policy".as_slice()),
                ("mcp", b"mcp".as_slice()),
                ("chrome", b"chrome".as_slice()),
                ("argument", b"argument".as_slice()),
                ("timeout", b"timed out".as_slice()),
                ("quota", b"quota".as_slice()),
                ("cancel", b"cancel".as_slice()),
            ]
            .into_iter()
            .filter_map(|(tag, word)| has(word).then_some(tag))
            .collect();
            let os_error = bounded_os_error(complete);
            let launcher = match Path::new(s(job, "codex"))
                .extension()
                .and_then(|e| e.to_str())
                .map(str::to_ascii_lowercase)
                .as_deref()
            {
                Some("cmd" | "bat") => "cmd",
                Some("exe") => "exe",
                _ => "unknown",
            };
            let mut signals = signals;
            if os_error == Some(5) && !signals.contains(&"permission") {
                let at = signals
                    .iter()
                    .position(|v| *v == "database")
                    .unwrap_or(signals.len());
                signals.insert(at, "permission")
            }
            Ok(
                json!({"metadata_only":true,"purpose":purpose,"id":id,"status":"failed","reason":reason,"diagnosticSignals":signals,"osErrorCode":os_error,"launcherKind":launcher,"exitCode":job["exit_code"].as_i64(),"steps":job["steps"].as_u64(),"stderrPresent":!stderr.is_empty(),"stderrTruncated":stderr.len()>16384,"resumeSession":FOUR_SESSION,"sourceBodiesReturned":false}),
            )
        }
        _ => Err(Error::new("job projection purpose or binding unavailable")),
    }
}

fn bounded_os_error(message: &[u8]) -> Option<u32> {
    let prefix = b"os error ";
    message
        .windows(prefix.len())
        .enumerate()
        .filter(|(at, v)| {
            v.eq_ignore_ascii_case(prefix)
                && (*at == 0 || !message[*at - 1].is_ascii_alphanumeric())
        })
        .find_map(|(at, _)| {
            let tail = &message[at + prefix.len()..];
            let count = tail.iter().take_while(|b| b.is_ascii_digit()).count();
            if count == 0 || count > 10 || tail.get(count).is_some_and(u8::is_ascii_alphanumeric) {
                return None;
            }
            let mut value = 0u32;
            for b in &tail[..count] {
                value = value.checked_mul(10)?.checked_add(u32::from(*b - b'0'))?
            }
            Some(value)
        })
}

impl Context {
    pub(crate) fn purpose_job_metadata(&self, id: &str, purpose: &str) -> Result<Value> {
        if !matches!(
            (id, purpose),
            (KARAOKE_JOB, "karaoke-1157-locators") | (FOUR_JOB, "fourbros-1245-launch-failure")
        ) {
            return Err(Error::new("job projection purpose unavailable"));
        }
        let dir = self.job_dir(id);
        // Keep the normal installed job-store spelling and each physical parent
        // stable for every read participating in this projection.
        let _parents = pin_projection_parents(&dir)?;
        let mut job_bytes = read_projection_file(self, &dir.join("job.json"), 1 << 20, false)?;
        let job = parse_projection_json(&job_bytes);
        job_bytes.fill(0);
        let job = job?;
        // Bind the original failed attempt before opening its diagnostic file.
        if id == FOUR_JOB {
            project(&job, id, purpose, None, None, &[])?;
        }
        if id == KARAOKE_JOB {
            if job["todo_id"].as_u64() != Some(1157) || s(&job, "id") != id {
                return Err(Error::new("job projection purpose or binding unavailable"));
            }
            let output = dir.join("last_message.md");
            // Resolve the locator through a metadata handle, never its body.
            // Use the same plain/single-link rules as the other source files.
            let exists = open_projection_file(self, &output).is_ok();
            project(
                &job,
                id,
                purpose,
                exists.then_some(output.as_path()),
                None,
                &[],
            )
        } else {
            // The normal job manager reads its one own failed attempt; only a
            // fixed category leaves this boundary, never command/error text.
            let path = dir.join("stderr.log");
            self.rt
                .before_load(&path)
                .map_err(|_| Error::new("launch diagnostic source unavailable"))?;
            let missing = std::fs::symlink_metadata(&path)
                .is_err_and(|e| e.kind() == std::io::ErrorKind::NotFound);
            let mut stderr = if missing {
                Vec::new()
            } else {
                read_projection_file(self, &path, 16384, true)
                    .map_err(|_| Error::new("launch diagnostic source unavailable"))?
            };
            let result = project(&job, id, purpose, None, None, &stderr);
            stderr.fill(0);
            result
        }
    }
}

fn http_401(message: &[u8]) -> bool {
    [
        b"HTTP 401".as_slice(),
        b"HTTP status 401".as_slice(),
        b"status code: 401".as_slice(),
    ]
    .iter()
    .any(|needle| {
        message.windows(needle.len()).enumerate().any(|(i, part)| {
            part.eq_ignore_ascii_case(needle)
                && (i == 0 || !message[i - 1].is_ascii_alphanumeric())
                && (i + needle.len() == message.len()
                    || !message[i + needle.len()].is_ascii_alphanumeric())
        })
    })
}

fn read_projection_file(ctx: &Context, path: &Path, max: u64, prefix: bool) -> Result<Vec<u8>> {
    let (file, _parents) = open_projection_file(ctx, path)?;
    let mut raw = vec![0u8; (max + 1) as usize];
    let n = fill_projection_buffer(file, &mut raw)?;
    raw.truncate(n);
    if !prefix && raw.len() as u64 > max {
        raw.fill(0);
        return Err(Error::new("job projection source too large"));
    }
    Ok(raw)
}

fn fill_projection_buffer(mut reader: impl Read, buffer: &mut [u8]) -> Result<usize> {
    let mut n = 0;
    while n < buffer.len() {
        match reader.read(&mut buffer[n..]) {
            Ok(0) => break,
            Ok(read) => n += read,
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
            Err(_) => {
                buffer.fill(0);
                return Err(Error::new("job projection source unavailable"));
            }
        }
    }
    Ok(n)
}

fn open_projection_file(ctx: &Context, path: &Path) -> Result<(File, Vec<File>)> {
    ctx.rt
        .before_load(path)
        .map_err(|_| Error::new("job projection source unavailable"))?;
    let parents = pin_projection_parents(
        path.parent()
            .ok_or_else(|| Error::new("job projection source unavailable"))?,
    )?;
    let mut options = File::options();
    options.read(true);
    #[cfg(windows)]
    {
        use std::os::windows::fs::OpenOptionsExt;
        options.share_mode(1).custom_flags(0x00200000);
    }
    let file = options
        .open(path)
        .map_err(|_| Error::new("job projection source unavailable"))?;
    let meta = file
        .metadata()
        .map_err(|_| Error::new("job projection source unavailable"))?;
    if !meta.is_file() {
        return Err(Error::new("job projection source unavailable"));
    }
    #[cfg(windows)]
    {
        use std::os::windows::fs::MetadataExt;
        use std::os::windows::io::AsRawHandle;
        use windows_sys::Win32::Storage::FileSystem::{
            BY_HANDLE_FILE_INFORMATION, GetFileInformationByHandle,
        };
        let mut info: BY_HANDLE_FILE_INFORMATION = unsafe { std::mem::zeroed() };
        if meta.file_attributes() & 0x400 != 0
            || unsafe { GetFileInformationByHandle(file.as_raw_handle() as _, &mut info) } == 0
            || info.nNumberOfLinks != 1
        {
            return Err(Error::new("job projection source unavailable"));
        }
    }
    #[cfg(not(windows))]
    {
        if !std::fs::symlink_metadata(path).is_ok_and(|m| !m.file_type().is_symlink()) {
            return Err(Error::new("job projection source unavailable"));
        }
    }
    Ok((file, parents))
}

fn pin_projection_parents(dir: &Path) -> Result<Vec<File>> {
    let mut directories = Vec::new();
    let mut names = dir.ancestors().collect::<Vec<_>>();
    names.reverse();
    for name in names {
        if name.as_os_str().is_empty() {
            return Err(Error::new("job projection parent unavailable"));
        }
        let mut options = File::options();
        options.read(true);
        #[cfg(windows)]
        {
            use std::os::windows::fs::OpenOptionsExt;
            options
                .access_mode(0x81)
                .share_mode(3)
                .custom_flags(0x02000000 | 0x00200000);
        }
        let file = options
            .open(name)
            .map_err(|_| Error::new("job projection parent unavailable"))?;
        let meta = file
            .metadata()
            .map_err(|_| Error::new("job projection parent unavailable"))?;
        if !meta.is_dir() {
            return Err(Error::new("job projection parent unavailable"));
        }
        #[cfg(windows)]
        {
            use std::os::windows::fs::MetadataExt;
            if meta.file_attributes() & 0x400 != 0 {
                return Err(Error::new("job projection parent unavailable"));
            }
        }
        #[cfg(not(windows))]
        {
            if !std::fs::symlink_metadata(name)
                .is_ok_and(|m| m.is_dir() && !m.file_type().is_symlink())
            {
                return Err(Error::new("job projection parent unavailable"));
            }
        }
        directories.push(file);
    }
    Ok(directories)
}

// Stored records can evolve, so unknown fields remain valid. Duplicate JSON
// keys are rejected before projection instead of allowing a last-key override.
fn parse_projection_json(bytes: &[u8]) -> Result<Value> {
    struct Unique(Value);
    impl<'de> serde::Deserialize<'de> for Unique {
        fn deserialize<D: serde::Deserializer<'de>>(d: D) -> std::result::Result<Self, D::Error> {
            struct V;
            impl<'de> serde::de::Visitor<'de> for V {
                type Value = Unique;
                fn expecting(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
                    f.write_str("one JSON value")
                }
                fn visit_bool<E: serde::de::Error>(
                    self,
                    v: bool,
                ) -> std::result::Result<Unique, E> {
                    Ok(Unique(json!(v)))
                }
                fn visit_i64<E: serde::de::Error>(self, v: i64) -> std::result::Result<Unique, E> {
                    Ok(Unique(json!(v)))
                }
                fn visit_u64<E: serde::de::Error>(self, v: u64) -> std::result::Result<Unique, E> {
                    Ok(Unique(json!(v)))
                }
                fn visit_f64<E: serde::de::Error>(self, v: f64) -> std::result::Result<Unique, E> {
                    Ok(Unique(json!(v)))
                }
                fn visit_str<E: serde::de::Error>(self, v: &str) -> std::result::Result<Unique, E> {
                    Ok(Unique(json!(v)))
                }
                fn visit_string<E: serde::de::Error>(
                    self,
                    v: String,
                ) -> std::result::Result<Unique, E> {
                    Ok(Unique(json!(v)))
                }
                fn visit_unit<E: serde::de::Error>(self) -> std::result::Result<Unique, E> {
                    Ok(Unique(Value::Null))
                }
                fn visit_seq<A: serde::de::SeqAccess<'de>>(
                    self,
                    mut a: A,
                ) -> std::result::Result<Unique, A::Error> {
                    let mut values = Vec::new();
                    while let Some(Unique(v)) = a.next_element()? {
                        values.push(v)
                    }
                    Ok(Unique(Value::Array(values)))
                }
                fn visit_map<A: serde::de::MapAccess<'de>>(
                    self,
                    mut a: A,
                ) -> std::result::Result<Unique, A::Error> {
                    let mut keys = BTreeSet::new();
                    let mut values = serde_json::Map::new();
                    while let Some(k) = a.next_key::<String>()? {
                        if !keys.insert(k.to_ascii_lowercase()) {
                            return Err(serde::de::Error::custom("duplicate record key"));
                        }
                        let Unique(v) = a.next_value()?;
                        values.insert(k, v);
                    }
                    Ok(Unique(Value::Object(values)))
                }
            }
            d.deserialize_any(V)
        }
    }
    use serde::Deserialize;
    let mut d = serde_json::Deserializer::from_slice(bytes);
    let Unique(v) =
        Unique::deserialize(&mut d).map_err(|_| Error::new("job projection record invalid"))?;
    d.end()
        .map_err(|_| Error::new("job projection record invalid"))?;
    Ok(v)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn original_unknown_diagnostic_returns_only_fixed_cause_signals() {
        let job = json!({"id":FOUR_JOB,"status":"failed","resume_session":FOUR_SESSION,"launch_key":FOUR_LAUNCH,"codex":"C:/owned/codex.cmd"});
        let raw=b"Error: failed to read configuration from PRIVATE (os error 5). State database unavailable.";
        let out = project(
            &job,
            FOUR_JOB,
            "fourbros-1245-launch-failure",
            None,
            None,
            raw,
        )
        .unwrap();
        assert_eq!(
            out["diagnosticSignals"],
            json!([
                "error",
                "configuration",
                "filesystem",
                "permission",
                "database"
            ])
        );
        assert_eq!(out["osErrorCode"], 5);
        assert_eq!(out["launcherKind"], "cmd");
        assert!(!out.to_string().contains("PRIVATE"));
    }
    #[test]
    fn extra_launch_signals_never_return_unmatched_text_or_unbounded_numbers() {
        let job = json!({"id":FOUR_JOB,"status":"failed","resume_session":FOUR_SESSION,"launch_key":FOUR_LAUNCH,"codex":"C:/PRIVATE/codex.exe"});
        for (input, number) in [
            ("PRIVATE arbitrary", None::<u32>),
            ("error PRIVATE os error 4294967296", None),
            ("os error 5x", None),
        ] {
            let p = project(
                &job,
                FOUR_JOB,
                "fourbros-1245-launch-failure",
                None,
                None,
                input.as_bytes(),
            )
            .unwrap();
            assert!(!p.to_string().contains("PRIVATE"));
            assert_eq!(p["osErrorCode"], number.map_or(Value::Null, |n| json!(n)));
            assert_eq!(p["launcherKind"], "exe");
            for tag in p["diagnosticSignals"].as_array().unwrap() {
                assert!(matches!(
                    tag.as_str(),
                    Some("error" | "filesystem" | "permission")
                ));
            }
        }
    }
    #[test]
    fn windows_invalid_data_code_is_not_permission_denial() {
        let job = json!({"id":FOUR_JOB,"status":"failed","resume_session":FOUR_SESSION,"launch_key":FOUR_LAUNCH});
        let p = project(
            &job,
            FOUR_JOB,
            "fourbros-1245-launch-failure",
            None,
            None,
            b"os error 13",
        )
        .unwrap();
        assert_eq!(p["osErrorCode"], 13);
        assert!(
            !p["diagnosticSignals"]
                .as_array()
                .unwrap()
                .iter()
                .any(|v| v == "permission")
        );
    }
    #[test]
    fn malformed_os_error_does_not_hide_a_later_valid_os_error() {
        assert_eq!(bounded_os_error(b"os error invalid; os error 5"), Some(5));
        assert_eq!(
            bounded_os_error(b"os error 4294967296; os error 2"),
            Some(2)
        );
        assert_eq!(bounded_os_error(b"os error 5x; os error 13"), Some(13));
        assert_eq!(bounded_os_error(b"chaos error 5"), None);
        assert_eq!(bounded_os_error(b"BIOS error 5; (os error 2)"), Some(2));
        assert_eq!(bounded_os_error(b"(OS error 4294967295)"), Some(u32::MAX));
        assert_eq!(bounded_os_error(b"(os error 2) then (os error 5)"), Some(2));
    }
    #[test]
    fn truncated_status_token_is_not_treated_as_complete() {
        let job = json!({"id":FOUR_JOB,"status":"failed","resume_session":FOUR_SESSION,"launch_key":FOUR_LAUNCH});
        let mut raw = vec![b' '; 16385];
        raw[16374..].copy_from_slice(b"os error 50");
        let p = project(
            &job,
            FOUR_JOB,
            "fourbros-1245-launch-failure",
            None,
            None,
            &raw,
        )
        .unwrap();
        assert!(p["osErrorCode"].is_null());
        assert!(
            !p["diagnosticSignals"]
                .as_array()
                .unwrap()
                .iter()
                .any(|v| v == "permission")
        );
        raw.fill(b' ');
        raw[16376..].copy_from_slice(b"HTTP 4010");
        let p = project(
            &job,
            FOUR_JOB,
            "fourbros-1245-launch-failure",
            None,
            None,
            &raw,
        )
        .unwrap();
        assert_ne!(p["reason"], "authentication");
    }
    #[test]
    fn lookahead_keeps_complete_numeric_tokens_and_rejects_continuations() {
        let job = json!({"id":FOUR_JOB,"status":"failed","resume_session":FOUR_SESSION,"launch_key":FOUR_LAUNCH});
        for (token, suffix, number, reason) in [
            (b"os error 5".as_slice(), b"".as_slice(), Some(5), "unknown"),
            (
                b"os error 5".as_slice(),
                b")".as_slice(),
                Some(5),
                "unknown",
            ),
            (b"os error 5".as_slice(), b"0".as_slice(), None, "unknown"),
            (b"os error 5".as_slice(), b"x".as_slice(), None, "unknown"),
            (
                b"os error 13".as_slice(),
                b" ".as_slice(),
                Some(13),
                "unknown",
            ),
            (
                b"HTTP 401".as_slice(),
                b"".as_slice(),
                None,
                "authentication",
            ),
            (
                b"HTTP 401".as_slice(),
                b" ".as_slice(),
                None,
                "authentication",
            ),
            (b"HTTP 401".as_slice(), b"0".as_slice(), None, "unknown"),
            (b"HTTP 401".as_slice(), b"x".as_slice(), None, "unknown"),
        ] {
            let mut raw = vec![b' '; 16384];
            raw[16384 - token.len()..].copy_from_slice(token);
            raw.extend_from_slice(suffix);
            let p = project(
                &job,
                FOUR_JOB,
                "fourbros-1245-launch-failure",
                None,
                None,
                &raw,
            )
            .unwrap();
            assert_eq!(p["osErrorCode"], number.map_or(Value::Null, |n| json!(n)));
            assert_eq!(p["reason"], reason);
            assert_eq!(p["stderrTruncated"], !suffix.is_empty());
            assert_eq!(
                p["diagnosticSignals"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .any(|v| v == "permission"),
                number == Some(5)
            );
        }
        // An earlier complete token remains available when the last token is
        // incomplete; lookahead cannot create a new token outside the prefix.
        let mut raw = vec![b' '; 16385];
        raw[..12].copy_from_slice(b"(os error 2)");
        raw[16374..].copy_from_slice(b"os error 50");
        assert_eq!(
            project(
                &job,
                FOUR_JOB,
                "fourbros-1245-launch-failure",
                None,
                None,
                &raw
            )
            .unwrap()["osErrorCode"],
            2
        );
        raw.fill(b' ');
        raw[16376..].copy_from_slice(b"HTTP 4010");
        raw[16384] = b' ';
        assert_eq!(
            project(
                &job,
                FOUR_JOB,
                "fourbros-1245-launch-failure",
                None,
                None,
                &raw
            )
            .unwrap()["reason"],
            "authentication"
        );
    }
    #[test]
    fn stored_projection_rejects_duplicate_keys_before_selecting_identity() {
        for raw in [
            br#"{"id":"first","id":"second"}"#.as_slice(),
            br#"{"id":"first","ID":"second"}"#.as_slice(),
            br#"{"nested":{"question_thread_id":"1","question_thread_id":"2"}}"#.as_slice(),
            br#"{} {}"#.as_slice(),
        ] {
            assert!(parse_projection_json(raw).is_err())
        }
        assert_eq!(
            parse_projection_json(br#"{"id":"one","unknown":[true,null,1,"text"]}"#).unwrap()["id"],
            "one"
        );
    }
    #[test]
    fn association_projection_requires_exact_original_job_and_single_declared_question() {
        let job = json!({"id":KARAOKE_JOB,"todo_id":1157});
        let id = "1557593952604131370";
        let associations = json!({"question_thread_ids":[id],"question_routes":{id:{"posting_job":KARAOKE_JOB,"todo_id":1157,"question_thread_id":id}}});
        assert!(
            project(
                &job,
                KARAOKE_JOB,
                "karaoke-1157-locators",
                None,
                Some(&associations),
                &[]
            )
            .unwrap()["originalQuestionId"]
                .is_null()
        );
        for case in [
            "other_job",
            "other_todo",
            "undeclared",
            "mismatched_key",
            "duplicate_declared",
            "ambiguous",
        ] {
            let mut a = associations.clone();
            match case {
                "other_job" => a["question_routes"][id]["posting_job"] = json!("other"),
                "other_todo" => a["question_routes"][id]["todo_id"] = json!(2162),
                "undeclared" => a["question_thread_ids"] = json!([]),
                "mismatched_key" => {
                    a["question_routes"][id]["question_thread_id"] = json!("1557593952604131371")
                }
                "duplicate_declared" => a["question_thread_ids"] = json!([id, id]),
                "ambiguous" => {
                    let second = "1557593952604131371";
                    a["question_thread_ids"] = json!([id, second]);
                    a["question_routes"][second] = json!({"posting_job":KARAOKE_JOB,"todo_id":1157,"question_thread_id":second});
                }
                _ => unreachable!(),
            }
            assert!(
                project(
                    &job,
                    KARAOKE_JOB,
                    "karaoke-1157-locators",
                    None,
                    Some(&a),
                    &[]
                )
                .unwrap()["originalQuestionId"]
                    .is_null(),
                "{case}"
            );
        }
    }
    #[test]
    fn a_later_unique_route_cannot_be_promoted_to_original_question() {
        let job = json!({"id":KARAOKE_JOB,"todo_id":1157});
        let id = "1557593952604131370";
        let association = json!({"question_thread_ids":[id],"question_routes":{id:{"posting_job":KARAOKE_JOB,"todo_id":1157,"question_thread_id":id}}});
        assert!(
            project(
                &job,
                KARAOKE_JOB,
                "karaoke-1157-locators",
                None,
                Some(&association),
                &[]
            )
            .unwrap()["originalQuestionId"]
                .is_null()
        );
    }
    #[test]
    fn launch_reason_http_status_needs_token_boundaries() {
        let job = json!({"id":FOUR_JOB,"status":"failed","resume_session":FOUR_SESSION,"launch_key":FOUR_LAUNCH});
        for (raw, want) in [
            ("request id 401923; rate limit exceeded", "usage_limit"),
            ("session returned 4012 during request", "unknown"),
            ("HTTP 401 Unauthorized", "authentication"),
        ] {
            assert_eq!(
                project(
                    &job,
                    FOUR_JOB,
                    "fourbros-1245-launch-failure",
                    None,
                    None,
                    raw.as_bytes()
                )
                .unwrap()["reason"],
                want
            )
        }
    }
    #[test]
    fn secret_read_uses_same_fixed_buffer_and_erases_it_on_error() {
        struct Chunked {
            remaining: usize,
            error: bool,
            expected: usize,
            offset: usize,
        }
        impl Read for Chunked {
            fn read(&mut self, out: &mut [u8]) -> std::io::Result<usize> {
                assert_eq!(out.as_ptr() as usize, self.expected + self.offset);
                if self.remaining == 0 {
                    return if self.error {
                        Err(std::io::Error::other("synthetic_private"))
                    } else {
                        Ok(0)
                    };
                }
                let n = self.remaining.min(out.len()).min(19);
                out[..n].fill(b'S');
                self.remaining -= n;
                self.offset += n;
                Ok(n)
            }
        }
        let mut buffer = vec![0; 16385];
        let address = buffer.as_ptr() as usize;
        assert_eq!(
            fill_projection_buffer(
                Chunked {
                    remaining: 11200,
                    error: false,
                    expected: address,
                    offset: 0
                },
                &mut buffer
            )
            .unwrap(),
            11200
        );
        assert_eq!(buffer.as_ptr() as usize, address);
        buffer.fill(0);
        assert!(
            fill_projection_buffer(
                Chunked {
                    remaining: 1024,
                    error: true,
                    expected: address,
                    offset: 0
                },
                &mut buffer
            )
            .is_err()
        );
        assert!(buffer.iter().all(|x| *x == 0));
        assert_eq!(buffer.as_ptr() as usize, address);
    }
    #[test]
    fn exact_projection_retains_missing_fields_as_null() {
        let job =
            json!({"id":KARAOKE_JOB,"todo_id":1157,"cwd":"relative","last_message":"PRIVATE"});
        let p = project(&job, KARAOKE_JOB, "karaoke-1157-locators", None, None, &[]).unwrap();
        assert!(
            p["cwd"].is_null() && p["resultLocator"].is_null() && p["originalQuestionId"].is_null()
        );
        assert!(!p.to_string().contains("PRIVATE"));
    }
    #[test]
    fn exact_locators_use_stored_values_only() {
        let job = json!({"id":KARAOKE_JOB,"todo_id":1157,"cwd":std::env::current_dir().unwrap(),"original_question_id":"1557593952604131370"});
        let p = project(
            &job,
            KARAOKE_JOB,
            "karaoke-1157-locators",
            Some(Path::new("owned-result.md")),
            None,
            &[],
        )
        .unwrap();
        assert_eq!(p["originalQuestionId"], job["original_question_id"]);
        assert!(
            project(
                &job,
                "different-job",
                "karaoke-1157-locators",
                None,
                None,
                &[]
            )
            .is_err()
        );
        let mut other = job;
        other["todo_id"] = json!(2162);
        assert!(
            project(
                &other,
                KARAOKE_JOB,
                "karaoke-1157-locators",
                None,
                None,
                &[]
            )
            .is_err()
        );
    }
    #[test]
    fn launch_diagnostic_returns_fixed_reason_not_stderr() {
        let job = json!({"id":FOUR_JOB,"status":"failed","resume_session":FOUR_SESSION,"launch_key":FOUR_LAUNCH,"exit_code":1,"steps":0});
        for (input, want) in [
            ("error: unexpected argument --PRIVATE", "cli_arguments"),
            ("Session already running PRIVATE", "session_in_use"),
            ("invalid config PRIVATE", "configuration"),
            ("PRIVATE arbitrary secret", "unknown"),
        ] {
            let p = project(
                &job,
                FOUR_JOB,
                "fourbros-1245-launch-failure",
                None,
                None,
                input.as_bytes(),
            )
            .unwrap();
            assert_eq!(p["reason"], want);
            assert!(!p.to_string().contains("PRIVATE"));
        }
        let mut forged = job;
        forged["resume_session"] = json!("other");
        assert!(
            project(
                &forged,
                FOUR_JOB,
                "fourbros-1245-launch-failure",
                None,
                None,
                b"private"
            )
            .is_err()
        );
    }
}
