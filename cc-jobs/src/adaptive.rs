//! Shared, bounded sampling. No running process is ever stopped by this controller.
use crate::{queue::QueueConfig, *};
use serde::{Deserialize, Serialize};
use std::{
    collections::HashSet,
    fs,
    io::{Read, Write},
    net::{Ipv4Addr, SocketAddr, TcpStream},
    time::{Duration, Instant},
};

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(default, deny_unknown_fields)]
pub struct AdaptiveConfig {
    pub min_running: usize,
    pub max_running: usize,
    pub increase_step: usize,
    pub interval_seconds: u64,
    pub quiet_seconds: u64,
    pub health_port: u16,
    pub health_fast_ms: u64,
    pub health_timeout_ms: u64,
    pub free_memory_gib: f64,
    pub cpu_percent: f64,
    pub quota_interval_seconds: u64,
    pub quota_timeout_seconds: u64,
}
impl Default for AdaptiveConfig {
    fn default() -> Self {
        Self {
            min_running: 4,
            max_running: 60,
            increase_step: 2,
            interval_seconds: 60,
            quiet_seconds: 300,
            health_port: 10100,
            health_fast_ms: 1000,
            health_timeout_ms: 2000,
            free_memory_gib: 16.,
            cpu_percent: 85.,
            quota_interval_seconds: 300,
            quota_timeout_seconds: 15,
        }
    }
}
impl AdaptiveConfig {
    pub fn stale_after_seconds(&self) -> f64 {
        (2 * self.interval_seconds
            + 2 * self.quota_timeout_seconds
            + self.health_timeout_ms.div_ceil(1000)
            + 10) as f64
    }
    pub fn validate(&self) -> Result<()> {
        if self.min_running == 0
            || self.max_running < self.min_running
            || self.increase_step == 0
            || self.interval_seconds < 10
            || self.quiet_seconds < self.interval_seconds
            || self.quiet_seconds > 3600
            || self.health_port == 0
            || self.health_fast_ms == 0
            || self.health_timeout_ms < self.health_fast_ms
            || self.health_timeout_ms > 10_000
            || !(1..=20).contains(&self.quota_timeout_seconds)
            || self.quota_interval_seconds < self.interval_seconds
            || !self.free_memory_gib.is_finite()
            || self.free_memory_gib <= 0.
            || !self.cpu_percent.is_finite()
            || !(1. ..=100.).contains(&self.cpu_percent)
        {
            return Err(Error::new("invalid adaptive queue configuration"));
        }
        Ok(())
    }
}
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
pub struct MachineSample {
    pub free_memory_gib: Option<f64>,
    pub cpu_times: Option<[u64; 3]>, // idle, kernel (including idle), user
    pub health_ms: Option<f64>,
}
#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct AccountQuota {
    pub id: String,
    pub used_percent: f64,
    pub reset_at: f64,
    pub short_exhausted: bool,
}
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
pub struct QuotaSnapshot {
    pub at: f64,
    pub accounts: Vec<AccountQuota>,
    pub complete: bool,
}
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct LimitState {
    pub checked_at: f64,
    pub first_at: f64,
    pub stable_since: f64,
    pub max_running: usize,
    pub reason: String,
    pub mode: String,
    pub settings: Option<AdaptiveConfig>,
    pub machine: MachineSample,
    pub cpu_percent: Option<f64>,
    pub last_429_at: f64,
    pub recent_429_count: usize,
    pub quota_checked_at: f64,
    pub openai: Vec<QuotaSnapshot>,
    pub anthropic: QuotaSnapshot,
    pub pace_ratio: Option<f64>,
    pub pace_adjusted_at: f64,
    pub preferred_provider: Option<String>,
    pub queued: usize,
}

pub fn cpu_load(previous: Option<[u64; 3]>, current: Option<[u64; 3]>) -> Option<f64> {
    let (a, b) = (previous?, current?);
    let idle = b[0].checked_sub(a[0])?;
    let total = b[1]
        .checked_sub(a[1])?
        .checked_add(b[2].checked_sub(a[2])?)?;
    (total > 0 && idle <= total).then(|| (total - idle) as f64 * 100. / total as f64)
}

