use cc_scheduler::policy::Processes;
use cc_todo::{Command, Paths, execute, recovery::plan};
use serde_json::{Value, json};
use std::path::Path;

fn paths() -> Paths {
    Paths::at(Path::new("unused"), Path::new("unused/jobs"))
}
fn db() -> Value {
    json!({"projects":{"demo":"C:/repo"},"items":[
        {"id":1,"project":"demo","title":"依存元","status":"doing","jobs":["old"],"note":"既存メモ","progress":60,"spec":"s.md","dispatchable":true,
         "scheduler":{"job":"old","key":"old-key","worktree":"C:/repo-wt/task","not_before":999999},
         "stall_recovery":{"next_at":999999,"history":[10000,10000],"pending":{"request":"stale"}}},
        {"id":2,"project":"demo","title":"待ち先","status":"doing","jobs":[],"note":""},
        {"id":3,"project":"demo","title":"待ち先2","status":"block","jobs":[],"note":""}
    ],"next_id":4})
}
#[test]
fn wait_keeps_history_progress_specs_and_shows_dependency() {
    let mut d = db();
    execute(
        &mut d,
        &Command::Wait {
            id: 1,
            on: vec![2, 2],
            progress: None,
        },
        &paths(),
    )
    .unwrap();
    assert_eq!(d["items"][0]["status"], "wait");
    assert_eq!(d["items"][0]["wait_on"], json!([2]));
    assert_eq!(d["items"][0]["progress"], 60);
    assert_eq!(d["items"][0]["jobs"], json!(["old"]));
    assert_eq!(d["items"][0]["note"], "既存メモ");
    assert_eq!(d["items"][0]["scheduler"]["worktree"], "C:/repo-wt/task");
    let listed = execute(
        &mut d,
        &Command::List {
            project: None,
            all: false,
        },
        &paths(),
    )
    .unwrap()
    .join("\n");
    assert!(listed.contains("ToDo 2 待ち"));
    let before = d.clone();
    assert!(
        execute(
            &mut d,
            &Command::Start {
                id: 1,
                job: "new".into(),
                progress: None
            },
            &paths()
        )
        .is_err()
    );
    assert_eq!(before, d);
}
#[test]
fn invalid_cycle_and_missing_dependency_leave_store_unchanged() {
    let mut d = db();
    execute(
        &mut d,
        &Command::Wait {
            id: 1,
            on: vec![2],
            progress: None,
        },
        &paths(),
    )
    .unwrap();
    for (id, on) in [(2, vec![1]), (1, vec![1]), (1, vec![999])] {
        let before = d.clone();
        assert!(
            execute(
                &mut d,
                &Command::Wait {
                    id,
                    on,
                    progress: None
                },
                &paths()
            )
            .is_err()
        );
        assert_eq!(before, d);
    }
}
#[test]
fn done_releases_all_dependents_without_retry_delay_and_never_owner_block() {
    let mut d = db();
    execute(
        &mut d,
        &Command::Wait {
            id: 1,
            on: vec![2, 3],
            progress: None,
        },
        &paths(),
    )
    .unwrap();
    execute(
        &mut d,
        &Command::Done {
            id: 2,
            note: "".into(),
        },
        &paths(),
    )
    .unwrap();
    assert_eq!(d["items"][0]["status"], "wait");
    execute(
        &mut d,
        &Command::Done {
            id: 3,
            note: "".into(),
        },
        &paths(),
    )
    .unwrap();
    assert_eq!(d["items"][0]["status"], "todo");
    assert_eq!(d["items"][0]["dependency_ready"], true);
    assert_eq!(d["items"][0]["scheduler"]["not_before"], 0);
    assert_eq!(d["items"][0]["scheduler"]["worktree"], "C:/repo-wt/task");
    let jobs = [
        json!({"id":"old","status":"succeeded","finished_at":10000,"cwd":"C:/repo-wt/task","last_message":"状態: 継続中"}),
    ];
    assert_eq!(
        plan(&mut d, &jobs, &Processes::new(), 10001., "").unwrap(),
        vec![1]
    );
}
#[test]
fn watchdog_never_reserves_waiting_item_and_observes_external_done() {
    let mut d = db();
    execute(
        &mut d,
        &Command::Wait {
            id: 1,
            on: vec![2],
            progress: None,
        },
        &paths(),
    )
    .unwrap();
    d["items"][1]["status"] = json!("block");
    assert!(
        !plan(&mut d, &[], &Processes::new(), 20000., "")
            .unwrap()
            .contains(&1)
    );
    assert!(!d["items"][0]["stall_recovery"]["pending"].is_object());
    d["items"][1]["status"] = json!("done");
    assert!(
        plan(&mut d, &[], &Processes::new(), 20001., "")
            .unwrap()
            .contains(&1)
    );
}
#[test]
fn already_completed_target_does_not_leave_a_permanent_wait() {
    let mut d = db();
    d["items"][1]["status"] = json!("done");
    execute(
        &mut d,
        &Command::Wait {
            id: 1,
            on: vec![2],
            progress: None,
        },
        &paths(),
    )
    .unwrap();
    assert_eq!(d["items"][0]["status"], "todo");
    assert!(d["items"][0]["wait_on"].is_null());
}
