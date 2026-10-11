//! One writer protocol for request ownership and separate owner-question routes.
use crate::core::*;
use serde_json::{json, Value};
use std::{path::PathBuf, time::Duration};

pub(crate) fn request_ids(value: &Value, job: &str) -> Result<Value> {
    if !value.is_object() {
        return Err("invalid Discord thread associations".into());
    }
    let requests = value.get("thread_ids");
    if requests.is_some_and(|ids| !ids.is_array()) {
        return Err("invalid Discord thread associations".into());
    }
    if value.get("question_thread_ids").is_some() || value.get("question_routes").is_some() {
        let ids = value["question_thread_ids"]
            .as_array()
            .ok_or("invalid Discord question associations")?;
        let routes = value["question_routes"]
            .as_object()
            .ok_or("invalid Discord question associations")?;
        let unique: std::collections::BTreeSet<_> = ids.iter().map(s).collect();
        if (ids.is_empty() && requests.is_none())
            || unique.len() != ids.len()
            || routes.len() != ids.len()
            || ids.iter().any(|id| {
                let text = s(id);
                text.is_empty()
                    || !text.bytes().all(|c| c.is_ascii_digit())
                    || routes.get(text).is_none()
                    || value["question_routes"][text]["question_thread_id"] != *id
                    || s(&value["question_routes"][text]["posting_job"]) != job
            })
        {
            return Err("invalid Discord question associations".into());
        }
    } else if requests.is_none() {
        return Err("invalid Discord thread associations".into());
    }
    Ok(requests.cloned().unwrap_or_else(|| json!([])))
}

/// Hold the same job lock through the fresh read, merge, validation and commit.
/// Unknown fields are retained. Question IDs never become request ownership.
pub(crate) struct Store {
    path: PathBuf,
    job: String,
    value: Value,
    _lock: ByteLock,
}
impl Store {
    pub(crate) fn open(ctx: &Context, job: &str) -> Result<Self> {
        if !valid_job(job) {
            return Err("invalid association job".into());
        }
        let dir = ctx.jobs.join("jobs").join(job);
        let lock = ByteLock::acquire(&dir.join("state.lock"), Duration::from_secs(45))?;
        let path = dir.join("discord-threads.json");
        let value = if path.exists() {
            read_json(&path)?
        } else {
            json!({"thread_ids":[]})
        };
        request_ids(&value, job)?;
        Ok(Self {
            path,
            job: job.into(),
            value,
            _lock: lock,
        })
    }
    pub(crate) fn update(&mut self, merge: impl FnOnce(&mut Value) -> Result<()>) -> Result<()> {
        let mut next = self.value.clone();
        merge(&mut next)?;
        request_ids(&next, &self.job)?;
        if next != self.value || !self.path.exists() {
            atomic_json(&self.path, &next, 2)?;
            self.value = next;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn questions_are_validated_with_or_without_request_ids() {
        let mut valid = json!({"question_thread_ids":["700"],"question_routes":{"700":{"question_thread_id":"700","posting_job":"worker"}}});
        assert_eq!(request_ids(&valid, "worker").unwrap(), json!([]));
        for typed in [false, true] {
            let mut value = valid.clone();
            if typed {
                value["thread_ids"] = json!(["100"])
            }
            value["question_routes"]["700"]["posting_job"] = json!("other");
            assert!(request_ids(&value, "worker").is_err());
        }
        valid["thread_ids"] = json!(["100"]);
        assert_eq!(request_ids(&valid, "worker").unwrap(), json!(["100"]));
        valid["question_routes"]["extra"] = json!({});
        assert!(request_ids(&valid, "worker").is_err());
    }
}
