//! One root worker per PR, branch or checkout. Child reviews do not claim work.
use serde_json::{Value, json};
use std::{fs, path::Path, process::Command};

fn tracked_scope(root: &Path, registered: &Path) -> Option<bool> {
    let relative = registered.strip_prefix(root).ok()?;
    let mut command = Command::new("git");
    command
        .args(["--literal-pathspecs", "-C"])
        .arg(root)
        .args(["ls-files", "-z", "--"])
        .arg(relative);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let output = command.output().ok()?;
    output.status.success().then_some(!output.stdout.is_empty())
}

fn s<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}
pub fn norm(path: &Path) -> String {
    let p = fs::canonicalize(path).unwrap_or_else(|_| path.to_path_buf());
    let p = p.to_string_lossy().replace('\\', "/");
    let p = p
        .trim_start_matches("//?/")
        .trim_end_matches('/')
        .to_string();
    if cfg!(windows) { p.to_lowercase() } else { p }
}
fn is_user_home(root: &str) -> bool {
    let key = if cfg!(windows) { "USERPROFILE" } else { "HOME" };
    std::env::var_os(key).is_some_and(|p| !root.is_empty() && norm(Path::new(&p)) == root)
}
/// Read local Git metadata only; never fetch, launch a shell or retain remote credentials.
pub fn checkout(cwd: &str) -> Value {
    if cwd.is_empty() {
        return json!({});
    }
    let root = Path::new(cwd).ancestors().find(|p| p.join(".git").exists());
    let Some(root) = root else {
        // A shared tools directory is not a Git worktree.
        return json!({});
    };
    let worktree = norm(root);
    let git = root.join(".git");
    let gitdir = if git.is_dir() {
        git
    } else {
        let Ok(pointer) = fs::read_to_string(git) else {
            return json!({"worktree":worktree});
        };
        let Some(dir) = pointer.trim().strip_prefix("gitdir: ") else {
            return json!({"worktree":worktree});
        };
        root.join(dir)
    };
    let common = fs::read_to_string(gitdir.join("commondir"))
        .map(|p| gitdir.join(p.trim()))
        .unwrap_or_else(|_| gitdir.clone());
    let common_key = norm(&common);
    let mut value = {
        let config = fs::read_to_string(common.join("config")).unwrap_or_default();
        let mut origin = false;
        let mut repo = String::new();
        for line in config.lines().map(str::trim) {
            if line.starts_with('[') {
                origin = line == "[remote \"origin\"]";
            }
            if origin
                && let Some((key, url)) = line.split_once('=')
                && key.trim() == "url"
            {
                let url = url.trim();
                let path = url
                    .split_once("github.com/")
                    .or_else(|| url.split_once("github.com:"));
                if let Some((_, path)) = path {
                    let parts: Vec<_> = path
                        .trim_end_matches('/')
                        .trim_end_matches(".git")
                        .split('/')
                        .collect();
                    if parts.len() == 2
                        && parts.iter().all(|p| {
                            !p.is_empty()
                                && p.chars()
                                    .all(|c| c.is_ascii_alphanumeric() || "_.-".contains(c))
                        })
                    {
                        repo = parts.join("/").to_lowercase();
                    }
                }
            }
        }
        json!({"repository":repo,"git_common":common_key})
    };
    value["worktree"] = json!(worktree);
    if is_user_home(s(&value, "worktree")) {
        value["root_kind"] = json!("user_home");
    }
    if let Ok(head) = fs::read_to_string(gitdir.join("HEAD"))
        && let Some(branch) = head.trim().strip_prefix("ref: refs/heads/")
    {
        value["branch"] = json!(branch);
    }
    value
}

