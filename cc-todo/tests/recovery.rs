use cc_scheduler::policy::Processes;
use cc_todo::{recovery::*, *};
use serde_json::{Value, json};
use std::{fs, path::PathBuf};

fn db(status: &str) -> Value {
    let mut db = json!({"projects":{"demo":"C:/repo"},"items":[{"id":1,"project":"demo","title":"実装を続ける","note":"残りの検証","status":status,"jobs":[]}],"stalls":{"demo":{"since":1}},"next_id":2});
    if status == "todo" {
        db["items"][0]["spec"] = json!("spec.md");
        db["items"][0]["dispatchable"] = json!(true);
    }
    db
}
fn job(status: &str) -> Value {
    json!({"id":"old","status":status,"cwd":"C:/repo-wt/task","created_at":1,"started_at":1,"last_event_at":8000,"finished_at":8000,"pid":100,"pid_created":10,"codex_pid":101,"codex_pid_created":11,"model":"gpt-6-astra","effort":"max","thread_id":"01a0fa4b-2898-7553-889f-0e934f0d32f8","last_message":"検証が残っています","last_error":"通信が途切れました"})
}
fn processes() -> Processes {
    [
        (100, json!({"pid":100,"created":10,"ppid":0})),
        (101, json!({"pid":101,"created":11,"ppid":100})),
    ]
    .into_iter()
    .collect()
}
#[test]
fn duplicate_pr_recovery_becomes_owner_completion_wait_without_dispatch() {
    let mut d = db("doing");
    d["items"][0]["jobs"] = json!(["owner"]);
    d["items"].as_array_mut().unwrap().push(json!({"id":2,"project":"demo","status":"doing","title":"PR86を修正","jobs":["previous"],"spec":"saved","note":"original evidence"}));
    let mut owner = job("running");
    owner["id"] = json!("owner");
    owner["todo_id"] = json!(1);
    owner["last_event_at"] = json!(9900);
    owner["work_owner"] = json!({"repository":"o/r","pr":86,"worktree":"/owner"});
    let old = json!({"id":"previous","status":"succeeded","todo_id":2,"created_at":1,"work_owner":{"repository":"o/r","pr":86,"worktree":"/other"}});
    assert!(
        plan(&mut d, &[owner, old], &processes(), 10000., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(d["items"][1]["status"], "wait");
    assert_eq!(d["items"][1]["wait_on"], json!([1]));
    assert_eq!(d["items"][1]["spec"], "saved");
    assert_eq!(d["items"][1]["note"], "original evidence");
    assert!(d["items"][1]["stall_recovery"]["pending"].is_null());
}
#[test]
fn running_stale_and_missing_tree_restart_but_fresh_and_reused_pids_do_not_look_alive() {
    for ps in [
        processes(),
        Processes::new(),
        [(100, json!({"pid":100,"created":99}))]
            .into_iter()
            .collect(),
    ] {
        let mut d = db("doing");
        d["items"][0]["jobs"] = json!(["old"]);
        assert_eq!(
            plan(&mut d, &[job("running")], &ps, 10000., "").unwrap(),
            vec![1]
        );
        let p = &d["items"][0]["stall_recovery"]["pending"];
        assert_eq!(p["kind"], "restart");
        assert_eq!(
            p["reason"],
            if ps.get(&100).is_some_and(|p| p["created"] == 10) {
                "30分以上進捗がなかった"
            } else {
                "ジョブのプロセスが消えた"
            }
        );
    }
    let mut d = db("doing");
    d["items"][0]["jobs"] = json!(["old"]);
    let mut j = job("running");
    j["last_event_at"] = json!(9900);
    assert!(
        plan(&mut d, &[j], &processes(), 10000., "")
            .unwrap()
            .is_empty()
    );
    assert!(d["stalls"].get("demo").is_none());
}
#[test]
fn terminal_jobs_continue_with_previous_result_and_error_in_prompt() {
    for status in ["succeeded", "failed", "lost", "finished"] {
        let mut d = db("doing");
        d["items"][0]["jobs"] = json!(["old"]);
        assert_eq!(
            plan(&mut d, &[job(status)], &Processes::new(), 10000., "").unwrap(),
            vec![1]
        );
        let p = &d["items"][0]["stall_recovery"]["pending"];
        assert_eq!(p["kind"], "delegate");
        assert!(p["prompt"].as_str().unwrap().contains("検証が残っています"));
        assert!(p["prompt"].as_str().unwrap().contains("通信が途切れました"));
        assert_eq!(p["request"], "stall-1-1");
    }
}
#[test]
fn spec_handoff_reserves_but_unspecified_todo_only_appears_on_board() {
    for spec in [true, false] {
        let mut d = db("todo");
        if spec {
            d["items"][0]["dispatchable"] = json!(true);
            d["items"][0]["spec"] = json!("spec.md");
        } else {
            d["items"][0]["spec"] = Value::Null;
        }
        assert_eq!(
            plan(&mut d, &[], &Processes::new(), 10000., "").unwrap(),
            if spec { vec![1] } else { vec![] }
        );
        assert_eq!(
            d["items"][0]["stall_recovery"]["pending"]["kind"],
            if spec {
                json!("scheduler")
            } else {
                Value::Null
            }
        );
        let mut busy = db("todo");
        if spec {
            busy["items"][0]["dispatchable"] = json!(true);
            busy["items"][0]["spec"] = json!("spec.md");
        } else {
            busy["items"][0]["spec"] = Value::Null;
        }
        assert_eq!(
            plan(&mut busy, &[job("running")], &processes(), 10000., "").unwrap(),
            if spec { vec![1] } else { vec![] },
            "an unrelated job cannot claim an unassigned work item"
        );
    }
}
#[test]
fn repeated_failures_keep_work_open_and_retry_hourly() {
    let mut d = db("doing");
    d["items"][0]["stall_recovery"] = json!({"history":[9000,9500],"attempt":2});
    assert!(
        plan(&mut d, &[], &Processes::new(), 10000., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(d["items"][0]["status"], "doing");
    assert_eq!(d["items"][0]["note"], "残りの検証");
    assert_eq!(d["items"][0]["stall_recovery"]["next_at"], 13100.);
    assert!(
        d["items"][0]["stall_recovery"]["report"]
            .as_str()
            .unwrap()
            .contains("1時間ごとに再試行")
    );
    assert!(
        plan(&mut d, &[], &Processes::new(), 13099., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        plan(&mut d, &[], &Processes::new(), 13100., "").unwrap(),
        vec![1]
    );
    assert_eq!(
        d["items"][0]["stall_recovery"]["pending"]["request"],
        "stall-1-3"
    );
    assert_eq!(d["items"][0]["status"], "doing");
}
#[test]
fn discord_recovery_migrates_old_six_hour_delay_to_hourly() {
    let mut d = db("doing");
    d["items"][0]["jobs"] = json!(["old"]);
    d["items"][0]["discord_restart_thread"] = json!("1234567890123456789");
    d["items"][0]["discord_restart_history"] = json!([9000, 9500]);
    d["items"][0]["discord_restart_not_before"] = json!(30600);
    assert!(
        plan(&mut d, &[job("failed")], &Processes::new(), 13099., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        plan(&mut d, &[job("failed")], &Processes::new(), 13100., "").unwrap(),
        vec![1]
    );
    assert_eq!(d["items"][0]["status"], "doing");
}
#[test]
fn intentional_cancel_is_not_restarted_by_stall_watchdog() {
    for status in ["cancelled", "lost"] {
        let mut d = db("doing");
        d["items"][0]["jobs"] = json!(["old"]);
        let mut j = job(status);
        j["cancel_requested"] = json!(true);
        assert!(
            plan(&mut d, &[j], &Processes::new(), 10000., "")
                .unwrap()
                .is_empty()
        );
        assert_eq!(d["items"][0]["status"], "doing");
    }
}
#[test]
fn all_projects_are_reserved_and_pending_requests_are_stable() {
    let mut d = db("todo");
    for id in 2..=5 {
        let name = format!("project{id}");
        d["projects"][&name] = json!(format!("C:/repo{id}"));
        d["stalls"][&name] = json!({"since":1});
        d["items"]
            .as_array_mut()
            .unwrap()
            .push(json!({"id":id,"project":name,"title":"続ける","status":"todo","spec":"spec.md","dispatchable":true,"jobs":[]}));
    }
    assert_eq!(
        plan(&mut d, &[], &Processes::new(), 10000., "").unwrap(),
        vec![1, 2, 3, 4, 5]
    );
    let reservations = d["items"].clone();
    assert!(
        plan(&mut d, &[], &Processes::new(), 10060., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        d["items"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|i| i["stall_recovery"]["pending"].is_object())
            .count(),
        5
    );
    assert_eq!(d["items"], reservations);
}

#[test]
fn healthy_and_pending_items_do_not_suppress_other_work_in_the_same_project() {
    let mut d = db("doing");
    d["items"][0]["jobs"] = json!(["old"]);
    let mut running = job("running");
    running["last_event_at"] = json!(9900);
    let mut dead = job("running");
    dead["id"] = json!("dead");
    dead["pid"] = Value::Null;
    dead["codex_pid"] = Value::Null;
    d["items"].as_array_mut().unwrap().extend([
        json!({"id":2,"project":"demo","status":"todo","spec":"spec.md","dispatchable":true,"title":"未担当の実装","jobs":[]}),
        json!({"id":3,"project":"demo","status":"doing","title":"消えた担当の検証","jobs":["dead"]}),
        json!({"id":4,"project":"demo","status":"todo","spec":"spec.md","dispatchable":true,"jobs":[],"stall_recovery":{"pending":{"request":"already-queued","key":"existing-key"}}}),
    ]);
    let queued = d["items"][3].clone();
    assert_eq!(
        plan(&mut d, &[running, dead], &processes(), 10000., "").unwrap(),
        vec![3, 2]
    );
    assert!(d["items"][0]["stall_recovery"]["pending"].is_null());
    assert_eq!(
        d["items"][1]["stall_recovery"]["pending"]["kind"],
        "scheduler"
    );
    assert_eq!(
        d["items"][2]["stall_recovery"]["pending"]["kind"],
        "restart"
    );
    assert_eq!(d["items"][3], queued);
}

#[test]
fn explicit_todo_metadata_and_scheduler_links_prevent_only_the_same_item_duplicate() {
    for association in ["todo_id", "scheduler", "session"] {
        let mut d = db("doing");
        let mut running = job("running");
        running["last_event_at"] = json!(9900);
        match association {
            "todo_id" => {
                running["todo_id"] = json!(1);
                running["project"] = json!("demo");
            }
            "scheduler" => d["items"][0]["scheduler"] = json!({"job":"old"}),
            _ => d["items"][0]["jobs"] = json!([running["thread_id"]]),
        }
        d["items"].as_array_mut().unwrap().push(
            json!({"id":2,"project":"demo","title":"別の未担当作業","status":"todo","spec":"spec.md","dispatchable":true,"jobs":[]}),
        );
        assert_eq!(
            plan(&mut d, &[running], &processes(), 10000., "").unwrap(),
            vec![2],
            "{association}"
        );
        assert!(d["items"][0]["stall_recovery"]["pending"].is_null());
        assert_eq!(
            d["items"][1]["stall_recovery"]["pending"]["request"],
            "stall-2-1"
        );
    }
}

#[test]
fn absence_timers_use_item_creation_and_linked_progress_instead_of_project_activity() {
    let mut d = db("todo");
    d["items"][0]["created_s"] = json!(8000);
    d["items"].as_array_mut().unwrap().push(json!({"id":2,"project":"demo","title":"新しい作業","status":"doing","created_s":9999,"jobs":[]}));
    let mut unrelated = job("running");
    unrelated["last_event_at"] = json!(10000);
    assert_eq!(
        plan(&mut d, &[unrelated.clone()], &processes(), 10000., "").unwrap(),
        vec![1]
    );
    assert_eq!(d["items"][1]["stall_recovery"]["since"], 9999.);
    unrelated["last_event_at"] = json!(11198);
    assert!(
        plan(&mut d, &[unrelated.clone()], &processes(), 11198., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        plan(&mut d, &[unrelated], &processes(), 11199., "").unwrap(),
        vec![2]
    );
}

#[test]
fn workspaces_reuse_free_owned_checkouts_and_request_isolation_for_actual_overlap() {
    let d = db("doing");
    let old = job("failed");
    for (cwd, isolate) in [
        ("C:/repo-wt/other", false),
        ("C:/repo-wt/task-other", false),
        ("c:/REPO-wt/TASK", true),
        ("C:/repo-wt/task/src", true),
        ("C:/unrelated", false),
    ] {
        let other = json!({"id":"other-owner","status":"running","cwd":cwd});
        let got = workspace(&d["items"][0], &old, &d, &[other], "C:/repo");
        assert_eq!(got["cwd"], "C:/repo-wt/task");
        assert_eq!(got["isolate_worktree"], isolate, "{cwd}");
    }
    let got = workspace(&d["items"][0], &Value::Null, &d, &[], "C:/repo");
    assert_eq!(got, json!({"cwd":"C:/repo","isolate_worktree":true}));
    let mut d = d;
    d["items"][0]["scheduler"] = json!({"worktree":"C:/repo-wt/assigned"});
    let got = workspace(&d["items"][0], &old, &d, &[], "C:/repo");
    assert_eq!(
        got,
        json!({"cwd":"C:/repo-wt/assigned","isolate_worktree":false})
    );
    d["items"].as_array_mut().unwrap().push(json!({"id":2,"status":"doing","stall_recovery":{"pending":{"workspace":{"cwd":"C:/repo-wt/assigned","isolate_worktree":false}}}}));
    assert_eq!(
        workspace(&d["items"][0], &old, &d, &[], "C:/repo")["isolate_worktree"],
        true
    );
}

#[test]
fn successful_continuations_resume_and_recent_repetitions_request_a_design_review() {
    let mut d = db("doing");
    let mut jobs = vec![];
    for (id, finished, message) in [
        ("old-repeat", 1000., "状態: 継続中\n検証が残っています"),
        (
            "recent-1",
            48000.,
            "状態: 継続中\n細分化したToDoを登録しました",
        ),
        (
            "recent-2",
            49990.,
            "状態: 継続中\n子作業の統合が残っています",
        ),
        ("complete", 48000., "状態: 完了\n依頼を完了しました"),
        (
            "unrelated",
            49990.,
            "状態: 継続中\n別作業の検証が残っています",
        ),
    ] {
        let mut j = job("succeeded");
        j["id"] = json!(id);
        j["created_at"] = json!(finished - 100.);
        j["finished_at"] = json!(finished);
        j["last_event_at"] = json!(finished);
        j["last_message"] = json!(message);
        j["last_error"] = Value::Null;
        jobs.push(j);
    }
    d["items"][0]["jobs"] = json!(["old-repeat", "recent-1", "recent-2", "complete"]);
    assert_eq!(
        plan(&mut d, &jobs, &Processes::new(), 50000., "").unwrap(),
        vec![1]
    );
    let pending = &d["items"][0]["stall_recovery"]["pending"];
    assert_eq!(pending["recent_continuations"], 2);
    let prompt = pending["prompt"].as_str().unwrap();
    assert!(prompt.contains("直近6時間に同じ作業の未完了の引き継ぎが2回"));
    assert!(prompt.contains("進め方を設計から見直してください"));
    assert!(prompt.contains("元のToDoを維持"));
    assert!(prompt.contains("不足の説明や反復回数だけ"));
    let request = delegation_request(&d["items"][0], pending);
    assert_eq!(
        request["resume_session"],
        "01a0fa4b-2898-7553-889f-0e934f0d32f8"
    );
    assert_eq!(request["source_job_id"], "recent-2");
    assert_eq!(request["cwd"], "C:/repo-wt/task");
    assert_eq!(request["request_id"], "stall-1-1");
    assert_eq!(d["items"].as_array().unwrap().len(), 1);
    assert_eq!(d["items"][0]["status"], "doing");
}

#[test]
fn concrete_owner_decisions_and_explicit_stops_are_preserved_but_missing_details_are_not_blocks() {
    for (message, stop) in [
        ("状態: 要判断\n公開してよいですか。\na. 公開\nb. 停止", true),
        ("状態: 要判断\n資源の空きを待っています。", false),
        ("状態: 継続中\n説明の不足を整理し直します。", false),
        (
            "状態: 継続中\n前回の質問:\n```\n公開してよいですか。\n```",
            false,
        ),
    ] {
        let mut d = db("doing");
        d["items"][0]["jobs"] = json!(["old"]);
        let mut j = job("succeeded");
        j["last_message"] = json!(message);
        let selected = plan(&mut d, &[j], &Processes::new(), 10000., "").unwrap();
        assert_eq!(selected, if stop { vec![] } else { vec![1] }, "{message}");
        assert_eq!(d["items"][0]["status"], "doing");
    }
    for state in ["block", "done", "cancelled"] {
        let mut d = db(state);
        assert!(
            plan(&mut d, &[], &Processes::new(), 10000., "")
                .unwrap()
                .is_empty()
        );
        assert_eq!(d["items"][0]["status"], state);
    }
    for marker in [
        json!({"stopped":true}),
        json!({"cancel_requested":true}),
        json!({"scheduler":{"question":{"id":"waiting"}}}),
        json!({"scheduler":{"answer":"stop"},"note":"オーナーの回答に従い、停止したままにします。"}),
    ] {
        let mut d = db("doing");
        for (key, value) in marker.as_object().unwrap() {
            d["items"][0][key] = value.clone();
        }
        assert!(
            plan(&mut d, &[], &Processes::new(), 10000., "")
                .unwrap()
                .is_empty(),
            "{marker}"
        );
    }
    let mut d = db("doing");
    d["items"][0]["jobs"] = json!(["old"]);
    assert!(
        plan(&mut d, &[job("cancelled")], &Processes::new(), 10000., "")
            .unwrap()
            .is_empty()
    );
}
#[test]
fn twenty_minute_boundary_and_one_job_per_item_including_session_links() {
    let mut d = db("doing");
    d["stalls"] = json!({});
    assert!(
        plan(&mut d, &[], &Processes::new(), 10000., "")
            .unwrap()
            .is_empty()
    );
    assert!(
        plan(&mut d, &[], &Processes::new(), 11199., "")
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        plan(&mut d, &[], &Processes::new(), 11200., "").unwrap(),
        vec![1]
    );
    let mut d = db("doing");
    let mut j = job("running");
    j["cwd"] = json!("D:/other");
    j["last_event_at"] = json!(9900);
    d["items"][0]["jobs"] = json!([j["thread_id"]]);
    assert!(
        plan(&mut d, &[j], &processes(), 10000., "")
            .unwrap()
            .is_empty()
    );
    assert!(!under("C:/repo-wt-other/task", "C:/repo"));
}
#[test]
fn owner_client_names_paths_and_linked_worktrees_are_excluded() {
    for (name, path) in [
        ("owner", "C:/repo"),
        ("OWNER", "C:/repo"),
        ("ClientCase", "C:/repo"),
        ("demo", "C:/ClientCase-wt/task"),
    ] {
        let mut d = db("todo");
        d["projects"] = json!({name:path});
        d["items"][0]["project"] = json!(name);
        d["stalls"] = json!({name:{"since":1}});
        let before = d["items"].clone();
        assert!(
            plan(
                &mut d,
                &[],
                &Processes::new(),
                10000.,
                "# comment\n\u{feff}\nClientCase\n"
            )
            .unwrap()
            .is_empty()
        );
        assert_eq!(d["items"], before);
    }
    let mut d = db("doing");
    d["items"][0]["jobs"] = json!(["old"]);
    let mut j = job("failed");
    j["cwd"] = json!("C:/ClientCase/task");
    assert!(
        plan(&mut d, &[j], &Processes::new(), 10000., "ClientCase")
            .unwrap()
            .is_empty()
    );
}
#[test]
fn resume_arguments_preserve_model_effort_cwd_and_fresh_fallback() {
    let j = job("running");
    let args = restart_args(
        &j,
        &db("doing")["items"][0],
        "C:/repo",
        "0123456789abcdef0123456789abcdef",
        std::path::Path::new("prompt.md"),
    );
    for pair in [
        ["--model", "gpt-6-astra"],
        ["--effort", "max"],
        ["--cwd", "C:/repo-wt/task"],
        ["--resume-session", "01a0fa4b-2898-7553-889f-0e934f0d32f8"],
        ["--task-title", "実装を続ける"],
        ["--project", "demo"],
        ["--todo-id", "1"],
    ] {
        assert!(args.windows(2).any(|a| a == pair));
    }
    let mut j = j;
    j["thread_id"] = Value::Null;
    assert!(
        !restart_args(
            &j,
            &db("doing")["items"][0],
            "C:/repo",
            "key",
            std::path::Path::new("p.md")
        )
        .contains(&"--resume-session".into())
    );
}

struct Fake {
    intake: Value,
    error: Option<String>,
    remedies: Vec<cc_scheduler::recovery_failure::Failure>,
    remedy_error: bool,
    paths: Paths,
    jobs: Vec<Value>,
    t: f64,
    calls: Vec<String>,
    allow: bool,
    queued: bool,
    fail_after_launch: bool,
    deferred_ids: Vec<i64>,
    assign_owner: bool,
    supersede: bool,
}
impl RecoveryBackend for Fake {
    fn reconcile_intake(&mut self, paths: &Paths) -> Result<IntakeOwners> {
        let mut db = load(&paths.store)?;
        let result = reconcile_pending_intake(&mut db, &self.intake, &self.jobs, self.t, "")?;
        if !result.migrated.is_empty() {
            save(&paths.store, &db)?;
        }
        Ok(result)
    }
    fn remedy(&mut self, class: cc_scheduler::recovery_failure::Failure) -> Result<String> {
        self.remedies.push(class);
        if self.remedy_error {
            Err("schtasks: Access is denied".into())
        } else {
            Ok(class.remedy().into())
        }
    }
    fn clock(&self) -> f64 {
        self.t
    }
    fn clients(&mut self) -> Result<String> {
        Ok(String::new())
    }
    fn snapshot(&mut self) -> Result<(Vec<Value>, Processes)> {
        let mut ps = Processes::new();
        for job in &self.jobs {
            if job["id"].as_str().is_some_and(|id| id.starts_with("new-")) {
                let pid = job["pid"].as_u64().unwrap() as u32;
                ps.insert(
                    pid,
                    json!({"pid":pid,"created":job["pid_created"],"ppid":0}),
                );
            }
        }
        Ok((self.jobs.clone(), ps))
    }
    fn perform(&mut self, item: &Value, p: &Value, _: &Value) -> Result<Outcome> {
        // Tools can edit ToDo or re-enter the watcher; no state lock may be held.
        drop(Lock::acquire(&self.paths.lock, std::time::Duration::ZERO).unwrap());
        drop(
            Lock::acquire(
                &self.paths.store.with_file_name("watch.lock"),
                std::time::Duration::ZERO,
            )
            .unwrap(),
        );
        let durable = load(&self.paths.store)?;
        let row = durable["items"]
            .as_array()
            .unwrap()
            .iter()
            .find(|row| row["id"] == item["id"])
            .unwrap();
        assert_eq!(row["stall_recovery"]["pending"]["request"], p["request"]);
        assert!(row["stall_recovery"]["pending"]["claim"].is_string());
        assert!(
            row["stall_recovery"]["pending"]["claim_until"]
                .as_f64()
                .unwrap()
                > self.t
        );
        if self.supersede {
            self.supersede = false;
            let mut next = p.clone();
            next["request"] = json!("replacement-request");
            next["key"] = json!(hash("manual:replacement-request"));
            let mut durable = durable;
            find(&mut durable, item["id"].as_i64().unwrap())?["stall_recovery"]["pending"] = next;
            save(&self.paths.store, &durable)?;
            return Ok(Outcome::Obsolete);
        }
        if let Some(j) = self.jobs.iter().find(|j| j["launch_key"] == p["key"]) {
            return Ok(Outcome::Started(j["id"].as_str().unwrap().into()));
        }
        if !self.allow || self.deferred_ids.contains(&item["id"].as_i64().unwrap()) {
            return Ok(Outcome::Deferred);
        }
        if let Some(error) = self.error.take() {
            return Err(error);
        }
        self.calls.push(p["request"].as_str().unwrap().into());
        if self.queued {
            return Ok(Outcome::Queued);
        }
        let j = json!({"id":format!("new-{}",self.calls.len()),"launch_key":p["key"],"todo_id":item["id"],"project":item["project"],"cwd":"C:/repo","status":"running","created_at":self.t,"last_event_at":self.t,"pid":1000+item["id"].as_u64().unwrap(),"pid_created":5000+item["id"].as_u64().unwrap()});
        self.jobs.push(j.clone());
        // External writers may finish another item; watchdog commits must preserve that write.
        let mut d = load(&self.paths.store)?;
        d["external_update"] = json!(true);
        if self.assign_owner {
            let row = find(&mut d, item["id"].as_i64().unwrap())?;
            cc_scheduler::common::assign_worker(row, j["id"].as_str().unwrap());
        }
        save(&self.paths.store, &d)?;
        if self.fail_after_launch {
            self.fail_after_launch = false;
            return Err("lost receipt".into());
        }
        Ok(Outcome::Started(j["id"].as_str().unwrap().into()))
    }
}
#[derive(Default)]
struct Notices {
    lines: Vec<String>,
    fail: bool,
}
impl Notifier for Notices {
    fn notify(&mut self, _: &Paths, text: &str, mention: bool) -> Result<()> {
        assert!(!mention);
        if self.fail {
            return Err("offline".into());
        }
        self.lines.push(text.into());
        Ok(())
    }
}
fn fixture() -> (PathBuf, Paths, Fake, Notices) {
    let root = std::env::temp_dir().join(format!("cc-stall-{}", uuid::Uuid::new_v4()));
    fs::create_dir(&root).unwrap();
    let p = Paths::at(&root, &root.join("jobs"));
    save(&p.store, &db("doing")).unwrap();
    let f = Fake {
        intake: json!({"queue":[],"launches":{}}),
        error: None,
        remedies: vec![],
        remedy_error: false,
        paths: p.clone(),
        jobs: vec![],
        t: 10000.,
        calls: vec![],
        allow: true,
        queued: false,
        fail_after_launch: false,
        deferred_ids: vec![],
        assign_owner: false,
        supersede: false,
    };
    (root, p, f, Notices::default())
}
#[test]
fn accepted_manual_intake_replaces_stale_pending_and_watch_never_recreates_it() {
    for spec in [true, false] {
        let (root, p, mut f, mut notices) = fixture();
        let mut d = db("todo");
        if !spec {
            d["items"][0]["spec"] = Value::Null;
        }
        d["items"][0]["stall_recovery"] = json!({"pending":{"request":"stall-1-1","key":"old-key","reserved_at":1.,"prompt":"保存済みの指示"}});
        save(&p.store, &d).unwrap();
        f.intake = json!({"queue":[{"todo_id":1,"key":"manual:req-todo-1-triage-20261007"}],"launches":{}});
        for _ in 0..2 {
            run_watch(&p, &mut notices, &mut f).unwrap();
            assert!(f.calls.is_empty());
            assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_null());
        }
        assert_eq!(f.intake["queue"].as_array().unwrap().len(), 1);
        fs::remove_dir_all(root).unwrap();
    }
}

#[test]
fn dependency_release_is_consumed_after_started_or_accepted_and_retries_obey_history() {
    for queued in [false, true] {
        let (root, p, mut f, mut notices) = fixture();
        let mut d = load(&p.store).unwrap();
        d["items"][0]["dependency_ready"] = json!(true);
        d["items"][0]["stall_recovery"] = json!({"history":[9000,9900],"next_at":20000});
        save(&p.store, &d).unwrap();
        f.queued = queued;
        run_watch(&p, &mut notices, &mut f).unwrap();
        let mut d = load(&p.store).unwrap();
        assert!(d["items"][0]["dependency_ready"].is_null());
        assert_eq!(f.calls.len(), 1);
        if !queued {
            f.jobs.last_mut().unwrap()["status"] = json!("failed");
            f.jobs.last_mut().unwrap()["last_error"] = json!("task failed");
            f.jobs.last_mut().unwrap()["finished_at"] = json!(10000);
            d["items"][0]["stall_recovery"]["next_at"] = json!(0);
            save(&p.store, &d).unwrap();
            f.t = 10001.;
            run_watch(&p, &mut notices, &mut f).unwrap();
            assert_eq!(
                f.calls.len(),
                1,
                "failed worker must respect retry history after release"
            );
            assert!(
                load(&p.store).unwrap()["items"][0]["stall_recovery"]["next_at"]
                    .as_f64()
                    .unwrap()
                    > f.t
            );
        }
        fs::remove_dir_all(root).unwrap();
    }
}
#[test]
fn fresh_pending_and_unreadable_intake_never_start_against_a_manual_receipt() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = db("todo");
    d["items"][0]["stall_recovery"] =
        json!({"pending":{"request":"stall-1-1","key":"old-key","reserved_at":f.t}});
    save(&p.store, &d).unwrap();
    f.intake =
        json!({"queue":[{"todo_id":1,"key":"manual:req-todo-1-triage-20261007"}],"launches":{}});
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert!(f.calls.is_empty());
    assert_eq!(
        load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"],
        d["items"][0]["stall_recovery"]["pending"]
    );
    f.t += 300.;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert!(f.calls.is_empty());
    assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_null());
    save(&p.store, &d).unwrap();
    f.intake["launches"] = Value::Null;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert!(f.calls.is_empty());
    assert_eq!(load(&p.store).unwrap(), d);
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn capacity_deferral_queue_and_lost_receipt_never_double_start() {
    let (root, p, mut f, mut notices) = fixture();
    let mut initial = load(&p.store).unwrap();
    initial["items"][0]["scheduler"] =
        json!({"job":"old","key":"old-key","phase":"running","pending_cancel":"hung"});
    save(&p.store, &initial).unwrap();
    f.allow = false;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert!(f.calls.is_empty());
    assert!(notices.lines.is_empty());
    f.allow = true;
    f.queued = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls, vec!["stall-1-1"]);
    assert!(notices.lines.is_empty());
    f.queued = false;
    f.fail_after_launch = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.jobs.len(), 1);
    assert_eq!(notices.lines.len(), 1);
    assert!(notices.lines[0].contains("lost receipt"));
    let d = load(&p.store).unwrap();
    assert_eq!(d["external_update"], true);
    assert_eq!(d["items"][0]["scheduler"]["job"], Value::Null);
    assert_eq!(d["items"][0]["scheduler"]["phase"], "watchdog");
    assert_eq!(d["items"][0]["scheduler"]["last_job"], "old");
    assert_eq!(d["items"][0]["jobs"], json!(["new-2"]));
    assert_eq!(d["items"][0]["stall_recovery"]["history"], json!([10000.]));
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn startup_recovery_uses_scheduled_remedy_once_then_retries_the_reserved_job() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    d["items"][0]["jobs"] = json!(["old"]);
    save(&p.store, &d).unwrap();
    let mut j = job("failed");
    j["recovery_evidence"] = json!("app-server client: Access is denied");
    f.jobs.push(j);
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(
        f.remedies,
        vec![cc_scheduler::recovery_failure::Failure::Console]
    );
    assert!(f.calls.is_empty());
    f.t += 59.;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert!(f.calls.is_empty());
    f.t += 1.;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls, vec!["stall-1-1"]);
    assert_eq!(f.remedies.len(), 1);
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn remedy_failure_reports_concrete_cause_once_and_retries_hourly_without_blocking() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    d["items"][0]["jobs"] = json!(["old"]);
    save(&p.store, &d).unwrap();
    let mut j = job("failed");
    j["last_error"] = json!("Access is denied");
    f.jobs.push(j);
    f.remedy_error = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(notices.lines.len(), 1);
    assert!(notices.lines[0].contains("schtasks: Access is denied"));
    assert!(notices.lines[0].contains("定期タスク"));
    f.t += 3599.;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.remedies.len(), 1);
    assert_eq!(load(&p.store).unwrap()["items"][0]["status"], "doing");
    f.t += 1.;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.remedies.len(), 2);
    assert_eq!(notices.lines.len(), 1);
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn old_watchdog_hold_is_released_but_owner_decision_is_preserved() {
    let mut d = db("block");
    d["items"][0]["note"] = json!(
        "6時間以内に自動再開が2回止まったため、3回目の起動を止めました。原因を調べてから再開してください。"
    );
    d["items"][0]["stall_recovery"] = json!({"history":[9000,9500]});
    d["items"].as_array_mut().unwrap().push(
        json!({"id":2,"project":"demo","status":"block","note":"公開先の判断を待っています"}),
    );
    plan(&mut d, &[], &Processes::new(), 10000., "").unwrap();
    assert_eq!(d["items"][0]["status"], "doing");
    assert_eq!(d["items"][1]["status"], "block");
}
#[test]
fn notification_failure_for_one_item_does_not_stop_another_recovery() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    d["items"].as_array_mut().unwrap().insert(0, json!({"id":2,"project":"demo","status":"done","stall_recovery":{"report":"原因の通知を再試行"}}));
    save(&p.store, &d).unwrap();
    notices.fail = true;
    assert!(run_watch(&p, &mut notices, &mut f).is_err());
    assert_eq!(f.calls, vec!["stall-1-1"]);
    assert_eq!(f.jobs.len(), 1);
    assert_eq!(
        load(&p.store).unwrap()["items"][0]["stall_recovery"]["report"],
        "原因の通知を再試行"
    );
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn model_and_context_errors_reserve_fresh_continuation_with_handoff() {
    for error in ["context window exceeded", "model unavailable"] {
        let mut d = db("doing");
        d["items"][0]["jobs"] = json!(["old"]);
        let mut j = job("failed");
        j["last_error"] = json!(error);
        j["recovery_prompt"] = json!("元の受入条件を保持してください");
        plan(&mut d, &[j], &Processes::new(), 10000., "").unwrap();
        let p = &d["items"][0]["stall_recovery"]["pending"];
        assert_eq!(p["kind"], "restart");
        assert!(p["prompt"].as_str().unwrap().contains("元の受入条件"));
        let request = delegation_request(&d["items"][0], p);
        assert!(request["resume_session"].is_null());
        let args = restart_args(
            &p["job"],
            &d["items"][0],
            "C:/repo",
            "key",
            std::path::Path::new("prompt.md"),
        );
        assert!(!args.iter().any(|arg| arg == "--resume-session"));
        let expected_model = if error.contains("model") {
            "gpt-6.1-sol"
        } else {
            "gpt-6-astra"
        };
        assert!(
            args.windows(2)
                .any(|pair| pair == ["--model", expected_model])
        );
        assert_eq!(request["model"], expected_model);
    }
}
#[test]
fn full_context_reported_by_launcher_rebuilds_a_short_resume_prompt_before_fresh_retry() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    d["items"][0]["jobs"] = json!(["old"]);
    save(&p.store, &d).unwrap();
    let mut j = job("running");
    j["recovery_prompt"] = json!("元の作業指示：検証して配置する");
    f.jobs.push(j);
    f.error = Some("context window exceeded".into());
    run_watch(&p, &mut notices, &mut f).unwrap();
    let d = load(&p.store).unwrap();
    let pending = &d["items"][0]["stall_recovery"]["pending"];
    assert_eq!(pending["failure"], "context");
    assert!(pending["job"]["thread_id"].is_null());
    assert!(
        pending["prompt"]
            .as_str()
            .unwrap()
            .contains("検証して配置する")
    );
    assert!(
        pending["prompt"]
            .as_str()
            .unwrap()
            .contains("検証が残っています")
    );
    f.t += 3600.;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls.len(), 1);
    assert_eq!(notices.lines.len(), 1);
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn cancellation_after_reservation_is_observed_before_perform() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    d["items"][0]["jobs"] = json!(["old"]);
    save(&p.store, &d).unwrap();
    f.jobs.push(job("failed"));
    f.allow = false;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_object());
    f.jobs[0]["cancel_requested"] = json!(true);
    f.allow = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert!(f.calls.is_empty());
    assert!(notices.lines.is_empty());
    assert!(!load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_object());
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn shared_restart_attempt_is_reserved_before_launch_and_survives_lost_commit() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    d["items"][0]["jobs"] = json!(["old"]);
    d["items"][0]["discord_restart_thread"] = json!("1234567890123456789");
    d["items"][0]["discord_restart_history"] = json!([9000.]);
    save(&p.store, &d).unwrap();
    f.jobs.push(job("failed"));
    f.fail_after_launch = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    let d = load(&p.store).unwrap();
    assert_eq!(
        d["items"][0]["discord_restart_history"],
        json!([9000., 10000.])
    );
    assert_eq!(
        d["items"][0]["stall_recovery"]["pending"]["discord_reserved_at"],
        10000.
    );
    f.jobs.last_mut().unwrap()["status"] = json!("failed");
    run_watch(&p, &mut notices, &mut f).unwrap();
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls.len(), 1);
    assert_eq!(
        load(&p.store).unwrap()["items"][0]["discord_restart_history"],
        json!([9000., 10000.])
    );
    assert_eq!(notices.lines.len(), 1);
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn orphan_helpers_survive_dead_parent_in_ledger_but_reused_pids_and_other_owners_are_preserved() {
    let old = job("running");
    let mut ps = processes();
    ps.insert(
        102,
        json!({"pid":102,"created":12,"ppid":101,"name":"node.exe"}),
    );
    let ledger = cc_scheduler::policy::ownership(std::slice::from_ref(&old), &ps, &json!({}));
    ps.remove(&100);
    ps.remove(&101);
    ps.insert(
        200,
        json!({"pid":200,"created":20,"ppid":0,"name":"node.exe"}),
    );
    let got = owned_helpers(&old, std::slice::from_ref(&old), &ps, &ledger);
    assert_eq!(
        got.iter()
            .map(|p| p["pid"].as_u64().unwrap())
            .collect::<Vec<_>>(),
        vec![102]
    );
    ps.get_mut(&102).unwrap()["created"] = json!(99);
    assert!(owned_helpers(&old, std::slice::from_ref(&old), &ps, &ledger).is_empty());
    ps.get_mut(&102).unwrap()["created"] = json!(12);
    let newer = json!({"id":"new-owner","status":"running","pid":102,"pid_created":12});
    assert!(owned_helpers(&old, &[old.clone(), newer], &ps, &ledger).is_empty());
}
#[test]
fn notification_failure_retries_only_report_and_backoff_logs_once() {
    let (root, p, mut f, mut notices) = fixture();
    notices.fail = true;
    assert!(run_watch(&p, &mut notices, &mut f).is_err());
    notices.fail = false;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls.len(), 1);
    assert_eq!(notices.lines.len(), 1);
    let mut d = load(&p.store).unwrap();
    d["items"][0]["stall_recovery"]["history"] = json!([9000, 9500]);
    d["stalls"]["demo"] = json!({"since":1});
    d["items"][0]["stall_recovery"]["next_at"] = json!(0);
    save(&p.store, &d).unwrap();
    f.jobs.clear();
    run_watch(&p, &mut notices, &mut f).unwrap();
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls.len(), 1);
    assert_eq!(notices.lines.len(), 2);
    assert!(notices.lines[1].contains("1時間ごとに再試行"));
    assert_eq!(load(&p.store).unwrap()["items"][0]["status"], "doing");
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn a_deferred_item_and_two_old_pending_requests_do_not_limit_other_launches() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    for id in 2..=5 {
        d["items"].as_array_mut().unwrap().push(
            json!({"id":id,"project":"demo","status":"todo","spec":"spec.md","dispatchable":true,"title":"並行して検証する","jobs":[]}),
        );
    }
    assert_eq!(
        plan(&mut d, &[], &Processes::new(), f.t, "").unwrap(),
        vec![1, 2, 3, 4, 5]
    );
    let old_one = d["items"][0]["stall_recovery"]["pending"].clone();
    let old_two = d["items"][1]["stall_recovery"]["pending"].clone();
    d["items"][1]["stall_recovery"]["next_at"] = json!(11000.);
    save(&p.store, &d).unwrap();
    f.deferred_ids.push(1);
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls, vec!["stall-3-1", "stall-4-1", "stall-5-1"]);
    assert_eq!(f.jobs.len(), 3);
    let current = load(&p.store).unwrap();
    assert_eq!(current["items"][0]["stall_recovery"]["pending"], old_one);
    assert_eq!(current["items"][1]["stall_recovery"]["pending"], old_two);
    for index in 2..5 {
        assert_eq!(current["items"][index]["status"], "doing");
        assert!(current["items"][index]["stall_recovery"]["pending"].is_null());
        assert_eq!(current["items"][index]["jobs"].as_array().unwrap().len(), 1);
    }
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls.len(), 3);
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn every_unassigned_item_can_enter_the_durable_queue_in_one_pass() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    for id in 2..=5 {
        let project = format!("project-{id}");
        d["projects"][&project] = json!(format!("C:/project-{id}"));
        d["items"].as_array_mut().unwrap().push(json!({"id":id,"project":project,"status":"todo","spec":"spec.md","dispatchable":true,"created_s":1,"title":"未担当の作業を再開する","jobs":[]}));
    }
    save(&p.store, &d).unwrap();
    f.queued = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(
        f.calls,
        vec![
            "stall-1-1",
            "stall-2-1",
            "stall-3-1",
            "stall-4-1",
            "stall-5-1"
        ]
    );
    assert!(f.jobs.is_empty());
    assert!(notices.lines.is_empty());
    let first = load(&p.store).unwrap();
    for item in first["items"].as_array().unwrap() {
        assert!(item["stall_recovery"]["pending"].is_object());
        assert!(item["stall_recovery"]["pending"]["claim"].is_null());
    }
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(load(&p.store).unwrap()["items"], first["items"]);
    assert_eq!(&f.calls[..5], &f.calls[5..]);
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn a_crashed_watch_claim_expires_and_does_not_block_another_item() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    plan(&mut d, &[], &Processes::new(), f.t, "").unwrap();
    d["items"][0]["stall_recovery"]["pending"]["claim"] = json!("crashed-watcher");
    d["items"][0]["stall_recovery"]["pending"]["claim_until"] = json!(10300.);
    d["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":2,"project":"demo","status":"todo","spec":"spec.md","dispatchable":true,"title":"別の作業","jobs":[]}));
    save(&p.store, &d).unwrap();
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls, vec!["stall-2-1"]);
    f.t = 10299.;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls.len(), 1);
    f.t = 10300.;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls, vec!["stall-2-1", "stall-1-1"]);
    assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_null());
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn a_new_concrete_decision_or_owner_block_invalidates_an_existing_reservation() {
    for block in [false, true] {
        let (root, p, mut f, mut notices) = fixture();
        let mut d = load(&p.store).unwrap();
        d["items"][0]["jobs"] = json!(["old"]);
        save(&p.store, &d).unwrap();
        f.jobs.push(job("succeeded"));
        f.allow = false;
        run_watch(&p, &mut notices, &mut f).unwrap();
        assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_object());
        if block {
            let mut d = load(&p.store).unwrap();
            d["items"][0]["status"] = json!("block");
            d["items"][0]["note"] = json!("公開先の回答を待っています");
            save(&p.store, &d).unwrap();
        } else {
            f.jobs[0]["last_message"] =
                json!("状態: 要判断\n公開してよいですか。\na. 公開\nb. 停止");
        }
        f.allow = true;
        run_watch(&p, &mut notices, &mut f).unwrap();
        assert!(f.calls.is_empty());
        assert!(notices.lines.is_empty());
        assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_null());
        if block {
            assert_eq!(load(&p.store).unwrap()["items"][0]["status"], "block");
        }
        fs::remove_dir_all(root).unwrap();
    }
}

