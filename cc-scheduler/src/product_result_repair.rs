//! Consume only the exact finished finite product retry, without granting dispatch.
use super::*;
use cc_job_policy::product_retry as scope;

fn generation(row: &Value, job: &Value) -> bool {
    let intent = &row["product_retry_intent"];
    row["resume_generation"]
        .as_f64()
        .is_some_and(|n| n.is_finite() && n >= 0. && n.fract() == 0. && n < 9_007_199_254_740_991.)
        && row["worker_receipt"]["generation"] == row["resume_generation"]
        && intent["bound_generation"] == row["resume_generation"]
        && intent["bound_job"] == job["id"]
        && row["worker_receipt"]["job"] == job["id"]
        && row["scheduler"]["job"] == job["id"]
}

fn inactive(job: &Value, ps: &Processes) -> bool {
    if live(job) {
        return false;
    }
    for field in ["pid", "codex_pid"] {
        if job[field].is_null() {
            continue;
        }
        let Some(pid) = job[field]
            .as_u64()
            .and_then(|p| u32::try_from(p).ok())
            .filter(|p| *p > 0)
        else {
            return false;
        };
        if let Some(process) = ps.get(&pid) {
            let Some(expected) = job[format!("{field}_created")].as_u64() else {
                return false;
            };
            if let Some(birth) = process["created"].as_u64() {
                if birth == expected {
                    return false;
                }
            } else {
                let (Some(lower), Some(upper)) = (
                    process["creation_lower"].as_u64(),
                    process["creation_upper"].as_u64(),
                ) else {
                    return false;
                };
                if lower == 0
                    || lower > upper
                    || upper - lower > crate::backend::BIRTH_FALLBACK_MAX_SPAN
                    || (lower..=upper).contains(&expected)
                {
                    return false;
                }
            }
        }
    }
    true
}

fn supplement_births<B: Backend>(
    backend: &mut B,
    row: &Value,
    jobs: &[Value],
    ps: &Processes,
) -> Result<Processes> {
    let mut wanted = std::collections::BTreeSet::new();
    for job in jobs
        .iter()
        .filter(|j| !live(j) && cc_job_policy::priority::linked(row, j))
    {
        for field in ["pid", "codex_pid"] {
            if let Some(pid) = job[field]
                .as_u64()
                .and_then(|p| u32::try_from(p).ok())
                .filter(|p| *p > 0)
            {
                if job[format!("{field}_created")].as_u64().is_some()
                    && ps
                        .get(&pid)
                        .is_some_and(|p| p["created"].as_u64().is_none())
                {
                    wanted.insert(pid);
                }
            }
        }
    }
    let mut enriched = ps.clone();
    if !wanted.is_empty() {
        for (pid, (lower, upper)) in
            backend.birth_bounds(&wanted.iter().copied().collect::<Vec<_>>())?
        {
            if !wanted.contains(&pid)
                || lower == 0
                || lower > upper
                || upper - lower > crate::backend::BIRTH_FALLBACK_MAX_SPAN
            {
                return Err(Error("BirthMetadataInvalid"));
            }
            if let Some(process) = enriched.get_mut(&pid) {
                process["creation_lower"] = json!(lower);
                process["creation_upper"] = json!(upper);
            }
        }
    }
    Ok(enriched)
}

fn original_wait(row: &Value, at: u64) -> Option<Value> {
    let intent = &row["product_retry_intent"];
    let wait = &intent["snapshot"]["wait_ready"];
    (scope::key(wait).as_deref() == scope::base_key(intent).as_str()
        && scope::base_key(intent).as_str().is_some()
        && cc_job_policy::todo_wait::retry_due(wait, at)
        && scope::attempt_key(
            scope::base_key(intent).as_str()?,
            intent["attempt"].as_u64()?,
        )
        .as_deref()
            == intent["key"].as_str())
    .then(|| wait.clone())
}

fn qualified(db: &Value, job: &Value, at: u64) -> bool {
    let Some(row) = scope::row(db) else {
        return false;
    };
    let intent = &row["product_retry_intent"];
    let project = s(&row["project"]);
    row["status"] == "doing"
        && row["scheduler"]["phase"] == "running"
        && row["dispatchable"] == false
        && !project.is_empty()
        && !db["projects"][project].is_null()
        && scope::same_path(
            &db["projects"][project],
            &intent["proof"]["snapshot"]["registered_path"],
        )
        && scope::valid_proof(&row["product_resume_qualification"]["receipt"], at)
        && scope::valid_proof(&intent["proof"], at)
        && job["status"] == "succeeded"
        && generation(row, job)
        && scope::job_matches(job, intent)
        && scope::admission_reason(db, job, at).is_none()
        && row["owner_question_wait"].is_null()
        && ["stopped", "cancel_requested", "owner_postponement_recorded"]
            .iter()
            .all(|field| row[*field].is_null() || row[*field] == false)
        && (row["stop_kind"].is_null() || row["stop_kind"] == "")
        && row["resume_condition"].is_null()
        && row["external_resume_condition"].is_null()
        && row["scheduler"]["resource_wait"].is_null()
        && (row["scheduler"]["not_before"].is_null()
            || row["scheduler"]["not_before"]
                .as_f64()
                .is_some_and(|n| n.is_finite() && n >= 0. && n <= at as f64))
        && row["scheduler"]["question"].is_null()
        && row["scheduler"]["question_close_pending"].is_null()
        && (row["processed_results"].is_null() || row["processed_results"].is_object())
        && !result_received(row, &job["id"])
        && original_wait(row, at).is_some()
        && ["started_at", "finished_at"].iter().all(|key| {
            job[*key]
                .as_str()
                .and_then(|v| chrono::DateTime::parse_from_rfc3339(v).ok())
                .is_some_and(|v| v.timestamp() >= 0 && v.timestamp() as u64 <= at)
        })
        && epoch(&job["finished_at"]) >= epoch(&job["started_at"])
}

