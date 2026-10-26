use super::*;
use crate::recovery_messages::pending_reconcile as pr;
use std::cell::RefCell;

fn hash(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

#[derive(Default)]
struct Fault {
    history: u8,
    posts_without_attempt: usize,
}

struct AuditMock {
    inner: Mock,
    faults: Arc<Mutex<Fault>>,
}
impl Effects for AuditMock {
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.inner.run(argv, timeout)
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if method == "POST" {
            let ledger: Value =
                serde_json::from_slice(&fs::read(self.inner.home.join(LEDGER)).unwrap()).unwrap();
            let events = ledger["events"].as_array().unwrap();
            if !events
                .iter()
                .any(|e| e["event"] == "pending_recovery_attempt")
            {
                self.faults.lock().unwrap().posts_without_attempt += 1;
                return Err("missing durable recovery attempt".into());
            }
        }
        let mut value = self.inner.http(method, url, body, discord)?;
        if method == "GET" && url.contains("/messages?limit=100") {
            match self.faults.lock().unwrap().history {
                1 => return Ok(json!([])),
                2 => {
                    if let Some(rows) = value.as_array_mut() {
                        if let Some(row) = rows.first().cloned() {
                            rows.push(row);
                        }
                    }
                }
                3 => return Err("history transport lost".into()),
                4 => {
                    if let Some(row) = value.as_array_mut().and_then(|r| r.first_mut()) {
                        row.as_object_mut().unwrap().remove("content");
                    }
                }
                5 => {
                    if let Some(row) = value.as_array_mut().and_then(|r| r.first_mut()) {
                        row["channel_id"] = json!("999");
                    }
                }
                _ => {}
            }
        }
        if method == "GET" && value["type"] == 11 {
            let state = self.inner.server.lock().unwrap();
            let id = core::s(&value["id"]);
            let last = state
                .messages
                .get(id)
                .into_iter()
                .flatten()
                .map(|m| core::n(&m["id"]))
                .max()
                .ok_or("no starter")?;
            value["last_message_id"] = json!(last.to_string());
        }
        Ok(value)
    }
}

