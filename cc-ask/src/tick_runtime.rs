//! Monotonic, cooperative deadlines. No worker can continue Discord effects after a tick exits.
use crate::core::{timestamp, Result};
use std::{
    cell::RefCell,
    time::{Duration, Instant},
};

pub const TICK_LIMIT: Duration = Duration::from_secs(50);
pub const EXTERNAL_LIMIT: Duration = Duration::from_secs(20);
thread_local! {
    static DEADLINES: RefCell<Vec<Instant>> = const { RefCell::new(Vec::new()) };
}

pub struct Deadline;
impl Deadline {
    pub fn new(limit: Duration) -> Self {
        DEADLINES.with(|d| d.borrow_mut().push(Instant::now() + limit));
        Self
    }
}
impl Drop for Deadline {
    fn drop(&mut self) {
        DEADLINES.with(|d| {
            d.borrow_mut().pop();
        });
    }
}
pub fn active() -> bool {
    DEADLINES.with(|d| !d.borrow().is_empty())
}
pub fn remaining() -> Option<Duration> {
    DEADLINES.with(|d| {
        d.borrow()
            .iter()
            .min()
            .map(|t| t.saturating_duration_since(Instant::now()))
    })
}
pub fn check() -> Result<()> {
    if remaining().is_some_and(|left| left.is_zero()) {
        Err("TickDeadline: remaining work deferred to next tick".into())
    } else {
        Ok(())
    }
}
pub fn timeout(limit: Duration) -> Result<Duration> {
    check()?;
    Ok(remaining().map_or(limit, |left| left.min(limit)))
}
pub fn stage<T>(name: &str, limit: Duration, action: impl FnOnce() -> Result<T>) -> Result<T> {
    check()?;
    let _deadline = Deadline::new(limit);
    let _span = Span::new(format!("stage {name}"));
    let result = action();
    if let Err(error) = &result {
        println!(
            "{} tick stage failed {name}: {}",
            timestamp(),
            crate::core::error_detail(error)
        );
    }
    result
}
pub struct Span {
    name: String,
    start: Instant,
    enabled: bool,
}
impl Span {
    pub fn new(name: impl Into<String>) -> Self {
        let span = Self {
            name: name.into(),
            start: Instant::now(),
            enabled: active(),
        };
        if span.enabled {
            println!(
                "{} tick span start pid={} {}",
                timestamp(),
                std::process::id(),
                span.name
            );
        }
        span
    }
}

/// Last-resort ceiling for native calls that cannot be interrupted cooperatively.
/// Atomic state checkpoints survive; process exit releases all OS byte locks.
pub struct HardLimit(Vec<std::sync::mpsc::Sender<()>>);
impl HardLimit {
    pub fn new(log: std::path::PathBuf, limit: Duration) -> Result<Self> {
        Self::for_command(log, limit, "tick")
    }
    pub fn for_command(
        log: std::path::PathBuf,
        limit: Duration,
        command: &'static str,
    ) -> Result<Self> {
        let (send, receive) = std::sync::mpsc::channel();
        let (kill_send, kill_receive) = std::sync::mpsc::channel();
        // The logger may block on a stream or filesystem. It never gates the
        // independent termination thread; allow at most 100ms to record evidence.
        std::thread::Builder::new().name("cc-ask-limit-log".into()).spawn(move || {
            if receive.recv_timeout(limit) == Err(std::sync::mpsc::RecvTimeoutError::Timeout) {
                use std::io::Write;
                if let Ok(mut file) = std::fs::OpenOptions::new().create(true).append(true).open(log) {
                    let _ = writeln!(file, "{} {command} hard deadline pid={} elapsed_ms={}; remaining work deferred; releasing locks", timestamp(), std::process::id(), limit.as_millis());
                }
                eprintln!("{} {command} hard deadline pid={} elapsed_ms={}; remaining work deferred; releasing locks", timestamp(), std::process::id(), limit.as_millis());
            }
        }).map_err(|e| e.to_string())?;
        std::thread::Builder::new()
            .name("cc-ask-tick-limit".into())
            .spawn(move || {
                if kill_receive.recv_timeout(limit + Duration::from_millis(100))
                    == Err(std::sync::mpsc::RecvTimeoutError::Timeout)
                {
                    // Avoid CRT/stdio cleanup waiting behind the stuck main thread.
                    #[cfg(windows)]
                    unsafe {
                        windows_sys::Win32::System::Threading::TerminateProcess(
                            windows_sys::Win32::System::Threading::GetCurrentProcess(),
                            0,
                        );
                    }
                    #[cfg(not(windows))]
                    std::process::exit(0);
                }
            })
            .map_err(|e| e.to_string())?;
        Ok(Self(vec![send, kill_send]))
    }
}
impl Drop for HardLimit {
    fn drop(&mut self) {
        for send in &self.0 {
            let _ = send.send(());
        }
    }
}
impl Drop for Span {
    fn drop(&mut self) {
        if self.enabled {
            println!(
                "{} tick span end pid={} {} elapsed_ms={}",
                timestamp(),
                std::process::id(),
                self.name,
                self.start.elapsed().as_millis()
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn nested_deadlines_cannot_extend_outer_budget_and_restore_on_exit() {
        let _outer = Deadline::new(Duration::from_millis(25));
        {
            let _inner = Deadline::new(Duration::from_secs(10));
            assert!(timeout(Duration::from_secs(20)).unwrap() <= Duration::from_millis(25));
        }
        std::thread::sleep(Duration::from_millis(30));
        assert!(check().is_err());
        drop(_outer);
        assert!(!active());
        assert!(check().is_ok());
    }
}
