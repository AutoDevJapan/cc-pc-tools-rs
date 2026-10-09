use crate::common::*;
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet};
pub type Processes = BTreeMap<u32, Value>;
pub const GIB: f64 = 1073741824.;
pub const FILETIME_EPOCH: u64 = 116444736000000000;
pub fn defaults() -> Value {
    json!({"reserve_gb":16,"commit_limit_percent":85,"cpu_limit_percent":80,"disk_min_gb":30,"hard_cap":null,"launch_interval_s":10,"cpu_window_s":180,"footprint_window_s":3600,"footprint_min_gb":2,"footprint_margin":1.25,"hang_s":1800,"orphan_age_s":600,"dispatch_enabled":true,"kill_enabled":false})
}
pub fn live(j: &Value) -> bool {
    matches!(s(&j["status"]), "queued" | "starting" | "running")
}
pub fn identity(p: &Value) -> Option<String> {
    p["created"].as_u64().map(|c| format!("{}:{}", p["pid"], c))
}
pub fn matches(j: &Value, k: &str, ps: &Processes) -> bool {
    j[k].as_u64()
        .and_then(|p| ps.get(&(p as u32)))
        .is_some_and(|p| !p["created"].is_null() && p["created"] == j[format!("{k}_created")])
}
pub fn alive(j: &Value, ps: &Processes, now: f64) -> bool {
    if s(&j["status"]) == "queued"
        && j["cancel_requested"] != true
        && j["queue_restart_pending"] == true
        && j["reboot_launch_pending"] == true
    {
        return true; // Durable reboot launch reservation; the minute tick retries it.
    }
    for k in ["pid", "codex_pid"] {
        if let Some(p) = j[k].as_u64().and_then(|p| ps.get(&(p as u32))) {
            if p["created"].is_null() || matches(j, k, ps) {
                return true;
            }
        }
    }
    live(j) && now - epoch(&j["created_at"]) < 60.
}
pub fn process_tree(j: &Value, ps: &Processes) -> BTreeSet<u32> {
    let mut owned: BTreeSet<u32> = ["pid", "codex_pid"]
        .into_iter()
        .filter(|k| matches(j, k, ps))
        .filter_map(|k| j[k].as_u64().map(|x| x as u32))
        .collect();
    loop {
        let old = owned.len();
        for (pid, p) in ps {
            let pp = n(&p["ppid"]) as u32;
            if owned.contains(&pp)
                && p["created"]
                    .as_u64()
                    .zip(ps.get(&pp).and_then(|v| v["created"].as_u64()))
                    .is_some_and(|(c, pc)| c >= pc)
            {
                owned.insert(*pid);
            }
        }
        if old == owned.len() {
            return owned;
        }
    }
}
pub fn protected(ps: &Processes) -> BTreeSet<u32> {
    let names = [
        "civilizationvi",
        "docker",
        "com.docker",
        "vmmem",
        "wsl",
        "opencodex",
        "bun",
    ];
    let mut roots: BTreeSet<u32> = ps
        .iter()
        .filter(|(_, p)| {
            names
                .iter()
                .any(|prefix| s(&p["name"]).to_lowercase().starts_with(prefix))
        })
        .map(|(pid, _)| *pid)
        .collect();
    loop {
        let old = roots.len();
        for (pid, p) in ps {
            let pp = n(&p["ppid"]) as u32;
            if roots.contains(&pp)
                && p["created"]
                    .as_u64()
                    .zip(ps.get(&pp).and_then(|v| v["created"].as_u64()))
                    .is_some_and(|(c, pc)| c >= pc)
            {
                roots.insert(*pid);
            }
        }
        if old == roots.len() {
            return roots;
        }
    }
}
/// Only birth-verified job ancestry. Persist it so dead parents do not lose provenance.
pub fn ownership(jobs: &[Value], ps: &Processes, previous: &Value) -> Value {
    let mut current = json!({});
    for p in ps.values() {
        if let Some(key) = identity(p) {
            if let Some(v) = previous.get(&key) {
                current[key] = v.clone();
            }
        }
    }
    for j in jobs {
        for pid in process_tree(j, ps) {
            current[identity(&ps[&pid]).unwrap()] =
                json!({"job":j["id"],"proof":"birth-verified job ancestry"});
        }
    }
    loop {
        let mut changed = false;
        for p in ps.values() {
            let pp = n(&p["ppid"]) as u32;
            if let (Some(key), Some(parent)) = (identity(p), ps.get(&pp)) {
                if let Some(pk) = identity(parent) {
                    if current.get(&key).is_none()
                        && current.get(&pk).is_some()
                        && p["created"].as_u64() >= parent["created"].as_u64()
                    {
                        current[key] = current[pk].clone();
                        changed = true;
                    }
                }
            }
        }
        if !changed {
            return current;
        }
    }
}
pub fn orphan_candidates(
    jobs: &[Value],
    ps: &Processes,
    ledger: &Value,
    now: f64,
    minimum_age: f64,
) -> Vec<Value> {
    let mut exclude = protected(ps);
    for j in jobs {
        if live(j) && alive(j, ps, now) {
            exclude.extend(process_tree(j, ps));
        }
    }
    let mut out = vec![];
    for (pid, p) in ps {
        let Some(key) = identity(p) else {
            continue;
        };
        let Some(link) = ledger.get(key) else {
            continue;
        };
        let name = s(&p["name"]).to_lowercase();
        if exclude.contains(pid)
            || !(matches!(
                name.as_str(),
                "node.exe" | "node" | "codex.exe" | "codex" | "mcp.exe"
            ) || name.starts_with("mcp-"))
        {
            continue;
        }
        let Some(j) = jobs.iter().find(|j| j["id"] == link["job"]) else {
            continue;
        };
        if live(j) && alive(j, ps, now) {
            continue;
        }
        let age =
            now - (p["created"].as_u64().unwrap() as i128 - FILETIME_EPOCH as i128) as f64 / 1e7;
        if age >= minimum_age {
            out.push(json!({"pid":pid,"created":p["created"],"name":p["name"],"age_minutes":round(age/60.,1),"rss_gb":round(n(&p["rss"])/GIB,3),"job":j["id"],"proof":link["proof"]}));
        }
    }
    out
}
pub fn round(x: f64, d: i32) -> f64 {
    let power = 10f64.powi(d);
    (x * power).round_ties_even() / power
}
pub fn p90(values: &[f64]) -> f64 {
    let mut v = values.to_vec();
    v.sort_by(f64::total_cmp);
    if v.is_empty() {
        0.
    } else {
        v[((0.9 * v.len() as f64).ceil() as usize).saturating_sub(1)]
    }
}
pub fn priority(item: &Value, priorities: &Value) -> f64 {
    priorities.get(s(&item["project"])).map(n).unwrap_or(3.)
}
pub fn priority_queue(items: &[Value], priorities: &Value, now: f64) -> Vec<Value> {
    let mut q: Vec<Value> = items
        .iter()
        .filter(|i| {
            s(&i["status"]) == "todo"
                && truth(&i["dispatchable"])
                && truth(&i["spec"])
                && !truth(&i["scheduler"]["question_close_pending"])
                && n(&i["scheduler"]["not_before"]) <= now
        })
        .cloned()
        .collect();
    q.sort_by(|a, b| {
        priority(a, priorities)
            .total_cmp(&priority(b, priorities))
            .then_with(|| {
                n(a.get("created_s").unwrap_or(&a["id"]))
                    .total_cmp(&n(b.get("created_s").unwrap_or(&b["id"])))
            })
            .then_with(|| n(&a["id"]).total_cmp(&n(&b["id"])))
    });
    q
}
pub fn resources_busy(
    items: &[Value],
    jobs: &[Value],
    ps: &Processes,
    now: f64,
) -> BTreeSet<String> {
    let active: BTreeSet<&str> = jobs
        .iter()
        .filter(|j| live(j) && alive(j, ps, now))
        .map(|j| s(&j["id"]))
        .collect();
    let mut busy = BTreeSet::new();
    for i in items {
        if active.contains(s(&i["scheduler"]["job"]))
            || arr(&i["jobs"]).iter().any(|j| active.contains(s(j)))
        {
            busy.extend(arr(&i["needs"]).iter().map(|x| s(x).to_owned()));
        }
    }
    for j in jobs {
        if active.contains(s(&j["id"])) {
            busy.extend(arr(&j["needs"]).iter().map(|x| s(x).to_owned()));
        }
    }
    if ps
        .values()
        .any(|p| s(&p["name"]).to_lowercase().starts_with("civilizationvi"))
    {
        busy.insert("civ6".into());
    }
    if ps.values().any(|p| {
        matches!(
            s(&p["name"]).to_lowercase().as_str(),
            "cargo.exe" | "rustc.exe" | "cargo" | "rustc"
        )
    }) {
        busy.insert("cargo".into());
    }
    busy
}

