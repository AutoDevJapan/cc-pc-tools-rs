mod common;
use cc_ask::{
    core::*,
    history_audit::{self, Args},
};
use common::*;
use serde_json::{json, Value};
use std::{cell::RefCell, rc::Rc, time::Duration};

struct PrivateEffects {
    inner: SharedFake,
    protected: bool,
    malformed_history: bool,
    change_protection: Option<std::path::PathBuf>,
    budgets: Rc<RefCell<Vec<Duration>>>,
}
impl Effects for PrivateEffects {
    fn http(
        &mut self,
        method: &str,
        url: &str,
        body: Option<&Value>,
        discord: bool,
    ) -> Result<Value> {
        assert!(method == "GET" && discord && body.is_none());
        if self.malformed_history && url.contains("/messages?") {
            return Ok(json!({}));
        }
        let result = self
            .inner
            .http(method, url, body, discord)
            .map_err(|error| {
                if error == "message missing" && url.contains("/messages/") {
                    let path = url.strip_prefix("https://discord.com/api/v10").unwrap();
                    format!("Discord API GET {path} failed: 404 Unknown Message")
                } else {
                    error
                }
            });
        if url.contains("/messages?") {
            if let Some(path) = self.change_protection.take() {
                std::fs::write(path, "Confidential").unwrap();
            }
        }
        result
    }
    fn run(&mut self, args: &[String], timeout: u64) -> Result<Output> {
        assert!(args.len() == 3 && args[1] == "--guard-path" && timeout == 15);
        self.inner
            .0
            .borrow_mut()
            .runs
            .push((args.to_vec(), timeout));
        Ok(Output {
            stdout: json!({"client_resource":self.protected || args[2].ends_with("/protected")})
                .to_string(),
            ..Default::default()
        })
    }
    fn local_llm(
        &mut self,
        prompt: &str,
        budget: Duration,
        _accept: &dyn Fn(&str) -> bool,
    ) -> Option<String> {
        self.budgets.borrow_mut().push(budget);
        // Deliberately bypass accept: production code must validate even an
        // injected effect's answer rather than trusting its implementation.
        let mut fake = self.inner.0.borrow_mut();
        fake.llm_calls.push(prompt.into());
        (fake.llm_answer != "__none").then(|| fake.llm_answer.clone())
    }
}

fn setup() -> (Harness, Args, Rc<RefCell<Vec<Duration>>>) {
    let mut h = Harness::new();
    let budgets = Rc::new(RefCell::new(vec![]));
    h.ctx.effects = Box::new(PrivateEffects {
        inner: SharedFake(h.fake.clone()),
        protected: false,
        malformed_history: false,
        change_protection: None,
        budgets: budgets.clone(),
    });
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["guild_id"] = json!("G");
    let mut question = message(
        "100",
        "Atlas migrationを進めますか。a/bで返信してください。",
        "BOT",
    );
    question["author"]["bot"] = json!(true);
    h.fake
        .borrow_mut()
        .messages
        .insert("100".into(), vec![question, message("110", "a", "OWN")]);
    h.fake.borrow_mut().llm_answer = "{\"candidate_todos\":[17]}".into();
    h.save(&json!({"questions":[{"thread_id":"100","after":"100","question_message":"100","source":"posted","cwd":"D:/fixture"}],"answer_receipts":{}}));
    atomic_json(
        &h.ctx.todo,
        &json!({"projects":{"fixture":"D:/fixture"},"items":[
            {"id":17,"title":"Atlas migrationを実装する","project":"fixture"},
            {"id":18,"title":"無関係な処理","project":"fixture"}
        ]}),
        2,
    )
    .unwrap();
    let targets = h.root.path().join("targets.json");
    atomic_json(&targets, &json!([{"thread":"100","message":"110"}]), 2).unwrap();
    let args = Args {
        output: h.root.path().join("references.json"),
        reference_targets: Some(targets),
        limit: 100,
        capture: false,
        refresh: false,
        partition: 0,
        partitions: 1,
    };
    (h, args, budgets)
}

