use cc_scheduler::common::*;
use cc_shepherd::*;
use serde_json::{json, Value};
use std::{fs, path::Path};
struct Fake {
    calls: Vec<Vec<String>>,
    trees: Value,
    created: bool,
    prs: Value,
    todo: Value,
    todo_after: Option<Value>,
    todo_calls: usize,
    cleared: Vec<Value>,
    clear_fails: bool,
}
impl Default for Fake {
    fn default() -> Self {
        Self {
            calls: vec![],
            trees: json!({"a":"C:/w/a","b":"C:/w/b","c":"C:/w/c"}),
            created: true,
            todo: json!({"items":[]}),
            todo_after: None,
            todo_calls: 0,
            cleared: vec![],
            clear_fails: false,
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
    fn todo(&mut self) -> Result<Value> {
        self.todo_calls += 1;
        // The second read happens in the late recheck just before launch.
        match (&self.todo_after, self.todo_calls) {
            (Some(after), n) if n >= 2 => Ok(after.clone()),
            _ => Ok(self.todo.clone()),
        }
    }
    fn clear_dependency_ready(&mut self, ids: &[Value]) -> Result<()> {
        if self.clear_fails {
            return Err(Error("OSError"));
        }
        self.cleared.extend(ids.iter().cloned());
        Ok(())
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
    adopt_state(
        &mut state,
        key,
        "root880",
        std::slice::from_ref(&worker),
        10.,
    )
    .unwrap();
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

fn pr_one() -> Value {
    json!([{"number":1,"headRefName":"feat/one","url":"https://github.com/o/r/pull/1","isDraft":false}])
}
fn keys(ready: &[Value]) -> Vec<String> {
    ready.iter().map(|r| s(&r["key"]).to_owned()).collect()
}
fn worktree_calls(b: &Fake) -> usize {
    b.calls
        .iter()
        .filter(|c| c.iter().any(|a| a == "worktree" || a == "fetch"))
        .count()
}

#[test]
fn waiting_todo_suppresses_its_pr_before_a_worktree_exists_then_releases_when_target_is_done() {
    let mut b = Fake {
        trees: json!({}),
        created: false,
        prs: pr_one(),
        todo: json!({"projects":{},"items":[
            {"id":10,"status":"wait","wait_on":[11],"title":"仕上げ","note":"https://github.com/o/r/pull/1 の続き"},
            {"id":11,"status":"doing"}]}),
        ..Default::default()
    };
    let (ready, _) = candidates(&mut b, &repo(), &[], &json!({}), 10.).unwrap();
    assert!(ready.is_empty());
    assert_eq!(worktree_calls(&b), 0, "no fetch/worktree add while waiting");
    let (_, _, waits) = candidates_with_waits(&mut b, &repo(), &[], &json!({}), 10.).unwrap();
    assert_eq!(
        waits,
        ["o/r#1: ToDo 11 待ち"],
        "names the prerequisite, not the waiting ToDo"
    );
    // The prerequisite finishing releases the PR; the checkout is then prepared as usual.
    b.todo["items"][1]["status"] = json!("done");
    candidates(&mut b, &repo(), &[], &json!({}), 10.).unwrap();
    assert!(worktree_calls(&b) > 0);
}

#[test]
fn missing_wait_target_keeps_the_pr_suppressed() {
    let mut b = Fake {
        prs: pr_one(),
        trees: json!({"feat/one":"C:/w/one"}),
        todo: json!({"items":[{"id":10,"status":"todo","wait_on":[99],"note":"https://github.com/o/r/pull/1"}]}),
        ..Default::default()
    };
    assert!(candidates(&mut b, &repo(), &[], &json!({}), 10.)
        .unwrap()
        .0
        .is_empty());
}

#[test]
fn link_uses_explicit_jobs_and_exact_identifiers_only() {
    let waiting = |item: Value| {
        let mut row =
            json!({"project":"x","status":"wait","wait_on":[11],"id":10,"title":"","note":""});
        row.as_object_mut()
            .unwrap()
            .extend(item.as_object().unwrap().clone());
        Fake {
            prs: pr_one(),
            trees: json!({"feat/one":"C:/w/one"}),
            todo: json!({"projects":{"mine":"P","nested":"P/sub","other":"Z/elsewhere"},"items":[
            {"id":11,"status":"doing"},
            row]}),
            ..Default::default()
        }
    };
    let one = |item: Value| {
        let mut b = waiting(item);
        let jobs = [json!({"id":"j-old","status":"succeeded","cwd":"C:/w/one","name":"x"})];
        keys(
            &candidates(&mut b, &repo(), &jobs, &json!({}), 10.)
                .unwrap()
                .0,
        )
    };
    // Linked: the ToDo lists a job that ran in this PR's worktree.
    assert!(one(json!({"jobs":["j-old"]})).is_empty());
    // Linked: PR#1 in the note and the ToDo project path is inside this repository.
    assert!(one(json!({"project":"mine","note":"PR#1 を直す"})).is_empty());
    // Not linked: same note but the ToDo belongs to another repository.
    assert_eq!(
        one(json!({"project":"other","note":"PR#1 を直す"})),
        ["o/r#1"]
    );
    assert_eq!(
        one(json!({"project":"nested","note":"PR#1 を直す"})),
        ["o/r#1"]
    );
    // Not linked: PR#12 / pull/12 are different PRs; a stranger's job id proves nothing.
    assert_eq!(
        one(json!({"project":"mine","note":"PR#12 と https://github.com/o/r/pull/12"})),
        ["o/r#1"]
    );
    assert_eq!(one(json!({"jobs":["unrelated"]})), ["o/r#1"]);
}

#[test]
fn released_dependency_bypasses_rest_once() {
    let item = json!({"id":10,"status":"todo","dependency_ready":true,"note":"https://github.com/o/r/pull/2"});
    let prs = json!([{"number":2,"headRefName":"b","url":"https://github.com/o/r/pull/2"}]);
    let mut b = Fake {
        prs,
        todo: json!({"items":[item]}),
        ..Default::default()
    };
    let jobs = [json!({"id":"old","status":"succeeded","cwd":"C:/w/b","name":"shep-2"})];
    let rest = json!({"o/r#2":{"job":"old","ended":4500}});
    let (ready, _) = candidates(&mut b, &repo(), &jobs, &rest, 4600.).unwrap();
    assert_eq!(ready[0]["dependency_ids"], json!([10]));
    // Once a shepherd was launched for that release the marker no longer bypasses the rest period.
    let used = json!({"o/r#2":{"job":"old","ended":4500,"dependency_consumed":["10:0"]}});
    assert!(candidates(&mut b, &repo(), &jobs, &used, 4600.)
        .unwrap()
        .0
        .is_empty());
    // Without any marker the owner-decision rest still applies.
    b.todo = json!({"items":[]});
    assert!(candidates(&mut b, &repo(), &jobs, &rest, 4600.)
        .unwrap()
        .0
        .is_empty());
}

fn one_pr_fake(todo: Value) -> Fake {
    Fake {
        prs: json!([{"number":1,"headRefName":"a","url":"u1"}]),
        todo,
        ..Default::default()
    }
}
fn released(generation: u64) -> Value {
    json!({"items":[{"id":10,"status":"todo","dependency_ready":true,"dependency_generation":generation,"note":"u1 の続き"}]})
}
fn finish(jobs: &Path, id: &str) {
    let dir = jobs.join("jobs").join(id);
    fs::create_dir_all(&dir).unwrap();
    fs::write(
        dir.join("job.json"),
        json!({"id":id,"status":"succeeded","cwd":"C:/w/a"}).to_string(),
    )
    .unwrap();
}
#[test]
fn each_release_generation_launches_once_and_state_is_preserved() {
    let (_t, root, jobs) = fixture();
    fs::write(
        root.join("shepherd_state.json"),
        "{\"retained\":{\"hold\":true}}",
    )
    .unwrap();
    let mut runner = LaunchFake { calls: vec![] };
    let mut b = one_pr_fake(released(1));
    cmd_step(&root, &jobs, &mut b, &mut runner, 10000., false, &repo()).unwrap();
    assert_eq!(runner.calls.len(), 1);
    let args = &runner.calls[0].args;
    assert_eq!(
        args[args.iter().position(|a| a == "--todo-id").unwrap() + 1],
        "10"
    );
    assert_eq!(b.cleared, [json!("10:1")]);
    assert_eq!(
        args[args.iter().position(|a| a == "--related-todo-ids").unwrap() + 1],
        "10"
    );
    let state = read_json(&root.join("shepherd_state.json"), Value::Null).unwrap();
    assert_eq!(state["o/r#1"]["dependency_consumed"], json!(["10:1"]));
    assert_eq!(state["retained"]["hold"], true);
    // Same generation again (marker not cleared): the rest period applies, no second launch.
    finish(&jobs, "fixture-job");
    let mut stale = one_pr_fake(released(1));
    cmd_step(
        &root,
        &jobs,
        &mut stale,
        &mut runner,
        10001.,
        false,
        &repo(),
    )
    .unwrap();
    assert_eq!(runner.calls.len(), 1);
    // A new wait cycle (generation 2) bypasses the rest once more and keeps the old record.
    let mut next = one_pr_fake(released(2));
    cmd_step(&root, &jobs, &mut next, &mut runner, 10002., false, &repo()).unwrap();
    assert_eq!(runner.calls.len(), 2);
    let state = read_json(&root.join("shepherd_state.json"), Value::Null).unwrap();
    assert_eq!(
        state["o/r#1"]["dependency_consumed"],
        json!(["10:1", "10:2"])
    );
    assert_eq!(state["retained"]["hold"], true);
}
#[test]
fn failed_marker_clear_still_prevents_repeat_without_generation() {
    let (_t, root, jobs) = fixture();
    let mut runner = LaunchFake { calls: vec![] };
    let mut b = one_pr_fake(released(0));
    b.clear_fails = true;
    cmd_step(&root, &jobs, &mut b, &mut runner, 10000., false, &repo()).unwrap();
    assert_eq!(runner.calls.len(), 1);
    finish(&jobs, "fixture-job");
    let mut again = one_pr_fake(released(0));
    again.clear_fails = true;
    cmd_step(
        &root,
        &jobs,
        &mut again,
        &mut runner,
        10001.,
        false,
        &repo(),
    )
    .unwrap();
    assert_eq!(runner.calls.len(), 1);
}
#[test]
fn todo_that_starts_waiting_during_preparation_is_not_launched() {
    let (_t, root, jobs) = fixture();
    let mut runner = LaunchFake { calls: vec![] };
    let mut b = one_pr_fake(json!({"items":[{"id":10,"status":"doing","note":"u1"}]}));
    b.todo_after = Some(
        json!({"items":[{"id":10,"status":"wait","wait_on":[11],"note":"u1"},{"id":11,"status":"doing"}]}),
    );
    cmd_step(&root, &jobs, &mut b, &mut runner, 10000., false, &repo()).unwrap();
    assert!(runner.calls.is_empty());
    let state = read_json(&root.join("shepherd_state.json"), json!({})).unwrap();
    assert!(state.get("o/r#1").is_none());
}
