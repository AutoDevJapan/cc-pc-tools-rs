mod common;
use cc_ask::{commands::cmd_sweep, core::*};
use common::{Harness, SharedFake};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{cell::Cell, path::PathBuf, rc::Rc};

fn thread(h: &Harness, age: f64, name: &str) -> String {
    let id = ((((h.ctx.now() - age) * 1000.) as u64 - 1420070400000) << 22).to_string();
    h.fake.borrow_mut().threads.insert(
        id.clone(),
        json!({
            "id":id,"last_message_id":id,"parent_id":"T","name":name,
            "thread_metadata":{"archived":false}
        }),
    );
    id
}
fn task(h: &Harness, id: &str, tid: &str, status: &str) {
    let name = format!("{:x}.json", Sha256::digest(id.as_bytes()));
    atomic_json(
        &h.ctx.discord_tasks.join(name),
        &json!({"id":id,"thread_id":tid,"status":status}),
        2,
    )
    .unwrap();
}
fn job(h: &Harness, id: &str, status: &str) {
    let dir = h.ctx.jobs.join("jobs").join(id);
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":id,"status":status,"thread_id":"codex-session-uuid"}),
        2,
    )
    .unwrap();
}

#[test]
fn large_notifier_inventory_stops_without_authorizing_partial_cleanup() {
    let h = Harness::new();
    for index in 0..2000 {
        task(&h, &format!("old-{index}"), "777", "completed");
    }
    let deadline = cc_ask::tick_runtime::Deadline::new(std::time::Duration::from_millis(10));
    let start = std::time::Instant::now();
    let result = cc_ask::sweep::protected_threads(&h.ctx, &json!({"requests":{}}));
    drop(deadline);
    assert!(result.unwrap_err().starts_with("TickDeadline:"));
    assert!(start.elapsed() < std::time::Duration::from_secs(1));
    assert!(h.fake.borrow().patches.is_empty());
}

#[test]
fn progress_edits_keep_old_job_thread_open_across_sweeps_and_finish_releases_it() {
    let mut h = Harness::new();
    let live = thread(&h, 7200., "old progress card");
    let unrelated = thread(&h, 7100., "quiet unrelated task");
    task(&h, "long-job", &live, "in_progress");
    job(&h, "long-job", "running");
    for _ in 0..3 {
        cmd_sweep(&mut h.ctx).unwrap();
        h.fake.borrow_mut().now += 60.;
    }
    assert_eq!(
        h.fake.borrow().patches,
        vec![(unrelated, json!({"archived":true}))]
    );
    assert_eq!(
        h.fake.borrow().threads[&live]["thread_metadata"]["archived"],
        false
    );
    // A delayed finish notice must not keep a stopped job's old progress card open forever.
    job(&h, "long-job", "succeeded");
    cmd_sweep(&mut h.ctx).unwrap();
    assert_eq!(
        h.fake.borrow().patches.last(),
        Some(&(live, json!({"archived":true})))
    );
}

#[test]
fn queued_starting_running_and_unknown_job_states_are_protected_even_with_stale_task_status() {
    let mut h = Harness::new();
    for (i, status) in ["queued", "starting", "running", "future-active"]
        .iter()
        .enumerate()
    {
        let tid = thread(&h, 7200. + i as f64, status);
        let id = format!("job-{i}");
        task(&h, &id, &tid, "completed");
        job(&h, &id, status);
    }
    cmd_sweep(&mut h.ctx).unwrap();
    assert!(h.fake.borrow().patches.is_empty());
}

#[test]
fn all_terminal_jobs_release_quiet_threads_but_recent_threads_and_questions_stay() {
    let mut h = Harness::new();
    let mut expected = vec![];
    for (i, status) in ["succeeded", "failed", "cancelled", "lost"]
        .iter()
        .enumerate()
    {
        let tid = thread(&h, 7200. + i as f64, status);
        let id = format!("job-{i}");
        task(&h, &id, &tid, "in_progress");
        job(&h, &id, status);
        expected.push((tid, json!({"archived":true})));
    }
    let question = thread(&h, 8000., "open question");
    h.save(&json!({"questions":[{"thread_id":question,"open":true}]}));
    thread(&h, 60., "recent task");
    cmd_sweep(&mut h.ctx).unwrap();
    let mut actual = h.fake.borrow().patches.clone();
    actual.sort_by(|a, b| a.0.cmp(&b.0));
    expected.sort_by(|a, b| a.0.cmp(&b.0));
    assert_eq!(actual, expected);
}

