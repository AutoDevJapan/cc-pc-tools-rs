mod common;
use cc_ask::{automation::*, core::*, patrol, routing};
use common::{message, Harness};
use serde_json::{json, Value};
use std::{
    panic::{catch_unwind, AssertUnwindSafe},
    path::PathBuf,
};

fn cursor(floor: u64) -> Value {
    json!({"messages":{},"cursor":{"since":floor.to_string(),"places":{},"channels":{}}})
}
fn setup() -> Harness {
    let mut h = Harness::new();
    h.ctx.cfg["task_state_root"] = json!(h.root.path().join("tasks"));
    std::fs::create_dir_all(h.root.path().join("tasks")).unwrap();
    h
}
#[test]
fn independent_cursor_fetches_all_205_messages_and_preserves_failed_place() {
    let mut h = setup();
    let mut ledger = cursor(100);
    {
        let mut f = h.fake.borrow_mut();
        for id in ["A", "B"] {
            f.threads.insert(
                id.into(),
                json!({"id":id,"type":11,"parent_id":"T","owner_id":"BOT","name":id}),
            );
        }
        f.messages.insert(
            "A".into(),
            (101..306)
                .map(|i| message(&i.to_string(), "追加依頼", "OWN"))
                .collect(),
        );
        f.messages
            .insert("B".into(), vec![message("400", "別の依頼", "OWN")]);
        f.http_errors
            .insert("/channels/B/messages?limit=100".into(), 1);
    }
    assert_eq!(routing::collect(&mut h.ctx, &mut ledger).unwrap(), 205);
    assert_eq!(ledger["messages"].as_object().unwrap().len(), 205);
    assert_eq!(ledger["cursor"]["channels"]["A"], "305");
    assert!(ledger["cursor"]["channels"]["B"].is_null());
    assert_eq!(routing::collect(&mut h.ctx, &mut ledger).unwrap(), 1);
    assert_eq!(ledger["cursor"]["channels"]["B"], "400");
    assert_eq!(ledger["messages"]["101"]["message"]["text"], "追加依頼 ");
}
#[test]
fn failed_later_page_keeps_whole_place_uncommitted() {
    let mut h = setup();
    let mut ledger = cursor(100);
    h.fake.borrow_mut().threads.insert(
        "A".into(),
        json!({"id":"A","type":11,"parent_id":"T","owner_id":"BOT","name":"A"}),
    );
    h.fake.borrow_mut().messages.insert(
        "A".into(),
        (101..306)
            .map(|i| message(&i.to_string(), "追加", "OWN"))
            .collect(),
    );
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/A/messages?limit=100&before=206".into(), 1);
    assert_eq!(routing::collect(&mut h.ctx, &mut ledger).unwrap(), 0);
    assert!(ledger["cursor"]["channels"]["A"].is_null());
    assert!(ledger["messages"].as_object().unwrap().is_empty());
    assert_eq!(routing::collect(&mut h.ctx, &mut ledger).unwrap(), 205);
}
#[test]
fn opening_question_and_native_option_answer_are_not_duplicate_requests() {
    let thread = json!({"id":"200","parent_id":"T"});
    let event = json!({"id":"500","text":"a"});
    assert_eq!(
        routing::classify(
            &event,
            &thread,
            &json!({"questions":[{"thread_id":"200","open":true}]}),
            None
        ),
        "handled"
    );
    assert_eq!(
        routing::classify(
            &event,
            &thread,
            &json!({"questions":[{"thread_id":"200","open":false,"seen_reply":"600"}]}),
            None
        ),
        "handled"
    );
    let task = json!({"task_question":{"message_id":"400","options":["直す","続ける"]}});
    assert_eq!(
        routing::classify(&event, &thread, &json!({"questions":[]}), Some(&task)),
        "handled"
    );
    assert_eq!(
        routing::classify(
            &json!({"id":"501","text":"追加でテストも直して"}),
            &thread,
            &json!({"questions":[]}),
            Some(&task)
        ),
        "task"
    );
}
fn log(h: &Harness, verdict: &str) -> (PathBuf, Value) {
    let p = h.root.path().join("rollout.jsonl");
    let now = h.fake.borrow().now;
    let at = chrono::DateTime::from_timestamp((now - 2000.) as i64, 0)
        .unwrap()
        .to_rfc3339();
    let text = if verdict == "needs_input" {
        "状態: 要判断\nどの方法で進めるか決めてください。"
    } else {
        "未完了"
    };
    let events = [
        json!({"type":"session_meta","payload":{"id":"01a0f0b3-bc0f-72f2-9e2a-2afd2c5aa108","cwd":h.root.path(),"source":"cli"}}),
        json!({"type":"event_msg","timestamp":at,"payload":{"type":"task_started","turn_id":"t1"}}),
        json!({"type":"event_msg","timestamp":at,"payload":{"type":"task_complete","turn_id":"t1","last_agent_message":text}}),
    ];
    std::fs::write(
        &p,
        events
            .iter()
            .map(Value::to_string)
            .collect::<Vec<_>>()
            .join("\n")
            + "\n",
    )
    .unwrap();
    let row = patrol::summary(&p).unwrap().unwrap();
    (p, row)
}
fn own(h: &Harness, row: &Value, status: &str, id: u64) {
    let dir = h.root.path().join("jobs/original");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"),&json!({"id":"original","status":"failed","cwd":h.root.path(),"thread_id":row["session_id"]}),2).unwrap();
    atomic_json(
        &h.ctx.todo,
        &json!({"projects":{},"items":[{"id":id,"status":status,"jobs":["original"]}]}),
        2,
    )
    .unwrap();
}