/// Only copy quota metadata. Never retain email, label, token or the raw CLI reply.
pub fn parse_quotas(value: &Value, at: f64) -> QuotaSnapshot {
    let mut snapshot = QuotaSnapshot {
        at,
        ..Default::default()
    };
    let Some(rows) = value["accounts"].as_array() else {
        return snapshot;
    };
    snapshot.complete = true;
    let mut seen = HashSet::new();
    for row in rows {
        if row["paused"] == true
            || row["needsReauth"] == true
            || row["validationPending"] == true
            || row["selectionExcludedReason"].as_str().is_some()
            || row["autoSelectable"] == false
        {
            continue;
        }
        let q = &row["quota"];
        let fields = (
            row["id"].as_str(),
            q["weeklyPercent"].as_f64(),
            q["weeklyResetAt"].as_f64(),
        );
        let (Some(id), Some(used), Some(reset)) = fields else {
            snapshot.complete = false;
            continue;
        };
        let reset = if reset > 1e12 { reset / 1000. } else { reset };
        // CLI projects seconds or milliseconds depending on provider/version.
        if id.is_empty()
            || !seen.insert(id.to_owned())
            || !(0. ..=100.).contains(&used)
            || reset <= at
        {
            snapshot.complete = false;
            continue;
        }
        if row["quotaUnavailable"] == true
            || !row["quotaFailure"].is_null()
            || row["quotaRefresh"]["status"]
                .as_str()
                .is_some_and(|s| s != "ok")
        {
            snapshot.complete = false;
            continue;
        }
        let short_used = q["fiveHourPercent"]
            .as_f64()
            .or_else(|| q["shortPercent"].as_f64());
        let short_reset = q["fiveHourResetAt"]
            .as_f64()
            .or_else(|| q["shortResetAt"].as_f64())
            .map(|r| if r > 1e12 { r / 1000. } else { r });
        let short_exhausted =
            short_used.is_some_and(|p| p >= 100.) && short_reset.is_none_or(|r| r > at);
        snapshot.accounts.push(AccountQuota {
            id: id.into(),
            used_percent: used,
            reset_at: reset,
            short_exhausted,
        });
    }
    snapshot
}
fn available(snapshot: &QuotaSnapshot) -> bool {
    snapshot.complete
        && snapshot
            .accounts
            .iter()
            .any(|q| q.used_percent < 100. && !q.short_exhausted)
}
/// Match account identities and reset windows, then protect the account whose
/// consumption most exceeds its own remaining budget.
pub fn pace_ratio(history: &[QuotaSnapshot]) -> Option<f64> {
    pace_ratio_since(history, 0., history.last()?.at)
}
fn pace_ratio_since(history: &[QuotaSnapshot], since: f64, now: f64) -> Option<f64> {
    let latest = history.last()?;
    if latest.at > now {
        return None;
    }
    let mut ratio: f64 = 0.;
    let mut compared = false;
    for q in &latest.accounts {
        if q.reset_at <= now {
            continue;
        }
        // Unused accounts can publish a rolling estimate for their reset time.
        // Their changing window must not suppress a stable account's spending.
        // Find each account's own oldest comparable observation independently.
        let previous = history.iter().find_map(|snapshot| {
            let elapsed = latest.at - snapshot.at;
            if snapshot.at < since || !(300. ..=3600.).contains(&elapsed) {
                return None;
            }
            snapshot
                .accounts
                .iter()
                .find(|old| {
                    old.id == q.id
                        && (old.reset_at - q.reset_at).abs() <= 1.
                        && old.used_percent <= q.used_percent
                })
                .map(|old| (old, elapsed))
        });
        let Some((old, elapsed)) = previous else {
            continue;
        };
        compared = true;
        let actual = (q.used_percent - old.used_percent) / elapsed;
        let budget = (100. - q.used_percent) / (q.reset_at - latest.at);
        ratio = ratio.max(if budget > 0. {
            actual / budget
        } else if actual > 0. {
            f64::MAX
        } else {
            0.
        });
    }
    compared.then_some(ratio)
}

