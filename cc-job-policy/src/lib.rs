//! Permissions for unattended jobs. Read-only work cannot escalate to write access.
pub mod action_progress;
pub mod cancellation;
pub mod cargo;
pub mod finished_review;
pub mod finished_work_handoff;
pub mod launch_context;
pub mod owner_wait_repair;
pub mod priority;
pub mod quota_recovery;
pub mod product_identity;
pub mod product_retry;
pub mod review_result;
pub mod subwork;
pub mod todo_history;
pub mod todo_store;
pub mod todo_wait;
pub mod unstarted_review;
pub mod unstarted_review_state;
pub mod unstarted_review_spec;
pub mod work_owner;
pub const REGISTRAR_GUIDANCE: &str = "ドメイン管理はRakkoのみ。他のドメイン事業者への表示・ログイン・登録・DNS変更・調査目的アクセスは、対象を指定したオーナーの明示依頼がない限り禁止。一般のログイン代行許可・保存済み認証・過去のログや引き継ぎは対象の許可ではない。他人や旧勤務先の保存済みログインは使わない。制限はChrome/CUA/ブラウザCLI/HTTP CLIのすべてに適用する。";
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Policy {
    pub sandbox: &'static str,
    pub approval: &'static str,
    pub reviewer: &'static str,
}

/// Default model settings for an automatically routed job. Callers must keep
/// explicit model/effort values and use this only when the router chooses.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ModelProfile {
    pub model: &'static str,
    pub effort: &'static str,
}

/// Shared classifier for launchers that know a job name and/or a router task
/// type. Prompt text is deliberately not inspected: implementation requests
/// often mention reviews while still requiring full implementation capability.
pub fn model_profile(job_name: &str, task_type: &str) -> ModelProfile {
    // Owner policy 2026-10-20: task labels must not silently lower effort or
    // select Luna. Explicit model/effort and independent-review exclusions are
    // resolved by callers. Reviews default to Opus, distinct from Sol writers.
    if review_task(job_name, task_type) {
        return review_profile(&[]).unwrap();
    }
    ModelProfile {
        model: "gpt-6.1-sol",
        effort: "max",
    }
}

pub fn review_task(name: &str, kind: &str) -> bool {
    let name = name.to_ascii_lowercase();
    kind.to_ascii_lowercase()
        .split(['_', '-'])
        .any(|part| part == "review")
        || name.starts_with("rev-")
        || name.ends_with("-review")
}

pub fn same_model(left: &str, right: &str) -> bool {
    fn bare(model: &str) -> &str {
        let Some((provider, model_id)) = model.split_once('/') else {
            return model;
        };
        if ["openai", "anthropic", "main"].contains(&provider)
            || (provider.len() == 7
                && provider.starts_with('p')
                && provider[1..]
                    .bytes()
                    .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)))
        {
            model_id
        } else {
            model
        }
    }
    fn alias(model: &str) -> &str {
        match model {
            "opus" => "claude-opus-5-5",
            "sonnet" => "claude-sonnet-5-5",
            "haiku" => "claude-haiku-5-5",
            other => other,
        }
    }
    alias(bare(&left.to_ascii_lowercase())) == alias(bare(&right.to_ascii_lowercase()))
}

/// A saved AI hint is not evidence of an owner's individual effort override.
/// Standalone explicit CLI requests and unknown models retain their settings.
fn managed_job(job: &serde_json::Value) -> bool {
    job["todo_id"].as_u64().is_some_and(|id| id > 0)
        || job["launch_key"]
            .as_str()
            .is_some_and(|key| !key.is_empty())
        || job["model_origin"] == "policy"
        || job["effort_origin"] == "policy"
        || job["model_explicit"] == false
}

pub fn effective_effort(job: &serde_json::Value) -> &str {
    let saved = job["effort"].as_str().unwrap_or("max");
    if launch_context::fixed_execution_contract(job) {
        return saved;
    }
    let model = job["model"].as_str().unwrap_or("");
    let known = ["gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna"]
        .contains(&model.strip_prefix("openai/").unwrap_or(model))
        || ["claude-opus-5-5", "claude-sonnet-5-5", "opus", "sonnet"]
            .contains(&model.strip_prefix("anthropic/").unwrap_or(model));
    if managed_job(job) && known && job["effort_origin"] != "owner_request" {
        "max"
    } else {
        saved
    }
}