#[test]
fn session_tail_can_start_inside_each_multibyte_utf8_position() {
    let h = setup();
    let (path, expected) = log(&h, "partial");
    let original = std::fs::read(&path).unwrap();
    let header_end = original.iter().position(|b| *b == b'\n').unwrap() + 1;
    let mut checked = 0;
    for character in ["日", "😀"] {
        for padding in 0..character.len() {
            let mut bytes = original[..header_end].to_vec();
            bytes.extend_from_slice(b"{\"type\":\"response_item\",\"payload\":\"");
            bytes.extend_from_slice(character.repeat(400_000).as_bytes());
            bytes.extend(std::iter::repeat_n(b'x', padding));
            bytes.extend_from_slice(b"\"}\n");
            bytes.extend_from_slice(&original[header_end..]);
            let offset = bytes.len() - 1024 * 1024;
            if bytes[offset] & 0xc0 != 0x80 {
                continue;
            }
            assert!(std::str::from_utf8(&bytes).is_ok());
            std::fs::write(&path, bytes).unwrap();
            assert_eq!(patrol::summary(&path).unwrap(), Some(expected.clone()));
            let mut inventory_expected = expected.clone();
            inventory_expected["session_path"] =
                json!(path.canonicalize().unwrap().to_string_lossy());
            assert_eq!(
                patrol::sessions(h.root.path(), 1.).unwrap(),
                vec![inventory_expected]
            );
            checked += 1;
        }
    }
    assert_eq!(checked, 5);
}

#[test]
fn session_tail_ignores_unfinished_utf8_but_rejects_corrupt_complete_lines() {
    let h = setup();
    let (path, expected) = log(&h, "partial");
    let mut bytes = std::fs::read(&path).unwrap();
    bytes.extend_from_slice(b"{\"type\":\"response_item\",\"payload\":\"");
    bytes.push(0xe6);
    std::fs::write(&path, &bytes).unwrap();
    assert_eq!(patrol::summary(&path).unwrap(), Some(expected));
    bytes.push(b'\n');
    std::fs::write(&path, bytes).unwrap();
    assert_eq!(patrol::summary(&path).unwrap_err(), "session read failed");
    let mut corrupt = std::fs::read(&path).unwrap();
    corrupt.pop();
    *corrupt.last_mut().unwrap() = 0xff;
    std::fs::write(&path, corrupt).unwrap();
    assert_eq!(patrol::summary(&path).unwrap_err(), "session read failed");
    assert!(patrol::summary(&h.root.path().join("missing.jsonl")).is_err());
}

