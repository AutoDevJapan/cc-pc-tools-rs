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
const IMAGE_LIMIT: u64 = 10 * 1024 * 1024;
const IMAGE_TOTAL_LIMIT: u64 = 23 * 1024 * 1024;
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

pub(crate) fn canonical(value: &Value) -> Value {
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
    source: PathBuf,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct MediaSource {
    domain: String,
    source_path: PathBuf,
    snapshot_path: PathBuf,
    sha256: String,
    size: u64,
}

fn image_source(path: &Path) -> Result<PathBuf> {
    let absolute = std::path::absolute(path).map_err(|_| INVALID_INPUT)?;
    for parent in absolute.ancestors() {
        let key = parent
            .file_name()
            .unwrap_or_default()
            .to_string_lossy()
            .to_lowercase();
        if key.starts_with(".env")
            || key.contains("password")
            || key.contains("secret")
            || key.contains("token")
            || matches!(
                key.as_str(),
                ".ssh" | ".aws" | ".azure" | ".gnupg" | "credentials" | "id_rsa" | "id_ed25519"
            )
        {
            return Err(INVALID_INPUT.into());
        }
        let info = fs::symlink_metadata(parent).map_err(|_| INVALID_INPUT)?;
        if info.file_type().is_symlink() {
            return Err(INVALID_INPUT.into());
        }
        #[cfg(windows)]
        {
            use std::os::windows::fs::MetadataExt;
            if info.file_attributes() & 0x400 != 0 {
                return Err(INVALID_INPUT.into());
            }
        }
    }
    fs::canonicalize(&absolute).map_err(|_| INVALID_INPUT.into())
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
    question: Option<&crate::question_images::Scope>,
) -> Result<Vec<Attachment>> {
    if paths.len() > 10 {
        return Err("新しい投稿に添付できる画像は10件までです".into());
    }
    let mut inputs = Vec::new();
    let mut total = 0u64;
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
        let source = image_source(path)?;
        let info = fs::metadata(&source).map_err(|_| INVALID_INPUT)?;
        total = total.checked_add(info.len()).ok_or(INVALID_INPUT)?;
        if !info.is_file() || info.len() > IMAGE_LIMIT || total > IMAGE_TOTAL_LIMIT {
            return Err(INVALID_INPUT.into());
        }
        inputs.push((source, extension, info.len()));
    }
    let mut result = Vec::new();
    for (source, extension, expected_size) in inputs {
        // Fresh Go verification rechecks this original scope and the immutable
        // source receipt. A generic snapshot name never grants image authority.
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
        let qualified = if guard.code == 0
            && proof["client_resource"] == true
            && proof["classification_unverified"] == true
        {
            Some(crate::question_images::preflight(
                effects,
                &source,
                question.ok_or("添付元の保護範囲を確認できません。画像は送信していません")?,
            )?)
        } else if guard.code == 0
            && proof["client_resource"] == false
            && proof["classification_unverified"] == false
        {
            None
        } else {
            return Err("添付元の保護範囲を確認できません。画像は送信していません".into());
        };
        let input = fs::File::open(&source).map_err(|_| INVALID_INPUT)?;
        let info = input.metadata().map_err(|_| INVALID_INPUT)?;
        if !info.is_file() || info.len() != expected_size || info.len() > IMAGE_LIMIT {
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
        let hash = format!("{:x}", digest.finalize());
        if qualified.is_some_and(|proof| proof.sha256 != hash || proof.size != size) {
            return Err(INVALID_INPUT.into());
        }
        snapshot.as_file().sync_all().map_err(|_| SAVE_FAILED)?;
        result.push(Attachment {
            file: snapshot,
            hash,
            size,
            extension,
            source,
        });
    }
    Ok(result)
}

fn save_media_source(
    root: &Path,
    path: &Path,
    source: &Path,
    snapshot: &Path,
    hash: &str,
    size: u64,
) -> Result<()> {
    match fs::symlink_metadata(path) {
        Ok(info) => {
            if !info.is_file() || info.file_type().is_symlink() {
                return Err(SAVE_FAILED.into());
            }
            let receipt: MediaSource =
                serde_json::from_slice(&read_bounded(path, 64 * 1024)?).map_err(|_| SAVE_FAILED)?;
            if receipt.domain != "discord-conversation-media-source-v1"
                || receipt.snapshot_path != snapshot
                || receipt.sha256 != hash
                || receipt.size != size
                || !receipt.source_path.is_absolute()
            {
                return Err(SAVE_FAILED.into());
            }
            // Same-byte retries retain the first source, which Go must verify
            // again. Renaming an input must never overwrite that provenance.
            return Ok(());
        }
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
        Err(_) => return Err(SAVE_FAILED.into()),
    }
    let receipt = json!({"domain":"discord-conversation-media-source-v1", "source_path":string_path(source)?, "snapshot_path":string_path(snapshot)?, "sha256":hash, "size":size});
    save_bytes(root, path, receipt.to_string().as_bytes())
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
        if result["phase"] == "duplicate"
            && (result["outcome"] != "existing_question_images_differ"
                || result["wait_requested"] != false
                || result.get("url").is_some_and(|url| !url.is_string()))
        {
            return Err(CHILD_FAILED.into());
        }
        if !matches!(
            result["phase"].as_str(),
            Some("posted" | "waiting" | "answered" | "applied" | "closed" | "duplicate")
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
                Some("posted" | "waiting" | "answered" | "applied" | "closed" | "duplicate")
            ),
            "delivery" => value == "verified",
            "outcome" => value == "existing_question_images_differ",
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
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(90);
    loop {
        let remaining = deadline.saturating_duration_since(std::time::Instant::now());
        if remaining.as_secs() == 0 {
            return Err(CHILD_FAILED.into());
        }
        let output = effects
            .run(args, remaining.as_secs().min(65))
            .map_err(|_| CHILD_FAILED)?;
        // Only a proven pre-operation lease collision is retried automatically.
        // The exact immutable request/arguments are retained on every attempt.
        if output.code == 2 && output.stdout.trim() == "busy" {
            std::thread::sleep(std::time::Duration::from_millis(250));
            continue;
        }
        if output.code != 0 {
            return Err(CHILD_FAILED.into());
        }
        return Ok(Some(metadata(&output.stdout, ask)?));
    }
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
    let root = home.join("conversation-bridge");
    private_directory(&root)?;
    let scope = crate::question_images::Scope::from_decision(todo, &text, &decision);
    let snapshots = attachments(&root, effects, &args.attach, scope.as_ref())?;
    let decision = canonical(&decision).to_string();
    let mut identity = json!([
        "normal-ask-v1",
        sha(text.as_bytes()),
        sha(decision.as_bytes()),
        todo,
        args.title,
        origin
    ]);
    if !snapshots.is_empty() {
        identity
            .as_array_mut()
            .ok_or(INVALID_INPUT)?
            .push(json!(snapshots
                .iter()
                .map(|file| json!([file.hash, file.extension]))
                .collect::<Vec<_>>()));
    }
    let request = format!("normal-ask-{}", sha(identity.to_string().as_bytes()));
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
    append_attachments(&root, &directory, snapshots, &mut argv)?;
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
    let snapshots = attachments(&root, effects, attach, None)?;
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
    append_attachments(&root, &directory, snapshots, &mut argv)?;
    execute(effects, &argv, false)
}

fn append_attachments(
    root: &Path,
    directory: &Path,
    snapshots: Vec<Attachment>,
    argv: &mut Vec<String>,
) -> Result<()> {
    for (index, file) in snapshots.into_iter().enumerate() {
        let path = directory.join(format!("attachment-{index}.{}", file.extension));
        install(file.file, &path, &file.hash, file.size)?;
        save_media_source(
            root,
            &directory.join(format!("attachment-{index}.source.json")),
            &file.source,
            &path,
            &file.hash,
            file.size,
        )?;
        argv.extend(["--attach".into(), string_path(&path)?]);
    }
    Ok(())
}
