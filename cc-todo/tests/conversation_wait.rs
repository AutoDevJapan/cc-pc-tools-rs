//! Exercise the actual parser, canonical lock, dispatcher and atomic store writer.
use cc_todo::{Lock, Paths};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    fs,
    path::Path,
    process::{Command, Output},
    time::Duration,
};

const WAIT: &str = "owner_question_wait";
const ANSWERS: &str = "owner_question_answer_receipts";
const DELIVERIES: &str = "owner_question_delivery_receipts";

#[test]
fn conversation_scope_rejects_project_or_root_changes_before_any_mutation() {
    for command in [
        "edit",
        "question-wait",
        "question-answer",
        "question-withdraw",
    ] {
        for change in ["project", "root"] {
            let f = Fixture::new();
            let (_, answer_file, _) = f.park();
            let mut db = f.read();
            let scope_file = f.file("scope.json", &json!({"domain":"discord-conversation-todo-scope-v1",
                "todo_id":"1", "project":"cc-pc-tools-rs", "project_path_sha256":sha(f.temp.path().to_str().unwrap().as_bytes())}));
            let expected = f.file("expected.json", &db["items"][0]);
            let note = f.temp.path().join("scope-note.txt");
            fs::write(&note, "guarded delivery reference").unwrap();
            if change == "project" {
                db["projects"]["other-project"] = db["projects"]["cc-pc-tools-rs"].clone();
                db["items"][0]["project"] = json!("other-project");
            } else {
                db["projects"]["cc-pc-tools-rs"] =
                    json!(f.temp.path().join("another-root").to_str().unwrap());
            }
            f.write(&db);
            let before = f.raw();
            let q = f.temp.path().join("question.json");
            let mut args = vec![command, "1", "--scope-file", scope_file.to_str().unwrap()];
            match command {
                "edit" => args.extend([
                    "--note-file",
                    note.to_str().unwrap(),
                    "--expected-state-file",
                    expected.to_str().unwrap(),
                ]),
                "question-wait" => args.extend(["--question-file", q.to_str().unwrap()]),
                "question-answer" => args.extend(["--answer-file", answer_file.to_str().unwrap()]),
                "question-withdraw" => args.extend([
                    "--question-file",
                    q.to_str().unwrap(),
                    "--reason-file",
                    note.to_str().unwrap(),
                    "--expected-state-file",
                    expected.to_str().unwrap(),
                ]),
                _ => unreachable!(),
            }
            let out = f.command(&args).output().unwrap();
            assert!(
                !out.status.success(),
                "accepted changed {change} for {command}"
            );
            assert_eq!(
                String::from_utf8_lossy(&out.stderr).trim(),
                "conversation_scope_changed",
                "wrong failure for {command}"
            );
            assert_eq!(f.raw(), before, "scope failure changed {command}");
        }
    }
}

#[test]
fn scoped_edit_requires_the_exact_guarded_row_even_when_the_note_is_unchanged() {
    let f = Fixture::new();
    let mut db = f.read();
    let scope_file = f.file("scope.json", &json!({"domain":"discord-conversation-todo-scope-v1", "todo_id":"1",
        "project":"cc-pc-tools-rs", "project_path_sha256":sha(f.temp.path().to_str().unwrap().as_bytes())}));
    let expected = f.file("expected.json", &db["items"][0]);
    let note = f.temp.path().join("scope-note.txt");
    fs::write(&note, "guarded delivery reference").unwrap();
    db["items"][0]["resume_generation"] = json!(8);
    f.write(&db);
    let before = f.raw();
    let out = f
        .command(&[
            "edit",
            "1",
            "--scope-file",
            scope_file.to_str().unwrap(),
            "--expected-state-file",
            expected.to_str().unwrap(),
            "--note-file",
            note.to_str().unwrap(),
        ])
        .output()
        .unwrap();
    assert!(!out.status.success());
    assert_eq!(
        String::from_utf8_lossy(&out.stderr).trim(),
        "conversation_state_changed"
    );
    assert_eq!(f.raw(), before);
}

#[test]
fn withdrawal_accepts_a_large_original_row_without_losing_history() {
    let f = Fixture::new();
    f.withdraw_ready();
    let mut db = f.read();
    let history = json!([{"note":"saved history ".repeat(90_000)}]);
    db["items"][0]["note_history"] = history.clone();
    f.write(&db);
    f.snapshot();
    assert!(
        fs::metadata(f.temp.path().join("withdrawal-origin.json"))
            .unwrap()
            .len()
            > 1024 * 1024
    );
    let out = f.withdraw();
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    assert_eq!(f.read()["items"][0]["note_history"], history);
}

#[test]
fn snapshot_larger_than_the_store_limit_is_rejected_without_mutation() {
    let f = Fixture::new();
    f.withdraw_ready();
    f.snapshot();
    fs::OpenOptions::new()
        .write(true)
        .open(f.temp.path().join("withdrawal-origin.json"))
        .unwrap()
        .set_len(cc_job_policy::todo_store::MAX_BYTES as u64 + 1)
        .unwrap();
    let before = f.raw();
    let out = f.withdraw();
    assert!(!out.status.success());
    assert_eq!(
        String::from_utf8_lossy(&out.stderr).trim(),
        "conversation_file_too_large"
    );
    assert_eq!(f.raw(), before);
}

fn continued(f: &Fixture) -> (std::path::PathBuf, Value) {
    let (_, answer_file, answer) = f.park();
    let mut db = f.read();
    let row = &mut db["items"][0];
    row["status"] = json!("doing");
    row["resume_generation"] = json!(8);
    row["worker_receipt"] = json!({"job":null,"generation":8});
    row["scheduler"] = json!({"job":null,"worktree":"saved-worktree","branch":"saved-branch"});
    row["note"] = json!("新しい直接指示を優先する\n元の依頼と途中成果を保持する");
    f.write(&db);
    (answer_file, answer)
}

