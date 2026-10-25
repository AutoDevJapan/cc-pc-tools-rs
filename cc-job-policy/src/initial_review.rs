//! The existing policy-origin, never-started 3900 review only. The caller must
//! acquire normal inputs; no caller-supplied receipt can authorize this route.
use serde_json::{Value, json};
use std::{
    fs,
    io::{self, Read},
    path::Path,
    process::Command,
};

pub const ID: u64 = 3900;
pub const AUTHOR: &str = "20261026-064014-todo-3894";
pub const SPEC_SHA: &str = "e8b9f72b283c1add07eb93e0b554fd8d6b3598bec4e4ef7fa5cd64d1d28c418e";
const MODEL: &str = "gpt-6-luna";
fn s(v: &Value) -> &str {
    v.as_str().unwrap_or("")
}
fn hash(v: &Value) -> String {
    crate::product_retry::digest(&serde_json::to_vec(v).unwrap())
}
fn norm(v: &Value) -> String {
    // Registration canonicalizes an existing cwd. Compare the same qualified
    // physical checkout, including a normal Windows 8.3/verbatim spelling.
    crate::work_owner::norm(Path::new(s(v)))
}
fn empty(v: &Value) -> bool {
    v.is_null() || v.as_array().is_some_and(Vec::is_empty)
}
fn fresh(v: &Value, now: f64) -> bool {
    now.is_finite()
        && v.as_f64()
            .is_some_and(|at| at <= now + 1. && now - at <= 60.)
}

/// Typed CLI, with no arbitrary metadata/path/proof inputs.
pub fn arguments(args: &[String]) -> Option<Option<&str>> {
    if !matches!(args.len(), 3 | 6)
        || args[0] != "reevaluate-unstarted-review"
        || args[1] != "--id"
        || args[2] != "3900"
    {
        return None;
    }
    if args.len() == 3 {
        return Some(None);
    }
    (args[3] == "--apply" && args[4] == "--expected-state-sha256" && sha(&args[5]))
        .then_some(Some(args[5].as_str()))
}
fn sha(v: &str) -> bool {
    v.len() == 64 && v.bytes().all(|b| b.is_ascii_hexdigit())
}

/// Read just the normal 3900 specification namespace; pin its bytes before use.
pub fn read_spec(path: &Path) -> io::Result<Vec<u8>> {
    let name = path
        .to_string_lossy()
        .replace('\\', "/")
        .to_ascii_lowercase();
    let allowed = name
        .strip_prefix("d:/cc/tools/todo/specs/3900-")
        .and_then(|p| p.strip_suffix(".md"))
        .is_some_and(|p| p.len() == 32 && p.bytes().all(|b| b.is_ascii_hexdigit()));
    if !allowed {
        return Err(io::Error::other("review specification namespace changed"));
    }
    for part in path.ancestors() {
        let m = fs::symlink_metadata(part)?;
        #[cfg(windows)]
        let link = {
            use std::os::windows::fs::MetadataExt;
            m.file_attributes() & 0x400 != 0
        };
        #[cfg(not(windows))]
        let link = m.file_type().is_symlink();
        if link {
            return Err(io::Error::other("review specification link refused"));
        }
    }
    let file = fs::File::open(path)?;
    if !file.metadata()?.is_file() {
        return Err(io::Error::other("review specification refused"));
    }
    let mut bytes = Vec::new();
    file.take(1024 * 1024 + 1).read_to_end(&mut bytes)?;
    if crate::product_retry::digest(&bytes) != SPEC_SHA {
        return Err(io::Error::other("fixed review specification changed"));
    }
    Ok(bytes)
}