#[test]
fn unreadable_sessions_are_reported_and_do_not_stop_healthy_dry_run() {
    let mut h = setup();
    let (path, row) = log(&h, "partial");
    own(&h, &row, "doing", 60);
    let mut corrupt = std::fs::read(&path).unwrap();
    corrupt.extend_from_slice(&[0xff, b'\n']);
    std::fs::write(h.root.path().join("corrupt.jsonl"), corrupt).unwrap();
    std::fs::write(h.root.path().join("invalid-metadata.jsonl"), "not JSON\n").unwrap();
    atomic_json(
        &h.ctx.here.join("automation.json"),
        &json!({"enabled":true,"since":1}),
        2,
    )
    .unwrap();
    let root = h.root.path().to_path_buf();
    let report = patrol::run(&mut h.ctx, &root, true, &mut false).unwrap();
    assert_eq!(report["eligible"], json!([row["session_id"]]));
    assert_eq!(
        report["session_read_errors"],
        json!({"read":1,"invalid_metadata":1})
    );
    assert_eq!(patrol::sessions(&root, 1.).unwrap().len(), 1);
    assert!(h.fake.borrow().picks.is_empty());
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
    assert!(!h.ctx.here.join("patrol_state.json").exists());
    assert!(patrol::sessions(&root.join("missing"), 1.).is_err());
}

#[test]
fn session_that_disappears_before_resume_is_reported_without_launching() {
    let mut h = setup();
    let (path, row) = log(&h, "partial");
    own(&h, &row, "doing", 60);
    std::fs::remove_file(path).unwrap();
    let report = patrol::run_rows(&mut h.ctx, &[row], &[], 1., false, &mut false).unwrap();
    assert_eq!(report["session_read_errors"], json!({"open":1}));
    assert_eq!(report["launched"], json!([]));
    assert!(h.fake.borrow().picks.is_empty());
    assert!(h.fake.borrow().launches.is_empty());
}

#[test]
fn missing_work_directory_is_excluded_but_denylist_errors_still_stop_effects() {
    let mut h = setup();
    let (_, healthy) = log(&h, "partial");
    own(&h, &healthy, "doing", 60);
    let mut gone = healthy.clone();
    gone["cwd"] = json!(h.root.path().join("removed-worktree"));
    let report = patrol::run_rows(
        &mut h.ctx,
        &[gone, healthy.clone()],
        &[],
        1.,
        true,
        &mut false,
    )
    .unwrap();
    assert_eq!(
        report["session_read_errors"],
        json!({"working_directory":1})
    );
    assert_eq!(report["eligible"], json!([healthy["session_id"]]));
    assert!(h.fake.borrow().picks.is_empty());
    assert!(h.fake.borrow().launches.is_empty());
    std::fs::remove_file(&h.ctx.client_projects).unwrap();
    assert_eq!(
        patrol::run_rows(&mut h.ctx, &[healthy], &[], 1., false, &mut false).unwrap_err(),
        "OSError"
    );
    assert!(h.fake.borrow().picks.is_empty());
    assert!(h.fake.borrow().launches.is_empty());
}

