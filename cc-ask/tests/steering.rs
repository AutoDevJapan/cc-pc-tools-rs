mod common;
use cc_ask::core::*;
use common::*;
use serde_json::{json, Value};
use std::panic::{catch_unwind, AssertUnwindSafe};

struct ReplyAtArchive {
    shared: SharedFake,
    injected: bool,
}
impl Effects for ReplyAtArchive {
    fn now(&self) -> f64 {
        self.shared.now()
    }
    fn run(&mut self, argv: &[String], timeout: u64) -> Result<Output> {
        self.shared.run(argv, timeout)
    }
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        if !self.injected && method == "PATCH" && body.is_some_and(|b| b["archived"] == true) {
            self.injected = true;
            let mut fake = self.shared.0.borrow_mut();
            fake.sequence += 1;
            let id = fake.sequence.to_string();
            let tid = url.split('/').next_back().unwrap();
            fake.messages.entry(tid.into()).or_default().push(message(
                &id,
                "終了と同時に届いた追加指示を必ず反映して",
                "OWN",
            ));
        }
        self.shared.http(method, url, body, discord)
    }
}

fn source(h: &Harness, context: u64) {
    let file = h.ctx.jobs.join("jobs/job1/job.json");
    let mut job = read_json(&file).unwrap();
    job["cwd"] = json!(h.root.path().to_string_lossy());
    job["sandbox"] = json!("danger-full-access");
    job["network"] = json!(false);
    job["context_pct"] = json!(context);
    atomic_json(&file, &job, 2).unwrap();
}
fn question(h: &Harness) {
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"100","after":"100","title":"依頼A","project":"テスト","open":true,"continuation":{"job":"job1","after":"100","selection":{"model":"gpt-6.1-sol","effort":"max"}}}]);
    h.save(&state);
}
#[test]
fn unavailable_todo_store_keeps_each_source_and_reply_unprocessed() {
    for status in ["queued", "running", "succeeded"] {
        for missing in [true, false] {
            let mut h = Harness::new();
            h.tick();
            source(&h, 10);
            h.finish("job1", status, None, None);
            let mid = h.reply("保存された待ち先を確認してから反映して", "100");
            if missing {
                std::fs::remove_file(&h.ctx.todo).unwrap();
            } else {
                std::fs::write(&h.ctx.todo, "{}").unwrap();
            }
            let mut state = h.state();
            let mut record = state["requests"]["100"].clone();
            let thread = h.fake.borrow().threads["100"].clone();
            assert!(cc_ask::steering::deliver(
                &mut h.ctx,
                &mut state,
                &thread,
                &mut record,
                cc_ask::restart::Slot::Request,
                "OWN",
                &mut false
            )
            .is_err());
            assert_eq!(h.fake.borrow().launches.len(), 1);
            assert_eq!(job_info(&h.ctx, "job1")["status"], status);
            assert_eq!(record["after"], "100");
            assert!(record["owner_update"].is_null());
            assert!(state["answer_receipts"][&mid]["handled_by"].is_null());
            assert!(state["answer_receipts"][&mid]["launch_job"].is_null());
        }
    }
}
fn run(h: &mut Harness, question: bool) {
    if question {
        cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    } else {
        h.tick();
    }
}

fn dependency_db(h: &Harness, association: &str, status: &str) {
    let mut item = json!({"id":2073,"status":"wait","wait_on":[32,33],"jobs":[]});
    match association {
        "jobs" => item["jobs"] = json!(["job1"]),
        "scheduler" => item["scheduler"] = json!({"job":"job1"}),
        _ => {}
    }
    atomic_json(
        &h.ctx.todo,
        &json!({"items":[item,
        {"id":32,"status":status},{"id":33,"status":"todo"}]}),
        2,
    )
    .unwrap();
}

