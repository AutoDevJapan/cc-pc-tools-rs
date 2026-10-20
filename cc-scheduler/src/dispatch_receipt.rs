//! Diagnose one ordinary scheduler launch using its existing, bounded receipts.
//! No raw ToDo row, launch key or prompt text leaves this module.
use crate::policy::Processes;
use crate::{backend::Backend, common::*, engine::Scheduler, policy};
use serde_json::{json, Value};
use std::{fs::File, io::Read, path::Path, time::Duration};

fn existing_workspace(repo: &str, item: &Value) -> bool {
    let verify = || -> Result<()> {
        let target = Path::new(s(&item["scheduler"]["worktree"]));
        let base = Path::new(repo);
        let mut runner = SystemRunner;
        if !s(&item["scheduler"]["branch"]).starts_with(&format!("scheduler/todo-{}-", item["id"]))
            || crate::preparation::git_root(&mut runner, base)?.is_none()
            || crate::preparation::git_root(&mut runner, target)?.is_none()
            || crate::backend::run_git(&mut runner, target, &["branch", "--show-current"])?
                != s(&item["scheduler"]["branch"])
        {
            return Err(Error("BranchMismatch"));
        }
        crate::preparation::same_common(&mut runner, base, target)
    };
    verify().is_ok()
}

fn read_text(path: &Path) -> Result<String> {
    let mut bytes = Vec::new();
    File::open(path)?
        .take(2 * 1024 * 1024 + 1)
        .read_to_end(&mut bytes)?;
    if bytes.len() > 2 * 1024 * 1024 {
        return Err(Error("InputRejected"));
    }
    String::from_utf8(bytes).map_err(|_| Error("InputRejected"))
}

