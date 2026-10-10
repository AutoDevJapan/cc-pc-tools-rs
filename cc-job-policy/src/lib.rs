//! Permissions for unattended jobs. Read-only work cannot escalate to write access.
pub mod cargo;
pub mod subwork;
pub mod todo_wait;
pub mod work_owner;
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
    let name = job_name.to_ascii_lowercase();
    let kind = task_type.to_ascii_lowercase();
    let light_name = name.starts_with("rev-")
        || name.ends_with("-review")
        || [
            "diag-",
            "audit-",
            "inspect-",
            "check-",
            "verify-",
            "research-",
            "cleanup-",
            "clean-",
        ]
        .iter()
        .any(|prefix| name.starts_with(prefix));
    let light_kind = [
        "review",
        "code_review",
        "documentation",
        "research",
        "live_debug",
        "verification",
        "check",
        "confirmation",
        "audit",
        "inspection",
        "cleanup",
        "housekeeping",
    ]
    .contains(&kind.as_str());
    if light_name || light_kind {
        ModelProfile {
            model: "gpt-6-luna",
            effort: "high",
        }
    } else {
        ModelProfile {
            model: "gpt-6.1-sol",
            effort: "high",
        }
    }
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

pub const WRITE_GUIDANCE: &str = "\n## この実装ジョブの実行範囲\n依頼に必要な変更のgit commit・git push、ghによるPR作成・CI確認・別モデルのレビュー後のマージは、PCオーナーの作業指示の範囲で実行する。作業先が人間レビューを明示要求する場合はその規則に従う。通常の実行権限で行い、承認が必要な呼び出しは設定された自動審査に提出する。拒否された同じ操作を別経路で迂回しない。失敗の正確な理由と次の処置を元の共有ToDoへ記録する。\n一時データはSkill codex-safe-owned-temp-cleanup（D:/CC/Skills/codex-safe-owned-temp-cleanup/SKILL.md）に従う。新規の一時フォルダはD:/CC/Tools/safe-delete/safe-delete.exe createで所有記録を作り、planが返す通常削除を別の呼び出しで実行し、成功後はfinish --id <所有ID>で記録する。実行前に削除が拒否されたら再試行せずquarantine --id <所有ID> --reason policyで削除希望フォルダへ退避する。所有記録のない既存の一時データは作成元・不要性・絶対パス・リンクの有無を確認してからSkillの手順で同じドライブの削除希望へ移す。未コミット成果を先に保存し、他の担当のデータを片付けない。削除拒否だけをオーナー判断待ちにしない。\n\nChromeで自分が作ったタブは、ブラウザの工程が終わった時点で1枚ずつ D:/CC/Tools/owner-chrome/owner_chrome.exe close <CC_JOB_ID> <chrome-tabs内の記録UUID> <工程終了の根拠> を実行する。担当全体の終了まで待たない。所有記録のinstance・generation・tabとinventoryを照合する。オーナーのタブと直近必要なタブは閉じない。オーナーが残すよう指示した納品タブだけkeepで記録する。失敗・既に無い・保持を区別し、終了前にclose結果を確認する。finishは担当実行中の未指定タブを閉じない。\n\nほかのToDoの完了だけを待って終了するときは、必ずD:/CC/Tools/todo/todo.exe wait <自分のToDo番号> --on <待ち先の番号>で記録する。メモだけで終了せず、判断待ちのblockを使わない。複数なら--onを繰り返す。待ち先がdoneになるまで担当を起動しない。\n";

pub const READ_GUIDANCE: &str = "\n## この閲覧ジョブの実行範囲\n読むだけの調査・レビューです。ファイル編集・git push・PR作成・マージ・削除・退避は実行しない。調査結果を返す。\n";

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
        let light = ModelProfile {
            model: "gpt-6-luna",
            effort: "high",
        };
        let full = ModelProfile {
            model: "gpt-6.1-sol",
            effort: "high",
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
            assert_eq!(model_profile(name, "implementation"), light, "{name}");
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
            assert_eq!(model_profile("req-123", kind), light, "{kind}");
        }
        assert_eq!(model_profile("shep-123", "pr_shepherd"), full);
        assert_eq!(model_profile("req-123", "implementation"), full);
    }
}
