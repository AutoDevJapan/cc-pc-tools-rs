mod common;
use cc_ask::{core::*, job_titles::*};
use common::*;
use serde_json::json;
use sha2::{Digest, Sha256};

fn fixture() -> Harness {
    let h = Harness::new();
    let id = "job-todo-1104";
    let dir = h.ctx.jobs.join("jobs").join(id);
    std::fs::create_dir_all(&dir).unwrap();
    std::fs::write(
        dir.join("job.json"),
        json!({"id":id,"name":"todo-1104","cwd":"D:/demo"}).to_string(),
    )
    .unwrap();
    std::fs::write(&h.ctx.todo, json!({"items":[{"id":1104,"project":"署名ツール","title":"署名の拒否を確認する","jobs":[id]}]}).to_string()).unwrap();
    let path = h
        .ctx
        .discord_tasks
        .join(format!("{:x}.json", Sha256::digest(id.as_bytes())));
    std::fs::write(path, json!({"id":id,"title":"todo-1104","thread_id":"200","starter_channel_id":"200","starter_message_id":"200","fallback_title_pending":true}).to_string()).unwrap();
    let mut f = h.fake.borrow_mut();
    f.threads.get_mut("200").unwrap()["parent_id"] = json!("T");
    f.threads.get_mut("200").unwrap()["name"] = json!("todo-1104");
    f.messages.insert("200".into(), vec![json!({"id":"200","content":"🔵 todo-1104 ・ 作業中\n-# [godd-task:hash:start]","embeds":[{"description":"**todo-1104**\n作業中","color":42}]})]);
    drop(f);
    h
}

#[test]
fn busy_notifier_card_lock_returns_within_deadline_without_card_changes() {
    let mut h = fixture();
    let path = h
        .ctx
        .discord_tasks
        .join(format!("{:x}.lock", Sha256::digest(b"job-todo-1104")));
    let held = std::fs::OpenOptions::new()
        .create(true)
        .read(true)
        .write(true)
        .truncate(false)
        .open(path)
        .unwrap();
    held.lock().unwrap();
    let deadline = cc_ask::tick_runtime::Deadline::new(std::time::Duration::from_millis(75));
    let start = std::time::Instant::now();
    assert!(sync_job(&mut h.ctx, "job-todo-1104").is_err());
    drop(deadline);
    assert!(start.elapsed() < std::time::Duration::from_secs(1));
    assert!(h.fake.borrow().patches.is_empty());
}

