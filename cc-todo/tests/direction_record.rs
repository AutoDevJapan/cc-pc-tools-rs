//! The real CLI must atomically append a direction without releasing product work.
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    fs,
    process::{Command, Output},
};

fn sha(b: &[u8]) -> String {
    format!("{:x}", Sha256::digest(b))
}
struct Fixture {
    temp: tempfile::TempDir,
}
impl Fixture {
    fn new() -> Self {
        let f = Self {
            temp: tempfile::tempdir().unwrap(),
        };
        let root = f.temp.path().to_str().unwrap();
        fs::create_dir_all(f.temp.path().join("jobs/actual-worker")).unwrap();
        let scope = "normal-question 123 124";
        fs::write(f.temp.path().join("actor.md"), scope).unwrap();
        fs::write(f.temp.path().join("jobs/actual-worker/prompt.md"), scope).unwrap();
        f.write(
            "jobs/actual-worker/job.json",
            &json!({"id":"actual-worker", "todo_id":2,
            "cwd":root, "status":"running"}),
        );
        f.write(
            "todo.json",
            &json!({"projects":{"tools":root}, "items":[
            {"id":1,"project":"tools","status":"wait","note":"unfinished product",
             "wait_on":[3],"progress":30,"spec":"original.md","jobs":["product-worker"]},
            {"id":2,"project":"tools","status":"doing","note":"record only",
             "spec":f.temp.path().join("actor.md"),"jobs":["actual-worker"],
             "scheduler":{"job":"actual-worker","worktree":root},"resume_generation":1},
            {"id":3,"project":"tools","status":"todo"}]}),
        );
        f.write("input.json", &json!({"domain":"discord-conversation-direction-v1",
            "kind":"specification_direction","source":"discord-api-v10","request_id":"normal-question",
            "sender_request_id":"sender-question","origin_todo_id":"1","actor_todo_id":"2",
            "job_id":"actual-worker","project":"tools","revision":"a".repeat(40),
            "thread_id":"123","question_message_id":"123","message_id":"124","author_id":"456",
            "question_fingerprint":"b".repeat(64),"question_receipt_sha256":"c".repeat(64),
            "decision_identity_sha256":"d".repeat(64),"reply_sha256":"e".repeat(64),
            "source_receipt_sha256":"f".repeat(64),"original_question":"a=product responsibility, b=resident workers",
            "question_created_at":"2026-10-20T00:00:00Z","owner_reply":"a",
            "created_at":"2026-10-20T00:01:00Z","edited_at":"","acquired_at":"2026-10-20T00:02:00Z"}));
        f.snapshot();
        f
    }
    fn write(&self, name: &str, v: &Value) {
        fs::write(self.temp.path().join(name), serde_json::to_vec(v).unwrap()).unwrap();
    }
    fn db(&self) -> Value {
        serde_json::from_slice(&fs::read(self.temp.path().join("todo.json")).unwrap()).unwrap()
    }
    fn snapshot(&self) {
        let db = self.db();
        for (i, name) in [(0, "origin"), (1, "actor")] {
            self.write(&format!("{name}-expected.json"), &db["items"][i]);
            self.write(
                &format!("{name}-scope.json"),
                &json!({"domain":"discord-conversation-todo-scope-v1",
                "todo_id":(i+1).to_string(),"project":"tools",
                "project_path_sha256":sha(self.temp.path().to_str().unwrap().as_bytes())}),
            );
        }
    }
    fn call(&self, verify: bool) -> Output {
        let mut c = Command::new(env!("CARGO_BIN_EXE_todo"));
        c.arg("direction-record")
            .arg("--input-file")
            .arg(self.temp.path().join("input.json"));
        for name in ["origin", "actor"] {
            c.arg(format!("--{name}-scope-file"))
                .arg(self.temp.path().join(format!("{name}-scope.json")));
            c.arg(format!("--{name}-state-file"))
                .arg(self.temp.path().join(format!("{name}-expected.json")));
        }
        if verify {
            c.arg("--verify-only");
        }
        c.env("CC_TODO_HOME", self.temp.path())
            .env("CC_TODO_JOBS", self.temp.path().join("jobs"))
            .env("CC_JOB_ID", "actual-worker");
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            c.creation_flags(0x08000000);
        }
        c.output().unwrap()
    }
    fn ok(&self, verify: bool) -> Value {
        let out = self.call(verify);
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        serde_json::from_slice(&out.stdout).unwrap()
    }
    fn reject_unchanged(&self, verify: bool) {
        let before = fs::read(self.temp.path().join("todo.json")).unwrap();
        let out = self.call(verify);
        assert!(
            !out.status.success(),
            "unexpected success: {}",
            String::from_utf8_lossy(&out.stdout)
        );
        assert_eq!(
            before,
            fs::read(self.temp.path().join("todo.json")).unwrap()
        );
    }
}

