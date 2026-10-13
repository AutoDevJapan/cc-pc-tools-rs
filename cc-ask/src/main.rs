//! CLI for Discord owner questions, request intake and periodic maintenance.
use cc_ask::{commands::*, core::*, intake, resume};
use clap::{Parser, Subcommand};
use std::path::PathBuf;

#[derive(Parser)]
#[command(
    name = "discord_ask",
    about = "Ask the owner a question on Discord and read the owner's replies."
)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    Check,
    /// Audit full archived histories with an ID-only checkpoint; never posts or launches.
    AuditHistory(cc_ask::history_audit::Args),
    /// Send a new explicitly requested message using independent append-only receipts.
    RecoveryLog(cc_ask::recovery_messages::LogArgs),
    /// Create one new owner decision and its separate silent progress post.
    RecoveryAsk(cc_ask::recovery_messages::AskArgs),
    /// Repair one exact unsent recovery ask to a previously verified progress post.
    RecoveryRepairOrigin(cc_ask::recovery_messages::RepairOriginArgs),
    /// Read only an exact new recovery question; never consumes replies or starts work.
    RecoveryReplies(cc_ask::recovery_messages::RepliesArgs),
    /// Independent one-question followup; never runs legacy tick or lost questions.
    RecoveryFollowup {
        #[command(subcommand)]
        command: cc_ask::recovery_followup::Command,
    },
    Ask(AskArgs),
    /// Bind explicitly reviewed duplicate threads to one scope; never sends or launches.
    ReconcileDecisions {
        #[arg(long)]
        manifest: PathBuf,
    },
    /// Inspect an exact decision and its owner evidence without creating a question.
    LookupDecision {
        #[arg(long)]
        decision_file: PathBuf,
    },
    Replies {
        #[arg(long)]
        thread: Option<String>,
        /// Inspect a trusted thread without acknowledging, posting, or changing state.
        #[arg(long, requires = "thread")]
        read_only: bool,
    },
    /// Suppress one question's reminders until an RFC3339 timestamp; sends no messages.
    Snooze {
        #[arg(long)]
        thread: String,
        #[arg(long)]
        until: String,
    },
    Remind(cc_ask::reminders::RemindArgs),
    /// Inspect or register concrete questions already posted in trusted Discord threads.
    RecoverQuestions(cc_ask::questions::RecoverArgs),
    Sweep,
    /// Preview job title changes; --apply PATCHes only registered open job cards.
    RenameJobThreads {
        #[arg(long)]
        apply: bool,
        #[arg(long, default_value_t = 20)]
        limit: usize,
    },
    /// Preserve a registered job's title and small ID note after notifier updates.
    SyncJobCard {
        #[arg(long)]
        job: String,
    },
    Tick,
    /// Update the silent work-status thread in the server's general text channel.
    Board,
    /// Inspect stale Codex helpers without terminating them when --dry-run is set.
    ReapHelpers {
        #[arg(long)]
        dry_run: bool,
    },
    Intake,
    Resume,
    /// Reopen one failed reply delivery for the next tick, including an expired retry.
    RetryReply {
        #[arg(long)]
        thread: String,
    },
    /// Retry one missed owner reply; confirmed deliveries never start another job.
    DeliverReply {
        #[arg(long)]
        thread: String,
        #[arg(long)]
        message: String,
        #[arg(long)]
        job: Option<String>,
        #[arg(long, conflicts_with = "job")]
        handled_by: Option<String>,
    },
    /// Retired: stopped work now goes through the ToDo watchdog and console delegation.
    AutomationStart,
    /// Inspect recent stopped sessions with --dry-run; launching is retired.
    Patrol {
        #[arg(long)]
        dry_run: bool,
        #[arg(long)]
        sessions: Option<PathBuf>,
    },
    Routing,
    /// Post to one thread per topic, silently unless --notify is supplied.
    Log {
        #[arg(long = "attach")]
        attach: Vec<PathBuf>,
        #[arg(long)]
        topic: String,
        #[arg(long, default_value = "")]
        text: String,
        #[arg(long)]
        body_file: Option<PathBuf>,
        #[arg(long)]
        notify: bool,
    },
    Inbox {
        #[arg(long)]
        state_file: PathBuf,
    },
    /// Post to an existing thread, silently unless --notify is supplied.
    Post {
        #[arg(long)]
        thread: String,
        #[arg(long)]
        body_file: PathBuf,
        #[arg(long = "attach")]
        attach: Vec<PathBuf>,
        #[arg(long)]
        notify: bool,
    },
    Close {
        thread: String,
    },
    /// Check owner-facing Japanese (Skill japanese-report-writing) without posting.
    Lint {
        #[arg(long, default_value = "")]
        text: String,
        #[arg(long)]
        body_file: Option<PathBuf>,
    },
}

