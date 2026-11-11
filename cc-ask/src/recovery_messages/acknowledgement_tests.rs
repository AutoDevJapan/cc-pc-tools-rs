use super::*;

fn receipt_case(f: &Fixture, ctx: &mut Context) -> (LogArgs, PathBuf, Value) {
    let question = f.ask("receipt-question", "元の画面案を選んでください。");
    let sent = ask(ctx, &question).unwrap();
    let thread = core::s(&sent["thread_id"]);
    let reply = f.add_reply(thread, OWNER, "a", false);
    let mut args = f.log("receiver-receipt-test-version", "受け取りました。\n", false);
    args.thread = Some(thread.into());
    assert!(log(ctx, &args).unwrap_err().contains("explicit log thread"));
    let ledger = Ledger::load(&f.home).unwrap();
    let original = ledger.request(&question.request_id).unwrap();
    let hash = |s: &str| format!("{:x}", Sha256::digest(s.as_bytes()));
    let binding = json!({
        "domain":"discord-owner-reply-acknowledgement-v1", "purpose":"acknowledge_owner_reply",
        "request_id":args.request_id,"question_request_id":question.request_id,
        "todo_id":question.todo_id,"thread_id":thread,"question_message_id":sent["message_id"],
        "question_fingerprint":original.fingerprint,"question_body_sha256":hash(&original.body),
        "bot_id":BOT,"owner_id":OWNER,"reply_message_id":reply,"reply_body_sha256":hash("a"),
        "reply_timestamp":"2026-10-12T00:00:01+00:00","reply_edited_timestamp":null,
        "body_sha256":hash("受け取りました。\n")
    });
    let path = f.root.path().join("owner-acknowledgement.json");
    fs::write(&path, serde_json::to_vec(&binding).unwrap()).unwrap();
    (args, path, binding)
}

#[test]
fn owner_receipt_resumes_the_same_unsent_log_and_delivers_once() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, binding, _) = receipt_case(&f, &mut ctx);
    let before = Ledger::load(&f.home)
        .unwrap()
        .request(&args.request_id)
        .unwrap()
        .clone();
    let posts = f.server.lock().unwrap().posts.len();
    let first = log_acknowledgement(&mut ctx, &args, &binding).unwrap();
    let again = log_acknowledgement(&mut ctx, &args, &binding).unwrap();
    assert_eq!(first["delivery"], "verified");
    assert_eq!(first["message_id"], again["message_id"]);
    assert_eq!(
        Ledger::load(&f.home).unwrap().request(&args.request_id),
        Some(&before)
    );
    let server = f.server.lock().unwrap();
    assert_eq!(server.posts.len(), posts + 1);
    assert_eq!(
        server.posts.last().unwrap().1["allowed_mentions"]["replied_user"],
        false
    );
    assert_eq!(
        server.posts.last().unwrap().1["allowed_mentions"]["users"],
        json!([])
    );
    assert_eq!(server.forbidden_effects, 0);
    drop(server);
    f.unchanged();
}

#[test]
fn owner_receipt_check_is_get_only_and_does_not_pin_or_rewrite_intents() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, binding, _) = receipt_case(&f, &mut ctx);
    let before = fs::read(f.home.join(LEDGER)).unwrap();
    let posts = f.server.lock().unwrap().posts.len();
    assert_eq!(
        check_acknowledgement(&mut ctx, &args, &binding).unwrap()["delivery"],
        "not_sent"
    );
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
    assert!(!f.home.join("recovery-owner-acknowledgements.json").exists());
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    f.unchanged();
}

