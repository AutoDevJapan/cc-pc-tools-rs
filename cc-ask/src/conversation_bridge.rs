//! Opt-in transport for future normal posts. Legacy question state is never opened.
use crate::{
    ask_gate,
    commands::AskArgs,
    core::{Effects, Result},
};
use serde::Deserialize;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    fs,
    io::{Read, Write},
    path::{Path, PathBuf},
};

const DOMAIN: &str = "discord-conversation-mode-v1";
const RUNNER: &str = "D:/CC/Tools/discord-ask/unattended-runner.exe";
const INPUT_LIMIT: u64 = 1 << 20;
const INVALID_MODE: &str = "新しい質問・報告の設定が不正です。設定を有効として扱いません";
const INVALID_INPUT: &str = "新しい投稿の入力を読み取れないか、内容が不正です";
const SAVE_FAILED: &str = "新しい投稿の入力を保存できません";
const CHILD_FAILED: &str = "新しい質問・報告の処理が完了していません。同じ内容で再実行してください";
const DECISION_REQUIRED: &str =
    "新しい質問には --decision-file で元ToDoと判断範囲を指定してください";
const ORIGIN_REQUIRED: &str = "元スレッドの数値IDを --thread または判断範囲の origin_thread_id に指定し、両方の指定を一致させてください";

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Mode {
    domain: String,
    enabled: bool,
}

pub fn enabled(home: &Path) -> Result<bool> {
    let path = home.join("conversation-mode.json");
    match fs::symlink_metadata(&path) {
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(false),
        Ok(info) if info.is_file() => {}
        _ => return Err(INVALID_MODE.into()),
    }
    let bytes = read_bounded(&path, 4096).map_err(|_| INVALID_MODE)?;
    let mode: Mode = serde_json::from_slice(&bytes).map_err(|_| INVALID_MODE)?;
    if mode.domain != DOMAIN || (mode.enabled && !home.is_absolute()) {
        return Err(INVALID_MODE.into());
    }
    Ok(mode.enabled)
}

fn read_bounded(path: &Path, limit: u64) -> Result<Vec<u8>> {
    let file = fs::File::open(path).map_err(|_| INVALID_INPUT)?;
    let info = file.metadata().map_err(|_| INVALID_INPUT)?;
    if !info.is_file() || info.len() > limit {
        return Err(INVALID_INPUT.into());
    }
    let mut bytes = Vec::new();
    file.take(limit + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| INVALID_INPUT)?;
    if bytes.len() as u64 > limit {
        return Err(INVALID_INPUT.into());
    }
    Ok(bytes)
}

fn body(path: &Path) -> Result<String> {
    let text = String::from_utf8(read_bounded(path, INPUT_LIMIT)?).map_err(|_| INVALID_INPUT)?;
    Ok(text.replace("\r\n", "\n").replace('\r', "\n"))
}