/// Git metadata only, after the runtime's normal source guards. Never reads SDK
/// files, changes a checkout, fetches, or treats an observed runtime hash as source.
pub fn workspace(row: &Value, root: &str) -> Option<Value> {
    let cwd = s(&row["scheduler"]["worktree"]);
    if row["id"] != ID
        || row["project"] != "cc-pc-tools-rs"
        || norm(&json!(cwd)) != "d:/cc/work/unattended-operations-20261013-wt/todo-3900-ci-sdk"
        || row["scheduler"]["branch"] != "scheduler/todo-3900-ci-sdk"
    {
        return None;
    }
    let owner = crate::work_owner::checkout(cwd);
    let base = crate::work_owner::checkout(root);
    if owner["repository"] != "autodevjapan/cc-pc-tools-rs"
        || owner["repository"] != base["repository"]
        || owner["git_common"] != base["git_common"]
        || owner["branch"] != row["scheduler"]["branch"]
    {
        return None;
    }
    let run = |args: &[&str]| -> Option<String> {
        let mut c = Command::new("git");
        c.args(["--no-replace-objects", "-C", cwd]).args(args);
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            c.creation_flags(0x08000000);
        }
        let out = c.output().ok()?;
        out.status
            .success()
            .then(|| String::from_utf8(out.stdout).ok())
            .flatten()
            .map(|s| s.trim().to_owned())
    };
    let head = run(&["rev-parse", "HEAD"])?;
    if head.len() != 40
        || !head.bytes().all(|b| b.is_ascii_hexdigit())
        || !run(&["status", "--porcelain"])?.is_empty()
    {
        return None;
    }
    Some(
        json!({"repository":owner["repository"],"git_common":owner["git_common"],
        "worktree":owner["worktree"],"branch":owner["branch"],"head":head}),
    )
}

pub fn author(jobs: &[Value]) -> Option<Value> {
    let rows: Vec<_> = jobs.iter().filter(|j| j["id"] == AUTHOR).collect();
    if rows.len() != 1 {
        return None;
    }
    let j = rows[0];
    if j["todo_id"] != 3894
        || !["running", "succeeded"].contains(&s(&j["status"]))
        || j["execution_observed"]["source"] != "native_turn_context"
        || crate::quota_recovery::provider(s(&j["execution_observed"]["model"])).is_none()
        || j["subwork"].is_object()
        || j["work_owner"]["repository"] != "autodevjapan/cc-pc-tools-rs"
        || j["work_owner"]["branch"] != "scheduler/todo-3894-ci"
    {
        return None;
    }
    let mut models = crate::author_models(j);
    let observed = s(&j["execution_observed"]["model"]);
    if !models.iter().any(|m| crate::same_model(m, observed)) {
        models.push(observed.into());
    }
    models.sort();
    models.dedup();
    let mut input = json!({});
    for key in [
        "id",
        "todo_id",
        "cwd",
        "work_owner",
        "execution_observed",
        "model",
        "requested_model",
        "legacy_model_hint",
        "capacity_history",
    ] {
        input[key] = j[key].clone();
    }
    Some(json!({"jobs":[AUTHOR],"models":models,"sha256":hash(&input)}))
}

fn request(row: &Value) -> String {
    let mut fields = serde_json::Map::new();
    for k in [
        "id",
        "project",
        "title",
        "spec",
        "spec_history",
        "model",
        "effort",
        "model_origin",
        "effort_origin",
        "legacy_model_hint",
        "legacy_effort_hint",
        "needs",
        "role",
        "task_type",
        "sandbox",
        "mcp_profile",
        "network",
        "add_dirs",
        "review_source_jobs",
        "subwork",
        "resume_generation",
        "dependency_generation",
        "wait_on",
    ] {
        if let Some(v) = row.get(k) {
            fields.insert(k.into(), v.clone());
        }
    }
    let mut reservation = serde_json::Map::new();
    for k in [
        "key",
        "branch",
        "worktree",
        "attempt",
        "owner_instruction",
        "console_request",
    ] {
        if let Some(v) = row["scheduler"].get(k) {
            reservation.insert(k.into(), v.clone());
        }
    }
    fields.insert("reservation".into(), Value::Object(reservation));
    hash(&Value::Object(fields))
}
fn eligible(row: &Value) -> bool {
    let rel = &row["subwork"];
    row["id"] == ID
        && row["project"] == "cc-pc-tools-rs"
        && row["dispatchable"] == true
        && row["model"] == MODEL
        && row["effort"] == "max"
        && row["model_origin"] == "policy"
        && row["effort_origin"] == "policy"
        && rel["kind"] == "review"
        && rel["parent_todo_id"] == 3894
        && rel["origin_job_id"] == AUTHOR
        && rel["owner"]["repository"] == "autodevjapan/cc-pc-tools-rs"
        && rel["owner"]["branch"] == "scheduler/todo-3894-ci"
        && empty(&row["review_source_jobs"])
        && empty(&row["needs"])
        && empty(&row["wait_on"])
        && !crate::cancellation::owner_stopped(row, &Value::Null)
        && ["stopped", "owner_stopped", "owner_stop"]
            .iter()
            .all(|k| row[*k].is_null() || row[*k] == false)
        && [
            "stop_kind",
            "owner_question_wait",
            "decision_wait",
            "wait_ready",
            "wait_retry",
            "work_owner_job",
            "pending_cancel",
        ]
        .iter()
        .all(|k| row[*k].is_null())
        && [
            "job",
            "last_job",
            "owner_stop_job",
            "pending_cancel",
            "question",
            "console_request",
        ]
        .iter()
        .all(|k| row["scheduler"][*k].is_null())
        && (row["scheduler"]["owner_instruction"].is_null()
            || row["scheduler"]["owner_instruction"] == "")
        && row.get("resume_generation").is_none_or(|v| v.is_u64())
        && row.get("dependency_generation").is_none_or(|v| v.is_u64())
        && row["scheduler"]["key"]
            .as_str()
            .is_some_and(|s| s.len() == 32 && s.bytes().all(|b| b.is_ascii_hexdigit()))
}
fn route(health: &Value, selector: Option<&str>, now: f64) -> Option<String> {
    (fresh(&health["at"], now)
        && fresh(&health["openai"]["at"], now)
        && health["openai"]["verified"] == true
        && health["openai"]["available"] == true)
        .then(|| {
            crate::quota_recovery::safe_openai_selector(health, selector, now).map(str::to_owned)
        })
        .flatten()
}