pub fn pr_number(text: &str) -> Option<u64> {
    let text = text.to_ascii_lowercase();
    for (pos, _) in text.match_indices("pr") {
        if pos > 0 && text.as_bytes()[pos - 1].is_ascii_alphanumeric() {
            continue;
        }
        let tail = text[pos + 2..].trim_start_matches([' ', '#', '＃']);
        let digits: String = tail.chars().take_while(char::is_ascii_digit).collect();
        if let Ok(id) = digits.parse::<u64>()
            && id > 0
        {
            return Some(id);
        }
    }
    None
}
pub fn from_job(job: &Value) -> Value {
    let mut value = if job["work_owner"].is_object() {
        job["work_owner"].clone()
    } else {
        checkout(s(job, "cwd"))
    };
    let registered = norm(Path::new(s(job, "registered_cwd")));
    let cwd = norm(Path::new(s(job, "cwd")));
    let tree = s(&value, "worktree").to_owned();
    // A registered non-repository folder can sit inside the owner's home Git.
    // Keep that discovery as evidence, but claim the actual working folder only.
    if !s(job, "registered_cwd").is_empty()
        && !s(job, "cwd").is_empty()
        && !tree.is_empty()
        && (value["root_kind"] == "user_home" || is_user_home(&tree))
        && registered.starts_with(&format!("{tree}/"))
        && (cwd == registered || cwd.starts_with(&format!("{registered}/")))
    {
        let explicit_pr = value["explicit_pr"].clone();
        let ancestor = value.clone();
        let actual = checkout(s(job, "cwd"));
        if !s(&actual, "worktree").is_empty() && s(&actual, "worktree") != tree {
            value = actual;
            value["scope"] = json!("git_checkout");
            value["ancestor_checkout"] = ancestor;
        } else if tracked_scope(Path::new(&tree), Path::new(&registered)) == Some(false) {
            value =
                json!({"worktree":cwd,"scope":"registered_folder","ancestor_checkout":ancestor});
        }
        if explicit_pr.is_object() {
            value["repository"] = explicit_pr["repository"].clone();
            value["pr"] = explicit_pr["pr"].clone();
            value["explicit_pr"] = explicit_pr;
        }
    }
    // A project display name is not a repository identity.
    if !value["explicit_pr"].is_object()
        && let Some(pr) = pr_number(s(job, "task_title"))
    {
        value["pr"] = json!(pr);
    }
    value
}
/// Registration is authoritative; do not infer a project from a display title.
pub fn registered_job(db: &Value, job: &Value) -> Value {
    let mut job = job.clone();
    if s(&job, "registered_cwd").is_empty()
        && let Some(path) = db["projects"][s(&job, "project")].as_str()
    {
        job["registered_cwd"] = json!(path);
    }
    job
}
pub fn pr_url(url: &str) -> Option<Value> {
    let path = url.strip_prefix("https://github.com/")?;
    let parts: Vec<_> = path.trim_end_matches('/').split('/').collect();
    if parts.len() != 4
        || parts[2] != "pull"
        || parts[..2].iter().any(|p| {
            p.is_empty()
                || !p
                    .chars()
                    .all(|c| c.is_ascii_alphanumeric() || "_.-".contains(c))
        })
    {
        return None;
    }
    let pr = parts[3].parse::<u64>().ok().filter(|n| *n > 0)?;
    Some(json!({"repository":parts[..2].join("/").to_lowercase(),"pr":pr}))
}
pub fn same(a: &Value, b: &Value) -> bool {
    relation(a, b).is_some()
}
/// Only a shared PR or branch is a durable result dependency. A folder alone
/// reserves a live writer, not completion of every task ever using that folder.
pub fn relation(a: &Value, b: &Value) -> Option<&'static str> {
    let equal = |key| !s(a, key).is_empty() && s(a, key) == s(b, key);
    if equal("repository") && a["pr"].is_u64() && a["pr"] == b["pr"] {
        Some("shared_pr")
    } else if equal("git_common") && equal("branch") {
        Some("shared_branch")
    } else if equal("worktree") {
        Some("live_worktree")
    } else {
        None
    }
}
fn root(job: &Value) -> bool {
    job["subwork"].is_object() || s(job, "parent_job_id").is_empty()
}
fn live(job: &Value) -> bool {
    ["starting", "running"].contains(&s(job, "status"))
        || job["work_owner_live"] == true
        || (s(job, "status") == "queued" && job["cancel_requested"] != true)
}
fn rank(job: &Value) -> (u8, u64, &str, &str) {
    (
        u8::from(s(job, "status") == "queued" && job["work_owner_live"] != true),
        job["queue_sequence"].as_u64().unwrap_or(0),
        s(job, "created_at"),
        s(job, "id"),
    )
}
pub fn blocker<'a>(candidate: &Value, jobs: &'a [Value]) -> Option<&'a Value> {
    if !root(candidate) {
        return None;
    }
    let identity = from_job(candidate);
    jobs.iter()
        .filter(|j| root(j) && live(j) && j["id"] != candidate["id"])
        .filter(|j| !crate::subwork::permits(candidate, j))
        .filter(|j| !live(candidate) || rank(j) < rank(candidate))
        .filter(|j| same(&identity, &from_job(j)))
        .min_by_key(|j| rank(j))
}
pub fn reason(owner: &Value) -> Value {
    json!({"code":"work_owner","message":format!("同じPR・作業フォルダの担当 {} の完了待ち",s(owner,"id")),"owner_job_id":owner["id"],"owner_todo_id":owner["todo_id"]})
}
/// Queued subwork that cannot be admitted must not reserve a PR indefinitely.
/// Retain starting/running jobs (and observed surviving processes) until they actually stop.
pub fn observed_jobs(db: &Value, jobs: &[Value]) -> Vec<Value> {
    jobs.iter()
        .filter_map(|j| {
            let mut candidate = registered_job(db, j);
            if j["subwork"].is_object() {
                candidate["subwork_validated"] = json!(false);
                if crate::subwork::refresh_job(db, jobs, &mut candidate).is_err()
                    && s(j, "status") == "queued"
                    && j["work_owner_live"] != true
                {
                    return None;
                }
            }
            Some(candidate)
        })
        .collect()
}
/// Under the ToDo store lock: preserve all evidence and turn collisions into real prerequisites.
pub fn reconcile_todos(db: &mut Value, jobs: &[Value]) -> Vec<i64> {
    reconcile_todos_if(db, jobs, |_| true)
}

