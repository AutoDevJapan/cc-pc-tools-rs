//! ATDD through the real CLI: a verified owner answer reaches the next prompt in full
//! and only the proven asking launch may return to its own conversation session.
use cc_todo::Paths;
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{fs, path::PathBuf, process::Command};

const PRIOR: &str = "最新の直接指示を優先する";

fn sha(data: &[u8]) -> String {
    format!("{:x}", Sha256::digest(data))
}

struct Fixture {
    temp: tempfile::TempDir,
    paths: Paths,
}

impl Fixture {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let paths = Paths::at(temp.path(), &temp.path().join("jobs"));
        fs::create_dir(temp.path().join("jobs")).unwrap();
        fs::write(
            temp.path().join("notify-config.json"),
            br#"{"guild_id":"100","owner_user_id":"101","application_id":"102"}"#,
        )
        .unwrap();
        fs::write(
            &paths.store,
            serde_json::to_vec(&json!({
                "projects":{"cc-pc-tools-rs":temp.path().to_str().unwrap()}, "next_id":3,
                "original_registry_recovered":false,
                "items":[{
                    "id":1,"project":"cc-pc-tools-rs","title":"質問に回答する","status":"doing",
                    "note":"元のメモ", "updated":"2026-10-13 10:00", "progress":40,
                    "spec":"specs/original.md", "dispatchable":true, "jobs":["original-worker"],
                    "scheduler":{"job":"original-worker","key":"original-key","phase":"finished",
                        "worktree":"saved-worktree","branch":"saved-branch","not_before":0,
                        "owner_instruction":PRIOR},
                    "worker_receipt":{"job":"original-worker"}, "resume_generation":7
                }, {"id":2,"project":"cc-pc-tools-rs","status":"todo","note":"他の作業","jobs":[]}]
            }))
            .unwrap(),
        )
        .unwrap();
        Self { temp, paths }
    }
    fn read(&self) -> Value {
        cc_todo::read_store(&self.paths.store).unwrap()
    }
    fn write(&self, value: &Value) {
        fs::write(&self.paths.store, serde_json::to_vec(value).unwrap()).unwrap();
    }
    fn file(&self, name: &str, value: &Value) -> PathBuf {
        let path = self.temp.path().join(name);
        fs::write(&path, serde_json::to_vec(value).unwrap()).unwrap();
        path
    }
    fn job(&self, record: &Value) {
        let dir = self.paths.jobs.join("original-worker");
        fs::create_dir_all(&dir).unwrap();
        fs::write(dir.join("job.json"), serde_json::to_vec(record).unwrap()).unwrap();
    }
    fn run(&self, command: &str, flag: &str, path: &std::path::Path) -> Value {
        let mut cmd = Command::new(env!("CARGO_BIN_EXE_todo"));
        cmd.args([command, "1", flag, path.to_str().unwrap()])
            .env("CC_TODO_HOME", self.temp.path())
            .env("CC_ASK_CONFIG", self.temp.path().join("notify-config.json"))
            .env("CC_TODO_JOBS", &self.paths.jobs);
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            cmd.creation_flags(0x08000000);
        }
        let out = cmd.output().unwrap();
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        serde_json::from_slice(&out.stdout).unwrap()
    }
    /// Park the question, then add the owner's private reply text to the note the
    /// way the delivery layer does. Returns the answer file and the reply text.
    fn park(&self, reply: &str) -> PathBuf {
        let question = json!({
            "domain":"discord-conversation-question-v1",
            "request_id":"conversation-v1-0123456789abcdef0123456789abcdef",
            "todo_id":"1", "guild_id":"100", "owner_id":"101", "bot_id":"102",
            "thread_id":"200", "question_message_id":"200", "progress_thread_id":"201",
            "question_fingerprint":"a".repeat(64), "created_at":"2026-10-13T01:00:00Z"
        });
        let q_file = self.file("question.json", &question);
        let q_sha = sha(&fs::read(&q_file).unwrap());
        self.run("question-wait", "--question-file", &q_file);
        let mut db = self.read();
        let note = db["items"][0]["note"].as_str().unwrap().to_owned();
        db["items"][0]["note"] = json!(format!("{note}\nconversation-owner-reply:synthetic\n{reply}"));
        self.write(&db);
        self.file(
            "answer.json",
            &json!({
                "domain":"discord-conversation-answer-v1", "request_id":question["request_id"],
                "todo_id":question["todo_id"], "thread_id":question["thread_id"],
                "question_message_id":question["question_message_id"],
                "question_fingerprint":question["question_fingerprint"],
                "question_receipt_sha256":q_sha, "message_id":"202", "author_id":"101",
                "source":"discord-api-v10", "reply_sha256":"b".repeat(64),
                "acquired_at":"2026-10-13T01:00:01Z", "receipt_file_sha256":"c".repeat(64)
            }),
        )
    }
}

