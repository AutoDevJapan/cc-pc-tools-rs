//! One root worker per PR, branch or checkout. Child reviews do not claim work.
use serde_json::{Value, json};
use std::{fs, path::Path};

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
    if job["work_owner"].is_object() {
        return job["work_owner"].clone();
    }
    let mut value = checkout(s(job, "cwd"));
    // A project display name is not a repository identity.
    if let Some(pr) = pr_number(s(job, "task_title")) {
        value["pr"] = json!(pr);
    }
    value
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
    let equal = |key| !s(a, key).is_empty() && s(a, key) == s(b, key);
    equal("worktree")
        || (equal("repository") && a["pr"].is_u64() && a["pr"] == b["pr"])
        || (equal("git_common") && equal("branch"))
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
            let mut candidate = j.clone();
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
pub fn reconcile_todos_if(
    db: &mut Value,
    jobs: &[Value],
    eligible: impl Fn(&Value) -> bool,
) -> Vec<i64> {
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
        let cwd = old
            .map(|j| s(j, "cwd"))
            .filter(|p| !p.is_empty())
            .unwrap_or_else(|| s(&item["scheduler"], "worktree"));
        let mut candidate = json!({"id":format!("todo-{}",item["id"]),"cwd":cwd,"project":item["project"],"task_title":item["title"],"todo_id":item["id"]});
        if let Some(old) = old {
            if live(old) {
                candidate = old.clone();
            }
            candidate["work_owner"] = from_job(old);
        }
        // Current task title supplies an explicit PR for an older record without one.
        let mut identity = from_job(&candidate);
        if s(&identity, "repository").is_empty() {
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
    let mut changed = Vec::new();
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
        item.as_object_mut().unwrap().remove("dependency_ready");
        if let Some(recovery) = item["stall_recovery"].as_object_mut() {
            recovery.remove("pending");
            recovery.remove("report");
        }
        changed.push(id);
    }
    changed
}

#[cfg(test)]
mod tests {
    use super::*;
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
