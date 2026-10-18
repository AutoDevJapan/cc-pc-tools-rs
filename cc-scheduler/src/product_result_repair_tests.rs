use super::*;
use std::path::Path;

const AT: u64 = 1792281600;
struct Fake {
    records: Vec<Value>,
    ps: Processes,
    allowed: bool,
    clock: f64,
    on_outcome: Option<Box<dyn FnOnce()>>,
    births: std::collections::BTreeMap<u32, (u64, u64)>,
    births_after_first: Option<std::collections::BTreeMap<u32, (u64, u64)>>,
    birth_calls: usize,
}
impl Backend for Fake {
    fn repair_scope_allowed(&mut self, path: &str) -> bool {
        self.allowed && path == scope::TREE
    }
    fn clock(&self) -> f64 {
        self.clock
    }
    fn measure(&mut self, _: &mut Value) -> Result<(Value, Processes)> {
        panic!("unexpected measurement")
    }
    fn snapshot(&mut self) -> Result<Processes> {
        Ok(self.ps.clone())
    }
    fn birth_bounds(
        &mut self,
        pids: &[u32],
    ) -> Result<std::collections::BTreeMap<u32, (u64, u64)>> {
        self.birth_calls += 1;
        assert!(pids.iter().all(|pid| *pid > 0));
        Ok(if self.birth_calls > 1 {
            self.births_after_first
                .clone()
                .unwrap_or_else(|| self.births.clone())
        } else {
            self.births.clone()
        })
    }
    fn jobs(&mut self) -> Result<Vec<Value>> {
        Ok(self.records.clone())
    }
    fn event_mtime(&mut self, _: &Value) -> Result<f64> {
        panic!("unexpected event read")
    }
    fn mark_lost(&mut self, _: &mut Value) -> Result<bool> {
        panic!("unexpected lost marking")
    }
    fn launch(&mut self, _: &Value, _: &Path, _: &Value) -> Result<String> {
        panic!("unexpected launch")
    }
    fn cancel(&mut self, _: &mut Value) -> Result<bool> {
        panic!("unexpected cancellation")
    }
    fn kill_orphan(&mut self, _: &Value) -> Result<bool> {
        panic!("unexpected termination")
    }
    fn pick(&mut self, _: &str) -> Result<Value> {
        panic!("unexpected model selection")
    }
    fn model_exists(&mut self, _: &str) -> bool {
        panic!("unexpected model query")
    }
    fn notify(&mut self, _: &Value) -> Result<Value> {
        panic!("unexpected notification")
    }
    fn reply(&mut self, _: &Value) -> Result<Option<Value>> {
        panic!("unexpected reply")
    }
    fn close_question(&mut self, _: &Value) -> Result<()> {
        panic!("unexpected close")
    }
    fn outcome(&mut self, job: &Value) -> Result<String> {
        if let Some(hook) = self.on_outcome.take() {
            hook();
        }
        Ok(crate::backend::job_result_outcome(job, s(&job["last_message"])).into())
    }
    fn worktree(&mut self, _: &str, _: &Value) -> Result<()> {
        panic!("unexpected worktree")
    }
}

