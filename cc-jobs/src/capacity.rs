//! Resume a capacity failure inside the existing supervisor and queue entry.
use crate::{queue::prepare_resume, *};

const MODELS: [&str; 5] = [
    "anthropic/claude-sonnet-5-5",
    "anthropic/claude-opus-5-5",
    "gpt-6.1-sol",
    "gpt-6-astra",
    "gpt-6-luna",
];
const MAX_SWITCHES: u64 = 3;
const MAX_WAITS: u64 = 3;

pub fn provider(model: &str) -> Option<&'static str> {
    if ["claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-5-5"]
        .contains(&model.strip_prefix("anthropic/").unwrap_or(model))
    {
        Some("anthropic")
    } else if crate::pool_retry::native_model(model).is_some() {
        Some("openai")
    } else {
        None
    }
}

/// Called only for protocol error events, never tool results or assistant text.
pub fn failure_kind(event: &Value) -> Option<&'static str> {
    let message = match s(event, "type") {
        "error" => s(event, "message").to_owned(),
        "turn.failed" => err_text(&event["error"]),
        _ => return None,
    };
    let code = if s(&event["error"], "code").is_empty() {
        s(event, "code")
    } else {
        s(&event["error"], "code")
    }
    .to_ascii_lowercase();
    let text = message.to_ascii_lowercase();
    Some(
        if code.contains("cancel") || text.contains("request cancelled") {
            "cancel"
        } else if code.contains("auth")
            || text.contains("unauthorized")
            || text.contains("authentication failed")
        {
            "auth"
        } else if is_capacity(&message) {
            "capacity"
        } else if text.contains("quota")
            || text.contains("usage limit")
            || text.contains("429 too many requests")
            || cc_job_policy::quota_recovery::grok_balance_exhausted(&message)
        {
            "quota"
        } else if code == "request_failed" {
            "request_failed"
        } else {
            "transient"
        },
    )
}

pub fn review_job(job: &Value) -> bool {
    cc_job_policy::review_task(s(job, "name"), s(job, "task_type"))
        || s(&job["subwork"], "kind") == "review"
}

