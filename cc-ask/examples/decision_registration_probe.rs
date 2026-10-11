//! Run the real registration comparison and Discord GET boundary in an owned
//! scratch directory, stopping before a new intent or POST.
use cc_ask::{core::*, decision_identity, question_progress};
use serde_json::{json, Value};
use std::{cell::Cell, path::PathBuf, rc::Rc};

const REACHED: &str = "verified registration boundary; no send";

struct ReadOnlyBoundary {
    parent: String,
    verified: Rc<Cell<bool>>,
}

impl Effects for ReadOnlyBoundary {
    fn http(&mut self, m: &str, url: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        if !d || m != "GET" || b.is_some() {
            return Err("probe forbids mutating or non-Discord HTTP".into());
        }
        let endpoint = url.trim_start_matches("https://discord.com/api/v10");
        if endpoint != format!("/channels/{}", self.parent) && endpoint != "/users/@me" {
            return Err("probe forbids unrelated HTTP".into());
        }
        let value = RealEffects.http(m, url, b, d)?;
        if endpoint == "/users/@me" {
            if !truth(&value["bot"]) || s(&value["id"]).is_empty() {
                return Err("question sender unavailable".into());
            }
            self.verified.set(true);
            // question_progress writes its first new intent only after this
            // call succeeds. Returning here also prevents inventory and POST.
            return Err(REACHED.into());
        }
        Ok(value)
    }

    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        Err("probe forbids subprocesses".into())
    }
}

fn probe() -> Result<Value> {
    let mut args = std::env::args_os().skip(1);
    let scratch = PathBuf::from(args.next().ok_or("owned scratch directory required")?);
    if !scratch.is_dir() || scratch.join("state.json").exists() {
        return Err("probe directory must be new and isolated".into());
    }
    let decision = PathBuf::from(args.next().ok_or("decision file required")?);
    let body = PathBuf::from(args.next().ok_or("original body file required")?);
    let job = args.next().ok_or("original job required")?;
    let job = job.to_str().ok_or("invalid job")?;
    if args.next().is_some() || !valid_job(job) {
        return Err("invalid probe arguments".into());
    }
    let user = std::env::var_os("USERPROFILE").ok_or("home unavailable")?;
    let cfg = read_json(&PathBuf::from(user).join(".godd/discord-notify.json"))?;
    let verified = Rc::new(Cell::new(false));
    let home = PathBuf::from("D:/CC/Tools/discord-ask");
    let jobs = PathBuf::from("D:/CC/Tools/codex-jobs");
    let info = read_json(&jobs.join("jobs").join(job).join("job.json"))?;
    let mut ctx = Context {
        here: scratch.clone(),
        // No project-name scan occurs in this core boundary probe.
        client_projects: home.join("unused-probe-client-list"),
        civ6_home: home.clone(),
        jobs,
        discord_tasks: discord_tasks_dir()?,
        todo: PathBuf::from("D:/CC/Tools/todo/todo.json"),
        effects: Box::new(ReadOnlyBoundary {
            parent: s(&cfg["task_channel_id"]).into(),
            verified: verified.clone(),
        }),
        cfg,
        codex_command: home.clone(),
        scheduler_command: home.clone(),
        shepherd_command: home.clone(),
        todo_command: home,
    };
    let value = read_json(&decision)?;
    let identity = decision_identity::normalize(&ctx, &value)?;
    if n(&info["todo_id"]) != n(&identity["todo_id"]) {
        return Err("probe original job and decision ToDo differ".into());
    }
    let projected = scratch.join("question-progress");
    std::fs::create_dir(&projected).map_err(|e| e.to_string())?;
    let routes = PathBuf::from("D:/CC/Tools/discord-ask/question-progress");
    if routes.exists() {
        for entry in std::fs::read_dir(routes).map_err(|e| e.to_string())? {
            let path = entry.map_err(|e| e.to_string())?.path();
            if path.extension().is_some_and(|v| v == "json") {
                let record = read_json(&path)?;
                if record["guild"] == ctx.cfg["guild_id"]
                    && record["parent"] == ctx.cfg["task_channel_id"]
                    && record["decision_identity"] == identity
                {
                    return Ok(json!({"existing_intent":true,"complete":record["complete"],
                        "todo_id":identity["todo_id"],"next_action":"lookup-decision"}));
                }
                if record["guild"] == ctx.cfg["guild_id"]
                    && record["parent"] == ctx.cfg["task_channel_id"]
                    && !record["decision_identity"].is_object()
                {
                    // Only comparison evidence, never bodies, replies, tokens
                    // or attachments. Production ledgers are read-only.
                    let mut context = json!({});
                    for key in ["todo_id", "posting_job", "session_id", "cwd", "project"] {
                        context[key] = record["context"][key].clone();
                    }
                    atomic_json(
                        &projected.join(path.file_name().ok_or("invalid route path")?),
                        &json!({"guild":record["guild"],"parent":record["parent"],
                            "context":context,"work_thread_id":record["work_thread_id"],
                            "question_thread_id":record["question_thread_id"]}),
                        2,
                    )?;
                }
            }
        }
    }
    let mut context = json!({"todo_id":info["todo_id"],"posting_job":job,
        "cwd":info["cwd"],"project":info["project"],"source":"scheduler",
        "scheduler_item":info["todo_id"],"scheduler_job":job});
    decision_identity::bind(&ctx, &mut context, Some(&value))?;
    let result = question_progress::ask(
        &mut ctx,
        &context,
        s(&info["discord_thread_id"]),
        &read_text(&body)?,
        &[],
    );
    match result {
        Err(e) if e == REACHED && verified.get() => Ok(json!({
            "todo_id":identity["todo_id"],"registration_boundary_verified":true,
            "discord_gets":2,"mutating_requests":0,"question_registered":false
        })),
        Err(e) => Err(e),
        Ok(value) => Err(format!(
            "registration suppressed: {}",
            s(&value["suppressed"])
        )),
    }
}

fn main() {
    match probe() {
        Ok(value) => println!("{value}"),
        Err(e) => {
            eprintln!("{}", safe_text(e));
            std::process::exit(1);
        }
    }
}