#[test]
fn dependency_wait_precedes_owner_update_reservation_and_cancellation() {
    for q in [false, true] {
        for status in ["queued", "succeeded"] {
            for association in [
                "todo_id",
                "related_todo_ids",
                "jobs",
                "scheduler",
                "selection",
            ] {
                let mut h = Harness::new();
                h.tick();
                source(&h, 10);
                if q {
                    question(&h);
                }
                h.finish("job1", status, Some("状態: 完了\n以前の結果"), None);
                let path = h.ctx.jobs.join("jobs/job1/job.json");
                let mut job = read_json(&path).unwrap();
                if association == "todo_id" {
                    job["todo_id"] = json!(2073);
                }
                if association == "related_todo_ids" {
                    job["related_todo_ids"] = json!([2073]);
                }
                atomic_json(&path, &job, 2).unwrap();
                if association == "selection" {
                    let mut st = h.state();
                    let e = if q {
                        &mut st["questions"][0]["continuation"]
                    } else {
                        &mut st["requests"]["100"]
                    };
                    e["selection"]["todo_id"] = json!(2073);
                    h.save(&st);
                }
                dependency_db(&h, association, "todo");
                let mid = h.reply("依存完了後に反映するテスト指示", "100");
                for _ in 0..2 {
                    run(&mut h, q);
                }
                assert_eq!(
                    h.fake.borrow().launches.len(),
                    1,
                    "{q} {status} {association}"
                );
                assert!(!h
                    .fake
                    .borrow()
                    .runs
                    .iter()
                    .any(|(a, _)| a.iter().any(|s| s == "cancel")));
                let st = h.state();
                let e = if q {
                    &st["questions"][0]["continuation"]
                } else {
                    &st["requests"]["100"]
                };
                assert_eq!(e["work_wait_on"], json!([32, 33]));
                assert!(!e["owner_update"].is_object());
                assert_eq!(e["after"], "100");
                assert_eq!(
                    e["dependency_messages"][0]["content"],
                    "依存完了後に反映するテスト指示"
                );
                assert_eq!(st["answer_receipts"][&mid]["posted"], true);
                assert!(!truth(&st["answer_receipts"][&mid]["handled_by"]));
                let mut db = read_json(&h.ctx.todo).unwrap();
                db["items"][1]["status"] = json!("done");
                atomic_json(&h.ctx.todo, &db, 2).unwrap();
                run(&mut h, q);
                assert_eq!(h.fake.borrow().launches.len(), 1);
                db["items"][2]["status"] = json!("done");
                atomic_json(&h.ctx.todo, &db, 2).unwrap();
                run(&mut h, q);
                run(&mut h, q);
                assert_eq!(h.fake.borrow().launches.len(), 2);
                let f = h.fake.borrow();
                let args = &f
                    .runs
                    .iter()
                    .find(|(a, _)| {
                        a.iter().any(|s| s == "--name") && a.iter().any(|s| s == "resume-100")
                    })
                    .unwrap()
                    .0;
                let at = args.iter().position(|s| s == "--related-todo-ids").unwrap();
                assert_eq!(args[at + 1], "2073");
            }
        }
    }
}

fn deliver_direct(h: &mut Harness, q: bool) -> bool {
    deliver_attempt(h, q).unwrap()
}

fn deliver_attempt(h: &mut Harness, q: bool) -> Result<bool> {
    let mut st = h.state();
    let mut record = if q {
        st["questions"][0].clone()
    } else {
        st["requests"]["100"].clone()
    };
    let slot = if q {
        cc_ask::restart::Slot::Question(0)
    } else {
        cc_ask::restart::Slot::Request
    };
    cc_ask::steering::deliver(
        &mut h.ctx,
        &mut st,
        &json!({"id":"100","name":"依頼A"}),
        &mut record,
        slot,
        "OWN",
        &mut false,
    )
}

fn complete_dependencies(h: &Harness) {
    let mut db = read_json(&h.ctx.todo).unwrap();
    for item in db["items"].as_array_mut().unwrap().iter_mut().skip(1) {
        item["status"] = json!("done");
    }
    atomic_json(&h.ctx.todo, &db, 2).unwrap();
}