#[test]
fn direction_records_actual_actor_and_specification_without_completing_product() {
    let f = Fixture::new();
    let before = f.db();
    f.reject_unchanged(true); // Delivery or an input file alone is not application.
    let saved = f.ok(false);
    assert_eq!(saved["kind"], "specification_direction");
    assert_eq!(saved["changed"], true);
    let after = f.db();
    for key in ["status", "wait_on", "jobs", "spec", "progress"] {
        assert_eq!(before["items"][0][key], after["items"][0][key]);
    }
    assert_eq!(before["items"][1], after["items"][1]);
    assert_eq!(before["items"][2], after["items"][2]);
    let note = after["items"][0]["note"].as_str().unwrap();
    assert!(note.starts_with("unfinished product"));
    assert!(note.contains("a=product responsibility, b=resident workers"));
    assert!(note.contains("仕様へ記録、機能は未完了"));
    assert_eq!(
        after["items"][0]["owner_direction_records"][0]["input"]["job_id"],
        "actual-worker"
    );
    f.snapshot();
    assert_eq!(f.ok(true)["receipt_sha256"], saved["receipt_sha256"]);
    let raw = fs::read(f.temp.path().join("todo.json")).unwrap();
    assert_eq!(f.ok(false)["changed"], false);
    assert_eq!(raw, fs::read(f.temp.path().join("todo.json")).unwrap());
}

#[test]
fn direction_rejects_changed_scope_snapshot_and_new_stops_even_with_same_note() {
    for change in [
        "project",
        "root",
        "block",
        "dependency",
        "actor-job",
        "actor-project",
    ] {
        let f = Fixture::new();
        let mut db = f.db();
        match change {
            "project" => {
                db["projects"]["other"] = db["projects"]["tools"].clone();
                db["items"][0]["project"] = json!("other");
            }
            "root" => db["projects"]["tools"] = json!("D:/different"),
            "block" => db["items"][0]["owner_stopped"] = json!(true),
            "dependency" => db["items"][0]["wait_on"] = json!([3, 4]),
            "actor-job" => db["items"][1]["scheduler"]["job"] = json!("other"),
            _ => db["items"][1]["project"] = json!("other"),
        }
        f.write("todo.json", &db);
        f.reject_unchanged(false);
    }
}