#[test]
fn disappearance_after_selection_keeps_scanning_and_preserves_shared_attempt_budget() {
    let mut h = setup();
    let (path, first) = log(&h, "partial");
    own(&h, &first, "doing", 60);
    let second_path = h.root.path().join("second.jsonl");
    let second_text = std::fs::read_to_string(&path).unwrap().replace(
        "01a0f0b3-bc0f-72f2-9e2a-2afd2c5aa108",
        "01a0f0b3-bc0f-72f2-9e2a-2afd2c5aa109",
    );
    std::fs::write(&second_path, second_text).unwrap();
    let second = patrol::summary(&second_path).unwrap().unwrap();
    let second_job = h.root.path().join("jobs/second");
    std::fs::create_dir_all(&second_job).unwrap();
    atomic_json(&second_job.join("job.json"), &json!({"id":"second","status":"failed","cwd":h.root.path(),"thread_id":second["session_id"]}), 2).unwrap();
    let mut todo = read_json(&h.ctx.todo).unwrap();
    todo["items"][0]["jobs"]
        .as_array_mut()
        .unwrap()
        .push(json!("second"));
    atomic_json(&h.ctx.todo, &todo, 2).unwrap();
    h.fake.borrow_mut().remove_session_on_pick = Some(path);
    let mut budget = false;
    let report = patrol::run_rows(
        &mut h.ctx,
        &[first.clone(), second.clone()],
        &[],
        1.,
        false,
        &mut budget,
    )
    .unwrap();
    assert_eq!(report["session_read_errors"], json!({"open":1}));
    assert_eq!(
        report["eligible"],
        json!([first["session_id"], second["session_id"]])
    );
    assert_eq!(report["launched"], json!([]));
    assert!(budget);
    assert_eq!(h.fake.borrow().picks.len(), 1);
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
}
#[test]
fn dry_run_never_writes_selects_posts_or_launches_and_current_turn_overrides_old() {
    let mut h = setup();
    let (p, row) = log(&h, "partial");
    own(&h, &row, "doing", 60);
    let report = patrol::run_rows(
        &mut h.ctx,
        std::slice::from_ref(&row),
        &[],
        1.,
        true,
        &mut false,
    )
    .unwrap();
    assert_eq!(report["eligible"].as_array().unwrap().len(), 1);
    assert!(!h.ctx.here.join("patrol_state.json").exists());
    assert!(h.fake.borrow().picks.is_empty());
    assert!(h.fake.borrow().posts.is_empty());
    assert!(h.fake.borrow().launches.is_empty());
    use std::io::Write;
    let mut f = std::fs::OpenOptions::new().append(true).open(p).unwrap();
    writeln!(f,"{}",json!({"type":"event_msg","timestamp":"2026-10-02T19:00:00Z","payload":{"type":"task_started","turn_id":"t2"}})).unwrap();
    drop(f);
    let report = patrol::run_rows(&mut h.ctx, &[row], &[], 1., false, &mut false).unwrap();
    assert!(report["launched"].as_array().unwrap().is_empty());
    assert!(h.fake.borrow().launches.is_empty());
}
#[test]
fn patrol_adopts_launch_after_crash_and_never_launches_twice() {
    let mut h = setup();
    let (_, row) = log(&h, "partial");
    own(&h, &row, "doing", 60);
    h.fake.borrow_mut().crash_after_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| patrol::run_rows(
        &mut h.ctx,
        std::slice::from_ref(&row),
        &[],
        1.,
        false,
        &mut false
    )))
    .is_err());
    assert_eq!(h.fake.borrow().launches.len(), 1);
    patrol::run_rows(&mut h.ctx, &[row], &[], 1., false, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    let ledger = read_json(&h.ctx.here.join("patrol_state.json")).unwrap();
    assert_eq!(
        ledger["stops"]
            .as_object()
            .unwrap()
            .values()
            .next()
            .unwrap()["job"],
        "job1"
    );
}
#[test]
fn blocked_historical_unknown_process_and_task57_never_launch() {
    for (status, id, processes, since) in [
        ("block", 60, vec![], 1.),
        ("doing", 57, vec![], 1.),
        ("doing", 60, vec![String::new()], 1.),
        ("doing", 60, vec![], f64::MAX),
    ] {
        let mut h = setup();
        let (_, row) = log(&h, "partial");
        own(&h, &row, status, id);
        patrol::run_rows(&mut h.ctx, &[row], &processes, since, false, &mut false).unwrap();
        assert!(h.fake.borrow().launches.is_empty());
    }
}
#[test]
fn corrupt_ledger_is_preserved_and_subagent_is_skipped() {
    let mut h = setup();
    let (path, row) = log(&h, "partial");
    own(&h, &row, "doing", 60);
    let ledger = h.ctx.here.join("patrol_state.json");
    std::fs::write(&ledger, "broken").unwrap();
    assert!(patrol::run_rows(&mut h.ctx, &[row], &[], 1., false, &mut false).is_err());
    assert_eq!(std::fs::read_to_string(ledger).unwrap(), "broken");
    std::fs::write(&path,json!({"type":"session_meta","payload":{"id":"01a0f0b3-bc0f-72f2-9e2a-2afd2c5aa108","source":{"subagent":"review"}}}).to_string()+"\n").unwrap();
    assert!(patrol::summary(&path).unwrap().is_none());
}
#[test]
fn owner_wait_is_matched_by_work_and_question_not_resumed() {
    let mut h = setup();
    let (_, mut row) = log(&h, "needs_input");
    own(&h, &row, "doing", 60);
    row["final_excerpt"] = json!("状態: 要判断\n1555000000000000000");
    h.save(&json!({"questions":[{"thread_id":"1555000000000000000","cwd":h.root.path(),"title":"方針","open":true}]}));
    assert!(patrol::questions_match(&row, &h.state()["questions"][0]));
    patrol::run_rows(&mut h.ctx, &[row], &[], 1., false, &mut false).unwrap();
    assert!(h.fake.borrow().launches.is_empty());
}
#[test]
fn retired_start_preserves_existing_ledgers_and_protected_paths_fail_closed() {
    let h = setup();
    assert!(start(&h.ctx).unwrap_err().contains("共有ToDo"));
    assert!(!h.ctx.here.join("automation.json").exists());
    for (name, value) in [
        (
            "automation.json",
            json!({"enabled":true,"since":1,"unknown":"keep"}),
        ),
        (
            "routing_state.json",
            json!({"messages":{"pending":{"ready":true}},"cursor":{"since":"123"}}),
        ),
        (
            "patrol_state.json",
            json!({"stops":{"old":{"job":"existing"}}}),
        ),
    ] {
        atomic_json(&h.ctx.here.join(name), &value, 2).unwrap();
    }
    let files = ["automation.json", "routing_state.json", "patrol_state.json"];
    let before: Vec<_> = files
        .iter()
        .map(|name| std::fs::read(h.ctx.here.join(name)).unwrap())
        .collect();
    assert!(start(&h.ctx).is_err());
    let mut ctx = h.ctx;
    assert_eq!(
        patrol::run(&mut ctx, &h.root.path().join("missing"), false, &mut false).unwrap_err(),
        reject_legacy_launch().unwrap_err()
    );
    for (name, bytes) in files.iter().zip(before) {
        assert_eq!(std::fs::read(ctx.here.join(name)).unwrap(), bytes);
    }
    assert!(h.fake.borrow().runs.is_empty());
    assert!(h.fake.borrow().http_calls.is_empty());
    assert!(h.fake.borrow().launches.is_empty());
    let client = h.root.path().join("SecretClient");
    std::fs::create_dir(&client).unwrap();
    std::fs::write(&ctx.client_projects, "SecretClient\n").unwrap();
    assert!(protected(&ctx, &client.to_string_lossy()).unwrap());
    std::fs::remove_file(&ctx.client_projects).unwrap();
    assert!(protected(&ctx, &h.root.path().to_string_lossy()).is_err());
}
#[test]
fn routing_preserves_backlog_and_adopts_lost_response_once() {
    let mut h = setup();
    let external_cwd = tempfile::tempdir_in(r"C:\Windows\Temp").unwrap();
    let now = h.fake.borrow().now;
    let since = now - 3600.;
    let mid = (((now * 1000. - 1420070400000.) as u64) << 22).to_string();
    let tid = "700";
    h.fake.borrow_mut().threads.insert(tid.into(),json!({"id":tid,"parent_id":"T","name":"作業の追加","type":11,"thread_metadata":{"archived":false}}));
    h.fake
        .borrow_mut()
        .messages
        .insert(tid.into(), vec![message(&mid, "テストを追加して", "OWN")]);
    // Closed question routes to the original work, using a fixture directory.
    h.save(&json!({"questions":[{"thread_id":tid,"after":"10","cwd":external_cwd.path(),"open":false,"title":"質問"}]}));
    atomic_json(
        &h.ctx.here.join("automation.json"),
        &json!({"enabled":true,"since":since}),
        2,
    )
    .unwrap();
    atomic_json(
        &h.ctx.here.join("routing_state.json"),
        &cursor(((since * 1000. - 1420070400000.) as u64) << 22),
        2,
    )
    .unwrap();
    h.fake.borrow_mut().lose_launch_response = true;
    let report = routing::run(&mut h.ctx, &mut false).unwrap();
    let failed_ledger = read_json(&h.ctx.here.join("routing_state.json")).unwrap();
    assert_eq!(
        report["errors"], 0,
        "routing errors: {report}; row: {}",
        failed_ledger["messages"][&mid]
    );
    assert_eq!(report["launched"], json!(["job1"]));
    assert_eq!(h.fake.borrow().launches.len(), 1);
    routing::run(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.fake.borrow().posts.len(), 1);
    let ledger = read_json(&h.ctx.here.join("routing_state.json")).unwrap();
    assert_eq!(ledger["messages"][&mid]["done"], true);
    assert_eq!(h.state()["questions"][0]["continuation"]["job"], "job1");
    h.finish(
        "job1",
        "succeeded",
        Some("状態: 完了\n追加テストを確認しました。"),
        None,
    );
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().posts.len(), 2);
    assert_eq!(h.state()["questions"][0]["open"], false);
    assert_eq!(h.fake.borrow().patches.last().unwrap().1["archived"], true);
}

