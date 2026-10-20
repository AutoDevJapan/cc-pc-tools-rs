//! Fixed-field operational sources. Never acquire process command lines or environments.
use super::{Process, Sample};
use serde::Deserialize;
use serde_json::{json, Value};
use std::{
    fs,
    io::{Read, Seek},
    path::Path,
    process::{Command, Stdio},
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};

pub(super) fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}

pub(super) fn native_sample() -> Result<Sample, &'static str> {
    let host = cc_scheduler::native::resource_totals().map_err(|_| "host_counters_unavailable")?;
    let ps =
        cc_scheduler::native::process_snapshot().map_err(|_| "process_snapshot_unavailable")?;
    let times = host["system_times"]
        .as_array()
        .filter(|a| a.len() == 3)
        .ok_or("host_counters_invalid")?;
    Ok(Sample {
        at_ms: now_ms(),
        tick_ms: host["tick_ms"].as_u64().ok_or("clock_unavailable")?,
        logical_cpus: host["logical_cpus"]
            .as_u64()
            .and_then(|n| u32::try_from(n).ok())
            .filter(|n| *n > 0)
            .ok_or("cpu_count_unavailable")?,
        system_times: [
            times[0].as_u64().ok_or("counter_invalid")?,
            times[1].as_u64().ok_or("counter_invalid")?,
            times[2].as_u64().ok_or("counter_invalid")?,
        ],
        total_bytes: host["total_bytes"].as_u64().ok_or("memory_unavailable")?,
        available_bytes: host["available_bytes"]
            .as_u64()
            .ok_or("memory_unavailable")?,
        processes: ps
            .into_iter()
            .map(|(pid, p)| Process {
                pid,
                parent_pid: p["ppid"].as_u64().unwrap_or(0) as u32,
                created: p["created"].as_u64(),
                name: p["name"].as_str().unwrap_or("").into(),
                cpu_seconds: p["cpu"].as_f64(),
                private_bytes: p["private_bytes"].as_u64(),
                working_set_bytes: p["private_bytes"].as_u64().and_then(|_| p["rss"].as_u64()),
            })
            .collect(),
    })
}

fn plain(path: &Path, directory: bool) -> bool {
    use std::os::windows::fs::MetadataExt;
    fs::symlink_metadata(path).is_ok_and(|m| {
        m.file_attributes() & 0x400 == 0 && if directory { m.is_dir() } else { m.is_file() }
    })
}
fn job_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 128
        && id
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}
#[derive(Deserialize)]
struct Marker {
    job_id: String,
    kind: String,
    pid: u32,
    pid_created: u64,
    since: String,
}
fn markers(jobs_path: &Path, id: &str, budget: &mut usize, now: u64) -> (Vec<Value>, &'static str) {
    let parent = jobs_path.join(id);
    let dir = parent.join("resource-waits");
    if !plain(jobs_path, true) || !plain(&parent, true) {
        return (vec![], "unavailable");
    }
    let entries = match fs::read_dir(&dir) {
        Ok(entries) if plain(&dir, true) => entries,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return (vec![], "ok"),
        _ => return (vec![], "unavailable"),
    };
    let mut out = Vec::new();
    for (index, entry) in entries.enumerate() {
        if index >= 64 || *budget == 0 {
            return (out, "truncated");
        }
        *budget -= 1;
        let Ok(entry) = entry else {
            return (out, "unavailable");
        };
        let leaf = entry.file_name();
        let Some(leaf) = leaf.to_str() else {
            return (out, "invalid");
        };
        let Some(pid) = leaf
            .strip_prefix("cargo-")
            .and_then(|s| s.strip_suffix(".json"))
            .and_then(|s| s.parse::<u32>().ok())
        else {
            return (out, "invalid");
        };
        if !plain(&entry.path(), false) {
            return (out, "unavailable");
        }
        let mut bytes = Vec::new();
        if fs::File::open(entry.path())
            .and_then(|f| f.take(4097).read_to_end(&mut bytes))
            .is_err()
            || bytes.len() > 4096
        {
            return (out, "unavailable");
        }
        let Ok(marker) = serde_json::from_slice::<Marker>(&bytes) else {
            return (out, "invalid");
        };
        if marker.job_id != id
            || marker.pid != pid
            || marker.pid == 0
            || marker.pid_created == 0
            || !matches!(marker.kind.as_str(), "cargo" | "cargo-active")
        {
            return (out, "invalid");
        }
        let Some(since) = chrono::DateTime::parse_from_rfc3339(&marker.since)
            .ok()
            .and_then(|t| u64::try_from(t.timestamp_millis()).ok())
            .filter(|at| *at <= now.saturating_add(5_000))
        else {
            return (out, "invalid");
        };
        out.push(json!({"pid":pid,"created":marker.pid_created.to_string(),"kind":marker.kind,"since":since}));
    }
    (out, "ok")
}