struct Fixture {
    _temp: tempfile::TempDir,
    engine: Scheduler<Fake>,
    job_file: PathBuf,
}
fn stamp(at: u64) -> String {
    chrono::DateTime::from_timestamp(at as i64, 0)
        .unwrap()
        .to_rfc3339()
}
impl Fixture {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path().join("scheduler");
        let todo = temp.path().join("todo.json");
        let mut db = json!({"projects":{"fixture":"C:/fixture/product"},"items":[{
            "id":scope::ID,"project":"fixture","status":"wait","spec":"saved-spec.md","needs":[],
            "jobs":["source"],"note":"元の作業と途中成果","dispatchable":false,
            "wait_ready":{"condition":"retry-time","since":AT-120,"not_before":AT-60},
            "resume_generation":7,"worker_receipt":{"job":"source","generation":7},
            "scheduler":{"job":"source","worktree":scope::TREE,"branch":"saved-branch"}
        }],"unrelated":{"original":true}});
        let proof = json!({"verified":true,"session_receipt_verified":true,"id":scope::ID,
            "session":scope::QUALIFICATION_SESSION,"job":scope::QUALIFICATION_JOB,
            "request_sha256":scope::REQUEST_HASH,"worktree":scope::TREE,"expires_at":stamp(scope::LIMIT),
            "snapshot":cc_todo::product_resume::snapshot(&db,&db["items"][0])});
        db["items"][0]["product_resume_qualification"] = json!({"receipt":proof});
        let source = json!({"id":"source","todo_id":scope::ID,"status":"succeeded","retry_live":false,
            "thread_id":scope::QUALIFICATION_SESSION,"cwd":scope::TREE,"model":"fixture-model","effort":"high",
            "sandbox":"workspace-write","mcp_profile":"fixture-profile","scope_source":"owner_request","network":false,"add_dirs":[]});
        let key = scope::key(&db["items"][0]["wait_ready"]).unwrap();
        cc_todo::product_retry::stage(
            &mut db,
            &proof,
            &source,
            &key,
            "C:/fixture/retry.md",
            &"a".repeat(64),
            AT,
        )
        .unwrap();
        let intent = &db["items"][0]["product_retry_intent"];
        let mut job = json!({"id":"actual","todo_id":scope::ID,"status":"queued","launch_key":key,
            "original_product_retry":scope::marker(intent),"resume_session":scope::QUALIFICATION_SESSION,
            "cwd":scope::TREE,"prompt_source":"C:/fixture/retry.md","requested_model":"fixture-model",
            "model":"fixture-model","effort":"high","sandbox":"workspace-write","mcp_profile":"fixture-profile",
            "scope_source":"owner_request","network":false,"add_dirs":[]});
        cc_todo::product_retry::bind(&mut db, &job, &key, AT).unwrap();
        job["status"] = json!("succeeded");
        job["started_at"] = json!(stamp(AT - 30));
        job["first_started_at"] = job["started_at"].clone();
        job["finished_at"] = json!(stamp(AT - 1));
        job["pid"] = json!(42);
        job["pid_created"] = json!(100);
        job["codex_pid"] = json!(43);
        job["codex_pid_created"] = json!(101);
        let job_file = temp.path().join("codex-jobs/jobs/actual/job.json");
        fs::create_dir_all(job_file.parent().unwrap()).unwrap();
        write_json(&job_file, &job, 1).unwrap();
        write_json(&todo, &db, 1).unwrap();
        let engine = Scheduler::new(
            root,
            todo,
            Fake {
                records: vec![job],
                ps: Processes::new(),
                allowed: true,
                clock: AT as f64,
                on_outcome: None,
                births: Default::default(),
                births_after_first: None,
                birth_calls: 0,
            },
        )
        .unwrap();
        Self {
            _temp: temp,
            engine,
            job_file,
        }
    }
    fn db(&self) -> Value {
        self.engine.todo().unwrap()
    }
    fn save(&self, db: &Value) {
        write_json(&self.engine.todo_path, db, 1).unwrap();
    }
    fn reconcile(&mut self) {
        let mut jobs = self.engine.backend.records.clone();
        let ps = self.engine.backend.ps.clone();
        // The production sample decorates every job with this observation.
        cc_job_policy::subwork::mark_live(&mut jobs, |job| {
            ["pid", "codex_pid"]
                .iter()
                .any(|key| matches(job, key, &ps))
        });
        self.engine
            .reconcile_repairs(&mut jobs, &ps, &json!({}))
            .unwrap();
    }
}