#[test]
fn recovery_stop_is_distinct_from_a_later_explicit_cancel() {
    for explicit in [false, true] {
        let (root, p, mut f, mut notices) = fixture();
        let mut d = load(&p.store).unwrap();
        d["items"][0]["jobs"] = json!(["old"]);
        plan(&mut d, &[job("running")], &Processes::new(), f.t, "").unwrap();
        d["items"][0]["stall_recovery"]["pending"]["recovery_cancel"] = json!("old");
        save(&p.store, &d).unwrap();
        let mut source = job("cancelled");
        source["cancel_requested"] = json!(explicit);
        f.jobs.push(source);
        run_watch(&p, &mut notices, &mut f).unwrap();
        assert_eq!(f.calls.len(), if explicit { 0 } else { 1 });
        assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_null());
        fs::remove_dir_all(root).unwrap();
    }
}

#[test]
fn receipt_reconciliation_keeps_the_new_scheduler_owner_assigned_by_console() {
    let (root, p, mut f, mut notices) = fixture();
    let mut d = load(&p.store).unwrap();
    d["items"][0]["scheduler"] =
        json!({"job":"old","phase":"running","worktree":"C:/repo-wt/task","key":"old-key"});
    save(&p.store, &d).unwrap();
    f.assign_owner = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    let current = load(&p.store).unwrap();
    assert_eq!(current["items"][0]["scheduler"]["job"], "new-1");
    assert_eq!(current["items"][0]["scheduler"]["last_job"], "old");
    assert_eq!(current["items"][0]["scheduler"]["phase"], "running");
    assert_eq!(
        current["items"][0]["scheduler"]["worktree"],
        "C:/repo-wt/task"
    );
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn conflicting_workspace_starts_fresh_without_resuming_into_another_writers_checkout() {
    let mut d = db("doing");
    d["items"][0]["jobs"] = json!(["old"]);
    let mut source = job("succeeded");
    source["last_message"] = json!("状態: 継続中\n検証を続けます。");
    let unrelated = json!({"id":"other-owner","status":"running","cwd":"C:/repo-wt/task","created_at":9900,"last_event_at":9900});
    assert_eq!(
        plan(&mut d, &[source, unrelated], &Processes::new(), 10000., "").unwrap(),
        vec![1]
    );
    let pending = &d["items"][0]["stall_recovery"]["pending"];
    assert_eq!(pending["workspace"]["isolate_worktree"], true);
    assert!(pending["resume_session"].is_null());
    let request = delegation_request(&d["items"][0], pending);
    assert_eq!(request["isolate_worktree"], true);
    assert!(request["resume_session"].is_null());
    assert_eq!(request["source_job_id"], "old");
}

#[test]
fn client_owned_checkouts_and_old_client_holds_are_not_modified() {
    let mut d = db("todo");
    d["items"][0]["scheduler"] = json!({"worktree":"C:/ClientCase-wt/assigned"});
    let before = d["items"].clone();
    assert!(
        plan(&mut d, &[], &Processes::new(), 10000., "ClientCase")
            .unwrap()
            .is_empty()
    );
    assert_eq!(d["items"], before);
    let mut d = db("block");
    d["projects"]["demo"] = json!("C:/ClientCase");
    d["items"][0]["note"] = json!(
        "6時間以内に自動再開が2回止まったため、3回目の起動を止めました。原因を調べてから再開してください。"
    );
    let before = d["items"].clone();
    assert!(
        plan(&mut d, &[], &Processes::new(), 10000., "ClientCase")
            .unwrap()
            .is_empty()
    );
    assert_eq!(d["items"], before);
}

#[test]
fn an_obsolete_result_does_not_erase_a_newer_durable_reservation() {
    let (root, p, mut f, mut notices) = fixture();
    f.supersede = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    let current = load(&p.store).unwrap();
    assert_eq!(
        current["items"][0]["stall_recovery"]["pending"]["request"],
        "replacement-request"
    );
    assert!(f.calls.is_empty());
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls, vec!["replacement-request"]);
    assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_null());
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn a_long_unicode_handoff_fits_the_console_prompt_limit_and_keeps_the_task_header() {
    let mut source = job("failed");
    source["recovery_prompt"] = json!("受入条件を維持してください。".repeat(10000));
    let text = continuation(&db("doing")["items"][0], &source);
    assert!(text.len() < 64 * 1024);
    assert!(text.starts_with("前回のジョブが止まったので再開してください。共有ToDo #1"));
    assert!(text.contains("検証が残っています"));
    assert!(text.contains("受入条件を維持してください。"));
}

