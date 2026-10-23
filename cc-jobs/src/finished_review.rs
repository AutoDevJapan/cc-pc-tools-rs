use crate::*;
use cc_job_policy::finished_review::{self, Request};
use std::{fs, path::Path};

impl Context {
    /// A normal scheduler continuation has a new prompt and job ID. It cannot
    /// reuse the old qualification, nor silently return to launcher privileges.
    /// Read only this ToDo's recorded history while registration holds its lock.
    pub(crate) fn inherit_fixed_review_requirement(
        &self,
        db: &Value,
        job: &mut Value,
    ) -> Result<()> {
        let Some(row) = db["items"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|row| job["todo_id"].is_u64() && row["id"] == job["todo_id"])
        else {
            return Ok(());
        };
        for id in row["jobs"].as_array().into_iter().flatten().rev() {
            let id = id
                .as_str()
                .ok_or_else(|| Error::new("invalid fixed review history ID"))?;
            if !queue::valid_job_id(id) {
                return Err(Error::new("invalid fixed review history ID"));
            }
            // Legacy ToDos also retain native Codex session UUIDs. They are
            // not managed queue records and cannot carry this protocol. Only
            // skip an absent UUID directory; an existing/unreadable record or
            // a missing managed job must still fail the ordinary strict load.
            if normalize_uuid(id).as_deref() == Some(id) {
                match fs::symlink_metadata(self.job_dir(id)) {
                    Err(error) if error.kind() == std::io::ErrorKind::NotFound => continue,
                    Err(error) => {
                        return Err(Error::file_io(
                            "inspect review history",
                            &self.job_dir(id),
                            error,
                        ));
                    }
                    Ok(_) => (),
                }
            }
            let source = self.load(id)?;
            if !source["fixed_read_only_review"].is_null()
                || !source["fixed_review_requalification"].is_null()
            {
                // A qualified continuation can fail after starting. Retain its
                // original anchor instead of making that failed turn the next
                // qualification's successful-source prerequisite.
                let source_id = if !source["fixed_review_requalification"].is_null() {
                    &source["fixed_review_requalification"]["source_job_id"]
                } else {
                    &source["id"]
                };
                if !source_id.as_str().is_some_and(queue::valid_job_id) {
                    return Err(Error::new("invalid fixed review continuation source"));
                }
                job["fixed_review_requalification"] = json!({"source_job_id":source_id});
                break;
            }
        }
        Ok(())
    }

