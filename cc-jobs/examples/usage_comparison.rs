//! One-shot deployment observation. Uses only the installed ocx CLI, never an
//! API key or a model call; records adjacent equal-length measured windows.
use cc_jobs::{hidden_command, s, which};
use serde_json::Value;
use std::{
    collections::BTreeMap,
    env, fs,
    io::Write,
    path::Path,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as u64
}
fn local(ms: u64) -> String {
    chrono::DateTime::from_timestamp_millis(ms as i64)
        .unwrap()
        .with_timezone(&chrono::FixedOffset::east_opt(9 * 3600).unwrap())
        .format("%Y-%m-%d %H:%M:%S JST")
        .to_string()
}
fn observe(cli: &str, since: u64, until: u64) -> Result<Value, Box<dyn std::error::Error>> {
    let out = hidden_command("cmd.exe")
        .args([
            "/d",
            "/c",
            cli,
            "observe",
            "usage",
            "--range",
            "7d",
            "--surface",
            "codex",
            "--since",
            &since.to_string(),
            "--until",
            &until.to_string(),
            "--json",
        ])
        .output()?;
    if !out.status.success() {
        return Err(format!("ocx failed: {}", String::from_utf8_lossy(&out.stderr)).into());
    }
    let v: Value = serde_json::from_slice(&out.stdout)?;
    if v["summary"]["requests"].as_u64().is_none()
        || v["summary"]["totalTokens"].as_u64().is_none()
        || v["historyTruncated"] == true
        || v["entriesTruncated"] == true
    {
        return Err("incomplete ocx usage observation".into());
    }
    Ok(v)
}
fn record_counts(jobs: &Path) -> Value {
    let mut running = 0u64;
    let mut queued = 0u64;
    let mut rotated = 0u64;
    let mut summarizing = 0u64;
    for e in fs::read_dir(jobs).into_iter().flatten().flatten() {
        let Ok(raw) = fs::read(e.path().join("job.json")) else {
            continue;
        };
        let Ok(v) = serde_json::from_slice::<Value>(&raw) else {
            continue;
        };
        match s(&v, "status") {
            "running" => running += 1,
            "queued" => queued += 1,
            _ => (),
        }
        rotated += v["context_rotation_count"].as_u64().unwrap_or(0);
        if s(&v, "context_phase") == "summarize" && ["queued", "running"].contains(&s(&v, "status"))
        {
            summarizing += 1;
        }
    }
    serde_json::json!({"running":running,"queued":queued,"rotations":rotated,"summarizing":summarizing})
}
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = env::args().skip(1).collect();
    let get = |name: &str| {
        args.windows(2)
            .find(|w| w[0] == name)
            .map(|w| w[1].clone())
            .ok_or_else(|| format!("missing {name}"))
    };
    let deployment: u64 = get("--deployment-ms")?.parse()?;
    let seconds: u64 = get("--window-seconds")?.parse()?;
    if !(60..=86400).contains(&seconds) {
        return Err("window must be 60..86400 seconds".into());
    }
    let window = seconds * 1000;
    let before_start = deployment
        .checked_sub(window)
        .ok_or("invalid deployment timestamp")?;
    let after_end = deployment
        .checked_add(window)
        .ok_or("invalid deployment timestamp")?;
    let report = get("--report")?;
    let evidence = get("--evidence")?;
    let jobs = get("--jobs")?;
    let cli =
        which("ocx.cmd", &env::vars().collect::<BTreeMap<_, _>>()).ok_or("ocx.cmd unavailable")?;
    let initial_counts = record_counts(Path::new(&jobs));
    let before = observe(&cli, before_start, deployment)?;
    fs::write(
        Path::new(&evidence).with_extension("before.json"),
        serde_json::to_vec_pretty(&before)?,
    )?;
    println!("before measured; after window ends {}", local(after_end));
    // One absolute deadline. No polling, recurring task, or additional model job.
    if after_end > now_ms() {
        std::thread::sleep(Duration::from_millis(after_end - now_ms()));
    }
    let after = observe(&cli, deployment, after_end)?;
    let counts = record_counts(Path::new(&jobs));
    let result = serde_json::json!({"deployment_ms":deployment,"window_seconds":seconds,"before":before,"after":after,"counts_at_start":initial_counts,"counts_at_end":counts});
    fs::write(&evidence, serde_json::to_vec_pretty(&result)?)?;
    let mut text = format!(
        "\n## 差し替え前後の測定結果\n\n差し替え時刻: {}。各区間は{}秒。要求とトークンは観測した実数から1時間あたりへ換算しました。\n\n| 区間 | 要求の実数 | 要求/時間 | 総トークンの実数 | 総トークン/時間 | キャッシュ済み入力/時間 | 報告率 |\n|---|---:|---:|---:|---:|---:|---:|\n",
        local(deployment),
        seconds
    );
    for (label, v) in [("変更前", &result["before"]), ("変更後", &result["after"])] {
        let sum = &v["summary"];
        let requests = sum["requests"].as_u64().unwrap();
        let total = sum["totalTokens"].as_u64().unwrap();
        let rate = 3600. / seconds as f64;
        text.push_str(&format!(
            "| {label} | {requests} | {:.2} | {total} | {:.2} | {:.2} | {:.2}% |\n",
            requests as f64 * rate,
            total as f64 * rate,
            sum["cachedInputTokens"].as_u64().unwrap_or(0) as f64 * rate,
            sum["coverageRatio"].as_f64().unwrap_or(0.) * 100.
        ));
    }
    let b = result["before"]["summary"]["totalTokens"].as_u64().unwrap() as f64;
    let a = result["after"]["summary"]["totalTokens"].as_u64().unwrap() as f64;
    if b > 0. {
        text.push_str(&format!(
            "\n総トークンの変化: {:.2}%（負なら減少）。",
            (a / b - 1.) * 100.
        ));
    }
    text.push_str(&format!("\n変更前: {}～{}。変更後: {}～{}。\n観測開始時のジョブ数: {}。終了時: {}。\n並行ジョブ数・作業内容・旧版の担当が切り替わる時刻が異なるため、変化のすべてを今回の修正の効果とは断定しません。キャッシュ入力は入力の内数です。モデル別の内訳は実測記録に保存しました。\n",local(before_start),local(deployment),local(deployment),local(after_end),result["counts_at_start"],result["counts_at_end"]));
    fs::OpenOptions::new()
        .append(true)
        .open(report)?
        .write_all(text.as_bytes())?;
    println!("equal-window usage comparison recorded");
    Ok(())
}
