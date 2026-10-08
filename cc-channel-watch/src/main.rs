use cc_channel_watch::*;
use fs2::FileExt;
use serde_json::{Value, json};
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

fn command(exe: &Path) -> Command {
    #[cfg(windows)]
    let mut command = Command::new(exe);
    #[cfg(not(windows))]
    let command = Command::new(exe);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000); // CREATE_NO_WINDOW, including children of VBS.
    }
    command
}

// File-backed pipes avoid deadlocks on large channel listings and allow bounded waits.
fn execute(
    exe: &Path,
    args: &[String],
    root: &Path,
    seconds: u64,
) -> Result<(bool, Vec<u8>, String)> {
    let out = root.join("command.stdout");
    let err = root.join("command.stderr");
    let mut child = command(exe)
        .args(args)
        .stdin(Stdio::null())
        .stdout(fs::File::create(&out)?)
        .stderr(fs::File::create(&err)?)
        .spawn()?;
    let start = Instant::now();
    let status = loop {
        if let Some(status) = child.try_wait()? {
            break status;
        }
        if start.elapsed() > Duration::from_secs(seconds) {
            child.kill()?;
            child.wait()?;
            return Err(
                format!("{} が{}秒以内に終了しませんでした", exe.display(), seconds).into(),
            );
        }
        std::thread::sleep(Duration::from_millis(100));
    };
    Ok((status.success(), fs::read(out)?, fs::read_to_string(err)?))
}

struct RealTodo<'a> {
    config: &'a Config,
    root: &'a Path,
}
impl Todo for RealTodo<'_> {
    fn find(&mut self, marker: &str, project: &str) -> Result<Option<i64>> {
        let db: Value = serde_json::from_slice(&fs::read(&self.config.todo_store)?)?;
        let items = db["items"].as_array().ok_or("共有ToDoのitemsが不正です")?;
        Ok(items
            .iter()
            .find(|i| i["project"].as_str() == Some(project) && i["note"].as_str() == Some(marker))
            .and_then(|i| i["id"].as_i64()))
    }
    fn add(&mut self, channel: &Channel, batch: &Batch) -> Result<i64> {
        let args = vec![
            "add".into(),
            channel.project.clone(),
            batch.title.clone(),
            "--spec".into(),
            batch.spec.display().to_string(),
            "--effort".into(),
            "high".into(),
            "--note".into(),
            batch.marker.clone(),
        ];
        let (ok, stdout, stderr) = execute(&self.config.todo, &args, self.root, 30)?;
        if !ok {
            return Err(format!("ToDo登録に失敗: {}", stderr.trim()).into());
        }
        let out = String::from_utf8(stdout)?;
        out.trim()
            .strip_prefix("added ")
            .ok_or("ToDo登録の応答が不正です")?
            .parse()
            .map_err(Into::into)
    }
}

