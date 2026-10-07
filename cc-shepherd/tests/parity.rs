use cc_scheduler::common::*;
use cc_shepherd::*;
use serde_json::{json, Value};
use std::{fs, path::Path};
struct Fake {
    calls: Vec<Vec<String>>,
    trees: Value,
    created: bool,
    prs: Value,
}
impl Default for Fake {
    fn default() -> Self {
        Self {
            calls: vec![],
            trees: json!({"a":"C:/w/a","b":"C:/w/b","c":"C:/w/c"}),
            created: true,
            prs: json!([{"number":1,"headRefName":"a","url":"u1","isDraft":false},{"number":2,"headRefName":"b","url":"u2","isDraft":false},{"number":3,"headRefName":"c","url":"u3","isDraft":false}]),
        }
    }
}
impl Backend for Fake {
    fn run(&mut self, a: &[&str]) -> Result<String> {
        self.calls.push(a.iter().map(|s| s.to_string()).collect());
        if a[..3] == ["gh", "pr", "list"] {
            Ok(self.prs.to_string())
        } else {
            Ok("".into())
        }
    }
    fn trees(&mut self, _: &Value) -> Result<Value> {
        Ok(self.trees.clone())
    }
    fn is_dir(&self, _: &str) -> bool {
        self.created
    }
    fn last_state(&self, id: &str) -> String {
        if id == "old" {
            "状態: 要判断"
        } else {
            "状態: 完了"
        }
        .into()
    }
}
fn repo() -> Vec<Value> {
    vec![json!({"slug":"o/r","path":"P","wt":"W"})]
}
fn jobs() -> Vec<Value> {
    vec![
        json!({"id":"impl","status":"running","cwd":"C:/w/a","name":"civ-x"}),
        json!({"id":"old","status":"succeeded","cwd":"C:/w/b","name":"shep-2"}),
    ]
}
#[test]
fn original_busy_implementation_and_owner_decision_rest() {
    let (ready, running) = candidates(
        &mut Fake::default(),
        &repo(),
        &jobs(),
        &json!({"o/r#2":{"job":"old","ended":1000}}),
        4600.,
    )
    .unwrap();
    assert_eq!(
        ready.iter().map(|r| s(&r["key"])).collect::<Vec<_>>(),
        ["o/r#3"]
    );
    assert_eq!(running, 0);
}

#[test]
fn explicit_main_worker_suppresses_only_its_pr_across_worktrees() {
    let key = "AutoDevJapan/civ6-banseiki#86";
    let worker = json!({"id":"root880","status":"running","cwd":"C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki-wt/agent-todo-880-owned","name":"orch-todo-880"});
    let mut state =
        json!({key:{"job":"old","ended":1,"hold":false},"other/repo#1":{"job":"untouched"}});
    adopt_state(&mut state, key, "root880", &[worker.clone()], 10.).unwrap();
    assert_eq!(state[key]["job"], "root880");
    assert!(state[key].get("ended").is_none());
    assert_eq!(state["other/repo#1"]["job"], "untouched");
    let mut b = Fake {
        trees: json!({"other":"C:/w/other-pr"}),
        created: true,
        prs: json!([{"number":86,"headRefName":"feat/leaderperson","url":"u86"},{"number":87,"headRefName":"other","url":"u87"}]),
        ..Default::default()
    };
    let (ready, _) = candidates(&mut b, &repos(), &[worker], &state, 10000.).unwrap();
    assert_eq!(
        ready.iter().map(|v| s(&v["key"])).collect::<Vec<_>>(),
        ["AutoDevJapan/civ6-banseiki#87"]
    );
    // An adopted live worker needs no spare checkout of its public branch.
    assert!(b
        .calls
        .iter()
        .all(|c| !c.iter().any(|s| s == "feat/leaderperson")));
}

#[test]
fn explicit_main_worker_rejects_stopped_foreign_or_unknown_workers_without_change() {
    let key = "AutoDevJapan/civ6-banseiki#86";
    for worker in [
        json!({"id":"j","status":"succeeded","cwd":"C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki-wt/a"}),
        json!({"id":"j","status":"running","cwd":"C:/unrelated"}),
        json!({"id":"j","status":"running","cwd":"C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki-wt-foreign/a"}),
        json!({"id":"j","status":"running"}),
    ] {
        let mut state = json!({key:{"job":"previous","ended":1}});
        let before = state.clone();
        assert!(adopt_state(&mut state, key, "j", &[worker], 2.).is_err());
        assert_eq!(state, before);
    }
    for key in [
        "unknown/repo#86",
        "AutoDevJapan/civ6-banseiki#0",
        "AutoDevJapan/civ6-banseiki#086",
        "AutoDevJapan/civ6-banseiki#86x",
    ] {
        assert!(adopt_state(&mut json!({}), key, "j", &[], 2.).is_err());
    }
}