/// The exact recorded worker set of an original relocation; shared by its
/// admission proof and ordinary ownership reconciliation.
pub fn relocation_worker_ids(item: &Value, jobs: &[Value]) -> Value {
    let mut ids: Vec<_> = jobs
        .iter()
        .filter(|job| {
            job["todo_id"] == item["id"]
                || job["related_todo_ids"]
                    .as_array()
                    .is_some_and(|ids| ids.contains(&item["id"]))
                || item["jobs"]
                    .as_array()
                    .is_some_and(|ids| ids.contains(&job["id"]))
        })
        .map(|job| s(job, "id").to_owned())
        .collect();
    ids.sort();
    json!(ids)
}

// A qualified, not-yet-used scheduler relocation reserves its real target.
// Missing or changed evidence is deferred to the scheduler's strict gate;
// never turn the historical shared cwd into a new completion prerequisite.
fn relocated_identity(item: &Value, jobs: &[Value]) -> Option<Value> {
    let receipt = &item["scheduler"]["workspace_relocation"];
    let request = &receipt["request"];
    let prefix = format!("scheduler/todo-{}-", item["id"]);
    if receipt["schema"] != "scheduler-workspace-relocation-v1"
        || request["id"] != item["id"]
        || item["jobs"] != receipt["original_jobs"]
        || !s(request, "branch").starts_with(&prefix)
        || item["scheduler"]["branch"] != request["branch"]
        || norm(Path::new(s(&item["scheduler"], "worktree")))
            != norm(Path::new(s(request, "worktree")))
    {
        return None;
    }
    let source = jobs.iter().find(|job| job["id"] == request["source_job"])?;
    if source["todo_id"] != item["id"]
        || source["status"] != "succeeded"
        || source["exit_code"] != 0
        || [
            "id",
            "todo_id",
            "project",
            "status",
            "exit_code",
            "cwd",
            "work_owner",
        ]
        .iter()
        .any(|key| source[*key] != receipt["source_identity"][*key])
    {
        return None;
    }
    if relocation_worker_ids(item, jobs) != receipt["worker_ids"] {
        return None;
    }
    let owner = checkout(s(&item["scheduler"], "worktree"));
    if owner["repository"] != "autodevjapan/cc-pc-tools-rs"
        || owner["worktree"] != norm(Path::new(s(&item["scheduler"], "worktree")))
        || ["repository", "git_common", "worktree", "branch"]
            .iter()
            .any(|key| owner[*key] != receipt["target_owner"][*key])
        || ["repository", "git_common"]
            .iter()
            .any(|key| owner[*key] != source["work_owner"][*key])
    {
        return None;
    }
    Some(owner)
}
pub fn reconcile_todos_if(
    db: &mut Value,
    jobs: &[Value],
    eligible: impl Fn(&Value) -> bool,
) -> Vec<i64> {
    let mut changed = reevaluate_todos_if(db, jobs, &eligible);
    let jobs = observed_jobs(db, jobs);
    let jobs = jobs.as_slice();
    let mut edges = Vec::new();
    for item in db["items"].as_array().into_iter().flatten() {
        if !eligible(item) || item["id"].as_i64().is_none() {
            continue;
        }
        if !["todo", "doing"].contains(&s(item, "status")) || crate::todo_wait::waiting(db, item) {
            continue;
        }
        if jobs.iter().any(|j| {
            root(j)
                && ["starting", "running"].contains(&s(j, "status"))
                && j["todo_id"] == item["id"]
        }) {
            continue;
        }
        let old = jobs
            .iter()
            .filter(|j| {
                j["todo_id"] == item["id"]
                    || item["jobs"]
                        .as_array()
                        .is_some_and(|ids| ids.contains(&j["id"]))
            })
            .max_by_key(|j| (s(j, "created_at"), s(j, "id")));
        let moved = item["scheduler"]["workspace_relocation"].is_object()
            && old.is_none_or(|job| {
                norm(Path::new(s(job, "cwd"))) != norm(Path::new(s(&item["scheduler"], "worktree")))
            });
        let relocation = if moved {
            let Some(identity) = relocated_identity(item, jobs) else {
                continue;
            };
            Some(identity)
        } else {
            None
        };
        let cwd = if moved {
            s(&item["scheduler"], "worktree")
        } else {
            old.map(|j| s(j, "cwd"))
                .filter(|p| !p.is_empty())
                .unwrap_or_else(|| s(&item["scheduler"], "worktree"))
        };
        let mut candidate = registered_job(
            db,
            &json!({"id":format!("todo-{}",item["id"]),"cwd":cwd,"project":item["project"],"task_title":item["title"],"todo_id":item["id"]}),
        );
        if let Some(identity) = relocation {
            candidate["work_owner"] = identity;
        } else if let Some(old) = old {
            if live(old) {
                candidate = old.clone();
            }
            candidate["work_owner"] = from_job(old);
        }
        // Current task title supplies an explicit PR for an older record without one.
        let mut identity = from_job(&candidate);
        if s(&identity, "repository").is_empty() && identity["scope"] != "registered_folder" {
            // A project checkout supplies repository scope only, never a blanket
            // worktree/branch claim for tasks that have not got a checkout yet.
            let scope = checkout(db["projects"][s(item, "project")].as_str().unwrap_or(""));
            identity["repository"] = scope["repository"].clone();
        }
        if let Some(pr) = pr_number(s(item, "title")) {
            identity["pr"] = json!(pr);
        }
        candidate["work_owner"] = identity;
        if item["subwork"].is_object() {
            // An invalid relation is held for repair, never converted into a parent prerequisite.
            if crate::subwork::apply(db, item, jobs, &mut candidate, false).is_err() {
                continue;
            }
        }
        if let Some(owner) = blocker(&candidate, jobs)
            && let Some(on) = owner["todo_id"]
                .as_i64()
                .filter(|id| Some(*id) != item["id"].as_i64())
        {
            if !db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .any(|i| i["id"] == on && i["status"] != "done")
            {
                continue;
            }
            if crate::todo_wait::validate(db, item["id"].as_i64().unwrap(), &[on]).is_ok() {
                edges.push((item["id"].as_i64().unwrap(), on, owner["id"].clone()));
            }
        }
    }
    for (id, on, job) in edges {
        // Recheck cycles after each edge, including edges added in this transaction.
        if crate::todo_wait::validate(db, id, &[on]).is_err() {
            continue;
        }
        let item = db["items"]
            .as_array_mut()
            .unwrap()
            .iter_mut()
            .find(|i| i["id"] == id)
            .unwrap();
        item["status"] = json!("wait");
        item["wait_on"] = json!([on]);
        item["work_owner_job"] = job;
        item["work_owner_wait_on"] = json!(on);
        item.as_object_mut().unwrap().remove("dependency_ready");
        if let Some(recovery) = item["stall_recovery"].as_object_mut() {
            recovery.remove("pending");
            recovery.remove("report");
        }
        changed.push(id);
    }
    changed
}