#[test]
fn latest_worker_keeps_its_checkout_after_a_concurrent_task_required_isolation() {
    let item = json!({"id":1,"scheduler":{"worktree":"C:/repo-wt/old"}});
    let job = json!({"id":"new-worker","todo_id":1,"cwd":"C:/repo-wt/agent-todo-1"});
    let workspace = cc_todo::recovery::workspace(&item, &job, &json!({"items":[]}), &[], "C:/repo");
    assert_eq!(workspace["cwd"], "C:/repo-wt/agent-todo-1");
    assert_eq!(workspace["isolate_worktree"], false);
}
#[test]
fn a_new_healthy_owner_invalidates_only_its_own_pending_request() {
    let (root, p, mut f, mut notices) = fixture();
    f.allow = false;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_object());
    let mut d = load(&p.store).unwrap();
    d["items"]
        .as_array_mut()
        .unwrap()
        .push(json!({"id":2,"project":"demo","title":"別の未担当作業","status":"todo","spec":"spec.md","dispatchable":true,"jobs":[]}));
    save(&p.store, &d).unwrap();
    f.jobs.push(json!({"id":"another-worker","todo_id":1,"project":"demo","status":"running","cwd":"C:/repo-wt/owned","created_at":10000,"last_event_at":10000}));
    f.allow = true;
    run_watch(&p, &mut notices, &mut f).unwrap();
    assert_eq!(f.calls, vec!["stall-2-1"]);
    let current = load(&p.store).unwrap();
    assert!(current["items"][0]["stall_recovery"]["pending"].is_null());
    assert_eq!(current["items"][1]["status"], "doing");
    assert_eq!(current["items"][1]["jobs"], json!(["new-1"]));
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn old_queued_owner_invalidates_a_pending_retry_even_after_claim_expires() {
    for status in ["queued", "starting"] {
        let (root, p, mut f, mut notices) = fixture();
        f.allow = false;
        run_watch(&p, &mut notices, &mut f).unwrap();
        assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_object());
        f.jobs.push(json!({"id":"accepted-owner","todo_id":1,"project":"demo","status":status,"created_at":1,"last_event_at":1}));
        f.allow = true;
        f.t += 3600.;
        run_watch(&p, &mut notices, &mut f).unwrap();
        assert!(
            f.calls.is_empty(),
            "accepted {status} must not launch twice"
        );
        assert!(load(&p.store).unwrap()["items"][0]["stall_recovery"]["pending"].is_null());
        fs::remove_dir_all(root).unwrap();
    }
}