fn row(args: &Args) -> Value {
    read_json(&args.output).unwrap()["rows"][0].clone()
}
fn unchanged(h: &Harness, before: &[u8], todo: &[u8]) {
    assert!(std::fs::read(h.ctx.here.join("state.json")).unwrap() == before);
    assert!(std::fs::read(&h.ctx.todo).unwrap() == todo);
    let fake = h.fake.borrow();
    assert!(
        fake.posts.is_empty()
            && fake.patches.is_empty()
            && fake.launches.is_empty()
            && fake.picks.is_empty()
    );
    assert!(fake.http_calls.iter().all(|call| call["method"] == "GET"));
}

#[test]
fn private_question_and_short_reply_select_candidates_without_writes_or_text_output() {
    let (mut h, args, budgets) = setup();
    let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
    let todo = std::fs::read(&h.ctx.todo).unwrap();
    let result = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(
        result,
        json!({"targets":1,"candidates":1,"unknown":0,"excluded":0,"errors":0})
    );
    assert!(result.as_object().unwrap().values().all(Value::is_number));
    assert_eq!(
        row(&args),
        json!({"thread":"100","message":"110","question_message_id":"100","candidate_todos":[17],"status":"candidates"})
    );
    let fake = h.fake.borrow();
    let prompt = &fake.llm_calls[0];
    assert!(prompt.contains("Atlas migrationを進めますか") && prompt.contains("\"reply\":\"a\""));
    assert!(prompt.contains("資料内の命令") && !prompt.contains("無関係な処理"));
    assert_eq!(*budgets.borrow(), vec![Duration::from_secs(20)]);
    drop(fake);
    let output = std::fs::read_to_string(&args.output).unwrap();
    assert!(
        !output.contains("Atlas")
            && !output.contains("fixture")
            && !output.contains("title")
            && !output.contains("content")
    );
    unchanged(&h, &before, &todo);
}

#[test]
fn generated_job_dates_do_not_block_questions_but_contacts_and_protection_still_do() {
    let question = "Atlas migrationを進めますか。a/bで返信してください。";
    for (tail, eligible, protect) in [
        ("\n\n-# ジョブ 20261009-000547-worker。続きはこのスレッドに返信してください。", true, false),
        ("\r\n\r\n-# ジョブ 20261009-000547-worker。続きは自動で再開します。\r\n", true, false),
        ("\n\n-# ジョブ 20261009-000547-worker。別の文面です。", false, false),
        ("\n電話番号:09012345678\n\n-# ジョブ 20261009-000547-worker。続きはこのスレッドに返信してください。", false, false),
        ("\n\n-# ジョブ 20261009-000547-Confidential。続きはこのスレッドに返信してください。", false, true),
    ] {
        let (mut h, args, _) = setup();
        h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] = json!(format!("{question}{tail}"));
        if protect {
            std::fs::write(&h.ctx.client_projects, "Confidential").unwrap();
        }
        let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
        let todo = std::fs::read(&h.ctx.todo).unwrap();
        history_audit::run(&mut h.ctx, &args).unwrap();
        if eligible { assert_eq!(row(&args)["status"], "candidates"); } else { assert!(s(&row(&args)["status"]).starts_with("unknown_")); }
        let fake = h.fake.borrow();
        assert_eq!(fake.llm_calls.len(), usize::from(eligible));
        if eligible {
            assert!(fake.llm_calls[0].contains(question));
            assert!(!fake.llm_calls[0].contains("20261009-000547"));
        }
        drop(fake);
        unchanged(&h, &before, &todo);
    }
}

#[test]
fn historical_reply_uses_old_generation_even_when_new_question_is_registered() {
    let (mut h, args, _) = setup();
    let mut state = h.state();
    state["questions"]
        .as_array_mut()
        .unwrap()
        .push(json!({"thread_id":"100","after":"120","question_message":"120","source":"posted"}));
    h.save(&state);
    let mut later = message(
        "120",
        "Newtopicを進めますか。a/bで返信してください。",
        "BOT",
    );
    later["author"]["bot"] = json!(true);
    h.fake
        .borrow_mut()
        .messages
        .get_mut("100")
        .unwrap()
        .push(later);
    history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(row(&args)["question_message_id"], "100");
    assert!(!h.fake.borrow().llm_calls[0].contains("Newtopic"));
}

