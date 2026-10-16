use super::*;
use runtime::Runtime;
use std::{cell::Cell, collections::BTreeMap};

const T: u64 = 1792108800;
const SESSION: &str = "22222222-2222-4222-8222-222222222222";
type Mutation = Box<dyn FnOnce(&Paths)>;

struct Fake {
    paths: Paths,
    at: Cell<u64>,
    proof_error: bool,
    guard_calls: usize,
    bulk_calls: usize,
    known: Option<Vec<Value>>,
    ps: processes::Processes,
    births: BTreeMap<u32, Result<Option<u64>>>,
    probes: Vec<u32>,
    on_jobs: Option<Mutation>,
    expire_after_jobs: bool,
}
fn proof(db: &Value) -> Value {
    json!({"id":policy::ID,"verified":true,"session_receipt_verified":true,
        "session":policy::QUALIFICATION_SESSION,"job":policy::QUALIFICATION_JOB,
        "request_sha256":policy::REQUEST_HASH,"request":"fixture-original.md","worktree":policy::TREE,
        "expires_at":"2026-10-21T00:00:00Z","snapshot":crate::product_resume::snapshot(db,policy::row(db).unwrap())})
}
fn write(path: &Path, value: &Value) {
    fs::write(path, serde_json::to_vec(value).unwrap()).unwrap();
}
fn jobs(paths: &Paths) -> Vec<Value> {
    fs::read_dir(&paths.jobs)
        .unwrap()
        .map(|entry| entry.unwrap().path().join("job.json"))
        .filter(|file| file.is_file())
        .map(|file| load(&file).unwrap())
        .collect()
}
impl Fake {
    fn assert_outside_gate(&self) {
        drop(
            Lock::acquire(&self.paths.jobs.join(".queue.lock"), Duration::ZERO)
                .expect("bulk IO inside queue gate"),
        );
    }
}
impl Runtime for Fake {
    fn guard(&mut self) -> Result<Value> {
        self.assert_outside_gate();
        self.guard_calls += 1;
        if self.proof_error {
            return Err("fixture_guard_failure".into());
        }
        Ok(proof(&load(&self.paths.store)?))
    }
    fn jobs(&mut self) -> Result<Vec<Value>> {
        self.assert_outside_gate();
        self.bulk_calls += 1;
        let known = self.known.clone().unwrap_or_else(|| jobs(&self.paths));
        if let Some(mutate) = self.on_jobs.take() {
            mutate(&self.paths);
        }
        if self.expire_after_jobs {
            self.at.set(policy::LIMIT);
        }
        Ok(known)
    }
    fn snapshot(&mut self) -> Result<processes::Processes> {
        self.assert_outside_gate();
        self.bulk_calls += 1;
        Ok(self.ps.clone())
    }
    fn process_birth(&mut self, pid: u32) -> Result<Option<u64>> {
        assert!(Lock::acquire(&self.paths.jobs.join(".queue.lock"), Duration::ZERO).is_err());
        assert!(Lock::acquire(&self.paths.lock, Duration::ZERO).is_err());
        self.probes.push(pid);
        self.births.get(&pid).cloned().unwrap_or(Ok(None))
    }
    fn clock(&self) -> u64 {
        self.at.get()
    }
}
struct Fixture {
    _temp: tempfile::TempDir,
    paths: Paths,
    runtime: Fake,
    base: String,
}
impl Fixture {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let home = temp.path().join("todo");
        let job_root = temp.path().join("jobs");
        fs::create_dir_all(&home).unwrap();
        fs::create_dir_all(job_root.join("source")).unwrap();
        let paths = Paths::at(&home, &job_root);
        let db = json!({"projects":{"PhosGaea-Vol1":"fixture-repo"},"items":[{
            "id":1626,"project":"PhosGaea-Vol1","status":"wait","dispatchable":false,
            "spec":"fixture-spec.md","jobs":["source"],"needs":[],"resume_generation":9,
            "worker_receipt":{"job":"source","generation":9},
            "scheduler":{"job":"source","worktree":policy::TREE,"branch":"scheduler/todo-1626-1"},
            "wait_ready":{"condition":"retry-time","since":T-120,"not_before":T-60},
            "progress":40,"note":"Keep previous work."}]});
        write(&paths.store, &db);
        write(
            &job_root.join("source/job.json"),
            &json!({"id":"source","todo_id":1626,
            "status":"succeeded","cwd":policy::TREE,"thread_id":SESSION,
            "model":"gpt-6.1-sol","effort":"high","sandbox":"danger-full-access",
            "mcp_profile":"browser","network":false,"add_dirs":[],"scope_source":"launcher"}),
        );
        let base = policy::key(&db["items"][0]["wait_ready"]).unwrap();
        let runtime = Fake {
            paths: paths.clone(),
            at: Cell::new(T),
            proof_error: false,
            guard_calls: 0,
            bulk_calls: 0,
            known: None,
            ps: BTreeMap::new(),
            births: BTreeMap::new(),
            probes: vec![],
            on_jobs: None,
            expire_after_jobs: false,
        };
        Self {
            _temp: temp,
            paths,
            runtime,
            base,
        }
    }
    fn db(&self) -> Value {
        load(&self.paths.store).unwrap()
    }
    fn mutate_row(&self, mutate: impl FnOnce(&mut Value)) {
        let mut db = self.db();
        mutate(&mut db["items"][0]);
        write(&self.paths.store, &db);
    }
    fn mutate_job(&self, id: &str, mutate: impl FnOnce(&mut Value)) {
        let file = self.paths.jobs.join(id).join("job.json");
        let mut job = load(&file).unwrap();
        mutate(&mut job);
        write(&file, &job);
    }
    fn reserve(&mut self) -> Result<Value> {
        let worker = self.db()["items"][0]["worker_receipt"]["job"]
            .as_str()
            .unwrap()
            .to_owned();
        runtime::run(
            &self.paths,
            &self.base,
            Some(&worker),
            None,
            &mut self.runtime,
        )
    }
    fn bind(&mut self, id: &str) -> Result<Value> {
        let key = self.db()["items"][0]["product_retry_intent"]["key"]
            .as_str()
            .unwrap()
            .to_owned();
        runtime::run(&self.paths, &key, None, Some(id), &mut self.runtime)
    }
    // The real shared registration helper stamps the marker on a normal queued
    // job, using the body that the ordinary CLI copies from --prompt-file.
    fn accept(&self, id: &str) {
        let db = self.db();
        let intent = &db["items"][0]["product_retry_intent"];
        let source = &intent["source"];
        let body = prompt_bytes(Path::new(intent["prompt_file"].as_str().unwrap())).unwrap();
        let mut job = json!({"id":id,"todo_id":policy::ID,"status":"queued","started_at":null,"codex_pid":null,
            "launch_key":intent["key"],"resume_session":source["session"],"cwd":source["cwd"],
            "prompt_source":intent["prompt_file"],"model":source["model"],"requested_model":source["model"],
            "effort":source["effort"],"sandbox":source["sandbox"],"mcp_profile":source["mcp_profile"],
            "network":source["network"],"add_dirs":source["add_dirs"],"scope_source":source["scope_source"]});
        job["original_product_retry"] = policy::registration(
            &db,
            &job,
            intent["key"].as_str().unwrap(),
            &policy::digest(&body),
            T,
        )
        .unwrap();
        let folder = self.paths.jobs.join(id);
        fs::create_dir_all(&folder).unwrap();
        write(&folder.join("job.json"), &job);
        fs::write(folder.join("prompt.md"), body).unwrap();
    }
    fn folder(&self) -> PathBuf {
        self.paths
            .store
            .parent()
            .unwrap()
            .join("product-retry-prompts")
    }
    fn new_wait(&mut self) {
        self.mutate_row(|row| {
            row["status"] = json!("wait");
            row["wait_ready"] = json!({"condition":"retry-time","since":T-60,"not_before":T});
        });
        self.base = policy::key(&self.db()["items"][0]["wait_ready"]).unwrap();
    }
}

