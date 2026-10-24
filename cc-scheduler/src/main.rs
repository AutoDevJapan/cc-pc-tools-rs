#![windows_subsystem = "windows"]
use cc_scheduler::{
    backend::RealBackend,
    common::*,
    engine::{step, Scheduler},
};
use std::path::PathBuf;
fn main() {
    if cc_scheduler::product_identity::status_requested(std::env::args().skip(1)) {
        println!("{}", cc_scheduler::product_identity::status());
        return;
    }
    let args: Vec<String> = std::env::args().skip(1).collect();
    let command = args.first().map(String::as_str);
    if command == Some("resume-registration") {
        if args.len() == 2 && matches!(args[1].as_str(), "-h" | "--help") {
            println!("usage: scheduler resume-registration --id <ID> --request-file <JSON> [--apply --expected-state-sha256 <SHA256>]\n\nRecord an explicit request to resume one idle, qualified repair task. Restores only its dispatchable flag; preserves original work, stops, dependencies, waits and protection. Never launches. JSON: todo_id, intent=resume_registration_only, request, source_ref.");
            return;
        }
        let result = (|| -> Result<serde_json::Value> {
            if !matches!(args.len(), 5 | 8) || args[1] != "--id" || args[3] != "--request-file" {
                return Err(Error("InputRejected"));
            }
            let id = args[2].parse().map_err(|_| Error("InputRejected"))?;
            let expected = if args.len() == 8 {
                if args[5] != "--apply" || args[6] != "--expected-state-sha256" {
                    return Err(Error("InputRejected"));
                }
                Some(args[7].as_str())
            } else {
                None
            };
            let request = cc_scheduler::registration_resume::read_request(
                std::path::Path::new(&args[4]),
                id,
            )?;
            let root = PathBuf::from("D:/CC/Tools/scheduler");
            let mut engine = Scheduler::new(
                root.clone(),
                PathBuf::from("D:/CC/Tools/todo/todo.json"),
                RealBackend::new(root),
            )?;
            cc_scheduler::registration_resume::run(&mut engine, id, &request, expected)
        })();
        match result {
            Ok(report) => println!("{}", encode(&report, 2)),
            Err(error) => {
                eprintln!("registration resume failed {}", error.0);
                std::process::exit(1);
            }
        }
        return;
    }
    if command == Some("dispatch-subwork") {
        if args.len() == 2 && matches!(args[1].as_str(), "-h" | "--help") {
            println!(
                "usage: scheduler dispatch-subwork --id <ID>\n\nTry one existing finite subwork through normal priority, capacity, resource and ownership gates. Preserve source, history and waits; no new registration.\n\noptions:\n  -h, --help  show this help message and exit"
            );
            return;
        }
        let result = (|| -> Result<serde_json::Value> {
            if args.len() != 3 || args[1] != "--id" {
                return Err(Error("InputRejected"));
            }
            let id = args[2].parse().map_err(|_| Error("InputRejected"))?;
            let root = PathBuf::from("D:/CC/Tools/scheduler");
            let mut engine = Scheduler::new(
                root.clone(),
                PathBuf::from("D:/CC/Tools/todo/todo.json"),
                RealBackend::new(root),
            )?;
            engine.dispatch_existing_subwork(id)
        })();
        match result {
            Ok(report) => println!("{}", encode(&report, 2)),
            Err(error) => {
                eprintln!("subwork dispatch failed {}", error.0);
                std::process::exit(1);
            }
        }
        return;
    }
    if command == Some("dispatch-one") {
        if args.len() == 2 && matches!(args[1].as_str(), "-h" | "--help") {
            println!(
                "usage: scheduler dispatch-one --id <ID> [--wait]\n\nAllocate only this existing ToDo through ordinary priority, ownership and capacity gates. Keeps global settings, stops, dependencies and the saved launch scope. Does not run a global tick. --wait waits up to30 seconds for the current scheduler operation.\n"
            );
            return;
        }
        let result = (|| -> Result<serde_json::Value> {
            let wait = args.len() == 4 && args[3] == "--wait";
            let id = cc_scheduler::targeted_dispatch::parse_id(if wait {
                &args[1..3]
            } else {
                &args[1..]
            })?;
            let root = PathBuf::from("D:/CC/Tools/scheduler");
            let mut engine = Scheduler::new(
                root.clone(),
                PathBuf::from("D:/CC/Tools/todo/todo.json"),
                RealBackend::new(root),
            )?;
            if wait {
                cc_scheduler::targeted_dispatch::run_waiting(&mut engine, id)
            } else {
                cc_scheduler::targeted_dispatch::run(&mut engine, id)
            }
        })();
        match result {
            Ok(report) => println!("{}", encode(&report, 2)),
            Err(error) => {
                eprintln!("targeted dispatch failed {}", error.0);
                std::process::exit(1);
            }
        }
        return;
    }
    if command == Some("dispatch-receipt") {
        if args.len() == 2 && matches!(args[1].as_str(), "-h" | "--help") {
            println!(
                "usage: scheduler dispatch-receipt --id <ID> [--apply --expected-state-sha256 <SHA256>]\n\nInspect one ordinary repair launch without exposing source text. Apply only an exact verified receipt; never launch, cancel, release waits or change source files.\n\noptions:\n  -h, --help  show this help message and exit"
            );
            return;
        }
        let result = (|| -> Result<serde_json::Value> {
            let expected = if args.len() == 6
                && args[3] == "--apply"
                && args[4] == "--expected-state-sha256"
                && args[5].len() == 64
                && args[5].bytes().all(|b| b.is_ascii_hexdigit())
            {
                Some(args[5].as_str())
            } else if args.len() == 3 {
                None
            } else {
                return Err(Error("InputRejected"));
            };
            if args[1] != "--id" {
                return Err(Error("InputRejected"));
            }
            let id = args[2].parse().map_err(|_| Error("InputRejected"))?;
            let root = PathBuf::from("D:/CC/Tools/scheduler");
            let mut engine = Scheduler::new(
                root.clone(),
                PathBuf::from("D:/CC/Tools/todo/todo.json"),
                RealBackend::new(root),
            )?;
            cc_scheduler::dispatch_receipt::run(&mut engine, id, expected)
        })();
        match result {
            Ok(report) => println!("{}", encode(&report, 2)),
            Err(error) => {
                eprintln!("dispatch receipt failed {}", error.0);
                std::process::exit(1);
            }
        }
        return;
    }
    if command == Some("checkpoint-workspace") {
        if args.len() == 2 && matches!(args[1].as_str(), "-h" | "--help") {
            println!(
                "usage: scheduler checkpoint-workspace --request-file <JSON> [--check]\n\nExplicitly save one owned task's current branch and HEAD receipt. --check validates without saving and reports fixed reasons and comparisons only. Never launches or changes source files.\nJSON: id, job, expected_branch, branch, head\n\noptions:\n  -h, --help  show this help message and exit"
            );
            return;
        }
        let result = (|| -> Result<serde_json::Value> {
            let check_only = args.len() == 4 && args[3] == "--check";
            if !(args.len() == 3 || check_only) || args[1] != "--request-file" {
                return Err(Error("InputRejected"));
            }
            use std::io::Read;
            let mut bytes = Vec::new();
            std::fs::File::open(&args[2])?
                .take(4097)
                .read_to_end(&mut bytes)?;
            if bytes.len() > 4096 {
                return Err(Error("InputRejected"));
            }
            let request = serde_json::from_slice(&bytes)?;
            let root = PathBuf::from("D:/CC/Tools/scheduler");
            let mut engine = Scheduler::new(
                root.clone(),
                PathBuf::from("D:/CC/Tools/todo/todo.json"),
                RealBackend::new(root),
            )?;
            if check_only {
                cc_scheduler::workspace_checkpoint::inspect(
                    &mut engine,
                    &mut SystemRunner,
                    &request,
                )
            } else {
                cc_scheduler::workspace_checkpoint::checkpoint(
                    &mut engine,
                    &mut SystemRunner,
                    &request,
                )
            }
        })();
        match result {
            Ok(report) => println!("{}", encode(&report, 2)),
            Err(error) => {
                eprintln!("workspace checkpoint failed {}", error.0);
                std::process::exit(1);
            }
        }
        return;
    }
    if (matches!(
        command,
        Some("status" | "step" | "repair-step" | "recover" | "orphans")
    ) || matches!(command, Some("-h" | "--help")))
        && args.iter().any(|x| matches!(x.as_str(), "-h" | "--help"))
    {
        match command {
            Some("orphans") => println!(
                "usage: scheduler orphans [-h] [--dry-run | --apply]\n\noptions:\n  -h, --help  show this help message and exit\n  --dry-run\n  --apply     also requires config.json kill_enabled=true"
            ),
            Some(c @ ("status" | "step" | "repair-step" | "recover")) => println!(
                "usage: scheduler {c} [-h]\n\noptions:\n  -h, --help  show this help message and exit"
            ),
            _ => println!(
                "usage: scheduler [-h] {{status,step,recover,orphans}} ...\n\nWindows capacity-aware ToDo dispatcher.\n\nstatus and orphans --dry-run only measure; step is also called by the hidden Discord tick.\nGeneral process termination requires config.json kill_enabled. Verified no-action recovery is checked separately in step/repair-step.\nOnly event type/id/status metadata is used; command lines, payloads and credentials are not retained.\n\npositional arguments:\n  {{status,step,recover,orphans}}\n    recover  reconcile dead jobs before Discord intake; never launch or kill\n\noptions:\n  -h, --help  show this help message and exit"
            ),
        }
        return;
    }
    let valid = match args.first().map(String::as_str) {
        Some("status" | "step" | "repair-step" | "recover") => args.len() == 1,
        Some("orphans") => {
            args.len() == 1
                || args.len() == 2 && matches!(args[1].as_str(), "--dry-run" | "--apply")
        }
        _ => false,
    };
    if !valid {
        eprintln!("usage: scheduler [-h] {{status,step,recover,orphans}} ...");
        std::process::exit(if args == ["-h"] || args == ["--help"] {
            0
        } else {
            2
        });
    }
    let root = PathBuf::from("D:/CC/Tools/scheduler");
    let engine = Scheduler::new(
        root.clone(),
        PathBuf::from("D:/CC/Tools/todo/todo.json"),
        RealBackend::new(root),
    );
    let report = if args[0] == "step" {
        step(engine)
    } else {
        match engine.and_then(|mut e| e.run(&args[0], args.iter().any(|a| a == "--apply"))) {
            Ok(v) => v,
            Err(e) => {
                eprintln!("measurement failed {}", e.0);
                std::process::exit(1);
            }
        }
    };
    println!("{}", encode(&report, 2));
    std::process::exit(i32::from(report.get("error").is_some()));
}
