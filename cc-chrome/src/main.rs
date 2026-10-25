use std::{io, path::PathBuf};
fn main() {
    if let Err(e) = run() {
        eprintln!("owner_chrome: {e}");
        std::process::exit(1);
    }
}
fn run() -> io::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let jobs = PathBuf::from("D:/CC/Tools/codex-jobs/jobs");
    match args.first().map(String::as_str) {
        Some("guard") => {
            let event: serde_json::Value = serde_json::from_reader(std::io::stdin().lock())?;
            if let Some(decision) = cc_chrome::guard::decision(&event) {
                println!("{decision}");
            }
        }
        Some("install-claude-hook") => println!(
            "updated={}",
            cc_chrome::guard::install(&PathBuf::from(
                std::env::var_os("USERPROFILE")
                    .ok_or_else(|| io::Error::other("USERPROFILE missing"))?
            ))?
        ),
        #[cfg(windows)]
        Some("inventory") => println!("{}", cc_chrome::native::inventory()?),
        Some("install-plugin-hook") => println!(
            "updated={}",
            cc_chrome::install_plugin_hook(&PathBuf::from(
                std::env::var_os("USERPROFILE")
                    .ok_or_else(|| io::Error::other("USERPROFILE missing"))?
            ))?
        ),
        Some("sweep") => println!(
            "{:?}",
            cc_chrome::sweep(&jobs, None, args.iter().any(|a| a == "--dry-run"))?
        ),
        Some("finish") if args.len() == 2 && cc_chrome::safe_job(&args[1]) => {
            println!("{:?}", cc_chrome::sweep(&jobs, Some(&args[1]), false)?)
        }
        Some(command @ ("close" | "keep")) if args.len() == 4 => {
            let report = cc_chrome::completion::complete(
                &jobs,
                &args[1],
                &args[2],
                command == "close",
                &args[3],
            )?;
            println!("{report:?}");
            if report.failed > 0 || report.deferred {
                return Err(io::Error::other(
                    "tab completion pending; existing sweep will retry",
                ));
            }
        }
        #[cfg(windows)]
        Some("open") if args.len() == 3 && cc_chrome::safe_job(&args[1]) => {
            let dir = jobs.join(&args[1]);
            let job: serde_json::Value =
                serde_json::from_slice(&std::fs::read(dir.join("job.json"))?)?;
            if !["running", "queued"].contains(&job["status"].as_str().unwrap_or("")) {
                return Err(io::Error::other("open requires an active owner job"));
            }
            println!(
                "{}",
                serde_json::to_string(&cc_chrome::native::open_with_receipt(&dir, &args[1], &args[2])?)?
            );
        }
        _ => {
            return Err(io::Error::other(
                "usage: owner_chrome open <job-id> <url> | close <job-id> <record-uuid> <reason> | keep <job-id> <record-uuid> <owner-instruction> | finish <job-id> | inventory | sweep [--dry-run]",
            ));
        }
    }
    Ok(())
}
