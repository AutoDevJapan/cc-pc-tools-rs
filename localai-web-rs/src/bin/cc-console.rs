use serde_json::{Value, json};
use std::{
    path::Path,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

fn valid_date(value: &str) -> bool {
    chrono::NaiveDate::parse_from_str(value, "%Y-%m-%d")
        .is_ok_and(|date| date.format("%Y-%m-%d").to_string() == value)
}

fn take(args: &mut Vec<String>, name: &str) -> Result<Option<String>, String> {
    if let Some(i) = args.iter().position(|s| s == name) {
        args.remove(i);
        if i >= args.len() {
            return Err(format!("{name} needs a value"));
        }
        Ok(Some(args.remove(i)))
    } else {
        Ok(None)
    }
}
fn parse(mut a: Vec<String>) -> Result<(&'static str, String, Value), String> {
    if a.is_empty() {
        return Err("missing command".into());
    }
    let command = a.remove(0);
    match command.as_str() {
        "workload" => {
            let date = take(&mut a, "--date")?;
            if !a.is_empty() {
                return Err("use workload [--date YYYY-MM-DD]".into());
            }
            if date.as_deref().is_some_and(|value| !valid_date(value)) {
                return Err("date must be a valid YYYY-MM-DD date".into());
            }
            let path = date.map_or_else(|| "workload".into(), |d| format!("workload?date={d}"));
            Ok(("WORKLOAD", path, json!({})))
        }
        "jobs" | "handoff" => {
            if !a.is_empty() {
                return Err("unexpected argument".into());
            }
            Ok(("GET", command, json!({})))
        }
        "delegation" => {
            if a.len() != 1 {
                return Err("use delegation status|on|off".into());
            }
            match a[0].as_str() {
                "status" => Ok(("GET", "delegation".into(), json!({}))),
                "on" | "off" => Ok(("POST", format!("delegation/{}", a[0]), json!({}))),
                _ => Err("use delegation status|on|off".into()),
            }
        }
        "todo" => {
            if a.is_empty() {
                return Err("use todo list|add|start|block|unblock|done".into());
            }
            let action = a.remove(0);
            if action == "list" {
                if !a.is_empty() {
                    return Err("todo list takes no arguments".into());
                }
                return Ok(("GET", "todo".into(), json!({})));
            }
            let note = take(&mut a, "--note")?;
            let job = take(&mut a, "--job")?;
            let mut v = json!({});
            match action.as_str() {
                "add" if a.len() == 2 => {
                    v["project"] = json!(a[0]);
                    v["title"] = json!(a[1]);
                }
                "start" | "unblock" | "done" if a.len() == 1 => {
                    v["id"] = json!(
                        a[0].parse::<u64>()
                            .map_err(|_| "id must be a positive integer")?
                    );
                }
                "block" if a.len() == 2 => {
                    v["id"] = json!(
                        a[0].parse::<u64>()
                            .map_err(|_| "id must be a positive integer")?
                    );
                    v["reason"] = json!(a[1]);
                }
                _ => return Err("invalid todo arguments; see --help".into()),
            }
            if let Some(note) = note {
                if !matches!(action.as_str(), "add" | "done") {
                    return Err("--note is for add/done".into());
                }
                v["note"] = json!(note);
            }
            if let Some(job) = job {
                if !matches!(action.as_str(), "start" | "unblock") {
                    return Err("--job is for start/unblock".into());
                }
                v["job"] = json!(job);
            }
            Ok(("POST", format!("todo/{action}"), v))
        }
        "delegate" => {
            let model = take(&mut a, "--model")?;
            let effort = take(&mut a, "--effort")?;
            if model
                .as_deref()
                .is_some_and(|v| !cc_scheduler::backend::valid_model(v))
                || effort
                    .as_deref()
                    .is_some_and(|v| !cc_scheduler::backend::valid_effort(v))
            {
                return Err("invalid --model or --effort".into());
            }
            let sandbox = take(&mut a, "--sandbox")?;
            if sandbox.as_deref().is_some_and(|v| {
                !["read-only", "workspace-write", "danger-full-access"].contains(&v)
            }) {
                return Err(
                    "--sandbox must be read-only|workspace-write|danger-full-access".into(),
                );
            }
            let prompt = take(&mut a, "--prompt-file")?.ok_or("--prompt-file is required")?;
            let request = take(&mut a, "--request-id")?.unwrap_or_else(|| {
                format!(
                    "cli-{}-{}",
                    std::process::id(),
                    SystemTime::now()
                        .duration_since(UNIX_EPOCH)
                        .unwrap_or_default()
                        .as_nanos()
                )
            });
            if a.len() != 1 {
                return Err("delegate <id> --prompt-file <UTF-8 file> [--request-id <id>]".into());
            }
            let file = Path::new(&prompt);
            if file
                .metadata()
                .map_err(|_| "cannot read prompt file")?
                .len()
                > 65536
            {
                return Err("prompt file exceeds 64KiB".into());
            }
            let text = std::fs::read_to_string(file).map_err(|_| "prompt file must be UTF-8")?;
            let mut payload = json!({"id":a[0].parse::<u64>().map_err(|_|"id must be a positive integer")?,"prompt":text.trim_start_matches('\u{feff}'),"request_id":request});
            if let Some(model) = model {
                payload["model"] = json!(model);
            }
            if let Some(effort) = effort {
                payload["effort"] = json!(effort);
            }
            if let Some(sandbox) = sandbox {
                payload["sandbox"] = json!(sandbox);
            }
            Ok(("POST", "delegate".into(), payload))
        }
        _ => Err("unknown command; see --help".into()),
    }
}
const HELP: &str = "cc-console — このPCの共有ToDoと引き継ぎ（出力はJSON）\n\
  cc-console handoff\n  cc-console jobs\n  cc-console todo list\n\
  cc-console workload [--date YYYY-MM-DD]\n\
  cc-console todo add <project> <title> [--note <text>]\n\
  cc-console todo start <id> [--job <job-id>]\n\
  cc-console todo block <id> <reason>\n\
  cc-console todo unblock <id> [--job <job-id>]\n\
  cc-console todo done <id> [--note <text>]\n\
  cc-console delegate <id> --prompt-file <UTF-8 file> [--request-id <stable-id>] [--model <model>] [--effort <effort>]\n\
  cc-console delegation status|on|off\n\
委譲は保存された待ち行列への登録です。job_idは起動後にstatusで確認できます。\n\
再試行には同じrequest-idを使ってください。接続先は127.0.0.1:10012のみ。";

#[tokio::main]
async fn main() {
    let args: Vec<_> = std::env::args().skip(1).collect();
    if args.first().is_some_and(|s| s == "--help" || s == "-h") {
        println!("{HELP}");
        return;
    }
    match run(args).await {
        Ok((v, good)) => {
            println!("{v}");
            if !good {
                std::process::exit(1);
            }
        }
        Err(e) => {
            println!(
                "{}",
                json!({"ok":false,"version":1,"error":{"code":"cli_error","message":e}})
            );
            std::process::exit(2);
        }
    }
}
async fn run(args: Vec<String>) -> Result<(String, bool), String> {
    let (method, path, payload) = parse(args)?;
    let client = reqwest::Client::builder()
        .no_proxy()
        .redirect(reqwest::redirect::Policy::none())
        .connect_timeout(Duration::from_secs(2))
        .timeout(Duration::from_secs(120))
        .build()
        .map_err(|_| "cannot create HTTP client")?;
    let api_path = path.strip_prefix("workload?").map_or_else(
        || format!("/api/agent/{path}"),
        |query| format!("/api/agent/workload?{query}"),
    );
    let url = format!("http://127.0.0.1:10012{api_path}");
    let mut req = client.request(
        if method == "GET" || method == "WORKLOAD" {
            reqwest::Method::GET
        } else {
            reqwest::Method::POST
        },
        url,
    );
    if method == "POST" {
        req = req
            .header("content-type", "application/json")
            .body(payload.to_string());
    }
    let mut response = req.send().await.map_err(
        |_| "コンソールに接続できません。委譲の再試行には同じrequest-idを使ってください。",
    )?;
    let success = response.status().is_success();
    let mut bytes = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(|_| "response interrupted")? {
        if bytes.len() + chunk.len() > 16 * 1024 * 1024 {
            return Err("response too large".into());
        }
        bytes.extend_from_slice(&chunk);
    }
    let value: Value =
        serde_json::from_slice(&bytes).map_err(|_| "console returned invalid JSON")?;
    if method == "WORKLOAD" {
        return Ok((render_workload(&value)?, success));
    }
    Ok((value.to_string(), success))
}

fn render_workload(value: &Value) -> Result<String, String> {
    if value["ok"] != true || value["version"] != 1 {
        let message = value["error"]["message"]
            .as_str()
            .or_else(|| value["message"].as_str())
            .unwrap_or("作業状況を取得できませんでした");
        return Err(message.to_string());
    }
    let data = &value["data"];
    let date = data["date"].as_str().unwrap_or("日付不明");
    let totals = &data["totals"];
    let hours = totals["active_ms"].as_u64().unwrap_or(0) as f64 / 3_600_000.0;
    let mut lines = vec![format!("{date} の作業状況")];
    if data["collecting"] == true {
        lines.push("集計中のデータを含みます。".into());
    }
    if let Some(warnings) = data["warnings"].as_array() {
        for warning in warnings.iter().filter_map(Value::as_str) {
            lines.push(format!("注意: {warning}"));
        }
    }
    lines.push(format!(
        "作業時間 {:.1} 時間 / ジョブ {} 件 / マージ {} 件",
        hours,
        totals["jobs"].as_u64().unwrap_or(0),
        totals["merged_prs"].as_u64().unwrap_or(0)
    ));
    if let Some(projects) = data["projects"].as_array() {
        for project in projects {
            let name = project["name"].as_str().unwrap_or("名前なし");
            lines.push(format!(
                "\n{name} — 作業時間 {:.1} 時間 / ジョブ {} 件 / マージ {} 件",
                project["active_ms"].as_u64().unwrap_or(0) as f64 / 3_600_000.0,
                project["jobs"].as_u64().unwrap_or(0),
                project["merged_prs"].as_u64().unwrap_or(0)
            ));
            let texts: Vec<&str> = project["summary"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|item| item["text"].as_str())
                .filter(|text| !text.trim().is_empty())
                .take(5)
                .collect();
            if texts.is_empty() {
                lines.push("・この日の作業要約はありません。".into());
            } else {
                lines.extend(texts.into_iter().map(|text| format!("・{text}")));
            }
        }
    }
    Ok(lines.join("\n"))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn delegate_model_flags_reach_payload_and_reject_bad_or_duplicate_values() {
        let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/workload/git.txt");
        let base = vec![
            "delegate".into(),
            "1624".into(),
            "--prompt-file".into(),
            path.to_string_lossy().into(),
            "--request-id".into(),
            "review-once".into(),
        ];
        for flags in [
            vec!["--model", "gpt-6-luna"],
            vec!["--effort", "max"],
            vec!["--model", "gpt-6-luna", "--effort", "max"],
        ] {
            let mut args = base.clone();
            args.extend(flags.iter().map(|v| (*v).into()));
            let (_, route, payload) = parse(args).unwrap();
            assert_eq!(route, "delegate");
            if flags.contains(&"--model") {
                assert_eq!(payload["model"], "gpt-6-luna");
            }
            if flags.contains(&"--effort") {
                assert_eq!(payload["effort"], "max");
            }
        }
        for flags in [
            vec!["--model", "bad value"],
            vec!["--effort", "ultra"],
            vec!["--model", "gpt-6-luna", "--model", "gpt-6-astra"],
        ] {
            let mut args = base.clone();
            args.extend(flags.iter().map(|v| (*v).into()));
            assert!(parse(args).is_err());
        }
    }
    #[test]
    fn todo_json_preserves_windows_text() {
        let (_, p, v) = parse(vec![
            "todo".into(),
            "add".into(),
            "project".into(),
            "引用 \" と C:\\work と 日本語".into(),
            "--note".into(),
            "a\nb".into(),
        ])
        .unwrap();
        assert_eq!(p, "todo/add");
        assert_eq!(v["title"], "引用 \" と C:\\work と 日本語");
        assert_eq!(v["note"], "a\nb");
    }
    #[test]
    fn invalid_flags_and_ids_fail() {
        assert!(
            parse(vec![
                "todo".into(),
                "done".into(),
                "1".into(),
                "--job".into(),
                "x".into()
            ])
            .is_err()
        );
        assert!(parse(vec!["todo".into(), "start".into(), "bad".into()]).is_err());
    }

    #[test]
    fn workload_date_builds_api_query_and_validates_calendar_date() {
        let (method, path, payload) = parse(vec![
            "workload".into(),
            "--date".into(),
            "2026-10-01".into(),
        ])
        .unwrap();
        assert_eq!(method, "WORKLOAD");
        assert_eq!(path, "workload?date=2026-10-01");
        assert_eq!(payload, json!({}));
        assert!(
            parse(vec![
                "workload".into(),
                "--date".into(),
                "2026-02-30".into()
            ])
            .is_err()
        );
        assert!(parse(vec!["workload".into(), "--date".into(), "2026-1-01".into()]).is_err());
    }

    #[test]
    fn workload_rejects_unknown_and_duplicate_arguments() {
        assert!(parse(vec!["workload".into(), "--wat".into()]).is_err());
        assert!(
            parse(vec![
                "workload".into(),
                "--date".into(),
                "2026-10-01".into(),
                "--date".into(),
                "2026-10-02".into()
            ])
            .is_err()
        );
    }

    #[test]
    fn workload_summary_is_japanese_and_does_not_print_raw_json() {
        let rendered = render_workload(&json!({
            "ok": true, "version": 1, "data": {
                "date": "2026-10-01", "collecting": false, "warnings": [],
                "totals": {"active_ms": 5_400_000, "jobs": 3, "merged_prs": 1},
                "projects": [{"name": "demo", "summary": [{"text": "画面を追加しました。"}] }]
            }
        }))
        .unwrap();
        assert!(rendered.contains("作業時間 1.5 時間 / ジョブ 3 件 / マージ 1 件"));
        assert!(rendered.contains("・画面を追加しました。"));
        assert!(!rendered.contains("\"active_ms\""));
    }
}