#[test]
fn owner_receipt_rejects_changed_binding_and_arbitrary_text_before_any_post() {
    for field in [
        "purpose",
        "question_request_id",
        "todo_id",
        "thread_id",
        "question_message_id",
        "question_fingerprint",
        "question_body_sha256",
        "bot_id",
        "owner_id",
        "reply_message_id",
        "reply_body_sha256",
        "reply_timestamp",
        "reply_edited_timestamp",
        "body_sha256",
    ] {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, path, mut proof) = receipt_case(&f, &mut ctx);
        proof[field] = if field == "todo_id" {
            json!(999)
        } else if field.ends_with("sha256") || field == "question_fingerprint" {
            json!("c".repeat(64))
        } else if field == "reply_edited_timestamp" || field == "reply_timestamp" {
            json!("2026-10-12T00:00:02Z")
        } else {
            json!("999")
        };
        fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
        let before = fs::read(f.home.join(LEDGER)).unwrap();
        let posts = f.server.lock().unwrap().posts.len();
        assert!(
            log_acknowledgement(&mut ctx, &args, &path).is_err(),
            "{field}"
        );
        assert_eq!(f.server.lock().unwrap().posts.len(), posts, "{field}");
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before, "{field}");
        assert!(!f.home.join("recovery-owner-acknowledgements.json").exists());
    }
    let f = Fixture::new();
    let mut ctx = f.context();
    let (mut args, path, mut proof) = receipt_case(&f, &mut ctx);
    for body in [
        "aで進めます。",
        "受け取りました。承認してください。",
        "<@100> 受け取りました。",
    ] {
        fs::write(&args.body_file, body).unwrap();
        proof["body_sha256"] = json!(format!("{:x}", Sha256::digest(body.as_bytes())));
        fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
        assert!(log_acknowledgement(&mut ctx, &args, &path).is_err());
    }
    fs::write(&args.body_file, "受け取りました。\n").unwrap();
    args.notify = true;
    assert!(log_acknowledgement(&mut ctx, &args, &path).is_err());
}

#[test]
fn owner_receipt_does_not_promote_question_or_rebind_version_to_another_request() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, mut proof) = receipt_case(&f, &mut ctx);
    log_acknowledgement(&mut ctx, &args, &path).unwrap();
    let posts = f.server.lock().unwrap().posts.len();
    let mut arbitrary = f.log("arbitrary-question-log", "別の進捗です。", false);
    arbitrary.thread = args.thread.clone();
    assert!(log(&mut ctx, &arbitrary).is_err());
    let mut alias = f.log("another-receipt", "受け取りました。\n", false);
    alias.thread = args.thread.clone();
    proof["request_id"] = json!(alias.request_id);
    fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
    assert!(log_acknowledgement(&mut ctx, &alias, &path).is_err());
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
}

#[test]
fn owner_receipt_recovers_lost_response_without_posting_again() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, _) = receipt_case(&f, &mut ctx);
    let before = f.server.lock().unwrap().posts.len();
    {
        let mut server = f.server.lock().unwrap();
        server.drop_response = true;
        server.block_reads_after_post = true;
    }
    assert!(log_acknowledgement(&mut ctx, &args, &path).is_err());
    {
        let mut server = f.server.lock().unwrap();
        server.blocked = false;
        server.block_reads_after_post = false;
    }
    assert_eq!(
        log_acknowledgement(&mut ctx, &args, &path).unwrap()["delivery"],
        "verified"
    );
    assert_eq!(f.server.lock().unwrap().posts.len(), before + 1);
    f.unchanged();
}

#[test]
fn owner_receipt_reuses_linked_silent_ack_from_another_path() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, proof) = receipt_case(&f, &mut ctx);
    {
        let mut server = f.server.lock().unwrap();
        let thread = args.thread.as_ref().unwrap();
        server.messages.get_mut(thread).unwrap().push(json!({"id":"9000", "channel_id":thread,
            "author":{"id":BOT,"bot":true},"type":19,"content":"受け取りました。",
            "timestamp":"2026-10-12T00:00:02Z","flags":SILENT,"mentions":[],"mention_roles":[],
            "mention_everyone":false,"attachments":[],"message_reference":{"message_id":proof["reply_message_id"],"channel_id":thread}}));
    }
    let posts = f.server.lock().unwrap().posts.len();
    let result = log_acknowledgement(&mut ctx, &args, &path).unwrap();
    assert_eq!(result["existing_acknowledgement"], true);
    assert_eq!(result["message_id"], "9000");
    assert_eq!(
        log_acknowledgement(&mut ctx, &args, &path).unwrap()["message_id"],
        "9000"
    );
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    f.server
        .lock()
        .unwrap()
        .messages
        .get_mut(args.thread.as_ref().unwrap())
        .unwrap()
        .retain(|m| m["id"] != "9000");
    assert!(log_acknowledgement(&mut ctx, &args, &path).is_err());
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
}