#[test]
fn direction_rejects_foreign_jobs_missing_assignment_and_functional_promotion() {
    for change in [
        "job",
        "foreign-todo",
        "prompt",
        "spec",
        "kind",
        "source",
        "missing-source",
        "unknown-field",
    ] {
        let f = Fixture::new();
        let mut input: Value =
            serde_json::from_slice(&fs::read(f.temp.path().join("input.json")).unwrap()).unwrap();
        match change {
            "job" => input["job_id"] = json!("product-worker"),
            "foreign-todo" => {
                let mut j: Value = serde_json::from_slice(
                    &fs::read(f.temp.path().join("jobs/actual-worker/job.json")).unwrap(),
                )
                .unwrap();
                j["todo_id"] = json!(9);
                f.write("jobs/actual-worker/job.json", &j);
            }
            "prompt" => fs::write(
                f.temp.path().join("jobs/actual-worker/prompt.md"),
                "unrelated task",
            )
            .unwrap(),
            "spec" => fs::write(f.temp.path().join("actor.md"), "unrelated task").unwrap(),
            "kind" => input["kind"] = json!("implementation"),
            "source" => input["source"] = json!("main-handoff"),
            "missing-source" => input["source_receipt_sha256"] = json!(""),
            _ => input["image_approved"] = json!(true),
        }
        f.write("input.json", &input);
        f.reject_unchanged(false);
    }
}

#[test]
fn direction_verification_rechecks_actual_note_and_preserves_new_control_state() {
    for change in ["note", "block", "dependency", "project", "record"] {
        let f = Fixture::new();
        f.ok(false);
        let mut db = f.db();
        match change {
            "note" => db["items"][0]["note"] = json!("only a delivery reference"),
            "block" => db["items"][0]["owner_stopped"] = json!(true),
            "dependency" => db["items"][0]["wait_on"] = json!([9]),
            "project" => db["items"][0]["project"] = json!("other"),
            _ => db["items"][0]["owner_direction_records"] = json!([]),
        }
        f.write("todo.json", &db);
        f.snapshot();
        f.reject_unchanged(true);
    }
}

#[test]
fn direction_assignment_accepts_explicit_q_reply_labels_but_not_numeric_substrings() {
    let f = Fixture::new();
    for name in ["actor.md", "jobs/actual-worker/prompt.md"] {
        fs::write(
            f.temp.path().join(name),
            "request normal-question: q123→本人reply124",
        )
        .unwrap();
    }
    f.ok(false);
    let f = Fixture::new();
    fs::write(
        f.temp.path().join("actor.md"),
        "normal-question q1239 reply1249",
    )
    .unwrap();
    f.reject_unchanged(false);
}

#[test]
fn direction_rejects_explicit_actor_stops_even_when_status_is_doing() {
    for (key, value) in [
        ("owner_stopped", json!(true)),
        ("paused", json!(true)),
        ("stop_kind", json!("manual")),
    ] {
        let f = Fixture::new();
        let mut db = f.db();
        db["items"][1][key] = value;
        f.write("todo.json", &db);
        f.snapshot();
        f.reject_unchanged(false);
    }
}

#[test]
fn recorded_direction_survives_normal_work_transitions_and_original_actor_retirement() {
    for change in [
        "dependency-completed",
        "new-worker",
        "origin-spec",
        "actor-spec",
        "actor-retired",
    ] {
        let f = Fixture::new();
        let saved = f.ok(false);
        let mut db = f.db();
        match change {
            "dependency-completed" => {
                db["items"][2]["status"] = json!("done");
                db["items"][0]["status"] = json!("todo");
                db["items"][0].as_object_mut().unwrap().remove("wait_on");
            }
            "new-worker" => {
                db["items"][0]["status"] = json!("doing");
                db["items"][0]["scheduler"] =
                    json!({"job":"next-product-worker","worktree":"D:/next-worktree"});
                db["items"][0]["jobs"] = json!(["product-worker", "next-product-worker"]);
                db["items"][0]["resume_generation"] = json!(2);
            }
            "origin-spec" => db["items"][0]["spec"] = json!("next-version.md"),
            "actor-spec" => {
                fs::write(f.temp.path().join("next-actor.md"), "next assignment").unwrap();
                db["items"][1]["spec"] = json!(f.temp.path().join("next-actor.md"));
            }
            _ => {
                db["items"][1]["status"] = json!("done");
                db["items"][1]["scheduler"] = json!({"job":null});
                let job_path = f.temp.path().join("jobs/actual-worker/job.json");
                let mut j: Value = serde_json::from_slice(&fs::read(job_path).unwrap()).unwrap();
                j["status"] = json!("cancelled");
                j["cancel_reason"] = json!("completed-parent");
                f.write("jobs/actual-worker/job.json", &j);
            }
        }
        f.write("todo.json", &db);
        f.snapshot();
        assert_eq!(
            f.ok(true)["receipt_sha256"],
            saved["receipt_sha256"],
            "{change}"
        );
        assert_eq!(f.ok(false)["changed"], false, "{change}");
        assert_eq!(f.db(), db, "readback mutated normal work state");
    }
}