#[test]
fn standalone_active_tasks_and_waiting_answers_stay_finished_standalone_tasks_close() {
    let mut h = Harness::new();
    for (i, status) in ["in_progress", "waiting_for_answer"].iter().enumerate() {
        let tid = thread(&h, 7200. + i as f64, status);
        task(&h, &format!("standalone-{i}"), &tid, status);
    }
    let waiting = thread(&h, 7300., "answer still pending after job exit");
    task(&h, "pending-answer", &waiting, "waiting_for_answer");
    job(&h, "pending-answer", "succeeded");
    let finished = thread(&h, 7400., "finished standalone");
    task(&h, "standalone-done", &finished, "completed");
    cmd_sweep(&mut h.ctx).unwrap();
    assert_eq!(
        h.fake.borrow().patches,
        vec![(finished, json!({"archived":true}))]
    );
}

#[test]
fn unknown_task_status_aborts_even_when_associated_job_has_finished() {
    for status in ["future-active", ""] {
        for terminal in ["succeeded", "failed", "cancelled", "lost"] {
            let mut h = Harness::new();
            let tid = thread(&h, 7200., "unknown state");
            task(&h, "job", &tid, status);
            job(&h, "job", terminal);
            assert_eq!(
                cmd_sweep(&mut h.ctx).unwrap_err(),
                "Invalid Discord task status; cleanup skipped"
            );
            assert!(h.fake.borrow().patches.is_empty());
        }
    }
}

#[test]
fn missing_referenced_job_stays_open_and_invalid_request_mapping_aborts_cleanup() {
    let mut h = Harness::new();
    let tid = thread(&h, 7200., "missing job");
    h.save(&json!({"questions":[],"requests":{&tid:{"job":"missing-job"}}}));
    cmd_sweep(&mut h.ctx).unwrap();
    assert!(h.fake.borrow().patches.is_empty());
    for requests in [
        json!([]),
        json!({&tid:[]}),
        json!({&tid:{"job":123}}),
        json!({&tid:{"job":"../bad"}}),
    ] {
        h.save(&json!({"questions":[],"requests":requests}));
        assert!(cmd_sweep(&mut h.ctx).is_err());
        assert!(h.fake.borrow().patches.is_empty());
    }
}

#[test]
fn candidate_with_busy_notifier_lock_stays_open() {
    let mut h = Harness::new();
    let tid = thread(&h, 7200., "notifier updating");
    task(&h, "job", &tid, "in_progress");
    job(&h, "job", "succeeded");
    let path = h
        .ctx
        .discord_tasks
        .join(format!("{:x}.lock", Sha256::digest(b"job")));
    let lock = std::fs::OpenOptions::new()
        .create(true)
        .write(true)
        .truncate(false)
        .open(path)
        .unwrap();
    lock.lock().unwrap();
    cmd_sweep(&mut h.ctx).unwrap();
    assert!(h.fake.borrow().patches.is_empty());
    drop(lock);
    cmd_sweep(&mut h.ctx).unwrap();
    assert_eq!(
        h.fake.borrow().patches,
        vec![(tid, json!({"archived":true}))]
    );
}

struct RaceEffects {
    shared: SharedFake,
    after_patch: Option<(String, PathBuf, Value)>,
    check_lock: Option<(String, PathBuf, Rc<Cell<usize>>)>,
}
impl Effects for RaceEffects {
    fn now(&self) -> f64 {
        self.shared.now()
    }
    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        Err("no process allowed".into())
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if let Some((tid, path, checks)) = &self.check_lock {
            if url.ends_with(&format!("/channels/{tid}")) {
                let file = std::fs::OpenOptions::new().write(true).open(path).unwrap();
                assert!(
                    file.try_lock().is_err(),
                    "notifier lock must cover both GET and PATCH"
                );
                checks.set(checks.get() + 1);
            }
        }
        let reply = self.shared.http(method, url, body, discord)?;
        if method == "PATCH" {
            if let Some((tid, path, state)) = &self.after_patch {
                if url.ends_with(&format!("/channels/{tid}")) {
                    atomic_json(path, state, 2)?;
                }
            }
        }
        Ok(reply)
    }
}

