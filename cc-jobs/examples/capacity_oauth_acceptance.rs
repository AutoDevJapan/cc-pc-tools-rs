//! Explicit opt-in acceptance with the already installed OAuth Codex shim.
//! It injects one protocol capacity event and its account attribution.
//! Both model responses are real; this is not evidence of natural capacity.
use cc_jobs::*;
use std::{
    io::Read,
    path::PathBuf,
    sync::{Arc, mpsc},
    time::{Duration, Instant},
};

fn run() -> Result<bool> {
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
    let mut job = json!({"id":"acceptance-3081","name":"capacity-oauth-acceptance","model":"gpt-6.1-sol","requested_model":"gpt-6.1-sol","model_explicit":true,"sandbox":"read-only","effort":"max","mcp_profile":"minimal","codex":codex,"cwd":cwd,"status":"running","todo_id":3081});
    // Fix only these opt-in verification requests to known public selectors.
    // Never select the operator's global active credential for an experiment.
    let pool = ctx.rt.capacity_pool_sample(&job, ctx.rt.epoch());
    let initial_selector = pool["eligible"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .find(|selector| pool_retry::public_selector(selector))
        .ok_or_else(|| Error::new("verified OAuth account selector unavailable"))?
        .to_owned();
    job["capacity_account_selector"] = json!(initial_selector);
    let call = |j: &Value, prompt: &str| -> Result<Value> {
        let mut args = codex_args(j);
        ctx.capacity_catalog_args(j, &mut args)?;
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
        let child_birth = ctx.rt.proc_created_checked(child.pid())?;
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
                if let Some(created) = child_birth {
                    let _ = ctx.rt.kill_owned_tree(child.pid(), created);
                }
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
        "Reply exactly ACCEPTANCE_INITIAL_3081. Do not use tools.",
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
    // The first real request succeeded, so production failed-request attribution
    // correctly refuses to treat it as a failure. Inject only the harness's
    // known selector alongside the fresh public OAuth eligibility snapshot.
    let at = ctx.rt.epoch();
    let mut injected_pool = ctx.rt.capacity_pool_sample(&job, at);
    injected_pool["verified"] = json!(true);
    injected_pool["failed"] = json!([initial_selector]);
    injected_pool["at"] = json!(at);
    job["capacity_inventory"] = json!({"at":at,"pool":injected_pool});
    if !ctx.retry_capacity(&mut job) {
        return Err(Error::new("capacity transition unavailable"));
    }
    if job["model"] != "gpt-6.1-sol"
        || job["capacity_account_selector"] == initial_selector
        || job["capacity_same_model_retry_count"] != 1
    {
        return Err(Error::new(
            "same-model alternate account transition unavailable",
        ));
    }
    while job["retry_not_before"]
        .as_f64()
        .is_some_and(|at| at > ctx.rt.epoch())
    {
        ctx.rt.sleep(0.1);
    }
    std::fs::create_dir_all(ctx.job_dir(s(&job, "id")))?;
    ctx.save(&job)?;
    job = call(
        &job,
        "Reply exactly ACCEPTANCE_SWITCH_3081. Do not use tools.",
    )?;
    Ok(job["status"] == "succeeded"
        && job["model"] == "gpt-6.1-sol"
        && job["resume_session"] == session
        && job["last_message"]
            .as_str()
            .is_some_and(|m| m.contains("ACCEPTANCE_SWITCH_3081")))
}
fn main() {
    match run() {
        Ok(true) => {
            // Account metadata and provider errors never become log values.
            println!(
                "OAuth acceptance verified: same model and conversation, max/fast requested; capacity event was synthetic"
            );
        }
        Ok(false) | Err(_) => {
            eprintln!("OAuth acceptance did not complete; no account metadata is logged");
            std::process::exit(1);
        }
    }
}
