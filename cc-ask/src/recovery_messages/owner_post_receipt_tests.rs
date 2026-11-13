use super::*;

fn post_case(f: &Fixture, ctx: &mut Context) -> (LogArgs, PathBuf, Value) {
    let message = f.add_reply(ORIGIN, OWNER, "https://example.test/read-this", false);
    let mut args = f.log(
        "conversation-log-original-unsent",
        "受け取りました。Skills化の依頼として元の管理担当へ伝えました。\n",
        false,
    );
    args.thread = Some(ORIGIN.into());
    assert!(log(ctx, &args).unwrap_err().contains("explicit log thread"));
    let hash = |s: &str| format!("{:x}", Sha256::digest(s.as_bytes()));
    let proof = json!({"domain":"discord-owner-post-receipt-v1","purpose":"acknowledge_owner_post",
        "request_id":args.request_id,"guild_id":GUILD,"request_forum_id":REQUEST_FORUM,
        "thread_id":ORIGIN,"owner_id":OWNER,"bot_id":BOT,"message_id":message,
        "message_body_sha256":hash("https://example.test/read-this"),
        "message_timestamp":"2026-10-12T00:00:01+00:00","message_edited_timestamp":null,
        "body_sha256":hash(&fs::read_to_string(&args.body_file).unwrap())});
    let path = f.root.path().join("standalone-owner-post.json");
    fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
    (args, path, proof)
}

#[test]
fn owner_post_receipt_resumes_original_request_and_normal_readback_without_repost() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, _) = post_case(&f, &mut ctx);
    let original = Ledger::load(&f.home)
        .unwrap()
        .request(&args.request_id)
        .unwrap()
        .clone();
    let before = fs::read(f.home.join(LEDGER)).unwrap();
    let posts = f.server.lock().unwrap().posts.len();
    assert_eq!(
        check_owner_post_receipt(&mut ctx, &args, &path).unwrap()["delivery"],
        "not_sent"
    );
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
    assert!(!f.home.join("recovery-owner-acknowledgements.json").exists());
    let sent = log_owner_post_receipt(&mut ctx, &args, &path).unwrap();
    let repeated = log_owner_post_receipt(&mut ctx, &args, &path).unwrap();
    let normal = log(&mut ctx, &args).unwrap();
    assert_eq!(sent["delivery"], "verified");
    assert_eq!(sent["message_id"], repeated["message_id"]);
    assert_eq!(sent["message_id"], normal["message_id"]);
    assert_eq!(
        Ledger::load(&f.home).unwrap().request(&args.request_id),
        Some(&original)
    );
    let server = f.server.lock().unwrap();
    assert_eq!(server.posts.len(), posts + 1);
    let payload = &server.posts.last().unwrap().1;
    assert_eq!(payload["allowed_mentions"]["replied_user"], false);
    assert_eq!(payload["allowed_mentions"]["users"], json!([]));
    assert_eq!(payload["message_reference"]["message_id"], "1001");
    assert_eq!(payload["flags"].as_u64().unwrap() & SILENT, SILENT);
    drop(server);
    let other = LogArgs {
        thread: args.thread.clone(),
        ..f.log("ordinary-log", "進捗です。", false)
    };
    assert!(log(&mut ctx, &other).is_err());
    let question = f.ask("after-owner-read-receipt", "続ける内容を選んでください。");
    assert_eq!(ask(&mut ctx, &question).unwrap()["delivery"], "verified");
    f.unchanged();
}

#[test]
fn owner_post_receipt_rejects_wrong_source_destination_text_and_mixed_purpose() {
    for field in [
        "domain",
        "purpose",
        "request_id",
        "guild_id",
        "request_forum_id",
        "thread_id",
        "bot_id",
        "owner_id",
        "message_id",
        "message_body_sha256",
        "message_timestamp",
        "message_edited_timestamp",
        "body_sha256",
    ] {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, path, mut proof) = post_case(&f, &mut ctx);
        proof[field] = match field {
            "message_timestamp" | "message_edited_timestamp" => json!("2026-10-12T00:00:03Z"),
            key if key.ends_with("sha256") => json!("f".repeat(64)),
            _ => json!("999"),
        };
        fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
        let before = fs::read(f.home.join(LEDGER)).unwrap();
        assert!(
            log_owner_post_receipt(&mut ctx, &args, &path).is_err(),
            "{field}"
        );
        assert!(f.server.lock().unwrap().posts.is_empty(), "{field}");
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before, "{field}");
    }
    let f = Fixture::new();
    let mut ctx = f.context();
    let (mut args, path, mut proof) = post_case(&f, &mut ctx);
    assert!(log_acknowledgement(&mut ctx, &args, &path).is_err());
    args.notify = true;
    assert!(log_owner_post_receipt(&mut ctx, &args, &path).is_err());
    args.notify = false;
    fs::write(&args.body_file, "勝手なログ本文").unwrap();
    proof["body_sha256"] = json!(format!("{:x}", Sha256::digest("勝手なログ本文".as_bytes())));
    fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
    assert!(log_owner_post_receipt(&mut ctx, &args, &path).is_err());
    assert!(f.server.lock().unwrap().posts.is_empty());
}