fn sha(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

fn canonical(value: &Value) -> Value {
    match value {
        Value::Object(values) => {
            let sorted: std::collections::BTreeMap<_, _> = values.iter().collect();
            Value::Object(
                sorted
                    .into_iter()
                    .map(|(key, value)| (key.clone(), canonical(value)))
                    .collect(),
            )
        }
        Value::Array(values) => Value::Array(values.iter().map(canonical).collect()),
        _ => value.clone(),
    }
}

fn numeric_id(value: &str) -> bool {
    !value.is_empty()
        && value.bytes().all(|byte| byte.is_ascii_digit())
        && value.parse::<u64>().is_ok_and(|id| id != 0)
}

fn origin(args: &AskArgs, decision: &Value) -> Result<String> {
    let mut found: Option<&str> = None;
    for value in [
        decision.get("origin_thread_id"),
        decision
            .get("scope")
            .and_then(|scope| scope.get("origin_thread_id")),
    ]
    .into_iter()
    .flatten()
    {
        let id = value
            .as_str()
            .filter(|id| numeric_id(id))
            .ok_or(ORIGIN_REQUIRED)?;
        if found.is_some_and(|prior| prior != id) {
            return Err(ORIGIN_REQUIRED.into());
        }
        found = Some(id);
    }
    if let Some(id) = args.thread.as_deref() {
        if !numeric_id(id) || found.is_some_and(|prior| prior != id) {
            return Err(ORIGIN_REQUIRED.into());
        }
        found = Some(id);
    }
    found
        .map(str::to_owned)
        .ok_or_else(|| ORIGIN_REQUIRED.into())
}

struct Attachment {
    file: tempfile::NamedTempFile,
    hash: String,
    size: u64,
    extension: String,
}

fn private_directory(path: &Path) -> Result<()> {
    let mut builder = fs::DirBuilder::new();
    builder.recursive(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::DirBuilderExt;
        builder.mode(0o700);
    }
    builder.create(path).map_err(|_| SAVE_FAILED.into())
}

fn attachments(
    root: &Path,
    effects: &mut dyn Effects,
    paths: &[PathBuf],
) -> Result<Vec<Attachment>> {
    if paths.len() > 10 {
        return Err("新しい投稿に添付できる画像は10件までです".into());
    }
    let mut result = Vec::new();
    for path in paths {
        let extension = path
            .extension()
            .and_then(|ext| ext.to_str())
            .unwrap_or("")
            .to_ascii_lowercase();
        if !matches!(
            extension.as_str(),
            "png" | "jpg" | "jpeg" | "webp" | "gif" | "bmp" | "tif" | "tiff"
        ) {
            return Err("新しい投稿の添付は現在は画像だけに対応しています".into());
        }
        // A generic snapshot name must not hide the original source's protected
        // path. This preflight grants no exception to the sender's separate image
        // protection list, byte checks or individual provenance requirements.
        let source = fs::canonicalize(path).map_err(|_| INVALID_INPUT)?;
        let guard = effects
            .run(
                &[
                    "D:/CC/Tools/client-records/scrub391.exe".into(),
                    "--guard-path".into(),
                    string_path(&source)?,
                ],
                10,
            )
            .map_err(|_| "添付元の保護範囲を確認できません。画像は送信していません")?;
        let proof: Value = serde_json::from_str(&guard.stdout)
            .map_err(|_| "添付元の保護範囲を確認できません。画像は送信していません")?;
        if guard.code != 0
            || proof["client_resource"] != false
            || proof["classification_unverified"] != false
        {
            return Err("添付元の保護範囲を確認できません。画像は送信していません".into());
        }
        let input = fs::File::open(&source).map_err(|_| INVALID_INPUT)?;
        let info = input.metadata().map_err(|_| INVALID_INPUT)?;
        if !info.is_file() {
            return Err(INVALID_INPUT.into());
        }
        let mut input = input.take(info.len().checked_add(1).ok_or(INVALID_INPUT)?);
        let mut snapshot = tempfile::NamedTempFile::new_in(root).map_err(|_| SAVE_FAILED)?;
        let mut digest = Sha256::new();
        let mut size = 0u64;
        let mut buffer = [0u8; 65536];
        loop {
            let count = input.read(&mut buffer).map_err(|_| INVALID_INPUT)?;
            if count == 0 {
                break;
            }
            size += count as u64;
            if size > info.len() {
                return Err(INVALID_INPUT.into());
            }
            digest.update(&buffer[..count]);
            snapshot
                .write_all(&buffer[..count])
                .map_err(|_| SAVE_FAILED)?;
        }
        if size != info.len() {
            return Err(INVALID_INPUT.into());
        }
        snapshot.as_file().sync_all().map_err(|_| SAVE_FAILED)?;
        result.push(Attachment {
            file: snapshot,
            hash: format!("{:x}", digest.finalize()),
            size,
            extension,
        });
    }
    Ok(result)
}

fn install(file: tempfile::NamedTempFile, path: &Path, expected: &str, size: u64) -> Result<()> {
    match file.persist_noclobber(path) {
        Ok(_) => Ok(()),
        Err(error) if error.error.kind() == std::io::ErrorKind::AlreadyExists => {
            let info = fs::symlink_metadata(path).map_err(|_| SAVE_FAILED)?;
            if !info.is_file() || info.len() != size {
                return Err(SAVE_FAILED.into());
            }
            let mut input = fs::File::open(path)
                .map_err(|_| SAVE_FAILED)?
                .take(size.checked_add(1).ok_or(SAVE_FAILED)?);
            let mut digest = Sha256::new();
            let mut count = 0u64;
            let mut buffer = [0u8; 65536];
            loop {
                let n = input.read(&mut buffer).map_err(|_| SAVE_FAILED)?;
                if n == 0 {
                    break;
                }
                count += n as u64;
                digest.update(&buffer[..n]);
            }
            if count != size || format!("{:x}", digest.finalize()) != expected {
                return Err(SAVE_FAILED.into());
            }
            Ok(())
        }
        Err(_) => Err(SAVE_FAILED.into()),
    }
}

fn save_bytes(root: &Path, path: &Path, bytes: &[u8]) -> Result<()> {
    let mut file = tempfile::NamedTempFile::new_in(root).map_err(|_| SAVE_FAILED)?;
    file.write_all(bytes).map_err(|_| SAVE_FAILED)?;
    file.as_file().sync_all().map_err(|_| SAVE_FAILED)?;
    install(file, path, &sha(bytes), bytes.len() as u64)
}

fn runner_args(home: &Path, command: &str) -> Result<Vec<String>> {
    let config = home.join("unattended/config.json");
    Ok(vec![
        RUNNER.into(),
        "conversation".into(),
        "--config".into(),
        config.to_str().ok_or(INVALID_INPUT)?.into(),
        command.into(),
    ])
}

fn string_path(path: &Path) -> Result<String> {
    path.to_str()
        .map(str::to_owned)
        .ok_or_else(|| INVALID_INPUT.into())
}

// Only receipt metadata is public. In particular, private snapshot/attachment
// paths, title, raw child diagnostics, question/answer bodies and intents stay private.
fn metadata(raw: &str, ask: bool) -> Result<Value> {
    if raw.len() > INPUT_LIMIT as usize {
        return Err(CHILD_FAILED.into());
    }
    let result: Value = serde_json::from_str(raw).map_err(|_| CHILD_FAILED)?;
    let values = result.as_object().ok_or(CHILD_FAILED)?;
    let request = values
        .get("request_id")
        .and_then(Value::as_str)
        .ok_or(CHILD_FAILED)?;
    if request.is_empty()
        || request.len() > 80
        || !request
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || c == b'-' || c == b'_')
    {
        return Err(CHILD_FAILED.into());
    }
    if ask {
        if !matches!(
            result["phase"].as_str(),
            Some("posted" | "waiting" | "answered" | "applied" | "closed")
        ) {
            return Err(CHILD_FAILED.into());
        }
    } else if result["domain"] != "discord-conversation-v1" || result["delivery"] != "verified" {
        return Err(CHILD_FAILED.into());
    }
    let mut public = serde_json::Map::new();
    for (key, value) in values {
        let valid = match key.as_str() {
            "request_id" | "sender_request_id" => value.as_str().is_some_and(|v| {
                !v.is_empty()
                    && v.len() <= 80
                    && v.bytes()
                        .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
            }),
            "todo_id" => value.as_u64().is_some_and(|id| id != 0),
            "origin_thread_id"
            | "thread_id"
            | "message_id"
            | "question_message_id"
            | "progress_thread_id" => value.as_str().is_some_and(numeric_id),
            "input_sha256"
            | "question_fingerprint"
            | "decision_identity_sha256"
            | "question_body_sha256"
            | "question_receipt_sha256" => value
                .as_str()
                .is_some_and(|v| v.len() == 64 && v.bytes().all(|b| b.is_ascii_hexdigit())),
            "created_at" => value
                .as_str()
                .is_some_and(|v| chrono::DateTime::parse_from_rfc3339(v).is_ok()),
            "wait_requested" | "reused" => value.is_boolean(),
            "phase" => matches!(
                value.as_str(),
                Some("posted" | "waiting" | "answered" | "applied" | "closed")
            ),
            "delivery" => value == "verified",
            "domain" => value == "discord-conversation-v1",
            "url" => value.as_str().is_some_and(|url| {
                url.strip_prefix("https://discord.com/channels/")
                    .is_some_and(|path| {
                        let parts: Vec<_> = path.split('/').collect();
                        parts.len() == 2 && parts.iter().all(|part| numeric_id(part))
                    })
            }),
            _ => continue,
        };
        if !valid {
            return Err(CHILD_FAILED.into());
        }
        public.insert(key.clone(), value.clone());
    }
    Ok(Value::Object(public))
}

