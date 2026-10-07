//! Stop at the existing idle tool boundary, summarize in the old session, then
//! start a fresh session while preserving job/ToDo ownership and permissions.
use crate::*;
use std::{collections::HashSet, fs};

const SECTIONS: [&str; 6] = [
    "## 目的",
    "## 終わったこと",
    "## 残り",
    "## 次の一手",
    "## 関係ファイルとPR",
    "## 最新の指示と注意点",
];

impl Context {
    pub(crate) fn context_limit_reached(&self, job: &Value) -> bool {
        let Ok(cfg) = self.queue_config() else {
            return false;
        };
        if s(job, "status") != "running"
            || job["cancel_requested"] == true
            || s(job, "context_phase") == "summarize"
            || normalize_uuid(s(job, "thread_id")).is_none()
            || !job["context_pct_exact"]
                .as_f64()
                .or_else(|| job["context_pct"].as_f64())
                .is_some_and(|pct| pct > f64::from(cfg.context_limit_percent))
        {
            return false;
        }
        true
    }
    pub(crate) fn context_rotation_due(&self, job: &Value, jobs: &[Value]) -> bool {
        if !self.context_limit_reached(job) {
            return false;
        }
        // A child may rotate, but no running/queued descendants may be lost.
        !jobs.iter().any(|other| {
            if s(other, "id") == s(job, "id") {
                return false;
            }
            if other["queue_unreadable"] == true {
                return true;
            }
            if !(LIVE.contains(&s(other, "status")) || self.is_ours(other, "codex_pid")) {
                return false;
            }
            if crate::queue::same_task(job, other) {
                return true;
            }
            let mut parent = s(other, "parent_job_id");
            let mut seen = HashSet::new();
            while !parent.is_empty() && seen.insert(parent) {
                if parent == s(job, "id") {
                    return true;
                }
                let Some(ancestor) = jobs.iter().find(|j| s(j, "id") == parent) else {
                    return true;
                };
                parent = s(ancestor, "parent_job_id");
            }
            false
        })
    }

    pub(crate) fn prepare_context_summary(&self, job: &mut Value, session: &str) -> Result<()> {
        let servers = self
            .rt
            .summary_mcp_servers(s(job, "codex"), std::path::Path::new(s(job, "cwd")))?;
        let dir = self.job_dir(s(job, "id"));
        let prior = fs::read_to_string(dir.join("yield-resume.md"))?;
        fs::write(
            dir.join("context-summary-prompt.md"),
            format!(
                "文脈の使用率が上限を超えたので、新しいセッションへ引き継ぎます。実装や外部操作を続けず、これまでの文脈を使い引き継ぎだけを書いて終了してください。ツールやコマンドは実行しないでください。出力は24KB以内。次の見出しをすべて使用し各節に具体的な内容を書いてください（該当なしなら『なし』）。目的・受入条件、終わった操作と検証結果、未完了と判断待ち、次に実行する一手、絶対ファイルパスとPR URL、最新のオーナー指示・権限・未コミット変更・送信済み操作を必ず含めます。新しいセッションでは旧文脈を参照できません。\n{}\n\n停止前の記録:\n{prior}",
                SECTIONS.join("\n")
            ),
        )?;
        job["context_phase"] = json!("summarize");
        job["context_disabled_mcp_servers"] = json!(servers);
        job["context_source_session"] = json!(session);
        job["context_source_pct"] = job["context_pct"].clone();
        job["activity"] = json!("順番待ち: 新しいセッションへの引き継ぎを作成します");
        Ok(())
    }

    /// The caller holds queue and state locks after the summary process exits.
    pub(crate) fn complete_context_summary(&self, job: &mut Value) -> Result<bool> {
        if s(job, "context_phase") != "summarize" || s(job, "status") != "succeeded" {
            return Ok(false);
        }
        let summary = s(job, "last_message");
        if !valid_summary(summary) {
            job["status"] = json!("failed");
            job["last_error"] = json!(
                "context handoff summary is incomplete or exceeds 24KB; original session retained"
            );
            return Ok(false);
        }
        let summary = redact_local_log_secrets(summary, &self.rt.env());
        let dir = self.job_dir(s(job, "id"));
        // Write durable evidence and the next prompt before clearing the old ID.
        fs::write(dir.join("context-handoff.md"), &summary)?;
        fs::write(
            dir.join("context-continue.md"),
            format!(
                "同じ共有ToDoの新しいセッションです。次の引き継ぎを読み、残りと次の一手から作業を続けてください。完了済みのコマンド・送信・委任は繰り返さないでください。元の依頼全文は {} にあります。受入条件を先に読み、引き継ぎの最新の指示と合わせて満たしてください。作業フォルダと実行範囲は維持されています。\n\n{summary}\n",
                dir.join("prompt.md").display()
            ),
        )?;
        let session = s(job, "context_source_session").to_string();
        crate::queue::prepare_resume(job, &session, &self.rt.now());
        job["resume_session"] = Value::Null;
        job["thread_id"] = Value::Null;
        job["context_pct"] = Value::Null;
        job["context_pct_exact"] = Value::Null;
        job["tokens"] = usage_tokens(&json!({}));
        job["context_phase"] = json!("continue");
        job["yield_resume"] = json!(false);
        job["context_rotation_count"] = json!(number(job, "context_rotation_count") + 1);
        job["context_rotated_at"] = json!(self.rt.now());
        job["queue_sequence"] = json!(self.next_queue_sequence()?);
        job["retry_not_before"] = Value::Null;
        job["activity"] = json!("順番待ち: 引き継ぎから新しいセッションで続けます");
        Ok(true)
    }
}

fn valid_summary(text: &str) -> bool {
    !text.is_empty()
        && text.len() <= 24 * 1024
        && SECTIONS.iter().all(|section| {
            text.split_once(section).is_some_and(|(_, body)| {
                !body.split("\n## ").next().unwrap_or("").trim().is_empty()
            })
        })
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn incomplete_and_oversized_summaries_cannot_discard_the_old_context() {
        assert!(!valid_summary("目的だけ"));
        let valid = SECTIONS
            .iter()
            .map(|s| format!("{s}\n具体的な引き継ぎ\n"))
            .collect::<String>();
        assert!(valid_summary(&valid));
        assert!(!valid_summary(&format!("{valid}{}", "あ".repeat(9000))));
        assert!(!valid_summary(&SECTIONS.join("\n")));
    }
}