#[test]
fn dependency_wait_rechecks_pending_before_lost_response_recovery() {
    for q in [false, true] {
        for recovery in ["before", "queued", "cancelled", "dispatched"] {
            let mut h = Harness::new();
            h.tick();
            source(&h, 10);
            if q {
                question(&h);
            }
            dependency_db(&h, "jobs", "done");
            complete_dependencies(&h);
            let mid = h.reply("予約後に依存が追加されるテスト", "100");
            if recovery == "before" {
                h.fake.borrow_mut().crash_before_launch = true;
            } else {
                h.fake.borrow_mut().launch_queued = true;
                h.fake.borrow_mut().crash_after_launch = true;
            }
            assert!(catch_unwind(AssertUnwindSafe(|| deliver_direct(&mut h, q))).is_err());
            let before = h.state();
            let e = if q {
                &before["questions"][0]["continuation"]
            } else {
                &before["requests"]["100"]
            };
            let key = e["owner_update"]["key"].clone();
            if ["cancelled", "dispatched"].contains(&recovery) {
                h.finish("job2", "cancelled", None, None);
            }
            if recovery == "dispatched" {
                std::fs::write(
                    h.ctx.jobs.join("jobs/job2/events.jsonl"),
                    "{\"type\":\"item.started\",\"item\":{\"type\":\"command_execution\"}}\n",
                )
                .unwrap();
            }
            dependency_db(&h, "jobs", "todo");
            deliver_direct(&mut h, q);
            let st = h.state();
            let e = if q {
                &st["questions"][0]["continuation"]
            } else {
                &st["requests"]["100"]
            };
            if recovery == "dispatched" {
                assert_eq!(e["job"], "job2");
                assert_eq!(st["answer_receipts"][&mid]["handled_by"], "job2");
            } else {
                assert_eq!(e["job"], "job1", "{q} {recovery}");
                assert_eq!(e["owner_update"]["key"], key);
                assert_eq!(e["work_wait_on"], json!([32, 33]));
                assert_eq!(e["after"], "100");
                assert_eq!(
                    e["owner_update"]["messages"][0]["content"],
                    "予約後に依存が追加されるテスト"
                );
                assert_eq!(st["answer_receipts"][&mid]["posted"], true);
                assert!(!truth(&st["answer_receipts"][&mid]["handled_by"]));
            }
            let count = h.fake.borrow().launches.len();
            deliver_direct(&mut h, q);
            assert_eq!(h.fake.borrow().launches.len(), count);
            complete_dependencies(&h);
            h.fake.borrow_mut().launch_queued = false;
            deliver_direct(&mut h, q);
            deliver_direct(&mut h, q);
            assert_eq!(
                h.fake.borrow().launches.len(),
                if recovery == "cancelled" { 3 } else { 2 }
            );
            let st = h.state();
            if recovery == "queued" {
                assert!(!truth(&st["answer_receipts"][&mid]["handled_by"]));
                assert_eq!(st["answer_receipts"][&mid]["launch_job"], "job2");
                assert_eq!(job_info(&h.ctx, "job2")["related_todo_ids"], json!([2073]));
            }
        }
    }
}

#[test]
fn dependency_scope_does_not_infer_numbers_or_folders_and_keeps_all_explicit_ids() {
    for associated in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        dependency_db(&h, "none", "todo");
        let mut st = h.state();
        if associated {
            st["requests"]["100"]["selection"]["related_todo_ids"] = json!([2073, 2074]);
        }
        h.save(&st);
        let path = h.ctx.jobs.join("jobs/job1/job.json");
        let mut job = read_json(&path).unwrap();
        if associated {
            job["todo_id"] = json!(2075);
        }
        atomic_json(&path, &job, 2).unwrap();
        h.reply("ToDo2073と同じフォルダーを使うだけのテスト", "100");
        deliver_direct(&mut h, false);
        assert_eq!(
            h.fake.borrow().launches.len(),
            if associated { 1 } else { 2 }
        );
        if associated {
            complete_dependencies(&h);
            deliver_direct(&mut h, false);
            assert_eq!(
                job_info(&h.ctx, "job2")["related_todo_ids"],
                json!([2073, 2074, 2075])
            );
            assert_eq!(job_info(&h.ctx, "job2")["todo_id"], 2075);
        }
    }
}

