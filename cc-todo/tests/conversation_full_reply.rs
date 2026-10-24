//! ATDD through the real CLI: a verified owner answer reaches the next prompt in full
//! and only the proven asking launch may return to its own conversation session.
use cc_todo::Paths;
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    fs,
    path::{Path, PathBuf},
    process::{Command, Output},
};

const PRIOR: &str = "最新の直接指示を優先する";
const DELIVERIES: &str = "owner_question_delivery_receipts";
const V1: &str = "0123456789abcdef";
const V2: &str = "fedcba9876543210";

/// The versioned block the Go bridge writes for one owner reply.
fn block(message: &str, version: &str, body: &str) -> String {
    format!(
        "conversation-owner-reply:900:{message}:{version}\nsource=discord message={message}\n{body}\nconversation-owner-reply-end:900:{message}:{version}"
    )
}

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
    fn command(&self) -> Command {
        let mut cmd = Command::new(env!("CARGO_BIN_EXE_todo"));
        cmd.env("CC_TODO_HOME", self.temp.path())
            .env("CC_ASK_CONFIG", self.temp.path().join("notify-config.json"))
            .env("CC_TODO_JOBS", &self.paths.jobs);
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            cmd.creation_flags(0x08000000);
        }
        cmd
    }
    /// The same question's reply after it was applied: a new or edited message.
    fn later_answer(&self, answer: &Path, edit: impl FnOnce(&mut Value)) -> PathBuf {
        let mut value: Value = serde_json::from_slice(&fs::read(answer).unwrap()).unwrap();
        value["reply_sha256"] = json!("d".repeat(64));
        value["receipt_file_sha256"] = json!("e".repeat(64));
        value["acquired_at"] = json!("2026-10-13T01:00:03Z");
        edit(&mut value);
        self.file("later-answer.json", &value)
    }
    fn deliver(&self, answer: &Path, note: &str, expected: &Value) -> Output {
        let expected_file = self.file("delivery-expected.json", expected);
        let note_file = self.temp.path().join("delivery-note.txt");
        fs::write(&note_file, note).unwrap();
        self.command()
            .args([
                "question-answer",
                "1",
                "--delivery-only",
                "--answer-file",
                answer.to_str().unwrap(),
                "--delivery-note-file",
                note_file.to_str().unwrap(),
                "--expected-state-file",
                expected_file.to_str().unwrap(),
            ])
            .output()
            .unwrap()
    }
    fn run(&self, command: &str, flag: &str, path: &std::path::Path) -> Value {
        let out = self
            .command()
            .args([command, "1", flag, path.to_str().unwrap()])
            .output()
            .unwrap();
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
        db["items"][0]["note"] = json!(format!(
            "{note}\nconversation-owner-reply:synthetic\n{reply}"
        ));
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
    format!(
        "先頭の回答。{}末尾の回答。",
        "長い回答の本文。".repeat(10_000)
    )
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
    assert_eq!(
        f.run("question-answer", "--answer-file", &answer)["resumed"],
        true
    );
    let row = f.read()["items"][0].clone();
    assert_eq!(row["status"], "todo");
    let instruction = row["scheduler"]["owner_instruction"].as_str().unwrap();
    assert!(
        instruction.starts_with(&format!("{PRIOR}\n")),
        "prior direction was overwritten"
    );
    assert!(
        instruction.contains(&reply),
        "the owner reply was cut or altered"
    );
    assert!(instruction.chars().count() > 40_000);
    assert_eq!(row["scheduler"]["last_job"], "original-worker");
    assert_eq!(row["scheduler"]["resume"], true);
    let proof = &row["scheduler"]["conversation_answer"];
    assert_eq!(proof["job"], "original-worker");
    assert_eq!(proof["launch_key"], "original-key");
    assert_eq!(
        proof["request_id"],
        "conversation-v1-0123456789abcdef0123456789abcdef"
    );
    assert_eq!(proof["message_id"], "202");
    assert_eq!(proof["question_receipt_sha256"].as_str().unwrap().len(), 64);
}

#[test]
fn only_the_verified_asking_launch_gets_a_session_proof() {
    for (case, extra) in [
        (
            "job created after the question",
            json!({"created_at":"2026-10-13T01:00:05Z"}),
        ),
        ("another launch key", json!({"launch_key":"other-key"})),
        ("failed owner", json!({"status":"failed"})),
    ] {
        let f = Fixture::new();
        let answer = f.park("回答");
        f.job(&asked_job(extra));
        f.run("question-answer", "--answer-file", &answer);
        let row = f.read()["items"][0].clone();
        // The answer is still delivered to the retired owner's next launch.
        assert!(
            row["scheduler"]["owner_instruction"]
                .as_str()
                .unwrap()
                .contains("回答"),
            "{case}"
        );
        assert_eq!(row["scheduler"]["last_job"], "original-worker", "{case}");
        assert!(
            row["scheduler"]["conversation_answer"].is_null(),
            "{case} got a proof"
        );
    }
}

