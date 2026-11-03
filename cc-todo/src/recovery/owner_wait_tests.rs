use super::*;
use cc_scheduler::received_input::ReadError;
use std::collections::{BTreeMap, VecDeque};

type GuardHook = Box<dyn FnMut(&Path, &mut BTreeMap<PathBuf, Vec<u8>>)>;

struct Fixture {
    _temp: tempfile::TempDir,
    paths: Paths,
    host: FakeHost,
    writes: std::cell::Cell<usize>,
}
struct FakeHost {
    files: BTreeMap<PathBuf, Vec<u8>>,
    reads: Vec<PathBuf>,
    guards: Vec<PathBuf>,
    verdicts: VecDeque<std::result::Result<bool, ReadError>>,
    hook: Option<GuardHook>,
    ended: Option<String>,
    started: String,
    now_calls: std::cell::Cell<usize>,
}
impl Host for FakeHost {
    fn read(&mut self, path: &Path, _: usize) -> std::result::Result<Vec<u8>, ReadError> {
        self.reads.push(path.into());
        self.files.get(path).cloned().ok_or(ReadError("not_found"))
    }
    fn guard(&mut self, path: &Path) -> std::result::Result<bool, ReadError> {
        self.guards.push(path.into());
        if let Some(hook) = &mut self.hook {
            hook(path, &mut self.files);
        }
        self.verdicts.pop_front().unwrap_or(Ok(true))
    }
    fn now(&self) -> String {
        self.now_calls.set(self.now_calls.get() + 1);
        if !self.guards.is_empty()
            && let Some(ended) = &self.ended
        {
            return ended.clone();
        }
        self.started.clone()
    }
}
impl Host for &mut FakeHost {
    fn read(&mut self, path: &Path, limit: usize) -> std::result::Result<Vec<u8>, ReadError> {
        (**self).read(path, limit)
    }
    fn guard(&mut self, path: &Path) -> std::result::Result<bool, ReadError> {
        (**self).guard(path)
    }
    fn now(&self) -> String {
        (**self).now()
    }
}
impl Fixture {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let paths = Paths::at(temp.path(), &temp.path().join("jobs"));
        let db = json!({"projects":{"fixture":"D:/fixture-source"},"items":[
            {"id":1,"project":"fixture","status":"wait","jobs":["candidate"],
             "work_owner_job":"owner","work_owner_wait_on":2,"wait_on":[2],"spec":"saved"},
            {"id":2,"project":"fixture","status":"todo","jobs":["owner"]}
        ]});
        save(&paths.store, &db).unwrap();
        let mut files = BTreeMap::new();
        for (id, todo) in [("candidate", 1), ("owner", 2)] {
            let job = json!({"id":id,"todo_id":todo,"project":"fixture","status":"succeeded",
                "created_at":"2026-10-26T00:00:00Z","cwd":"D:/fixture-tree",
                "registered_cwd":"D:/fixture-source","work_owner":{"worktree":"d:/fixture-tree"}});
            let path = paths.jobs.join(id).join("job.json");
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            files.insert(path, serde_json::to_vec(&job).unwrap());
        }
        Self {
            _temp: temp,
            paths,
            host: FakeHost {
                files,
                reads: vec![],
                guards: vec![],
                verdicts: VecDeque::new(),
                hook: None,
                ended: None,
                started: "2026-10-27T00:00:00Z".into(),
                now_calls: std::cell::Cell::new(0),
            },
            writes: std::cell::Cell::new(0),
        }
    }
    fn run(&mut self) -> Result<Vec<i64>> {
        self.run_snapshot(|| Ok(Processes::new()))
    }
    fn run_snapshot(&mut self, snapshot: impl FnMut() -> Result<Processes>) -> Result<Vec<i64>> {
        let writes = &self.writes;
        let path = &self.paths.store;
        run(
            &self.paths,
            1,
            || &mut self.host,
            snapshot,
            |db| {
                writes.set(writes.get() + 1);
                save(path, db)
            },
        )
    }
    fn unchanged(&self, before: &[u8]) {
        assert_eq!(self.bytes(), before);
        assert_eq!(self.writes.get(), 0);
    }
    fn edit(&self, edit: impl FnOnce(&mut Value)) {
        let mut db = load(&self.paths.store).unwrap();
        edit(&mut db);
        save(&self.paths.store, &db).unwrap();
    }
    fn job(&mut self, id: &str, edit: impl FnOnce(&mut Value)) {
        let path = self.paths.jobs.join(id).join("job.json");
        let mut job: Value = serde_json::from_slice(&self.host.files[&path]).unwrap();
        edit(&mut job);
        self.host
            .files
            .insert(path, serde_json::to_vec(&job).unwrap());
    }
    fn bytes(&self) -> Vec<u8> {
        fs::read(&self.paths.store).unwrap()
    }
    fn completely_released(&self) -> Value {
        let db = load(&self.paths.store).unwrap();
        let item = &db["items"][0];
        assert_eq!(item["status"], "todo");
        assert!(item["wait_on"].is_null());
        assert!(item["work_owner_job"].is_null());
        assert!(item["work_owner_wait_on"].is_null());
        assert_eq!(item["dependency_ready"], true);
        assert_eq!(self.writes.get(), 1);
        db
    }
}

