//! Unit tests loaded by queue.rs so the real index publication can be exercised
//! without changing its visibility or running the unrelated admission pipeline.
use super::*;
use std::{
    cell::Cell,
    collections::BTreeMap,
    io,
    path::PathBuf,
    sync::{Arc, Mutex},
};

thread_local! {
    static SNAPSHOT_READS: Cell<usize> = const { Cell::new(0) };
}

/// Count attempts immediately before the real canonical read/parse. Thread-local
/// accounting keeps parallel tests independent; no production counter or cache.
pub(super) fn record_snapshot_read() {
    SNAPSHOT_READS.with(|reads| reads.set(reads.get() + 1));
}

fn take_reads() -> usize {
    SNAPSHOT_READS.with(|reads| reads.replace(0))
}

struct SnapshotRuntime {
    todo_home: Option<PathBuf>,
    checkout_hook: Mutex<Option<Box<dyn FnOnce() + Send>>>,
}

impl Runtime for SnapshotRuntime {
    fn env(&self) -> BTreeMap<String, String> {
        self.todo_home
            .as_ref()
            .map(|home| {
                BTreeMap::from([("CC_TODO_HOME".into(), home.to_string_lossy().into_owned())])
            })
            .unwrap_or_default()
    }

    fn mono(&self) -> f64 {
        0.
    }

    fn work_checkout(&self, _: &str) -> Value {
        if let Some(hook) = self.checkout_hook.lock().unwrap().take() {
            hook();
        }
        json!({"repository":"fixture/repo","worktree":"fixture-tree"})
    }

    fn proc_created(&self, _: u32) -> Option<u64> {
        None
    }

    fn call(&self, _: Call) -> io::Result<Reply> {
        Err(io::Error::other("unexpected external call"))
    }

    fn spawn(&self, _: &Spawn) -> io::Result<Box<dyn Running>> {
        Err(io::Error::other("unexpected process spawn"))
    }

    fn spawn_supervisor(&self, _: &Path, _: &Path, _: &Path, _: &str) -> io::Result<bool> {
        Err(io::Error::other("unexpected supervisor spawn"))
    }
}

struct Fixture {
    dir: tempfile::TempDir,
    ctx: Context,
    rt: Arc<SnapshotRuntime>,
    store: PathBuf,
}

impl Fixture {
    fn new(default_store: bool) -> Self {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path().join("tool");
        let home = dir
            .path()
            .join(if default_store { "todo" } else { "canonical" });
        fs::create_dir_all(root.join("jobs")).unwrap();
        fs::create_dir_all(&home).unwrap();
        let rt = Arc::new(SnapshotRuntime {
            todo_home: (!default_store).then(|| home.clone()),
            checkout_hook: Mutex::new(None),
        });
        let ctx = Context {
            jobs: root.join("jobs"),
            home: dir.path().join("home"),
            codex_home: dir.path().join("codex-home"),
            gto: root.join("unused-gto"),
            executable: root.join("unused-supervisor"),
            root,
            rt: rt.clone(),
        };
        Self {
            dir,
            ctx,
            rt,
            store: home.join("todo.json"),
        }
    }

    fn db(&self, pr: u64, count: usize) -> Value {
        json!({"projects":{"fixture":self.dir.path().join(format!("registered-{pr}"))},
            "items":(1..=count).map(|id| json!({"id":id,"status":"todo",
                "title":format!("作業 PR #{pr}"),"jobs":[format!("fixture-{id}")]})).collect::<Vec<_>>()})
    }

    fn write_db(&self, db: &Value) {
        fs::write(&self.store, serde_json::to_vec(db).unwrap()).unwrap();
    }

    fn publish(&self, index: &Value, jobs: Vec<Value>) -> Vec<Value> {
        self.ctx.save_admission_index(index, jobs, 100.).unwrap()
    }

    fn index(&self) -> Value {
        serde_json::from_slice(&fs::read(self.ctx.jobs.join(".queue-index.json")).unwrap()).unwrap()
    }
}

