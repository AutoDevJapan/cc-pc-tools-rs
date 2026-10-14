use cc_jobs::{adaptive::*, *};
use std::{
    io::{Read, Write},
    net::TcpListener,
    time::{Duration, Instant},
};

#[test]
fn capacity_eligibility_accepts_cli_healthy_label_and_excludes_paid_keys_and_auth_failure() {
    for (kind, health, expected) in [
        ("oauth", "Healthy", 1),
        ("api_key", "Healthy", 0),
        ("oauth", "reauth_required", 0),
    ] {
        let q = parse_quotas(
            &json!({"accounts":[{"id":"existing","type":kind,"health":health,"quota":{"weeklyPercent":2.,"weeklyResetAt":2000.}}]}),
            1000.,
        );
        assert_eq!(q.accounts.len(), expected);
    }
}

#[test]
fn cargo_limit_patch_retains_owner_job_limit_and_rejects_invalid_values() {
    let current = json!({"fixed_max_running":100,"max_running":100});
    let merged = queue::merge_queue_config(
        &current,
        &json!({"cargo_slots":2,"cargo_jobs_per_build":8,"test_threads":8}),
        false,
    )
    .unwrap();
    assert_eq!(merged["fixed_max_running"], 100);
    assert_eq!(merged["max_running"], 100);
    let cfg: queue::QueueConfig = serde_json::from_value(merged).unwrap();
    assert_eq!(
        (cfg.cargo_slots, cfg.cargo_jobs_per_build, cfg.test_threads),
        (Some(2), Some(8), Some(8))
    );
    for patch in [
        json!({"cargo_slots":0}),
        json!({"cargo_jobs_per_build":11}),
        json!({"test_threads":0}),
        json!({"test_threads":11}),
        json!({"test_threds":8}),
    ] {
        assert!(queue::merge_queue_config(&current, &patch, false).is_err());
    }
}

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
fn next_sample(s: &LimitState, health_ms: Option<f64>, memory: f64, cpu: u64) -> MachineSample {
    let [idle, kernel, user] = s.machine.cpu_times.unwrap();
    MachineSample {
        cpu_times: Some([idle + 100 - cpu, kernel + 100, user]),
        free_memory_gib: Some(memory),
        health_ms,
    }
}
#[test]
fn hardware_grows_fast_then_holds_the_cpu_operating_band() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.max_running = 4;
    for (now, expected) in [
        (1360., 12),
        (1420., 20),
        (1480., 30),
        (1540., 45),
        (1600., 60),
    ] {
        let sample = next_sample(&s, Some(2.), 40., 7);
        s = decide(&cfg, s, sample, now, 36, 0, 0.);
        assert_eq!(s.max_running, expected);
    }
    for cpu in [75, 80, 84] {
        let mut s = state();
        s.max_running = 32;
        let sample = next_sample(&s, Some(2.), 40., cpu);
        assert_eq!(decide(&cfg, s, sample, 1360., 36, 0, 0.).max_running, 32);
    }
}
#[test]
fn hardware_resource_thresholds_and_growth_speed() {
    let cfg = AdaptiveConfig::default();
    for (memory, cpu, expected) in [
        (16., 74, 28),
        (16., 75, 25),
        (16., 85, 22),
        (15.99, 7, 20),
        (40., 95, 21),
        (40., 7, 37),
        (24., 50, 31),
        (20., 65, 28),
    ] {
        let s = state();
        let sample = next_sample(&s, Some(2.), memory, cpu);
        assert_eq!(
            decide(&cfg, s, sample, 1360., 36, 0, 0.).max_running,
            expected,
            "memory {memory}, CPU {cpu}"
        );
    }
}
#[test]
fn quota_and_codex_429_never_reduce_the_hardware_ceiling() {
    let cfg = AdaptiveConfig::default();
    for (known, used) in [(true, 100.), (false, 100.), (false, 20.)] {
        let mut s = state();
        s.openai = vec![quota(1000., 20.), quota(1300., used)];
        s.openai.last_mut().unwrap().complete = known;
        let sample = next_sample(&s, Some(2.), 40., 7);
        assert_eq!(decide(&cfg, s, sample, 1360., 36, 3, 1350.).max_running, 37);
    }
}
#[test]
fn no_demand_unknown_counters_or_memory_hold_the_hardware_limit() {
    let cfg = AdaptiveConfig::default();
    assert_eq!(
        decide(&cfg, state(), healthy(), 1360., 0, 0, 0.).max_running,
        25
    );
    for which in [0, 1, 2] {
        let mut sample = healthy();
        match which {
            0 => sample.cpu_times = None,
            1 => sample.free_memory_gib = None,
            _ => sample.cpu_times = Some([1, 2, 1]),
        }
        assert_eq!(
            decide(&cfg, state(), sample, 1360., 36, 0, 0.).max_running,
            25
        );
    }
}
#[test]
fn health_delays_need_consecutive_fresh_samples_and_failure_reduces_immediately() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.stable_since = 1000.;
    for (now, health, expected) in [
        (1360., Some(1926.), 25),
        (1420., Some(1129.), 12),
        (1480., Some(2.), 20),
        (1540., Some(1129.), 20),
        (1600., None, 10),
    ] {
        let sample = next_sample(&s, health, 40., 7);
        s = decide(&cfg, s, sample, now, 36, 0, 0.);
        assert_eq!(s.max_running, expected);
        s = serde_json::from_value(json!(s)).unwrap();
    }
    let mut s = state();
    s.machine.health_ms = Some(1926.);
    s.checked_at = 1000.;
    let sample = next_sample(&s, Some(1129.), 40., 7);
    assert_eq!(decide(&cfg, s, sample, 1360., 36, 0, 0.).max_running, 25);
}
fn pool(at: f64, main_used: f64, reserve_used: f64) -> QuotaSnapshot {
    let mut q = quota(at, main_used);
    q.accounts[0].id = "__main__".into();
    for i in 0..4 {
        q.accounts.push(AccountQuota {
            id: format!("reserve{i}"),
            used_percent: reserve_used,
            reset_at: 200000. + at,
            short_exhausted: false,
        });
    }
    q
}
#[test]
fn aggregate_budget_includes_all_four_unused_accounts_with_rolling_resets() {
    let history = vec![pool(1000., 69., 1.), pool(1300., 70., 1.)];
    let expected = (1. / 300.) / (30. / 98700. + 396. / 200000.);
    assert!((pace_ratio(&history).unwrap() - expected).abs() < 1e-9);
    let mut s = state();
    s.openai = history;
    s.anthropic = quota(1300., 10.);
    let sample = next_sample(&s, Some(2.), 40., 7);
    let s = decide_providers(
        &AdaptiveConfig::default(),
        s,
        sample,
        1360.,
        36,
        0,
        0.,
        0,
        0.,
    );
    assert_eq!(s.hardware_max_running, 37);
    assert_eq!(s.max_running, 37);
    assert_eq!(s.claude_max_running, 37);
    assert!(s.preferred_provider.is_none());
}
#[test]
fn aggregate_consumption_sums_accounts_and_rebases_reset_or_decreased_usage() {
    let mut old = pool(1000., 90., 80.);
    let mut new = pool(1300., 91., 81.);
    for q in &mut old.accounts {
        q.reset_at = 100000.;
    }
    for q in &mut new.accounts {
        q.reset_at = 100000.;
    }
    let expected = (5. / 300.) / (85. / 98700.);
    assert!((pace_ratio(&[old.clone(), new.clone()]).unwrap() - expected).abs() < 1e-9);
    new.accounts[0].used_percent = 1.;
    new.accounts[1].reset_at += 100.;
    let expected = (3. / 300.) / (156. / 98700. + 19. / 98800.);
    assert!((pace_ratio(&[old, new]).unwrap() - expected).abs() < 1e-9);
    assert!(pace_ratio(&[quota(1000., 20.), quota(1299., 21.)]).is_none());
    let mut incomplete = quota(1300., 99.);
    incomplete.complete = false;
    assert!(pace_ratio(&[quota(1000., 20.), incomplete]).is_none());
}
#[test]
fn true_pool_depletion_brakes_codex_and_routes_claude_without_lowering_hardware() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.openai = vec![pool(1000., 90., 90.), pool(1300., 95., 95.)];
    s.anthropic = quota(1300., 10.);
    let sample = next_sample(&s, Some(2.), 40., 7);
    let mut s = decide_providers(&cfg, s, sample, 1360., 36, 0, 0., 0, 0.);
    assert_eq!(s.hardware_max_running, 37);
    assert_eq!(s.max_running, 10);
    assert_eq!(s.claude_max_running, 37);
    assert_eq!(s.preferred_provider.as_deref(), Some("anthropic"));
    let sample = next_sample(&s, Some(2.), 40., 7);
    s = decide_providers(&cfg, s, sample, 1420., 36, 0, 0., 0, 0.);
    assert_eq!(s.hardware_max_running, 55);
    assert_eq!(s.claude_max_running, 55);
    assert_eq!(s.max_running, 10); // no repeated multiplicative collapse
}
#[test]
fn weekly_or_short_exhaustion_stops_only_the_affected_lane_and_recovers() {
    let cfg = AdaptiveConfig::default();
    for short in [true, false] {
        let mut s = state();
        s.anthropic = quota(1300., 10.);
        for a in &mut s.openai[0].accounts {
            a.used_percent = if short { 30. } else { 100. };
            a.short_exhausted = short;
        }
        let sample = next_sample(&s, Some(2.), 40., 7);
        let mut s = decide_providers(&cfg, s, sample, 1360., 36, 0, 0., 0, 0.);
        assert_eq!(s.max_running, 0);
        assert_eq!(s.claude_max_running, 37);
        s.openai = vec![quota(1420., 0.)];
        let sample = next_sample(&s, Some(2.), 40., 7);
        s = decide_providers(&cfg, s, sample, 1420., 36, 0, 0., 0, 0.);
        assert_eq!(s.max_running, 55);
    }
    let mut s = state();
    s.anthropic = quota(1300., 100.);
    let sample = next_sample(&s, Some(2.), 40., 7);
    let s = decide_providers(&cfg, s, sample, 1360., 36, 0, 0., 0, 0.);
    assert_eq!(s.claude_max_running, 0);
    assert_eq!(s.max_running, 37);
}
#[test]
fn rate_limits_are_independent_and_not_repeatedly_halved() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.claude_max_running = 24;
    s.hardware_max_running = 32;
    s.max_running = 32;
    s.anthropic = quota(1300., 10.);
    let sample = next_sample(&s, Some(2.), 40., 7);
    let mut s = decide_providers(&cfg, s, sample, 1360., 36, 2, 1350., 0, 0.);
    assert_eq!(
        (s.hardware_max_running, s.max_running, s.claude_max_running),
        (48, 16, 48)
    );
    let sample = next_sample(&s, Some(2.), 40., 7);
    s = decide_providers(&cfg, s, sample, 1420., 36, 2, 1350., 2, 1410.);
    assert_eq!(
        (s.hardware_max_running, s.max_running, s.claude_max_running),
        (60, 16, 24)
    );
    let sample = next_sample(&s, Some(2.), 40., 7);
    s = decide_providers(&cfg, s, sample, 1480., 36, 2, 1350., 2, 1410.);
    assert_eq!((s.max_running, s.claude_max_running), (16, 24));
}
#[test]
fn claude_floor_recovers_immediately_and_unknown_or_expired_quota_cannot_brake_growth() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.max_running = 4;
    s.claude_max_running = 4;
    s.hardware_max_running = 16;
    s.openai = vec![quota(1000., 99.)];
    s.anthropic = quota(1000., 99.);
    let sample = next_sample(&s, Some(2.), 40., 7);
    let s = decide_providers(&cfg, s, sample, 1361., 36, 0, 0., 0, 0.);
    assert_eq!(
        (s.hardware_max_running, s.max_running, s.claude_max_running),
        (24, 24, 24)
    );
}
#[test]
fn switching_is_ready_only_after_three_fresh_healthy_resource_samples() {
    let cfg = AdaptiveConfig::default();
    let mut s = state();
    s.hardware_max_running = 32;
    for (now, expected) in [(1360., 1), (1420., 2), (1480., 3)] {
        let sample = next_sample(&s, Some(2.), 40., 80);
        s = decide_providers(&cfg, s, sample, now, 36, 0, 0., 0, 0.);
        assert_eq!(s.auto_ready_samples, expected);
    }
    let sample = next_sample(&s, Some(2.), 15., 80);
    s = decide_providers(&cfg, s, sample, 1540., 36, 0, 0., 0, 0.);
    assert_eq!(s.auto_ready_samples, 0);
}
#[test]
fn cache_supplements_all_accounts_without_private_fields_or_overriding_exclusions() {
    let cache = json!({"quotas":{"__main__":{"weeklyPercent":75.,"weeklyResetAt":100000.,"updatedAt":1290.},
        "reserve":{"weeklyPercent":1.,"weeklyResetAt":200000.,"updatedAt":1290.,"token":"private"}}});
    let cli = json!({"accounts":[{"id":"__main__","quota":{"weeklyPercent":76.,"weeklyResetAt":100000.}}]});
    let q = parse_codex_quotas(Some(&cli), &cache, 1300.);
    assert!(q.complete);
    assert_eq!(q.accounts.len(), 2);
    assert_eq!(q.accounts[0].used_percent, 76.);
    assert!(!serde_json::to_string(&q).unwrap().contains("private"));
    let excluded = json!({"accounts":[{"id":"reserve","paused":true}]});
    assert_eq!(
        parse_codex_quotas(Some(&excluded), &cache, 1300.)
            .accounts
            .len(),
        1
    );
    assert_eq!(parse_codex_quotas(None, &cache, 1300.).accounts.len(), 2);
    assert!(!parse_codex_quotas(None, &cache, 1300.).complete);
    assert!(!parse_codex_quotas(Some(&cli), &Value::Null, 1300.).complete);
    assert!(!parse_codex_quotas(None, &cache, 1800.).complete);
    assert!(!parse_codex_quotas(None, &Value::Null, 1300.).complete);
}
#[test]
fn quota_parser_ignores_private_and_excluded_metadata_and_rejects_partial_windows() {
    let q = parse_quotas(
        &json!({"accounts":[{"id":"a","email":"private","token":"private",
        "quota":{"weeklyPercent":30.,"weeklyResetAt":1792000000000u64}},
        {"id":"b","paused":true},{"id":"c","needsReauth":true}]}),
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
fn cpu_delta_is_average_and_zero_initial_or_rolled_back_counters_are_unknown() {
    assert_eq!(
        cpu_load(Some([100, 200, 100]), Some([180, 300, 100])),
        Some(20.)
    );
    for previous in [None, Some([180, 300, 100]), Some([200, 400, 100])] {
        assert_eq!(cpu_load(previous, Some([180, 300, 100])), None);
    }
}
#[test]
fn repeated_429_events_are_deduplicated_and_survive_success() {
    let mut job = json!({});
    apply_event(
        &mut job,
        &json!({"type":"error","message":"429 Too Many Requests","_ts":"2026-10-08T17:00:00+09:00"}),
    );
    apply_event(
        &mut job,
        &json!({"type":"turn.failed","error":{"message":"429 Too Many Requests"},"_ts":"2026-10-08T17:00:01+09:00"}),
    );
    apply_event(&mut job, &json!({"type":"turn.completed","usage":{}}));
    assert_eq!(job["rate_limit_times"].as_array().unwrap().len(), 1);
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