#[test]
fn patrol_rechecks_new_process_after_selection_and_recover_failure_blocks_dispatch() {
    let mut h = setup();
    let (_, row) = log(&h, "partial");
    own(&h, &row, "doing", 60);
    h.fake.borrow_mut().codex_directories = vec![h.root.path().to_string_lossy().to_string()];
    let report = patrol::run_rows(&mut h.ctx, &[row], &[], 1., false, &mut false).unwrap();
    assert_eq!(h.fake.borrow().picks.len(), 1);
    assert!(h.fake.borrow().launches.is_empty());
    assert_eq!(report["waiting"].as_array().unwrap().len(), 1);
    h.fake.borrow_mut().recover_fail = true;
    assert!(!recover_before_launch(&mut h.ctx));
    h.fake.borrow_mut().recover_fail = false;
    assert!(recover_before_launch(&mut h.ctx));
    let runs = &h.fake.borrow().runs;
    let reboot = runs
        .iter()
        .rposition(|(argv, _)| argv.iter().any(|a| a == "recover-reboot"))
        .unwrap();
    let scheduler = runs
        .iter()
        .rposition(|(argv, _)| argv.last().is_some_and(|a| a == "recover"))
        .unwrap();
    assert!(reboot < scheduler);
    assert!(runs[reboot].0.iter().any(|a| a == "--apply"));
}