#[test]
fn full_reserve_register_bind_and_lost_response_replay_preserve_the_wait() {
    let mut f = Fixture::new();
    let before = f.db();
    let receipt = f.reserve().unwrap();
    assert_eq!(receipt["launched"], 0);
    assert_eq!(receipt["key"], f.base);
    assert_eq!(
        f.db()["items"][0]["wait_ready"],
        before["items"][0]["wait_ready"]
    );
    let intent = f.db()["items"][0]["product_retry_intent"].clone();
    let file = Path::new(intent["prompt_file"].as_str().unwrap());
    assert_eq!(file, runtime::physical(file).unwrap());
    assert_eq!(
        policy::digest(&fs::read(file).unwrap()),
        intent["prompt_sha256"]
    );
    let reserved = fs::read(&f.paths.store).unwrap();
    assert_eq!(f.reserve().unwrap()["saved"], false);
    assert_eq!(fs::read(&f.paths.store).unwrap(), reserved);
    f.accept("accepted");
    assert_eq!(f.reserve().unwrap()["saved"], false);
    assert_eq!(f.bind("accepted").unwrap()["reused"], false);
    let bound = fs::read(&f.paths.store).unwrap();
    assert!(f.db()["items"][0]["wait_ready"].is_null());
    assert_eq!(f.db()["items"][0]["worker_receipt"]["job"], "accepted");
    assert_eq!(f.db()["items"][0]["progress"], 40);
    assert_eq!(f.db()["items"][0]["dispatchable"], false);
    assert_eq!(f.bind("accepted").unwrap()["reused"], true);
    assert_eq!(fs::read(&f.paths.store).unwrap(), bound);
    assert_eq!(f.runtime.guard_calls, 5);
    assert_eq!(f.runtime.bulk_calls, 10);
}

