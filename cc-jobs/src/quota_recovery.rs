use crate::*;
use std::path::Path;

/// Fixed scalar fields only: no prompts, replies, errors, account IDs or paths.
pub(crate) fn status_metadata(job: &Value) -> Value {
    let mut v = json!({"metadata_only":true});
    if crate::queue::valid_job_id(s(job,"id")) { v["id"] = job["id"].clone(); }
    for key in ["todo_id", "steps", "capacity_same_model_retry_count", "capacity_retry_count", "capacity_wait_count"] {
        v[key] = job[key].as_u64().map_or(Value::Null, |n| json!(n));
    }
    for (key, allowed) in [
        ("status", &["queued","starting","running","succeeded","failed","lost","cancelled","canceled"][..]),
        ("model_origin", &["policy","owner_request"][..]),
        ("effort_origin", &["policy","owner_request"][..]),
        ("effort", &["none","minimal","low","medium","high","xhigh","max"][..]),
        ("last_failure_kind", &["quota","capacity","transient","auth","cancel","request_failed"][..]),
    ] { v[key] = if allowed.contains(&s(job,key)) {job[key].clone()} else {Value::Null}; }
    if cc_job_policy::quota_recovery::provider(s(job,"model")).is_some() {
        v["model"] = job["model"].clone();
    }
    let selector = s(job, "capacity_account_selector");
    if cc_job_policy::quota_retry::public_selector(selector) { v["selector"] = json!(selector); }
    let finite = if job["finite_subwork"].is_object() {&job["finite_subwork"]} else {&job["finite_review"]};
    let sha = s(finite, "spec_sha256");
    if sha.len() == 64 && sha.bytes().all(|b| b.is_ascii_hexdigit()) { v["spec_sha256"] = json!(sha); }
    let observed = &job["execution_observed"];
    if observed["source"] == "native_turn_context"
        && [s(job,"model"), crate::pool_retry::launch_model(job).as_str()].contains(&s(observed,"model")) {
        v["native"]["model"] = observed["model"].clone();
        if ["none","minimal","low","medium","high","xhigh","max"].contains(&s(observed,"effort")) {v["native"]["effort"] = observed["effort"].clone();}
        if ["fast","priority","default","standard","flex","ultrafast"].contains(&s(observed,"service_tier")) {v["native"]["service_tier"] = observed["service_tier"].clone();}
    }
    if job["quota_recovery"].is_object() {
        v["recovery_version"] = job["quota_recovery"]["version"].as_u64().map_or(Value::Null, |n| json!(n));
        if crate::queue::valid_job_id(s(&job["quota_recovery"], "source_job")) { v["recovery_source_job"] = job["quota_recovery"]["source_job"].clone(); }
    }
    v
}

impl Context {
    /// Slow refreshed OAuth metadata is collected before queue/state/ToDo locks.
    pub(crate) fn prepared_quota_inventory(&self, a: &crate::cli::Args) -> Result<Value> {
        let Some(id) = a.value("todo-id").and_then(|s| s.parse::<u64>().ok()) else {
            return Ok(Value::Null);
        };
        if !self.todo_store().exists() {
            return Ok(Value::Null);
        }
        let db = self.read_json(&self.todo_store())?;
        let row = db["items"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|row| row["id"] == id);
        let unstarted =
            row.is_some_and(|row| row["scheduler"]["selection"]["unstarted_review"].is_object());
        let initial = row.is_some_and(|row| row["scheduler"]["selection"]["initial_review"].is_object());
        let prepared = unstarted || initial
            || row.is_some_and(|row| row["scheduler"]["selection"]["quota_recovery"].is_object());
        Ok(if prepared {
            self.review_recovery_inventory(unstarted,initial)
        } else {
            Value::Null
        })
    }

    pub(crate) fn review_recovery_inventory(&self, unstarted: bool, initial: bool) -> Value {
        if initial {
            let mut inventory=self.rt.initial_review_inventory();
            inventory["initial_review"]=json!(true);
            return inventory;
        }
        // Normal provenance reads precede the quota refresh, outside locks.
        let normal = if unstarted {
            self.rt.unstarted_review_registration()
        } else {
            Value::Null
        };
        let mut inventory = if unstarted {
            self.rt.unstarted_review_inventory()
        } else {
            self.rt.quota_recovery_inventory()
        };
        if unstarted {
            inventory["unstarted_registration"] = normal;
        }
        inventory
    }

