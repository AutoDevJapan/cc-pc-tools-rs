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

#[test]
fn normal_lookup_redacts_synthetic_client_names_in_every_output_branch() {
    let (f, mut ctx, args, sent) = lookup_fixture();
    fs::write(&ctx.client_projects, "ExampleClient3392\n").unwrap();
    f.add_reply(
        core::s(&sent["thread_id"]),
        OWNER,
        "ExampleClient3392 は保持してください",
        false,
    );
    let result = normal_lookup(&mut ctx, &args).unwrap();
    assert_eq!(result["found"], true);
    assert!(!result.to_string().contains("ExampleClient3392"));
    assert!(result["owner_replies"][0]["body"]
        .as_str()
        .unwrap()
        .contains("クライアント案件"));
    let mut different = core::read_json(&args.decision_file).unwrap();
    different["resources"][0]["id"] = json!("ExampleClient3392");
    let missing = crate::decision_identity::lookup(&mut ctx, &different).unwrap();
    assert_eq!(missing["found"], false);
    assert!(!missing.to_string().contains("ExampleClient3392"));
    f.unchanged();
}

#[test]
fn normal_lookup_missing_redaction_registry_keeps_answer_reference_without_text() {
    let (f, mut ctx, args, sent) = lookup_fixture();
    let id = f.add_reply(
        core::s(&sent["thread_id"]),
        OWNER,
        "ExampleClient3392 は保持してください",
        false,
    );
    fs::remove_file(&ctx.client_projects).unwrap();
    let before = fs::read(f.home.join(LEDGER)).unwrap();
    let result = normal_lookup(&mut ctx, &args).unwrap();
    assert_eq!(result["found"], true);
    assert_eq!(result["suppressed"], "already_answered");
    assert_eq!(result["url"], sent["url"]);
    assert_eq!(result["text_available"], false);
    assert!(!result.to_string().contains("ExampleClient3392"));
    assert!(result.get("original_question").is_none());
    assert!(result.get("original_decision").is_none());
    assert_eq!(result["owner_replies"][0]["message_id"], id);
    assert_eq!(result["owner_replies"][0]["author_id"], OWNER);
    assert!(result["owner_replies"][0].get("body").is_none());
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
    assert!(!ctx.client_projects.exists());
    f.unchanged();
}

#[test]
fn normal_lookup_posted_but_unverified_question_is_pending_never_not_sent() {
    for lost_response in [false, true] {
        let f = Fixture::new();
        fs::write(
            f.home.join("conversation-mode.json"),
            r#"{"domain":"discord-conversation-mode-v1","enabled":true}"#,
        )
        .unwrap();
        let mut ctx = f.context();
        let args = f.ask("lookup-lost-post", "仕様を選んでください。");
        {
            let mut server = f.server.lock().unwrap();
            server.drop_response = lost_response;
            server.block_reads_after_post = true;
        }
        assert!(ask(&mut ctx, &args).is_err());
        let ledger = fs::read(f.home.join(LEDGER)).unwrap();
        let posts = f.server.lock().unwrap().posts.len();
        let calls = f.server.lock().unwrap().calls.len();
        assert!(
            posts > 0,
            "positive control: the question reached the mock server"
        );
        let result = normal_lookup(&mut ctx, &args).unwrap();
        assert_eq!(result["found"], true);
        assert_eq!(result["suppressed"], "existing_question_pending");
        assert_eq!(result["delivery"], "pending");
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), ledger);
        assert_eq!(f.server.lock().unwrap().posts.len(), posts);
        assert_eq!(f.server.lock().unwrap().calls.len(), calls);
        f.unchanged();
    }
}

#[test]
fn normal_lookup_archived_reply_is_get_only_and_api_failure_is_not_no_answer() {
    let (f, mut ctx, args, sent) = lookup_fixture();
    let thread = core::s(&sent["thread_id"]);
    f.server.lock().unwrap().channels.get_mut(thread).unwrap()["thread_metadata"]["archived"] =
        json!(true);
    let id = f.add_reply(thread, OWNER, "<@101>", false);
    let before = fs::read(f.home.join(LEDGER)).unwrap();
    let calls = f.server.lock().unwrap().calls.len();
    let result = normal_lookup(&mut ctx, &args).unwrap();
    assert_eq!(result["found"], true);
    assert_eq!(result["owner_replies"][0]["message_id"], id);
    {
        let server = f.server.lock().unwrap();
        assert!(server.calls.len() > calls);
        assert!(server.calls[calls..]
            .iter()
            .all(|(method, _, _)| method == "GET"));
    }
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
    f.server.lock().unwrap().blocked = true;
    assert!(normal_lookup(&mut ctx, &args).is_err());
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
    f.unchanged();
}

#[test]
fn normal_lookup_invalid_mode_fails_before_discord_without_legacy_fallback() {
    let (f, mut ctx, args, _) = lookup_fixture();
    fs::write(f.home.join("conversation-mode.json"), b"{interrupted").unwrap();
    let before = fs::read(f.home.join(LEDGER)).unwrap();
    let calls = f.server.lock().unwrap().calls.len();
    assert!(normal_lookup(&mut ctx, &args).is_err());
    assert_eq!(f.server.lock().unwrap().calls.len(), calls);
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
    assert!(!f.home.join("question-progress.lock").exists());
    f.unchanged();
}

#[test]
fn normal_lookup_redaction_cannot_change_protocol_keys_or_delivery_values() {
    for name in ["found", "answered"] {
        let (f, mut ctx, args, sent) = lookup_fixture();
        let id = f.add_reply(core::s(&sent["thread_id"]), OWNER, "a", false);
        fs::write(&ctx.client_projects, format!("{name}\n")).unwrap();
        let before = fs::read(f.home.join(LEDGER)).unwrap();
        let result = normal_lookup(&mut ctx, &args).unwrap();
        assert_eq!(result["found"], true, "redaction changed a machine field");
        assert_eq!(result["suppressed"], "already_answered");
        assert_eq!(result["approval"], "not_inferred");
        assert_eq!(result["url"], sent["url"]);
        assert_eq!(result["owner_replies"][0]["message_id"], id);
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), before);
        f.unchanged();
    }
}

#[test]
fn normal_lookup_marks_display_redaction_and_does_not_call_pending_text_available() {
    let (f, mut ctx, args, sent) = lookup_fixture();
    fs::write(&ctx.client_projects, "ExampleClient3392\n").unwrap();
    f.add_reply(
        core::s(&sent["thread_id"]),
        OWNER,
        "ExampleClient3392 は保持してください",
        false,
    );
    let result = normal_lookup(&mut ctx, &args).unwrap();
    assert_eq!(result["text_redacted"], true);
    assert_eq!(result["text_available"], true);
    assert!(result["next_action"].as_str().unwrap().contains("私的原票"));
    let mut changed = core::read_json(&args.decision_file).unwrap();
    changed["resources"][0]["id"] = json!("unasked-resource");
    let missing = crate::decision_identity::lookup(&mut ctx, &changed).unwrap();
    assert_eq!(missing["found"], false);
    assert_eq!(missing["text_available"], false);
    f.unchanged();
}