/// Called with the existing Operations collector's already acquired current jobs.
/// Only opaque keys, canonical numeric relationships and process identities survive.
pub fn job_metadata(
    jobs: &[Value],
    jobs_path: &Path,
    now: u64,
    label: impl Fn(&Value) -> Option<String>,
) -> Value {
    let mut out = Vec::new();
    let mut budget = 256;
    let current: Vec<_> = jobs
        .iter()
        .filter(|j| {
            matches!(j["status"].as_str(), Some("running" | "starting"))
                && j["cancel_requested"] != true
        })
        .take(513)
        .collect();
    let truncated = current.len() > 512;
    for job in current.into_iter().take(512) {
        let Some(id) = job["id"].as_str().filter(|id| job_id(id)) else {
            continue;
        };
        let mut roots = Vec::new();
        for key in ["pid", "codex_pid"] {
            if let Some((pid, birth)) = job[key]
                .as_u64()
                .and_then(|n| u32::try_from(n).ok())
                .filter(|n| *n > 0)
                .zip(job[format!("{key}_created")].as_u64().filter(|n| *n > 0))
            {
                roots.push(json!({"pid":pid,"created":birth.to_string()}));
            }
        }
        let (markers, state) = markers(jobs_path, id, &mut budget, now);
        let key = ring::digest::digest(&ring::digest::SHA256, id.as_bytes())
            .as_ref()
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect::<String>();
        out.push(json!({"key":format!("job-{key}"),"todoId":job["todo_id"].as_u64().filter(|n|*n>0),
            "product":label(job).unwrap_or_else(||"作業名は未確認".into()),"roots":roots,"markers":markers,"markerState":state}));
    }
    json!({"observedAt":now,"jobs":out,"truncated":truncated})
}

