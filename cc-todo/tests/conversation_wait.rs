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
        br#"{"id":"original-worker","status":"succeeded"}"#,
    )
    .unwrap();
    f.ok("question-answer", "--answer-file", &a_file);
    assert_work_preserved(&base, &f.read());
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
