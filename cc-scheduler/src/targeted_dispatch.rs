//! One explicit allocation through the existing dispatcher, without a global tick.
use crate::{backend::Backend, common::*, engine::Scheduler};
use serde_json::{json, Value};
use std::time::Duration;

pub fn parse_id(args: &[String]) -> Result<i64> {
    if args.len() != 2 || args[0] != "--id" {
        return Err(Error("InputRejected"));
    }
    args[1]
        .parse::<i64>()
        .ok()
        .filter(|id| *id > 0)
        .ok_or(Error("InputRejected"))
}

/// A targeted call is not authority to release any recorded stop or recovery.
pub(crate) fn held(item: &Value) -> bool {
    cc_job_policy::cancellation::owner_stopped(item, &Value::Null)
        || item["owner_stopped"] == true
        || item["owner_postponement_recorded"] == true
        || truth(&item["stop_kind"])
        || ["owner_question_wait", "decision_wait", "wait_ready"]
            .iter()
            .any(|field| truth(&item[*field]))
        // answers() keeps resolved questions as history. The existing priority
        // gate separately retains any unfinished question-close prerequisite.
        || (truth(&item["scheduler"]["question"])
            && !truth(&item["scheduler"]["answer"])
            && item["scheduler"]["retire_retry_question"] != true)
        || [
            "owner_stop_job",
            "pending_cancel",
        ]
        .iter()
        .any(|field| truth(&item["scheduler"][*field]))
}

pub fn run<B: Backend>(engine: &mut Scheduler<B>, id: i64) -> Result<Value> {
    if id <= 0 {
        return Err(Error("InputRejected"));
    }
    let Some(_lock) = ByteLock::acquire(&engine.root.join("scheduler.lock"), Duration::ZERO)?
    else {
        return Ok(json!({"id":id,"busy":true,"launched":null}));
    };
    engine.state = read_json(&engine.root.join("state.json"), json!({}))?;
    if !engine.state.is_object() {
        return Err(Error("AttributeError"));
    }
    engine.report = json!({});
    engine.failures.clear();
    let db = engine.todo()?;
    let item = arr(&db["items"])
        .into_iter()
        .find(|item| item["id"] == id)
        .ok_or(Error("InputRejected"))?;
    // Leave an already accepted watchdog reservation to its existing owner.
    if truth(&item["stall_recovery"]["pending"]) {
        return Ok(json!({"id":id,"busy":false,"launched":null,"recovery_pending":true}));
    }
    let (cfg, pri) = engine.settings()?;
    // This includes current global enablement, priority, capacity, workspace,
    // source protection, ownership, declared resources and subwork admission.
    let result = engine.dispatch_for(&cfg, &pri, Some(id));
    engine.persist()?;
    result?;
    // Do not return the sampled jobs, other task bodies or private source paths.
    Ok(
        json!({"id":id,"busy":false,"launched":engine.report["launched"]["job"],
        "dispatch_enabled":cfg["dispatch_enabled"],
        "priority_wait":engine.report["priority_wait"] == id,
        "allowance_now":engine.report["allowance_now"]}),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_one_explicit_positive_id_is_accepted() {
        assert_eq!(parse_id(&["--id".into(), "3280".into()]).unwrap(), 3280);
        for args in [
            vec![],
            vec!["3280"],
            vec!["--id", "0"],
            vec!["--id", "-1"],
            vec!["--id", "all"],
            vec!["--id", "1", "--id", "2"],
        ] {
            assert!(parse_id(&args.into_iter().map(String::from).collect::<Vec<_>>()).is_err());
        }
    }
}
