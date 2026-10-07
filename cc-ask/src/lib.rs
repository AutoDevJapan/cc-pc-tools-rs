//! Ask the owner a question on Discord and read the owner's replies.
//!
//! Uses the GoDD notifier bot (~/.godd/discord-notify.json). The bot token is read from the
//! GODD_DISCORD_BOT_TOKEN environment variable or the user's registry environment and is never printed.
//!
//!   discord_ask.exe check
//!   discord_ask.exe ask --title "Q24 ..." --body-file question.md
//!   discord_ask.exe replies --thread <thread_id>  # consume new replies once
//!   discord_ask.exe replies --thread <thread_id> --read-only  # inspect history
//!   discord_ask.exe close <thread_id>
//!   discord_ask.exe lint --body-file report.md  # phrasing check before an owner post
//!   discord_ask.exe intake
//!   discord_ask.exe tick  # run every minute by CC-DiscordAskTick
//!
//! Every TUI agent on this PC shares this tool and state.json (Skill: discord-owner-ask), so every
//! state change runs under one lock file.

pub mod ask_gate;
pub mod attachments;
pub mod automation;
pub mod board;
pub mod civ6_idle;
pub mod commands;
pub mod core;
pub mod helper_reaper;
pub mod intake;
pub mod ja_check;
pub mod job_titles;
pub mod owner_wait;
pub mod patrol;
mod process_guard;
pub mod project;
pub mod prompt_context;
pub mod questions;
pub mod recovery_failure;
pub mod reminders;
pub mod restart;
pub mod result_delivery;
pub mod resume;
pub mod routing;
pub mod snooze;
pub mod state_work;
pub mod steering;
pub mod sweep;
pub mod tick_runtime;
pub mod unassigned;
pub use cc_job_policy as job_policy;
pub use cc_task_title as task_title;
pub use core::*;