#[test]
fn dependency_release_preserves_other_writer_and_scheduler_reservation_priority() {
    for reservation in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        dependency_db(&h, "jobs", "todo");
        h.reply("依存解除後も担当を重ねないテスト", "100");
        deliver_direct(&mut h, false);
        complete_dependencies(&h);
        if reservation {
            let mut db = read_json(&h.ctx.todo).unwrap();
            db["items"][0]["stall_recovery"]["pending"] = json!({"key":"scheduler-key"});
            atomic_json(&h.ctx.todo, &db, 2).unwrap();
        } else {
            let dir = h.ctx.jobs.join("jobs/other");
            std::fs::create_dir_all(&dir).unwrap();
            atomic_json(
                &dir.join("job.json"),
                &json!({"id":"other","status":"queued","todo_id":2073}),
                2,
            )
            .unwrap();
        }
        deliver_direct(&mut h, false);
        assert_eq!(h.fake.borrow().launches.len(), 1);
        assert!(!h
            .fake
            .borrow()
            .runs
            .iter()
            .any(|(a, _)| a.iter().any(|s| s == "cancel")));
    }
}

struct WaitDuringCancellation(SharedFake, bool);
impl Effects for WaitDuringCancellation {
    fn now(&self) -> f64 {
        self.0.now()
    }
    fn http(&mut self, m: &str, u: &str, b: Option<&Value>, d: bool) -> Result<Value> {
        self.0.http(m, u, b, d)
    }
    fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
        if !self.1 && args.iter().any(|arg| arg == "cancel") {
            self.1 = true;
            let path = self.0 .0.borrow().root.join("todo.json");
            let _todo =
                ByteLock::acquire(&path.with_file_name("todo.lock"), std::time::Duration::ZERO)?;
            let mut db = read_json(&path)?;
            db["items"][0]["wait_on"] = json!([32, 33]);
            db["items"][1]["status"] = json!("todo");
            db["items"][2]["status"] = json!("todo");
            atomic_json(&path, &db, 2)?;
        }
        self.0.run(args, timeout)
    }
}

#[test]
fn dependency_added_after_reservation_is_checked_again_before_launch() {
    for q in [false, true] {
        for status in ["queued", "running", "succeeded"] {
            let mut h = Harness::new();
            h.tick();
            source(&h, 10);
            if q {
                question(&h);
            }
            h.finish("job1", status, None, None);
            let original = job_info(&h.ctx, "job1");
            dependency_db(&h, "jobs", "done");
            complete_dependencies(&h);
            h.ctx.effects = Box::new(WaitDuringCancellation(SharedFake(h.fake.clone()), false));
            let mid = h.reply("起動直前に依存が追加されるテスト", "100");
            deliver_direct(&mut h, q);
            assert_eq!(h.fake.borrow().launches.len(), 1);
            assert_eq!(
                job_info(&h.ctx, "job1"),
                original,
                "wait registered before cancellation must preserve the source"
            );
            let st = h.state();
            let e = if q {
                &st["questions"][0]["continuation"]
            } else {
                &st["requests"]["100"]
            };
            assert_eq!(e["work_wait_on"], json!([32, 33]));
            assert!(e["owner_update"].is_object());
            assert_eq!(e["after"], "100");
            assert_eq!(
                e["owner_update"]["messages"][0]["content"],
                "起動直前に依存が追加されるテスト"
            );
            assert_eq!(st["answer_receipts"][&mid]["posted"], true);
            assert!(!truth(&st["answer_receipts"][&mid]["launch_job"]));
            assert!(!truth(&st["answer_receipts"][&mid]["handled_by"]));
            complete_dependencies(&h);
            deliver_direct(&mut h, q);
            deliver_direct(&mut h, q);
            assert_eq!(h.fake.borrow().launches.len(), 2);
        }
    }
}

