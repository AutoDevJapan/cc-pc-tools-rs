use cc_ask::core::{Effects, RealEffects, ScopedHttpPool};
use std::{
    io::{Read, Write},
    net::TcpListener,
    sync::{
        atomic::{AtomicUsize, Ordering},
        Arc,
    },
    time::{Duration, Instant},
};

#[test]
fn explicit_bulk_scope_reuses_connection_for_two_real_requests() {
    let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
    let address = listener.local_addr().unwrap();
    listener.set_nonblocking(true).unwrap();
    let served = Arc::new(AtomicUsize::new(0));
    let accepted = Arc::new(AtomicUsize::new(0));
    let served_server = served.clone();
    let accepted_server = accepted.clone();
    let server = std::thread::spawn(move || {
        let deadline = Instant::now() + Duration::from_secs(5);
        let mut connections = vec![];
        while served_server.load(Ordering::SeqCst) < 2 && Instant::now() < deadline {
            match listener.accept() {
                Ok((mut stream, _)) => {
                    // Windows accepts inherit the listener's nonblocking mode.
                    stream.set_nonblocking(false).unwrap();
                    accepted_server.fetch_add(1, Ordering::SeqCst);
                    let served = served_server.clone();
                    connections.push(std::thread::spawn(move || {
                        stream.set_read_timeout(Some(Duration::from_millis(500))).unwrap();
                        stream.set_write_timeout(Some(Duration::from_millis(500))).unwrap();
                        for _ in 0..2 {
                            let mut header=vec![];
                            loop {
                                let mut byte=[0];
                                if stream.read_exact(&mut byte).is_err() {return;}
                                header.push(byte[0]);
                                if header.ends_with(b"\r\n\r\n") {break;}
                                if header.len()>8192 {return;}
                            }
                            stream.write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: keep-alive\r\n\r\n{}").unwrap();
                            served.fetch_add(1,Ordering::SeqCst);
                        }
                    }));
                }
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(5))
                }
                Err(e) => panic!("loopback fixture: {e}"),
            }
        }
        for c in connections {
            c.join().unwrap();
        }
    });
    {
        let _pool = ScopedHttpPool::new();
        for _ in 0..2 {
            assert_eq!(
                RealEffects
                    .http("GET", &format!("http://{address}/fixture"), None, false)
                    .unwrap(),
                serde_json::json!({})
            );
        }
    }
    server.join().unwrap();
    assert_eq!(served.load(Ordering::SeqCst), 2);
    assert_eq!(
        accepted.load(Ordering::SeqCst),
        1,
        "bulk reads reconnected instead of reusing their socket"
    );
}