#[test]
fn production_entry_cannot_enable_the_private_fixture_runtime() {
    let f = Fixture::new();
    assert_eq!(
        run(&f.paths, &f.base, Some("source"), None).unwrap_err(),
        "product_retry_input_invalid"
    );
    assert!(!f.folder().exists());
}

#[test]
fn guard_failure_invalid_input_and_expiry_do_not_publish_a_body() {
    let mut f = Fixture::new();
    let before = fs::read(&f.paths.store).unwrap();
    assert!(
        runtime::run(
            &f.paths,
            "../../invalid",
            Some("source"),
            None,
            &mut f.runtime
        )
        .is_err()
    );
    assert_eq!(f.runtime.guard_calls, 0);
    f.runtime.proof_error = true;
    assert!(f.reserve().is_err());
    assert_eq!(f.runtime.bulk_calls, 0);
    f.runtime.proof_error = false;
    f.runtime.expire_after_jobs = true;
    assert!(f.reserve().is_err());
    assert!(!f.folder().exists());
    assert_eq!(fs::read(&f.paths.store).unwrap(), before);
}

#[test]
fn complete_scope_and_candidate_validation_precedes_prompt_io() {
    for gate in [
        "wrong_key",
        "not_due",
        "owner_stop",
        "cwd",
        "settings",
        "running",
        "source",
        "history",
    ] {
        let mut f = Fixture::new();
        match gate {
            "wrong_key" => f.base = "a".repeat(32),
            "not_due" => f.mutate_row(|row| row["wait_ready"]["not_before"] = json!(T + 300)),
            "owner_stop" => f.mutate_row(|row| row["stopped"] = json!(true)),
            "cwd" => f.mutate_job("source", |job| job["cwd"] = json!("fixture-other")),
            "settings" => f.mutate_job("source", |job| job["sandbox"] = json!("unknown")),
            "running" => f.mutate_job("source", |job| job["status"] = json!("running")),
            "source" => f.mutate_job("source", |job| job["todo_id"] = json!(99)),
            "history" => f.mutate_row(|row| row["product_retry_history"] = json!("invalid")),
            _ => unreachable!(),
        }
        let before = fs::read(&f.paths.store).unwrap();
        assert!(f.reserve().is_err(), "{gate}");
        assert!(!f.folder().exists(), "{gate}");
        assert_eq!(fs::read(&f.paths.store).unwrap(), before, "{gate}");
    }
}