/// Owner posts get the phrasing advice on stderr first; the post is never blocked.
fn advise(ctx: &mut Context, text: &str, body_file: Option<&std::path::Path>) {
    let body = body_file
        .and_then(|p| std::fs::read_to_string(p).ok())
        .unwrap_or_default();
    cc_ask::ja_check::advise(ctx.effects.as_mut(), &format!("{text}\n{body}"));
}

fn env_path(key: &str, default: &str) -> PathBuf {
    std::env::var_os(key)
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from(default))
}

fn context(tick: bool) -> Result<Context> {
    let here = env_path("CC_ASK_HOME", "D:/CC/Tools/discord-ask");
    let config = match std::env::var_os("CC_ASK_CONFIG") {
        Some(path) => PathBuf::from(path),
        None => std::env::var_os("USERPROFILE")
            .or_else(|| std::env::var_os("HOME"))
            .map(PathBuf::from)
            .ok_or("home directory is not set")?
            .join(".godd/discord-notify.json"),
    };
    let cfg = match read_json(&config) {
        Ok(cfg) => cfg,
        Err(error) if tick => serde_json::json!({"__cc_ask_config_error":error}),
        Err(error) => return Err(error),
    };
    Ok(Context {
        here,
        client_projects: std::env::var_os("CC_ASK_CLIENT_PROJECTS")
            .map(PathBuf::from)
            .unwrap_or_else(|| PathBuf::from("D:/CC/Tools/client-projects.txt")),
        civ6_home: env_path("CC_CIV6_LIVE_HOME", "D:/CC/Tools/civ6-live"),
        jobs: env_path("CC_CODEX_JOBS_HOME", "D:/CC/Tools/codex-jobs"),
        discord_tasks: discord_tasks_dir()?,
        todo: env_path("CC_TODO_STATE", "D:/CC/Tools/todo/todo.json"),
        cfg,
        effects: Box::new(RealEffects),
        codex_command: env_path(
            "CC_CODEX_JOBS_COMMAND",
            "D:/CC/Tools/codex-jobs/codex_jobs.py",
        ),
        scheduler_command: env_path("CC_SCHEDULER_COMMAND", "D:/CC/Tools/scheduler/scheduler.py"),
        shepherd_command: env_path("CC_SHEPHERD_COMMAND", "D:/CC/Tools/shepherd/shepherd.py"),
        todo_command: env_path("CC_TODO_COMMAND", "D:/CC/Tools/todo/todo.py"),
    })
}