pub fn decide(
    cfg: &AdaptiveConfig,
    mut state: LimitState,
    sample: MachineSample,
    now: f64,
    queued: usize,
    recent_429_count: usize,
    last_429_at: f64,
) -> LimitState {
    if let (Some(old), Some(new)) = (state.machine.cpu_times, sample.cpu_times)
        && old.iter().zip(new).any(|(a, b)| b < *a)
    {
        state.max_running = cfg.min_running;
        state.first_at = now;
        state.stable_since = now;
    }
    let current = state.max_running.clamp(cfg.min_running, cfg.max_running);
    if state.first_at <= 0. || now < state.first_at {
        state.first_at = now;
    }
    if state.stable_since <= 0. || now < state.stable_since {
        state.stable_since = state.first_at;
    }
    state.cpu_percent = cpu_load(state.machine.cpu_times, sample.cpu_times);
    state.pace_ratio = pace_ratio_since(&state.openai, state.pace_adjusted_at, now);
    let latest = state.openai.last();
    let quota_fresh = state.quota_checked_at > 0.
        && now - state.quota_checked_at <= cfg.quota_interval_seconds as f64 + 60.;
    let quota_known = quota_fresh
        && latest.is_some_and(|q| q.complete && q.accounts.iter().all(|a| a.reset_at > now));
    let quota_empty = quota_known && !available(latest.unwrap());
    let mut reasons = Vec::new();
    if recent_429_count >= 2 && last_429_at > state.last_429_at {
        reasons.push("429が続いた");
    }
    if sample.health_ms.is_none() {
        reasons.push("プロキシ応答失敗");
    } else if sample
        .health_ms
        .is_some_and(|ms| ms >= cfg.health_fast_ms as f64)
    {
        reasons.push("プロキシ応答遅延");
    }
    if sample
        .free_memory_gib
        .is_some_and(|gb| gb < cfg.free_memory_gib)
    {
        reasons.push("空きメモリ不足");
    }
    if state.cpu_percent.is_some_and(|cpu| cpu >= cfg.cpu_percent) {
        reasons.push("CPU高負荷");
    }
    if quota_empty {
        reasons.push("Codex利用枠が回復待ち");
    }
    let over_pace = quota_fresh && state.pace_ratio.is_some_and(|ratio| ratio > 1.);
    let pace_waiting = quota_known
        && state.pace_adjusted_at > 0.
        && state.pace_ratio.is_none()
        && latest.is_some_and(|q| q.at - state.pace_adjusted_at < 300.);
    let pace_new = over_pace && latest.unwrap().at > state.pace_adjusted_at;
    let pressure = !reasons.is_empty();
    if pace_new {
        reasons.push("週の利用枠を使うペースが速い");
        state.pace_adjusted_at = latest.unwrap().at;
    }
    if !reasons.is_empty() || recent_429_count > 0 {
        state.stable_since = now;
    }
    state.preferred_provider = ((over_pace || pace_waiting || quota_empty)
        && quota_fresh
        && available(&state.anthropic)
        && state.anthropic.accounts.iter().all(|q| q.reset_at > now))
    .then(|| "anthropic".to_owned());
    state.max_running = if !reasons.is_empty() {
        let mut reduced = if pressure {
            (current / 2).max(cfg.min_running)
        } else {
            current
        };
        if pace_new {
            reduced = reduced
                .min((current as f64 / state.pace_ratio.unwrap()).floor() as usize)
                .max(cfg.min_running);
        }
        if quota_empty {
            reduced = cfg.min_running;
        }
        state.reason = reasons.join("・");
        reduced
    } else if over_pace || pace_waiting {
        state.reason = "減枠後の利用ペースを確認中".into();
        current
    } else if recent_429_count > 0
        || (last_429_at > 0. && now - last_429_at < cfg.quiet_seconds as f64)
        || now - state.stable_since < cfg.quiet_seconds as f64
    {
        state.reason = "429なしの安定時間を確認中".into();
        current
    } else if sample.free_memory_gib.is_none() || state.cpu_percent.is_none() || !quota_known {
        state.reason = "増枠に必要な測定値または利用枠を確認中".into();
        current
    } else if queued == 0 {
        state.reason = "順番待ちなし".into();
        current
    } else {
        state.reason = if current == cfg.max_running {
            "設定の最大値"
        } else {
            "応答・メモリ・CPU・利用枠が安定"
        }
        .into();
        current
            .saturating_add(cfg.increase_step)
            .min(cfg.max_running)
    };
    state.settings = Some(cfg.clone());
    state.mode = "auto".into();
    state.checked_at = now;
    state.machine = sample;
    state.queued = queued;
    state.last_429_at = state.last_429_at.max(last_429_at);
    state.recent_429_count = recent_429_count;
    state
}

