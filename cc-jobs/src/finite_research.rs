//! One original research request may submit its finite operations to normal review.
//! This grants no operation, writable sandbox, network flag or general reader exception.
use crate::*;

const SPEC: &str = "D:/CC/Tools/todo/specs/2939-839caa665f5640c9ba4f44ede34d420d.md";
const SPEC_HASH: &str = "2fce874d23c72128dbdec3b599ecc96a9a98f767c2466bb1387f297ab393f5b6";
const TREE: &str = "D:/CC/Work/unattended-operations-20261013-wt/todo-2939-sdk";
const SOURCE: &str = "20261020-122536-todo-3047-dispatch-repair";
const START: u64 = 1792466736;
const END: u64 = 1792627200; // 2026-10-22 00:00 UTC

fn same(a: &Value, b: &str) -> bool {
    a.as_str()
        .is_some_and(|a| a.replace('\\', "/").eq_ignore_ascii_case(b))
}
fn binding(job: &Value, row: &Value, hash: &str, at: u64) -> Option<Value> {
    let scheduler = &row["scheduler"];
    let key = s(job, "launch_key");
    let linked = scheduler["key"] == job["launch_key"]
        || (!s(job, "id").is_empty()
            && scheduler["job"] == job["id"]
            && row["jobs"]
                .as_array()
                .is_some_and(|jobs| jobs.contains(&job["id"])));
    if !(START..END).contains(&at)
        || hash != SPEC_HASH
        || job["todo_id"] != 2939
        || row["id"] != 2939
        || row["project"] != "cc-pc-tools-rs"
        || !["todo", "doing"].contains(&s(row, "status"))
        || row["dispatchable"] != true
        || job["subwork_validated"] != true
        || job["subwork"] != row["subwork"]
        || job["subwork"]["parent_todo_id"] != 2757
        || job["subwork"]["kind"] != "research"
        || job["subwork"]["owner"]["repository"] != "autodevjapan/cc-pc-tools-rs"
        || !same(&job["subwork_spec"], SPEC)
        || !same(&row["spec"], SPEC)
        || !same(&job["cwd"], TREE)
        || !same(&scheduler["worktree"], TREE)
        || key.len() != 32
        || !key.bytes().all(|b| b.is_ascii_hexdigit())
        || !linked
        || scheduler["attempt"] != 175
        || job["assigned_needs"] != json!([])
        || row["needs"] != json!([])
        || s(job, "model").is_empty()
        || job["model"] != scheduler["selection"]["model"]
        || job["effort"] != scheduler["selection"]["effort"]
        || job["sandbox"] != "read-only"
        || job["network"] != false
        || row["stopped"] == true
        || row["owner_stopped"] == true
        || truth(&scheduler["question"])
        || truth(&scheduler["owner_stop_job"])
        || row["stall_recovery"]["pending"].is_object()
        || row["wait_on"].as_array().is_some_and(|ids| !ids.is_empty())
        || row["processed_results"].get(s(job, "id")).is_some()
    {
        return None;
    }
    Some(
        json!({"version":1,"todo_id":2939,"parent_todo_id":2757,"attempt":175,
        "spec_sha256":SPEC_HASH,"source_job":SOURCE,"expires":END,"launch_key":key,
        "subwork":job["subwork"],"cwd":TREE,"model":job["model"],"effort":job["effort"],
        "sandbox":"read-only","network":false,"approval_policy":"on-request","approvals_reviewer":"auto_review"}),
    )
}

pub(crate) const GUIDANCE: &str = "\n\n## 元ToDo2939の有限な審査提出\n2026-10-20の元ToDo3047の本人依頼に基づき、元2939の次の1試行だけ通常の自動審査へ提出できます。sandboxはread-only、networkはfalseのままです。これは操作の許可ではありません。原仕様の固定gh_gate GET、専用worktreeへの匿名モデル/照合票保存、原仕様の資源制約を守った所有Dockerでの工具版照合だけをrequire_escalatedで通常審査へ提出してください。拒否は守り、別API/ref/認証/経路で迂回せず、再拒否の反復をしないで原失敗を返してください。先行の一般的な閲覧guidanceの編集禁止に対する例外は、この原仕様の匿名出力の新規保存だけです。原source・設定・他担当のソース・他ToDo状態・秘密の保存や変更、配備、sandbox/network設定の変更は許可されません。審査が拒否した項目は具体的に未確認として返し、実行したと報告しないでください。\n";

