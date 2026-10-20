use localai_web_rs::resource_observer::{expire, History, Process, Sample, MAX_PROCESSES};
use serde_json::{json, Value};

fn process(pid: u32, parent: u32, birth: u64, cpu: f64, memory: u64, name: &str) -> Process {
    Process {
        pid,
        parent_pid: parent,
        created: Some(birth),
        cpu_seconds: Some(cpu),
        private_bytes: Some(memory),
        working_set_bytes: Some(memory * 2),
        name: name.into(),
    }
}
fn sample(tick: u64, processes: Vec<Process>) -> Sample {
    Sample {
        at_ms: 1_800_000_000_000 + tick,
        tick_ms: tick,
        logical_cpus: 32,
        system_times: [tick * 10_000 * 16, tick * 10_000 * 24, tick * 10_000 * 8],
        total_bytes: 96 << 30,
        available_bytes: 48 << 30,
        processes,
    }
}
fn row(v: &Value, pid: u32) -> &Value {
    v["processes"]
        .as_array()
        .unwrap()
        .iter()
        .find(|p| p["pid"] == pid)
        .unwrap()
}

#[test]
fn cpu_and_memory_deltas_have_real_windows_and_machine_cpu_units() {
    let mut h = History::default();
    let first = h.observe(
        sample(1_000, vec![process(42, 1, 123, 1., 100, "cargo.exe")]),
        &json!({}),
    );
    assert_eq!(first["state"], "collecting");
    assert!(row(&first, 42)["cpuPercent"].is_null());
    let second = h.observe(
        sample(11_000, vec![process(42, 1, 123, 17., 300, "cargo.exe")]),
        &json!({}),
    );
    assert_eq!(row(&second, 42)["cpuPercent"], 5.);
    assert_eq!(row(&second, 42)["privateDeltaBytes"], 200);
    assert_eq!(row(&second, 42)["growthWindowMs"], 10_000);
    assert_eq!(second["host"]["cpuPercent"], 50.);
    assert_eq!(second["buildCounts"]["processing"], 1);
}

#[test]
fn pid_reuse_denial_and_reused_parent_do_not_fabricate_deltas_or_job_links() {
    let mut h = History::default();
    h.observe(
        sample(1_000, vec![process(42, 1, 123, 1., 100, "cargo.exe")]),
        &json!({}),
    );
    let mut denied = process(7, 0, 80, 1., 100, "private-customer.exe");
    denied.created = None;
    denied.private_bytes = None;
    denied.cpu_seconds = None;
    let jobs = json!({"observedAt":1_800_000_011_000u64,"jobs":[{"key":"job-a","todoId":3,"product":"Local AI Console","roots":[{"pid":1,"created":"999"}],"markers":[]}]});
    let v = h.observe(
        sample(
            11_000,
            vec![
                process(1, 0, 999, 1., 1, "codex.exe"),
                process(42, 1, 456, 9., 900, "cargo.exe"),
                denied,
            ],
        ),
        &jobs,
    );
    assert!(row(&v, 42)["cpuPercent"].is_null());
    assert!(row(&v, 42)["privateDeltaBytes"].is_null());
    assert!(row(&v, 42)["parentPid"].is_null());
    assert!(v["builds"][0]["job"].is_null());
    assert!(row(&v, 7)["privateBytes"].is_null());
    assert!(!v.to_string().contains("private-customer"));
}

#[test]
fn alive_cargo_is_unverified_and_only_matching_current_markers_prove_wait_or_execution() {
    let mut h = History::default();
    let ps = vec![
        process(1, 0, 10, 0., 10, "codex_jobs.exe"),
        process(42, 1, 20, 0., 100, "cargo.exe"),
    ];
    let alive = h.observe(sample(1_000, ps.clone()), &json!({}));
    assert_eq!(alive["builds"][0]["state"], "unverified");
    let mut jobs = json!({"observedAt":1_800_000_011_000u64,"jobs":[{"key":"job-a","todoId":3134,"product":"Local AI Console","roots":[{"pid":1,"created":"10"}],"markerState":"ok","markers":[{"pid":42,"created":"20","kind":"cargo","since":1_800_000_001_000u64}]}]});
    let waiting = h.observe(sample(11_000, ps.clone()), &jobs);
    assert_eq!(waiting["builds"][0]["state"], "waiting");
    assert_eq!(waiting["builds"][0]["job"]["todoId"], 3134);
    jobs["jobs"][0]["markers"][0]["kind"] = json!("cargo-active");
    let running = h.observe(sample(21_000, ps.clone()), &jobs);
    assert_eq!(running["builds"][0]["state"], "executing");
    jobs["jobs"][0]["markers"][0]["created"] = json!("21");
    let reused = h.observe(sample(31_000, ps), &jobs);
    assert_eq!(reused["builds"][0]["state"], "unverified");
}

#[test]
fn history_is_bounded_and_memory_accounting_does_not_add_vm_and_containers() {
    let mut h = History::default();
    let mut v = Value::Null;
    for n in 1..=60 {
        v = h.observe(
            sample(n * 15_000, vec![process(42, 0, 1, 0., n * 100, "vmmemWSL")]),
            &json!({}),
        );
    }
    assert_eq!(v["history"].as_array().unwrap().len(), 41);
    assert_eq!(row(&v, 42)["growthWindowMs"], 600_000);
    assert_eq!(row(&v, 42)["growthBytes"], 4_000);
    assert_eq!(v["host"]["usedPhysicalBytes"], 48u64 << 30);
    assert_eq!(row(&v, 42)["layer"], "vm");
    assert!(v["privateBytesTotal"].is_null());
}