impl Context {
    fn limit_state(&self) -> Result<LimitState> {
        let path = self.root.join("queue-limit-state.json");
        match self.read_file(&path) {
            Ok(bytes) => Ok(serde_json::from_slice(&bytes)?),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(LimitState::default()),
            Err(e) => Err(Error::file_io("read limit state", &path, e)),
        }
    }
    fn limit_state_for_update(&self, floor: usize) -> Result<LimitState> {
        let path = self.root.join("queue-limit-state.json");
        match self.read_file(&path) {
            Ok(bytes) => match serde_json::from_slice(&bytes) {
                Ok(state) => Ok(state),
                Err(_) => {
                    self.write_json(
                        &self.root.join("queue-limit-state.invalid.json"),
                        &json!({"at":self.rt.now(),"bytes":bytes}),
                    )?;
                    self.rt
                        .diagnostic("上限の測定状態が壊れているため原文を保管し最小値から再測定");
                    Ok(LimitState {
                        max_running: floor,
                        ..Default::default()
                    })
                }
            },
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(LimitState::default()),
            Err(e) => Err(Error::file_io("read limit state for update", &path, e)),
        }
    }
    pub(crate) fn effective_limit(&self, cfg: &QueueConfig) -> Result<usize> {
        if let Some(fixed) = cfg.fixed_max_running {
            return Ok(fixed);
        }
        let Some(adaptive) = &cfg.adaptive else {
            return Ok(cfg.max_running);
        };
        let state = match self.limit_state() {
            Ok(state) => state,
            Err(_) => return Ok(adaptive.min_running),
        };
        if state.checked_at > 0.
            && (self.rt.epoch() < state.checked_at
                || self.rt.epoch() - state.checked_at > adaptive.stale_after_seconds())
        {
            return Ok(adaptive.min_running);
        }
        Ok(if state.mode == "auto" && state.max_running > 0 {
            state.max_running
        } else {
            cfg.max_running
        }
        .clamp(adaptive.min_running, adaptive.max_running))
    }
    pub fn limit_status(&self, cfg: &QueueConfig) -> Result<Value> {
        let (state, state_error) = match self.limit_state() {
            Ok(state) => (state, false),
            Err(_) => (LimitState::default(), true),
        };
        let (mode, reason) = if cfg.fixed_max_running.is_some() {
            ("fixed", "手動で固定")
        } else if cfg.adaptive.is_none() {
            ("manual", "設定値")
        } else if state_error {
            ("auto", "測定状態を読めないため最小値")
        } else if state.checked_at > 0.
            && (self.rt.epoch() < state.checked_at
                || self.rt.epoch() - state.checked_at
                    > cfg.adaptive.as_ref().unwrap().stale_after_seconds())
        {
            ("auto", "測定が古いため最小値")
        } else if state.mode != "auto" {
            ("auto", "最初の測定待ち")
        } else {
            ("auto", state.reason.as_str())
        };
        let label = format!(
            "上限 {}（{}・{}）",
            cfg.max_running,
            match mode {
                "auto" => "自動",
                "fixed" => "固定",
                _ => "手動",
            },
            reason
        );
        Ok(
            json!({"max_running":cfg.max_running,"mode":mode,"reason":reason,"label":label,
            "checked_at":state.checked_at,"preferred_provider":if mode == "auto" && !state_error && state.mode == "auto" && self.rt.epoch() >= state.checked_at && self.rt.epoch()-state.checked_at <= cfg.adaptive.as_ref().unwrap().stale_after_seconds() { state.preferred_provider } else { None },
            "pace_ratio":state.pace_ratio,"cpu_percent":state.cpu_percent,"free_memory_gib":state.machine.free_memory_gib,
            "health_ms":state.machine.health_ms,"recent_429_count":state.recent_429_count,"quota_checked_at":state.quota_checked_at}),
        )
    }
    /// Sampling holds only its own nonblocking lock, never the admission lock.
    /// Competing supervisors continue using the last durable shared limit.
    pub fn adjust_limit(&self) -> Result<()> {
        let cfg = self.queue_config()?;
        let Some(adaptive) = &cfg.adaptive else {
            return Ok(());
        };
        fs::create_dir_all(&self.root)?;
        let _lock = match FileLock::try_acquire(&self.root.join("queue-limit.lock")) {
            Ok(lock) => lock,
            Err(e)
                if matches!(e.raw_os_error(), Some(32 | 33))
                    || e.kind() == std::io::ErrorKind::WouldBlock =>
            {
                return Ok(());
            }
            Err(e) => return Err(e.into()),
        };
        let mut state = self.limit_state_for_update(adaptive.min_running)?;
        let now = self.rt.epoch();
        if state.checked_at > 0.
            && now >= state.checked_at
            && (now / adaptive.interval_seconds as f64).floor()
                == (state.checked_at / adaptive.interval_seconds as f64).floor()
            && state.settings.as_ref() == Some(adaptive)
            && ((cfg.fixed_max_running.is_some()
                && state.mode == "fixed"
                && cfg.max_running == state.max_running)
                || (cfg.fixed_max_running.is_none() && state.mode == "auto"))
        {
            return Ok(());
        }
        let previous = cfg.max_running;
        if let Some(fixed) = cfg.fixed_max_running {
            state.max_running = fixed;
            state.mode = "fixed".into();
            state.reason = "手動で固定".into();
            state.checked_at = now;
            state.settings = Some(adaptive.clone());
        } else {
            if state.checked_at > 0.
                && (now < state.checked_at
                    || now - state.checked_at > adaptive.stale_after_seconds())
            {
                state = LimitState {
                    max_running: adaptive.min_running,
                    first_at: now,
                    stable_since: now,
                    ..Default::default()
                };
            }
            if state.max_running == 0 || state.mode != "auto" {
                state.max_running = cfg.max_running;
                state.first_at = now;
                state.stable_since = now;
                state.machine = MachineSample::default();
            }
            let machine = self.rt.queue_machine_sample(adaptive);
            if state.quota_checked_at <= 0.
                || now < state.quota_checked_at
                || now - state.quota_checked_at >= adaptive.quota_interval_seconds as f64
            {
                state.openai.push(self.rt.queue_quota_sample(
                    "openai",
                    now,
                    adaptive.quota_timeout_seconds,
                ));
                state.openai.retain(|q| q.at <= now && now - q.at <= 3600.);
                state.anthropic =
                    self.rt
                        .queue_quota_sample("anthropic", now, adaptive.quota_timeout_seconds);
                state.quota_checked_at = now;
            }
            let jobs = self.list_jobs(&HashSet::new());
            // Only unmet demand for global slots justifies raising that limit.
            // Dependency/cooldown/child-limit waits cannot use extra slots.
            let queued = self
                .queue_decisions(&jobs, &cfg, None)
                .values()
                .filter(|reason| reason["code"] == "running_limit")
                .count();
            let mut recent = 0;
            let mut latest: f64 = 0.;
            for job in &jobs {
                if let Some(times) = job["rate_limit_times"].as_array() {
                    for time in times
                        .iter()
                        .filter_map(Value::as_f64)
                        .filter(|at| *at <= now)
                    {
                        latest = latest.max(time);
                        if now - time <= adaptive.quiet_seconds as f64 {
                            recent += 1;
                        }
                    }
                } else if s(job, "status") != "succeeded"
                    && (crate::queue::is_429(s(job, "last_error"))
                        || crate::queue::is_429(s(job, "retry_last_error")))
                {
                    // Existing supervisors have no event counter; conservatively count
                    // one recent failed/retried job, not its whole log on every tick.
                    let time =
                        parse_iso(s(job, "finished_at")).or_else(|| parse_iso(s(job, "queued_at")));
                    if let Some(at) = time.filter(|at| *at <= now) {
                        latest = latest.max(at);
                        if now - at <= adaptive.quiet_seconds as f64 {
                            recent += 1;
                        }
                    }
                }
            }
            state = decide(adaptive, state, machine, now, queued, recent, latest);
        }
        // Keep a finite journal: roughly one day at the default cadence.
        let journal = self.root.join("queue-limit-history.json");
        let mut history: Vec<Value> = match self.read_file(&journal) {
            Ok(bytes) => match serde_json::from_slice(&bytes) {
                Ok(history) => history,
                Err(_) => {
                    self.write_json(
                        &self.root.join("queue-limit-history.invalid.json"),
                        &json!({"at":self.rt.now(),"bytes":bytes}),
                    )?;
                    self.rt
                        .diagnostic("上限の履歴が壊れているため原文を保管し履歴を再作成");
                    Vec::new()
                }
            },
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Vec::new(),
            Err(e) => return Err(e.into()),
        };
        history.push(json!({"at":self.rt.now(),"epoch":now,"previous":previous,"max_running":state.max_running,
            "mode":state.mode,"reason":state.reason,"queued":state.queued,"cpu_percent":state.cpu_percent,
            "machine":state.machine,"recent_429_count":state.recent_429_count,"pace_ratio":state.pace_ratio,
            "preferred_provider":state.preferred_provider,"openai":state.openai.last(),"anthropic":state.anthropic}));
        if history.len() > 1440 {
            history.drain(..history.len() - 1440);
        }
        self.write_json(&journal, &json!(history))?;
        self.write_json(&self.root.join("queue-limit-state.json"), &json!(state))?;
        // Older running controllers may later requeue for context rotation or
        // retry. They cannot parse the new config fields; upgrade only once
        // they are queued, never while their Codex process is live.
        self.reload_legacy_queue()?;
        Ok(())
    }
}