/// Retain unclassified legacy waits. Only exact recorded automatic owner edges
/// may be released; a manual wait never has this provenance.
pub fn remembered_pending(db: &Value, candidate: &Value, jobs: &[Value]) -> Vec<i64> {
    let candidate = registered_job(db, candidate);
    crate::todo_wait::pending_on(db, &json!({"wait_on":candidate["work_wait_on"]}))
        .into_iter()
        .filter(|on| {
            let evidence = &candidate["work_wait_evidence"];
            let owner_id = evidence["owner_job_id"]
                .as_str()
                .or_else(|| candidate["queue_wait_reason"]["owner_job_id"].as_str())
                .or_else(|| {
                    db["items"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .find(|i| i["id"] == candidate["todo_id"])
                        .and_then(|i| automatic_owner(i, *on))
                });
            let owner = owner_id.and_then(|id| jobs.iter().find(|j| s(j, "id") == id));
            if let Some(owner) = owner.filter(|o| o["todo_id"].as_i64() == Some(*on)) {
                return owner_wait_valid(db, &candidate, owner);
            }
            // New records preserve the exact owner claim across worker exit.
            if evidence["owner"]["todo_id"].as_i64() == Some(*on) {
                let mut owner = evidence["owner"].clone();
                // A missing record is not evidence that its writer has stopped.
                owner["work_owner_live"] = json!(true);
                return owner_wait_valid(db, &candidate, &owner);
            }
            true
        })
        .collect()
}
pub fn owner_wait_valid(db: &Value, candidate: &Value, owner: &Value) -> bool {
    match relation(
        &from_job(&registered_job(db, candidate)),
        &from_job(&registered_job(db, owner)),
    ) {
        Some("shared_pr" | "shared_branch") => true,
        Some("live_worktree") => live(owner),
        _ => false,
    }
}
/// The evidence contains identity and ownership fields only, never prompts or logs.
pub fn owner_evidence(owner: &Value) -> Value {
    Value::Object(
        [
            "id",
            "todo_id",
            "cwd",
            "project",
            "registered_cwd",
            "work_owner",
        ]
        .into_iter()
        .map(|key| (key.to_owned(), owner[key].clone()))
        .collect(),
    )
}
/// Every DB-derived release uses this single provenance check. Legacy marks
/// alone cannot distinguish an automatic collision from a later manual wait.
fn automatic_owner(item: &Value, on: i64) -> Option<&str> {
    if item["work_owner_wait_on"].as_i64() == Some(on)
        && item["wait_on"]
            .as_array()
            .is_some_and(|ids| ids.contains(&json!(on)))
    {
        item["work_owner_job"].as_str()
    } else {
        None
    }
}
pub fn reevaluate_todos_if(
    db: &mut Value,
    jobs: &[Value],
    eligible: impl Fn(&Value) -> bool,
) -> Vec<i64> {
    let mut releases = Vec::new();
    for item in db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|i| i["id"].is_i64() && eligible(i))
    {
        let Some(owner) = jobs
            .iter()
            .find(|j| j["id"].is_string() && j["id"] == item["work_owner_job"])
        else {
            continue;
        };
        let Some(on) = owner["todo_id"].as_i64() else {
            continue;
        };
        if automatic_owner(item, on) != owner["id"].as_str() {
            continue;
        }
        let Some(candidate) = jobs
            .iter()
            .filter(|j| {
                j["todo_id"] == item["id"]
                    || item["jobs"]
                        .as_array()
                        .is_some_and(|ids| ids.contains(&j["id"]))
            })
            .max_by_key(|j| (s(j, "created_at"), s(j, "id")))
        else {
            continue;
        };
        if !owner_wait_valid(db, candidate, owner) {
            releases.push((
                item["id"].clone(),
                on,
                owner["id"].clone(),
                from_job(&registered_job(db, candidate)),
                from_job(&registered_job(db, owner)),
            ));
        }
    }
    let mut changed = Vec::new();
    for (id, on, owner, candidate_identity, owner_identity) in releases {
        let item = db["items"]
            .as_array_mut()
            .unwrap()
            .iter_mut()
            .find(|i| i["id"] == id)
            .unwrap();
        if !item["work_owner_wait_history"].is_array() {
            item["work_owner_wait_history"] = json!([]);
        }
        item["work_owner_wait_history"].as_array_mut().unwrap().push(json!({"wait_on":on,"owner_job_id":owner,"reason":"owner_claim_reevaluated","candidate":candidate_identity,"owner":owner_identity}));
        item["wait_on"] = json!(
            item["wait_on"]
                .as_array()
                .unwrap()
                .iter()
                .filter(|i| **i != on)
                .cloned()
                .collect::<Vec<_>>()
        );
        item.as_object_mut().unwrap().remove("work_owner_job");
        item.as_object_mut().unwrap().remove("work_owner_wait_on");
        if item["wait_on"].as_array().is_some_and(Vec::is_empty) {
            item.as_object_mut().unwrap().remove("wait_on");
            crate::todo_wait::release_item(item);
        }
        changed.push(id.as_i64().unwrap());
    }
    changed
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn registered_folders_do_not_inherit_home_git_ownership() {
        let dir = tempfile::tempdir().unwrap();
        assert!(
            Command::new("git")
                .arg("init")
                .arg(dir.path())
                .output()
                .unwrap()
                .status
                .success()
        );
        let tree = norm(dir.path());
        let a_path = dir.path().join("products/a");
        let b_path = dir.path().join("products/b");
        fs::create_dir_all(&a_path).unwrap();
        fs::create_dir_all(&b_path).unwrap();
        let identity = json!({"repository":"adj/home","git_common":format!("{tree}/.git"),"worktree":tree,"root_kind":"user_home","branch":"fix/other"});
        let a = json!({"cwd":a_path,"registered_cwd":a_path,"work_owner":identity});
        let b = json!({"cwd":b_path,"registered_cwd":b_path,"work_owner":identity});
        assert!(!same(&from_job(&a), &from_job(&b)));
        assert!(same(&from_job(&a), &from_job(&a)));
        // An actual nested repository still claims its real checkout and branch.
        let mut nested = a.clone();
        nested["work_owner"]["worktree"] = json!(format!("{tree}/products/a/repo"));
        nested["work_owner"]["git_common"] = json!(format!("{tree}/products/a/repo/.git"));
        nested["work_owner"]
            .as_object_mut()
            .unwrap()
            .remove("root_kind");
        assert_eq!(from_job(&nested)["branch"], "fix/other");
        // Registered source folders tracked by the real Git still share writer exclusion.
        fs::write(a_path.join("source.rs"), "tracked").unwrap();
        assert!(
            Command::new("git")
                .arg("-C")
                .arg(dir.path())
                .args(["add", "."])
                .output()
                .unwrap()
                .status
                .success()
        );
        assert_eq!(from_job(&a)["worktree"], tree);
        assert_eq!(from_job(&a)["branch"], "fix/other");
    }
    #[test]
    fn stale_ancestor_identity_recovers_actual_nested_checkout() {
        let dir = tempfile::tempdir().unwrap();
        assert!(
            Command::new("git")
                .arg("init")
                .arg(dir.path())
                .output()
                .unwrap()
                .status
                .success()
        );
        let registered = dir.path().join("products");
        let cwd = registered.join("repo");
        fs::create_dir_all(&cwd).unwrap();
        assert!(
            Command::new("git")
                .arg("init")
                .arg(&cwd)
                .output()
                .unwrap()
                .status
                .success()
        );
        let mut job = json!({"registered_cwd":registered,"cwd":cwd,"work_owner":checkout(dir.path().to_str().unwrap())});
        job["work_owner"]["root_kind"] = json!("user_home");
        assert_eq!(from_job(&job)["worktree"], norm(&cwd));
        assert_eq!(from_job(&job)["git_common"], norm(&cwd.join(".git")));
    }
    #[test]
    fn automatic_owner_waits_release_only_disproved_claims_and_keep_manual_edges() {
        let a = json!({"id":"a","todo_id":1,"status":"succeeded","work_owner":{"worktree":"/a"}});
        let b = json!({"id":"b","todo_id":2,"status":"queued","work_owner":{"worktree":"/b"}});
        let mut db = json!({"items":[{"id":1,"status":"todo"},{"id":2,"status":"wait","wait_on":[1,3],"work_owner_job":"a","work_owner_wait_on":1,"jobs":["b"],"spec":"saved","note":"proof"},{"id":3,"status":"todo"},{"id":4,"status":"wait","wait_on":[1],"work_owner_job":"a","jobs":["legacy"]}]});
        assert_eq!(
            reevaluate_todos_if(&mut db, &[a.clone(), b.clone()], |_| true),
            vec![2]
        );
        assert_eq!(db["items"][1]["wait_on"], json!([3]));
        assert_eq!(db["items"][1]["status"], "wait");
        assert_eq!(db["items"][1]["spec"], "saved");
        assert_eq!(db["items"][1]["note"], "proof");
        assert_eq!(db["items"][3]["wait_on"], json!([1]));
        assert_eq!(db["items"][1]["work_owner_wait_history"][0]["wait_on"], 1);
        assert!(reevaluate_todos_if(&mut db, &[a, b], |_| true).is_empty());
    }
    #[test]
    fn untracked_registered_subfolders_of_real_repo_keep_worktree_exclusion() {
        let dir = tempfile::tempdir().unwrap();
        assert!(
            Command::new("git")
                .arg("init")
                .arg(dir.path())
                .output()
                .unwrap()
                .status
                .success()
        );
        let a = dir.path().join("a");
        let b = dir.path().join("b");
        fs::create_dir(&a).unwrap();
        fs::create_dir(&b).unwrap();
        let first = json!({"cwd":a,"registered_cwd":a,"work_owner":checkout(a.to_str().unwrap())});
        let second = json!({"cwd":b,"registered_cwd":b,"work_owner":checkout(b.to_str().unwrap())});
        assert!(same(&from_job(&first), &from_job(&second)));
    }
    #[test]
    fn remembered_claims_keep_real_pr_and_branch_dependencies_after_worker_exit() {
        let db = json!({"items":[{"id":1,"status":"todo"}]});
        let mut owner =
            json!({"id":"a","todo_id":1,"status":"succeeded","work_owner":{"worktree":"/a"}});
        let mut candidate = json!({"id":"b","work_wait_on":[1],"queue_wait_reason":{"owner_job_id":"a"},"work_owner":{"worktree":"/a"}});
        assert!(remembered_pending(&db, &candidate, std::slice::from_ref(&owner)).is_empty());
        owner["status"] = json!("running");
        assert_eq!(
            remembered_pending(&db, &candidate, std::slice::from_ref(&owner)),
            vec![1]
        );
        owner["status"] = json!("succeeded");
        for identity in [
            json!({"repository":"adj/game","pr":7}),
            json!({"git_common":"/git","branch":"fix/shared"}),
        ] {
            owner["work_owner"] = identity.clone();
            candidate["work_owner"] = identity;
            assert_eq!(
                remembered_pending(&db, &candidate, std::slice::from_ref(&owner)),
                vec![1]
            );
        }
        assert_eq!(remembered_pending(&db, &candidate, &[]), vec![1]);
    }
    #[test]
    fn remembered_db_fallback_requires_typed_automatic_provenance() {
        let owner =
            json!({"id":"owner","todo_id":1,"status":"succeeded","work_owner":{"worktree":"/a"}});
        let candidate = json!({"id":"candidate","todo_id":2,"work_wait_on":[1],"queue_wait_reason":{"code":"todo_dependency"},"work_owner":{"worktree":"/b"}});
        let mut db = json!({"items":[{"id":1,"status":"todo"},{"id":2,"status":"wait","wait_on":[1],"work_owner_job":"owner"}]});
        assert_eq!(
            remembered_pending(&db, &candidate, std::slice::from_ref(&owner)),
            vec![1]
        );
        db["items"][1]["work_owner_wait_on"] = json!(1);
        assert!(remembered_pending(&db, &candidate, std::slice::from_ref(&owner)).is_empty());
    }
    fn job(id: &str, status: &str, repo: &str, pr: u64, tree: &str) -> Value {
        json!({"id":id,"status":status,"created_at":id,"todo_id":if id=="a" {1} else {2},"work_owner":{"repository":repo,"pr":pr,"worktree":tree}})
    }
    #[test]
    fn pr_scope_children_and_same_checkout() {
        let a = job("a", "running", "adj/game", 86, "/a");
        let mut b = job("b", "queued", "adj/game", 86, "/b");
        assert_eq!(blocker(&b, std::slice::from_ref(&a)).unwrap()["id"], "a");
        b["parent_job_id"] = json!("a");
        assert!(blocker(&b, std::slice::from_ref(&a)).is_none());
        b.as_object_mut().unwrap().remove("parent_job_id");
        b["work_owner"]["repository"] = json!("adj/other");
        assert!(blocker(&b, std::slice::from_ref(&a)).is_none());
        b["work_owner"]["worktree"] = json!("/a");
        assert!(blocker(&b, &[a]).is_some());
    }
    #[test]
    fn first_queued_owner_beats_priority_and_releases_on_completion() {
        let mut a = job("a", "queued", "adj/game", 86, "/a");
        let b = job("b", "queued", "adj/game", 86, "/b");
        assert!(blocker(&b, std::slice::from_ref(&a)).is_some());
        assert!(blocker(&a, std::slice::from_ref(&b)).is_none());
        a["status"] = json!("succeeded");
        assert!(blocker(&b, &[a]).is_none());
    }
    #[test]
    fn branch_is_scoped_and_detached_is_not_a_shared_branch() {
        let mut a = json!({"repository":"adj/game","git_common":"/git/a","worktree":"/a","branch":"feat/x"});
        let mut b = json!({"repository":"adj/game","git_common":"/git/a","worktree":"/b","branch":"feat/x"});
        assert!(same(&a, &b));
        b["repository"] = json!("adj/other");
        b["git_common"] = json!("/git/b");
        assert!(!same(&a, &b));
        a["branch"] = json!("");
        b["branch"] = json!("");
        assert!(!same(&a, &b));
    }
    #[test]
    fn cancelling_active_owner_still_blocks_but_cancelled_waiter_does_not() {
        let mut a = job("a", "running", "adj/game", 86, "/a");
        let b = job("b", "queued", "adj/game", 86, "/b");
        a["cancel_requested"] = json!(true);
        assert!(blocker(&b, std::slice::from_ref(&a)).is_some());
        a["status"] = json!("queued");
        assert!(blocker(&b, &[a]).is_none());
    }
    #[test]
    fn independent_clones_on_main_only_collide_if_the_pr_matches() {
        let a =
            json!({"repository":"adj/game","git_common":"/git/a","worktree":"/a","branch":"main"});
        let b =
            json!({"repository":"adj/game","git_common":"/git/b","worktree":"/b","branch":"main"});
        assert!(!same(&a, &b));
    }
    #[test]
    fn todo_collision_preserves_spec_and_evidence_and_is_idempotent() {
        let a = job("a", "running", "adj/game", 86, "/a");
        let mut b = job("b", "succeeded", "adj/game", 86, "/b");
        b["task_title"] = json!("PR86を直す");
        let mut db = json!({"items":[{"id":1,"status":"doing"},{"id":2,"status":"doing","jobs":["b"],"spec":"saved","note":"proof","title":"PR86を直す"}]});
        assert_eq!(reconcile_todos(&mut db, &[a.clone(), b.clone()]), vec![2]);
        assert_eq!(db["items"][1]["wait_on"], json!([1]));
        assert_eq!(db["items"][1]["note"], "proof");
        assert_eq!(db["items"][1]["spec"], "saved");
        assert!(reconcile_todos(&mut db, &[a, b]).is_empty());
    }
    #[test]
    fn title_numbers_are_not_substrings_of_other_identifiers() {
        assert_eq!(pr_number("PR #86 を直す"), Some(86));
        assert_eq!(pr_number("PR86の検査"), Some(86));
        assert_eq!(pr_number("APR86 price86"), None);
    }
}
