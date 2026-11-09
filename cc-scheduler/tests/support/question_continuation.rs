use super::*;
use cc_scheduler::question_continuation::{control, metadata, RECORDS};

#[test]
fn prepared_owner_input_change_defers_before_launch() {
    prepared_input_window(false);
}
#[test]
fn prepared_owner_input_last_window_defers_before_launch() {
    prepared_input_window(true);
}
fn prepared_input_window(final_window: bool) {
    let mut f = Fixture::new();
    let mut row = item(1, "other", "todo", &[]);
    row["scheduler"] = json!({"owner_instruction":"FIRST_OWNER_REPLY"});
    f.put(row);
    f.e.backend.question_store = Some(f.e.todo_path.clone());
    let path = f.e.todo_path.clone();
    let action = Box::new(move || {
        let mut db = read_json(&path, json!({})).unwrap();
        db["items"][0]["scheduler"]["owner_instruction"] = json!("LATER_EDITED_OWNER_REPLY");
        write_json(&path, &db, 1).unwrap();
    });
    if final_window {
        f.e.backend.question_launch = Some(action);
    } else {
        f.e.backend.question_measure = Some(action);
    }
    let _ = f.e.dispatch(&f.cfg, &f.pri);
    assert!(
        f.e.backend.launched.is_empty(),
        "stale prepared prompt launched: final_window={final_window}"
    );
    assert_eq!(
        f.get(1)["scheduler"]["owner_instruction"],
        "LATER_EDITED_OWNER_REPLY"
    );
}

#[test]
fn answered_question_records_actual_scheduler_prepare_and_link_once() {
    let mut f = Fixture::new();
    let mut row = item(1, "other", "todo", &[]);
    row["jobs"] = json!(["original"]);
    row["resume_generation"] = json!(2);
    row["worker_receipt"] = json!({"job":null,"generation":2});
    row["scheduler"] = json!({"job":null,"key":null,"last_job":"original","phase":"resumed",
        "resume":true,"owner_instruction":"full verified owner reply"});
    row["owner_question_answer_receipts"] = json!([{"domain":"todo-owner-question-answer-receipt-v1",
        "answer_file_sha256":"a".repeat(64),"answer":{"request_id":"question", "question_receipt_sha256":"b".repeat(64)}}]);
    f.put(row);
    let original = f.get(1);
    let prepared =
        f.e.prepare(&original, f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    assert_eq!(prepared[RECORDS].as_array().unwrap().len(), 2);
    assert_eq!(prepared[RECORDS][0]["operation"], "scheduler_prepare");
    assert_eq!(prepared[RECORDS][0]["before"], control(&original));
    assert_eq!(
        prepared[RECORDS][0]["after"]["scheduler"]["phase"],
        "preparing"
    );
    assert_eq!(
        prepared[RECORDS][1]["before"],
        prepared[RECORDS][0]["after"]
    );
    assert_eq!(prepared[RECORDS][1]["operation"], "scheduler_prepared");
    assert_eq!(prepared[RECORDS][1]["after"], control(&prepared));
    assert_eq!(
        prepared[RECORDS][0]["metadata"],
        metadata(&original, &json!(f.repo))
    );
    assert!(fs::read_to_string(s(&prepared["scheduler"]["prompt"]))
        .unwrap()
        .contains("full verified owner reply"));
    let next = json!({"id":"successor","todo_id":1,"status":"running","created_at":T,
        "launch_key":prepared["scheduler"]["key"]});
    f.e.link(&json!(1), &next, &prepared["scheduler"]["key"])
        .unwrap();
    let linked = f.get(1);
    assert_eq!(linked[RECORDS].as_array().unwrap().len(), 3);
    assert_eq!(linked[RECORDS][2]["operation"], "scheduler_link");
    assert_eq!(linked[RECORDS][2]["before"], control(&prepared));
    assert_eq!(linked[RECORDS][2]["after"], control(&linked));
    assert_eq!(linked["resume_generation"], 2);
    assert_eq!(linked["worker_receipt"], original["worker_receipt"]);
    assert_eq!(linked["scheduler"]["job"], "successor");
    assert_eq!(
        linked["scheduler"]["owner_instruction"],
        original["scheduler"]["owner_instruction"]
    );
    f.e.link(&json!(1), &next, &prepared["scheduler"]["key"])
        .unwrap();
    assert_eq!(f.get(1), linked, "same link added a duplicate transition");
}

#[test]
fn ordinary_scheduler_tasks_do_not_get_question_continuation_rights() {
    let mut f = Fixture::new();
    f.put(item(1, "other", "todo", &[]));
    let prepared =
        f.e.prepare(&f.get(1), f.repo.to_str().unwrap())
            .unwrap()
            .unwrap();
    let next = json!({"id":"successor","todo_id":1,"status":"running","created_at":T});
    f.e.link(&json!(1), &next, &prepared["scheduler"]["key"])
        .unwrap();
    assert!(f.get(1)[RECORDS].is_null());
}

#[test]
fn unstarted_input_retirement_keeps_concurrent_holds() {
    for field in ["owner_stopped", "paused", "owner_question_wait"] {
        let mut f = Fixture::new();
        let mut row = item(1, "other", "doing", &[]);
        row["resume_generation"] = json!(3);
        row["jobs"] = json!(["original", "unstarted"]);
        row["worker_receipt"] = json!({"job":"unstarted","generation":3});
        row["scheduler"] = json!({"job":"unstarted","key":"a".repeat(32),"phase":"running","owner_instruction":"LATEST_REPLY"});
        row[field] = if field == "owner_question_wait" {
            json!({"domain":"other-question"})
        } else {
            json!(true)
        };
        f.put(row);
        let before = f.get(1);
        f.e.requeue(&json!(1), &json!("unstarted"), "stale_prepared_input")
            .unwrap();
        assert_eq!(f.get(1), before, "{field} lost to stale input recovery");
    }
}