#[test]
fn positive_uses_current_guard_without_reading_legacy_list() {
    let mut f = Fixture::new();
    let before = load(&f.paths.store).unwrap();
    let jobs_before = f.host.files.clone();
    assert_eq!(f.run(), Ok(vec![1]));
    let db = load(&f.paths.store).unwrap();
    assert_eq!(db["items"][0]["status"], "todo");
    assert_eq!(db["items"][0]["spec"], "saved");
    assert!(db["items"][0]["wait_on"].is_null());
    assert!(f.host.guards.contains(&PathBuf::from("D:/fixture-source")));
    assert!(f.host.reads.iter().all(|p| p.starts_with(&f.paths.jobs)));
    assert_eq!(db["items"][1], before["items"][1]);
    assert_eq!(f.host.files, jobs_before);
    assert_eq!(
        db["items"][0]["work_owner_wait_history"][0]["reason"],
        "owner_claim_reevaluated"
    );
    assert_eq!(f.run(), Ok(vec![]));
    assert_eq!(f.writes.get(), 1);
}

#[test]
fn explicit_holds_on_candidate_or_owner_never_release_or_save() {
    let mut failures = vec![];
    for row in [0, 1] {
        for scheduler in [false, true] {
            let fields: Vec<_> = crate::direction_record::HOLD_FIELDS
                .iter()
                .copied()
                .chain(
                    scheduler
                        .then_some([
                            "owner_stop_job",
                            "pending_cancel",
                            "question_close_pending",
                            "question",
                        ])
                        .into_iter()
                        .flatten(),
                )
                .collect();
            for field in fields {
                // Even zero and empty containers are malformed hold values, not
                // proof that an explicit hold was cleared.
                for value in [
                    json!(true),
                    json!("hold"),
                    json!(1),
                    json!(0),
                    json!([]),
                    json!({}),
                ] {
                    let mut f = Fixture::new();
                    f.edit(|db| {
                        let item = &mut db["items"][row];
                        if scheduler {
                            item["scheduler"][field] = value.clone();
                        } else {
                            item[field] = value.clone();
                        }
                    });
                    let before = f.bytes();
                    let released = f.run();
                    if released != Ok(vec![]) || f.writes.get() != 0 || f.bytes() != before {
                        failures.push(format!("row={row} scheduler={scheduler} field={field} value={value}: released={released:?} commits={} bytes_unchanged={}", f.writes.get(), f.bytes() == before));
                    }
                }
            }
        }
    }
    assert!(
        failures.is_empty(),
        "explicit holds were released/saved:\n{}",
        failures.join("\n")
    );
}

#[test]
fn inactive_hold_markers_except_wait_ready_keep_legitimate_release() {
    for value in [Value::Null, json!(false), json!("")] {
        let mut f = Fixture::new();
        f.edit(|db| {
            for row in [0, 1] {
                for key in crate::direction_record::HOLD_FIELDS {
                    // todo_wait::release_item requires null/absent wait_ready.
                    if *key == "wait_ready" {
                        continue;
                    }
                    db["items"][row][*key] = value.clone();
                    db["items"][row]["scheduler"][*key] = value.clone();
                }
                for key in [
                    "owner_stop_job",
                    "pending_cancel",
                    "question_close_pending",
                    "question",
                ] {
                    db["items"][row]["scheduler"][key] = value.clone();
                }
            }
        });
        assert_eq!(f.run(), Ok(vec![1]), "inactive marker {value}");
        f.completely_released();
    }
    let mut f = Fixture::new();
    f.edit(|db| db["items"][1]["status"] = json!("done"));
    assert_eq!(f.run(), Ok(vec![1]));
}

