//! Reuse only a recent, successful normal Browser Use attachment for this job.
//! The extension network policy is an opaque normal-runtime result, not an
//! allow policy that a standalone caller is permitted to manufacture.
use crate::*;

const CONTEXT_FILE: &str = "chrome-browser-context.json";
const MAX_CONTEXT: u64 = 128 * 1024;
const MAX_AGE: i64 = 60;

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct BrowserContext {
    domain: String,
    job: String,
    pipe: String,
    instance: String,
    generation: String,
    pub(crate) session: String,
    pub(crate) turn: String,
    recorded_at: i64,
    pub(crate) network_policy: Value,
    session_params: Value,
}

fn policy_shape(v: &Value) -> bool {
    let Some(object) = v.as_object() else {
        return false;
    };
    if object
        .keys()
        .any(|k| k != "constraints" && k != "timeoutMs")
    {
        return false;
    }
    if v.get("timeoutMs")
        .is_some_and(|n| !n.as_u64().is_some_and(|n| (1..=30_000).contains(&n)))
    {
        return false;
    }
    let Some(constraints) = v["constraints"].as_array() else {
        return false;
    };
    // A normal getNetworkPolicy result includes its requirements/config/origin
    // constraints even when each list is empty. An absent result is not [].
    if constraints.is_empty() || constraints.len() > 128 {
        return false;
    }
    let mut count = 0;
    let mut bytes = 0;
    constraints.iter().all(|c| {
        let Some(o) = c.as_object() else { return false };
        if o.len() != 3
            || o.keys()
                .any(|k| !["kind", "mode", "patterns"].contains(&k.as_str()))
            || !matches!(c["kind"].as_str(), Some("domains" | "origins"))
            || !matches!(c["mode"].as_str(), Some("denylist" | "allowlist"))
        {
            return false;
        }
        let Some(patterns) = c["patterns"].as_array() else {
            return false;
        };
        count += patterns.len();
        if count > 4096 {
            return false;
        }
        patterns.iter().all(|p| {
            let Some(s) = p.as_str() else { return false };
            bytes += s.len();
            !s.is_empty() && !s.contains('\0') && count <= 4096 && bytes <= 64 * 1024
        })
    })
}

impl BrowserContext {
    pub(crate) fn params(&self) -> Value {
        self.session_params.clone()
    }

    pub(crate) fn attachment_params(&self, record: &Record) -> io::Result<Value> {
        if self.job != record.job
            || self.session != record.session
            || self.turn != record.turn
            || self.instance != record.instance
            || self.generation != record.generation
        {
            return Err(io::Error::other(
                "normal browser attachment identity differs",
            ));
        }
        let mut params = self.params();
        params["tabId"] = json!(record.tab);
        params["networkPolicy"] = self.network_policy.clone();
        Ok(params)
    }

    pub(crate) fn matches(&self, job: &str, owner: &Value, info: &Value, now: i64) -> bool {
        self.domain == "owner-chrome-normal-context-v1"
            && self.job == job
            && owner["id"] == job
            && owner["thread_id"] == self.session
            && owner["status"] == "running"
            && !self.session.is_empty()
            && !self.turn.is_empty()
            && self.session_params["session_id"] == self.session
            && self.session_params["turn_id"] == self.turn
            && self.session_params["session_context"] == "live"
            && now >= self.recorded_at
            && now - self.recorded_at <= MAX_AGE
            && policy_shape(&self.network_policy)
            && (Record {
                job: job.into(),
                pipe: self.pipe.clone(),
                instance: self.instance.clone(),
                generation: self.generation.clone(),
                session: self.session.clone(),
                turn: self.turn.clone(),
                tab: 1,
                created: now,
            })
            .matches_backend(info)
    }
}

/// Called only for the response to a successful attach already forwarded by
/// this job's normal CUA proxy. Neither arbitrary CLI input nor a failed attach
/// can publish a context. Requirements/config/origin denies and request-header
/// metadata are preserved byte-for-byte as JSON values; never print them.
pub(crate) fn observe_attachment(
    dir: &Path,
    job: &str,
    pipe: &str,
    info: &Value,
    params: &Value,
) -> io::Result<()> {
    let owner: Value = serde_json::from_slice(&fs::read(dir.join("job.json"))?)?;
    let mut session_params = json!({});
    for key in [
        "session_id",
        "turn_id",
        "session_context",
        "request_headers",
        "agent_request_header_enabled",
        "root_session_id",
    ] {
        if let Some(v) = params.get(key) {
            session_params[key] = v.clone();
        }
    }
    let context = BrowserContext {
        domain: "owner-chrome-normal-context-v1".into(),
        job: job.into(),
        pipe: pipe.into(),
        instance: info["metadata"]["extensionInstanceId"]
            .as_str()
            .unwrap_or("")
            .into(),
        generation: info["browserTabMentions"]["generation"]
            .as_str()
            .unwrap_or("")
            .into(),
        session: params["session_id"].as_str().unwrap_or("").into(),
        turn: params["turn_id"].as_str().unwrap_or("").into(),
        recorded_at: epoch(),
        network_policy: params["networkPolicy"].clone(),
        session_params,
    };
    if !context.matches(job, &owner, info, epoch()) {
        return Err(io::Error::other("normal browser context unavailable"));
    }
    lifecycle::atomic_json(&dir.join(CONTEXT_FILE), &context)
}