impl Context {
    pub(crate) fn research_qualification(&self, job: &Value, db: &Value) -> Result<Option<Value>> {
        if job["todo_id"] != 2939 {
            return Ok(None);
        }
        let Some(row) = db["items"]
            .as_array()
            .into_iter()
            .flatten()
            .find(|row| row["id"] == 2939)
        else {
            return Ok(None);
        };
        // First screen all metadata with the known digest; never open an input-supplied path.
        let at = self.rt.epoch();
        if !at.is_finite() || binding(job, row, SPEC_HASH, at as u64).is_none() {
            return Ok(None);
        }
        if cc_job_policy::todo_wait::waiting(db, row) || self.rt.recovery_protected(TREE)? {
            return Ok(None);
        }
        use std::io::Read;
        let mut bytes = Vec::new();
        std::fs::File::open(SPEC)?
            .take(131073)
            .read_to_end(&mut bytes)?;
        if bytes.len() > 131072 {
            return Ok(None);
        }
        Ok(binding(
            job,
            row,
            &cc_job_policy::product_retry::digest(&bytes),
            at as u64,
        ))
    }
    pub(crate) fn research_policy(&self, job: &Value) -> Result<Option<cc_job_policy::Policy>> {
        if job["finite_research_approval"].is_null()
            || crate::execution::Profile::of(job).isolated()
        {
            return Ok(None);
        }
        let db = self.read_json(&self.todo_store())?;
        let proof = self.research_qualification(job, &db)?;
        if proof.as_ref() != Some(&job["finite_research_approval"]) {
            return Err(Error::new(
                "finite research review submission qualification changed or expired",
            ));
        }
        Ok(Some(cc_job_policy::Policy {
            sandbox: "read-only",
            approval: "on-request",
            reviewer: "auto_review",
        }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn fixture() -> (Value, Value) {
        let relation = json!({"parent_todo_id":2757,"kind":"research",
            "owner":{"repository":"autodevjapan/cc-pc-tools-rs"}});
        let key = "a".repeat(32);
        let row = json!({"id":2939,"project":"cc-pc-tools-rs","status":"todo","dispatchable":true,
            "spec":SPEC,"subwork":relation,"needs":[],"scheduler":{"key":key,"attempt":175,
            "worktree":TREE,"selection":{"model":"anthropic/claude-sonnet-5-5","effort":"high"}}});
        let job = json!({"id":"next","todo_id":2939,"sandbox":"read-only","network":false,
            "subwork_validated":true,"subwork":relation,"subwork_spec":SPEC,"assigned_needs":[],
            "launch_key":key,"cwd":TREE,"model":"anthropic/claude-sonnet-5-5","effort":"high"});
        (job, row)
    }
    #[test]
    fn only_the_next_exact_original_research_can_submit_to_review() {
        let (job, row) = fixture();
        let proof = binding(&job, &row, SPEC_HASH, START).expect("original finite research");
        assert_eq!(proof["sandbox"], "read-only");
        assert_eq!(proof["network"], false);
        assert_eq!(proof["approval_policy"], "on-request");
        assert_eq!(proof["approvals_reviewer"], "auto_review");
        assert_eq!(proof["source_job"], SOURCE);
        for changed in [
            "todo",
            "parent",
            "kind",
            "repo",
            "spec",
            "cwd",
            "key",
            "model",
            "effort",
            "needs",
            "sandbox",
            "network",
            "validated",
            "wait",
            "stop",
            "question",
            "processed",
            "attempt",
            "hash",
            "expired",
            "early",
        ] {
            let (mut job, mut row) = fixture();
            let mut hash = SPEC_HASH;
            let mut at = START;
            match changed {
                "todo" => job["todo_id"] = json!(2940),
                "parent" => job["subwork"]["parent_todo_id"] = json!(2758),
                "kind" => {
                    job["subwork"]["kind"] = json!("review");
                    row["subwork"] = job["subwork"].clone();
                }
                "repo" => {
                    job["subwork"]["owner"]["repository"] = json!("other/repo");
                    row["subwork"] = job["subwork"].clone();
                }
                "spec" => job["subwork_spec"] = json!("other.md"),
                "cwd" => job["cwd"] = json!("D:/other"),
                "key" => job["launch_key"] = json!("b".repeat(32)),
                "model" => job["model"] = json!("other-model"),
                "effort" => job["effort"] = json!("low"),
                "needs" => job["assigned_needs"] = json!(["cargo"]),
                "sandbox" => job["sandbox"] = json!("danger-full-access"),
                "network" => job["network"] = json!(true),
                "validated" => job["subwork_validated"] = json!(false),
                "wait" => row["wait_on"] = json!([3047]),
                "stop" => row["owner_stopped"] = json!(true),
                "question" => row["scheduler"]["question"] = json!("question"),
                "processed" => row["processed_results"]["next"] = json!({}),
                "attempt" => row["scheduler"]["attempt"] = json!(176),
                "hash" => hash = "changed",
                "expired" => at = END,
                "early" => at = START - 1,
                _ => unreachable!(),
            }
            assert!(binding(&job, &row, hash, at).is_none(), "{changed}");
        }
    }
    #[test]
    fn saved_approval_hints_do_not_grant_submission_and_effective_review_keeps_reader_sandbox() {
        let (mut job, _) = fixture();
        job["approval_policy"] = json!("on-request");
        job["approvals_reviewer"] = json!("auto_review");
        job["finite_research_approval"] = json!({"forged":true});
        let args = codex_args(&job);
        assert!(args.contains(&"approval_policy=never".into()));
        assert!(args.contains(&"approvals_reviewer=user".into()));
        let reviewed = codex_args_with_policy(
            &job,
            cc_job_policy::Policy {
                sandbox: "read-only",
                approval: "on-request",
                reviewer: "auto_review",
            },
        );
        assert!(reviewed.windows(2).any(|a| a == ["-s", "read-only"]));
        assert!(reviewed.contains(&"approval_policy=on-request".into()));
        assert!(reviewed.contains(&"approvals_reviewer=auto_review".into()));
        assert!(!reviewed.iter().any(|a| a.contains("network_access=true")));
        for change in ["summary", "fixed_review"] {
            let mut isolated = job.clone();
            if change == "summary" {
                isolated["context_phase"] = json!("summarize");
            } else {
                isolated["fixed_read_only_review"] = json!({});
            }
            let args = codex_args(&isolated);
            assert!(args.contains(&"approval_policy=never".into()));
            assert!(args.contains(&"approvals_reviewer=user".into()));
            assert!(args.windows(2).any(|a| a == ["-s", "read-only"]));
        }
    }
}