#[test]
fn answered_question_history_allows_only_the_existing_automatic_release() {
    for rows in [vec![0], vec![1], vec![0, 1]] {
        for answer in [json!("recorded reply"), json!({"message_id":"saved-reply"})] {
            let mut f = Fixture::new();
            f.edit(|db| {
                for row in &rows {
                    db["items"][*row]["scheduler"] = json!({
                        "question":{"thread_id":"saved-question"},"answer":answer});
                }
            });
            let before = load(&f.paths.store).unwrap();
            assert_eq!(f.run(), Ok(vec![1]), "rows={rows:?} answer={answer}");
            let after = f.completely_released();
            for row in &rows {
                for field in ["question", "answer"] {
                    assert_eq!(
                        after["items"][*row]["scheduler"][field],
                        before["items"][*row]["scheduler"][field]
                    );
                }
            }
            assert_eq!(after["items"][1], before["items"][1]);
        }
    }
}

#[test]
fn question_prerequisites_and_recorded_stops_survive_answers_on_either_row() {
    for row in [0, 1] {
        let patches = [
            json!({"scheduler":{"question":{"thread_id":"saved-question"}}}),
            json!({"scheduler":{"question":"saved-question","answer":false}}),
            json!({"scheduler":{"question":"saved-question","answer":""}}),
            json!({"scheduler":{"question_close_pending":true}}),
            json!({"status":"block"}),
            json!({"owner_stopped":true}),
            json!({"cancel_requested":true}),
            json!({"scheduler":{"owner_stop_job":"recorded-job"}}),
            json!({"scheduler":{"pending_cancel":"manual"}}),
            json!({"note":"停止したまま"}),
            json!({"note":"再開しない"}),
            json!({"note":"止めておく"}),
        ];
        for patch in patches {
            let mut f = Fixture::new();
            f.edit(|db| {
                db["items"][row]["scheduler"] = json!({
                    "question":{"thread_id":"saved-question"},"answer":"recorded reply"});
                // The unanswered cases explicitly replace/remove the answer.
                if patch["scheduler"]["question"].is_object() {
                    db["items"][row]["scheduler"]
                        .as_object_mut()
                        .unwrap()
                        .remove("answer");
                }
                common::merge(&mut db["items"][row], patch.clone());
            });
            let before = f.bytes();
            assert_eq!(f.run(), Ok(vec![]), "row={row} patch={patch}");
            f.unchanged(&before);
        }
    }
}

#[test]
fn non_null_wait_ready_never_saves_a_partial_automatic_release() {
    let mut failures = vec![];
    for row in [0, 1] {
        for scheduler in [false, true] {
            for value in [json!(false), json!("")] {
                let mut f = Fixture::new();
                f.edit(|db| {
                    if scheduler {
                        db["items"][row]["scheduler"]["wait_ready"] = value.clone();
                    } else {
                        db["items"][row]["wait_ready"] = value.clone();
                    }
                });
                let before = f.bytes();
                let result = f.run();
                if result != Ok(vec![]) || f.writes.get() != 0 || f.bytes() != before {
                    let db = load(&f.paths.store).unwrap();
                    failures.push(format!("row={row} scheduler={scheduler} value={value}: result={result:?} saves={} candidate_status={} wait_on={} work_owner_job={} work_owner_wait_on={} bytes_unchanged={}",
                        f.writes.get(), db["items"][0]["status"], db["items"][0]["wait_on"],
                        db["items"][0]["work_owner_job"], db["items"][0]["work_owner_wait_on"], f.bytes() == before));
                }
            }
        }
    }
    assert!(
        failures.is_empty(),
        "non-null readiness was released/saved:\n{}",
        failures.join("\n")
    );
}

#[test]
fn null_or_absent_wait_ready_allows_complete_release() {
    for present in [false, true] {
        let mut f = Fixture::new();
        if present {
            f.edit(|db| {
                for row in [0, 1] {
                    db["items"][row]["wait_ready"] = Value::Null;
                    db["items"][row]["scheduler"]["wait_ready"] = Value::Null;
                }
            });
        }
        assert_eq!(f.run(), Ok(vec![1]));
        f.completely_released();
    }
}