#[test]
fn foreign_owner_channel_anchor_or_reply_generation_is_never_selected() {
    for case in 0..7 {
        let (mut h, args, _) = setup();
        match case {
            0 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[1]["author"]["id"] =
                    json!("OTHER")
            }
            1 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[1]["channel_id"] = json!("200")
            }
            2 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["author"]["id"] =
                    json!("OTHERBOT")
            }
            3 => {
                let mut state = h.state();
                state["questions"][0]["after"] = json!("120");
                h.save(&state);
            }
            4 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[1]["message_reference"] =
                    json!({"channel_id":"200","message_id":"100"})
            }
            5 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[1]["message_reference"] =
                    json!({"channel_id":"100","message_id":"120"})
            }
            _ => {
                let mut state = h.state();
                state["questions"][0]["thread_id"] = json!("200");
                h.save(&state);
            }
        }
        history_audit::run(&mut h.ctx, &args).unwrap();
        assert!(row(&args)["candidate_todos"].as_array().unwrap().is_empty());
        assert!(h.fake.borrow().llm_calls.is_empty());
    }
}

#[test]
fn missing_or_newer_unregistered_question_is_unknown() {
    for missing in [true, false] {
        let (mut h, args, _) = setup();
        if missing {
            h.fake
                .borrow_mut()
                .messages
                .get_mut("100")
                .unwrap()
                .remove(0);
        } else {
            let mut newer = message("105", "別の質問です。a/bで返信してください。", "BOT");
            newer["author"]["bot"] = json!(true);
            h.fake
                .borrow_mut()
                .messages
                .get_mut("100")
                .unwrap()
                .push(newer);
        }
        history_audit::run(&mut h.ctx, &args).unwrap();
        assert!(s(&row(&args)["status"]).starts_with("unknown_"));
        assert!(h.fake.borrow().llm_calls.is_empty());
    }
}

#[test]
fn invalid_empty_incomplete_or_out_of_candidate_model_answer_is_unknown() {
    for answer in [
        "",
        "__none",
        "not json",
        "{\"candidate_todos\":[18]}",
        "{\"candidate_todos\":[999]}",
        "{\"candidate_todos\":[17],\"completed\":true}",
        "{\"candidate_todos\":[\"17\"]}",
        "{\"candidate_todos\":[17,17]}",
        "{\"candidate_todos\":[]}",
        "{\"candidate_todos\":[17]",
        "<|channel|>analysis<|message|>{\"candidate_todos\":[17]}",
    ] {
        let (mut h, args, _) = setup();
        h.fake.borrow_mut().llm_answer = answer.into();
        history_audit::run(&mut h.ctx, &args).unwrap();
        assert!(s(&row(&args)["status"]).starts_with("unknown_"));
        assert_eq!(row(&args)["candidate_todos"], json!([]));
        assert_eq!(h.fake.borrow().llm_calls.len(), 1);
    }
}

#[test]
fn protected_thread_is_excluded_before_history_and_protected_todos_never_enter_prompt() {
    for mode in 0..4 {
        let (mut h, args, _) = setup();
        if mode == 0 {
            std::fs::write(&h.ctx.client_projects, "Confidential").unwrap();
            h.fake.borrow_mut().threads.get_mut("100").unwrap()["name"] = json!("Confidential");
        } else if mode == 1 {
            h.ctx.effects = Box::new(PrivateEffects {
                inner: SharedFake(h.fake.clone()),
                protected: true,
                malformed_history: false,
                change_protection: None,
                budgets: Rc::default(),
            });
        } else {
            std::fs::write(&h.ctx.client_projects, "Confidential").unwrap();
            let mut db = read_json(&h.ctx.todo).unwrap();
            db["items"][0][if mode == 2 { "project" } else { "title" }] =
                json!("Confidential Atlas migration");
            atomic_json(&h.ctx.todo, &db, 2).unwrap();
        }
        history_audit::run(&mut h.ctx, &args).unwrap();
        assert!(h.fake.borrow().llm_calls.is_empty());
        if mode < 2 {
            assert_eq!(row(&args)["status"], "excluded");
            assert!(!h
                .fake
                .borrow()
                .http_calls
                .iter()
                .any(|c| s(&c["url"]).contains("/messages")));
        }
    }
}