#[test]
fn cargo_gate_and_children_form_one_build_and_sccache_alone_is_not_a_build() {
    let mut h = History::default();
    let ps = vec![
        process(1, 0, 10, 0., 10, "codex_jobs.exe"),
        process(42, 1, 20, 0., 10, "cargo_gate.exe"),
        process(43, 42, 21, 0., 100, "cargo.exe"),
        process(44, 43, 22, 0., 100, "rustc.exe"),
        process(9, 0, 5, 0., 100, "sccache.exe"),
    ];
    let jobs = json!({"observedAt":1_800_000_001_000u64,"jobs":[{"key":"job-a","todoId":3134,"product":"Local AI Console",
        "roots":[{"pid":1,"created":"10"}],"markerState":"ok","markers":[{"pid":42,"created":"20","kind":"cargo","since":1_800_000_001_000u64}]}]});
    let value = h.observe(sample(1_000, ps), &jobs);
    assert_eq!(value["buildCounts"]["observed"], 1);
    assert_eq!(value["buildCounts"]["waiting"], 1);
    assert_eq!(value["builds"][0]["processCount"], 3);
}

#[test]
fn stale_metadata_and_stale_cache_do_not_claim_current_cpu_or_work() {
    let mut h = History::default();
    let ps = vec![
        process(1, 0, 10, 0., 10, "codex_jobs.exe"),
        process(42, 1, 20, 0., 100, "cargo.exe"),
    ];
    let old = json!({"observedAt":1_800_000_000_000u64,"jobs":[{"key":"job-a","todoId":3134,"product":"Local AI Console",
        "roots":[{"pid":1,"created":"10"}],"markerState":"ok","markers":[{"pid":42,"created":"20","kind":"cargo-active","since":1_800_000_000_000u64}]}]});
    let mut value = h.observe(sample(60_000, ps), &old);
    assert!(value["builds"][0]["job"].is_null());
    assert_eq!(value["builds"][0]["state"], "unverified");
    expire(&mut value, 1_800_000_106_000u64);
    assert_eq!(value["state"], "stale");
    assert_eq!(value["currentAvailable"], false);
    assert!(value["host"]["cpuPercent"].is_null());
    assert!(value["buildCounts"]["executing"].is_null());
    assert_eq!(value["sampledAt"], 1_800_000_060_000u64);
}

#[test]
fn excessive_processes_and_reset_counters_remain_explicitly_partial_or_unknown() {
    let mut h = History::default();
    h.observe(
        sample(1_000, vec![process(1, 0, 1, 10., 100, "rustc.exe")]),
        &json!({}),
    );
    let value = h.observe(
        sample(11_000, vec![process(1, 0, 1, 1., 100, "rustc.exe")]),
        &json!({}),
    );
    assert!(row(&value, 1)["cpuPercent"].is_null());
    let ps = (1..=(MAX_PROCESSES + 2))
        .map(|n| process(n as u32, 0, 1, 0., 100, "chrome.exe"))
        .collect();
    let value = h.observe(sample(21_000, ps), &json!({}));
    assert_eq!(value["state"], "partial");
    assert_eq!(value["processesTruncated"], true);
    assert_eq!(value["processCount"], MAX_PROCESSES);
    assert!(value["processes"].as_array().unwrap().len() <= 128);
}

#[test]
fn build_totals_cover_all_observed_roots_when_the_detail_table_is_truncated() {
    let mut h = History::default();
    let ps = (1..=70)
        .map(|pid| process(pid, 0, 1, 0., 100, "cargo.exe"))
        .collect();
    let value = h.observe(sample(1_000, ps), &json!({}));
    assert_eq!(value["buildCounts"]["observed"], 70);
    assert_eq!(value["buildCounts"]["unverified"], 70);
    assert_eq!(value["buildCounts"]["truncated"], true);
    assert_eq!(value["builds"].as_array().unwrap().len(), 64);
}

#[test]
fn slow_memory_growth_is_selected_even_without_a_large_last_interval_delta() {
    let mut h = History::default();
    let ps = |ordinary: u64, slow: u64| {
        let mut rows: Vec<_> = (1..=200)
            .map(|pid| process(pid, 0, 1, 0., ordinary + u64::from(pid), "chrome.exe"))
            .collect();
        rows.push(process(999, 0, 1, 0., slow, "node.exe"));
        rows
    };
    h.observe(sample(1_000, ps(10_000, 1)), &json!({}));
    h.observe(sample(16_000, ps(10_000, 501)), &json!({}));
    let value = h.observe(sample(31_000, ps(10_001, 501)), &json!({}));
    assert_eq!(row(&value, 999)["privateDeltaBytes"], 0);
    assert_eq!(row(&value, 999)["growthBytes"], 500);
    assert_eq!(row(&value, 999)["growthWindowMs"], 30_000);
}

#[test]
fn host_cpu_is_unknown_when_windows_system_timer_cannot_cover_all_processor_groups() {
    let mut h = History::default();
    let mut first = sample(1_000, vec![]);
    first.logical_cpus = 128;
    h.observe(first, &json!({}));
    let mut second = sample(11_000, vec![]);
    second.logical_cpus = 128;
    assert!(h.observe(second, &json!({}))["host"]["cpuPercent"].is_null());
}
