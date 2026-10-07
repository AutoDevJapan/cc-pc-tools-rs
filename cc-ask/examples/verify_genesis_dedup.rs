//! ToDo1857 acceptance against real Discord history, with all remote writes
//! and subprocess launches rejected. Only the supplied private scratch is changed.
use cc_ask::{commands::*, core::*};
use serde_json::{json, Value};
use std::{cell::Cell, path::PathBuf, rc::Rc};

const FIRST: &str = "1556678685426454568";
const SECOND: &str = "1556679305818542161";
const TITLE: &str = "【GoDD-Genesis】税・返金・問い合わせの担当と方針";

#[derive(Clone, Default)]
struct ReadOnly {
    gets: Rc<Cell<usize>>,
    writes: Rc<Cell<usize>>,
    subprocesses: Rc<Cell<usize>>,
}
impl Effects for ReadOnly {
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if method != "GET" {
            self.writes.set(self.writes.get() + 1);
            return Err("verification forbids remote writes".into());
        }
        if !discord || !url.starts_with("https://discord.com/api/v10/") {
            return Err("verification permits only Discord GET requests".into());
        }
        self.gets.set(self.gets.get() + 1);
        RealEffects.http(method, url, body, discord)
    }
    fn run(&mut self, _: &[String], _: u64) -> Result<Output> {
        self.subprocesses.set(self.subprocesses.get() + 1);
        Err("verification forbids subprocesses".into())
    }
    fn now(&self) -> f64 {
        RealEffects.now()
    }
}

fn verify() -> Result<()> {
    let here = PathBuf::from(
        std::env::args_os()
            .nth(1)
            .ok_or("supply an owned scratch directory")?,
    );
    // Do not overwrite a previous verification or touch the live state.
    std::fs::create_dir(&here).map_err(|_| "scratch directory must be new")?;
    let cfg = read_json(
        &PathBuf::from(std::env::var_os("USERPROFILE").ok_or("home missing")?)
            .join(".godd/discord-notify.json"),
    )?;
    let audit = ReadOnly::default();
    let mut ctx = Context {
        here: here.clone(),
        client_projects: "D:/CC/Tools/client-projects.txt".into(),
        civ6_home: here.join("unused-civ6"),
        jobs: here.join("unused-jobs"),
        discord_tasks: here.join("unused-tasks"),
        todo: here.join("unused-todo.json"),
        cfg,
        effects: Box::new(audit.clone()),
        codex_command: here.join("unused-codex"),
        scheduler_command: here.join("unused-scheduler"),
        shepherd_command: here.join("unused-shepherd"),
        todo_command: here.join("unused-todo"),
    };
    let owner = ctx.owner();
    for (thread, answer) in [
        (FIRST, "1556678932798120018"),
        (SECOND, "1556679451796971662"),
    ] {
        let channel = ctx.call("GET", &format!("/channels/{thread}"), None)?;
        if channel["name"] != TITLE || channel["guild_id"] != ctx.cfg["guild_id"] {
            return Err("incident thread context changed".into());
        }
        let history = thread_history(&mut ctx, thread)?;
        if !history.iter().any(|m| {
            s(&m["id"]) == answer
                && s(&m["author"]["id"]) == owner
                && s(&m["content"]).ends_with("a税込")
        }) {
            return Err("original tax answer not found".into());
        }
    }
    // Read only this thread's explicit request routing; do not infer a working
    // folder from its title. Replay an open recovered question in private state.
    let live = read_json(&PathBuf::from("D:/CC/Tools/discord-ask/state.json"))?;
    let cwd = s(&live["requests"][FIRST]["job_info"]["cwd"]);
    if cwd.is_empty() {
        return Err("incident request working folder unavailable".into());
    }
    let mut state = ctx.load()?;
    state["questions"] = json!([{"thread_id":FIRST,"after":FIRST,"title":TITLE,
        "open":true,"agent":"Codex","asked_at":"2026-10-05T23:45:20+09:00"}]);
    state["requests"][FIRST] = json!({"job_info":{"cwd":cwd}});
    ctx.save(&state)?;
    let body = here.join("question.txt");
    std::fs::write(
        &body,
        "【GoDD-Genesis】税の方針を決めてください。\na 税込\nb 税抜",
    )
    .map_err(|_| "scratch body write failed")?;
    let result = cmd_ask(
        &mut ctx,
        &AskArgs {
            title: TITLE.into(),
            body_file: body,
            cwd: Some(cwd.into()),
            agent: "Codex".into(),
            ..Default::default()
        },
    )?;
    if result["thread_id"] != FIRST
        || result["suppressed"] != "already_answered"
        || result["latest_owner_reply"]["id"] != "1556678932798120018"
        || ctx.load()? != state
        || audit.writes.get() != 0
    {
        return Err("incident replay did not preserve the first answer and question".into());
    }
    println!(
        "{}",
        json!({"verified":true,"original_threads":2,"remote_gets":audit.gets.get(),
        "remote_write_attempts":audit.writes.get(),"blocked_subprocess_attempts":audit.subprocesses.get(),
        "state_scope":"private scratch; explicit incident request routing",
        "limitations":"git project detection and cwd spelling variations are not exercised"})
    );
    Ok(())
}

fn main() {
    if let Err(error) = verify() {
        eprintln!("{error}");
        std::process::exit(1);
    }
}
