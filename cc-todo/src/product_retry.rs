//! Reserve and bind the existing finite product retry without a destructive release/start gap.
use crate::{Lock, Paths, Result, find, load, save};
use cc_job_policy::{cancellation, product_retry as policy};
use cc_scheduler::{
    backend::{Backend, RealBackend},
    common::{assign_worker, s},
    policy as processes,
};
use serde_json::{Value, json};
use std::{fs, io::Write, path::PathBuf, process::Command, time::Duration};

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
        || source["id"] != worker
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
    let session = ["thread_id", "resume_session"]
        .iter()
        .filter_map(|key| source[*key].as_str())
        .find(|session| processes::valid_session(session))
        .ok_or("product_retry_session_missing")?;
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
    let old = &row["product_retry_intent"];
    if old["key"] == key {
        if old["snapshot"] != policy::snapshot(row)
            || old["source"]["id"] != worker
            || !old["bound_job"].is_null()
            || !policy::valid_proof(&old["proof"], at)
            || old["proof"]["expires_at"] != proof["expires_at"]
        {
            return Err("product_retry_intent_changed".into());
        }
        return Ok(report(old, false));
    }
    let scope = source["scope_source"].as_str().unwrap_or("launcher");
    if !["launcher", "owner_marker", "owner_request"].contains(&scope) {
        return Err("product_retry_source_settings_invalid".into());
    }
    let intent = json!({"version":1,"key":key,"reserved_at":at,"proof":proof,"snapshot":policy::snapshot(row),
        "source":{"id":worker,"session":session,"cwd":source["cwd"],"model":source["model"],"effort":source["effort"],
            "sandbox":source["sandbox"],"mcp_profile":source["mcp_profile"],"scope_source":scope,
            "network":source["network"],"add_dirs":source["add_dirs"]},
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
    if intent["key"] != key
        || !policy::valid_proof(&intent["proof"], at)
        || !policy::job_matches(job, &intent)
    {
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

fn guard() -> Result<Value> {
    let mut command = Command::new("D:/CC/Tools/client-records/scrub391.exe");
    command.args(["--verify-product-resume", "1626"]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let output = command
        .output()
        .map_err(|_| "product_retry_guard_unavailable")?;
    let proof: Value =
        serde_json::from_slice(&output.stdout).map_err(|_| "product_retry_guard_invalid")?;
    if !output.status.success() || !policy::valid_proof(&proof, crate::time_s() as u64) {
        return Err("product_retry_guard_unverified".into());
    }
    Ok(proof)
}

const CONTINUATION: &str = "元のToDo1626を、期限に達した通常の技術再試行として、同じセッション・同じ作業場所・同じ許可範囲から続行してください。起動済みの担当や完了した操作を重複させず、共有ToDoの最新記録・以前の成果・本人の既存回答を読み、元の未完工程を実装と検証まで進めてください。原資格や期限を延長せず、新費用・APIキー・本人のPC操作要求・範囲外の資源へ広げないでください。\n";

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
    if accepted_job.is_none() {
        let current = load(&paths.store)?;
        if policy::row(&current).is_none_or(|row| row["product_retry_intent"]["key"] != key) {
            let qualified = crate::product_resume::run(paths, policy::ID as i64, true)?;
            if qualified["qualified"] != true {
                return Err("product_retry_qualification_unavailable".into());
            }
        }
    }
    let proof = guard()?; // Never call an external verifier while holding queue or ToDo locks.
    let _queue = Lock::acquire(
        &paths
            .jobs
            .parent()
            .ok_or("product_retry_paths_invalid")?
            .join("queue.lock"),
        Duration::from_secs(30),
    )?;
    let _job = accepted_job
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
        let bytes = fs::read(paths.jobs.join(jid).join("prompt.md"))
            .map_err(|_| "product_retry_prompt_unavailable")?;
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
        fs::create_dir_all(&folder).map_err(|_| "product_retry_prompt_unavailable")?;
        let file = folder.join(format!("{key}.md"));
        match fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&file)
        {
            Ok(mut file) => {
                file.write_all(CONTINUATION.as_bytes())
                    .map_err(|_| "product_retry_prompt_unavailable")?;
                file.sync_all()
                    .map_err(|_| "product_retry_prompt_unavailable")?;
            }
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
                if fs::read(&file).map_err(|_| "product_retry_prompt_unavailable")?
                    != CONTINUATION.as_bytes()
                {
                    return Err("product_retry_prompt_changed".into());
                }
            }
            Err(_) => return Err("product_retry_prompt_unavailable".into()),
        }
        stage(
            &mut db,
            &proof,
            source,
            key,
            &file.to_string_lossy(),
            &policy::digest(CONTINUATION.as_bytes()),
            at,
        )?
    };
    if result["saved"] == true || (result["bound"] == true && result["reused"] == false) {
        save(&paths.store, &db)?;
    }
    Ok(result)
}
