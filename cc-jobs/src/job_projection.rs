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
    associations: Option<&Value>,
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
            let mut question = snowflake(&job["original_question_id"]);
            if question.is_null()
                && let Some(a) = associations
                && let Some(routes) = a["question_routes"].as_object()
            {
                let declared = a["question_thread_ids"].as_array();
                let candidates: Vec<_> = routes
                    .iter()
                    .filter(|(key, r)| {
                        s(r, "posting_job") == id
                            && r["todo_id"].as_u64() == Some(1157)
                            && r["question_thread_id"].as_str() == Some(key.as_str())
                            && declared.is_some_and(|ids| {
                                ids.iter()
                                    .filter(|v| v.as_str() == Some(key.as_str()))
                                    .count()
                                    == 1
                            })
                    })
                    .map(|(_, r)| r)
                    .map(|r| snowflake(&r["question_thread_id"]))
                    .filter(|v| !v.is_null())
                    .collect();
                if candidates.len() == 1 {
                    question = candidates[0].clone()
                }
            }
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
            } else if has(b"authentication") || has(b"unauthorized") || has(b"401") {
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
            Ok(
                json!({"metadata_only":true,"purpose":purpose,"id":id,"status":"failed","reason":reason,"exitCode":job["exit_code"].as_i64(),"steps":job["steps"].as_u64(),"stderrPresent":!stderr.is_empty(),"stderrTruncated":stderr.len()>16384,"resumeSession":FOUR_SESSION,"sourceBodiesReturned":false}),
            )
        }
        _ => Err(Error::new("job projection purpose or binding unavailable")),
    }
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
            let exists = self.rt.before_load(&output).is_ok()
                && std::fs::symlink_metadata(&output)
                    .is_ok_and(|m| m.is_file() && !m.file_type().is_symlink());
            let association_path = dir.join("discord-threads.json");
            let associations = if self.rt.before_load(&association_path).is_ok() {
                read_projection_file(self, &association_path, 65536, false)
                    .ok()
                    .and_then(|mut bytes| {
                        let value = parse_projection_json(&bytes).ok();
                        bytes.fill(0);
                        value
                    })
            } else {
                None
            };
            project(
                &job,
                id,
                purpose,
                exists.then_some(output.as_path()),
                associations.as_ref(),
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

fn read_projection_file(ctx: &Context, path: &Path, max: u64, prefix: bool) -> Result<Vec<u8>> {
    ctx.rt
        .before_load(path)
        .map_err(|_| Error::new("job projection source unavailable"))?;
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
    let mut raw = Vec::new();
    if file.take(max + 1).read_to_end(&mut raw).is_err() {
        raw.fill(0);
        return Err(Error::new("job projection source unavailable"));
    }
    if !prefix && raw.len() as u64 > max {
        raw.fill(0);
        return Err(Error::new("job projection source too large"));
    }
    Ok(raw)
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
        assert_eq!(
            project(
                &job,
                KARAOKE_JOB,
                "karaoke-1157-locators",
                None,
                Some(&associations),
                &[]
            )
            .unwrap()["originalQuestionId"],
            id
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
