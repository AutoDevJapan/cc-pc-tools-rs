//! Native partial results have their own receipt namespace and never authorize close.
use crate::{Paths, Result};
use chrono::{DateTime, Utc};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    fs::{self, File, OpenOptions},
    io::{Read, Seek, SeekFrom},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    time::{Duration, Instant},
};

const RECORDS: &str = "owner_native_partial_implementation_records";
const INPUT: &str = "discord-native-partial-implementation-v1";
const VERIFICATION: &str = "discord-native-partial-verification-v1";
const RECEIPT: &str = "todo-native-partial-implementation-record-v1";
const KIND: &str = "native_partial_implementation";
const RUNNER: &str = "D:/CC/Tools/discord-ask/unattended-runner.exe";
const INPUT_LIMIT: u64 = 1024 * 1024;
const OUTPUT_LIMIT: u64 = 64 * 1024;
#[derive(clap::Args, Debug)]
pub struct Options {
    #[arg(long)]
    input_file: PathBuf,
    #[arg(long)]
    origin_scope_file: PathBuf,
    #[arg(long)]
    origin_state_file: PathBuf,
    #[arg(long)]
    verify_only: bool,
}
const VERIFICATION_FIELDS: &[&str] = &[
    "domain",
    "verified",
    "input_sha256",
    "origin_state_sha256",
    "project_path_sha256",
    "source_receipt_sha256",
    "reply_sha256",
    "native_evidence_sha256",
    "acceptance",
    "application_inferred",
    "question_closed_get_verified",
];
const INPUT_FIELDS: &[&str] = &[
    "domain",
    "kind",
    "request_id",
    "message_id",
    "source_receipt_sha256",
    "reply_sha256",
    "origin_todo_id",
    "project",
    "revision",
    "parent_thread_id",
    "child_thread_id",
    "assignment",
    "verification_commands",
    "artifact",
    "evidence",
];
const RECEIPT_FIELDS: &[&str] = &[
    "domain",
    "kind",
    "input",
    "input_sha256",
    "origin_scope",
    "origin_state_sha256",
    "verification",
    "recorded_at",
    "acceptance",
    "application_inferred",
    "question_closed_get_verified",
];
fn fail<T>(code: &str) -> Result<T> {
    Err(format!("native_implementation_{code}"))
}
fn sha(b: &[u8]) -> String {
    format!("{:x}", Sha256::digest(b))
}
fn receipt_hash(value: &Value) -> Result<String> {
    fn ordered(value: &Value) -> Value {
        match value {
            Value::Object(map) => {
                let mut entries: Vec<_> = map.iter().collect();
                entries.sort_by_key(|(key, _)| *key);
                Value::Object(
                    entries
                        .into_iter()
                        .map(|(key, value)| (key.clone(), ordered(value)))
                        .collect(),
                )
            }
            Value::Array(values) => Value::Array(values.iter().map(ordered).collect()),
            _ => value.clone(),
        }
    }
    Ok(sha(&serde_json::to_vec(&ordered(value))
        .map_err(|_| "native_implementation_receipt_invalid")?))
}
fn text<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}
fn hex(s: &str, count: usize) -> bool {
    s.len() == count
        && s.bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn shape(v: &Value, keys: &[&str]) -> Result<()> {
    if v.as_object()
        .is_none_or(|o| o.len() != keys.len() || keys.iter().any(|k| !o.contains_key(*k)))
    {
        return fail("schema_invalid");
    }
    Ok(())
}
fn identifier(s: &str, max: usize) -> bool {
    !s.is_empty()
        && s.len() <= max
        && s.bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}
fn native_id(s: &str) -> bool {
    uuid::Uuid::parse_str(s).is_ok_and(|id| id.to_string() == s)
}
fn decimal(s: &str) -> bool {
    !s.starts_with('0')
        && !s.is_empty()
        && s.bytes().all(|b| b.is_ascii_digit())
        && s.parse::<u64>().is_ok_and(|id| id > 0)
}
fn colons(bytes: &[u8]) -> usize {
    let (mut quoted, mut escaped, mut count) = (false, false, 0);
    for b in bytes {
        if quoted {
            if escaped {
                escaped = false;
            } else if *b == b'\\' {
                escaped = true;
            } else if *b == b'"' {
                quoted = false;
            }
        } else if *b == b'"' {
            quoted = true;
        } else if *b == b':' {
            count += 1;
        }
    }
    count
}
fn parse(bytes: &[u8]) -> Result<Value> {
    let value: Value =
        serde_json::from_slice(bytes).map_err(|_| "native_implementation_json_invalid")?;
    let serialized =
        serde_json::to_vec(&value).map_err(|_| "native_implementation_json_invalid")?;
    if colons(bytes) != colons(&serialized) {
        return fail("duplicate_keys");
    }
    Ok(value)
}

// Keep these files open through verification and the origin update. Windows
// share-read excludes writes/replacements while the exact bytes are consumed.
struct PinnedInput {
    file: File,
    bytes: Vec<u8>,
}
impl PinnedInput {
    fn open(path: &Path, limit: u64) -> Result<Self> {
        let absolute =
            std::path::absolute(path).map_err(|_| "native_implementation_input_unavailable")?;
        for ancestor in absolute.ancestors() {
            let meta = fs::symlink_metadata(ancestor)
                .map_err(|_| "native_implementation_input_unavailable")?;
            if reparse(&meta) {
                return fail("input_not_regular");
            }
        }
        let mut options = OpenOptions::new();
        options.read(true);
        #[cfg(windows)]
        {
            use std::os::windows::fs::OpenOptionsExt;
            use windows_sys::Win32::Storage::FileSystem::{
                FILE_FLAG_OPEN_REPARSE_POINT, FILE_SHARE_READ,
            };
            options
                .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT)
                .share_mode(FILE_SHARE_READ);
        }
        #[cfg(target_os = "linux")]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.custom_flags(0o400000 | 0o4000);
        }
        let mut file = options
            .open(absolute)
            .map_err(|_| "native_implementation_input_unavailable")?;
        let meta = file
            .metadata()
            .map_err(|_| "native_implementation_input_unavailable")?;
        if !meta.is_file() || reparse(&meta) {
            return fail("input_not_regular");
        }
        if meta.len() > limit {
            return fail("input_limit");
        }
        let mut bytes = Vec::new();
        file.by_ref()
            .take(limit + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| "native_implementation_input_unavailable")?;
        if bytes.len() as u64 > limit {
            return fail("input_limit");
        }
        Ok(Self { file, bytes })
    }
    fn unchanged(&mut self) -> Result<()> {
        self.file
            .seek(SeekFrom::Start(0))
            .map_err(|_| "native_implementation_input_changed")?;
        let mut current = Vec::new();
        self.file
            .by_ref()
            .take(self.bytes.len() as u64 + 1)
            .read_to_end(&mut current)
            .map_err(|_| "native_implementation_input_changed")?;
        if current != self.bytes {
            return fail("input_changed");
        }
        Ok(())
    }
}
fn reparse(meta: &fs::Metadata) -> bool {
    #[cfg(windows)]
    {
        use std::os::windows::fs::MetadataExt;
        meta.file_attributes() & 0x400 != 0
    }
    #[cfg(not(windows))]
    {
        meta.file_type().is_symlink()
    }
}
fn validate(input: &Value) -> Result<i64> {
    shape(input, INPUT_FIELDS)?;
    if input["domain"] != INPUT
        || input["kind"] != KIND
        || !identifier(text(input, "request_id"), 96)
        || !decimal(text(input, "message_id"))
        || !decimal(text(input, "origin_todo_id"))
        || !hex(text(input, "revision"), 40)
        || ["source_receipt_sha256", "reply_sha256"]
            .iter()
            .any(|k| !hex(text(input, k), 64))
        || text(input, "project").is_empty()
        || text(input, "project").eq_ignore_ascii_case("owner")
        || text(input, "project").len() > 256
        || text(input, "project").chars().any(char::is_control)
        || ["parent_thread_id", "child_thread_id"]
            .iter()
            .any(|k| !native_id(text(input, k)))
        || input["parent_thread_id"] == input["child_thread_id"]
    {
        return fail("input_invalid");
    }
    let assignment = &input["assignment"];
    shape(assignment, &["turn_id", "item_id", "prompt_sha256"])?;
    if !native_id(text(assignment, "turn_id"))
        || !identifier(text(assignment, "item_id"), 192)
        || !hex(text(assignment, "prompt_sha256"), 64)
    {
        return fail("assignment_invalid");
    }
    let commands = input["verification_commands"]
        .as_array()
        .ok_or("native_implementation_commands_invalid")?;
    if !(1..=16).contains(&commands.len()) {
        return fail("commands_invalid");
    }
    let mut seen = std::collections::BTreeSet::new();
    for command in commands {
        shape(
            command,
            &["turn_id", "item_id", "command_sha256", "output_sha256"],
        )?;
        if !native_id(text(command, "turn_id"))
            || !identifier(text(command, "item_id"), 192)
            || ["command_sha256", "output_sha256"]
                .iter()
                .any(|k| !hex(text(command, k), 64))
            || !seen.insert((text(command, "turn_id"), text(command, "item_id")))
        {
            return fail("commands_invalid");
        }
    }
    let artifact = &input["artifact"];
    shape(artifact, &["path", "sha256"])?;
    if !Path::new(text(artifact, "path")).is_absolute()
        || text(artifact, "path").contains('\0')
        || !hex(text(artifact, "sha256"), 64)
    {
        return fail("artifact_invalid");
    }
    let evidence = &input["evidence"];
    shape(
        evidence,
        &[
            "completion",
            "execution",
            "origin_saved",
            "identity",
            "applied_at",
        ],
    )?;
    if ["completion", "execution", "origin_saved", "identity"]
        .iter()
        .any(|k| {
            let s = text(evidence, k);
            s.trim().is_empty() || s.len() > 16 * 1024 || s.contains('\0')
        })
    {
        return fail("evidence_invalid");
    }
    let applied = DateTime::parse_from_rfc3339(text(evidence, "applied_at"))
        .map_err(|_| "native_implementation_time_invalid")?;
    if applied.timestamp() <= 0 || applied > Utc::now() {
        return fail("time_invalid");
    }
    text(input, "origin_todo_id")
        .parse::<i64>()
        .map_err(|_| "native_implementation_input_invalid".into())
}
fn row_index(db: &Value, id: i64) -> Result<usize> {
    let mut rows = db["items"]
        .as_array()
        .ok_or("native_implementation_store_invalid")?
        .iter()
        .enumerate()
        .filter(|(_, v)| v["id"].as_i64() == Some(id));
    let (index, _) = rows.next().ok_or("native_implementation_origin_missing")?;
    if rows.next().is_some() {
        return fail("duplicate_origin");
    }
    Ok(index)
}
fn check_scope(db: &Value, index: usize, input: &Value, scope: &Value) -> Result<()> {
    shape(
        scope,
        &["domain", "todo_id", "project", "project_path_sha256"],
    )?;
    let project = text(input, "project");
    let root = db["projects"][project]
        .as_str()
        .ok_or("native_implementation_root_unavailable")?;
    if scope["domain"] != "discord-conversation-todo-scope-v1"
        || scope["todo_id"] != input["origin_todo_id"]
        || scope["project"] != input["project"]
        || db["items"][index]["project"] != input["project"]
        || !Path::new(root).is_absolute()
        || root.contains('\0')
        || scope["project_path_sha256"] != sha(root.as_bytes())
    {
        return fail("scope_changed");
    }
    Ok(())
}
fn check_verification(
    v: &Value,
    input: &Value,
    input_sha: &str,
    state_sha: &str,
    root_sha: &Value,
) -> Result<()> {
    shape(v, VERIFICATION_FIELDS)?;
    if v["domain"] != VERIFICATION
        || v["verified"] != true
        || v["input_sha256"] != input_sha
        || v["origin_state_sha256"] != state_sha
        || v["project_path_sha256"] != *root_sha
        || v["source_receipt_sha256"] != input["source_receipt_sha256"]
        || v["reply_sha256"] != input["reply_sha256"]
        || !hex(text(v, "native_evidence_sha256"), 64)
        || !pending(v)
    {
        return fail("verification_mismatch");
    }
    Ok(())
}
fn pending(value: &Value) -> bool {
    value["acceptance"] == "pending"
        && value["application_inferred"] == false
        && value["question_closed_get_verified"] == false
}

