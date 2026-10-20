//! Per-question qualification is independent of general resource classification.
use crate::core::{Context, Effects, Result};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::path::Path;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct Scope {
    pub todo_id: u64,
    pub question_sha256: String,
    pub decision_sha256: String,
    pub owner_id: String,
    pub guild_id: String,
    pub forum_id: String,
}

fn sha(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

impl Scope {
    pub fn from_decision(todo: u64, body: &str, decision: &Value) -> Option<Self> {
        Some(Self {
            todo_id: todo,
            question_sha256: sha(body.as_bytes()),
            decision_sha256: sha(crate::conversation_bridge::canonical(decision)
                .to_string()
                .as_bytes()),
            owner_id: decision.get("owner")?.as_str()?.into(),
            guild_id: decision.get("guild")?.as_str()?.into(),
            forum_id: decision.get("parent")?.as_str()?.into(),
        })
    }
    pub fn for_ask(ctx: &Context, todo: u64, body: &str, decision: &Value) -> Self {
        Self {
            todo_id: todo,
            question_sha256: sha(body.as_bytes()),
            decision_sha256: sha(crate::conversation_bridge::canonical(decision)
                .to_string()
                .as_bytes()),
            owner_id: ctx.owner(),
            guild_id: crate::core::s(&ctx.cfg["guild_id"]).into(),
            forum_id: crate::core::s(&ctx.cfg["task_channel_id"]).into(),
        }
    }
    pub fn append_args(&self, args: &mut Vec<String>) {
        args.extend([
            "--question-todo-id".into(),
            self.todo_id.to_string(),
            "--question-sha256".into(),
            self.question_sha256.clone(),
            "--decision-sha256".into(),
            self.decision_sha256.clone(),
            "--question-owner".into(),
            self.owner_id.clone(),
            "--question-guild".into(),
            self.guild_id.clone(),
            "--question-forum".into(),
            self.forum_id.clone(),
        ]);
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct Qualification {
    pub domain: String,
    pub qualified: bool,
    pub scope: Scope,
    pub sha256: String,
    pub size: u64,
    pub mime: String,
    pub extension: String,
}

impl Qualification {
    pub fn matches(&self, scope: &Scope) -> bool {
        self.domain == "owner-generated-question-image-v1"
            && self.qualified
            && self.scope == *scope
            && self.sha256.len() == 64
            && self
                .sha256
                .bytes()
                .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
            && self.size > 0
            && self.size <= 10 * 1024 * 1024
            && self.mime == "image/png"
            && self.extension == "png"
    }
}

pub(crate) fn preflight(
    effects: &mut dyn Effects,
    path: &Path,
    scope: &Scope,
) -> Result<Qualification> {
    const FAILED: &str = "確認画像の作成証拠と質問の資格を確認できません。画像は送信していません";
    let mut args = vec![
        "D:/CC/Tools/discord-ask/unattended-runner.exe".into(),
        "--qualify-generated-question-media".into(),
        "--path".into(),
        path.to_str().ok_or(FAILED)?.into(),
    ];
    scope.append_args(&mut args);
    let output = effects.run(&args, 20).map_err(|_| FAILED)?;
    if output.code != 0 || output.stdout.len() > 64 * 1024 {
        return Err(FAILED.into());
    }
    let proof: Qualification = serde_json::from_str(&output.stdout).map_err(|_| FAILED)?;
    if !proof.matches(scope) {
        return Err(FAILED.into());
    };
    Ok(proof)
}
