//! Install the reviewed CLI under the normal tick/board/reply locks; preserve a rollback binary.
use cc_ask::core::*;
use std::{fs, path::PathBuf, time::Duration};
fn main() {
    if let Err(e) = run() {
        eprintln!("{e}");
        std::process::exit(1);
    }
}
fn run() -> Result<()> {
    let mut a = std::env::args_os().skip(1);
    let source = PathBuf::from(a.next().ok_or("source required")?);
    let backup = PathBuf::from(a.next().ok_or("backup required")?);
    if a.next().is_some() || backup.exists() {
        return Err("invalid backup arguments".into());
    }
    let home = PathBuf::from("D:/CC/Tools/discord-ask");
    let _tick = ByteLock::acquire(&home.join("tick.lock"), Duration::from_secs(45))?;
    let _board = ByteLock::acquire(&home.join("board.lock"), Duration::from_secs(45))?;
    let _reader = ByteLock::acquire(&home.join("reply-work.lock"), Duration::from_secs(45))?;
    let target = home.join("discord_ask.exe");
    fs::copy(&target, &backup).map_err(|_| "could not preserve rollback binary")?;
    // Rename the in-use image first. A failed replacement restores its original name.
    let retired = home.join(format!(
        "discord_ask.history-audit-2162-{}.retired.exe",
        uuid::Uuid::new_v4().simple()
    ));
    if retired.exists() {
        return Err("retired image already exists".into());
    }
    fs::rename(&target, &retired).map_err(|_| "could not retire old executable")?;
    if fs::copy(&source, &target).is_err() {
        fs::rename(&retired, &target).map_err(|_| "replacement and restore failed")?;
        return Err("replacement failed; previous image restored".into());
    }
    // The owned backup may be on another volume. Keep the retired image on D:
    // until the caller verifies deployment and performs its normal cleanup.
    println!("installed");
    Ok(())
}
