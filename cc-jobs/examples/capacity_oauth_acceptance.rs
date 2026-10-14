//! Explicit opt-in acceptance with the already installed OAuth Codex shim.
//! It injects one protocol capacity event; both model responses are real.
use cc_jobs::*;
use std::{
    io::Read,
    path::PathBuf,
    sync::{Arc, mpsc},
    time::{Duration, Instant},
};

fn run() -> Result<Value> {
    let root = PathBuf::from(
        std::env::args()
            .nth(1)
            .ok_or_else(|| Error::new("receipt directory required"))?,
    );
    let rt: SharedRuntime = Arc::new(System::default());
    let codex = rt
        .which("codex.cmd")
        .ok_or_else(|| Error::new("OAuth shim unavailable"))?;
    let cwd = std::env::current_dir()?;
    let ctx = Context {
        jobs: root.join("jobs"),
        root: root.clone(),
        home: PathBuf::new(),
        codex_home: PathBuf::new(),
        gto: root.join("no-gto"),
        executable: std::env::current_exe()?,
        rt,
    };
    let mut job = json!({"id":"acceptance-2528","name":"capacity-oauth-acceptance","model":"gpt-6.1-sol","requested_model":"gpt-6.1-sol","model_explicit":true,"sandbox":"read-only","effort":"low","mcp_profile":"minimal","codex":codex,"cwd":cwd,"status":"running","todo_id":2528});
    let call = |j: &Value, prompt: &str| -> Result<Value> {
        let mut args = codex_args(j);
        // This tiny acceptance must not start configured plugin/MCP helpers.
        args.splice(
            2..2,
            [
                "-c".into(),
                "features.plugins=false".into(),
                "-c".into(),
                "features.apps=false".into(),
            ],
        );
        std::fs::create_dir_all(&ctx.root)?;
        let input = ctx.root.join("acceptance-input.txt");
        std::fs::write(&input, prompt)?;
        let mut child = ctx.rt.spawn(&Spawn {
            argv: args,
            prompt: input,
            cwd: cwd.clone(),
            env: ctx.job_environment(),
            flags: NO_WINDOW,
        })?;
        let (tx, rx) = mpsc::channel();
        for (stdout, stream) in [(true, child.stdout()), (false, child.stderr())] {
            let tx = tx.clone();
            std::thread::spawn(move || {
                let mut bytes = Vec::new();
                if let Some(mut stream) = stream {
                    let _ = stream.read_to_end(&mut bytes);
                }
                let _ = tx.send((stdout, bytes));
            });
        }
        let end = Instant::now() + Duration::from_secs(90);
        let mut reply = Reply {
            code: 1,
            stdout: vec![],
            stderr: vec![],
        };
        let mut drained = 0;
        let mut code = None;
        while code.is_none() || drained < 2 {
            if Instant::now() >= end {
                let _ = ctx.rt.kill_tree(child.pid());
                let _ = child.kill();
                return Err(Error::new(
                    "OAuth acceptance timed out; owned process tree stopped",
                ));
            }
            if let Ok((stdout, bytes)) = rx.recv_timeout(Duration::from_millis(20)) {
                if stdout {
                    reply.stdout = bytes;
                } else {
                    reply.stderr = bytes;
                }
                drained += 1;
            }
            if code.is_none() {
                code = child.poll()?;
            }
        }
        reply.code = code.unwrap();
        let mut result = j.clone();
        for line in reply.stdout.split(|b| *b == b'\n') {
            if let Ok(ev) = serde_json::from_slice::<Value>(line) {
                apply_event(&mut result, &ev);
            }
        }
        finish(&mut result, Some(reply.code), 0, &ctx.rt.now());
        Ok(result)
    };
    job = call(
        &job,
        "Reply exactly ACCEPTANCE_INITIAL_2528. Do not use tools.",
    )?;
    if job["status"] != "succeeded" {
        return Err(Error::new("initial OAuth response failed"));
    }
    let session = job["thread_id"].clone();
    apply_event(
        &mut job,
        &json!({"type":"turn.failed","error":{"message":"Selected model is at capacity"}}),
    );
    job["status"] = json!("failed");
    if !ctx.retry_capacity(&mut job) {
        return Err(Error::new("capacity transition unavailable"));
    }
    std::fs::create_dir_all(ctx.job_dir(s(&job, "id")))?;
    ctx.save(&job)?;
    job = call(
        &job,
        "Reply exactly ACCEPTANCE_SWITCH_2528. Do not use tools.",
    )?;
    let ok = job["status"] == "succeeded"
        && job["last_message"]
            .as_str()
            .is_some_and(|m| m.contains("ACCEPTANCE_SWITCH_2528"));
    Ok(
        json!({"initial_model":"gpt-6.1-sol","switched_model":job["model"],"same_session":job["resume_session"]==session,"todo_id":job["todo_id"],"capacity_event":"injected canonical turn.failed","oauth_response_verified":ok,"switch_count":job["capacity_retry_count"]}),
    )
}
fn main() {
    match run() {
        Ok(v) => {
            println!("{v}");
            if v["oauth_response_verified"] != true {
                std::process::exit(1);
            }
        }
        Err(e) => {
            eprintln!("{}", e.message);
            std::process::exit(1);
        }
    }
}
