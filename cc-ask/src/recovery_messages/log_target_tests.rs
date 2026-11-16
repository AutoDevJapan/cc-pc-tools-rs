use super::*;

fn failed_log(f: &Fixture, ctx: &mut Context, id: &str) -> LogArgs {
    let mut args = f.log(id, "対応不要。通常の進捗です。", false);
    args.thread = Some("106".into());
    assert!(log(ctx, &args).unwrap_err().contains("explicit log thread"));
    let ledger = Ledger::load(&f.home).unwrap();
    assert!(ledger.request(id).is_some());
    assert!(ledger.effect(id, "log:0").is_none());
    args
}

fn target(f: &Fixture, ctx: &mut Context, id: &str) -> String {
    core::s(&log(ctx, &f.log(id, "対応不要。既存の製品記録です。", false)).unwrap()["thread_id"]).to_owned()
}

/// The separate baseline snippet uses only the old public log entry.
/// This after-fix suite additionally verifies the typed correction history.
#[test]
fn same_request_wrong_unposted_log_target_is_corrected_and_delivers_once() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let thread = target(&f, &mut ctx, "verified-log-target");
    let mut args = failed_log(&f, &mut ctx, "same-request-log");
    let original = Ledger::load(&f.home).unwrap().request(&args.request_id).unwrap().clone();
    let posts = f.server.lock().unwrap().posts.len();
    args.thread = Some(thread.clone());
    let first = log(&mut ctx, &args).unwrap();
    assert_eq!(first["delivery"], "verified");
    assert_eq!(first["request_id"], args.request_id);
    assert_eq!(first["thread_id"], thread);
    assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
    assert_eq!(log(&mut ctx, &args).unwrap()["message_id"], first["message_id"]);
    assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
    let mut duplicate = f.log("new-key-same-corrected-log", "対応不要。通常の進捗です。", false);
    duplicate.thread = Some(thread);
    let reused = log(&mut ctx, &duplicate).unwrap();
    assert_eq!(reused["message_id"], first["message_id"]);
    assert_eq!(reused["reused"], true);
    assert_eq!(f.server.lock().unwrap().posts.len(), posts + 1);
    let ledger = Ledger::load(&f.home).unwrap();
    assert!(ledger.events.iter().any(|event| matches!(event, Event::Intent{request} if **request == original)));
    assert_eq!(ledger.events.iter().filter(|event| matches!(event,Event::LogTargetCorrected{request,..} if request.request_id == args.request_id)).count(), 1);
    assert_eq!(ledger.events.iter().filter(|event| matches!(event,Event::OriginCorrected{request} if request.request_id == args.request_id)).count(), 0);
    f.unchanged();
}

#[test]
fn ordinary_log_inspection_reads_state_without_journal_or_http_changes() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let thread = target(&f, &mut ctx, "inspect-target");
    let mut args = failed_log(&f, &mut ctx, "inspect-same-request");
    args.thread = Some(thread);
    let before = fs::read(f.home.join(LEDGER)).unwrap();
    let before_lock = fs::read(f.home.join(LOCK)).unwrap();
    let calls = f.server.lock().unwrap().calls.len();
    let value = inspect_log(&mut ctx, &args).unwrap();
    assert_eq!(value["delivery"],"inspection_only");
    assert_eq!(value["intent_exists"],true);
    assert_eq!(value["prior_kind"],"log");
    assert_eq!(value["prior_thread"],"106");
    assert_eq!(value["possible_post"],false);
    assert_eq!(value["reserved_effect_count"],0);
    assert_eq!(value["retarget_eligible"],true);
    assert_eq!(value["live_destination_verified"],false);
    assert_eq!(f.server.lock().unwrap().calls.len(),calls);
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(),before);
    assert_eq!(fs::read(f.home.join(LOCK)).unwrap(),before_lock);
    f.unchanged();
}

