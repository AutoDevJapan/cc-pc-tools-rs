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

#[derive(Debug,Clone)]
pub struct AttemptPlan {pub base_key:String,pub key:String,pub attempt:u64,pub retire:bool}
pub fn attempt_plan(row:&Value,base:&str,retire:bool)->Result<AttemptPlan>{
    let old=&row["product_retry_intent"];
    let same=policy::base_key(old)==base;
    let old_attempt=if old.get("attempt").is_some(){old["attempt"].as_u64().filter(|n|*n<=2).ok_or("product_retry_attempt_invalid")?}else{0};
    let attempt=if same {old_attempt+u64::from(retire)}else{0};
    let key=policy::attempt_key(base,attempt).ok_or("product_retry_attempts_exhausted")?;
    Ok(AttemptPlan{base_key:base.to_owned(),key,attempt,retire})
}
pub fn stage(
    db: &mut Value,
    proof: &Value,
    source: &Value,
    key: &str,
    prompt_file: &str,
    prompt_digest: &str,
    at: u64,
) -> Result<Value> {
    let row=policy::row(db).ok_or("product_retry_item_missing")?;
    let plan=attempt_plan(row,key,false)?;
    stage_attempt(db,proof,source,&plan,prompt_file,prompt_digest,at)
}
pub fn stage_attempt(db:&mut Value,proof:&Value,source:&Value,plan:&AttemptPlan,prompt_file:&str,prompt_digest:&str,at:u64)->Result<Value>{
    let key=plan.key.as_str();
    let worker = source["id"]
        .as_str()
        .filter(|id| crate::job_refs::valid_id(id))
        .ok_or("product_retry_source_invalid")?;
    if !policy::valid_proof(proof, at) {
        return Err("product_retry_qualification_invalid".into());
    }
    let row = policy::candidate(db, at).ok_or("product_retry_not_due")?;
    let expected=attempt_plan(row,&plan.base_key,plan.retire)?;
    if expected.key!=plan.key || expected.attempt!=plan.attempt {return Err("product_retry_attempt_invalid".into());}
    if proof["snapshot"] != crate::product_resume::snapshot(db, row)
        || !row["jobs"].is_array()
        || (row.get("product_retry_history").is_some() && !row["product_retry_history"].is_array())
    {
        return Err("product_retry_snapshot_changed".into());
    }
    if policy::key(&row["wait_ready"]).as_deref() != Some(plan.base_key.as_str())
        || policy::attempt_key(&plan.base_key,plan.attempt).as_deref()!=Some(key)
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
    if policy::base_key(old).as_str()==Some(plan.base_key.as_str()) && !plan.retire {
        if old["snapshot"] != policy::snapshot(row)
            || old["source"] != identity
            || !old["bound_job"].is_null()
            || !policy::valid_proof(&old["proof"], at)
            || old["proof"]["expires_at"] != proof["expires_at"]
            || old["key"]!=key
        {
            return Err("product_retry_intent_changed".into());
        }
        return Ok(report(old, false));
    }
    if plan.retire && (!old["bound_job"].is_null() || policy::base_key(old).as_str()!=Some(plan.base_key.as_str())) {return Err("product_retry_attempt_cannot_retire".into());}
    let intent = json!({"version":1,"base_key":plan.base_key,"key":key,"attempt":plan.attempt,"reserved_at":at,"proof":proof,"snapshot":policy::snapshot(row),
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
    json!({"id":policy::ID,"reserved":true,"saved":saved,"key":intent["key"],"base_key":policy::base_key(intent),"attempt":intent["attempt"],"launched":0,"wait_preserved":true,
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
        let reason=policy::admission_reason(db,job,at);
        if reason.as_ref().is_some_and(|reason|reason["terminal"]==true) {
            return Err("product_retry_binding_changed".into());
        }
        return Ok(
            json!({"id":policy::ID,"bound":true,"reused":true,"deferred":reason.is_some(),"job":job["id"],"launched":0}),
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

#[path = "product_retry_runtime.rs"]
mod runtime;

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
    if policy::attempt_key(key, 0).is_none() {
        return Err("product_retry_input_invalid".into());
    }
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
    if replay {
        let saved = Path::new(
            intent["prompt_file"]
                .as_str()
                .ok_or("product_retry_prompt_changed")?,
        );
        let file = runtime::physical(saved)?;
        let expected = runtime::physical(folder)?.join(format!("{key}-{}.md", &digest[..16]));
        if file != expected || policy::digest(&prompt_bytes(&file)?) != digest {
            return Err("product_retry_prompt_changed".into());
        }
        return Ok((file, digest));
    }
    if template.len() > 65536 {
        return Err("product_retry_prompt_too_large".into());
    }
    fs::create_dir_all(folder).map_err(|_| "product_retry_prompt_unavailable")?;
    let folder = runtime::physical(folder)?;
    let file = folder.join(format!("{key}-{}.md", &digest[..16]));
    // A crash can leave only a random, unpublished temp. It cannot poison the
    // deterministic destination used by the next reservation attempt.
    struct Temporary(PathBuf);
    impl Drop for Temporary {
        fn drop(&mut self) {
            let _ = fs::remove_file(&self.0);
        }
    }
    let temporary = Temporary(folder.join(format!(".{key}-{}.tmp", uuid::Uuid::new_v4())));
    let mut output = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&temporary.0)
        .map_err(|_| "product_retry_prompt_unavailable")?;
    output
        .write_all(template)
        .and_then(|_| output.sync_all())
        .map_err(|_| "product_retry_prompt_unavailable")?;
    drop(output);
    match fs::hard_link(&temporary.0, &file) {
        Ok(()) => {}
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {}
        Err(_) => return Err("product_retry_prompt_unavailable".into()),
    }
    let physical = runtime::physical(&file)?;
    if physical != file || policy::digest(&prompt_bytes(&physical)?) != digest {
        return Err("product_retry_prompt_changed".into());
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
    let fixed = Paths::production();
    if paths.store != fixed.store || paths.jobs != fixed.jobs || paths.lock != fixed.lock {
        return Err("product_retry_input_invalid".into());
    }
    runtime::run(
        paths,
        key,
        worker,
        accepted_job,
        &mut runtime::Production::new(),
    )
}

#[cfg(test)]
#[path = "product_retry_tests.rs"]
mod tests;