struct Case {
    f: Fixture,
    ctx: Context,
    outer: String,
    id: String,
    thread: String,
    fingerprint: String,
    body: String,
    q: Value,
    args: pr::ReconcilePendingArgs,
    faults: Arc<Mutex<Fault>>,
}
impl Case {
    fn new() -> Self {
        let f = Fixture::new();
        let mut ctx = f.context();
        let anchor = log(
            &mut ctx,
            &f.log("anchor-4040", "対応不要。元の通常進捗です。", false),
        )
        .unwrap();
        let thread = core::s(&anchor["thread_id"]).to_owned();
        let body = "対応不要。予約済み通常報告の回復テストです。\n".to_owned();
        let outer = format!(
            "normal-log-{}",
            hash(
                json!([
                    "normal-log-v1",
                    "【情報システム部】作業完了",
                    hash(body.as_bytes()),
                    thread,
                    false,
                    []
                ])
                .to_string()
                .as_bytes()
            )
        );
        let id = format!("conversation-log-{}", &hash(outer.as_bytes())[..32]);
        let mut args = f.log(&id, &body, false);
        args.thread = Some(thread.clone());
        let (request, _) = prepare_log(&mut ctx, &args).unwrap();
        let mut ledger = Ledger::load(&f.home).unwrap();
        let request = reserve(&mut ledger, &f.home, &request).unwrap();
        {
            let _api = RecoveryApi::bind(&mut ctx, &mut ledger, &request).unwrap();
        }
        let effect = effect(
            &request,
            "log:0",
            Target::Thread {
                thread: thread.clone(),
            },
            &body,
            false,
            false,
        )
        .unwrap();
        ledger
            .append(
                &f.home,
                Event::Pending {
                    request_id: id.clone(),
                    phase: "log:0".into(),
                    effect,
                },
            )
            .unwrap();
        let dir = f.home.join("unattended/conversation-logs");
        fs::create_dir_all(&dir).unwrap();
        let body_file = dir.join(format!("{outer}.txt"));
        fs::write(&body_file, &body).unwrap();
        fs::write(f.home.join("unattended/runner.lock"), b"").unwrap();
        let input_sha = digest(&json!([
            request.title,
            hash(body.as_bytes()),
            thread,
            false,
            []
        ]))
        .unwrap();
        let registry = json!({"domain":"discord-conversation-v1","destination":{"guild_id":GUILD,"application_id":BOT,
            "task_channel_id":FORUM,"owner_user_id":OWNER},"questions":{},"logs":{&outer:{"input_sha256":input_sha,
            "body_file":body_file,"destination_thread_id":thread}},"topics":{}});
        fs::write(
            f.home.join("unattended/conversation-questions.json"),
            serde_json::to_vec(&registry).unwrap(),
        )
        .unwrap();
        let q = json!({"domain":"discord-pending-log-reconcile-v1","purpose":"reconcile_exact_pending_text_log",
            "run":"automation:2026-10-26T18:34:26.422Z","outer_request_id":outer,"request_id":id,
            "fingerprint":request.fingerprint,"phase":"log:0","body_sha256":hash(body.as_bytes()),"body_utf8_bytes":body.len(),
            "guild_id":GUILD,"thread_id":thread,"bot_id":BOT,"notify":false,"media_count":0,
            "journal_sha256":"","registry_sha256":hash(&fs::read(f.home.join("unattended/conversation-questions.json")).unwrap()),
            "transport_input_sha256":input_sha,"relay_session":"01a16dad-9ec4-76a1-9586-1f819c9330b1",
            "lease":{"kind":"existing-runner-and-sender-locks-v1","holder_pid":1,"holder_created_filetime":1,
            "expires_at":"2026-10-27T00:00:00Z"}});
        let faults = Arc::new(Mutex::new(Fault::default()));
        ctx.effects = Box::new(AuditMock {
            inner: Mock {
                server: f.server.clone(),
                home: f.home.clone(),
                pause: None,
            },
            faults: faults.clone(),
        });
        let qualification_file = f.root.path().join("reconcile.json");
        let mut c = Self {
            fingerprint: request.fingerprint,
            f,
            ctx,
            outer,
            id,
            thread,
            body,
            q,
            args: pr::ReconcilePendingArgs {
                qualification_file,
                check_only: true,
                expected_precheck: None,
            },
            faults,
        };
        c.refresh();
        c
    }
    fn write(&self) {
        fs::write(
            &self.args.qualification_file,
            serde_json::to_vec(&self.q).unwrap(),
        )
        .unwrap();
    }
    fn refresh(&mut self) {
        self.q["journal_sha256"] = json!(hash(&fs::read(self.f.home.join(LEDGER)).unwrap()));
        self.write();
    }
    fn run(&mut self) -> Result<Value> {
        self.run_save(Ledger::append)
    }
    fn run_save(&mut self, save: fn(&mut Ledger, &Path, Event) -> Result<()>) -> Result<Value> {
        let scope = pr::Scope {
            run: "automation:2026-10-26T18:34:26.422Z",
            outer: &self.outer,
            request: &self.id,
            fingerprint: &self.fingerprint,
            body_sha256: &hash(self.body.as_bytes()),
            body_bytes: self.body.len(),
            guild: GUILD,
            thread: &self.thread,
            bot: BOT,
        };
        pr::reconcile_with_store(&mut self.ctx, &self.args, &scope, |_| Ok(()), save)
    }
    fn precheck(&mut self) {
        self.args.check_only = true;
        self.args.expected_precheck = None;
        let out = self.run().unwrap();
        self.args.check_only = false;
        self.args.expected_precheck = Some(core::s(&out["command_id"]).into());
    }
    fn posts(&self) -> usize {
        self.f.server.lock().unwrap().posts.len() - 1
    }
    fn saved(&self) -> Vec<u8> {
        fs::read(self.f.home.join(LEDGER)).unwrap()
    }
    fn remote(&mut self) -> String {
        let ledger = Ledger::load(&self.f.home).unwrap();
        let effect = ledger.effect(&self.id, "log:0").unwrap();
        let mut state = self.f.server.lock().unwrap();
        state.next += 1;
        let id = state.next.to_string();
        let message = server_message(&mut state, &self.thread, &id, &effect.payload(OWNER));
        state.messages.get_mut(&self.thread).unwrap().push(message);
        id
    }
    fn assert_preserved(&self, before: &[u8]) {
        let old: Value = serde_json::from_slice(before).unwrap();
        let now: Value = serde_json::from_slice(&self.saved()).unwrap();
        assert_eq!(
            &now["events"].as_array().unwrap()[..old["events"].as_array().unwrap().len()],
            old["events"].as_array().unwrap()
        );
        self.f.unchanged();
        assert_eq!(self.faults.lock().unwrap().posts_without_attempt, 0);
    }
}

