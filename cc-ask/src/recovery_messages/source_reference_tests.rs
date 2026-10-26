use super::*;

const NORMAL: &str = "normal-ask-9620ba9f12adf45e1a01ee67010dafe616750231a1625ffcca0c7ed470a6a862";

fn qualified(f: &Fixture) -> AskArgs {
    let mut args = f.ask(
        "conversation-v1-c5fd3cd83bfb64d7b04e0701b4e178ad",
        "回答が必要：同じ条件でa/bを選んでください。",
    );
    args.todo_id = 1171;
    args.origin_thread = Some("106".into());
    args.conversation_source_request = Some(NORMAL.into());
    let mut decision: Value =
        serde_json::from_slice(&fs::read(&args.decision_file).unwrap()).unwrap();
    decision["todo_id"] = json!(1171);
    decision["origin_thread_id"] = json!("106");
    fs::write(&args.decision_file, decision.to_string()).unwrap();
    let request = prepare_ask(&f.context(), &args).unwrap();
    let proof = json!({"domain":"discord-conversation-origin-reference-v1","verified":true,
        "normal_request_id":NORMAL,"sender_request_id":request.request_id,"todo_id":1171,
        "actor_todo_id":3277,"actor_job_id":"actual-3277","origin_thread_id":"106",
        "input_sha256":"a".repeat(64),"body_sha256":format!("{:x}",Sha256::digest(request.body.as_bytes())),
        "decision_sha256":digest(&decision).unwrap(),"title":request.title,"succession_sha256":"b".repeat(64),
        "guild_id":GUILD,"forum_id":FORUM,"owner_id":OWNER,"bot_id":BOT});
    let mut state = f.server.lock().unwrap();
    state.source_reference_proof = Some(proof);
    state.messages.insert(
        "106".into(),
        vec![json!({"id":"106","channel_id":"106",
        "author":{"id":BOT,"bot":true},"type":0,"content":"retained original question"})],
    );
    args
}

#[test]
fn qualified_source_is_read_only_and_same_retry_is_deduplicated() {
    let f = Fixture::new();
    let args = qualified(&f);
    let request = prepare_ask(&f.context(), &args).unwrap();
    let first = ask(&mut f.context(), &args).unwrap();
    assert_eq!(first["delivery"], "verified");
    assert_eq!(first["origin_reference_only"], true);
    assert_eq!(first["original_fingerprint"], request.fingerprint);
    let before = f.server.lock().unwrap().posts.len();
    let again = ask(&mut f.context(), &args).unwrap();
    assert_eq!(first["thread_id"], again["thread_id"]);
    let state = f.server.lock().unwrap();
    assert_eq!(state.posts.len(), before);
    assert_eq!(state.source_reference_checks, 2);
    assert!(state
        .calls
        .iter()
        .filter(|(method, _, _)| method != "GET")
        .all(|(_, path, _)| !path.starts_with("/channels/106/")));
    assert_eq!(state.posts.len(), 3); // New question, progress, and links on new question only.
    drop(state);
    let ledger = Ledger::load(&f.home).unwrap();
    assert!(ledger.effect(&args.request_id, "origin-links").is_none());
    f.unchanged();
}

