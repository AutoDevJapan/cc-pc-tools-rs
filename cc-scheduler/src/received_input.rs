//! Refetch one scheduler input; never produces an owner or asset attestation.
use serde_json::{json, Value};
use std::{
    collections::BTreeSet,
    path::{Path, PathBuf},
};
mod strict;
mod system;
pub use system::SystemHost;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ReadError(pub &'static str);
type Result<T> = std::result::Result<T, ReadError>;
const REQUEST_LIMIT: usize = 64 * 1024;
const BODY_LIMIT: usize = 2 * 1024 * 1024;
const RECORD_LIMIT: usize = 1024 * 1024;

/// Only trusted embedding code supplies a layout. The CLI always uses production().
pub struct Layout {
    pub todo: PathBuf,
    pub specs: PathBuf,
    pub prompts: PathBuf,
    pub jobs: PathBuf,
}
impl Layout {
    pub fn production() -> Self {
        Self {
            todo: "D:/CC/Tools/todo/todo.json".into(),
            specs: "D:/CC/Tools/todo/specs".into(),
            prompts: "D:/CC/Tools/scheduler/prompts".into(),
            jobs: "D:/CC/Tools/codex-jobs/jobs".into(),
        }
    }
}
/// The host reads bounded exact files and applies the existing resource guard.
pub trait Host {
    fn read(&mut self, path: &Path, limit: usize) -> Result<Vec<u8>>;
    fn guard(&mut self, path: &Path) -> Result<bool>;
    fn now(&self) -> String;
}
pub struct Request(Value);
fn s(v: &Value) -> &str {
    v.as_str().unwrap_or("")
}
fn reject<T>(reason: &'static str) -> Result<T> {
    Err(ReadError(reason))
}
fn keys(v: &Value, required: &[&str], optional: &[&str]) -> bool {
    v.as_object().is_some_and(|map| {
        required.iter().all(|k| map.contains_key(*k))
            && map
                .keys()
                .all(|k| required.contains(&k.as_str()) || optional.contains(&k.as_str()))
    })
}
fn id(text: &str) -> bool {
    !text.is_empty()
        && text.len() <= 128
        && text
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b))
}
fn hash(text: &str) -> bool {
    text.len() == 64
        && text
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn absolute(text: &str) -> bool {
    let b = text.as_bytes();
    b.len() > 3
        && b[0].is_ascii_alphabetic()
        && b[1] == b':'
        && matches!(b[2], b'/' | b'\\')
        && text.len() <= 1024
        && !text.chars().any(|c| c.is_control())
        && text[2..]
            .split(['/', '\\'])
            .filter(|v| !v.is_empty())
            .all(|v| v != "." && v != ".." && !v.ends_with(['.', ' ']) && !v.contains(':'))
}
fn path_key(path: &str) -> String {
    path.replace('\\', "/")
        .trim_end_matches('/')
        .to_ascii_lowercase()
}
fn same_path(a: &str, b: &Path) -> bool {
    absolute(a) && path_key(a) == path_key(&b.to_string_lossy())
}
fn spec_name(text: &str, todo: u64) -> bool {
    text.strip_prefix(&format!("{todo}-"))
        .and_then(|s| s.strip_suffix(".md"))
        .is_some_and(|s| s.len() == 32 && s.bytes().all(|b| b.is_ascii_hexdigit()))
}
fn edge_valid(v: &Value) -> bool {
    v.is_null()
        || (keys(v, &["todo_id", "job_id", "origin_job_id", "kind"], &[])
            && v["todo_id"].as_u64().is_some_and(|n| n > 0)
            && (v["job_id"].is_null() || id(s(&v["job_id"])))
            && id(s(&v["origin_job_id"]))
            && ["research", "review", "check"].contains(&s(&v["kind"])))
}
/// Strict bounded JSON. A caller's expected values are comparisons, not authority.
pub fn parse_request(bytes: &[u8]) -> Result<Request> {
    if bytes.len() > REQUEST_LIMIT {
        return reject("input_rejected");
    }
    let v = strict::parse(bytes)?;
    let todo = v["todo_id"]
        .as_u64()
        .filter(|n| *n > 0)
        .ok_or(ReadError("input_rejected"))?;
    if !keys(
        &v,
        &["todo_id", "job_id", "registered_scope", "source_kind"],
        &["expected"],
    ) || !id(s(&v["job_id"]))
        || !absolute(s(&v["registered_scope"]))
        || !["scheduler_dispatch", "scheduler_subwork"].contains(&s(&v["source_kind"]))
    {
        return reject("input_rejected");
    }
    if let Some(e) = v.get("expected") {
        if !keys(
            e,
            &[
                "spec_ref",
                "spec_sha256",
                "dispatch_sha256",
                "received_sha256",
                "binding_sha256",
                "parent_edge",
            ],
            &[],
        ) || !spec_name(s(&e["spec_ref"]), todo)
            || !edge_valid(&e["parent_edge"])
            || [
                "spec_sha256",
                "dispatch_sha256",
                "received_sha256",
                "binding_sha256",
            ]
            .iter()
            .any(|k| !hash(s(&e[*k])))
        {
            return reject("input_rejected");
        }
    }
    Ok(Request(v))
}
fn bytes(host: &mut impl Host, path: &Path, limit: usize) -> Result<Vec<u8>> {
    let bytes = host.read(path, limit)?;
    if bytes.len() > limit {
        return reject("too_large");
    }
    Ok(bytes)
}
fn record(host: &mut impl Host, path: &Path, limit: usize) -> Result<Value> {
    strict::parse(&bytes(host, path, limit)?).map_err(|_| ReadError("record_invalid_json"))
}
fn one(db: &Value, todo: u64) -> Result<&Value> {
    let rows = db["items"].as_array().ok_or(ReadError("invalid_record"))?;
    let mut selected = rows.iter().filter(|r| r["id"].as_u64() == Some(todo));
    let row = selected.next().ok_or(ReadError("not_found"))?;
    if selected.next().is_some() {
        return reject("duplicate_todo");
    }
    Ok(row)
}
fn listed(row: &Value, job: &str) -> bool {
    row["jobs"]
        .as_array()
        .is_some_and(|v| v.iter().filter(|v| **v == job).count() == 1)
}
fn guarded(host: &mut impl Host, path: &str) -> Result<()> {
    if !absolute(path) {
        return reject("scope_mismatch");
    }
    if !host.guard(Path::new(path))? {
        return reject("permission_denied");
    }
    Ok(())
}
fn job(host: &mut impl Host, l: &Layout, job_id: &str) -> Result<Value> {
    if !id(job_id) {
        return reject("invalid_record");
    }
    record(host, &l.jobs.join(job_id).join("job.json"), RECORD_LIMIT)
}
fn parent(
    host: &mut impl Host,
    l: &Layout,
    db: &Value,
    row: &Value,
    j: &Value,
    scope: &str,
) -> Result<(Value, Vec<Value>)> {
    let relation = &row["subwork"];
    if relation.is_null()
        && j["subwork"].is_null()
        && j["parent_job_id"].is_null()
        && j["subwork_owner_job_id"].is_null()
        && j["subwork_validated"] != true
    {
        return Ok((Value::Null, Vec::new()));
    }
    if !relation.is_object()
        || j["subwork"] != *relation
        || j["subwork_validated"] != true
        || j["subwork_owner_job_id"] != j["parent_job_id"]
    {
        return reject("parent_mismatch");
    }
    let parent_id = relation["parent_todo_id"]
        .as_u64()
        .filter(|n| *n > 0 && *n != row["id"].as_u64().unwrap())
        .ok_or(ReadError("parent_mismatch"))?;
    let origin = s(&relation["origin_job_id"]);
    let current = j["parent_job_id"].as_str();
    if !id(origin)
        || (!j["parent_job_id"].is_null() && !current.is_some_and(id))
        || !["check", "review", "research"].contains(&s(&relation["kind"]))
    {
        return reject("parent_mismatch");
    }
    let p = one(db, parent_id).map_err(|_| ReadError("parent_mismatch"))?;
    if !listed(p, origin)
        || current.is_some_and(|current| !listed(p, current))
        || !p["subwork"].is_null()
        || p["project"] != row["project"]
        || path_key(s(&db["projects"][s(&p["project"])])) != path_key(scope)
    {
        return reject("parent_mismatch");
    }
    // Preserve the original edge and the actual current parent separately.
    let mut records = Vec::new();
    let mut parents = BTreeSet::from([origin]);
    if let Some(current) = current {
        parents.insert(current);
    }
    for parent_job in parents {
        let pj = job(host, l, parent_job)?;
        if pj["id"] != parent_job
            || pj["todo_id"] != parent_id
            || pj["project"] != p["project"]
            || !pj["subwork"].is_null()
            || !pj["parent_job_id"].is_null()
            || !same_path(s(&pj["registered_cwd"]), Path::new(scope))
        {
            return reject("parent_mismatch");
        }
        guarded(host, s(&pj["cwd"]))?;
        records.push(
            json!({"id":pj["id"],"todo_id":pj["todo_id"],"project":pj["project"],
            "registered_cwd":pj["registered_cwd"],"cwd":pj["cwd"],"work_owner":pj["work_owner"]}),
        );
    }
    Ok((
        json!({"todo_id":parent_id,"job_id":current,"origin_job_id":origin,"kind":relation["kind"]}),
        records,
    ))
}
fn spec_paths(row: &Value, l: &Layout, todo: u64) -> Result<Vec<PathBuf>> {
    let mut paths = Vec::new();
    let mut add = |v: &Value| -> Result<()> {
        let p = Path::new(s(v));
        let name = p
            .file_name()
            .and_then(|n| n.to_str())
            .ok_or(ReadError("path_mismatch"))?;
        let expected = l.specs.join(name);
        if !spec_name(name, todo) || !same_path(s(v), &expected) {
            return reject("path_mismatch");
        }
        if !paths.contains(&expected) {
            paths.push(expected);
        }
        Ok(())
    };
    add(&row["spec"])?;
    if let Some(history) = row.get("spec_history") {
        let history = history.as_array().ok_or(ReadError("invalid_record"))?;
        if history.len() > 64 {
            return reject("too_large");
        }
        for entry in history {
            add(&entry["spec"])?;
        }
    }
    Ok(paths)
}
fn matches_spec(dispatch: &str, spec: &str, row: &Value) -> bool {
    let suffix = if row["subwork"].is_object() {
        format!("\nこの作業はToDo {}の独立した{}です。保存済み仕様の有限な範囲だけを扱い、主担当の完了を前提にせず結果を返してください。主担当のソースを編集したり、独立したPRの作成やマージを担当したりしないでください。\n",row["subwork"]["parent_todo_id"],s(&row["subwork"]["kind"]))
    } else {
        String::new()
    };
    // Match the complete saved spec and the known suffix from the end. The
    // delimiter may legitimately appear in earlier results, answers or the spec.
    dispatch
        .strip_suffix(&suffix)
        .and_then(|v| v.strip_suffix(spec))
        .is_some_and(|prefix| prefix.ends_with("\n依頼:\n"))
}
fn observe(host: &mut impl Host, l: &Layout, r: &Value) -> Result<Value> {
    let db = record(host, &l.todo, cc_job_policy::todo_store::MAX_BYTES)?;
    let row = one(&db, r["todo_id"].as_u64().unwrap())?;
    let j = job(host, l, s(&r["job_id"]))?;
    if j["id"] != r["job_id"]
        || j["todo_id"] != r["todo_id"]
        || j["project"] != row["project"]
        || !listed(row, s(&r["job_id"]))
    {
        return reject("todo_job_mismatch");
    }
    let scope = s(&db["projects"][s(&row["project"])]);
    if !absolute(scope) || path_key(scope) != path_key(s(&r["registered_scope"])) {
        return reject("scope_mismatch");
    }
    if !same_path(s(&j["registered_cwd"]), Path::new(scope))
        || (j["work_owner"].is_object()
            && !same_path(s(&j["work_owner"]["worktree"]), Path::new(s(&j["cwd"]))))
    {
        return reject("scope_mismatch");
    }
    if !["launcher", "owner_marker", "owner_request"].contains(&s(&j["scope_source"])) {
        return reject("invalid_record");
    }
    guarded(host, scope)?;
    guarded(host, s(&j["cwd"]))?;
    let (edge, parent_records) = parent(host, l, &db, row, &j, scope)?;
    let kind = if edge.is_null() {
        "scheduler_dispatch"
    } else {
        "scheduler_subwork"
    };
    if kind != s(&r["source_kind"]) {
        return reject("source_mismatch");
    }
    if j["finite_research_approval"].is_object() || j["original_product_retry"].is_object() {
        return reject("unsupported_received_variant");
    }
    let console = &row["scheduler"]["console_request"];
    if (row["product_review_allocation"].is_object() && row["scheduler"]["job"] == j["id"])
        || (console["prompt"].is_string()
            && row["scheduler"]["console_receipts"][s(&console["key"])]["launch_key"]
                == j["launch_key"])
    {
        return reject("unsupported_dispatch_variant");
    }
    let key = s(&j["launch_key"]);
    if key.len() != 32 || !key.bytes().all(|b| b.is_ascii_hexdigit()) {
        return reject("invalid_record");
    }
    let dispatch_path = l.prompts.join(format!("{}-{key}.md", r["todo_id"]));
    let received_path = l.jobs.join(s(&r["job_id"])).join("prompt.md");
    if !same_path(s(&j["prompt_source"]), &dispatch_path)
        || !same_path(s(&j["prompt"]), &received_path)
    {
        return reject("path_mismatch");
    }
    let dispatch = bytes(host, &dispatch_path, BODY_LIMIT)?;
    let received = bytes(host, &received_path, BODY_LIMIT)?;
    let text = std::str::from_utf8(&dispatch).map_err(|_| ReadError("invalid_utf8"))?;
    std::str::from_utf8(&received).map_err(|_| ReadError("invalid_utf8"))?;
    if text
        .lines()
        .find_map(|line| line.strip_prefix("元の登録作業場所: "))
        != Some(scope)
    {
        return reject("scope_mismatch");
    }
    let mut guidance = match s(&j["sandbox"]) {
        "read-only" => cc_job_policy::READ_GUIDANCE,
        "workspace-write" | "danger-full-access" => cc_job_policy::WRITE_GUIDANCE,
        _ => return reject("invalid_record"),
    };
    if j["fixed_review_requalification"].is_object() {
        guidance = cc_job_policy::READ_GUIDANCE;
    }
    let expected_received = if text.contains(guidance.trim()) {
        dispatch.clone()
    } else {
        [dispatch.as_slice(), guidance.as_bytes()].concat()
    };
    if received != expected_received {
        return reject("received_mismatch");
    }
    let paths = spec_paths(row, l, r["todo_id"].as_u64().unwrap())?;
    let mut matched = Vec::new();
    for path in &paths {
        let spec = bytes(host, path, RECORD_LIMIT)?;
        let body = std::str::from_utf8(&spec).map_err(|_| ReadError("invalid_utf8"))?;
        if !body.trim().is_empty() && matches_spec(text, body, row) {
            matched.push((path, spec));
        }
    }
    let [(spec_path, spec)] = matched.as_slice() else {
        return reject("spec_mismatch");
    };
    if (!edge.is_null() && !same_path(s(&j["subwork_spec"]), spec_path))
        || (edge.is_null() && !j["subwork_spec"].is_null())
    {
        return reject("spec_mismatch");
    }
    let binding = json!({"todo_id":row["id"],"project":row["project"],"registered_scope":scope,
        "spec_paths":paths,"subwork":row["subwork"],"parent_records":parent_records,"job":{
            "id":j["id"],"todo_id":j["todo_id"],"project":j["project"],"cwd":j["cwd"],
            "registered_cwd":j["registered_cwd"],"work_owner":j["work_owner"],"subwork_spec":j["subwork_spec"],
            "fixed_review_requalification":j["fixed_review_requalification"],
            "subwork_validated":j["subwork_validated"],"subwork_owner_job_id":j["subwork_owner_job_id"],
            "scope_source":j["scope_source"],"sandbox":j["sandbox"],"launch_key":j["launch_key"],
            "prompt_source":j["prompt_source"],"prompt":j["prompt"],"subwork":j["subwork"],"parent_origin":j["parent_origin"],"parent_job_id":j["parent_job_id"]}});
    let digest = cc_job_policy::product_retry::digest;
    Ok(
        json!({"domain":"received-input-v1","todo_id":r["todo_id"],"job_id":r["job_id"],
        "ok":true,"source_kind":kind,"scope_source":j["scope_source"],"registered_scope":scope,
        "coverage":"selected_scheduler_dispatch_and_job_registration",
        "version":{"spec_ref":spec_path.file_name().unwrap().to_string_lossy(),"spec_sha256":digest(spec),
            "dispatch_sha256":digest(&dispatch),"received_sha256":digest(&received),
            "binding_sha256":digest(&serde_json::to_vec(&binding).unwrap()),"parent_edge":edge},
        "spec_bytes":spec.len(),"dispatch_bytes":dispatch.len(),"received_bytes":received.len(),
        "human_identity_attested":false,"asset_qualified":false,"saved":false,"launched":0,
        "upstream_reference":null,"target_binding":null,
        "delegation_edge_recorded":!edge.is_null(),
        "gaps":["upstream_provider_reference_not_recorded","target_binding_not_recorded","human_identity_not_attested"],
        "hash_domain":"saved_file_bytes","binding_hash_domain":"selected_record_projection_json_utf8",
        "historical_immutability_attested":false}),
    )
}
/// Two bounded observations of the selected binding, not a historical immutability proof.
pub fn inspect(host: &mut impl Host, l: &Layout, request: &Request) -> Result<Value> {
    let started = host.now();
    let first = observe(host, l, &request.0)?;
    let second = observe(host, l, &request.0).map_err(|e| match e.0 {
        "received_mismatch" | "spec_mismatch" | "todo_job_mismatch" | "scope_mismatch"
        | "parent_mismatch" | "path_mismatch" | "source_mismatch" => {
            ReadError("changed_during_read")
        }
        _ => e,
    })?;
    if first != second {
        return reject("changed_during_read");
    }
    if request
        .0
        .get("expected")
        .is_some_and(|v| *v != first["version"])
    {
        return reject("version_mismatch");
    }
    let mut report = second;
    report["observation_started_at"] = json!(started);
    report["fetched_at"] = json!(host.now());
    Ok(report)
}
