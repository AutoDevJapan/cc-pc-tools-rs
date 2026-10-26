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
        if ["valid", "incomplete", "stale", "future"].contains(&kind) {
            assert_eq!(result.unwrap(), ids);
        } else {
            assert!(result.unwrap().is_empty(), "{kind}");
        }
        assert_eq!(fs::read(&path).unwrap(), bytes);
    }
}

#[test]
fn stale_index_inspection_finds_unindexed_waiters_without_mutating_the_old_index() {
    let root = tempfile::tempdir().unwrap();
    let mut ctx = Context::installed().unwrap();
    ctx.root = root.path().to_path_buf();
    ctx.jobs = ctx.root.join("jobs");
    fs::create_dir(&ctx.jobs).unwrap();
    let _queue = FileLock::try_acquire(&ctx.root.join("queue.lock")).unwrap();
    let stale = b"{\"scan_complete\":true,\"scanned_at\":1,\"ids\":[]}";
    let index = ctx.jobs.join(".queue-index.json");
    fs::write(&index, stale).unwrap();
    fs::create_dir(ctx.jobs.join("non_job_metadata")).unwrap();
    for (id, status) in [
        ("20261026-000001-todo-1", "queued"),
        ("20261026-000002-todo-2", "running"),
        ("20261026-000003-todo-3", "succeeded"),
    ] {
        let dir = ctx.job_dir(id);
        fs::create_dir(&dir).unwrap();
        fs::write(
            dir.join("job.json"),
            serde_json::to_vec(&json!({"id":id,"status":status})).unwrap(),
        )
        .unwrap();
    }
    assert_eq!(
        ctx.active_job_ids_snapshot().unwrap(),
        vec!["20261026-000001-todo-1", "20261026-000002-todo-2"]
    );
    assert_eq!(fs::read(index).unwrap(), stale);
}

#[cfg(windows)]
#[test]
fn terminal_job_with_live_supervisor_is_kept_even_when_a_fresh_index_omits_it() {
    let root = tempfile::tempdir().unwrap();
    let mut ctx = Context::installed().unwrap();
    ctx.root = root.path().to_path_buf();
    ctx.jobs = ctx.root.join("jobs");
    fs::create_dir(&ctx.jobs).unwrap();
    let _queue = FileLock::try_acquire(&ctx.root.join("queue.lock")).unwrap();
    let index = ctx.jobs.join(".queue-index.json");
    let bytes =
        serde_json::to_vec(&json!({"scan_complete":true,"scanned_at":ctx.rt.epoch(),"ids":[]}))
            .unwrap();
    fs::write(&index, &bytes).unwrap();
    let id = "20261026-000001-todo-1";
    let dir = ctx.job_dir(id);
    fs::create_dir(&dir).unwrap();
    let pid = std::process::id();
    fs::write(
        dir.join("job.json"),
        serde_json::to_vec(
            &json!({"id":id,"status":"succeeded","pid":pid,"pid_created":ctx.rt.proc_created(pid)}),
        )
        .unwrap(),
    )
    .unwrap();
    assert_eq!(ctx.active_job_ids_snapshot().unwrap(), vec![id]);
    assert_eq!(fs::read(&index).unwrap(), bytes);
}
