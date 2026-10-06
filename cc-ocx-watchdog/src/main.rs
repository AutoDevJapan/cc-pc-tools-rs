#![cfg_attr(all(windows, not(debug_assertions)), windows_subsystem = "windows")]

#[cfg(windows)]
mod runtime {
    use cc_ocx_watchdog::windows::{MutexGuard, Process, discover};
    use cc_ocx_watchdog::{
        Config, FailureStreak, Identity, hidden_command, probe, run_bounded, unresponsive,
    };
    use serde::{Deserialize, Serialize};
    use serde_json::json;
    use std::fs::{self, OpenOptions};
    use std::io::Write;
    use std::path::{Path, PathBuf};
    use std::time::{Duration, Instant};

    const WATCH_TASK: &str = "CC-OcxHealthWatchdog";
    fn system_exe(name: &str) -> PathBuf {
        PathBuf::from(std::env::var_os("SystemRoot").unwrap_or_else(|| "C:\\Windows".into()))
            .join("System32")
            .join(name)
    }
    fn default_config() -> Result<Config, String> {
        let user = PathBuf::from(std::env::var_os("USERPROFILE").ok_or("USERPROFILE missing")?);
        let appdata = PathBuf::from(std::env::var_os("APPDATA").ok_or("APPDATA missing")?);
        Ok(Config {
            home: user.join(".opencodex"),
            expected_exe: appdata
                .join("npm/node_modules/@bitkyc08/opencodex/node_modules/bun/bin/bun.exe"),
            port: 10100,
            task: "opencodex-proxy".into(),
            log_dir: PathBuf::from("D:/CC/Tools/ocx-health-watchdog"),
            discord_exe: Some(PathBuf::from("D:/CC/Tools/discord-ask/discord_ask.exe")),
            timeout_ms: 20_000,
            interval_ms: 10_000,
            startup_grace_ms: 120_000,
            cooldown_ms: 120_000,
        })
    }
    fn log(config: &Config, event: serde_json::Value) -> Result<(), String> {
        let path = config.log_dir.join("events.jsonl");
        if fs::metadata(&path).is_ok_and(|m| m.len() > 5 * 1024 * 1024) {
            let previous = path.with_extension("jsonl.1");
            if previous.exists() {
                fs::remove_file(&previous).map_err(|e| format!("rotate: {e}"))?;
            }
            fs::rename(&path, previous).map_err(|e| format!("rotate: {e}"))?;
        }
        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(path)
            .map_err(|e| e.to_string())?;
        let record = json!({"at":chrono::Local::now().to_rfc3339(), "event":event});
        writeln!(file, "{record}").map_err(|e| e.to_string())?;
        file.sync_data().map_err(|e| e.to_string())
    }
    fn status(config: &Config, value: serde_json::Value) -> Result<(), String> {
        atomic_json(&config.log_dir.join("status.json"), &value)
    }
    fn atomic_json(path: &Path, value: &impl Serialize) -> Result<(), String> {
        let bytes = serde_json::to_vec_pretty(value).map_err(|e| e.to_string())?;
        let temp = path.with_extension("tmp");
        let mut file = fs::File::create(&temp).map_err(|e| e.to_string())?;
        file.write_all(&bytes).map_err(|e| e.to_string())?;
        file.sync_all().map_err(|e| e.to_string())?;
        drop(file);
        use windows_sys::Win32::Storage::FileSystem::{
            MOVEFILE_REPLACE_EXISTING, MOVEFILE_WRITE_THROUGH, MoveFileExW,
        };
        if unsafe {
            MoveFileExW(
                cc_ocx_watchdog::windows::wide(&temp).as_ptr(),
                cc_ocx_watchdog::windows::wide(path).as_ptr(),
                MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH,
            )
        } == 0
        {
            return Err(format!(
                "publish_journal: {}",
                std::io::Error::last_os_error()
            ));
        }
        Ok(())
    }
    #[derive(Deserialize, Serialize)]
    struct Pending {
        pid: u32,
        born: u64,
        at: String,
        recovered_pid: Option<u32>,
        terminated: bool,
    }
    #[derive(Default, Deserialize, Serialize)]
    struct Journal {
        events: Vec<Pending>,
        older_recoveries: u64,
    }
    fn save_pending(config: &Config, pending: &Journal) -> Result<(), String> {
        atomic_json(&config.log_dir.join("restart-pending.json"), pending)
    }
    fn notify(config: &Config, journal: &Journal) -> Result<(), String> {
        let Some(executable) = &config.discord_exe else {
            return Ok(());
        };
        let confirmed: Vec<_> = journal
            .events
            .iter()
            .filter(|p| p.recovered_pid.is_some())
            .collect();
        let mut text = format!(
            "【opencodex】応答が途絶えたプロキシの復旧を{}件確認しました。20秒の検査が3回続けて失敗したため、対象プロセスの終了とサービスの再起動を試みました。\n",
            confirmed.len() as u64 + journal.older_recoveries
        );
        for event in confirmed.iter().rev().take(6).rev() {
            text.push_str(&format!(
                "{}: {} → {}。終了確認: {}。\n",
                event.at,
                event.pid,
                event.recovered_pid.unwrap(),
                if event.terminated {
                    "済み"
                } else {
                    "記録前に監視が中断"
                }
            ));
        }
        text.push_str("詳細は監視の記録に保存しています。通知とメンションは付けていません。");
        let mut command = hidden_command(executable);
        command.args(["log", "--topic", "後片付けの記録", "--text", &text]);
        run_bounded(command, Duration::from_secs(30))
    }
    fn start_service(config: &Config) -> Result<(), String> {
        let mut command = hidden_command(system_exe("schtasks.exe"));
        command.args(["/Run", "/TN", &config.task]);
        run_bounded(command, Duration::from_secs(15))
    }
    fn eligible(config: &Config, process: &Process, last_kill: Option<Instant>) -> bool {
        process.age_ms() >= config.startup_grace_ms
            && last_kill.is_none_or(|at| at.elapsed() >= Duration::from_millis(config.cooldown_ms))
    }
    fn watch(config: &Config, once: bool) -> Result<(), String> {
        fs::create_dir_all(&config.log_dir).map_err(|e| e.to_string())?;
        let _guard = MutexGuard::acquire()?;
        log(
            config,
            json!({"kind":"started","watchdog_pid":std::process::id(),"port":config.port}),
        )?;
        let pending_path = config.log_dir.join("restart-pending.json");
        let mut pending: Journal = match fs::read(&pending_path) {
            Ok(bytes) => {
                serde_json::from_slice(&bytes).map_err(|e| format!("restart_journal: {e}"))?
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Journal::default(),
            Err(e) => return Err(format!("restart_journal: {e}")),
        };
        let mut current: Option<Process> = None;
        let mut streak = FailureStreak::default();
        let mut last_kill = None;
        let mut last_sample = Instant::now().checked_sub(Duration::from_secs(60)).unwrap();
        let mut notify_after = Instant::now();
        let mut service_after = Instant::now();
        loop {
            let observed = discover(config);
            let mut discovery_error = None;
            match observed {
                Ok(process) => {
                    if process.as_ref().map(|p| p.identity) != current.as_ref().map(|p| p.identity)
                    {
                        current = process;
                    }
                }
                Err(error) => {
                    current = None;
                    discovery_error = Some(error);
                }
            }
            let started = Instant::now();
            let outcome = match current.as_ref() {
                Some(process) => probe(
                    config.port,
                    Duration::from_millis(config.timeout_ms),
                    process.identity.pid,
                ),
                None => Err(discovery_error.unwrap_or_else(|| "no_listener".into())),
            };
            let identity = current.as_ref().map(|p| p.identity);
            let healthy = outcome.is_ok();
            let can_kill = current
                .as_ref()
                .is_some_and(|p| eligible(config, p, last_kill));
            let timed_out = unresponsive(&outcome)
                && started.elapsed() >= Duration::from_millis(config.timeout_ms);
            let kill = streak.observe(identity, !timed_out, can_kill);
            let state = json!({"at":chrono::Local::now().to_rfc3339(),"watchdog_pid":std::process::id(),"identity":identity,
                "healthy":healthy,"failures":streak.failures(),"eligible":can_kill,"probe_ms":started.elapsed().as_millis(),
                "error":outcome.as_ref().err(),"metrics":current.as_ref().and_then(|p| p.metrics())});
            status(config, state.clone())?;
            if !healthy || last_sample.elapsed() >= Duration::from_secs(60) {
                log(config, json!({"kind":"sample","status":state}))?;
                last_sample = Instant::now();
            }
            if healthy {
                let mut changed = false;
                for event in &mut pending.events {
                    if identity
                        != Some(Identity {
                            pid: event.pid,
                            born: event.born,
                        })
                        && event.recovered_pid.is_none()
                    {
                        event.recovered_pid = identity.map(|i| i.pid);
                        changed = true;
                        log(
                            config,
                            json!({"kind":"restart_confirmed","old_pid":event.pid,"new_identity":identity}),
                        )?;
                    }
                }
                if changed {
                    save_pending(config, &pending)?;
                }
                if pending.events.iter().any(|p| p.recovered_pid.is_some())
                    && Instant::now() >= notify_after
                {
                    match notify(config, &pending) {
                        Ok(()) => {
                            pending.events.retain(|p| p.recovered_pid.is_none());
                            pending.older_recoveries = 0;
                            save_pending(config, &pending)?;
                        }
                        Err(error) => {
                            log(config, json!({"kind":"report_failed","error":error}))?;
                        }
                    }
                    notify_after = Instant::now() + Duration::from_secs(600);
                }
            }
            // Retry a service-start failure only for a restart this watchdog requested.
            // An intentionally absent service, with no outstanding restart, remains stopped.
            if current.is_none()
                && pending.events.iter().any(|p| p.recovered_pid.is_none())
                && Instant::now() >= service_after
            {
                let restart = start_service(config);
                log(
                    config,
                    json!({"kind":"service_requested","error":restart.err()}),
                )?;
                service_after = Instant::now() + Duration::from_secs(60);
            }
            if kill {
                let process = current.as_ref().unwrap();
                // Persist the intent before attempting termination. A storage failure must never cause an unrecorded restart.
                log(
                    config,
                    json!({"kind":"termination_requested","identity":process.identity,"failures":3}),
                )?;
                // Store recoverable intent before the kill, so a watchdog restart can finish it.
                pending.events.push(Pending {
                    pid: process.identity.pid,
                    born: process.identity.born,
                    at: chrono::Local::now().to_rfc3339(),
                    recovered_pid: None,
                    terminated: false,
                });
                while pending.events.len() > 100 {
                    let Some(index) = pending
                        .events
                        .iter()
                        .position(|e| e.recovered_pid.is_some())
                    else {
                        break;
                    };
                    pending.events.remove(index);
                    pending.older_recoveries += 1;
                }
                save_pending(config, &pending)?;
                match process.terminate_if_still_owner(config) {
                    Ok(()) => {
                        last_kill = Some(Instant::now());
                        pending.events.last_mut().unwrap().terminated = true;
                        save_pending(config, &pending)?;
                        log(
                            config,
                            json!({"kind":"terminated","identity":process.identity}),
                        )?;
                        // Usually the existing wrapper respawns. A repaired/manual proxy can be orphaned;
                        // running the service task also covers that case (Task Scheduler ignores duplicate runs).
                        let restart = start_service(config);
                        service_after = Instant::now() + Duration::from_secs(60);
                        log(
                            config,
                            json!({"kind":"service_requested","error":restart.err()}),
                        )?;
                        current = None;
                    }
                    Err(error) => {
                        pending.events.pop();
                        save_pending(config, &pending)?;
                        log(config, json!({"kind":"termination_refused","error":error}))?;
                    }
                }
                streak.reset();
            }
            if once {
                return outcome;
            }
            std::thread::sleep(Duration::from_millis(config.interval_ms));
        }
    }
    fn xml_escape(value: &str) -> String {
        value
            .replace('&', "&amp;")
            .replace('<', "&lt;")
            .replace('>', "&gt;")
            .replace('"', "&quot;")
    }
    fn install(config: &Config, config_file: Option<&Path>) -> Result<(), String> {
        fs::create_dir_all(&config.log_dir).map_err(|e| e.to_string())?;
        if config_file.is_some() {
            return Err("installation uses the production configuration".into());
        }
        let exe = std::env::current_exe().map_err(|e| e.to_string())?;
        let xml = format!(
            r#"<?xml version="1.0" encoding="UTF-8"?><Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers><Principals><Principal id="Owner"><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure></Settings><Actions Context="Owner"><Exec><Command>{}</Command><Arguments>watch</Arguments><WorkingDirectory>{}</WorkingDirectory></Exec></Actions></Task>"#,
            xml_escape(&exe.to_string_lossy()),
            xml_escape(&config.log_dir.to_string_lossy())
        );
        let path = config.log_dir.join("task.xml");
        fs::write(&path, xml).map_err(|e| e.to_string())?;
        let mut command = hidden_command(system_exe("schtasks.exe"));
        command
            .args(["/Create", "/TN", WATCH_TASK, "/XML"])
            .arg(&path)
            .arg("/F");
        run_bounded(command, Duration::from_secs(15))?;
        let mut command = hidden_command(system_exe("schtasks.exe"));
        command.args(["/Run", "/TN", WATCH_TASK]);
        run_bounded(command, Duration::from_secs(15))?;
        log(config, json!({"kind":"installed","task":WATCH_TASK}))
    }
    pub fn run() -> Result<(), String> {
        let mut args = std::env::args().skip(1);
        let mode = args.next().unwrap_or_else(|| "check".into());
        let config_file = match args.next().as_deref() {
            Some("--config") => Some(PathBuf::from(args.next().ok_or("missing config path")?)),
            None => None,
            _ => return Err("usage: watch|check|install [--config <absolute path>]".into()),
        };
        if args.next().is_some() {
            return Err("extra arguments".into());
        }
        let config: Config = if let Some(path) = &config_file {
            serde_json::from_slice(&fs::read(path).map_err(|e| e.to_string())?)
                .map_err(|e| e.to_string())?
        } else {
            default_config()?
        };
        config.validate()?;
        match mode.as_str() {
            "watch" => watch(&config, false),
            "check" => watch(&config, true),
            "install" => install(&config, config_file.as_deref()),
            _ => Err("unknown command".into()),
        }
    }
}

fn main() -> std::process::ExitCode {
    #[cfg(windows)]
    let outcome = runtime::run();
    #[cfg(not(windows))]
    let outcome: Result<(), String> = Err("Windows is required".into());
    match outcome {
        Ok(()) => std::process::ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("ocx watchdog: {error}");
            std::process::ExitCode::FAILURE
        }
    }
}
