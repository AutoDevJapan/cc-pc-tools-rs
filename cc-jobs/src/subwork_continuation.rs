use crate::*;
use cc_job_policy::subwork_continuation as continuation;

impl Context {
    pub(crate) fn inherit_subwork_continuation(
        &self,
        job: &mut Value,
        inventory: &Value,
    ) -> Result<()> {
        if !job["todo_id"].is_number() || !self.todo_store().exists() {
            return Ok(());
        }
        let db = self.read_json(&self.todo_store())?;
        let Some(row) = db["items"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|r| r["id"] == job["todo_id"])
        else {
            return Ok(());
        };
        let proof = &row["scheduler"]["selection"]["subwork_continuation"];
        if proof.is_null() {
            return Ok(());
        }
        if row["scheduler"]["phase"] != "prepared" || row["scheduler"]["key"] != job["launch_key"] {
            return Err(Error::new("subwork continuation preparation changed"));
        }
        job["subwork_continuation"] = proof.clone();
        job["capacity_account_selector"] = proof["selector"].clone();
        let source = self.load(s(&proof["request"], "source_job"))?;
        if job["model"] != proof["model"]
            || job["model_origin"] != proof["model_origin"]
            || job["effort_origin"] != proof["effort_origin"]
        {
            return Err(Error::new(
                "subwork continuation model differs from preparation",
            ));
        }
        if proof["quota_retry"].is_object() {
            cc_job_policy::quota_retry::inherit(job, &source, &proof["quota_retry"]);
        } else {
            for key in continuation::RETRY_FIELDS {
                if !source[key].is_null() {
                    job[key] = source[key].clone();
                }
            }
        }
        job["resume_session"] = Value::Null;
        self.verify_subwork_continuation(row, job, inventory)?;
        Ok(())
    }
    pub(crate) fn verify_subwork_continuation(
        &self,
        row: &Value,
        job: &Value,
        inventory: &Value,
    ) -> Result<()> {
        let proof = &job["subwork_continuation"];
        let request = &proof["request"];
        let source_id = s(request, "source_job");
        let actor_id = s(request, "actor_job");
        if !crate::queue::valid_job_id(source_id) || !crate::queue::valid_job_id(actor_id) {
            return Err(Error::new("invalid subwork continuation source"));
        }
        let source = self.load(source_id)?;
        let actor = self.load(actor_id)?;
        let db = self.read_json(&self.todo_store())?;
        if self.is_ours_checked(&source, "pid")?
            || self.is_ours_checked(&source, "codex_pid")?
            || self.tool_handoff_pending(&source)
            || !continuation::parent_registered(&db, row, &actor)
            || !continuation::verify(proof, row, &source, &actor, job, inventory, self.rt.epoch())
            || cc_job_policy::todo_wait::waiting(&db, row)
            || (job["capacity_account_selector"].is_string()
                && !cc_job_policy::quota_retry::current_available(job, inventory, self.rt.epoch()))
            || (job["subwork_continuation_initial_admitted"] != true
                && (!(if proof["quota_retry"].is_object() {
                    cc_job_policy::quota_retry::inherited(job, &source, &proof["quota_retry"])
                } else {
                    continuation::inherited(job, &source)
                }) || job["model"] != proof["model"]))
            || !job["resume_session"].is_null()
        {
            return Err(Error::new(
                "subwork continuation source, generation, scope or OAuth changed",
            ));
        }
        Ok(())
    }
}