/// Preserve explicit lightweight and independent review constraints.
pub fn candidates(job: &Value) -> Vec<&'static str> {
    if review_job(job)
        && s(job, "parent_job_id").is_empty()
        && job["review_excluded_models"]
            .as_array()
            .is_none_or(|v| v.is_empty())
    {
        return Vec::new();
    }
    let requested = if s(job, "requested_model").is_empty() {
        s(job, "model")
    } else {
        s(job, "requested_model")
    };
    let lightweight = requested.contains("haiku");
    let current = s(job, "model");
    let mut options: Vec<_> = MODELS
        .into_iter()
        .filter(|model| {
            *model != current
                && (!lightweight
                    || ((review_job(job) || s(job, "task_type").starts_with("single_file_"))
                        && s(job, "sandbox") == "read-only"
                        && *model == "gpt-6-luna"))
                && (lightweight
                    || provider(current) != Some("openai")
                    || provider(model) == Some("anthropic"))
                && (provider(model) != Some("openai")
                    || lightweight
                    || crate::pool_retry::native_model(requested).unwrap_or("gpt-6.1-sol")
                        == *model)
                && !job["review_excluded_models"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .any(|v| same_model(s_value(v), model))
                && !job["capacity_history"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .any(|entry| {
                        same_model(s(entry, "from_model"), model)
                            || same_model(s(entry, "to_model"), model)
                    })
        })
        .collect();
    let preferred_claude = if [
        "implementation",
        "architecture",
        "design",
        "live_debug",
        "review",
        "code_review",
        "pr_shepherd",
    ]
    .contains(&s(job, "task_type"))
    {
        "anthropic/claude-opus-5-5"
    } else {
        "anthropic/claude-sonnet-5-5"
    };
    options.sort_by_key(|model| {
        (
            provider(model) == provider(current),
            *model != preferred_claude,
        )
    });
    options
}

fn s_value(value: &Value) -> &str {
    value.as_str().unwrap_or("")
}

fn same_model(left: &str, right: &str) -> bool {
    cc_job_policy::same_model(left, right)
}

fn review_exclusions(job: &Value, authors: &[String]) -> Vec<Value> {
    let mut merged: Vec<Value> = Vec::new();
    for model in job["review_excluded_models"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .chain(authors.iter().map(String::as_str))
    {
        if !merged.iter().any(|old| same_model(s_value(old), model)) {
            merged.push(json!(model));
        }
    }
    merged
}

pub fn is_capacity(message: &str) -> bool {
    message
        .to_ascii_lowercase()
        .contains("selected model is at capacity")
}

impl Context {
    pub(crate) fn recheck_queued_capacity(
        &self,
        jid: &str,
        expected: &Value,
        inventory: Value,
    ) -> Result<()> {
        let _gate = self.queue_lock()?;
        let _state = self.job_lock(jid, "state.lock")?;
        let mut fresh = self.load(jid)?;
        if !crate::pool_retry::same_queued_attempt(expected, &fresh)
            || fresh["capacity_wait_pending"] != true
        {
            return Ok(());
        }
        let _todo = if fresh["todo_id"].is_number() {
            Some(FileLock::acquire(
                &self.todo_store().with_file_name("todo.lock"),
            )?)
        } else {
            None
        };
        if _todo.is_some()
            && !crate::pool_retry::todo_owns_attempt(&self.read_json(&self.todo_store())?, &fresh)
        {
            self.end_capacity_owner(&mut fresh);
        } else {
            fresh["capacity_inventory"] = inventory;
            self.recheck_capacity_wait(&mut fresh);
        }
        self.save(&fresh)
    }

    pub(crate) fn defer_capacity_recheck_error(&self, jid: &str, expected: &Value) -> Result<()> {
        // No queue/ToDo lock is held after the failed transaction. Only publish
        // a private technical cooldown if this is still the same queued attempt.
        let _state = self.job_lock(jid, "state.lock")?;
        let mut fresh = self.load(jid)?;
        if !crate::pool_retry::same_queued_attempt(expected, &fresh) {
            return Ok(());
        }
        fresh["status"] = json!("failed");
        fresh["last_error"] = fresh["capacity_wait_error"].clone();
        if !self.defer_capacity_lock(&mut fresh) {
            fresh["capacity_exhausted"] = json!(true);
            fresh["finished_at"] = json!(self.rt.now());
            fresh["capacity_wait_pending"] = json!(false);
            fresh["capacity_retry_ended"] = json!("capacity_recheck_lock_wait_exhausted");
            fresh["last_error"] =
                json!("capacity recheck unavailable after the finite technical wait budget");
            fresh["retry_not_before"] = Value::Null;
        }
        self.save(&fresh)
    }

    pub(crate) fn defer_capacity_lock(&self, job: &mut Value) -> bool {
        if job["capacity_wait_count"].as_u64().unwrap_or(0) >= MAX_WAITS {
            return false;
        }
        let session = normalize_uuid(s(job, "thread_id"))
            .or_else(|| normalize_uuid(s(job, "resume_session")))
            .unwrap_or_default();
        self.wait_capacity(job, &session, self.rt.epoch())
    }
    pub(crate) fn end_capacity_owner(&self, job: &mut Value) {
        // The authoritative ToDo keeps its stop/dependencies and worktree.
        // A drained, superseded worker must release its active registration.
        job["status"] = json!("failed");
        job["finished_at"] = json!(self.rt.now());
        job["capacity_owner_recheck"] = json!(false);
        job["capacity_wait_pending"] = json!(false);
        job["capacity_retry_ended"] = json!("todo_owner_or_generation_changed");
        job["last_error"] = json!(
            "capacity retry ended because ToDo ownership, generation or execution conditions changed"
        );
        job["retry_not_before"] = Value::Null;
        job["activity"] =
            json!("元のToDoの担当・停止・依存関係が変わったため、この実行の再試行を終了");
    }
    fn review_provenance(&self, job: &Value) -> Result<(Vec<String>, Vec<String>)> {
        let mut authors = Vec::new();
        let mut sources: Vec<String> = job["review_source_jobs"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(|v| v.as_str().map(str::to_owned))
            .collect();
        let explicit_sources = !sources.is_empty();
        for id in [
            s(&job["subwork"], "origin_job_id"),
            s(&job["fixed_read_only_review"], "parent_job_id"),
            s(&job["fixed_review_requalification"], "parent_job_id"),
        ] {
            if !explicit_sources && !id.is_empty() {
                sources.push(id.into());
            }
        }
        sources.sort();
        sources.dedup();
        if job["review_source_verified"] == true && !sources.is_empty() {
            let saved = job
                .get("review_author_models")
                .unwrap_or(&job["review_excluded_models"]);
            authors = saved
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|value| value.as_str().map(str::to_owned))
                .collect();
            if !authors.is_empty() {
                return Ok((sources, authors));
            }
        }
        for id in &sources {
            if !crate::queue::valid_job_id(id) {
                return Err(Error::new("invalid review source job identity"));
            }
            let source = self.load(id)?;
            if s(&source, "id") != id {
                return Err(Error::new("review source job identity changed"));
            }
            authors.extend(cc_job_policy::author_models(&source));
        }
        authors.sort();
        authors.dedup();
        Ok((sources, authors))
    }
    /// Durable source metadata survives the end of the live parent relationship.
    pub(crate) fn validate_review_launch(&self, job: &mut Value, verified_quota: bool) -> Result<()> {
        if !review_job(job) {
            return Ok(());
        }
        let (sources, authors) = self.review_provenance(job)?;
        let mut excluded = review_exclusions(job, &authors);
        // The launcher may also be a writer, but parenthood alone is not
        // provenance. Use it only as a routing exclusion when no author is known.
        if sources.is_empty()
            && !s(job, "parent_job_id").is_empty()
            && let Ok(parent) = self.load(s(job, "parent_job_id"))
        {
            let mut routing = job.clone();
            routing["review_excluded_models"] = json!(excluded);
            if !s(&parent, "model").is_empty() {
                excluded = review_exclusions(&routing, &[s(&parent, "model").to_owned()]);
            }
        }
        let managed =
            job["todo_id"].as_u64().is_some_and(|id| id > 0) || !s(job, "launch_key").is_empty();
        let owner_choice = job["model_origin"] == "owner_request"
            || (!managed && s(job, "parent_job_id").is_empty() && job["model_explicit"] != false);
        let author_conflict = authors
            .iter()
            .any(|author| same_model(author, s(job, "model")));
        if author_conflict && owner_choice {
            return Err(Error::new(
                "independent review requires a model different from the source author",
            ));
        }
        let mut excluded_models: Vec<String> = excluded
            .iter()
            .filter_map(|value| value.as_str().map(str::to_owned))
            .collect();
        // A model that just failed is unavailable for this transition, not a
        // source author. Use the hint only for selection, without persisting
        // it in either durable review exclusion or authorship metadata.
        if let Some(failed) = job["model_recovery_from"].as_str()
            && !cc_job_policy::launch_context::fixed_execution_contract(job)
            && !excluded_models
                .iter()
                .any(|model| same_model(model, failed))
        {
            excluded_models.push(failed.to_owned());
        }
        let routing_conflict = excluded_models
            .iter()
            .any(|model| same_model(model, s(job, "model")));
        let default_writer = ["gpt-6.1-sol", "gpt-6-luna", "gpt-6-astra"]
            .iter()
            .any(|model| same_model(s(job, "model"), model));
        if verified_quota && (author_conflict || routing_conflict) {
            return Err(Error::new("quota recovery requires an independent available review model"));
        }
        if !owner_choice && !verified_quota && (author_conflict || routing_conflict || default_writer) {
            let profile = cc_job_policy::review_profile(&excluded_models)
                .ok_or_else(|| Error::new("no independent review model is available"))?;
            job["model"] = json!(profile.model);
            job["requested_model"] = job["model"].clone();
            job["model_origin"] = json!("policy");
        }
        job["review_author_models"] = json!(authors);
        job["review_excluded_models"] = json!(excluded);
        job["review_source_jobs"] = json!(sources);
        job["review_source_verified"] = json!(!authors.is_empty());
        Ok(())
    }
    /// Query before taking queue/state locks. Persist only safe provider metadata.
    pub fn capacity_inventory(&self, job: &Value) -> Value {
        let at = self.rt.epoch();
        let mut inventory = json!({"at":at});
        for provider in ["openai", "anthropic"] {
            let q = self.rt.queue_quota_sample(provider, at, 15);
            inventory[provider] = json!(
                q.accounts
                    .iter()
                    .any(|a| a.reset_at > at && a.used_percent < 100. && !a.short_exhausted)
            );
            inventory[format!("{provider}_at")] = json!(self.rt.epoch());
        }
        if crate::pool_retry::native_model(s(job, "model")).is_some() {
            inventory["pool"] = self.rt.capacity_pool_sample(job, self.rt.epoch());
        }
        inventory["at"] = json!(self.rt.epoch());
        inventory
    }

    /// A due cooldown rechecks availability; it never starts a depleted model.
    pub fn recheck_capacity_wait(&self, job: &mut Value) -> bool {
        if s(job, "status") != "queued"
            || job["capacity_wait_pending"] != true
            || job["cancel_requested"] == true
        {
            return false;
        }
        if job["retry_not_before"]
            .as_f64()
            .is_some_and(|at| at > self.rt.epoch())
        {
            return false;
        }
        job["status"] = json!("failed");
        job["last_error"] = job["capacity_wait_error"].clone();
        job["last_failure_kind"] = json!("capacity");
        self.retry_capacity(job)
    }
    /// The caller has drained the exited child and holds the job's state lock.
    /// Preserve permissions, prompt, working directory and ToDo ownership.
    pub fn retry_capacity(&self, job: &mut Value) -> bool {
        if s(job, "status") != "failed"
            || !is_capacity(s(job, "last_error"))
            || s(job, "last_failure_kind") != "capacity"
            || job["cancel_requested"] == true
        {
            return false;
        }
        let stop = |reason: &str| {
            self.rt
                .diagnostic(&format!("モデルの混雑による自動再開を終了: {reason}"));
            false
        };
        let count = job["capacity_retry_count"].as_u64().unwrap_or(0);
        if count >= MAX_SWITCHES {
            job["capacity_exhausted"] = json!(true);
            return stop("切り替えが最大3回に達しました");
        }
        if provider(s(job, "model")).is_none() {
            return stop("指定モデルは切り替えの対象外です");
        }
        let session = ["thread_id", "resume_session"]
            .iter()
            .find_map(|key| normalize_uuid(s(job, key)));
        if session.is_none()
            && ["thread_id", "resume_session"]
                .iter()
                .any(|key| !s(job, key).is_empty())
        {
            return stop("有効なセッションIDがありません");
        }
        let session = session.unwrap_or_default();
        if !job["capacity_history"].is_null() && !job["capacity_history"].is_array() {
            return stop("保存された切り替え履歴の形式が不正です");
        }
        // The worker has exited. Do not discard a still-live identity, including
        // an AccessDenied inspection, and accidentally start a second worker.
        if let Some(pid) = job["codex_pid"]
            .as_u64()
            .and_then(|p| u32::try_from(p).ok())
        {
            match self.rt.proc_created_checked(pid) {
                Ok(None) => (),
                Ok(Some(at))
                    if job["codex_pid_created"]
                        .as_u64()
                        .is_some_and(|old| old != at) => {}
                _ => return stop("元の子処理の終了を確認できません"),
            }
        }
        let at = self.rt.epoch();
        // A review must remain different from the model that wrote the source.
        // Read only its persisted model metadata, never copy the parent's prompt.
        let mut selection = job.clone();
        if review_job(job)
            && let Ok((sources, authors)) = self.review_provenance(job)
        {
            selection["review_source_jobs"] = json!(sources);
            selection["review_source_verified"] = json!(!authors.is_empty());
            let excluded = review_exclusions(&selection, &authors);
            selection["review_excluded_models"] = json!(excluded);
            selection["review_author_models"] = json!(authors);
        }
        // An older incorrectly routed reviewer cannot acquire a second attempt
        // on its author's model, including the provider-qualified spelling.
        if review_job(job)
            && selection["review_author_models"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|v| same_model(s_value(v), s(job, "model")))
        {
            job["capacity_exhausted"] = json!(true);
            return stop("実装担当と同じモデルではレビューを再開できません");
        }
        let same_count = job["capacity_same_model_retry_count"].as_u64().unwrap_or(0);
        let observed_same = job["capacity_history"]
            .as_array()
            .into_iter()
            .flatten()
            .filter(|entry| s(entry, "kind") == "same_model_account_retry")
            .count() as u64;
        if (!job["capacity_same_model_retry_count"].is_null()
            && job["capacity_same_model_retry_count"].as_u64().is_none())
            || same_count > crate::pool_retry::MAX_SAME_MODEL_RETRIES
            || same_count < observed_same
        {
            job["capacity_exhausted"] = json!(true);
            return stop("保存された同モデル再試行回数が不正です");
        }
        if crate::pool_retry::native_model(s(job, "model")).is_some()
            && same_count < crate::pool_retry::MAX_SAME_MODEL_RETRIES
            && !s(job, "requested_model").contains("haiku")
        {
            let pool = if job["capacity_inventory"]["pool"].is_object() {
                job["capacity_inventory"]["pool"].clone()
            } else {
                self.rt.capacity_pool_sample(job, at)
            };
            match crate::pool_retry::next_selector(job, &pool, at) {
                Some(Some(selector)) => {
                    let tried = crate::pool_retry::tried_accounts(job, &pool, &selector);
                    let deadline = at + crate::pool_retry::RETRY_DELAYS[same_count as usize];
                    let entry = json!({"kind":"same_model_account_retry","attempt":same_count+1,
                        "at":self.rt.now(),"session":session,"from_model":job["model"],"to_model":job["model"],
                        "account_selector":selector,"failure_kind":"capacity","requested_effort":cc_job_policy::effective_effort(job),
                        "requested_tier":crate::pool_retry::requested_tier(s(job,"model")),"observation":pool["observation"],"retry_not_before":deadline});
                    if job["capacity_history"].is_null() {
                        job["capacity_history"] = json!([]);
                    }
                    job["capacity_history"].as_array_mut().unwrap().push(entry);
                    job["capacity_same_model_retry_count"] = json!(same_count + 1);
                    job["capacity_tried_accounts"] = json!(tried);
                    let mut labels: Vec<_> = pool["failed_labels"]
                        .as_array()
                        .cloned()
                        .unwrap_or_default();
                    if let Some(label) = pool["public_labels"][&selector]
                        .as_str()
                        .filter(|label| crate::pool_retry::public_selector(label))
                    {
                        labels.push(json!(label));
                    }
                    job["capacity_tried_labels"] = json!(labels);
                    job["capacity_account_selector"] = json!(selector);
                    if selection["review_excluded_models"].is_array() {
                        job["review_excluded_models"] = selection["review_excluded_models"].clone();
                        job["review_source_jobs"] = selection["review_source_jobs"].clone();
                        job["review_source_verified"] = selection["review_source_verified"].clone();
                        job["review_author_models"] = selection["review_author_models"].clone();
                    }
                    job["capacity_wait_error"] = job["last_error"].clone();
                    prepare_resume(job, &session, &self.rt.now());
                    job["capacity_wait_pending"] = json!(false);
                    job["capacity_wait_error"] = Value::Null;
                    job["last_failure_kind"] = Value::Null;
                    job["capacity_error"] = Value::Null;
                    job["retry_not_before"] = json!(deadline);
                    job["activity"] = json!(format!(
                        "モデルの混雑: 同じモデルの別OAuth枠で再試行（{}回目、最大3回）",
                        same_count + 1
                    ));
                    self.rt.diagnostic(s(job, "activity"));
                    return true;
                }
                None if job["capacity_wait_count"].as_u64().unwrap_or(0) < MAX_WAITS => {
                    // Missing attribution cannot prove a different account. Use
                    // the persisted ordinary cooldown, never a blind repeat.
                    return self.wait_capacity(job, &session, at);
                }
                _ => (), // Verified alternatives exhausted, or finite metadata waits spent.
            }
        }
        if review_job(job) && selection["review_source_verified"] != true {
            job["capacity_exhausted"] = json!(true);
            return stop("モデルの切替に必要なレビューの実装担当を確認できません");
        }
        let mut snapshots = std::collections::HashMap::new();
        let next = if count < MAX_SWITCHES {
            candidates(&selection).into_iter().find(|model| {
                let provider = provider(model).unwrap();
                if job["capacity_inventory"].is_object() {
                    let checked = job["capacity_inventory"][format!("{provider}_at")]
                        .as_f64()
                        .or_else(|| job["capacity_inventory"]["at"].as_f64());
                    return checked.is_some_and(|checked| at >= checked && at - checked <= 60.)
                        && job["capacity_inventory"][provider] == true;
                }
                let q = snapshots
                    .entry(provider)
                    .or_insert_with(|| self.rt.queue_quota_sample(provider, at, 15));
                // One verified eligible OAuth account suffices even when a
                // different account's quota is unavailable. Never guess a quota.
                q.accounts
                    .iter()
                    .any(|a| a.reset_at > at && a.used_percent < 100. && !a.short_exhausted)
            })
        } else {
            None
        };
        let Some(next) = next else {
            let waits = job["capacity_wait_count"].as_u64().unwrap_or(0);
            if waits >= MAX_WAITS || candidates(&selection).is_empty() {
                job["capacity_exhausted"] = json!(true);
                return stop("有限の候補と待機回数を使い切りました");
            }
            return self.wait_capacity(job, &session, at);
        };
        let now = self.rt.now();
        let entry = json!({
            "attempt": count + 1, "at": now, "session": session,
            "from_model": job["model"], "to_model": next,
            "from_provider":provider(s(job,"model")), "to_provider":provider(next),
            "failure_kind":"capacity", "requested_effort":"max"
        });
        if job["capacity_history"].is_null() {
            job["capacity_history"] = json!([]);
        }
        job["capacity_history"].as_array_mut().unwrap().push(entry);
        job["capacity_retry_count"] = json!(count + 1);
        if selection["review_excluded_models"].is_array() {
            job["review_excluded_models"] = selection["review_excluded_models"].clone();
            job["review_source_jobs"] = selection["review_source_jobs"].clone();
            job["review_source_verified"] = selection["review_source_verified"].clone();
            job["review_author_models"] = selection["review_author_models"].clone();
        }
        if s(job, "requested_model").is_empty() {
            job["requested_model"] = job["model"].clone();
        }
        job["model"] = json!(next);
        // Fallback effort is a policy decision, not the source model's default.
        // Explicit effort on the original model was retained by account retries.
        job["effort"] = json!("max");
        job["model_origin"] = json!("policy");
        job["effort_origin"] = json!("policy");
        job["capacity_account_selector"] = Value::Null;
        prepare_resume(job, &session, &now);
        job["last_failure_kind"] = Value::Null;
        job["capacity_error"] = Value::Null;
        job["capacity_wait_pending"] = json!(false);
        job["capacity_wait_error"] = Value::Null;
        job["resource_wait"] = Value::Null;
        job["retry_not_before"] = Value::Null;
        job["activity"] = json!(format!(
            "順番待ち: モデルの混雑により{next}で再開（{}回目、最大3回）",
            count + 1
        ));
        self.rt.diagnostic(s(job, "activity"));
        true
    }

    fn wait_capacity(&self, job: &mut Value, session: &str, at: f64) -> bool {
        let waits = job["capacity_wait_count"].as_u64().unwrap_or(0);
        if !s(job, "started_at").is_empty() {
            job["capacity_failed_started_at"] = job["started_at"].clone();
        }
        job["capacity_wait_error"] = job["last_error"].clone();
        prepare_resume(job, session, &self.rt.now());
        job["capacity_wait_pending"] = json!(true);
        job["capacity_wait_count"] = json!(waits + 1);
        job["retry_not_before"] = json!(at + 900.);
        job["activity"] = json!(format!(
            "モデルの混雑: 15分後に既存OAuth枠を再確認（{}回目、最大3回）",
            waits + 1
        ));
        true
    }
}
