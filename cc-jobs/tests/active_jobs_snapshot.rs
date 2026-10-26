use cc_jobs::{Context, FileLock, json};
use std::fs;

#[test]
fn active_snapshot_is_read_only_fresh_complete_and_keeps_waiting_ids() {
    let root = tempfile::tempdir().unwrap();
    let mut ctx = Context::installed().unwrap();
    ctx.root = root.path().to_path_buf();
    ctx.jobs = ctx.root.join("jobs");
    fs::create_dir(&ctx.jobs).unwrap();
    let _queue = FileLock::try_acquire(&ctx.root.join("queue.lock")).unwrap();
    let path = ctx.jobs.join(".queue-index.json");
    let ids = vec!["20261026-000001-todo-1", "20261026-000002-todo-2"];
    for kind in [
        "valid",
        "incomplete",
        "stale",
        "future",
        "invalid-id",
        "broken",
    ] {
        let mut index = json!({"scan_complete":true,"scanned_at":ctx.rt.epoch(),"ids":ids});
        match kind {
            "incomplete" => index["scan_complete"] = json!(false),
            "stale" => index["scanned_at"] = json!(ctx.rt.epoch() - 31.),
            "future" => index["scanned_at"] = json!(ctx.rt.epoch() + 30.),
            "invalid-id" => index["ids"] = json!(["../outside"]),
            _ => (),
        }
        let bytes = if kind == "broken" {
            b"{broken".to_vec()
        } else {
            serde_json::to_vec(&index).unwrap()
        };
        fs::write(&path, &bytes).unwrap();
        let result = ctx.active_job_ids_snapshot();
        if kind == "valid" {
            assert_eq!(result.unwrap(), ids);
        } else {
            assert!(result.is_err(), "{kind}");
        }
        assert_eq!(fs::read(&path).unwrap(), bytes);
    }
}