fn jobs(count: usize) -> Vec<Value> {
    (1..=count)
        .map(|id| {
            json!({"id":format!("fixture-{id}"),"status":"queued","todo_id":id,
                "project":"fixture","cwd":"fixture-cwd","queue_sequence":id,
                "work_owner":{"repository":"fixture/repo","worktree":"fixture-tree"}})
        })
        .collect()
}

#[test]
fn large_canonical_store_reproduces_per_job_reads_but_publication_reads_once() {
    let f = Fixture::new(false);
    let mut db = f.db(86, 8);
    db["items"][0]["note_history"] = json!(["x".repeat(20 * 1024 * 1024)]);
    // Exercise the canonical reader's BOM and >16 MiB contract as well as its
    // real filesystem read and parse, without wall-clock performance assertions.
    let mut bytes = vec![0xef, 0xbb, 0xbf];
    bytes.extend(serde_json::to_vec(&db).unwrap());
    fs::write(&f.store, bytes).unwrap();
    assert!(fs::metadata(&f.store).unwrap().len() > 20 * 1024 * 1024);

    for count in [1, 8] {
        let source: Vec<_> = jobs(count)
            .into_iter()
            .map(|mut job| {
                // A populated title isolates the old loop's N reads, rather
                // than its additional second read for each missing title.
                job["task_title"] = json!("作業 PR #86");
                job
            })
            .collect();
        let mut legacy = source.clone();
        take_reads();
        // This is the former save_admission_index loop, using real public calls.
        for job in &mut legacy {
            f.ctx.record_work_owner(job);
        }
        assert_eq!(take_reads(), count, "legacy publication reads once per job");

        let active = f.publish(&json!({}), source);
        assert_eq!(take_reads(), 1, "publication must share one canonical read");
        assert_eq!(active, legacy);
        let index = f.index();
        assert_eq!(index["ids"].as_array().unwrap().len(), count);
        for job in &active {
            assert_eq!(index["owners"][s(job, "id")], admission_owner(job));
        }
    }
}

#[test]
fn snapshot_matches_standalone_title_registration_unknown_and_owner_policy() {
    for default_store in [false, true] {
        let f = Fixture::new(default_store);
        let mut db = f.db(86, 2);
        db["items"][0]["jobs"] = json!(["fixture-2"]);
        db["items"][1]["title"] = json!("別の作業 PR #87");
        f.write_db(&db);
        assert_eq!(f.ctx.todo_store(), f.store);
        let mut source = jobs(6);
        source[1]["todo_id"] = json!("1"); // Linked job ID, not numeric coercion.
        source[2]["todo_id"] = json!(2);
        source[2]["task_title"] = json!("記録済み PR #91");
        source[2]["registered_cwd"] = json!("already-registered");
        source[3]["project"] = json!("unknown");
        source[3]["work_owner"] = Value::Null; // Runtime checkout default.
        source[4]["todo_id"] = json!("1");
        source[4]["project"] = json!("unknown");
        source[4]["work_owner"] = json!({}); // An explicit empty identity stays empty.
        source[5]["todo_id"] = json!(2);
        source[5]["work_owner"] = json!({"repository":"fixture/other","branch":"kept"});

        let mut standalone = source.clone();
        take_reads();
        for job in &mut standalone {
            f.ctx.record_work_owner(job);
        }
        assert_eq!(
            take_reads(),
            source.len(),
            "title lookup shares the same read"
        );
        let active = f.publish(&json!({}), source);
        assert_eq!(take_reads(), 1);
        assert_eq!(active, standalone);
        assert_eq!(active[0]["task_title"], "作業 PR #86");
        assert_eq!(active[1]["task_title"], active[0]["task_title"]);
        assert_eq!(active[0]["registered_cwd"], db["projects"]["fixture"]);
        assert_eq!(active[2]["task_title"], "記録済み PR #91");
        assert_eq!(active[2]["registered_cwd"], "already-registered");
        assert_eq!(active[2]["work_owner"]["pr"], 91);
        assert!(active[3]["registered_cwd"].is_null());
        assert!(active[3]["task_title"].is_null());
        assert_eq!(active[3]["work_owner"]["repository"], "fixture/repo");
        assert!(active[4]["task_title"].is_null());
        assert_eq!(active[4]["work_owner"], json!({}));
        assert_eq!(active[5]["work_owner"]["repository"], "fixture/other");
        assert_eq!(active[5]["work_owner"]["branch"], "kept");
        assert_eq!(active[5]["work_owner"]["pr"], 87);
        let blocker = cc_job_policy::work_owner::blocker(&active[1], &active).unwrap();
        assert_eq!(blocker["id"], active[0]["id"]);
        assert_eq!(
            blocker["id"],
            cc_job_policy::work_owner::blocker(&standalone[1], &standalone).unwrap()["id"]
        );
    }
}