#[test]
fn conversation_wait_on_candidate_or_owner_is_preserved() {
    for row in [0, 1] {
        for scheduler in [false, true] {
            for value in [
                json!(true),
                json!({"thread_id":"saved-conversation"}),
                json!("waiting"),
                json!(0),
                json!({}),
            ] {
                let mut f = Fixture::new();
                f.edit(|db| {
                    if scheduler {
                        db["items"][row]["scheduler"]["conversation_wait"] = value.clone();
                    } else {
                        db["items"][row]["conversation_wait"] = value.clone();
                    }
                });
                let before = f.bytes();
                assert_eq!(
                    f.run(),
                    Ok(vec![]),
                    "row={row} scheduler={scheduler} value={value}"
                );
                f.unchanged(&before);
            }
        }
    }
}

#[test]
fn dependency_reopened_during_second_guard_never_commits() {
    let mut failures = vec![];
    for row in [0, 1] {
        let mut f = Fixture::new();
        f.edit(|db| {
            db["items"]
                .as_array_mut()
                .unwrap()
                .push(json!({"id":3,"status":"done"}));
            if row == 0 {
                db["items"][0]["wait_on"] = json!([2, 3]);
            } else {
                db["items"][1]["status"] = json!("wait");
                db["items"][1]["wait_on"] = json!([3]);
            }
        });
        let paths = f.paths.clone();
        let expected = std::rc::Rc::new(std::cell::RefCell::new(Vec::new()));
        let captured = expected.clone();
        let mut calls = 0;
        f.host.hook = Some(Box::new(move |_, _| {
            calls += 1;
            if calls != 3 {
                // root and job cwd were checked in the first pass.
                return;
            }
            let _lock = Lock::acquire(&paths.lock, Duration::ZERO).unwrap();
            let mut db = load(&paths.store).unwrap();
            db["items"][2]["status"] = json!("todo");
            save(&paths.store, &db).unwrap();
            *captured.borrow_mut() = fs::read(&paths.store).unwrap();
        }));
        let result = f.run();
        assert!(
            !expected.borrow().is_empty(),
            "the concurrent writer must run"
        );
        if result != Ok(vec![]) || f.writes.get() != 0 || f.bytes() != *expected.borrow() {
            failures.push(format!(
                "dependency of row={row}: result={result:?} saves={} only_concurrent_change={}",
                f.writes.get(),
                f.bytes() == *expected.borrow()
            ));
        } else {
            f.unchanged(&expected.borrow());
        }
    }
    assert!(
        failures.is_empty(),
        "reopened dependency was released/saved:\n{}",
        failures.join("\n")
    );
}

#[test]
fn owner_block_delay_dependency_and_malformed_hold_containers_never_save() {
    for row in [0, 1] {
        for patch in [
            json!({"status":"block"}),
            json!({"status":"recovery_unverified"}),
            json!({"scheduler":false}),
            json!({"stall_recovery":[]}),
            json!({"scheduler":{"not_before":"unknown"}}),
            json!({"scheduler":{"not_before":9_999_999_999u64}}),
            json!({"stall_recovery":{"next_at":9_999_999_999u64}}),
            json!({"discord_restart_not_before":9_999_999_999u64}),
            json!({"status":"wait","wait_on":[3]}),
        ] {
            let mut f = Fixture::new();
            f.edit(|db| {
                common::merge(&mut db["items"][row], patch.clone());
                db["items"]
                    .as_array_mut()
                    .unwrap()
                    .push(json!({"id":3,"status":"todo"}));
            });
            let before = f.bytes();
            assert_eq!(f.run(), Ok(vec![]), "row={row} patch={patch}");
            f.unchanged(&before);
        }
    }
}