#[test]
fn owner_receipt_reuses_existing_linked_steering_receipt_without_a_second_post() {
    for content in ["【対象製品】受け取りました。今の作業に反映します。",
        "追加の依頼を受け取りました。実行中の担当が終わった後に、元の作業と追加依頼を照合して進めます。"] {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, proof) = receipt_case(&f, &mut ctx);
    {
        let mut server = f.server.lock().unwrap();
        let thread = args.thread.as_ref().unwrap();
        server.messages.get_mut(thread).unwrap().push(json!({"id":"9001","channel_id":thread,
            "author":{"id":BOT,"bot":true},"type":19,
            "content":content,
            "timestamp":"2026-10-12T00:00:02Z","flags":SILENT,"mentions":[],"mention_roles":[],
            "mention_everyone":false,"attachments":[],"message_reference":{"message_id":proof["reply_message_id"],"channel_id":thread}}));
    }
    let posts = f.server.lock().unwrap().posts.len();
    let result = log_acknowledgement(&mut ctx, &args, &path).unwrap();
    assert_eq!(result["existing_acknowledgement"], true);
    assert_eq!(result["message_id"], "9001");
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    }
}

#[test]
fn owner_receipt_pins_the_version_before_post_and_keeps_get_recovery_after_an_edit() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, proof) = receipt_case(&f, &mut ctx);
    log_acknowledgement(&mut ctx, &args, &path).unwrap();
    let posts = f.server.lock().unwrap().posts.len();
    {
        let mut server = f.server.lock().unwrap();
        let reply = server
            .messages
            .get_mut(args.thread.as_ref().unwrap())
            .unwrap()
            .iter_mut()
            .find(|m| m["id"] == proof["reply_message_id"])
            .unwrap();
        reply["content"] = json!("b");
        reply["edited_timestamp"] = json!("2026-10-12T00:00:03Z");
    }
    assert_eq!(
        log_acknowledgement(&mut ctx, &args, &path).unwrap()["delivery"],
        "verified"
    );
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    let other = Fixture::new();
    let mut other_ctx = other.context();
    let (other_args, other_path, other_proof) = receipt_case(&other, &mut other_ctx);
    {
        let mut server = other.server.lock().unwrap();
        let reply = server
            .messages
            .get_mut(other_args.thread.as_ref().unwrap())
            .unwrap()
            .iter_mut()
            .find(|m| m["id"] == other_proof["reply_message_id"])
            .unwrap();
        reply["content"] = json!("edited before send");
    }
    let before = other.server.lock().unwrap().posts.len();
    assert!(log_acknowledgement(&mut other_ctx, &other_args, &other_path).is_err());
    assert_eq!(other.server.lock().unwrap().posts.len(), before);
}

#[test]
fn owner_receipt_new_message_with_identical_text_has_a_distinct_effect_and_nonce() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, mut proof) = receipt_case(&f, &mut ctx);
    let first = log_acknowledgement(&mut ctx, &args, &path).unwrap();
    let reply = f.add_reply(args.thread.as_ref().unwrap(), OWNER, "a", false);
    let mut second_args = f.log(
        "receiver-receipt-test-new-message",
        "受け取りました。\n",
        false,
    );
    second_args.thread = args.thread.clone();
    proof["request_id"] = json!(second_args.request_id);
    proof["reply_message_id"] = json!(reply);
    fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
    let second = log_acknowledgement(&mut ctx, &second_args, &path).unwrap();
    assert_ne!(first["message_id"], second["message_id"]);
    let server = f.server.lock().unwrap();
    let tail = &server.posts[server.posts.len() - 2..];
    assert_ne!(tail[0].1["nonce"], tail[1].1["nonce"]);
    assert_ne!(tail[0].1["content"], tail[1].1["content"]);
}

