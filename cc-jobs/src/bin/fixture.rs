//! Test fixture only. Never starts Codex or accesses the production job store.
use cc_jobs::*;
use std::{
    fs,
    io::{Read, Write},
    time::Duration,
};
fn main() {
    let args = std::env::args().skip(1).collect::<Vec<_>>();
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
        std::thread::sleep(Duration::from_millis(350));
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
        writeln!(log, "{}", json!({"argv":args,"cwd":std::env::current_dir().unwrap().to_string_lossy(),"prompt":String::from_utf8_lossy(&fs::read(root.join("stdin.bin")).unwrap())})).unwrap();
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