#[test]
fn answer_started_after_snapshot_is_rechecked_before_archiving_second_candidate() {
    let mut h = Harness::new();
    let first = thread(&h, 7400., "unrelated quiet thread");
    let second = thread(&h, 7200., "late question");
    task(&h, "late-job", &second, "in_progress");
    job(&h, "late-job", "succeeded");
    let path = h
        .ctx
        .discord_tasks
        .join(format!("{:x}.json", Sha256::digest(b"late-job")));
    h.ctx.effects = Box::new(RaceEffects {
        shared: SharedFake(h.fake.clone()),
        after_patch: Some((
            first.clone(),
            path,
            json!({"id":"late-job","thread_id":second,"status":"waiting_for_answer"}),
        )),
        check_lock: None,
    });
    cmd_sweep(&mut h.ctx).unwrap();
    assert_eq!(
        h.fake.borrow().patches,
        vec![(first, json!({"archived":true}))]
    );
}

#[test]
fn notifier_lock_is_held_through_freshness_read_and_archive_patch() {
    let mut h = Harness::new();
    let tid = thread(&h, 7200., "finished task");
    task(&h, "job", &tid, "completed");
    job(&h, "job", "succeeded");
    let path = h
        .ctx
        .discord_tasks
        .join(format!("{:x}.lock", Sha256::digest(b"job")));
    let checks = Rc::new(Cell::new(0));
    h.ctx.effects = Box::new(RaceEffects {
        shared: SharedFake(h.fake.clone()),
        after_patch: None,
        check_lock: Some((tid.clone(), path.clone(), checks.clone())),
    });
    cmd_sweep(&mut h.ctx).unwrap();
    assert_eq!(checks.get(), 2);
    assert_eq!(
        h.fake.borrow().patches,
        vec![(tid, json!({"archived":true}))]
    );
    let file = std::fs::OpenOptions::new().write(true).open(path).unwrap();
    assert!(
        file.try_lock().is_ok(),
        "notifier lock is released after sweep"
    );
}

#[test]
fn request_mapping_protects_running_job_without_notifier_state() {
    let mut h = Harness::new();
    let live = thread(&h, 7200., "request with running job");
    job(&h, "request-job", "running");
    h.save(&json!({"questions":[],"requests":{live:{"job":"request-job"}}}));
    cmd_sweep(&mut h.ctx).unwrap();
    assert!(h.fake.borrow().patches.is_empty());
}

#[test]
fn unreadable_task_directory_or_corrupt_task_aborts_before_archiving() {
    let mut h = Harness::new();
    thread(&h, 7200., "quiet");
    std::fs::write(
        h.ctx.discord_tasks.join("broken.json"),
        "{broken secret must not appear",
    )
    .unwrap();
    let err = cmd_sweep(&mut h.ctx).unwrap_err();
    assert_eq!(err, "Invalid Discord task state; cleanup skipped");
    assert!(h.fake.borrow().patches.is_empty());
    h.ctx.discord_tasks = h.root.path().join("missing");
    assert_eq!(
        cmd_sweep(&mut h.ctx).unwrap_err(),
        "Discord task state unavailable; cleanup skipped"
    );
    assert!(h.fake.borrow().patches.is_empty());
}

#[test]
fn corrupt_or_missing_job_status_aborts_before_archiving() {
    for payload in ["{broken", "{}", "{\"status\":null}"] {
        let mut h = Harness::new();
        let tid = thread(&h, 7200., "active");
        thread(&h, 7100., "quiet unrelated");
        task(&h, "broken-job", &tid, "in_progress");
        job(&h, "broken-job", "running");
        std::fs::write(h.ctx.jobs.join("jobs/broken-job/job.json"), payload).unwrap();
        assert!(cmd_sweep(&mut h.ctx).is_err());
        assert!(h.fake.borrow().patches.is_empty());
    }
}

#[test]
fn incomplete_task_creation_and_non_json_lock_files_do_not_block_other_cleanup() {
    let mut h = Harness::new();
    let quiet = thread(&h, 7200., "quiet");
    task(&h, "creating-job", "", "in_progress");
    std::fs::write(h.ctx.discord_tasks.join("state.lock"), "not JSON").unwrap();
    cmd_sweep(&mut h.ctx).unwrap();
    assert_eq!(
        h.fake.borrow().patches,
        vec![(quiet, json!({"archived":true}))]
    );
}
