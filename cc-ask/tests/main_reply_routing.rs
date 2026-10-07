mod common;
use cc_ask::{commands::cmd_inbox, intake, resume, routing};
use common::{message, Harness};
use serde_json::json;

fn setup(agent: &str, open: bool, archived: bool, cwd: bool) -> Harness {
    let h = Harness::new();
    let mut question = json!({"thread_id":"300","after":"300","title":"メインの質問",
        "agent":agent,"open":open});
    if cwd {
        question["cwd"] = json!(h.root.path().to_string_lossy());
    }
    h.save(&json!({"questions":[question],"requests":{},"reply_intake_since":"300"}));
    let mut f = h.fake.borrow_mut();
    f.threads.clear();
    f.messages.clear();
    f.channels = vec![json!({"id":"T","type":15,"name":"作業"})];
    f.threads.insert(
        "300".into(),
        json!({"id":"300","guild_id":"G","type":11,
        "parent_id":"T","owner_id":"BOT","name":"メインの質問","last_message_id":"400",
        "thread_metadata":{"archived":archived}}),
    );
    f.messages.insert(
        "300".into(),
        vec![message("400", "この方針で進めて", "OWN")],
    );
    f.now += 600.;
    drop(f);
    h
}

#[test]
fn main_answers_only_reach_inbox_including_closed_archived_and_pending_questions() {
    for open in [false, true] {
        for archived in [false, true] {
            for cwd in [false, true] {
                for pending in [false, true] {
                    let mut h = setup("claude-main", open, archived, cwd);
                    // Additional names never remove the built-in main session.
                    h.ctx.cfg["inbox_only_agents"] = json!(["other-main"]);
                    if pending {
                        let mut state = h.state();
                        let launch = json!({"key":"1234567890abcdef1234567890abcdef",
                            "selection":{"model":"gpt-6.1-sol","effort":"high"}});
                        state["requests"]["300"] = json!({"after":"300","launch":launch});
                        state["questions"][0]["continuation"] = json!({"launch":launch,
                            "after":"400","prompt":"古い起動待ち"});
                        h.save(&state);
                    }
                    let before = h.state();
                    let mut budget = false;
                    // Both orders occur on alternating ticks.
                    for intake_first in [false, true] {
                        if intake_first {
                            intake::cmd_intake(&mut h.ctx, &mut budget).unwrap();
                            resume::cmd_resume(&mut h.ctx, &mut budget).unwrap();
                        } else {
                            resume::cmd_resume(&mut h.ctx, &mut budget).unwrap();
                            intake::cmd_intake(&mut h.ctx, &mut budget).unwrap();
                        }
                    }
                    let mut question = h.state()["questions"][0].clone();
                    let mut state = h.state();
                    resume::resume_question(
                        &mut h.ctx,
                        &mut state,
                        &mut question,
                        "OWN",
                        &mut budget,
                    )
                    .unwrap();
                    assert!(!budget);
                    let after = h.state();
                    assert_eq!(after["questions"], before["questions"]);
                    assert_eq!(after["requests"], before["requests"]);
                    assert!(after["answer_receipts"]["400"].is_null());
                    let mark = h.root.path().join("inbox.mark");
                    std::fs::write(&mark, "300").unwrap();
                    let messages = cmd_inbox(&mut h.ctx, &mark).unwrap();
                    assert_eq!(messages.len(), 1);
                    assert_eq!(messages[0]["id"], "400");
                    assert_eq!(messages[0]["text"], "この方針で進めて");
                    assert!(cmd_inbox(&mut h.ctx, &mark).unwrap().is_empty());
                    let f = h.fake.borrow();
                    assert!(f.launches.is_empty());
                    assert!(f.picks.is_empty());
                    assert!(f.posts.is_empty());
                    assert!(f.patches.is_empty());
                }
            }
        }
    }
}

#[test]
fn configured_main_name_is_inbox_only_but_regular_and_similar_names_still_launch() {
    for (agent, launches) in [
        ("codex-main", 0),
        ("Codex-worker", 1),
        ("claude-main-worker", 1),
        ("", 1),
    ] {
        let mut h = setup(agent, false, true, false);
        h.ctx.cfg["inbox_only_agents"] = json!(["codex-main"]);
        intake::cmd_intake(&mut h.ctx, &mut false).unwrap();
        assert_eq!(h.fake.borrow().launches.len(), launches, "{agent}");
    }
}

#[test]
fn regular_open_question_with_cwd_still_resumes() {
    let mut h = setup("Codex-worker", true, false, true);
    resume::cmd_resume(&mut h.ctx, &mut false).unwrap();
    assert_eq!(h.fake.borrow().launches.len(), 1);
    assert_eq!(h.state()["answer_receipts"]["400"]["handled_by"], "job1");
}

#[test]
fn main_policy_requires_a_registered_agent_and_ignores_legacy_routing_records() {
    let h = setup("claude-main", false, false, false);
    assert!(routing::question_inbox_only(
        &h.ctx,
        &json!({"agent":"claude-main"})
    ));
    assert!(!routing::question_inbox_only(
        &h.ctx,
        &json!({"agent":"claude-main","source":"routing"})
    ));
    assert!(!routing::question_inbox_only(
        &h.ctx,
        &json!({"title":"claude-main"})
    ));
}