    fn verify_unstarted_review(&self, row: &Value, job: &Value, inventory: &Value) -> Result<()> {
        if !self.rt.unstarted_review_workspace() {
            return Err(Error::new(
                "unstarted review current checkout or target tree changed",
            ));
        }
        let mut writer_jobs = Vec::new();
        for (id, todo) in cc_job_policy::unstarted_review_state::WRITERS
            .iter()
            .zip([3387, 3648])
        {
            let author = self.load(id)?;
            if author["id"] != *id || author["todo_id"] != todo || author["status"] != "succeeded" {
                return Err(Error::new("unstarted review author provenance changed"));
            }
            writer_jobs.push(author);
        }
        let authors = cc_job_policy::unstarted_review_state::observed_authors(&writer_jobs)
            .ok_or_else(|| Error::new("unstarted review observed author model is unavailable"))?;
        let path = Path::new(s(row, "spec"));
        if !cc_job_policy::unstarted_review_state::spec_path_matches(s(row, "spec")) {
            return Err(Error::new(
                "unstarted review specification namespace changed",
            ));
        }
        self.rt.before_load(path)?;
        let bytes = cc_job_policy::unstarted_review_spec::read(path)?;
        if bytes.is_empty()
            || bytes.len() > 1024 * 1024
            || !cc_job_policy::unstarted_review_state::admission(
                row,
                job,
                &inventory["unstarted_registration"],
                inventory,
                &authors,
                &cc_job_policy::product_retry::digest(&bytes),
                self.rt.epoch(),
            )
        {
            return Err(Error::new(
                "unstarted review registration, scope, generation or OAuth availability changed",
            ));
        }
        Ok(())
    }

    pub(crate) fn inherit_quota_recovery(
        &self,
        job: &mut Value,
        inventory: &Value,
    ) -> Result<bool> {
        if !job["todo_id"].is_number() || !self.todo_store().exists() {
            return Ok(false);
        }
        let db = self.read_json(&self.todo_store())?;
        let Some(row) = db["items"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|r| r["id"] == job["todo_id"])
        else {
            return Ok(false);
        };
        let initial=&row["scheduler"]["selection"]["initial_review"];
        if initial.is_object() {
            if row["scheduler"]["phase"]!="prepared" || row["scheduler"]["key"]!=job["launch_key"] {
                return Err(Error::new("initial review preparation generation changed"));
            }
            job["initial_review"]=initial.clone();
            job["capacity_account_selector"]=initial["selector"].clone();
            self.verify_initial_review(row,job,inventory)?;
            return Ok(true);
        }
        let unstarted = &row["scheduler"]["selection"]["unstarted_review"];
        if unstarted.is_object() {
            if row["scheduler"]["phase"] != "prepared"
                || row["scheduler"]["key"] != job["launch_key"]
            {
                return Err(Error::new(
                    "unstarted review preparation generation changed",
                ));
            }
            job["unstarted_review"] = unstarted.clone();
            job["capacity_account_selector"] = unstarted["selected_selector"].clone();
            self.verify_unstarted_review(row, job, inventory)?;
            return Ok(true);
        }
        let proof = &row["scheduler"]["selection"]["quota_recovery"];
        if !proof.is_object() {
            return Ok(false);
        }
        if row["scheduler"]["phase"] != "prepared" || row["scheduler"]["key"] != job["launch_key"] {
            return Err(Error::new("quota recovery preparation generation changed"));
        }
        if proof["version"] == 2 {
            job["capacity_account_selector"] = proof["retry"]["selector"].clone();
        }
        self.verify_quota_recovery(proof, row, job, inventory, true)?;
        let source = self.load(s(proof, "source_job"))?;
        cc_job_policy::quota_retry::inherit(job, &source, proof);
        job["quota_recovery"] = proof.clone();
        Ok(true)
    }

