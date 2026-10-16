//! Private transaction kernel. CLI callers can only select the production runtime.
use super::*;
use std::collections::BTreeSet;

pub(super) trait Runtime {
    fn guard(&mut self) -> Result<Value>;
    fn jobs(&mut self) -> Result<Vec<Value>>;
    fn snapshot(&mut self) -> Result<processes::Processes>;
    fn process_birth(&mut self, pid: u32) -> Result<Option<u64>>;
    fn clock(&self) -> u64;
}

pub(super) struct Production(RealBackend);
impl Production {
    pub(super) fn new() -> Self {
        Self(RealBackend::new(PathBuf::from("D:/CC/Tools/scheduler")))
    }
}
impl Runtime for Production {
    fn guard(&mut self) -> Result<Value> {
        super::guard()
    }
    fn jobs(&mut self) -> Result<Vec<Value>> {
        self.0
            .jobs()
            .map_err(|_| "product_retry_jobs_unavailable".into())
    }
    fn snapshot(&mut self) -> Result<processes::Processes> {
        self.0
            .snapshot()
            .map_err(|_| "product_retry_processes_unavailable".into())
    }
    fn process_birth(&mut self, pid: u32) -> Result<Option<u64>> {
        process_birth(pid).map_err(|_| "product_retry_process_identity_unavailable".into())
    }
    fn clock(&self) -> u64 {
        crate::time_s() as u64
    }
}

// Query failures are unknown, never evidence that the recorded process is gone.
#[cfg(windows)]
fn process_birth(pid: u32) -> std::io::Result<Option<u64>> {
    use windows_sys::Win32::{Foundation::*, System::Threading::*};
    unsafe {
        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if handle.is_null() {
            let error = std::io::Error::last_os_error();
            return if error.raw_os_error() == Some(ERROR_INVALID_PARAMETER as i32) {
                Ok(None)
            } else {
                Err(error)
            };
        }
        struct Handle(HANDLE);
        impl Drop for Handle {
            fn drop(&mut self) {
                unsafe { CloseHandle(self.0) };
            }
        }
        let handle = Handle(handle);
        let mut times = [FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        }; 4];
        if GetProcessTimes(
            handle.0,
            &mut times[0],
            &mut times[1],
            &mut times[2],
            &mut times[3],
        ) == 0
        {
            return Err(std::io::Error::last_os_error());
        }
        let ticks =
            |time: FILETIME| ((time.dwHighDateTime as u64) << 32) | time.dwLowDateTime as u64;
        Ok((ticks(times[1]) == 0).then(|| ticks(times[0])))
    }
}
#[cfg(not(windows))]
fn process_birth(_pid: u32) -> std::io::Result<Option<u64>> {
    Err(std::io::Error::other("process identity unavailable"))
}

pub(super) fn physical(path: &Path) -> Result<PathBuf> {
    let resolved = fs::canonicalize(path).map_err(|_| "product_retry_prompt_unavailable")?;
    let text = resolved.to_string_lossy();
    Ok(if let Some(unc) = text.strip_prefix(r"\\?\UNC\") {
        PathBuf::from(format!(r"\\{unc}"))
    } else if let Some(path) = text.strip_prefix(r"\\?\") {
        PathBuf::from(path)
    } else {
        resolved
    })
}

fn valid_key(key: &str) -> bool {
    policy::attempt_key(key, 0).is_some()
}
fn terminal(job: &Value) -> bool {
    matches!(
        s(&job["status"]),
        "succeeded" | "failed" | "lost" | "cancelled" | "canceled"
    )
}
fn related(job: &Value, session: &str) -> bool {
    job["todo_id"] == policy::ID
        || job["related_todo_ids"]
            .as_array()
            .is_some_and(|ids| ids.contains(&json!(policy::ID)))
        || ["thread_id", "resume_session"]
            .iter()
            .any(|field| job[*field] == session)
        || policy::same_path(&job["cwd"], &json!(policy::TREE))
}
fn pristine(job: &Value) -> bool {
    job["status"] == "queued" && job["started_at"].is_null() && job["codex_pid"].is_null()
}
fn fresh_job(paths: &Paths, id: &str, runtime: &mut impl Runtime) -> Result<Value> {
    let mut job = cc_job_policy::todo_store::read_bounded(
        &paths.jobs.join(id).join("job.json"),
        16 * 1024 * 1024,
    )
    .map_err(|_| "product_retry_job_unavailable")?;
    if job["id"] != id || !job["status"].is_string() {
        return Err("product_retry_job_invalid".into());
    }
    let mut alive = processes::live(&job);
    for field in ["pid", "codex_pid"] {
        if job[field].is_null() {
            continue;
        }
        let pid = job[field]
            .as_u64()
            .and_then(|pid| u32::try_from(pid).ok())
            .filter(|pid| *pid > 0)
            .ok_or("product_retry_process_identity_unavailable")?;
        if let Some(birth) = runtime.process_birth(pid)? {
            let recorded = job[format!("{field}_created")]
                .as_u64()
                .ok_or("product_retry_process_identity_unavailable")?;
            alive |= birth == recorded;
        }
    }
    job["retry_live"] = json!(alive);
    Ok(job)
}