fn report(config: &Config, root: &Path, channel: &Channel, body: Option<&str>) -> Result<()> {
    queue_report(
        &root
            .join("ledgers")
            .join(format!("{}-reports.json", channel.key)),
        body,
        &chrono::Utc::now()
            .with_timezone(&chrono::FixedOffset::east_opt(9 * 3600).unwrap())
            .format("%Y-%m-%d")
            .to_string(),
        |text| {
            let args = ["log", "--topic", &channel.name, "--text", text].map(String::from);
            let (ok, _, _) = execute(&config.discord_ask, &args, root, 90)?;
            if ok {
                Ok(())
            } else {
                Err("Discordへ活動の報告を送れませんでした。次回に再送します".into())
            }
        },
    )
}
fn run(root: &Path, config_path: &Path, seed_only: bool) -> Result<Value> {
    let config: Config = serde_json::from_slice(&fs::read(config_path)?)?;
    validate(&config)?;
    fs::create_dir_all(root.join("ledgers"))?;
    fs::create_dir_all(root.join("evidence"))?;
    let mut todo = RealTodo {
        config: &config,
        root,
    };
    let mut results = Vec::new();
    let mut errors = 0;
    for channel in &config.channels {
        let channel_result = (|| -> Result<Value> {
            let path = root.join("ledgers").join(format!("{}.json", channel.key));
            let mut ledger = load_ledger(&path, channel)?;
            if seed_only {
                return Ok(
                    json!({"channel":channel.key,"seeded_seen":ledger.seen.len(),"skipped":ledger.skipped.len()}),
                );
            }
            let args = [
                "--ignore-config",
                "--flat-playlist",
                "--dump-single-json",
                "--skip-download",
                "--no-warnings",
                "--socket-timeout",
                "20",
                "--retries",
                "1",
                "--extractor-retries",
                "1",
            ]
            .into_iter()
            .map(String::from)
            .chain(std::iter::once(channel.url.clone()))
            .collect::<Vec<_>>();
            let (ok, bytes, stderr) = execute(&config.yt_dlp, &args, root, 240)?;
            // Never commit a partial listing returned with failure.
            if !ok {
                return Err(format!(
                    "一覧取得に失敗: {}",
                    stderr.trim().chars().take(350).collect::<String>()
                )
                .into());
            }
            let value: Value = serde_json::from_slice(&bytes)?;
            let listed = videos(&value);
            if !value["entries"].is_array() {
                return Err("一覧JSONにentries配列がありません".into());
            }
            save_json(
                &root
                    .join("evidence")
                    .join(format!("{}-latest.json", channel.key)),
                &value,
            )?;
            if let Some(body) = metrics_report(channel, &value, &now())
                && let Err(error) = report(&config, root, channel, Some(&body))
            {
                errors += 1;
                results.push(json!({"channel":channel.key,"report_error":error.to_string()}));
            }
            let outcome = process_channel(
                channel,
                &path,
                &root.join("specs"),
                &mut ledger,
                &listed,
                &mut todo,
            )?;
            Ok(json!({"channel":channel.key,"outcome":outcome}))
        })();
        match channel_result {
            Ok(result) => results.push(result),
            Err(error) => {
                errors += 1;
                results.push(json!({"channel":channel.key,"error":error.to_string()}));
            }
        }
    }
    Ok(
        json!({"status":if errors == 0 {"ok"} else {"error"},"mode":if seed_only {"seed"} else {"run"},"channels":results}),
    )
}

fn main() {
    let args: Vec<_> = std::env::args().skip(1).collect();
    let mut seed_only = false;
    let mut config = PathBuf::from("D:/CC/Tools/channel-watch/channels.json");
    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "run" => {}
            "seed" => seed_only = true,
            "--config" if i + 1 < args.len() => {
                i += 1;
                config = PathBuf::from(&args[i]);
            }
            "--help" | "-h" => {
                println!("channel-watch [run|seed] [--config PATH]");
                return;
            }
            other => {
                eprintln!("不明な引数: {other}");
                std::process::exit(2);
            }
        }
        i += 1;
    }
    let root = config.parent().unwrap_or(Path::new("."));
    if let Err(error) = fs::create_dir_all(root) {
        eprintln!("{error}");
        std::process::exit(1);
    }
    let started = now();
    let start = Instant::now();
    // OS lock releases even if the watcher crashes; don't create stale PID lockfiles.
    let lock = match OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(root.join("run.lock"))
    {
        Ok(file) => file,
        Err(error) => {
            eprintln!("{error}");
            std::process::exit(1);
        }
    };
    let mut report = match lock.try_lock_exclusive() {
        Ok(()) => run(root, &config, seed_only)
            .unwrap_or_else(|error| json!({"status":"error","error":error.to_string()})),
        Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
            json!({"status":"already_running"})
        }
        Err(error) => json!({"status":"error","error":error.to_string()}),
    };
    report["started_at"] = json!(started);
    report["duration_ms"] = json!(start.elapsed().as_millis());
    let line = serde_json::to_string(&report).expect("serializable report");
    let write = (|| -> std::io::Result<()> {
        let mut log = OpenOptions::new()
            .append(true)
            .create(true)
            .open(root.join("runs.jsonl"))?;
        writeln!(log, "{line}")?;
        log.sync_all()
    })();
    println!("{line}");
    if write.is_err() || report["status"] == "error" {
        std::process::exit(1);
    }
}