/// A revoked allocation no longer reserves Civ6; a replacement lock does.
pub fn resources_busy_with_lock(
    items: &[Value],
    jobs: &[Value],
    ps: &Processes,
    now: f64,
    home: &std::path::Path,
) -> Result<BTreeSet<String>> {
    let lock = read_json(&home.join("civ6.lock"), Value::Null)?;
    let revoked = read_json(&home.join("civ6-revoked.json"), Value::Null)?;
    let owner = &revoked["owner"];
    let revoked_job = owner["owner_job"]
        .as_str()
        .or_else(|| owner["job"].as_str());
    let current_job = lock["owner_job"].as_str().or_else(|| lock["job"].as_str());
    let is_revoked = |id: &Value| {
        !revoked.is_null()
            && id.as_str().is_some()
            && id.as_str() == revoked_job
            && (lock.is_null() || current_job != revoked_job)
    };
    let strip = |mut v: Value| {
        if let Some(a) = v["needs"].as_array_mut() {
            a.retain(|x| x != "civ6");
        }
        v
    };
    let active: BTreeSet<&str> = jobs
        .iter()
        .filter(|j| live(j) && alive(j, ps, now))
        .map(|j| s(&j["id"]))
        .collect();
    let items: Vec<Value> = items
        .iter()
        .map(|i| {
            let mut i = i.clone();
            if !revoked.is_null() {
                let mut claims = arr(&i["jobs"]);
                claims.push(i["scheduler"]["job"].clone());
                claims.retain(|j| active.contains(s(j)));
                let all_revoked = !claims.is_empty() && claims.iter().all(&is_revoked);
                // A current assigned job retains its reservation even while
                // blocked. Historical continuations of a finished allocation
                // do not acquire Civ6 merely by appearing in its jobs list.
                let ended = s(&i["scheduler"]["phase"]) == "finished"
                    && !active.contains(s(&i["scheduler"]["job"]));
                if is_revoked(&i["scheduler"]["job"]) || all_revoked || ended {
                    i = strip(i);
                }
            }
            i
        })
        .collect();
    let jobs: Vec<Value> = jobs
        .iter()
        .map(|j| {
            if is_revoked(&j["id"]) {
                strip(j.clone())
            } else {
                j.clone()
            }
        })
        .collect();
    let mut processes = ps.clone();
    if lock.is_null() && !revoked.is_null() {
        // Recent scenario steps can leave the game alive after revocation.
        processes.retain(|_, p| !s(&p["name"]).to_lowercase().starts_with("civilizationvi"));
    }
    let mut busy = resources_busy(&items, &jobs, &processes, now);
    if !lock.is_null() {
        busy.insert("civ6".into());
    }
    // A ToDo's `needs: cargo` describes future work, not a build lease for the
    // whole model session. The installed gate owns target and machine-slot locks.
    // Older installations retain the conservative, process-based policy.
    let gate_home = home.parent().unwrap_or(home).join("codex-jobs");
    if home.file_name().is_some_and(|name| name == "civ6-live")
        && gate_home.join("cargo_gate.exe").is_file()
    {
        let mut free_slots = Vec::new();
        let mut probe_failed = false;
        let dynamic = gate_home.join(cc_job_policy::cargo::CONFIG_FILE).exists()
            || gate_home.join(cc_job_policy::cargo::STATE_FILE).exists();
        let ceiling = if dynamic {
            match cc_job_policy::cargo::configured_ceiling(
                &gate_home,
                std::thread::available_parallelism().map_or(1, |n| n.get()),
            ) {
                Ok(ceiling) => ceiling,
                Err(_) => {
                    probe_failed = true;
                    0
                }
            }
        } else {
            3
        }; // Compatibility with installations that still run the old gate.
        for slot in 0..ceiling {
            match ByteLock::acquire(
                &gate_home.join(format!("cargo-build.lock.{slot}")),
                std::time::Duration::ZERO,
            ) {
                Ok(Some(guard)) => free_slots.push(guard),
                Ok(None) => (),
                // Only Cargo admission closes; reconciliation and unrelated
                // work must still run when a slot file cannot be inspected.
                Err(_) => probe_failed = true,
            }
        }
        let unmanaged = processes.values().any(|p| {
            matches!(
                s(&p["name"]).to_ascii_lowercase().as_str(),
                "cargo.exe" | "rustc.exe" | "cargo" | "rustc"
            ) && !cargo_gate_ancestor(p, &processes)
        });
        if free_slots.is_empty() || unmanaged || probe_failed {
            busy.insert("cargo".into());
        } else {
            busy.remove("cargo");
        }
        // This is an admission observation, never a reservation. The build gate
        // rechecks and acquires its own locks when the command actually runs.
    }
    Ok(busy)
}

