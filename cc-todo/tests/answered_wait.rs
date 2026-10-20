//! ATDD through the real CLI, lock and durable writer; all records are synthetic.
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    fs,
    path::PathBuf,
    process::{Command, Output},
};

fn sha(b: &[u8]) -> String {
    format!("{:x}", Sha256::digest(b))
}
struct Fixture {
    temp: tempfile::TempDir,
    evidence: PathBuf,
}
impl Fixture {
    fn new(id: i64) -> Self {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path();
        fs::create_dir(root.join("jobs")).unwrap();
        let legacy = "本人の画像付きUI確認への回答待ち。synthetic original hold";
        let old = format!("{id} block cc-pc-tools-rs synthetic — {legacy}\n");
        let old_path = root.join("original.txt");
        fs::write(&old_path, old.as_bytes()).unwrap();
        let identity = json!({"todo_id":3050,"decision":"direction",
            "guild":"1552944707464597524","owner":"730822776147148835",
            "resources":[{"kind":"repository","id":"https://github.com/AutoDevJapan/cc-pc-tools-rs/tree/main/localai-web-rs"}],
            "scope":{"subject":"console-first-view"}});
        let reply = json!({"domain":"new-owner-recovery-messages","read_only":true,"ledger_written":false,
            "source":"discord-api-v10","owner_id":"730822776147148835",
            "request_id":"conversation-v1-5266c54d760e3cf975c7401aaa439b0b",
            "thread_id":"1561951095847260160","question_message_id":"1561951095847260160",
            "question_timestamp":"2026-10-20T03:56:00.898000+00:00",
            "original_question":"synthetic question","decision_identity":identity,
            "owner_replies":[{"request_id":"conversation-v1-5266c54d760e3cf975c7401aaa439b0b",
                "todo_id":3050,"question_thread_id":"1561951095847260160",
                "question_message_id":"1561951095847260160","author_id":"730822776147148835",
                "message_id":"1561951289468911707","timestamp":"2026-10-20T03:56:47.061000+00:00",
                "body":"synthetic owner answer","decision_identity":identity}]});
        let reply_path = root.join("reply.json");
        let reply_bytes = serde_json::to_vec(&reply).unwrap();
        fs::write(&reply_path, &reply_bytes).unwrap();
        let instruction =
            "Synthetic current user task: retain layout; await 3084; never deploy old candidates.";
        let evidence = root.join("evidence.json");
        fs::write(&evidence, serde_json::to_vec(&json!({
            "domain":"todo-answered-dependency-wait-v1","todo_id":id,"wait_on":[3084],
            "expected_note":"synthetic parent delivery: answered; await 3084",
            "legacy_note":{"path":old_path,"sha256":sha(old.as_bytes())},
            "api_reply":{"path":reply_path,"sha256":sha(&reply_bytes)},
            "current_instruction":{"source":"scheduler-user-task:3126","text":instruction,"sha256":sha(instruction.as_bytes()),
                "scope":{"todo_ids":[3050,3070],"dependency_id":3084,"retain_current_layout":true,"deploy_old_candidates":false}}
        })).unwrap()).unwrap();
        let db = json!({"projects":{"cc-pc-tools-rs":root.to_str().unwrap()},"items":[
            {"id":id,"project":"cc-pc-tools-rs","title":"synthetic Console","status":"block",
             "note":"synthetic parent delivery: answered; await 3084","updated":"2026-10-20 23:59",
             "note_history":[{"note":legacy,"updated":"2026-10-20 12:00"}],
             "jobs":[],"progress":40,"spec":"original-spec.md","dispatchable":true,"needs":["cargo"],
             "wait_on":[3084],"resume_generation":3,"worker_receipt":{"generation":3,"job":null},
             "scheduler":{"phase":"finished","worktree":"original-worktree","branch":"original-branch","not_before":0},
             "stall_recovery":{"history":[1,2],"next_at":0}},
            {"id":3084,"project":"cc-pc-tools-rs","status":"doing","note":"unfinished work","jobs":[]},
            {"id":9999,"project":"owner","status":"block","note":"unrelated private hold","opaque_protection":true}
        ],"next_id":10000,"original_registry_recovered":false,"unknown_root_protection":{"keep":true}});
        fs::write(root.join("todo.json"), serde_json::to_vec(&db).unwrap()).unwrap();
        fs::write(
            root.join("original-spec.md"),
            b"unfinished original specification",
        )
        .unwrap();
        Self { temp, evidence }
    }
    fn raw(&self) -> Vec<u8> {
        fs::read(self.temp.path().join("todo.json")).unwrap()
    }
    fn db(&self) -> Value {
        serde_json::from_slice(&self.raw()).unwrap()
    }
    fn put(&self, v: &Value) {
        fs::write(
            self.temp.path().join("todo.json"),
            serde_json::to_vec(v).unwrap(),
        )
        .unwrap()
    }
    fn change_evidence(&self, edit: impl FnOnce(&mut Value)) {
        let mut v: Value = serde_json::from_slice(&fs::read(&self.evidence).unwrap()).unwrap();
        edit(&mut v);
        fs::write(&self.evidence, serde_json::to_vec(&v).unwrap()).unwrap();
    }
    fn call(&self, id: i64, args: &[&str]) -> Output {
        let mut c = Command::new(env!("CARGO_BIN_EXE_todo"));
        c.args([
            "answered-wait",
            &id.to_string(),
            "--evidence-file",
            self.evidence.to_str().unwrap(),
        ])
        .args(args)
        .env("CC_TODO_HOME", self.temp.path())
        .env("CC_TODO_JOBS", self.temp.path().join("jobs"));
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            c.creation_flags(0x08000000);
        }
        c.output().unwrap()
    }
    fn ok(&self, id: i64, args: &[&str]) -> Value {
        let out = self.call(id, args);
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        assert!(out.stderr.is_empty());
        let v = serde_json::from_slice(&out.stdout).unwrap();
        let s = String::from_utf8(out.stdout).unwrap();
        assert!(!s.contains("synthetic owner answer"));
        v
    }
    fn reject(&self, id: i64, token: &str) {
        let before = self.raw();
        let out = self.call(id, &["--apply", "--expected-state-sha256", token]);
        assert!(
            !out.status.success(),
            "unexpected success: {}",
            String::from_utf8_lossy(&out.stdout)
        );
        assert_eq!(before, self.raw());
    }
}