#[test]
fn an_inaccessible_reused_historical_pid_does_not_strand_the_finished_product() {
    let mut f = Fixture::new();
    let mut db = f.db();
    let mut historical = f.engine.backend.records[0].clone();
    historical["id"] = json!("historical");
    historical["pid"] = json!(55);
    historical["pid_created"] = json!(1_000_003u64);
    historical["codex_pid"] = Value::Null;
    f.engine.backend.records.push(historical);
    db["items"][0]["jobs"]
        .as_array_mut()
        .unwrap()
        .push(json!("historical"));
    f.save(&db);
    f.engine
        .backend
        .ps
        .insert(55, json!({"pid":55,"created":null}));
    assert!(qualified(&db, &f.engine.backend.records[0], AT));
    f.reconcile();
    assert_eq!(
        f.db(),
        db,
        "unavailable metadata must keep the original hold"
    );
    assert_eq!(f.engine.backend.birth_calls, 1);

    f.engine.backend.birth_calls = 0;
    f.engine.backend.births.insert(55, (999_990, 1_000_010));
    f.reconcile();
    assert_eq!(
        f.db(),
        db,
        "overlapping precision bounds cannot rule out a live owner"
    );

    f.engine.backend.birth_calls = 0;
    f.engine.backend.births.insert(55, (2_000_000, 2_000_020));
    f.engine.backend.births_after_first = Some(std::collections::BTreeMap::from([(
        55,
        (999_990, 1_000_010),
    )]));
    f.reconcile();
    assert_eq!(
        f.db(),
        db,
        "the fresh query must still win before committing"
    );
    assert_eq!(f.engine.backend.birth_calls, 2);

    f.engine.backend.birth_calls = 0;
    f.engine.backend.births_after_first = None;
    f.reconcile();
    assert_eq!(f.db()["items"][0]["status"], "wait");
    assert_eq!(f.engine.backend.birth_calls, 2);
    assert_eq!(
        f.db()["items"][0]["wait_ready"],
        db["items"][0]["product_retry_intent"]["snapshot"]["wait_ready"]
    );
    assert!(
        f.engine.backend.ps[&55]["created"].is_null(),
        "the shared snapshot must not invent an exact identity"
    );
}

#[test]
fn finished_product_reenters_the_existing_bounded_attempt_without_new_budget() {
    let mut f = Fixture::new();
    let before = f.db();
    assert!(qualified(&before, &f.engine.backend.records[0], AT));
    f.reconcile();
    let after = f.db();
    let row = scope::row(&after).unwrap();
    let old = &before["items"][0];
    assert!(row["status"] == "wait" && row["dispatchable"] == false);
    assert!(row["wait_ready"] == old["product_retry_intent"]["snapshot"]["wait_ready"]);
    assert!(
        scope::base_key(&row["product_retry_intent"])
            == scope::base_key(&old["product_retry_intent"])
    );
    assert!(row["product_retry_intent"]["attempt"] == old["product_retry_intent"]["attempt"]);
    assert!(row["product_retry_intent"]["bound_job"].is_null());
    assert!(row["processed_results"]["actual"]["binding"] == old["product_retry_intent"]);
    for key in [
        "spec",
        "jobs",
        "note",
        "needs",
        "resume_generation",
        "worker_receipt",
        "product_resume_qualification",
    ] {
        assert!(row[key] == old[key], "original work changed");
    }
    assert!(row["scheduler"]["job"] == old["scheduler"]["job"]);
    assert!(row["scheduler"]["worktree"] == old["scheduler"]["worktree"]);
    assert!(scope::candidate(&after, AT).is_some());
    let plan = cc_todo::product_retry::attempt_plan(
        row,
        scope::base_key(&row["product_retry_intent"])
            .as_str()
            .unwrap(),
        true,
    )
    .unwrap();
    assert!(plan.attempt == 1 && plan.key != old["product_retry_intent"]["key"]);
    let mut proposed = after.clone();
    let mut source = f.engine.backend.records[0].clone();
    source["retry_live"] = json!(false);
    source["thread_id"] = json!(scope::QUALIFICATION_SESSION);
    let mut proof = old["product_retry_intent"]["proof"].clone();
    proof["snapshot"] = cc_todo::product_resume::snapshot(&proposed, &proposed["items"][0]);
    cc_todo::product_retry::stage_attempt(
        &mut proposed,
        &proof,
        &source,
        &plan,
        "C:/fixture/next.md",
        &"b".repeat(64),
        AT,
    )
    .unwrap();
    let stable = f.db();
    f.reconcile();
    assert!(f.db() == stable, "result was consumed twice");
}