#[test]
fn adopted_worker_completion_returns_to_existing_cooldown() {
    let key = "AutoDevJapan/civ6-banseiki#86";
    let state = json!({key:{"job":"root880","adopted_main_worker":true,"ended":1000}});
    let jobs = vec![json!({"id":"root880","status":"succeeded","cwd":"different"})];
    let mut b = Fake {
        trees: json!({"feat/leaderperson":"C:/w/leaderperson"}),
        prs: json!([{"number":86,"headRefName":"feat/leaderperson","url":"u"}]),
        ..Default::default()
    };
    assert!(candidates(&mut b, &repos(), &jobs, &state, 1500.)
        .unwrap()
        .0
        .is_empty());
    assert_eq!(
        candidates(&mut b, &repos(), &jobs, &state, 3000.)
            .unwrap()
            .0
            .len(),
        1
    );
}
#[test]
fn original_decision_cooldown_expires() {
    let (ready, _) = candidates(
        &mut Fake::default(),
        &repo(),
        &jobs(),
        &json!({"o/r#2":{"job":"old","ended":1000}}),
        15400.,
    )
    .unwrap();
    assert!(ready.iter().any(|r| s(&r["key"]) == "o/r#2"));
}
#[test]
fn holds_live_shepherds_missing_ended_and_normal_cooldown() {
    let state =
        json!({"o/r#1":{"hold":true},"o/r#2":{"job":"done","ended":1000},"o/r#3":{"job":"live"}});
    let mut jobs = jobs();
    jobs.push(json!({"id":"live","status":"queued","cwd":"X","name":"shep-3"}));
    let (ready, running) = candidates(&mut Fake::default(), &repo(), &jobs, &state, 2800.).unwrap();
    assert_eq!(running, 1);
    assert_eq!(
        ready.iter().map(|r| s(&r["key"])).collect::<Vec<_>>(),
        ["o/r#2"]
    );
    let (ready, _) = candidates(
        &mut Fake::default(),
        &repo(),
        &[],
        &json!({"o/r#2":{"job":"done"},"o/r#1":{"hold":true},"o/r#3":{"hold":true}}),
        90000.,
    )
    .unwrap();
    assert!(ready.is_empty());
}
#[test]
fn missing_worktree_fetches_and_adds_then_skips_failed_creation() {
    let mut b = Fake {
        trees: json!({}),
        created: false,
        ..Default::default()
    };
    let (ready, _) = candidates(&mut b, &repo(), &[], &json!({}), 1000.).unwrap();
    assert!(ready.is_empty());
    assert_eq!(b.calls.len(), 7);
    assert_eq!(b.calls[1], ["git", "-C", "P", "fetch", "-q", "origin", "a"]);
    assert_eq!(
        b.calls[2],
        ["git", "-C", "P", "worktree", "add", "-q", "W/pr-1", "a"]
    );
}
#[test]
fn porcelain_parser_excludes_main_and_detached_worktrees() {
    assert_eq!(parse_trees("worktree C:/main\nbranch refs/heads/main\n\nworktree C:/w/a\nHEAD abc\nbranch refs/heads/feature/a\n\nworktree C:/w/detached\ndetached\n\n"),json!({"feature/a":"C:/w/a"}));
}
#[test]
fn prompt_format_preserves_escaped_braces() {
    let r =
        json!({"pr":{"url":"url","headRefName":"branch"},"repo":{"slug":"o/r"},"path":"worktree"});
    assert_eq!(
        render("{PR_URL} {REPO} {BRANCH} {WORKTREE} {{value}}", &r).unwrap(),
        "url o/r branch worktree {value}"
    );
    assert!(render("{MISSING}", &r).is_err());
}
struct LaunchFake {
    calls: Vec<Invocation>,
}
impl Runner for LaunchFake {
    fn run(&mut self, c: &Invocation) -> Result<Output> {
        self.calls.push(c.clone());
        Ok(Output {
            code: 0,
            stdout: "fixture-job\n".into(),
            stderr: String::new(),
        })
    }
}
fn fixture() -> (tempfile::TempDir, std::path::PathBuf, std::path::PathBuf) {
    let temp = tempfile::tempdir().unwrap();
    let root = temp.path().join("shepherd");
    let jobs = temp.path().join("codex-jobs");
    fs::create_dir_all(&root).unwrap();
    fs::create_dir_all(jobs.join("jobs")).unwrap();
    fs::write(root.join("shepherd_prompt.md"), "{PR_URL} {WORKTREE}").unwrap();
    (temp, root, jobs)
}
#[test]
fn dry_run_does_not_save_or_launch() {
    let (_t, root, jobs) = fixture();
    fs::write(
        root.join("shepherd_state.json"),
        "{\n \"o/r#2\": {\"job\": \"old\"}\n}",
    )
    .unwrap();
    let before = fs::read(root.join("shepherd_state.json")).unwrap();
    let mut runner = LaunchFake { calls: vec![] };
    let result = cmd_step(
        &root,
        &jobs,
        &mut Fake::default(),
        &mut runner,
        10000.,
        true,
        &repo(),
    )
    .unwrap()
    .unwrap();
    assert_eq!(result["running_shepherds"], 0);
    assert!(runner.calls.is_empty());
    assert_eq!(fs::read(root.join("shepherd_state.json")).unwrap(), before);
}
#[test]
fn launches_at_most_one_and_keeps_existing_state() {
    let (_t, root, jobs) = fixture();
    fs::write(
        root.join("shepherd_state.json"),
        "{\"retained\":{\"hold\":true,\"future\":42}}",
    )
    .unwrap();
    let mut runner = LaunchFake { calls: vec![] };
    cmd_step(
        &root,
        &jobs,
        &mut Fake::default(),
        &mut runner,
        10000.,
        false,
        &repo(),
    )
    .unwrap();
    assert_eq!(runner.calls.len(), 1);
    assert!(runner.calls[0].args.iter().any(|a| a == "pr_shepherd"));
    let state = read_json(&root.join("shepherd_state.json"), Value::Null).unwrap();
    assert_eq!(state["retained"]["future"], 42);
    assert_eq!(state["o/r#1"]["job"], "fixture-job");
    assert_eq!(
        fs::read_to_string(root.join("prompts/shep-1.md")).unwrap(),
        "u1 C:/w/a"
    );
}
#[test]
fn four_active_shepherds_prevent_launch_and_finished_time_is_saved() {
    let (_t, root, jobs) = fixture();
    for i in 0..4 {
        let dir = jobs.join("jobs").join(format!("j{i}"));
        fs::create_dir_all(&dir).unwrap();
        fs::write(dir.join("job.json"),json!({"id":format!("j{i}"),"name":format!("shep-{i}"),"cwd":format!("other{i}"),"status":"running"}).to_string()).unwrap();
    }
    let dir = jobs.join("jobs/done");
    fs::create_dir_all(&dir).unwrap();
    fs::write(
        dir.join("job.json"),
        json!({"id":"done","status":"succeeded"}).to_string(),
    )
    .unwrap();
    fs::write(
        root.join("shepherd_state.json"),
        "{\"o/r#2\":{\"job\":\"done\"}}",
    )
    .unwrap();
    let mut runner = LaunchFake { calls: vec![] };
    cmd_step(
        &root,
        &jobs,
        &mut Fake::default(),
        &mut runner,
        10000.,
        false,
        &repo(),
    )
    .unwrap();
    assert!(runner.calls.is_empty());
    assert_eq!(
        n(&read_json(&root.join("shepherd_state.json"), Value::Null).unwrap()["o/r#2"]["ended"]),
        10000.
    );
}
#[test]
fn status_returns_python_text_and_default_empty_object() {
    let (_t, root, _jobs) = fixture();
    assert_eq!(status(&root).unwrap(), "{}");
    let text = "{\r\n \"日本語\": {\"started\": 123.456}\r\n}\r\n";
    fs::write(root.join("shepherd_state.json"), text).unwrap();
    assert_eq!(status(&root).unwrap(), text.replace("\r\n", "\n"));
}
#[test]
fn failed_command_exit_does_not_change_original_launcher_behavior() {
    let c = launch_command(
        Path::new("D:/fixture"),
        &json!({"pr":{"number":1,"title":"捕虜を返還できない問題を修正する"},"path":"D:/civ6-wt/fix"}),
        Path::new("D:/prompt.md"),
    );
    assert!(c.args.iter().any(|a| a == "danger-full-access"));
    assert!(c.args.iter().any(|a| a == "gpt-6.1-sol"));
    assert_eq!(
        c.args[c.args.iter().position(|v| v == "--task-title").unwrap() + 1],
        "捕虜を返還できない問題を修正する（レビュー・仕上げ）"
    );
    assert_eq!(
        c.args[c.args.iter().position(|v| v == "--project").unwrap() + 1],
        "civ6"
    );
    assert_eq!(c.timeout, None);
}
#[test]
fn english_pr_title_is_summarized_before_adding_review_work() {
    let c = launch_command(
        Path::new("D:/fixture"),
        &json!({"pr":{"number":86,"title":"Fix crash when returning captives"},"path":"D:/civ6-wt/fix"}),
        Path::new("D:/prompt.md"),
    );
    let title = &c.args[c.args.iter().position(|v| v == "--task-title").unwrap() + 1];
    assert_eq!(
        title,
        "異常終了 時の 捕虜の返還を修正する（レビュー・仕上げ）"
    );
    assert!(!title.contains("Fix crash"));
}
