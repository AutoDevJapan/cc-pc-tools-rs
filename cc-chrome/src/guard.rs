//! Claude's raw creation tools cannot participate in the CUA tab ledger.
use crate::*;
const COMMAND: &str = "D:/CC/Tools/owner-chrome/owner_chrome.exe guard";

pub fn decision(event: &Value) -> Option<Value> {
    let tool = event["tool_name"].as_str().unwrap_or("");
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
    if hooks.iter().any(|h| {
        h["hooks"]
            .as_array()
            .is_some_and(|items| items.iter().any(|i| i["command"] == COMMAND))
    }) {
        return Ok(false);
    }
    hooks.push(json!({"matcher":".*tabs_create_mcp|.*tabs_context_mcp","hooks":[{"type":"command","command":COMMAND,"timeout":5}]}));
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
}