#[test]
fn secrets_contact_data_and_changed_client_exclusion_never_enter_inference() {
    for text in [
        "Atlas api_key=private-secret-value",
        "Atlas hidden@example.invalid",
        "Atlas 090-1234-5678",
        "Atlas 09012345678",
        "Atlas 4155550123",
        "Atlas +1 415 555 0123",
        "Confidential Atlas",
    ] {
        for position in 0..3 {
            let (mut h, args, _) = setup();
            std::fs::write(&h.ctx.client_projects, "Confidential").unwrap();
            if position == 0 {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] =
                    json!(format!("{text}。a/bで返信してください。"));
            } else if position == 1 {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[1]["content"] = json!(text);
            } else {
                let mut db = read_json(&h.ctx.todo).unwrap();
                db["items"][0]["title"] = json!(text);
                atomic_json(&h.ctx.todo, &db, 2).unwrap();
            }
            history_audit::run(&mut h.ctx, &args).unwrap();
            assert!(s(&row(&args)["status"]).starts_with("unknown_"));
            assert!(h.fake.borrow().llm_calls.is_empty());
        }
    }
    let (mut h, args, _) = setup();
    h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] =
        json!("Confidential Atlas。a/bで返信してください。");
    h.ctx.effects = Box::new(PrivateEffects {
        inner: SharedFake(h.fake.clone()),
        protected: false,
        malformed_history: false,
        change_protection: Some(h.ctx.client_projects.clone()),
        budgets: Rc::default(),
    });
    history_audit::run(&mut h.ctx, &args).unwrap();
    assert!(h.fake.borrow().llm_calls.is_empty());
}

#[test]
fn each_failure_and_limit_is_recorded_and_checkpoint_kinds_cannot_mix() {
    let (mut h, mut args, _) = setup();
    atomic_json(args.reference_targets.as_ref().unwrap(), &json!([
        {"thread":"100","message":"110"},{"thread":"200","message":"210"},{"thread":"300","message":"310"}]), 2).unwrap();
    args.limit = 2;
    h.fake
        .borrow_mut()
        .http_errors
        .insert("/channels/200".into(), 1);
    let summary = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(summary["targets"], 3);
    assert_eq!(summary["errors"], 2);
    let report = read_json(&args.output).unwrap();
    assert_eq!(report["rows"][1]["status"], "metadata_unavailable");
    assert_eq!(report["rows"][2]["status"], "limit_reached");
    let before = std::fs::read(&args.output).unwrap();
    args.reference_targets = None;
    assert!(history_audit::run(&mut h.ctx, &args).is_err());
    assert!(std::fs::read(&args.output).unwrap() == before);
    let (mut h, args, _) = setup();
    atomic_json(
        &args.output,
        &json!({"version":1,"rows":{},"inventory":{}}),
        2,
    )
    .unwrap();
    let before = std::fs::read(&args.output).unwrap();
    assert!(history_audit::run(&mut h.ctx, &args).is_err());
    assert!(std::fs::read(&args.output).unwrap() == before);
    assert!(h.fake.borrow().http_calls.is_empty());
}

#[test]
fn no_lexical_candidates_means_no_model_call_and_malformed_history_is_an_error() {
    let (mut h, args, _) = setup();
    atomic_json(
        &h.ctx.todo,
        &json!({"items":[{"id":17,"title":"Differentword"}]}),
        2,
    )
    .unwrap();
    history_audit::run(&mut h.ctx, &args).unwrap();
    assert!(s(&row(&args)["status"]).starts_with("unknown_"));
    assert!(h.fake.borrow().llm_calls.is_empty());
    let (mut h, args, _) = setup();
    h.ctx.effects = Box::new(PrivateEffects {
        inner: SharedFake(h.fake.clone()),
        protected: false,
        malformed_history: true,
        change_protection: None,
        budgets: Rc::default(),
    });
    let summary = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(row(&args)["status"], "history_unavailable");
    assert_eq!(summary["errors"], 1);
    assert!(h.fake.borrow().llm_calls.is_empty());
}