#[test]
fn log_target_change_keeps_every_other_request_field_immutable() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let thread = target(&f, &mut ctx, "immutable-target");
    let mut args = failed_log(&f, &mut ctx, "immutable-log");
    let ledger = Ledger::load(&f.home).unwrap();
    let prior = ledger.request(&args.request_id).unwrap().clone();
    args.thread = Some(thread);
    let (incoming,_) = prepare_log(&mut ctx,&args).unwrap();
    assert!(log_target::only_thread_changed(&prior,&incoming).unwrap());
    for fault in ["body","title","notify","destination","request-id","image","program","native","deadline","kind"] {
        let mut changed = incoming.clone();
        match fault {
            "body" => changed.body.push_str("変更"),
            "title" => changed.title.push_str("別題"),
            "notify" => changed.notify=true,
            "destination" => changed.destination.guild="999".into(),
            "request-id" => changed.request_id="other-request".into(),
            "image" => changed.images.push(ImageReceipt{filename:"x.png".into(),mime:"image/png".into(),size:1,sha256:"a".repeat(64)}),
            "program" => changed.program_evidence=Some(PathBuf::from("not-admitted")),
            "native" => changed.native_evidence=Some(PathBuf::from("not-admitted")),
            "deadline" => changed.send_before=Some("2099-01-01T00:00:00Z".into()),
            "kind" => changed.operation=Kind::Ask{todo_id:1,origin_thread:"999".into(),decision_original:json!({}),decision_identity:json!({})},
            _ => unreachable!(),
        }
        changed.fingerprint=changed.fingerprint().unwrap();
        assert!(!log_target::only_thread_changed(&prior,&changed).unwrap(),"{fault}");
    }
    f.unchanged();
}

#[test]
fn durable_possible_post_refuses_log_retarget_before_http() {
    let f = Fixture::new();
    let mut ctx = f.context();
    let thread = target(&f,&mut ctx,"pending-target");
    let mut args=failed_log(&f,&mut ctx,"possible-post-log");
    let mut ledger=Ledger::load(&f.home).unwrap();
    let prior=ledger.request(&args.request_id).unwrap().clone();
    ledger.append(&f.home,Event::Pending{request_id:prior.request_id.clone(),phase:"log:0".into(),
        effect:effect(&prior,"log:0",Target::Thread{thread:"106".into()},&prior.body,false,true).unwrap()}).unwrap();
    args.thread=Some(thread);
    let before=fs::read(f.home.join(LEDGER)).unwrap();
    let calls=f.server.lock().unwrap().calls.len();
    assert!(log(&mut ctx,&args).unwrap_err().contains("possible POST"));
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(),before);
    assert_eq!(f.server.lock().unwrap().calls.len(),calls);
    let status=inspect_log(&mut ctx,&args).unwrap();
    assert_eq!(status["possible_post"],true);
    assert_eq!(status["retarget_eligible"],false);
    f.unchanged();
}

#[test]
fn log_retarget_requires_stored_independent_ordinary_authority() {
    let f=Fixture::new();let mut ctx=f.context();
    let thread=target(&f,&mut ctx,"authority-target");
    let mut args=failed_log(&f,&mut ctx,"authority-log");
    let ledger=Ledger::load(&f.home).unwrap();
    let prior=ledger.request(&args.request_id).unwrap().clone();
    args.thread=Some(thread);
    let (incoming,_)=prepare_log(&mut ctx,&args).unwrap();
    let before=fs::read(f.home.join(LEDGER)).unwrap();
    for (id,phase,fp) in [
        ("missing","log:0",prior.fingerprint.clone()),
        (args.request_id.as_str(),"log:0",prior.fingerprint.clone()),
        ("authority-target","progress",prior.fingerprint.clone()),
        ("authority-target","log:0","0".repeat(64)),
    ] {
        let mut copy=Ledger{domain:ledger.domain.clone(),version:ledger.version,events:ledger.events.clone()};
        assert!(copy.append(&f.home,Event::LogTargetCorrected{request:Box::new(incoming.clone()),
            previous_fingerprint:fp,authority_request_id:id.into(),authority_phase:phase.into()}).is_err());
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(),before);
    }
    let mut unknown=f.log(&args.request_id,"対応不要。通常の進捗です。",false);unknown.thread=Some("107".into());
    assert!(log(&mut ctx,&unknown).is_err());
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(),before);
    f.unchanged();
}