fn run(command: Commands) -> Result<()> {
    // Check the original full text before config, lint/model calls, locks or Discord.
    if let Commands::Ask(args) = &command {
        cc_ask::ask_gate::enforce(&args.title, &read_text(&args.body_file)?, args.owner_only)?;
    }
    let _sync_deadline = if matches!(&command, Commands::SyncJobCard { .. }) {
        Some(cc_ask::tick_runtime::Deadline::new(
            std::time::Duration::from_secs(10),
        ))
    } else {
        None
    };
    let _hard_limit = if matches!(&command, Commands::Board) {
        Some(cc_ask::tick_runtime::HardLimit::for_command(
            env_path("CC_ASK_HOME", "D:/CC/Tools/discord-ask").join("board.log"),
            cc_ask::board::LIMIT + std::time::Duration::from_secs(5),
            "board",
        )?)
    } else if matches!(&command, Commands::Tick) {
        Some(cc_ask::tick_runtime::HardLimit::new(
            env_path("CC_ASK_HOME", "D:/CC/Tools/discord-ask").join("tick.log"),
            cc_ask::tick_runtime::TICK_LIMIT + std::time::Duration::from_secs(5),
        )?)
    } else if matches!(
        &command,
        Commands::RecoveryFollowup {
            command: cc_ask::recovery_followup::Command::Tick
        }
    ) {
        Some(cc_ask::tick_runtime::HardLimit::for_command(
            env_path("CC_ASK_HOME", "D:/CC/Tools/discord-ask").join("recovery-followup.log"),
            std::time::Duration::from_secs(40),
            "recovery-followup",
        )?)
    } else if matches!(&command, Commands::SyncJobCard { .. }) {
        Some(cc_ask::tick_runtime::HardLimit::for_command(
            env_path("CC_ASK_HOME", "D:/CC/Tools/discord-ask").join("tick.log"),
            std::time::Duration::from_secs(15),
            "sync-job-card",
        )?)
    } else {
        None
    };
    if matches!(
        &command,
        Commands::AutomationStart | Commands::Routing | Commands::Patrol { dry_run: false, .. }
    ) {
        return cc_ask::automation::reject_legacy_launch();
    }
    if let Commands::ReapHelpers { dry_run } = command {
        let here = env_path("CC_ASK_HOME", "D:/CC/Tools/discord-ask");
        let _lock = ByteLock::acquire(&here.join("tick.lock"), std::time::Duration::ZERO)?;
        let jobs = env_path("CC_CODEX_JOBS_HOME", "D:/CC/Tools/codex-jobs");
        println!("{}", cc_ask::helper_reaper::run(&jobs, dry_run)?);
        if !dry_run {
            cc_ask::helper_reaper::flush_log(&mut context(false)?)?;
        }
        return Ok(());
    }
    if let Commands::Lint { text, body_file } = &command {
        let body = match body_file {
            Some(p) => std::fs::read_to_string(p).map_err(|e| format!("OSError: {e}"))?,
            None => String::new(),
        };
        let found = cc_ask::ja_check::check(&mut RealEffects, &format!("{text}\n{body}"));
        if found.is_empty() {
            println!("日本語の点検: 指摘なし");
        }
        for finding in found {
            println!("日本語の点検: {finding}");
        }
        return Ok(());
    }
    let mut ctx = context(matches!(&command, Commands::Tick))?;
    match &command {
        Commands::Ask(args) if !args.dry_run => advise(&mut ctx, "", Some(&args.body_file)),
        Commands::Log {
            text, body_file, ..
        } => advise(&mut ctx, text, body_file.as_deref()),
        Commands::Post { body_file, .. } => advise(&mut ctx, "", Some(body_file)),
        _ => {}
    }
    match command {
        Commands::RecoveryFollowup { command } => {
            let _deadline = cc_ask::tick_runtime::Deadline::new(std::time::Duration::from_secs(35));
            let _pool = cc_ask::ScopedHttpPool::new();
            use cc_ask::recovery_followup as followup;
            let value = match command {
                followup::Command::Register { config } => followup::register(&mut ctx, &config)?,
                followup::Command::Tick => followup::tick(&mut ctx)?,
                followup::Command::Inspect => followup::inspect(&ctx)?,
                followup::Command::Claim{message,handler} => followup::claim(&mut ctx,&message,&handler)?,
                followup::Command::Applied {
                    message,
                    result_file,
                } => followup::applied(&mut ctx, &message, &result_file)?,
            };
            println!("{value}");
        }
        Commands::RecoveryLog(args) => {
            println!("{}", cc_ask::recovery_messages::log(&mut ctx, &args)?)
        }
        Commands::RecoveryAsk(args) => {
            println!("{}", cc_ask::recovery_messages::ask(&mut ctx, &args)?)
        }
        Commands::RecoveryRepairOrigin(args) => {
            println!("{}", cc_ask::recovery_messages::repair_origin(&mut ctx, &args)?)
        }
        Commands::RecoveryReplies(args) => {
            println!("{}", cc_ask::recovery_messages::replies(&mut ctx, &args)?)
        }
        Commands::AuditHistory(args) => {
            println!("{}", cc_ask::history_audit::run(&mut ctx, &args)?)
        }
        Commands::Check => {
            cmd_check(&mut ctx)?;
        }
        Commands::Ask(args) => {
            cmd_ask(&mut ctx, &args)?;
        }
        Commands::ReconcileDecisions { manifest } => {
            println!(
                "{}",
                cc_ask::decision_identity::reconcile(&mut ctx, &read_json(&manifest)?)?
            );
        }
        Commands::LookupDecision { decision_file } => {
            println!(
                "{}",
                cc_ask::decision_identity::lookup(&mut ctx, &read_json(&decision_file)?)?
            );
        }
        Commands::Replies { thread, read_only } => {
            if read_only {
                cmd_replies_read_only(&mut ctx, thread.as_deref().ok_or("thread is required")?)?;
            } else {
                cmd_replies(&mut ctx, thread.as_deref())?;
            }
        }
        Commands::Snooze { thread, until } => cmd_snooze(&mut ctx, &thread, &until)?,
        Commands::Remind(args) => println!("{}", cc_ask::reminders::run(&mut ctx, &args)?),
        Commands::RecoverQuestions(args) => {
            println!("{}", cc_ask::questions::recover(&mut ctx, &args)?)
        }
        Commands::Sweep => cmd_sweep(&mut ctx)?,
        Commands::RenameJobThreads { apply, limit } => {
            println!(
                "{}",
                cc_ask::job_titles::rename_open(&mut ctx, apply, limit)?
            );
        }
        Commands::SyncJobCard { job } => {
            cc_ask::job_titles::sync_job(&mut ctx, &job)?;
            println!("{{\"ok\":true}}");
        }
        Commands::Tick => cmd_tick(&mut ctx)?,
        Commands::Board => println!("{}", cc_ask::board::update(&mut ctx)?),
        Commands::ReapHelpers { .. } => unreachable!(),
        Commands::Intake => intake::cmd_intake(&mut ctx, &mut false)?,
        Commands::Resume => resume::cmd_resume(&mut ctx, &mut false)?,
        Commands::RetryReply { thread } => {
            let result = cc_ask::reply_recovery::retry_reply(&mut ctx, &thread)?;
            println!("{result}");
        }
        Commands::DeliverReply {
            thread,
            message,
            job,
            handled_by,
        } => println!(
            "{}",
            if let Some(jid) = handled_by {
                intake::confirm_reply(&mut ctx, &thread, &message, &jid)?
            } else {
                intake::deliver_reply(&mut ctx, &thread, &message, job.as_deref())?
            }
        ),
        Commands::AutomationStart => cc_ask::automation::start(&ctx)?,
        Commands::Patrol { dry_run, sessions } => {
            let _tick = if dry_run {
                None
            } else {
                Some(ByteLock::acquire(
                    &ctx.here.join("tick.lock"),
                    std::time::Duration::ZERO,
                )?)
            };
            let mut budget = !dry_run && !cc_ask::automation::recover_before_launch(&mut ctx);
            let root = sessions.unwrap_or_else(cc_ask::patrol::sessions_root);
            let report = cc_ask::patrol::run(&mut ctx, &root, dry_run, &mut budget)?;
            println!("{report}");
        }
        Commands::Routing => {
            let _tick = ByteLock::acquire(&ctx.here.join("tick.lock"), std::time::Duration::ZERO)?;
            let mut budget = !cc_ask::automation::recover_before_launch(&mut ctx);
            println!("{}", cc_ask::routing::run(&mut ctx, &mut budget)?);
        }
        Commands::Log {
            attach,
            topic,
            text,
            body_file,
            notify,
        } => {
            cmd_log_attached(
                &mut ctx,
                &topic,
                &text,
                body_file.as_deref(),
                notify,
                &attach,
            )?;
        }
        Commands::Post {
            thread,
            body_file,
            attach,
            notify,
        } => {
            cmd_post(&mut ctx, &thread, &body_file, &attach, notify)?;
        }
        Commands::Inbox { state_file } => {
            cmd_inbox(&mut ctx, &state_file)?;
        }
        Commands::Close { thread } => cmd_close(&mut ctx, &thread)?,
        Commands::Lint { .. } => unreachable!(),
    }
    Ok(())
}