#[test]
fn capture_refresh_partition_and_non_id_input_are_rejected_before_effects() {
    for mode in 0..5 {
        let (mut h, mut args, _) = setup();
        match mode {
            0 => args.capture = true,
            1 => args.refresh = true,
            2 => args.partition = 1,
            3 => args.partitions = 2,
            _ => atomic_json(
                args.reference_targets.as_ref().unwrap(),
                &json!([{"thread":"100","message":"110","content":"forbidden"}]),
                2,
            )
            .unwrap(),
        }
        assert!(history_audit::run(&mut h.ctx, &args).is_err());
        assert!(h.fake.borrow().http_calls.is_empty());
    }
}

#[test]
fn only_fifteen_lexically_ranked_safe_todos_are_sent_to_model() {
    let (mut h, args, _) = setup();
    let mut items: Vec<Value> = (1..=30)
        .map(|id| json!({"id":id,"title":"Atlas migration","project":"fixture"}))
        .collect();
    items.push(json!({"id":99,"title":"Atlas migration protected","project":"hidden"}));
    atomic_json(
        &h.ctx.todo,
        &json!({"projects":{"fixture":"D:/fixture","hidden":"D:/protected"},"items":items}),
        2,
    )
    .unwrap();
    h.fake.borrow_mut().llm_answer = "{\"candidate_todos\":[1]}".into();
    history_audit::run(&mut h.ctx, &args).unwrap();
    let fake = h.fake.borrow();
    let prompt = &fake.llm_calls[0];
    let (_, data) = prompt.split_once('\n').unwrap();
    let data: Value = serde_json::from_str(data).unwrap();
    let candidates = data["candidates"].as_array().unwrap();
    assert_eq!(candidates.len(), 15);
    assert!(candidates.iter().all(|c| c.as_object().unwrap().len() == 2));
    assert_eq!(candidates[0]["id"], 1);
    assert_eq!(candidates[14]["id"], 15);
    assert!(!prompt.contains("D:/") && !prompt.contains("protected"));
}

#[test]
fn untrusted_scope_is_refused_before_history_but_allowed_log_and_bot_task_work() {
    for mode in 0..6 {
        let (mut h, args, _) = setup();
        let state = h.state();
        h.save(&json!({"questions":[],"requests":{}}));
        let mut thread = h.fake.borrow().threads["100"].clone();
        thread["parent_id"] = json!("OTHER");
        match mode {
            0 => thread["guild_id"] = json!("OTHER"),
            1 => thread["type"] = json!(0),
            2 => {
                thread["parent_id"] = json!("T");
                thread["owner_id"] = json!("OTHERBOT");
            }
            3 => {}
            4 => {
                thread["parent_id"] = json!("T");
                thread["owner_id"] = json!("BOT");
            }
            _ => atomic_json(
                &h.ctx.here.join("log_threads.json"),
                &json!({"allowed":"100"}),
                2,
            )
            .unwrap(),
        }
        h.fake.borrow_mut().threads.insert("100".into(), thread);
        // Scope permissions alone cannot invent a missing registered generation.
        history_audit::run(&mut h.ctx, &args).unwrap();
        if mode < 4 {
            assert_eq!(row(&args)["status"], "outside_scope");
            assert!(!h
                .fake
                .borrow()
                .http_calls
                .iter()
                .any(|c| s(&c["url"]).contains("/messages")));
        } else {
            assert!(s(&row(&args)["status"]).starts_with("unknown_"));
            assert!(h
                .fake
                .borrow()
                .http_calls
                .iter()
                .any(|c| s(&c["url"]).contains("/messages")));
            h.save(&state);
            history_audit::run(&mut h.ctx, &args).unwrap();
            assert_eq!(row(&args)["status"], "candidates");
        }
    }
}