#[test]
fn answered_wait_moves_only_verified_manual_hold_to_existing_dependency() {
    for id in [3050, 3070] {
        let f = Fixture::new(id);
        let original = f.db();
        let raw = f.raw();
        let plan = f.ok(id, &[]);
        assert_eq!(raw, f.raw());
        assert_eq!(plan["changed"], false);
        let token = plan["state_sha256"].as_str().unwrap();
        let result = f.ok(id, &["--apply", "--expected-state-sha256", token]);
        assert_eq!(result["changed"], true);
        let after = f.db();
        assert_eq!(after["items"][0]["status"], "wait");
        let receipt = &after["items"][0]["answered_dependency_wait_receipt"];
        assert_eq!(receipt["original_status"], "block");
        assert_eq!(receipt["original_wait_on"], json!([3084]));
        assert_eq!(
            receipt["api_reply"]["owner_replies"][0]["body"],
            "synthetic owner answer"
        );
        let mut normalized = after.clone();
        normalized["items"][0]["status"] = original["items"][0]["status"].clone();
        normalized["items"][0]["updated"] = original["items"][0]["updated"].clone();
        normalized["items"][0]
            .as_object_mut()
            .unwrap()
            .remove("answered_dependency_wait_receipt");
        assert_eq!(normalized, original);
        let raw = f.raw();
        assert_eq!(
            f.ok(id, &["--apply", "--expected-state-sha256", token])["changed"],
            false
        );
        assert_eq!(raw, f.raw());
        let mut stopped = f.db();
        stopped["items"][0]["status"] = json!("block");
        f.put(&stopped);
        f.reject(id, token); // never release a later stop on replay
    }
}