#[test]
fn guard_paths_are_unique_per_pass_and_every_pass_is_fresh() {
    let mut f = Fixture::new();
    f.edit(|db| {
        db["items"][0]["scheduler"]["worktree"] = json!("d:\\FIXTURE-tree\\");
        db["items"][1]["scheduler"]["worktree"] = json!("D:/fixture-source/");
    });
    assert_eq!(f.run(), Ok(vec![1]));
    let paths: Vec<_> = f
        .host
        .guards
        .iter()
        .map(|p| normalized(p.to_str().unwrap()))
        .collect();
    assert_eq!(
        paths,
        [
            "D:/fixture-source",
            "D:/fixture-tree",
            "D:/fixture-source",
            "D:/fixture-tree"
        ]
        .map(normalized)
    );
    assert_eq!(
        f.host.now_calls.get(),
        2,
        "the single scope deadline must not be renewed"
    );
}

#[test]
fn malformed_scheduler_worktree_on_either_source_never_saves() {
    for row in [0, 1] {
        for value in [
            json!(42),
            json!(false),
            json!([]),
            json!({}),
            json!("relative"),
            json!(" "),
        ] {
            let mut f = Fixture::new();
            f.edit(|db| db["items"][row]["scheduler"]["worktree"] = value.clone());
            let before = f.bytes();
            assert!(f.run().is_err(), "row={row} worktree={value}");
            f.unchanged(&before);
        }
    }
}

#[test]
fn host_budget_begins_after_initial_scope_snapshot_and_lock_release() {
    let mut f = Fixture::new();
    let paths = f.paths.clone();
    let writes = &f.writes;
    let result = run(
        &f.paths,
        1,
        || {
            // The host factory must run after the snapshot and after its lock was
            // released. This writer then makes that snapshot stale, proving order
            // without sleeping for the production 30/45-second deadlines.
            let _lock = Lock::acquire(&paths.lock, Duration::ZERO).unwrap();
            let mut db = load(&paths.store).unwrap();
            db["items"][0]["note"] = json!("writer at scope start");
            save(&paths.store, &db).unwrap();
            &mut f.host
        },
        || Ok(Processes::new()),
        |_| {
            writes.set(writes.get() + 1);
            Ok(())
        },
    );
    assert_eq!(result, Err("reevaluation source changed".into()));
    assert_eq!(f.writes.get(), 0);
    let db = load(&paths.store).unwrap();
    assert_eq!(db["items"][0]["note"], "writer at scope start");
    assert_eq!(db["items"][0]["status"], "wait");
}

#[test]
fn guards_never_hold_shared_todo_or_referenced_job_locks() {
    let mut f = Fixture::new();
    let paths = f.paths.clone();
    let count = std::rc::Rc::new(std::cell::Cell::new(0));
    let seen = count.clone();
    f.host.hook = Some(Box::new(move |_, _| {
        let _todo = Lock::acquire(&paths.lock, Duration::ZERO).unwrap();
        let _candidate =
            common::ByteLock::acquire(&paths.jobs.join("candidate/state.lock"), Duration::ZERO)
                .unwrap()
                .unwrap();
        let _owner =
            common::ByteLock::acquire(&paths.jobs.join("owner/state.lock"), Duration::ZERO)
                .unwrap()
                .unwrap();
        seen.set(seen.get() + 1);
    }));
    assert_eq!(f.run(), Ok(vec![1]));
    assert_eq!(count.get(), f.host.guards.len());
    assert!(count.get() > 0);
}

#[test]
fn cross_project_owner_is_rejected_without_reinterpreting_its_scope() {
    let mut f = Fixture::new();
    f.edit(|db| {
        db["projects"]["other"] = json!("D:/other-root");
        db["items"][1]["project"] = json!("other");
    });
    f.job("owner", |job| job["project"] = json!("other"));
    let before = f.bytes();
    assert_eq!(f.run(), Err("owner source mismatch".into()));
    f.unchanged(&before);
}

#[test]
fn actual_owner_question_stops_and_terminal_items_never_save() {
    for patch in [
        json!({"status":"block"}),
        json!({"status":"done"}),
        json!({"stopped":true}),
        json!({"cancel_requested":true}),
        json!({"scheduler":{"question":{"thread":"existing"}}}),
        json!({"scheduler":{"answer":"received"},"note":"停止したまま"}),
        json!({"wait_ready":{"condition":"retry-time"}}),
        json!({"scheduler":{"not_before":9_999_999_999u64}}),
    ] {
        let mut f = Fixture::new();
        f.edit(|db| common::merge(&mut db["items"][0], patch));
        let before = f.bytes();
        assert_eq!(f.run(), Ok(vec![]));
        f.unchanged(&before);
    }
    for patch in [
        json!({"stopped":true}),
        json!({"cancel_requested":true}),
        json!({"cancel_reason":"manual"}),
        json!({"status":"cancelled"}),
        json!({"last_message":"状態: 要判断\nどちらの方針にしますか？\na. 保持する（推奨）\nb. 中止する"}),
    ] {
        let mut f = Fixture::new();
        f.job("candidate", |job| common::merge(job, patch));
        let before = f.bytes();
        assert_eq!(f.run(), Ok(vec![]));
        f.unchanged(&before);
    }
}