#[test]
fn a_live_owner_is_never_claimed_and_gets_the_full_answer_as_the_pending_instruction() {
    let f = Fixture::new();
    let reply = long_reply();
    let answer = f.park(&reply);
    f.job(&json!({"id":"original-worker","status":"running"}));
    let mut before = f.read()["items"][0]["scheduler"].clone();
    f.run("question-answer", "--answer-file", &answer);
    let row = f.read()["items"][0].clone();
    let mut scheduler = row["scheduler"].clone();
    let pending = scheduler
        .as_object_mut()
        .unwrap()
        .remove("owner_instruction")
        .unwrap();
    let pending = pending.as_str().unwrap();
    assert!(pending.starts_with(&format!("{PRIOR}\n")));
    assert!(pending.contains(&reply), "the parked answer was cut");
    // No owner, key, phase or generation changed and no session proof was written.
    before.as_object_mut().unwrap().remove("owner_instruction");
    assert_eq!(scheduler, before);
    assert_eq!(row["resume_generation"], 7);
    assert!(row["note"].as_str().unwrap().contains("回答"));
}

#[test]
fn a_todo_without_any_worker_stores_the_reply_for_its_first_launch() {
    let f = Fixture::new();
    let mut db = f.read();
    db["items"][0]["scheduler"]["job"] = Value::Null;
    db["items"][0]["scheduler"]["key"] = Value::Null;
    db["items"][0]["scheduler"]["conversation_answer"] = json!({"job":"older"});
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
    assert_eq!(
        row["resume_generation"], 7,
        "no worker may be advanced or retired"
    );
    assert!(row["scheduler"]["last_job"].is_null());
    assert!(row["scheduler"]["conversation_answer"].is_null());
    assert!(row["processed_results"].is_null());
}

#[test]
fn the_first_answer_keeps_only_the_newest_version_of_each_reply_in_the_instruction() {
    let f = Fixture::new();
    let reply = format!(
        "{}\n{}\n{}",
        block("202", V1, "旧い版の本文"),
        block("203", V1, "別の返信の本文"),
        block("202", V2, "新しい版の本文")
    );
    let answer = f.park(&reply);
    f.job(&asked_job(json!({})));
    f.run("question-answer", "--answer-file", &answer);
    let row = f.read()["items"][0].clone();
    let instruction = row["scheduler"]["owner_instruction"].as_str().unwrap();
    assert!(instruction.contains("新しい版の本文"));
    assert!(instruction.contains("別の返信の本文"));
    assert!(!instruction.contains("旧い版の本文"));
    // The note keeps every version as history.
    assert!(row["note"].as_str().unwrap().contains("旧い版の本文"));
}

#[test]
fn an_edited_reply_after_the_answer_replaces_the_old_block_in_the_next_prompt() {
    let f = Fixture::new();
    let answer = f.park(&block("202", V1, "最初の本文：旧い指示"));
    f.job(&asked_job(json!({})));
    f.run("question-answer", "--answer-file", &answer);
    let applied = f.read()["items"][0].clone();
    assert!(
        applied["scheduler"]["owner_instruction"]
            .as_str()
            .unwrap()
            .contains("旧い指示")
    );

    let edited = f.later_answer(&answer, |_| ());
    let second = block("202", V2, "編集後の本文：新しい指示");
    let note = format!("{}\n\n{second}", applied["note"].as_str().unwrap());
    let out = f.deliver(&edited, &note, &applied);
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let meta: Value = serde_json::from_slice(&out.stdout).unwrap();
    assert_eq!(meta["delivered"], true);
    assert_eq!(meta["changed"], true);
    assert_eq!(meta["after_answer"], true);
    assert_eq!(meta["replaced_blocks"], 1);
    assert_eq!(meta["answered"], false);
    assert_eq!(meta["resumed"], false);

    let mut after = f.read()["items"][0].clone();
    let instruction = after["scheduler"]["owner_instruction"].as_str().unwrap();
    assert!(instruction.starts_with(&format!("{PRIOR}\n")));
    assert!(instruction.contains("編集後の本文：新しい指示"));
    assert!(
        !instruction.contains("旧い指示"),
        "the stale answer stayed in the next prompt"
    );
    let kept = after["note"].as_str().unwrap();
    assert!(kept.contains("旧い指示") && kept.contains("新しい指示"));
    assert_eq!(after[DELIVERIES][0]["after_answer"], true);
    // Nothing else changed: no start, finish, owner, generation, hold or proof change.
    assert_eq!(after["status"], "todo");
    let mut before = applied.clone();
    for row in [&mut before, &mut after] {
        let object = row.as_object_mut().unwrap();
        for key in ["note", "updated", DELIVERIES] {
            object.remove(key);
        }
        row["scheduler"]
            .as_object_mut()
            .unwrap()
            .remove("owner_instruction");
    }
    assert_eq!(before, after);

    // An exact replay confirms the old delivery without writing.
    let raw = fs::read(&f.paths.store).unwrap();
    let replay = f.deliver(&edited, &note, &applied);
    assert!(replay.status.success());
    let meta: Value = serde_json::from_slice(&replay.stdout).unwrap();
    assert_eq!(meta["changed"], false);
    assert_eq!(fs::read(&f.paths.store).unwrap(), raw);
}

