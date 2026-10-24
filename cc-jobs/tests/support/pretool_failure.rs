//! A real protocol error followed by a child that never exits. The clock and
//! escape hatch are controlled by the fixture, not by wall-clock sleeps.
use super::*;

const QUOTA: &str = "Reconnecting... 1/5 (exceeded retry limit, last status: 429 Too Many Requests)";

#[test]
fn protocol_429_before_first_tool_reaps_the_idle_child_before_the_escape_hatch() {
    let t = Test::new();
    let j = t.put(job(json!({"id":"pretool", "name":"pretool", "model":"anthropic/claude-opus-5-5",
        "cwd":t.ctx.root, "codex":"codex.CMD", "sandbox":"read-only"})));
    t.rt.pause_allowed.store(true, Ordering::Relaxed);
    t.rt.procs.lock().unwrap().insert(321, 456);
    t.rt.child_alive.store(true, Ordering::Relaxed);
    let alive = t.rt.child_alive.clone();
    *t.rt.spawn_hook.lock().unwrap() = Some(Box::new(move || Box::new(FakeChild {
        alive: alive.clone(),
        out: Some(Box::new(Cursor::new(format!("{}\n", json!({"type":"error","message":QUOTA})).into_bytes()))),
    })));
    let clock = Arc::downgrade(&t.rt);
    let alive = t.rt.child_alive.clone();
    let escaped = Arc::new(AtomicBool::new(false));
    let escape = escaped.clone();
    *t.rt.sleep_hook.lock().unwrap() = Some(Box::new(move |_| {
        if clock.upgrade().unwrap().mono() >= 220. {
            escape.store(true, Ordering::Relaxed);
            alive.store(false, Ordering::Relaxed);
        }
    }));
    t.ctx.supervise(s(&j, "id")).unwrap();
    let saved = t.ctx.load(s(&j, "id")).unwrap();
    assert!(!escaped.load(Ordering::Relaxed), "the protocol error never reached bounded collection");
    assert_eq!(saved["status"], "failed");
    assert_eq!(saved["last_failure_kind"], "quota");
    assert_eq!(saved["last_error"], QUOTA);
    assert_eq!(saved["protocol_stall"]["stage"], "drained");
    assert_eq!(saved["steps"], 0);
    assert_eq!(*t.rt.kills.lock().unwrap(), [321]);
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed), 1);
    assert!(saved["cancel_requested"] != true);
    for key in ["id", "cwd", "sandbox", "model", "prompt", "todo_id"] {
        assert_eq!(saved[key], j[key], "{key}");
    }
}
