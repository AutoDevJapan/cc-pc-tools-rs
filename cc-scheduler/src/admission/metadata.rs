//! Strict projection: unknown fields (including note/history/Source) use IgnoredAny.
use super::inputs::{self, Kind};
use super::*;
use serde::de::{DeserializeSeed, IgnoredAny, MapAccess, SeqAccess, Visitor};
use std::fmt;
use std::io::BufRead;
use std::sync::LazyLock;
static INPUT_PATHS: LazyLock<Vec<String>> = LazyLock::new(inputs::paths);
const STORE_LIMIT: u64 = 64 * 1024 * 1024;
const PATHS: &[&str] = &[
    "id",
    "project",
    "status",
    "dispatchable",
    "spec",
    "created_s",
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
    "scheduler.launch_input.launch_key",
    "scheduler.launch_input.spec_sha256",
    "scheduler.launch_input.dispatch_sha256",
    "scheduler.launch_outcome_deferred.reason",
    "scheduler.launch_outcome_deferred.job",
    "scheduler.launch_outcome_deferred.launch_key",
    "scheduler.launch_outcome_deferred.request_fingerprint",
    "scheduler.launch_outcome_deferred.job_state_fingerprint",
    "scheduler.launch_outcome_deferred.receipt_id",
    "scheduler.launch_outcome_replay.job",
    "scheduler.launch_outcome_replay.launch_key",
    "scheduler.launch_outcome_replay.request_fingerprint",
    "scheduler.launch_outcome_replay.job_state_fingerprint",
    "scheduler.launch_outcome_replay.count",
    "scheduler.launch_outcome_replay.first_at",
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
    PATHS
        .iter()
        .copied()
        .chain(INPUT_PATHS.iter().map(String::as_str))
        .any(|p| {
            p.strip_prefix(path)
                .is_some_and(|rest| rest.starts_with('.'))
        })
}
pub(super) fn project(item: &Value) -> Value {
    // The exact same visitor handles the already-loaded normal row and the
    // read-only stream. &Value borrows; it never clones note/Source bodies.
    Project("")
        .deserialize(item)
        .unwrap_or_else(|_| json!({"$unsupported":true}))
}
fn non_object(truth: bool) -> Value {
    json!({"$shape":"non_object","$truth":truth})
}
fn body_key(key: &str) -> bool {
    let key = key.to_ascii_lowercase();
    [
        "prompt",
        "title",
        "name",
        "note",
        "source",
        "body",
        "text",
        "content",
        "message",
        "owner_instruction",
        "notes_hint",
        "command",
        "command_line",
        "commandline",
        "token",
        "password",
        "secret",
        "api_key",
        "credentials",
        "auth",
        "env",
        "headers",
    ]
    .iter()
    .any(|name| key == *name)
        || ["password", "token", "secret", "credential"]
            .iter()
            .any(|part| key.contains(part))
}
fn control_key(key: &str) -> bool {
    const KEYS: &[&str] = &[
        "schema",
        "version",
        "domain",
        "id",
        "todo_id",
        "job",
        "job_id",
        "source_job",
        "source_jobs",
        "request_id",
        "request",
        "key",
        "launch_key",
        "parent_todo_id",
        "origin_todo_id",
        "actor_todo_id",
        "origin_job_id",
        "kind",
        "status",
        "phase",
        "reason",
        "at",
        "until",
        "not_before",
        "deadline",
        "expires_at",
        "count",
        "attempt",
        "generation",
        "resume_generation",
        "verified",
        "repository",
        "git_common",
        "worktree",
        "branch",
        "head",
        "pr",
        "path",
        "root",
        "registered_root",
        "spec",
        "inputs",
        "options",
        "checks",
        "receipt",
        "allocation",
        "model",
        "effort",
        "model_origin",
        "effort_origin",
        "sandbox",
        "mcp_profile",
        "network",
        "add_dirs",
        "review_source_jobs",
        "task_type",
        "role",
        "needs",
        "execution",
        "author",
        "author_job",
        "source_model",
        "source_head",
        "expected_head",
        "expected_branch",
        "owner",
        "repository_id",
        "owner_id",
        "product_id",
        "current",
        "target",
        "operation",
        "original_registry_recovered",
        "source_kind",
        "source_session",
        "scope",
        "stop_kind",
        "source_finished_at",
        "worktree_matches",
        "branch_matches",
        "root_matches",
        "input_valid",
        "prepared",
        "accepted",
        "allowed",
        "read_only",
        "cwd",
        "resume_session",
    ];
    KEYS.contains(&key)
        || PATHS.iter().any(|p| p.rsplit('.').next() == Some(key))
        || inputs::LAUNCH
            .iter()
            .chain(inputs::REVIEW)
            .any(|(k, _, _)| *k == key)
        || ["_sha256", "_id", "_at", "_count"]
            .iter()
            .any(|suffix| key.ends_with(suffix))
}
struct TruthValue;
impl<'de> DeserializeSeed<'de> for TruthValue {
    type Value = Value;
    fn deserialize<D: serde::Deserializer<'de>>(
        self,
        d: D,
    ) -> std::result::Result<Value, D::Error> {
        d.deserialize_any(self)
    }
}
impl<'de> Visitor<'de> for TruthValue {
    type Value = Value;
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("presence only")
    }
    fn visit_unit<E: serde::de::Error>(self) -> std::result::Result<Value, E> {
        Ok(json!(false))
    }
    fn visit_bool<E: serde::de::Error>(self, v: bool) -> std::result::Result<Value, E> {
        Ok(json!(v))
    }
    fn visit_i64<E: serde::de::Error>(self, v: i64) -> std::result::Result<Value, E> {
        Ok(json!(v != 0))
    }
    fn visit_u64<E: serde::de::Error>(self, v: u64) -> std::result::Result<Value, E> {
        Ok(json!(v != 0))
    }
    fn visit_f64<E: serde::de::Error>(self, v: f64) -> std::result::Result<Value, E> {
        Ok(json!(v != 0.))
    }
    fn visit_str<E: serde::de::Error>(self, v: &str) -> std::result::Result<Value, E> {
        Ok(json!(!v.is_empty()))
    }
    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> std::result::Result<Value, A::Error> {
        let mut any = false;
        while seq.next_element::<IgnoredAny>()?.is_some() {
            any = true;
        }
        Ok(json!(any))
    }
    fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> std::result::Result<Value, A::Error> {
        let mut any = false;
        while map.next_entry::<IgnoredAny, IgnoredAny>()?.is_some() {
            any = true;
        }
        Ok(json!(any))
    }
}
struct Control;
impl<'de> DeserializeSeed<'de> for Control {
    type Value = Value;
    fn deserialize<D: serde::Deserializer<'de>>(
        self,
        d: D,
    ) -> std::result::Result<Value, D::Error> {
        d.deserialize_any(self)
    }
}
impl<'de> Visitor<'de> for Control {
    type Value = Value;
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("control projection")
    }
    fn visit_unit<E: serde::de::Error>(self) -> std::result::Result<Value, E> {
        Leaf.visit_unit()
    }
    fn visit_bool<E: serde::de::Error>(self, v: bool) -> std::result::Result<Value, E> {
        Leaf.visit_bool(v)
    }
    fn visit_i64<E: serde::de::Error>(self, v: i64) -> std::result::Result<Value, E> {
        Leaf.visit_i64(v)
    }
    fn visit_u64<E: serde::de::Error>(self, v: u64) -> std::result::Result<Value, E> {
        Leaf.visit_u64(v)
    }
    fn visit_f64<E: serde::de::Error>(self, v: f64) -> std::result::Result<Value, E> {
        Leaf.visit_f64(v)
    }
    fn visit_str<E: serde::de::Error>(self, v: &str) -> std::result::Result<Value, E> {
        Leaf.visit_str(v)
    }
    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> std::result::Result<Value, A::Error> {
        let mut a = vec![];
        while let Some(v) = seq.next_element_seed(Control)? {
            a.push(v);
        }
        Ok(json!(a))
    }
    fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> std::result::Result<Value, A::Error> {
        let mut out = BTreeMap::new();
        let mut any = false;
        while let Some(key) = map.next_key::<String>()? {
            any = true;
            let v = if body_key(&key) {
                Some(map.next_value_seed(TruthValue)?)
            } else if control_key(&key) {
                Some(map.next_value_seed(Control)?)
            } else {
                map.next_value::<IgnoredAny>()?;
                out.insert("$unsupported".to_string(), json!(true));
                None
            };
            if let Some(v) = v {
                if out.insert(key, v).is_some() {
                    return Err(serde::de::Error::custom("duplicate control key"));
                }
            }
        }
        out.insert("$shape".into(), json!("object"));
        out.insert("$truth".into(), json!(any));
        serde_json::to_value(out).map_err(serde::de::Error::custom)
    }
}
pub(crate) fn fingerprint(item: &Value, root: &Value) -> String {
    let root = if root.is_string() || root.is_null() {
        root.clone()
    } else {
        json!({"unsupported_root":true})
    };
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
        Ok(Value::Null)
    }
    fn visit_bool<E: serde::de::Error>(self, v: bool) -> std::result::Result<Value, E> {
        Ok(non_object(v))
    }
    fn visit_i64<E: serde::de::Error>(self, v: i64) -> std::result::Result<Value, E> {
        Ok(non_object(v != 0))
    }
    fn visit_u64<E: serde::de::Error>(self, v: u64) -> std::result::Result<Value, E> {
        Ok(non_object(v != 0))
    }
    fn visit_f64<E: serde::de::Error>(self, v: f64) -> std::result::Result<Value, E> {
        Ok(non_object(v != 0.))
    }
    fn visit_str<E: serde::de::Error>(self, v: &str) -> std::result::Result<Value, E> {
        Ok(non_object(!v.is_empty()))
    }
    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> std::result::Result<Value, A::Error> {
        let mut any = false;
        while seq.next_element::<IgnoredAny>()?.is_some() {
            any = true;
        }
        Ok(non_object(any))
    }
    fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> std::result::Result<Value, A::Error> {
        let mut out = BTreeMap::new();
        let mut any = false;
        while let Some(key) = map.next_key::<String>()? {
            any = true;
            let p = path(self.0, &key);
            let value = if inputs::kind(&p) == Some(Kind::Truth) {
                Some(map.next_value_seed(TruthValue)?)
            } else if inputs::kind(&p) == Some(Kind::Tree) {
                Some(map.next_value_seed(Control)?)
            } else if PATHS.contains(&p.as_str()) || inputs::kind(&p) == Some(Kind::Scalar) {
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
        out.insert("$shape".into(), json!("object"));
        out.insert("$truth".into(), json!(any));
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
    fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> std::result::Result<Value, A::Error> {
        // Recovered/unknown references on OTHER tasks must not abort a query.
        // Never deserialize their nested fields or silently equate them to null.
        while map.next_entry::<IgnoredAny, IgnoredAny>()?.is_some() {}
        Ok(json!({}))
    }
}
fn unsupported(item: &Value, prefix: &str) -> bool {
    fn invalid_leaf(value: &Value) -> bool {
        value.is_object() || value.as_array().is_some_and(|a| a.iter().any(invalid_leaf))
    }
    fn marked(value: &Value) -> bool {
        value.get("$unsupported") == Some(&Value::Bool(true))
            || value.as_array().is_some_and(|a| a.iter().any(marked))
            || value.as_object().is_some_and(|m| m.values().any(marked))
    }
    item.as_object().is_some_and(|fields| {
        fields.iter().any(|(key, value)| {
            let p = path(prefix, key);
            if inputs::kind(&p) == Some(Kind::Tree) {
                marked(value)
            } else if inputs::kind(&p) == Some(Kind::Truth) {
                false
            } else if PATHS.contains(&p.as_str()) || inputs::kind(&p) == Some(Kind::Scalar) {
                invalid_leaf(value)
            } else {
                unsupported(value, &p)
            }
        })
    })
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
struct Projects;
impl<'de> DeserializeSeed<'de> for Projects {
    type Value = BTreeMap<String, Value>;
    fn deserialize<D: serde::Deserializer<'de>>(
        self,
        d: D,
    ) -> std::result::Result<Self::Value, D::Error> {
        d.deserialize_map(self)
    }
}
impl<'de> Visitor<'de> for Projects {
    type Value = BTreeMap<String, Value>;
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("registered roots")
    }
    fn visit_map<A: MapAccess<'de>>(
        self,
        mut map: A,
    ) -> std::result::Result<Self::Value, A::Error> {
        let mut out = BTreeMap::new();
        while let Some(key) = map.next_key::<String>()? {
            let value = map.next_value_seed(Leaf)?;
            if out.insert(key, value).is_some() {
                return Err(serde::de::Error::custom("duplicate project"));
            }
        }
        Ok(out)
    }
}
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
                "projects" if projects.is_none() => projects = Some(map.next_value_seed(Projects)?),
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
        if unsupported(&item, "") {
            return Err(serde::de::Error::custom(
                "unsupported selected control shape",
            ));
        }
        let root = projects
            .and_then(|p| p.get(s(&item["project"])).cloned())
            .unwrap_or(Value::Null);
        if !root.is_string() && !root.is_null() {
            return Err(serde::de::Error::custom("unsupported root"));
        }
        Ok((item, root))
    }
}
pub(crate) fn read(todo: &Path, id: i64) -> Result<(Value, Value)> {
    let file = fs::File::open(todo)?;
    if file.metadata()?.len() > STORE_LIMIT {
        return Err(Error("MetadataTooLarge"));
    }
    let mut reader = std::io::BufReader::new(file.take(STORE_LIMIT + 1));
    if reader.fill_buf()?.starts_with(&[0xef, 0xbb, 0xbf]) {
        reader.consume(3);
    }
    let mut de = serde_json::Deserializer::from_reader(reader);
    let value = Store(id).deserialize(&mut de)?;
    de.end()?;
    Ok(value)
}