#[test]
fn reboot_recovery_failure_prevents_scheduler_from_resurrecting_same_work() {
    let mut h = setup();
    assert!(recover_before_launch(&mut h.ctx));
    assert!(h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(argv, _)| argv.last().is_some_and(|a| a == "recover")));
    h.fake.borrow_mut().runs.clear();
    h.fake.borrow_mut().reboot_recover_fail = true;
    assert!(!recover_before_launch(&mut h.ctx));
    assert_eq!(h.fake.borrow().runs.len(), 1);
    assert!(h.fake.borrow().runs[0]
        .0
        .iter()
        .any(|a| a == "recover-reboot"));
}

#[test]
fn abort_of_another_turn_does_not_overwrite_top_level_completion() {
    let h = setup();
    let (path, _) = log(&h, "partial");
    use std::io::Write;
    let mut file = std::fs::OpenOptions::new()
        .append(true)
        .open(&path)
        .unwrap();
    for payload in [
        json!({"type":"task_complete","turn_id":"t1","last_agent_message":"状態: 完了"}),
        json!({"type":"turn_aborted","turn_id":"child","root_turn_id":"t1"}),
    ] {
        writeln!(
            file,
            "{}",
            json!({"type":"event_msg","timestamp":"2026-10-02T19:00:00Z","payload":payload})
        )
        .unwrap();
    }
    drop(file);
    let row = patrol::summary(&path).unwrap().unwrap();
    assert_eq!(row["verdict"], "completed");
    assert_eq!(row["turn_id"], "t1");
}

#[test]
fn stale_ready_entry_waits_for_original_worker_and_does_not_launch() {
    let mut h = setup();
    let now = h.fake.borrow().now;
    let since = now - 3600.;
    let mid = (((now * 1000. - 1420070400000.) as u64) << 22).to_string();
    atomic_json(
        &h.ctx.here.join("automation.json"),
        &json!({"enabled":true,"since":since}),
        2,
    )
    .unwrap();
    let mut ledger = cursor(((since * 1000. - 1420070400000.) as u64) << 22);
    h.fake.borrow_mut().threads.insert(
        "200".into(),
        json!({"id":"200","type":11,"parent_id":"T","owner_id":"BOT","name":"作業"}),
    );
    ledger["messages"][&mid] = json!({"message":{"id":mid,"channel":"200","text":"追加"},"key":"stable","kind":"task","thread":{"id":"200","type":11},"cwd":h.root.path(),"original":{"id":"original","cwd":h.root.path()},"acknowledged":true,"ready":true,"prompt":"追加","selection":{"model":"gpt-6-luna","effort":"high","task_type":"implementation"}});
    atomic_json(&h.ctx.here.join("routing_state.json"), &ledger, 2).unwrap();
    let dir = h.root.path().join("jobs/original");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"original","status":"running","cwd":h.root.path()}),
        2,
    )
    .unwrap();
    routing::run(&mut h.ctx, &mut false).unwrap();
    assert!(h.fake.borrow().launches.is_empty());
    assert!(h.fake.borrow().picks.is_empty());
    let ledger = read_json(&h.ctx.here.join("routing_state.json")).unwrap();
    assert_eq!(ledger["messages"][&mid]["ready"], false);
}