#[test]
fn pending_reconcile_precheck_is_read_only_and_qualified_apply_sends_once() {
    let mut c = Case::new();
    let original = c.saved();
    let registry = fs::read(c.f.home.join("unattended/conversation-questions.json")).unwrap();
    let checked = c.run().unwrap();
    assert_eq!(checked["exact_candidates"], 0);
    assert_eq!(c.saved(), original);
    assert_eq!(c.posts(), 0);
    c.precheck();
    let out = c.run().unwrap();
    assert_eq!(out["delivery"], "verified");
    assert_eq!(out["new_posts"], 1);
    assert_eq!(out["flags"], SILENT | EMBEDS);
    assert_eq!(out["owner_mentions"], 0);
    assert_eq!(c.posts(), 1);
    c.refresh();
    c.precheck();
    let repeated = c.run().unwrap();
    assert_eq!(repeated["new_posts"], 0);
    assert_eq!(c.posts(), 1);
    assert_eq!(
        fs::read(c.f.home.join("unattended/conversation-questions.json")).unwrap(),
        registry
    );
    c.assert_preserved(&original);
}

#[test]
fn pending_reconcile_exact_existing_is_adopted_without_post() {
    let mut c = Case::new();
    let original = c.saved();
    let id = c.remote();
    c.precheck();
    let out = c.run().unwrap();
    assert_eq!(out["message_id"], id);
    assert_eq!(out["new_posts"], 0);
    assert_eq!(c.posts(), 0);
    assert!(!Ledger::load(&c.f.home)
        .unwrap()
        .events
        .iter()
        .any(|e| matches!(e, Event::PendingRecoveryAttempt { .. })));
    c.assert_preserved(&original);
}

#[test]
fn pending_reconcile_lost_response_requires_get_adoption_never_another_post() {
    let mut c = Case::new();
    c.precheck();
    {
        let mut s = c.f.server.lock().unwrap();
        s.drop_response = true;
        s.block_reads_after_post = true;
    }
    assert!(c.run().is_err());
    assert_eq!(c.posts(), 1);
    {
        let mut s = c.f.server.lock().unwrap();
        s.blocked = false;
        s.drop_response = false;
        s.block_reads_after_post = false;
    }
    c.refresh();
    c.precheck();
    let out = c.run().unwrap();
    assert_eq!(out["delivery"], "verified");
    assert_eq!(out["new_posts"], 0);
    assert_eq!(c.posts(), 1);
}

#[test]
fn pending_reconcile_unknown_outcome_without_message_never_retries() {
    let mut c = Case::new();
    let original = c.saved();
    c.precheck();
    c.f.server.lock().unwrap().reject_post = true;
    assert!(c.run().is_err());
    assert_eq!(c.posts(), 1);
    c.refresh();
    c.precheck();
    assert!(c.run().is_err());
    assert_eq!(c.posts(), 1);
    c.assert_preserved(&original);
}

#[test]
fn pending_reconcile_rejects_each_identity_and_unknown_lease_field() {
    for (key, value) in [
        ("request_id", json!("another")),
        ("outer_request_id", json!("another")),
        ("run", json!("another")),
        ("fingerprint", json!("a".repeat(64))),
        ("body_sha256", json!("b".repeat(64))),
        ("body_utf8_bytes", json!(1)),
        ("thread_id", json!("999")),
        ("guild_id", json!("999")),
        ("bot_id", json!("999")),
        ("notify", json!(true)),
        ("media_count", json!(1)),
        ("phase", json!("question:0")),
        ("relay_session", json!("another")),
        ("transport_input_sha256", json!("c".repeat(64))),
        ("registry_sha256", json!("d".repeat(64))),
        ("journal_sha256", json!("e".repeat(64))),
        ("lease", json!({"unknown":true})),
    ] {
        let mut c = Case::new();
        let before = c.saved();
        c.q[key] = value;
        c.write();
        assert!(c.run().is_err(), "accepted {key}");
        assert_eq!(c.posts(), 0);
        assert_eq!(c.saved(), before);
    }
    let mut c = Case::new();
    c.q["lease"]["kind"] = json!("unknown");
    c.write();
    assert!(c.run().is_err());
}