#[test]
fn recorded_scope_and_prompt_remain_bound_after_normal_snapshot_refresh() {
    for change in ["root", "prompt", "job-cwd", "job-todo"] {
        let f = Fixture::new();
        f.ok(false);
        let mut db = f.db();
        match change {
            "root" => db["projects"]["tools"] = json!("D:/rebound-root"),
            "prompt" => fs::write(
                f.temp.path().join("jobs/actual-worker/prompt.md"),
                "normal-question 123 124 changed",
            )
            .unwrap(),
            _ => {
                let mut j: Value = serde_json::from_slice(
                    &fs::read(f.temp.path().join("jobs/actual-worker/job.json")).unwrap(),
                )
                .unwrap();
                if change == "job-cwd" {
                    j["cwd"] = json!("D:/rebound-job")
                } else {
                    j["todo_id"] = json!(3)
                }
                f.write("jobs/actual-worker/job.json", &j);
            }
        }
        f.write("todo.json", &db);
        f.snapshot();
        if change == "root" {
            for (i, name) in [(0, "origin"), (1, "actor")] {
                f.write(&format!("{name}-scope.json"), &json!({"domain":"discord-conversation-todo-scope-v1",
                    "todo_id":(i+1).to_string(),"project":"tools","project_path_sha256":sha(b"D:/rebound-root")}));
            }
        }
        f.reject_unchanged(true);
    }
}

#[test]
fn new_hold_defers_close_but_releasing_it_recovers_the_same_record() {
    for change in ["block", "stop", "dependency"] {
        let f = Fixture::new();
        let saved = f.ok(false);
        let recorded = f.db();
        let mut db = recorded.clone();
        match change {
            "block" => db["items"][0]["status"] = json!("block"),
            "stop" => db["items"][0]["owner_stopped"] = json!(true),
            _ => db["items"][0]["wait_on"] = json!([3, 9]),
        }
        f.write("todo.json", &db);
        f.snapshot();
        f.reject_unchanged(true);
        f.write("todo.json", &recorded);
        f.snapshot();
        assert_eq!(f.ok(true)["receipt_sha256"], saved["receipt_sha256"]);
    }
}

#[test]
fn direction_preserves_raw_input_and_normalizes_only_the_note_display() {
    let f = Fixture::new();
    let mut input: Value =
        serde_json::from_slice(&fs::read(f.temp.path().join("input.json")).unwrap()).unwrap();
    input["owner_reply"] = json!("a\r\nkeep\rstructure");
    f.write("input.json", &input);
    f.ok(false);
    let db = f.db();
    assert!(
        db["items"][0]["note"]
            .as_str()
            .unwrap()
            .contains("a\nkeep\nstructure")
    );
    assert_eq!(
        db["items"][0]["owner_direction_records"][0]["input"]["owner_reply"],
        input["owner_reply"]
    );
    f.snapshot();
    f.ok(true);
}

#[test]
fn direction_rejects_the_original_product_todo_as_its_distinct_actor() {
    let f = Fixture::new();
    let mut input: Value =
        serde_json::from_slice(&fs::read(f.temp.path().join("input.json")).unwrap()).unwrap();
    input["actor_todo_id"] = json!("1");
    f.write("input.json", &input);
    f.reject_unchanged(false);
}