#[test]
fn owner_post_receipt_live_edit_before_post_never_sends_but_posted_recovery_survives_edit() {
    for sent_first in [false, true] {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, path, proof) = post_case(&f, &mut ctx);
        let first = sent_first.then(|| log_owner_post_receipt(&mut ctx, &args, &path).unwrap());
        let posts = f.server.lock().unwrap().posts.len();
        let mut server = f.server.lock().unwrap();
        let original = server
            .messages
            .get_mut(ORIGIN)
            .unwrap()
            .iter_mut()
            .find(|m| m["id"] == proof["message_id"])
            .unwrap();
        original["content"] = json!("edited");
        original["edited_timestamp"] = json!("2026-10-12T00:00:03Z");
        drop(server);
        let result = log_owner_post_receipt(&mut ctx, &args, &path);
        if let Some(first) = first {
            assert_eq!(result.unwrap()["message_id"], first["message_id"]);
        } else {
            assert!(result.is_err());
        }
        assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    }
}

#[test]
fn owner_post_receipt_lost_response_and_other_path_receipt_are_not_posted_twice() {
    for prior_path in [false, true] {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, path, proof) = post_case(&f, &mut ctx);
        if prior_path {
            f.server
                .lock()
                .unwrap()
                .messages
                .get_mut(ORIGIN)
                .unwrap()
                .push(json!({
                "id":"9000","channel_id":ORIGIN,"author":{"id":BOT,"bot":true},"type":19,
                "content":"受け取りました。","timestamp":"2026-10-12T00:00:02Z","flags":SILENT,
                "mentions":[],"mention_roles":[],"mention_everyone":false,"attachments":[],
                "message_reference":{"message_id":proof["message_id"],"channel_id":ORIGIN}}));
        } else {
            let mut server = f.server.lock().unwrap();
            server.drop_response = true;
            server.block_reads_after_post = true;
        }
        let initial = log_owner_post_receipt(&mut ctx, &args, &path);
        if prior_path {
            assert_eq!(initial.unwrap()["existing_acknowledgement"], true);
        } else {
            assert!(initial.is_err());
        }
        {
            let mut server = f.server.lock().unwrap();
            server.blocked = false;
            server.block_reads_after_post = false;
        }
        assert_eq!(
            log_owner_post_receipt(&mut ctx, &args, &path).unwrap()["delivery"],
            "verified"
        );
        assert_eq!(
            f.server.lock().unwrap().posts.len(),
            usize::from(!prior_path)
        );
        f.unchanged();
    }
}

#[test]
fn owner_post_receipt_rechecks_edit_at_post_boundary_and_rejects_request_rebinding() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, proof) = post_case(&f, &mut ctx);
    f.server.lock().unwrap().owner_post_edit_on_get =
        Some((ORIGIN.into(), core::s(&proof["message_id"]).into(), 3));
    assert!(log_owner_post_receipt(&mut ctx, &args, &path).is_err());
    assert!(f.server.lock().unwrap().posts.is_empty());
    f.unchanged();

    let f = Fixture::new();
    let mut ctx = f.context();
    let (args, path, mut proof) = post_case(&f, &mut ctx);
    log_owner_post_receipt(&mut ctx, &args, &path).unwrap();
    let alias = LogArgs {
        thread: args.thread.clone(),
        ..f.log(
            "different-request",
            &fs::read_to_string(&args.body_file).unwrap(),
            false,
        )
    };
    proof["request_id"] = json!(alias.request_id);
    fs::write(&path, serde_json::to_vec(&proof).unwrap()).unwrap();
    let count = f.server.lock().unwrap().posts.len();
    assert!(log_owner_post_receipt(&mut ctx, &alias, &path).is_err());
    assert_eq!(f.server.lock().unwrap().posts.len(), count);
}

#[test]
fn owner_post_receipt_allows_omitted_optional_bot_flag_but_rejects_non_owner_bot_values() {
    for value in [
        None,
        Some(json!(false)),
        Some(json!(true)),
        Some(json!("false")),
    ] {
        let f = Fixture::new();
        let mut ctx = f.context();
        let (args, path, proof) = post_case(&f, &mut ctx);
        let allowed = value.is_none() || value == Some(json!(false));
        let mut server = f.server.lock().unwrap();
        let original = server
            .messages
            .get_mut(ORIGIN)
            .unwrap()
            .iter_mut()
            .find(|m| m["id"] == proof["message_id"])
            .unwrap();
        match value {
            None => {
                original["author"].as_object_mut().unwrap().remove("bot");
            }
            Some(value) => {
                original["author"]["bot"] = value;
            }
        }
        drop(server);
        let result = log_owner_post_receipt(&mut ctx, &args, &path);
        assert_eq!(result.is_ok(), allowed);
        assert_eq!(f.server.lock().unwrap().posts.len(), usize::from(allowed));
    }
}
