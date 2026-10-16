//! Reserve and bind the existing finite product retry without a destructive release/start gap.
use crate::{Lock, Paths, Result, find, load, save};
use cc_job_policy::{cancellation, product_retry as policy};
use cc_scheduler::{
    backend::{Backend, RealBackend},
    common::{assign_worker, s},
    policy as processes,
};
use serde_json::{Value, json};
use std::{
    fs,
    io::{Read, Write},
    path::{Path, PathBuf},
    time::Duration,
};

pub fn stage(
    db: &mut Value,
    proof: &Value,
    source: &Value,
    key: &str,
    prompt_file: &str,
    prompt_digest: &str,
    at: u64,
) -> Result<Value> {
    let worker = source["id"]
        .as_str()
        .filter(|id| crate::job_refs::valid_id(id))
        .ok_or("product_retry_source_invalid")?;
    if !policy::valid_proof(proof, at) {
        return Err("product_retry_qualification_invalid".into());
    }
    let row = policy::candidate(db, at).ok_or("product_retry_not_due")?;
    if proof["snapshot"] != crate::product_resume::snapshot(db, row)
        || !row["jobs"].is_array()
        || (row.get("product_retry_history").is_some() && !row["product_retry_history"].is_array())
    {
        return Err("product_retry_snapshot_changed".into());
    }
    if policy::key(&row["wait_ready"]).as_deref() != Some(key)
        || row["worker_receipt"]["job"] != worker
        || source["todo_id"] != policy::ID
        || !policy::same_path(&source["cwd"], &json!(policy::TREE))
        || cancellation::owner_stopped(row, source)
        || source["retry_live"] != false
        || !matches!(
            s(&source["status"]),
            "succeeded" | "failed" | "lost" | "cancelled" | "canceled"
        )
    {
        return Err("product_retry_source_changed".into());
    }
    if source["sandbox"] != "danger-full-access"
        && source["sandbox"] != "workspace-write"
        && source["sandbox"] != "read-only"
    {
        return Err("product_retry_source_settings_invalid".into());
    }
    for field in ["model", "effort", "mcp_profile"] {
        if source[field].as_str().is_none_or(str::is_empty) {
            return Err("product_retry_source_settings_invalid".into());
        }
    }
    if !source["network"].is_boolean()
        || !source["add_dirs"]
            .as_array()
            .is_some_and(|dirs| dirs.iter().all(Value::is_string))
    {
        return Err("product_retry_source_settings_invalid".into());
    }
    if ["model", "effort"]
        .iter()
        .any(|field| !row[*field].is_null() && row[*field] != source[*field])
    {
        return Err("product_retry_explicit_settings_changed".into());
    }
    let identity = source_identity(source).ok_or("product_retry_source_settings_invalid")?;
    let old = &row["product_retry_intent"];
    if old["key"] == key {
        if old["snapshot"] != policy::snapshot(row)
            || old["source"] != identity
            || !old["bound_job"].is_null()
            || !policy::valid_proof(&old["proof"], at)
            || old["proof"]["expires_at"] != proof["expires_at"]
        {
            return Err("product_retry_intent_changed".into());
        }
        return Ok(report(old, false));
    }
    let intent = json!({"version":1,"key":key,"reserved_at":at,"proof":proof,"snapshot":policy::snapshot(row),
        "source":identity,
        "prompt_file":prompt_file,"prompt_sha256":prompt_digest,"bound_job":null});
    let row = find(db, policy::ID as i64)?;
    if row["product_retry_intent"].is_object() {
        let previous = row["product_retry_intent"].clone();
        cc_scheduler::common::push(&mut row["product_retry_history"], previous);
    }
    row["product_retry_intent"] = intent.clone();
    row["updated"] = json!(crate::now());
    Ok(report(&intent, true))
}