#[test]
fn owner_receipt_rejects_long_whitespace_without_multiple_posts() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, mut proof) = receipt_case(&f, &mut ctx);
    let body = format!("{}受け取りました。", " ".repeat(4000));
    fs::write(&args.body_file, &body).unwrap();
    proof["body_sha256"] = json!(format!("{:x}", Sha256::digest(body.as_bytes())));
    fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
    let before = f.server.lock().unwrap().posts.len();
    assert!(log_acknowledgement(&mut ctx, &args, &path).is_err());
    assert_eq!(f.server.lock().unwrap().posts.len(), before);
}

#[test]
fn owner_receipt_does_not_reuse_a_different_edited_version_marker() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, proof) = receipt_case(&f, &mut ctx);
    {
        let mut server = f.server.lock().unwrap();
        let thread = args.thread.as_ref().unwrap();
        server.messages.get_mut(thread).unwrap().push(json!({"id":"9000", "channel_id":thread,
            "author":{"id":BOT,"bot":true},"type":19,
            "content":format!("受け取りました。\n\n-# recovery:other:log:0\n-# owner-receipt:{}:{}",proof["reply_message_id"].as_str().unwrap(),"0".repeat(24)),
            "timestamp":"2026-10-12T00:00:02Z","flags":SILENT,"mentions":[],"mention_roles":[],
            "mention_everyone":false,"attachments":[],"message_reference":{"message_id":proof["reply_message_id"],"channel_id":thread}}));
    }
    let result = check_acknowledgement(&mut ctx, &args, &path).unwrap();
    assert!(result["existing_acknowledgement"].is_null());
}

fn explicit_close_record(f: &Fixture, proof: &Value) {
    let mut close = CloseLedger::load(&f.home).unwrap();
    close
        .save(
            &f.home,
            CloseRecord {
                binding: CloseBinding {
                    request_id: core::s(&proof["question_request_id"]).into(),
                    thread_id: core::s(&proof["thread_id"]).into(),
                    question_message_id: core::s(&proof["question_message_id"]).into(),
                    origin_todo_id: 2472,
                    todo_id: 2472,
                    message_id: core::s(&proof["reply_message_id"]).into(),
                    author_id: OWNER.into(),
                    job_id: "fixture-job".into(),
                    revision: "a".repeat(40),
                    question_fingerprint: core::s(&proof["question_fingerprint"]).into(),
                    reply_sha256: "a".repeat(64),
                    receipt_sha256: "a".repeat(64),
                    verification_sha256: "a".repeat(64),
                    completion: "fixture complete".into(),
                },
                phase: "verified".into(),
                outcome: Some("closed".into()),
                pending_reply_ids: vec![],
                patch_attempted: true,
            },
        )
        .unwrap();
}

#[test]
fn owner_receipt_cannot_reopen_explicitly_closed_question_but_can_recover_old_effect() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, proof) = receipt_case(&f, &mut ctx);
    explicit_close_record(&f, &proof);
    let posts = f.server.lock().unwrap().posts.len();
    assert!(log_acknowledgement(&mut ctx, &args, &path)
        .unwrap_err()
        .contains("explicit close"));
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    assert!(Ledger::load(&f.home)
        .unwrap()
        .effect(&args.request_id, "log:0")
        .is_none());
    let other = Fixture::new();
    let mut other_ctx = other.context();
    let (args, path, proof) = receipt_case(&other, &mut other_ctx);
    log_acknowledgement(&mut other_ctx, &args, &path).unwrap();
    explicit_close_record(&other, &proof);
    let posts = other.server.lock().unwrap().posts.len();
    assert_eq!(
        log_acknowledgement(&mut other_ctx, &args, &path).unwrap()["delivery"],
        "verified"
    );
    assert_eq!(other.server.lock().unwrap().posts.len(), posts);
}
