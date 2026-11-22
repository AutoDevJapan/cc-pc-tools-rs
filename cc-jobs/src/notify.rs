use crate::*;
use regex::Regex;
use std::{
    path::{Path, PathBuf},
    sync::{Arc, Mutex, mpsc},
    thread,
    time::{Duration,Instant},
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
    let tools = rt.task_title_tools();
    let Ok(names) = cc_task_title::Names::load(&tools.join("client-projects.txt")) else {
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
            let Ok(title) = rt.task_job_title(job, &prompt) else {
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
                    tools
                        .join("discord-ask/discord_ask.exe")
                        .to_string_lossy()
                        .into_owned(),
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

#[derive(Clone,Copy,Debug,Default,PartialEq,Eq)]
enum NoticeDeliveryState { #[default] NotRequested, Queued, InFlight, Accepted, Failed, Suppressed }
impl NoticeDeliveryState {
    fn label(self)->&'static str {match self {Self::NotRequested=>"not_requested",Self::Queued=>"queued",Self::InFlight=>"in_flight",Self::Accepted=>"accepted",Self::Failed=>"failed",Self::Suppressed=>"suppressed"}}
}
#[derive(Default)]
struct NoticeLifecycle {
    binding:Option<(Value,Value)>,start:NoticeDeliveryState,start_attempts:u64,start_failures:u64,
    updates_attempted:u64,updates_accepted:u64,updates_failed:u64,
    finish:NoticeDeliveryState,finish_claimed:bool,finish_attempts:u64,finish_failures:u64,
    finish_suppressed:Option<&'static str>,
}
impl NoticeLifecycle {
    fn bind_start(&mut self,job:&Value)->bool {
        if self.binding.is_some() {return false;}
        self.binding=Some((job["id"].clone(),job["started_at"].clone()));self.start=NoticeDeliveryState::Queued;true
    }
    fn same_attempt(&self,job:&Value)->bool {self.binding.as_ref().is_some_and(|(id,generation)|*id==job["id"]&&*generation==job["started_at"])}
    fn queue_finish(&mut self,job:&Value)->bool {
        if !self.same_attempt(job)||self.finish!=NoticeDeliveryState::NotRequested {return false;}
        self.finish=NoticeDeliveryState::Queued;true
    }
    fn suppress_finish(&mut self,reason:&'static str) {
        if !self.finish_claimed&&!matches!(self.finish,NoticeDeliveryState::Accepted|NoticeDeliveryState::Failed) {
            self.finish=NoticeDeliveryState::Suppressed;self.finish_suppressed=Some(reason);
        }
    }
    fn claim_finish(&mut self,job:&Value)->bool {
        if !self.same_attempt(job) {return false;}
        if self.start!=NoticeDeliveryState::Accepted {self.suppress_finish("start_not_accepted");return false;}
        if self.finish_claimed||!matches!(self.finish,NoticeDeliveryState::NotRequested|NoticeDeliveryState::Queued) {return false;}
        self.finish_claimed=true;self.finish=NoticeDeliveryState::InFlight;true
    }
    fn before_attempt(&mut self,kind:&str) {
        match kind {"start"=>{self.start=NoticeDeliveryState::InFlight;self.start_attempts=self.start_attempts.saturating_add(1);},
            "finish"=>{self.finish=NoticeDeliveryState::InFlight;self.finish_attempts=self.finish_attempts.saturating_add(1);},
            _=>self.updates_attempted=self.updates_attempted.saturating_add(1)}
    }
    fn after_attempt(&mut self,kind:&str,accepted:bool) {
        match kind {"start"=>{self.start=if accepted{NoticeDeliveryState::Accepted}else{NoticeDeliveryState::Failed};if !accepted{self.start_failures=self.start_failures.saturating_add(1);}},
            "finish"=>{self.finish=if accepted{NoticeDeliveryState::Accepted}else{NoticeDeliveryState::Failed};if !accepted{self.finish_failures=self.finish_failures.saturating_add(1);}},
            _=>{if accepted{self.updates_accepted=self.updates_accepted.saturating_add(1);}else{self.updates_failed=self.updates_failed.saturating_add(1);}}}
    }
    fn facts(&self)->Value {
        json!({"start":self.start.label(),"start_attempts":self.start_attempts,"start_failures":self.start_failures,
            "updates_attempted":self.updates_attempted,"updates_accepted":self.updates_accepted,"updates_failed":self.updates_failed,
            "finish":self.finish.label(),"finish_claimed":self.finish_claimed,"finish_attempts":self.finish_attempts,
            "finish_failures":self.finish_failures,"finish_suppressed":self.finish_suppressed,
            "attempt_scope":"same_notifier_instance","attempt_count_scope":"ao_notify_invocations_not_process_launches"})
    }
}

#[cfg(test)]
pub(crate) type ProgressTimeoutHook=Box<dyn FnOnce(&thread::JoinHandle<()>)>;
#[cfg(test)]
thread_local! {
    pub(crate) static PROGRESS_AFTER_TIMEOUT: std::cell::RefCell<Option<ProgressTimeoutHook>> = const { std::cell::RefCell::new(None) };
}
#[derive(Clone,Copy,Debug,PartialEq,Eq)]
pub enum ProgressCompletion { Pending, Done, Failed }
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
    worker:Option<thread::JoinHandle<()>>,
    completion:ProgressCompletion,
    done_received:bool,channel_failed:bool,close_requested:bool,
    worker_start_attempted:bool,worker_started:bool,worker_joined:bool,
    delivery:Arc<Mutex<NoticeLifecycle>>,
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
            done: None,worker:None,completion:ProgressCompletion::Done,
            done_received:false,channel_failed:false,close_requested:false,
            worker_start_attempted:false,worker_started:false,worker_joined:false,
            delivery:Arc::new(Mutex::new(NoticeLifecycle::default())),
        }
    }
    pub fn start(&mut self, job: &Value) {
        let Some(exe) = self.executable.clone() else {
            return;
        };
        if self.worker_start_attempted||self.close_requested {return;}
        if !self.delivery.lock().unwrap().bind_start(job) {return;}
        let deadline = self.rt.mono() + NOTIFY_DEADLINE_S;
        let (tx, rx) = mpsc::channel::<Notice>();
        let rt = self.rt.clone();
        let jobs = self.jobs.clone();
        let pending = self.pending.clone();
        let backoff = self.backoff;
        let delivery=self.delivery.clone();
        let started=self.install_worker(move |done_tx| {
            work_notices_observed(&exe,&jobs,rt.as_ref(),&rx,&pending,NoticeRetry{backoff,delivery:Some(&delivery)});
            let _=done_tx.send(());
        });
        if !started{return;}
        self.sender=Some(tx);
        let _ = self.sender.as_ref().unwrap().send(Notice {
            kind: "start".into(),
            job: job.clone(),
            deadline,
        });
    }
    pub fn update(&mut self, job: &Value, final_update: bool) {
        if self.close_requested||!self.delivery.lock().unwrap().same_attempt(job) {return;}
        let Some(tx) = &self.sender else {
            return;
        };
        let now = self.rt.mono();
        if final_update || now - self.last_edit >= NOTIFY_INTERVAL_S {
            self.last_edit = now;
            let deadline = now + NOTIFY_DEADLINE_S;
            if final_update {
                if !self.delivery.lock().unwrap().queue_finish(job) {return;}
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
    fn install_worker(&mut self,work:impl FnOnce(mpsc::Sender<()>)+Send+'static)->bool {
        if self.worker_start_attempted {return false;}
        let (done_tx,done_rx)=mpsc::channel();
        self.worker_start_attempted=true;self.close_requested=false;self.done=Some(done_rx);self.completion=ProgressCompletion::Pending;
        match thread::Builder::new().name("task-progress".into()).spawn(move || work(done_tx)) {
            Ok(worker)=>{self.worker=Some(worker);self.worker_started=true;true},
            Err(_)=>{
                self.done=None;self.channel_failed=true;self.completion=ProgressCompletion::Failed;
                let mut state=self.delivery.lock().unwrap();
                if state.binding.is_some() {state.start=NoticeDeliveryState::Failed;state.suppress_finish("worker_setup_failed");}
                false
            }
        }
    }
    #[cfg(test)]
    pub(crate) fn completion_test_worker(&mut self,work:impl FnOnce(mpsc::Sender<()>)+Send+'static) {assert!(self.install_worker(work));}
    pub fn completion(&self)->ProgressCompletion {self.completion}
    pub fn collection_facts(&self)->Value {
        json!({"state":match self.completion {ProgressCompletion::Pending=>"pending",ProgressCompletion::Done=>"done",ProgressCompletion::Failed=>"failed"},
            "worker_start_attempted":self.worker_start_attempted,"worker_started":self.worker_started,"worker_joined":self.worker_joined,
            "done_received":self.done_received,"channel_failed":self.channel_failed,"close_requested":self.close_requested,"delivery":self.delivery_facts()})
    }
    pub fn close_status(&mut self)->ProgressCompletion {
        // Close also ends a retry/requeue worker without synthesizing finish.
        // Only a separately requested terminal delivery consumes the start gate.
        if !self.close_requested {self.close_requested=true;self.sender.take();}
        if self.worker.is_none() {return self.completion;}
        let cap=if self.shutdown_cap.is_finite()&&self.shutdown_cap>=0. {self.shutdown_cap.min(NOTIFY_SHUTDOWN_CAP_S)}else{0.};
        let deadline=Instant::now()+Duration::from_secs_f64(cap);
        loop {
            if !self.done_received&&!self.channel_failed&&let Some(rx)=&self.done {
                match rx.recv_timeout(deadline.saturating_duration_since(Instant::now())) {
                    Ok(())=>self.done_received=true,
                    Err(mpsc::RecvTimeoutError::Timeout)=>{
                        #[cfg(test)]
                        PROGRESS_AFTER_TIMEOUT.with(|hook| { if let Some(hook)=hook.borrow_mut().take() { hook(self.worker.as_ref().unwrap()); } });
                    },
                    Err(mpsc::RecvTimeoutError::Disconnected)=>self.channel_failed=true,
                }
            }
            if self.worker.as_ref().is_some_and(thread::JoinHandle::is_finished) {
                let joined=self.worker.take().unwrap().join();
                self.worker_joined=true;
                // Timeout describes the earlier receive, not the worker's final
                // state. Join synchronizes the same worker's last send; retain
                // its receiver until that queued completion has been observed.
                if !self.done_received&&!self.channel_failed {
                    match self.done.as_ref().map(mpsc::Receiver::try_recv) {
                        Some(Ok(()))=>self.done_received=true,
                        _=>self.channel_failed=true,
                    }
                }
                self.done=None;
                self.completion=if joined.is_ok()&&self.done_received&&!self.channel_failed {ProgressCompletion::Done}else{ProgressCompletion::Failed};
                return self.completion;
            }
            if Instant::now()>=deadline {
                self.rt.diagnostic("task notification exceeded shutdown cap");
                return ProgressCompletion::Pending;
            }
            thread::yield_now();
        }
    }
    pub fn close(&mut self)->bool {self.close_status()==ProgressCompletion::Done}
    pub fn delivery_facts(&self)->Value {self.delivery.lock().unwrap().facts()}
    pub(crate) fn suppress_final_delivery(&mut self,reason:&'static str) {self.delivery.lock().unwrap().suppress_finish(reason);}
    pub(crate) fn finish_after_collection(&mut self,job:&Value) {
        if self.executable.is_none() {self.suppress_final_delivery("notifier_unavailable");return;}
        let _=self.notify_with_retry("finish",job,self.rt.mono()+NOTIFY_DEADLINE_S);
    }
    pub fn notify_with_retry(&self,kind:&str,job:&Value,deadline:f64)->bool {
        let Some(exe)=&self.executable else {return false;};
        if kind=="start" {
            // Existing standalone retry callers do not mint this worker's
            // start acceptance. Only work_notices_observed owns that fact.
            return notify_with_retry(exe,kind,job,&self.jobs,self.rt.as_ref(),deadline,self.backoff);
        }
        {
            let mut state=self.delivery.lock().unwrap();
            if kind=="finish" {
                if self.completion!=ProgressCompletion::Done||!self.close_requested||
                    (self.worker_started&&(!self.worker_joined||!self.done_received||self.channel_failed)) {
                    return false;
                }
                if !state.claim_finish(job) {return false;}
            }else if kind!="update"||self.close_requested||!state.same_attempt(job)||state.start!=NoticeDeliveryState::Accepted {
                return false;
            }
        }
        let accepted=notify_with_retry_observed(exe,kind,job,&self.jobs,self.rt.as_ref(),deadline,
            NoticeRetry{backoff:self.backoff,delivery:Some(&self.delivery)});
        if kind=="finish"&&!accepted {self.delivery.lock().unwrap().finish=NoticeDeliveryState::Failed;}
        accepted
    }
}
pub fn notify_with_retry(
    exe:&str,kind:&str,job:&Value,jobs:&Path,rt:&dyn Runtime,deadline:f64,backoff:f64,
)->bool {notify_with_retry_observed(exe,kind,job,jobs,rt,deadline,NoticeRetry{backoff,delivery:None})}
struct NoticeRetry<'a> {backoff:f64,delivery:Option<&'a Arc<Mutex<NoticeLifecycle>>>}
fn notify_with_retry_observed(
    exe:&str,kind:&str,job:&Value,jobs:&Path,rt:&dyn Runtime,deadline:f64,observation:NoticeRetry<'_>,
)->bool {
    let NoticeRetry{backoff,delivery}=observation;
    for attempt in 0..NOTIFY_START_ATTEMPTS {
        if rt.mono()>=deadline {return false;}
        if let Some(state)=delivery {state.lock().unwrap().before_attempt(kind);}
        let accepted=ao_notify(exe,kind,job,jobs,rt,deadline)["ok"]==true;
        if let Some(state)=delivery {state.lock().unwrap().after_attempt(kind,accepted);}
        if accepted {return true;}
        if attempt+1==NOTIFY_START_ATTEMPTS {return false;}
        let remaining=deadline-rt.mono();if remaining<=0. {return false;}
        rt.sleep((backoff*2_f64.powi(attempt as i32)).min(remaining));
    }
    false
}
/// Worker loop; completion and cancellation share one observed start gate.
pub fn work_notices(
    exe:&str,jobs:&Path,rt:&dyn Runtime,rx:&mpsc::Receiver<Notice>,
    pending:&Mutex<Option<(Value,f64)>>,backoff:f64,
) {
    let delivery=Arc::new(Mutex::new(NoticeLifecycle::default()));
    work_notices_observed(exe,jobs,rt,rx,pending,NoticeRetry{backoff,delivery:Some(&delivery)});
}
fn work_notices_observed(
    exe:&str,jobs:&Path,rt:&dyn Runtime,rx:&mpsc::Receiver<Notice>,
    pending:&Mutex<Option<(Value,f64)>>,observation:NoticeRetry<'_>,
) {
    let NoticeRetry{backoff,delivery}=observation;
    let delivery=delivery.expect("owned notice lifecycle");
    let Ok(start)=rx.recv() else {delivery.lock().unwrap().suppress_finish("start_not_received");return;};
    {
        let mut state=delivery.lock().unwrap();
        if state.binding.is_none() {let _=state.bind_start(&start.job);}
        if start.kind!="start"||!state.same_attempt(&start.job) {
            state.start=NoticeDeliveryState::Failed;state.suppress_finish("invalid_start_notice");return;
        }
    }
    if !notify_with_retry_observed(exe,"start",&start.job,jobs,rt,start.deadline,NoticeRetry{backoff,delivery:Some(delivery)}) {
        let mut state=delivery.lock().unwrap();state.start=NoticeDeliveryState::Failed;state.suppress_finish("start_not_accepted");return;
    }
    while let Ok(mut notice)=rx.recv() {
        if notice.kind=="update" {
            let Some((job,deadline))=pending.lock().unwrap().take() else {continue;};
            notice.job=job;notice.deadline=deadline;
        }
        if !delivery.lock().unwrap().same_attempt(&notice.job) {continue;}
        if rt.mono()>=notice.deadline {
            if notice.kind=="finish" {delivery.lock().unwrap().suppress_finish("finish_deadline_elapsed");return;}
            continue;
        }
        if notice.kind=="finish" {
            let claimed=delivery.lock().unwrap().claim_finish(&notice.job);
            if claimed {
                let accepted=notify_with_retry_observed(exe,"finish",&notice.job,jobs,rt,notice.deadline,NoticeRetry{backoff,delivery:Some(delivery)});
                if !accepted {delivery.lock().unwrap().finish=NoticeDeliveryState::Failed;}
            }
            return;
        }
        if notice.kind!="update" {continue;}
        delivery.lock().unwrap().before_attempt("update");
        let accepted=ao_notify(exe,"update",&notice.job,jobs,rt,notice.deadline)["ok"]==true;
        delivery.lock().unwrap().after_attempt("update",accepted);
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