#[test]
fn fresh_target_metadata_and_owner_stop_win_over_the_bulk_snapshot() {
    for gate in ["source_stop", "source_running", "owner_stop"] {
        let mut f = Fixture::new();
        f.runtime.on_jobs = Some(Box::new(move |paths| {
            if gate == "owner_stop" {
                let mut db = load(&paths.store).unwrap();
                db["items"][0]["stopped"] = json!(true);
                write(&paths.store, &db);
            } else {
                let file = paths.jobs.join("source/job.json");
                let mut job = load(&file).unwrap();
                if gate == "source_stop" {
                    job["cancel_requested"] = json!(true);
                } else {
                    job["status"] = json!("running");
                }
                write(&file, &job);
            }
        }));
        assert!(f.reserve().is_err(), "{gate}");
        assert!(f.db()["items"][0]["product_retry_intent"].is_null());
        assert!(!f.folder().exists());
        if gate == "owner_stop" {
            assert_eq!(f.db()["items"][0]["stopped"], true);
        }
    }
    let mut f = Fixture::new();
    f.reserve().unwrap();
    f.accept("accepted");
    let reserved = f.db();
    f.runtime.on_jobs = Some(Box::new(|paths| {
        let file = paths.jobs.join("accepted/job.json");
        let mut job = load(&file).unwrap();
        job["sandbox"] = json!("workspace-write");
        write(&file, &job);
    }));
    assert!(f.bind("accepted").is_err());
    assert_eq!(f.db(), reserved);
}

#[test]
fn target_pid_identity_is_fresh_and_unknown_is_never_gone() {
    for birth in [
        Ok(Some(12)),
        Ok(Some(13)),
        Ok(None),
        Err("identity denied".into()),
    ] {
        let mut f = Fixture::new();
        f.mutate_job("source", |job| {
            job["pid"] = json!(444);
            job["pid_created"] = json!(12);
        });
        let allowed = matches!(birth, Ok(Some(13) | None));
        f.runtime.births.insert(444, birth);
        assert_eq!(f.reserve().is_ok(), allowed);
        assert_eq!(f.runtime.probes, vec![444]);
        assert_eq!(f.folder().exists(), allowed);
    }
}

#[test]
fn busy_job_or_todo_lock_releases_the_queue_without_a_long_wait() {
    for target in ["state", "todo"] {
        let mut f = Fixture::new();
        let path = if target == "state" {
            f.paths.jobs.join("source/state.lock")
        } else {
            f.paths.lock.clone()
        };
        let held = Lock::acquire(&path, Duration::ZERO).unwrap();
        let start = std::time::Instant::now();
        assert!(f.reserve().is_err());
        assert!(start.elapsed() < Duration::from_secs(2));
        drop(Lock::acquire(&f.paths.jobs.join(".queue.lock"), Duration::ZERO).unwrap());
        assert!(!f.folder().exists());
        drop(held);
    }
}

#[test]
fn prompt_failure_and_partial_unpublished_files_cannot_poison_a_reservation() {
    let mut f = Fixture::new();
    fs::write(f.folder(), b"blocked directory").unwrap();
    let before = fs::read(&f.paths.store).unwrap();
    assert!(f.reserve().is_err());
    assert_eq!(fs::read(&f.paths.store).unwrap(), before);
    assert_eq!(fs::read(f.folder()).unwrap(), b"blocked directory");
    fs::remove_file(f.folder()).unwrap();
    fs::create_dir(f.folder()).unwrap();
    fs::write(
        f.folder().join(format!(".{}-abandoned.tmp", f.base)),
        b"partial",
    )
    .unwrap();
    f.reserve().unwrap();
    let intent = f.db()["items"][0]["product_retry_intent"].clone();
    assert_eq!(
        fs::read(intent["prompt_file"].as_str().unwrap()).unwrap(),
        CONTINUATION.as_bytes()
    );
    assert_eq!(
        fs::read(f.folder().join(format!(".{}-abandoned.tmp", f.base))).unwrap(),
        b"partial"
    );
}