fn record_with(
    db: &mut Value,
    input_path: &Path,
    scope_path: &Path,
    state_path: &Path,
    verify_only: bool,
    checker: &mut dyn FnMut(&Path, &Path) -> Result<Vec<u8>>,
) -> Result<Value> {
    let mut input_file = PinnedInput::open(input_path, INPUT_LIMIT)?;
    let mut state_file = PinnedInput::open(state_path, crate::STORE_MAX_BYTES as u64)?;
    let mut scope_file = PinnedInput::open(scope_path, INPUT_LIMIT)?;
    let input = parse(&input_file.bytes)?;
    let origin_id = validate(&input)?;
    let snapshot = parse(&state_file.bytes)?;
    let scope = parse(&scope_file.bytes)?;
    let index = row_index(db, origin_id)?;
    check_scope(db, index, &input, &scope)?;
    if db["items"][index] != snapshot {
        return fail("state_changed");
    }
    let input_sha = sha(&input_file.bytes);
    let state_sha = sha(&state_file.bytes);
    let origin = &db["items"][index];
    let records = match origin.get(RECORDS) {
        Some(value) => value
            .as_array()
            .ok_or("native_implementation_records_invalid")?
            .clone(),
        None => Vec::new(),
    };
    if records.iter().any(|v| {
        v["input"]["request_id"] == input["request_id"]
            && v["input"]["message_id"] == input["message_id"]
            && v["input"]["source_receipt_sha256"] == input["source_receipt_sha256"]
            && v["input_sha256"] != input_sha
    }) {
        return fail("record_conflict");
    }
    let matches: Vec<_> = records
        .iter()
        .filter(|v| v["input_sha256"] == input_sha)
        .collect();
    if matches.len() > 1 {
        return fail("duplicate_receipt");
    }
    if verify_only && matches.is_empty() {
        return fail("record_unavailable");
    }
    let raw =
        checker(input_path, state_path).map_err(|_| "native_implementation_verifier_failed")?;
    if raw.len() as u64 > OUTPUT_LIMIT {
        return fail("verification_limit");
    }
    let verification = parse(&raw)?;
    check_verification(
        &verification,
        &input,
        &input_sha,
        &state_sha,
        &scope["project_path_sha256"],
    )?;
    input_file.unchanged()?;
    state_file.unchanged()?;
    scope_file.unchanged()?;
    let changed = matches.is_empty();
    let receipt = if let Some(saved) = matches.first() {
        shape(saved, RECEIPT_FIELDS)?;
        if saved["domain"] != RECEIPT
            || saved["kind"] != KIND
            || saved["input"] != input
            || saved["origin_scope"] != scope
            || !pending(saved)
            || !hex(text(saved, "origin_state_sha256"), 64)
            || DateTime::parse_from_rfc3339(text(saved, "recorded_at")).is_err()
        {
            return fail("record_mismatch");
        }
        check_verification(
            &saved["verification"],
            &input,
            &input_sha,
            text(saved, "origin_state_sha256"),
            &scope["project_path_sha256"],
        )?;
        if saved["verification"]["native_evidence_sha256"] != verification["native_evidence_sha256"]
        {
            return fail("native_evidence_changed");
        }
        (*saved).clone()
    } else {
        json!({"domain":RECEIPT,"kind":KIND,"input":input,"input_sha256":input_sha,
            "origin_scope":scope,"origin_state_sha256":state_sha,"verification":verification,
            "recorded_at":Utc::now().to_rfc3339(),"acceptance":"pending",
            "application_inferred":false,"question_closed_get_verified":false})
    };
    let receipt_sha = receipt_hash(&receipt)?;
    if changed {
        let mut updated = origin.clone();
        updated[RECORDS] = json!(records.into_iter().chain([receipt]).collect::<Vec<_>>());
        db["items"][index] = updated;
    }
    Ok(
        json!({"domain":RECEIPT,"kind":KIND,"recorded":true,"changed":changed,
        "input_sha256":input_sha,"receipt_sha256":receipt_sha,"acceptance":"pending",
        "application_inferred":false,"question_closed_get_verified":false}),
    )
}