#[test]
fn a_new_answer_preserves_marker_examples_in_prior_owner_instructions() {
    let f = Fixture::new();
    let quoted = format!(
        "以前の指示にある引用を保持。\n{}\n{}",
        block("202", V1, "引用の古い例"),
        block("202", V2, "引用の新しい例")
    );
    let mut db = f.read();
    db["items"][0]["scheduler"]["owner_instruction"] = json!(quoted);
    f.write(&db);
    let answer = f.park(&block("202", V1, "今回の本人回答を全文で渡す"));
    f.job(&asked_job(json!({})));
    f.run("question-answer", "--answer-file", &answer);
    let row = f.read()["items"][0].clone();
    assert!(
        row["scheduler"]["owner_instruction"]
            .as_str()
            .unwrap()
            .starts_with(&format!("{quoted}\n"))
    );
    assert!(
        row["scheduler"]["owner_instruction"]
            .as_str()
            .unwrap()
            .contains("今回の本人回答を全文で渡す")
    );
}

#[test]
fn delivery_after_the_answer_never_crosses_questions_stops_stale_state_or_generations() {
    for case in [
        "other_question",
        "stopped",
        "stale_snapshot",
        "done",
        "older_generation",
        "bad_instruction",
    ] {
        let f = Fixture::new();
        let answer = f.park(&block("202", V1, "最初の本文"));
        f.job(&asked_job(json!({})));
        f.run("question-answer", "--answer-file", &answer);
        let applied = f.read();
        let mut expected = applied["items"][0].clone();
        let mut stored = applied.clone();
        let row = &mut stored["items"][0];
        let mut change: Box<dyn FnOnce(&mut Value)> = Box::new(|_| ());
        match case {
            "other_question" => {
                change = Box::new(|v| {
                    v["request_id"] = json!("conversation-v1-ffffffffffffffffffffffffffffffff")
                })
            }
            "stopped" => row["stopped"] = json!(true),
            "stale_snapshot" => row["progress"] = json!(41),
            "done" => row["status"] = json!("done"),
            "older_generation" => row["resume_generation"] = json!(6),
            "bad_instruction" => row["scheduler"]["owner_instruction"] = json!(5),
            _ => unreachable!(),
        }
        if case != "stale_snapshot" {
            expected = row.clone();
        }
        f.write(&stored);
        let later = f.later_answer(&answer, change);
        let note = format!(
            "{}\n\n{}",
            expected["note"].as_str().unwrap(),
            block("202", V2, "編集後")
        );
        let before = fs::read(&f.paths.store).unwrap();
        let out = f.deliver(&later, &note, &expected);
        assert!(!out.status.success(), "{case} was accepted");
        assert_eq!(fs::read(&f.paths.store).unwrap(), before, "{case} wrote");
    }
}