#[test]
fn a_corrupt_existing_destination_is_preserved_and_never_committed() {
    let mut f = Fixture::new();
    fs::create_dir(f.folder()).unwrap();
    let digest = policy::digest(CONTINUATION.as_bytes());
    let file = f.folder().join(format!("{}-{}.md", f.base, &digest[..16]));
    fs::write(&file, b"partial original").unwrap();
    let before = fs::read(&f.paths.store).unwrap();
    assert!(f.reserve().is_err());
    assert_eq!(fs::read(&file).unwrap(), b"partial original");
    assert_eq!(fs::read(&f.paths.store).unwrap(), before);
    assert_eq!(fs::read_dir(f.folder()).unwrap().count(), 1);
}

#[test]
fn a_missing_committed_body_is_not_recreated_from_the_current_tool() {
    let mut f = Fixture::new();
    f.reserve().unwrap();
    let intent = f.db()["items"][0]["product_retry_intent"].clone();
    let file = Path::new(intent["prompt_file"].as_str().unwrap());
    fs::remove_file(file).unwrap();
    let before = fs::read(&f.paths.store).unwrap();
    assert!(f.reserve().is_err());
    assert!(!file.exists());
    assert_eq!(fs::read(&f.paths.store).unwrap(), before);
}

#[test]
fn a_replayed_intent_keeps_its_committed_prompt_across_tool_updates() {
    let temp = tempfile::tempdir().unwrap();
    let key = "a".repeat(32);
    let (file, digest) =
        prepare_prompt(temp.path(), &key, &Value::Null, b"old committed version").unwrap();
    let intent = json!({"key":key,"prompt_file":file,"prompt_sha256":digest});
    let (same, same_digest) =
        prepare_prompt(temp.path(), &key, &intent, b"new tool version").unwrap();
    assert_eq!(same, file);
    assert_eq!(same_digest, digest);
    assert_eq!(fs::read(&same).unwrap(), b"old committed version");
    fs::remove_file(&file).unwrap();
    assert!(prepare_prompt(temp.path(), &key, &intent, b"new tool version").is_err());
    assert!(!file.exists());
}

#[test]
fn bound_context_replay_and_nonterminal_defer_keep_the_exact_worker_and_wait() {
    let mut f = Fixture::new();
    f.reserve().unwrap();
    f.accept("accepted");
    f.bind("accepted").unwrap();
    f.mutate_job("accepted", |job| {
        job["first_started_at"] = json!("2026-10-16T00:00:00Z");
        job["resume_session"] = Value::Null;
        job["context_phase"] = json!("continue");
        job["model"] = json!("anthropic/claude-sonnet-5-5");
    });
    assert_eq!(f.bind("accepted").unwrap()["reused"], true);
    f.new_wait();
    f.mutate_row(|row| row["wait_ready"]["not_before"] = json!(T + 300));
    let before = f.db();
    let replay = f.bind("accepted").unwrap();
    assert_eq!(replay["reused"], true);
    assert_eq!(replay["deferred"], true);
    assert_eq!(f.db(), before);
    f.mutate_row(|row| row["status"] = json!("block"));
    let before = f.db();
    assert!(f.bind("accepted").is_err());
    assert_eq!(f.db(), before);
}

