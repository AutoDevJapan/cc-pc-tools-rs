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
    // Request IDs are retry keys; distinct bodies create distinct fixture authorities.
    let args = f.log(id, &format!("対応不要。既存の製品記録です。{id}"), false);
    core::s(&log(ctx, &args).unwrap()["thread_id"]).to_owned()
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
    assert_ne!(old, new, "fixture authorities must use distinct threads");
    let mut args=f.log("authorized-unsent","対応不要。保持する元記録です。",false);
    args.thread=Some(old.clone());
    let (request,_)=prepare_log(&mut ctx,&args).unwrap();
    let mut ledger=Ledger::load(&f.home).unwrap();
    let old_receipt = ledger.receipt("already-authorized", "log:0").unwrap();
    let new_receipt = ledger.receipt("different-authorized", "log:0").unwrap();
    assert_eq!(old_receipt.thread_id, old);
    assert_eq!(new_receipt.thread_id, new);
    assert_ne!(old_receipt.message_id, new_receipt.message_id);
    reserve(&mut ledger,&f.home,&request).unwrap();
    ledger.append(&f.home,Event::Bot{request_id:request.request_id.clone(),author_id:BOT.into()}).unwrap();
    assert_eq!(ledger.request(&request.request_id), Some(&request));
    assert!(ledger.effect(&request.request_id, "log:0").is_none());
    let before=fs::read(f.home.join(LEDGER)).unwrap();
    let posts = f.server.lock().unwrap().posts.len();
    args.thread=Some(new);
    assert!(log(&mut ctx,&args).unwrap_err().contains("already authorized"));
    assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(),before);
    assert_eq!(f.server.lock().unwrap().posts.len(), posts);
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


#[derive(Clone, Copy, Debug)]
enum Damage {
    Remove,
    Directory,
    ReplaceSame,
    ReplaceChanged,
}

fn inspection_case(id: &str) -> (Fixture, Context, LogArgs) {
    let f = Fixture::new();
    let mut ctx = f.context();
    let thread = target(&f, &mut ctx, "inspection-existing-target");
    let mut args = failed_log(&f, &mut ctx, id);
    args.thread = Some(thread);
    (f, ctx, args)
}

fn inspection_storage_view(home: &Path) -> BTreeMap<String, (bool, u64, Option<Vec<u8>>)> {
    fs::read_dir(home)
        .unwrap()
        .map(|entry| {
            let entry = entry.unwrap();
            let name = entry.file_name().to_string_lossy().into_owned();
            let metadata = entry.metadata().unwrap();
            // The observer must not read the byte-range locked lock file.
            let body = (metadata.is_file() && name != LOCK)
                .then(|| fs::read(entry.path()).unwrap());
            (name, (metadata.is_file(), metadata.len(), body))
        })
        .collect()
}

fn prepare_replacement(home: &Path, name: &str, damage: Damage) {
    if matches!(damage, Damage::ReplaceSame | Damage::ReplaceChanged) {
        let mut bytes = fs::read(home.join(name)).unwrap();
        if matches!(damage, Damage::ReplaceChanged) {
            bytes.push(b'\n');
        }
        fs::write(home.join("inspection-replacement"), bytes).unwrap();
    }
}

fn damage_storage(home: &Path, name: &str, damage: Damage) {
    let path = home.join(name);
    match damage {
        Damage::Remove => fs::remove_file(path).unwrap(),
        Damage::Directory => {
            fs::remove_file(&path).unwrap();
            fs::create_dir(path).unwrap();
        }
        Damage::ReplaceSame | Damage::ReplaceChanged => {
            fs::rename(home.join("inspection-replacement"), path).unwrap();
        }
    }
}

#[test]
fn inspection_never_recreates_files_lost_before_open() {
    for (name, damage) in [
        (LOCK, Damage::Remove),
        (LEDGER, Damage::Remove),
        (LOCK, Damage::Directory),
        (LEDGER, Damage::Directory),
    ] {
        let (f, mut ctx, args) = inspection_case("inspection-before-open");
        let calls = f.server.lock().unwrap().calls.len();
        let mut mutated = None;
        let result = log_target::inspect_at(&mut ctx, &args, |stage| {
            if stage == log_target::InspectStage::BeforeOpen {
                damage_storage(&f.home, name, damage);
                mutated = Some(inspection_storage_view(&f.home));
            }
            Ok(())
        });
        let mutated = mutated.expect("inspection seam reached");
        let error = result.unwrap_err();
        assert!(error.contains("inspection"), "{name} {damage:?}: {error}");
        assert_eq!(inspection_storage_view(&f.home), mutated, "{name} {damage:?}");
        assert_eq!(f.server.lock().unwrap().calls.len(), calls);
        f.unchanged();
    }
}