fn main() {
    let cli = Cli::parse();
    if let Err(error) = run(cli.command) {
        eprintln!("{}", safe_text(&error));
        std::process::exit(if error == cc_ask::ask_gate::BLOCKED_MESSAGE {
            cc_ask::ask_gate::BLOCKED_EXIT
        } else {
            1
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn independent_recovery_cli_requires_exact_inputs_and_reply_thread() {
        let cli = Cli::try_parse_from([
            "discord_ask",
            "recovery-log",
            "--request-id",
            "new-notice-2472",
            "--title",
            "作業完了",
            "--body-file",
            "notice.txt",
            "--thread",
            "123",
            "--attach",
            "image.png",
        ])
        .unwrap();
        let Commands::RecoveryLog(args) = cli.command else {
            panic!("wrong recovery command");
        };
        assert_eq!(args.request_id, "new-notice-2472");
        assert_eq!(args.thread.as_deref(), Some("123"));
        assert!(!args.notify);
        assert_eq!(args.attach, vec![PathBuf::from("image.png")]);
        assert!(Cli::try_parse_from([
            "discord_ask",
            "recovery-log",
            "--title",
            "作業完了",
            "--body-file",
            "notice.txt"
        ])
        .is_err());
        assert!(Cli::try_parse_from([
            "discord_ask",
            "recovery-ask",
            "--request-id",
            "new-decision-2472",
            "--todo-id",
            "2472",
            "--decision-file",
            "decision.json",
            "--body-file",
            "question.txt"
        ])
        .is_ok());
        let cli = Cli::try_parse_from([
            "discord_ask",
            "recovery-repair-origin",
            "--request-id",
            "exact-device-approval",
            "--todo-id",
            "2497",
            "--expected-origin-thread",
            "123",
            "--owner-reply-message",
            "124",
            "--progress-message",
            "125",
        ])
        .unwrap();
        assert!(matches!(cli.command, Commands::RecoveryRepairOrigin(_)));
        assert!(Cli::try_parse_from([
            "discord_ask",
            "recovery-repair-origin",
            "--request-id",
            "exact-device-approval",
        ])
        .is_err());
        assert!(Cli::try_parse_from([
            "discord_ask",
            "recovery-ask",
            "--request-id",
            "new-decision-2472",
            "--body-file",
            "question.txt"
        ])
        .is_err());
        assert!(Cli::try_parse_from(["discord_ask", "recovery-replies"]).is_err());
        let cli = Cli::try_parse_from([
            "discord_ask",
            "recovery-replies",
            "--thread",
            "123",
            "--message",
            "123",
            "--request-id",
            "new-decision-2472",
            "--todo-id",
            "2472",
            "--read-only",
        ])
        .unwrap();
        let Commands::RecoveryReplies(args) = cli.command else {
            panic!("wrong recovery command");
        };
        assert_eq!(args.thread, "123");
        assert_eq!(args.message.as_deref(), Some("123"));
        assert_eq!(args.todo_id, Some(2472));
        assert!(args.read_only);
    }

    #[test]
    fn repeated_attachments_parse_for_all_three_commands() {
        for command in ["ask", "log", "post"] {
            let mut args = vec!["discord_ask", command];
            match command {
                "ask" => args.extend(["--title", "質問", "--body-file", "body.md"]),
                "log" => args.extend(["--topic", "記録"]),
                _ => args.extend(["--thread", "123", "--body-file", "body.md"]),
            }
            args.extend(["--attach", "image.png", "--attach", "video.mp4"]);
            let cli = Cli::try_parse_from(args.clone()).unwrap();
            match cli.command {
                Commands::Ask(a) => assert_eq!(
                    a.attach,
                    vec![PathBuf::from("image.png"), PathBuf::from("video.mp4")]
                ),
                Commands::Log { attach, notify, .. } | Commands::Post { attach, notify, .. } => {
                    assert_eq!(attach.len(), 2);
                    assert!(!notify);
                }
                _ => panic!("wrong command"),
            }
            if command != "ask" {
                args.push("--notify");
                assert!(matches!(
                    Cli::try_parse_from(args).unwrap().command,
                    Commands::Log { notify: true, .. } | Commands::Post { notify: true, .. }
                ));
            }
        }
    }

    #[test]
    fn original_required_flags_and_explicit_resume_are_supported() {
        assert!(Cli::try_parse_from([
            "discord_ask",
            "ask",
            "--title",
            "質問",
            "--body-file",
            "q.md",
            "--agent",
            "Codex",
            "--cwd",
            "work"
        ])
        .is_ok());
        assert!(Cli::try_parse_from(["discord_ask", "ask", "--title", "質問"]).is_err());
        assert!(Cli::try_parse_from(["discord_ask", "resume"]).is_ok());
        assert!(Cli::try_parse_from(["discord_ask", "reap-helpers", "--dry-run"]).is_ok());
        assert!(Cli::try_parse_from(["discord_ask", "replies", "--thread", "123"]).is_ok());
        assert!(
            Cli::try_parse_from(["discord_ask", "replies", "--thread", "123", "--read-only"])
                .is_ok()
        );
        assert!(Cli::try_parse_from(["discord_ask", "replies", "--read-only"]).is_err());
        assert!(Cli::try_parse_from([
            "discord_ask",
            "snooze",
            "--thread",
            "123",
            "--until",
            "2026-10-03T09:00:00+09:00"
        ])
        .is_ok());
        assert!(Cli::try_parse_from(["discord_ask", "snooze", "--thread", "123"]).is_err());
        assert!(Cli::try_parse_from(["discord_ask", "close", "123"]).is_ok());
        assert!(Cli::try_parse_from(["discord_ask", "inbox"]).is_err());
        assert!(
            Cli::try_parse_from(["discord_ask", "inbox", "--state-file", "cursor.txt"]).is_ok()
        );
    }

    #[test]
    fn log_notify_and_scheduler_question_metadata_parse() {
        let cli = Cli::try_parse_from([
            "discord_ask",
            "log",
            "--topic",
            "記録",
            "--text",
            "完了",
            "--notify",
        ])
        .unwrap();
        assert!(matches!(cli.command, Commands::Log { notify: true, .. }));
        let cli = Cli::try_parse_from([
            "discord_ask",
            "ask",
            "--title",
            "質問",
            "--body-file",
            "q.md",
            "--source",
            "scheduler",
            "--scheduler-item",
            "12",
            "--scheduler-job",
            "job1",
        ])
        .unwrap();
        let Commands::Ask(args) = cli.command else {
            panic!("wrong command")
        };
        assert_eq!(args.source.as_deref(), Some("scheduler"));
        assert_eq!(args.scheduler_item, Some(12));
        assert_eq!(args.scheduler_job.as_deref(), Some("job1"));
    }

    #[test]
    fn recovery_is_read_only_by_default_and_bounded_reminder_flags_parse() {
        let cli = Cli::try_parse_from([
            "discord_ask",
            "recover-questions",
            "--thread",
            "123",
            "--message",
            "456",
            "--limit",
            "1",
        ])
        .unwrap();
        let Commands::RecoverQuestions(args) = cli.command else {
            panic!("wrong command")
        };
        assert!(!args.apply);
        assert_eq!(args.message.as_deref(), Some("456"));
        assert_eq!(args.limit, 1);
        assert!(
            Cli::try_parse_from(["discord_ask", "recover-questions", "--apply", "--dry-run"])
                .is_err()
        );
        assert!(
            Cli::try_parse_from(["discord_ask", "recover-questions", "--message", "456"]).is_err()
        );
        let cli = Cli::try_parse_from([
            "discord_ask",
            "remind",
            "--dry-run",
            "--thread",
            "123",
            "--limit",
            "1",
        ])
        .unwrap();
        let Commands::Remind(args) = cli.command else {
            panic!("wrong command")
        };
        assert!(args.dry_run);
        assert_eq!(args.limit, 1);
    }
}