#[test]
fn queued_bound_context_continues_but_terminal_bound_context_stages_a_new_wait() {
    for terminal in [false, true] {
        let mut f = Fixture::new();
        f.reserve().unwrap();
        f.accept("accepted");
        f.bind("accepted").unwrap();
        f.mutate_job("accepted", |job| {
            job["first_started_at"] = json!("2026-10-16T00:00:00Z");
            job["thread_id"] = json!("33333333-3333-4333-8333-333333333333");
            job["resume_session"] = Value::Null;
            job["model"] = json!("anthropic/claude-sonnet-5-5");
            if terminal {
                job["status"] = json!("succeeded");
            }
        });
        let previous = f.db()["items"][0]["product_retry_intent"].clone();
        f.new_wait();
        let result = f.reserve().unwrap();
        let after = f.db();
        if terminal {
            assert_eq!(result["reserved"], true);
            assert_eq!(result["key"], f.base);
            assert_eq!(result["attempt"], 0);
            assert_eq!(
                after["items"][0]["product_retry_intent"]["source"]["id"],
                "accepted"
            );
            assert!(after["items"][0]["product_retry_intent"]["bound_job"].is_null());
            assert!(after["items"][0]["wait_ready"].is_object());
            assert_eq!(after["items"][0]["product_retry_history"][0], previous);
        } else {
            assert_eq!(result["continued_existing"], true);
            assert_eq!(
                after["items"][0]["product_retry_intent"]["key"],
                previous["key"]
            );
            assert!(after["items"][0]["wait_ready"].is_null());
        }
    }
}

#[test]
fn unbound_context_changes_cannot_bind_or_clear_the_original_wait() {
    let mut f = Fixture::new();
    f.reserve().unwrap();
    f.accept("accepted");
    f.mutate_job("accepted", |job| {
        job["first_started_at"] = json!("2026-10-16T00:00:00Z");
        job["resume_session"] = Value::Null;
    });
    let before = f.db();
    assert!(f.bind("accepted").is_err());
    assert_eq!(f.db(), before);
}

#[test]
fn accepted_terminal_attempts_rotate_only_within_the_three_keys() {
    let mut f = Fixture::new();
    assert_eq!(f.reserve().unwrap()["attempt"], 0);
    for attempt in 0..=2 {
        let id = format!("accepted-{attempt}");
        f.accept(&id);
        f.mutate_job(&id, |job| job["status"] = json!("failed"));
        let before = f.db();
        let result = f.reserve();
        if attempt == 2 {
            assert_eq!(result.unwrap_err(), "product_retry_attempts_exhausted");
            assert_eq!(f.db(), before);
        } else {
            let result = result.unwrap();
            let effective = policy::attempt_key(&f.base, attempt + 1).unwrap();
            assert_eq!(result["attempt"], attempt + 1);
            assert_eq!(result["key"], effective);
            let args = result["run_args"].as_array().unwrap();
            for flag in ["--launch-key", "--original-product-retry-key"] {
                let index = args.iter().position(|arg| arg == flag).unwrap();
                assert_eq!(args[index + 1], effective);
            }
            assert_eq!(f.reserve().unwrap()["saved"], false);
        }
    }
    assert!(f.db()["items"][0]["wait_ready"].is_object());
}