#[test]
fn deliberate_stop_during_pending_update_keeps_reply_and_does_not_launch() {
    for q in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        if q {
            question(&h);
        }
        let mid = h.reply("本人の停止後は起動しないテスト", "100");
        h.fake.borrow_mut().crash_before_launch = true;
        assert!(catch_unwind(AssertUnwindSafe(|| deliver_direct(&mut h, q))).is_err());
        let path = h.ctx.jobs.join("jobs/job1/job.json");
        let mut source = read_json(&path).unwrap();
        source["cancel_requested"] = json!(true);
        atomic_json(&path, &source, 2).unwrap();
        for _ in 0..2 {
            deliver_direct(&mut h, q);
        }
        assert_eq!(read_json(&path).unwrap(), source);
        assert_eq!(h.fake.borrow().launches.len(), 1);
        let st = h.state();
        let e = if q {
            &st["questions"][0]["continuation"]
        } else {
            &st["requests"]["100"]
        };
        assert_eq!(e["after"], "100");
        assert_eq!(
            e["owner_update"]["messages"][0]["content"],
            "本人の停止後は起動しないテスト"
        );
        assert_eq!(st["answer_receipts"][&mid]["posted"], true);
        assert!(!truth(&st["answer_receipts"][&mid]["handled_by"]));
    }
}

#[test]
fn dependency_wait_preserves_verbatim_reply_when_history_no_longer_has_it() {
    for q in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        if q {
            question(&h);
        }
        dependency_db(&h, "jobs", "todo");
        let mid = h.reply("原文保持のテスト\n二行目も保持", "100");
        deliver_direct(&mut h, q);
        h.fake
            .borrow_mut()
            .messages
            .get_mut("100")
            .unwrap()
            .retain(|m| m["id"] != mid);
        let mut st = h.state();
        let r = if q {
            &mut st["questions"][0]
        } else {
            &mut st["requests"]["100"]
        };
        r["reply_resume_retry"] = json!({"deadline":0,"next_at":0,"exhausted":true,"reply":mid});
        r["resume_status_reply"] = json!(mid);
        r["resume_status"] = json!("再開失敗");
        h.save(&st);
        deliver_direct(&mut h, q);
        complete_dependencies(&h);
        deliver_direct(&mut h, q);
        deliver_direct(&mut h, q);
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert!(
            s(&h.fake.borrow().launches[1]["prompt"]).contains("原文保持のテスト\n二行目も保持")
        );
        let st = h.state();
        let e = if q {
            &st["questions"][0]["continuation"]
        } else {
            &st["requests"]["100"]
        };
        assert!(e["dependency_messages"].is_null());
        assert!(e["work_wait_on"].is_null());
        assert_eq!(st["answer_receipts"][&mid]["handled_by"], "job2");
    }
}

