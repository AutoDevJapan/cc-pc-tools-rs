use super::*;

fn lookup_fixture() -> (Fixture, Context, AskArgs, Value) {
    let fixture = Fixture::new();
    fs::write(
        fixture.home.join("conversation-mode.json"),
        r#"{"domain":"discord-conversation-mode-v1","enabled":true}"#,
    )
    .unwrap();
    let mut ctx = fixture.context();
    let args = fixture.ask(
        "lookup-original",
        "保持範囲を選んでください。\na 保持\nb 整理",
    );
    let sent = ask(&mut ctx, &args).unwrap();
    (fixture, ctx, args, sent)
}

fn normal_lookup(ctx: &mut Context, args: &AskArgs) -> Result<Value> {
    crate::decision_identity::lookup(ctx, &core::read_json(&args.decision_file)?)
}

#[test]
fn normal_lookup_finds_current_answer_without_writes_or_another_question() {
    let (f, mut ctx, args, sent) = lookup_fixture();
    let id = f.add_reply(
        core::s(&sent["thread_id"]),
        OWNER,
        "a 残す方でお願いします",
        false,
    );
    let ledger = fs::read(f.home.join(LEDGER)).unwrap();
    let posts = f.server.lock().unwrap().posts.len();
    let lookup = normal_lookup(&mut ctx, &args).unwrap();
    assert_eq!(
        lookup["found"], true,
        "normal lookup lost the new conversation answer"
    );
    assert_eq!(lookup["suppressed"], "already_answered");
    assert_eq!(lookup["url"], sent["url"]);
    assert_eq!(lookup["original_question"], sent["original_question"]);
    assert_eq!(lookup["owner_replies"][0]["message_id"], id);
    assert_eq!(lookup["owner_replies"][0]["author_id"], OWNER);
    assert_eq!(lookup["owner_replies"][0]["body"], "a 残す方でお願いします");
    assert_eq!(lookup["approval"], "not_inferred");
    for _ in 0..2 {
        let mut restarted = f.context();
        let mut input = core::read_json(&args.decision_file).unwrap();
        input["caller_todo_id"] = json!(9999);
        input["posting_job"] = json!("different-worker-session");
        input["title"] = json!("言い換えた題名");
        assert_eq!(
            crate::decision_identity::lookup(&mut restarted, &input).unwrap()["owner_replies"],
            lookup["owner_replies"]
        );
    }
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), ledger);
    // Positive control: this same fixture already submitted the original question.
    assert!(posts > 0);
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    assert!(!f.home.join("question-progress.lock").exists());
    let renamed = f.ask(
        "lookup-retry-new-agent",
        "別の言い回しです。\na 残す\nb 整理",
    );
    assert_eq!(
        ask(&mut ctx, &renamed).unwrap()["thread_id"],
        sent["thread_id"]
    );
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
    f.unchanged();
}

#[test]
fn normal_lookup_keeps_new_scope_resource_image_and_original_todo_distinct() {
    let (f, mut ctx, args, _) = lookup_fixture();
    let original = core::read_json(&args.decision_file).unwrap();
    let calls = f.server.lock().unwrap().calls.len();
    for which in ["scope", "image", "resource", "todo"] {
        let mut changed = original.clone();
        match which {
            "scope" => changed["scope"]["retain_incomplete"] = json!(false),
            "image" => changed["scope"]["image_sha256"] = json!("a".repeat(64)),
            "resource" => changed["resources"][0]["id"] = json!("other-resource"),
            _ => changed["todo_id"] = json!(9999),
        }
        let result = crate::decision_identity::lookup(&mut ctx, &changed).unwrap();
        assert_eq!(
            result["found"], false,
            "cross-scope answer reused for {which}"
        );
        assert!(result.get("owner_replies").is_none());
    }
    assert_eq!(f.server.lock().unwrap().calls.len(), calls);
    f.unchanged();
}

#[test]
fn normal_lookup_preserves_waiting_and_current_free_text_edits_without_approval() {
    let (f, mut ctx, args, sent) = lookup_fixture();
    let thread = core::s(&sent["thread_id"]);
    assert_eq!(
        normal_lookup(&mut ctx, &args).unwrap()["suppressed"],
        "existing_question"
    );
    let id = f.add_reply(thread, OWNER, "a", false);
    let first = normal_lookup(&mut ctx, &args).unwrap();
    {
        let mut server = f.server.lock().unwrap();
        let reply = server
            .messages
            .get_mut(thread)
            .unwrap()
            .iter_mut()
            .find(|m| m["id"] == id)
            .unwrap();
        reply["content"] = json!("訂正します。作業を止めてください。");
        reply["edited_timestamp"] = json!("2026-10-12T00:00:02Z");
    }
    let revised = normal_lookup(&mut ctx, &args).unwrap();
    assert_ne!(first["owner_replies"], revised["owner_replies"]);
    assert_eq!(
        revised["owner_replies"][0]["body"],
        "訂正します。作業を止めてください。"
    );
    assert_eq!(
        revised["reply_versions"][id.as_str()]["edited_timestamp"],
        "2026-10-12T00:00:02Z"
    );
    for body in ["", "<@101>"] {
        f.add_reply(thread, OWNER, body, false);
        let current = normal_lookup(&mut ctx, &args).unwrap();
        assert_eq!(current["suppressed"], "already_answered");
        assert_eq!(current["approval"], "not_inferred");
        assert_eq!(
            current["owner_replies"].as_array().unwrap().last().unwrap()["body"],
            body
        );
    }
    f.unchanged();
}

#[test]
fn normal_lookup_retains_pending_intent_and_rejects_corrupt_or_foreign_input() {
    let f = Fixture::new();
    fs::write(
        f.home.join("conversation-mode.json"),
        r#"{"domain":"discord-conversation-mode-v1","enabled":true}"#,
    )
    .unwrap();
    let mut ctx = f.context();
    let args = f.ask("pending-lookup", "仕様を選んでください。");
    let incoming = prepare_ask(&ctx, &args).unwrap();
    let mut ledger = Ledger::load(&f.home).unwrap();
    ledger
        .append(
            &f.home,
            Event::Intent {
                request: Box::new(incoming),
            },
        )
        .unwrap();
    let pending = normal_lookup(&mut ctx, &args).unwrap();
    assert_eq!(pending["found"], true);
    assert_eq!(pending["suppressed"], "existing_question_pending");
    assert_eq!(pending["delivery"], "not_sent");
    for key in ["owner", "guild", "parent"] {
        let mut input = core::read_json(&args.decision_file).unwrap();
        input[key] = json!("999");
        assert!(crate::decision_identity::lookup(&mut ctx, &input).is_err());
    }
    fs::write(f.home.join(LEDGER), b"{corrupt").unwrap();
    assert!(normal_lookup(&mut ctx, &args).is_err());
    assert!(f.server.lock().unwrap().calls.is_empty());
    f.unchanged();
}