pub(crate) fn load(dir: &Path, job: &str, info: &Value) -> io::Result<BrowserContext> {
    let owner: Value = serde_json::from_slice(&fs::read(dir.join("job.json"))?)?;
    let path = dir.join(CONTEXT_FILE);
    let metadata = fs::symlink_metadata(&path)?;
    if !metadata.is_file() || metadata.file_type().is_symlink() || metadata.len() > MAX_CONTEXT {
        return Err(io::Error::other("normal browser context unavailable"));
    }
    let context: BrowserContext = serde_json::from_slice(&fs::read(path)?)?;
    if !context.matches(job, &owner, info, epoch()) {
        return Err(io::Error::other("normal browser context unavailable"));
    }
    Ok(context)
}

pub(crate) fn attachment_params(record: &Record, info: &Value) -> io::Result<Value> {
    let dir = Path::new("D:/CC/Tools/codex-jobs/jobs").join(&record.job);
    load(&dir, &record.job, info)?.attachment_params(record)
}

pub(crate) fn invalidate(dir: &Path, session: &Value, turn: &Value) {
    let path = dir.join(CONTEXT_FILE);
    if let Ok(bytes) = fs::read(&path)
        && let Ok(context) = serde_json::from_slice::<BrowserContext>(&bytes)
        && session == &context.session
        && turn == &context.turn
    {
        let _ = fs::remove_file(path);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn normal_policy_is_required_and_never_replaced_by_allow() {
        let policy = json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]},{"kind":"origins","mode":"allowlist","patterns":["https://allowed.invalid"]}]});
        assert!(policy_shape(&policy));
        for bad in [
            Value::Null,
            json!({}),
            json!({"constraints":[]}),
            json!({"constraints":[{"kind":"domains","mode":"allowlist","patterns":[],"extra":true}]}),
            json!({"constraints":[{"kind":"domains","mode":"allowlist","patterns":[1]}]}),
        ] {
            assert!(!policy_shape(&bad));
        }
        assert_eq!(policy["constraints"][0]["patterns"][0], "blocked.invalid");
        assert_eq!(policy["constraints"][1]["mode"], "allowlist");
    }
    #[test]
    fn normal_context_rejects_another_job_session_generation_or_age() {
        let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        let owner = json!({"id":"job","thread_id":"real-session","status":"running"});
        let context = BrowserContext {
            domain: "owner-chrome-normal-context-v1".into(),
            job: "job".into(),
            pipe: "pipe".into(),
            instance: "instance".into(),
            generation: "generation".into(),
            session: "real-session".into(),
            turn: "real-turn".into(),
            recorded_at: 100,
            network_policy: json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]}),
            session_params: json!({"session_id":"real-session","turn_id":"real-turn","session_context":"live"}),
        };
        assert!(context.matches("job", &owner, &info, 100));
        assert!(!context.matches("other", &owner, &info, 100));
        assert!(!context.matches(
            "job",
            &json!({"id":"job","thread_id":"other","status":"running"}),
            &info,
            100
        ));
        let mut changed = info.clone();
        changed["browserTabMentions"]["generation"] = json!("other");
        assert!(!context.matches("job", &owner, &changed, 100));
        assert!(!context.matches("job", &owner, &info, 99));
        assert!(!context.matches("job", &owner, &info, 161));
    }

    #[test]
    fn normal_attachment_keeps_full_policy_and_turn_end_invalidates_it() {
        let root = tempfile::tempdir().unwrap();
        fs::write(
            root.path().join("job.json"),
            json!({"id":"job","thread_id":"session","status":"running"}).to_string(),
        )
        .unwrap();
        let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        let policy = json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]},{"kind":"origins","mode":"denylist","patterns":["https://private.invalid"]}]});
        let params = json!({"session_id":"session","turn_id":"turn","session_context":"live","networkPolicy":policy});
        observe_attachment(root.path(), "job", "pipe", &info, &params).unwrap();
        let saved = load(root.path(), "job", &info).unwrap();
        assert_eq!(saved.network_policy, policy);
        invalidate(root.path(), &json!("session"), &json!("other-turn"));
        assert!(load(root.path(), "job", &info).is_ok());
        invalidate(root.path(), &json!("session"), &json!("turn"));
        assert!(load(root.path(), "job", &info).is_err());
    }
}
