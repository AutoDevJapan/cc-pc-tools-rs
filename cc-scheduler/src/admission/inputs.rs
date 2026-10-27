//! Shared field selection for the ordinary gates and their body-free projection.
//! The ordinary gates still compare the original, unredacted values.
use serde_json::{json, Value};

#[derive(Clone, Copy, PartialEq)]
pub(crate) enum Kind {
    Scalar,
    Truth,
    Tree,
}
use Kind::*;
pub(crate) const LAUNCH: &[(&str, &str, Kind)] = &[
    ("spec", "spec", Scalar),
    ("needs", "needs", Scalar),
    ("subwork", "subwork", Tree),
    ("subwork_continuation", "subwork_continuation", Tree),
    ("title", "title", Truth),
    ("project", "project", Scalar),
    ("dispatchable", "dispatchable", Scalar),
    ("model", "model", Scalar),
    ("effort", "effort", Scalar),
    ("model_origin", "model_origin", Scalar),
    ("effort_origin", "effort_origin", Scalar),
    ("sandbox", "sandbox", Scalar),
    ("mcp_profile", "mcp_profile", Scalar),
    ("network", "network", Scalar),
    ("add_dirs", "add_dirs", Scalar),
    ("wait_on", "wait_on", Scalar),
    ("review_source_jobs", "review_source_jobs", Scalar),
    ("task_type", "task_type", Scalar),
    ("role", "role", Scalar),
    ("worktree", "scheduler.worktree", Scalar),
    ("branch", "scheduler.branch", Scalar),
    ("not_before", "scheduler.not_before", Scalar),
    ("resource_wait", "scheduler.resource_wait", Tree),
    ("workspace_receipt", "scheduler.workspace_receipt", Tree),
    ("jobs", "jobs", Scalar),
    (
        "subwork_workspace_migration",
        "scheduler.subwork_workspace_migration",
        Tree,
    ),
    ("resume_generation", "resume_generation", Scalar),
    ("worker_receipt", "worker_receipt", Tree),
    ("console_request", "scheduler.console_request", Tree),
    (
        "console_fresh_request",
        "scheduler.console_fresh_request",
        Tree,
    ),
    ("unstarted_review", "scheduler.unstarted_review", Tree),
    ("initial_review", "scheduler.initial_review", Tree),
];
pub(crate) const REVIEW: &[(&str, &str, Kind)] = &[
    ("spec", "spec", Scalar),
    ("model", "model", Scalar),
    ("effort", "effort", Scalar),
    ("model_origin", "model_origin", Scalar),
    ("effort_origin", "effort_origin", Scalar),
    ("options", "scheduler.console_request.options", Tree),
    ("subwork", "subwork", Tree),
    ("review_source_jobs", "review_source_jobs", Scalar),
    ("task_type", "task_type", Scalar),
    ("role", "role", Scalar),
];
const EXTRA: &[(&str, Kind)] = &[
    ("decision_wait", Truth),
    ("owner_question_wait", Truth),
    ("product_review_allocation", Tree),
    ("wait_ready", Tree),
    ("scheduler.question", Truth),
    ("scheduler.answer", Truth),
    ("scheduler.retire_retry_question", Scalar),
    ("scheduler.last_stop", Scalar),
];
fn value<'a>(item: &'a Value, path: &str) -> &'a Value {
    path.split('.').fold(item, |value, key| &value[key])
}
fn pick(item: &Value, fields: &[(&str, &str, Kind)]) -> Value {
    let mut out = json!({});
    for (key, path, _) in fields {
        out[*key] = value(item, path).clone();
    }
    out
}
pub(crate) fn launch(item: &Value) -> Value {
    let mut out = pick(item, LAUNCH);
    out["targeted_hold"] = json!(crate::targeted_dispatch::held(item));
    out
}
pub(crate) fn review(item: &Value) -> Value {
    pick(item, REVIEW)
}
pub(crate) fn kind(path: &str) -> Option<Kind> {
    LAUNCH
        .iter()
        .chain(REVIEW)
        .find(|(_, p, _)| *p == path)
        .map(|(_, _, k)| *k)
        .or_else(|| EXTRA.iter().find(|(p, _)| *p == path).map(|(_, k)| *k))
        .or_else(|| {
            path.strip_prefix("scheduler.review_preparation_failure.inputs.")
                .and_then(|key| {
                    REVIEW
                        .iter()
                        .find(|(k, _, _)| *k == key)
                        .map(|(_, _, k)| *k)
                })
        })
}
pub(crate) fn paths() -> Vec<String> {
    LAUNCH
        .iter()
        .chain(REVIEW)
        .map(|(_, p, _)| p.to_string())
        .chain(EXTRA.iter().map(|(p, _)| p.to_string()))
        .chain(
            REVIEW
                .iter()
                .map(|(key, _, _)| format!("scheduler.review_preparation_failure.inputs.{key}")),
        )
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn sharing_field_selection_never_redacts_the_ordinary_input_equality() {
        let mut row = json!({"title":"TITLE_A","scheduler":{"console_request":{"prompt":"PROMPT_A",
            "options":{"notes_hint":"NOTE_A","sandbox":"read-only"}}}});
        let launch = launch(&row);
        let review = review(&row);
        assert_eq!(launch["title"], "TITLE_A");
        assert_eq!(launch["console_request"]["prompt"], "PROMPT_A");
        row["title"] = json!("TITLE_B");
        row["scheduler"]["console_request"]["prompt"] = json!("PROMPT_B");
        row["scheduler"]["console_request"]["options"]["notes_hint"] = json!("NOTE_B");
        assert_ne!(super::launch(&row), launch);
        assert_ne!(super::review(&row), review);
    }
}
