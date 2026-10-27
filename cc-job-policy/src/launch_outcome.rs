//! The effect of this invocation, separate from the existing job's saved state.
use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Clone, Copy, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum Kind {
    ExistingJobNoAction,
    Accepted,
    Queued,
    Unknown,
}

#[derive(Clone, Copy, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum JobState {
    Queued,
    Starting,
    Running,
    Terminal,
    Unknown,
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Outcome {
    pub schema: u32,
    pub kind: Kind,
    pub job_id: String,
    pub launch_key: Option<String>,
    pub todo_id: Option<u64>,
    pub job_resume_generation: Option<u64>,
    pub job_dependency_generation: Option<u64>,
    pub registration_generation: Option<u64>,
    pub registration_attempt: Option<u64>,
    pub dispatch_sha256: Option<String>,
    pub received_sha256: Option<String>,
    pub cwd_sha256: Option<String>,
    pub branch_sha256: Option<String>,
    pub owner_sha256: Option<String>,
    pub job_state: JobState,
    pub queue_restart_pending: Option<bool>,
    pub reboot_launch_pending: Option<bool>,
}

pub fn path_fingerprint(path: &str) -> Option<String> {
    (!path.is_empty()).then(|| {
        let path = path.replace('\\', "/");
        let path = path.trim_start_matches("//?/").trim_end_matches('/');
        let path = if cfg!(windows) {
            path.to_lowercase()
        } else {
            path.to_string()
        };
        crate::product_retry::digest(path.as_bytes())
    })
}

impl Outcome {
    /// No process, prompt, credential, or filesystem inspection.
    pub fn from_job(kind: Kind, job: &Value) -> Self {
        Self {
            schema: 1,
            kind,
            job_id: job["id"].as_str().unwrap_or("").to_string(),
            launch_key: job["launch_key"].as_str().map(str::to_string),
            todo_id: job["todo_id"].as_u64(),
            job_resume_generation: job["todo_resume_generation"].as_u64(),
            job_dependency_generation: job["todo_dependency_generation"].as_u64(),
            registration_generation: job["launch_request_generation"].as_u64(),
            registration_attempt: job["launch_request_attempt"].as_u64(),
            dispatch_sha256: job["launch_dispatch_sha256"].as_str().map(str::to_string),
            received_sha256: job["launch_received_sha256"].as_str().map(str::to_string),
            cwd_sha256: job["cwd"].as_str().and_then(path_fingerprint),
            branch_sha256: job["work_owner"]["branch"]
                .as_str()
                .map(|b| crate::product_retry::digest(b.as_bytes())),
            owner_sha256: job["work_owner"].as_object().map(|_| {
                crate::product_retry::digest(&serde_json::to_vec(&job["work_owner"]).unwrap())
            }),
            job_state: match job["status"].as_str() {
                Some("queued") => JobState::Queued,
                Some("starting") => JobState::Starting,
                Some("running") => JobState::Running,
                Some("succeeded" | "failed" | "cancelled" | "lost") => JobState::Terminal,
                _ => JobState::Unknown,
            },
            queue_restart_pending: job["queue_restart_pending"].as_bool(),
            reboot_launch_pending: job["reboot_launch_pending"].as_bool(),
        }
    }
    pub fn valid(&self) -> bool {
        let hash = |v: &Option<String>, size| {
            v.as_ref().is_none_or(|v| {
                v.len() == size
                    && v.bytes()
                        .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
            })
        };
        self.schema == 1
            && !self.job_id.is_empty()
            && self.job_id.len() <= 128
            && self
                .job_id
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b))
            && hash(&self.launch_key, 32)
            && hash(&self.cwd_sha256, 64)
            && hash(&self.branch_sha256, 64)
            && hash(&self.owner_sha256, 64)
            && hash(&self.dispatch_sha256, 64)
            && hash(&self.received_sha256, 64)
    }
    pub fn terminal_no_action(&self) -> bool {
        self.kind == Kind::ExistingJobNoAction
            && self.job_state == JobState::Terminal
            && self.queue_restart_pending == Some(false)
            && self.reboot_launch_pending == Some(false)
    }
    pub fn reserved(&self) -> bool {
        matches!(
            self.job_state,
            JobState::Queued | JobState::Starting | JobState::Running
        ) || self.queue_restart_pending == Some(true)
            || self.reboot_launch_pending == Some(true)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn pending_requires_a_boolean_and_absence_is_unknown() {
        let mut job = json!({"id":"old","status":"failed","queue_restart_pending":false,"reboot_launch_pending":false});
        assert!(Outcome::from_job(Kind::ExistingJobNoAction, &job).terminal_no_action());
        for value in [
            Value::Null,
            json!("false"),
            json!(0),
            json!({}),
            json!(true),
        ] {
            job["queue_restart_pending"] = value;
            assert!(!Outcome::from_job(Kind::ExistingJobNoAction, &job).terminal_no_action());
        }
        job["queue_restart_pending"] = json!(true);
        assert!(Outcome::from_job(Kind::ExistingJobNoAction, &job).reserved());
    }
}
