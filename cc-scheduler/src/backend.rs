use crate::{common::*, native, policy::*};
use serde_json::{Value, json};
use std::{
    fs,
    path::{Path, PathBuf},
    time::Duration,
};

pub trait Backend {
    fn clock(&self) -> f64;
    fn measure(&mut self, state: &mut Value) -> Result<(Value, Processes)>;
    fn snapshot(&mut self) -> Result<Processes>;
    fn jobs(&mut self) -> Result<Vec<Value>>;
    fn event_mtime(&mut self, job: &Value) -> Result<f64>;
    fn mark_lost(&mut self, job: &mut Value) -> Result<bool>;
    fn launch(&mut self, item: &Value, prompt: &Path, selection: &Value) -> Result<String>;
    fn cancel(&mut self, job: &mut Value) -> Result<bool>;
    fn cancel_for_recovery(&mut self, job: &mut Value) -> Result<bool> {
        self.cancel(job)
    }
    fn kill_orphan(&mut self, c: &Value) -> Result<bool>;
    fn pick(&mut self, prompt: &str) -> Result<Value>;
    fn model_exists(&mut self, model: &str) -> bool;
    fn notify(&mut self, item: &Value) -> Result<Value>;
    fn reply(&mut self, receipt: &Value) -> Result<Option<Value>>;
    fn close_question(&mut self, receipt: &Value) -> Result<()>;
    fn outcome(&mut self, job: &Value) -> Result<String>;
    fn worktree(&mut self, repo: &str, item: &Value) -> Result<()>;
}
pub trait Http {
    fn request(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
        timeout: u64,
    ) -> Result<Value>;
}
pub struct Network;
pub fn token() -> Result<String> {
    let mut value = std::env::var("GODD_DISCORD_BOT_TOKEN").unwrap_or_default();
    if value.is_empty() {
        use winreg::{RegKey, enums::HKEY_CURRENT_USER};
        value = RegKey::predef(HKEY_CURRENT_USER)
            .open_subkey("Environment")
            .and_then(|k| k.get_value("GODD_DISCORD_BOT_TOKEN"))
            .unwrap_or_default();
    }
    if value.is_empty() {
        Err(Error("SystemExit"))
    } else {
        Ok(value)
    }
}
impl Http for Network {
    fn request(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
        timeout: u64,
    ) -> Result<Value> {
        let client = reqwest::blocking::Client::builder()
            .timeout(Duration::from_secs(timeout))
            .user_agent("discord-ask (local, 1.0)")
            .build()
            .map_err(|_| Error("OSError"))?;
        for _ in 0..if discord { 3 } else { 1 } {
            let mut request = client
                .request(
                    reqwest::Method::from_bytes(method.as_bytes())
                        .map_err(|_| Error("ValueError"))?,
                    url,
                )
                .header("Content-Type", "application/json");
            if discord {
                let mut header =
                    reqwest::header::HeaderValue::from_str(&format!("Bot {}", token()?))
                        .map_err(|_| Error("ValueError"))?;
                header.set_sensitive(true);
                request = request.header(reqwest::header::AUTHORIZATION, header);
            }
            if let Some(body) = body {
                request = request.json(body);
            }
            let response = request.send().map_err(|_| Error("OSError"))?;
            let status = response.status();
            let text = response.text().map_err(|_| Error("OSError"))?;
            let data = if text.is_empty() {
                Value::Null
            } else {
                serde_json::from_str(&text)?
            };
            if discord && status.as_u16() == 429 {
                std::thread::sleep(Duration::from_secs_f64(
                    data.get("retry_after").map(n).unwrap_or(1.),
                ));
                continue;
            }
            if !status.is_success() {
                return Err(Error(if discord { "SystemExit" } else { "OSError" }));
            }
            return Ok(data);
        }
        Err(Error("SystemExit"))
    }
}
#[derive(Default)]
pub struct ModelsCache {
    pub at: f64,
    pub ids: Vec<String>,
}
impl ModelsCache {
    /// Whether OpenCodex serves this model id (bare or provider/model). Cached for 10 minutes; if OpenCodex cannot be
    /// asked, assume it exists rather than blocking work.
    pub fn exists(&mut self, http: &mut dyn Http, model: &str, now: f64) -> bool {
        if now - self.at > 600. {
            match http.request("GET", "http://127.0.0.1:10100/v1/models", None, false, 5) {
                Ok(v) => {
                    self.ids = arr(v.get("data").unwrap_or(&v["models"]))
                        .iter()
                        .map(|m| s(&m["id"]).to_string())
                        .collect();
                    self.at = now;
                }
                Err(_) => return true,
            }
        }
        model == "auto"
            || self
                .ids
                .iter()
                .any(|i| i == model || i.split_once('/').map(|(_, m)| m) == Some(model))
    }
}
pub fn safe_text(text: &str) -> String {
    let mut text = text.to_string();
    let secret_name = regex::Regex::new("(?i)TOKEN|PASSWORD|SECRET|API_KEY|WEBHOOK").unwrap();
    for (name, secret) in std::env::vars() {
        if secret_name.is_match(&name) && secret.len() >= 8 {
            text = text.replace(&secret, "[秘密]");
        }
    }
    for (pattern, replacement) in [
        (
            r#"(?i)https://(?:discord\.com|discordapp\.com)/api/webhooks/[^\s\"']+"#,
            "[秘密]",
        ),
        (
            r#"(?i)(authorization[\"']?\s*[:=]\s*[\"']?)(?:Bearer|Bot)\s+[^\s\"']+"#,
            "${1}[秘密]",
        ),
        (
            r#"(?i)((?:api[_-]?key|access[_-]?token|password|secret|bot[_-]?token)[\"']?\s*[:=]\s*[\"']?)[^\s\"',}]+"#,
            "${1}[秘密]",
        ),
    ] {
        text = regex::Regex::new(pattern)
            .unwrap()
            .replace_all(&text, replacement)
            .into_owned();
    }
    text
}
pub fn valid_model(m: &str) -> bool {
    regex::Regex::new(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$")
        .unwrap()
        .is_match(m)
}
pub fn valid_effort(e: &str) -> bool {
    matches!(
        e,
        "none" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max"
    )
}
pub fn pick_model(http: &mut dyn Http, task: &str) -> Result<Value> {
    let result=http.request("POST","http://127.0.0.1:11500/api/pick",Some(&json!({"task":safe_text(task),"exclude":[]})),false,20).and_then(|v|{if !["model","effort","task_type","reason"].iter().all(|k|!s(&v[k]).trim().is_empty())||!valid_model(s(&v["model"]))||!valid_effort(s(&v["effort"]))||!regex::Regex::new(r"^[a-z][a-z0-9_-]{0,63}$").unwrap().is_match(s(&v["task_type"])){return Err(Error("ValueError"));}let reason=safe_text(s(&v["reason"])).split_whitespace().collect::<Vec<_>>().join(" ").chars().take(400).collect::<String>();let profile=cc_ask::job_policy::model_profile("",s(&v["task_type"]));let model=profile.model;let mut selected=json!({"model":model,"effort":profile.effort,"task_type":v["task_type"],"reason":reason,"source":v.get("source").unwrap_or(&json!("jev"))});if s(&selected["source"])=="fallback"{println!("pick fallback (router) {} {}",s(&selected["model"]),s(&selected["task_type"]));}remove(&mut selected,"exclude");Ok(selected)});
    match result {
        Ok(v) => Ok(v),
        Err(e) => {
            let lower = task.to_lowercase();
            let kind = [
                (
                    "live_debug",
                    vec![
                        "debug",
                        "デバッグ",
                        "原因調査",
                        "原因を調べ",
                        "停止原因",
                        "障害を調査",
                        "エラーの原因",
                        "失敗の原因",
                    ],
                ),
                (
                    "review",
                    vec![
                        "レビューして",
                        "レビューしてください",
                        "レビューをお願いします",
                        "コードレビュー",
                        "review this",
                        "review the",
                        "review pr",
                        "code review",
                    ],
                ),
                ("documentation", vec!["readme", "ドキュメント", "文書"]),
                ("research", vec!["research", "調査", "調べて"]),
            ]
            .into_iter()
            .find(|(_, words)| words.iter().any(|w| lower.contains(w)))
            .map(|(k, _)| k)
            .unwrap_or("implementation");
            let profile = cc_ask::job_policy::model_profile("", kind);
            let model = profile.model;
            eprintln!("pick fallback (local) {} {} {}", e.0, model, kind);
            Ok(
                json!({"model":model,"effort":profile.effort,"task_type":kind,"source":"fallback","reason":"Jev のモデル選択が準備中のため、作業の種類ごとの既定の割り当てで選びました。"}),
            )
        }
    }
}

pub struct RealBackend {
    pub tools: PathBuf,
    pub root: PathBuf,
    pub runner: Box<dyn Runner>,
    pub http: Box<dyn Http>,
    pub models: ModelsCache,
    pub config_path: PathBuf,
}
impl RealBackend {
    fn cancel_job(&mut self, j: &mut Value, recovery: bool) -> Result<bool> {
        backup(&self.job_dir(j).join("job.json"))?;
        let mut args = vec!["cancel", s(&j["id"])];
        if recovery {
            args.push("--recovery");
        }
        let c = tool_command(
            &self.tools.join("codex-jobs/codex_jobs.py"),
            &args,
            Some(45),
        );
        let result = self.runner.run(&c)?;
        if let Ok(fresh) = read_json(&self.job_dir(j).join("job.json"), Value::Null) {
            if fresh.is_object() {
                *j = fresh;
            }
        }
        Ok(result.code == 0 && !alive(j, &native::process_snapshot()?, now()))
    }
    pub fn new(root: PathBuf) -> Self {
        Self {
            tools: root.parent().unwrap().to_owned(),
            root,
            runner: Box::new(SystemRunner),
            http: Box::new(Network),
            models: ModelsCache::default(),
            config_path: PathBuf::from(std::env::var_os("USERPROFILE").unwrap_or_default())
                .join(".godd/discord-notify.json"),
        }
    }
    fn job_dir(&self, j: &Value) -> PathBuf {
        self.tools.join("codex-jobs/jobs").join(s(&j["id"]))
    }
    fn call_tool(&mut self, tool: &str, args: &[&str], timeout: Option<u64>) -> Result<String> {
        let output = self
            .runner
            .run(&tool_command(&self.tools.join(tool), args, timeout))?;
        if output.code != 0 {
            return Err(Error("RuntimeError"));
        }
        Ok(output.stdout)
    }
    pub fn launch_command(&self, item: &Value, prompt: &Path, selection: &Value) -> Invocation {
        let sandbox = if cc_ask::job_policy::explicit_read_only(s(&item["title"])) {
            "read-only"
        } else {
            selection["sandbox"]
                .as_str()
                .or(item["sandbox"].as_str())
                .unwrap_or("")
        };
        let policy = cc_ask::job_policy::for_task(s(&selection["task_type"]), sandbox);
        let mut invocation = tool_command(
            &self.tools.join("codex-jobs/codex_jobs.py"),
            &[
                "run",
                "--name",
                &format!("todo-{}", item["id"]),
                "--task-title",
                s(&item["title"]),
                "--project",
                s(&item["project"]),
                "--todo-id",
                &item["id"].to_string(),
                if truth(&item["model"]) {
                    "--model"
                } else {
                    "--auto-model"
                },
                s(&selection["model"]),
                "--effort",
                s(&selection["effort"]),
                "--task-type",
                s(&selection["task_type"]),
                "--cwd",
                s(&item["scheduler"]["worktree"]),
                "--sandbox",
                policy.sandbox,
                "--add-dir",
                self.tools.parent().unwrap().to_str().unwrap(),
                "--prompt-file",
                prompt.to_str().unwrap(),
                "--launch-key",
                s(&item["scheduler"]["key"]),
            ],
            Some(45),
        );
        if let Some(session) = item["scheduler"]["resume_session"]
            .as_str()
            .filter(|session| uuid::Uuid::parse_str(session).is_ok())
        {
            invocation
                .args
                .extend(["--resume-session".into(), session.into()]);
        }
        invocation
    }
}
impl Backend for RealBackend {
    fn clock(&self) -> f64 {
        now()
    }
    fn measure(&mut self, state: &mut Value) -> Result<(Value, Processes)> {
        native::measure(state)
    }
    fn snapshot(&mut self) -> Result<Processes> {
        native::process_snapshot()
    }
    fn jobs(&mut self) -> Result<Vec<Value>> {
        jobs_from(&self.tools.join("codex-jobs/jobs"), true)
    }
    fn event_mtime(&mut self, j: &Value) -> Result<f64> {
        let p = self.job_dir(j).join("events.jsonl");
        if p.exists() {
            Ok(p.metadata()?
                .modified()?
                .duration_since(std::time::UNIX_EPOCH)
                .map_err(|_| Error("OSError"))?
                .as_secs_f64())
        } else {
            Ok(0.)
        }
    }
    fn mark_lost(&mut self, j: &mut Value) -> Result<bool> {
        let dir = self.job_dir(j);
        let _lock = ByteLock::acquire(&dir.join("state.lock"), Duration::from_secs(10))?
            .ok_or(Error("OSError"))?;
        let mut fresh = read_json(&dir.join("job.json"), Value::Null)?;
        if !live(&fresh)
            || ["pid", "codex_pid"].iter().any(|k| {
                fresh[k].as_u64().is_some_and(|pid| {
                    native::process_birth(pid as u32)
                        .is_some_and(|c| Some(c) == fresh[format!("{k}_created")].as_u64())
                })
            })
            || now() - epoch(&fresh["created_at"]) < 60.
        {
            return Ok(false);
        }
        if alive(&fresh, &native::process_snapshot()?, now()) {
            return Ok(false);
        }
        merge(
            &mut fresh,
            json!({"status":"lost","finished_at":chrono::Local::now().format("%Y-%m-%dT%H:%M:%S%z").to_string(),"last_error":"scheduler: both job roots are gone"}),
        );
        write_json(&dir.join("job.json"), &fresh, 1)?;
        *j = fresh;
        Ok(true)
    }
    fn launch(&mut self, i: &Value, p: &Path, sel: &Value) -> Result<String> {
        let c = self.launch_command(i, p, sel);
        let output = self.runner.run(&c)?;
        if output.code != 0 {
            return Err(Error("RuntimeError"));
        }
        let jid = output
            .stdout
            .trim()
            .lines()
            .last()
            .unwrap_or("")
            .to_string();
        if !regex::Regex::new(r"^[A-Za-z0-9_-]+$")
            .unwrap()
            .is_match(&jid)
        {
            return Err(Error("ValueError"));
        }
        Ok(jid)
    }
    fn cancel(&mut self, j: &mut Value) -> Result<bool> {
        self.cancel_job(j, false)
    }
    fn cancel_for_recovery(&mut self, j: &mut Value) -> Result<bool> {
        self.cancel_job(j, true)
    }
    fn kill_orphan(&mut self, c: &Value) -> Result<bool> {
        Ok(native::kill_orphan(c))
    }
    fn pick(&mut self, prompt: &str) -> Result<Value> {
        pick_model(self.http.as_mut(), prompt)
    }
    fn model_exists(&mut self, m: &str) -> bool {
        self.models.exists(self.http.as_mut(), m, now())
    }
    fn notify(&mut self, i: &Value) -> Result<Value> {
        let statepath = self.tools.join("discord-ask/state.json");
        let lockpath = self.tools.join("discord-ask/state.lock");
        {
            let _lock = ByteLock::acquire(&lockpath, Duration::from_secs(60))?
                .ok_or(Error("SystemExit"))?;
            let state = read_json(&statepath, json!({"questions":[]}))?;
            if let Some(q) = arr(&state["questions"]).iter().find(|q| {
                s(&q["source"]) == "scheduler"
                    && q["scheduler_item"] == i["id"]
                    && q["scheduler_job"] == i["scheduler"]["last_job"]
            }) {
                return Ok(json!({"thread_id":q["thread_id"]}));
            }
        }
        let body = self.root.join(format!("question-{}.md", i["id"]));
        backup(&body)?;
        fs::write(&body, question_body(i))?;
        backup(&statepath)?;
        let title = format!("{}の停止原因を確認する", s(&i["title"]));
        let cwd = s(&i["scheduler"]["worktree"]);
        let output = self.call_tool(
            "discord-ask/discord_ask.py",
            &[
                "ask",
                "--agent",
                "Scheduler",
                "--title",
                &title,
                "--body-file",
                body.to_str().unwrap(),
                "--cwd",
                cwd,
                "--source",
                "scheduler",
                "--scheduler-item",
                &i["id"].to_string(),
                "--scheduler-job",
                s(&i["scheduler"]["last_job"]),
            ],
            Some(90),
        )?;
        Ok(serde_json::from_str(&output)?)
    }
    fn reply(&mut self, r: &Value) -> Result<Option<Value>> {
        let _lock = ByteLock::acquire(
            &self.tools.join("discord-ask/state.lock"),
            Duration::from_secs(60),
        )?
        .ok_or(Error("SystemExit"))?;
        let mut state = read_json(
            &self.tools.join("discord-ask/state.json"),
            json!({"questions":[]}),
        )?;
        let Some(q) = arr(&state["questions"])
            .into_iter()
            .find(|q| q["thread_id"] == r["thread_id"] && s(&q["source"]) == "scheduler")
        else {
            return Ok(None);
        };
        if !truth(&q["open"]) {
            return Ok(None);
        }
        let cfg = read_json(&self.config_path, Value::Null)?;
        let path = format!(
            "https://discord.com/api/v10/channels/{}/messages?after={}&limit=100",
            s(&r["thread_id"]),
            s(&q["after"])
        );
        let messages = self.http.request("GET", &path, None, true, 20)?;
        let owner = pystr(&cfg["owner_user_id"]);
        let replies: Vec<_> = arr(&messages)
            .into_iter()
            .filter(|m| {
                s(&m["author"]["id"]) == owner
                    && s(&m["id"]).parse::<u64>().unwrap_or(0)
                        > s(&q["after"]).parse::<u64>().unwrap_or(0)
            })
            .collect();
        cc_ask::snooze::apply_thread(&mut state, s(&r["thread_id"]), &replies, &owner);
        write_json(&self.tools.join("discord-ask/state.json"), &state, 2)?;
        if cc_ask::snooze::thread_paused(&state, s(&r["thread_id"]), now()) {
            return Ok(None);
        }
        let Some(response) = replies
            .into_iter()
            .filter(|m| !cc_ask::snooze::control_only(m))
            .max_by_key(|m| s(&m["id"]).parse::<u64>().unwrap_or(0))
        else {
            return Ok(None);
        };
        let text = safe_text(s(&response["content"])).trim().to_string();
        if text.is_empty() {
            return Ok(None);
        }
        let stop = regex::Regex::new(r"(?i)^(?:1\s*)?b(?:$|[.、。:\s])")
            .unwrap()
            .is_match(&text)
            || ["停止したまま", "再開しない", "止めておく"]
                .iter()
                .any(|w| text.contains(w));
        Ok(Some(json!({"id":response["id"],"text":text,"stop":stop})))
    }
    fn close_question(&mut self, r: &Value) -> Result<()> {
        backup(&self.tools.join("discord-ask/state.json"))?;
        self.call_tool(
            "discord-ask/discord_ask.py",
            &["close", s(&r["thread_id"])],
            Some(90),
        )?;
        Ok(())
    }
    fn outcome(&mut self, j: &Value) -> Result<String> {
        let path = self.job_dir(j).join("last_message.md");
        if !path.exists() {
            return Ok("todo".into());
        }
        let result = fs::read_to_string(path)?;
        cc_ask::core::log_result_misuse(s(&j["id"]), &result);
        Ok(result_outcome(&result).into())
    }
    fn worktree(&mut self, repo: &str, i: &Value) -> Result<()> {
        ensure_worktree(self.runner.as_mut(), repo, i)
    }
}
pub fn result_outcome(result: &str) -> &'static str {
    match cc_ask::core::result_state(result) {
        "状態: 完了" => "done",
        "状態: 要判断" => "block",
        _ => "todo",
    }
}
pub fn question_body(i: &Value) -> String {
    format!(
        "🙇 {} の作業が2回止まったため、再開方法を決めてください。\n作業: {}\na. 原因を調べてから再開する（推奨）。同じ停止が続くのを防げます。\nb. この作業を停止したままにする。ほかの作業を進めます。\na か b で教えてほしいです！\n回答後の再開も、PC の負荷と作業の優先順を確認してから開始します。\n共有 ToDo の項目番号: {}\n作業フォルダ: {}\n",
        s(&i["project"]),
        s(&i["title"]),
        i["id"],
        s(&i["scheduler"]["worktree"])
    )
}
pub fn run_git(runner: &mut dyn Runner, repo: &Path, args: &[&str]) -> Result<String> {
    let mut c = invocation(&["git", "-C", repo.to_str().unwrap()], Some(40));
    c.args.extend(args.iter().map(|x| x.to_string()));
    c.git_prompt_off = true;
    let out = runner.run(&c)?;
    if out.code != 0 {
        return Err(Error("RuntimeError"));
    }
    Ok(out.stdout.trim().to_string())
}
pub fn ensure_worktree(runner: &mut dyn Runner, repo: &str, i: &Value) -> Result<()> {
    let base = resolve(Path::new(repo))?;
    let target = resolve(Path::new(s(&i["scheduler"]["worktree"])))?;
    let container = PathBuf::from(format!("{}-wt", base.display()));
    let branch = s(&i["scheduler"]["branch"]);
    if target.parent() != Some(container.as_path())
        || !branch.starts_with(&format!("scheduler/todo-{}-", i["id"]))
    {
        return Err(Error("ValueError"));
    }
    if target.exists() {
        if norm(&run_git(
            runner,
            &target,
            &["rev-parse", "--show-toplevel"],
        )?) != norm(target.to_str().unwrap())
            || run_git(runner, &target, &["branch", "--show-current"])? != branch
        {
            return Err(Error("ValueError"));
        }
        let left = resolve(&target.join(run_git(
            runner,
            &target,
            &["rev-parse", "--git-common-dir"],
        )?))?;
        let right =
            resolve(&base.join(run_git(runner, &base, &["rev-parse", "--git-common-dir"])?))?;
        if norm(left.to_str().unwrap()) != norm(right.to_str().unwrap()) {
            return Err(Error("ValueError"));
        }
        return Ok(());
    }
    if run_git(runner, &base, &["rev-parse", "--is-inside-work-tree"])? != "true" {
        return Err(Error("ValueError"));
    }
    run_git(runner, &base, &["fetch", "--quiet", "origin", "main"])?;
    run_git(
        runner,
        &base,
        &["rev-parse", "--verify", "refs/remotes/origin/main"],
    )?;
    fs::create_dir_all(container)?;
    run_git(
        runner,
        &base,
        &[
            "worktree",
            "add",
            "-b",
            branch,
            target.to_str().unwrap(),
            "origin/main",
        ],
    )?;
    Ok(())
}
