mod send_deadline_tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    const BEFORE: f64 = 1791820800.;
    const EXPIRED: f64 = BEFORE + 11.;
    const UNTIL: &str = "2026-10-12T16:00:10Z";

    struct ClockEffects {
        inner: Box<dyn Effects>,
        now: Arc<Mutex<f64>>,
        samples: AtomicUsize,
        at_submit: bool,
        at_origin: bool,
        late_response: bool,
        lost: bool,
    }
    impl Effects for ClockEffects {
        fn now(&self) -> f64 {
            if self.at_submit && self.samples.fetch_add(1, Ordering::Relaxed) >= 1 {
                EXPIRED
            } else {
                *self.now.lock().unwrap()
            }
        }
        fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
            self.inner.run(args, timeout)
        }
        fn multipart(&mut self, url: &str, kind: &str, bytes: &[u8]) -> Result<Value> {
            self.inner.multipart(url, kind, bytes)
        }
        fn http(
            &mut self,
            method: &str,
            url: &str,
            body: Option<&Value>,
            discord: bool,
        ) -> Result<Value> {
            let result = self.inner.http(method, url, body, discord);
            if self.at_origin && method == "GET" && url == format!("{API}/channels/{ORIGIN}") {
                *self.now.lock().unwrap() = EXPIRED;
            }
            if method == "POST" && self.late_response {
                assert!(
                    crate::tick_runtime::remaining()
                        .is_some_and(|left| left <= Duration::from_secs(10)),
                    "deadline must reach the transport"
                );
                self.late_response = false;
                *self.now.lock().unwrap() = EXPIRED;
                if self.lost {
                    return Err("accepted question response lost".into());
                }
            }
            result
        }
    }
    fn wrap(
        mut ctx: Context,
        at_origin: bool,
        at_submit: bool,
        late_response: bool,
        lost: bool,
    ) -> Context {
        // Use the fixture's existing transport and an independently controlled clock.
        let inner = ctx.effects;
        ctx.effects = Box::new(ClockEffects {
            inner,
            now: Arc::new(Mutex::new(BEFORE)),
            samples: AtomicUsize::new(0),
            at_origin,
            at_submit,
            late_response,
            lost,
        });
        ctx
    }
    fn posts(f: &Fixture) -> usize {
        f.server
            .lock()
            .unwrap()
            .calls
            .iter()
            .filter(|(m, _, _)| m == "POST")
            .count()
    }

    #[test]
    fn expiry_during_sender_preflight_never_reserves_or_posts_and_retry_cannot_drop_deadline() {
        let f = Fixture::new();
        let mut ctx = wrap(f.context(), true, false, false, false);
        let mut args = f.ask("deadline-preflight", "choose the original direction");
        args.send_before = Some(UNTIL.into());
        assert!(ask(&mut ctx, &args).is_err());
        assert_eq!(posts(&f), 0);
        assert!(Ledger::load(&f.home)
            .unwrap()
            .effect(&args.request_id, "question:0")
            .is_none());
        args.send_before = None;
        assert!(ask(&mut ctx, &args).is_err());
        assert_eq!(posts(&f), 0);
        args.send_before = Some("2026-10-13T16:00:10Z".into());
        assert!(ask(&mut ctx, &args)
            .unwrap_err()
            .contains("deadline differs"));
        assert_eq!(posts(&f), 0);
        f.unchanged();
    }
    #[test]
    fn expiry_after_reservation_is_checked_at_json_and_multipart_submit() {
        for with_image in [false, true] {
            for expired in [false, true] {
                let f = Fixture::new();
                let mut ctx = wrap(f.context(), false, expired, false, false);
                let mut args = f.ask("deadline-submit", "choose the original direction");
                args.send_before = Some(UNTIL.into());
                let mut request = prepare_ask(&ctx, &args).unwrap();
                let images = if with_image {
                    vec![Image {
                        receipt: ImageReceipt {
                            filename: "fixture.png".into(),
                            mime: "image/png".into(),
                            size: 3,
                            sha256: "a".repeat(64),
                        },
                        bytes: vec![1, 2, 3],
                    }]
                } else {
                    vec![]
                };
                request.images = images.iter().map(|i| i.receipt.clone()).collect();
                request.fingerprint = request.fingerprint().unwrap();
                let result = execute_inner(&mut ctx, Some(request), None, &images, None, None);
                assert_eq!(
                    result.is_err(),
                    expired,
                    "image={with_image} expired={expired}"
                );
                assert_eq!(posts(&f) == 0, expired);
                let ledger = Ledger::load(&f.home).unwrap();
                assert!(ledger.effect(&args.request_id, "question:0").is_some());
                f.unchanged();
            }
        }
    }
    #[test]
    fn in_flight_and_lost_response_after_deadline_recover_same_question_without_new_post() {
        for lost in [false, true] {
            let f = Fixture::new();
            let mut ctx = wrap(f.context(), false, false, true, lost);
            let mut args = f.ask("deadline-response", "choose the original direction");
            let original = prepare_ask(&ctx, &args).unwrap().fingerprint;
            args.send_before = Some(UNTIL.into());
            assert_eq!(prepare_ask(&ctx, &args).unwrap().fingerprint, original);
            let result = ask(&mut ctx, &args).unwrap();
            let before = posts(&f);
            assert!(before > 0);
            args.send_before = None;
            let retry = ask(&mut ctx, &args).unwrap();
            assert_eq!(retry["thread_id"], result["thread_id"]);
            assert_eq!(posts(&f), before);
            let ledger = Ledger::load(&f.home).unwrap();
            assert_eq!(
                ledger.request(&args.request_id).unwrap().fingerprint,
                original
            );
            assert_eq!(
                ledger
                    .request(&args.request_id)
                    .unwrap()
                    .send_before
                    .as_deref(),
                Some(UNTIL)
            );
            f.unchanged();
        }
    }
}