pub fn machine_sample(cfg: &AdaptiveConfig) -> MachineSample {
    let mut sample = MachineSample::default();
    #[cfg(windows)]
    unsafe {
        use windows_sys::Win32::{
            Foundation::FILETIME,
            System::{
                SystemInformation::{GlobalMemoryStatusEx, MEMORYSTATUSEX},
                Threading::GetSystemTimes,
            },
        };
        let mut memory: MEMORYSTATUSEX = std::mem::zeroed();
        memory.dwLength = std::mem::size_of::<MEMORYSTATUSEX>() as u32;
        if GlobalMemoryStatusEx(&mut memory) != 0 {
            sample.free_memory_gib = Some(memory.ullAvailPhys as f64 / (1024u64.pow(3)) as f64);
        }
        let (mut idle, mut kernel, mut user): (FILETIME, FILETIME, FILETIME) =
            (std::mem::zeroed(), std::mem::zeroed(), std::mem::zeroed());
        if GetSystemTimes(&mut idle, &mut kernel, &mut user) != 0 {
            let time =
                |t: FILETIME| (u64::from(t.dwHighDateTime) << 32) | u64::from(t.dwLowDateTime);
            sample.cpu_times = Some([time(idle), time(kernel), time(user)]);
        }
    }
    sample.health_ms = health_probe(
        cfg.health_port,
        Duration::from_millis(cfg.health_timeout_ms),
    )
    .ok();
    sample
}
pub fn health_probe(port: u16, timeout: Duration) -> std::io::Result<f64> {
    let started = Instant::now();
    let deadline = started + timeout;
    let remaining = || {
        deadline
            .checked_duration_since(Instant::now())
            .filter(|d| !d.is_zero())
            .ok_or_else(|| std::io::Error::new(std::io::ErrorKind::TimedOut, "health deadline"))
    };
    let mut socket =
        TcpStream::connect_timeout(&SocketAddr::from((Ipv4Addr::LOCALHOST, port)), remaining()?)?;
    socket.set_write_timeout(Some(remaining()?))?;
    socket.write_all(
        format!("GET /healthz HTTP/1.0\r\nHost: 127.0.0.1:{port}\r\nConnection: close\r\n\r\n")
            .as_bytes(),
    )?;
    let mut data = Vec::new();
    loop {
        socket.set_read_timeout(Some(remaining()?))?;
        let mut chunk = [0; 1024];
        let n = socket.read(&mut chunk)?;
        if n == 0 {
            break;
        }
        data.extend_from_slice(&chunk[..n]);
        if data.len() > 16_384 {
            return Err(std::io::Error::other("health response too large"));
        }
    }
    let boundary = data
        .windows(4)
        .position(|w| w == b"\r\n\r\n")
        .ok_or_else(|| std::io::Error::other("health headers"))?;
    let headers = std::str::from_utf8(&data[..boundary]).map_err(std::io::Error::other)?;
    if headers
        .lines()
        .next()
        .is_none_or(|line| line.split_whitespace().nth(1) != Some("200"))
    {
        return Err(std::io::Error::other("health status"));
    }
    let body: Value =
        serde_json::from_slice(&data[boundary + 4..]).map_err(std::io::Error::other)?;
    if body["service"] != "opencodex" || body["status"] != "ok" {
        return Err(std::io::Error::other("health body"));
    }
    Ok(started.elapsed().as_secs_f64() * 1000.)
}