    fn verify_quota_recovery(
        &self,
        proof: &Value,
        row: &Value,
        job: &Value,
        inventory: &Value,
        registration: bool,
    ) -> Result<()> {
        let id = s(proof, "source_job");
        if !crate::queue::valid_job_id(id) {
            return Err(Error::new("invalid quota recovery source"));
        }
        let source = self.load(id)?;
        if s(&source, "id") != id
            || self.is_ours_checked(&source, "pid")?
            || self.is_ours_checked(&source, "codex_pid")?
            || self.tool_handoff_pending(&source)
        {
            return Err(Error::new("quota recovery source has not drained"));
        }
        let now = self.rt.epoch();
        // At registration both the original prepared observation and a fresh
        // independent observation must agree. Queue waits recheck fresh health.
        let mut initial=job.clone();
        initial["model"]=proof["model"].clone();
        initial["effort"]=proof["effort"].clone();
        initial["capacity_account_selector"]=proof["retry"]["selector"].clone();
        let verified = if registration {
            cc_job_policy::quota_recovery::verify(proof, row, &source, &initial, now)
        } else {
            let old_at = proof["health"]["at"].as_f64().unwrap_or(f64::NAN);
            cc_job_policy::quota_recovery::verify(proof, row, &source, &initial, old_at)
        };
        let current = if proof["version"]==1 && job["capacity_account_selector"].is_null() && job["model"]==proof["model"] {
            cc_job_policy::quota_recovery::healthy_transition(inventory,s(&source,"model"),s(job,"model"),now)
        } else {cc_job_policy::quota_retry::current_available(job,inventory,now)};
        if !verified
            || !current
            || (!registration
                && !cc_job_policy::quota_retry::inherited(job, &source, proof))
        {
            return Err(Error::new(
                "quota recovery source, scope, generation or OAuth availability changed",
            ));
        }
        Ok(())
    }

    /// Called under the final queue -> state -> ToDo admission locks. It never
    /// sends network requests, relaxes a dependency or restarts the old source.
    pub(crate) fn recheck_quota_recovery(
        &self,
        row: &Value,
        job: &mut Value,
        inventory: &Value,
    ) -> Result<()> {
        if job["initial_review"].is_object() {
            self.verify_initial_review(row,job,inventory)?;
            job["review_source_verified"]=json!(false);
            return self.validate_review_launch(job,true);
        }
        if job["unstarted_review"].is_object() {
            self.verify_unstarted_review(row, job, inventory)?;
            job["review_source_verified"] = json!(false);
            return self.validate_review_launch(job, true);
        }
        if !job["quota_recovery"].is_object() {
            return Ok(());
        }
        if job["quota_recovery"]["version"]==1 && job["quota_retry_seeded_version"]!=1 {
            let proof=job["quota_recovery"].clone();let source=self.load(s(&proof,"source_job"))?;
            if !cc_job_policy::quota_retry::adopt_legacy(job,&source,&proof) {return Err(Error::new("legacy quota retry history cannot be preserved"));}
        }
        let linked = (row["scheduler"]["phase"] == "prepared"
            && row["scheduler"]["key"] == job["launch_key"])
            || (row["scheduler"]["job"] == job["id"]
                && row["jobs"]
                    .as_array()
                    .is_some_and(|v| v.contains(&job["id"])));
        if !linked {
            return Err(Error::new("quota recovery worker ownership changed"));
        }
        self.verify_quota_recovery(&job["quota_recovery"], row, job, inventory, false)?;
        job["review_source_verified"] = json!(false);
        self.validate_review_launch(job, true)
    }

    fn verify_initial_review(&self, row: &Value, job: &Value, inventory: &Value) -> Result<()> {
        let source=self.load(cc_job_policy::initial_review::AUTHOR)?;
        let observed=cc_job_policy::initial_review::author(&[source])
            .ok_or_else(||Error::new("initial review author execution changed"))?;
        let bytes=self.rt.initial_review_spec(row)?;
        let workspace=self.rt.initial_review_workspace(row);
        if !workspace.is_object() || !cc_job_policy::initial_review::current(row,&observed,&workspace,
            &cc_job_policy::product_retry::digest(&bytes),inventory,self.rt.epoch(),Some(job)) {
            return Err(Error::new("initial review specification, scope, ownership, generation or OAuth changed"));
        }
        Ok(())
    }
}
