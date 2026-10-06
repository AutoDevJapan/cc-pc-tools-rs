//! Owner actions remain pending after permission or unrelated editorial feedback.
use crate::core::*;

pub fn actionable(body: &str) -> bool {
    [
        "ログインをお願いしたい",
        "ログインをお願いします",
        "ログインしていただきたい",
        "認証を完了してください",
    ]
    .iter()
    .any(|text| body.contains(text))
}
pub fn waiting_notice(body: &str) -> bool {
    ["質問への返事を待", "公開範囲の回答を待"]
        .iter()
        .any(|text| body.contains(text))
}

/// An outstanding human action is independent of a canceled duplicate agent job.
pub fn pending(ctx: &Context, thread: &str, title: &str) -> bool {
    let Ok(db) = read_json(&ctx.todo) else {
        return false;
    };
    db["items"].as_array().into_iter().flatten().any(|item| {
        if !matches!(s(&item["status"]), "block" | "todo") {
            return false;
        }
        let note = s(&item["note"]);
        if [
            "回答待ちではない",
            "回答待ちはありません",
            "判断は不要",
            "判断待ちではなく",
            "自動で再開しない",
            "停止したまま",
        ]
        .iter()
        .any(|text| note.contains(text))
        {
            return false;
        }
        let human = [
            "回答を待",
            "返事を待",
            "判断を待",
            "本人のログイン操作待ち",
            "本人認証を待",
            "本人の認証操作",
            "方向が決まったら",
        ]
        .iter()
        .any(|text| note.contains(text));
        if !human {
            return false;
        }
        let project = s(&item["project"]);
        note.contains(thread)
            || (project == "owner"
                && title
                    .split(['【', '】'])
                    .filter(|part| !part.is_empty())
                    .any(|label| s(&item["title"]).contains(label) && label.chars().count() >= 3))
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn physical_login_and_unresolved_publication_are_actionable_without_new_choices() {
        assert!(actionable("このPCのChromeで、Stripeにログインをお願いしたいです！ログインしたら、このスレッドに一言ください。"));
        assert!(waiting_notice(
            "前に送った「投稿してよいか・公開範囲」の質問への返事を待っています。"
        ));
        assert!(!actionable("Stripeの確認と動画の声の修正が完了しました。"));
    }
    #[test]
    fn internal_resource_wait_and_finished_actions_do_not_become_owner_waits() {
        let file = tempfile::NamedTempFile::new().unwrap();
        let ctx = Context {
            here: "".into(),
            client_projects: "".into(),
            civ6_home: "".into(),
            jobs: "".into(),
            discord_tasks: "".into(),
            todo: file.path().into(),
            cfg: json!({}),
            effects: Box::new(RealEffects),
            codex_command: "".into(),
            scheduler_command: "".into(),
            shepherd_command: "".into(),
            todo_command: "".into(),
        };
        for (status, note, expected) in [
            ("block", "質問123の公開範囲の回答を待ちます", true),
            (
                "block",
                "質問123の検査待ち。オーナーの回答待ちはありません",
                false,
            ),
            ("done", "質問123の公開範囲の回答を待ちます", false),
        ] {
            std::fs::write(
                file.path(),
                json!({"items":[{"status":status,"note":note,"project":"ADJ"}]}).to_string(),
            )
            .unwrap();
            assert_eq!(pending(&ctx, "123", "健康動画"), expected);
        }
    }
}