#[test]
fn unaccepted_snapshot_change_rotates_but_same_snapshot_replays() {
    let mut f = Fixture::new();
    f.reserve().unwrap();
    assert_eq!(f.reserve().unwrap()["attempt"], 0);
    f.mutate_row(|row| row["spec"] = json!("fixture-updated-spec.md"));
    let result = f.reserve().unwrap();
    assert_eq!(result["attempt"], 1);
    assert_eq!(result["key"], policy::attempt_key(&f.base, 1).unwrap());
    assert_eq!(
        f.db()["items"][0]["product_retry_history"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    assert_eq!(f.reserve().unwrap()["saved"], false);
}

#[test]
fn live_or_unknown_accepted_pid_cannot_be_retired_from_a_stale_snapshot() {
    for birth in [Ok(Some(9)), Err("query denied".into()), Ok(Some(10))] {
        let mut f = Fixture::new();
        f.reserve().unwrap();
        f.accept("accepted");
        f.mutate_job("accepted", |job| {
            job["status"] = json!("failed");
            job["pid"] = json!(555);
            job["pid_created"] = json!(9);
        });
        let allowed = matches!(birth, Ok(Some(10)));
        f.runtime.births.insert(555, birth);
        let before = f.db();
        assert_eq!(f.reserve().is_ok(), allowed);
        assert_eq!(f.runtime.probes, vec![555]);
        if !allowed {
            assert_eq!(f.db(), before);
        }
    }
}

#[test]
fn a_new_other_worker_in_the_bulk_snapshot_blocks_reservation() {
    let mut f = Fixture::new();
    let mut known = jobs(&f.paths);
    known.push(json!({"id":"another","todo_id":policy::ID,"status":"running"}));
    f.runtime.known = Some(known);
    assert!(f.reserve().is_err());
    assert!(!f.folder().exists());
}

#[test]
fn final_bind_checks_the_fresh_stop_and_actual_registered_body() {
    for gate in [
        "owner_stop",
        "accepted_stop",
        "body_changed",
        "body_missing",
    ] {
        let mut f = Fixture::new();
        f.reserve().unwrap();
        f.accept("accepted");
        let before = f.db();
        match gate {
            "owner_stop" => {
                f.runtime.on_jobs = Some(Box::new(|paths| {
                    let mut db = load(&paths.store).unwrap();
                    db["items"][0]["stopped"] = json!(true);
                    write(&paths.store, &db);
                }))
            }
            "accepted_stop" => {
                f.runtime.on_jobs = Some(Box::new(|paths| {
                    let file = paths.jobs.join("accepted/job.json");
                    let mut job = load(&file).unwrap();
                    job["cancel_requested"] = json!(true);
                    write(&file, &job);
                }))
            }
            "body_changed" => {
                fs::write(f.paths.jobs.join("accepted/prompt.md"), b"changed").unwrap()
            }
            "body_missing" => fs::remove_file(f.paths.jobs.join("accepted/prompt.md")).unwrap(),
            _ => unreachable!(),
        }
        assert!(f.bind("accepted").is_err(), "{gate}");
        let after = f.db();
        assert_eq!(
            after["items"][0]["wait_ready"], before["items"][0]["wait_ready"],
            "{gate}"
        );
        assert_eq!(
            after["items"][0]["product_retry_intent"], before["items"][0]["product_retry_intent"],
            "{gate}"
        );
        if gate == "owner_stop" {
            assert_eq!(after["items"][0]["stopped"], true);
        }
    }
}

#[cfg(windows)]
#[test]
fn the_production_query_limited_probe_identifies_this_process() {
    let mut runtime = runtime::Production::new();
    let first = runtime.process_birth(std::process::id()).unwrap().unwrap();
    assert!(first > 0);
    assert_eq!(
        runtime.process_birth(std::process::id()).unwrap(),
        Some(first)
    );
    assert_eq!(runtime.process_birth(0).unwrap(), None);
}

#[cfg(windows)]
#[test]
fn junction_home_publishes_the_physical_prompt_path_used_by_registration() {
    use std::{os::windows::process::CommandExt, process::Command};
    let mut f = Fixture::new();
    let alias = f._temp.path().join("alias");
    let output = Command::new("cmd.exe")
        .args(["/d", "/c", "mklink", "/J"])
        .arg(&alias)
        .arg(f.paths.store.parent().unwrap())
        .creation_flags(0x08000000)
        .output()
        .unwrap();
    assert!(output.status.success());
    f.paths = Paths::at(&alias, &f.paths.jobs);
    f.runtime.paths = f.paths.clone();
    f.reserve().unwrap();
    let intent = f.db()["items"][0]["product_retry_intent"].clone();
    let file = Path::new(intent["prompt_file"].as_str().unwrap());
    assert_eq!(file, runtime::physical(file).unwrap());
    assert!(!file.starts_with(&alias));
    f.accept("accepted");
    f.bind("accepted").unwrap();
    assert_eq!(f.bind("accepted").unwrap()["reused"], true);
    fs::remove_dir(&alias).unwrap();
}