pub struct Proposal {
    original: Value,
    pub proof: Value,
}
impl Proposal {
    pub fn state_sha256(&self) -> String {
        hash(&self.original)
    }
}

pub fn proposal(
    row: &Value,
    source: &Value,
    workspace: &Value,
    spec_sha: &str,
    health: &Value,
    worker_absent: bool,
    now: f64,
) -> Option<Proposal> {
    let until = row["scheduler"]["not_before"].as_f64()?;
    if !eligible(row)
        || !worker_absent
        || !workspace.is_object()
        || !sha(spec_sha)
        || row["status"] != "todo"
        || !row["jobs"].as_array().is_some_and(Vec::is_empty)
        || row["scheduler"]["phase"] != "preparing"
        || !row["worker_receipt"].is_null()
        || !row["scheduler"]["selection"].is_null()
        || !row["scheduler"]["initial_review"].is_null()
        || !row["scheduler"]["unstarted_review"].is_null()
        || until <= now
        || !until.is_finite()
        || row["scheduler"]["resource_wait"]["reason"] != "quota_exhausted"
        || row["scheduler"]["resource_wait"]["until"] != row["scheduler"]["not_before"]
        || !crate::quota_recovery::healthy_transition(health, "claude-opus-5-5", MODEL, now)
        || source["jobs"] != json!([AUTHOR])
        || !source["models"]
            .as_array()?
            .iter()
            .all(|m| !crate::same_model(s(m), MODEL))
        || source["models"].as_array()?.is_empty()
    {
        return None;
    }
    let selector = route(health, None, now)?;
    let proof = json!({"version":1,"todo_id":ID,"at":now,"request_sha256":request(row),
        "spec_sha256":spec_sha,"author":source,"workspace":workspace,"selector":selector,
        "launch_key":row["scheduler"]["key"],"model":MODEL,"effort":"max",
        "generation_present":row.get("resume_generation").is_some(),
        "generation_before":row["resume_generation"].as_u64().unwrap_or(0),
        "prior_not_before":row["scheduler"]["not_before"],"prior_resource_wait":row["scheduler"]["resource_wait"],
        "health":health});
    Some(Proposal {
        original: row.clone(),
        proof,
    })
}
pub fn apply(row: &mut Value, p: &Proposal) -> bool {
    if *row != p.original {
        return false;
    }
    row["scheduler"]["initial_review"] = p.proof.clone();
    row["scheduler"]
        .as_object_mut()
        .unwrap()
        .remove("not_before");
    row["scheduler"]
        .as_object_mut()
        .unwrap()
        .remove("resource_wait");
    true
}

