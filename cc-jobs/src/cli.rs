use crate::notify::task_cancel;
use crate::queue::valid_job_id;
use crate::*;
use std::{
    collections::{BTreeMap, HashMap, HashSet},
    fs,
    io::Write,
    path::{Path, PathBuf},
};
pub fn help_text(command: &str) -> &'static str {
    match command {
        "run" => include_str!("../tests/fixtures/help-run.txt"),
        "status" => include_str!("../tests/fixtures/help-status.txt"),
        "tail" => include_str!("../tests/fixtures/help-tail.txt"),
        "wait" => include_str!("../tests/fixtures/help-wait.txt"),
        "watch" => include_str!("../tests/fixtures/help-watch.txt"),
        "cancel" => include_str!("../tests/fixtures/help-cancel.txt"),
        "rate" => include_str!("../tests/fixtures/help-rate.txt"),
        "backfill" => include_str!("../tests/fixtures/help-backfill.txt"),
        "recover-429" => {
            "usage: codex_jobs.exe recover-429 --since <RFC3339> [--todo-file <path>] [--apply]\n\nWithout --apply, print the recovery plan only.\n"
        }
        "recover-children" => {
            "usage: codex_jobs.exe recover-children --since <RFC3339> [--apply]\n\nLink old queued children and recover never-started cancelled children of running parents.\n"
        }
        _ => include_str!("../tests/fixtures/help-root.txt"),
    }
}
pub fn with_usage(mut e: Error) -> Error {
    if e.code == 2 {
        let command = e
            .message
            .strip_prefix("codex_jobs.py ")
            .and_then(|s| s.split(':').next())
            .unwrap_or("");
        let help = help_text(command);
        let usage = help.split("\n\n").next().unwrap();
        e.message = format!("{usage}\n{}", e.message);
    }
    e
}
#[derive(Debug, Clone)]
pub struct Args {
    pub cmd: String,
    pub id: String,
    pub values: BTreeMap<String, String>,
    pub flags: HashSet<String>,
    pub add_dirs: Vec<String>,
}
impl Args {
    pub fn value(&self, name: &str) -> Option<&str> {
        self.values.get(name).map(String::as_str)
    }
    pub fn flag(&self, name: &str) -> bool {
        self.flags.contains(name)
    }
    pub fn required(&self, name: &str) -> Result<&str> {
        self.value(name).ok_or_else(|| Error {
            code: 2,
            message: format!(
                "codex_jobs.py {}: error: the following arguments are required: --{}",
                self.cmd, name
            ),
        })
    }
}
pub fn parse(args: &[String]) -> Result<Args> {
    let Some(cmd) = args.first() else {
        return Err(Error {
            code: 2,
            message: "codex_jobs.py: error: the following arguments are required: cmd".into(),
        });
    };
    if ![
        "run",
        "status",
        "tail",
        "wait",
        "watch",
        "cancel",
        "rate",
        "backfill",
        "_supervise",
        "recover-429",
        "reload-queue",
        "recover-children",
    ]
    .contains(&cmd.as_str())
    {
        return Err(Error {
            code: 2,
            message: format!(
                "codex_jobs.py: error: argument cmd: invalid choice: '{cmd}' (choose from run, status, tail, wait, watch, cancel, rate, backfill)"
            ),
        });
    }
    let mut a = Args {
        cmd: cmd.clone(),
        id: String::new(),
        values: BTreeMap::new(),
        flags: HashSet::new(),
        add_dirs: vec![],
    };
    let mut i = 1;
    while i < args.len() {
        let arg = &args[i];
        if arg.starts_with('-') {
            let mut parts = arg.splitn(2, '=');
            let mut name = parts.next().unwrap().trim_start_matches('-').to_string();
            if name == "f" {
                name = "follow".into();
            }
            let val = parts.next();
            let is_flag = match cmd.as_str() {
                "run" => name == "network",
                "status" => ["all", "json"].contains(&name.as_str()),
                "tail" => name == "follow",
                "rate" => name == "force",
                "cancel" => name == "recovery",
                "recover-429" | "recover-children" => name == "apply",
                _ => false,
            };
            let allowed = match cmd.as_str() {
                "run" => [
                    "name",
                    "task-title",
                    "project",
                    "todo-id",
                    "model",
                    "effort",
                    "sandbox",
                    "scope-source",
                    "cwd",
                    "add-dir",
                    "network",
                    "mcp-profile",
                    "prompt-file",
                    "task-type",
                    "notes-hint",
                    "launch-key",
                    "resume-session",
                    "parent-job-id",
                ]
                .contains(&name.as_str()),
                "status" => ["all", "json"].contains(&name.as_str()),
                "tail" => ["n", "follow"].contains(&name.as_str()),
                "wait" => name == "timeout",
                "watch" => name == "interval",
                "rate" => ["quality", "task-type", "notes", "force"].contains(&name.as_str()),
                "backfill" => name == "days",
                "cancel" => name == "recovery",
                "recover-429" => ["since", "todo-file", "apply"].contains(&name.as_str()),
                "recover-children" => ["since", "apply"].contains(&name.as_str()),
                _ => false,
            };
            if !allowed {
                return Err(Error {
                    code: 2,
                    message: format!("codex_jobs.py: error: unrecognized arguments: {arg}"),
                });
            }
            if is_flag {
                if let Some(val) = val {
                    return Err(Error {
                        code: 2,
                        message: format!(
                            "codex_jobs.py {cmd}: error: argument --{name}: ignored explicit argument '{}'",
                            val
                        ),
                    });
                }
                a.flags.insert(name);
            } else {
                let value = if let Some(v) = val {
                    v.to_string()
                } else {
                    i += 1;
                    args.get(i).cloned().ok_or_else(|| Error {
                        code: 2,
                        message: format!(
                            "codex_jobs.py {cmd}: error: argument --{name}: expected one argument"
                        ),
                    })?
                };
                if name == "add-dir" {
                    a.add_dirs.push(value);
                } else {
                    a.values.insert(name, value);
                }
            }
        } else if a.id.is_empty()
            && ["tail", "wait", "cancel", "rate", "_supervise"].contains(&cmd.as_str())
        {
            a.id = arg.clone();
        } else {
            return Err(Error {
                code: 2,
                message: format!("codex_jobs.py: error: unrecognized arguments: {arg}"),
            });
        }
        i += 1;
    }
    if cmd == "run" {
        let missing = ["name", "model", "effort", "prompt-file"]
            .into_iter()
            .filter(|name| a.value(name).is_none())
            .map(|name| format!("--{name}"))
            .collect::<Vec<_>>();
        if !missing.is_empty() {
            return Err(Error {
                code: 2,
                message: format!(
                    "codex_jobs.py run: error: the following arguments are required: {}",
                    missing.join(", ")
                ),
            });
        }
        for (name, default) in [("sandbox", "read-only"), ("mcp-profile", "minimal")] {
            a.values.entry(name.into()).or_insert(default.into());
        }
        for (name, choices) in [
            (
                "sandbox",
                &["read-only", "workspace-write", "danger-full-access"][..],
            ),
            ("mcp-profile", &["minimal", "browser", "full"][..]),
        ] {
            let v = a.required(name)?;
            if !choices.contains(&v) {
                return Err(Error {
                    code: 2,
                    message: format!(
                        "codex_jobs.py run: error: argument --{name}: invalid choice: '{v}'"
                    ),
                });
            }
        }
    }
    if ["tail", "wait", "cancel", "rate", "_supervise"].contains(&cmd.as_str()) && a.id.is_empty() {
        return Err(Error {
            code: 2,
            message: format!(
                "codex_jobs.py {cmd}: error: the following arguments are required: id"
            ),
        });
    }
    for (name, default) in match cmd.as_str() {
        "tail" => vec![("n", "20")],
        "watch" => vec![("interval", "2.0")],
        "backfill" => vec![("days", "7")],
        _ => vec![],
    } {
        a.values.entry(name.into()).or_insert(default.into());
    }
    for name in ["n", "days", "quality"] {
        if cmd == "rate" && name == "quality" {
            a.required(name)?;
        }
        if let Some(v) = a.value(name) {
            let n = v.parse::<i64>().map_err(|_| Error {
                code: 2,
                message: format!(
                    "codex_jobs.py {cmd}: error: argument {}: invalid int value: '{v}'",
                    if name == "n" {
                        "-n".into()
                    } else {
                        format!("--{name}")
                    }
                ),
            })?;
            if name == "days" && n < 1 {
                return Err(Error {
                    code: 2,
                    message:
                        "codex_jobs.py backfill: error: argument --days: --days must be positive"
                            .into(),
                });
            }
            if name == "quality" && !(1..=5).contains(&n) {
                return Err(Error {
                    code: 2,
                    message: format!(
                        "codex_jobs.py rate: error: argument --quality: invalid choice: '{n}' (choose from 1, 2, 3, 4, 5)"
                    ),
                });
            }
        }
    }
    for name in ["timeout", "interval"] {
        if let Some(v) = a.value(name) {
            v.parse::<f64>().map_err(|_| Error {
                code: 2,
                message: format!(
                    "codex_jobs.py {cmd}: error: argument --{name}: invalid float value: '{v}'"
                ),
            })?;
        }
    }
    Ok(a)
}
pub fn normalize_windows_path(p: &Path) -> PathBuf {
    let text = p.to_string_lossy();
    if let Some(unc) = text.strip_prefix(r"\\?\UNC\") {
        return PathBuf::from(format!(r"\\{unc}"));
    }
    PathBuf::from(text.strip_prefix(r"\\?\").unwrap_or(&text))
}
fn abs(p: &Path) -> std::io::Result<PathBuf> {
    if p.exists() {
        let p = fs::canonicalize(p)?;
        #[cfg(windows)]
        {
            return Ok(normalize_windows_path(&p));
        }
        #[cfg(not(windows))]
        {
            return Ok(p);
        }
    }
    let absolute = if p.is_absolute() {
        p.to_path_buf()
    } else {
        std::env::current_dir()?.join(p)
    };
    let mut normalized = PathBuf::new();
    for c in absolute.components() {
        match c {
            std::path::Component::CurDir => (),
            std::path::Component::ParentDir => {
                normalized.pop();
            }
            _ => normalized.push(c.as_os_str()),
        }
    }
    Ok(normalized)
}
impl Context {
    pub fn dispatch(
        &self,
        args: &[String],
        out: &mut dyn Write,
        err: &mut dyn Write,
    ) -> Result<i32> {
        if args.iter().any(|s| s == "--help" || s == "-h") {
            out.write_all(help_text(args.first().map(String::as_str).unwrap_or("")).as_bytes())?;
            return Ok(0);
        }
        let a = parse(args).map_err(with_usage)?;
        match a.cmd.as_str() {
            "run" => self.cmd_run(&a, out, err),
            "status" => self.cmd_status(&a, out),
            "tail" => self.cmd_tail(&a, out),
            "wait" => self.cmd_wait(&a, out),
            "watch" => {
                let mut seen = HashMap::new();
                for j in self.list_jobs(&HashSet::new()) {
                    let j = self.refresh(j, true)?;
                    seen.insert(s(&j, "id").into(), s(&j, "status").into());
                }
                loop {
                    self.rt.sleep(a.required("interval")?.parse().unwrap());
                    for l in self.watch_step(&mut seen)? {
                        writeln!(out, "{l}")?;
                        out.flush()?;
                    }
                }
            }
            "cancel" => self.cmd_cancel_mode(&a.id, a.flag("recovery"), out, err),
            "rate" => self.cmd_rate(&a, out),
            "backfill" => self.cmd_backfill(a.required("days")?.parse().unwrap(), out),
            "recover-429" => {
                let since = a.required("since")?;
                let todo = a.value("todo-file").map(PathBuf::from).unwrap_or_else(|| {
                    self.root
                        .parent()
                        .unwrap_or(&self.root)
                        .join("todo/todo.json")
                });
                writeln!(
                    out,
                    "{}",
                    pretty(&self.recover_429(since, &todo, a.flag("apply"))?)
                )?;
                Ok(0)
            }
            "reload-queue" => {
                writeln!(out, "{}", pretty(&self.reload_queue()?))?;
                Ok(0)
            }
            "recover-children" => {
                writeln!(
                    out,
                    "{}",
                    pretty(&self.recover_children(a.required("since")?, a.flag("apply"))?)
                )?;
                Ok(0)
            }
            "_supervise" => {
                self.supervise(&a.id)?;
                Ok(0)
            }
            _ => unreachable!(),
        }
    }
    pub fn cmd_run(&self, a: &Args, out: &mut dyn Write, err: &mut dyn Write) -> Result<i32> {
        if let Some(key) = a.value("launch-key") {
            if key.len() != 32
                || !key
                    .bytes()
                    .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
            {
                return Err(Error::new(
                    "--launch-key must be 32 lowercase hex characters",
                ));
            }
            fs::create_dir_all(&self.jobs)?;
            let _lock = FileLock::acquire(&self.jobs.join(format!(".launch-{key}.lock")))?;
            if let Some(job) = self
                .list_jobs(&HashSet::new())
                .into_iter()
                .find(|j| s(j, "launch_key") == key)
            {
                writeln!(out, "{}", s(&job, "id"))?;
                return Ok(0);
            }
            self.run_new_job(a, out, err)
        } else {
            self.run_new_job(a, out, err)
        }
    }
    pub fn run_new_job(&self, a: &Args, out: &mut dyn Write, err: &mut dyn Write) -> Result<i32> {
        self.queue_config()?;
        let resume = a
            .value("resume-session")
            .filter(|s| !s.is_empty())
            .map(|s| {
                normalize_uuid(s)
                    .ok_or_else(|| Error::new("--resume-session must be a session UUID"))
            })
            .transpose()?;
        // Different launch keys (including manual launches) can target the same
        // Codex thread. Hold its gate until the queued job is durably registered.
        fs::create_dir_all(&self.jobs)?;
        let _session_lock = resume
            .as_ref()
            .map(|id| FileLock::acquire(&self.jobs.join(format!(".resume-{id}.lock"))))
            .transpose()?;
        // Registration shares admission's gate. This also serializes different
        // launch keys targeting the same ToDo.
        let _queue = self.queue_lock()?;
        let env = self.rt.env();
        let parent = if let Some(id) = a.value("parent-job-id") {
            if !valid_job_id(id) || !self.running_job(&self.load(id)?) {
                return Err(Error::new("parent job must be a running job in this queue"));
            }
            Some(id)
        } else {
            env.get("CC_JOB_ID")
                .map(String::as_str)
                .filter(|id| !id.is_empty())
                .filter(|id| {
                    let running = valid_job_id(id)
                        && self.try_load(id).is_some_and(|job| self.running_job(&job));
                    if !running {
                        self.rt.diagnostic(
                            "inherited parent has exited or is unavailable; queueing as a root job",
                        );
                    }
                    running
                })
        };
        if let Some(todo) = a.value("todo-id").and_then(|id| id.parse::<u64>().ok())
            && let Some(job) = self.list_jobs(&HashSet::new()).into_iter().find(|j| {
                j["todo_id"].as_u64() == Some(todo)
                    && (LIVE.contains(&s(j, "status")) || self.is_ours(j, "codex_pid"))
            })
        {
            return Err(Error::new(format!(
                "ToDo {todo} already has active job {}",
                s(&job, "id")
            )));
        }
        if let Some(session) = &resume
            && let Some(job) = self.list_jobs(&HashSet::new()).into_iter().find(|job| {
                (["queued", "starting", "running"].contains(&s(job, "status"))
                    || self.is_ours(job, "codex_pid"))
                    && ["thread_id", "resume_session"]
                        .iter()
                        .any(|field| s(job, field).eq_ignore_ascii_case(session))
            })
        {
            return Err(Error::new(format!(
                "resume session {session} already has active job {}",
                s(&job, "id")
            )));
        }
        if a.flag("network") && a.required("sandbox")? != "workspace-write" {
            return Err(Error::new(
                "--network only has an effect with --sandbox workspace-write",
            ));
        }
        let prompt = Path::new(a.required("prompt-file")?);
        if !prompt.is_file() {
            return Err(Error::new(format!(
                "prompt file not found: {}",
                prompt.display()
            )));
        }
        let bytes = fs::read(prompt)?;
        if let Err(e) = std::str::from_utf8(&bytes) {
            return Err(Error::new(format!(
                "prompt file is not UTF-8 ({e}); save it as UTF-8 (Windows PowerShell 5.1 writes UTF-16)"
            )));
        }
        let cwd = abs(Path::new(
            a.value("cwd")
                .map(str::to_string)
                .unwrap_or(std::env::current_dir()?.to_string_lossy().into())
                .as_str(),
        ))?;
        if !cwd.is_dir() {
            return Err(Error::new(format!(
                "cwd is not a directory: {}",
                cwd.display()
            )));
        }
        let codex = self
            .rt
            .which("codex.cmd")
            .or_else(|| self.rt.which("codex"))
            .ok_or_else(|| Error::new("codex not found on PATH"))?;
        let name = a.required("name")?;
        let slug = regex::Regex::new(r"[^A-Za-z0-9_-]+")
            .unwrap()
            .replace_all(name, "-")
            .trim_matches('-')
            .chars()
            .take(40)
            .collect::<String>();
        let date = chrono::DateTime::parse_from_rfc3339(&self.rt.now())
            .unwrap()
            .format("%Y%m%d-%H%M%S")
            .to_string();
        let base = format!("{date}-{}", if slug.is_empty() { "job" } else { &slug });
        let dirs = a
            .add_dirs
            .iter()
            .map(|p| abs(Path::new(p)).map(|p| p.to_string_lossy().into_owned()))
            .collect::<std::io::Result<Vec<_>>>()?;
        let mut job = json!({"name":name,"model":a.required("model")?,"effort":a.required("effort")?,"sandbox":a.required("sandbox")?,"cwd":cwd.to_string_lossy(),"add_dirs":dirs,"network":a.flag("network"),"mcp_profile":a.required("mcp-profile")?,"task_type":a.value("task-type"),"notes_hint":a.value("notes-hint"),"prompt_source":abs(prompt)?.to_string_lossy(),"codex":codex,"created_at":self.rt.now(),"status":"queued","pid":null,"codex_pid":null,"started_at":null,"finished_at":null,"exit_code":null,"duration_ms":null,"tokens":usage_tokens(&json!({})),"steps":0,"activity":"queued","last_message":null,"last_error":null,"thread_id":null,"launch_key":a.value("launch-key"),"resume_session":resume});
        let policy = cc_job_policy::for_sandbox(s(&job, "sandbox"));
        let scope_source = a.value("scope-source").unwrap_or("launcher");
        if !["launcher", "owner_marker", "owner_request"].contains(&scope_source) {
            return Err(Error::new("invalid --scope-source"));
        }
        job["scope_source"] = json!(scope_source);
        job["parent_job_id"] = json!(parent);
        job["sandbox"] = json!(policy.sandbox);
        job["approval_policy"] = json!(policy.approval);
        job["approvals_reviewer"] = json!(policy.reviewer);
        job["mcp_profile"] = json!(cc_chrome::profile(
            s(&job, "mcp_profile"),
            std::str::from_utf8(&bytes).unwrap()
        ));
        job["task_title"] = json!(a.value("task-title"));
        job["project"] = json!(a.value("project"));
        job["todo_id"] = json!(a.value("todo-id").and_then(|id| id.parse::<u64>().ok()));
        let bad = codex_args(&job, &self.jobs.join(&base))
            .into_iter()
            .filter(|s| s.chars().any(|c| "&|<>^%!\"".contains(c)))
            .collect::<Vec<_>>();
        if !bad.is_empty() {
            return Err(Error::new(format!(
                "cmd.exe would misparse these codex arguments (they contain one of & | < > ^ % ! \" ): {:?}",
                bad
            )));
        }
        fs::create_dir_all(&self.jobs)?;
        let mut jid = base.clone();
        let mut n = 1;
        loop {
            match fs::create_dir(self.job_dir(&jid)) {
                Ok(()) => break,
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
                    n += 1;
                    jid = format!("{base}-{n}");
                }
                Err(e) => return Err(e.into()),
            }
        }
        let dst = self.job_dir(&jid).join("prompt.md");
        let mut stored_prompt = bytes;
        let guidance = if policy.sandbox == "read-only" {
            cc_job_policy::READ_GUIDANCE
        } else {
            cc_job_policy::WRITE_GUIDANCE
        };
        if !std::str::from_utf8(&stored_prompt)
            .unwrap()
            .contains(guidance.trim())
        {
            stored_prompt.extend_from_slice(guidance.as_bytes());
        }
        fs::write(&dst, stored_prompt)?;
        let mut stored = json!({"id":jid});
        stored
            .as_object_mut()
            .unwrap()
            .extend(job.as_object_mut().unwrap().clone());
        stored["prompt"] = json!(dst.to_string_lossy());
        stored["queue_sequence"] = json!(self.next_queue_sequence()?);
        self.save(&stored)?;
        self.track_queued_job(&jid)?;
        let state = self.job_lock(&jid, "state.lock")?;
        let mut stored = self.load(&jid)?;
        let detached = if s(&stored, "status") == "queued" && stored["cancel_requested"] != true {
            let (detached, pid) = self.rt.spawn_supervisor_identified(
                &self.executable,
                &self.root,
                &self.job_dir(&jid),
                &jid,
            )?;
            if let Some(pid) = pid {
                stored["pid"] = json!(pid);
                stored["pid_created"] = json!(self.rt.proc_created(pid));
                self.save(&stored)?;
            }
            detached
        } else {
            true
        };
        drop(state);
        if !detached {
            writeln!(
                err,
                "warning: could not detach from the caller's job object; this job is killed if the caller's job object closes"
            )?;
        }
        writeln!(out, "{jid}")?;
        Ok(0)
    }
    pub fn cmd_status(&self, a: &Args, out: &mut dyn Write) -> Result<i32> {
        let mut jobs = Vec::new();
        for j in self.list_jobs(&HashSet::new()) {
            let j = self.refresh(j, true)?;
            if a.flag("all")
                || LIVE.contains(&s(&j, "status"))
                || parse_iso(s(&j, "finished_at")).is_some_and(|t| t >= self.rt.epoch() - 86400.)
            {
                jobs.push(j);
            }
        }
        if a.flag("json") {
            writeln!(out, "{}", pretty(&json!(jobs)))?;
        } else if jobs.is_empty() {
            writeln!(
                out,
                "no jobs{}",
                if a.flag("all") {
                    ""
                } else {
                    " in the last 24h (use --all)"
                }
            )?;
        } else {
            writeln!(out, "{}", self.fmt_table(&jobs))?;
        }
        Ok(0)
    }
    pub fn cmd_tail(&self, a: &Args, out: &mut dyn Write) -> Result<i32> {
        self.load(&a.id)?;
        let path = self.job_dir(&a.id).join("events.jsonl");
        let n = a.required("n")?.parse::<i64>().unwrap().max(0) as usize;
        let (lines, mut pos, mut buf) = read_tail(&path, n, !a.flag("follow"))?;
        for line in lines {
            writeln!(out, "{line}")?;
            out.flush()?;
        }
        if a.flag("follow") {
            loop {
                let job = self.refresh(self.load(&a.id)?, true)?;
                let final_state = !LIVE.contains(&s(&job, "status"));
                let data = read_from(&path, pos)?;
                pos += data.len() as u64;
                buf.extend(data);
                let mut parts = buf.split(|c| *c == b'\n').collect::<Vec<_>>();
                let rest = parts.pop().unwrap_or(&[]).to_vec();
                if final_state {
                    parts.push(&rest);
                }
                for line in event_lines(parts) {
                    writeln!(out, "{line}")?;
                    out.flush()?;
                }
                buf = rest;
                if final_state {
                    break;
                }
                self.rt.sleep(1.);
            }
        }
        writeln!(
            out,
            "{}",
            self.summary(&self.refresh(self.load(&a.id)?, true)?)
        )?;
        Ok(0)
    }
    pub fn cmd_wait(&self, a: &Args, out: &mut dyn Write) -> Result<i32> {
        let timeout = a.value("timeout").map(|s| s.parse::<f64>().unwrap());
        let deadline = timeout.map(|s| self.rt.mono() + s);
        let job = loop {
            let j = self.refresh(self.load(&a.id)?, true)?;
            if !LIVE.contains(&s(&j, "status")) {
                break j;
            }
            if deadline.is_some_and(|d| self.rt.mono() >= d) {
                writeln!(
                    out,
                    "timeout after {}s: {}",
                    float_python(timeout.unwrap()),
                    self.summary(&j)
                )?;
                return Ok(2);
            }
            self.rt.sleep(2.);
        };
        writeln!(out, "{}", self.summary(&job))?;
        let msg = self.job_dir(&a.id).join("last_message.md");
        if msg.exists() {
            writeln!(out, "--- {}", msg.display())?;
            writeln!(
                out,
                "{}",
                String::from_utf8_lossy(&fs::read(msg)?)
                    .replace("\r\n", "\n")
                    .replace("\r", "\n")
                    .trim_end()
            )?;
        }
        Ok(if s(&job, "status") == "succeeded" {
            0
        } else {
            1
        })
    }
    pub fn cmd_cancel(&self, jid: &str, out: &mut dyn Write, err: &mut dyn Write) -> Result<i32> {
        self.cmd_cancel_mode(jid, false, out, err)
    }
    pub fn cmd_cancel_mode(
        &self,
        jid: &str,
        recovery: bool,
        out: &mut dyn Write,
        err: &mut dyn Write,
    ) -> Result<i32> {
        self.load(jid)?;
        let lock = self.job_lock(jid, "state.lock")?;
        let mut job = self.refresh_locked(self.load(jid)?, false)?;
        if !LIVE.contains(&s(&job, "status")) && s(&job, "status") != "lost" {
            return Err(Error::new(format!(
                "{jid} is already {}",
                s(&job, "status")
            )));
        }
        if recovery && job["cancel_requested"] == true {
            return Err(Error::new("job was explicitly cancelled"));
        }
        if !recovery {
            // Save cancellation intent before killing; restart uses the same job lock.
            job["cancel_requested"] = json!(true);
            job["cancel_requested_at"] = json!(self.rt.now());
            self.save(&job)?;
        }
        let targets = ["pid", "codex_pid"]
            .into_iter()
            .filter(|k| self.is_ours(&job, k))
            .collect::<Vec<_>>();
        for k in &targets {
            self.rt.kill_tree(job[*k].as_u64().unwrap() as u32)?;
        }
        for _ in 0..50 {
            if !targets.iter().any(|k| self.is_ours(&job, k)) {
                break;
            }
            self.rt.sleep(0.1);
        }
        let alive = targets
            .iter()
            .filter(|k| self.is_ours(&job, k))
            .map(|k| pystr(&job[*k]))
            .collect::<Vec<_>>();
        if !alive.is_empty() {
            writeln!(
                err,
                "could not stop pid {}; {jid} left as {}",
                alive.join(", "),
                s(&job, "status")
            )?;
            return Ok(1);
        }
        job = self.load(jid)?;
        let cancelled = LIVE.contains(&s(&job, "status"));
        if cancelled {
            job["status"] = json!("cancelled");
            job["finished_at"] = json!(self.rt.now());
            job["activity"] = json!("cancelled");
            if let Some(start) = parse_iso(s(&job, "started_at")) {
                job["duration_ms"] = json!(((self.rt.epoch() - start) * 1000.) as i64);
            }
            self.save(&job)?;
        }
        drop(lock);
        self.close_chrome_tabs(jid);
        if cancelled {
            self.auto_rate(&mut job, None);
            task_cancel(
                &job,
                self.ao_executable(),
                self.jobs.clone(),
                self.rt.clone(),
            );
        } else if s(&job, "status") == "lost" {
            self.auto_rate(&mut job, None);
            writeln!(
                out,
                "{}",
                if targets.is_empty() {
                    "no leftover process to kill"
                } else {
                    "killed the leftover codex process tree"
                }
            )?;
        }
        writeln!(out, "{}", self.summary(&job))?;
        Ok(0)
    }
    pub fn cmd_rate(&self, a: &Args, out: &mut dyn Write) -> Result<i32> {
        let mut job = self.refresh(self.load(&a.id)?, false)?;
        let status = s(&job, "status");
        if LIVE.contains(&status) {
            return Err(Error::new(format!(
                "{} is still {status}; rate it after it finishes",
                a.id
            )));
        }
        if ["cancelled", "lost"].contains(&status) && !a.flag("force") {
            return Err(Error::new(format!(
                "{} is {status}, so its duration and tokens are partial (--force sends it anyway)",
                a.id
            )));
        }
        if !self.gto.exists() {
            writeln!(out, "gto not found at {}; skipped", self.gto.display())?;
            return Ok(0);
        }
        let _lock = self.job_lock(&a.id, "gto.lock")?;
        job = self.load(&a.id)?;
        if truth(&job["gto"]) && !truth(&job["gto"]["automatic"]) && !a.flag("force") {
            return Err(Error::new(format!(
                "{} was already sent to gto at {} (--force sends it again)",
                a.id,
                getstr(&job["gto"], "sent_at")
            )));
        }
        let q = a.required("quality")?.parse().unwrap();
        let payload = self.gto_payload(&job, q, a.value("task-type"), a.value("notes"));
        if !self.send_gto(&payload) {
            return Ok(1);
        }
        self.store_rating(&mut job, payload, false)?;
        writeln!(out, "sent to gto: {}, quality={q}", a.id)?;
        Ok(0)
    }
    pub fn cmd_backfill(&self, days: i64, out: &mut dyn Write) -> Result<i32> {
        let cutoff = self.rt.epoch() - days as f64 * 86400.;
        let since = chrono::DateTime::parse_from_rfc3339(&self.rt.now()).unwrap()
            - chrono::Duration::days(days);
        let evals = self.jev_evaluations(None, Some(&since.to_rfc3339()));
        let mut counts =
            json!({"days":days,"considered":0,"sent":0,"already_rated":0,"failed":0,"skipped":0});
        for mut job in self.list_jobs(&HashSet::new()) {
            let when = parse_iso(if s(&job, "finished_at").is_empty() {
                s(&job, "created_at")
            } else {
                s(&job, "finished_at")
            });
            let verdict = evals.get(s(&job, "thread_id")).map(String::as_str);
            let key = if !TERMINAL.contains(&s(&job, "status")) || when.is_none_or(|w| w < cutoff) {
                "skipped"
            } else {
                counts["considered"] = json!(number(&counts, "considered") + 1);
                if truth(&job["gto"]) {
                    "already_rated"
                } else if self.auto_rate(&mut job, Some(verdict)) {
                    "sent"
                } else if truth(&job["gto"]) {
                    "already_rated"
                } else {
                    "failed"
                }
            };
            counts[key] = json!(number(&counts, key) + 1);
        }
        writeln!(out, "{}", compact(&counts))?;
        Ok(if number(&counts, "failed") > 0 { 1 } else { 0 })
    }
}
fn float_python(x: f64) -> String {
    if x.fract() == 0. && x.is_finite() {
        format!("{x:.1}")
    } else {
        x.to_string()
    }
}