#[test]
fn a_legacy_wait_without_any_scheduler_key_proves_only_the_exact_current_owner() {
    for (case, generation, processed, launch, proven) in [
        ("exact current owner", Some(7), None, "7765-launch", true),
        (
            "receipt without generation",
            None,
            None,
            "7765-launch",
            false,
        ),
        (
            "other receipt generation",
            Some(6),
            None,
            "7765-launch",
            false,
        ),
        (
            "result of another generation",
            Some(7),
            Some(3),
            "7765-launch",
            false,
        ),
        ("no launch key", Some(7), None, "", false),
    ] {
        let f = Fixture::new();
        let mut db = f.read();
        let row = &mut db["items"][0];
        row["scheduler"]["key"] = Value::Null;
        row["worker_receipt"] = match generation {
            Some(g) => json!({"job":"original-worker","generation":g}),
            None => json!({"job":"original-worker"}),
        };
        if let Some(g) = processed {
            row["processed_results"] = json!({"original-worker":{"generation":g,"reason":"block"}});
        }
        f.write(&db);
        let answer = f.park("回答");
        f.job(&asked_job(json!({"launch_key":launch})));
        f.run("question-answer", "--answer-file", &answer);
        let row = f.read()["items"][0].clone();
        assert!(
            row["scheduler"]["owner_instruction"]
                .as_str()
                .unwrap()
                .contains("回答"),
            "{case}"
        );
        if proven {
            assert_eq!(
                row["scheduler"]["conversation_answer"]["launch_key"], launch,
                "{case}"
            );
            assert_eq!(
                row["scheduler"]["conversation_answer"]["job"],
                "original-worker"
            );
        } else {
            assert!(
                row["scheduler"]["conversation_answer"].is_null(),
                "{case} got a proof"
            );
        }
    }
}

/// An answered question whose ToDo now waits on an unfinished dependency.
fn answered_behind_dependency(f: &Fixture) -> (PathBuf, Value) {
    let answer = f.park(&block("202", V1, "最初の本文：旧い指示"));
    f.job(&asked_job(json!({})));
    f.run("question-answer", "--answer-file", &answer);
    let mut db = f.read();
    db["items"][0]["status"] = json!("wait");
    db["items"][0]["wait_on"] = json!([2]);
    f.write(&db);
    (answer, db["items"][0].clone())
}

#[test]
fn a_reply_after_the_answer_is_parked_behind_an_unfinished_dependency_without_releasing_it() {
    let f = Fixture::new();
    let (answer, waiting) = answered_behind_dependency(&f);
    assert_eq!(f.read()["items"][1]["status"], "todo");
    let later = f.later_answer(&answer, |_| ());
    let note = format!(
        "{}\n\n{}",
        waiting["note"].as_str().unwrap(),
        block("202", V2, "編集後の本文：新しい指示")
    );
    let out = f.deliver(&later, &note, &waiting);
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let after = f.read();
    let row = &after["items"][0];
    // The dependency, status and owner are exactly as before; only the reply moved.
    assert_eq!(row["status"], "wait");
    assert_eq!(row["wait_on"], json!([2]));
    assert_eq!(after["items"][1]["status"], "todo");
    let instruction = row["scheduler"]["owner_instruction"].as_str().unwrap();
    assert!(instruction.contains("新しい指示") && !instruction.contains("旧い指示"));
    let (mut before, mut now) = (waiting.clone(), row.clone());
    for value in [&mut before, &mut now] {
        let object = value.as_object_mut().unwrap();
        for key in ["note", "updated", DELIVERIES] {
            object.remove(key);
        }
        value["scheduler"]
            .as_object_mut()
            .unwrap()
            .remove("owner_instruction");
    }
    assert_eq!(before, now);
}

#[test]
fn a_dependency_wait_does_not_excuse_any_other_hold_or_a_malformed_dependency() {
    for case in [
        "stopped",
        "owner_postponement_recorded",
        "decision_wait",
        "wait_ready",
        "resume_condition",
        "pending_cancel",
        "self_dependency",
        "non_positive_dependency",
        "not_a_list",
        "blocked_status",
    ] {
        let f = Fixture::new();
        let (answer, _) = answered_behind_dependency(&f);
        let mut db = f.read();
        let row = &mut db["items"][0];
        match case {
            "stopped" => row["stopped"] = json!(true),
            "owner_postponement_recorded" => row["owner_postponement_recorded"] = json!(true),
            "decision_wait" => row["decision_wait"] = json!({"x":1}),
            "wait_ready" => row["wait_ready"] = json!({"x":1}),
            "resume_condition" => row["resume_condition"] = json!({"x":1}),
            "pending_cancel" => row["scheduler"]["pending_cancel"] = json!(true),
            "self_dependency" => row["wait_on"] = json!([1]),
            "non_positive_dependency" => row["wait_on"] = json!([0]),
            "not_a_list" => row["wait_on"] = json!("2"),
            "blocked_status" => row["status"] = json!("block"),
            _ => unreachable!(),
        }
        let expected = row.clone();
        f.write(&db);
        let later = f.later_answer(&answer, |_| ());
        let note = format!(
            "{}\n\n{}",
            expected["note"].as_str().unwrap(),
            block("202", V2, "編集後")
        );
        let before = fs::read(&f.paths.store).unwrap();
        let out = f.deliver(&later, &note, &expected);
        assert!(!out.status.success(), "{case} was accepted");
        assert_eq!(fs::read(&f.paths.store).unwrap(), before, "{case} wrote");
    }
}
