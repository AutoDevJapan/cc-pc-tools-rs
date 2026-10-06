use crate::*;
use regex::Regex;
use std::{collections::BTreeMap, path::Path, sync::OnceLock};
#[path = "east_asian.rs"]
mod east_asian;
pub fn short(text: &str, n: usize) -> String {
    let text = text.split_whitespace().collect::<Vec<_>>().join(" ");
    if text.chars().count() <= n {
        text
    } else {
        format!(
            "{}...",
            text.chars().take(n.saturating_sub(3)).collect::<String>()
        )
    }
}
pub fn fmt_elapsed(seconds: Option<f64>) -> String {
    let Some(s) = seconds else { return "-".into() };
    let s = s as i64;
    if s < 60 {
        format!("{s}s")
    } else if s < 3600 {
        format!("{}m{:02}s", s / 60, s % 60)
    } else {
        format!("{}h{:02}m", s / 3600, s % 3600 / 60)
    }
}
pub fn fmt_tokens(n: i64) -> String {
    if n >= 1_000_000 {
        format!("{:.1}M", n as f64 / 1e6)
    } else if n >= 1000 {
        format!("{:.1}k", n as f64 / 1000.)
    } else {
        n.to_string()
    }
}
pub fn tail_path(p: &str, n: usize) -> String {
    if p.chars().count() <= n {
        p.into()
    } else {
        format!(
            "...{}",
            p.chars()
                .skip(p.chars().count() - n.saturating_sub(3))
                .collect::<String>()
        )
    }
}
/// Terminal columns of s (CJK and emoji take two).
pub fn width(s: &str) -> usize {
    s.chars()
        .map(|c| {
            let c = c as u32;
            if east_asian::WIDE.iter().any(|&(a, b)| a <= c && c <= b) {
                2
            } else {
                1
            }
        })
        .sum()
}
/// Unwrap the `pwsh -Command '<script>'` codex puts around every command on Windows.
pub fn clean_cmd(cmd: &str) -> String {
    static RE: OnceLock<Regex> = OnceLock::new();
    let re = RE.get_or_init(|| {
        Regex::new(r#"(?s)^"?[^"]*?pwsh(?:\.exe)?"?\s+(?:-NoProfile\s+)?-Command\s+(.*)"#).unwrap()
    });
    let Some(m) = re.captures(cmd) else {
        return cmd.into();
    };
    let text = m.get(1).unwrap().as_str();
    match posix_split(text) {
        Some(p) if p.len() == 1 => p[0].clone(),
        _ => text.into(),
    }
}
fn posix_split(text: &str) -> Option<Vec<String>> {
    let mut words = Vec::new();
    let (mut q, mut word, mut active) = (None, String::new(), false);
    let mut chars = text.chars();
    while let Some(c) = chars.next() {
        match q {
            Some('\'') => {
                if c == '\'' {
                    q = None
                } else {
                    word.push(c)
                }
            }
            Some('"') => {
                if c == '"' {
                    q = None
                } else if c == '\\' {
                    let n = chars.next()?;
                    if !matches!(n, '"' | '\\' | '$' | '`' | '\n') {
                        word.push('\\');
                    }
                    word.push(n);
                } else {
                    word.push(c)
                }
            }
            _ => {
                if c == '\'' || c == '"' {
                    q = Some(c);
                    active = true;
                } else if c == '\\' {
                    word.push(chars.next()?);
                    active = true;
                } else if c.is_whitespace() {
                    if active {
                        words.push(std::mem::take(&mut word));
                        active = false;
                    }
                } else {
                    word.push(c);
                    active = true;
                }
            }
        }
    }
    if q.is_some() {
        return None;
    }
    if active {
        words.push(word)
    }
    Some(words)
}
pub fn comma(n: i64) -> String {
    let s = n.to_string();
    let mut out = String::new();
    for (i, c) in s.chars().enumerate() {
        if i > 0 && (s.len() - i).is_multiple_of(3) && c != '-' {
            out.push(',')
        }
        out.push(c)
    }
    out
}
pub fn err_text(v: &Value) -> String {
    if v.is_object() {
        s(v, "message").into()
    } else if v.is_null() {
        String::new()
    } else {
        pystr(v)
    }
}
fn array(v: &Value) -> &[Value] {
    v.as_array().map(Vec::as_slice).unwrap_or(&[])
}
/// One human-readable progress line for a codex JSONL event (None = not worth showing).
pub fn describe(ev: &Value) -> Option<String> {
    if !ev.is_object() {
        return None;
    }
    let t = s(ev, "type");
    match t {
        "thread.started" => return Some(format!("session {}", getstr(ev, "thread_id"))),
        "turn.completed" => {
            let u = &ev["usage"];
            return Some(format!(
                "turn completed: {} tokens (in {}, cached {}, out {})",
                comma(number(u, "input_tokens") + number(u, "output_tokens")),
                comma(number(u, "input_tokens")),
                comma(number(u, "cached_input_tokens")),
                comma(number(u, "output_tokens"))
            ));
        }
        "turn.failed" => return Some(format!("FAILED: {}", short(&err_text(&ev["error"]), 300))),
        "error" => return Some(format!("error: {}", short(s(ev, "message"), 300))),
        _ => (),
    }
    if !t.starts_with("item.") {
        return None;
    }
    let it = &ev["item"];
    if !it.is_object() {
        return None;
    }
    let (started, completed) = (t == "item.started", t == "item.completed");
    let line = match s(it, "type") {
        "command_execution" if started => format!("$ {}", short(&clean_cmd(s(it, "command")), 200)),
        "command_execution" if completed => {
            let status = s(it, "status");
            let extra = if !status.is_empty() && status != "completed" {
                format!(" [{status}]")
            } else {
                String::new()
            };
            format!(
                "  -> exit {}{}, {} lines: {}",
                getstr(it, "exit_code"),
                extra,
                s(it, "aggregated_output").trim().lines().count(),
                short(&clean_cmd(s(it, "command")), 60)
            )
        }
        "agent_message" if completed => format!("> {}", short(s(it, "text"), 300)),
        "reasoning" if completed => format!("~ {}", short(s(it, "text"), 200)),
        "file_change" if completed => format!(
            "edit: {}",
            array(&it["changes"])
                .iter()
                .map(|c| format!("{} {}", getstr(c, "kind"), getstr(c, "path")))
                .collect::<Vec<_>>()
                .join(", ")
        ),
        "mcp_tool_call" if started => {
            format!("mcp {}.{}", getstr(it, "server"), getstr(it, "tool"))
        }
        "mcp_tool_call" if completed => format!(
            "  -> mcp {}{}",
            getstr(it, "status"),
            if truth(&it["error"]) {
                format!(": {}", short(&err_text(&it["error"]), 200))
            } else {
                String::new()
            }
        ),
        "web_search" if completed => format!("search: {}", short(s(it, "query"), 200)),
        "todo_list" => format!(
            "plan: {}",
            array(&it["items"])
                .iter()
                .map(|i| format!(
                    "{} {}",
                    if truth(&i["completed"]) { "[x]" } else { "[ ]" },
                    short(s(i, "text"), 60)
                ))
                .collect::<Vec<_>>()
                .join(" | ")
        ),
        "collab_tool_call" if started => format!("agent {}", getstr(it, "tool")),
        "error" => format!("warning: {}", short(s(it, "message"), 300)),
        _ => return None,
    };
    Some(line)
}
/// Progress lines ('HH:MM:SS text') for raw events.jsonl lines; odd or half-written lines are skipped.
pub fn event_lines<'a>(raws: impl IntoIterator<Item = &'a [u8]>) -> Vec<String> {
    raws.into_iter()
        .filter_map(|raw| {
            let ev: Value = serde_json::from_slice(raw).ok()?;
            let line = describe(&ev)?;
            let ts = s(&ev, "_ts");
            Some(if ts.is_empty() {
                line
            } else {
                format!("{} {line}", ts.chars().skip(11).take(8).collect::<String>())
            })
        })
        .collect()
}
/// Short 'what is it doing now' text for the status table.
pub fn activity(t: &str, it: &Value) -> Option<String> {
    let kind = s(it, "type");
    let started = t == "item.started";
    if t == "item.completed" && TOOL_KINDS.contains(&kind) {
        return Some("thinking".into());
    }
    Some(match kind {
        "command_execution" => {
            if started {
                format!("running: {}", short(&clean_cmd(s(it, "command")), 150))
            } else {
                "thinking".into()
            }
        }
        "mcp_tool_call" => {
            if started {
                format!("mcp: {}.{}", getstr(it, "server"), getstr(it, "tool"))
            } else {
                "thinking".into()
            }
        }
        "file_change" => format!(
            "editing: {}",
            array(&it["changes"])
                .iter()
                .map(|c| getstr(c, "path"))
                .collect::<Vec<_>>()
                .join(", ")
        ),
        "web_search" => format!("searching: {}", short(s(it, "query"), 150)),
        "collab_tool_call" => format!("agent: {}", getstr(it, "tool")),
        "todo_list" => format!(
            "plan {}/{}",
            array(&it["items"])
                .iter()
                .filter(|i| truth(&i["completed"]))
                .count(),
            array(&it["items"]).len()
        ),
        "reasoning" => "thinking".into(),
        "agent_message" => format!("said: {}", short(s(it, "text"), 150)),
        _ => return None,
    })
}
pub fn usage_tokens(u: &Value) -> Value {
    let mut v = json!({});
    for k in USAGE_KEYS {
        v[*k] = json!(number(u, k));
    }
    v["total"] = json!(number(&v, "input_tokens") + number(&v, "output_tokens"));
    v
}
/// Fold one codex JSONL event into job.json fields.
pub fn apply_event(job: &mut Value, ev: &Value) {
    let t = s(ev, "type");
    match t {
        "thread.started" => job["thread_id"] = ev["thread_id"].clone(),
        "turn.started" => {
            job["activity"] = json!("thinking");
            job["activity_detail"] = Value::Null;
        }
        "turn.completed" => {
            job["tokens"] = usage_tokens(&ev["usage"]);
            job["turn_completed"] = json!(true);
        }
        "turn.failed" => {
            let e = err_text(&ev["error"]);
            job["last_error"] = json!(if e.is_empty() { "作業エラー" } else { &e });
        }
        "error" => {
            job["last_error"] = json!(if s(ev, "message").is_empty() {
                "作業エラー"
            } else {
                s(ev, "message")
            });
        }
        _ if t.starts_with("item.") => {
            let it = &ev["item"];
            if !it.is_object() {
                return;
            }
            let kind = s(it, "type");
            let done = t == "item.completed";
            if done && TOOL_KINDS.contains(&kind) {
                job["steps"] = json!(number(job, "steps") + 1);
            }
            if done && kind == "agent_message" {
                job["last_message"] = json!(s(it, "text"));
            }
            if let Some(act) = activity(t, it) {
                job["activity"] = json!(act);
                let detail = if done && TOOL_KINDS.contains(&kind) {
                    String::new()
                } else {
                    match kind {
                        "command_execution" if t == "item.started" => clean_cmd(s(it, "command")),
                        "web_search" => s(it, "query").into(),
                        "agent_message" => s(it, "text").into(),
                        "file_change" => array(&it["changes"])
                            .iter()
                            .map(|c| getstr(c, "path"))
                            .collect::<Vec<_>>()
                            .join(", "),
                        "mcp_tool_call" | "collab_tool_call" => {
                            format!("{} {}", s(it, "server"), s(it, "tool"))
                        }
                        _ => String::new(),
                    }
                };
                job["activity_detail"] = if detail.is_empty() {
                    Value::Null
                } else {
                    json!(detail)
                };
            }
        }
        _ => (),
    }
}
/// Fold a `token_count` entry of codex's session log (live usage, context and rate-limit use).
pub fn apply_token_count(job: &mut Value, p: &Value) {
    let info = &p["info"];
    if truth(&info["total_token_usage"]) {
        job["tokens"] = usage_tokens(&info["total_token_usage"]);
    }
    if let (Some(w), Some(last)) = (
        info["model_context_window"].as_f64(),
        info["last_token_usage"]["total_tokens"].as_f64(),
    ) && w != 0.
    {
        job["context_pct"] = json!((100. * last / w).round_ties_even() as i64);
    }
    if !p["rate_limits"]["primary"]["used_percent"].is_null() {
        job["rate_limit_pct"] = p["rate_limits"]["primary"]["used_percent"].clone();
    }
}
pub fn finish(job: &mut Value, exit: Option<i64>, duration: i64, now: &str) {
    let ok = exit == Some(0) && truth(&job["turn_completed"]);
    job["status"] = json!(if ok { "succeeded" } else { "failed" });
    job["exit_code"] = json!(exit);
    job["finished_at"] = json!(now);
    job["duration_ms"] = json!(duration);
    if ok {
        job["activity"] = json!(format!("done: {}", short(s(job, "last_message"), 150)));
    } else {
        if !truth(&job["last_error"]) {
            job["last_error"] = json!(format!(
                "codex exited with {} without turn.completed",
                exit.map(|x| x.to_string()).unwrap_or("None".into())
            ));
        }
        job["activity"] = json!(format!("failed: {}", short(s(job, "last_error"), 150)));
    }
}
/// Keep credentials out of local diagnostic logs.
pub fn redact_local_log_secrets(text: &str, env: &BTreeMap<String, String>) -> String {
    LogRedactor::new(env).redact(text)
}
/// Cache environment and compiled patterns once per stdout/stderr pump.
pub struct LogRedactor {
    secrets: Vec<Regex>,
}
impl LogRedactor {
    pub fn new(env: &BTreeMap<String, String>) -> Self {
        let names = [
            "GODD_DISCORD_BOT_TOKEN",
            "GODD_DISCORD_WEBHOOK_URL",
            "DISCORD_WEBHOOK_URL",
            "CODEX_JOBS_DISCORD_WEBHOOK",
        ];
        Self {
            secrets: env
                .iter()
                .filter(|(name, value)| {
                    !value.is_empty() && names.iter().any(|n| name.eq_ignore_ascii_case(n))
                })
                .map(|(_, value)| Regex::new(&format!("(?i){}", regex::escape(value))).unwrap())
                .collect(),
        }
    }
    pub fn redact(&self, text: &str) -> String {
        static RE: OnceLock<Regex> = OnceLock::new();
        let mut text = RE
            .get_or_init(|| {
                Regex::new(r#"(?i)https://(?:discord\.com|discordapp\.com)/api/webhooks/[^\s"']+"#)
                    .unwrap()
            })
            .replace_all(text, "[秘密]")
            .into_owned();
        for secret in &self.secrets {
            text = secret.replace_all(&text, "[秘密]").into_owned();
        }
        text
    }
    pub fn event(&self, v: &Value) -> Value {
        match v {
            Value::String(s) => json!(self.redact(s)),
            Value::Array(a) => Value::Array(a.iter().map(|v| self.event(v)).collect()),
            Value::Object(o) => Value::Object(
                o.iter()
                    .map(|(k, v)| (self.redact(k), self.event(v)))
                    .collect(),
            ),
            _ => v.clone(),
        }
    }
}
pub fn redact_log_event(v: &Value, env: &BTreeMap<String, String>) -> Value {
    LogRedactor::new(env).event(v)
}
/// Copy codex stdout to events.jsonl, stamping each event with its arrival time, and queue parsed events.
pub fn pump(
    stream: impl std::io::Read,
    path: &Path,
    rt: &dyn Runtime,
    mut send: impl FnMut(Value),
) -> std::io::Result<()> {
    use std::io::{BufRead, Write};
    let mut f = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)?;
    let mut reader = std::io::BufReader::new(stream);
    let redactor = LogRedactor::new(&rt.env());
    loop {
        let mut raw = vec![];
        if reader.read_until(b'\n', &mut raw)? == 0 {
            break;
        }
        if let Ok(mut ev) = serde_json::from_slice::<Value>(&raw).filter_object() {
            let mut stamped = json!({"_ts":rt.now()});
            stamped["_filetime"] = json!(((rt.epoch() + 11_644_473_600.) * 10_000_000.) as u64);
            for (k, v) in ev.as_object_mut().unwrap().iter() {
                stamped[k] = v.clone();
            }
            writeln!(f, "{}", compact(&redactor.event(&stamped)))?;
            f.flush()?;
            send(stamped.clone());
        } else {
            f.write_all(redactor.redact(&String::from_utf8_lossy(&raw)).as_bytes())?;
        }
        f.flush()?;
    }
    Ok(())
}
trait ObjectResult {
    fn filter_object(self) -> std::result::Result<Value, ()>;
}
impl ObjectResult for std::result::Result<Value, serde_json::Error> {
    fn filter_object(self) -> std::result::Result<Value, ()> {
        self.map_err(|_| ())
            .and_then(|v| if v.is_object() { Ok(v) } else { Err(()) })
    }
}
/// Keep the child stderr log redacted while draining its pipe.
pub fn pump_stderr(
    stream: impl std::io::Read,
    path: &Path,
    rt: &dyn Runtime,
) -> std::io::Result<()> {
    use std::io::{BufRead, Write};
    let mut f = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)?;
    let mut reader = std::io::BufReader::new(stream);
    let redactor = LogRedactor::new(&rt.env());
    loop {
        let mut raw = vec![];
        if reader.read_until(b'\n', &mut raw)? == 0 {
            break;
        }
        f.write_all(redactor.redact(&String::from_utf8_lossy(&raw)).as_bytes())?;
        f.flush()?;
    }
    Ok(())
}