#[test]
fn reads_entire_history_and_does_not_pick_a_later_owner_reply() {
    let (mut h, args, _) = setup();
    h.fake
        .borrow_mut()
        .messages
        .get_mut("100")
        .unwrap()
        .extend((111..=315).map(|id| message(&id.to_string(), "later unrelated reply", "OWN")));
    history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(row(&args)["status"], "candidates");
    let fake = h.fake.borrow();
    assert!(
        fake.http_calls
            .iter()
            .filter(|c| s(&c["url"]).contains("/messages?"))
            .count()
            >= 3
    );
    assert!(!fake.llm_calls[0].contains("later unrelated reply"));
}

#[test]
fn protected_attachment_labels_and_ambiguous_or_routing_generations_are_unknown() {
    for mode in 0..3 {
        let (mut h, args, _) = setup();
        match mode {
            0 => {
                std::fs::write(&h.ctx.client_projects, "Confidential").unwrap();
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["attachments"] =
                    json!([{"filename":"Confidential.txt"}]);
            }
            1 => {
                let mut state = h.state();
                let duplicate = state["questions"][0].clone();
                state["questions"].as_array_mut().unwrap().push(duplicate);
                h.save(&state);
            }
            _ => {
                let mut state = h.state();
                state["questions"][0]["source"] = json!("routing");
                h.save(&state);
            }
        }
        history_audit::run(&mut h.ctx, &args).unwrap();
        assert!(s(&row(&args)["status"]).starts_with("unknown_"));
        assert!(h.fake.borrow().llm_calls.is_empty());
    }
}

#[test]
fn cli_flag_works_with_defaults_and_rejects_explicit_mutating_or_partition_flags() {
    use clap::Parser;
    #[derive(Parser)]
    struct Cli {
        #[command(flatten)]
        args: Args,
    }
    assert!(Cli::try_parse_from([
        "audit",
        "--output",
        "out.json",
        "--reference-targets",
        "in.json"
    ])
    .is_ok());
    for extra in [
        vec!["--capture"],
        vec!["--refresh"],
        vec!["--partition", "0"],
        vec!["--partitions", "1"],
    ] {
        let mut argv = vec![
            "audit",
            "--output",
            "out.json",
            "--reference-targets",
            "in.json",
        ];
        argv.extend(extra);
        assert!(Cli::try_parse_from(argv).is_err());
    }
}

#[test]
fn empty_target_list_and_failed_todo_read_remain_id_only() {
    let (mut h, args, _) = setup();
    atomic_json(args.reference_targets.as_ref().unwrap(), &json!([]), 2).unwrap();
    let summary = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(summary["targets"], 0);
    assert_eq!(read_json(&args.output).unwrap()["rows"], json!([]));
    assert!(h.fake.borrow().http_calls.is_empty());
    let (mut h, args, _) = setup();
    std::fs::write(&h.ctx.todo, "{invalid}").unwrap();
    history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(row(&args)["status"], "todo_unavailable");
    assert!(h.fake.borrow().llm_calls.is_empty());
}

#[test]
fn output_cannot_replace_or_create_operational_state_or_input_files() {
    for name in [
        "state.json",
        "routing_state.json",
        "config.json",
        "log_threads.json",
        "todo.json",
        "client-projects.txt",
        "targets.json",
    ] {
        let (mut h, mut args, _) = setup();
        args.output = h.ctx.here.join(name);
        let before = std::fs::read(&args.output).ok();
        assert!(history_audit::run(&mut h.ctx, &args).is_err());
        assert!(std::fs::read(&args.output).ok() == before);
        assert!(h.fake.borrow().http_calls.is_empty());
    }
}

#[test]
fn old_anchor_link_cannot_steal_a_reply_to_a_new_question() {
    let (mut h, args, _) = setup();
    let mut state = h.state();
    state["questions"].as_array_mut().unwrap().push(json!({"thread_id":"100","after":"105",
        "question_message":"105","original_question":"100","message_ids":["100","105"],"source":"posted"}));
    h.save(&state);
    let mut question = message("105", "新しい質問です。a/bで返信してください。", "BOT");
    question["author"]["bot"] = json!(true);
    h.fake
        .borrow_mut()
        .messages
        .get_mut("100")
        .unwrap()
        .push(question);
    history_audit::run(&mut h.ctx, &args).unwrap();
    assert!(s(&row(&args)["status"]).starts_with("unknown_"));
    assert!(h.fake.borrow().llm_calls.is_empty());
}