fn execute(effects: &mut dyn Effects, args: &[String], ask: bool) -> Result<Option<Value>> {
    let output = effects.run(args, 65).map_err(|_| CHILD_FAILED)?;
    if output.code != 0 {
        return Err(CHILD_FAILED.into());
    }
    Ok(Some(metadata(&output.stdout, ask)?))
}

pub fn try_ask(home: &Path, effects: &mut dyn Effects, args: &AskArgs) -> Result<Option<Value>> {
    if args.dry_run || !enabled(home)? {
        return Ok(None);
    }
    let text = body(&args.body_file)?;
    ask_gate::enforce(&args.title, &text, args.owner_only)?;
    if text.trim().is_empty() || args.title.trim().is_empty() {
        return Err(INVALID_INPUT.into());
    }
    let decision_path = args.decision_file.as_ref().ok_or(DECISION_REQUIRED)?;
    let decision: Value = serde_json::from_slice(&read_bounded(decision_path, INPUT_LIMIT)?)
        .map_err(|_| INVALID_INPUT)?;
    let todo = decision
        .get("todo_id")
        .and_then(Value::as_u64)
        .filter(|id| *id != 0)
        .ok_or(DECISION_REQUIRED)?;
    let origin = origin(args, &decision)?;
    if !args.attach.is_empty() {
        return Err("新しい質問の添付は log または post で進捗投稿へ送ってください".into());
    }
    let decision = canonical(&decision).to_string();
    let identity = json!([
        "normal-ask-v1",
        sha(text.as_bytes()),
        sha(decision.as_bytes()),
        todo,
        args.title,
        origin
    ]);
    let request = format!("normal-ask-{}", sha(identity.to_string().as_bytes()));
    let root = home.join("conversation-bridge");
    let directory = root.join(&request);
    private_directory(&directory)?;
    let body_path = directory.join("body.txt");
    let decision_path = directory.join("decision.json");
    save_bytes(&root, &body_path, text.as_bytes())?;
    save_bytes(&root, &decision_path, decision.as_bytes())?;
    let mut argv = runner_args(home, "ask")?;
    argv.extend([
        "--request-id".into(),
        request,
        "--todo-id".into(),
        todo.to_string(),
        "--origin-thread".into(),
        origin,
        "--body-file".into(),
        string_path(&body_path)?,
        "--decision-file".into(),
        string_path(&decision_path)?,
        "--title".into(),
        args.title.clone(),
        "--wait".into(),
    ]);
    execute(effects, &argv, true)
}