#[test]
fn busy_notifier_card_defers_before_reading_task_metadata() {
    let mut h = fixture();
    let path = h
        .ctx
        .discord_tasks
        .join(format!("{:x}.lock", Sha256::digest(b"job-todo-1104")));
    let held = std::fs::OpenOptions::new()
        .create(true)
        .read(true)
        .write(true)
        .truncate(false)
        .open(path)
        .unwrap();
    held.lock().unwrap();
    std::fs::write(
        &h.ctx.todo,
        "incomplete metadata: do not read while card is busy",
    )
    .unwrap();
    let _deadline = cc_ask::tick_runtime::Deadline::new(std::time::Duration::from_millis(75));
    let error = sync_job(&mut h.ctx, "job-todo-1104")
        .unwrap_err()
        .to_string();
    assert!(
        error.contains("TickDeadline") || error.contains("task card lock is busy"),
        "{error}"
    );
    assert!(h.fake.borrow().patches.is_empty());
}
#[test]
fn preview_has_no_mutations_apply_updates_channel_card_and_state_then_is_idempotent() {
    let mut h = fixture();
    assert_eq!(
        rename_open(&mut h.ctx, false, 1).unwrap()["changes"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    assert!(h.fake.borrow().patches.is_empty());
    let result = rename_open(&mut h.ctx, true, 1).unwrap();
    assert_eq!(
        result["changes"][0]["title"],
        "【署名ツール】署名の拒否を確認する"
    );
    let f = h.fake.borrow();
    assert_eq!(
        f.threads["200"]["name"],
        "【署名ツール】署名の拒否を確認する"
    );
    let m = &f.messages["200"][0];
    assert!(s(&m["content"]).contains("-# ジョブ job-todo-1104"));
    assert!(s(&m["content"]).ends_with("-# [godd-task:hash:start]"));
    assert_eq!(m["embeds"][0]["color"], 42);
    assert!(!s(&m["embeds"][0]["description"]).contains("todo-1104"));
    assert!(f.posts.is_empty());
    drop(f);
    assert_eq!(
        rename_open(&mut h.ctx, true, 10).unwrap()["changes"],
        json!([])
    );
    let count = h.fake.borrow().patches.len();
    sync_job(&mut h.ctx, "job-todo-1104").unwrap();
    assert_eq!(h.fake.borrow().patches.len(), count);
}
#[test]
fn unregistered_and_archived_threads_are_untouched_and_clients_are_redacted() {
    let mut h = fixture();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["thread_metadata"]["archived"] =
        json!(true);
    assert_eq!(
        rename_open(&mut h.ctx, true, 10).unwrap()["changes"],
        json!([])
    );
    assert!(h.fake.borrow().patches.is_empty());
    let body = card_body(
        &json!({"content":"ClientNameを直す\n-# [godd-task:hash:start]","embeds":[]}),
        "ClientNameを直す",
        "【案件】接続を修正する",
        "ClientName-job",
        &cc_ask::task_title::Names::parse("ClientName"),
    );
    assert!(!body.to_string().to_lowercase().contains("clientname"));
    assert_eq!(body["allowed_mentions"]["parse"], json!([]));
}
#[test]
fn finish_card_sync_restores_the_archive_and_retains_the_actual_id_note() {
    let mut h = fixture();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["thread_metadata"]["archived"] =
        json!(true);
    sync_job(&mut h.ctx, "job-todo-1104").unwrap();
    let f = h.fake.borrow();
    assert_eq!(f.threads["200"]["thread_metadata"]["archived"], true);
    assert!(s(&f.messages["200"][0]["content"]).contains("-# ジョブ job-todo-1104"));
    assert!(f.patches.iter().any(|(_, b)| b["archived"] == false));
    assert!(f.posts.is_empty());
}
#[test]
fn formatted_but_stale_work_title_is_still_updated() {
    let mut h = fixture();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["name"] =
        json!("【署名ツール】古い署名を検証する");
    assert_eq!(
        rename_open(&mut h.ctx, true, 1).unwrap()["changes"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    assert_eq!(
        h.fake.borrow().threads["200"]["name"],
        "【署名ツール】署名の拒否を確認する"
    );
}
#[test]
fn matching_thread_title_still_repairs_a_missing_starter_id_note() {
    let mut h = fixture();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["name"] =
        json!("【署名ツール】署名の拒否を確認する");
    let result = rename_open(&mut h.ctx, true, 1).unwrap();
    assert_eq!(result["changes"].as_array().unwrap().len(), 1);
    assert!(s(&h.fake.borrow().messages["200"][0]["content"]).contains("-# ジョブ job-todo-1104"));
    assert_eq!(
        rename_open(&mut h.ctx, true, 1).unwrap()["changes"],
        json!([])
    );
}
#[test]
fn failed_starter_edit_restores_archive_and_retry_repairs_the_note() {
    let mut h = fixture();
    {
        let mut f = h.fake.borrow_mut();
        f.threads.get_mut("200").unwrap()["name"] = json!("【署名ツール】署名の拒否を確認する");
        f.threads.get_mut("200").unwrap()["thread_metadata"]["archived"] = json!(true);
        f.http_errors
            .insert("PATCH /channels/200/messages/200".into(), 1);
    }
    assert!(sync_job(&mut h.ctx, "job-todo-1104").is_err());
    assert_eq!(
        h.fake.borrow().threads["200"]["thread_metadata"]["archived"],
        true
    );
    sync_job(&mut h.ctx, "job-todo-1104").unwrap();
    let f = h.fake.borrow();
    assert_eq!(f.threads["200"]["thread_metadata"]["archived"], true);
    assert!(s(&f.messages["200"][0]["content"]).contains("-# ジョブ job-todo-1104"));
}
#[test]
fn interrupted_archive_restoration_is_retained_and_retried() {
    let mut h = fixture();
    let path = h.ctx.discord_tasks.join(format!(
        "{:x}.json",
        Sha256::digest("job-todo-1104".as_bytes())
    ));
    atomic_json(
        &path.with_extension("restore"),
        &json!({"thread_id":"200","archived":true}),
        0,
    )
    .unwrap();
    // Represents a previous helper killed after opening the archived thread.
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["thread_metadata"]["archived"] =
        json!(false);
    sync_job(&mut h.ctx, "job-todo-1104").unwrap();
    assert_eq!(
        h.fake.borrow().threads["200"]["thread_metadata"]["archived"],
        true
    );
    assert_eq!(
        read_json(&path.with_extension("restore")).unwrap()["archived"],
        false
    );
}
#[test]
fn request_and_resume_launches_send_the_actual_owner_work_not_the_thread_id() {
    let mut h = fixture();
    let prompt =
        "オーナー（Makhmeto）が書いた依頼です。\n### オーナー（最新）\n保存後に署名を再照合する。";
    launch_job(
        &mut h.ctx,
        &json!({"id":"200","name":"【署名ツール】確認事項"}),
        prompt,
        &json!({"model":"gpt-6-luna","effort":"high","task_type":"review"}),
        "key-work",
        false,
        Some("D:/signature-wt/fix"),
        Some("resume"),
    )
    .unwrap();
    let f = h.fake.borrow();
    let args = &f
        .runs
        .iter()
        .find(|(args, _)| args.iter().any(|v| v == "--task-title"))
        .unwrap()
        .0;
    assert_eq!(
        args[args.iter().position(|v| v == "--task-title").unwrap() + 1],
        "保存後に署名を再照合する"
    );
    assert_eq!(
        args[args.iter().position(|v| v == "--project").unwrap() + 1],
        "署名ツール"
    );
}
