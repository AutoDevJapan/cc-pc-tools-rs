//! Bounded calls to the PC's loopback Ollama for light text work.
//! llm-jp is the main model; gemma3:4b only runs when llm-jp gives no usable
//! answer. Callers always keep a deterministic fallback for `None`.
use serde_json::{json, Value};
use std::time::{Duration, Instant};

pub const MAIN: &str = "llm-jp-4-8b-thinking:latest";
pub const FALLBACK: &str = "gemma3:4b";
const URL: &str = "http://127.0.0.1:11434/api/generate";

/// Loopback only: no remote endpoint, credentials or redirects.
fn url() -> String {
    std::env::var("CC_LOCAL_LLM_URL")
        .ok()
        .filter(|u| u.starts_with("http://127.0.0.1:") && u.ends_with("/api/generate"))
        .unwrap_or_else(|| URL.into())
}

/// The answer part of a reply. llm-jp emits harmony channels even with
/// `think:false`; a reply cut off before its final channel is unusable.
pub fn final_answer(raw: &str) -> Option<String> {
    let text = if raw.contains("<|channel|>") {
        let (_, tail) = raw.rsplit_once("final<|message|>")?;
        tail.split("<|end|>").next()?.split("<|return|>").next()?
    } else if let Some((_, tail)) = raw.rsplit_once("</think>") {
        tail
    } else if raw.contains("<think>") {
        return None;
    } else {
        raw
    };
    let text = text.trim();
    (!text.is_empty()).then(|| text.to_owned())
}

pub fn generate(
    model: &str,
    prompt: &str,
    timeout: Duration,
    max_tokens: u32,
) -> Result<String, String> {
    let agent = ureq::AgentBuilder::new()
        .timeout_connect(timeout.min(Duration::from_secs(2)))
        .timeout(timeout)
        .redirects(0)
        .build();
    let body = json!({"model":model,"prompt":prompt,"stream":false,"think":false,
        "options":{"temperature":0,"num_predict":max_tokens}});
    let value: Value = agent
        .post(&url())
        .send_json(body)
        .map_err(|e| e.to_string())?
        .into_json()
        .map_err(|e| e.to_string())?;
    value["response"]
        .as_str()
        .and_then(final_answer)
        .ok_or_else(|| "no final answer".into())
}

/// Try llm-jp, then gemma3:4b, inside one total budget. `accept` validates and
/// converts the answer; a rejected answer counts as "llm-jp was not enough".
pub fn ask<T>(
    prompt: &str,
    budget: Duration,
    max_tokens: u32,
    accept: impl Fn(&str) -> Option<T>,
) -> Option<T> {
    ask_with(
        budget,
        |model, left| generate(model, prompt, left, max_tokens).ok(),
        accept,
    )
}

pub fn ask_with<T>(
    budget: Duration,
    mut call: impl FnMut(&str, Duration) -> Option<String>,
    accept: impl Fn(&str) -> Option<T>,
) -> Option<T> {
    let deadline = Instant::now() + budget;
    for model in [MAIN, FALLBACK] {
        let left = deadline.saturating_duration_since(Instant::now());
        if left < Duration::from_secs(1) {
            return None;
        }
        if let Some(found) = call(model, left).as_deref().and_then(&accept) {
            return Some(found);
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn extracts_final_channel_and_rejects_cut_off_thinking() {
        let raw = "<|channel|> analysis<|message|> think<|end|><|start|> assistant<|channel|> final<|message|> PR #118 の指摘を直す<|end|>";
        assert_eq!(final_answer(raw).as_deref(), Some("PR #118 の指摘を直す"));
        assert_eq!(
            final_answer("<|channel|> analysis<|message|> still thinking"),
            None
        );
        assert_eq!(
            final_answer("<think>x</think>\n答え").as_deref(),
            Some("答え")
        );
        assert_eq!(final_answer("<think>never closed"), None);
        assert_eq!(final_answer("  素の答え \n").as_deref(), Some("素の答え"));
    }
    #[test]
    fn falls_back_to_gemma_only_when_llm_jp_is_not_enough() {
        let mut seen = vec![];
        let got = ask_with(
            Duration::from_secs(10),
            |m, _| {
                seen.push(m.to_owned());
                Some(if m == MAIN {
                    "english".into()
                } else {
                    "日本語".into()
                })
            },
            |a| a.contains('日').then(|| a.to_owned()),
        );
        assert_eq!((got.as_deref(), seen.len()), (Some("日本語"), 2));
        let mut calls = 0;
        assert_eq!(
            ask_with(
                Duration::from_secs(10),
                |_, _| {
                    calls += 1;
                    Some("ok".into())
                },
                |a| Some(a.len())
            ),
            Some(2)
        );
        assert_eq!(calls, 1);
        assert_eq!(
            ask_with(
                Duration::ZERO,
                |_, _| panic!("no budget"),
                |a| Some(a.len())
            ),
            None
        );
    }
    #[test]
    fn unreachable_server_fails_fast() {
        // Port 9 (discard) is closed on this PC; the call must error within the bound.
        std::env::set_var("CC_LOCAL_LLM_URL", "http://127.0.0.1:9/api/generate");
        let start = Instant::now();
        assert!(generate(MAIN, "x", Duration::from_secs(3), 8).is_err());
        assert!(start.elapsed() < Duration::from_secs(4));
    }
}
