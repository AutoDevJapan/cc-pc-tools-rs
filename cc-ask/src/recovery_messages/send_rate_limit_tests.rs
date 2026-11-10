mod send_rate_limit_tests {
    use super::*;
    use crate::core::RealEffects;
    use std::{
        io::{Read, Write},
        net::{TcpListener, TcpStream},
        sync::atomic::{AtomicBool, Ordering},
        thread,
        time::Instant,
    };

    #[derive(Clone, Copy)]
    enum Outcome {
        Success,
        Expires,
        Lost,
    }
    fn prepare_stream(stream: &TcpStream) {
        // Windows inherits the listener's nonblocking mode on accept. A read
        // timeout alone does not make that socket wait for later TCP segments.
        stream.set_nonblocking(false).unwrap();
        stream
            .set_read_timeout(Some(Duration::from_secs(2)))
            .unwrap();
    }
    fn read_http_body(stream: &mut impl Read) -> Vec<u8> {
        let mut raw = vec![];
        let mut chunk = [0u8; 4096];
        let split = loop {
            let n = stream.read(&mut chunk).unwrap();
            assert!(n > 0);
            raw.extend_from_slice(&chunk[..n]);
            assert!(raw.len() < 1 << 20);
            if let Some(i) = raw.windows(4).position(|w| w == b"\r\n\r\n") {
                break i + 4;
            }
        };
        let header = std::str::from_utf8(&raw[..split]).unwrap();
        assert!(header.starts_with("POST /question "));
        let length = header
            .lines()
            .find_map(|line| {
                line.split_once(':')
                    .filter(|(k, _)| k.eq_ignore_ascii_case("content-length"))
                    .map(|(_, v)| v.trim().parse::<usize>().unwrap())
            })
            .unwrap();
        assert!(length < 1 << 20);
        while raw.len() - split < length {
            let n = stream.read(&mut chunk).unwrap();
            assert!(n > 0);
            raw.extend_from_slice(&chunk[..n]);
        }
        raw[split..split + length].to_vec()
    }
    struct LocalServer {
        url: String,
        accepted: Arc<AtomicBool>,
        stop: Arc<AtomicBool>,
        bodies: Arc<Mutex<Vec<Vec<u8>>>>,
        handle: Option<thread::JoinHandle<()>>,
    }
    impl LocalServer {
        fn new(outcome: Outcome) -> Self {
            let socket = TcpListener::bind("127.0.0.1:0").unwrap();
            socket.set_nonblocking(true).unwrap();
            let url = format!("http://{}/question", socket.local_addr().unwrap());
            let accepted = Arc::new(AtomicBool::new(false));
            let stop = Arc::new(AtomicBool::new(false));
            let bodies = Arc::new(Mutex::new(vec![]));
            let (ack, done, seen) = (accepted.clone(), stop.clone(), bodies.clone());
            let handle = thread::spawn(move || {
                let until = Instant::now() + Duration::from_secs(10);
                while !done.load(Ordering::SeqCst) && Instant::now() < until {
                    match socket.accept() {
                        Ok((mut stream, peer)) => {
                            assert!(peer.ip().is_loopback());
                            prepare_stream(&stream);
                            let body = read_http_body(&mut stream);
                            let count = {
                                let mut b = seen.lock().unwrap();
                                b.push(body);
                                b.len()
                            };
                            if count == 1 {
                                let delay = if matches!(outcome, Outcome::Expires) {
                                    10.0
                                } else {
                                    0.01
                                };
                                let body = json!({"retry_after":delay}).to_string();
                                write!(stream,"HTTP/1.1 429 Too Many Requests\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",body.len()).unwrap();
                                stream.flush().unwrap();
                            } else {
                                ack.store(true, Ordering::SeqCst);
                                if !matches!(outcome, Outcome::Lost) {
                                    let body = r#"{"ok":true}"#;
                                    write!(stream,"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",body.len()).unwrap();
                                    stream.flush().unwrap();
                                }
                            }
                        }
                        Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                            thread::sleep(Duration::from_millis(1))
                        }
                        Err(e) => panic!("local listener failed: {e}"),
                    }
                }
            });
            Self {
                url,
                accepted,
                stop,
                bodies,
                handle: Some(handle),
            }
        }
    }
    impl Drop for LocalServer {
        fn drop(&mut self) {
            self.stop.store(true, Ordering::SeqCst);
            self.handle.take().unwrap().join().unwrap();
        }
    }

    fn inherited_connection() -> (TcpStream, TcpStream) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        listener.set_nonblocking(true).unwrap();
        let client = TcpStream::connect(listener.local_addr().unwrap()).unwrap();
        let until = Instant::now() + Duration::from_secs(2);
        let stream = loop {
            match listener.accept() {
                Ok((stream, _)) => break stream,
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    assert!(Instant::now() < until);
                    thread::yield_now();
                }
                Err(e) => panic!("accept failed: {e}"),
            }
        };
        // Model Windows inheritance explicitly on every test platform.
        stream.set_nonblocking(true).unwrap();
        prepare_stream(&stream);
        (client, stream)
    }
    #[test]
    fn accepted_stream_has_finite_blocking_read_before_any_client_data() {
        let (_client, mut stream) = inherited_connection();
        let before = Instant::now();
        let error = stream.read(&mut [0u8; 1]).unwrap_err();
        #[cfg(windows)]
        assert_ne!(
            error.raw_os_error(),
            Some(10035),
            "nonblocking mode survived accept setup"
        );
        assert!(matches!(
            error.kind(),
            std::io::ErrorKind::WouldBlock | std::io::ErrorKind::TimedOut
        ));
        assert!(
            before.elapsed() >= Duration::from_secs(1),
            "read returned before finite timeout: {error}"
        );
    }
    #[test]
    fn accepted_stream_decodes_separately_arriving_header_and_body_fragments() {
        struct Observed {
            stream: TcpStream,
            read: mpsc::Sender<usize>,
        }
        impl Read for Observed {
            fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
                let n = self.stream.read(buf)?;
                self.read.send(n).unwrap();
                Ok(n)
            }
        }
        let (mut client, stream) = inherited_connection();
        let fragments: [&[u8]; 5] = [
            b"POST /que",
            b"stion HTTP/1.1\r\nContent-Len",
            b"gth: 5\r\n\r",
            b"\nhe",
            b"llo",
        ];
        let (tx, rx) = mpsc::channel();
        // Only one fragment can be in flight: consume it before releasing the
        // next. TCP cannot coalesce these into one initial request read.
        client.write_all(fragments[0]).unwrap();
        let reader = thread::spawn(move || read_http_body(&mut Observed { stream, read: tx }));
        for (index, fragment) in fragments.iter().enumerate() {
            let mut received = 0;
            while received < fragment.len() {
                let n = rx.recv_timeout(Duration::from_secs(3)).unwrap();
                assert!(n > 0);
                received += n;
            }
            assert_eq!(received, fragment.len());
            if let Some(next) = fragments.get(index + 1) {
                client.write_all(next).unwrap();
            }
        }
        assert_eq!(reader.join().unwrap(), b"hello");
    }

    // Only POST transport is real, and it is hard-bound to the local listener.
    // Authenticated GET responses and accepted message IDs use the existing
    // synthetic server, while the actual sender owns intent/Pending/receipt IO.
    struct LocalTransport {
        inner: Box<dyn Effects>,
        url: String,
        accepted: Arc<AtomicBool>,
        home: PathBuf,
        request: String,
        phase: String,
    }
    impl LocalTransport {
        fn pending(&self) {
            assert!(Ledger::load(&self.home)
                .unwrap()
                .effect(&self.request, &self.phase)
                .is_some());
        }
    }
    impl Effects for LocalTransport {
        fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
            self.inner.run(args, timeout)
        }
        fn http(
            &mut self,
            method: &str,
            url: &str,
            body: Option<&Value>,
            discord: bool,
        ) -> Result<Value> {
            if method != "POST"
                || !body.is_some_and(|b| {
                    core::s(&b.get("message").unwrap_or(b)["content"])
                        .contains(&format!(":{}", self.phase))
                })
            {
                return self.inner.http(method, url, body, discord);
            }
            self.pending();
            let result = RealEffects.http(method, &self.url, body, true);
            if self.accepted.swap(false, Ordering::SeqCst) {
                let receipt = self.inner.http(method, url, body, discord)?;
                if result.is_ok() {
                    return Ok(receipt);
                }
            }
            result
        }
        fn multipart(&mut self, url: &str, kind: &str, bytes: &[u8]) -> Result<Value> {
            self.pending();
            let result = RealEffects.multipart(&self.url, kind, bytes);
            if self.accepted.swap(false, Ordering::SeqCst) {
                let receipt = self.inner.multipart(url, kind, bytes)?;
                if result.is_ok() {
                    return Ok(receipt);
                }
            }
            result
        }
    }
    fn trial(with_image: bool, deadline: bool, outcome: Outcome) -> (bool, usize) {
        // Nextest isolates each test process. Override before creating any
        // transport thread; never read a real token or fall back to user auth.
        unsafe {
            std::env::set_var("GODD_DISCORD_BOT_TOKEN", "only-local-http-fixture");
        }
        let f = Fixture::new();
        let local = LocalServer::new(outcome);
        let mut ctx = f.context();
        let inner = ctx.effects;
        ctx.effects = Box::new(LocalTransport {
            inner,
            url: local.url.clone(),
            accepted: local.accepted.clone(),
            home: f.home.clone(),
            request: "rate-question".into(),
            phase: "question:0".into(),
        });
        let mut args = f.ask("rate-question", "choose the original direction");
        if with_image {
            let path = f.root.path().join("owned.png");
            fs::write(&path, b"\x89PNG\r\n\x1a\nsynthetic image fixture").unwrap();
            args.attach.push(path);
        }
        let expires = chrono::Utc::now() + chrono::Duration::seconds(2);
        if deadline {
            args.send_before = Some(expires.to_rfc3339());
        }
        let first = ask(&mut ctx, &args);
        let calls = local.bodies.lock().unwrap().len();
        let ledger = Ledger::load(&f.home).unwrap();
        assert_eq!(
            ledger
                .events
                .iter()
                .filter(|e| matches!(e, Event::Intent { .. }))
                .count(),
            1
        );
        assert!(ledger.effect(&args.request_id, "question:0").is_some());
        if let Ok(report) = &first {
            assert!(ledger.receipt(&args.request_id, "question:0").is_some());
            assert_eq!(report["delivery"], "verified");
            assert!(ledger.complete(&args.request_id));
        } else {
            assert!(ledger.receipt(&args.request_id, "question:0").is_none());
        }
        if matches!(outcome, Outcome::Expires) {
            let delay = (expires - chrono::Utc::now()).to_std().unwrap_or_default();
            thread::sleep(delay + Duration::from_millis(5));
        }
        let retry = ask(&mut ctx, &args);
        assert_eq!(retry.is_ok(), first.is_ok());
        assert_eq!(
            local.bodies.lock().unwrap().len(),
            calls,
            "journal must never resubmit an ambiguous phase"
        );
        if let (Ok(a), Ok(b)) = (&first, &retry) {
            assert_eq!(b["delivery"], "verified");
            assert_eq!(a["thread_id"], b["thread_id"]);
        }
        let bodies = local.bodies.lock().unwrap();
        if bodies.len() > 1 {
            assert!(
                bodies.windows(2).all(|p| p[0] == p[1]),
                "429 retry must use identical upload bytes"
            );
        }
        f.unchanged();
        (first.is_ok(), calls)
    }
    #[test]
    fn question_deadline_retries_definite_429_json_and_multipart() {
        let got: Vec<_> = [false, true]
            .into_iter()
            .map(|image| trial(image, true, Outcome::Success))
            .collect();
        assert_eq!(got, vec![(true, 2), (true, 2)]);
    }
    #[test]
    fn rate_limit_beyond_deadline_never_reposts() {
        for image in [false, true] {
            assert_eq!(trial(image, true, Outcome::Expires), (false, 1));
        }
    }
    #[test]
    fn lost_response_after_429_recovers_by_get_without_another_post() {
        let got: Vec<_> = [false, true]
            .into_iter()
            .map(|image| trial(image, true, Outcome::Lost))
            .collect();
        assert_eq!(got, vec![(true, 2), (true, 2)]);
    }
    #[test]
    fn ordinary_sender_retains_429_retry_compatibility() {
        for image in [false, true] {
            assert_eq!(trial(image, false, Outcome::Success), (true, 2));
        }
    }
    #[test]
    fn ordinary_log_keeps_real_http_retry_and_journal_deduplication() {
        unsafe {
            std::env::set_var("GODD_DISCORD_BOT_TOKEN", "only-local-http-fixture");
        }
        let f = Fixture::new();
        let local = LocalServer::new(Outcome::Success);
        let mut ctx = f.context();
        let inner = ctx.effects;
        ctx.effects = Box::new(LocalTransport {
            inner,
            url: local.url.clone(),
            accepted: local.accepted.clone(),
            home: f.home.clone(),
            request: "rate-log".into(),
            phase: "log:0".into(),
        });
        let args = f.log("rate-log", "A silent progress record.", false);
        let first = log(&mut ctx, &args).unwrap();
        assert_eq!(local.bodies.lock().unwrap().len(), 2);
        assert_eq!(
            log(&mut ctx, &args).unwrap()["thread_id"],
            first["thread_id"]
        );
        assert_eq!(local.bodies.lock().unwrap().len(), 2);
        assert!(Ledger::load(&f.home)
            .unwrap()
            .receipt("rate-log", "log:0")
            .is_some());
        f.unchanged();
    }
}