    pub fn qualify_finished_review(&self, r: &Request, apply: bool) -> Result<Value> {
        let ids = [&r.job_id, &r.parent_job_id, &r.owner_job_id];
        if ids.iter().any(|id| !queue::valid_job_id(id))
            || r.job_id == r.parent_job_id
            || r.job_id == r.owner_job_id
            || r.parent_job_id == r.owner_job_id
        {
            return Err(Error::new(
                "fixed review needs three distinct recorded jobs",
            ));
        }
        let _gate = self.queue_lock()?;
        let _reader = FileLock::try_acquire(&self.job_dir(&r.job_id).join("state.lock"))?;
        let _parent = FileLock::try_acquire(&self.job_dir(&r.parent_job_id).join("state.lock"))?;
        let _writer = FileLock::try_acquire(&self.job_dir(&r.owner_job_id).join("state.lock"))?;
        let store = self.todo_store();
        let job = self.load(&r.job_id)?;
        let parent = self.load(&r.parent_job_id)?;
        let owner = self.load(&r.owner_job_id)?;
        let source_id = if !job["fixed_review_requalification"].is_null() {
            let id = s(&job["fixed_review_requalification"], "source_job_id");
            if !queue::valid_job_id(id) || ids.iter().any(|other| other.as_str() == id) {
                return Err(Error::new("invalid fixed review continuation source"));
            }
            Some(id)
        } else {
            None
        };
        let _source_state = source_id
            .map(|id| FileLock::try_acquire(&self.job_dir(id).join("state.lock")))
            .transpose()?;
        let _todo = FileLock::try_acquire(&store.with_file_name("todo.lock"))?;
        let source = if let Some(id) = source_id {
            let source = self.load(id)?;
            if self.is_ours_checked(&source, "pid")?
                || self.is_ours_checked(&source, "codex_pid")?
            {
                return Err(Error::new("fixed review continuation source is still live"));
            }
            let old = &source["fixed_read_only_review"];
            if finished_review::hex(s(&old["request"], "prompt_sha256"), 64)
                && cc_job_policy::product_retry::digest(&fs::read(
                    self.job_dir(id).join("prompt.md"),
                )?) == old["request"]["prompt_sha256"]
                && cc_job_policy::product_retry::digest(&fs::read(Path::new(s(old, "spec")))?)
                    == old["request"]["child_spec_sha256"]
            {
                Some(source)
            } else {
                return Err(Error::new(
                    "fixed review continuation original inputs changed",
                ));
            }
        } else {
            None
        };
        if !self.is_ours_checked(&job, "pid")?
            || self.is_ours_checked(&job, "codex_pid")?
            || [&parent, &owner]
                .iter()
                .any(|j| self.is_ours(j, "pid") || self.is_ours(j, "codex_pid"))
        {
            return Err(Error::new(
                "fixed review process identity is live or unverified",
            ));
        }
        // A standard targeted reload must bring the old queued supervisor onto
        // this protocol BEFORE its wait is removed. Never fake a launch receipt.
        let ack = &job["fixed_review_protocol"];
        if ack["version"] != finished_review::EXECUTION_PROTOCOL
            || ack["pid"] != job["pid"]
            || ack["created"] != job["pid_created"]
        {
            return Err(Error::new(
                "reload the exact queued supervisor before qualifying its fixed review",
            ));
        }
        for worker in [&job, &parent, &owner] {
            if self.rt.recovery_protected(s(worker, "cwd"))? {
                return Err(Error::new("fixed review resource is protected"));
            }
        }
        let db = self.read_json(&store)?;
        let spec = |id: i64| -> Result<Vec<u8>> {
            let row = db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .find(|i| i["id"] == id)
                .ok_or_else(|| Error::new("fixed review ToDo missing"))?;
            if s(row, "spec").is_empty() {
                return Err(Error::new("fixed review spec missing"));
            }
            Ok(fs::read(Path::new(s(row, "spec")))?)
        };
        let mut next = finished_review::plan_continuation(
            &db,
            &job,
            &parent,
            &owner,
            r,
            [
                &spec(r.todo_id)?,
                &spec(r.parent_todo_id)?,
                &fs::read(self.job_dir(&r.job_id).join("prompt.md"))?,
            ],
            source.as_ref(),
        )
        .map_err(Error::new)?;
        let git = |args: &[&str]| -> Result<Reply> {
            let mut argv = vec![
                "git".into(),
                "--no-optional-locks".into(),
                "-C".into(),
                s(&parent, "cwd").into(),
            ];
            argv.extend(args.iter().map(|s| s.to_string()));
            let reply = self.rt.call(Call::new(argv, vec![], 10.))?;
            if reply.code != 0 {
                return Err(Error::new(
                    "fixed review source is missing, changed or not a clean ancestor range",
                ));
            }
            Ok(reply)
        };
        if !finished_review::hex(&r.fixed_head, 40)
            || !finished_review::hex(&r.fixed_base, 40)
            || String::from_utf8_lossy(&git(&["rev-parse", "--verify", "HEAD"])?.stdout).trim()
                != r.fixed_head
        {
            return Err(Error::new("fixed review source HEAD changed"));
        }
        git(&["merge-base", "--is-ancestor", &r.fixed_base, &r.fixed_head])?;
        git(&["diff", "--quiet", "--exit-code", "HEAD", "--"])?;
        let last = next["work_wait_history"]
            .as_array_mut()
            .unwrap()
            .last_mut()
            .unwrap();
        last["checked_at"] = json!(self.rt.now());
        if apply {
            self.save(&next)?;
        }
        Ok(
            json!({"job_id":r.job_id,"applied":apply,"proposed_removed_wait":
                if job["work_wait_on"] == json!([r.owner_todo_id]) {Some(r.owner_todo_id)} else {None},
            "launched":0,"todo_changed":false,"sandbox":"read-only","fixed_head":r.fixed_head}),
        )
    }

    pub(crate) fn fixed_review_guidance(
        &self,
        job: &Value,
        original_prompt: &[u8],
    ) -> Result<String> {
        let b = &job["fixed_read_only_review"];
        if b.is_null() {
            return Ok(String::new());
        }
        let db = self.read_json(&self.todo_store())?;
        let current = db["items"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|row| row["id"] == job["todo_id"])
            .ok_or_else(|| Error::new("fixed review ToDo missing before execution"))?;
        if current["spec"] != b["spec"] || s(current, "spec").is_empty() {
            return Err(Error::new(
                "fixed review specification changed before execution",
            ));
        }
        let spec = self.read_file(Path::new(s(current, "spec")))?;
        let r = finished_review::binding(&db, job, &spec, original_prompt).map_err(Error::new)?;
        if self.rt.recovery_protected(s(job, "cwd"))?
            || self.rt.recovery_protected(s(b, "source_cwd"))?
        {
            return Err(Error::new("fixed review resource became protected"));
        }
        Ok(format!(
            "\n\n## 固定した読み取り専用レビュー\n元の依頼範囲だけを読み、結果を最終応答で返してください。書き込み・配備・検査実行は行いません。\n読取元: {}\n固定base: {}\n固定head: {}\n作業場所の最新差分ではなく、このbase/headのGitオブジェクトを使います。元promptと仕様は保持しています。\n{}",
            s(b, "source_cwd"),
            r.fixed_base,
            r.fixed_head,
            cc_job_policy::READ_GUIDANCE
        ))
    }
}