fn capture(program: &Path, args: &[&str], timeout: Duration) -> Result<String, &'static str> {
    let mut output = tempfile::tempfile().map_err(|_| "capture_failed")?;
    let mut command = Command::new(program);
    command
        .args(args)
        .stdin(Stdio::null())
        .stderr(Stdio::null())
        .stdout(output.try_clone().map_err(|_| "capture_failed")?);
    let started = Instant::now();
    let (mut child, group) =
        cc_ask::ProcessGroup::spawn(&mut command).map_err(|_| "collector_unavailable")?;
    let result = loop {
        if output.metadata().map(|m| m.len()).unwrap_or(u64::MAX) > 65536 {
            break Err("output_limit");
        }
        match child.try_wait() {
            Ok(Some(status)) => {
                break if status.success() {
                    Ok(())
                } else {
                    Err("collector_failed")
                }
            }
            Ok(None) if started.elapsed() < timeout => {
                std::thread::sleep(Duration::from_millis(20))
            }
            _ => break Err("collector_timeout"),
        }
    };
    // Drain and close the complete finite group on every path, including success.
    group.stop(&mut child);
    let _ = child.wait();
    result?;
    output.rewind().map_err(|_| "capture_failed")?;
    let mut bytes = Vec::new();
    output
        .take(65537)
        .read_to_end(&mut bytes)
        .map_err(|_| "capture_failed")?;
    if bytes.len() > 65536 {
        return Err("output_limit");
    }
    String::from_utf8(bytes).map_err(|_| "invalid_output")
}
fn quantity(value: &str) -> Option<u64> {
    let split = value.find(|c: char| !c.is_ascii_digit() && c != '.')?;
    let n = value[..split].parse::<f64>().ok()?;
    let multiplier = match value[split..].trim() {
        "B" => 1.,
        "kB" => 1e3,
        "MB" => 1e6,
        "GB" => 1e9,
        "TB" => 1e12,
        "KiB" => 1024.,
        "MiB" => 1048576.,
        "GiB" => 1073741824.,
        "TiB" => 1099511627776.,
        _ => return None,
    };
    let bytes = n * multiplier;
    (bytes.is_finite() && bytes >= 0. && bytes < u64::MAX as f64).then_some(bytes as u64)
}
pub(super) fn parse_docker(raw: &str, now: u64) -> Result<Value, &'static str> {
    let mut containers = Vec::new();
    for line in raw.lines().filter(|l| !l.trim().is_empty()) {
        if containers.len() >= 128 {
            return Err("container_limit");
        }
        let parts: Vec<_> = line.split('\t').collect();
        if parts.len() != 3
            || ![12, 64].contains(&parts[0].len())
            || !parts[0].bytes().all(|b| b.is_ascii_hexdigit())
        {
            return Err("invalid_output");
        }
        let cpu = parts[1]
            .trim_end_matches('%')
            .parse::<f64>()
            .ok()
            .filter(|n| n.is_finite() && *n >= 0.)
            .ok_or("invalid_cpu")?;
        let (used, limit) = parts[2].split_once(" / ").ok_or("invalid_memory")?;
        containers.push(json!({"id":parts[0],"cpuPercentOfOneCore":cpu,"memoryBytes":quantity(used).ok_or("invalid_memory")?,
            "memoryLimitBytes":quantity(limit).ok_or("invalid_memory")?,"workload":"未確認"}));
    }
    Ok(
        json!({"state":"ready","sampledAt":now,"attemptedAt":now,"currentAvailable":true,"layer":"inside-vm",
        "addToHost":false,"containers":containers,"cpuBasis":"Docker: one core = 100%; separate from host percentage"}),
    )
}
pub(super) fn docker_stats(previous: &Value, now: u64) -> Value {
    match capture(
        Path::new("docker"),
        &[
            "stats",
            "--no-stream",
            "--format",
            "{{.ID}}\t{{.CPUPerc}}\t{{.MemUsage}}",
        ],
        Duration::from_secs(4),
    )
    .and_then(|raw| parse_docker(&raw, now_ms()))
    {
        Ok(v) => v,
        Err(code) => {
            let mut v = if previous.is_object() {
                previous.clone()
            } else {
                json!({"sampledAt":null,"containers":[],"layer":"inside-vm","addToHost":false})
            };
            v["attemptedAt"] = json!(now);
            v["state"] = json!("unavailable");
            v["failureCode"] = json!(code);
            v["currentAvailable"] = json!(false);
            v
        }
    }
}
pub(super) fn expire_docker(value: &mut Value, now: u64) {
    if !value.is_object() {
        return;
    }
    let age = value["sampledAt"]
        .as_u64()
        .and_then(|at| now.checked_sub(at));
    value["ageMs"] = json!(age);
    if age.is_none_or(|ms| ms > 90_000) {
        value["currentAvailable"] = json!(false);
        if value["state"] == "ready" {
            value["state"] = json!("stale");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    #[ignore = "child fixture"]
    fn fixture_capture_large_stdout() {
        use std::io::Write;
        std::io::stdout().write_all(&[b'x'; 131_072]).unwrap();
        std::io::stdout().flush().unwrap();
        std::thread::sleep(Duration::from_secs(30));
    }
    #[test]
    #[ignore = "child fixture"]
    fn fixture_capture_timeout() {
        std::thread::sleep(Duration::from_secs(30));
    }
    #[test]
    fn bounded_capture_stops_timeout_and_large_output_and_recovers_after_failure() {
        let exe = std::env::current_exe().unwrap();
        let fixture = |name: &str| format!("resource_observer::sources::tests::{name}");
        assert_eq!(
            capture(
                &exe,
                &[
                    "--exact",
                    &fixture("fixture_capture_timeout"),
                    "--ignored",
                    "--nocapture"
                ],
                Duration::from_millis(200)
            ),
            Err("collector_timeout")
        );
        assert_eq!(
            capture(
                &exe,
                &[
                    "--exact",
                    &fixture("fixture_capture_large_stdout"),
                    "--ignored",
                    "--nocapture"
                ],
                Duration::from_secs(4)
            ),
            Err("output_limit")
        );
        assert_eq!(
            capture(
                Path::new("cmd.exe"),
                &["/c", "exit 7"],
                Duration::from_secs(2)
            ),
            Err("collector_failed")
        );
        assert!(capture(
            Path::new("cmd.exe"),
            &["/c", "echo captured"],
            Duration::from_secs(2)
        )
        .unwrap()
        .contains("captured"));
    }
    #[test]
    fn marker_budgets_and_malformed_receipts_preserve_incomplete_observation() {
        let temp = tempfile::tempdir().unwrap();
        let now = 1_792_454_400_000u64;
        let mut jobs = Vec::new();
        for n in 0..5 {
            let id = format!("job{n}");
            let dir = temp.path().join(&id).join("resource-waits");
            fs::create_dir_all(&dir).unwrap();
            jobs.push(json!({"id":id,"status":"running","pid":1,"pid_created":1}));
            for pid in 1..=64 {
                fs::write(dir.join(format!("cargo-{pid}.json")),json!({"job_id":id,"kind":"cargo","pid":pid,"pid_created":2,"since":"2026-10-20T00:00:00Z"}).to_string()).unwrap();
            }
        }
        let value = job_metadata(&jobs, temp.path(), now, |_| None);
        assert_eq!(value["jobs"][3]["markers"].as_array().unwrap().len(), 64);
        assert_eq!(value["jobs"][4]["markerState"], "truncated");
        let dir = temp.path().join("job0/resource-waits");
        fs::write(dir.join("cargo-65.json"), "{}").unwrap();
        let value = job_metadata(&jobs[..1], temp.path(), now, |_| None);
        assert_ne!(value["jobs"][0]["markerState"], "ok");
        fs::remove_file(dir.join("cargo-65.json")).unwrap();
        fs::write(dir.join("cargo-1.json"), "{").unwrap();
        assert_eq!(
            job_metadata(&jobs[..1], temp.path(), now, |_| None)["jobs"][0]["markerState"],
            "invalid"
        );
    }
    #[test]
    fn marker_reparse_points_are_rejected_without_following_the_target() {
        let temp = tempfile::tempdir().unwrap();
        let parent = temp.path().join("job1");
        fs::create_dir(&parent).unwrap();
        let actual = temp.path().join("actual");
        fs::create_dir(&actual).unwrap();
        fs::write(actual.join("cargo-1.json"), "must not be read").unwrap();
        let mut command = Command::new("cmd.exe");
        command
            .args(["/c", "mklink", "/J"])
            .arg(parent.join("resource-waits"))
            .arg(&actual)
            .stdout(Stdio::null())
            .stderr(Stdio::null());
        let (mut child, group) = cc_ask::ProcessGroup::spawn(&mut command).unwrap();
        let result = child.wait().unwrap();
        group.completed().unwrap();
        assert!(result.success());
        let value = job_metadata(
            &[json!({"id":"job1","status":"running"})],
            temp.path(),
            u64::MAX,
            |_| None,
        );
        let removed = fs::remove_dir(parent.join("resource-waits"));
        assert_eq!(value["jobs"][0]["markerState"], "unavailable");
        assert!(removed.is_ok());
    }
    #[test]
    fn docker_units_stay_in_their_layer_and_malformed_output_is_not_empty_success() {
        let v = parse_docker("abcdef012345\t125.00%\t1.5GiB / 12GiB\n", 10).unwrap();
        assert_eq!(v["containers"][0]["memoryBytes"], 1610612736u64);
        assert_eq!(v["containers"][0]["cpuPercentOfOneCore"], 125.);
        assert_eq!(v["addToHost"], false);
        assert!(parse_docker("abcdef012345\tNaN%\t1GiB / 2GiB", 10).is_err());
        assert!(parse_docker("customer-name\t1%\t1GiB / 2GiB", 10).is_err());
        assert_eq!(parse_docker("", 10).unwrap()["containers"], json!([]));
    }
    #[test]
    fn marker_metadata_never_copies_job_text_and_rejects_mismatched_receipts() {
        let temp = tempfile::tempdir().unwrap();
        let jobs = temp.path();
        let dir = jobs.join("job1/resource-waits");
        fs::create_dir_all(&dir).unwrap();
        let input = json!([{"id":"job1","status":"running","todo_id":3134,"pid":8,"pid_created":134000000000000001u64,
            "prompt":"secret","cwd":"private-path","last_message":"customer","command":"secret command"}]);
        fs::write(dir.join("cargo-9.json"),br#"{"job_id":"other","kind":"cargo","pid":9,"pid_created":20,"since":"2026-10-20T00:00:00Z"}"#).unwrap();
        let v = job_metadata(input.as_array().unwrap(), jobs, u64::MAX, |_| None);
        assert_eq!(v["jobs"][0]["markerState"], "invalid");
        assert_eq!(v["jobs"][0]["roots"][0]["created"], "134000000000000001");
        let text = v.to_string();
        for secret in ["secret", "private-path", "customer"] {
            assert!(!text.contains(secret));
        }
        fs::write(dir.join("cargo-9.json"),br#"{"job_id":"job1","kind":"cargo","pid":9,"pid_created":20,"since":"2026-10-20T00:00:00Z"}"#).unwrap();
        let v = job_metadata(input.as_array().unwrap(), jobs, u64::MAX, |_| None);
        assert_eq!(v["jobs"][0]["markers"][0]["created"], "20");
        assert_eq!(v["jobs"][0]["markerState"], "ok");
        let started = chrono::DateTime::parse_from_rfc3339("2026-10-20T00:00:00Z")
            .unwrap()
            .timestamp_millis() as u64;
        assert_eq!(
            job_metadata(input.as_array().unwrap(), jobs, started - 1000, |_| None)["jobs"][0]
                ["markerState"],
            "ok"
        );
    }
    #[test]
    fn real_native_sample_has_the_current_process_without_command_or_environment_fields() {
        let sample = native_sample().unwrap();
        assert!(sample.total_bytes >= sample.available_bytes);
        assert!(sample.logical_cpus > 0);
        let own = sample
            .processes
            .iter()
            .find(|p| p.pid == std::process::id())
            .unwrap();
        assert!(own.created.is_some());
        assert!(own.cpu_seconds.is_some());
        assert!(own.private_bytes.is_some());
    }

    #[test]
    fn real_native_two_sample_observation_reports_machine_cpu_and_bounded_processes() {
        let mut history = super::super::History::default();
        let first = native_sample().unwrap();
        history.observe(first, &json!({}));
        std::thread::sleep(Duration::from_millis(300));
        let second = native_sample().unwrap();
        let value = history.observe(second, &json!({}));
        assert!(value["host"]["cpuPercent"]
            .as_f64()
            .is_some_and(|cpu| (0. ..=100.).contains(&cpu)));
        assert!(value["processCount"].as_u64().is_some_and(|n| n > 0));
        assert!(value["processes"].as_array().unwrap().len() <= 128);
        println!(
            "RESOURCE_NATIVE_ACCEPTANCE {}",
            json!({"sampledAt":value["sampledAt"],"host":value["host"],
            "processCount":value["processCount"],"metricsUnavailable":value["metricsUnavailable"],
            "buildCounts":value["buildCounts"],"sampleIntervalMs":value["sampleIntervalMs"]})
        );
    }
}