fn deliver_only(f: &Fixture, answer: &Path, expected: &Value) -> Output {
    let expected_file = f.file("delivery-expected.json", expected);
    let note_file = f.temp.path().join("delivery-note.txt");
    fs::write(
        &note_file,
        format!(
            "{}\n\nprivate reply reference",
            expected["note"].as_str().unwrap()
        ),
    )
    .unwrap();
    f.command(&[
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

#[test]
fn continued_delivery_preserves_everything_except_its_note_time_and_receipt() {
    let f = Fixture::new();
    let (answer_file, a) = continued(&f);
    let before = f.read();
    let out = deliver_only(&f, &answer_file, &before["items"][0]);
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let meta: Value = serde_json::from_slice(&out.stdout).unwrap();
    assert_eq!(meta["delivered"], true);
    assert_eq!(meta["resumed"], false);
    assert_eq!(meta["answered"], false);
    let mut after = f.read();
    let record = &after["items"][0][DELIVERIES][0];
    assert!(
        after["items"][0]["scheduler"]["owner_instruction"]
            .as_str()
            .is_some_and(|s| s.contains("private reply reference")),
        "the next worker would lose the delivered reference"
    );
    assert_eq!(record["answer"], a);
    assert_eq!(record["wait"], before["items"][0][WAIT]);
    assert_eq!(record["recipient"]["resume_generation"], 8);
    assert_eq!(
        record["recipient"]["worker_receipt"],
        before["items"][0]["worker_receipt"]
    );
    assert_eq!(
        record["note_sha256"],
        sha(after["items"][0]["note"].as_str().unwrap().as_bytes())
    );
    assert_eq!(
        record["instruction_sha256"],
        sha(after["items"][0]["scheduler"]["owner_instruction"]
            .as_str()
            .unwrap()
            .as_bytes())
    );
    after["items"][0]["scheduler"]
        .as_object_mut()
        .unwrap()
        .remove("owner_instruction");
    for key in ["note", "updated", DELIVERIES] {
        let row = after["items"][0].as_object_mut().unwrap();
        match before["items"][0].get(key) {
            Some(value) => {
                row.insert(key.into(), value.clone());
            }
            None => {
                row.remove(key);
            }
        }
    }
    assert_eq!(
        after, before,
        "delivery changed task/hold/owner/original work"
    );
}

#[test]
fn continued_delivery_keeps_prior_instructions_and_future_time_holds_for_the_next_worker() {
    let f = Fixture::new();
    let (answer_file, _) = continued(&f);
    let mut db = f.read();
    let row = &mut db["items"][0];
    row["scheduler"]["owner_instruction"] = json!("最新の直接指示を優先する");
    row["scheduler"]["not_before"] = json!(10_000_000_000_f64);
    row["stall_recovery"]["next_at"] = json!(10_000_000_000_f64);
    row["discord_restart_not_before"] = json!(10_000_000_000_f64);
    f.write(&db);
    let out = deliver_only(&f, &answer_file, &db["items"][0]);
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let mut after = f.read();
    let row = &mut after["items"][0];
    let instruction = row["scheduler"]["owner_instruction"]
        .as_str()
        .unwrap()
        .to_owned();
    assert!(instruction.starts_with("最新の直接指示を優先する\n\n"));
    assert!(instruction.contains("private reply reference"));
    assert_eq!(
        row["scheduler"]["not_before"],
        db["items"][0]["scheduler"]["not_before"]
    );
    assert_eq!(row["stall_recovery"], db["items"][0]["stall_recovery"]);
    assert_eq!(
        row["discord_restart_not_before"],
        db["items"][0]["discord_restart_not_before"]
    );
    row["note"] = json!("通常の結果取込で更新されたメモ");
    cc_scheduler::common::resume_without_worker(row);
    assert_eq!(row["scheduler"]["owner_instruction"], instruction);
    assert_eq!(row[WAIT], db["items"][0][WAIT]);
}

#[test]
fn continued_delivery_retry_does_not_touch_a_later_stop_and_edits_need_a_new_delivery() {
    let f = Fixture::new();
    let (answer_file, _) = continued(&f);
    let before = f.read();
    assert!(
        deliver_only(&f, &answer_file, &before["items"][0])
            .status
            .success()
    );
    let mut db = f.read();
    db["items"][0]["status"] = json!("block");
    db["items"][0]["owner_stopped"] = json!(true);
    f.write(&db);
    let raw = f.raw();
    let out = deliver_only(&f, &answer_file, &before["items"][0]);
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let meta: Value = serde_json::from_slice(&out.stdout).unwrap();
    assert_eq!(meta["changed"], false);
    assert_eq!(meta["resumed"], false);
    assert_eq!(f.raw(), raw);
    db["items"][0]["status"] = json!("doing");
    db["items"][0]
        .as_object_mut()
        .unwrap()
        .remove("owner_stopped");
    f.write(&db);
    let mut edited: Value = serde_json::from_slice(&fs::read(&answer_file).unwrap()).unwrap();
    edited["reply_sha256"] = json!("d".repeat(64));
    edited["receipt_file_sha256"] = json!("e".repeat(64));
    let edit_file = f.file("edited-answer.json", &edited);
    assert!(
        deliver_only(&f, &edit_file, &db["items"][0])
            .status
            .success()
    );
    let after = f.read();
    assert_eq!(after["items"][0][DELIVERIES].as_array().unwrap().len(), 2);
    assert_eq!(after["items"][0][DELIVERIES][1]["answer"], edited);
    assert!(after["items"][0].get(ANSWERS).is_none());
}

#[test]
fn continued_delivery_rejects_changed_state_generation_identity_and_holds_without_writing() {
    for case in [
        "cas",
        "missing-wait",
        "question",
        "owner",
        "sha",
        "missing-generation",
        "same-generation",
        "backward-generation",
        "missing-origin",
        "different-wait",
        "block",
        "done",
        "unknown",
        "stop",
        "dependency",
        "ready",
    ] {
        let f = Fixture::new();
        let (answer_file, _) = continued(&f);
        let mut db = f.read();
        let mut expected = db["items"][0].clone();
        let row = &mut db["items"][0];
        match case {
            "cas" => row["note"] = json!("途中で新しい指示"),
            "missing-wait" => {
                row.as_object_mut().unwrap().remove(WAIT);
            }
            "question" => row[WAIT]["question"]["question_fingerprint"] = json!("f".repeat(64)),
            "owner" => row[WAIT]["question"]["owner_id"] = json!("999"),
            "sha" => row[WAIT]["question_receipt_sha256"] = json!("f".repeat(64)),
            "missing-generation" => {
                row.as_object_mut().unwrap().remove("resume_generation");
            }
            "same-generation" => row["resume_generation"] = json!(7),
            "backward-generation" => row["resume_generation"] = json!(6),
            "missing-origin" => {
                row[WAIT].as_object_mut().unwrap().remove("origin");
            }
            "different-wait" => row[WAIT]["question"]["request_id"] = json!("another-generation"),
            "block" | "done" | "unknown" => row["status"] = json!(case),
            "stop" => row["owner_stopped"] = json!(true),
            "dependency" => row["wait_on"] = json!([2]),
            "ready" => row["wait_ready"] = json!({"condition":"external"}),
            _ => unreachable!(),
        }
        if case != "cas" {
            expected = row.clone();
        }
        f.write(&db);
        let raw = f.raw();
        let out = deliver_only(&f, &answer_file, &expected);
        assert!(!out.status.success(), "accepted {case}");
        assert!(
            String::from_utf8_lossy(&out.stderr).starts_with("conversation_"),
            "{case}: {}",
            String::from_utf8_lossy(&out.stderr)
        );
        assert_eq!(f.raw(), raw, "rejection changed {case}");
    }
}

#[test]
fn normal_answer_cannot_release_a_different_current_generation() {
    let f = Fixture::new();
    let (_, answer_file, _) = f.park();
    let mut db = f.read();
    db["items"][0]["resume_generation"] = json!(8);
    f.write(&db);
    f.reject("question-answer", "--answer-file", &answer_file);
}

struct Fixture {
    temp: tempfile::TempDir,
    paths: Paths,
}
fn sha(data: &[u8]) -> String {
    format!("{:x}", Sha256::digest(data))
}
fn question() -> Value {
    json!({
        "domain":"discord-conversation-question-v1", "request_id":"conversation-v1-0123456789abcdef0123456789abcdef",
        "todo_id":"1", "guild_id":"100", "owner_id":"101", "bot_id":"102",
        "thread_id":"200", "question_message_id":"200", "progress_thread_id":"201",
        "question_fingerprint":"a".repeat(64), "created_at":"2026-10-13T01:00:00Z"
    })
}
fn answer(q: &Value, question_sha: &str) -> Value {
    json!({
        "domain":"discord-conversation-answer-v1", "request_id":q["request_id"], "todo_id":q["todo_id"],
        "thread_id":q["thread_id"], "question_message_id":q["question_message_id"],
        "question_fingerprint":q["question_fingerprint"], "question_receipt_sha256":question_sha,
        "message_id":"202", "author_id":q["owner_id"], "source":"discord-api-v10",
        "reply_sha256":"b".repeat(64), "acquired_at":"2026-10-13T01:00:01Z", "receipt_file_sha256":"c".repeat(64)
    })
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
        fs::create_dir(temp.path().join("specs")).unwrap();
        fs::write(
            temp.path().join("specs/original.md"),
            b"original saved specification",
        )
        .unwrap();
        fs::write(
            temp.path().join("unfinished.rs"),
            b"unfinished source bytes",
        )
        .unwrap();
        fs::write(&paths.store, serde_json::to_vec(&json!({
            "projects":{"cc-pc-tools-rs":temp.path().to_str().unwrap()}, "next_id":3,
            "original_registry_recovered":false, "other_root":{"unknown":[1,2,3]},
            "items":[{
                "id":1,"project":"cc-pc-tools-rs","title":"新しい質問と回答を確認する","status":"doing",
                "note":"元のメモ\n引き継ぐ途中成果", "updated":"2026-10-13 10:00", "progress":40,
                "spec":"specs/original.md", "dispatchable":true, "jobs":["original-worker"],
                "needs":["cargo"], "model":"fixture-model", "effort":"high",
                "scheduler":{"job":"original-worker","key":"original-key","phase":"running",
                    "worktree":"saved-worktree", "branch":"saved-branch", "not_before":0,"selection":{"saved":true}},
                "stall_recovery":{"history":[{"job":"earlier"}],"next_at":0},
                "worker_receipt":{"job":"original-worker","original":true}, "resume_generation":7,
                "other_item":{"unknown":[4,5,6]}
            }, {"id":2,"project":"cc-pc-tools-rs","status":"todo","note":"他の作業","jobs":[]}]
        })).unwrap()).unwrap();
        Self { temp, paths }
    }
    fn read(&self) -> Value {
        cc_todo::read_store(&self.paths.store).unwrap()
    }
    fn write(&self, value: &Value) {
        fs::write(&self.paths.store, serde_json::to_vec(value).unwrap()).unwrap();
    }
    fn raw(&self) -> Vec<u8> {
        fs::read(&self.paths.store).unwrap()
    }
    fn file(&self, name: &str, value: &Value) -> std::path::PathBuf {
        let p = self.temp.path().join(name);
        fs::write(&p, serde_json::to_vec(value).unwrap()).unwrap();
        p
    }
    fn command(&self, args: &[&str]) -> Command {
        let mut cmd = Command::new(env!("CARGO_BIN_EXE_todo"));
        cmd.args(args)
            .env("CC_TODO_HOME", self.temp.path())
            .env("CC_ASK_CONFIG", self.temp.path().join("notify-config.json"))
            .env("CC_TODO_JOBS", &self.paths.jobs);
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            cmd.creation_flags(0x08000000);
        }
        cmd
    }
    fn call(&self, command: &str, flag: &str, path: &Path) -> Output {
        self.command(&[command, "1", flag, path.to_str().unwrap()])
            .output()
            .unwrap()
    }
    fn ok(&self, command: &str, flag: &str, path: &Path) -> Value {
        let out = self.call(command, flag, path);
        assert!(
            out.status.success(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        assert!(
            out.stderr.is_empty(),
            "{}",
            String::from_utf8_lossy(&out.stderr)
        );
        let result: Value = serde_json::from_slice(&out.stdout).unwrap();
        assert_eq!(result["id"], 1);
        let stdout = String::from_utf8(out.stdout).unwrap();
        assert!(!stdout.contains(path.to_str().unwrap()));
        assert!(!stdout.contains("元のメモ"));
        assert!(!stdout.contains("private reply"));
        assert!(!stdout.contains("domain"));
        result
    }
    fn reject(&self, command: &str, flag: &str, path: &Path) {
        let before = self.raw();
        let out = self.call(command, flag, path);
        assert!(
            !out.status.success(),
            "accepted invalid input: {}",
            String::from_utf8_lossy(&out.stdout)
        );
        assert!(out.stdout.is_empty());
        let stderr = String::from_utf8(out.stderr).unwrap();
        assert!(stderr.starts_with("conversation_"), "{stderr}");
        assert!(!stderr.contains(path.to_str().unwrap()));
        assert!(!stderr.contains("private reply"));
        assert_eq!(self.raw(), before, "rejection rewrote the canonical store");
    }
    fn park(&self) -> (Value, std::path::PathBuf, Value) {
        let q = question();
        let q_file = self.file("question.json", &q);
        let q_sha = sha(&fs::read(&q_file).unwrap());
        let meta = self.ok("question-wait", "--question-file", &q_file);
        assert_eq!(meta["question_receipt_sha256"], q_sha);
        assert_eq!(meta["changed"], true);
        let a = answer(&q, &q_sha);
        let a_file = self.file("answer.json", &a);
        (q, a_file, a)
    }
}

fn assert_work_preserved(before: &Value, after: &Value) {
    let (mut a, mut b) = (before.clone(), after.clone());
    for db in [&mut a, &mut b] {
        let row = db["items"][0].as_object_mut().unwrap();
        for key in ["status", "note", "updated", WAIT, ANSWERS] {
            row.remove(key);
        }
    }
    assert_eq!(
        a, b,
        "changed unrelated state, ownership or unfinished work"
    );
}

#[test]
fn canonical_wait_edit_answer_keeps_the_original_work_and_new_private_reply() {
    let f = Fixture::new();
    let before = f.read();
    let (q, a_file, a) = f.park();
    let parked = f.read();
    assert_eq!(parked["items"][0]["status"], "block");
    let q_bytes = fs::read(f.temp.path().join("question.json")).unwrap();
    assert_eq!(
        parked["items"][0][WAIT]["question_receipt_sha256"],
        sha(&q_bytes)
    );
    assert_eq!(parked["items"][0][WAIT]["question"], q);
    assert!(
        parked["items"][0]["note"]
            .as_str()
            .unwrap()
            .starts_with(before["items"][0]["note"].as_str().unwrap())
    );
    assert_work_preserved(&before, &parked);

    // The Go bridge uses the existing edit command before applying the answer.
    let note = format!(
        "{}\n本人の回答: private reply \"a:b\"\n元の作業を続ける",
        parked["items"][0]["note"].as_str().unwrap()
    );
    let note_file = f.temp.path().join("private-note.txt");
    fs::write(&note_file, &note).unwrap();
    let edited = f
        .command(&["edit", "1", "--note-file", note_file.to_str().unwrap()])
        .output()
        .unwrap();
    assert!(
        edited.status.success(),
        "{}",
        String::from_utf8_lossy(&edited.stderr)
    );
    let edited_state = f.read();
    assert_eq!(
        edited_state["items"][0]["note_history"][0]["note"],
        parked["items"][0]["note"]
    );
    let meta = f.ok("question-answer", "--answer-file", &a_file);
    assert_eq!(meta["resumed"], true);
    assert_eq!(meta["changed"], true);
    let after = f.read();
    let row = &after["items"][0];
    assert_eq!(row["status"], "todo");
    assert!(row.get(WAIT).is_none());
    assert_eq!(
        row["note"],
        format!(
            "{}\n本人の回答: private reply \"a:b\"\n元の作業を続ける",
            before["items"][0]["note"].as_str().unwrap()
        )
    );
    let receipts = row[ANSWERS].as_array().unwrap();
    assert_eq!(receipts.len(), 1);
    assert_eq!(receipts[0]["wait"], parked["items"][0][WAIT]);
    assert_eq!(receipts[0]["answer"], a);
    assert_eq!(
        receipts[0]["answer_file_sha256"],
        sha(&fs::read(&a_file).unwrap())
    );
    assert_work_preserved(&edited_state, &after);
    assert_eq!(
        fs::read(f.temp.path().join("specs/original.md")).unwrap(),
        b"original saved specification"
    );
    assert_eq!(
        fs::read(f.temp.path().join("unfinished.rs")).unwrap(),
        b"unfinished source bytes"
    );
    assert!(
        fs::read_dir(&f.paths.jobs).unwrap().next().is_none(),
        "a worker was launched"
    );
}

#[test]
fn exact_raw_retries_do_not_rewrite_the_store_or_release_a_later_stop_or_done() {
    let f = Fixture::new();
    let (_, a_file, _) = f.park();
    let q_file = f.temp.path().join("question.json");
    let parked = f.raw();
    assert_eq!(
        f.ok("question-wait", "--question-file", &q_file)["changed"],
        false
    );
    assert_eq!(f.raw(), parked);
    f.ok("question-answer", "--answer-file", &a_file);
    for status in [
        "todo",
        "doing",
        "block",
        "done",
        "wait",
        "recovery_unverified",
    ] {
        let mut state = f.read();
        state["items"][0]["status"] = json!(status);
        state["items"][0]["stopped"] = json!(true);
        state["items"][0]["note"] = json!("本人が後から停止した記録");
        f.write(&state);
        let before = f.raw();
        let r = f.ok("question-answer", "--answer-file", &a_file);
        assert_eq!(r["changed"], false);
        assert_eq!(r["resumed"], false);
        assert_eq!(f.raw(), before);
        assert_eq!(
            f.ok("question-wait", "--question-file", &q_file)["answered"],
            true
        );
        assert_eq!(f.raw(), before);
        assert_eq!(f.read()["items"][0][ANSWERS].as_array().unwrap().len(), 1);
    }
}

#[test]
fn existing_wait_cannot_be_replaced_and_same_question_with_different_bytes_is_rejected() {
    let f = Fixture::new();
    f.park();
    let mut q = question();
    q["request_id"] = json!("another-request");
    q["thread_id"] = json!("300");
    q["question_message_id"] = json!("300");
    let file = f.file("other.json", &q);
    f.reject("question-wait", "--question-file", &file);
    fs::write(&file, serde_json::to_vec_pretty(&question()).unwrap()).unwrap();
    f.reject("question-wait", "--question-file", &file);
    let mut a = answer(
        &question(),
        &sha(&fs::read(f.temp.path().join("question.json")).unwrap()),
    );
    f.ok(
        "question-answer",
        "--answer-file",
        &f.file("answer.json", &a),
    );
    a["message_id"] = json!("203");
    f.reject(
        "question-answer",
        "--answer-file",
        &f.file("different-answer.json", &a),
    );
    let original: Value =
        serde_json::from_slice(&fs::read(f.temp.path().join("answer.json")).unwrap()).unwrap();
    let file = f.temp.path().join("different-bytes.json");
    fs::write(&file, serde_json::to_vec_pretty(&original).unwrap()).unwrap();
    f.reject("question-answer", "--answer-file", &file);
}

#[test]
fn another_question_after_a_consumed_answer_keeps_both_generations() {
    let f = Fixture::new();
    let (_, a_file, _) = f.park();
    f.ok("question-answer", "--answer-file", &a_file);
    let mut q = question();
    q["request_id"] = json!("new-generation");
    q["thread_id"] = json!("300");
    q["question_message_id"] = json!("300");
    q["progress_thread_id"] = json!("301");
    let file = f.file("second-question.json", &q);
    f.ok("question-wait", "--question-file", &file);
    let parked = f.raw();
    assert_eq!(
        f.ok("question-answer", "--answer-file", &a_file)["changed"],
        false
    );
    assert_eq!(f.raw(), parked, "old answer released a different question");
    let mut a = answer(&q, &sha(&fs::read(&file).unwrap()));
    a["message_id"] = json!("302");
    f.ok(
        "question-answer",
        "--answer-file",
        &f.file("second-answer.json", &a),
    );
    assert_eq!(f.read()["items"][0][ANSWERS].as_array().unwrap().len(), 2);
}

fn holds() -> Vec<(&'static str, Value)> {
    vec![
        ("stopped", json!(true)),
        ("cancel_requested", json!(true)),
        ("owner_stopped", json!(true)),
        ("paused", json!(true)),
        ("owner_postponement_recorded", json!(true)),
        ("stop_kind", json!("owner")),
        ("decision_wait", json!({"pending":true})),
        (
            "wait_ready",
            json!({"condition":"retry-time","since":1,"not_before":61}),
        ),
        ("resume_condition", json!("external")),
        ("external_resume_condition", json!({})),
        ("wait_on", json!([2])),
        ("wait_on", json!([999])),
        ("wait_on", json!([1])),
        ("wait_on", json!(["2"])),
        ("wait_on", json!({})),
        ("work_owner_wait_on", json!([2])),
        ("scheduler", json!({"owner_stop_job":"original-worker"})),
        ("scheduler", json!({"question":{"thread":"legacy"}})),
        ("scheduler", json!({"question_close_pending":true})),
        ("scheduler", json!({"pending_cancel":"manual"})),
        ("scheduler", json!({"not_before":32_503_680_000_u64})),
        ("scheduler", json!({"not_before":"bad"})),
        ("scheduler", json!("bad")),
        ("stall_recovery", json!({"next_at":32_503_680_000_u64})),
        ("discord_restart_not_before", json!(32_503_680_000_u64)),
        ("stopped", json!("false")),
    ]
}

#[test]
fn registration_rejects_stops_dependency_waits_external_conditions_and_unknown_states() {
    let f = Fixture::new();
    let base = f.read();
    let q_file = f.file("question.json", &question());
    for (key, value) in holds() {
        let mut state = base.clone();
        state["items"][0][key] = value;
        f.write(&state);
        f.reject("question-wait", "--question-file", &q_file);
    }
    for status in [
        "block",
        "done",
        "wait",
        "recovery_unverified",
        "paused",
        "unknown",
    ] {
        let mut state = base.clone();
        state["items"][0]["status"] = json!(status);
        f.write(&state);
        f.reject("question-wait", "--question-file", &q_file);
    }
    for project in ["foreign-project", "owner", "pc-capacity-audit"] {
        let mut state = base.clone();
        state["items"][0]["project"] = json!(project);
        f.write(&state);
        f.reject("question-wait", "--question-file", &q_file);
    }
}

#[test]
fn another_registered_project_is_allowed_but_owner_and_invalid_registrations_are_rejected() {
    let f = Fixture::new();
    let original = f.read();
    let mut registered = original.clone();
    registered["projects"]["synthetic-other"] = json!(f.temp.path().to_str().unwrap());
    registered["items"][0]["project"] = json!("synthetic-other");
    f.write(&registered);
    let (_, a_file, _) = f.park();
    f.ok("question-answer", "--answer-file", &a_file);
    assert_eq!(f.read()["items"][0]["status"], "todo");
    assert_work_preserved(&registered, &f.read());
    for (project, path) in [
        ("owner", json!(f.temp.path().to_str().unwrap())),
        ("OWNER", json!(f.temp.path().to_str().unwrap())),
        ("synthetic-other", Value::Null),
        ("synthetic-other", json!(123)),
        ("synthetic-other", json!("")),
        ("synthetic-other", json!(" \t")),
        ("synthetic-other", json!("invalid\u{0}path")),
    ] {
        let mut invalid = original.clone();
        invalid["items"][0]["project"] = json!(project);
        invalid["projects"][project] = path;
        f.write(&invalid);
        f.reject(
            "question-wait",
            "--question-file",
            &f.file("invalid-project.json", &question()),
        );
    }
    // A registration removed between wait and answer must also stay blocked.
    f.write(&registered);
    let (_, a_file, _) = f.park();
    let mut removed = f.read();
    removed["projects"]
        .as_object_mut()
        .unwrap()
        .remove("synthetic-other");
    f.write(&removed);
    f.reject("question-answer", "--answer-file", &a_file);
}

#[test]
fn current_owner_job_stops_are_preserved_before_wait_and_before_answer() {
    let f = Fixture::new();
    let base = f.read();
    let dir = f.paths.jobs.join("original-worker");
    fs::create_dir(&dir).unwrap();
    let q_file = f.file("question.json", &question());
    let records = [
        json!({"id":"original-worker","status":"running","stopped":true}),
        json!({"id":"original-worker","status":"cancelled","cancel_reason":"manual"}),
        json!({"id":"original-worker","status":"cancelled","cancel_reason":"person"}),
        json!({"id":"original-worker","status":"running","cancel_requested":true}),
        json!({"id":"original-worker","status":"cancelled"}),
        json!({"id":"foreign-worker","status":"succeeded"}),
    ];
    for record in &records {
        fs::write(dir.join("job.json"), serde_json::to_vec(record).unwrap()).unwrap();
        f.reject("question-wait", "--question-file", &q_file);
    }
    fs::write(dir.join("job.json"), b"{").unwrap();
    f.reject("question-wait", "--question-file", &q_file);
    fs::write(
        dir.join("job.json"),
        br#"{"id":"original-worker","status":"running"}"#,
    )
    .unwrap();
    let (_, a_file, _) = f.park();
    let parked = f.raw();
    for record in &records {
        fs::write(dir.join("job.json"), serde_json::to_vec(record).unwrap()).unwrap();
        f.reject("question-answer", "--answer-file", &a_file);
        assert_eq!(f.raw(), parked);
    }
    fs::write(
        dir.join("job.json"),
        br#"{"id":"original-worker","status":"running"}"#,
    )
    .unwrap();
    f.ok("question-answer", "--answer-file", &a_file);
    // A live owner is never claimed or restarted: the full answer is only parked
    // as the pending instruction for the next regular launch.
    let mut after = f.read();
    assert!(
        after["items"][0]["scheduler"]["owner_instruction"]
            .as_str()
            .is_some_and(|text| text.contains("元ToDo 1の質問への本人回答")),
        "the live owner's answer was not parked for the next launch"
    );
    after["items"][0]["scheduler"]
        .as_object_mut()
        .unwrap()
        .remove("owner_instruction");
    assert_work_preserved(&base, &after);
}

#[test]
fn first_answer_retires_only_the_finished_owner_launch_and_keeps_the_saved_work() {
    for phase in ["running", "prepared", "finished"] {
        let f = Fixture::new();
        let (_, a_file, _) = f.park();
        let mut parked = f.read();
        parked["items"][0]["scheduler"]["phase"] = json!(phase);
        parked["items"][0]["scheduler"]["workspace_receipt"] =
            json!({"job":"original-worker","todo_id":1,"cwd":"saved-worktree"});
        parked["items"][0]["note"] = json!(format!(
            "{}\nconversation-owner-reply:private-source-reference",
            parked["items"][0]["note"].as_str().unwrap()
        ));
        f.write(&parked);
        let dir = f.paths.jobs.join("original-worker");
        fs::create_dir(&dir).unwrap();
        let original_job = br#"{"id":"original-worker","status":"succeeded","todo_id":1,"launch_key":"original-key","cwd":"saved-worktree"}"#;
        fs::write(dir.join("job.json"), original_job).unwrap();
        assert_eq!(
            f.ok("question-answer", "--answer-file", &a_file)["resumed"],
            true
        );
        let after = f.read();
        let row = &after["items"][0];
        assert_eq!(row["status"], "todo");
        assert!(row["scheduler"]["job"].is_null());
        assert!(row["scheduler"]["key"].is_null());
        assert_eq!(row["scheduler"]["last_job"], "original-worker");
        assert_eq!(row["scheduler"]["phase"], "resumed");
        assert_eq!(row["scheduler"]["resume"], true);
        assert_eq!(row["resume_generation"], 8);
        assert_eq!(row["worker_receipt"], json!({"job":null,"generation":8}));
        assert_eq!(
            row["processed_results"]["original-worker"]["reason"],
            "conversation_answer"
        );
        assert!(
            row["scheduler"]["owner_instruction"]
                .as_str()
                .unwrap()
                .contains("conversation-owner-reply:private-source-reference")
        );
        for key in [
            "worktree",
            "branch",
            "selection",
            "not_before",
            "workspace_receipt",
        ] {
            assert_eq!(row["scheduler"][key], parked["items"][0]["scheduler"][key]);
        }
        for key in [
            "spec",
            "jobs",
            "needs",
            "model",
            "effort",
            "progress",
            "other_item",
            "stall_recovery",
        ] {
            assert_eq!(row[key], parked["items"][0][key]);
        }
        assert_eq!(fs::read(dir.join("job.json")).unwrap(), original_job);
        assert_eq!(
            fs::read(f.temp.path().join("unfinished.rs")).unwrap(),
            b"unfinished source bytes"
        );
        assert_eq!(
            fs::read(f.temp.path().join("specs/original.md")).unwrap(),
            b"original saved specification"
        );
        let mut later = after;
        later["items"][0]["status"] = json!("block");
        later["items"][0]["note"] = json!("データ到着まで停止");
        later["items"][0]["scheduler"]["key"] = json!("new-turn-key");
        later["items"][0]["scheduler"]["job"] = json!("later-worker");
        f.write(&later);
        let before_replay = f.raw();
        assert_eq!(
            f.ok("question-answer", "--answer-file", &a_file)["resumed"],
            false
        );
        assert_eq!(
            f.raw(),
            before_replay,
            "answer replay retired a later turn or released its stop"
        );
    }
}

#[test]
fn stale_note_compare_and_swap_cannot_restore_an_overwritten_question_hold() {
    let f = Fixture::new();
    let (_, a_file, _) = f.park();
    let parked = f.read();
    let expected = sha(parked["items"][0]["note"].as_str().unwrap().as_bytes());
    let stale_note = f.temp.path().join("stale-reply-note.txt");
    fs::write(
        &stale_note,
        format!(
            "{}\nprivate reply reference",
            parked["items"][0]["note"].as_str().unwrap()
        ),
    )
    .unwrap();
    let stopped = f
        .command(&["block", "1", "データ到着まで停止"])
        .output()
        .unwrap();
    assert!(stopped.status.success());
    let before = f.raw();
    let edit = f
        .command(&[
            "edit",
            "1",
            "--note-file",
            stale_note.to_str().unwrap(),
            "--expected-note-sha256",
            &expected,
        ])
        .output()
        .unwrap();
    assert!(!edit.status.success());
    assert!(edit.stdout.is_empty());
    assert_eq!(
        String::from_utf8(edit.stderr).unwrap().trim(),
        "conversation_note_changed"
    );
    assert_eq!(f.raw(), before, "stale edit restored the question marker");
    f.reject("question-answer", "--answer-file", &a_file);
    assert_eq!(f.read()["items"][0]["note"], "データ到着まで停止");

    // An exact current note may still be edited without changing any holds.
    let current = sha("データ到着まで停止".as_bytes());
    fs::write(&stale_note, "データ到着まで停止\nprivate reply reference").unwrap();
    let accepted = f
        .command(&[
            "edit",
            "1",
            "--note-file",
            stale_note.to_str().unwrap(),
            "--expected-note-sha256",
            &current,
        ])
        .output()
        .unwrap();
    assert!(accepted.status.success());
    assert_eq!(f.read()["items"][0]["status"], "block");
    assert_eq!(f.read()["items"][0][WAIT], parked["items"][0][WAIT]);
}

#[test]
fn canonical_commands_validate_fixture_notify_guild_owner_and_application_bot_exactly() {
    let f = Fixture::new();
    let config = f.temp.path().join("notify-config.json");
    let q = f.file("question.json", &question());
    let valid = json!({"guild_id":"100","owner_user_id":"101","application_id":"102"});
    for key in [
        "guild_id",
        "owner_user_id",
        "application_id",
        "bot_id",
        "bot_user_id",
    ] {
        let mut invalid = valid.clone();
        invalid[key] = json!("999");
        fs::write(&config, serde_json::to_vec(&invalid).unwrap()).unwrap();
        f.reject("question-wait", "--question-file", &q);
    }
    fs::write(&config, serde_json::to_vec(&valid).unwrap()).unwrap();
    let (_, a_file, _) = f.park();
    for key in [
        "guild_id",
        "owner_user_id",
        "application_id",
        "bot_id",
        "bot_user_id",
    ] {
        let mut invalid = valid.clone();
        invalid[key] = json!("999");
        fs::write(&config, serde_json::to_vec(&invalid).unwrap()).unwrap();
        f.reject("question-answer", "--answer-file", &a_file);
    }
    fs::write(&config, b"{").unwrap();
    f.reject("question-answer", "--answer-file", &a_file);
    fs::write(&config, b"{}").unwrap();
    f.reject("question-answer", "--answer-file", &a_file);
    fs::remove_file(&config).unwrap();
    f.reject("question-answer", "--answer-file", &a_file);
    fs::write(&config, serde_json::to_vec(&valid).unwrap()).unwrap();
    f.ok("question-answer", "--answer-file", &a_file);
}

#[test]
fn answer_never_overrides_new_stops_waits_or_changed_block_ownership() {
    let f = Fixture::new();
    let (_, a_file, _) = f.park();
    let parked = f.read();
    for (key, value) in holds() {
        let mut state = parked.clone();
        state["items"][0][key] = value;
        f.write(&state);
        f.reject("question-answer", "--answer-file", &a_file);
    }
    for status in ["todo", "doing", "done", "wait", "recovery_unverified"] {
        let mut state = parked.clone();
        state["items"][0]["status"] = json!(status);
        f.write(&state);
        f.reject("question-answer", "--answer-file", &a_file);
    }
    f.write(&parked);
    let blocked = f
        .command(&["block", "1", "本人の指示で停止"])
        .output()
        .unwrap();
    assert!(blocked.status.success());
    f.reject("question-answer", "--answer-file", &a_file);
    for note in ["another block", "prefix without the signature", ""] {
        let mut state = parked.clone();
        state["items"][0]["note"] = json!(note);
        f.write(&state);
        f.reject("question-answer", "--answer-file", &a_file);
    }
    let mut state = parked.clone();
    state["items"][0]["note"] = json!(format!(
        "{}\n{}",
        parked["items"][0]["note"].as_str().unwrap(),
        parked["items"][0][WAIT]["block_note"].as_str().unwrap()
    ));
    f.write(&state);
    f.reject("question-answer", "--answer-file", &a_file);
}

#[test]
fn finished_prerequisites_remain_intact_but_missing_or_duplicate_targets_do_not_pass() {
    let f = Fixture::new();
    let mut state = f.read();
    state["items"][0]["status"] = json!("todo");
    state["items"][0]["wait_on"] = json!([2]);
    state["items"][1]["status"] = json!("done");
    let before = state.clone();
    f.write(&state);
    let (_, a_file, _) = f.park();
    let mut duplicate = f.read();
    duplicate["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":2,"status":"done"}));
    f.write(&duplicate);
    f.reject("question-answer", "--answer-file", &a_file);
    duplicate["items"].as_array_mut().unwrap().pop();
    f.write(&duplicate);
    f.ok("question-answer", "--answer-file", &a_file);
    assert_eq!(f.read()["items"][0]["wait_on"], json!([2]));
    assert_work_preserved(&before, &f.read());
}

#[test]
fn foreign_owner_scope_hash_reply_source_and_timestamp_mismatches_are_rejected() {
    let f = Fixture::new();
    let (_, _, a) = f.park();
    for (key, value) in [
        ("author_id", json!("999")),
        ("todo_id", json!("2")),
        ("thread_id", json!("300")),
        ("question_message_id", json!("300")),
        ("request_id", json!("foreign")),
        ("question_fingerprint", json!("d".repeat(64))),
        ("question_receipt_sha256", json!("d".repeat(64))),
        ("source", json!("html")),
        ("acquired_at", json!("2999-01-01T00:00:00Z")),
        ("acquired_at", json!("2026-10-13T00:59:59Z")),
        ("acquired_at", json!("invalid")),
        ("acquired_at", json!("2026-10-13 01:00:01")),
        ("message_id", json!("200")),
        ("message_id", json!("199")),
        ("reply_sha256", json!("B".repeat(64))),
        ("receipt_file_sha256", json!("not-a-hash")),
        ("domain", json!("discord-conversation-answer-v2")),
        ("unknown", json!("private reply")),
    ] {
        let mut invalid = a.clone();
        invalid[key] = value;
        f.reject(
            "question-answer",
            "--answer-file",
            &f.file("invalid-answer.json", &invalid),
        );
    }
    for key in a.as_object().unwrap().keys() {
        let mut invalid = a.clone();
        invalid.as_object_mut().unwrap().remove(key);
        f.reject(
            "question-answer",
            "--answer-file",
            &f.file("missing-answer.json", &invalid),
        );
    }
}

#[test]
fn invalid_question_schema_duplicate_keys_and_unbounded_identity_are_rejected() {
    let f = Fixture::new();
    let q = question();
    for (key, value) in [
        ("domain", json!("discord-conversation-question-v2")),
        ("request_id", json!("")),
        ("request_id", json!("a".repeat(97))),
        ("request_id", json!("illegal/request")),
        ("request_id", json!("質問")),
        ("todo_id", json!(1)),
        ("guild_id", json!("0")),
        ("guild_id", json!("0100")),
        ("bot_id", json!("18446744073709551616")),
        ("owner_id", json!("-1")),
        ("owner_id", json!("+101")),
        ("owner_id", json!(" 101")),
        ("bot_id", json!("101")),
        ("question_message_id", json!("201")),
        ("progress_thread_id", json!("200")),
        ("question_fingerprint", json!("A".repeat(64))),
        ("question_fingerprint", json!("a".repeat(63))),
        ("created_at", json!("2999-01-01T00:00:00Z")),
        ("created_at", json!("not-a-date")),
        ("unknown", json!("private reply")),
    ] {
        let mut invalid = q.clone();
        invalid[key] = value;
        f.reject(
            "question-wait",
            "--question-file",
            &f.file("invalid-question.json", &invalid),
        );
    }
    for key in q.as_object().unwrap().keys() {
        let mut invalid = q.clone();
        invalid.as_object_mut().unwrap().remove(key);
        f.reject(
            "question-wait",
            "--question-file",
            &f.file("missing-question.json", &invalid),
        );
    }
    let file = f.temp.path().join("raw.json");
    let normal = serde_json::to_string(&q).unwrap();
    for raw in [
        format!("{{\"owner_id\":\"999\",{}", &normal[1..]),
        format!("{{\"owner_\\u0069d\":\"999\",{}", &normal[1..]),
        format!("{normal} {{}}"),
        "{".into(),
        "[]".into(),
        "null".into(),
        "".into(),
    ] {
        fs::write(&file, raw).unwrap();
        f.reject("question-wait", "--question-file", &file);
    }
    f.park();
    let a: Value =
        serde_json::from_slice(&fs::read(f.temp.path().join("answer.json")).unwrap()).unwrap();
    let raw = serde_json::to_string(&a).unwrap();
    fs::write(&file, format!("{{\"author_id\":\"999\",{}", &raw[1..])).unwrap();
    f.reject("question-answer", "--answer-file", &file);
}

#[test]
fn same_request_or_question_identity_cannot_be_assigned_to_another_todo() {
    for conflict in ["request", "thread"] {
        let f = Fixture::new();
        f.park();
        let mut q = question();
        q["todo_id"] = json!("2");
        if conflict == "request" {
            q["thread_id"] = json!("300");
            q["question_message_id"] = json!("300");
        } else {
            q["request_id"] = json!("different-request");
        }
        let p = f.file("second.json", &q);
        let before = f.raw();
        let out = f
            .command(&["question-wait", "2", "--question-file", p.to_str().unwrap()])
            .output()
            .unwrap();
        assert!(!out.status.success());
        assert_eq!(f.raw(), before);
    }
}

#[test]
fn corrupt_wait_receipt_or_duplicate_selected_row_is_rejected_atomically() {
    let f = Fixture::new();
    let (_, a_file, _) = f.park();
    let parked = f.read();
    for wait in [
        Value::Null,
        json!({}),
        json!("bad"),
        {
            let mut w = parked["items"][0][WAIT].clone();
            w["question_receipt_sha256"] = json!("d".repeat(64));
            w
        },
        {
            let mut w = parked["items"][0][WAIT].clone();
            w["question"]["owner_id"] = json!("999");
            w
        },
        {
            let mut w = parked["items"][0][WAIT].clone();
            w["blocked_at"] = json!("2999-01-01T00:00:00Z");
            w
        },
        {
            let mut w = parked["items"][0][WAIT].clone();
            w["unknown"] = json!(true);
            w
        },
    ] {
        let mut state = parked.clone();
        state["items"][0][WAIT] = wait;
        f.write(&state);
        f.reject("question-answer", "--answer-file", &a_file);
    }
    let mut state = parked.clone();
    state["items"]
        .as_array_mut()
        .unwrap()
        .push(parked["items"][0].clone());
    f.write(&state);
    f.reject("question-answer", "--answer-file", &a_file);
    f.write(&parked);
    f.ok("question-answer", "--answer-file", &a_file);
    let applied = f.read();
    for value in [json!({}), json!([{}]), json!("bad")] {
        let mut state = applied.clone();
        state["items"][0][ANSWERS] = value;
        f.write(&state);
        f.reject("question-answer", "--answer-file", &a_file);
    }
}

#[test]
fn no_note_null_note_empty_note_and_trailing_newlines_round_trip() {
    for note in [
        None,
        Some(Value::Null),
        Some(json!("")),
        Some(json!("saved\n")),
        Some(json!("saved\n\n")),
    ] {
        let f = Fixture::new();
        let mut db = f.read();
        match &note {
            None => {
                db["items"][0].as_object_mut().unwrap().remove("note");
            }
            Some(v) => db["items"][0]["note"] = v.clone(),
        }
        f.write(&db);
        let (_, a_file, _) = f.park();
        f.ok("question-answer", "--answer-file", &a_file);
        assert_eq!(f.read()["items"][0].get("note"), note.as_ref());
    }
}

#[test]
fn missing_nonregular_and_oversized_envelopes_are_rejected_without_reading_text() {
    let f = Fixture::new();
    let missing = f.temp.path().join("missing-private-reply.json");
    let large = f.temp.path().join("large.json");
    fs::write(&large, vec![b' '; 1024 * 1024 + 1]).unwrap();
    for p in [&missing, f.temp.path(), &large] {
        f.reject("question-wait", "--question-file", p);
    }
    f.park();
    for p in [&missing, f.temp.path(), &large] {
        f.reject("question-answer", "--answer-file", p);
    }
}

#[cfg(any(unix, windows))]
#[test]
fn symlink_envelopes_and_symlink_parents_are_rejected() {
    let f = Fixture::new();
    let q = f.file("real.json", &question());
    let link = f.temp.path().join("link.json");
    #[cfg(unix)]
    std::os::unix::fs::symlink(&q, &link).unwrap();
    #[cfg(windows)]
    std::os::windows::fs::symlink_file(&q, &link).unwrap();
    f.reject("question-wait", "--question-file", &link);
    let dir = f.temp.path().join("linked");
    #[cfg(unix)]
    std::os::unix::fs::symlink(f.temp.path().join("specs"), &dir).unwrap();
    #[cfg(windows)]
    std::os::windows::fs::symlink_dir(f.temp.path().join("specs"), &dir).unwrap();
    fs::copy(&q, f.temp.path().join("specs/question.json")).unwrap();
    f.reject(
        "question-wait",
        "--question-file",
        &dir.join("question.json"),
    );
    let (_, a_file, _) = f.park();
    let a_link = f.temp.path().join("answer-link.json");
    #[cfg(unix)]
    std::os::unix::fs::symlink(&a_file, &a_link).unwrap();
    #[cfg(windows)]
    std::os::windows::fs::symlink_file(&a_file, &a_link).unwrap();
    f.reject("question-answer", "--answer-file", &a_link);
}

#[test]
fn real_canonical_lock_defers_registration_until_the_latest_stop_can_be_seen() {
    let f = Fixture::new();
    let q = f.file("question.json", &question());
    let lock = Lock::acquire(&f.paths.lock, Duration::ZERO).unwrap();
    let mut child = f.command(&["question-wait", "1", "--question-file", q.to_str().unwrap()]);
    child
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped());
    let child = child.spawn().unwrap();
    let mut stopped = f.read();
    stopped["items"][0]["stopped"] = json!(true);
    f.write(&stopped);
    let before = f.raw();
    drop(lock);
    let out = child.wait_with_output().unwrap();
    assert!(!out.status.success());
    assert_eq!(f.raw(), before);
}

