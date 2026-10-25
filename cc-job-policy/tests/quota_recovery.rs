use cc_job_policy::quota_recovery::*;
use serde_json::{Value, json};

#[test]
fn stop_intent_is_bound_to_the_same_native_attempt_and_requires_confirmed_drain() {
    let mut source = json!({"codex_pid":7,"codex_pid_created":8,"started_at":"attempt-A","attempt_events_offset":20,
        "protocol_stall":{"stage":"stopping","child_pid":7,"child_created":8,"started_at":"attempt-A","attempt_events_offset":20}});
    assert!(collection_pending(&source));
    source["protocol_stall"]["stage"] = json!("drained");
    assert!(!collection_pending(&source));
    source["protocol_stall"]["stage"] = json!("stopping");
    for key in [
        "codex_pid",
        "codex_pid_created",
        "started_at",
        "attempt_events_offset",
    ] {
        let mut next = source.clone();
        next[key] = json!("new generation");
        assert!(!collection_pending(&next), "{key}");
    }
    source["protocol_stall"] = Value::Null;
    assert!(!collection_pending(&source));
}

#[test]
fn refreshed_oauth_aggregates_exhaustion_and_eligibility_without_private_fields() {
    let now = 1000.;
    let row = json!({"id":"private-id","email":"private@example.invalid","type":"codex",
        "health":"Healthy","creditsAfterLimit":false,"quotaRefresh":{"status":"ok"},
        "quota":{"weeklyPercent":3.,"weeklyResetAt":2000.,"updatedAt":1000.}});
    assert_eq!(
        aggregate(&json!({"accounts":[row.clone()]}), "openai", now),
        json!({"at":1000.,"verified":true,"available":true,"exhausted":false,"reason":"available_oauth"})
    );
    for field in [
        "type",
        "creditsAfterLimit",
        "health",
        "quotaUnavailable",
        "paused",
        "needsReauth",
        "autoSelectable",
    ] {
        let mut bad = row.clone();
        bad[field] = match field {
            "type" => json!("apikey"),
            "health" => json!("unknown"),
            "autoSelectable" => json!(false),
            _ => json!(true),
        };
        assert_ne!(
            aggregate(&json!({"accounts":[bad]}), "openai", now)["verified"],
            json!(true),
            "{field}"
        );
    }
    for update in [json!(500.), json!(1002.)] {
        let mut bad = row.clone();
        bad["quota"]["updatedAt"] = update;
        bad["quotaRefresh"] = Value::Null;
        assert_eq!(
            aggregate(&json!({"accounts":[bad]}), "openai", now)["verified"],
            false
        );
    }
    let mut openai = row.clone();
    openai["quota"]["updatedAt"] = Value::Null;
    openai["quotaRefresh"] = Value::Null;
    let mut unknown = openai.clone();
    unknown["quota"] = Value::Null;
    assert_eq!(
        aggregate(&json!({"accounts":[unknown,openai]}), "openai", now)["available"],
        true
    );
    assert_eq!(
        aggregate(
            &json!({"accounts":[row.clone(),{"type":"codex","health":"Healthy"}]}),
            "openai",
            now
        )["verified"],
        true
    );
    let mut depleted = row;
    depleted["type"] = json!("oauth");
    depleted["health"] = json!("Rate limited");
    depleted["quota"]["weeklyPercent"] = json!(100.);
    depleted["autoSelectable"] = json!(false);
    depleted["selectionExcludedReason"] = json!("weekly limit");
    assert_eq!(
        aggregate(&json!({"accounts":[depleted]}), "anthropic", now),
        json!({"at":1000.,"verified":true,"available":false,"exhausted":true,"reason":"exhausted_oauth"})
    );
}

#[test]
fn healthy_transition_rejects_stale_unknown_exhausted_and_same_provider() {
    let good = json!({"at":1000.,"anthropic":{"at":1000.,"verified":true,"exhausted":true,"available":false},
        "openai":{"at":1000.,"verified":true,"available":true,"exhausted":false}});
    assert!(healthy_transition(&good, "opus", "gpt-6-luna", 1000.));
    for (field, value) in [
        ("verified", json!(false)),
        ("available", json!(false)),
        ("at", json!(900.)),
        ("at", json!(1002.)),
    ] {
        let mut bad = good.clone();
        bad["openai"][field] = value;
        assert!(!healthy_transition(&bad, "opus", "gpt-6-luna", 1000.));
    }
    assert!(!healthy_transition(&good, "opus", "sonnet", 1000.));
    assert!(!healthy_transition(&good, "opus", "gpt-6-luna", f64::NAN));
}

#[test]
fn missing_quota_timestamp_is_only_a_fresh_openai_observation() {
    for used in [3., 100.] {
        let row = json!({"type":"oauth","health":if used == 100. { "Rate limited" } else { "Healthy" },
            "creditsAfterLimit":false,"quotaRefresh":{"status":"ok"},
            "quota":{"weeklyPercent":used,"weeklyResetAt":2000.}});
        let observed = aggregate(&json!({"accounts":[row]}), "anthropic", 1000.);
        assert_eq!(observed["verified"], false, "{used}: {observed}");
        assert_eq!(observed["available"], false);
        assert_eq!(observed["exhausted"], false);
    }
    let row = json!({"type":"codex","health":"Healthy","creditsAfterLimit":false,
        "quota":{"weeklyPercent":3.,"weeklyResetAt":2000.}});
    assert_eq!(
        aggregate(&json!({"accounts":[row]}), "openai", 1000.)["available"],
        true
    );
}

#[test]
fn a_reset_short_window_is_not_current_oauth_exhaustion() {
    for (percent, reset) in [
        ("fiveHourPercent", "fiveHourResetAt"),
        ("shortPercent", "shortResetAt"),
    ] {
        for reset_at in [json!(999.), json!(1000.), json!("1970-01-01T00:16:39Z")] {
            let mut row = json!({"type":"codex","health":"Healthy","creditsAfterLimit":false,
                "quota":{"weeklyPercent":3.,"weeklyResetAt":2000.,"updatedAt":1000.}});
            row["quota"][percent] = json!(100.);
            row["quota"][reset] = reset_at.clone();
            let observed = aggregate(&json!({"accounts":[row.clone()]}), "openai", 1000.);
            assert_eq!(
                observed["available"], true,
                "{reset}: {reset_at}: {observed}"
            );
            row["quota"][reset] = json!(1001.);
            assert_eq!(
                aggregate(&json!({"accounts":[row.clone()]}), "openai", 1000.)["exhausted"],
                true
            );
            row["quota"]["weeklyPercent"] = json!(100.);
            row["quota"][reset] = json!(999.);
            assert_eq!(
                aggregate(&json!({"accounts":[row]}), "openai", 1000.)["exhausted"],
                true
            );
        }
    }
}
