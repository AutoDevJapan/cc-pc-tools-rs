//! Launch one PR-shepherd Codex job per open PR that nobody is working on, so PRs reach merge without the
//! main session (owner, 2026-10-01: keep working when Claude's usage limit is reached).
//!
//! shepherd.exe step [--dry-run] — called every minute from Discord tick.
//! shepherd.exe status
//!
//! Rules: a PR is skipped while any running codex_jobs job works in its worktree (implementation or shepherd); at
//! most MAX_RUNNING shepherds at once and one launch per call; after a shepherd ends the PR rests COOLDOWN_S
//! (REST_DECISION_S when it ended waiting for the owner) before another is started.
use cc_scheduler::{common::*, policy::live};
use serde_json::{json, Value};
use std::{
    fs,
    path::{Path, PathBuf},
};
pub const MAX_RUNNING: usize = 4; // live runs still serialize on civ6.lock; review/test steps run in parallel
pub const COOLDOWN_S: f64 = 30. * 60.;
pub const REST_DECISION_S: f64 = 3. * 3600.;
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
pub fn candidates<B: Backend>(
    b: &mut B,
    repos: &[Value],
    jobs: &[Value],
    state: &Value,
    now: f64,
) -> Result<(Vec<Value>, usize)> {
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
                if now - ended < rest {
                    continue;
                }
            }
            ready.push(json!({"key":key,"repo":repo,"pr":pr,"path":path}));
        }
    }
    Ok((ready, running))
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
    tool_command(
        &jobs.join("codex_jobs.py"),
        &[
            "run",
            "--name",
            &format!("shep-{}", r["pr"]["number"]),
            "--task-title",
            &format!(
                "{}（レビュー・仕上げ）",
                cc_task_title::summary(s(&r["pr"]["title"]))
            ),
            "--project",
            &cc_task_title::project_from_folder(s(&r["path"])),
            "--model",
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
        ],
        None,
    )
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
    let (ready, running) = candidates(b, repos, &jobs, &state, now)?;
    if dry_run {
        return Ok(Some(
            json!({"running_shepherds":running,"ready":ready.iter().map(|r|r["key"].clone()).collect::<Vec<_>>()}),
        ));
    }
    if let Some(r) = ready.first().filter(|_| running < MAX_RUNNING) {
        let prompt = render(&fs::read_to_string(root.join("shepherd_prompt.md"))?, r)?;
        let folder = root.join("prompts");
        fs::create_dir_all(&folder)?;
        let pf = folder.join(format!("shep-{}.md", r["pr"]["number"]));
        fs::write(&pf, prompt)?;
        let output = runner.run(&launch_command(jobs_dir, r, &pf))?.stdout;
        let job = output.trim().lines().last().unwrap_or("").trim();
        if !job.is_empty() {
            state[s(&r["key"])] = json!({"job":job,"started":now});
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