#[test]
fn dependency_recovery_keeps_unknown_dispatch_evidence_reserved() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    dependency_db(&h, "jobs", "done");
    complete_dependencies(&h);
    h.reply("不明な実行記録を再配送しないテスト", "100");
    h.fake.borrow_mut().launch_queued = true;
    h.fake.borrow_mut().crash_after_launch = true;
    assert!(catch_unwind(AssertUnwindSafe(|| deliver_direct(&mut h, false))).is_err());
    h.finish("job2", "cancelled", None, None);
    let events = h.ctx.jobs.join("jobs/job2/events.jsonl");
    std::fs::write(&events, "{incomplete\n").unwrap();
    assert!(deliver_attempt(&mut h, false).is_err());
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert!(h.state()["requests"]["100"]["owner_update"].is_object());
    std::fs::write(
        &events,
        "{\"type\":\"item.started\",\"item\":{\"type\":\"mcp_tool_call\"}}\n",
    )
    .unwrap();
    deliver_direct(&mut h, false);
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(h.state()["requests"]["100"]["job"], "job2");
}
#[test]
fn message_during_run_cancels_only_source_and_resumes_with_verbatim_quote_and_same_settings() {
    for q in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 40);
        if q {
            question(&h);
        }
        let text = "a なぜ追加費用がかかる？\n前の指示を優先しないで。";
        let mid = h.reply(text, "100");
        run(&mut h, q);
        let f = h.fake.borrow();
        assert_eq!(f.launches.len(), 2);
        let launch = &f.launches[1];
        assert_eq!(launch["resume_session"], "session-job1");
        assert_eq!(launch["sandbox"], "danger-full-access");
        assert_eq!(launch["cwd"], h.root.path().to_string_lossy().as_ref());
        assert_eq!(launch["model"], f.launches[0]["model"]);
        assert_eq!(launch["effort"], f.launches[0]["effort"]);
        assert!(s(&launch["prompt"]).contains(text));
        assert!(s(&launch["prompt"]).contains("以前の指示より優先"));
        let cancelled: Vec<_> = f
            .runs
            .iter()
            .filter(|(a, _)| a.iter().any(|a| a == "cancel"))
            .collect();
        assert_eq!(cancelled.len(), 1);
        let at = cancelled[0].0.iter().position(|a| a == "cancel").unwrap();
        assert_eq!(cancelled[0].0[at + 1], "job1");
        let receipt = f
            .posts
            .iter()
            .find(|(_, p)| p["message_reference"]["message_id"] == mid)
            .unwrap();
        assert_eq!(receipt.1["flags"], SILENT);
        assert_eq!(receipt.1["allowed_mentions"]["users"], json!([]));
    }
}
#[test]
fn high_context_or_missing_session_uses_fresh_handoff_with_all_owner_messages_in_order() {
    for context in [75, 90, 10] {
        let mut h = Harness::new();
        h.tick();
        source(&h, context);
        if context == 10 {
            let p = h.ctx.jobs.join("jobs/job1/job.json");
            let mut j = read_json(&p).unwrap();
            j["thread_id"] = Value::Null;
            atomic_json(&p, &j, 2).unwrap();
        }
        h.finish("job1", "running", Some("前の作業で修正済みの内容"), None);
        h.reply("追加指示その一", "100");
        h.reply("追加指示その二", "100");
        h.tick();
        let f = h.fake.borrow();
        let p = s(&f.launches[1]["prompt"]);
        assert_eq!(f.launches[1]["resume_session"], "");
        assert!(p.contains("READMEを直して"));
        assert!(p.contains("前の作業で修正済みの内容"));
        assert!(p.contains(h.root.path().to_str().unwrap()));
        let section = p.split("オーナーの全投稿（古い順）:").nth(1).unwrap();
        assert!(section.find("READMEを直して").unwrap() < section.find("追加指示その一").unwrap());
        assert!(section.find("追加指示その一").unwrap() < section.find("追加指示その二").unwrap());
    }
}
#[test]
fn finished_with_unseen_message_follows_up_immediately_and_reply_identifies_the_message() {
    for q in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        if q {
            question(&h);
        }
        h.finish("job1", "succeeded", Some("状態: 完了\n古い結果"), None);
        let mid = h.reply(
            "どういう意味？前回の指示がまったく反映されてないじゃん。",
            "100",
        );
        run(&mut h, q);
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert!(!h
            .fake
            .borrow()
            .posts
            .iter()
            .any(|(_, p)| s(&p["content"]).contains("古い結果")));
        h.finish(
            "job2",
            "succeeded",
            Some("状態: 完了\n指示を反映しました"),
            None,
        );
        run(&mut h, q);
        assert!(h.text().contains("今回反映したオーナーの投稿"));
        assert!(h.text().contains(&mid));
        assert!(h.text().contains("どういう意味？"));
    }
}
#[test]
fn receipt_is_once_even_after_checkpoint_loss_and_new_message_triggers_another_resume() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    let mid = h.reply("指示を追加", "100");
    h.tick();
    let mut st = h.state();
    st["answer_receipts"][&mid]["posted"] = json!(false);
    h.save(&st);
    h.tick();
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake
            .borrow()
            .posts
            .iter()
            .filter(|(_, p)| p["message_reference"]["message_id"] == mid)
            .count(),
        1
    );
    h.reply("再開中にもう一件", "100");
    h.tick();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert!(s(&h.fake.borrow().launches[2]["prompt"]).contains("再開中にもう一件"));
}
#[test]
fn lost_launch_response_and_crash_adopt_one_reserved_successor() {
    for crash in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        h.reply("追加指示", "100");
        if crash {
            h.fake.borrow_mut().crash_after_launch = true;
            assert!(catch_unwind(AssertUnwindSafe(|| h.tick())).is_err());
        } else {
            h.fake.borrow_mut().lose_launch_response = true;
            h.tick();
        }
        h.tick();
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert_eq!(h.state()["requests"]["100"]["job"], "job2");
    }
}
#[test]
fn failed_resume_session_starts_fresh_immediately() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    h.reply("追加指示", "100");
    h.tick();
    h.finish("job2", "failed", None, Some("no rollout found for session"));
    h.tick();
    let f = h.fake.borrow();
    assert_eq!(f.launches.len(), 3);
    assert_eq!(f.launches[2]["resume_session"], "");
    assert!(s(&f.launches[2]["prompt"]).contains("READMEを直して"));
    assert!(s(&f.launches[2]["prompt"]).contains("追加指示"));
}
#[test]
fn final_reply_gate_rechecks_after_result_preparation() {
    for snapshot in [2, 3] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        h.finish("job1", "succeeded", Some("状態: 完了\n古い結果"), None);
        h.fake.borrow_mut().history_injection =
            Some((snapshot, "100".into(), "最終返信の直前の指示".into()));
        h.tick();
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert!(!h
            .fake
            .borrow()
            .posts
            .iter()
            .any(|(_, p)| s(&p["content"]).contains("古い結果")));
        assert!(s(&h.fake.borrow().launches[1]["prompt"]).contains("最終返信の直前の指示"));
    }
}
#[test]
fn unrelated_thread_job_is_never_cancelled_even_in_same_folder() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    let dir = h.ctx.jobs.join("jobs/other");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(&dir.join("job.json"),&json!({"id":"other","status":"running","discord_thread_id":"200","cwd":h.root.path().to_string_lossy()}),2).unwrap();
    h.reply("追加指示", "100");
    h.tick();
    assert_eq!(
        read_json(&dir.join("job.json")).unwrap()["status"],
        "running"
    );
    assert!(!h
        .fake
        .borrow()
        .runs
        .iter()
        .any(|(a, _)| a.iter().any(|x| x == "cancel") && a.iter().any(|x| x == "other")));
}

