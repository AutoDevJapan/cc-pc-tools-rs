//! Durable retry state and worker activity; supervisor heartbeats are not work.
use crate::*;

#[derive(Default, Serialize, Deserialize)]
#[serde(default)]
pub struct Retry {
    pub attempts: u32,
    pub last_attempt: i64,
    pub notice_sent: bool,
    pub kind: String,
}

pub fn atomic_json(path: &Path, value: &impl Serialize) -> io::Result<()> {
    let parent = path
        .parent()
        .ok_or_else(|| io::Error::other("missing parent"))?;
    fs::create_dir_all(parent)?;
    let mut temp = tempfile::NamedTempFile::new_in(parent)?;
    serde_json::to_writer_pretty(&mut temp, value)?;
    temp.as_file().sync_all()?;
    temp.persist(path).map_err(|e| e.error)?;
    Ok(())
}
pub fn retry_path(record: &Path) -> PathBuf {
    record
        .parent()
        .unwrap()
        .parent()
        .unwrap()
        .join("chrome-retries")
        .join(record.file_name().unwrap())
}
pub fn retry(record: &Path) -> io::Result<Retry> {
    match fs::read(retry_path(record)) {
        Ok(bytes) => serde_json::from_slice(&bytes).map_err(io::Error::other),
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(Retry::default()),
        Err(e) => Err(e),
    }
}
pub fn failed(record: &Path, now: i64, error: &io::Error) {
    let mut retry = retry(record).unwrap_or_default();
    retry.attempts = retry.attempts.saturating_add(1);
    retry.last_attempt = now;
    retry.kind = format!("{:?}", error.kind());
    let _ = atomic_json(&retry_path(record), &retry);
}
pub fn activity(dir: &Path) -> io::Result<()> {
    atomic_json(&dir.join("chrome-activity.json"), &json!({"at":epoch()}))
}
pub fn idle(dir: &Path, job: &Value, record: &Record, now: i64) -> bool {
    let mut last = record.created;
    // Only a running worker's event timestamp is meaningful. Queue supervision updates it too.
    if job["status"] == "running"
        && let Some(at) = job["last_event_at"]
            .as_str()
            .and_then(|v| chrono::DateTime::parse_from_rfc3339(v).ok())
    {
        last = last.max(at.timestamp());
    }
    match fs::read(dir.join("chrome-activity.json")) {
        Ok(bytes) => match serde_json::from_slice::<Value>(&bytes)
            .ok()
            .and_then(|v| v["at"].as_i64())
        {
            Some(at) => last = last.max(at),
            None => return false,
        },
        Err(e) if e.kind() == io::ErrorKind::NotFound => {}
        Err(_) => return false,
    }
    // Older supervisors did not maintain an activity marker. Actual event-file writes count.
    match fs::metadata(dir.join("events.jsonl")) {
        Ok(meta) => match meta
            .modified()
            .ok()
            .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
        {
            Some(at) => last = last.max(at.as_secs() as i64),
            None => return false,
        },
        Err(e) if e.kind() == io::ErrorKind::NotFound => {}
        Err(_) => return false,
    }
    now.saturating_sub(last) >= 30 * 60
}

/// The minute worker serializes retry state and aggregates one silent report.
pub fn notices_with(
    jobs: &Path,
    send: &mut dyn FnMut(&str) -> io::Result<()>,
) -> io::Result<usize> {
    let lock = fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(jobs.join("chrome-cleanup.lock"))?;
    if lock.try_lock().is_err() {
        return Ok(0);
    }
    let mut pending = vec![];
    for dir in fs::read_dir(jobs)? {
        let Ok(dir) = dir else {
            continue;
        };
        if !dir.file_type().is_ok_and(|t| t.is_dir()) {
            continue;
        }
        let folder = dir.path().join("chrome-retries");
        let entries = match fs::read_dir(folder) {
            Ok(v) => v,
            Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
            Err(_) => continue,
        };
        for entry in entries {
            let Ok(entry) = entry else {
                continue;
            };
            let path = entry.path();
            if path.extension().and_then(|s| s.to_str()) != Some("json") {
                continue;
            }
            let Ok(bytes) = fs::read(&path) else {
                continue;
            };
            let Ok(r) = serde_json::from_slice::<Retry>(&bytes) else {
                continue;
            };
            if r.attempts >= 3 && !r.notice_sent {
                pending.push((path, r));
            }
        }
    }
    if pending.is_empty() {
        return Ok(0);
    }
    let message = format!(
        "Chromeの作業用タブ{}件で、終了処理が3回以上失敗しました。記録を保ち、毎分の再試行を続けます。",
        pending.len()
    );
    send(&message)?;
    for (path, r) in &mut pending {
        r.notice_sent = true;
        atomic_json(path, r)?;
    }
    Ok(pending.len())
}
#[cfg(windows)]
pub fn notices(jobs: &Path) -> io::Result<usize> {
    use std::os::windows::process::CommandExt;
    notices_with(jobs, &mut |message| {
        let mut child = std::process::Command::new("D:/CC/Tools/discord-ask/discord_ask.exe")
            .args(["log", "--topic", "後片付けの記録", "--text", message])
            .creation_flags(0x08000000)
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()?;
        let started = std::time::Instant::now();
        loop {
            if let Some(status) = child.try_wait()? {
                if !status.success() {
                    return Err(io::Error::other("silent cleanup report failed"));
                }
                break;
            }
            if started.elapsed() >= std::time::Duration::from_secs(15) {
                let _ = child.kill();
                let _ = child.wait();
                return Err(io::Error::new(
                    io::ErrorKind::TimedOut,
                    "silent cleanup report timed out",
                ));
            }
            std::thread::sleep(std::time::Duration::from_millis(50));
        }
        Ok(())
    })
}
