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

#[test]
fn claude_lane_ignores_codex_pace_but_obeys_resources_and_its_own_429() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.max_running = 4;
    s.claude_max_running = 12;
    s.claude_stable_since = 1000.;
    s.anthropic = quota(1300., 10.);
    s.openai = vec![quota(1000., 20.), quota(1300., 22.2)];
    let result = decide_providers(
        &cfg,
        s.clone(),
        next_sample(&s, Some(2.), 40., 7),
        1360.,
        36,
        2,
        1350.,
        0,
        0.,
    );
    assert_eq!(result.max_running, 4);
    assert_eq!(result.claude_max_running, 20);
    assert_eq!(result.preferred_provider.as_deref(), Some("anthropic"));
    for (health, memory, cpu, recent, latest) in [
        (None, 40., 7, 0, 0.),
        (Some(2.), 15., 7, 0, 0.),
        (Some(2.), 40., 85, 0, 0.),
        (Some(2.), 40., 7, 2, 1350.),
    ] {
        let sample = next_sample(&s, health, memory, cpu);
        let result = decide_providers(&cfg, s.clone(), sample, 1360., 36, 0, 0., recent, latest);
        assert_eq!(result.claude_max_running, 6);
    }
    s.anthropic = quota(1300., 100.);
    let sample = next_sample(&s, Some(2.), 40., 7);
    let result = decide_providers(&cfg, s, sample, 1360., 36, 0, 0., 0, 0.);
    assert_eq!(result.claude_max_running, 4);
    assert_eq!(result.preferred_provider, None);
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
fn next_sample(s: &LimitState, health_ms: Option<f64>, memory: f64, cpu: u64) -> MachineSample {
    let [idle, kernel, user] = s.machine.cpu_times.unwrap();
    MachineSample {
        cpu_times: Some([idle + 100 - cpu, kernel + 100, user]),
        free_memory_gib: Some(memory),
        health_ms,
    }
}
#[test]
fn isolated_slow_probe_preserves_quiet_time_and_recovery_after_serialization() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.stable_since = 1000.;
    let sample = next_sample(&s, Some(1926.), 40., 7);
    let s = decide(&cfg, s, sample, 1360., 36, 0, 0.);
    assert_eq!(s.max_running, 25);
    assert_eq!(s.stable_since, 1000.);
    assert!(s.reason.contains("単発"));
    let s: LimitState = serde_json::from_str(&serde_json::to_string(&s).unwrap()).unwrap();
    let sample = next_sample(&s, Some(2.), 40., 7);
    let s = decide(&cfg, s, sample, 1420., 36, 0, 0.);
    assert_eq!(s.max_running, 37);
    assert_eq!(s.stable_since, 1000.);
}
#[test]
fn repeated_slow_probes_halve_and_a_fast_probe_breaks_the_sequence() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    for (now, ms, expected) in [
        (1360., 1129., 25),
        (1420., 1926., 12),
        (1480., 2., 12),
        (1540., 1129., 12),
        (1600., 1000., 6),
    ] {
        let sample = next_sample(&s, Some(ms), 40., 7);
        s = decide(&cfg, s, sample, now, 36, 0, 0.);
        assert_eq!(s.max_running, expected, "at {now}");
    }
    assert_eq!(s.stable_since, 1600.);
}
#[test]
fn old_slow_probe_does_not_count_as_consecutive_pressure() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.machine.health_ms = Some(1926.);
    s.checked_at = 1000.;
    let sample = next_sample(&s, Some(1129.), 40., 7);
    let s = decide(&cfg, s, sample, 1360., 36, 0, 0.);
    assert_eq!(s.max_running, 25);
}
#[test]
fn pace_spike_is_bounded_and_only_new_quota_can_reduce_again() {
    let cfg = AdaptiveConfig::default();
    for claude_available in [false, true] {
        let mut s = state();
        s.max_running = 14;
        s.openai = vec![quota(1000., 20.), quota(1300., 22.2)];
        if claude_available {
            s.anthropic = quota(1300., 10.);
        }
        let sample = next_sample(&s, Some(2.), 40., 7);
        let mut s = decide(&cfg, s, sample, 1360., 36, 0, 0.);
        assert!(s.pace_ratio.unwrap() > 9.);
        assert_eq!(s.max_running, 11);
        assert_eq!(
            s.preferred_provider.as_deref(),
            claude_available.then_some("anthropic")
        );
        let sample = next_sample(&s, Some(2.), 40., 7);
        s = decide(&cfg, s, sample, 1420., 36, 0, 0.);
        assert_eq!(s.max_running, 11);
        s.openai.push(quota(1600., 24.4));
        s.quota_checked_at = 1600.;
        s.anthropic.at = 1600.;
        let sample = next_sample(&s, Some(2.), 40., 7);
        s = decide(&cfg, s, sample, 1600., 36, 0, 0.);
        assert_eq!(s.max_running, 9);
        assert_eq!(
            s.preferred_provider.as_deref(),
            claude_available.then_some("anthropic")
        );
    }
}
#[test]
fn pace_differences_shorter_than_five_minutes_are_not_used() {
    assert!(pace_ratio(&[quota(1000., 20.), quota(1299., 22.)]).is_none());
}
#[test]
fn ample_headroom_recovers_quickly_and_respects_all_growth_gates() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.max_running = 4;
    s.stable_since = 1000.;
    for (now, expected) in [
        (1360., 12),
        (1420., 20),
        (1480., 30),
        (1540., 45),
        (1600., 60),
    ] {
        // A valid, unchanged quota remains fresh across the recovery sequence.
        s.quota_checked_at = now;
        s.openai = vec![quota(now, 20.)];
        let sample = next_sample(&s, Some(2.), 40., 7);
        s = decide(&cfg, s, sample, now, 36, 0, 0.);
        assert_eq!(s.max_running, expected);
    }
    for (queued, errors, last, stable, quota_at) in [
        (0, 0, 0., 1000., 1300.),
        (36, 1, 1350., 1000., 1300.),
        (36, 0, 0., 1300., 1300.),
        (36, 0, 0., 1000., 500.),
    ] {
        let mut s = state();
        s.max_running = 4;
        s.stable_since = stable;
        s.quota_checked_at = quota_at;
        let sample = next_sample(&s, Some(2.), 40., 7);
        assert_eq!(
            decide(&cfg, s, sample, 1360., queued, errors, last).max_running,
            4
        );
    }
    for (ms, memory, cpu) in [(500., 40., 7), (2., 31.9, 7), (2., 40., 43)] {
        let s = state();
        let sample = next_sample(&s, Some(ms), memory, cpu);
        assert_eq!(decide(&cfg, s, sample, 1360., 36, 0, 0.).max_running, 27);
    }
}
#[test]
fn machine_pressure_thresholds_still_halve_immediately_even_with_a_pace_spike() {
    let cfg = AdaptiveConfig::default();
    for (memory, cpu, expected) in [(16., 84, 27), (16., 85, 12), (15.99, 7, 12)] {
        let s = state();
        let sample = next_sample(&s, Some(2.), memory, cpu);
        assert_eq!(
            decide(&cfg, s, sample, 1360., 36, 0, 0.).max_running,
            expected
        );
    }
    let mut s = state();
    s.openai = vec![quota(1000., 20.), quota(1300., 22.2)];
    let sample = next_sample(&s, Some(2.), 40., 85);
    assert_eq!(decide(&cfg, s, sample, 1360., 36, 0, 0.).max_running, 12);
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
    let mut failed = healthy();
    failed.health_ms = None;
    let mut memory = healthy();
    memory.free_memory_gib = Some(15.9);
    let mut cpu = healthy();
    cpu.cpu_times = Some([100, 300, 100]);
    for sample in [failed, memory, cpu] {
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
    assert_eq!(s.max_running, 19);
    assert_eq!(s.preferred_provider.as_deref(), Some("anthropic"));
    let mut sample = healthy();
    sample.cpu_times = Some([260, 400, 100]);
    let s = decide(&cfg, s, sample, 1420., 1, 0, 0.);
    assert_eq!(s.max_running, 19);
    assert!(s.reason.contains("減枠後"));
}
#[test]
fn changing_unused_reset_windows_do_not_hide_the_active_account_pace() {
    let mut earlier = quota(1000., 11.);
    let mut later = quota(1300., 12.);
    earlier.accounts.push(AccountQuota {
        id: "unused".into(),
        used_percent: 0.,
        reset_at: 200000.,
        short_exhausted: false,
    });
    later.accounts.push(AccountQuota {
        id: "unused".into(),
        used_percent: 0.,
        reset_at: 200300.,
        short_exhausted: false,
    });
    let expected = (1. / 300.) / (88. / 98700.);
    let history = vec![earlier, later];
    assert!((pace_ratio(&history).unwrap() - expected).abs() < 1e-8);
    let mut s = state();
    s.openai = history;
    s.anthropic = quota(1300., 48.);
    let s = decide(&AdaptiveConfig::default(), s, healthy(), 1360., 1, 0, 0.);
    assert_eq!(s.max_running, 19);
    assert_eq!(s.preferred_provider.as_deref(), Some("anthropic"));
}
#[test]
fn account_addition_and_another_account_reset_keep_stable_account_comparisons() {
    let old = quota(1000., 20.);
    let mut new = quota(1300., 21.);
    new.accounts.push(AccountQuota {
        id: "new".into(),
        used_percent: 1.,
        reset_at: 200000.,
        short_exhausted: false,
    });
    assert!(pace_ratio(&[old, new]).is_some());
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
fn known_account_pressure_is_not_hidden_by_unknown_accounts_but_expired_windows_are_not_used() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.openai = vec![quota(1000., 20.), quota(1300., 21.)];
    for snapshot in &mut s.openai {
        snapshot.complete = false;
    }
    assert_eq!(
        decide(&cfg, s.clone(), healthy(), 1360., 1, 0, 0.).max_running,
        19
    );
    for snapshot in &mut s.openai {
        snapshot.accounts[0].reset_at = 1361.;
    }
    s.openai[1].accounts[0].used_percent = 100.;
    assert_eq!(decide(&cfg, s, healthy(), 1362., 1, 0, 0.).max_running, 25);
}
#[test]
fn modest_overspend_is_proportional_and_next_sample_uses_post_reduction_baseline() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.openai = vec![quota(1000., 20.), quota(1300., 21.)];
    for q in &mut s.openai {
        q.accounts[0].reset_at = 27370.;
    }
    let mut s = decide(&cfg, s, healthy(), 1360., 1, 0, 0.);
    assert_eq!(s.max_running, 22);
    let mut next = quota(1600., 21.1);
    next.accounts[0].reset_at = 27370.;
    s.openai.push(next);
    s.quota_checked_at = 1600.;
    let mut sample = healthy();
    sample.cpu_times = Some([260, 400, 100]);
    let s = decide(&cfg, s, sample, 1600., 1, 0, 0.);
    assert_eq!(s.max_running, 22);
    assert!(s.pace_ratio.unwrap() < 1.);
}
#[test]
fn incompatible_old_quota_does_not_hide_new_comparable_window() {
    let mut history = vec![quota(1000., 20.), quota(1300., 20.), quota(1600., 21.)];
    history[0].accounts[0].reset_at = 99000.;
    let expected = (1. / 300.) / (79. / 98400.);
    assert!((pace_ratio(&history).unwrap() - expected).abs() < 1e-8);
}
#[test]
fn usage_decrease_rebases_only_that_account_and_future_quota_cannot_grow_the_limit() {
    let history = vec![quota(1000., 90.), quota(1300., 20.), quota(1600., 21.)];
    let expected = (1. / 300.) / (79. / 98400.);
    assert!((pace_ratio(&history).unwrap() - expected).abs() < 1e-8);
    let mut s = state();
    s.openai[0].at = 1460.;
    let s = decide(&AdaptiveConfig::default(), s, healthy(), 1360., 1, 0, 0.);
    assert!(s.pace_ratio.is_none());
    assert_eq!(s.max_running, 25);
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