#[test]
fn real_canonical_lock_defers_answer_until_a_new_dependency_has_been_saved() {
    let f = Fixture::new();
    let (_, a_file, _) = f.park();
    let lock = Lock::acquire(&f.paths.lock, Duration::ZERO).unwrap();
    let mut child = f.command(&[
        "question-answer",
        "1",
        "--answer-file",
        a_file.to_str().unwrap(),
    ]);
    child
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped());
    let child = child.spawn().unwrap();
    let mut waiting = f.read();
    waiting["items"][0]["wait_on"] = json!([2]);
    f.write(&waiting);
    let before = f.raw();
    drop(lock);
    let out = child.wait_with_output().unwrap();
    assert!(!out.status.success());
    assert_eq!(f.raw(), before);
}

#[test]
fn corrupt_canonical_store_is_not_reconstructed_from_an_envelope() {
    let f = Fixture::new();
    let q = f.file("question.json", &question());
    fs::write(&f.paths.store, b"{").unwrap();
    let before = f.raw();
    let out = f.call("question-wait", "--question-file", &q);
    assert!(!out.status.success());
    assert_eq!(f.raw(), before);
}

impl Fixture {
    fn withdraw_ready(&self) {
        let mut db = self.read();
        db["items"][0]["worker_receipt"]["generation"] = json!(7);
        self.write(&db);
        self.park();
        let dir = self.paths.jobs.join("original-worker");
        fs::create_dir(&dir).unwrap();
        fs::write(
            dir.join("job.json"),
            serde_json::to_vec(&json!({
                "id":"original-worker","status":"succeeded","todo_id":1,
                "launch_key":"original-key","created_at":"2023-11-15T07:13:20+09:00","cwd":"saved-worktree"
            }))
            .unwrap(),
        )
        .unwrap();
        fs::write(
            self.temp.path().join("withdrawal-reason.txt"),
            "既存許可のある通常読取を再確認したため取り下げる",
        )
        .unwrap();
    }
    fn snapshot(&self) {
        self.file("withdrawal-origin.json", &self.read()["items"][0]);
    }
    fn withdraw(&self) -> Output {
        self.command(&[
            "question-withdraw",
            "1",
            "--question-file",
            self.temp.path().join("question.json").to_str().unwrap(),
            "--reason-file",
            self.temp
                .path()
                .join("withdrawal-reason.txt")
                .to_str()
                .unwrap(),
            "--expected-state-file",
            self.temp
                .path()
                .join("withdrawal-origin.json")
                .to_str()
                .unwrap(),
        ])
        .output()
        .unwrap()
    }
    fn withdraw_rejected(&self) {
        let before = self.raw();
        let result = self.withdraw();
        assert!(!result.status.success(), "invalid withdrawal accepted");
        assert!(result.stdout.is_empty());
        assert!(String::from_utf8_lossy(&result.stderr).starts_with("conversation_"));
        assert_eq!(
            self.raw(),
            before,
            "rejected withdrawal changed the canonical store"
        );
    }
}

