//! Claude's raw creation tools cannot participate in the CUA tab ledger.
use crate::*;
const COMMAND: &str = "D:/CC/Tools/owner-chrome/owner_chrome.exe guard";
const MATCHER: &str = ".*tabs_create_mcp|.*tabs_context_mcp|.*navigate";

pub fn decision(event: &Value) -> Option<Value> {
    let tool = event["tool_name"].as_str().unwrap_or("");
    if tool.ends_with("navigate") && event["tool_input"]["url"].as_str().is_some_and(registrar::restricted_url) {
        return Some(json!({"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":registrar::REASON}}));
    }
    let creates = tool.ends_with("tabs_create_mcp")
        || (tool.ends_with("tabs_context_mcp") && event["tool_input"]["createIfEmpty"] == true);
    creates.then(||json!({"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Chromeのタブは作成記録が必要です。CC_JOB_IDを使い D:/CC/Tools/owner-chrome/owner_chrome.exe open <ジョブID> <URL> で作成してください。ジョブがなければブラウザ作業をConsoleへ委譲してください。既存タブの読み取り・操作はそのまま利用できます。"}}))
}
pub fn install(home: &Path) -> io::Result<bool> {
    let path = home.join(".claude/settings.json");
    let mut settings: Value = match fs::read(&path) {
        Ok(bytes) => serde_json::from_slice(&bytes).map_err(io::Error::other)?,
        Err(e) if e.kind() == io::ErrorKind::NotFound => json!({}),
        Err(e) => return Err(e),
    };
    if settings["hooks"].is_null() {
        settings["hooks"] = json!({});
    }
    if settings["hooks"]["PreToolUse"].is_null() {
        settings["hooks"]["PreToolUse"] = json!([]);
    }
    let hooks = settings["hooks"]["PreToolUse"]
        .as_array_mut()
        .ok_or_else(|| io::Error::other("invalid Claude hooks"))?;
    let owned: Vec<_> = hooks.iter().filter(|h| {
        h["hooks"]
            .as_array()
            .is_some_and(|items| items.iter().any(|i| i["command"] == COMMAND))
    }).collect();
    if owned.len() == 1 && owned[0]["matcher"] == MATCHER
        && owned[0]["hooks"].as_array().is_some_and(|items| items.len() == 1)
    {
        return Ok(false);
    }
    // Split our command from shared hook entries without changing the matcher
    // or metadata of any unrelated command.
    let mut retained = Vec::new();
    for mut hook in std::mem::take(hooks) {
        if let Some(items) = hook["hooks"].as_array_mut() {
            let before = items.len();
            items.retain(|item| item["command"] != COMMAND);
            if before > 0 && items.is_empty() {
                continue;
            }
        }
        retained.push(hook);
    }
    *hooks = retained;
    hooks.push(json!({"matcher":MATCHER,"hooks":[{"type":"command","command":COMMAND,"timeout":5}]}));
    lifecycle::atomic_json(&path, &settings)?;
    Ok(true)
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn direct_and_implicit_creation_must_use_ledger_but_existing_tabs_are_usable() {
        assert!(decision(&json!({"tool_name":"mcp__claude-in-chrome__tabs_create_mcp"})).is_some());
        assert!(decision(&json!({"tool_name":"mcp__claude-in-chrome__tabs_context_mcp","tool_input":{"createIfEmpty":true}})).is_some());
        assert!(decision(&json!({"tool_name":"mcp__claude-in-chrome__tabs_context_mcp","tool_input":{"createIfEmpty":false}})).is_none());
        assert!(decision(&json!({"tool_name":"mcp__claude-in-chrome__navigate"})).is_none());
        assert!(decision(&json!({"tool_name":"mcp__claude-in-chrome__navigate","tool_input":{"url":"https://navi.onamae.com"}})).is_some());
        assert!(decision(&json!({"tool_name":"mcp__claude-in-chrome__navigate","tool_input":{"url":"https://rakko-domain.com"}})).is_none());
    }
    #[test]
    fn installation_preserves_other_hooks_and_settings_and_is_idempotent() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join(".claude/settings.json");
        lifecycle::atomic_json(&path,&json!({"custom":"unchanged","hooks":{"Stop":[{"hooks":[{"command":"original"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[]}]}})).unwrap();
        assert!(install(root.path()).unwrap());
        assert!(!install(root.path()).unwrap());
        let settings: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
        assert_eq!(settings["custom"], "unchanged");
        assert_eq!(
            settings["hooks"]["Stop"][0]["hooks"][0]["command"],
            "original"
        );
        assert_eq!(settings["hooks"]["PreToolUse"].as_array().unwrap().len(), 2);
    }
    #[test]
    fn upgrading_a_shared_hook_keeps_other_commands_on_their_original_matcher() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join(".claude/settings.json");
        lifecycle::atomic_json(&path, &json!({"hooks":{"PreToolUse":[{"matcher":"Bash","custom":"keep","hooks":[{"type":"command","command":COMMAND,"timeout":5},{"type":"command","command":"other","timeout":42}]}]}})).unwrap();
        assert!(install(root.path()).unwrap());
        assert!(!install(root.path()).unwrap());
        let settings: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
        assert_eq!(settings["hooks"]["PreToolUse"][0], json!({"matcher":"Bash","custom":"keep","hooks":[{"type":"command","command":"other","timeout":42}]}));
        assert_eq!(settings["hooks"]["PreToolUse"][1]["matcher"], MATCHER);
    }
}
