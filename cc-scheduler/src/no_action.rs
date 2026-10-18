//! One bounded pass in the existing minute tick, independent of global kill_enabled.
use crate::{backend::Backend, common::*, engine::Scheduler, policy::*};
use cc_job_policy::action_progress::{self, Progress};
use serde_json::{json, Value};

pub fn idle_tree(job: &Value, ps: &Processes) -> bool {
    ["pid", "codex_pid"].iter().all(|k| matches(job, k, ps))
        && process_tree(job, ps).iter().all(|pid| {
            job["pid"] == *pid
                || job["codex_pid"] == *pid
                || matches!(
                    s(&ps[pid]["name"]).to_ascii_lowercase().as_str(),
                    "cmd.exe"
                        | "node.exe"
                        | "codex.exe"
                        | "node_repl.exe"
                        | "codex-code-mode-host.exe"
                        | "conhost.exe"
                )
        })
}

impl<B: Backend> Scheduler<B> {
    pub fn recover_no_action(&mut self, jobs: &mut [Value], ps: &Processes) -> Result<()> {
        let db = self.todo()?;
        let mut rows: Vec<_> = arr(&db["items"])
            .into_iter()
            .filter(|row| {
                ["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"]
                    .contains(&s(&row["project"]))
                    && jobs.iter().any(|j| action_progress::eligible(&db, row, j))
            })
            .collect();
        rows.sort_by_key(|r| r["id"].as_u64().unwrap_or(0));
        if rows.is_empty() {
            return Ok(());
        }
        let cursor = self.state["no_action_cursor"].as_u64().unwrap_or(0);
        let start = rows
            .iter()
            .position(|r| r["id"].as_u64().unwrap_or(0) > cursor)
            .unwrap_or(0);
        // At most two eligible workers and one cancellation per tick; no new loop/service.
        for row in rows.iter().cycle().skip(start).take(rows.len().min(2)) {
            self.state["no_action_cursor"] = row["id"].clone();
            let Some(job) = jobs.iter_mut().find(|j| j["id"] == row["scheduler"]["job"]) else {
                continue;
            };
            if n(&row["scheduler"]["no_action_retry"]["not_before"]) > self.backend.clock() {
                continue;
            }
            if !idle_tree(job, ps) {
                remove(&mut self.state["no_action"], s(&job["id"]));
                continue;
            }
            let repo = s(&db["projects"][s(&row["project"])]);
            if !self.backend.no_action_scope(repo, row, job)? {
                remove(&mut self.state["no_action"], s(&job["id"]));
                continue;
            }
            let previous: Progress =
                serde_json::from_value(self.state["no_action"][s(&job["id"])].clone())
                    .unwrap_or_default();
            let Some(progress) = self.backend.action_progress(job, &previous)? else {
                remove(&mut self.state["no_action"], s(&job["id"]));
                continue;
            };
            if !self.state["no_action"].is_object() {
                self.state["no_action"] = json!({});
            }
            self.state["no_action"][s(&job["id"])] = serde_json::to_value(&progress)?;
            if !progress.stalled(self.backend.clock()) {
                continue;
            }
            let current = self.todo()?;
            let Some(fresh) = arr(&current["items"])
                .into_iter()
                .find(|r| r["id"] == row["id"])
            else {
                continue;
            };
            if fresh != *row
                || !action_progress::eligible(&current, &fresh, job)
                || current["projects"] != db["projects"]
                || !idle_tree(job, &self.backend.snapshot()?)
            {
                continue;
            }
            let at = self.backend.clock();
            let mut reserved_here = false;
            let reserved=self.edit(&fresh["id"], |r| {
                if *r != fresh {return;}
                reserved_here=true;
                let old=&r["scheduler"]["no_action_retry"];
                let count=if old["job"]==job["id"] && old["attempt"]==progress.attempt {old["count"].as_u64().unwrap_or(0).saturating_add(1)}else{1};
                r["scheduler"]["no_action_retry"]=json!({"job":job["id"],"attempt":progress.attempt,"count":count,
                    "not_before":at+if count<2 {crate::recovery_failure::FAST_RETRY_S}else{crate::recovery_failure::SLOW_RETRY_S}});
            })?;
            if !reserved_here {
                continue;
            }
            let proof = json!({"ticket":action_progress::ticket(&current, &reserved, job),"progress":progress});
            // The launcher rechecks generation, waits, stop, event cursor and roots under its locks.
            let stopped =
                match self
                    .backend
                    .cancel_no_action(&reserved, job, &proof, &self.todo_path)
                {
                    Ok(stopped) => stopped,
                    Err(error) => {
                        push(
                            &mut self.report["no_action_deferred"],
                            json!({"item":reserved["id"],"reason":error.0}),
                        );
                        false
                    }
                };
            if stopped {
                self.requeue_if(&reserved["id"], &job["id"], "no_action", Some(&reserved))?;
                push(
                    &mut self.report["no_action_recovered"],
                    json!({"item":fresh["id"],"job":job["id"]}),
                );
            }
            break;
        }
        // Completed workers have no active cursor; avoid unbounded state growth.
        if let Some(states) = self.state["no_action"].as_object_mut() {
            states.retain(|id, _| jobs.iter().any(|j| s(&j["id"]) == id && live(j)));
        }
        Ok(())
    }
}
