//! Reproduce the title-only historical audit without Discord or model calls.
use cc_ask::ask_gate::blocked;
use serde_json::{json, Value};

fn main() {
    let rows: Value =
        serde_json::from_str(include_str!("../tests/fixtures/ask-history-20261007.json")).unwrap();
    let mut counts = json!({"total":0,"blocked":0,"passed":0,"owner_blocked":0,"explicit_routine_passed":0,"routine_without_permission_passed":0});
    let decisions: Vec<_> = rows
        .as_array()
        .unwrap()
        .iter()
        .map(|row| {
            let title = row["title"].as_str().unwrap();
            let denied = blocked(title, "", None);
            let explicit = row["expected_blocked"] == true;
            for key in ["total", if denied { "blocked" } else { "passed" }] {
                counts[key] = json!(counts[key].as_u64().unwrap() + 1);
            }
            let mismatch = if denied && row["classification"] == "owner" {
                Some("owner_blocked")
            } else if !denied && row["classification"] == "routine" {
                Some(if explicit {
                    "explicit_routine_passed"
                } else {
                    "routine_without_permission_passed"
                })
            } else {
                None
            };
            if let Some(key) = mismatch {
                counts[key] = json!(counts[key].as_u64().unwrap() + 1);
            }
            json!({"title":title,"classification":row["classification"],"blocked":denied})
        })
        .collect();
    println!(
        "{}",
        serde_json::to_string_pretty(&json!({"counts":counts,"decisions":decisions})).unwrap()
    );
}
