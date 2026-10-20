//! Diagnose one ordinary scheduler launch using its existing, bounded receipts.
//! No raw ToDo row, launch key or prompt text leaves this module.
use crate::{backend::Backend, common::*, engine::Scheduler, policy};
use crate::policy::Processes;
use serde_json::{json, Value};
use std::{fs::File, io::Read, path::Path};

fn read_text(path: &Path) -> Result<String> {
    let mut bytes = Vec::new();
    File::open(path)?.take(2 * 1024 * 1024 + 1).read_to_end(&mut bytes)?;
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
        ($name:literal, $condition:expr) => { checks[$name] = json!($condition); };
    }
    check!("repair_project", ["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&s(&item["project"])));
    check!("status", matches!(s(&item["status"]), "todo" | "doing" | "wait"));
    check!("dispatchable", item["dispatchable"] == true);
    check!("ordinary", item["subwork"].is_null());
    check!("key", key.len() == 32 && key.bytes().all(|c| c.is_ascii_hexdigit()));
    check!("unlinked", !truth(&scheduler["job"]));
    check!("no_stop", !truth(&scheduler["question"]) && !truth(&scheduler["owner_stop_job"])
        && !truth(&scheduler["pending_cancel"]) && item["stopped"] != true && item["owner_stopped"] != true
        && !item["stall_recovery"]["pending"].is_object() && item["wait_ready"].is_null());
    check!("selection", !s(&scheduler["selection"]["model"]).is_empty()
        && !s(&scheduler["selection"]["effort"]).is_empty()
        && ["model", "effort"].iter().all(|field| item[*field] == scheduler["selection_input"][*field]));
    check!("scope", checks["repair_project"] == true && !repo.is_empty()
        && !s(&scheduler["worktree"]).is_empty()
        && engine.backend.repair_scope_allowed(repo)
        && engine.backend.repair_scope_allowed(s(&scheduler["worktree"])));

    let matching: Vec<_> = jobs.iter().filter(|job| !key.is_empty() && job["launch_key"] == scheduler["key"]).collect();
    check!("unique_key", matching.len() == 1);
    let mut selected = Value::Null;
    if let [job] = matching.as_slice() {
        selected = job["id"].clone();
        check!("identity", job["todo_id"] == item["id"] && job["project"] == item["project"]
            && job["scope_source"] == "launcher" && !s(&job["cwd"]).is_empty()
            && norm(s(&job["cwd"])) == norm(s(&scheduler["worktree"])));
        check!("model", job["model"] == scheduler["selection"]["model"] && job["effort"] == scheduler["selection"]["effort"]);
        check!("no_parent", job["subwork"].is_null() && job["subwork_spec"].is_null()
            && job["assigned_needs"].is_null() && !truth(&job["parent_job_id"]));
        check!("unreceived", !result_received(item, &job["id"]));
        check!("exclusive", !jobs.iter().any(|other| other["id"] != job["id"]
            && (policy::live(other) || policy::alive(other, ps, engine.backend.clock()))
            && (other["todo_id"] == item["id"] || norm(s(&other["cwd"])) == norm(s(&scheduler["worktree"])))));
        check!("state", policy::live(job) || (job["status"] == "succeeded" && job["exit_code"] == 0
            && truth(&job["finished_at"]) && !policy::alive(job, ps, engine.backend.clock())));
        check!("waiting_result", item["status"] != "wait" || (job["status"] == "succeeded"
            && item["wait_on"].as_array().is_some_and(|ids| !ids.is_empty())));
        let source = engine.root.join("prompts").join(format!("{}-{key}.md", item["id"]));
        let job_id = s(&job["id"]);
        let valid_id = !job_id.is_empty() && job_id.bytes().all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b));
        let stored = engine.root.parent().unwrap_or(&engine.root).join("codex-jobs/jobs").join(job_id).join("prompt.md");
        check!("prompt_paths", valid_id && norm(s(&scheduler["prompt"])) == norm(&source.to_string_lossy())
            && norm(s(&job["prompt_source"])) == norm(&source.to_string_lossy())
            && norm(s(&job["prompt"])) == norm(&stored.to_string_lossy()));
        // Read only after the registered scope and exact operational paths agree.
        if checks["scope"] == true && checks["prompt_paths"] == true {
            let proof = (|| -> Result<(bool, bool, bool, bool)> {
                let original = read_text(&source)?;
                let actual = read_text(&stored)?;
                let spec = read_text(Path::new(s(&item["spec"])))?;
                let resources = item["needs"].as_array().filter(|ids| ids.iter().all(Value::is_string)).ok_or(Error("InputRejected"))?
                    .iter().map(s).collect::<Vec<_>>().join(", ");
                let guidance = if job["sandbox"] == "read-only" { cc_job_policy::READ_GUIDANCE } else { cc_job_policy::WRITE_GUIDANCE };
                let expected = if original.contains(guidance.trim()) { original.clone() } else { format!("{original}{guidance}") };
                Ok((!spec.trim().is_empty() && original.contains(&format!("\n依頼:\n{spec}")),
                    original.contains(&format!("\n割り当てた資源: {}\n", if resources.is_empty() { "なし" } else { &resources })),
                    original.contains(&format!("\n元の登録作業場所: {repo}\n")), actual == expected))
            })();
            if let Ok((spec, needs, root, prompt)) = proof {
                check!("spec_bytes", spec);
                check!("resources", needs);
                check!("registered_root", root);
                check!("stored_prompt", prompt);
            } else { check!("prompt_read", false); }
        } else { check!("prompt_read", false); }
    }
    let failed: Vec<_> = checks.as_object().unwrap().iter().filter(|(_, value)| *value != &json!(true)).map(|(name, _)| name.clone()).collect();
    json!({"id":item["id"],"job":selected,"status":item["status"],"phase":scheduler["phase"],
        "eligible":failed.is_empty(),"checks":checks,"failed":failed,"saved":false,"launched":0,
        "state_sha256":cc_job_policy::product_retry::digest(&serde_json::to_vec(item).unwrap())})
}

pub fn inspect<B: Backend>(engine: &mut Scheduler<B>, id: u64) -> Result<Value> {
    let db = engine.todo()?;
    let item = arr(&db["items"]).into_iter().find(|item| item["id"] == id).ok_or(Error("InputRejected"))?;
    if !["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&s(&item["project"])) {
        return Err(Error("InputRejected"));
    }
    let jobs = engine.backend.jobs()?;
    let ps = engine.backend.snapshot()?;
    Ok(inspect_item(engine, &db, &item, &jobs, &ps))
}