struct Capture {
    file: Option<File>,
    path: PathBuf,
}
impl Capture {
    fn new(parent: &Path) -> Result<Self> {
        let path = parent.join(format!(".native-verification-{}.tmp", uuid::Uuid::new_v4()));
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create_new(true)
            .open(&path)
            .map_err(|_| "native_implementation_capture_unavailable")?;
        Ok(Self {
            file: Some(file),
            path,
        })
    }
    fn file(&mut self) -> &mut File {
        self.file.as_mut().expect("capture is owned until drop")
    }
}
impl Drop for Capture {
    fn drop(&mut self) {
        self.file.take();
        let _ = fs::remove_file(&self.path);
    }
}
struct CheckerChild {
    child: std::process::Child,
    group: cc_ask::ProcessGroup,
}
impl Drop for CheckerChild {
    fn drop(&mut self) {
        self.group.stop(&mut self.child);
        let _ = self.child.wait();
    }
}
fn run_checker_command(mut command: Command, parent: &Path, budget: Duration) -> Result<Vec<u8>> {
    let start = Instant::now();
    let mut output = Capture::new(parent)?;
    command.stdin(Stdio::null()).stderr(Stdio::null()).stdout(
        output
            .file()
            .try_clone()
            .map_err(|_| "native_implementation_capture_unavailable")?,
    );
    let (child, group) = cc_ask::ProcessGroup::spawn(&mut command)
        .map_err(|_| "native_implementation_verifier_start")?;
    drop(command);
    let mut owned = CheckerChild { child, group };
    let status = loop {
        if output
            .file()
            .metadata()
            .map_err(|_| "native_implementation_capture_unavailable")?
            .len()
            > OUTPUT_LIMIT
        {
            return fail("verification_limit");
        }
        if let Some(status) = owned
            .child
            .try_wait()
            .map_err(|_| "native_implementation_verifier_wait")?
        {
            break status;
        }
        if start.elapsed() >= budget {
            return fail("verifier_deadline");
        }
        std::thread::sleep(Duration::from_millis(10));
    };
    // This verifier has no detached children; close the owning group before
    // reading capture, including successful exits and inherited writer handles.
    drop(owned);
    if start.elapsed() >= budget {
        return fail("verifier_deadline");
    }
    if !status.success() {
        return fail("verifier_exit");
    }
    output
        .file()
        .rewind()
        .map_err(|_| "native_implementation_capture_unavailable")?;
    let mut bytes = Vec::new();
    output
        .file()
        .by_ref()
        .take(OUTPUT_LIMIT + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "native_implementation_capture_unavailable")?;
    if bytes.len() as u64 > OUTPUT_LIMIT {
        return fail("verification_limit");
    }
    Ok(bytes)
}
fn installed_verifier(input: &Path, state: &Path) -> Result<Vec<u8>> {
    if !input.is_absolute() || !state.is_absolute() {
        return fail("absolute_paths_required");
    }
    let mut command = Command::new(RUNNER);
    command
        .current_dir(
            Path::new(RUNNER)
                .parent()
                .ok_or("native_implementation_verifier_start")?,
        )
        .args([
            "conversation",
            "verify-native-implementation",
            "--input-file",
        ])
        .arg(input)
        .arg("--origin-state-file")
        .arg(state);
    run_checker_command(
        command,
        input
            .parent()
            .ok_or("native_implementation_capture_unavailable")?,
        Duration::from_secs(45),
    )
}

pub fn command(db: &mut Value, paths: &Paths, options: &Options) -> Result<Vec<String>> {
    Ok(vec![
        record(
            db,
            paths,
            &options.input_file,
            &options.origin_scope_file,
            &options.origin_state_file,
            options.verify_only,
        )?
        .to_string(),
    ])
}

fn record(
    db: &mut Value,
    paths: &Paths,
    input: &Path,
    scope: &Path,
    state: &Path,
    verify_only: bool,
) -> Result<Value> {
    let production = Paths::production();
    if paths.store != production.store || paths.lock != production.lock {
        return fail("production_store_required");
    }
    record_with(
        db,
        input,
        scope,
        state,
        verify_only,
        &mut installed_verifier,
    )
}

#[cfg(test)]
#[path = "native_implementation_tests.rs"]
mod tests;
