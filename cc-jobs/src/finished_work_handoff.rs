use crate::*;
use cc_job_policy::finished_work_handoff::{self, Request};
use std::path::Path;

impl Context {
    /// Acknowledge exactly one completed writer edge after validating its
    /// delivered Git object and the parent task's durable receipt. Admission is
    /// still performed by the existing queue; this method never launches.
    pub fn handoff_finished_work(&self, value: &Value, apply: bool) -> Result<Value> {
        let request: Request = serde_json::from_value(value.clone())
            .map_err(|_| Error::new("invalid finished-work handoff request"))?;
        let ids = [
            &request.job_id,
            &request.parent_job_id,
            &request.owner_job_id,
        ];
        if ids.iter().any(|id| !queue::valid_job_id(id)) {
            return Err(Error::new("handoff needs valid recorded job IDs"));
        }
        let _gate = self.queue_lock()?;
        let store = self.todo_store();
        let initial_db = self.read_json(&store)?;
        let initial_owner = initial_db["items"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|row| row["id"].as_i64() == Some(request.owner_todo_id))
            .ok_or_else(|| Error::new("writer ToDo record missing"))?;
        let initial_history = initial_owner["jobs"]
            .as_array()
            .ok_or_else(|| Error::new("writer job history missing"))?;
        let mut state_ids = vec![request.job_id.clone(), request.parent_job_id.clone()];
        let mut history_ids = Vec::with_capacity(initial_history.len());
        for id in initial_history {
            let id = id
                .as_str()
                .ok_or_else(|| Error::new("invalid writer job history"))?;
            if !queue::valid_job_id(id) {
                return Err(Error::new("invalid writer job history"));
            }
            history_ids.push(id.to_owned());
            state_ids.push(id.to_owned());
        }
        if !history_ids.contains(&request.owner_job_id) {
            return Err(Error::new(
                "selected writer is absent from its ToDo history",
            ));
        }
        state_ids.sort();
        state_ids.dedup();
        let mut _state_locks = Vec::with_capacity(state_ids.len());
        for id in &state_ids {
            _state_locks.push(FileLock::try_acquire(&self.job_dir(id).join("state.lock"))?);
        }
        let _todo = FileLock::try_acquire(&store.with_file_name("todo.lock"))?;
        let db = self.read_json(&store)?;
        let current_owner = db["items"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|row| row["id"].as_i64() == Some(request.owner_todo_id))
            .ok_or_else(|| Error::new("writer ToDo record missing"))?;
        if current_owner["jobs"] != initial_owner["jobs"] {
            return Err(Error::new(
                "writer history changed while locks were acquired",
            ));
        }
        let candidate = self.load(&request.job_id)?;
        let parent = self.load(&request.parent_job_id)?;
        let mut writer_jobs = Vec::with_capacity(history_ids.len());
        for id in &history_ids {
            let job = self.load(id)?;
            if !TERMINAL.contains(&s(&job, "status"))
                || self.is_ours_checked(&job, "pid")?
                || self.is_ours_checked(&job, "codex_pid")?
            {
                return Err(Error::new(
                    "writer history contains a live or nonterminal job",
                ));
            }
            writer_jobs.push(job);
        }
        let owner = writer_jobs
            .iter()
            .find(|job| s(job, "id") == request.owner_job_id)
            .ok_or_else(|| Error::new("selected writer record is missing"))?;
        if !self.is_ours_checked(&candidate, "pid")?
            || self.is_ours_checked(&candidate, "codex_pid")?
            || self.is_ours_checked(&parent, "pid")?
            || self.is_ours_checked(&parent, "codex_pid")?
        {
            return Err(Error::new(
                "handoff process identities changed or a worker remains live",
            ));
        }
        for worker in [&candidate, &parent, owner] {
            if self.rt.recovery_protected(s(worker, "cwd"))? {
                return Err(Error::new("handoff source is protected"));
            }
        }
        let item = |id: i64| -> Result<&Value> {
            db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .find(|row| row["id"].as_i64() == Some(id))
                .ok_or_else(|| Error::new("handoff ToDo record missing"))
        };
        let bytes = |id: i64| -> Result<Vec<u8>> {
            let path = s(item(id)?, "spec");
            if path.is_empty() {
                return Err(Error::new("handoff instruction file missing"));
            }
            Ok(self.read_file(Path::new(path))?)
        };
        let child_spec = bytes(request.todo_id)?;
        let parent_spec = bytes(request.parent_todo_id)?;
        let owner_spec = bytes(request.owner_todo_id)?;
        let prompt = self.read_file(&self.job_dir(&request.job_id).join("prompt.md"))?;

        let owner_cwd = Path::new(s(owner, "cwd"));
        let git = |args: &[String]| -> Result<String> {
            let mut argv = vec![
                "git".to_owned(),
                "--no-optional-locks".to_owned(),
                "-C".to_owned(),
                owner_cwd.to_string_lossy().into_owned(),
            ];
            argv.extend(args.iter().cloned());
            let reply = self.rt.call(Call::new(argv, vec![], 10.))?;
            if reply.code != 0 {
                return Err(Error::new(
                    "handoff source receipt is missing or inconsistent",
                ));
            }
            String::from_utf8(reply.stdout)
                .map_err(|_| Error::new("handoff Git receipt is not valid UTF-8"))
        };
        for commit in [&request.writer_code_head, &request.delivered_source_head] {
            git(&[
                "rev-parse".into(),
                "--verify".into(),
                format!("{commit}^{{commit}}"),
            ])?;
        }
        if git(&["rev-parse".into(), "HEAD".into()])?.trim() != request.delivered_source_head
            || !git(&["status".into(), "--porcelain".into()])?
                .trim()
                .is_empty()
        {
            return Err(Error::new(
                "writer checkout moved or changed after result delivery",
            ));
        }
        git(&[
            "merge-base".into(),
            "--is-ancestor".into(),
            request.writer_code_head.clone(),
            request.delivered_source_head.clone(),
        ])?;
        let changed = git(&[
            "diff-tree".into(),
            "--no-commit-id".into(),
            "--name-only".into(),
            "-r".into(),
            request.writer_code_head.clone(),
        ])?;
        let prefix = format!("{}/", request.writer_scope);
        if changed.lines().all(str::is_empty)
            || changed.lines().any(|path| !path.starts_with(&prefix))
        {
            return Err(Error::new(
                "writer source commit is empty or extends beyond its declared module",
            ));
        }
        let files = git(&[
            "ls-tree".into(),
            "-r".into(),
            "--name-only".into(),
            request.delivered_source_head.clone(),
            "--".into(),
            request.writer_scope.clone(),
        ])?;
        if files.lines().all(str::is_empty) {
            return Err(Error::new("delivered source has no files in writer module"));
        }
        let actual = [
            cc_job_policy::work_owner::checkout(s(&candidate, "cwd")),
            cc_job_policy::work_owner::checkout(s(&parent, "cwd")),
            cc_job_policy::work_owner::checkout(s(owner, "cwd")),
        ];
        for (index, worker) in [&candidate, &parent, owner].into_iter().enumerate() {
            let identity = cc_job_policy::work_owner::from_job(worker);
            if ["repository", "git_common", "worktree", "branch"]
                .iter()
                .any(|key| identity[*key] != actual[index][*key])
            {
                return Err(Error::new(
                    "handoff checkout no longer matches recorded ownership",
                ));
            }
        }
        let next = finished_work_handoff::plan(
            &db,
            &candidate,
            &parent,
            owner,
            &request,
            finished_work_handoff::Inputs {
                candidate_spec: &child_spec,
                parent_spec: &parent_spec,
                owner_spec: &owner_spec,
                prompt: &prompt,
            },
        )
        .map_err(Error::new)?;
        if apply {
            self.save(&next)?;
        }
        Ok(json!({
            "job_id":request.job_id,
            "owner_job_id":request.owner_job_id,
            "applied":apply,
            "removed_wait":request.owner_todo_id,
            "remaining_work_wait_on":next["work_wait_on"],
            "launched":0,
            "todo_changed":false,
            "permissions_changed":false
        }))
    }
}