fn long_reply() -> String {
    // Far beyond the 8000 character cap applied to the previous result.
    format!("先頭の回答。{}末尾の回答。", "長い回答の本文。".repeat(10_000))
}

fn asked_job(extra: Value) -> Value {
    let mut job = json!({"id":"original-worker","status":"succeeded","todo_id":1,
        "launch_key":"original-key","cwd":"saved-worktree","created_at":"2026-10-13T00:59:00Z"});
    for (key, value) in extra.as_object().unwrap() {
        job[key] = value.clone();
    }
    job
}

#[test]
fn full_reply_and_proof_reach_the_next_launch_of_the_asking_conversation() {
    let f = Fixture::new();
    let reply = long_reply();
    let answer = f.park(&reply);
    f.job(&asked_job(json!({})));
    assert_eq!(f.run("question-answer", "--answer-file", &answer)["resumed"], true);
    let row = f.read()["items"][0].clone();
    assert_eq!(row["status"], "todo");
    let instruction = row["scheduler"]["owner_instruction"].as_str().unwrap();
    assert!(instruction.starts_with(&format!("{PRIOR}\n")), "prior direction was overwritten");
    assert!(instruction.contains(&reply), "the owner reply was cut or altered");
    assert!(instruction.chars().count() > 40_000);
    assert_eq!(row["scheduler"]["last_job"], "original-worker");
    assert_eq!(row["scheduler"]["resume"], true);
    let proof = &row["scheduler"]["conversation_answer"];
    assert_eq!(proof["job"], "original-worker");
    assert_eq!(proof["launch_key"], "original-key");
    assert_eq!(proof["request_id"], "conversation-v1-0123456789abcdef0123456789abcdef");
    assert_eq!(proof["message_id"], "202");
    assert_eq!(proof["question_receipt_sha256"].as_str().unwrap().len(), 64);
}

#[test]
fn only_the_verified_asking_launch_gets_a_session_proof() {
    for (case, extra) in [
        ("job created after the question", json!({"created_at":"2026-10-13T01:00:05Z"})),
        ("another launch key", json!({"launch_key":"other-key"})),
        ("failed owner", json!({"status":"failed"})),
    ] {
        let f = Fixture::new();
        let answer = f.park("回答");
        f.job(&asked_job(extra));
        f.run("question-answer", "--answer-file", &answer);
        let row = f.read()["items"][0].clone();
        // The answer is still delivered to the retired owner's next launch.
        assert!(row["scheduler"]["owner_instruction"].as_str().unwrap().contains("回答"), "{case}");
        assert_eq!(row["scheduler"]["last_job"], "original-worker", "{case}");
        assert!(row["scheduler"]["conversation_answer"].is_null(), "{case} got a proof");
    }
}

#[test]
fn a_live_owner_is_never_claimed_and_keeps_note_only_delivery() {
    let f = Fixture::new();
    let answer = f.park("回答");
    f.job(&json!({"id":"original-worker","status":"running"}));
    let before = f.read()["items"][0]["scheduler"].clone();
    f.run("question-answer", "--answer-file", &answer);
    let row = f.read()["items"][0].clone();
    assert_eq!(row["scheduler"], before);
    assert_eq!(row["resume_generation"], 7);
    assert!(row["note"].as_str().unwrap().contains("回答"));
}

#[test]
fn a_todo_without_any_worker_stores_the_reply_for_its_first_launch() {
    let f = Fixture::new();
    let mut db = f.read();
    db["items"][0]["scheduler"]["job"] = Value::Null;
    db["items"][0]["scheduler"]["key"] = Value::Null;
    db["items"][0]["worker_receipt"]["job"] = Value::Null;
    f.write(&db);
    let reply = long_reply();
    let answer = f.park(&reply);
    f.run("question-answer", "--answer-file", &answer);
    let row = f.read()["items"][0].clone();
    assert_eq!(row["status"], "todo");
    let instruction = row["scheduler"]["owner_instruction"].as_str().unwrap();
    assert!(instruction.starts_with(&format!("{PRIOR}\n")));
    assert!(instruction.contains(&reply));
    assert_eq!(row["resume_generation"], 7, "no worker may be advanced or retired");
    assert!(row["scheduler"]["last_job"].is_null());
    assert!(row["scheduler"]["conversation_answer"].is_null());
    assert!(row["processed_results"].is_null());
}
