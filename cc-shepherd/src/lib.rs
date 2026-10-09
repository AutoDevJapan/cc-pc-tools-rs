//! Launch one PR-shepherd Codex job per open PR that nobody is working on, so PRs reach merge without the
//! main session (owner, 2026-10-01: keep working when Claude's usage limit is reached).
//!
//! shepherd.exe step [--dry-run] — called every minute from Discord tick.
//! shepherd.exe status
//!
//! Rules: a PR is skipped while any running codex_jobs job works in its worktree (implementation or shepherd); at
//! most MAX_RUNNING shepherds at once and one launch per call; after a shepherd ends the PR rests COOLDOWN_S
//! (REST_DECISION_S when it ended waiting for the owner) before another is started.
use cc_job_policy::todo_wait;
use cc_scheduler::{common::*, policy::live};
use serde_json::{json, Value};
use std::{
    fs,
    path::{Path, PathBuf},
};
pub const MAX_RUNNING: usize = 4; // live runs still serialize on civ6.lock; review/test steps run in parallel
pub const COOLDOWN_S: f64 = 30. * 60.;
pub const REST_DECISION_S: f64 = 3. * 3600.;
fn todo_path() -> PathBuf {
    std::env::var_os("CC_TODO_HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("D:/CC/Tools/todo"))
        .join("todo.json")
}
pub fn repos() -> Vec<Value> {
    vec![
        json!({"slug":"AutoDevJapan/civ6-banseiki","path":"C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki","wt":"C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki-wt"}),
    ]
}
pub trait Backend {
    fn run(&mut self, args: &[&str]) -> Result<String>;
    fn trees(&mut self, repo: &Value) -> Result<Value>;
    fn is_dir(&self, path: &str) -> bool;
    fn last_state(&self, job: &str) -> String;
    /// The shared ToDo database; tests without ToDo links keep the default empty one.
    fn todo(&mut self) -> Result<Value> {
        Ok(json!({"items":[]}))
    }
    /// End the one-shot "prerequisite finished" marker of these ToDos once a shepherd owns the PR.
    fn clear_dependency_ready(&mut self, _keys: &[Value]) -> Result<()> {
        Ok(())
    }
}
pub struct RealBackend {
    pub jobs: PathBuf,
    pub runner: Box<dyn Runner>,
}
impl Backend for RealBackend {
    fn run(&mut self, args: &[&str]) -> Result<String> {
        Ok(self.runner.run(&invocation(args, None))?.stdout)
    }
    fn trees(&mut self, repo: &Value) -> Result<Value> {
        let text = self.run(&[
            "git",
            "-C",
            s(&repo["path"]),
            "worktree",
            "list",
            "--porcelain",
        ])?;
        Ok(parse_trees(&text))
    }
    fn is_dir(&self, p: &str) -> bool {
        Path::new(p).is_dir()
    }
    fn todo(&mut self) -> Result<Value> {
        let path = todo_path();
        read_json(&path, json!({"items":[]}))
    }
    fn clear_dependency_ready(&mut self, keys: &[Value]) -> Result<()> {
        let path = todo_path();
        let _lock = ByteLock::acquire(
            &path.with_file_name("todo.lock"),
            std::time::Duration::from_secs(5),
        )?
        .ok_or(Error("TimeoutError"))?;
        let mut db = read_json(&path, json!({"items":[]}))?;
        let mut changed = false;
        for item in db["items"].as_array_mut().into_iter().flatten() {
            if keys.contains(&json!(release_key(item))) && item.get("dependency_ready").is_some() {
                if let Some(fields) = item.as_object_mut() {
                    fields.remove("dependency_ready");
                    changed = true;
                }
            }
        }
        if changed {
            write_json(&path, &db, 1)?;
        }
        Ok(())
    }
    fn last_state(&self, j: &str) -> String {
        fs::read_to_string(self.jobs.join(j).join("last_message.md"))
            .unwrap_or_default()
            .trim()
            .lines()
            .next()
            .unwrap_or("")
            .trim()
            .to_owned()
    }
}
/// branch -> worktree path, excluding the repository's main folder.
pub fn parse_trees(text: &str) -> Value {
    let mut found = json!({});
    for block in text.split("\n\n").skip(1) {
        let path = block.lines().find_map(|l| l.strip_prefix("worktree "));
        let branch = block
            .lines()
            .find_map(|l| l.strip_prefix("branch refs/heads/"));
        if let (Some(p), Some(b)) = (path, branch) {
            found[b.trim()] = json!(p.trim());
        }
    }
    found
}
/// True when `text` contains `needle` not followed by another digit (PR#1 must not match PR#12).
fn mentions(text: &str, needle: &str) -> bool {
    let (text, needle) = (text.to_lowercase(), needle.to_lowercase());
    let mut from = 0;
    while let Some(p) = text[from..].find(&needle) {
        let at = from + p;
        if !text[at + needle.len()..]
            .chars()
            .next()
            .is_some_and(|c| c.is_ascii_digit())
        {
            return true;
        }
        from = at + text[at..].chars().next().map_or(1, char::len_utf8);
    }
    false
}
/// Identity of one release: the ToDo id plus its `dependency_generation` (absent = 0, synthetic only).
fn release_key(item: &Value) -> String {
    format!(
        "{}:{}",
        item["id"],
        item["dependency_generation"].as_u64().unwrap_or(0)
    )
}
fn in_repo(db: &Value, item: &Value, repo: &Value) -> bool {
    let project = norm(s(&db["projects"][s(&item["project"])])).to_lowercase();
    !project.is_empty()
        && ["path", "wt"].iter().any(|field| {
            let root = norm(s(&repo[*field])).trim_end_matches('\\').to_lowercase();
            !root.is_empty()
                && (project == root || (*field == "wt" && project.starts_with(&(root + "\\"))))
        })
}
/// Unfinished ToDos tied to this PR by explicit evidence only: the PR's own jobs listed in the ToDo,
/// a job in the PR worktree carrying the ToDo's id, or an exact PR URL / `repo#N` / (same project path)
/// `PR#N` in the title or note. Nothing is matched across repositories by similarity.
pub fn linked_todos(
    db: &Value,
    repo: &Value,
    pr: &Value,
    path: &str,
    entry: &Value,
    jobs: &[Value],
) -> Vec<Value> {
    let here = norm(path);
    let mut pr_jobs: Vec<Value> = jobs
        .iter()
        .filter(|j| !s(&j["cwd"]).is_empty() && norm(s(&j["cwd"])) == here)
        .map(|j| j["id"].clone())
        .collect();
    if truth(&entry["job"]) {
        pr_jobs.push(entry["job"].clone());
    }
    let number = pr["number"].to_string();
    arr(&db["items"])
        .into_iter()
        .filter(|i| s(&i["status"]) != "done")
        .filter(|i| {
            let by_job = arr(&i["jobs"]).iter().any(|j| pr_jobs.contains(j))
                || (truth(&i["scheduler"]["job"]) && pr_jobs.contains(&i["scheduler"]["job"]))
                || jobs.iter().any(|j| {
                    (j["todo_id"] == i["id"] || arr(&j["related_todo_ids"]).contains(&i["id"]))
                        && i["id"].is_number()
                        && pr_jobs.contains(&j["id"])
                });
            let text = format!("{}\n{}", s(&i["title"]), s(&i["note"]));
            let url = !s(&pr["url"]).is_empty() && mentions(&text, s(&pr["url"]));
            let named = mentions(&text, &format!("{}#{number}", s(&repo["slug"])));
            let numbered = in_repo(db, i, repo)
                && (mentions(&text, &format!("pr#{number}"))
                    || mentions(&text, &format!("pr #{number}")));
            by_job || url || named || numbered
        })
        .collect()
}
pub fn candidates<B: Backend>(
    b: &mut B,
    repos: &[Value],
    jobs: &[Value],
    state: &Value,
    now: f64,
) -> Result<(Vec<Value>, usize)> {
    candidates_with_waits(b, repos, jobs, state, now).map(|(ready, running, _)| (ready, running))
}
/// Same as `candidates`, also reporting the PRs skipped because their ToDo waits for another ToDo.
pub fn candidates_with_waits<B: Backend>(
    b: &mut B,
    repos: &[Value],
    jobs: &[Value],
    state: &Value,
    now: f64,
) -> Result<(Vec<Value>, usize, Vec<String>)> {
    let todo = b.todo()?;
    let ownership_jobs = cc_job_policy::work_owner::observed_jobs(&todo, jobs);
    let mut waits = vec![];
    let cwds: Vec<String> = jobs
        .iter()
        .filter(|j| live(j))
        .map(|j| norm(s(&j["cwd"])))
        .collect();
    let running = jobs
        .iter()
        .filter(|j| live(j) && s(&j["name"]).starts_with("shep-"))
        .count();
    let mut ready = vec![];
    for repo in repos {
        let output = b.run(&[
            "gh",
            "pr",
            "list",
            "-R",
            s(&repo["slug"]),
            "--state",
            "open",
            "--json",
            "number,title,headRefName,url,isDraft",
        ])?;
        let prs: Value = serde_json::from_str(if output.is_empty() { "[]" } else { &output })?;
        let trees = b.trees(repo)?;
        for pr in arr(&prs) {
            let key = format!("{}#{}", s(&repo["slug"]), pr["number"]);
            if state[&key]["adopted_main_worker"] == true
                && jobs
                    .iter()
                    .any(|j| j["id"] == state[&key]["job"] && live(j))
            {
                continue;
            }
            // Decide waiting from the checkout's expected path, before any fetch/worktree is created.
            let expected = match s(&trees[s(&pr["headRefName"])]) {
                "" => format!("{}/pr-{}", s(&repo["wt"]), pr["number"]),
                found => found.to_owned(),
            };
            let candidate = json!({"id":format!("shepherd-candidate-{key}"),"work_owner":{
                "repository":s(&repo["slug"]).to_lowercase(),"pr":pr["number"],"worktree":cc_job_policy::work_owner::norm(Path::new(&expected)),"branch":pr["headRefName"]}});
            if let Some(owner) = cc_job_policy::work_owner::blocker(&candidate, &ownership_jobs) {
                waits.push(format!("{key}: 担当 {} の完了待ち", s(&owner["id"])));
                continue;
            }
            let linked = linked_todos(&todo, repo, &pr, &expected, &state[&key], jobs);
            let mut pending: Vec<i64> = linked
                .iter()
                .flat_map(|i| todo_wait::pending_on(&todo, i))
                .collect();
            pending.sort_unstable();
            pending.dedup();
            let blockers: Vec<String> =
                pending.iter().map(|id| format!("ToDo {id} 待ち")).collect();
            if !blockers.is_empty() {
                waits.push(format!("{key}: {}", blockers.join(", ")));
                continue;
            }
            let consumed = arr(&state[&key]["dependency_consumed"]);
            let released: Vec<&Value> = linked
                .iter()
                .filter(|i| {
                    i["dependency_ready"] == true && !consumed.contains(&json!(release_key(i)))
                })
                .collect();
            let mut path = s(&trees[s(&pr["headRefName"])]).to_owned();
            if path.is_empty() {
                path = format!("{}/pr-{}", s(&repo["wt"]), pr["number"]);
                if !b.is_dir(&path) {
                    b.run(&[
                        "git",
                        "-C",
                        s(&repo["path"]),
                        "fetch",
                        "-q",
                        "origin",
                        s(&pr["headRefName"]),
                    ])?;
                    b.run(&[
                        "git",
                        "-C",
                        s(&repo["path"]),
                        "worktree",
                        "add",
                        "-q",
                        &path,
                        s(&pr["headRefName"]),
                    ])?;
                }
                if !b.is_dir(&path) {
                    continue;
                }
            }
            if cwds.contains(&norm(&path)) {
                continue;
            }
            let last = &state[&key];
            if truth(&last["hold"]) {
                continue;
            }
            if truth(&last["job"]) {
                if jobs.iter().any(|j| j["id"] == last["job"] && live(j)) {
                    continue;
                }
                let ended = if truth(&last["ended"]) {
                    n(&last["ended"])
                } else {
                    now
                };
                let rest = if b.last_state(s(&last["job"])) == "状態: 要判断" {
                    REST_DECISION_S
                } else {
                    COOLDOWN_S
                };
                // A finished prerequisite lets the released PR work resume without the old rest period.
                if now - ended < rest && released.is_empty() {
                    continue;
                }
            }
            let mut entry = json!({"key":key,"repo":repo,"pr":pr,"path":path});
            if !released.is_empty() {
                entry["dependency_ids"] =
                    json!(released.iter().map(|i| i["id"].clone()).collect::<Vec<_>>());
                entry["dependency_keys"] =
                    json!(released.iter().map(|i| release_key(i)).collect::<Vec<_>>());
            }
            ready.push(entry);
        }
    }
    Ok((ready, running, waits))
}
pub fn render(template: &str, r: &Value) -> Result<String> {
    let fields = [
        ("PR_URL", s(&r["pr"]["url"])),
        ("REPO", s(&r["repo"]["slug"])),
        ("BRANCH", s(&r["pr"]["headRefName"])),
        ("WORKTREE", s(&r["path"])),
    ];
    let mut chars = template.chars().peekable();
    let mut text = String::new();
    while let Some(c) = chars.next() {
        match c {
            '{' if chars.peek() == Some(&'{') => {
                chars.next();
                text.push('{');
            }
            '}' if chars.peek() == Some(&'}') => {
                chars.next();
                text.push('}');
            }
            '{' => {
                let mut key = String::new();
                let mut closed = false;
                for c in chars.by_ref() {
                    if c == '}' {
                        closed = true;
                        break;
                    }
                    key.push(c);
                }
                if !closed {
                    return Err(Error("ValueError"));
                }
                text += fields
                    .iter()
                    .find(|(k, _)| *k == key)
                    .ok_or(Error("KeyError"))?
                    .1;
            }
            '}' => return Err(Error("ValueError")),
            _ => text.push(c),
        }
    }
    Ok(text)
}
pub fn launch_command(jobs: &Path, r: &Value, prompt: &Path) -> Invocation {
    let name = format!("shep-{}", r["pr"]["number"]);
    let title = format!(
        "{}（レビュー・仕上げ）",
        cc_task_title::summary(s(&r["pr"]["title"]))
    );
    let project = cc_task_title::project_from_folder(s(&r["path"]));
    let todo = r["todo_id"].as_u64().map(|id| id.to_string());
    let related = arr(&r["related_todo_ids"])
        .iter()
        .filter_map(Value::as_u64)
        .map(|id| id.to_string())
        .collect::<Vec<_>>()
        .join(",");
    let mut args: Vec<&str> = vec![
        "run",
        "--name",
        &name,
        "--task-title",
        &title,
        "--project",
        &project,
        "--pr-url",
        s(&r["pr"]["url"]),
        "--auto-model",
        "gpt-6.1-sol",
        "--effort",
        "high",
        "--sandbox",
        "danger-full-access",
        "--cwd",
        s(&r["path"]),
        "--add-dir",
        "D:/CC",
        "--add-dir",
        "C:/Program Files (x86)/Steam/steamapps/common/Sid Meier's Civilization VI",
        "--prompt-file",
        prompt.to_str().unwrap(),
        "--task-type",
        "pr_shepherd",
    ];
    if let Some(id) = &todo {
        // The job carries its ToDo so job admission also honours that ToDo's prerequisites.
        args.extend(["--todo-id", id]);
    }
    if !related.is_empty() {
        args.extend(["--related-todo-ids", &related]);
    }
    tool_command(&jobs.join("codex_jobs.py"), &args, None)
}
pub fn cmd_step<B: Backend>(
    root: &Path,
    jobs_dir: &Path,
    b: &mut B,
    runner: &mut dyn Runner,
    now: f64,
    dry_run: bool,
    repos: &[Value],
) -> Result<Option<Value>> {
    let _state_lock = ByteLock::acquire(
        &root.join("shepherd_state.lock"),
        std::time::Duration::from_secs(10),
    )?
    .ok_or(Error("StateLockBusy"))?;
    let statepath = root.join("shepherd_state.json");
    let mut state = read_json(&statepath, json!({}))?;
    let jobs = jobs_from(&jobs_dir.join("jobs"), false)?;
    for entry in state
        .as_object_mut()
        .ok_or(Error("ValueError"))?
        .values_mut()
    {
        if truth(&entry["job"])
            && jobs.iter().any(|j| j["id"] == entry["job"] && !live(j))
            && !truth(&entry["ended"])
        {
            entry["ended"] = json!(now);
        }
    }
    let (ready, running, waits) = candidates_with_waits(b, repos, &jobs, &state, now)?;
    if dry_run {
        let mut report = json!({"running_shepherds":running,"ready":ready.iter().map(|r|r["key"].clone()).collect::<Vec<_>>()});
        if !waits.is_empty() {
            report["waiting"] = json!(waits);
        }
        return Ok(Some(report));
    }
    if let Some(r) = ready.first().filter(|_| running < MAX_RUNNING) {
        let prompt = render(&fs::read_to_string(root.join("shepherd_prompt.md"))?, r)?;
        let folder = root.join("prompts");
        fs::create_dir_all(&folder)?;
        let pf = folder.join(format!("shep-{}.md", r["pr"]["number"]));
        fs::write(&pf, prompt)?;
        // Late recheck: a ToDo may have started waiting while the checkout and prompt were prepared.
        let todo = b.todo()?;
        let linked = linked_todos(
            &todo,
            &r["repo"],
            &r["pr"],
            s(&r["path"]),
            &state[s(&r["key"])],
            &jobs,
        );
        if linked.iter().any(|i| todo_wait::waiting(&todo, i)) {
            write_json(&statepath, &state, 1)?;
            return Ok(None);
        }
        let mut r = r.clone();
        r["related_todo_ids"] = json!(linked.iter().map(|i| i["id"].clone()).collect::<Vec<_>>());
        if let [only] = linked.as_slice() {
            r["todo_id"] = only["id"].clone();
        }
        let output = runner.run(&launch_command(jobs_dir, &r, &pf))?.stdout;
        let job = output.trim().lines().last().unwrap_or("").trim();
        if !job.is_empty() {
            // The shepherd now owns the release: end the marker so the next wait cycle starts fresh.
            let ready_keys: Vec<Value> = linked
                .iter()
                .filter(|i| i["dependency_ready"] == true)
                .map(|i| json!(release_key(i)))
                .collect();
            let cleared = ready_keys.is_empty() || b.clear_dependency_ready(&ready_keys).is_ok();
            let key = s(&r["key"]).to_owned();
            let mut consumed: Vec<Value> = arr(&state[&key]["dependency_consumed"])
                .into_iter()
                .filter(|k| !s(k).ends_with(":0"))
                .collect();
            // A generation is unique per release; generation 0 is remembered only if clearing failed.
            for k in arr(&r["dependency_keys"]) {
                if (!cleared || !s(&k).ends_with(":0")) && !consumed.contains(&k) {
                    consumed.push(k);
                }
            }
            let mut entry = json!({"job":job,"started":now});
            if !consumed.is_empty() {
                entry["dependency_consumed"] = json!(consumed);
            }
            state[&key] = entry;
            println!("shepherd started {} {}", s(&r["key"]), job);
        }
    } // Preserve the original one-space state format.
    write_json(&statepath, &state, 1)?;
    Ok(None)
}

/// Explicitly register the live main worker even when it uses a different
/// worktree/branch than the PR. A stopped worker never suppresses future work.
pub fn adopt_state(
    state: &mut Value,
    key: &str,
    job_id: &str,
    jobs: &[Value],
    now: f64,
) -> Result<()> {
    let (slug, number) = key.rsplit_once('#').ok_or(Error("InvalidPrKey"))?;
    if number
        .parse::<u64>()
        .ok()
        .filter(|n| *n > 0 && n.to_string() == number)
        .is_none()
    {
        return Err(Error("InvalidPrKey"));
    }
    let repository = repos()
        .into_iter()
        .find(|r| s(&r["slug"]) == slug)
        .ok_or(Error("UnknownRepository"))?;
    let worker = jobs
        .iter()
        .find(|j| s(&j["id"]) == job_id && live(j))
        .ok_or(Error("WorkerNotLive"))?;
    if s(&worker["cwd"]).is_empty() {
        return Err(Error("WorkerRepositoryMismatch"));
    }
    let cwd = norm(s(&worker["cwd"]));
    if !["path", "wt"].iter().any(|field| {
        let root = norm(s(&repository[*field]))
            .trim_end_matches('\\')
            .to_owned();
        cwd == root || cwd.starts_with(&(root + "\\"))
    }) {
        return Err(Error("WorkerRepositoryMismatch"));
    }
    let entries = state.as_object_mut().ok_or(Error("ValueError"))?;
    let entry = entries.entry(key.to_owned()).or_insert_with(|| json!({}));
    let fields = entry.as_object_mut().ok_or(Error("ValueError"))?;
    fields.insert("job".into(), json!(job_id));
    fields.insert("started".into(), json!(now));
    fields.insert("adopted_main_worker".into(), json!(true));
    fields.remove("ended");
    Ok(())
}

pub fn cmd_adopt(root: &Path, jobs_dir: &Path, key: &str, job: &str, now: f64) -> Result<Value> {
    let _state_lock = ByteLock::acquire(
        &root.join("shepherd_state.lock"),
        std::time::Duration::from_secs(10),
    )?
    .ok_or(Error("StateLockBusy"))?;
    let path = root.join("shepherd_state.json");
    let mut state = read_json(&path, json!({}))?;
    let jobs = jobs_from(&jobs_dir.join("jobs"), true)?;
    adopt_state(&mut state, key, job, &jobs, now)?;
    write_json(&path, &state, 1)?;
    Ok(json!({"pr":key,"main_worker":job,"registered":true}))
}
pub fn status(root: &Path) -> Result<String> {
    let path = root.join("shepherd_state.json");
    if path.exists() {
        Ok(fs::read_to_string(path)?
            .replace("\r\n", "\n")
            .replace('\r', "\n"))
    } else {
        Ok("{}".into())
    }
}
