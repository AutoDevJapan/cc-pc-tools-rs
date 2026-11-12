//! A stage-only recovery of an unsent intent. The ordinary Go verifier binds
//! native preview bytes and the already accepted async question. No HTTP.
use super::*;

#[derive(Args, Debug)]
pub struct NativeIntentArgs {
    #[command(flatten)]
    pub question: AskArgs,
    #[arg(long)]
    pub evidence: PathBuf,
    /// Append the exact verified intent and async association atomically.
    #[arg(long)]
    pub stage: bool,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct NativeImage {
    path: PathBuf,
    sha256: String,
    size: usize,
    #[serde(default)]
    producer_path: Option<PathBuf>,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct NativeProof {
    domain: String,
    validated: bool,
    send_allowed: bool,
    request_id: String,
    origin_thread_id: String,
    scope: crate::question_images::Scope,
    title: String,
    images: Vec<NativeImage>,
    async_call_id: String,
    evidence_sha256: String,
}

pub fn check_native_intent(ctx: &mut Context, args: &NativeIntentArgs) -> Result<Value> {
    if args.question.program_evidence.is_some()
        || args.question.conversation_source_request.is_some()
    {
        return Err("native intent cannot substitute another source purpose".into());
    }
    let mut incoming = prepare_ask(ctx, &args.question)?;
    let _task_control = if args.stage {
        Some(program_question::task_lock(ctx)?)
    } else {
        None
    };
    let output = ctx
        .effects
        .run(
            &[
                "D:/CC/Tools/discord-ask/unattended-runner.exe".into(),
                "--check-native-question-evidence".into(),
                "--evidence".into(),
                args.evidence
                    .to_str()
                    .ok_or("native evidence path invalid")?
                    .into(),
            ],
            25,
        )
        .map_err(|_| "native question evidence check failed")?;
    if output.code != 0 || output.stdout.len() > 64 * 1024 {
        return Err("native question evidence check failed".into());
    }
    let proof: NativeProof =
        serde_json::from_str(&output.stdout).map_err(|_| "native proof invalid")?;
    let scope = question_image_scope(ctx, &incoming).ok_or("native scope unavailable")?;
    let origin = match &incoming.operation {
        Kind::Ask { origin_thread, .. } => origin_thread,
        _ => unreachable!(),
    };
    if proof.domain != "owner-native-question-proof-v1"
        || !proof.validated
        || proof.send_allowed
        || proof.request_id != incoming.request_id
        || proof.origin_thread_id != *origin
        || proof.scope != scope
        || proof.title != incoming.title
        || proof.images.is_empty()
        || proof.images.len() > 10
        || proof.async_call_id.is_empty()
        || proof.async_call_id.len() > 128
        || proof.evidence_sha256.len() != 64
        || !proof
            .evidence_sha256
            .bytes()
            .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
        || proof.images.len() != args.question.attach.len()
        || proof.images.len() != args.question.expected_image_sha256.len()
    {
        return Err("native proof differs from original question".into());
    }
    let mut total = 0usize;
    for ((image, path), expected) in proof
        .images
        .iter()
        .zip(&args.question.attach)
        .zip(&args.question.expected_image_sha256)
    {
        if !path.is_absolute()
            || image_path_key(&image.path) != image_path_key(path)
            || image.sha256.len() != 64
            || !image
                .sha256
                .bytes()
                .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
            || image.sha256 != *expected
            || image.size == 0
            || image.size > IMAGE_BYTES
        {
            return Err("native proof image association differs".into());
        }
        let _ = &image.producer_path;
        total = total
            .checked_add(image.size)
            .ok_or("native image size overflow")?;
        if total > UPLOAD_BYTES {
            return Err("native images exceed total limit".into());
        }
        incoming.images.push(ImageReceipt {
            filename: format!("image-{}.png", &image.sha256[..20]),
            mime: "image/png".into(),
            size: image.size,
            sha256: image.sha256.clone(),
        });
    }
    incoming.native_evidence = Some(args.evidence.clone());
    incoming.fingerprint = incoming.fingerprint()?;
    // Check-only does not even create a lock or a storage directory.
    let home = ctx.here.clone();
    let _lock = if args.stage {
        Some(ByteLock::acquire(&home.join(LOCK), Duration::ZERO)?)
    } else {
        None
    };
    let mut ledger = Ledger::load(&home)?;
    let existing = ledger.request(&incoming.request_id);
    let reused = existing.is_some();
    if let Some(old) = existing {
        if old != &incoming || ledger.effect(&incoming.request_id, "question:0").is_some() {
            return Err("native restore conflicts with existing request; no write or HTTP".into());
        }
        let matched = ledger.events.iter().any(|event| matches!(event,
            Event::NativeAssociation { request_id, fingerprint, evidence_sha256, async_call_id }
            if request_id == &incoming.request_id && fingerprint == &incoming.fingerprint
                && evidence_sha256 == &proof.evidence_sha256 && async_call_id == &proof.async_call_id));
        if !matched {
            return Err("native restore association unavailable".into());
        }
    } else {
        if ledger.events.iter().any(|event| match event {
            Event::Intent { request } => request.destination == incoming.destination
                && matches!((&request.operation, &incoming.operation),
                    (Kind::Ask { decision_identity:a,..},Kind::Ask {decision_identity:b,..}) if a==b),
            _=>false,
        }) { return Err("native restore decision already has another request".into()); }
        if args.stage {
            ledger.append_all(
                &home,
                vec![
                    Event::Intent {
                        request: Box::new(incoming.clone()),
                    },
                    Event::NativeAssociation {
                        request_id: incoming.request_id.clone(),
                        fingerprint: incoming.fingerprint.clone(),
                        evidence_sha256: proof.evidence_sha256.clone(),
                        async_call_id: proof.async_call_id.clone(),
                    },
                ],
            )?;
            let readback = Ledger::load(&home)?;
            if readback.request(&incoming.request_id) != Some(&incoming) {
                return Err("native intent readback failed".into());
            }
        }
    }
    Ok(
        json!({"domain":"owner-native-intent-recovery-v1","request_id":incoming.request_id,
        "todo_id":args.question.todo_id,"validated":true,"staged":args.stage,"reused":reused,
        "fingerprint":incoming.fingerprint,"evidence_sha256":proof.evidence_sha256,
        "async_call_id":proof.async_call_id,"send_allowed":false,"http_performed":false,
        "wait_registered":false,"delivery":"not_sent","approval":"not_inferred"}),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::{Effects, Output};
    use std::sync::{Arc, Mutex};
    struct NativeMock {
        proof: Value,
        effects: Arc<Mutex<(usize, usize)>>,
    }
    impl Effects for NativeMock {
        fn http(&mut self, _: &str, _: &str, _: Option<&Value>, _: bool) -> Result<Value> {
            self.effects.lock().unwrap().1 += 1;
            Err("HTTP forbidden in check/stage".into())
        }
        fn run(&mut self, args: &[String], _: u64) -> Result<Output> {
            if args.len() != 4 || args[1] != "--check-native-question-evidence" {
                return Err("wrong verifier".into());
            }
            self.effects.lock().unwrap().0 += 1;
            Ok(Output {
                code: 0,
                stdout: self.proof.to_string(),
                stderr: String::new(),
            })
        }
    }
    #[test]
    fn stage_native_intent_is_idempotent_and_never_calls_http_or_waits() {
        let f = super::super::tests::Fixture::new();
        let mut q = f.ask(
            "native-fixture",
            "この構図で進めますか？\na:進める\nb:修正する",
        );
        let image = f.root.path().join("never-read.png");
        q.attach.push(image.clone());
        q.expected_image_sha256.push("b".repeat(64));
        let effects = Arc::new(Mutex::new((0, 0)));
        let mut ctx = f.context();
        let incoming = prepare_ask(&ctx, &q).unwrap();
        let scope = question_image_scope(&ctx, &incoming).unwrap();
        let origin = match &incoming.operation {
            Kind::Ask { origin_thread, .. } => origin_thread.clone(),
            _ => unreachable!(),
        };
        let proof = json!({"domain":"owner-native-question-proof-v1","validated":true,"send_allowed":false,
            "request_id":q.request_id,"origin_thread_id":origin,"scope":scope,"title":incoming.title,
            "images":[{"path":image,"sha256":"b".repeat(64),"size":10}],"async_call_id":"original-async",
            "evidence_sha256":"c".repeat(64)});
        ctx.effects = Box::new(NativeMock {
            proof: proof.clone(),
            effects: effects.clone(),
        });
        let mut args = NativeIntentArgs {
            question: q,
            evidence: f.root.path().join("native-evidence.json"),
            stage: false,
        };
        let before = fs::read(f.home.join(LEDGER)).ok();
        let lock_before = fs::read(f.home.join(LOCK)).ok();
        assert_eq!(
            check_native_intent(&mut ctx, &args).unwrap()["staged"],
            false
        );
        assert_eq!(fs::read(f.home.join(LEDGER)).ok(), before);
        assert_eq!(fs::read(f.home.join(LOCK)).ok(), lock_before);
        args.stage = true;
        assert_eq!(
            check_native_intent(&mut ctx, &args).unwrap()["staged"],
            true
        );
        let once = fs::read(f.home.join(LEDGER)).unwrap();
        assert_eq!(
            check_native_intent(&mut ctx, &args).unwrap()["reused"],
            true
        );
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), once);
        let ledger = Ledger::load(&f.home).unwrap();
        assert_eq!(ledger.events.len(), 2);
        assert!(ledger
            .effect(&args.question.request_id, "question:0")
            .is_none());
        assert!(ask(&mut ctx, &args.question)
            .unwrap_err()
            .contains("stage-only"));
        let request = ledger.request(&args.question.request_id).unwrap();
        let result = execute_inner(&mut ctx, Some(request.clone()), None, &[], None, None, None);
        assert!(result.unwrap_err().contains("stage-only"));
        let mut alias = request.clone();
        alias.request_id = "native-fixture-another-title".into();
        alias.title = "renamed".into();
        alias.fingerprint = alias.fingerprint().unwrap();
        assert!(
            execute_inner(&mut ctx, Some(alias), None, &[], None, None, None)
                .unwrap_err()
                .contains("stage-only")
        );
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), once);
        assert_eq!(effects.lock().unwrap().1, 0);
        // A different retry key for the same decision cannot create an intent.
        let mut conflicting_question = f.ask(
            "another-native-key",
            "この構図で進めますか？\na:進める\nb:修正する",
        );
        conflicting_question.attach = args.question.attach.clone();
        conflicting_question.expected_image_sha256 = args.question.expected_image_sha256.clone();
        let mut conflict = NativeIntentArgs {
            question: conflicting_question,
            evidence: args.evidence.clone(),
            stage: true,
        };
        conflict.question.request_id = "another-native-key".into();
        let mut other_proof = proof.clone();
        other_proof["request_id"] = json!("another-native-key");
        ctx.effects = Box::new(NativeMock {
            proof: other_proof,
            effects: effects.clone(),
        });
        assert!(check_native_intent(&mut ctx, &conflict)
            .unwrap_err()
            .contains("another request"));
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), once);
        ctx.effects = Box::new(NativeMock {
            proof: proof.clone(),
            effects: effects.clone(),
        });
        // A missing association must not be silently reconstituted on retry.
        let mut incomplete = Ledger::load(&f.home).unwrap();
        incomplete.events.pop();
        fs::write(
            f.home.join(LEDGER),
            serde_json::to_vec(&incomplete).unwrap(),
        )
        .unwrap();
        let damaged = fs::read(f.home.join(LEDGER)).unwrap();
        assert!(check_native_intent(&mut ctx, &args)
            .unwrap_err()
            .contains("association unavailable"));
        assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), damaged);
        fs::write(f.home.join(LEDGER), &once).unwrap();
        for (field, value) in [
            ("send_allowed", json!(true)),
            ("request_id", json!("other")),
            ("async_call_id", json!("")),
            ("evidence_sha256", json!("bad")),
        ] {
            let mut altered = proof.clone();
            altered[field] = value;
            ctx.effects = Box::new(NativeMock {
                proof: altered,
                effects: effects.clone(),
            });
            assert!(check_native_intent(&mut ctx, &args).is_err());
            assert_eq!(fs::read(f.home.join(LEDGER)).unwrap(), once);
        }
        assert_eq!(effects.lock().unwrap().1, 0);
    }
}