#[test]
fn withdrawal_retires_exact_owner_preserves_deliveries_and_never_creates_an_answer() {
    let f = Fixture::new();
    f.withdraw_ready();
    let mut before = f.read();
    let original_job = fs::read(f.paths.jobs.join("original-worker/job.json")).unwrap();
    before["items"][0]["note"] = json!(format!(
        "{}\n別の配送文を保持",
        before["items"][0]["note"].as_str().unwrap()
    ));
    f.write(&before);
    f.snapshot();
    let result = f.withdraw();
    assert!(
        result.status.success(),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
    let public: Value = serde_json::from_slice(&result.stdout).unwrap();
    assert_eq!(public["withdrawn"], true);
    assert_eq!(public["answered"], false);
    let after = f.read();
    let row = &after["items"][0];
    assert_eq!(row["status"], "todo");
    assert_eq!(row["resume_generation"], 8);
    assert!(row["scheduler"]["key"].is_null());
    assert!(row["scheduler"]["job"].is_null());
    assert_eq!(row["scheduler"]["last_job"], "original-worker");
    assert_eq!(
        row["processed_results"]["original-worker"]["reason"],
        "conversation_withdrawal"
    );
    assert!(row[WAIT].is_null());
    assert!(row.get(ANSWERS).is_none());
    let receipt = &row["owner_question_withdrawal_receipts"][0];
    assert_eq!(receipt["domain"], "todo-question-withdrawal-receipt-v1");
    assert_eq!(receipt["origin"]["resume_generation"], 7);
    assert!(receipt.get("answer").is_none());
    assert_eq!(row["note"], "元のメモ\n引き継ぐ途中成果\n別の配送文を保持");
    for key in [
        "spec",
        "jobs",
        "needs",
        "progress",
        "other_item",
        "stall_recovery",
    ] {
        assert_eq!(row[key], before["items"][0][key]);
    }
    for key in ["worktree", "branch", "selection", "not_before"] {
        assert_eq!(row["scheduler"][key], before["items"][0]["scheduler"][key]);
    }
    assert_eq!(
        fs::read(f.paths.jobs.join("original-worker/job.json")).unwrap(),
        original_job
    );
    let mut later = after;
    later["items"][0]["status"] = json!("block");
    later["items"][0]["stopped"] = json!(true);
    later["items"][0]["note"] = json!("別の停止");
    later["items"][0]["scheduler"]["key"] = json!("later-key");
    f.write(&later);
    let before_retry = f.raw();
    let replay = f.withdraw();
    assert!(replay.status.success());
    let replay: Value = serde_json::from_slice(&replay.stdout).unwrap();
    assert_eq!(replay["changed"], false);
    assert_eq!(replay["resumed"], false);
    assert_eq!(f.raw(), before_retry);
    f.reject(
        "question-answer",
        "--answer-file",
        &f.temp.path().join("answer.json"),
    );
    f.ok(
        "question-wait",
        "--question-file",
        &f.temp.path().join("question.json"),
    );
    assert_eq!(
        f.raw(),
        before_retry,
        "registration revived the withdrawn wait"
    );
    fs::write(f.temp.path().join("withdrawal-reason.txt"), "別の理由").unwrap();
    f.withdraw_rejected();
}

#[test]
fn withdrawal_cas_and_current_holds_preserve_stops_waits_other_notes_and_generation() {
    let f = Fixture::new();
    f.withdraw_ready();
    f.snapshot();
    let parked = f.read();
    for (key, value) in holds().into_iter().chain([
        ("status", json!("done")),
        ("status", json!("wait")),
        ("resume_generation", json!(8)),
        ("note", json!("別の停止")),
        ("worker_receipt", json!({"job":"another","generation":7})),
        ("jobs", json!(["original-worker", "another"])),
    ]) {
        let mut changed = parked.clone();
        changed["items"][0][key] = value;
        f.write(&changed);
        f.withdraw_rejected();
    }
    for (key, value) in holds() {
        let mut changed = parked.clone();
        changed["items"][0][key] = value;
        f.write(&changed);
        f.snapshot();
        f.withdraw_rejected();
    }
    let mut changed = parked.clone();
    changed["items"][0]["resume_generation"] = json!(8);
    changed["items"][0]["worker_receipt"]["generation"] = json!(8);
    f.write(&changed);
    f.snapshot();
    f.withdraw_rejected(); // registering generation is immutable, even with a new snapshot
    let mut changed = parked.clone();
    changed["items"][0]["note"] = json!(format!(
        "別の停止\n{}",
        parked["items"][0]["note"].as_str().unwrap()
    ));
    f.write(&changed);
    f.snapshot();
    f.withdraw_rejected();
}

#[test]
fn withdrawal_requires_real_original_terminal_owner_and_exact_question_bytes() {
    let f = Fixture::new();
    f.withdraw_ready();
    f.snapshot();
    let job_file = f.paths.jobs.join("original-worker/job.json");
    let valid: Value = serde_json::from_slice(&fs::read(&job_file).unwrap()).unwrap();
    for (key, value) in [
        ("id", json!("another")),
        ("todo_id", json!(2)),
        ("launch_key", json!("later-key")),
        ("status", json!("running")),
        ("status", json!("queued")),
        ("status", json!("cancelled")),
        ("created_at", json!(32_503_680_000_u64)),
        ("created_at", Value::Null),
        ("stopped", json!(true)),
        ("cancel_requested", json!(true)),
    ] {
        let mut job = valid.clone();
        job[key] = value;
        fs::write(&job_file, serde_json::to_vec(&job).unwrap()).unwrap();
        f.withdraw_rejected();
    }
    fs::remove_file(&job_file).unwrap();
    f.withdraw_rejected();
    fs::write(&job_file, serde_json::to_vec(&valid).unwrap()).unwrap();
    let q_file = f.temp.path().join("question.json");
    fs::write(&q_file, serde_json::to_string_pretty(&question()).unwrap()).unwrap();
    f.withdraw_rejected(); // same JSON identity, different exact receipt bytes
    fs::write(&q_file, serde_json::to_vec(&question()).unwrap()).unwrap();
    let mut legacy = f.read();
    legacy["items"][0][WAIT]
        .as_object_mut()
        .unwrap()
        .remove("origin");
    f.write(&legacy);
    f.snapshot();
    assert!(
        f.withdraw().status.success(),
        "existing v1 waits must remain withdrawable"
    );
}

#[test]
fn withdrawal_compares_real_job_rfc3339_times_and_legacy_epochs_to_question() {
    for (created, accepted) in [
        (json!("2023-11-15T07:13:20+09:00"), true),
        (json!(1_700_000_000.), true),
        (json!("2026-10-13T10:00:00+09:00"), true),
        (json!("2026-10-13T10:00:00.000000001+09:00"), false),
        (json!("2026-10-13T02:00:00Z"), false),
        (json!("2099-01-01T00:00:00Z"), false),
        (json!("1700000000"), false),
        (json!("invalid"), false),
        (json!("1970-01-01T00:00:00Z"), false),
        (json!(-1), false),
        (json!(false), false),
        (Value::Null, false),
    ] {
        let f = Fixture::new();
        f.withdraw_ready();
        f.snapshot();
        let job_file = f.paths.jobs.join("original-worker/job.json");
        let mut job: Value = serde_json::from_slice(&fs::read(&job_file).unwrap()).unwrap();
        job["created_at"] = created.clone();
        fs::write(&job_file, serde_json::to_vec(&job).unwrap()).unwrap();
        if accepted {
            let out = f.withdraw();
            assert!(
                out.status.success(),
                "{created}: {}",
                String::from_utf8_lossy(&out.stderr)
            );
        } else {
            f.withdraw_rejected();
        }
    }
}

#[test]
fn withdrawal_reason_and_snapshot_files_are_bounded_and_failure_never_writes_state() {
    let f = Fixture::new();
    f.withdraw_ready();
    f.snapshot();
    let reason = f.temp.path().join("withdrawal-reason.txt");
    for data in [
        b" ".to_vec(),
        vec![0xff],
        b"reason\0hidden".to_vec(),
        vec![b'a'; 1024 * 1024 + 1],
    ] {
        fs::write(&reason, data).unwrap();
        f.withdraw_rejected();
    }
    fs::write(&reason, "既存許可による実行へ戻す").unwrap();
    for data in [b"{".to_vec(), b"null".to_vec(), vec![b' '; 1024 * 1024 + 1]] {
        fs::write(f.temp.path().join("withdrawal-origin.json"), data).unwrap();
        f.withdraw_rejected();
    }
}

#[test]
fn withdrawal_lock_observes_a_new_note_instead_of_erasing_another_delivery() {
    let f = Fixture::new();
    f.withdraw_ready();
    f.snapshot();
    let lock = Lock::acquire(&f.paths.lock, Duration::ZERO).unwrap();
    let mut command = f.command(&[
        "question-withdraw",
        "1",
        "--question-file",
        f.temp.path().join("question.json").to_str().unwrap(),
        "--reason-file",
        f.temp
            .path()
            .join("withdrawal-reason.txt")
            .to_str()
            .unwrap(),
        "--expected-state-file",
        f.temp
            .path()
            .join("withdrawal-origin.json")
            .to_str()
            .unwrap(),
    ]);
    command
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped());
    let child = command.spawn().unwrap();
    let mut changed = f.read();
    changed["items"][0]["note"] = json!(format!(
        "{}\n新しい配送文",
        changed["items"][0]["note"].as_str().unwrap()
    ));
    f.write(&changed);
    let before = f.raw();
    drop(lock);
    let result = child.wait_with_output().unwrap();
    assert!(!result.status.success());
    assert_eq!(f.raw(), before);
}