pub fn source_identity(source: &Value) -> Option<Value> {
    let session = ["thread_id", "resume_session"].iter().find_map(|field| {
        source[*field]
            .as_str()
            .filter(|session| processes::valid_session(session))
    })?;
    let scope = source["scope_source"].as_str().unwrap_or("launcher");
    ["launcher","owner_marker","owner_request"].contains(&scope).then(||json!({"id":source["id"],"session":session,
        "cwd":source["cwd"],"model":source["model"],"effort":source["effort"],"sandbox":source["sandbox"],"mcp_profile":source["mcp_profile"],
        "scope_source":scope,"network":source["network"],"add_dirs":source["add_dirs"]}))
}

fn report(intent: &Value, saved: bool) -> Value {
    let source = &intent["source"];
    let mut args = vec![
        "run".to_owned(),
        "--name".into(),
        "original-product-retry-1626".into(),
    ];
    for (flag, value) in [
        ("--model", s(&source["model"])),
        ("--effort", s(&source["effort"])),
        ("--sandbox", s(&source["sandbox"])),
        ("--cwd", s(&source["cwd"])),
        ("--mcp-profile", s(&source["mcp_profile"])),
        ("--prompt-file", s(&intent["prompt_file"])),
        ("--launch-key", s(&intent["key"])),
        ("--original-product-retry-key", s(&intent["key"])),
        ("--resume-session", s(&source["session"])),
        ("--scope-source", s(&source["scope_source"])),
    ] {
        args.extend([flag.to_owned(), value.to_owned()]);
    }
    args.extend([
        "--todo-id".into(),
        policy::ID.to_string(),
        "--project".into(),
        "PhosGaea-Vol1".into(),
        "--task-type".into(),
        "implementation".into(),
    ]);
    if source["network"] == true {
        args.push("--network".into());
    }
    for dir in source["add_dirs"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
    {
        args.extend(["--add-dir".into(), dir.to_owned()]);
    }
    json!({"id":policy::ID,"reserved":true,"saved":saved,"key":intent["key"],"launched":0,"wait_preserved":true,
        "run_executable":"D:/CC/Tools/codex-jobs/codex_jobs.exe","run_args":args})
}

pub fn bind(db: &mut Value, job: &Value, key: &str, at: u64) -> Result<Value> {
    let row = policy::row(db).ok_or("product_retry_item_missing")?;
    let intent = row["product_retry_intent"].clone();
    if cancellation::owner_stopped(row, job) || job["cancel_requested"] == true {
        return Err("product_retry_job_stopped".into());
    }
    if intent["key"] != key || !policy::valid_proof(&intent["proof"], at) {
        return Err("product_retry_job_differs".into());
    }
    if intent["bound_job"] == job["id"] {
        if policy::admission_reason(db, job, at).is_some() {
            return Err("product_retry_binding_changed".into());
        }
        return Ok(
            json!({"id":policy::ID,"bound":true,"reused":true,"job":job["id"],"launched":0}),
        );
    }
    if !policy::job_matches(job, &intent) {
        return Err("product_retry_job_differs".into());
    }
    let row = policy::candidate(db, at).ok_or("product_retry_original_wait_changed")?;
    if intent["snapshot"] != policy::snapshot(row)
        || !intent["bound_job"].is_null()
        || job["status"] != "queued"
        || !job["started_at"].is_null()
        || !job["codex_pid"].is_null()
        || !job["id"].as_str().is_some_and(crate::job_refs::valid_id)
    {
        return Err("product_retry_original_wait_changed".into());
    }
    let row = find(db, policy::ID as i64)?;
    assign_worker(row, s(&job["id"]));
    row["status"] = json!("doing");
    if !row["jobs"]
        .as_array()
        .is_some_and(|ids| ids.contains(&job["id"]))
    {
        cc_scheduler::common::push(&mut row["jobs"], job["id"].clone());
    }
    row.as_object_mut().unwrap().remove("wait_ready");
    row.as_object_mut().unwrap().remove("wait_on"); // candidate validated every dependency as done.
    row["scheduler"]["resume_session"] = intent["source"]["session"].clone();
    row["product_retry_intent"]["bound_job"] = job["id"].clone();
    row["product_retry_intent"]["bound_generation"] = row["resume_generation"].clone();
    row["updated"] = json!(crate::now());
    Ok(json!({"id":policy::ID,"bound":true,"reused":false,"job":job["id"],"launched":0}))
}

pub fn continue_queued(db: &mut Value, job: &Value, key: &str, at: u64) -> Result<Value> {
    let row = policy::row(db).ok_or("product_retry_item_missing")?;
    let intent = &row["product_retry_intent"];
    if intent["bound_job"] != job["id"] || job["todo_id"] != policy::ID {
        return Err("product_retry_not_a_continuation".into());
    }
    if intent["continuation_waits"]
        .as_array()
        .is_some_and(|waits| {
            waits
                .iter()
                .any(|wait| wait["key"] == key && wait["job"] == job["id"])
        })
        && policy::admission_reason(db, job, at).is_none()
    {
        return Ok(
            json!({"id":policy::ID,"continued_existing":true,"saved":false,"job":job["id"],"launched":0}),
        );
    }
    if policy::candidate(db, at).is_none()
        || policy::key(&row["wait_ready"]).as_deref() != Some(key)
        || job["status"] != "queued"
        || !job["started_at"].is_null()
        || !job["codex_pid"].is_null()
        || (intent.get("continuation_waits").is_some() && !intent["continuation_waits"].is_array())
    {
        return Err("product_retry_continuation_wait_changed".into());
    }
    let wait = row["wait_ready"].clone();
    let mut proposed = db.clone();
    let row = find(&mut proposed, policy::ID as i64)?;
    row["status"] = json!("doing");
    row.as_object_mut().unwrap().remove("wait_ready");
    row.as_object_mut().unwrap().remove("wait_on");
    if policy::admission_reason(&proposed, job, at).is_some() {
        return Err("product_retry_continuation_scope_changed".into());
    }
    let row = find(&mut proposed, policy::ID as i64)?;
    cc_scheduler::common::push(
        &mut row["product_retry_intent"]["continuation_waits"],
        json!({"key":key,"wait":wait,"job":job["id"],"at":at}),
    );
    row["updated"] = json!(crate::now());
    *find(db, policy::ID as i64)? = row.clone();
    Ok(json!({"id":policy::ID,"continued_existing":true,"saved":true,"job":job["id"],"launched":0}))
}

fn guard() -> Result<Value> {
    let proof = cc_ask::core::run_hidden_json(
        &[
            "D:/CC/Tools/client-records/scrub391.exe".into(),
            "--verify-product-resume".into(),
            "1626".into(),
        ],
        8,
    )
    .map_err(|_| "product_retry_guard_unavailable")?;
    if !policy::valid_proof(&proof, crate::time_s() as u64) {
        return Err("product_retry_guard_unverified".into());
    }
    Ok(proof)
}

fn prompt_bytes(file: &Path) -> Result<Vec<u8>> {
    let mut bytes = Vec::new();
    fs::File::open(file)
        .and_then(|file| file.take(65537).read_to_end(&mut bytes))
        .map_err(|_| "product_retry_prompt_unavailable")?;
    if bytes.len() > 65536 {
        return Err("product_retry_prompt_too_large".into());
    }
    Ok(bytes)
}

fn prepare_prompt(
    folder: &Path,
    key: &str,
    intent: &Value,
    template: &[u8],
) -> Result<(PathBuf, String)> {
    let replay = intent["key"] == key;
    let digest = if replay {
        intent["prompt_sha256"]
            .as_str()
            .filter(|hash| hash.len() == 64 && hash.bytes().all(|b| b.is_ascii_hexdigit()))
            .ok_or("product_retry_prompt_changed")?
            .to_owned()
    } else {
        policy::digest(template)
    };
    let file = folder.join(format!("{key}-{}.md", &digest[..16]));
    if replay {
        if !policy::same_path(&intent["prompt_file"], &json!(file.to_string_lossy()))
            || policy::digest(&prompt_bytes(&file)?) != digest
        {
            return Err("product_retry_prompt_changed".into());
        }
    } else {
        fs::create_dir_all(folder).map_err(|_| "product_retry_prompt_unavailable")?;
        match fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&file)
        {
            Ok(mut output) => {
                output
                    .write_all(template)
                    .map_err(|_| "product_retry_prompt_unavailable")?;
                output
                    .sync_all()
                    .map_err(|_| "product_retry_prompt_unavailable")?;
            }
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
                if policy::digest(&prompt_bytes(&file)?) != digest {
                    return Err("product_retry_prompt_changed".into());
                }
            }
            Err(_) => return Err("product_retry_prompt_unavailable".into()),
        }
    }
    Ok((file, digest))
}