pub(crate) fn inspect_item<B: Backend>(
    engine: &mut Scheduler<B>,
    db: &Value,
    item: &Value,
    jobs: &[Value],
    ps: &Processes,
) -> Value {
    let scheduler = &item["scheduler"];
    let repo = s(&db["projects"][s(&item["project"])]);
    let key = s(&scheduler["key"]);
    let mut checks = json!({});
    macro_rules! check {
        ($name:literal, $condition:expr) => {
            checks[$name] = json!($condition);
        };
    }
    check!(
        "repair_project",
        ["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&s(&item["project"]))
    );
    check!(
        "status",
        matches!(s(&item["status"]), "todo" | "doing" | "wait")
    );
    check!("dispatchable", item["dispatchable"] == true);
    check!("ordinary", item["subwork"].is_null());
    check!(
        "key",
        key.len() == 32 && key.bytes().all(|c| c.is_ascii_hexdigit())
    );
    check!("unlinked", !truth(&scheduler["job"]));
    check!("phase", scheduler["phase"] == "prepared");
    check!(
        "no_stop",
        !truth(&scheduler["question"])
            && !truth(&scheduler["owner_stop_job"])
            && !truth(&scheduler["pending_cancel"])
            && !truth(&scheduler["question_close_pending"])
            && item["stopped"] != true
            && item["owner_stopped"] != true
            && !cc_job_policy::cancellation::owner_stopped(item, &Value::Null)
            && !item["stall_recovery"]["pending"].is_object()
            && item["wait_ready"].is_null()
            && item["owner_question_wait"].is_null()
            && item["decision_wait"].is_null()
            && item["owner_postponement_recorded"] != true
            && s(&item["stop_kind"]).is_empty()
            && item["scheduler"]["console_request"].is_null()
    );
    check!(
        "selection",
        !s(&scheduler["selection"]["model"]).is_empty()
            && !s(&scheduler["selection"]["effort"]).is_empty()
            && ["model", "effort"]
                .iter()
                .all(|field| item[*field] == scheduler["selection_input"][*field])
    );
    let matching: Vec<_> = jobs
        .iter()
        .filter(|job| !key.is_empty() && job["launch_key"] == scheduler["key"])
        .collect();
    check!("unique_key", matching.len() == 1);
    let mut selected = Value::Null;
    if let [job] = matching.as_slice() {
        selected = job["id"].clone();
        check!(
            "identity",
            job["todo_id"] == item["id"]
                && job["project"] == item["project"]
                && job["scope_source"] == "launcher"
                && !s(&job["cwd"]).is_empty()
                && norm(s(&job["cwd"])) == norm(s(&scheduler["worktree"]))
        );
        check!(
            "model",
            job["model"] == scheduler["selection"]["model"]
                && job["effort"] == scheduler["selection"]["effort"]
        );
        check!(
            "no_parent",
            job["subwork"].is_null()
                && job["subwork_spec"].is_null()
                && job["assigned_needs"].is_null()
                && !truth(&job["parent_job_id"])
        );
        check!(
            "job_stop",
            !cc_job_policy::cancellation::owner_stopped(item, job)
        );
        check!("unreceived", !result_received(item, &job["id"]));
        check!(
            "exclusive",
            !jobs.iter().any(|other| other["id"] != job["id"]
                && (policy::live(other) || policy::alive(other, ps, engine.backend.clock()))
                && (other["todo_id"] == item["id"]
                    || norm(s(&other["cwd"])) == norm(s(&scheduler["worktree"]))))
        );
        check!(
            "state",
            policy::live(job)
                || (job["status"] == "succeeded"
                    && job["exit_code"] == 0
                    && truth(&job["finished_at"])
                    && !policy::alive(job, ps, engine.backend.clock()))
        );
        check!(
            "waiting_result",
            if item["status"] == "wait" {
                job["status"] == "succeeded"
                    && item["wait_on"].as_array().is_some_and(|ids| {
                        !ids.is_empty() && ids.iter().all(|id| id.as_u64().is_some_and(|id| id > 0))
                    })
            } else {
                !cc_job_policy::todo_wait::waiting(db, item)
            }
        );
        let source = engine
            .root
            .join("prompts")
            .join(format!("{}-{key}.md", item["id"]));
        let job_id = s(&job["id"]);
        let valid_id = !job_id.is_empty()
            && job_id
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b));
        let stored = engine
            .root
            .parent()
            .unwrap_or(&engine.root)
            .join("codex-jobs/jobs")
            .join(job_id)
            .join("prompt.md");
        check!(
            "prompt_paths",
            valid_id
                && norm(s(&scheduler["prompt"])) == norm(&source.to_string_lossy())
                && norm(s(&job["prompt_source"])) == norm(&source.to_string_lossy())
                && norm(s(&job["prompt"])) == norm(&stored.to_string_lossy())
        );
        // A prepared reservation is not a registered launch. Check its cheap
        // identity and generation before calling any external guard.
        let candidate = checks
            .as_object()
            .unwrap()
            .values()
            .all(|value| value == &json!(true));
        check!(
            "scope",
            candidate
                && !repo.is_empty()
                && !s(&scheduler["worktree"]).is_empty()
                && engine.backend.repair_scope_allowed(repo)
                && engine
                    .backend
                    .repair_scope_allowed(s(&scheduler["worktree"]))
        );
        // Read only after the registered scope and exact operational paths agree.
        if checks["scope"] == true && checks["prompt_paths"] == true {
            check!("workspace", existing_workspace(repo, item));
            let proof = (|| -> Result<(bool, bool, bool, bool)> {
                let original = read_text(&source)?;
                let actual = read_text(&stored)?;
                let spec = read_text(Path::new(s(&item["spec"])))?;
                let resources = item["needs"]
                    .as_array()
                    .filter(|ids| ids.iter().all(Value::is_string))
                    .ok_or(Error("InputRejected"))?
                    .iter()
                    .map(s)
                    .collect::<Vec<_>>()
                    .join(", ");
                let guidance = if job["sandbox"] == "read-only" {
                    cc_job_policy::READ_GUIDANCE
                } else {
                    cc_job_policy::WRITE_GUIDANCE
                };
                let expected = if original.contains(guidance.trim()) {
                    original.clone()
                } else {
                    format!("{original}{guidance}")
                };
                Ok((
                    !spec.trim().is_empty()
                        && original
                            .split_once("\n依頼:\n")
                            .is_some_and(|(_, tail)| tail == spec),
                    original
                        .split_once("\n割り当てた資源: ")
                        .and_then(|(_, tail)| tail.split_once('\n'))
                        .is_some_and(|(value, _)| {
                            value
                                == if resources.is_empty() {
                                    "なし"
                                } else {
                                    &resources
                                }
                        }),
                    original
                        .split_once("\n元の登録作業場所: ")
                        .and_then(|(_, tail)| tail.split_once('\n'))
                        .is_some_and(|(value, _)| value == repo),
                    actual == expected,
                ))
            })();
            if let Ok((spec, needs, root, prompt)) = proof {
                check!("spec_bytes", spec);
                check!("resources", needs);
                check!("registered_root", root);
                check!("stored_prompt", prompt);
            } else {
                check!("prompt_read", false);
            }
        } else {
            check!("prompt_read", false);
        }
    }
    let failed: Vec<_> = checks
        .as_object()
        .unwrap()
        .iter()
        .filter(|(_, value)| *value != &json!(true))
        .map(|(name, _)| name.clone())
        .collect();
    json!({"id":item["id"],"job":selected,"status":item["status"],"phase":scheduler["phase"],
        "eligible":failed.is_empty(),"checks":checks,"failed":failed,"saved":false,"launched":0,
        "state_sha256":cc_job_policy::product_retry::digest(&serde_json::to_vec(item).unwrap())})
}

