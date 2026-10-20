//! Test fixture only. Never starts Codex or accesses the production job store.
use cc_jobs::*;
use std::{
    fs,
    io::{Read, Write},
    time::Duration,
};
fn main() {
    let args = std::env::args().skip(1).collect::<Vec<_>>();
    if args.as_slice() == ["_supervise", "breakaway-probe"] {
        let root = std::env::current_dir().unwrap();
        fs::write(root.join("breakaway.ready"), std::process::id().to_string()).unwrap();
        let deadline = std::time::Instant::now() + Duration::from_secs(15);
        while !root.join("breakaway.release").exists() && std::time::Instant::now() < deadline {
            std::thread::sleep(Duration::from_millis(5));
        }
        return;
    }
    if args.iter().any(|a| a == "account") {
        let at = chrono::Local::now().timestamp() as f64;
        if args.iter().any(|a| a == "openai") && std::env::var("CC_JOBS_FIXTURE_MODE")
            .is_ok_and(|mode|mode.starts_with("capacity-")) {
            println!(
                "{}",
                json!({"accounts":(1..=4).map(|n| json!({
                "id":format!("fixture-{n}"),"type":"codex","health":"healthy","creditsAfterLimit":false,
                "quota":{"weeklyPercent":10.,"weeklyResetAt":at+86400.}
            })).collect::<Vec<_>>()})
            );
            return;
        }
        println!(
            "{}",
            json!({"accounts":[{"id":"fixture-oauth","type":"oauth","health":"healthy","quota":{"weeklyPercent":10.,"weeklyResetAt":at+86400.}}]})
        );
        return;
    }
    if args
        .windows(2)
        .any(|a| a == ["get", "codexAccountNamespaces"])
    {
        println!(
            "{}",
            json!({"p000001":"fixture-1","p000002":"fixture-2","p000003":"fixture-3","p000004":"fixture-4"})
        );
        return;
    }
    if args.iter().any(|a| a == "logs") {
        let model = args
            .windows(2)
            .find(|w| w[0] == "--model")
            .map(|w| w[1].as_str())
            .unwrap_or("gpt-6-luna");
        println!(
            "{}",
            json!({"logs":[{"model":model,"accountLogLabel":"p000001","status":503,
            "timestamp":chrono::Local::now().timestamp_millis()}]})
        );
        return;
    }
    if args.iter().any(|a| a == "catalog") {
        println!(
            "{}",
            json!({"models":(["gpt-6.1-sol","gpt-6-luna"].map(|slug|json!({
                "slug":slug,"context_window":1000000,"supports_reasoning_summaries":true,
                "supported_reasoning_levels":[{"effort":"max"}],"base_instructions":"fixture"
            })))})
        );
        return;
    }
    if args.first().is_some_and(|s| s == "--healthy-cargo-gate") {
        let dir = std::path::PathBuf::from(
            std::env::var("CC_JOBS_FIXTURE_DIR").expect("fixture directory required"),
        );
        let env = std::env::vars().collect();
        let exit = gate::run_with_admitter(&args[1..], &dir, &env, |dir, env, cores| {
            gate::admission::wait_with_sample(dir, env, cores, || {
                Ok(gate::admission::Sample {
                    cpu_percent: 50.,
                    free_memory_gib: 32.,
                })
            })
        })
        .unwrap();
        std::process::exit(exit as i32);
    }
    if args.first().is_some_and(|s| s == "--echo-test-limits") {
        let var = |name: &str| std::env::var(name).unwrap_or_default();
        println!(
            "{}|{}|{}|{}",
            var("CARGO_BUILD_JOBS"),
            var("RUST_TEST_THREADS"),
            var("NEXTEST_TEST_THREADS"),
            args[1..].join(" ")
        );
        return;
    }
    if args.first().is_some_and(|s| s == "--exit-seven") {
        std::process::exit(7);
    }
    if args.first().is_some_and(|s| s == "--fault-exit") {
        std::process::exit(0xc0000005_u32 as i32);
    }
    if args.first().is_some_and(|s| s == "--echo-env") {
        println!(
            "{}",
            std::env::var("CODEX_JOBS_CARGO_LOCK_HELD").unwrap_or_default()
        );
        return;
    }
    if args.first().is_some_and(|s| s == "--echo-build") {
        let var = |name: &str| std::env::var(name).unwrap_or_default();
        println!(
            "{}|{}|{}",
            var("CARGO_BUILD_JOBS"),
            var("CODEX_JOBS_CARGO_TARGET_HELD"),
            args[1..].join(" ")
        );
        return;
    }
    let mode = std::env::var("CC_JOBS_FIXTURE_MODE").unwrap_or_default();
    let root = std::path::PathBuf::from(
        std::env::var("CC_JOBS_FIXTURE_DIR").expect("fixture directory required"),
    );
    if mode == "dependency-parent" {
        // Dedicated dependency fixture: hold the source process until explicit
        // cancellation/release or actual controller exit, not a 40-second clock.
        #[cfg(windows)]
        {
            use windows_sys::Win32::{Foundation::*, System::Threading::*};
            let owner: Value =
                serde_json::from_slice(&fs::read(root.join("fixture-owner.json")).unwrap())
                    .unwrap();
            let pid = owner["pid"].as_u64().unwrap() as u32;
            let created = owner["created"].as_u64().unwrap();
            unsafe {
                struct Handle(HANDLE);
                impl Drop for Handle {
                    fn drop(&mut self) {
                        unsafe {
                            CloseHandle(self.0);
                        }
                    }
                }
                let handle = Handle(OpenProcess(
                    PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
                    0,
                    pid,
                ));
                assert!(!handle.0.is_null());
                let mut times = [std::mem::zeroed::<FILETIME>(); 4];
                assert_ne!(
                    GetProcessTimes(
                        handle.0,
                        &mut times[0],
                        &mut times[1],
                        &mut times[2],
                        &mut times[3]
                    ),
                    0
                );
                assert_eq!(
                    ((times[0].dwHighDateTime as u64) << 32) | times[0].dwLowDateTime as u64,
                    created
                );
                let me = std::process::id();
                fs::write(
                    root.join("sleep.identity.json"),
                    pretty(&json!({"pid":me,"created":proc_created(me).unwrap()})),
                )
                .unwrap();
                fs::write(root.join("sleep.pid"), me.to_string()).unwrap();
                loop {
                    let waited = WaitForSingleObject(handle.0, 50);
                    if waited == WAIT_OBJECT_0 || root.join("parent.release").exists() {
                        break;
                    }
                    assert_eq!(waited, WAIT_TIMEOUT);
                }
            }
        }
        #[cfg(not(windows))]
        panic!("dependency parent fixture requires Windows");
    }
    if args
        .first()
        .is_some_and(|s| s == "--supervisor-with-independent-job")
    {
        let mut child = hidden_command(&std::env::current_exe().unwrap().to_string_lossy())
            .arg("--hold")
            .spawn()
            .unwrap();
        fs::write(
            root.join("controller.ready"),
            std::process::id().to_string(),
        )
        .unwrap();
        std::thread::sleep(Duration::from_secs(40));
        let _ = child.kill();
        let _ = child.wait();
        return;
    }
    if args.first().is_some_and(|s| s == "--resource-worker") {
        let held = FileLock::acquire(&root.join("cargo-build.lock.0")).unwrap();
        fs::write(root.join("civ6.lock"), "fixture owned lease").unwrap();
        fs::write(root.join("worker.ready"), "ready").unwrap();
        if std::env::var_os("CC_JOBS_FIXTURE_RESOURCE_BARRIER").is_some() {
            let deadline = std::time::Instant::now() + Duration::from_secs(30);
            while !root.join("worker.release").exists() {
                assert!(
                    std::time::Instant::now() < deadline,
                    "worker release timeout"
                );
                std::thread::sleep(Duration::from_millis(5));
            }
        } else {
            std::thread::sleep(Duration::from_millis(350));
        }
        fs::remove_file(root.join("civ6.lock")).unwrap();
        drop(held);
        fs::write(root.join("worker.done"), "done").unwrap();
        return;
    }
    if args.first().is_some_and(|s| s == "--mcp-idle") {
        fs::write(root.join("mcp.ready"), std::process::id().to_string()).unwrap();
        std::thread::sleep(Duration::from_secs(40));
        return;
    }
    if args.first().is_some_and(|s| s == "--freeze-controller") {
        let rt = System::default();
        let _paused = rt
            .pause_idle_agent(
                args[1].parse().unwrap(),
                args[2].parse().unwrap(),
                None,
                Some(&root.join("yield-paused.json")),
            )
            .unwrap()
            .unwrap();
        fs::write(root.join("freeze.ready"), "ready").unwrap();
        std::thread::sleep(Duration::from_secs(40));
        return;
    }
    if args
        .first()
        .is_some_and(|s| s == "--controller" || s == "--controller-mcp")
    {
        let mut mcp = if args[0] == "--controller-mcp" {
            let child = hidden_command(&root.join("mcp.exe").to_string_lossy())
                .arg("--mcp-idle")
                .spawn()
                .unwrap();
            while !root.join("mcp.ready").exists() {
                std::thread::sleep(Duration::from_millis(5));
            }
            Some(child)
        } else {
            None
        };
        fs::write(
            root.join("first-tool.filetime"),
            (((System::default().epoch() + 11_644_473_600.) * 10_000_000.) as u64).to_string(),
        )
        .unwrap();
        let mut child = hidden_command(&root.join("worker.exe").to_string_lossy())
            .arg("--resource-worker")
            .spawn()
            .unwrap();
        assert!(child.wait().unwrap().success());
        for n in 0..1000 {
            fs::write(root.join("heartbeat"), n.to_string()).unwrap();
            std::thread::sleep(Duration::from_millis(20));
        }
        if let Some(p) = mcp.as_mut() {
            p.kill().unwrap();
            p.wait().unwrap();
        }
        return;
    }
    if mode == "external-error" {
        eprintln!("mock-sensitive-stderr SENTINEL-TOKEN");
        std::process::exit(1);
    }
    if args
        .first()
        .is_some_and(|s| s == "--hold" || s == "--sleep")
    {
        fs::write(
            root.join(format!("{}.pid", &args[0][2..])),
            std::process::id().to_string(),
        )
        .unwrap();
        std::thread::sleep(Duration::from_secs(40));
        return;
    }
    if mode == "lock" {
        let _l = FileLock::acquire(&root.join("probe.lock")).unwrap();
        println!("held");
        std::io::stdout().flush().unwrap();
        std::thread::sleep(Duration::from_millis(650));
        return;
    }
    if mode == "report-only" {
        let mut input = Vec::new();
        std::io::stdin().read_to_end(&mut input).unwrap();
        println!("{{\"type\":\"turn.started\"}}");
        let end = std::time::Instant::now() + Duration::from_secs(40);
        let mut id = 0;
        while std::time::Instant::now() < end {
            println!(
                "{}",
                json!({"type":"item.completed","item":{"id":format!("m{id}"),"type":"agent_message","text":"fixture progress only"}})
            );
            std::io::stdout().flush().unwrap();
            id += 1;
            std::thread::sleep(Duration::from_millis(50));
        }
        return;
    }
    if mode == "cargo" {
        let now = || {
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_secs_f64()
        };
        // The gate already owns its locks at this point. Measure that interval,
        // rather than when this child is next scheduled after the ready barrier.
        let started = now();
        fs::write(root.join(format!("ready.{}", std::process::id())), b"").unwrap();
        // Builds that must overlap wait until all of them were admitted.
        let slots = std::env::var("CC_JOBS_FIXTURE_READY")
            .or_else(|_| std::env::var("CODEX_JOBS_CARGO_SLOTS"))
            .unwrap()
            .parse::<usize>()
            .unwrap()
            .max(1);
        let deadline = std::time::Instant::now() + Duration::from_secs(30);
        while fs::read_dir(&root)
            .unwrap()
            .flatten()
            .filter(|e| e.file_name().to_string_lossy().starts_with("ready."))
            .count()
            < slots
        {
            assert!(
                std::time::Instant::now() < deadline,
                "slot admission stalled"
            );
            std::thread::sleep(Duration::from_millis(5));
        }
        println!("START {started}");
        std::io::stdout().flush().unwrap();
        std::thread::sleep(Duration::from_millis(600));
        println!("END {}", now());
        return;
    }
    if mode == "hold" || mode == "slow" {
        let mut c = hidden_command(&std::env::current_exe().unwrap().to_string_lossy());
        c.arg(if mode == "hold" { "--hold" } else { "--sleep" });
        // Deliberately orphan this child: the kill-tree integration tests must
        // verify that the supervisor reaps descendants after this fixture exits.
        #[allow(clippy::zombie_processes)]
        {
            c.spawn().unwrap();
        }
        if mode == "slow" {
            std::thread::sleep(Duration::from_secs(40));
        }
    }
    let mut input = vec![];
    std::io::stdin().read_to_end(&mut input).unwrap();
    fs::write(root.join("stdin.bin"), input).unwrap();
    fs::write(
        root.join("call.json"),
        pretty(&json!({"argv":args,"cwd":std::env::current_dir().unwrap().to_string_lossy()})),
    )
    .unwrap();
    if mode == "capacity-once" || mode == "capacity-always" {
        let calls = root.join("capacity-calls.jsonl");
        let mut log = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(calls)
            .unwrap();
        let catalog = args
            .iter()
            .find_map(|a| a.strip_prefix("model_catalog_json="))
            .and_then(|path| fs::read(path).ok())
            .and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok())
            .unwrap_or(Value::Null);
        writeln!(log, "{}", json!({"argv":args,"cwd":std::env::current_dir().unwrap().to_string_lossy(),
            "prompt":String::from_utf8_lossy(&fs::read(root.join("stdin.bin")).unwrap()),
            "catalog_model":catalog["models"][0]["slug"],"catalog_context":catalog["models"][0]["context_window"]})).unwrap();
        let selected = args
            .windows(2)
            .find(|w| w[0] == "-m")
            .map(|w| w[1].as_str());
        if mode == "capacity-always" || selected == Some("gpt-6-luna") {
            println!(
                "{}",
                json!({"type":"thread.started","thread_id":"01a0e883-7628-7121-9540-6780fd4cd6f4"})
            );
            println!(
                "{}",
                json!({"type":"turn.failed","error":{"message":"Selected model is at capacity"}})
            );
            std::process::exit(1);
        }
    }
    if mode == "secret-events" || mode == "secret-hold" {
        // Synthetic credentials, assembled at run time.
        let token = format!("{}{}", "gh", "p_") + &"Ab3De5Gh7J".repeat(4);
        let secret = format!("\"serviceSecret\":\"{}\"", "sv7Hq2Xw".repeat(3));
        eprintln!("Authorization: Bearer {}", "v1.Zx9Kq2Lm".repeat(3));
        // A private key and a quoted value, each split over several lines.
        let pem = ["PRIVATE", "KEY"].join(" ");
        eprintln!("key: -----BEGIN RSA {pem}-----");
        eprintln!("{}", "Pk4Vb8Nm".repeat(8));
        eprintln!("{}", "Rt6Yu1Io".repeat(8));
        eprintln!("-----END RSA {pem}----- after key");
        eprintln!("licenseKey = \"{}", "Lq3Wz5Ex".repeat(3));
        eprintln!("{}\" after value", "Mj2Kn4Bh".repeat(3));
        eprintln!("plain stderr line");
        let command = format!("curl -H \"Authorization: token {token}\" -d '{{{secret}}}'");
        if mode == "secret-hold" {
            // Report a final message, then stay running until the test kills
            // this process: the records must already be masked mid-run.
            println!(
                "{}",
                json!({"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":format!("PARTIAL {token}")}})
            );
            println!("not json {token}");
            std::thread::sleep(Duration::from_secs(40));
            return;
        }
        for event in [
            json!({"type":"thread.started","thread_id":"01a0e883-7628-7121-9540-6780fd4cd6f4"}),
            json!({"type":"turn.started"}),
            json!({"type":"item.started","item":{"id":"item_1","type":"command_execution","command":command,"aggregated_output":"","exit_code":null,"status":"in_progress"}}),
            json!({"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":command,"aggregated_output":format!("{{{secret}}}\r\n"),"exit_code":0,"status":"completed"}}),
            json!({"type":"item.completed","item":{"id":"item_2","type":"mcp_tool_call","server":"s","tool":"t","arguments":{"serviceSecret":"abcd1234efgh"},"status":"completed"}}),
            json!({"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":format!("DONE {token}")}}),
            json!({"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0}}),
        ] {
            println!("{event}");
        }
        println!("not json {token}");
        return;
    }
    std::thread::sleep(Duration::from_secs(1));
    print!("{}", include_str!("../../tests/fixtures/OK_EVENTS.jsonl"));
    println!();
}
