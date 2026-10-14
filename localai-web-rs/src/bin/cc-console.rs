use serde_json::{json, Value};
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
                let mut query = Vec::new();
                for (flag, key) in [
                    ("--after", "after"),
                    ("--id", "id"),
                    ("--limit", "limit"),
                    ("--link-offset", "link_offset"),
                    ("--revision", "revision"),
                ] {
                    if let Some(value) = take(&mut a, flag)? {
                        if (key == "revision"
                            && (value.len() != 64 || !value.bytes().all(|b| b.is_ascii_hexdigit())))
                            || (key != "revision"
                                && (value.is_empty() || !value.bytes().all(|b| b.is_ascii_digit())))
                        {
                            return Err("invalid listing value".into());
                        }
                        query.push(format!("{key}={value}"));
                    }
                }
                if !a.is_empty() {
                    return Err("unexpected todo list argument".into());
                }
                let query = query.join("&");
                localai_web_rs::todo_listing::Query::parse(&query).map_err(str::to_owned)?;
                return Ok(("LIST", format!("todo/list?{query}"), json!({})));
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
                    v["id"] = json!(a[0]
                        .parse::<u64>()
                        .map_err(|_| "id must be a positive integer")?);
                }
                "block" if a.len() == 2 => {
                    v["id"] = json!(a[0]
                        .parse::<u64>()
                        .map_err(|_| "id must be a positive integer")?);
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
  cc-console todo list [--limit 1..100] [--after <id>] [--revision <hash>]\n\
  cc-console todo list --id <id> [--link-offset <offset>] [--revision <hash>]\n\
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
        if matches!(method, "GET" | "WORKLOAD" | "LIST") {
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
    let response = req.send().await.map_err(|_| {
        "コンソールに接続できません。委譲の再試行には同じrequest-idを使ってください。"
    })?;
    read_response(response, method).await
}
async fn read_response(
    mut response: reqwest::Response,
    method: &str,
) -> Result<(String, bool), String> {
    let status = response.status();
    let success = status.is_success();
    let limit = if method == "LIST" {
        localai_web_rs::todo_listing::RESPONSE_LIMIT
    } else {
        16 * 1024 * 1024
    };
    let mut bytes = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(|_| "response interrupted")? {
        if chunk.len() > limit.saturating_sub(bytes.len()) {
            return Err("response too large".into());
        }
        bytes.extend_from_slice(&chunk);
    }
    let value: Value =
        serde_json::from_slice(&bytes).map_err(|_| "console returned invalid JSON")?;
    if method == "LIST" {
        if !success || value["ok"] == false {
            let code = match value["error"]["code"].as_str() {
                Some("listing_changed") => "listing_changed",
                Some("todo_not_found") => "todo_not_found",
                Some("invalid_request") => "invalid_request",
                Some("todo_store_too_large") => "todo_store_too_large",
                Some("todo_store_invalid_json") => "todo_store_invalid_json",
                Some("state_unavailable") => "state_unavailable",
                _ => "listing_failed",
            };
            return Ok((json!({"ok":false,"version":1,"error":{"code":code,"httpStatus":status.as_u16(),"message":"共有ToDoの有限一覧を取得できませんでした。listing_changedの場合は先頭から取得し直してください。"}}).to_string(), false));
        }
        return Ok((validate_listing(value)?.to_string(), true));
    }
    if method == "WORKLOAD" {
        return Ok((render_workload(&value)?, success));
    }
    Ok((value.to_string(), success))
}

fn validate_listing(value: Value) -> Result<Value, String> {
    fn hash(v: &Value) -> bool {
        v.as_str()
            .is_some_and(|s| s.len() == 64 && s.bytes().all(|b| b.is_ascii_hexdigit()))
    }
    fn positive(v: &Value) -> bool {
        v.as_u64().is_some_and(|n| n > 0)
    }
    let invalid = || "console returned invalid or unprotected listing".to_owned();
    let d = &value["data"];
    let limit = d["limit"]
        .as_u64()
        .filter(|n| *n > 0 && *n <= 100)
        .ok_or_else(invalid)?;
    let items = d["items"]
        .as_array()
        .filter(|rows| rows.len() <= limit as usize)
        .ok_or_else(invalid)?;
    if value["ok"] != true
        || value["version"] != 1
        || d["metadataOnly"] != true
        || d["protectionMode"] != "masked_metadata"
        || !hash(&d["revision"])
        || !d["total"].is_u64()
        || !d["linkOffset"].is_u64()
        || !(d["nextAfter"].is_null() || positive(&d["nextAfter"]))
    {
        return Err(invalid());
    }
    let mut previous = 0;
    let mut rows = Vec::new();
    for row in items {
        let id = row["id"]
            .as_u64()
            .filter(|n| *n > previous)
            .ok_or_else(invalid)?;
        previous = id;
        if !row["status"]
            .as_str()
            .is_some_and(|s| matches!(s, "todo" | "doing" | "block" | "wait" | "done" | "unknown"))
            || !(row["agent"].is_null() || hash(&row["agent"]))
            || !(row["progress"].is_null() || row["progress"].as_u64().is_some_and(|n| n <= 100))
        {
            return Err(invalid());
        }
        for (key, total, next, numeric) in [
            ("jobs", "jobsTotal", "jobsNextOffset", false),
            ("wait_on", "waitOnTotal", "waitOnNextOffset", true),
        ] {
            if !row[key].as_array().is_some_and(|a| {
                a.len() <= localai_web_rs::todo_listing::LINK_LIMIT
                    && a.iter()
                        .all(|v| if numeric { positive(v) } else { hash(v) })
            }) || !row[total].is_u64()
                || !(row[next].is_null() || row[next].is_u64())
            {
                return Err(invalid());
            }
        }
        // Output allowlist protects against an older/misconfigured server adding raw text.
        let mut safe = json!({});
        for key in [
            "id",
            "status",
            "progress",
            "agent",
            "jobs",
            "jobsTotal",
            "jobsNextOffset",
            "wait_on",
            "waitOnTotal",
            "waitOnNextOffset",
        ] {
            safe[key] = row[key].clone();
        }
        rows.push(safe);
    }
    let mut safe = json!({"items":rows});
    for key in [
        "total",
        "nextAfter",
        "revision",
        "limit",
        "linkOffset",
        "protectionMode",
        "metadataOnly",
    ] {
        safe[key] = d[key].clone();
    }
    Ok(json!({"ok":true,"version":1,"data":safe}))
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
    fn listing_flags_and_response_allowlist() {
        let (method, path, _) = parse(vec![
            "todo".into(),
            "list".into(),
            "--id".into(),
            "2628".into(),
        ])
        .unwrap();
        assert_eq!(method, "LIST");
        assert_eq!(path, "todo/list?id=2628");
        for args in [
            vec!["--after", "1&limit=2"],
            vec!["--limit", "101"],
            vec!["--id", "1", "--after", "2"],
            vec!["--limit", "1", "--limit", "2"],
        ] {
            let mut a = vec!["todo".into(), "list".into()];
            a.extend(args.into_iter().map(String::from));
            assert!(parse(a).is_err());
        }
        let data = localai_web_rs::todo_listing::project(
            &json!({"items":[{"id":2628,"status":"doing","wait_on":[2467],"agent":"reviewer"}]}),
            &localai_web_rs::todo_listing::Query::parse("").unwrap(),
        )
        .unwrap();
        let mut response = json!({"ok":true,"version":1,"data":data,"secret":"omit"});
        response["data"]["items"][0]["note"] = json!("omit");
        assert!(!validate_listing(response.clone())
            .unwrap()
            .to_string()
            .contains("omit"));
        response["data"]["metadataOnly"] = json!(false);
        assert!(validate_listing(response.clone()).is_err());
        response["data"]["metadataOnly"] = json!(true);
        response["data"]["items"][0]["agent"] = json!("secret");
        assert!(validate_listing(response).is_err());
    }
    async fn wire_response(
        header: String,
        body: Vec<u8>,
        method: &str,
    ) -> Result<(String, bool), String> {
        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.unwrap();
            let mut request = [0; 4096];
            let _ = stream.read(&mut request).await;
            let _ = stream.write_all(header.as_bytes()).await;
            let _ = stream.write_all(&body).await;
            let _ = stream.shutdown().await;
        });
        let response = reqwest::Client::builder()
            .no_proxy()
            .timeout(Duration::from_secs(10))
            .build()
            .unwrap()
            .get(format!("http://{address}/api/agent/todo/list"))
            .send()
            .await
            .unwrap();
        let result = read_response(response, method).await;
        server.await.unwrap();
        result
    }
    #[tokio::test]
    async fn listing_real_http_shape_success_failure_and_interrupted_body() {
        let data = localai_web_rs::todo_listing::project(
            &json!({"items":[{"id":1,"status":"wait","wait_on":[2]}]}),
            &localai_web_rs::todo_listing::Query::parse("").unwrap(),
        )
        .unwrap();
        let body = json!({"ok":true,"version":1,"data":data})
            .to_string()
            .into_bytes();
        let (out, good) = wire_response(
            format!(
                "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                body.len()
            ),
            body,
            "LIST",
        )
        .await
        .unwrap();
        assert!(good);
        assert_eq!(
            serde_json::from_str::<Value>(&out).unwrap()["data"]["items"][0]["wait_on"],
            json!([2])
        );
        let error = b"{\"ok\":false,\"error\":{\"message\":\"secret-response\"}}".to_vec();
        for status in ["503 Service Unavailable", "200 OK"] {
            let (out, good) = wire_response(
                format!(
                    "HTTP/1.1 {status}\r\nContent-Length: {}\r\n\r\n",
                    error.len()
                ),
                error.clone(),
                "LIST",
            )
            .await
            .unwrap();
            assert!(!good);
            assert!(!out.contains("secret-response"));
        }
        assert_eq!(
            wire_response(
                "HTTP/1.1 200 OK\r\nContent-Length: 200\r\n\r\n".into(),
                b"{\"ok\":true}".to_vec(),
                "LIST"
            )
            .await
            .unwrap_err(),
            "response interrupted"
        );
        assert!(wire_response(
            "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n".into(),
            b"{}".to_vec(),
            "LIST"
        )
        .await
        .is_err());
    }
    #[tokio::test]
    async fn response_caps_include_chunked_transfer_and_keep_legacy_16mib_bound() {
        for (method, size) in [
            ("GET", 16 * 1024 * 1024 + 1),
            ("LIST", localai_web_rs::todo_listing::RESPONSE_LIMIT + 1),
        ] {
            let body = vec![b' '; size];
            assert_eq!(
                wire_response(
                    format!("HTTP/1.1 200 OK\r\nContent-Length: {size}\r\n\r\n"),
                    body.clone(),
                    method
                )
                .await
                .unwrap_err(),
                "response too large"
            );
            let mut chunked = format!("{size:x}\r\n").into_bytes();
            chunked.extend(body);
            chunked.extend(b"\r\n0\r\n\r\n");
            assert_eq!(
                wire_response(
                    "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n".into(),
                    chunked,
                    method
                )
                .await
                .unwrap_err(),
                "response too large"
            );
        }
        let (out, good) = wire_response(
            "HTTP/1.1 200 OK\r\nContent-Length: 11\r\n\r\n".into(),
            b"{\"ok\":true}".to_vec(),
            "GET",
        )
        .await
        .unwrap();
        assert!(good);
        assert_eq!(out, "{\"ok\":true}");
    }
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
        assert!(parse(vec![
            "todo".into(),
            "done".into(),
            "1".into(),
            "--job".into(),
            "x".into()
        ])
        .is_err());
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
        assert!(parse(vec![
            "workload".into(),
            "--date".into(),
            "2026-02-30".into()
        ])
        .is_err());
        assert!(parse(vec!["workload".into(), "--date".into(), "2026-1-01".into()]).is_err());
    }

    #[test]
    fn workload_rejects_unknown_and_duplicate_arguments() {
        assert!(parse(vec!["workload".into(), "--wat".into()]).is_err());
        assert!(parse(vec![
            "workload".into(),
            "--date".into(),
            "2026-10-01".into(),
            "--date".into(),
            "2026-10-02".into()
        ])
        .is_err());
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
