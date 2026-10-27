//! Finite entry for reevaluating one recorded automatic ownership edge.
use super::*;
use cc_scheduler::received_input::{Host, SystemHost};
use std::path::Path;

const JOB_LIMIT: usize = 1024 * 1024;

/// Uses the normal per-path guard. No original registry, prompt or SDK data is read.
pub fn reevaluate_owner_wait_files(paths: &Paths, id: i64) -> Result<Vec<i64>> {
    let mut backend = RealBackend::new(PathBuf::from("D:/CC/Tools/scheduler"));
    run(
        paths,
        id,
        &mut SystemHost::default(),
        || {
            backend
                .snapshot()
                .map_err(|_| "process identity unavailable".into())
        },
        |db| save(&paths.store, db),
    )
}

fn run(
    paths: &Paths,
    id: i64,
    host: &mut impl Host,
    mut snapshot: impl FnMut() -> Result<Processes>,
    mut commit: impl FnMut(&Value) -> Result<()>,
) -> Result<Vec<i64>> {
    let started = host.now();
    // Only the selected row and its exact automatic owner participate in CAS.
    // Unrelated writers are preserved by reloading the store at commit.
    let mut proposed = {
        let _lock = Lock::acquire(&paths.lock, Duration::from_secs(30))?;
        load(&paths.store)?
    };
    let source = source(&proposed, id)?;
    guarded(host, &source.root)?;
    let mut records = vec![];
    let mut identities = vec![];
    for jid in references(&source.item)? {
        let path = paths.jobs.join(&jid).join("job.json");
        let bytes = read(host, &path)?;
        let job: Value = serde_json::from_slice(&bytes).map_err(|_| "receipt invalid")?;
        let expected = if source.item["work_owner_job"] == jid {
            source.owner.as_ref().ok_or("owner source unavailable")?
        } else {
            &source.item
        };
        if job["id"] != jid
            || job["project"] != expected["project"]
            || !(job["todo_id"] == expected["id"]
                || job["related_todo_ids"]
                    .as_array()
                    .is_some_and(|a| a.contains(&expected["id"])))
            || !job["status"].is_string()
            || (!job["registered_cwd"].is_null()
                && (s(&job["registered_cwd"]).is_empty()
                    || normalized(s(&job["registered_cwd"])) != normalized(&source.root)))
        {
            return Err("receipt source mismatch".into());
        }
        for key in ["pid", "codex_pid"] {
            if !job[key].is_null()
                && (job[key]
                    .as_u64()
                    .is_none_or(|p| p == 0 || p > u64::from(u32::MAX))
                    || job[format!("{key}_created")]
                        .as_u64()
                        .is_none_or(|b| b == 0))
            {
                return Err("process identity unavailable".into());
            }
        }
        guarded(host, s(&job["cwd"]))?;
        identities.push(identity(&proposed, &job)?);
        records.push((path, bytes, job));
    }
    let ps = snapshot()?;
    let jobs: Vec<_> = records
        .iter()
        .map(|(_, _, job)| observed(job, &ps))
        .collect();
    if held(&proposed, &source.item, &jobs) {
        return Ok(vec![]);
    }
    // The existing policy may inspect Git ownership. Do that outside shared locks,
    // then bind its proposal to unchanged inputs and a fresh liveness observation.
    let changed =
        cc_job_policy::work_owner::reevaluate_todos_if(&mut proposed, &jobs, |i| i["id"] == id);
    if changed.is_empty() {
        return Ok(changed);
    }
    let replacement = one(&proposed, id)?.clone();
    // Refresh permissions after planning as well; slow Git discovery or a changed
    // guard verdict must not turn an earlier positive into unconditional authority.
    guarded(host, &source.root)?;
    for ((_, _, job), before) in records.iter().zip(&identities) {
        guarded(host, s(&job["cwd"]))?;
        if identity(&proposed, job)? != *before {
            return Err("reevaluation ownership source changed".into());
        }
    }

    // Normal order is job state -> ToDo. No tool/guard runs under these locks.
    let mut locks = vec![];
    for (path, _, _) in &records {
        locks.push(
            common::ByteLock::acquire(&path.with_file_name("state.lock"), Duration::ZERO)
                .map_err(|_| "receipt lock unavailable")?
                .ok_or("receipt busy")?,
        );
    }
    let _lock = Lock::acquire(&paths.lock, Duration::ZERO)?;
    let mut current = load(&paths.store)?;
    if source != self::source(&current, id)? {
        return Err("reevaluation source changed".into());
    }
    for (path, bytes, _) in &records {
        if *bytes != read(host, path)? {
            return Err("reevaluation receipt changed".into());
        }
    }
    let fresh = snapshot()?;
    if records
        .iter()
        .zip(&jobs)
        .any(|((_, _, job), before)| observed(job, &fresh) != *before)
    {
        return Err("reevaluation liveness changed".into());
    }
    fresh_proof(&started, &host.now())?;
    *find(&mut current, id)? = replacement;
    commit(&current)?;
    Ok(changed)
}