#[test]
fn manual_dependencies_and_unproven_automatic_edges_stay_untouched() {
    for patch in [
        json!({"wait_on":[2,3]}),
        json!({"work_owner_job":null,"work_owner_wait_on":null}),
        json!({"wait_on":[3]}),
    ] {
        let mut f = Fixture::new();
        f.edit(|db| {
            common::merge(&mut db["items"][0], patch);
            db["items"]
                .as_array_mut()
                .unwrap()
                .push(json!({"id":3,"status":"todo"}));
        });
        let before = f.bytes();
        assert_eq!(f.run(), Ok(vec![]));
        f.unchanged(&before);
    }
}

#[test]
fn queued_running_surviving_and_unknown_processes_never_save() {
    for status in ["queued", "starting", "running"] {
        let mut f = Fixture::new();
        f.job("owner", |job| job["status"] = json!(status));
        let before = f.bytes();
        assert_eq!(f.run(), Ok(vec![]));
        f.unchanged(&before);
    }
    for created in [json!(9), Value::Null] {
        let mut f = Fixture::new();
        f.job("owner", |job| {
            job["codex_pid"] = json!(7);
            job["codex_pid_created"] = json!(9)
        });
        let before = f.bytes();
        let ps = Processes::from([(7, json!({"pid":7,"created":created}))]);
        assert_eq!(f.run_snapshot(|| Ok(ps.clone())), Ok(vec![]));
        f.unchanged(&before);
    }
}

#[test]
fn durable_pr_or_branch_owner_claim_is_not_released() {
    for identity in [
        json!({"repository":"fixture/repo","pr":42}),
        json!({"git_common":"D:/fixture/.git","branch":"existing"}),
    ] {
        let mut f = Fixture::new();
        for id in ["candidate", "owner"] {
            f.job(id, |job| {
                common::merge(&mut job["work_owner"], identity.clone())
            });
        }
        let before = f.bytes();
        assert_eq!(f.run(), Ok(vec![]));
        f.unchanged(&before);
    }
}

#[test]
fn guard_denial_unknown_malformed_missing_cli_error_and_timeout_never_save() {
    // These are the Host boundary outcomes, not manufactured production receipts.
    // SystemHost's wire-format tests exercise malformed/typed JSON separately.
    let mut baseline = Fixture::new();
    // Include a distinct scheduler worktree on both rows so every protected
    // source position, including pass two, is enumerated from a passing run.
    baseline.edit(|db| {
        db["items"][0]["scheduler"]["worktree"] = json!("D:/candidate-scheduler-tree");
        db["items"][1]["scheduler"]["worktree"] = json!("D:/owner-scheduler-tree");
    });
    assert_eq!(baseline.run(), Ok(vec![1]));
    let calls = baseline.host.guards.clone();
    assert!(!calls.is_empty());
    for at in 0..calls.len() {
        for verdict in [
            Ok(false),
            Err(ReadError("classification_unverified")),
            Err(ReadError("guard_unavailable")),
            Err(ReadError("query_deadline")),
        ] {
            let mut f = Fixture::new();
            f.edit(|db| {
                db["items"][0]["scheduler"]["worktree"] = json!("D:/candidate-scheduler-tree");
                db["items"][1]["scheduler"]["worktree"] = json!("D:/owner-scheduler-tree");
            });
            f.host.verdicts.extend((0..at).map(|_| Ok(true)));
            f.host.verdicts.push_back(verdict);
            let before = f.bytes();
            assert!(f.run().is_err(), "guard position {at}");
            assert_eq!(f.host.guards, calls[..=at]);
            f.unchanged(&before);
            assert!(f.host.reads.iter().all(|p| p.starts_with(&f.paths.jobs)));
        }
    }
}

