use super::*;

#[test]
fn answered_links_inspection_verifies_full_body_and_progress_without_retrying_post() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let args = f.ask("answered-links", &format!("aまたはb。{}", "背景".repeat(1500)));
    f.server.lock().unwrap().reject_auxiliary_links = true;
    assert!(ask(&mut ctx, &args).unwrap_err().contains("RecoveryMessagePending"));
    let ledger = Ledger::load(&f.home).unwrap();
    let start = ledger.receipt(&args.request_id, "question:0").unwrap();
    let input = RepliesArgs { thread: start.thread_id.clone(), message: Some(start.message_id.clone()), request_id: Some(args.request_id.clone()), todo_id: Some(args.todo_id), read_only: true };
    f.add_reply(&start.thread_id, OWNER, "a", false);
    let before = fs::read(f.home.join(LEDGER)).unwrap();
    let posts = f.server.lock().unwrap().posts.len();
    let result = replies(&mut ctx, &input).unwrap();
    assert_eq!(result["question_delivery_verified"], true);
    assert_eq!(result["auxiliary_links_unconfirmed"], json!(["question-links", "origin-links"]));
    assert_eq!(result["sender_delivery_complete"], false);
    assert_eq!(result["delivery"], "inspection_only");
    assert_eq!(result["owner_replies"].as_array().unwrap().len(), 1);
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    assert!(!Ledger::load(&f.home).unwrap().complete(&args.request_id));
    f.unchanged();
}

#[test]
fn answered_links_missing_continuation_or_changed_progress_is_not_verified() {
    for phase in ["question-continuation:1", "progress"] {
        let f = Fixture::new();
        let mut ctx = f.context();
        let args = f.ask("incomplete-body", &format!("aまたはb。{}", "背景".repeat(1500)));
        f.server.lock().unwrap().reject_auxiliary_links = true;
        assert!(ask(&mut ctx, &args).is_err());
        let ledger = Ledger::load(&f.home).unwrap();
        let start = ledger.receipt(&args.request_id, "question:0").unwrap();
        let target = ledger.receipt(&args.request_id, phase).unwrap();
        let mut state = f.server.lock().unwrap();
        state.messages.get_mut(&target.thread_id).unwrap().iter_mut().find(|m| m["id"] == target.message_id).unwrap()["content"] = json!("changed");
        drop(state);
        let result = replies(&mut ctx, &RepliesArgs { thread: start.thread_id.clone(), message: Some(start.message_id.clone()), request_id: Some(args.request_id.clone()), todo_id: Some(args.todo_id), read_only: true }).unwrap();
        assert_eq!(result["question_delivery_verified"], false, "{phase}");
        assert_eq!(result["delivery"], "inspection_only");
        f.unchanged();
    }
}
