use cc_jobs::{adaptive::*, *};
use std::{
    io::{Read, Write},
    net::TcpListener,
    time::{Duration, Instant},
};

fn quota(at: f64, used: f64) -> QuotaSnapshot {
    parse_quotas(
        &json!({"accounts":[{"id":"pool-a","quota":{"weeklyPercent":used,"weeklyResetAt":100000.}}]}),
        at,
    )
}
fn state() -> LimitState {
    LimitState {
        max_running: 25,
        first_at: 1000.,
        checked_at: 1300.,
        mode: "auto".into(),
        machine: MachineSample {
            cpu_times: Some([100, 200, 100]),
            ..Default::default()
        },
        openai: vec![quota(1300., 20.)],
        quota_checked_at: 1300.,
        ..Default::default()
    }
}
fn healthy() -> MachineSample {
    MachineSample {
        free_memory_gib: Some(20.),
        cpu_times: Some([180, 300, 100]),
        health_ms: Some(10.),
    }
}
#[test]
fn stable_queue_increases_and_clamps_at_max() {
    let cfg = AdaptiveConfig::default();
    assert_eq!(
        decide(&cfg, state(), healthy(), 1360., 1, 0, 0.).max_running,
        27
    );
    let mut s = state();
    s.max_running = 59;
    assert_eq!(decide(&cfg, s, healthy(), 1360., 1, 0, 0.).max_running, 60);
}
#[test]
fn queue_quiet_period_and_unknown_samples_prevent_growth() {
    let cfg = AdaptiveConfig::default();
    for (now, queued, errors, last) in [(1360., 0, 0, 0.), (1200., 1, 0, 0.), (1360., 1, 1, 1350.)]
    {
        assert_eq!(
            decide(&cfg, state(), healthy(), now, queued, errors, last).max_running,
            25
        );
    }
    let mut sample = healthy();
    sample.free_memory_gib = None;
    assert_eq!(
        decide(&cfg, state(), sample, 1360., 1, 0, 0.).max_running,
        25
    );
    let mut s = state();
    s.quota_checked_at = 500.;
    assert_eq!(decide(&cfg, s, healthy(), 1360., 1, 0, 0.).max_running, 25);
}
#[test]
fn every_pressure_signal_halves_without_going_below_min() {
    let cfg = AdaptiveConfig::default();
    let mut slow = healthy();
    slow.health_ms = Some(1000.);
    let mut failed = healthy();
    failed.health_ms = None;
    let mut memory = healthy();
    memory.free_memory_gib = Some(15.9);
    let mut cpu = healthy();
    cpu.cpu_times = Some([100, 300, 100]);
    for sample in [slow, failed, memory, cpu] {
        let s = decide(&cfg, state(), sample, 1360., 1, 0, 0.);
        assert_eq!(s.max_running, 12);
        assert!(!s.reason.is_empty());
    }
    let mut s = state();
    s.max_running = 4;
    assert_eq!(
        decide(&cfg, s, healthy(), 1360., 1, 2, 1350.).max_running,
        4
    );
}
#[test]
fn old_429_does_not_repeatedly_halve_but_new_errors_do() {
    let cfg = AdaptiveConfig::default();
    let s = decide(&cfg, state(), healthy(), 1360., 1, 2, 1350.);
    assert_eq!(s.max_running, 12);
    let mut next = healthy();
    next.cpu_times = Some([260, 400, 100]);
    let s = decide(&cfg, s, next.clone(), 1420., 1, 2, 1350.);
    assert_eq!(s.max_running, 12);
    assert_eq!(decide(&cfg, s, next, 1480., 1, 3, 1470.).max_running, 6);
}
#[test]
fn quota_parser_ignores_excluded_accounts_and_private_metadata() {
    let q = parse_quotas(
        &json!({"accounts":[
        {"id":"a","email":"private","token":"private","quota":{"weeklyPercent":30.,"weeklyResetAt":1792000000000u64}},
        {"id":"b","paused":true}, {"id":"c","needsReauth":true}]}),
        1000.,
    );
    assert!(q.complete);
    assert_eq!(q.accounts.len(), 1);
    assert_eq!(q.accounts[0].reset_at, 1792000000.);
    assert!(!serde_json::to_string(&q).unwrap().contains("private"));
    for bad in [
        json!({"accounts":[{"id":"a","quota":null}]}),
        json!({"accounts":[{"id":"a","quota":{"weeklyPercent":30.,"weeklyResetAt":999.}}]}),
        json!({"accounts":[{"id":"a","quotaRefresh":{"status":"timeout"},"quota":{"weeklyPercent":30.,"weeklyResetAt":9999.}}]}),
    ] {
        assert!(!parse_quotas(&bad, 1000.).complete);
    }
}
#[test]
fn pacing_matches_accounts_and_reset_windows() {
    let history = vec![quota(1000., 20.), quota(1300., 21.)];
    let expected = (1. / 300.) / (79. / 98700.);
    assert!((pace_ratio(&history).unwrap() - expected).abs() < 1e-8);
    let mut changed = history.clone();
    changed[1].accounts[0].reset_at += 100.;
    assert!(pace_ratio(&changed).is_none());
    changed = history.clone();
    changed[1].accounts[0].id = "new".into();
    assert!(pace_ratio(&changed).is_none());
    changed = history.clone();
    changed[1].accounts[0].used_percent = 0.;
    assert!(pace_ratio(&changed).is_none());
}
#[test]
fn pacing_reduces_once_per_new_measurement_and_recommends_claude() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.openai = vec![quota(1000., 20.), quota(1300., 21.)];
    s.anthropic = quota(1300., 10.);
    let s = decide(&cfg, s, healthy(), 1360., 1, 0, 0.);
    assert_eq!(s.max_running, 6);
    assert_eq!(s.preferred_provider.as_deref(), Some("anthropic"));
    let mut sample = healthy();
    sample.cpu_times = Some([260, 400, 100]);
    let s = decide(&cfg, s, sample, 1420., 1, 0, 0.);
    assert_eq!(s.max_running, 6);
    assert!(s.reason.contains("減枠後"));
}
#[test]
fn exhausted_weekly_or_short_window_reduces_to_floor() {
    let cfg = AdaptiveConfig::default();
    for short in [false, true] {
        let mut s = state();
        s.openai[0].accounts[0].used_percent = if short { 30. } else { 100. };
        s.openai[0].accounts[0].short_exhausted = short;
        assert_eq!(decide(&cfg, s, healthy(), 1360., 1, 0, 0.).max_running, 4);
    }
}
#[test]
fn cpu_initial_reset_or_zero_delta_is_unknown() {
    assert_eq!(
        cpu_load(Some([100, 200, 100]), Some([180, 300, 100])),
        Some(20.)
    );
    assert_eq!(cpu_load(None, Some([180, 300, 100])), None);
    assert_eq!(cpu_load(Some([100, 200, 100]), Some([10, 20, 10])), None);
    assert_eq!(cpu_load(Some([100, 200, 100]), Some([100, 200, 100])), None);
}
#[test]
fn error_events_survive_success_and_only_count_http_429() {
    let mut job = json!({});
    apply_event(
        &mut job,
        &json!({"type":"error","message":"429 Too Many Requests","_ts":"2026-10-08T17:00:00+09:00"}),
    );
    apply_event(
        &mut job,
        &json!({"type":"turn.failed","error":{"message":"429 Too Many Requests"},"_ts":"2026-10-08T17:00:01+09:00"}),
    );
    apply_event(
        &mut job,
        &json!({"type":"error","message":"ordinary error","_ts":"2026-10-08T17:01:00+09:00"}),
    );
    apply_event(&mut job, &json!({"type":"turn.completed","usage":{}}));
    assert_eq!(job["rate_limit_times"].as_array().unwrap().len(), 1);
}
#[test]
fn cpu_counter_rollback_starts_again_at_floor() {
    let mut sample = healthy();
    sample.cpu_times = Some([1, 2, 1]);
    assert_eq!(
        decide(&AdaptiveConfig::default(), state(), sample, 1360., 1, 0, 0.).max_running,
        4
    );
}
#[test]
fn health_response_and_slow_trickle_have_bounded_deadline() {
    for trickle in [false, true] {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let thread = std::thread::spawn(move || {
            let (mut socket, _) = listener.accept().unwrap();
            let mut request = [0; 512];
            let _ = socket.read(&mut request);
            let response = b"HTTP/1.0 200 OK\r\n\r\n{\"service\":\"opencodex\",\"status\":\"ok\"}";
            if trickle {
                for byte in response {
                    if socket.write_all(&[*byte]).is_err() {
                        break;
                    };
                    std::thread::sleep(Duration::from_millis(20));
                }
            } else {
                socket.write_all(response).unwrap();
            }
        });
        let start = Instant::now();
        let result = health_probe(port, Duration::from_millis(120));
        if trickle {
            assert!(result.is_err());
            assert!(start.elapsed() < Duration::from_millis(400));
        } else {
            assert!(result.is_ok());
        }
        thread.join().unwrap();
    }
}