#[test]
fn missing_or_wrong_project_owner_and_reference_sources_never_save() {
    for patch in [
        json!({"project":"owner"}),
        json!({"project":"unknown"}),
        json!({"project":null}),
        json!({"jobs":["../other"]}),
        json!({"jobs":[null]}),
        json!({"jobs":null}),
        json!({"scheduler":{"job":7}}),
        json!({"work_owner_job":7}),
        json!({"work_owner_wait_on":3}),
    ] {
        let mut f = Fixture::new();
        f.edit(|db| common::merge(&mut db["items"][0], patch));
        let before = f.bytes();
        assert!(f.run().is_err());
        f.unchanged(&before);
    }
    for root in [Value::Null, json!(""), json!("relative"), json!(42)] {
        let mut f = Fixture::new();
        f.edit(|db| db["projects"]["fixture"] = root);
        let before = f.bytes();
        assert!(f.run().is_err());
        f.unchanged(&before);
    }
    let mut f = Fixture::new();
    f.edit(|db| {
        let duplicate = db["items"][0].clone();
        db["items"].as_array_mut().unwrap().push(duplicate);
    });
    let before = f.bytes();
    assert!(f.run().is_err());
    f.unchanged(&before);
}

#[test]
fn wrong_job_bindings_or_omitted_receipt_sources_never_save() {
    for patch in [
        json!({"id":"other"}),
        json!({"todo_id":8}),
        json!({"project":"other"}),
        json!({"cwd":null}),
        json!({"cwd":"relative"}),
        json!({"status":null}),
        json!({"registered_cwd":"D:/other"}),
        json!({"work_owner":null}),
        json!({"work_owner":{}}),
        json!({"pid":7}),
        json!({"codex_pid":"7"}),
        json!({"codex_pid":4_294_967_296u64,"codex_pid_created":9}),
    ] {
        let mut f = Fixture::new();
        f.job("candidate", |job| common::merge(job, patch));
        let before = f.bytes();
        assert!(f.run().is_err());
        f.unchanged(&before);
    }
    for bytes in [None, Some(b"{".to_vec()), Some(vec![b' '; JOB_LIMIT + 1])] {
        let mut f = Fixture::new();
        let path = f.paths.jobs.join("candidate/job.json");
        if let Some(bytes) = bytes {
            f.host.files.insert(path, bytes);
        } else {
            f.host.files.remove(&path);
        }
        let before = f.bytes();
        assert!(f.run().is_err());
        f.unchanged(&before);
    }
}

#[test]
fn guards_run_without_todo_lock_and_changed_selected_source_is_rejected() {
    for change in [
        "item",
        "root",
        "owner",
        "references",
        "source_omitted",
        "scheduler_worktree",
        "owner_worktree",
    ] {
        let mut f = Fixture::new();
        let paths = f.paths.clone();
        let unlocked = std::rc::Rc::new(std::cell::Cell::new(false));
        let seen = unlocked.clone();
        f.host.hook = Some(Box::new(move |_, _| {
            if seen.replace(true) {
                return;
            }
            let _lock = Lock::acquire(&paths.lock, Duration::ZERO).unwrap();
            let mut db = load(&paths.store).unwrap();
            match change {
                "item" => db["items"][0]["note"] = json!("concurrent writer"),
                "root" => db["projects"]["fixture"] = json!("D:/new-source"),
                "owner" => db["items"][1]["stopped"] = json!(true),
                "references" => db["items"][0]["jobs"] = json!(["other"]),
                "scheduler_worktree" => {
                    db["items"][0]["scheduler"]["worktree"] = json!("D:/new-tree")
                }
                "owner_worktree" => {
                    db["items"][1]["scheduler"]["worktree"] = json!("D:/new-owner-tree")
                }
                _ => {
                    db["items"][0].as_object_mut().unwrap().remove("project");
                }
            }
            save(&paths.store, &db).unwrap();
        }));
        assert!(f.run().is_err(), "{change}");
        assert!(unlocked.get());
        let db = load(&f.paths.store).unwrap();
        assert_eq!(db["items"][0]["status"], "wait");
        assert_eq!(db["items"][0]["wait_on"], json!([2]));
        assert!(db["items"][0]["work_owner_wait_history"].is_null());
        assert_eq!(f.writes.get(), 0);
    }
}

