//! Cross-language acceptance endpoint, in the test binary only. All workers and
//! external services remain Fake; Task transitions and reply CLI are real.
use super::*;

#[test]
#[ignore = "invoked by the isolated Go Task-question integration fixture"]
fn native_question_fixture() {
    let path = std::env::var("CC_TASK_QUESTION_FIXTURE").expect("owned fixture config");
    let cfg = read_json(Path::new(&path), Value::Null).unwrap();
    assert_eq!(cfg["domain"], "task-question-integration-fixture-v1");
    let store = PathBuf::from(s(&cfg["store"]));
    assert_ne!(
        norm(&store.to_string_lossy()),
        norm(&cc_todo::Paths::production().store.to_string_lossy())
    );
    let mut f = Fixture::new();
    f.e.todo_path = store.clone();
    f.e.root = store.parent().unwrap().join("offline-scheduler");
    fs::create_dir_all(&f.e.root).unwrap();
    fs::write(store.parent().unwrap().join("client-projects.txt"), "").unwrap();
    let db = f.e.todo().unwrap();
    let row = arr(&db["items"])
        .into_iter()
        .find(|r| r["id"] == 42)
        .unwrap();
    assert_eq!(row["title"], "Maintain requested repositories");
    f.repo = PathBuf::from(s(&db["projects"][s(&row["project"])]));
    f.e.backend.clock = now().max(n(&row["scheduler"]["not_before"]) + 1.);
    f.e.backend.question_store = Some(store.clone());
    let mode = s(&cfg["mode"]);
    if mode == "input_requeue" {
        let id = s(&row["scheduler"]["job"]).to_owned();
        let path = PathBuf::from(s(&cfg["jobs"])).join(&id).join("job.json");
        let mut job = read_json(&path, Value::Null).unwrap();
        job["prepared_input"] = row["scheduler"]["prepared_input"].clone();
        assert_eq!(
            cc_job_policy::prepared_input::validate(&db, &job),
            Err("prepared_input_changed")
        );
        job["status"] = json!("failed");
        job["started_at"] = Value::Null;
        job["last_failure_kind"] = json!("stale_prepared_input");
        write_json(&path, &job, 1).unwrap();
        let mut records = vec![job];
        f.e.reconcile(&mut records, &Processes::new(), &f.cfg)
            .unwrap();
    } else if mode.starts_with("edit_") {
        let ready = PathBuf::from(s(&cfg["ready"]));
        let release = PathBuf::from(s(&cfg["release"]));
        let action = Box::new(move || {
            fs::write(&ready, b"ready").unwrap();
            let start = std::time::Instant::now();
            while !release.exists() {
                assert!(
                    start.elapsed() < Duration::from_secs(25),
                    "bounded fixture handshake expired"
                );
                std::thread::sleep(Duration::from_millis(10));
            }
        });
        if mode == "edit_measure" {
            f.e.backend.question_measure = Some(action);
        } else {
            f.e.backend.question_launch = Some(action);
        }
        let _ = f.e.dispatch(&f.cfg, &f.pri);
    } else if mode == "dispatch" {
        f.dispatch();
    } else if mode == "capacity_defer" {
        f.e.backend.prepare_pressure = true;
        f.dispatch();
        assert!(f.e.backend.launched.is_empty());
        assert_eq!(f.get(42)["scheduler"]["phase"], "prepared");
    } else {
        let p =
            f.e.prepare(&row, f.repo.to_str().unwrap())
                .unwrap()
                .unwrap();
        if mode == "linked_start" {
            let job = json!({"id":"regular-scheduler-successor","todo_id":42,"status":"running",
                "project":row["project"],"created_at":chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs,true),"launch_key":p["scheduler"]["key"]});
            let jobs = PathBuf::from(s(&cfg["jobs"]));
            fs::create_dir_all(jobs.join("regular-scheduler-successor")).unwrap();
            write_json(&jobs.join("regular-scheduler-successor/job.json"), &job, 1).unwrap();
            f.e.link(&json!(42), &job, &p["scheduler"]["key"]).unwrap();
            let mut command =
                std::process::Command::new(std::env::var("CC_CONVERSATION_TODO_BINARY").unwrap());
            command
                .args(["start", "42", "--job", "regular-scheduler-successor"])
                .env("CC_TODO_HOME", store.parent().unwrap())
                .env("CC_TODO_JOBS", &jobs);
            use std::os::windows::process::CommandExt;
            let out = command.creation_flags(0x08000000).output().unwrap();
            assert!(out.status.success(), "canonical same-job start failed");
        }
    }
    write_json(
        Path::new(s(&cfg["output"])),
        &json!({"launched":f.e.backend.launched,"task":f.get(42)}),
        1,
    )
    .unwrap();
}