#[test]
fn publication_uses_one_snapshot_and_the_next_call_observes_its_replacement() {
    let f = Fixture::new(false);
    let before = f.db(86, 2);
    let after = f.db(99, 2);
    f.write_db(&before);
    let mut source = jobs(2);
    for job in &mut source {
        job["work_owner"] = Value::Null;
    }
    let store = f.store.clone();
    let replacement = serde_json::to_vec(&after).unwrap();
    let write_result = Arc::new(Mutex::new(None));
    let result = write_result.clone();
    // Replace after the first job has received its title/registration, but
    // before the next job is enriched. Assert the hook's result outside it.
    *f.rt.checkout_hook.lock().unwrap() = Some(Box::new(move || {
        *result.lock().unwrap() = Some(fs::write(&store, &replacement));
    }));
    take_reads();
    let first = f.publish(&json!({}), source.clone());
    assert_eq!(take_reads(), 1);
    write_result.lock().unwrap().take().unwrap().unwrap();
    for job in &first {
        assert_eq!(job["registered_cwd"], before["projects"]["fixture"]);
        assert_eq!(job["work_owner"]["pr"], 86);
    }
    let second = f.publish(&f.index(), source.clone());
    assert_eq!(take_reads(), 1);
    for job in &second {
        assert_eq!(job["registered_cwd"], after["projects"]["fixture"]);
        assert_eq!(job["work_owner"]["pr"], 99);
    }
    let mut single = source[0].clone();
    f.ctx.record_work_owner(&mut single);
    assert_eq!(take_reads(), 1);
    assert_eq!(single, second[0]);
}

#[test]
fn canonical_read_failures_retain_existing_owner_evidence_for_every_job() {
    for failure in ["missing", "invalid", "directory"] {
        let f = Fixture::new(false);
        f.write_db(&f.db(86, 3));
        let mut source = jobs(3);
        source[0]["work_wait_on"] = json!([77]);
        source[0]["work_wait_evidence"] = json!({"owner_todo_id":77});
        source[1]["cancel_requested"] = json!(true);
        source[2]["queue_unreadable"] = json!(true);
        let enriched = f.publish(&json!({}), source);
        let index = f.index();
        fs::remove_file(&f.store).unwrap();
        match failure {
            "invalid" => fs::write(&f.store, b"{broken").unwrap(),
            "directory" => fs::create_dir(&f.store).unwrap(),
            _ => {}
        }
        take_reads();
        let active = f.publish(&index, enriched.clone());
        assert_eq!(take_reads(), 1, "a failed snapshot is not retried per job");
        assert_eq!(active, enriched);
        assert_eq!(f.index(), index);
        let mut standalone = enriched[0].clone();
        f.ctx.record_work_owner(&mut standalone);
        assert_eq!(take_reads(), 1);
        assert_eq!(standalone, enriched[0]);
    }
}

#[test]
fn publication_without_active_jobs_does_not_read_the_canonical_store() {
    let f = Fixture::new(false);
    take_reads();
    let active = f.publish(
        &json!({}),
        vec![json!({"id":"finished","status":"succeeded"})],
    );
    assert_eq!(take_reads(), 0);
    assert!(active.is_empty());
    assert_eq!(f.index()["owners"], json!({}));
    assert_eq!(f.index()["ids"], json!([]));
}
