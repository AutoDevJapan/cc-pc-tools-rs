use cc_job_policy::quota_recovery::*;
use serde_json::{Value, json};

#[test]
fn refreshed_oauth_aggregates_exhaustion_and_eligibility_without_private_fields() {
    let now = 1000.;
    let row = json!({"id":"private-id","email":"private@example.invalid","type":"codex",
        "health":"Healthy","creditsAfterLimit":false,"quotaRefresh":{"status":"ok"},
        "quota":{"weeklyPercent":3.,"weeklyResetAt":2000.,"updatedAt":1000.}});
    assert_eq!(
        aggregate(&json!({"accounts":[row.clone()]}), "openai", now),
        json!({"at":1000.,"verified":true,"available":true,"exhausted":false})
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
    for update in [json!(900.), json!(1002.), Value::Null] {
        let mut bad = row.clone();
        bad["quota"]["updatedAt"] = update;
        bad["quotaRefresh"] = Value::Null;
        assert_eq!(
            aggregate(&json!({"accounts":[bad]}), "openai", now)["verified"],
            false
        );
    }
    let mut depleted = row;
    depleted["type"] = json!("oauth");
    depleted["health"] = json!("Rate limited");
    depleted["quota"]["weeklyPercent"] = json!(100.);
    depleted["autoSelectable"] = json!(false);
    depleted["selectionExcludedReason"] = json!("weekly limit");
    assert_eq!(
        aggregate(&json!({"accounts":[depleted]}), "anthropic", now),
        json!({"at":1000.,"verified":true,"available":false,"exhausted":true})
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