#[test]
fn changed_exact_job_and_busy_normal_job_lock_are_rejected() {
    let mut f = Fixture::new();
    let candidate = f.paths.jobs.join("candidate/job.json");
    f.host.hook = Some(Box::new(move |path, files| {
        if path == Path::new("D:/fixture-tree") {
            let mut job: Value = serde_json::from_slice(&files[&candidate]).unwrap();
            job["cwd"] = json!("D:/changed-tree");
            files.insert(candidate.clone(), serde_json::to_vec(&job).unwrap());
        }
    }));
    let before = f.bytes();
    assert!(f.run().is_err());
    f.unchanged(&before);

    let mut f = Fixture::new();
    let _busy = common::ByteLock::acquire(&f.paths.jobs.join("owner/state.lock"), Duration::ZERO)
        .unwrap()
        .unwrap();
    let before = f.bytes();
    assert_eq!(f.run(), Err("receipt busy".into()));
    f.unchanged(&before);
}

#[test]
fn liveness_change_snapshot_error_and_expired_guard_cannot_commit() {
    let mut f = Fixture::new();
    f.job("owner", |job| {
        job["codex_pid"] = json!(7);
        job["codex_pid_created"] = json!(9)
    });
    let before = f.bytes();
    let mut calls = 0;
    assert_eq!(
        f.run_snapshot(|| {
            calls += 1;
            Ok(if calls == 1 {
                Processes::new()
            } else {
                Processes::from([(7, json!({"pid":7,"created":9}))])
            })
        }),
        Err("reevaluation liveness changed".into())
    );
    f.unchanged(&before);
    let mut f = Fixture::new();
    let before = f.bytes();
    assert!(
        f.run_snapshot(|| Err("process identity unavailable".into()))
            .is_err()
    );
    f.unchanged(&before);
    for ended in ["2026-10-27T00:00:45Z", "2026-10-26T23:59:59Z", "invalid"] {
        let mut f = Fixture::new();
        f.host.ended = Some(ended.into());
        let before = f.bytes();
        assert!(f.run().is_err());
        f.unchanged(&before);
    }
}

#[test]
fn unknown_start_is_rejected_and_sub_45_second_scope_is_not_renewed() {
    let mut f = Fixture::new();
    f.host.started = "unknown".into();
    let before = f.bytes();
    assert_eq!(f.run(), Err("guard time unavailable".into()));
    f.unchanged(&before);

    let mut f = Fixture::new();
    f.host.ended = Some("2026-10-27T00:00:44.999999Z".into());
    assert_eq!(f.run(), Ok(vec![1]));
    assert_eq!(f.host.now_calls.get(), 2);
    assert_eq!(f.writes.get(), 1);
}

#[test]
fn unrelated_item_changes_are_preserved_and_other_items_never_reevaluated() {
    let mut f = Fixture::new();
    f.edit(|db| {
        let mut other = db["items"][0].clone();
        other["id"] = json!(3);
        db["items"].as_array_mut().unwrap().push(other);
    });
    let paths = f.paths.clone();
    f.host.hook = Some(Box::new(move |_, _| {
        let _lock = Lock::acquire(&paths.lock, Duration::ZERO).unwrap();
        let mut db = load(&paths.store).unwrap();
        db["items"][2]["note"] = json!("independent edit");
        save(&paths.store, &db).unwrap();
    }));
    assert_eq!(f.run(), Ok(vec![1]));
    let db = load(&f.paths.store).unwrap();
    assert_eq!(db["items"][2]["note"], "independent edit");
    assert_eq!(db["items"][2]["status"], "wait");
    assert_eq!(db["items"][2]["wait_on"], json!([2]));
}

#[test]
fn normal_registered_root_fallback_and_ancestor_git_scope_are_preserved() {
    let mut f = Fixture::new();
    for id in ["candidate", "owner"] {
        f.job(id, |job| {
            job.as_object_mut().unwrap().remove("registered_cwd");
            job["work_owner"]["worktree"] = json!("D:/fixture-ancestor");
        });
    }
    assert_eq!(f.run(), Ok(vec![1]));
    assert_eq!(f.writes.get(), 1);
    assert!(
        f.host
            .guards
            .iter()
            .all(|p| p == Path::new("D:/fixture-source") || p == Path::new("D:/fixture-tree"))
    );
}
