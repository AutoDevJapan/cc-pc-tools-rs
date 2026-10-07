#![windows_subsystem = "windows"]
use cc_scheduler::common::*;
use cc_shepherd::*;
use std::path::PathBuf;
fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let root = PathBuf::from("D:/CC/Tools/shepherd");
    let result = if args.first().map(String::as_str) == Some("adopt") {
        if args.len() != 3 {
            Err(Error("Usage: shepherd adopt <repository#PR> <live-job-id>"))
        } else {
            cmd_adopt(
                &root,
                &PathBuf::from("D:/CC/Tools/codex-jobs"),
                &args[1],
                &args[2],
                now(),
            )
            .map(|report| println!("{report}"))
        }
    } else if args.first().map(String::as_str) == Some("step") {
        let jobs = PathBuf::from("D:/CC/Tools/codex-jobs");
        let mut b = RealBackend {
            jobs: jobs.join("jobs"),
            runner: Box::new(SystemRunner),
        };
        cmd_step(
            &root,
            &jobs,
            &mut b,
            &mut SystemRunner,
            now(),
            args.iter().any(|x| x == "--dry-run"),
            &repos(),
        )
        .map(|report| {
            if let Some(report) = report {
                println!("{}", serde_json::to_string(&report).unwrap());
            }
        })
    } else {
        status(&root).map(|text| println!("{text}"))
    };
    if let Err(e) = result {
        eprintln!("{}", e.0);
        std::process::exit(1);
    }
}
