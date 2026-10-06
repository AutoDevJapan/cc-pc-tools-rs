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
    let mode = std::env::var("CC_JOBS_FIXTURE_MODE").unwrap_or_default();
    let root = std::path::PathBuf::from(
        std::env::var("CC_JOBS_FIXTURE_DIR").expect("fixture directory required"),
    );
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
        fs::write(root.join(format!("ready.{}", std::process::id())), b"").unwrap();
        let slots = std::env::var("CODEX_JOBS_CARGO_SLOTS")
            .unwrap()
            .parse::<usize>()
            .unwrap()
            .max(1);
        let deadline = std::time::Instant::now() + Duration::from_secs(10);
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
        let now = || {
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_secs_f64()
        };
        println!("START {}", now());
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
    let out = args.iter().position(|a| a == "-o").unwrap();
    fs::write(&args[out + 1], "DONE").unwrap();
    std::thread::sleep(Duration::from_secs(1));
    print!("{}", include_str!("../../tests/fixtures/OK_EVENTS.jsonl"));
    println!();
}
