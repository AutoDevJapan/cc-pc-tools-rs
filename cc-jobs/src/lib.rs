//! codex-jobs: run `codex exec --json` jobs in the background and watch their progress.
//!
//! Usage: codex_jobs.exe {run,status,tail,wait,watch,cancel,rate,backfill} ...
//! Windows only (codex.CMD shim, taskkill, OpenProcess).
pub mod boundary;
pub mod cache;
pub mod cli;
pub mod events;
pub mod gate;
pub mod jobs;
pub mod notify;
mod pause;
pub mod platform;
pub mod queue;
pub mod recovery;
pub use events::*;
pub use jobs::*;
pub use platform::*;
pub use serde_json::{Value, json};

pub const LIVE: &[&str] = &["queued", "running"];
pub const TERMINAL: &[&str] = &["succeeded", "failed", "cancelled", "lost"];
pub const TOOL_KINDS: &[&str] = &[
    "command_execution",
    "file_change",
    "mcp_tool_call",
    "web_search",
    "collab_tool_call",
];
pub const USAGE_KEYS: &[&str] = &[
    "input_tokens",
    "cached_input_tokens",
    "cache_write_input_tokens",
    "output_tokens",
    "reasoning_output_tokens",
];
pub const NO_WINDOW: u32 = 0x08000000;
pub const QUEUED_GRACE_S: f64 = 60.;
pub const SESSION_POLL_S: f64 = 3.;
pub const EXIT_DRAIN_S: f64 = 3.;
pub fn s<'a>(v: &'a Value, key: &str) -> &'a str {
    v.get(key).and_then(Value::as_str).unwrap_or("")
}
pub fn number(v: &Value, key: &str) -> i64 {
    v.get(key).and_then(Value::as_i64).unwrap_or(0)
}
pub fn truth(v: &Value) -> bool {
    match v {
        Value::Null => false,
        Value::Bool(b) => *b,
        Value::Number(n) => n.as_f64() != Some(0.),
        Value::String(s) => !s.is_empty(),
        Value::Array(a) => !a.is_empty(),
        Value::Object(o) => !o.is_empty(),
    }
}
pub fn pystr(v: &Value) -> String {
    match v {
        Value::Null => "None".into(),
        Value::Bool(true) => "True".into(),
        Value::Bool(false) => "False".into(),
        Value::String(s) => s.clone(),
        _ => v.to_string(),
    }
}
pub fn getstr(v: &Value, key: &str) -> String {
    pystr(&v[key])
}
pub fn pretty(v: &Value) -> String {
    let mut b = Vec::new();
    let f = serde_json::ser::PrettyFormatter::with_indent(b" ");
    let mut ser = serde_json::Serializer::with_formatter(&mut b, f);
    serde::Serialize::serialize(v, &mut ser).unwrap();
    String::from_utf8(b).unwrap()
}
pub fn compact(v: &Value) -> String {
    // Python's default separators, without changing strings.
    let raw = v.to_string();
    let mut out = String::new();
    let (mut quoted, mut esc) = (false, false);
    for c in raw.chars() {
        out.push(c);
        if quoted {
            if esc {
                esc = false;
            } else if c == '\\' {
                esc = true;
            } else if c == '"' {
                quoted = false;
            }
        } else if c == '"' {
            quoted = true;
        } else if c == ',' || c == ':' {
            out.push(' ');
        }
    }
    out
}
#[derive(Debug, Clone)]
pub struct Error {
    pub code: i32,
    pub message: String,
}
impl Error {
    pub fn new(m: impl Into<String>) -> Self {
        Self {
            code: 1,
            message: m.into(),
        }
    }
}
impl From<std::io::Error> for Error {
    fn from(e: std::io::Error) -> Self {
        Self::new(e.to_string())
    }
}
impl From<serde_json::Error> for Error {
    fn from(e: serde_json::Error) -> Self {
        Self::new(e.to_string())
    }
}
pub type Result<T> = std::result::Result<T, Error>;