fn recover_item<B: Backend>(
    engine: &mut Scheduler<B>,
    db: &Value,
    item: &Value,
    report: &Value,
) -> Result<bool> {
    if report["eligible"] != true {
        return Ok(false);
    }
    // Scope checks and Git can yield. Refresh real owners and the actual copied
    // instruction immediately before the full-row CAS; a new owner always wins.
    let fresh = engine.todo()?;
    if fresh["projects"][s(&item["project"])] != db["projects"][s(&item["project"])]
        || !arr(&fresh["items"]).contains(item)
    {
        return Ok(false);
    }
    let jobs = engine.backend.jobs()?;
    let ps = engine.backend.snapshot()?;
    let verified = inspect_item(engine, &fresh, item, &jobs, &ps);
    if verified["eligible"] != true || verified["job"] != report["job"] {
        return Ok(false);
    }
    let job = jobs
        .iter()
        .find(|job| job["id"] == report["job"])
        .ok_or(Error("InputRejected"))?;
    let at = engine.backend.clock();
    let mut saved = false;
    engine.edit_in_project(&item["id"], Some(s(&db["projects"][s(&item["project"])])), |row| {
        if row != item { return; }
        if !arr(&row["jobs"]).contains(&job["id"]) { push(&mut row["jobs"], job["id"].clone()); }
        if row["status"] == "wait" {
            row["scheduler"]["last_job"] = job["id"].clone();
            row["scheduler"]["phase"] = json!("waiting");
            row["scheduler"]["resume"] = json!(true);
            record_result(row, &job["id"], "dependency_wait");
        } else {
            row["status"] = json!("doing");
            remove(row, "dependency_ready");
            let start = epoch(if truth(&job["started_at"]) { &job["started_at"] } else { &job["created_at"] });
            merge(&mut row["scheduler"], json!({"job":job["id"],
                "phase":if policy::live(job) { "running" } else { "result_pending" },
                "started_s":if start != 0. { start } else { at }}));
        }
        row["scheduler"]["dispatch_receipt"] = json!({"job":job["id"],"source_state_sha256":report["state_sha256"],
            "generation":row["resume_generation"],"at":at});
        saved = true;
    })?;
    Ok(saved)
}

/// The normal repair tick uses the same proof. No new job is launched here.
pub(crate) fn recover_pending<B: Backend>(
    engine: &mut Scheduler<B>,
    jobs: &[Value],
    ps: &Processes,
) -> Result<()> {
    let db = engine.todo()?;
    for item in arr(&db["items"]) {
        if !["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&s(&item["project"]))
            || !item["subwork"].is_null()
            || truth(&item["scheduler"]["job"])
            || !matches!(s(&item["status"]), "todo" | "doing" | "wait")
            || !truth(&item["scheduler"]["key"])
            || jobs.iter().any(|job| {
                job["launch_key"] == item["scheduler"]["key"] && result_received(&item, &job["id"])
            })
        {
            continue;
        }
        let report = inspect_item(engine, &db, &item, jobs, ps);
        if engine.guard(&format!("adopt ordinary item {}", item["id"]), |engine| {
            recover_item(engine, &db, &item, &report)
        }) == Some(true)
        {
            push(
                &mut engine.report["repair_dispatches_adopted"],
                item["id"].clone(),
            );
        }
    }
    Ok(())
}

pub fn run<B: Backend>(
    engine: &mut Scheduler<B>,
    id: u64,
    expected: Option<&str>,
) -> Result<Value> {
    let _lock = if expected.is_some() {
        Some(
            ByteLock::acquire(&engine.root.join("scheduler.lock"), Duration::from_secs(30))?
                .ok_or(Error("TimeoutError"))?,
        )
    } else {
        None
    };
    let db = engine.todo()?;
    let item = arr(&db["items"])
        .into_iter()
        .find(|item| item["id"] == id)
        .ok_or(Error("InputRejected"))?;
    if id == 0
        || !["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&s(&item["project"]))
    {
        return Err(Error("InputRejected"));
    }
    let jobs = engine.backend.jobs()?;
    let ps = engine.backend.snapshot()?;
    let mut report = inspect_item(engine, &db, &item, &jobs, &ps);
    if let Some(expected) = expected {
        let receipt = &item["scheduler"]["dispatch_receipt"];
        let already_recorded = receipt["job"] == report["job"]
            && !s(&receipt["job"]).is_empty()
            && s(&receipt["source_state_sha256"]).eq_ignore_ascii_case(expected)
            && receipt["generation"] == item["resume_generation"]
            && arr(&item["jobs"]).contains(&receipt["job"])
            && report["checks"]["status"] == true
            && report["checks"]["no_stop"] == true
            && (item["scheduler"]["job"] == receipt["job"]
                || (item["status"] == "wait" && result_received(&item, &receipt["job"])));
        if already_recorded {
            report["already_recorded"] = json!(true);
            return Ok(report);
        }
        if report["eligible"] == true {
            if !s(&report["state_sha256"]).eq_ignore_ascii_case(expected) {
                return Err(Error("ReceiptMismatch"));
            }
            report["saved"] = json!(recover_item(engine, &db, &item, &report)?);
            if report["saved"] != true {
                return Err(Error("ReceiptMismatch"));
            }
        } else {
            return Err(Error("InputRejected"));
        }
    }
    Ok(report)
}

pub fn inspect<B: Backend>(engine: &mut Scheduler<B>, id: u64) -> Result<Value> {
    run(engine, id, None)
}