#[test]
fn pending_reconcile_rejects_stale_cas_active_workers_and_concurrent_apply() {
    let mut c = Case::new();
    c.precheck();
    let runner =
        ByteLock::acquire(&c.f.home.join("unattended/runner.lock"), Duration::ZERO).unwrap();
    assert!(c.run().unwrap_err().contains("busy"));
    drop(runner);
    let sender = ByteLock::acquire(&c.f.home.join(LOCK), Duration::ZERO).unwrap();
    assert!(c.run().unwrap_err().contains("busy"));
    drop(sender);
    let _ = c.remote();
    assert!(c.run().unwrap_err().contains("CAS"));
    assert_eq!(c.posts(), 0);
    c.precheck();
    let _ = c.run().unwrap();
    assert_eq!(c.posts(), 0);
}

#[test]
fn pending_reconcile_refuses_empty_duplicate_failed_missing_content_and_wrong_thread_history() {
    for mode in 1..=5 {
        let mut c = Case::new();
        let before = c.saved();
        c.faults.lock().unwrap().history = mode;
        assert!(c.run().is_err(), "accepted ambiguous history {mode}");
        assert_eq!(c.posts(), 0);
        assert_eq!(c.saved(), before);
    }
}

#[test]
fn pending_reconcile_refuses_duplicate_near_match_and_wrong_quiet_or_media() {
    for mode in 0..5 {
        let mut c = Case::new();
        c.remote();
        if mode == 0 {
            c.remote();
        } else {
            let mut state = c.f.server.lock().unwrap();
            let message = state
                .messages
                .get_mut(&c.thread)
                .unwrap()
                .last_mut()
                .unwrap();
            match mode {
                1 => {
                    message["content"] = json!(format!("changed {}", core::s(&message["content"])))
                }
                2 => message["author"]["id"] = json!("999"),
                3 => message["flags"] = json!(EMBEDS),
                4 => message["attachments"] = json!([{"filename":"bad.png","size":1}]),
                _ => unreachable!(),
            }
        }
        assert!(c.run().is_err(), "accepted conflicting candidate {mode}");
        assert_eq!(c.posts(), 0);
    }
}

