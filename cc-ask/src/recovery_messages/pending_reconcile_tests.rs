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
    reject_get_before_first_submit: bool,
    gets_after_attempt_before_submit: usize,
    locked: bool,
    unknown_lock_state: bool,
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
        if method == "GET" && self.faults.lock().unwrap().reject_get_before_first_submit {
            let ledger: Value =
                serde_json::from_slice(&fs::read(self.inner.home.join(LEDGER)).unwrap()).unwrap();
            let attempted = ledger["events"]
                .as_array()
                .unwrap()
                .iter()
                .any(|e| e["event"] == "pending_recovery_attempt");
            if attempted && self.inner.server.lock().unwrap().posts.len() == 1 {
                self.faults.lock().unwrap().gets_after_attempt_before_submit += 1;
                return Err("a GET after attempt would consume the slot without POST".into());
            }
        }
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
            if self.faults.lock().unwrap().unknown_lock_state {
                value["thread_metadata"]
                    .as_object_mut()
                    .unwrap()
                    .remove("locked");
            } else {
                value["thread_metadata"]["locked"] = json!(self.faults.lock().unwrap().locked);
            }
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
        Self::with_body("対応不要。予約済み通常報告の回復テストです。\n")
    }
    fn with_body(body: &str) -> Self {
        let f = Fixture::new();
        let mut ctx = f.context();
        let anchor = log(
            &mut ctx,
            &f.log("anchor-4040", "対応不要。元の通常進捗です。", false),
        )
        .unwrap();
        let thread = core::s(&anchor["thread_id"]).to_owned();
        let body = body.to_owned();
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
        self.run_using(save, |_| Ok(()))
    }
    fn run_using(
        &mut self,
        save: fn(&mut Ledger, &Path, Event) -> Result<()>,
        verify_lease: fn(&pr::Qualification) -> Result<()>,
    ) -> Result<Value> {
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
        pr::reconcile_with_store(&mut self.ctx, &self.args, &scope, verify_lease, save)
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
    let mut normal = c.f.log(&c.id, &c.body, false);
    normal.thread = Some(c.thread.clone());
    assert_eq!(log(&mut c.ctx, &normal).unwrap()["delivery"], "verified");
    assert_eq!(
        c.posts(),
        1,
        "normal retry after reconciliation must only GET"
    );
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
fn pending_reconcile_attempt_is_followed_by_post_without_a_fallible_get() {
    let mut c = Case::new();
    c.precheck();
    c.faults.lock().unwrap().reject_get_before_first_submit = true;
    let result = c.run();
    assert_eq!(
        c.posts(),
        1,
        "a saved attempt must not be consumed by a later preflight GET"
    );
    assert_eq!(result.unwrap()["delivery"], "verified");
    assert_eq!(c.faults.lock().unwrap().gets_after_attempt_before_submit, 0);
}

thread_local! { static LEASE_GUARD_HOME: RefCell<Option<PathBuf>> = const { RefCell::new(None) }; }
fn lease_guard(_: &pr::Qualification) -> Result<()> {
    LEASE_GUARD_HOME.with(|p| {
        let ledger = Ledger::load(p.borrow().as_ref().unwrap())?;
        if ledger
            .events
            .iter()
            .any(|e| matches!(e, Event::PendingRecoveryAttempt { .. }))
        {
            Err("lease admission must finish before reserving the attempt".into())
        } else {
            Ok(())
        }
    })
}

#[test]
fn pending_reconcile_attempt_has_no_later_lease_admission_failure() {
    let mut c = Case::new();
    c.precheck();
    LEASE_GUARD_HOME.with(|p| *p.borrow_mut() = Some(c.f.home.clone()));
    let result = c.run_using(Ledger::append, lease_guard);
    assert_eq!(result.unwrap()["delivery"], "verified");
    assert_eq!(c.posts(), 1);
}

#[test]
fn pending_reconcile_locked_or_unknown_send_state_reserves_no_attempt() {
    for unknown in [false, true] {
        let mut c = Case::new();
        c.precheck();
        let original = c.saved();
        {
            let mut faults = c.faults.lock().unwrap();
            faults.locked = true;
            faults.unknown_lock_state = unknown;
        }
        assert!(c.run().unwrap_err().contains("locked"));
        assert_eq!(c.saved(), original);
        assert_eq!(c.posts(), 0);
        c.args.check_only = true;
        c.args.expected_precheck = None;
        assert!(c.run().unwrap_err().contains("locked"));
        assert_eq!(c.saved(), original);
        // Reading/adopting an exact existing message does not require sending.
        let existing = c.remote();
        c.precheck();
        assert_eq!(c.run().unwrap()["message_id"], existing);
        assert_eq!(c.posts(), 0);
    }
}

#[test]
fn pending_reconcile_same_body_without_tag_or_with_a_different_tag_stops() {
    for tag in ["", "\n\n-# recovery:another-request:log:0"] {
        let mut c = Case::new();
        let original = c.saved();
        let duplicate =
            c.f.add_reply(&c.thread, BOT, &format!("{}{tag}", c.body), true);
        let error = c.run().unwrap_err();
        assert!(error.contains("same bot body"), "{error}");
        assert!(error.contains(&duplicate), "missing candidate ID: {error}");
        assert_eq!(c.posts(), 0);
        assert_eq!(c.saved(), original);
    }
}

#[test]
fn pending_reconcile_multiple_ambiguous_candidates_report_only_the_latest_id() {
    let mut c = Case::new();
    c.precheck();
    let original = c.saved();
    let older = c.f.add_reply(&c.thread, BOT, &c.body, true);
    let latest = c.f.add_reply(&c.thread, BOT, &c.body, true);
    let error = c.run().unwrap_err();
    assert!(error.contains("same bot body"), "{error}");
    assert!(
        error.contains(&format!("latest candidate message {latest}")),
        "{error}"
    );
    assert!(error.contains("additional candidates may exist"), "{error}");
    assert!(
        !error.contains(&older),
        "only the latest ID is reported: {error}"
    );
    assert!(
        !error.contains("no POST"),
        "history may run after POST: {error}"
    );
    assert_eq!(c.posts(), 0);
    assert_eq!(c.saved(), original);
}

#[test]
fn pending_reconcile_transformed_duplicate_body_never_authorizes_a_post() {
    for shape in 0..3 {
        let mut c = if shape == 2 {
            Case::with_body(
                "対応不要。元の報告です。\n\n-# recovery:quoted-in-the-body\n報告の続きです。\n",
            )
        } else {
            Case::new()
        };
        assert!(
            c.run().is_ok(),
            "the valid initial record must reach precheck"
        );
        let original = c.saved();
        let duplicate = match shape {
            0 => c.body.trim_end().to_owned(),
            1 => format!("既存の作業への報告\n{}", c.body),
            _ => format!("{}\n\n-# recovery:another-request:log:0", c.body),
        };
        c.f.add_reply(&c.thread, BOT, &duplicate, true);
        let error = c.run().unwrap_err();
        assert!(error.contains("same bot body"), "shape {shape}: {error}");
        assert_eq!(
            c.posts(),
            0,
            "transformed duplicate shape {shape} allowed a POST"
        );
        assert_eq!(
            c.saved(),
            original,
            "ambiguity must retain the original journal"
        );
    }
}

#[test]
fn pending_reconcile_duplicate_or_mismatched_attempt_is_rejected_by_the_journal() {
    let mut c = Case::new();
    c.precheck();
    c.run().unwrap();
    let original: Value = serde_json::from_slice(&c.saved()).unwrap();
    for key in [
        "duplicate",
        "request_id",
        "phase",
        "qualification_receipt_sha256",
        "attempt_id",
    ] {
        let mut changed = original.clone();
        let events = changed["events"].as_array_mut().unwrap();
        let index = events
            .iter()
            .position(|e| e["event"] == "pending_recovery_attempt")
            .unwrap();
        if key == "duplicate" {
            events.insert(index + 1, events[index].clone());
        } else {
            events[index][key] = json!("mismatch");
        }
        fs::write(c.f.home.join(LEDGER), serde_json::to_vec(&changed).unwrap()).unwrap();
        assert!(Ledger::load(&c.f.home).is_err(), "accepted {key}");
        assert_eq!(c.posts(), 1);
    }
}

#[test]
fn pending_reconcile_reactions_do_not_invalidate_the_absence_precheck() {
    let mut c = Case::new();
    c.precheck();
    c.f.server
        .lock()
        .unwrap()
        .messages
        .get_mut(&c.thread)
        .unwrap()[0]["reactions"] = json!([{"count":1}]);
    assert_eq!(c.run().unwrap()["delivery"], "verified");
    assert_eq!(c.posts(), 1);
}

#[test]
fn pending_reconcile_profile_and_attachment_urls_do_not_change_identity_evidence() {
    let mut c = Case::new();
    let other = c.f.add_reply(&c.thread, BOT, "別の通常投稿", true);
    {
        let mut state = c.f.server.lock().unwrap();
        let message = state
            .messages
            .get_mut(&c.thread)
            .unwrap()
            .iter_mut()
            .find(|m| m["id"] == other)
            .unwrap();
        message["mentions"] = json!([{"id":"900","username":"old-name"}]);
        message["attachments"] = json!([{"id":"901","filename":"public.png","size":10,"content_type":"image/png","url":"https://example.invalid/old","proxy_url":"https://example.invalid/old-proxy"}]);
    }
    c.precheck();
    {
        let mut state = c.f.server.lock().unwrap();
        let message = state
            .messages
            .get_mut(&c.thread)
            .unwrap()
            .iter_mut()
            .find(|m| m["id"] == other)
            .unwrap();
        message["mentions"][0]["username"] = json!("new-name");
        message["attachments"][0]["url"] = json!("https://example.invalid/new");
        message["attachments"][0]["proxy_url"] = json!("https://example.invalid/new-proxy");
    }
    assert_eq!(c.run().unwrap()["delivery"], "verified");
    assert_eq!(c.posts(), 1);
}

#[test]
fn pending_reconcile_mention_or_attachment_identity_change_invalidates_precheck() {
    for changed in [
        "mention_id",
        "attachment_id",
        "filename",
        "size",
        "content_type",
    ] {
        let mut c = Case::new();
        let other = c.f.add_reply(&c.thread, BOT, "別の通常投稿", true);
        {
            let mut state = c.f.server.lock().unwrap();
            let message = state
                .messages
                .get_mut(&c.thread)
                .unwrap()
                .iter_mut()
                .find(|m| m["id"] == other)
                .unwrap();
            message["mentions"] = json!([{"id":"900","username":"same-name"}]);
            message["attachments"] =
                json!([{"id":"901","filename":"public.png","size":10,"content_type":"image/png"}]);
        }
        c.precheck();
        let original = c.saved();
        {
            let mut state = c.f.server.lock().unwrap();
            let message = state
                .messages
                .get_mut(&c.thread)
                .unwrap()
                .iter_mut()
                .find(|m| m["id"] == other)
                .unwrap();
            match changed {
                "mention_id" => message["mentions"][0]["id"] = json!("902"),
                "attachment_id" => message["attachments"][0]["id"] = json!("903"),
                "filename" => message["attachments"][0]["filename"] = json!("other.png"),
                "size" => message["attachments"][0]["size"] = json!(11),
                "content_type" => message["attachments"][0]["content_type"] = json!("image/jpeg"),
                _ => unreachable!(),
            }
        }
        let error = c.run().unwrap_err();
        assert!(error.contains("precheck CAS changed"), "{changed}: {error}");
        assert_eq!(c.posts(), 0, "{changed}");
        assert_eq!(c.saved(), original, "{changed}");
    }
}

#[test]
fn pending_reconcile_historical_receipt_keeps_its_stored_effect_format() {
    let c = Case::new();
    let mut ledger = Ledger::load(&c.f.home).unwrap();
    let mut historical = ledger.effect(&c.id, "log:0").unwrap().clone();
    historical.content = format!("previous presentation: {}", historical.content);
    for event in &mut ledger.events {
        if let Event::Pending {
            request_id, effect, ..
        } = event
        {
            if request_id == &c.id {
                *effect = historical.clone();
            }
        }
    }
    let mut binding = c.q.clone();
    binding["journal_sha256"] = json!(hash(&serde_json::to_vec_pretty(&ledger).unwrap()));
    let mut receipt = json!({"binding":binding,"input_sha256":hash(&serde_json::to_vec(&binding).unwrap()),
        "effect_sha256":digest(&serde_json::to_value(&historical).unwrap()).unwrap(),
        "history_sha256":"a".repeat(64),"command_id":""});
    receipt["command_id"] = json!(digest(&receipt).unwrap());
    ledger
        .append(
            &c.f.home,
            serde_json::from_value(
                json!({"event":"pending_qualification","qualification":receipt}),
            )
            .unwrap(),
        )
        .unwrap();
    let saved = Ledger::load(&c.f.home).unwrap();
    assert_eq!(saved.effect(&c.id, "log:0"), Some(&historical));
    let request = saved.request(&c.id).unwrap();
    let current = effect(
        request,
        "log:0",
        Target::Thread {
            thread: c.thread.clone(),
        },
        &c.body,
        false,
        false,
    )
    .unwrap();
    assert_ne!(
        saved.effect(&c.id, "log:0"),
        Some(&current),
        "historical loading must not regenerate today's presentation"
    );
    assert_eq!(c.posts(), 0);
}

#[cfg(windows)]
#[test]
fn pending_reconcile_lease_process_fixture() {
    let Ok(path) = std::env::var("CC_4040_LEASE_FIXTURE") else {
        return;
    };
    let q: pr::Qualification = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
    assert_eq!(
        pr::lease(&q).is_ok(),
        std::env::var("CC_4040_LEASE_EXPECT").unwrap() == "valid"
    );
    let acknowledgement = std::env::var("CC_4040_LEASE_ACK").unwrap();
    let mut marker = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(acknowledgement)
        .unwrap();
    marker
        .write_all(&[u8::from(
            std::env::var("CC_4040_LEASE_EXPECT").unwrap() == "valid",
        )])
        .unwrap();
    marker.sync_all().unwrap();
}

#[cfg(windows)]
#[test]
fn pending_reconcile_real_parent_identity_admits_lease_and_refuses_a_reused_pid() {
    use std::os::windows::{io::AsRawHandle, process::CommandExt};
    use windows_sys::Win32::{Foundation::*, System::Threading::*};
    let mut c = Case::new();
    let mut birth: FILETIME = unsafe { std::mem::zeroed() };
    let mut end = birth;
    let mut kernel = birth;
    let mut user = birth;
    assert_ne!(
        unsafe {
            GetProcessTimes(
                GetCurrentProcess(),
                &mut birth,
                &mut end,
                &mut kernel,
                &mut user,
            )
        },
        0
    );
    let stamp = ((birth.dwHighDateTime as u64) << 32) | birth.dwLowDateTime as u64;
    c.q["lease"]["holder_pid"] = json!(std::process::id());
    c.q["lease"]["expires_at"] =
        json!((chrono::Utc::now() + chrono::Duration::minutes(2)).to_rfc3339());
    for (expected, created) in [("valid", stamp), ("invalid", stamp + 1)] {
        c.q["lease"]["holder_created_filetime"] = json!(created);
        c.write();
        let acknowledgement = c.f.root.path().join(format!("lease-{expected}.ack"));
        let mut child = std::process::Command::new(std::env::current_exe().unwrap())
            .args(["--exact", "recovery_messages::tests::pending_reconcile_tests::pending_reconcile_lease_process_fixture"])
            .env("CC_4040_LEASE_FIXTURE", &c.args.qualification_file).env("CC_4040_LEASE_EXPECT",expected)
            .env("CC_4040_LEASE_ACK",&acknowledgement)
            .stdout(std::process::Stdio::null()).stderr(std::process::Stdio::null())
            .creation_flags(CREATE_NO_WINDOW).spawn().unwrap();
        let wait = unsafe { WaitForSingleObject(child.as_raw_handle() as _, 15_000) };
        if wait != WAIT_OBJECT_0 {
            let _ = child.kill();
        }
        let exit = child.wait().unwrap();
        assert_eq!(wait, WAIT_OBJECT_0, "lease fixture timed out");
        assert!(exit.success(), "lease fixture {expected}: {exit}");
        assert_eq!(
            fs::read(acknowledgement).unwrap(),
            vec![u8::from(expected == "valid")],
            "the actual fixture must run; zero selected tests are not a lease check"
        );
    }
    assert_eq!(c.posts(), 0);
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
