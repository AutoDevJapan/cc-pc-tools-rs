use crate::*;
use regex::Regex;
use std::{
    path::{Path, PathBuf},
    sync::{Arc, Mutex, mpsc},
    thread,
    time::Duration,
};
pub const NOTIFY_INTERVAL_S: f64 = 15.;
pub const NOTIFY_START_ATTEMPTS: usize = 4;
pub const NOTIFY_BACKOFF_BASE_S: f64 = 1.;
pub const NOTIFY_DEADLINE_S: f64 = 35.;
/// Bounded wait for start retries, pending updates, and the final notification.
pub const NOTIFY_SHUTDOWN_CAP_S: f64 = 300.;
/// Fixed labels only: commands, searches and speech may include private task text.
pub fn task_activity(job: &Value) -> String {
    let act = s(job, "activity");
    for (prefix, label) in [
        ("running:", "running command"),
        ("searching:", "searching"),
        ("editing:", "editing files"),
        ("mcp:", "using tool"),
        ("agent:", "using agent"),
        ("said:", "writing response"),
        ("done:", "done"),
        ("failed:", "failed"),
    ] {
        if act.starts_with(prefix) {
            return label.into();
        }
    }
    if act.starts_with("plan ") {
        return if Regex::new(r"^plan \d+/\d+$").unwrap().is_match(act) {
            short(act, 120)
        } else {
            "planning".into()
        };
    }
    if ["queued", "starting", "thinking", "cancelled"].contains(&act) {
        act.into()
    } else {
        "working".into()
    }
}
/// Working directory as the owner reads it: relative to the dev/ADJ folder when inside it.
pub fn short_cwd(cwd: &str) -> String {
    if cwd.is_empty() {
        return "（不明）".into();
    }
    let path = cwd.replace('\\', "/");
    for base in [
        "C:/Users/makhmeto/Desktop/dev/ADJ/",
        "C:/Users/makhmeto/Desktop/dev/",
    ] {
        if path.to_lowercase().starts_with(&base.to_lowercase()) {
            return path[base.len()..].to_string();
        }
    }
    path
}
/// An explicit installed binary wins; never launch a PowerShell alias.
pub fn ao_executable(root: &Path, home: &Path, rt: &dyn Runtime) -> Option<String> {
    if let Some(p) = rt.env().get("GODD_AO_BIN").filter(|s| !s.is_empty()) {
        return Path::new(p).is_file().then(|| p.clone());
    }
    for p in [
        home.join(".opencodex/godd-ao/godd-ao.exe"),
        root.join("bin/godd-ao.exe"),
    ] {
        if p.is_file() {
            return Some(p.to_string_lossy().into());
        }
    }
    rt.which("godd-ao.exe")
}
/// One JSON CLI call. Discord/API failures are data, never job failures.
pub fn ao_notify(
    exe: &str,
    action: &str,
    job: &Value,
    jobs: &Path,
    rt: &dyn Runtime,
    deadline: f64,
) -> Value {
    if rt.mono() >= deadline {
        return json!({"ok":false});
    }
    let Ok(names) =
        cc_task_title::Names::load(&Path::new(cc_task_title::TOOLS).join("client-projects.txt"))
    else {
        return json!({"ok":false});
    };
    let mut args = vec![
        exe.into(),
        "notify".into(),
        "task".into(),
        action.into(),
        "--id".into(),
        s(job, "id").into(),
    ];
    let detail = match action {
        "start" => {
            let prompt = std::fs::read_to_string(jobs.join(s(job, "id")).join("prompt.md"))
                .unwrap_or_default();
            let Ok(title) =
                cc_task_title::installed_job_title(job, &prompt, Path::new(cc_task_title::TOOLS))
            else {
                return json!({"ok":false});
            };
            args.extend([
                format!("--title={}", redact_local_log_secrets(&title, &rt.env())),
                format!(
                    "--phase=作業場所 {}",
                    names.redact(&short_cwd(s(job, "cwd")))
                ),
                format!("--activity={}", task_activity(job)),
            ]);
            // GoDD-AO interprets nonempty start detail as a request to replace the
            // supplied title with an LLM summary, including on a later update.
            // We already summarized it, and AO appends the job ID as a small marker.
            String::new()
        }
        "update" => {
            args.extend([
                format!("--percent={}", (5 + 5 * number(job, "steps")).min(95)),
                format!("--activity={}", task_activity(job)),
            ]);
            if truth(&job["activity_detail"]) {
                pystr(&job["activity_detail"])
            } else if truth(&job["activity"]) {
                pystr(&job["activity"])
            } else {
                task_activity(job)
            }
        }
        "finish" => {
            let status = s(job, "status");
            let source = if status == "succeeded" {
                s(job, "last_message")
            } else if !s(job, "last_error").is_empty() {
                s(job, "last_error")
            } else {
                s(job, "last_message")
            };
            args.push(format!(
                "--result={}",
                match status {
                    "succeeded" => "完了",
                    "cancelled" => "取り消し",
                    _ => "失敗",
                }
            ));
            if status != "succeeded" {
                args.push("--failed".into());
            }
            if status == "cancelled" {
                String::new()
            } else {
                source.into()
            }
        }
        _ => String::new(),
    };
    let remaining = deadline - rt.mono();
    if remaining <= 0. {
        return json!({"ok":false});
    }
    args.extend([
        "--detail-stdin".into(),
        format!("--deadline-ms={}", ((remaining * 1000.) as i64).max(1)),
    ]);
    match rt.call(Call::new(
        args,
        names.redact(&detail).into_bytes(),
        remaining,
    )) {
        Ok(r) if r.code == 0 => {
            let value = serde_json::from_slice::<Value>(&r.stdout)
                .ok()
                .filter(Value::is_object)
                .unwrap_or(json!({"ok":false}));
            if value["ok"] == true && value["thread_id"].as_str().is_some_and(|id| !id.is_empty()) {
                let remaining = deadline - rt.mono();
                if remaining <= 0. {
                    return json!({"ok":false});
                }
                let args = vec![
                    format!("{}/discord-ask/discord_ask.exe", cc_task_title::TOOLS),
                    "sync-job-card".into(),
                    "--job".into(),
                    s(job, "id").into(),
                ];
                if !rt
                    .call(Call::new(args, vec![], remaining))
                    .is_ok_and(|out| out.code == 0)
                {
                    return json!({"ok":false});
                }
            }
            value
        }
        _ => json!({"ok":false}),
    }
}
#[derive(Clone)]
pub struct Notice {
    pub kind: String,
    pub job: Value,
    pub deadline: f64,
}
pub struct TaskProgress {
    pub executable: Option<String>,
    pub last_edit: f64,
    pub backoff: f64,
    pub shutdown_cap: f64,
    rt: SharedRuntime,
    jobs: PathBuf,
    sender: Option<mpsc::Sender<Notice>>,
    pending: Arc<Mutex<Option<(Value, f64)>>>,
    done: Option<mpsc::Receiver<()>>,
}
impl TaskProgress {
    pub fn new(exe: Option<String>, jobs: PathBuf, rt: SharedRuntime) -> Self {
        let last_edit = rt.mono();
        Self {
            executable: exe,
            last_edit,
            backoff: NOTIFY_BACKOFF_BASE_S,
            shutdown_cap: NOTIFY_SHUTDOWN_CAP_S,
            rt,
            jobs,
            sender: None,
            pending: Arc::new(Mutex::new(None)),
            done: None,
        }
    }
    pub fn start(&mut self, job: &Value) {
        let Some(exe) = self.executable.clone() else {
            return;
        };
        let deadline = self.rt.mono() + NOTIFY_DEADLINE_S;
        let (tx, rx) = mpsc::channel::<Notice>();
        let (done_tx, done_rx) = mpsc::channel();
        let rt = self.rt.clone();
        let jobs = self.jobs.clone();
        let pending = self.pending.clone();
        let backoff = self.backoff;
        thread::spawn(move || {
            work_notices(&exe, &jobs, rt.as_ref(), &rx, &pending, backoff);
            let _ = done_tx.send(());
        });
        self.sender = Some(tx);
        self.done = Some(done_rx);
        let _ = self.sender.as_ref().unwrap().send(Notice {
            kind: "start".into(),
            job: job.clone(),
            deadline,
        });
    }
    pub fn update(&mut self, job: &Value, final_update: bool) {
        let Some(tx) = &self.sender else {
            return;
        };
        let now = self.rt.mono();
        if final_update || now - self.last_edit >= NOTIFY_INTERVAL_S {
            self.last_edit = now;
            let deadline = now + NOTIFY_DEADLINE_S;
            if final_update {
                let _ = tx.send(Notice {
                    kind: "finish".into(),
                    job: job.clone(),
                    deadline,
                });
            } else {
                let mut pending = self.pending.lock().unwrap();
                if pending.is_none() {
                    let _ = tx.send(Notice {
                        kind: "update".into(),
                        job: Value::Null,
                        deadline,
                    });
                }
                *pending = Some((job.clone(), deadline));
            }
        }
    }
    pub fn close(&mut self) -> bool {
        // Ending an attempt may only pause a job; disconnect without a finish
        // notice so its notification worker also exits on 429 requeue.
        self.sender.take();
        if let Some(rx) = &self.done
            && rx
                .recv_timeout(Duration::from_secs_f64(self.shutdown_cap))
                .is_err()
        {
            self.rt
                .diagnostic("task notification exceeded shutdown cap");
            return false;
        }
        self.done = None;
        true
    }
    pub fn notify_with_retry(&self, kind: &str, job: &Value, deadline: f64) -> bool {
        let Some(exe) = &self.executable else {
            return false;
        };
        notify_with_retry(
            exe,
            kind,
            job,
            &self.jobs,
            self.rt.as_ref(),
            deadline,
            self.backoff,
        )
    }
}
pub fn notify_with_retry(
    exe: &str,
    kind: &str,
    job: &Value,
    jobs: &Path,
    rt: &dyn Runtime,
    deadline: f64,
    backoff: f64,
) -> bool {
    for attempt in 0..NOTIFY_START_ATTEMPTS {
        if rt.mono() >= deadline {
            return false;
        }
        if ao_notify(exe, kind, job, jobs, rt, deadline)["ok"] == true {
            return true;
        }
        if attempt + 1 == NOTIFY_START_ATTEMPTS {
            return false;
        }
        let remaining = deadline - rt.mono();
        if remaining <= 0. {
            return false;
        }
        rt.sleep((backoff * 2_f64.powi(attempt as i32)).min(remaining));
    }
    false
}
/// Worker loop; the start gate is shared by ordinary completion and cancellation.
pub fn work_notices(
    exe: &str,
    jobs: &Path,
    rt: &dyn Runtime,
    rx: &mpsc::Receiver<Notice>,
    pending: &Mutex<Option<(Value, f64)>>,
    backoff: f64,
) {
    let Ok(start) = rx.recv() else {
        return;
    };
    if !notify_with_retry(exe, "start", &start.job, jobs, rt, start.deadline, backoff) {
        return;
    }
    while let Ok(mut notice) = rx.recv() {
        if notice.kind == "update" {
            let Some((job, deadline)) = pending.lock().unwrap().take() else {
                continue;
            };
            notice.job = job;
            notice.deadline = deadline;
        }
        if rt.mono() >= notice.deadline {
            if notice.kind == "finish" {
                return;
            }
            continue;
        }
        if notice.kind == "finish" {
            notify_with_retry(
                exe,
                "finish",
                &notice.job,
                jobs,
                rt,
                notice.deadline,
                backoff,
            );
            return;
        }
        ao_notify(exe, &notice.kind, &notice.job, jobs, rt, notice.deadline);
    }
}
pub fn task_cancel(job: &Value, exe: Option<String>, jobs: PathBuf, rt: SharedRuntime) {
    if !truth(&job["started_at"]) && !truth(&job["first_started_at"]) {
        return;
    }
    let mut n = TaskProgress::new(exe, jobs, rt);
    n.start(job);
    n.update(job, true);
    n.close();
}