/// Bulk observations happen before the queue gate. Only bounded target records and
/// their creation identities are refreshed inside queue -> job state -> ToDo.
pub(super) fn run(
    paths: &Paths,
    key: &str,
    worker: Option<&str>,
    accepted: Option<&str>,
    runtime: &mut impl Runtime,
) -> Result<Value> {
    if !valid_key(key)
        || worker.is_some_and(|id| !crate::job_refs::valid_id(id))
        || accepted.is_some_and(|id| !crate::job_refs::valid_id(id))
    {
        return Err("product_retry_input_invalid".into());
    }
    let proof = runtime.guard()?;
    if !policy::valid_proof(&proof, runtime.clock()) {
        return Err("product_retry_guard_unverified".into());
    }
    let ps = runtime.snapshot()?;
    let mut jobs = runtime.jobs()?;
    let observed_at = runtime.clock();
    for job in &mut jobs {
        job["retry_live"] =
            json!(processes::live(job) || processes::alive(job, &ps, observed_at as f64));
    }
    let observed = load(&paths.store)?;
    let before = policy::row(&observed).ok_or("product_retry_item_missing")?;
    let source_id = worker
        .or_else(|| {
            if accepted.is_some() {
                before["product_retry_intent"]["source"]["id"].as_str()
            } else {
                before["worker_receipt"]["job"].as_str()
            }
        })
        .filter(|id| crate::job_refs::valid_id(id))
        .ok_or("product_retry_source_missing")?
        .to_owned();
    let mut targets = BTreeSet::from([source_id.clone()]);
    if let Some(id) = accepted {
        targets.insert(id.to_owned());
    }
    let base = if accepted.is_some() {
        policy::base_key(&before["product_retry_intent"])
            .as_str()
            .unwrap_or(key)
    } else {
        key
    };
    let mut keys: BTreeSet<_> = (0..=2)
        .filter_map(|attempt| policy::attempt_key(base, attempt))
        .collect();
    if let Some(old) = before["product_retry_intent"]["key"].as_str() {
        keys.insert(old.to_owned());
    }
    for launch_key in &keys {
        let matching: Vec<_> = jobs
            .iter()
            .filter(|job| job["launch_key"] == *launch_key)
            .collect();
        if matching.len() > 1 {
            return Err("product_retry_duplicate_jobs".into());
        }
        for job in matching {
            let id = job["id"]
                .as_str()
                .filter(|id| crate::job_refs::valid_id(id))
                .ok_or("product_retry_job_invalid")?;
            targets.insert(id.to_owned());
        }
    }
    // A busy target must release the global gate immediately, rather than wait 30s.
    let _queue = Lock::acquire(&paths.jobs.join(".queue.lock"), Duration::from_secs(2))?;
    let _states = targets
        .iter()
        .map(|id| Lock::acquire(&paths.jobs.join(id).join("state.lock"), Duration::ZERO))
        .collect::<Result<Vec<_>>>()?;
    let _todo = Lock::acquire(&paths.lock, Duration::ZERO)?;
    let mut db = load(&paths.store)?;
    let row = policy::row(&db)
        .ok_or("product_retry_item_missing")?
        .clone();
    if proof["snapshot"] != crate::product_resume::snapshot(&db, &row)
        || row["product_retry_intent"] != before["product_retry_intent"]
        || (accepted.is_none() && row["worker_receipt"]["job"] != source_id)
    {
        return Err("product_retry_snapshot_changed".into());
    }
    let at = runtime.clock();
    if !policy::valid_proof(&proof, at) {
        return Err("product_retry_guard_unverified".into());
    }
    for id in &targets {
        let fresh = fresh_job(paths, id, runtime)?;
        jobs.retain(|job| job["id"] != *id);
        jobs.push(fresh);
    }
    let at = runtime.clock();
    if !policy::valid_proof(&proof, at) {
        return Err("product_retry_guard_unverified".into());
    }
    let source = jobs
        .iter()
        .find(|job| job["id"] == source_id)
        .ok_or("product_retry_source_missing")?;
    let intent = &row["product_retry_intent"];
    let session = (if intent["bound_job"] == source["id"] {
        intent["source"]["session"].as_str().map(str::to_owned)
    } else {
        None
    })
    .or_else(|| {
        source_identity(source).and_then(|identity| identity["session"].as_str().map(str::to_owned))
    })
    .ok_or("product_retry_source_settings_invalid")?;
    let pending: Vec<_> = jobs
        .iter()
        .filter(|job| job["retry_live"] == true && related(job, &session))
        .collect();
    if pending.len() > 1
        || pending.iter().any(|job| {
            if intent["bound_job"] == job["id"] {
                policy::admission_reason(&db, job, at)
                    .is_some_and(|reason| reason["terminal"] != false)
            } else {
                !policy::job_matches(job, intent)
                    || !pristine(job)
                    || (accepted.is_none() && policy::base_key(intent) != key)
            }
        })
    {
        return Err("product_retry_other_worker_active".into());
    }
    if accepted.is_none() && intent["bound_job"] == source["id"] && source["status"] == "queued" {
        let result = continue_queued(&mut db, source, key, at)?;
        if result["saved"] == true {
            save(&paths.store, &db)?;
        }
        return Ok(result);
    }
    if source["retry_live"] != false || cancellation::owner_stopped(&row, source) {
        return Err("product_retry_source_running_or_stopped".into());
    }
    if let Some(id) = accepted {
        let job = jobs
            .iter()
            .find(|job| job["id"] == id)
            .ok_or("product_retry_accepted_job_missing")?;
        if intent["bound_job"].is_null()
            && source_identity(source).as_ref() != Some(&intent["source"])
        {
            return Err("product_retry_source_settings_changed".into());
        }
        let bytes = super::prompt_bytes(&paths.jobs.join(id).join("prompt.md"))?;
        if policy::digest(&bytes) != intent["prompt_sha256"] {
            return Err("product_retry_prompt_changed".into());
        }
        let result = bind(&mut db, job, key, at)?;
        if result["reused"] == false {
            save(&paths.store, &db)?;
        }
        return Ok(result);
    }
    let candidate = policy::candidate(&db, at).ok_or("product_retry_not_due")?;
    if policy::key(&candidate["wait_ready"]).as_deref() != Some(key) {
        return Err("product_retry_key_changed".into());
    }
    let same = policy::base_key(intent) == key;
    let previous: Vec<_> = jobs
        .iter()
        .filter(|job| job["launch_key"] == intent["key"] && !intent["key"].is_null())
        .collect();
    // Never infer "unaccepted" from a live or unrecognised record. Registration
    // and final bind independently check the same immutable marker and scope.
    if previous.iter().any(|job| {
        if intent["bound_job"] == job["id"] {
            policy::admission_reason(&db, job, at).is_some_and(|reason| reason["terminal"] != false)
        } else {
            !policy::job_matches(job, intent)
        }
    }) {
        return Err("product_retry_previous_job_changed".into());
    }
    let retire = same
        && intent["bound_job"].is_null()
        && if previous.is_empty() {
            intent["snapshot"] != policy::snapshot(candidate)
        } else {
            previous
                .iter()
                .all(|job| terminal(job) && job["retry_live"] == false)
        };
    if jobs.iter().any(|job| {
        keys.contains(s(&job["launch_key"]))
            && job["launch_key"] != intent["key"]
            && (job["retry_live"] != false || !terminal(job))
    }) {
        return Err("product_retry_previous_job_active".into());
    }
    let plan = attempt_plan(candidate, key, retire)?;
    if plan.retire && !pending.is_empty() {
        return Err("product_retry_previous_job_active".into());
    }
    if intent["key"] != plan.key && jobs.iter().any(|job| job["launch_key"] == plan.key) {
        return Err("product_retry_attempt_key_used".into());
    }
    let folder = paths
        .store
        .parent()
        .ok_or("product_retry_paths_invalid")?
        .join("product-retry-prompts");
    let replay = intent["key"] == plan.key;
    let digest = if replay {
        intent["prompt_sha256"]
            .as_str()
            .filter(|hash| hash.len() == 64 && hash.bytes().all(|b| b.is_ascii_hexdigit()))
            .ok_or("product_retry_prompt_changed")?
            .to_owned()
    } else {
        policy::digest(CONTINUATION.as_bytes())
    };
    let expected =
        cc_scheduler::common::resolve(&folder.join(format!("{}-{}.md", plan.key, &digest[..16])))
            .map_err(|_| "product_retry_prompt_unavailable")?;
    let mut proposed = db.clone();
    if !replay {
        crate::product_resume::qualify(&mut proposed, policy::ID as i64, &proof, true)?;
    }
    // Validate the complete candidate, key, source and scope before creating any body.
    stage_attempt(
        &mut proposed,
        &proof,
        source,
        &plan,
        &expected.to_string_lossy(),
        &digest,
        at,
    )?;
    let (file, actual_digest) =
        super::prepare_prompt(&folder, &plan.key, intent, CONTINUATION.as_bytes())?;
    if file != expected || actual_digest != digest {
        return Err("product_retry_prompt_changed".into());
    }
    let saved = !replay || proposed != db;
    // Old aliases may resolve to the same committed bytes; store the physical path
    // used by the normal CLI's prompt_source after its own canonicalisation.
    proposed["items"]
        .as_array_mut()
        .unwrap()
        .iter_mut()
        .find(|row| row["id"] == policy::ID)
        .unwrap()["product_retry_intent"]["prompt_file"] = json!(file);
    let saved = saved || proposed != db;
    let result = super::report(
        &policy::row(&proposed).unwrap()["product_retry_intent"],
        saved,
    );
    if saved {
        save(&paths.store, &proposed)?;
    }
    Ok(result)
}
