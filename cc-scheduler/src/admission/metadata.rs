//! Strict projection: unknown fields (including note/history/Source) use IgnoredAny.
use super::*;
use serde::de::{DeserializeSeed, IgnoredAny, MapAccess, SeqAccess, Visitor};
use std::fmt;
const STORE_LIMIT: u64 = 64 * 1024 * 1024;
const PATHS: &[&str] = &[
    "id",
    "project",
    "status",
    "dispatchable",
    "spec",
    "created_s",
    "updated",
    "needs",
    "jobs",
    "wait_on",
    "work_wait_on",
    "resume_generation",
    "model",
    "effort",
    "model_origin",
    "effort_origin",
    "owner_project",
    "department",
    "product_id",
    "repository_id",
    "task_type",
    "role",
    "owner_stopped",
    "stopped",
    "cancel_requested",
    "owner_postponement_recorded",
    "stop_kind",
    "repository_owner_id",
    "product_identity.repository_id",
    "product_identity.owner_id",
    "product_identity.product_id",
    "scheduler.job",
    "scheduler.last_job",
    "scheduler.key",
    "scheduler.phase",
    "scheduler.not_before",
    "scheduler.quota_recheck_after",
    "scheduler.question_close_pending",
    "scheduler.owner_stop_job",
    "scheduler.pending_cancel",
    "scheduler.resume",
    "scheduler.attempt",
    "scheduler.worktree",
    "scheduler.branch",
    "scheduler.resource_wait.reason",
    "scheduler.resource_wait.until",
    "scheduler.quota_retry_terminal.source_job",
    "scheduler.quota_retry_terminal.at",
    "scheduler.preparation_failure.reason",
    "scheduler.preparation_failure.at",
    "scheduler.preparation_failure.retry_at",
    "scheduler.review_preparation_failure.exhausted",
    "scheduler.review_preparation_failure.count",
    "scheduler.review_preparation_failure.reason",
    "scheduler.review_preparation_failure.inputs.spec",
    "scheduler.review_preparation_failure.inputs.model",
    "scheduler.review_preparation_failure.inputs.effort",
    "scheduler.review_preparation_failure.inputs.model_origin",
    "scheduler.review_preparation_failure.inputs.effort_origin",
    "scheduler.review_preparation_failure.inputs.options.model",
    "scheduler.review_preparation_failure.inputs.options.effort",
    "stall_recovery.pending.job",
    "stall_recovery.pending.key",
    "subwork.parent_todo_id",
    "subwork.kind",
    "worker_receipt.job",
    "worker_receipt.generation",
    "wait_ready.condition",
    "wait_ready.next_check_at",
    "wait_ready.not_before",
    "wait_ready.since",
    "wait_ready.kind",
];
fn path(prefix: &str, key: &str) -> String {
    if prefix.is_empty() {
        key.into()
    } else {
        format!("{prefix}.{key}")
    }
}
fn branch(path: &str) -> bool {
    PATHS.iter().any(|p| {
        p.strip_prefix(path)
            .is_some_and(|rest| rest.starts_with('.'))
    })
}
fn leaf(value: &Value) -> Value {
    match value {
        Value::Object(_) => Value::Null,
        Value::Array(values) => Value::Array(values.iter().map(leaf).collect()),
        value => value.clone(),
    }
}
pub(super) fn project(item: &Value) -> Value {
    project_at(item, "")
}
fn project_at(item: &Value, prefix: &str) -> Value {
    let mut out = BTreeMap::new();
    if let Some(fields) = item.as_object() {
        for (key, value) in fields {
            let p = path(prefix, key);
            if PATHS.contains(&p.as_str()) {
                out.insert(key.clone(), leaf(value));
            } else if branch(&p) {
                out.insert(key.clone(), project_at(value, &p));
            }
        }
    }
    serde_json::to_value(out).unwrap()
}
pub(super) fn fingerprint(item: &Value, root: &Value) -> String {
    cc_job_policy::product_retry::digest(
        &serde_json::to_vec(&json!({"metadata":item,"registered_root":root})).unwrap(),
    )
}
struct Project<'a>(&'a str);
impl<'de> DeserializeSeed<'de> for Project<'_> {
    type Value = Value;
    fn deserialize<D: serde::Deserializer<'de>>(
        self,
        d: D,
    ) -> std::result::Result<Value, D::Error> {
        d.deserialize_any(self)
    }
}
impl<'de> Visitor<'de> for Project<'_> {
    type Value = Value;
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("control metadata")
    }
    fn visit_unit<E: serde::de::Error>(self) -> std::result::Result<Value, E> {
        Ok(json!({}))
    }
    fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> std::result::Result<Value, A::Error> {
        let mut out = BTreeMap::new();
        while let Some(key) = map.next_key::<String>()? {
            let p = path(self.0, &key);
            let value = if PATHS.contains(&p.as_str()) {
                Some(map.next_value_seed(Leaf)?)
            } else if branch(&p) {
                Some(map.next_value_seed(Project(&p))?)
            } else {
                map.next_value::<IgnoredAny>()?;
                None
            };
            if let Some(value) = value {
                if out.insert(key, value).is_some() {
                    return Err(serde::de::Error::custom("duplicate control field"));
                }
            }
        }
        serde_json::to_value(out).map_err(serde::de::Error::custom)
    }
}
struct Leaf;
impl<'de> DeserializeSeed<'de> for Leaf {
    type Value = Value;
    fn deserialize<D: serde::Deserializer<'de>>(
        self,
        d: D,
    ) -> std::result::Result<Value, D::Error> {
        d.deserialize_any(self)
    }
}
impl<'de> Visitor<'de> for Leaf {
    type Value = Value;
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("scalar control value")
    }
    fn visit_unit<E: serde::de::Error>(self) -> std::result::Result<Value, E> {
        Ok(Value::Null)
    }
    fn visit_bool<E: serde::de::Error>(self, v: bool) -> std::result::Result<Value, E> {
        Ok(json!(v))
    }
    fn visit_i64<E: serde::de::Error>(self, v: i64) -> std::result::Result<Value, E> {
        Ok(json!(v))
    }
    fn visit_u64<E: serde::de::Error>(self, v: u64) -> std::result::Result<Value, E> {
        Ok(json!(v))
    }
    fn visit_f64<E: serde::de::Error>(self, v: f64) -> std::result::Result<Value, E> {
        Ok(json!(v))
    }
    fn visit_str<E: serde::de::Error>(self, v: &str) -> std::result::Result<Value, E> {
        Ok(json!(v))
    }
    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> std::result::Result<Value, A::Error> {
        let mut values = vec![];
        while let Some(value) = seq.next_element_seed(Leaf)? {
            values.push(value);
        }
        Ok(json!(values))
    }
    // No visit_map: even malformed control fields cannot deserialize arbitrary bodies.
}
struct Items(i64);
impl<'de> DeserializeSeed<'de> for Items {
    type Value = Option<Value>;
    fn deserialize<D: serde::Deserializer<'de>>(
        self,
        d: D,
    ) -> std::result::Result<Self::Value, D::Error> {
        d.deserialize_seq(self)
    }
}
impl<'de> Visitor<'de> for Items {
    type Value = Option<Value>;
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("task array")
    }
    fn visit_seq<A: SeqAccess<'de>>(
        self,
        mut seq: A,
    ) -> std::result::Result<Self::Value, A::Error> {
        let mut found = None;
        while let Some(item) = seq.next_element_seed(Project(""))? {
            if item["id"] == self.0 {
                if found.is_some() {
                    return Err(serde::de::Error::custom("duplicate task"));
                }
                found = Some(item);
            }
        }
        Ok(found)
    }
}
struct Store(i64);
impl<'de> DeserializeSeed<'de> for Store {
    type Value = (Value, Value);
    fn deserialize<D: serde::Deserializer<'de>>(
        self,
        d: D,
    ) -> std::result::Result<Self::Value, D::Error> {
        d.deserialize_map(self)
    }
}
impl<'de> Visitor<'de> for Store {
    type Value = (Value, Value);
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("task store")
    }
    fn visit_map<A: MapAccess<'de>>(
        self,
        mut map: A,
    ) -> std::result::Result<Self::Value, A::Error> {
        let (mut item, mut projects) = (None, None);
        while let Some(key) = map.next_key::<String>()? {
            match key.as_str() {
                "items" if item.is_none() => item = Some(map.next_value_seed(Items(self.0))?),
                "projects" if projects.is_none() => {
                    projects = Some(map.next_value::<BTreeMap<String, String>>()?)
                }
                "items" | "projects" => {
                    return Err(serde::de::Error::custom("duplicate store key"))
                }
                _ => {
                    map.next_value::<IgnoredAny>()?;
                }
            }
        }
        let item = item
            .flatten()
            .ok_or_else(|| serde::de::Error::custom("task missing"))?;
        let root = projects
            .and_then(|p| p.get(s(&item["project"])).cloned())
            .map(Value::String)
            .unwrap_or(Value::Null);
        Ok((item, root))
    }
}
pub(super) fn read(todo: &Path, id: i64) -> Result<(Value, Value)> {
    let file = fs::File::open(todo)?;
    if file.metadata()?.len() > STORE_LIMIT {
        return Err(Error("MetadataTooLarge"));
    }
    let mut de =
        serde_json::Deserializer::from_reader(std::io::BufReader::new(file.take(STORE_LIMIT + 1)));
    let value = Store(id).deserialize(&mut de)?;
    de.end()?;
    Ok(value)
}