const CONTINUATION: &str = "元のToDo1626を、期限に達した通常の技術再試行として、同じセッション・同じ作業場所・同じ許可範囲から続行してください。自身の実jobと元ToDoのproduct_retry_intent・期限付きproduct-resume資格を照合し、正規APIで保存・結合した引継ぎ証拠を保持してください。起動済みの担当や完了した操作を重複させず、共有ToDoの最新記録・以前の成果・本人の既存回答を読み、元の未完工程を実装と検証まで進めてください。原資格や期限を延長せず、新費用・APIキー・本人のPC操作要求・範囲外の資源へ広げないでください。\n";

pub fn run(
    paths: &Paths,
    key: &str,
    worker: Option<&str>,
    accepted_job: Option<&str>,
) -> Result<Value> {
    if paths.store != Paths::production().store
        || paths.jobs != Paths::production().jobs
        || key.len() != 32
        || !key
            .bytes()
            .all(|v| v.is_ascii_digit() || (b'a'..=b'f').contains(&v))
        || worker.is_some_and(|id| !crate::job_refs::valid_id(id))
        || accepted_job.is_some_and(|id| !crate::job_refs::valid_id(id))
    {
        return Err("product_retry_input_invalid".into());
    }
    let proof = guard()?; // Never call an external verifier while holding queue or ToDo locks.
    let _queue = Lock::acquire(&paths.jobs.join(".queue.lock"), Duration::from_secs(30))?;
    let _job = accepted_job
        .or(worker)
        .map(|id| {
            Lock::acquire(
                &paths.jobs.join(id).join("state.lock"),
                Duration::from_secs(30),
            )
        })
        .transpose()?;
    let mut backend = RealBackend::new(PathBuf::from("D:/CC/Tools/scheduler"));
    let ps = backend
        .snapshot()
        .map_err(|_| "product_retry_processes_unavailable")?;
    let mut jobs = backend
        .jobs()
        .map_err(|_| "product_retry_jobs_unavailable")?;
    let at = crate::time_s() as u64;
    for job in &mut jobs {
        job["retry_live"] = json!(processes::live(job) || processes::alive(job, &ps, at as f64));
    }
    let _todo = Lock::acquire(&paths.lock, Duration::from_secs(30))?;
    let mut db = load(&paths.store)?;
    let row = policy::row(&db)
        .ok_or("product_retry_item_missing")?
        .clone();
    if proof["snapshot"] != crate::product_resume::snapshot(&db, &row) {
        return Err("product_retry_snapshot_changed".into());
    }
    let source_id = worker
        .or_else(|| row["product_retry_intent"]["source"]["id"].as_str())
        .ok_or("product_retry_source_missing")?
        .to_owned();
    let source = jobs
        .iter()
        .find(|job| job["id"] == source_id)
        .ok_or("product_retry_source_missing")?;
    if accepted_job.is_none() && row["product_retry_intent"]["bound_job"] == source["id"] {
        if jobs.iter().any(|other| {
            other["id"] != source["id"]
                && other["retry_live"] == true
                && (other["todo_id"] == policy::ID
                    || policy::same_path(&other["cwd"], &json!(policy::TREE)))
        }) {
            return Err("product_retry_other_worker_active".into());
        }
        let result = continue_queued(&mut db, source, key, at)?;
        if result["saved"] == true {
            save(&paths.store, &db)?;
        }
        return Ok(result);
    }
    if source["retry_live"] != false || cancellation::owner_stopped(&row, source) {
        return Err("product_retry_source_running_or_stopped".into());
    }
    let session = ["thread_id", "resume_session"]
        .iter()
        .find_map(|field| {
            source[*field]
                .as_str()
                .filter(|session| processes::valid_session(session))
        })
        .ok_or("product_retry_session_missing")?;
    let intent = &row["product_retry_intent"];
    if intent["key"] == key && source_identity(source).as_ref() != Some(&intent["source"]) {
        return Err("product_retry_source_settings_changed".into());
    }
    let pending: Vec<_> = jobs
        .iter()
        .filter(|job| {
            job["retry_live"] == true
                && (job["todo_id"] == policy::ID
                    || job["related_todo_ids"]
                        .as_array()
                        .is_some_and(|ids| ids.contains(&json!(policy::ID)))
                    || ["thread_id", "resume_session"]
                        .iter()
                        .any(|field| job[*field] == session)
                    || policy::same_path(&job["cwd"], &json!(policy::TREE)))
        })
        .collect();
    if pending.iter().any(|job| {
        !policy::job_matches(job, intent)
            || (intent["bound_job"] != job["id"]
                && (job["status"] != "queued"
                    || !job["started_at"].is_null()
                    || !job["codex_pid"].is_null()))
    }) || pending.len() > 1
    {
        return Err("product_retry_other_worker_active".into());
    }
    let result = if let Some(jid) = accepted_job {
        let job = jobs
            .iter()
            .find(|job| job["id"] == jid)
            .ok_or("product_retry_accepted_job_missing")?;
        let bytes = prompt_bytes(&paths.jobs.join(jid).join("prompt.md"))?;
        if policy::digest(&bytes) != intent["prompt_sha256"] {
            return Err("product_retry_prompt_changed".into());
        }
        bind(&mut db, job, key, at)?
    } else {
        let folder = paths
            .store
            .parent()
            .ok_or("product_retry_paths_invalid")?
            .join("product-retry-prompts");
        let (file, prompt_digest) = prepare_prompt(&folder, key, intent, CONTINUATION.as_bytes())?;
        if intent["key"] != key {
            crate::product_resume::qualify(&mut db, policy::ID as i64, &proof, true)?;
        }
        stage(
            &mut db,
            &proof,
            source,
            key,
            &file.to_string_lossy(),
            &prompt_digest,
            at,
        )?
    };
    if result["saved"] == true || (result["bound"] == true && result["reused"] == false) {
        save(&paths.store, &db)?;
    }
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn a_replayed_intent_keeps_its_committed_prompt_across_tool_updates() {
        let temp = tempfile::tempdir().unwrap();
        let key = "a".repeat(32);
        let (file, digest) =
            prepare_prompt(temp.path(), &key, &Value::Null, b"old committed version").unwrap();
        let intent = json!({"key":key,"prompt_file":file,"prompt_sha256":digest});
        let (same, same_digest) =
            prepare_prompt(temp.path(), &key, &intent, b"new tool version").unwrap();
        assert_eq!(same, file);
        assert_eq!(same_digest, digest);
        assert_eq!(fs::read(&same).unwrap(), b"old committed version");
        fs::remove_file(&file).unwrap();
        assert!(prepare_prompt(temp.path(), &key, &intent, b"new tool version").is_err());
        assert!(!file.exists());
    }
}