#[test]
fn existing_authorized_log_target_cannot_be_silently_retargeted() {
    let f=Fixture::new();let mut ctx=f.context();
    let old=target(&f,&mut ctx,"already-authorized");
    let new=target(&f,&mut ctx,"different-authorized");
    let mut args=f.log("authorized-unsent","対応不要。保持する元記録です。",false);
    args.thread=Some(old);
    let (request,_)=prepare_log(&mut ctx,&args).unwrap();
    let mut ledger=Ledger::load(&f.home).unwrap();
    reserve(&mut ledger,&f.home,&request).unwrap();
    ledger.append(&f.home,Event::Bot{request_id:request.request_id.clone(),author_id:BOT.into()}).unwrap();
    let before=fs::read(f.home.join(LEDGER)).unwrap();
    args.thread=Some(new);
    assert!(log(&mut ctx,&args).unwrap_err().contains("already authorized"));
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(),before);
    f.unchanged();
}

#[test]
fn inspect_missing_existing_files_does_not_create_a_ledger_or_lock() {
    let f=Fixture::new();let mut ctx=f.context();
    let mut args=f.log("missing-state","対応不要。状態照合です。",false);args.thread=Some("106".into());
    assert!(inspect_log(&mut ctx,&args).is_err());
    assert!(!f.home.join(LEDGER).exists());
    assert!(!f.home.join(LOCK).exists());
    assert!(f.server.lock().unwrap().calls.is_empty());
    f.unchanged();
}

#[test]
fn log_target_repair_does_not_bypass_semantic_duplicates_or_aliases() {
    for alias in [false,true] {
        let f=Fixture::new();let mut ctx=f.context();
        let thread=target(&f,&mut ctx,"collision-target");
        let mut duplicate=f.log("already-bound-log","対応不要。通常の進捗です。",false);
        duplicate.thread=Some(thread.clone());
        if !alias { log(&mut ctx,&duplicate).unwrap(); }
        let mut args=failed_log(&f,&mut ctx,"collision-log");
        if alias {
            let mut ledger=Ledger::load(&f.home).unwrap();
            let prior=ledger.request(&args.request_id).unwrap().clone();
            ledger.append(&f.home,Event::Alias{request_id:"immutable-alias".into(),
                canonical_request_id:prior.request_id,fingerprint:prior.fingerprint}).unwrap();
        }
        args.thread=Some(thread);
        let before=fs::read(f.home.join(LEDGER)).unwrap();
        let calls=f.server.lock().unwrap().calls.len();
        assert!(log(&mut ctx,&args).is_err());
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(),before);
        assert_eq!(f.server.lock().unwrap().calls.len(),calls);
        assert_eq!(inspect_log(&mut ctx,&args).unwrap()["retarget_eligible"],false);
        f.unchanged();
    }
}

#[test]
fn saved_question_purpose_cannot_be_stripped_for_ordinary_target_repair() {
    let f=Fixture::new();let mut ctx=f.context();
    let ordinary=target(&f,&mut ctx,"ordinary-target");
    let (mut args,manifest,_)=clarification_case(&f,&mut ctx,"対応不要。元質問に固定した説明です。",true);
    let binding=clarification::Binding::read(&manifest).unwrap();
    let (request,_)=prepare_clarification(&mut ctx,&args).unwrap();
    let mut ledger=Ledger::load(&f.home).unwrap();
    let mut api=RecoveryApi::inspect(&mut ctx,&mut ledger,&request).unwrap();
    clarification::authorize(&mut api,&ledger,&f.home,&request,&binding,true,false).unwrap();
    drop(api);
    assert!(ledger.effect(&args.request_id,"log:0").is_none());
    args.thread=Some(ordinary);
    let before=fs::read(f.home.join(LEDGER)).unwrap();
    let calls=f.server.lock().unwrap().calls.len();
    assert!(log(&mut ctx,&args).unwrap_err().contains("purpose-bound"));
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(),before);
    assert_eq!(f.server.lock().unwrap().calls.len(),calls);
    let inspection=inspect_log(&mut ctx,&args).unwrap();
    assert_eq!(inspection["purpose_bound"],true);
    assert_eq!(inspection["retarget_eligible"],false);
    f.unchanged();
}