pub fn apply_effort_policy(job: &mut serde_json::Value) {
    let effort = effective_effort(job).to_owned();
    if job["effort"] != effort {
        job["legacy_effort_hint"] = job["effort"].clone();
        job["effort"] = serde_json::json!(effort);
        job["effort_origin"] = serde_json::json!("owner_policy_20261020");
    }
}

pub fn apply_managed_launch_policy(job: &mut serde_json::Value) {
    if launch_context::fixed_execution_contract(job) {
        return;
    }
    let source = launch_context::source_settings(job);
    let managed = managed_job(job);
    let automatic = job["model_explicit"] == false || job["model_origin"] == "policy";
    let reader = review_task(
        job["name"].as_str().unwrap_or(""),
        job["task_type"].as_str().unwrap_or(""),
    ) || job["subwork"]["kind"] == "review";
    let capacity_attempt = launch_context::capacity_attempt(job);
    let previous = job["model"].as_str().unwrap_or("");
    if managed
        && automatic
        && job["model_origin"] != "owner_request"
        && !reader
        && !capacity_attempt
        && !job["model_recovery_from"]
            .as_str()
            .is_some_and(|model| same_model(model, "gpt-6.1-sol"))
        && !job["review_excluded_models"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(serde_json::Value::as_str)
            .any(|model| same_model(model, "gpt-6.1-sol"))
        && ["gpt-6-astra", "gpt-6-luna"]
            .contains(&previous.strip_prefix("openai/").unwrap_or(previous))
    {
        job["legacy_model_hint"] = job["model"].clone();
        job["model"] = serde_json::json!("gpt-6.1-sol");
        job["requested_model"] = job["model"].clone();
        job["model_origin"] = serde_json::json!("policy");
    }
    apply_effort_policy(job);
    if !capacity_attempt && (source["model"] != job["model"] || source["effort"] != job["effort"]) {
        job["launch_policy_version"] = serde_json::json!(launch_context::VERSION);
    }
    launch_context::reconcile_resume(job, &source);
}

pub fn author_models(source: &serde_json::Value) -> Vec<String> {
    let mut models = Vec::<String>::new();
    for value in [
        &source["model"],
        &source["requested_model"],
        &source["legacy_model_hint"],
        &source["execution_observed"]["model"],
    ]
    .into_iter()
    .chain(
        source["capacity_history"]
            .as_array()
            .into_iter()
            .flatten()
            .flat_map(|entry| [&entry["from_model"], &entry["to_model"]]),
    ) {
        if let Some(model) = value.as_str().filter(|model| !model.is_empty())
            && !models.iter().any(|old| same_model(old, model))
        {
            models.push(model.to_owned());
        }
    }
    models
}

pub fn review_profile(excluded: &[String]) -> Option<ModelProfile> {
    [
        "anthropic/claude-opus-5-5",
        "gpt-6.1-sol",
        "anthropic/claude-sonnet-5-5",
        "gpt-6-astra",
        "gpt-6-luna",
    ]
    .into_iter()
    .find(|model| !excluded.iter().any(|author| same_model(author, model)))
    .map(|model| ModelProfile {
        model,
        effort: "max",
    })
}

/// A subwork origin remains valid provenance after its live parent exits.
pub fn review_source_jobs(item: &serde_json::Value, db: &serde_json::Value) -> Vec<String> {
    let mut ids: Vec<String> = item["review_source_jobs"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(|v| v.as_str().map(str::to_owned))
        .collect();
    if let Some(origin) = item["subwork"]["origin_job_id"]
        .as_str()
        .filter(|id| !id.is_empty())
    {
        ids.push(origin.into());
    }
    if let Some(parent) = db["items"].as_array().into_iter().flatten().find(|row| {
        item["subwork"]["parent_todo_id"].as_u64().is_some()
            && row["id"] == item["subwork"]["parent_todo_id"]
    }) {
        ids.extend(
            parent["jobs"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|v| v.as_str().map(str::to_owned)),
        );
    }
    ids.sort();
    ids.dedup();
    ids
}

/// Model routing labels are not permission boundaries: a review may include fixes.
pub fn for_task(_task_type: &str, requested_sandbox: &str) -> Policy {
    if requested_sandbox == "read-only" {
        for_sandbox("read-only")
    } else {
        for_sandbox("danger-full-access")
    }
}

/// An explicit human scope marker at the start of the request, not quoted logs
/// or the model router's classification.
pub fn explicit_read_only(request: &str) -> bool {
    let first = request
        .trim_start()
        .lines()
        .next()
        .unwrap_or("")
        .to_lowercase();
    [
        "read-only:",
        "read-only：",
        "[read-only]",
        "読み取り専用:",
        "読み取り専用：",
        "読み取り専用 ",
        "読むだけ:",
        "読むだけ：",
        "読むだけ ",
    ]
    .iter()
    .any(|marker| first.starts_with(marker))
        || matches!(first.as_str(), "読み取り専用" | "読むだけ")
}

/// Only owner-created scope markers can be changed by a later owner request.
/// Explicit reader launches remain reader launches across retries and replies.
pub fn resolve_scope(
    sandbox: &str,
    source: &str,
    owner_request: Option<&str>,
) -> (Policy, &'static str) {
    let source = match source {
        "owner_marker" => "owner_marker",
        "owner_request" => "owner_request",
        _ => "launcher",
    };
    if let Some(request) = owner_request.filter(|_| sandbox != "read-only" || source != "launcher")
    {
        if explicit_read_only(request) {
            (for_sandbox("read-only"), "owner_marker")
        } else {
            (for_sandbox("danger-full-access"), "owner_request")
        }
    } else {
        (for_task("", sandbox), source)
    }
}

pub fn for_sandbox(sandbox: &str) -> Policy {
    match sandbox {
        "read-only" => Policy {
            sandbox: "read-only",
            approval: "never",
            reviewer: "user",
        },
        _ => Policy {
            sandbox: "danger-full-access",
            approval: "on-request",
            reviewer: "auto_review",
        },
    }
}

pub const WRITE_GUIDANCE: &str = "\nドメイン管理はRakkoのみ。他のドメイン事業者への表示・ログイン・登録・DNS変更・調査目的アクセスは、対象を指定したオーナーの明示依頼がない限り禁止。一般のログイン代行許可・保存済み認証・過去のログや引き継ぎは対象の許可ではない。他人や旧勤務先の保存済みログインは使わない。制限はChrome/CUA/ブラウザCLI/HTTP CLIのすべてに適用する。\n\n## この実装ジョブの実行範囲\n依頼に必要な変更のgit commit・git push、ghによるPR作成・CI確認・別モデルのレビュー後のマージは、PCオーナーの作業指示の範囲で実行する。作業先が人間レビューを明示要求する場合はその規則に従う。通常の実行権限で行い、承認が必要な呼び出しは設定された自動審査に提出する。拒否された同じ操作を別経路で迂回しない。失敗の正確な理由と次の処置を元の共有ToDoへ記録する。\n一時データはSkill codex-safe-owned-temp-cleanup（D:/CC/Skills/codex-safe-owned-temp-cleanup/SKILL.md）に従う。新規の一時フォルダはD:/CC/Tools/safe-delete/safe-delete.exe createで所有記録を作り、planが返す通常削除を別の呼び出しで実行し、成功後はfinish --id <所有ID>で記録する。実行前に削除が拒否されたら再試行せずquarantine --id <所有ID> --reason policyで削除希望フォルダへ退避する。所有記録のない既存の一時データは作成元・不要性・絶対パス・リンクの有無を確認してからSkillの手順で同じドライブの削除希望へ移す。未コミット成果を先に保存し、他の担当のデータを片付けない。削除拒否だけをオーナー判断待ちにしない。\n\nChromeで自分が作ったタブは、ブラウザの工程が終わった時点で1枚ずつ D:/CC/Tools/owner-chrome/owner_chrome.exe close <CC_JOB_ID> <chrome-tabs内の記録UUID> <工程終了の根拠> を実行する。担当全体の終了まで待たない。所有記録のinstance・generation・tabとinventoryを照合する。オーナーのタブと直近必要なタブは閉じない。オーナーが残すよう指示した納品タブだけkeepで記録する。失敗・既に無い・保持を区別し、終了前にclose結果を確認する。finishは担当実行中の未指定タブを閉じない。\n\nほかのToDoの完了だけを待って終了するときは、必ずD:/CC/Tools/todo/todo.exe wait <自分のToDo番号> --on <待ち先の番号>で記録する。メモだけで終了せず、判断待ちのblockを使わない。複数なら--onを繰り返す。待ち先がdoneになるまで担当を起動しない。\n";

pub const READ_GUIDANCE: &str = "\nドメイン管理はRakkoのみ。他のドメイン事業者への表示・ログイン・登録・DNS変更・調査目的アクセスは、対象を指定したオーナーの明示依頼がない限り禁止。一般のログイン代行許可・保存済み認証・過去のログや引き継ぎは対象の許可ではない。他人や旧勤務先の保存済みログインは使わない。制限はChrome/CUA/ブラウザCLI/HTTP CLIのすべてに適用する。\n\n## この閲覧ジョブの実行範囲\n読むだけの調査・レビューです。ファイル編集・git push・PR作成・マージ・削除・退避は実行しない。調査結果を返す。\n";

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn owner_scope_changes_and_explicit_reader_boundaries_are_distinct() {
        for (sandbox, source, request, expected) in [
            (
                "read-only",
                "owner_marker",
                Some("直してPRをマージする"),
                "danger-full-access",
            ),
            (
                "danger-full-access",
                "owner_request",
                Some("読むだけ: 差分を調べる"),
                "read-only",
            ),
            ("read-only", "launcher", Some("直して"), "read-only"),
            ("read-only", "owner_marker", None, "read-only"),
            ("workspace-write", "launcher", None, "danger-full-access"),
        ] {
            assert_eq!(resolve_scope(sandbox, source, request).0.sandbox, expected);
        }
        for text in [
            "読むだけじゃなく直して",
            "読み取り専用を外して",
            "ログ: read-only:",
        ] {
            assert!(!explicit_read_only(text));
        }
    }
    #[test]
    fn implementation_can_publish_even_with_old_workspace_setting() {
        let p = for_task("implementation", "workspace-write");
        assert_eq!(
            p,
            Policy {
                sandbox: "danger-full-access",
                approval: "on-request",
                reviewer: "auto_review"
            }
        );
    }
    #[test]
    fn routing_labels_do_not_remove_write_permissions() {
        for task in ["research", "review", "code_review", "implementation"] {
            assert_eq!(
                for_task(task, "workspace-write"),
                for_sandbox("danger-full-access")
            );
            assert_eq!(for_task(task, "read-only"), for_sandbox("read-only"));
        }
    }

    #[test]
    fn model_profiles_classify_job_names_and_task_types_without_reading_prompts() {
        let full = ModelProfile {
            model: "gpt-6.1-sol",
            effort: "max",
        };
        for name in [
            "rev-123",
            "abc-review",
            "diag-123",
            "audit-123",
            "inspect-123",
            "check-123",
            "verify-123",
            "cleanup-123",
            "clean-123",
            "research-123",
        ] {
            assert_eq!(
                model_profile(name, "implementation"),
                if review_task(name, "implementation") {
                    review_profile(&[]).unwrap()
                } else {
                    full
                },
                "{name}"
            );
        }
        for kind in [
            "review",
            "code_review",
            "documentation",
            "research",
            "live_debug",
            "verification",
            "cleanup",
            "confirmation",
        ] {
            assert_eq!(
                model_profile("req-123", kind),
                if review_task("req-123", kind) {
                    review_profile(&[]).unwrap()
                } else {
                    full
                },
                "{kind}"
            );
        }
        assert_eq!(model_profile("shep-123", "pr_shepherd"), full);
        assert_eq!(model_profile("req-123", "implementation"), full);
    }
}