#[allow(clippy::too_many_arguments)]
pub fn current(
    row: &Value,
    source: &Value,
    workspace: &Value,
    spec_sha: &str,
    health: &Value,
    now: f64,
    job: Option<&Value>,
) -> bool {
    let p = &row["scheduler"]["initial_review"];
    if p["version"] != 1
        || p["todo_id"] != ID
        || p["model"] != MODEL
        || p["effort"] != "max"
        || p["author"] != *source
        || p["workspace"] != *workspace
        || p["spec_sha256"] != spec_sha
        || !row["scheduler"]["not_before"].is_null()
        || !row["scheduler"]["resource_wait"].is_null()
        || !crate::quota_recovery::healthy_transition(
            &p["health"],
            "claude-opus-5-5",
            MODEL,
            p["at"].as_f64().unwrap_or(f64::NAN),
        )
        || route(health, Some(s(&p["selector"])), now).is_none()
    {
        return false;
    }
    let mut original = row.clone();
    let mut transferred = false;
    if row["status"] == "doing" {
        let Some(j) = job else {
            return false;
        };
        if row["jobs"] != json!([j["id"]]) || row["scheduler"]["job"] != j["id"] {
            return false;
        }
        if !row["worker_receipt"].is_null() {
            let before = p["generation_before"].as_u64();
            if row["worker_receipt"]["job"] != j["id"]
                || row["worker_receipt"]["generation"] != row["resume_generation"]
                || before.and_then(|g| g.checked_add(1)) != row["resume_generation"].as_u64()
                || j["todo_resume_generation"] != row["resume_generation"]
            {
                return false;
            }
            if p["generation_present"] == true {
                original["resume_generation"] = p["generation_before"].clone();
            } else {
                original
                    .as_object_mut()
                    .unwrap()
                    .remove("resume_generation");
            }
            if original["scheduler"]["key"].is_null() {
                original["scheduler"]["key"] = p["launch_key"].clone();
            }
            transferred = true;
        }
        original["scheduler"].as_object_mut().unwrap().remove("job");
    } else if row["status"] != "todo" || row["jobs"] != json!([]) {
        return false;
    }
    if !eligible(&original)
        || request(&original) != p["request_sha256"]
        || original["scheduler"]["key"] != p["launch_key"]
        || !["preparing", "prepared", "running"].contains(&s(&row["scheduler"]["phase"]))
    {
        return false;
    }
    if let Some(j) = job {
        let selected = &row["scheduler"]["selection"];
        if j["initial_review"] != *p
            || ((!transferred || !selected.is_null()) && selected["initial_review"] != *p)
            || j["todo_id"] != ID
            || j["launch_key"] != p["launch_key"]
            || norm(&j["cwd"]) != norm(&row["scheduler"]["worktree"])
            || j["capacity_account_selector"] != p["selector"]
            || j["review_source_jobs"] != json!([AUTHOR])
            || ((!transferred || !selected.is_null())
                && [
                    "model",
                    "effort",
                    "model_origin",
                    "effort_origin",
                    "sandbox",
                    "task_type",
                    "mcp_profile",
                    "network",
                    "add_dirs",
                ]
                .iter()
                .any(|k| j[*k] != selected[*k]))
        {
            return false;
        }
        for (k, v) in [
            ("model", json!(MODEL)),
            ("effort", json!("max")),
            ("model_origin", json!("policy")),
            ("effort_origin", json!("policy")),
            ("sandbox", json!("read-only")),
            ("task_type", json!("review")),
            ("mcp_profile", json!("minimal")),
            ("network", json!(false)),
            ("add_dirs", json!([])),
        ] {
            if j[k] != v {
                return false;
            }
        }
    }
    true
}

pub fn select(selection: &mut Value, proof: &Value) {
    for (k, v) in [
        ("model", json!(MODEL)),
        ("effort", json!("max")),
        ("model_origin", json!("policy")),
        ("effort_origin", json!("policy")),
        ("sandbox", json!("read-only")),
        ("task_type", json!("review")),
        ("mcp_profile", json!("minimal")),
        ("network", json!(false)),
        ("add_dirs", json!([])),
    ] {
        selection[k] = v;
    }
    selection["initial_review"] = proof.clone();
    selection["finite_review_spec_sha256"] = proof["spec_sha256"].clone();
}

/// Preflight before a quota refresh; unknown schemas/types never reach it.
pub fn oauth_only(raw: &Value) -> bool {
    raw["accounts"].as_array().is_some_and(|rows| {
        !rows.is_empty()
            && rows
                .iter()
                .all(|r| ["oauth", "codex"].contains(&s(&r["type"])))
    })
}
