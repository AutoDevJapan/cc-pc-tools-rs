//! Reproduce ureq's connect timeout overriding its overall request timeout.
#[cfg(windows)]
#[test]
fn saturated_local_connect_queue_obeys_tick_deadline_instead_of_default_thirty_seconds() {
    use cc_ask::{
        core::{Effects, RealEffects},
        tick_runtime::Deadline,
    };
    use std::{
        net::{TcpListener, TcpStream},
        os::windows::io::AsRawSocket,
        time::{Duration, Instant},
    };
    let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
    let address = listener.local_addr().unwrap();
    // Keep the accept queue small and never accept a connection. All traffic
    // stays on loopback; closing our listener also bounds the old broken path.
    assert_eq!(
        unsafe {
            windows_sys::Win32::Networking::WinSock::listen(listener.as_raw_socket() as _, 1)
        },
        0
    );
    let mut held = Vec::new();
    let mut saturated = false;
    for _ in 0..256 {
        match TcpStream::connect_timeout(&address, Duration::from_millis(20)) {
            Ok(stream) => held.push(stream),
            Err(error)
                if matches!(
                    error.kind(),
                    std::io::ErrorKind::TimedOut | std::io::ErrorKind::WouldBlock
                ) =>
            {
                saturated = true;
                break;
            }
            Err(error) => panic!("loopback fixture failed: {error}"),
        }
    }
    assert!(saturated, "local listener queue did not saturate");
    let (stop, stopped) = std::sync::mpsc::channel::<()>();
    let server = std::thread::spawn(move || {
        let _ = stopped.recv_timeout(Duration::from_millis(1500));
        drop(listener);
    });
    let deadline = Deadline::new(Duration::from_millis(200));
    let start = Instant::now();
    let result = RealEffects.http("GET", &format!("http://{address}/fixture"), None, false);
    let elapsed = start.elapsed();
    drop(deadline);
    let _ = stop.send(());
    server.join().unwrap();
    drop(held);
    assert!(result.is_err());
    assert!(
        elapsed < Duration::from_millis(800),
        "connect timeout overrode the tick deadline: {elapsed:?}"
    );
}