fn cargo_gate_ancestor(process: &Value, processes: &Processes) -> bool {
    let mut current = process;
    let mut seen = BTreeSet::new();
    while seen.insert(n(&current["pid"]) as u32) {
        if matches!(
            s(&current["name"]).to_ascii_lowercase().as_str(),
            "cargo_gate.exe" | "cargo_gate"
        ) {
            return true;
        }
        let Some(parent) = processes.get(&(n(&current["ppid"]) as u32)) else {
            return false;
        };
        if current["created"]
            .as_u64()
            .zip(parent["created"].as_u64())
            .is_none_or(|(child, parent)| child < parent)
        {
            return false;
        }
        current = parent;
    }
    false
}
pub fn pressure_victim(
    items: &[Value],
    jobs: &[Value],
    ps: &Processes,
    priorities: &Value,
    now: f64,
) -> Option<Value> {
    let active: BTreeSet<&str> = jobs
        .iter()
        .filter(|j| live(j) && alive(j, ps, now))
        .map(|j| s(&j["id"]))
        .collect();
    items
        .iter()
        .filter(|i| s(&i["status"]) == "doing" && active.contains(s(&i["scheduler"]["job"])))
        .max_by(|a, b| {
            priority(a, priorities)
                .total_cmp(&priority(b, priorities))
                .then_with(|| {
                    n(&a["scheduler"]["started_s"]).total_cmp(&n(&b["scheduler"]["started_s"]))
                })
                .then_with(|| n(&a["id"]).total_cmp(&n(&b["id"])))
        })
        .cloned()
}
pub fn hang_progress(
    prev: &Value,
    event: f64,
    cpu: Option<f64>,
    tree: &str,
    now: f64,
    timeout: f64,
) -> (Value, bool) {
    let changed = !truth(prev)
        || prev["cpu"].is_null()
        || n(&prev["event"]) != event
        || cpu.is_none_or(|c| (n(&prev["cpu"]) - c).abs() > 0.000001)
        || s(&prev["tree"]) != tree;
    let since = if changed { now } else { n(&prev["since"]) };
    (
        json!({"event":event,"cpu":cpu,"tree":tree,"since":since}),
        cpu.is_some() && now - since >= timeout,
    )
}
pub fn cpu_delta(prev: &[u64], current: &[u64]) -> f64 {
    let idle = (current[0] as i128 - prev[0] as i128) as f64;
    let total =
        ((current[1] as i128 - prev[1] as i128) + (current[2] as i128 - prev[2] as i128)) as f64;
    if total > 0. {
        ((total - idle) * 100. / total).clamp(0., 100.)
    } else {
        100.
    }
}
pub fn capacity(
    m: &Value,
    cfg: &Value,
    state: &mut Value,
    jobs: &[Value],
    ps: &Processes,
    now: f64,
) -> Value {
    let active: Vec<&Value> = jobs
        .iter()
        .filter(|j| live(j) && alive(j, ps, now))
        .collect();
    let mut history: Vec<Value> = arr(&state["cpu"])
        .into_iter()
        .filter(|p| now - n(&p["at"]) <= n(&cfg["cpu_window_s"]))
        .collect();
    if history.last().is_none_or(|p| now - n(&p["at"]) >= 15.) {
        history.push(json!({"at":now,"value":m["cpu_percent"]}));
    }
    state["cpu"] = json!(&history[history.len().saturating_sub(300)..]);
    let avg = history.iter().map(|p| n(&p["value"])).sum::<f64>() / history.len() as f64;
    let mut samples: Vec<Value> = arr(&state["footprints"])
        .into_iter()
        .filter(|p| now - n(&p["at"]) <= n(&cfg["footprint_window_s"]))
        .collect();
    let mut footprints = json!({});
    for j in &active {
        let tree = process_tree(j, ps);
        let size = tree.iter().map(|p| n(&ps[p]["rss"])).sum::<f64>() / GIB;
        footprints[s(&j["id"])] = json!(round(size, 3));
        if !tree.is_empty() && tree.iter().all(|p| truth(&ps[p]["metrics_ok"])) {
            samples.push(json!({"at":now,"gb":size}));
        }
    }
    state["footprints"] = json!(&samples[samples.len().saturating_sub(2000)..]);
    let measured = p90(&samples.iter().map(|p| n(&p["gb"])).collect::<Vec<_>>());
    let expected = n(&cfg["footprint_min_gb"]).max(measured * n(&cfg["footprint_margin"]));
    let automatic_cap = ((n(&m["ram_total_gb"]) - n(&cfg["reserve_gb"])) / expected)
        .floor()
        .max(1.)
        .min((n(&m["logical_cores"]).max(1.) * 2.).floor()) as i64;
    let cap = if truth(&cfg["hard_cap"]) {
        n(&cfg["hard_cap"]) as i64
    } else {
        automatic_cap
    };
    let memory_slots =
        (((n(&m["available_gb"]) - n(&cfg["reserve_gb"])) / expected).ceil() - 1.).max(0.) as i64;
    // Reserve one logical core's share for each additional agent, including
    // I/O-bound agents. Recheck after every launch so bursts cannot consume the reserve.
    let cpu_load = avg.max(n(&m["cpu_percent"]));
    let expected_cpu = 100. / n(&m["logical_cores"]).max(1.);
    let cpu_slots = ((n(&cfg["cpu_limit_percent"]) - cpu_load) / expected_cpu)
        .floor()
        .max(0.) as i64;
    let slots = memory_slots
        .min(cpu_slots)
        .min(cap - active.len() as i64)
        .max(0);
    let last = jobs
        .iter()
        .map(|j| epoch(&j["created_at"]))
        .fold(n(&state["last_launch"]), f64::max);
    let cooldown = (n(&cfg["launch_interval_s"]) - (now - last)).max(0.);
    let mut reasons = vec![];
    if n(&m["available_gb"]) < 8. {
        reasons.push("RAM below 8 GiB");
    }
    if n(&m["available_gb"]) - expected <= n(&cfg["reserve_gb"]) {
        reasons.push("RAM reserve");
    }
    if n(&m["commit_percent"]) >= n(&cfg["commit_limit_percent"]) {
        reasons.push("commit limit");
    }
    if cpu_load >= n(&cfg["cpu_limit_percent"]) {
        reasons.push("CPU rolling limit");
    }
    if !truth(&m["disk_free_gb"])
        || m["disk_free_gb"]
            .as_object()
            .unwrap()
            .values()
            .any(|x| n(x) <= n(&cfg["disk_min_gb"]))
    {
        reasons.push("disk reserve");
    }
    if active.len() as i64 >= cap {
        reasons.push("hard cap");
    }
    let slots = if reasons.is_empty() { slots } else { 0 };
    if cooldown != 0. {
        reasons.push("launch spacing");
    }
    let mut r = m.clone();
    merge(
        &mut r,
        json!({"measured_at":iso(now),"cpu_rolling_percent":round(avg,2),"cpu_samples":history.len(),"job_p90_gb":round(measured,3),"expected_job_gb":round(expected,3),"expected_job_cpu_percent":round(expected_cpu,2),"cpu_slots":cpu_slots,"memory_slots":memory_slots,"reserve_gb":cfg["reserve_gb"],"running_jobs":active.len(),"hard_cap":cap,"resource_slots":slots,"allowance_now":i32::from(slots>0&&cooldown==0.),"launch_wait_s":round(cooldown,1),"reasons":reasons,"job_footprints_gb":footprints,"node_count":node_count(ps),"kill_enabled":cfg["kill_enabled"],"dispatch_enabled":cfg["dispatch_enabled"]}),
    );
    r
}
/// Preserve the original conversation only for unfinished successful work in the
/// same folder, and never compete with another worker using that conversation.
pub fn continuation_session<'a>(job: &'a Value, cwd: &str, jobs: &[Value]) -> Option<&'a str> {
    let session = s(&job["thread_id"]);
    let norm = |p: &str| p.replace('\\', "/").trim_end_matches('/').to_lowercase();
    (s(&job["status"]) == "succeeded"
        && cc_ask::core::result_state(s(&job["last_message"])) == "状態: 継続中"
        && uuid::Uuid::parse_str(session).is_ok()
        && norm(s(&job["cwd"])) == norm(cwd)
        && !jobs.iter().any(|j| {
            live(j) && (s(&j["thread_id"]) == session || s(&j["resume_session"]) == session)
        }))
    .then_some(session)
}
pub fn valid_session(session: &str) -> bool {
    session.len() == 36 && uuid::Uuid::parse_str(session).is_ok()
}
pub fn node_count(ps: &Processes) -> usize {
    ps.values()
        .filter(|p| matches!(s(&p["name"]).to_lowercase().as_str(), "node" | "node.exe"))
        .count()
}