#[test]
fn withdrawal_accepts_only_the_same_owner_generation_scheduler_imported_block_note() {
    let f = Fixture::new();
    let original_note = "作業が残っています。資源の空きを確認して自動で続けます。";
    let mut db = f.read();
    db["items"][0]["note"] = json!(original_note);
    f.write(&db);
    f.withdraw_ready();
    let mut imported = f.read();
    imported["items"][0]["note"] = json!("ジョブの結果に判断が必要です。");
    imported["items"][0]["scheduler"]["phase"] = json!("finished");
    imported["items"][0]["processed_results"] =
        json!({"original-worker":{"generation":7,"reason":"block"}});
    // This is the reported runtime schema: exact wait and original marker in
    // block_note, but the scheduler's ordinary result-import note is current.
    assert!(
        imported["items"][0][WAIT]["block_note"]
            .as_str()
            .unwrap()
            .starts_with(original_note)
    );
    for change in [
        "no_result",
        "wrong_reason",
        "old_generation",
        "other_owner",
        "phase",
        "another_note",
        "appended_stop",
    ] {
        let mut db = imported.clone();
        match change {
            "no_result" => {
                db["items"][0]
                    .as_object_mut()
                    .unwrap()
                    .remove("processed_results");
            }
            "wrong_reason" => {
                db["items"][0]["processed_results"]["original-worker"]["reason"] = json!("done")
            }
            "old_generation" => {
                db["items"][0]["processed_results"]["original-worker"]["generation"] = json!(6)
            }
            "other_owner" => {
                db["items"][0]["processed_results"] =
                    json!({"another":{"generation":7,"reason":"block"}})
            }
            "phase" => db["items"][0]["scheduler"]["phase"] = json!("running"),
            "another_note" => db["items"][0]["note"] = json!("新しい本人指示で停止"),
            "appended_stop" => {
                db["items"][0]["note"] = json!("ジョブの結果に判断が必要です。\n別の停止条件")
            }
            _ => unreachable!(),
        }
        f.write(&db);
        f.snapshot();
        f.withdraw_rejected();
    }
    for (key, value) in holds() {
        let mut db = imported.clone();
        db["items"][0][key] = value;
        f.write(&db);
        f.snapshot();
        f.withdraw_rejected();
    }
    f.write(&imported);
    f.snapshot();
    let result = f.withdraw();
    assert!(
        result.status.success(),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
    let after = f.read();
    let row = &after["items"][0];
    assert_eq!(row["status"], "todo");
    assert_eq!(row["note"], original_note);
    assert!(row.get(ANSWERS).is_none());
    assert!(row[WAIT].is_null());
    assert_eq!(
        row["processed_results"],
        imported["items"][0]["processed_results"]
    );
    assert_eq!(
        row["owner_question_withdrawal_receipts"][0]["release_kind"],
        "scheduler_block_result"
    );
    assert_eq!(
        row["owner_question_withdrawal_receipts"][0]["released_note"],
        "ジョブの結果に判断が必要です。"
    );
    for key in ["jobs", "spec", "progress", "stall_recovery"] {
        assert_eq!(row[key], imported["items"][0][key]);
    }
    let before = f.raw();
    assert!(f.withdraw().status.success());
    assert_eq!(f.raw(), before);
}

#[test]
fn withdrawal_accepts_retired_legacy_worker_only_with_exact_import_evidence() {
    let f = Fixture::new();
    f.withdraw_ready();
    let mut legacy = f.read();
    let row = &mut legacy["items"][0];
    row[WAIT].as_object_mut().unwrap().remove("origin");
    row["worker_receipt"] = json!({"job":null,"generation":7});
    row["scheduler"]["phase"] = json!("finished");
    row["note"] = json!("ジョブの結果に判断が必要です。");
    row["processed_results"] = json!({"original-worker":{"generation":7,"reason":"block"}});
    let original_job = fs::read(f.paths.jobs.join("original-worker/job.json")).unwrap();
    for change in [
        "modern",
        "missing_worker",
        "missing_job",
        "foreign_worker",
        "worker_generation",
        "generation",
        "missing_result",
        "result_generation",
        "result_reason",
        "phase",
        "key",
        "history",
        "marker",
        "new_note",
    ] {
        let mut db = legacy.clone();
        let row = &mut db["items"][0];
        match change {
            "modern" => {
                row[WAIT]["origin"] = json!({"resume_generation":7,
                "worker_receipt":row["worker_receipt"],"job":"original-worker","key":"original-key"})
            }
            "missing_worker" => {
                row.as_object_mut().unwrap().remove("worker_receipt");
            }
            "missing_job" => {
                row["worker_receipt"].as_object_mut().unwrap().remove("job");
            }
            "foreign_worker" => row["worker_receipt"]["job"] = json!("another"),
            "worker_generation" => row["worker_receipt"]["generation"] = json!(6),
            "generation" => row["resume_generation"] = json!(8),
            "missing_result" => {
                row.as_object_mut().unwrap().remove("processed_results");
            }
            "result_generation" => {
                row["processed_results"]["original-worker"]["generation"] = json!(6)
            }
            "result_reason" => {
                row["processed_results"]["original-worker"]["reason"] = json!("done")
            }
            "phase" => row["scheduler"]["phase"] = json!("running"),
            "key" => row["scheduler"]["key"] = json!("another"),
            "history" => row["jobs"] = json!(["original-worker", "another"]),
            "marker" => row["note"] = row[WAIT]["block_note"].clone(),
            "new_note" => row["note"] = json!("ジョブの結果に判断が必要です。\n本人による停止"),
            _ => unreachable!(),
        }
        f.write(&db);
        f.snapshot();
        f.withdraw_rejected();
    }
    for (key, value) in holds() {
        let mut db = legacy.clone();
        db["items"][0][key] = value;
        f.write(&db);
        f.snapshot();
        f.withdraw_rejected();
    }
    for (key, value) in [
        ("status", json!("running")),
        ("status", json!("failed")),
        ("launch_key", json!("another")),
        ("todo_id", json!(2)),
        ("created_at", json!(2_000_000_000.)),
    ] {
        let mut job: Value = serde_json::from_slice(&original_job).unwrap();
        job[key] = value;
        f.file("jobs/original-worker/job.json", &job);
        f.write(&legacy);
        f.snapshot();
        f.withdraw_rejected();
    }
    fs::write(f.paths.jobs.join("original-worker/job.json"), &original_job).unwrap();
    f.write(&legacy);
    f.snapshot();
    let out = f.withdraw();
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let after = f.read();
    let row = &after["items"][0];
    assert_eq!(row["status"], "todo");
    assert_eq!(row["note"], legacy["items"][0][WAIT]["previous_note"]);
    assert_eq!(row["resume_generation"], 8);
    assert_eq!(row["worker_receipt"], json!({"job":null,"generation":8}));
    assert!(row[WAIT].is_null());
    assert!(row[ANSWERS].is_null());
    let receipt = &row["owner_question_withdrawal_receipts"][0];
    assert_eq!(
        receipt["origin"]["worker_receipt"],
        legacy["items"][0]["worker_receipt"]
    );
    assert_eq!(receipt["release_kind"], "scheduler_block_result");
    for key in ["jobs", "spec", "progress", "processed_results"] {
        assert_eq!(row[key], legacy["items"][0][key]);
    }
    assert_eq!(
        fs::read(f.paths.jobs.join("original-worker/job.json")).unwrap(),
        original_job
    );
    let before = f.raw();
    assert!(f.withdraw().status.success()); // receipt validation accepts actual null origin
    assert_eq!(before, f.raw());
    for change in ["modern", "marker", "missing_job"] {
        let mut db = after.clone();
        let receipt = &mut db["items"][0]["owner_question_withdrawal_receipts"][0];
        match change {
            "modern" => {
                receipt["wait"]["origin"] = json!({"resume_generation":7,
                "worker_receipt":receipt["origin"]["worker_receipt"],"job":"original-worker","key":"original-key"})
            }
            "marker" => receipt["release_kind"] = json!("question_marker"),
            "missing_job" => {
                receipt["origin"]["worker_receipt"]
                    .as_object_mut()
                    .unwrap()
                    .remove("job");
            }
            _ => unreachable!(),
        }
        f.write(&db);
        f.withdraw_rejected();
    }
}

#[test]
fn imported_answer_delivers_atomically_only_for_the_proven_finished_owner() {
    let f = Fixture::new();
    f.withdraw_ready();
    let mut legacy = f.read();
    let row = &mut legacy["items"][0];
    row[WAIT].as_object_mut().unwrap().remove("origin");
    row["worker_receipt"] = json!({"job":null,"generation":7});
    row["scheduler"]["phase"] = json!("finished");
    row["note"] = json!("ジョブの結果に判断が必要です。");
    row["processed_results"] = json!({"original-worker":{"generation":7,"reason":"block"}});
    let note_file = f.temp.path().join("reply-note.txt");
    let delivery = "ジョブの結果に判断が必要です。\n\nconversation-owner-reply:synthetic\nprivate reply reference";
    fs::write(&note_file, delivery).unwrap();
    let apply = || {
        f.command(&[
            "question-answer",
            "1",
            "--answer-file",
            f.temp.path().join("answer.json").to_str().unwrap(),
            "--delivery-note-file",
            note_file.to_str().unwrap(),
            "--expected-state-file",
            f.temp
                .path()
                .join("withdrawal-origin.json")
                .to_str()
                .unwrap(),
        ])
        .output()
        .unwrap()
    };
    for change in [
        "worker",
        "generation",
        "result",
        "key",
        "history",
        "modern",
        "note",
        "cas",
    ] {
        let mut db = legacy.clone();
        let row = &mut db["items"][0];
        match change {
            "worker" => row["worker_receipt"]["job"] = json!("another"),
            "generation" => row["resume_generation"] = json!(8),
            "result" => row["processed_results"]["original-worker"]["reason"] = json!("done"),
            "key" => row["scheduler"]["key"] = json!("another"),
            "history" => row["jobs"] = json!(["original-worker", "another"]),
            "modern" => {
                row[WAIT]["origin"] = json!({"resume_generation":7,
                "worker_receipt":row["worker_receipt"],"job":"original-worker","key":"original-key"})
            }
            "note" => row["note"] = json!("別の停止"),
            "cas" => (),
            _ => unreachable!(),
        }
        f.write(&db);
        f.snapshot();
        if change == "cas" {
            db["items"][0]["progress"] = json!(41);
            f.write(&db);
        }
        let before = f.raw();
        assert!(!apply().status.success(), "accepted {change}");
        assert_eq!(f.raw(), before);
    }
    for (key, value) in holds() {
        let mut db = legacy.clone();
        db["items"][0][key] = value;
        f.write(&db);
        f.snapshot();
        let before = f.raw();
        assert!(!apply().status.success());
        assert_eq!(f.raw(), before);
    }
    f.write(&legacy);
    f.snapshot();
    for bad in [
        "",
        "another\n\nreply",
        "ジョブの結果に判断が必要です。\n\n",
        "ジョブの結果に判断が必要です。\n\nreply\0",
    ] {
        fs::write(&note_file, bad).unwrap();
        let before = f.raw();
        assert!(!apply().status.success());
        assert_eq!(f.raw(), before);
    }
    fs::write(&note_file, delivery).unwrap();
    let out = apply();
    assert!(
        out.status.success(),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let after = f.read();
    let row = &after["items"][0];
    assert_eq!(row["status"], "todo");
    assert_eq!(
        row["note"],
        format!(
            "{}\n\nconversation-owner-reply:synthetic\nprivate reply reference",
            legacy["items"][0][WAIT]["previous_note"].as_str().unwrap()
        )
    );
    assert!(row[WAIT].is_null());
    assert_eq!(row[ANSWERS].as_array().unwrap().len(), 1);
    assert!(row["owner_question_withdrawal_receipts"].is_null());
    assert_eq!(row["resume_generation"], 8);
    assert!(row["scheduler"]["job"].is_null());
    for key in ["spec", "jobs", "progress", "processed_results"] {
        assert_eq!(row[key], legacy["items"][0][key]);
    }
    let before = f.raw();
    assert!(apply().status.success());
    assert_eq!(f.raw(), before);
}
