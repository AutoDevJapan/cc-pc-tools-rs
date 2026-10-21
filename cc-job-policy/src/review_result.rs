//! A completed read-only child review is not a claim that its parent is complete.
use serde_json::Value;

pub const COMPLETE: &str = "状態: 完了（レビュー作業のみ）";

pub fn is_review_subwork(job: &Value) -> bool {
    job["sandbox"] == "read-only"
        && job["subwork_validated"] == true
        && job["subwork"]["kind"] == "review"
        && job["todo_id"].as_u64().is_some_and(|id| id > 0)
        && job["subwork"]["parent_todo_id"]
            .as_u64()
            .is_some_and(|id| id > 0 && job["todo_id"] != id)
        && job["subwork"]["origin_job_id"]
            .as_str()
            .is_some_and(|id| !id.is_empty())
        && job["subwork_spec"].as_str().is_some_and(|s| !s.is_empty())
        && ["repository", "git_common", "worktree", "branch"]
            .iter()
            .all(|field| {
                job["subwork"]["owner"][*field]
                    .as_str()
                    .is_some_and(|s| !s.is_empty())
            })
}

pub fn completed(job: &Value, result: &str) -> bool {
    is_finite_review(job)
        && result
            .lines()
            .next()
            .is_some_and(|line| [COMPLETE, "状態: 完了"].contains(&line.trim()))
}

pub fn is_finite_review(job: &Value) -> bool {
    is_review_subwork(job)
        || (job["sandbox"] == "read-only"
            && crate::review_task(
                job["name"].as_str().unwrap_or(""),
                job["task_type"].as_str().unwrap_or(""),
            )
            && job["todo_id"].as_u64().is_some_and(|id| id > 0)
            && job["finite_review"]["todo_id"] == job["todo_id"]
            && job["finite_review"]["spec"]
                .as_str()
                .is_some_and(|spec| !spec.is_empty())
            && job["launch_key"]
                .as_str()
                .is_some_and(|key| !key.is_empty())
            && job["finite_review"]["launch_key"] == job["launch_key"])
}

pub fn matches_item(job: &Value, item: &Value) -> bool {
    job["todo_id"] == item["id"]
        && if is_review_subwork(job) {
            job["subwork"] == item["subwork"] && job["subwork_spec"] == item["spec"]
        } else {
            is_finite_review(job) && job["finite_review"]["spec"] == item["spec"]
        }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn review() -> Value {
        json!({"todo_id":2813,"sandbox":"read-only","subwork_validated":true,"subwork_spec":"saved-fixed-head.md",
            "subwork":{"kind":"review","parent_todo_id":2757,"origin_job_id":"writer",
                "owner":{"repository":"fixture/repo","git_common":"fixture-common","worktree":"fixture-tree","branch":"fixture-branch"}}})
    }
    #[test]
    fn exact_qualified_child_header_is_scoped_to_the_readonly_review() {
        let job = review();
        assert!(completed(&job, &format!("{COMPLETE}\nMustなし。")));
        for change in [
            "root",
            "writer",
            "unvalidated",
            "research",
            "missing_owner",
            "self_parent",
            "missing_spec",
        ] {
            let mut job = review();
            match change {
                "root" => {
                    job.as_object_mut().unwrap().remove("subwork");
                }
                "writer" => job["sandbox"] = json!("workspace-write"),
                "unvalidated" => job["subwork_validated"] = json!(false),
                "research" => job["subwork"]["kind"] = json!("research"),
                "missing_owner" => job["subwork"]["owner"] = Value::Null,
                "self_parent" => job["subwork"]["parent_todo_id"] = json!(2813),
                "missing_spec" => job["subwork_spec"] = Value::Null,
                _ => unreachable!(),
            }
            assert!(!completed(&job, COMPLETE));
        }
        for text in [
            "状態: 完了（実装だけ・検証未了）",
            "状態: 継続中",
            "前置き\n状態: 完了（レビュー作業のみ）",
            "状態: 完了（レビュー作業のみ）ただし未完",
        ] {
            assert!(!completed(&job, text));
        }
        let mut item = json!({"id":2813,"subwork":job["subwork"],"spec":job["subwork_spec"]});
        assert!(matches_item(&job, &item));
        item["spec"] = json!("new-fixed-head.md");
        assert!(!matches_item(&job, &item));
    }
}
