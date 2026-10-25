use crate::*;

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
        let unstarted = row.is_some_and(|row| row["scheduler"]["selection"]["unstarted_review"].is_object());
        let prepared = unstarted || row.is_some_and(|row| row["scheduler"]["selection"]["quota_recovery"].is_object());
        Ok(if prepared {
            self.review_recovery_inventory(unstarted)
        } else {
            Value::Null
        })
    }

    pub(crate) fn review_recovery_inventory(&self, unstarted: bool) -> Value {
        // Normal provenance reads precede the quota refresh, outside locks.
        let normal = if unstarted { self.rt.unstarted_review_registration() } else { Value::Null };
        let mut inventory = self.rt.quota_recovery_inventory();
        if unstarted { inventory["unstarted_registration"] = normal; }
        inventory
    }

    fn verify_unstarted_review(&self, row: &Value, job: &Value, inventory: &Value) -> Result<()> {
        let mut authors = Vec::new();
        for (id, todo) in cc_job_policy::unstarted_review_state::WRITERS.iter().zip([3387,3648]) {
            let author = self.load(id)?;
            if author["id"] != *id || author["todo_id"] != todo || author["status"] != "succeeded" {
                return Err(Error::new("unstarted review author provenance changed"));
            }
            let models = cc_job_policy::author_models(&author);
            if models.is_empty() { return Err(Error::new("unstarted review author model is unavailable")); }
            authors.extend(models);
        }
        authors.sort(); authors.dedup();
        let bytes = self.read_file(Path::new(s(row,"spec")))?;
        if bytes.is_empty() || bytes.len() > 1024*1024
            || !cc_job_policy::unstarted_review_state::admission(row, job,
                &inventory["unstarted_registration"], inventory, &authors,
                &cc_job_policy::product_retry::digest(&bytes), self.rt.epoch()) {
            return Err(Error::new("unstarted review registration, scope, generation or OAuth availability changed"));
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
        let unstarted = &row["scheduler"]["selection"]["unstarted_review"];
        if unstarted.is_object() {
            if row["scheduler"]["phase"] != "prepared" || row["scheduler"]["key"] != job["launch_key"] {
                return Err(Error::new("unstarted review preparation generation changed"));
            }
            job["unstarted_review"] = unstarted.clone();
            self.verify_unstarted_review(row,job,inventory)?;
            return Ok(true);
        }
        let proof = &row["scheduler"]["selection"]["quota_recovery"];
        if !proof.is_object() {
            return Ok(false);
        }
        if row["scheduler"]["phase"] != "prepared" || row["scheduler"]["key"] != job["launch_key"] {
            return Err(Error::new("quota recovery preparation generation changed"));
        }
        self.verify_quota_recovery(proof, row, job, inventory, true)?;
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
        let verified = if registration {
            cc_job_policy::quota_recovery::verify(proof, row, &source, job, now)
        } else {
            let old_at = proof["health"]["at"].as_f64().unwrap_or(f64::NAN);
            cc_job_policy::quota_recovery::verify(proof, row, &source, job, old_at)
        };
        if !verified
            || !cc_job_policy::quota_recovery::healthy_transition(
                inventory,
                s(&source, "model"),
                s(job, "model"),
                now,
            )
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
        if job["unstarted_review"].is_object() {
            self.verify_unstarted_review(row,job,inventory)?;
            job["review_source_verified"] = json!(false);
            return self.validate_review_launch(job,true);
        }
        if !job["quota_recovery"].is_object() {
            return Ok(());
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
}