fn same_owner_elsewhere(row: &Value, current: &Value, jobs: &[Value], ps: &Processes) -> bool {
    jobs.iter().any(|job| {
        job["id"] != current["id"]
            && cc_job_policy::priority::linked(row, job)
            && (live(job) || !inactive(job, ps))
    })
}

fn same_job_record(observed: &Value, stored: &Value) -> bool {
    // sample() adds subwork_live from the current process snapshot. It is an
    // observation, not a field in job.json or authority to change its owner.
    let mut observed = observed.clone();
    let mut stored = stored.clone();
    for record in [&mut observed, &mut stored] {
        if let Some(object) = record.as_object_mut() {
            object.remove("subwork_live");
        }
    }
    observed == stored
}

impl<B: Backend> Scheduler<B> {
    pub(super) fn reconcile_product_results(
        &mut self,
        jobs: &[Value],
        ps: &Processes,
    ) -> Result<()> {
        let at = self.backend.clock();
        if !at.is_finite() || at < 0. {
            return Ok(());
        }
        let before = self.todo()?;
        let Some(row) = scope::row(&before).cloned() else {
            return Ok(());
        };
        let mut selected = jobs
            .iter()
            .filter(|job| job["id"] == row["scheduler"]["job"]);
        let Some(job) = selected.next().cloned() else {
            return Ok(());
        };
        if selected.next().is_some() || !qualified(&before, &job, at as u64) {
            return Ok(());
        }
        let ps = supplement_births(&mut self.backend, &row, jobs, ps)?;
        if !inactive(&job, &ps) || same_owner_elsewhere(&row, &job, jobs, &ps) {
            return Ok(());
        }
        let Some(id) = job["id"].as_str().filter(|id| {
            !id.is_empty()
                && id
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
        }) else {
            return Ok(());
        };
        // This is the existing product guard, including original proof and expiry.
        if !self.backend.repair_scope_allowed(scope::TREE) || self.backend.outcome(&job)? != "todo"
        {
            return Ok(());
        }
        let folder = self
            .root
            .parent()
            .unwrap_or(&self.root)
            .join("codex-jobs/jobs")
            .join(id);
        let Some(_job_lock) = ByteLock::acquire(&folder.join("state.lock"), Duration::ZERO)? else {
            return Ok(());
        };
        // The ordinary queue and owner cancellation use this same job -> ToDo order.
        let fresh_job = read_json(&folder.join("job.json"), Value::Null)?;
        if !same_job_record(&job, &fresh_job) {
            return Ok(());
        }
        let fresh_jobs = self.backend.jobs()?;
        let fresh_ps = self.backend.snapshot()?;
        let fresh_ps = supplement_births(&mut self.backend, &row, &fresh_jobs, &fresh_ps)?;
        if fresh_jobs.iter().filter(|j| j["id"] == job["id"]).count() != 1
            || !fresh_jobs
                .iter()
                .any(|fresh| fresh["id"] == job["id"] && same_job_record(&job, fresh))
            || !inactive(&job, &fresh_ps)
            || same_owner_elsewhere(&row, &job, &fresh_jobs, &fresh_ps)
        {
            return Ok(());
        }
        let Some(_lock) = ByteLock::acquire(
            &self.todo_path.with_file_name("todo.lock"),
            Duration::from_secs(2),
        )?
        else {
            return Ok(());
        };
        let mut db = self.todo()?;
        let now = self.backend.clock();
        if !now.is_finite()
            || now < 0.
            || scope::row(&db) != Some(&row)
            || db["projects"][s(&row["project"])] != before["projects"][s(&row["project"])]
            || !qualified(&db, &job, now as u64)
        {
            return Ok(());
        }
        let row = db["items"]
            .as_array_mut()
            .unwrap()
            .iter_mut()
            .find(|r| r["id"] == scope::ID)
            .unwrap();
        let intent = row["product_retry_intent"].clone();
        let wait = original_wait(row, now as u64).unwrap();
        let exhausted = intent["attempt"].as_u64().unwrap() >= 2;
        record_result(row, &job["id"], "finished_product_retry");
        row["processed_results"][id]["generation"] = row["resume_generation"].clone();
        row["processed_results"][id]["binding"] = intent;
        row["processed_results"][id]["finished_at"] = job["finished_at"].clone();
        row["scheduler"]["last_job"] = job["id"].clone();
        row["scheduler"]["resume"] = json!(true);
        if exhausted {
            row["status"] = json!("todo");
            row["scheduler"]["phase"] = json!("retry_exhausted");
        } else {
            // Reuse the original wait/base key. Never mint a fresh retry budget.
            // The normal reserve API retires this terminal key and advances 0..2.
            row["status"] = json!("wait");
            row["wait_ready"] = wait;
            row["product_retry_intent"]["bound_job"] = Value::Null;
            row["scheduler"]["phase"] = json!("retry");
        }
        row["updated"] = json!(chrono::Local::now().format("%Y-%m-%d %H:%M").to_string());
        write_todo(&self.todo_path, &db)?;
        self.report[if exhausted {
            "product_results_exhausted"
        } else {
            "product_results_resumable"
        }] = json!([scope::ID]);
        Ok(())
    }
}

#[cfg(test)]
#[path = "product_result_repair_tests.rs"]
mod tests;
