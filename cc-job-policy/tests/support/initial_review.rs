use super::*;
use cc_job_policy::initial_review as rule;
pub const AT: f64 = 100000.;
pub fn health() -> Value {
    json!({"at":AT,"anthropic":{"at":AT,"verified":true,"exhausted":true,"available":false},
        "openai":{"at":AT,"verified":true,"available":true,"exhausted":false,
            "safe_routes":[{"selector":"main","at":AT,"verified":true}]}})
}
pub fn fixture() -> (Fixture, Value, Value) {
    let mut f = Fixture::new();
    fs::write(
        f.repo.join(".git/config"),
        "[remote \"origin\"]\nurl = https://github.com/AutoDevJapan/cc-pc-tools-rs.git\n",
    )
    .unwrap();
    fs::write(
        f.repo.join(".git/worktrees/parent/HEAD"),
        "ref: refs/heads/scheduler/todo-3894-ci",
    )
    .unwrap();
    fs::write(
        f.repo.join(".git/worktrees/todo-2-check/HEAD"),
        "ref: refs/heads/scheduler/todo-3900-review",
    )
    .unwrap();
    let mut owner = work_owner::checkout(f.jobs[0]["cwd"].as_str().unwrap());
    owner["pr"] = json!(528);
    f.jobs[0]["id"] = json!(rule::AUTHOR);
    f.jobs[0]["todo_id"] = json!(3894);
    f.jobs[0]["work_owner"] = owner;
    f.jobs[0]["execution_observed"] =
        json!({"source":"native_turn_context","model":"gpt-6.1-sol","effort":"max"});
    f.db["projects"] = json!({"cc-pc-tools-rs":f.repo});
    f.db["items"][0]["id"] = json!(3894);
    f.db["items"][0]["project"] = json!("cc-pc-tools-rs");
    f.db["items"][0]["jobs"] = json!([rule::AUTHOR]);
    f.db["items"][0]["scheduler"]["job"] = json!(rule::AUTHOR);
    let row = &mut f.db["items"][1];
    row["id"] = json!(3900);
    row["project"] = json!("cc-pc-tools-rs");
    row["title"] = json!("PR528 review");
    row["scheduler"]["branch"] = json!("scheduler/todo-3900-review");
    row["model"] = json!("gpt-6-luna");
    row["model_origin"] = json!("policy");
    row["effort"] = json!("max");
    row["effort_origin"] = json!("policy");
    row["task_type"] = json!("review");
    row["scheduler"]["phase"] = json!("preparing");
    row["scheduler"]["not_before"] = json!(AT + 900.);
    row["scheduler"]["resource_wait"] = json!({"reason":"quota_exhausted","until":AT+900.});
    f.db["items"][1]["subwork"] = subwork::register(
        &f.db,
        &f.db["items"][1],
        3894,
        rule::AUTHOR,
        "review",
        &f.jobs,
        false,
    )
    .unwrap();
    f.db["items"][1]["scheduler"]["subwork_admission"] = subwork::admission(&f.db["items"][1]);
    let source = rule::author(&f.jobs).unwrap();
    let workspace = json!({"repository":"autodevjapan/cc-pc-tools-rs","head":"synthetic-head","branch":"synthetic-branch"});
    (f, source, workspace)
}
pub fn planned() -> (Fixture, Value, Value) {
    let (mut f, source, workspace) = fixture();
    let bytes = fs::read(f.db["items"][1]["spec"].as_str().unwrap()).unwrap();
    let p = rule::proposal(
        &f.db["items"][1],
        &source,
        &workspace,
        &cc_job_policy::product_retry::digest(&bytes),
        &health(),
        true,
        AT,
    )
    .unwrap();
    assert!(rule::apply(&mut f.db["items"][1], &p));
    (f, source, workspace)
}