#[test]
fn qualified_source_rejects_mismatch_before_post_and_keeps_legacy_write_restriction() {
    for change in [
        "request", "body", "decision", "origin", "todo", "actor", "bot", "forum", "owner",
        "native", "webhook", "no-proof", "no-flag",
    ] {
        let f = Fixture::new();
        let mut args = qualified(&f);
        {
            let mut state = f.server.lock().unwrap();
            let proof = state.source_reference_proof.as_mut().unwrap();
            match change {
                "request" => proof["sender_request_id"] = json!("other"),
                "body" => proof["body_sha256"] = json!("changed"),
                "decision" => proof["decision_sha256"] = json!("changed"),
                "origin" => proof["origin_thread_id"] = json!("999"),
                "todo" => proof["todo_id"] = json!(1889),
                "actor" => proof["actor_todo_id"] = json!(999),
                "bot" => proof["bot_id"] = json!("999"),
                "forum" => proof["forum_id"] = json!("999"),
                "owner" => proof["owner_id"] = json!("999"),
                "native" => proof["verified"] = json!(false),
                "webhook" => state.messages.get_mut("106").unwrap()[0]["webhook_id"] = json!("999"),
                "no-proof" => state.source_reference_proof = None,
                "no-flag" => args.conversation_source_request = None,
                _ => unreachable!(),
            }
        }
        assert!(ask(&mut f.context(), &args).is_err(), "{change}");
        assert!(f.server.lock().unwrap().posts.is_empty(), "{change}");
        f.unchanged();
    }
}

#[test]
fn source_failure_diagnostics_are_only_fixed_codes() {
    for (error, stage) in [("conversation source reference is unverified", "source_scope"),
        ("origin must be the exact owner request or a verified recovery post; legacy questions are excluded", "origin_scope"),
        ("recovery-messages.lock is busy", "busy"), ("URLError: opaque detail", "network"),
        ("Discord API rate limited", "rate_limited"), ("untrusted private question text", "operation_pending")] {
        assert_eq!(ask_failure_report(error), json!({"domain":DOMAIN,"delivery":"not_verified","failure_stage":stage}));
    }
}

#[test]
fn real_http_boundaries_preserve_transport_stage_and_unknown_post_receipt() {
    for (raw, stage) in [
        ("URLError: fixture-private-network-detail", "network"),
        ("TimeoutExpired: fixture-private-timeout-detail", "network"),
        ("Discord API rate limited", "rate_limited"),
        ("Discord API rate limited; retry later", "rate_limited"),
        (
            "Discord upload rate limited: private-detail",
            "rate_limited",
        ),
    ] {
        for (method, path) in [
            ("GET", "/users/@me".to_owned()),
            ("GET", format!("/channels/{FORUM}")),
            ("GET", "/channels/106".to_owned()),
            ("POST", format!("/channels/{FORUM}/threads")),
        ] {
            let f = Fixture::new();
            let args = qualified(&f);
            f.server.lock().unwrap().transport_failure = Some((method.into(), path));
            f.server.lock().unwrap().transport_error = Some(raw.into());
            let error = ask(&mut f.context(), &args).unwrap_err();
            assert_eq!(
                ask_failure_report(&error)["failure_stage"],
                stage,
                "{method}: {error}"
            );
            assert!(!error.contains("fixture-private"));
            f.server.lock().unwrap().transport_failure = None;
            if method == "POST" {
                let ledger = Ledger::load(&f.home).unwrap();
                assert!(ledger.effect(&args.request_id, "question:0").is_some());
                assert!(ledger.receipt(&args.request_id, "question:0").is_none());
                let retry = ask(&mut f.context(), &args).unwrap_err();
                assert_eq!(
                    ask_failure_report(&retry)["failure_stage"],
                    "operation_pending"
                );
                assert!(f.server.lock().unwrap().posts.is_empty());
            } else {
                assert_eq!(
                    ask(&mut f.context(), &args).unwrap()["delivery"],
                    "verified"
                );
            }
            f.unchanged();
        }
    }
}

#[test]
fn source_reader_deadline_is_distinct_from_scope_rejection_before_http() {
    let f = Fixture::new();
    let args = qualified(&f);
    f.server.lock().unwrap().source_reference_timeout = true;
    let error = ask(&mut f.context(), &args).unwrap_err();
    assert_eq!(ask_failure_report(&error)["failure_stage"], "deadline");
    assert!(!error.contains("private-checker"));
    assert!(f.server.lock().unwrap().calls.is_empty());
    assert!(f.server.lock().unwrap().posts.is_empty());
    f.unchanged();
}