#[test]
fn diagnostic_statuses_separate_unresolved_origins_from_acquisition_failures() {
    for (case, expected) in [
        (0, "unknown_owner_message_missing"),
        (1, "unknown_owner_identity_mismatch"),
        (2, "unknown_registration_missing_before_reply"),
        (3, "unknown_registration_ambiguous"),
        (4, "unknown_anchor_not_found"),
        (5, "unknown_anchor_not_bot"),
        (6, "unknown_bot_identity_mismatch"),
        (7, "unknown_question_format_unrecognized"),
        (8, "anchor_unavailable"),
    ] {
        let (mut h, args, _) = setup();
        match case {
            0 => {
                h.fake
                    .borrow_mut()
                    .messages
                    .get_mut("100")
                    .unwrap()
                    .remove(1);
            }
            1 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[1]["author"]["id"] =
                    json!("OTHER")
            }
            2 => {
                let mut state = h.state();
                state["questions"] = json!([]);
                h.save(&state);
            }
            3 => {
                let mut state = h.state();
                let duplicate = state["questions"][0].clone();
                state["questions"].as_array_mut().unwrap().push(duplicate);
                h.save(&state);
            }
            4 => {
                h.fake
                    .borrow_mut()
                    .messages
                    .get_mut("100")
                    .unwrap()
                    .remove(0);
            }
            5 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["author"]["bot"] =
                    json!(false)
            }
            6 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["author"]["id"] =
                    json!("UNTRUSTED")
            }
            7 => {
                h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] =
                    json!("legacy migration material")
            }
            _ => {
                let mut state = h.state();
                state["questions"][0]["after"] = json!("105");
                state["questions"][0]["question_message"] = json!("105");
                h.save(&state);
                h.fake
                    .borrow_mut()
                    .http_errors
                    .insert("/channels/100/messages/105".into(), 1);
            }
        }
        let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
        let todo = std::fs::read(&h.ctx.todo).unwrap();
        let result = history_audit::run(&mut h.ctx, &args).unwrap();
        assert_eq!(row(&args)["status"], expected);
        assert_eq!(result["unknown"], if case == 8 { 0 } else { 1 });
        assert_eq!(result["errors"], if case == 8 { 1 } else { 0 });
        assert!(h.fake.borrow().llm_calls.is_empty());
        assert_eq!(row(&args).as_object().unwrap().len(), 5);
        unchanged(&h, &before, &todo);
    }
}

#[test]
fn legacy_format_requires_registered_exact_origin_and_verified_bot_thread_owner() {
    for (historical, registered, owner_matches) in [
        (false, true, true),
        (true, true, true),
        (true, false, true),
        (true, true, false),
        (false, true, false),
    ] {
        let (mut h, args, _) = setup();
        let author = if historical { "OLDBOT" } else { "BOT" };
        h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["author"]["id"] = json!(author);
        h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] =
            json!("legacy Atlas migration material");
        if owner_matches {
            h.fake.borrow_mut().threads.get_mut("100").unwrap()["owner_id"] = json!(author);
        }
        if !registered {
            let mut state = h.state();
            state["questions"] = json!([]);
            h.save(&state);
        }
        let before = std::fs::read(h.ctx.here.join("state.json")).unwrap();
        let todo = std::fs::read(&h.ctx.todo).unwrap();
        history_audit::run(&mut h.ctx, &args).unwrap();
        let accepted = registered && owner_matches;
        assert_eq!(h.fake.borrow().llm_calls.len(), usize::from(accepted));
        if accepted {
            assert_eq!(row(&args)["status"], "candidates");
        } else {
            assert!(s(&row(&args)["status"]).starts_with("unknown_"));
        }
        unchanged(&h, &before, &todo);
    }
}