#[test]
fn unverified_scope_stops_new_waits_identity_and_duplicate_jobs_are_preserved() {
    for gate in [
        "proof",
        "qualification",
        "expired",
        "stop",
        "wait",
        "dependency",
        "owner_question",
        "generation",
        "worker",
        "key",
        "scope",
        "live",
        "unknown_pid",
        "another",
        "duplicate",
        "guard",
        "registered",
        "already",
    ] {
        let mut f = Fixture::new();
        let mut db = f.db();
        match gate {
            "proof" => db["items"][0]["product_retry_intent"]["proof"]["verified"] = json!(false),
            "qualification" => db["items"][0]["product_resume_qualification"] = Value::Null,
            "expired" => f.engine.backend.clock = scope::LIMIT as f64,
            "stop" => db["items"][0]["stopped"] = json!(true),
            "wait" => db["items"][0]["wait_ready"] = json!({"condition":"retry-time"}),
            "dependency" => {
                db["items"][0]["wait_on"] = json!([2]);
                db["items"]
                    .as_array_mut()
                    .unwrap()
                    .push(json!({"id":2,"status":"todo"}));
            }
            "owner_question" => db["items"][0]["owner_question_wait"] = json!({}),
            "generation" => db["items"][0]["resume_generation"] = json!(99),
            "worker" => db["items"][0]["worker_receipt"]["job"] = json!("other"),
            "key" => f.engine.backend.records[0]["launch_key"] = json!("other"),
            "scope" => f.engine.backend.records[0]["sandbox"] = json!("danger-full-access"),
            "live" => {
                f.engine
                    .backend
                    .ps
                    .insert(42, json!({"pid":42,"created":100}));
            }
            "unknown_pid" => {
                f.engine
                    .backend
                    .ps
                    .insert(42, json!({"pid":42,"created":null}));
            }
            "another" => f
                .engine
                .backend
                .records
                .push(json!({"id":"other","todo_id":scope::ID,"status":"running"})),
            "duplicate" => f
                .engine
                .backend
                .records
                .push(f.engine.backend.records[0].clone()),
            "guard" => f.engine.backend.allowed = false,
            "registered" => db["projects"]["fixture"] = json!("C:/other"),
            "already" => record_result(&mut db["items"][0], &json!("actual"), "already"),
            _ => unreachable!(),
        }
        f.save(&db);
        let before = f.db();
        f.reconcile();
        assert!(f.db() == before, "protected state changed");
    }
}

#[test]
fn newer_wait_stop_handoff_and_terminal_job_change_win_the_locked_commit() {
    for gate in ["stop", "wait", "generation", "project", "job"] {
        let mut f = Fixture::new();
        let path = f.engine.todo_path.clone();
        let job_path = f.job_file.clone();
        let mut changed = f.db();
        match gate {
            "stop" => changed["items"][0]["stopped"] = json!(true),
            "wait" => changed["items"][0]["wait_on"] = json!([2]),
            "generation" => changed["items"][0]["resume_generation"] = json!(99),
            "project" => changed["projects"]["fixture"] = json!("C:/other"),
            _ => (),
        }
        let expected = changed.clone();
        f.engine.backend.on_outcome = Some(Box::new(move || {
            write_json(&path, &changed, 1).unwrap();
            if gate == "job" {
                let mut j = read_json(&job_path, Value::Null).unwrap();
                j["status"] = json!("running");
                write_json(&job_path, &j, 1).unwrap();
            }
        }));
        f.reconcile();
        assert!(f.db() == expected, "newer state overwritten");
    }
}