#[test]
fn answered_wait_rejects_state_note_dependency_and_unknown_hold_races_without_writing() {
    for field in [
        "status",
        "note",
        "wait_on",
        "owner_question_wait",
        "wait_ready",
        "stopped",
        "unknown_protection",
        "scheduler",
    ] {
        let f = Fixture::new(3050);
        let plan = f.ok(3050, &[]);
        let mut d = f.db();
        d["items"][0][field] = match field {
            "status" => json!("done"),
            "note" => json!("another stop"),
            "wait_on" => json!([9999]),
            "scheduler" => json!({"job":"another-worker","phase":"running"}),
            _ => json!(true),
        };
        f.put(&d);
        f.reject(3050, plan["state_sha256"].as_str().unwrap());
        let raw = f.raw();
        let out = f.call(3050, &[]);
        assert!(!out.status.success());
        assert_eq!(raw, f.raw());
    }
}

#[test]
fn answered_wait_rejects_other_identity_generation_missing_history_or_instruction() {
    for case in [
        "author",
        "generation",
        "todo",
        "source",
        "history",
        "directive",
        "bytes",
    ] {
        let f = Fixture::new(3050);
        let plan = f.ok(3050, &[]);
        if case == "history" {
            let mut d = f.db();
            d["items"][0]["note_history"] = json!([]);
            f.put(&d);
        } else if case == "directive" {
            f.change_evidence(|v| v["current_instruction"]["source"] = json!("unverified"));
        } else {
            let p = f.temp.path().join("reply.json");
            let mut v: Value = serde_json::from_slice(&fs::read(&p).unwrap()).unwrap();
            match case {
                "author" => v["owner_replies"][0]["author_id"] = json!("999"),
                "generation" => v["owner_replies"][0]["request_id"] = json!("later-question"),
                "todo" => v["owner_replies"][0]["todo_id"] = json!(3070),
                "source" => v["source"] = json!("page-quote"),
                _ => v["owner_replies"][0]["body"] = json!("edited"),
            };
            let b = serde_json::to_vec(&v).unwrap();
            fs::write(p, &b).unwrap();
            if case != "bytes" {
                f.change_evidence(|v| v["api_reply"]["sha256"] = json!(sha(&b)));
            }
        }
        f.reject(3050, plan["state_sha256"].as_str().unwrap());
    }
}

#[test]
fn answered_wait_does_not_replace_normal_wait_or_start_semantics() {
    let f = Fixture::new(3050);
    let paths = cc_todo::Paths::at(f.temp.path(), &f.temp.path().join("jobs"));
    let mut d = f.db();
    cc_todo::execute(
        &mut d,
        &cc_todo::Command::Wait {
            id: 3050,
            on: vec![3084],
            progress: None,
        },
        &paths,
    )
    .unwrap();
    assert_eq!(d["items"][0]["status"], "block");
    let before = d.clone();
    assert!(
        cc_todo::execute(
            &mut d,
            &cc_todo::Command::Start {
                id: 3050,
                job: String::new(),
                progress: None
            },
            &paths
        )
        .is_err()
    );
    assert_eq!(d, before);
    let plan = f.ok(3050, &[]);
    f.ok(
        3050,
        &[
            "--apply",
            "--expected-state-sha256",
            plan["state_sha256"].as_str().unwrap(),
        ],
    );
    let mut d = f.db();
    let before = d.clone();
    assert!(
        cc_todo::execute(
            &mut d,
            &cc_todo::Command::Start {
                id: 3050,
                job: String::new(),
                progress: None
            },
            &paths
        )
        .is_err()
    );
    assert_eq!(d, before);
    cc_todo::execute(
        &mut d,
        &cc_todo::Command::Done {
            id: 3084,
            note: String::new(),
        },
        &paths,
    )
    .unwrap();
    assert_eq!(d["items"][0]["status"], "todo");
    assert_eq!(d["items"][2]["status"], "block");
}