#[test]
fn legacy_origin_does_not_accept_an_arbitrary_cursor_or_intervening_bot_post() {
    for mode in 0..2 {
        let (mut h, args, _) = setup();
        h.fake.borrow_mut().threads.get_mut("100").unwrap()["owner_id"] = json!("BOT");
        h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] =
            json!("legacy Atlas migration material");
        let mut later = message("105", "another legacy post", "BOT");
        later["author"]["bot"] = json!(true);
        h.fake
            .borrow_mut()
            .messages
            .get_mut("100")
            .unwrap()
            .push(later);
        if mode == 0 {
            let mut state = h.state();
            state["questions"][0]["after"] = json!("105");
            state["questions"][0]
                .as_object_mut()
                .unwrap()
                .remove("question_message");
            h.save(&state);
        }
        history_audit::run(&mut h.ctx, &args).unwrap();
        assert_eq!(
            row(&args)["status"],
            if mode == 0 {
                "unknown_question_format_unrecognized"
            } else {
                "unknown_intervening_legacy_post"
            }
        );
        assert!(h.fake.borrow().llm_calls.is_empty());
    }
}

#[test]
fn not_found_registered_anchor_is_not_replaced_by_an_older_available_root() {
    let (mut h, args, _) = setup();
    let mut state = h.state();
    state["questions"][0]["after"] = json!("105");
    state["questions"][0]["question_message"] = json!("105");
    h.save(&state);
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["owner_id"] = json!("BOT");
    let result = history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(row(&args)["status"], "unknown_anchor_not_found");
    assert_eq!(row(&args)["question_message_id"], "105");
    assert_eq!(result["unknown"], 1);
    assert_eq!(result["errors"], 0);
    assert!(h.fake.borrow().llm_calls.is_empty());
    assert!(h
        .fake
        .borrow()
        .http_calls
        .iter()
        .any(|call| s(&call["url"]).ends_with("/messages/105")));
}

#[test]
fn path_guards_are_bounded_after_lexical_ranking_and_do_not_scan_unrelated_items() {
    let (mut h, args, _) = setup();
    let mut items: Vec<Value> = (1..=60)
        .map(|id| {
            json!({"id":id,"title":"Atlas migration",
        "cwd":format!("D:/item-{id}/protected")})
        })
        .collect();
    items.push(json!({"id":61,"title":"Atlas migration","cwd":"D:/safe"}));
    items.push(json!({"id":62,"title":"Differentword","cwd":"D:/unrelated"}));
    atomic_json(&h.ctx.todo, &json!({"items":items}), 2).unwrap();
    history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(row(&args)["status"], "unknown_no_candidates");
    let fake = h.fake.borrow();
    assert!(fake.llm_calls.is_empty());
    assert_eq!(
        fake.runs
            .iter()
            .filter(|(args, _)| args[2].ends_with("/protected"))
            .count(),
        60
    );
    assert!(!fake
        .runs
        .iter()
        .any(|(args, _)| args[2] == "D:/safe" || args[2] == "D:/unrelated"));
}

#[test]
fn reanchored_legacy_generation_cannot_hide_a_new_legacy_post() {
    let (mut h, args, _) = setup();
    h.fake.borrow_mut().threads.get_mut("100").unwrap()["owner_id"] = json!("BOT");
    h.fake.borrow_mut().messages.get_mut("100").unwrap()[0]["content"] =
        json!("legacy Atlas migration material");
    let mut later = message("105", "new legacy migration material", "BOT");
    later["author"]["bot"] = json!(true);
    h.fake
        .borrow_mut()
        .messages
        .get_mut("100")
        .unwrap()
        .push(later);
    let mut state = h.state();
    state["questions"].as_array_mut().unwrap().push(json!({"thread_id":"100","after":"105",
        "question_message":"105","original_question":"100","message_ids":["100","105"],"source":"posted"}));
    h.save(&state);
    history_audit::run(&mut h.ctx, &args).unwrap();
    assert_eq!(row(&args)["status"], "unknown_intervening_legacy_post");
    assert!(h.fake.borrow().llm_calls.is_empty());
}