#[test]
fn original_question_worker_receives_answer_despite_old_receipt_seen_or_finished_history() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 20);
    let dir = h.ctx.jobs.join("jobs/older");
    std::fs::create_dir_all(&dir).unwrap();
    atomic_json(
        &dir.join("job.json"),
        &json!({"id":"older","status":"succeeded","discord_thread_id":"100"}),
        2,
    )
    .unwrap();
    let mid = h.reply("前回の回答は反映済み扱いだったが作業では未反映", "100");
    let mut state = h.state();
    state["questions"] =
        json!([{"thread_id":"100","after":"100","seen_reply":mid,"title":"質問","open":true}]);
    h.save(&state);
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert_eq!(
        h.fake.borrow().launches[1]["resume_session"],
        "session-job1"
    );
    assert!(s(&h.fake.borrow().launches[1]["prompt"])
        .contains("前回の回答は反映済み扱いだったが作業では未反映"));
}

#[test]
fn updates_for_two_running_threads_share_one_launch_per_tick() {
    let mut h = Harness::new();
    h.fake.borrow_mut().threads.get_mut("200").unwrap()["owner_id"] = json!("OWN");
    h.fake
        .borrow_mut()
        .messages
        .get_mut("200")
        .unwrap()
        .push(message("200", "別の依頼", "OWN"));
    h.tick();
    h.tick();
    let first = h.reply("依頼Aの追加", "100");
    let second = h.reply("別の依頼の追加", "200");
    cc_ask::intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 3);
    assert_eq!(h.state()["answer_receipts"][&first]["handled_by"], "job3");
    assert!(!truth(&h.state()["answer_receipts"][&second]["handled_by"]));
    assert_eq!(job_status(&h.ctx, "job2"), "running");
    cc_ask::intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 4);
    assert!(s(&h.fake.borrow().launches[2]["prompt"]).contains("依頼Aの追加"));
    assert!(s(&h.fake.borrow().launches[3]["prompt"]).contains("別の依頼の追加"));
}