#[test]
fn attempt_limit_and_original_deadline_are_not_reset() {
    let mut f = Fixture::new();
    let mut db = f.db();
    let intent = &mut db["items"][0]["product_retry_intent"];
    intent["attempt"] = json!(2);
    let key = scope::attempt_key(scope::base_key(intent).as_str().unwrap(), 2).unwrap();
    intent["key"] = json!(key);
    f.engine.backend.records[0]["launch_key"] = json!(key);
    f.engine.backend.records[0]["original_product_retry"] = scope::marker(intent);
    write_json(&f.job_file, &f.engine.backend.records[0], 1).unwrap();
    f.save(&db);
    f.reconcile();
    let after = f.db();
    let row = scope::row(&after).unwrap();
    assert!(row["status"] == "todo" && row["dispatchable"] == false && row["wait_ready"].is_null());
    assert!(row["product_retry_intent"] == db["items"][0]["product_retry_intent"]);
    assert!(scope::candidate(&after, AT).is_none());
}

#[test]
fn pid_reuse_is_not_a_live_owner_and_decimal_generation_is_preserved() {
    let mut f = Fixture::new();
    let mut db = f.db();
    let generation = json!(8.0);
    db["items"][0]["resume_generation"] = generation.clone();
    db["items"][0]["worker_receipt"]["generation"] = generation.clone();
    db["items"][0]["product_retry_intent"]["bound_generation"] = generation.clone();
    f.save(&db);
    f.engine
        .backend
        .ps
        .insert(42, json!({"pid":42,"created":999}));
    f.reconcile();
    assert!(f.db()["items"][0]["status"] == "wait");
    assert!(f.db()["items"][0]["processed_results"]["actual"]["generation"] == generation);
}

fn fixed_review() -> (Value, Value) {
    let subwork = json!({"kind":"review","parent_todo_id":2757,"origin_job_id":"writer",
        "owner":{"repository":"fixture/repo","git_common":"fixture-common","worktree":"fixture-tree","branch":"fixture-branch","fixed_head":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}});
    let item = json!({"id":2813,"project":"cc-pc-tools-rs","status":"doing","dispatchable":true,
        "spec":"fixed-head-review.md","needs":[],"subwork":subwork,"jobs":["review"],
        "resume_generation":4,"worker_receipt":{"job":"review","generation":4},"note":"レビューの元の指示",
        "scheduler":{"job":"review","worktree":"fixture-tree","branch":"fixture-branch","phase":"running"}});
    let job = json!({"id":"review","todo_id":2813,"status":"succeeded","sandbox":"read-only",
        "subwork_validated":true,"subwork_spec":item["spec"],"subwork":subwork,
        "last_message":"状態: 完了（レビュー作業のみ）\n固定headの確認を完了しました。Mustなし。"});
    (item, job)
}

#[test]
fn a_lost_review_with_a_new_spec_uses_normal_recovery() {
    let mut f = Fixture::new();
    let (mut item, mut job) = fixed_review();
    item["spec"] = json!("new-fixed-head.md");
    job["status"] = json!("lost");
    assert!(cc_job_policy::review_result::is_review_subwork(&job));
    assert!(!cc_job_policy::review_result::matches_item(&job, &item));
    let db = json!({"projects":{"cc-pc-tools-rs":scope::TREE},"items":[item],"next_id":2814});
    f.save(&db);
    f.engine.backend.records = vec![job];
    f.reconcile();
    let after = f.db();
    let row = &after["items"][0];
    assert!(row["status"] == "todo");
    assert!(row["scheduler"]["phase"] == "retry");
    assert!(row["scheduler"]["last_stop"] == "lost");
    assert!(row["scheduler"]["last_job"] == "review");
    assert!(row["scheduler"]["job"].is_null());
    assert!(row["scheduler"]["retry_count"] == 1);
    assert!(n(&row["scheduler"]["not_before"]) > AT as f64);
    assert!(row["processed_results"]["review"].is_null());
    for key in [
        "spec",
        "subwork",
        "jobs",
        "resume_generation",
        "worker_receipt",
    ] {
        assert!(row[key] == db["items"][0][key], "saved work changed");
    }
    assert!(f.engine.failures.is_empty());
}

