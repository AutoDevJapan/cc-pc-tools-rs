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
        let started = std::time::Instant::now();
        let db = self.todo()?;
        let mut rows: Vec<_> = arr(&db["items"])
            .into_iter()
            .filter(|row| {
                // Match the existing ordinary dispatch boundary while the
                // original exclusions are unavailable. Never stop a worker
                // that this same recovery path cannot subsequently resume.
                ["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"]
                    .contains(&s(&row["project"]))
                    && jobs.iter().any(|j| action_progress::eligible(&db, row, j))
            })
            .collect();
        rows.sort_by_key(|r| r["id"].as_u64().unwrap_or(0));
        self.report["no_action_scan"] =
            json!({"eligible":rows.len(),"observed":0,"cancelled":0,"elapsed_ms":0});
        if rows.is_empty() {
            return Ok(());
        }
        let mut cursor = self.state["no_action_cursor"].as_u64().unwrap_or(0);
        let mut cycle_max = self.state["no_action_cycle_max"].as_u64().unwrap_or(0);
        if !rows.iter().any(|r| {
            r["id"]
                .as_u64()
                .is_some_and(|id| id > cursor && id <= cycle_max)
        }) {
            cursor = 0;
            self.state["no_action_cursor"] = json!(0);
            cycle_max = rows.last().and_then(|r| r["id"].as_u64()).unwrap_or(0);
        }
        // Freeze this pass's upper bound. New arrivals cannot postpone the wrap
        // to existing workers forever, even if two arrive on every minute tick.
        self.state["no_action_cycle_max"] = json!(cycle_max);
        // At most two eligible workers and one cancellation per tick; no new loop/service.
        for row in rows
            .iter()
            .filter(|r| {
                r["id"]
                    .as_u64()
                    .is_some_and(|id| id > cursor && id <= cycle_max)
            })
            .take(2)
        {
            if started.elapsed() >= std::time::Duration::from_secs(5) {
                break;
            }
            self.state["no_action_cursor"] = row["id"].clone();
            let Some(index) = jobs.iter().position(|j| j["id"] == row["scheduler"]["job"]) else {
                continue;
            };
            let parents: std::collections::BTreeMap<_, _> = jobs
                .iter()
                .filter_map(|j| j["id"].as_str().map(|id| (id, j)))
                .collect();
            if action_progress::active_descendant(
                &jobs[index],
                jobs,
                |j| alive(j, ps, self.backend.clock()),
                |id| {
                    let job = parents.get(id)?;
                    if job["parent_job_id"].is_null() {
                        Some(String::new())
                    } else {
                        job["parent_job_id"].as_str().map(str::to_owned)
                    }
                },
            ) {
                remove(&mut self.state["no_action"], s(&jobs[index]["id"]));
                continue;
            }
            let job = &mut jobs[index];
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
            self.report["no_action_scan"]["observed"] =
                add_count(&self.report["no_action_scan"]["observed"], 1);
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
            if started.elapsed() >= std::time::Duration::from_secs(5) {
                break;
            }
            let at = self.backend.clock();
            let mut reserved_here = false;
            let reserved = self.edit(&fresh["id"], |r| {
                if *r != fresh { return; }
                reserved_here = true;
                let old = &r["scheduler"]["no_action_retry"];
                let count = if old["job"] == job["id"] && old["attempt"] == progress.attempt {
                    old["count"].as_u64().unwrap_or(0).saturating_add(1)
                } else { 1 };
                let delay = if count < 2 { crate::recovery_failure::FAST_RETRY_S }
                    else { crate::recovery_failure::SLOW_RETRY_S };
                r["scheduler"]["no_action_retry"] = json!({"job":job["id"],"attempt":progress.attempt,
                    "count":count,"not_before":at+delay});
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
                self.report["no_action_scan"]["cancelled"] =
                    add_count(&self.report["no_action_scan"]["cancelled"], 1);
                self.requeue_if(&reserved["id"], &job["id"], "no_action", Some(&reserved))?;
                push(
                    &mut self.report["no_action_cancelled"],
                    json!({"item":fresh["id"],"job":job["id"]}),
                );
            }
            break;
        }
        self.report["no_action_scan"]["elapsed_ms"] =
            json!(started.elapsed().as_millis().min(u64::MAX as u128) as u64);
        // Completed workers have no active cursor; avoid unbounded state growth.
        if let Some(states) = self.state["no_action"].as_object_mut() {
            states.retain(|id, _| jobs.iter().any(|j| s(&j["id"]) == id && live(j)));
        }
        Ok(())
    }
}