#[derive(PartialEq)]
struct Source {
    item: Value,
    root: String,
    owner: Option<Value>,
}
fn one(db: &Value, id: i64) -> Result<&Value> {
    let mut rows = db["items"]
        .as_array()
        .ok_or("invalid items")?
        .iter()
        .filter(|i| i["id"] == id);
    let row = rows.next().ok_or("no item")?;
    if id <= 0 || rows.next().is_some() {
        return Err("ambiguous item".into());
    }
    Ok(row)
}
fn source(db: &Value, id: i64) -> Result<Source> {
    let item = one(db, id)?;
    let project = item["project"]
        .as_str()
        .filter(|p| !p.is_empty() && !p.eq_ignore_ascii_case("owner"))
        .ok_or("requested item excluded")?;
    let root = db["projects"][project]
        .as_str()
        .ok_or("project source unavailable")?;
    let owner = if item["work_owner_job"].is_null() && item["work_owner_wait_on"].is_null() {
        None
    } else {
        let on = item["work_owner_wait_on"]
            .as_i64()
            .filter(|on| *on != id)
            .ok_or("owner source unavailable")?;
        let row = one(db, on)?;
        if row["project"] != project {
            return Err("owner source mismatch".into());
        }
        Some(row.clone())
    };
    Ok(Source {
        item: item.clone(),
        root: root.into(),
        owner,
    })
}
fn references(item: &Value) -> Result<BTreeSet<String>> {
    if crate::job_refs::unverified_count(&item["jobs"]) != 0 {
        return Err("invalid receipt identifier".into());
    }
    let mut ids: BTreeSet<_> = crate::job_refs::ids(&item["jobs"])
        .map(str::to_owned)
        .collect();
    for value in [&item["work_owner_job"], &item["scheduler"]["job"]] {
        if !value.is_null() {
            let id = value
                .as_str()
                .filter(|id| crate::job_refs::valid_id(id))
                .ok_or("invalid receipt identifier")?;
            ids.insert(id.into());
        }
    }
    Ok(ids)
}
fn read(host: &mut impl Host, path: &Path) -> Result<Vec<u8>> {
    let bytes = host
        .read(path, JOB_LIMIT)
        .map_err(|_| "receipt unavailable")?;
    if bytes.len() > JOB_LIMIT {
        return Err("receipt too large".into());
    }
    Ok(bytes)
}
fn guarded(host: &mut impl Host, path: &str) -> Result<()> {
    if path.trim().is_empty()
        || path.chars().any(char::is_control)
        || !Path::new(path).is_absolute()
    {
        return Err("project source unavailable".into());
    }
    // SystemHost validates all four typed booleans, duplicate JSON keys, exit
    // status and deadline; unknown classification is never an empty client list.
    if !host
        .guard(Path::new(path))
        .map_err(|_| "client guard unavailable")?
    {
        return Err("requested item excluded".into());
    }
    Ok(())
}
fn observed(job: &Value, ps: &Processes) -> Value {
    let mut job = job.clone();
    job["work_owner_live"] = json!(policy::alive(&job, ps, time_s()));
    job
}
fn identity(db: &Value, job: &Value) -> Result<Value> {
    // Reuse normal registration/Git ownership projection, including legitimate
    // registered subfolders. This is not a new source or permission attestation.
    let identity =
        cc_job_policy::work_owner::from_job(&cc_job_policy::work_owner::registered_job(db, job));
    if s(&identity["worktree"]).is_empty() {
        return Err("ownership source unavailable".into());
    }
    Ok(identity)
}
fn held(db: &Value, item: &Value, jobs: &[Value]) -> bool {
    !matches!(s(&item["status"]), "todo" | "doing" | "wait")
        || !item["wait_ready"].is_null()
        || n(&item["scheduler"]["not_before"]) > time_s()
        || discord_wait(item, time_s())
        || cc_job_policy::todo_wait::pending_on(db, item)
            .iter()
            .any(|on| item["work_owner_wait_on"] != *on)
        || jobs
            .iter()
            .any(|j| stopped(item, j) || policy::live(j) || j["work_owner_live"] == true)
}
fn fresh_proof(started: &str, ended: &str) -> Result<()> {
    let start =
        chrono::DateTime::parse_from_rfc3339(started).map_err(|_| "guard time unavailable")?;
    let end = chrono::DateTime::parse_from_rfc3339(ended).map_err(|_| "guard time unavailable")?;
    let elapsed = end.signed_duration_since(start);
    if elapsed < chrono::Duration::zero() || elapsed >= chrono::Duration::seconds(45) {
        return Err("guard proof expired".into());
    }
    Ok(())
}

#[cfg(test)]
#[path = "owner_wait_tests.rs"]
mod tests;