#[test]
fn a_completed_fixed_head_review_is_consumed_once_instead_of_173_restarts() {
    let mut f = Fixture::new();
    let (item, job) = fixed_review();
    let db = json!({"projects":{"cc-pc-tools-rs":scope::TREE},"items":[item],"next_id":2814});
    f.save(&db);
    f.engine.backend.records = vec![job];
    for _ in 0..173 {
        f.reconcile();
    }
    let after = f.db();
    let row = &after["items"][0];
    assert!(row["status"] == "done");
    assert!(row["processed_results"]["review"]["reason"] == "done");
    assert!(row["scheduler"]["continuation_count"].is_null());
    assert!(row["resume_generation"] == 4 && row["jobs"] == json!(["review"]));
}

#[test]
fn root_implementation_qualifiers_stay_unfinished_and_ambiguous_review_stays_strict() {
    let (_, job) = fixed_review();
    let mut root = job.clone();
    root.as_object_mut().unwrap().remove("subwork");
    for result in [
        "状態: 完了（レビュー作業のみ）",
        "状態: 完了（実装だけ・検証未了）",
    ] {
        assert!(crate::backend::job_result_outcome(&root, result) == "todo");
    }
    assert!(
        crate::backend::job_result_outcome(&job, "状態: 完了（レビュー作業のみ）\n状態: 継続中")
            == "todo"
    );
    assert!(crate::backend::result_outcome("状態: 完了（レビュー作業のみ）") == "todo");
}

#[test]
fn unreadable_product_metadata_defers_only_that_product() {
    let mut f = Fixture::new();
    let before = f.db();
    fs::write(&f.job_file, b"{").unwrap();
    f.reconcile();
    assert!(
        f.db() == before,
        "unreadable product changed the canonical store"
    );
    assert!(f.engine.report["product_results_deferred"] == json!([scope::ID]));
    assert!(
        f.engine.failures.is_empty(),
        "one product suppressed unrelated repair dispatch"
    );
}

#[test]
fn an_old_review_result_cannot_complete_a_new_fixed_scope_or_concurrent_wait() {
    for gate in [
        "spec",
        "scope",
        "during_spec",
        "during_scope",
        "during_wait",
    ] {
        let mut f = Fixture::new();
        let (mut item, job) = fixed_review();
        if gate == "spec" {
            item["spec"] = json!("new-fixed-head.md");
        }
        if gate == "scope" {
            item["subwork"]["owner"]["fixed_head"] =
                json!("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
        }
        let db = json!({"projects":{"cc-pc-tools-rs":scope::TREE},"items":[item],"next_id":2814});
        f.save(&db);
        f.engine.backend.records = vec![job];
        let mut expected = db.clone();
        match gate {
            "during_spec" => expected["items"][0]["spec"] = json!("new-fixed-head.md"),
            "during_scope" => {
                expected["items"][0]["subwork"]["owner"]["fixed_head"] =
                    json!("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
            }
            "during_wait" => {
                expected["items"][0]["wait_ready"] =
                    json!({"condition":"retry-time","since":AT,"not_before":AT+3600})
            }
            _ => (),
        }
        if gate.starts_with("during_") {
            let path = f.engine.todo_path.clone();
            let saved = expected.clone();
            f.engine.backend.on_outcome = Some(Box::new(move || {
                write_json(&path, &saved, 1).unwrap();
            }));
        }
        f.reconcile();
        assert!(
            f.db() == expected,
            "old review result replaced new work or wait"
        );
    }
}
