use cc_ask::unassigned::*;
use serde_json::{Value, json};
fn item() -> Value {
    json!({"id":1,"project":"tools","status":"todo","jobs":[],"created_s":10000.,"updated":"2099-01-01 00:00"})
}
#[test]
fn thirty_minutes_uses_item_creation_even_with_unrelated_project_worker() {
    let jobs = [json!({"id":"other","todo_id":2,"project":"tools","status":"running"})];
    assert!(!detected(&item(), &jobs, &json!({}), 11799.999));
    assert!(detected(&item(), &jobs, &json!({}), 11800.));
}
#[test]
fn own_jobs_and_accepted_queue_are_ownership() {
    for status in ["queued", "starting", "running"] {
        for association in ["todo_id", "jobs", "scheduler", "session"] {
            let mut i = item();
            let mut j = json!({"id":"worker","status":status});
            match association {
                "todo_id" => j["todo_id"] = json!(1),
                "jobs" => i["jobs"] = json!(["worker"]),
                "scheduler" => i["scheduler"] = json!({"job":"worker"}),
                _ => {
                    i["jobs"] = json!(["session"]);
                    j["thread_id"] = json!("session");
                }
            }
            assert!(
                !detected(&i, &[j], &json!({}), 20000.),
                "{status}/{association}"
            );
        }
    }
    assert!(!detected(
        &item(),
        &[],
        &json!({"queue":[{"todo_id":1}]}),
        20000.
    ));
    assert!(detected(
        &item(),
        &[],
        &json!({"queue":[{"todo_id":2}]}),
        20000.
    ));
    assert!(detected(
        &item(),
        &[json!({"id":"old","todo_id":1,"status":"finished"})],
        &json!({}),
        20000.
    ));
}
#[test]
fn specifications_stops_reservations_and_owner_items_are_excluded() {
    for patch in [
        json!({"spec":"task.md"}),
        json!({"status":"doing"}),
        json!({"status":"done"}),
        json!({"status":"block"}),
        json!({"project":"owner"}),
        json!({"stopped":true}),
        json!({"cancel_requested":true}),
    ] {
        let mut i = item();
        i.as_object_mut()
            .unwrap()
            .extend(patch.as_object().unwrap().clone());
        assert!(!detected(&i, &[], &json!({}), 20000.));
    }
}
#[test]
fn saved_reservation_or_failed_post_alone_never_hides_an_unassigned_item() {
    for pending in [
        json!({}),
        json!({"reserved_at":10000.,"request":"req-todo-1-triage-20261007","key":"attempt","claim_until":30000.,"last_error":"POST timeout"}),
    ] {
        for phase in ["preparing", "reserved", "queued", "starting", "running"] {
            let mut i = item();
            i["stall_recovery"] = json!({"pending":pending});
            i["scheduler"] = json!({"phase":phase,"key":"attempt"});
            assert!(!detected(&i, &[], &json!({"queue":[]}), 11799.));
            assert!(detected(&i, &[], &json!({"queue":[]}), 11800.), "{phase}");
            assert!(!detected(
                &i,
                &[],
                &json!({"queue":[{"todo_id":1}]}),
                11800.
            ));
            assert!(!detected(
                &i,
                &[json!({"id":"actual","status":"queued","launch_key":"attempt"})],
                &json!({"queue":[]}),
                11800.
            ));
        }
    }
}
#[test]
fn legacy_created_time_and_unknown_or_future_time() {
    let mut i = item();
    i["created_s"] = Value::Null;
    i["created"] = json!("2026-09-30 22:00");
    assert!(!detected(&i, &[], &json!({}), 1790774999.));
    assert!(detected(&i, &[], &json!({}), 1790775000.));
    i["created"] = Value::Null;
    assert!(!detected(&i, &[], &json!({}), 20000.));
    i["created_s"] = json!(30000.);
    assert!(!detected(&i, &[], &json!({}), 20000.));
}
