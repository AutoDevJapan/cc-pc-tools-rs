//! The single owner-authorized, unlaunched review. This is not registry recovery.
use serde_json::{Value, json};
pub const HEAD: &str = "3b14dd1f43ef5a19e1f09b0959819f3e24f151c3";
pub const HASH: &str = "45acf355ad003318dc6ba4b06c0c1b0825e6805d8cb6427df53e9ae994b92f27";
pub const ORIGIN: &str = "20261011-234558-orch-9a5f7b7c6fb45eb2815aa60ea8a5df95";
pub const ROOT: &str = "C:/Users/makhmeto/Desktop/dev/ADJ/PhosGaea-Vol1";
pub const SOURCE: &str = "C:/Users/makhmeto/Desktop/dev/ADJ/PhosGaea-Vol1-wt/todo-1626-1";
pub const TREE: &str = "C:/Users/makhmeto/Desktop/dev/ADJ/PhosGaea-Vol1-wt/todo-2407-work";
pub const BRANCH: &str = "scheduler/todo-2407-work";
pub const KEY: &str = "c8809d28af00fce0cad714928b5aa3cf";
pub fn in_scope(at: f64) -> bool {
    at >= 1791849600. && at < 1792540800.
}
pub fn binding(item: &Value) -> Value {
    json!({"spec":item["spec"],"needs":item["needs"],"subwork":item["subwork"],
        "head":HEAD,"request_sha256":HASH,"key":KEY,"worktree":TREE,"branch":BRANCH})
}
pub fn eligible(item: &Value, at: f64) -> bool {
    in_scope(at)
        && item["id"] == 2407
        && item["project"] == "PhosGaea-Vol1"
        && item["subwork"]["parent_todo_id"] == 1626
        && item["subwork"]["origin_job_id"] == ORIGIN
        && item["subwork"]["kind"] == "review"
        && item["product_review_allocation"]["binding"] == binding(item)
        && item["scheduler"]["key"] == KEY
        && item["scheduler"]["worktree"] == TREE
        && item["scheduler"]["branch"] == BRANCH
        && item["needs"] == json!([])
        && !cc_job_policy::cancellation::owner_stopped(item, &Value::Null)
        && item["scheduler"]["owner_stop_job"].is_null()
        && item["decision_wait"].is_null()
        && item["owner_postponement_recorded"] != true
        && item["stop_kind"].as_str().is_none_or(str::is_empty)
}