#[test]
fn inspection_rejects_storage_lost_or_replaced_after_it_opened_the_files() {
    let scenarios = [
        (log_target::InspectStage::Opened, LOCK, Damage::Remove),
        (log_target::InspectStage::Opened, LOCK, Damage::ReplaceSame),
        (log_target::InspectStage::Locked, LOCK, Damage::Remove),
        (log_target::InspectStage::Locked, LOCK, Damage::ReplaceSame),
        (log_target::InspectStage::Loaded, LOCK, Damage::Remove),
        (log_target::InspectStage::Loaded, LOCK, Damage::ReplaceSame),
        (log_target::InspectStage::Locked, LEDGER, Damage::Remove),
        (log_target::InspectStage::Locked, LEDGER, Damage::Directory),
        (log_target::InspectStage::Loaded, LEDGER, Damage::Remove),
        (log_target::InspectStage::Loaded, LEDGER, Damage::ReplaceChanged),
    ];
    for (stage, name, damage) in scenarios {
        let (f, mut ctx, args) = inspection_case("inspection-after-open");
        prepare_replacement(&f.home, name, damage);
        let calls = f.server.lock().unwrap().calls.len();
        let mut mutated = None;
        let result = log_target::inspect_at(&mut ctx, &args, |at| {
            if at == stage {
                damage_storage(&f.home, name, damage);
                mutated = Some(inspection_storage_view(&f.home));
            }
            Ok(())
        });
        let mutated = mutated.expect("inspection seam reached");
        let error = result.unwrap_err();
        assert!(error.contains("inspection"), "{stage:?} {name} {damage:?}: {error}");
        assert_eq!(
            inspection_storage_view(&f.home),
            mutated,
            "{stage:?} {name} {damage:?}"
        );
        assert_eq!(f.server.lock().unwrap().calls.len(), calls);
        f.unchanged();
    }
}

#[test]
fn inspection_reports_the_same_state_when_the_ledger_object_has_identical_bytes() {
    let (f, mut ctx, args) = inspection_case("inspection-same-state");
    prepare_replacement(&f.home, LEDGER, Damage::ReplaceSame);
    let calls = f.server.lock().unwrap().calls.len();
    let mut replaced = false;
    let status = log_target::inspect_at(&mut ctx, &args, |stage| {
        if stage == log_target::InspectStage::Loaded {
            damage_storage(&f.home, LEDGER, Damage::ReplaceSame);
            replaced = true;
        }
        Ok(())
    })
    .unwrap();
    assert!(replaced);
    assert_eq!(status["intent_exists"], true);
    assert_eq!(status["possible_post"], false);
    assert_eq!(status["retarget_eligible"], true);
    assert_eq!(f.server.lock().unwrap().calls.len(), calls);
    f.unchanged();
}

#[test]
fn inspection_respects_the_sender_lock_and_releases_its_own_lock() {
    let (f, mut ctx, args) = inspection_case("inspection-lock-compatibility");
    let before = inspection_storage_view(&f.home);
    let calls = f.server.lock().unwrap().calls.len();
    let sender = ByteLock::acquire(&f.home.join(LOCK), Duration::ZERO).unwrap();
    let error = inspect_log(&mut ctx, &args).unwrap_err();
    assert!(error.contains("lock"), "{error}");
    drop(sender);
    assert_eq!(inspection_storage_view(&f.home), before);
    let status = inspect_log(&mut ctx, &args).unwrap();
    assert_eq!(status["intent_exists"], true);
    assert_eq!(status["possible_post"], false);
    assert_eq!(status["retarget_eligible"], true);
    let sender = ByteLock::acquire(&f.home.join(LOCK), Duration::ZERO).unwrap();
    drop(sender);
    assert_eq!(inspection_storage_view(&f.home), before);
    assert_eq!(f.server.lock().unwrap().calls.len(), calls);
    f.unchanged();
}

#[test]
fn inspection_does_not_treat_invalid_existing_bytes_as_an_empty_ledger() {
    let (f, mut ctx, args) = inspection_case("inspection-strict-decode");
    let calls = f.server.lock().unwrap().calls.len();
    let mut mutated = None;
    let result = log_target::inspect_at(&mut ctx, &args, |stage| {
        if stage == log_target::InspectStage::Locked {
            fs::write(f.home.join(LEDGER), b"").unwrap();
            mutated = Some(inspection_storage_view(&f.home));
        }
        Ok(())
    });
    let mutated = mutated.expect("inspection seam reached");
    let error = result.unwrap_err();
    assert!(error.contains("malformed"), "{error}");
    assert_eq!(inspection_storage_view(&f.home), mutated);
    assert_eq!(f.server.lock().unwrap().calls.len(), calls);
    f.unchanged();
}