thread_local! { static SAVE_FAULT:RefCell<Option<(&'static str,bool)>>=const{RefCell::new(None)}; }
fn event_name(e: &Event) -> &'static str {
    match e {
        Event::PendingQualification { .. } => "qualification",
        Event::PendingRecoveryAttempt { .. } => "attempt",
        Event::PendingRecoveryResult { .. } => "result",
        Event::Posted { .. } => "posted",
        Event::Verified { .. } => "verified",
        Event::DeliveryVerified { .. } => "complete",
        _ => "other",
    }
}
fn fail_save(ledger: &mut Ledger, home: &Path, event: Event) -> Result<()> {
    let failure = SAVE_FAULT.with(|fault| {
        let current = *fault.borrow();
        if current.is_some_and(|(name, _)| name == event_name(&event)) {
            fault.take()
        } else {
            None
        }
    });
    if let Some((_, persist)) = failure {
        if persist {
            ledger.append(home, event)?;
        }
        return Err("injected durable storage failure".into());
    }
    ledger.append(home, event)
}

#[test]
fn pending_reconcile_storage_failures_do_not_grant_an_extra_attempt() {
    for phase in [
        "qualification",
        "attempt",
        "result",
        "posted",
        "verified",
        "complete",
    ] {
        for persisted in [false, true] {
            let mut c = Case::new();
            c.precheck();
            SAVE_FAULT.with(|f| *f.borrow_mut() = Some((phase, persisted)));
            assert!(c.run_save(fail_save).is_err(), "fault {phase}");
            assert_eq!(
                c.posts(),
                usize::from(!["qualification", "attempt"].contains(&phase))
            );
            let before = c.posts();
            c.refresh();
            c.precheck();
            let _ = c.run();
            if phase == "qualification" || phase == "attempt" && !persisted {
                assert_eq!(c.posts(), 1);
            } else {
                assert_eq!(c.posts(), before);
            }
        }
    }
}

#[test]
fn pending_reconcile_original_normal_retry_stays_get_only() {
    let mut c = Case::new();
    let mut args = c.f.log(&c.id, &c.body, false);
    args.thread = Some(c.thread.clone());
    assert!(log(&mut c.ctx, &args)
        .unwrap_err()
        .contains("RecoveryMessagePending"));
    assert_eq!(c.posts(), 0);
    assert!(
        pr::reconcile_pending(&mut c.ctx, &c.args).is_err(),
        "production command must pin the original authorized request"
    );
}

#[test]
fn pending_reconcile_paginates_to_the_verified_starter_before_allowing_recovery() {
    let mut c = Case::new();
    for _ in 0..205 {
        c.f.add_reply(&c.thread, BOT, "別の通常報告", true);
    }
    c.precheck();
    let out = c.run().unwrap();
    assert_eq!(out["delivery"], "verified");
    assert_eq!(c.posts(), 1);
    assert!(c
        .f
        .server
        .lock()
        .unwrap()
        .calls
        .iter()
        .any(|(_, path, _)| path.contains("&before=")));
}

#[test]
fn pending_reconcile_expired_or_unknown_os_lease_is_rejected_before_post() {
    let mut c = Case::new();
    let q: pr::Qualification = serde_json::from_value(c.q.clone()).unwrap();
    assert!(pr::lease(&q).is_err());
    c.q["lease"]["expires_at"] =
        json!((chrono::Utc::now() + chrono::Duration::minutes(2)).to_rfc3339());
    c.write();
    let scope = pr::Scope {
        run: "automation:2026-10-26T18:34:26.422Z",
        outer: &c.outer,
        request: &c.id,
        fingerprint: &c.fingerprint,
        body_sha256: &hash(c.body.as_bytes()),
        body_bytes: c.body.len(),
        guild: GUILD,
        thread: &c.thread,
        bot: BOT,
    };
    assert!(pr::reconcile(&mut c.ctx, &c.args, &scope, pr::lease).is_err());
    assert_eq!(c.posts(), 0);
}

#[test]
fn pending_reconcile_simultaneous_apply_cannot_post_twice() {
    let mut c = Case::new();
    c.precheck();
    let home = c.f.home.clone();
    let root = c.f.root.path().to_owned();
    let server = c.f.server.clone();
    let faults = c.faults.clone();
    let outer = c.outer.clone();
    let id = c.id.clone();
    let fingerprint = c.fingerprint.clone();
    let thread = c.thread.clone();
    let body = c.body.clone();
    let qualification_file = c.args.qualification_file.clone();
    let expected_precheck = c.args.expected_precheck.clone();
    let (entered_tx, entered_rx) = mpsc::channel();
    let (release_tx, release_rx) = mpsc::channel();
    let worker = std::thread::spawn(move || {
        let mut ctx = context(&home, &root, server.clone(), None);
        ctx.effects = Box::new(AuditMock {
            inner: Mock {
                server,
                home,
                pause: Some((entered_tx, release_rx)),
            },
            faults,
        });
        let args = pr::ReconcilePendingArgs {
            qualification_file,
            check_only: false,
            expected_precheck,
        };
        let scope = pr::Scope {
            run: "automation:2026-10-26T18:34:26.422Z",
            outer: &outer,
            request: &id,
            fingerprint: &fingerprint,
            body_sha256: &hash(body.as_bytes()),
            body_bytes: body.len(),
            guild: GUILD,
            thread: &thread,
            bot: BOT,
        };
        pr::reconcile(&mut ctx, &args, &scope, |_| Ok(()))
    });
    let entered = entered_rx.recv_timeout(Duration::from_secs(15));
    let second = c.run();
    let _ = release_tx.send(());
    let first = worker.join().unwrap();
    assert!(entered.is_ok());
    assert!(second.unwrap_err().contains("busy"));
    assert_eq!(first.unwrap()["delivery"], "verified");
    assert_eq!(c.posts(), 1);
    c.refresh();
    c.precheck();
    assert_eq!(c.run().unwrap()["new_posts"], 0);
    assert_eq!(c.posts(), 1);
}
