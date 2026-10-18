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
        "qualify-fixed-review" => {
            "usage: codex_jobs.exe qualify-fixed-review --request-file <JSON> [--apply]\n\nBind one existing unstarted worker to a fixed read-only review after its exact automatic writer exits. Preview by default. Never changes ToDos, grants write access or launches work. Reload the exact queued supervisor first.\n"
        }
        "handoff-finished-work" => {
            "usage: codex_jobs.exe handoff-finished-work --request-file <JSON> [--apply]\n\nPreview or record one exact completed-writer handoff after verifying the delivered source receipt. Removes only that queued worker's automatic owner edge; normal admission and all other waits remain required. Does not launch work or change execution permissions.\n"
        }
        "repair-owner-wait" => {
            "usage: codex_jobs.exe repair-owner-wait --request-file <JSON> [--apply]\n\nPreview one exact never-started finite worker's automatic wait cycle. Apply changes only that job under queue, job and ToDo locks; normal admission and all other waits remain required.\n"
        }
        "rename-project" => {
            "usage: codex_jobs.exe rename-project <id> --from <name> --to <name>\n\nRename only this job's project, name and task_title under state.lock. Execution paths and identifiers are preserved.\n"
        }
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
        "recover-reboot" => {
            "usage: codex_jobs.exe recover-reboot [--apply]\n\nRecover only jobs interrupted by the current Windows boot; preview without --apply.\n"
        }
        "recover-children" => {
            "usage: codex_jobs.exe recover-children --since <RFC3339> [--apply]\n\nLink old queued children and recover never-started cancelled children of running parents.\n"
        }
        "cleanup-supervisors" => {
            "usage: codex_jobs.exe cleanup-supervisors [--apply]\n\nPreview terminal supervisors; --apply stops only their identified controller.\n"
        }
        "reload-queue" => {
            "usage: codex_jobs.exe reload-queue [--job-id <id>]\n\nRefresh queued controllers without execution admission; their successors still check history, capacity and ownership. --job-id keeps every other controller unchanged. Invalid ids exit 2; missing/unreadable records exit 1. Deferred updates appear in the report and do not mean execution has started.\n"
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
    pub model_explicit: bool,
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
        "recover-reboot",
        "reload-queue",
        "repair-owner-wait",
        "qualify-fixed-review",
        "handoff-finished-work",
        "adjust-limit",
        "update-queue-config",
        "recover-children",
        "cleanup-supervisors",
        "rename-project",
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
        model_explicit: false,
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
                "status" => ["all", "json", "summary"].contains(&name.as_str()),
                "tail" => name == "follow",
                "rate" => name == "force",
                "cancel" => ["recovery", "if-todo-ready"].contains(&name.as_str()),
                "update-queue-config" => name == "clear-fixed",
                "repair-owner-wait" => name == "apply",
                "qualify-fixed-review" => name == "apply",
                "handoff-finished-work" => name == "apply",
                "recover-429" | "recover-reboot" | "recover-children" | "cleanup-supervisors" => {
                    name == "apply"
                }
                _ => false,
            };
            let allowed = match cmd.as_str() {
                "run" => [
                    "name",
                    "task-title",
                    "project",
                    "todo-id",
                    "related-todo-ids",
                    "model",
                    "auto-model",
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
                    "original-product-retry-key",
                    "resume-session",
                    "parent-job-id",
                    "subwork-of",
                    "assigned-needs",
                    "pr-url",
                ]
                .contains(&name.as_str()),
                "status" => ["all", "json", "summary"].contains(&name.as_str()),
                "tail" => ["n", "follow"].contains(&name.as_str()),
                "wait" => name == "timeout",
                "watch" => name == "interval",
                "rate" => ["quality", "task-type", "notes", "force"].contains(&name.as_str()),
                "backfill" => name == "days",
                "cancel" => [
                    "recovery",
                    "if-todo-ready",
                    "todo-id",
                    "related-todo-ids",
                    "todo-file",
                    "cancel-key",
                    "no-action-check",
                ]
                .contains(&name.as_str()),
                "recover-429" => ["since", "todo-file", "apply"].contains(&name.as_str()),
                "recover-children" => ["since", "apply"].contains(&name.as_str()),
                "recover-reboot" => name == "apply",
                "cleanup-supervisors" => name == "apply",
                "reload-queue" => name == "job-id",
                "repair-owner-wait" => ["request-file", "apply"].contains(&name.as_str()),
                "qualify-fixed-review" => ["request-file", "apply"].contains(&name.as_str()),
                "handoff-finished-work" => ["request-file", "apply"].contains(&name.as_str()),
                "rename-project" => ["from", "to"].contains(&name.as_str()),
                "update-queue-config" => ["patch-file", "clear-fixed"].contains(&name.as_str()),
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
                    if name == "model" {
                        a.model_explicit = true;
                    }
                    a.values.insert(name, value);
                }
            }
        } else if a.id.is_empty()
            && [
                "tail",
                "wait",
                "cancel",
                "rate",
                "_supervise",
                "rename-project",
            ]
            .contains(&cmd.as_str())
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
        if a.value("auto-model").is_some() && a.value("model").is_some() {
            return Err(Error::new(
                "--model and --auto-model are mutually exclusive",
            ));
        }
        if let Some(model) = a.values.remove("auto-model") {
            a.values.insert("model".into(), model);
        }
        let missing = ["name", "prompt-file"]
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
        let profile = cc_job_policy::model_profile(
            a.required("name")?,
            a.value("task-type").unwrap_or("implementation"),
        );
        for (name, default) in [
            ("model", profile.model),
            ("effort", profile.effort),
            ("sandbox", "read-only"),
            ("mcp-profile", "minimal"),
        ] {
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
        original_product_retry_key(&a)?;
    }
    if [
        "tail",
        "wait",
        "cancel",
        "rate",
        "_supervise",
        "rename-project",
    ]
    .contains(&cmd.as_str())
        && a.id.is_empty()
    {
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

fn original_product_retry_key(a: &Args) -> Result<Option<&str>> {
    let Some(key) = a.value("original-product-retry-key") else {
        return Ok(None);
    };
    if key.len() != 32
        || !key
            .bytes()
            .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
    {
        return Err(Error::new(
            "--original-product-retry-key must be 32 lowercase hex characters",
        ));
    }
    if a.value("todo-id") != Some("1626") || a.value("launch-key") != Some(key) {
        return Err(Error::new(
            "--original-product-retry-key requires --todo-id 1626 and the same --launch-key",
        ));
    }
    if [
        "related-todo-ids",
        "subwork-of",
        "parent-job-id",
        "assigned-needs",
        "pr-url",
    ]
    .iter()
    .any(|name| a.value(name).is_some())
    {
        return Err(Error::new(
            "original product retry cannot add related ToDos, subwork, a parent or a PR override",
        ));
    }
    if a.value("project")
        .is_some_and(|project| project != "PhosGaea-Vol1")
    {
        return Err(Error::new(
            "original product retry is limited to PhosGaea-Vol1",
        ));
    }
    Ok(Some(key))
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
            "rename-project" => {
                writeln!(
                    out,
                    "{}",
                    pretty(&self.rename_display_project(
                        &a.id,
                        a.required("from")?,
                        a.required("to")?
                    )?)
                )?;
                Ok(0)
            }
            "run" => self.cmd_run(&a, out, err),
            "status" => self.cmd_status(&a, out),
            "cleanup-supervisors" => {
                writeln!(
                    out,
                    "{}",
                    pretty(&self.cleanup_supervisors(a.flag("apply"))?)
                )?;
                Ok(0)
            }
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
            "cancel" => {
                if a.flag("if-todo-ready") {
                    self.cmd_cancel_if_todo_ready(&a, out, err)
                } else if [
                    "todo-id",
                    "related-todo-ids",
                    "todo-file",
                    "cancel-key",
                    "no-action-check",
                ]
                .iter()
                .any(|key| a.value(key).is_some())
                {
                    Err(Error::new(
                        "cancel ToDo metadata requires --if-todo-ready --recovery",
                    ))
                } else {
                    self.cmd_cancel_mode(&a.id, a.flag("recovery"), out, err)
                }
            }
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
            "recover-reboot" => {
                let report = self.recover_reboot(a.flag("apply"))?;
                let deferred = a.flag("apply")
                    && report.as_array().is_some_and(|entries| {
                        entries.iter().any(|entry| entry["action"] == "deferred")
                    });
                writeln!(out, "{}", pretty(&report))?;
                Ok(if deferred { 75 } else { 0 })
            }
            "reload-queue" => {
                let report = match a.value("job-id") {
                    Some(id) => self.reload_queue_job(id)?,
                    None => self.reload_queue()?,
                };
                writeln!(out, "{}", pretty(&report))?;
                Ok(0)
            }
            "repair-owner-wait" => {
                let request = self.read_json(Path::new(a.required("request-file")?))?;
                writeln!(
                    out,
                    "{}",
                    pretty(&self.repair_owner_wait(&request, a.flag("apply"))?)
                )?;
                Ok(0)
            }
            "qualify-fixed-review" => {
                let bytes = self.read_file(Path::new(a.required("request-file")?))?;
                let request = serde_json::from_slice(&bytes)?;
                writeln!(
                    out,
                    "{}",
                    pretty(&self.qualify_finished_review(&request, a.flag("apply"))?)
                )?;
                Ok(0)
            }
            "handoff-finished-work" => {
                let request = self.read_json(Path::new(a.required("request-file")?))?;
                writeln!(
                    out,
                    "{}",
                    pretty(&self.handoff_finished_work(&request, a.flag("apply"))?)
                )?;
                Ok(0)
            }
            "adjust-limit" => {
                self.adjust_limit()?;
                writeln!(
                    out,
                    "{}",
                    pretty(&self.limit_status(&self.queue_config()?)?)
                )?;
                Ok(0)
            }
            "update-queue-config" => {
                let patch = if let Some(file) = a.value("patch-file") {
                    self.read_json(Path::new(file))?
                } else {
                    json!({})
                };
                writeln!(
                    out,
                    "{}",
                    pretty(&self.update_queue_config(&patch, a.flag("clear-fixed"))?)
                )?;
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
        original_product_retry_key(a)?;
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
            let path = self.jobs.join(format!(".launch-{key}.lock"));
            // Legacy launchers created this permanent per-key lock before any
            // record. Only its first creator can skip legacy-key reconciliation.
            let first_key = match fs::OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(&path)
            {
                Ok(file) => {
                    drop(file);
                    true
                }
                Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => false,
                Err(error) => return Err(Error::file_io("create launch key lock", &path, error)),
            };
            let _lock = FileLock::acquire(&path).map_err(|e| {
                Error::file_io("acquire exclusive lock byte range [0, 1)", &path, e)
            })?;
            // Never read the lock's byte zero on Windows. The receipt uses an
            // atomic sidecar, and its claimed record must match this exact key.
            let receipt_path = self.jobs.join(format!(".launch-{key}.json"));
            match self.read_file(&receipt_path) {
                Ok(bytes) => {
                    let receipt: Value = serde_json::from_slice(&bytes)?;
                    let id = s(&receipt, "id");
                    if !valid_job_id(id) {
                        return Err(Error::new("invalid launch key receipt"));
                    }
                    let job = self.load(id)?;
                    if s(&job, "launch_key") != key {
                        return Err(Error::new("invalid launch key receipt"));
                    }
                    self.validate_product_retry_replay(a, &job)?;
                    writeln!(out, "{id}")?;
                    return Ok(0);
                }
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => (),
                Err(error) => {
                    return Err(Error::file_io(
                        "read launch key receipt",
                        &receipt_path,
                        error,
                    ));
                }
            }
            // Another caller may acquire the lock between create_new and our
            // acquisition. Its in-progress receipt makes that crash window
            // recover through history rather than issuing a second record.
            let pending_path = self.jobs.join(format!(".launch-{key}.pending"));
            if (!first_key || pending_path.exists())
                && let Some(job) = self
                    .list_jobs(&HashSet::new())
                    .into_iter()
                    .find(|j| s(j, "launch_key") == key)
            {
                self.validate_product_retry_replay(a, &job)?;
                self.write_json(&receipt_path, &json!({"id":job["id"]}))?;
                writeln!(out, "{}", s(&job, "id"))?;
                return Ok(0);
            }
            fs::write(&pending_path, b"receipt pending").map_err(|error| {
                Error::file_io("reserve launch key receipt", &pending_path, error)
            })?;
            self.run_new_job(a, out, err)
        } else {
            self.run_new_job(a, out, err)
        }
    }

    // A launch receipt is reusable after a crash/bind/expiry, but only for the
    // identical request. Never create or restart a supervisor during replay.
    fn validate_product_retry_replay(&self, a: &Args, job: &Value) -> Result<()> {
        if a.value("original-product-retry-key").is_none()
            && job.get("original_product_retry").is_none()
        {
            return Ok(());
        }
        let key = original_product_retry_key(a)?
            .ok_or_else(|| Error::new("original product retry receipt requires its retry key"))?;
        let prompt = Path::new(a.required("prompt-file")?);
        let bytes = self
            .read_file(prompt)
            .map_err(|e| Error::file_io("read prompt", prompt, e))?;
        if job["todo_id"] != 1626
            || job["original_product_retry"]["key"] != key
            || job["original_product_retry"]["prompt_sha256"]
                != cc_job_policy::product_retry::digest(&bytes)
            || job["original_product_retry_request_sha256"]
                != self.product_retry_request_digest(a, &bytes)?
        {
            return Err(Error::new(
                "original product retry launch key request changed",
            ));
        }
        Ok(())
    }

    fn product_retry_request_digest(&self, a: &Args, bytes: &[u8]) -> Result<String> {
        let mut values = a.values.clone();
        let cwd = a
            .value("cwd")
            .map(PathBuf::from)
            .unwrap_or(std::env::current_dir()?);
        values.insert("cwd".into(), abs(&cwd)?.to_string_lossy().into_owned());
        values.insert(
            "prompt-file".into(),
            abs(Path::new(a.required("prompt-file")?))?
                .to_string_lossy()
                .into_owned(),
        );
        values
            .entry("scope-source".into())
            .or_insert("launcher".into());
        let dirs = a
            .add_dirs
            .iter()
            .map(|p| abs(Path::new(p)))
            .collect::<std::io::Result<Vec<_>>>()?;
        let flags: std::collections::BTreeSet<_> = a.flags.iter().collect();
        let request = json!({"options":values,"add_dirs":dirs,"flags":flags,
            "model_explicit":a.model_explicit,"prompt_sha256":cc_job_policy::product_retry::digest(bytes)});
        Ok(cc_job_policy::product_retry::digest(&serde_json::to_vec(
            &request,
        )?))
    }

    pub fn run_new_job(&self, a: &Args, out: &mut dyn Write, err: &mut dyn Write) -> Result<i32> {
        let retry_key = original_product_retry_key(a)?;
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
            .map(|id| {
                let path = self.jobs.join(format!(".resume-{id}.lock"));
                FileLock::acquire(&path).map_err(|e| {
                    Error::file_io("acquire exclusive lock byte range [0, 1)", &path, e)
                })
            })
            .transpose()?;
        if a.flag("network")
            && a.required("sandbox")? != "workspace-write"
            && !(retry_key.is_some() && a.required("sandbox")? == "danger-full-access")
        {
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
        let bytes = self
            .read_file(prompt)
            .map_err(|e| Error::file_io("read prompt", prompt, e))?;
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
        let mut policy = cc_job_policy::for_sandbox(s(&job, "sandbox"));
        if retry_key.is_some() && s(&job, "sandbox") == "workspace-write" {
            // The saved source scope, including a restricted writer, is exact.
            policy.sandbox = "workspace-write";
        }
        job["model_explicit"] = json!(a.model_explicit);
        job["requested_model"] = job["model"].clone();
        let scope_source = a.value("scope-source").unwrap_or("launcher");
        if !["launcher", "owner_marker", "owner_request"].contains(&scope_source) {
            return Err(Error::new("invalid --scope-source"));
        }
        job["scope_source"] = json!(scope_source);
        job["sandbox"] = json!(policy.sandbox);
        job["approval_policy"] = json!(policy.approval);
        job["approvals_reviewer"] = json!(policy.reviewer);
        if retry_key.is_none() {
            job["mcp_profile"] = json!(cc_chrome::profile(
                s(&job, "mcp_profile"),
                std::str::from_utf8(&bytes).unwrap()
            ));
        }
        // Intake uses the short publication gate; history reconciliation remains outside it.
        let _queue = self.queue_lock_for("registration", std::time::Duration::from_secs(60))?;
        // Shared lock order: queue -> job state (when present) -> ToDo. Hold through registration.
        let _subwork_store_lock = ((self.todo_store().exists() || retry_key.is_some())
            && (a.value("todo-id").is_some() || a.value("related-todo-ids").is_some()))
        .then(|| {
            FileLock::acquire(&self.todo_store().with_file_name("todo.lock")).map_err(Error::from)
        })
        .transpose()?;
        let related: Vec<u64> = a
            .value("related-todo-ids")
            .map(|ids| {
                ids.split(',')
                    .map(|id| {
                        id.parse::<u64>().ok().filter(|id| *id > 0).ok_or_else(|| {
                            Error::new(
                                "--related-todo-ids requires comma-separated positive ToDo IDs",
                            )
                        })
                    })
                    .collect::<Result<Vec<_>>>()
            })
            .transpose()?
            .unwrap_or_default();
        job["todo_id"] = json!(a.value("todo-id").and_then(|id| id.parse::<u64>().ok()));
        if !related.is_empty() {
            job["related_todo_ids"] = json!(&related);
        }
        if let Some(key) = retry_key {
            let db = self.read_json(&self.todo_store())?;
            job["original_product_retry"] = cc_job_policy::product_retry::registration(
                &db,
                &job,
                key,
                &cc_job_policy::product_retry::digest(&bytes),
                self.rt.epoch() as u64,
            )
            .map_err(Error::new)?;
            job["original_product_retry_request_sha256"] =
                json!(self.product_retry_request_digest(a, &bytes)?);
        }
        // Do not create another supervisor for a task already waiting on a
        // prerequisite. Existing queued supervisors are also gated at admission.
        if job["todo_id"].is_number() || !related.is_empty() {
            let store = self.todo_store();
            if store.exists() {
                let db = self.read_json(&store)?;
                let ready: Vec<_> = cc_job_policy::todo_wait::job_ready_waits(&db, &job)
                    .into_iter()
                    // Registration validated this exact saved wait; only bind
                    // can remove it. Other waits retain the ordinary rejection.
                    .filter(|wait| retry_key.is_none() || wait["item"] != 1626)
                    .collect();
                if !ready.is_empty() {
                    return Err(Error::new(
                        ready
                            .iter()
                            .filter_map(|wait| wait["message"].as_str())
                            .collect::<Vec<_>>()
                            .join("、"),
                    ));
                }
                let on = cc_job_policy::todo_wait::job_pending_on(&db, &job);
                if !on.is_empty() {
                    return Err(Error::new(
                        on.iter()
                            .map(|id| format!("ToDo {id} 待ち"))
                            .collect::<Vec<_>>()
                            .join("、"),
                    ));
                }
            }
        }
        let env = self.rt.env();
        let parent = if retry_key.is_some() || a.value("subwork-of").is_some() {
            None // Retry stays a root; only the saved subwork relation below chooses a parent.
        } else if let Some(id) = a.value("parent-job-id") {
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
        // The child exemption is for a running job's own sub-work. A job for a
        // different ToDo is top-level work and must wait in the shared queue.
        let mut registrations = self.registration_jobs()?;
        if let Some(parent) = parent.and_then(|id| self.try_load(id))
            && !registrations.iter().any(|job| job["id"] == parent["id"])
        {
            registrations.push(parent);
        }
        let parent = parent.filter(|id| {
            let own = job["todo_id"].as_u64().is_none_or(|todo| {
                self.try_load(id)
                    .is_some_and(|p| p["todo_id"].as_u64() == Some(todo))
            });
            if !own {
                self.rt.diagnostic(
                    "job for another ToDo is not sub-work of its parent; queueing as a root job",
                );
            }
            own
        });
        for todo in job["todo_id"]
            .as_u64()
            .into_iter()
            .chain(related.iter().copied())
        {
            if let Some(job) = registrations.iter().find(|j| {
                (j["todo_id"].as_u64() == Some(todo)
                    || j["related_todo_ids"]
                        .as_array()
                        .is_some_and(|ids| ids.contains(&json!(todo))))
                    && (LIVE.contains(&s(j, "status")) || self.is_ours(j, "codex_pid"))
            }) {
                return Err(Error::new(format!(
                    "ToDo {todo} already has active job {}",
                    s(job, "id")
                )));
            }
        }
        if let Some(session) = &resume
            && let Some(job) = registrations.iter().find(|job| {
                (["queued", "starting", "running"].contains(&s(job, "status"))
                    || self.is_ours(job, "codex_pid"))
                    && ["thread_id", "resume_session"]
                        .iter()
                        .any(|field| s(job, field).eq_ignore_ascii_case(session))
            })
        {
            return Err(Error::new(format!(
                "resume session {session} already has active job {}",
                s(job, "id")
            )));
        }
        job["parent_job_id"] = json!(parent);
        job["task_title"] = json!(a.value("task-title"));
        job["project"] = json!(
            a.value("project")
                .or(retry_key.map(|_| "PhosGaea-Vol1"))
                .map(cc_task_title::canonical_project)
        );
        let inferred = cc_task_title::project_from_folder(s(&job, "cwd"));
        if s(&job, "project").is_empty() && inferred == cc_task_title::PRODUCT_NAME {
            job["project"] = json!(inferred);
        }
        if s(&job, "project") == cc_task_title::PRODUCT_NAME {
            for key in ["name", "task_title"] {
                if let Some(text) = job[key].as_str() {
                    job[key] = json!(cc_task_title::canonical_text(text));
                }
            }
        }
        job["todo_id"] = json!(a.value("todo-id").and_then(|id| id.parse::<u64>().ok()));
        self.record_work_owner(&mut job);
        if !job["work_owner"]["pr"].is_u64()
            // Only the task's first line, never PR references in embedded instructions.
            && let Some(pr) = cc_job_policy::work_owner::pr_number(
                std::str::from_utf8(&bytes)
                    .unwrap_or("")
                    .lines()
                    .next()
                    .unwrap_or(""),
            )
        {
            job["work_owner"]["pr"] = json!(pr);
        }
        if let Some(url) = a.value("pr-url") {
            let target = cc_job_policy::work_owner::pr_url(url)
                .ok_or_else(|| Error::new("invalid --pr-url"))?;
            job["work_owner"]["repository"] = target["repository"].clone();
            job["work_owner"]["pr"] = target["pr"].clone();
            job["work_owner"]["explicit_pr"] = target;
        }
        let store = self.todo_store();
        if store.exists() {
            let db = self.read_json(&store)?;
            // The separately reserved original-product retry has its own exact
            // source/permission binding above; do not reinterpret its history.
            if retry_key.is_none() {
                self.inherit_fixed_review_requirement(&db, &mut job)?;
            }
            let item = db["items"]
                .as_array()
                .into_iter()
                .flatten()
                .find(|i| i["id"] == job["todo_id"]);
            if a.value("subwork-of").is_some() || item.is_some_and(|i| i["subwork"].is_object()) {
                let item = item.ok_or_else(|| Error::new("subwork ToDo is missing"))?;
                let parent = a
                    .required("subwork-of")?
                    .parse::<i64>()
                    .map_err(|_| Error::new("invalid --subwork-of"))?;
                if item["subwork"]["parent_todo_id"] != parent {
                    return Err(Error::new(
                        "subwork parent does not match the saved relation",
                    ));
                }
                let needs = a
                    .required("assigned-needs")?
                    .split(',')
                    .filter(|s| !s.is_empty())
                    .collect::<Vec<_>>();
                job["assigned_needs"] = json!(needs);
                job["subwork"] = item["subwork"].clone();
                job["subwork_spec"] = item["spec"].clone();
                if ["research", "review"].contains(&s(&job["subwork"], "kind"))
                    && s(&job, "sandbox") != "read-only"
                {
                    return Err(Error::new(
                        "independent research/review requires read-only scope",
                    ));
                }
                let mut jobs = registrations.clone();
                if let Some(requested) = a.value("parent-job-id") {
                    let p = self.load(requested)?;
                    if p["todo_id"] != parent
                        || !s(&p, "parent_job_id").is_empty()
                        || self.rt.work_checkout(s(&p, "cwd"))["worktree"]
                            != job["subwork"]["owner"]["worktree"]
                    {
                        return Err(Error::new("requested parent is not this subwork's owner"));
                    }
                    if !jobs.iter().any(|job| job["id"] == p["id"]) {
                        jobs.push(p);
                    }
                }
                cc_job_policy::subwork::mark_live(&mut jobs, |j| self.running_job(j));
                cc_job_policy::subwork::refresh_job(&db, &jobs, &mut job).map_err(Error::new)?;
            }
        } else if a.value("subwork-of").is_some() {
            return Err(Error::new("subwork ToDo store is missing"));
        }
        let bad = codex_args(&job)
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
        // The directory is private until job.json/index publication. Reserve
        // its state now so a supervisor cannot win it and make registration
        // wait under the global gate. Never wait for a private lock here.
        let state = FileLock::try_acquire(&self.job_dir(&jid).join("state.lock"))?;
        let dst = self.job_dir(&jid).join("prompt.md");
        let mut stored_prompt = bytes;
        let guidance =
            if policy.sandbox == "read-only" || !job["fixed_review_requalification"].is_null() {
                cc_job_policy::READ_GUIDANCE
            } else {
                cc_job_policy::WRITE_GUIDANCE
            };
        if retry_key.is_none()
            && !std::str::from_utf8(&stored_prompt)
                .unwrap()
                .contains(guidance.trim())
        {
            stored_prompt.extend_from_slice(guidance.as_bytes());
        }
        fs::write(&dst, stored_prompt).map_err(|e| Error::file_io("write prompt", &dst, e))?;
        let mut stored = json!({"id":jid});
        stored
            .as_object_mut()
            .unwrap()
            .extend(job.as_object_mut().unwrap().clone());
        stored["prompt"] = json!(dst.to_string_lossy());
        stored["queue_sequence"] = json!(self.next_queue_sequence()?);
        self.save(&stored)?;
        self.track_queued_job(&jid)?;
        if let Some(key) = a.value("launch-key") {
            // Publication precedes spawn and response. A failed spawn or lost
            // response still has exactly one durable receipt for this key.
            self.write_json(
                &self.jobs.join(format!(".launch-{key}.json")),
                &json!({"id":jid}),
            )?;
        }
        drop(_queue);
        let mut stored = self.load(&jid)?;
        let detached = if s(&stored, "status") == "queued" && stored["cancel_requested"] != true {
            let (detached, pid) = self
                .rt
                .spawn_supervisor_identified(
                    &self.executable,
                    &self.root,
                    &self.job_dir(&jid),
                    &jid,
                )
                .map_err(|e| {
                    Error::file_io(
                        "spawn supervisor with log",
                        &self.job_dir(&jid).join("supervisor.log"),
                        e,
                    )
                })?;
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
                || s(&j, "status") == "starting"
                || self.running_job(&j)
                || parse_iso(s(&j, "finished_at")).is_some_and(|t| t >= self.rt.epoch() - 86400.)
            {
                jobs.push(j);
            }
        }
        // Admission reserves unreadable records conservatively. Include those
        // reservations rather than displaying misleading empty capacity.
        let known: HashSet<_> = jobs.iter().map(|j| s(j, "id").to_string()).collect();
        if let Ok(entries) = fs::read_dir(&self.jobs) {
            for entry in entries.flatten() {
                let id = entry.file_name().to_string_lossy().to_string();
                if !known.contains(&id)
                    && entry.path().join("job.json").is_file()
                    && self.try_load(&id).is_none()
                {
                    jobs.push(json!({"id":id,"status":"running","queue_unreadable":true,"activity":"状態ファイルを読めないため実行枠を保持"}));
                }
            }
        }
        let report = self.queue_report(&jobs)?;
        for job in &mut jobs {
            if let Some(wait) = report["queued"]
                .as_array()
                .and_then(|q| q.iter().find(|q| q["id"] == job["id"]))
            {
                job["queue_wait_reason"] = wait["reason"].clone();
            }
            let waits = self.resource_waits(job);
            job["resource_waits"] = json!(waits);
            if s(job, "status") == "queued" {
                job["activity"] = job["queue_wait_reason"]["message"].clone();
            } else if !waits.is_empty() {
                job["activity"] = json!(waits.join("・"));
            }
        }
        if !a.flag("json") {
            writeln!(out, "{}", self.queue_report_line(&report))?;
        }
        if a.flag("json") {
            let value = if a.flag("summary") {
                json!({"summary":report,"jobs":jobs})
            } else {
                json!(jobs)
            };
            writeln!(out, "{}", pretty(&value))?;
        } else if a.flag("summary") {
            // The summary alone is useful to humans and monitoring scripts.
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
        self.cancel_locked(jid, recovery, lock, out, err)
    }

    /// Linearize recovery cancellation with wait registration. Never run a
    /// subprocess or refresh/notify while holding the shared ToDo lock.
    fn cmd_cancel_if_todo_ready(
        &self,
        a: &Args,
        out: &mut dyn Write,
        err: &mut dyn Write,
    ) -> Result<i32> {
        if !a.flag("recovery") {
            return Err(Error::new("--if-todo-ready requires --recovery"));
        }
        let key = a.required("cancel-key")?;
        if !valid_job_id(key) {
            return Err(Error::new("invalid guarded cancellation key"));
        }
        let id = |text: &str| {
            text.parse::<i64>()
                .ok()
                .filter(|id| *id > 0)
                .ok_or_else(|| Error::new("cancel ToDo metadata requires positive ToDo IDs"))
        };
        let primary = a.value("todo-id").map(id).transpose()?;
        let related = a
            .value("related-todo-ids")
            .map(|text| text.split(',').map(id).collect::<Result<Vec<_>>>())
            .transpose()?
            .unwrap_or_default();
        let store = a
            .value("todo-file")
            .map(PathBuf::from)
            .unwrap_or_else(|| self.todo_store());
        self.load(&a.id)?;
        let no_action_check = a.value("no-action-check").is_some();
        let _reservation = if no_action_check {
            let Some(reservation) = self.yield_reservation(&a.id) else {
                writeln!(
                    out,
                    "{}",
                    json!({"outcome":"deferred","job":a.id,"reason":"boundary_busy"})
                )?;
                return Ok(0);
            };
            Some(reservation)
        } else {
            None
        };
        // Same ordering as registration/admission: queue -> job state -> ToDo.
        let queue = self.queue_lock_for(
            "dependency guarded cancellation",
            std::time::Duration::from_secs(if no_action_check { 2 } else { 60 }),
        )?;
        let lock = if no_action_check {
            FileLock::try_acquire(&self.job_dir(&a.id).join("state.lock"))?
        } else {
            self.job_lock(&a.id, "state.lock")?
        };
        let mut job = self.load(&a.id)?;
        let mut paused = None;
        let no_action = if let Some(path) = a.value("no-action-check") {
            let proof: Value = self.read_json(Path::new(path))?;
            let progress: cc_job_policy::action_progress::Progress =
                serde_json::from_value(proof["progress"].clone())
                    .map_err(|_| Error::new("invalid activity proof"))?;
            if job["status"] != "running"
                || cc_job_policy::cancellation::owner_stopped(&Value::Null, &job)
                || progress.attempt != cc_job_policy::action_progress::attempt(&job)
                || !progress.stalled(self.rt.epoch())
            {
                writeln!(
                    out,
                    "{}",
                    json!({"outcome":"deferred","job":a.id,"reason":"proof_stale"})
                )?;
                return Ok(0);
            }
            // Freeze only the verified idle controller tree. A running command,
            // MCP child or a newly spawned descendant makes this return None.
            paused = self.rt.pause_idle_agent(
                number(&job, "codex_pid") as u32,
                job["codex_pid_created"]
                    .as_u64()
                    .ok_or_else(|| Error::new("missing controller birth"))?,
                None,
                Some(&self.job_dir(&a.id).join("yield-paused.json")),
            )?;
            if paused.is_none() {
                writeln!(
                    out,
                    "{}",
                    json!({"outcome":"deferred","job":a.id,"reason":"active_child"})
                )?;
                return Ok(0);
            }
            // Drain already emitted events while the controller is frozen. The
            // supervisor's event reader does not need state.lock to write them.
            let events = self.job_dir(&a.id).join("events.jsonl");
            let mut length = fs::metadata(&events)?.len();
            let mut stable = 0;
            for _ in 0..20 {
                self.rt.sleep(0.05);
                let next = fs::metadata(&events)?.len();
                stable = if next == length { stable + 1 } else { 0 };
                length = next;
                if stable >= 4 {
                    break;
                }
            }
            let fresh =
                cc_job_policy::action_progress::scan(&events, &job, &progress, self.rt.epoch())?;
            if stable < 4
                || !fresh.stalled(self.rt.epoch())
                || fresh.quiet_since != progress.quiet_since
            {
                writeln!(
                    out,
                    "{}",
                    json!({"outcome":"deferred","job":a.id,"reason":"activity_changed"})
                )?;
                return Ok(0);
            }
            Some(proof)
        } else {
            None
        };
        let todo = if no_action_check {
            FileLock::try_acquire(&store.with_file_name("todo.lock"))?
        } else {
            FileLock::acquire(&store.with_file_name("todo.lock"))?
        };
        // An unavailable managed store cannot prove that the source is ready.
        // Keep the source unchanged until its dependencies can be checked.
        let db = cc_job_policy::todo_store::read(&store).map_err(|e| Error::new(e.to_string()))?;
        if !db["items"].is_array() {
            return Err(Error::new("invalid ToDo store items"));
        }
        if let Some(proof) = &no_action {
            let row = db["items"]
                .as_array()
                .unwrap()
                .iter()
                .find(|r| r["id"].as_i64() == primary);
            let valid = row.is_some_and(|row| {
                cc_job_policy::action_progress::eligible(&db, row, &job)
                    && cc_job_policy::action_progress::ticket(&db, row, &job) == proof["ticket"]
                    && self.no_action_workspace(&db, row, &job)
            });
            if !valid {
                writeln!(
                    out,
                    "{}",
                    json!({"outcome":"deferred","job":a.id,"reason":"owner_or_scope_changed"})
                )?;
                return Ok(0);
            }
        }
        let mut identity = job.clone();
        let mut ids: std::collections::BTreeSet<i64> = identity["related_todo_ids"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(Value::as_i64)
            .collect();
        ids.extend(identity["todo_id"].as_i64());
        ids.extend(primary);
        ids.extend(related);
        identity["related_todo_ids"] = json!(ids);
        let mut on = cc_job_policy::todo_wait::job_pending_on(&db, &identity);
        on.extend(cc_job_policy::todo_wait::pending_on(
            &db,
            &json!({"wait_on":job["work_wait_on"]}),
        ));
        on.sort_unstable();
        on.dedup();
        let ready = cc_job_policy::todo_wait::job_ready_waits(&db, &identity);
        let outcome = if cc_job_policy::cancellation::owner_stopped(&Value::Null, &job) {
            Some(json!({"outcome":"owner_stopped","job":a.id}))
        } else if !ready.is_empty()
            && (job["recovery_cancel"]["key"] != key
                || !cc_job_policy::cancellation::recovery_cancel(&Value::Null, &job))
        {
            Some(json!({"outcome":"deferred","job":a.id,"wait_ready":ready}))
        } else if !on.is_empty()
            && (job["recovery_cancel"]["key"] != key
                || !cc_job_policy::cancellation::recovery_cancel(&Value::Null, &job))
        {
            Some(json!({"outcome":"deferred","job":a.id,"wait_on":on}))
        } else if !LIVE.contains(&s(&job, "status")) && s(&job, "status") != "lost" {
            Some(json!({"outcome":"already_terminal","job":a.id}))
        } else {
            None
        };
        if let Some(result) = outcome {
            writeln!(out, "{result}")?;
            return Ok(0);
        }
        // This durable intent is the ordering point, not process termination.
        // A wait registered afterwards may block the successor, but cannot undo
        // this admitted cancellation. Keep deliberate owner stops distinct.
        job["related_todo_ids"] = identity["related_todo_ids"].clone();
        if job["todo_id"].as_i64().is_none() {
            job["todo_id"] = json!(primary);
        }
        job["recovery_cancel"] = json!({"key":key,"committed_at":self.rt.now()});
        if let Some(proof) = no_action {
            job["no_action_recovery"] = proof;
        }
        self.save(&job)?;
        drop(todo);
        drop(queue);
        let code = self.cancel_locked(&a.id, true, lock, &mut Vec::new(), err)?;
        drop(paused);
        if code == 0 {
            writeln!(out, "{}", json!({"outcome":"cancelled","job":a.id}))?;
        }
        Ok(code)
    }

    fn no_action_workspace(&self, db: &Value, row: &Value, job: &Value) -> bool {
        if !["cc-pc-tools-rs", "pc-capacity-audit", "localai-web-rs"].contains(&s(row, "project")) {
            return false;
        }
        let repo = db["projects"][s(row, "project")].as_str().unwrap_or("");
        let cwd = s(job, "cwd");
        if repo.is_empty() || cwd.is_empty() || row["scheduler"]["worktree"] != job["cwd"] {
            return false;
        }
        let base = self.rt.work_checkout(repo);
        let work = self.rt.work_checkout(cwd);
        !s(&base, "git_common").is_empty()
            && base["git_common"] == work["git_common"]
            && work["worktree"] == cc_job_policy::work_owner::norm(Path::new(cwd))
            && work["branch"] == row["scheduler"]["branch"]
            && ["autodevjapan/", "bm1314/", "mashfromband/"]
                .iter()
                .any(|prefix| s(&base, "repository").starts_with(prefix))
    }

    fn cancel_locked(
        &self,
        jid: &str,
        recovery: bool,
        lock: FileLock,
        out: &mut dyn Write,
        err: &mut dyn Write,
    ) -> Result<i32> {
        let mut job = self.refresh_locked(self.load(jid)?, false)?;
        if !LIVE.contains(&s(&job, "status")) && s(&job, "status") != "lost" {
            return Err(Error::new(format!(
                "{jid} is already {}",
                s(&job, "status")
            )));
        }
        if recovery && cc_job_policy::cancellation::owner_stopped(&Value::Null, &job) {
            return Err(Error::new("job was explicitly cancelled"));
        }
        if recovery && !cc_job_policy::cancellation::recovery_cancel(&Value::Null, &job) {
            // Plain scheduler/watchdog recovery uses this path as well as the
            // guarded console path. Save intent before killing, under state.lock.
            job["recovery_cancel"] =
                json!({"key":format!("recovery-{jid}"),"committed_at":self.rt.now()});
            self.save(&job)?;
        }
        if !recovery {
            // Save cancellation intent before killing; restart uses the same job lock.
            job["cancel_requested"] = json!(true);
            job["cancel_requested_at"] = json!(self.rt.now());
            self.save(&job)?;
        }
        let managed_root = if job["process_tree_version"] == 1 {
            job["pid"]
                .as_u64()
                .and_then(|pid| u32::try_from(pid).ok())
                .zip(job["pid_created"].as_u64())
        } else {
            None
        };
        let recorded_child = job["codex_pid"]
            .as_u64()
            .and_then(|pid| u32::try_from(pid).ok())
            .zip(job["codex_pid_created"].as_u64());
        let mut targets = Vec::new();
        let managed_tree_stopped = if let Some((pid, created)) = managed_root {
            if self
                .rt
                .kill_process_tree_job(pid, created, recorded_child)?
            {
                // The same Job Object's active=0 result is the completion
                // evidence. Do not reopen PIDs after their identities can be reused.
                targets.extend(["pid", "codex_pid"].into_iter().filter(|key| {
                    job[*key].as_u64().is_some() && job[format!("{key}_created")].as_u64().is_some()
                }));
                true
            } else {
                false
            }
        } else {
            false
        };
        if !managed_tree_stopped {
            // A supervisor that exited releases its named Job Object handle.
            // Its surviving descendants remain recoverable through this
            // retained-handle process walk.
            for key in ["pid", "codex_pid"] {
                if self.is_ours_checked(&job, key)? {
                    targets.push(key);
                }
            }
            for k in &targets {
                // Stopping the supervisor's tree can already stop codex_pid.
                // Recheck the recorded creation time before opening a second root.
                if self.is_ours_checked(&job, k)? {
                    let created = job[format!("{k}_created")]
                        .as_u64()
                        .ok_or_else(|| Error::new("owned process creation time is missing"))?;
                    self.rt
                        .kill_owned_tree(job[*k].as_u64().unwrap() as u32, created)?;
                }
            }
        }
        if !managed_tree_stopped {
            for _ in 0..50 {
                let mut any_live = false;
                for key in &targets {
                    any_live |= self.is_ours_checked(&job, key)?;
                }
                if !any_live {
                    break;
                }
                self.rt.sleep(0.1);
            }
        }
        let mut alive = Vec::new();
        if !managed_tree_stopped {
            for key in &targets {
                if self.is_ours_checked(&job, key)? {
                    alive.push(pystr(&job[*key]));
                }
            }
        }
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
        job["process_tree_cancel_method"] = json!(if managed_tree_stopped {
            "job_object"
        } else {
            "retained_handle_walk"
        });
        if cancelled {
            job["status"] = json!("cancelled");
            job["finished_at"] = json!(self.rt.now());
            job["activity"] = json!("cancelled");
            if let Some(start) = parse_iso(s(&job, "started_at")) {
                job["duration_ms"] = json!(((self.rt.epoch() - start) * 1000.) as i64);
            }
            self.save(&job)?;
        } else if s(&job, "status") == "lost" {
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