pub fn try_log(
    home: &Path,
    effects: &mut dyn Effects,
    topic: &str,
    text: &str,
    body_file: Option<&Path>,
    notify: bool,
    attach: &[PathBuf],
) -> Result<Option<Value>> {
    if !enabled(home)? {
        return Ok(None);
    }
    let canonical = crate::channel_reports::canonical(topic);
    let title = canonical.unwrap_or(topic);
    let text = match body_file {
        Some(path) => body(path)?.trim().to_owned(),
        None => text.into(),
    };
    send_log(
        home,
        effects,
        title,
        &text,
        None,
        notify && canonical.is_none(),
        attach,
    )
}

pub fn try_post(
    home: &Path,
    effects: &mut dyn Effects,
    thread: &str,
    body_file: &Path,
    notify: bool,
    attach: &[PathBuf],
) -> Result<Option<Value>> {
    if !enabled(home)? {
        return Ok(None);
    }
    if !numeric_id(thread) {
        return Err(ORIGIN_REQUIRED.into());
    }
    send_log(
        home,
        effects,
        "既存の作業への報告",
        &body(body_file)?,
        Some(thread),
        notify,
        attach,
    )
}

fn send_log(
    home: &Path,
    effects: &mut dyn Effects,
    title: &str,
    text: &str,
    thread: Option<&str>,
    notify: bool,
    attach: &[PathBuf],
) -> Result<Option<Value>> {
    if title.trim().is_empty()
        || text.len() as u64 > INPUT_LIMIT
        || (text.trim().is_empty() && attach.is_empty())
    {
        return Err(INVALID_INPUT.into());
    }
    let root = home.join("conversation-bridge");
    private_directory(&root)?;
    let snapshots = attachments(&root, effects, attach)?;
    let files: Vec<_> = snapshots
        .iter()
        .map(|file| json!([file.hash, file.extension]))
        .collect();
    let identity = json!([
        "normal-log-v1",
        title,
        sha(text.as_bytes()),
        thread,
        notify,
        files
    ]);
    let request = format!("normal-log-{}", sha(identity.to_string().as_bytes()));
    let directory = root.join(&request);
    private_directory(&directory)?;
    let body_path = directory.join("body.txt");
    save_bytes(&root, &body_path, text.as_bytes())?;
    let mut argv = runner_args(home, "log")?;
    argv.extend([
        "--request-id".into(),
        request,
        "--title".into(),
        title.into(),
        "--body-file".into(),
        string_path(&body_path)?,
    ]);
    if let Some(thread) = thread {
        argv.extend(["--thread".into(), thread.into()]);
    }
    if notify {
        argv.push("--notify".into());
    }
    for (index, file) in snapshots.into_iter().enumerate() {
        let path = directory.join(format!("attachment-{index}.{}", file.extension));
        install(file.file, &path, &file.hash, file.size)?;
        argv.extend(["--attach".into(), string_path(&path)?]);
    }
    execute(effects, &argv, false)
}
