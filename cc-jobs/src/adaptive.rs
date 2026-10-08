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
    /// Independent hardware ceiling; max_running remains the Codex lane.
    pub hardware_max_running: usize,
    pub auto_ready_samples: usize,
    pub reason: String,
    pub claude_max_running: usize,
    pub claude_stable_since: f64,
    pub claude_last_429_at: f64,
    pub claude_recent_429_count: usize,
    pub claude_reason: String,
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
/// Consumption is summed across comparable accounts. Every current account's
/// remaining budget contributes, including unused accounts with rolling resets.
pub fn pace_ratio(history: &[QuotaSnapshot]) -> Option<f64> {
    let latest = history.last()?;
    if !latest.complete {
        return None;
    }
    let mut actual = 0.;
    let mut budget = 0.;
    let mut compared = false;
    for q in &latest.accounts {
        if q.reset_at <= latest.at {
            continue;
        }
        budget += (100. - q.used_percent) / (q.reset_at - latest.at);
        let previous = history.iter().find_map(|snapshot| {
            let elapsed = latest.at - snapshot.at;
            if !snapshot.complete || !(300. ..=3600.).contains(&elapsed) {
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
        if let Some((old, elapsed)) = previous {
            actual += (q.used_percent - old.used_percent) / elapsed;
            compared = true;
        }
    }
    compared.then_some(if budget > 0. {
        actual / budget
    } else if actual > 0. {
        f64::MAX
    } else {
        0.
    })
}
/// Supplement the refresh response from OpenCodex's whole-account cache.
/// CLI exclusions take precedence; stale/invalid cache entries cannot establish
/// pool exhaustion. Only quota metadata reaches the durable snapshot.
pub fn parse_codex_quotas(cli: Option<&Value>, cache: &Value, at: f64) -> QuotaSnapshot {
    let mut rows = cli
        .and_then(|v| v["accounts"].as_array())
        .cloned()
        .unwrap_or_default();
    let mut complete = cli.is_some_and(|v| v["accounts"].is_array()) && cache["quotas"].is_object();
    if let Some(quotas) = cache["quotas"].as_object() {
        for (id, q) in quotas {
            if rows.iter().any(|row| row["id"] == *id) {
                continue;
            }
            let updated = q["updatedAt"]
                .as_f64()
                .map(|t| if t > 1e12 { t / 1000. } else { t });
            if updated.is_none_or(|t| t > at || at - t > 360.) {
                complete = false;
                continue;
            }
            rows.push(json!({"id":id,"quota":q}));
        }
    }
    let mut snapshot = parse_quotas(&json!({"accounts":rows}), at);
    snapshot.complete &= complete;
    snapshot
}

fn fresh_quota(q: &QuotaSnapshot, now: f64, cfg: &AdaptiveConfig) -> bool {
    q.complete
        && q.at > 0.
        && q.at <= now
        && now - q.at <= cfg.quota_interval_seconds as f64 + 60.
        && q.accounts.iter().all(|a| a.reset_at > now)
}
fn nearly_empty(q: &QuotaSnapshot) -> bool {
    !q.accounts.is_empty()
        && q.accounts
            .iter()
            .map(|a| 100. - a.used_percent)
            .sum::<f64>()
            <= 10. * q.accounts.len() as f64
}

/// Hardware control only. Provider failures and spending never reduce this
/// ceiling. CPU deltas average the entire sampling interval, not a point sample.
pub fn decide(
    cfg: &AdaptiveConfig,
    mut state: LimitState,
    sample: MachineSample,
    now: f64,
    queued: usize,
    _recent_429_count: usize,
    _last_429_at: f64,
) -> LimitState {
    let current = state.max_running.clamp(cfg.min_running, cfg.max_running);
    state.cpu_percent = cpu_load(state.machine.cpu_times, sample.cpu_times);
    if state.first_at <= 0. || now < state.first_at {
        state.first_at = now;
    }
    if state.stable_since <= 0. || now < state.stable_since {
        state.stable_since = now;
    }
    let slow = sample
        .health_ms
        .is_some_and(|ms| ms >= cfg.health_fast_ms as f64);
    let repeated_slow = slow
        && state.checked_at > 0.
        && now > state.checked_at
        && now - state.checked_at <= cfg.stale_after_seconds()
        && state
            .machine
            .health_ms
            .is_some_and(|ms| ms >= cfg.health_fast_ms as f64);
    let memory_pressure = sample
        .free_memory_gib
        .is_some_and(|gb| gb < cfg.free_memory_gib);
    let cpu_pressure = state.cpu_percent.is_some_and(|cpu| cpu >= cfg.cpu_percent);
    state.max_running = if sample.health_ms.is_none() || repeated_slow {
        state.reason = "プロキシ応答失敗または連続遅延".into();
        state.stable_since = now;
        (current / 2).max(cfg.min_running)
    } else if memory_pressure || cpu_pressure {
        // Move towards the operating band without repeatedly halving all work.
        let factor = if memory_pressure {
            0.8
        } else {
            ((cfg.cpu_percent - 5.).max(1.) / state.cpu_percent.unwrap()).min(0.9)
        };
        state.reason = if memory_pressure {
            "空きメモリ不足"
        } else {
            "CPU高負荷"
        }
        .into();
        state.stable_since = now;
        ((current as f64 * factor).floor() as usize).max(cfg.min_running)
    } else if queued == 0 {
        state.reason = "順番待ちなし".into();
        current
    } else if slow {
        state.reason = "単発のプロキシ応答遅延を確認中".into();
        current
    } else if let (Some(cpu), Some(memory)) = (state.cpu_percent, sample.free_memory_gib) {
        if cpu >= (cfg.cpu_percent - 10.).max(1.) {
            state.reason = "CPUが目標の範囲内".into();
            current
        } else {
            let step = if cpu < 40. && memory >= cfg.free_memory_gib * 2. {
                cfg.increase_step.saturating_mul(4).max(current / 2)
            } else if cpu < 60. && memory >= cfg.free_memory_gib * 1.5 {
                cfg.increase_step.saturating_mul(2).max(current / 4)
            } else {
                cfg.increase_step.max(current / 8)
            };
            state.reason = "CPUと空きメモリに余裕があり増枠".into();
            current.saturating_add(step).min(cfg.max_running)
        }
    } else {
        state.reason = "CPUと空きメモリの測定待ち".into();
        current
    };
    state.settings = Some(cfg.clone());
    state.mode = "auto".into();
    state.checked_at = now;
    state.machine = sample;
    state.queued = queued;
    state
}

#[allow(clippy::too_many_arguments)]
fn provider_limit(
    cfg: &AdaptiveConfig,
    previous: usize,
    hardware: usize,
    stable: &mut f64,
    last: &mut f64,
    recent: usize,
    latest: f64,
    now: f64,
    exhausted: bool,
    pace_pressure: bool,
    claude: bool,
) -> (usize, String) {
    if exhausted {
        return (0, "利用枠が回復待ち".into());
    }
    let floor = cfg.min_running.min(hardware);
    let initial = if claude {
        16.max(cfg.min_running)
    } else {
        cfg.min_running
    };
    let current = if previous == 0 { initial } else { previous }.min(hardware);
    if recent >= 2 && latest > *last {
        *last = latest;
        *stable = now;
        return ((current / 2).max(floor), "429が続いたため減枠".into());
    }
    *last = last.max(latest);
    if recent > 0 || (*last > 0. && now - *last < cfg.quiet_seconds as f64) {
        *stable = now;
        return (current, "429後の安定時間を確認中".into());
    }
    if pace_pressure {
        return (
            current.min(hardware.div_ceil(4)).max(floor),
            "全アカウントの週の残り枠が少ないためClaudeへ振り分け".into(),
        );
    }
    *stable = now;
    (hardware, "CPUと空きメモリに応じた上限".into())
}

/// Independent provider admission limits beneath a shared hardware ceiling.
#[allow(clippy::too_many_arguments)]
pub fn decide_providers(
    cfg: &AdaptiveConfig,
    state: LimitState,
    sample: MachineSample,
    now: f64,
    queued: usize,
    codex_recent: usize,
    codex_latest: f64,
    claude_recent: usize,
    claude_latest: f64,
) -> LimitState {
    let old = state.clone();
    let mut hardware_state = state;
    hardware_state.max_running = if old.hardware_max_running > 0 {
        old.hardware_max_running
    } else {
        old.max_running.max(old.claude_max_running)
    };
    let mut state = decide(cfg, hardware_state, sample, now, queued, 0, 0.);
    let hardware = state.max_running;
    state.hardware_max_running = hardware;
    state.pace_ratio = pace_ratio(&state.openai);
    let codex_quota = state.openai.last().filter(|q| fresh_quota(q, now, cfg));
    let codex_empty = codex_quota.is_some_and(|q| !available(q));
    let pace_pressure =
        codex_quota.is_some_and(nearly_empty) && state.pace_ratio.is_some_and(|r| r > 1.);
    let claude_known = fresh_quota(&state.anthropic, now, cfg);
    let claude_empty = claude_known && !available(&state.anthropic);
    let (codex, codex_reason) = provider_limit(
        cfg,
        old.max_running,
        hardware,
        &mut state.stable_since,
        &mut state.last_429_at,
        codex_recent,
        codex_latest,
        now,
        codex_empty,
        pace_pressure,
        false,
    );
    let (claude, claude_reason) = provider_limit(
        cfg,
        old.claude_max_running,
        hardware,
        &mut state.claude_stable_since,
        &mut state.claude_last_429_at,
        claude_recent,
        claude_latest,
        now,
        claude_empty,
        false,
        true,
    );
    state.max_running = codex;
    state.claude_max_running = claude;
    state.recent_429_count = codex_recent;
    state.claude_recent_429_count = claude_recent;
    state.claude_reason = claude_reason;
    if codex < hardware {
        state.reason.push_str(&format!("・Codex: {codex_reason}"));
    }
    state.preferred_provider = ((codex_empty || pace_pressure || codex < hardware)
        && claude_known
        && !claude_empty
        && claude_recent == 0)
        .then(|| "anthropic".to_owned());
    state.auto_ready_samples = if hardware >= 16
        && state.cpu_percent.is_some_and(|p| p < cfg.cpu_percent)
        && state
            .machine
            .free_memory_gib
            .is_some_and(|m| m >= cfg.free_memory_gib)
        && state
            .machine
            .health_ms
            .is_some_and(|ms| ms < cfg.health_fast_ms as f64)
        && old.checked_at > 0.
        && old.settings.as_ref().is_none_or(|settings| settings == cfg)
        && now > old.checked_at
        && now - old.checked_at <= cfg.stale_after_seconds()
    {
        old.auto_ready_samples.saturating_add(1)
    } else {
        0
    };
    state
}
impl Context {
    /// The hardware ceiling is shared; only Codex has the weekly pace ceiling.
    pub(crate) fn provider_limits(&self, cfg: &QueueConfig) -> (usize, usize, usize, bool) {
        if let Some(limits) = cfg.admission_limits {
            return limits;
        }
        let fallback = (cfg.max_running, cfg.max_running, cfg.max_running, false);
        let Some(adaptive) = cfg
            .adaptive
            .as_ref()
            .filter(|_| cfg.fixed_max_running.is_none())
        else {
            return fallback;
        };
        let Ok(state) = self.limit_state() else {
            return fallback;
        };
        let now = self.rt.epoch();
        // After a measured fixed override is removed, use the fresh shadow
        // lanes immediately; do not briefly discard Codex exhaustion/routing.
        if (state.mode != "auto" && !(state.mode == "fixed" && state.hardware_max_running > 0))
            || state.checked_at <= 0.
            || now < state.checked_at
            || now - state.checked_at > adaptive.stale_after_seconds()
        {
            return fallback;
        }
        if state.hardware_max_running > 0 {
            let hardware = state
                .hardware_max_running
                .clamp(adaptive.min_running, adaptive.max_running);
            return (
                hardware,
                state.max_running.min(hardware),
                state.claude_max_running.min(hardware),
                state.preferred_provider.as_deref() == Some("anthropic"),
            );
        }
        let claude = if state.claude_max_running > 0 {
            state
                .claude_max_running
                .clamp(adaptive.min_running, adaptive.max_running)
        } else {
            adaptive.min_running
        };
        // Derive both lanes and the routing hint from this single durable
        // snapshot. The sampler can publish while the admission gate is held.
        let codex = if state.max_running > 0 {
            state
                .max_running
                .clamp(adaptive.min_running, adaptive.max_running)
        } else {
            cfg.max_running
        };
        (
            codex.max(claude),
            codex,
            claude,
            state.preferred_provider.as_deref() == Some("anthropic"),
        )
    }

    pub(crate) fn launch_model(&self, job: &Value, prefer_claude: bool) -> String {
        // Old records have no provenance: keep their model, as with explicit jobs.
        // Once an attempt has a session, retries/resumes must retain its model.
        if !prefer_claude
            || job["model_explicit"] != false
            || !s(job, "thread_id").is_empty()
            || !s(job, "resume_session").is_empty()
            || !s(job, "context_phase").is_empty()
            || truth(&job["first_started_at"])
        {
            return s(job, "model").to_owned();
        }
        // Haiku is limited to an explicitly classified single-file read task.
        // Multi-file reviews, research and implementation require Sonnet.
        if s(job, "sandbox") == "read-only"
            && [
                "single_file_review",
                "single_file_summary",
                "single_file_check",
            ]
            .contains(&s(job, "task_type"))
        {
            "anthropic/claude-haiku-5-5".into()
        } else {
            "anthropic/claude-sonnet-5-5".into()
        }
    }

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
        Ok(if state.hardware_max_running > 0 {
            state.hardware_max_running
        } else if state.mode == "auto" && state.max_running > 0 {
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
        } else if state.mode != "auto" && !(state.mode == "fixed" && state.hardware_max_running > 0)
        {
            ("auto", "最初の測定待ち")
        } else {
            ("auto", state.reason.as_str())
        };
        let (hardware, codex, claude, _) = self.provider_limits(cfg);
        let providers = if cfg.adaptive.is_some() && cfg.fixed_max_running.is_none() {
            format!("・Codex {codex}枠・Claude {claude}枠")
        } else {
            String::new()
        };
        let label = format!(
            "上限 {}（{}・{}{}）",
            hardware,
            match mode {
                "auto" => "自動",
                "fixed" => "固定",
                _ => "手動",
            },
            reason,
            providers
        );
        Ok(
            json!({"max_running":hardware,"codex_max_running":codex,"claude_max_running":claude,"mode":mode,"reason":reason,"claude_reason":state.claude_reason,"label":label,
            "checked_at":state.checked_at,"preferred_provider":if mode == "auto" && !state_error && (state.mode == "auto" || (state.mode == "fixed" && state.hardware_max_running > 0)) && self.rt.epoch() >= state.checked_at && self.rt.epoch()-state.checked_at <= cfg.adaptive.as_ref().unwrap().stale_after_seconds() { state.preferred_provider } else { None },
            "pace_ratio":state.pace_ratio,"cpu_percent":state.cpu_percent,"free_memory_gib":state.machine.free_memory_gib,
            "health_ms":state.machine.health_ms,"recent_429_count":state.recent_429_count,"quota_checked_at":state.quota_checked_at,
            "auto_hardware_max_running":state.hardware_max_running,"auto_codex_max_running":state.max_running,
            "auto_claude_max_running":state.claude_max_running,"auto_ready_samples":state.auto_ready_samples}),
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
            && ((cfg.fixed_max_running.is_some() && state.mode == "fixed")
                || (cfg.fixed_max_running.is_none() && state.mode == "auto"))
        {
            return Ok(());
        }
        let previous = cfg.max_running;
        {
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
            if state.hardware_max_running == 0 && (state.max_running == 0 || state.mode != "auto") {
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
                .filter(|reason| {
                    reason["code"] == "running_limit" && reason["hardware_limited"] == true
                })
                .count();
            let mut recent = 0;
            let mut latest: f64 = 0.;
            let mut claude_recent = 0;
            let mut claude_latest: f64 = 0.;
            for job in &jobs {
                let (recent, latest) = if s(job, "model").starts_with("anthropic/") {
                    (&mut claude_recent, &mut claude_latest)
                } else {
                    (&mut recent, &mut latest)
                };
                if let Some(times) = job["rate_limit_times"].as_array() {
                    for time in times
                        .iter()
                        .filter_map(Value::as_f64)
                        .filter(|at| *at <= now)
                    {
                        *latest = latest.max(time);
                        if now - time <= adaptive.quiet_seconds as f64 {
                            *recent += 1;
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
                        *latest = latest.max(at);
                        if now - at <= adaptive.quiet_seconds as f64 {
                            *recent += 1;
                        }
                    }
                }
            }
            state = decide_providers(
                adaptive,
                state,
                machine,
                now,
                queued,
                recent,
                latest,
                claude_recent,
                claude_latest,
            );
            if cfg.fixed_max_running.is_some() {
                state.mode = "fixed".into();
            }
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
            "preferred_provider":state.preferred_provider,"openai":state.openai.last(),"anthropic":state.anthropic,
            "claude_max_running":state.claude_max_running,"claude_reason":state.claude_reason,"claude_recent_429_count":state.claude_recent_429_count,
            "hardware_max_running":state.hardware_max_running,"auto_ready_samples":state.auto_ready_samples}));
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
    let mut sample = machine_resources();
    sample.health_ms = health_probe(
        cfg.health_port,
        Duration::from_millis(cfg.health_timeout_ms),
    )
    .ok();
    sample
}
/// Native machine counters without the queue controller's HTTP health probe.
pub fn machine_resources() -> MachineSample {
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