#[test]
fn reply_racing_archive_is_kept_across_restart_and_reaches_next_job() {
    for q in [false, true] {
        let mut h = Harness::new();
        h.tick();
        source(&h, 10);
        if q {
            question(&h);
        }
        h.finish(
            "job1",
            "succeeded",
            Some("状態: 完了\n最初の作業が完了しました"),
            None,
        );
        h.ctx.effects = Box::new(ReplyAtArchive {
            shared: SharedFake(h.fake.clone()),
            injected: false,
        });
        run(&mut h, q);
        assert_eq!(
            h.fake.borrow().threads["100"]["thread_metadata"]["archived"],
            false
        );
        // Reload the durable checkpoint; no in-memory receipt can rescue the test.
        let persisted = h.state();
        h.save(&persisted);
        for _ in 0..3 {
            run(&mut h, q);
        }
        assert_eq!(h.fake.borrow().launches.len(), 2);
        assert!(s(&h.fake.borrow().launches[1]["prompt"])
            .contains("終了と同時に届いた追加指示を必ず反映して"));
        let receipts = h.state()["answer_receipts"].clone();
        assert!(receipts
            .as_object()
            .unwrap()
            .values()
            .any(|r| r["handled_by"] == "job2"));
    }
}

#[test]
fn secondary_explicit_thread_of_one_job_receives_owner_update() {
    let mut h = Harness::new();
    h.tick();
    source(&h, 10);
    let path = h.ctx.jobs.join("jobs/job1/job.json");
    let mut info = read_json(&path).unwrap();
    info["discord_thread_id"] = json!("100");
    info["discord_thread_ids"] = json!(["100", "200"]);
    atomic_json(&path, &info, 2).unwrap();
    atomic_json(
        &h.ctx.jobs.join("jobs/job1/discord-threads.json"),
        &json!({"discord_thread_id":"100","thread_ids":["100","200"],"source":"explicit"}),
        2,
    )
    .unwrap();
    let mut state = h.state();
    state["questions"] = json!([{"thread_id":"200","after":"200","title":"同じ担当への質問","open":true,"continuation":{"job":"job1","after":"200","selection":{"model":"gpt-6.1-sol","effort":"max"}}}]);
    h.save(&state);
    let mid = h.reply("副スレッドの追加指示", "200");
    cc_ask::resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 2);
    assert!(s(&h.fake.borrow().launches[1]["prompt"]).contains("副スレッドの追加指示"));
    assert_eq!(h.state()["answer_receipts"][&mid]["handled_by"], "job2");
}
