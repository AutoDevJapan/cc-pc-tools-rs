use crate::*;
use std::{fs, path::Path};

impl Context {
    /// Preview and apply use the same locked verification. Nothing launches here.
    pub fn repair_owner_wait(&self, request: &Value, apply: bool) -> Result<Value> {
        let id = s(request, "job_id");
        let owner_id = s(request, "owner_job_id");
        if !queue::valid_job_id(id) || !queue::valid_job_id(owner_id) || id == owner_id {
            return Err(Error::new("repair needs two distinct exact job ids"));
        }
        let _gate = self.queue_lock()?;
        let _state = FileLock::try_acquire(&self.job_dir(id).join("state.lock"))?;
        let _owner = FileLock::try_acquire(&self.job_dir(owner_id).join("state.lock"))?;
        let store = self.todo_store();
        let _todo = FileLock::try_acquire(&store.with_file_name("todo.lock"))?;
        let db = self.read_json(&store)?;
        let job = self.load(id)?;
        let owner = self.load(owner_id)?;
        if !self.is_ours_checked(&job, "pid")?
            || self.is_ours_checked(&job, "codex_pid")?
            || self.is_ours_checked(&owner, "pid")?
            || self.is_ours_checked(&owner, "codex_pid")?
        {
            return Err(Error::new(
                "repair process identities changed or a worker remains live",
            ));
        }
        let items = db["items"]
            .as_array()
            .ok_or_else(|| Error::new("invalid ToDo store"))?;
        let spec = |key: &str| -> Result<Vec<u8>> {
            let row = items
                .iter()
                .find(|i| i["id"] == request[key])
                .ok_or_else(|| Error::new("repair ToDo missing"))?;
            if s(row, "spec").is_empty() {
                return Err(Error::new("saved finite spec missing"));
            }
            Ok(fs::read(Path::new(s(row, "spec")))?)
        };
        let repaired = cc_job_policy::owner_wait_repair::plan(
            &db,
            &job,
            &owner,
            request,
            &spec("todo_id")?,
            &spec("parent_todo_id")?,
            &fs::read(self.job_dir(id).join("prompt.md"))?,
        )
        .map_err(Error::new)?;
        if apply {
            self.save(&repaired)?;
        }
        Ok(json!({"job_id":id,"owner_job_id":owner_id,"applied":apply,
            "removed_wait":request["parent_todo_id"],"launched":0,"todo_changed":false,
            "remaining_work_wait_on":repaired["work_wait_on"]}))
    }
}
