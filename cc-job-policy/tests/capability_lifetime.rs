use cc_job_policy::capability_scope::*;
use serde_json::json;

#[test]
fn field_masks_keep_durable_and_temporary_constraints_distinct() {
    for (sandbox, mcp) in [(true, false), (false, true), (true, true), (false, false)] {
        let mut source = json!({"id":"source","todo_id":1,"launch_key":"finite-key",
            "sandbox":"read-only","mcp_profile":"minimal","scope_source":"owner_marker",
            "capability_provenance_version":1,
            "explicit_capability_constraints":{"sandbox":sandbox,"mcp_profile":mcp},
            "fixed_read_only_review":{"bound":true}});
        let fixed = retained(&source);
        assert_eq!(fixed["sandbox"], "read-only");
        assert_eq!(fixed["mcp_profile"], "minimal");
        let args = launch_args(&fixed);
        let mask = match (sandbox, mcp) {
            (true, true) => "both",
            (true, false) => "sandbox",
            (false, true) => "mcp_profile",
            _ => "none",
        };
        assert!(
            args.windows(2)
                .any(|pair| pair == ["--capability-constraints", mask])
        );
        source
            .as_object_mut()
            .unwrap()
            .remove("fixed_read_only_review");
        let ordinary = retained(&source);
        assert_eq!(
            ordinary["sandbox"],
            if sandbox { "read-only" } else { "inherit" }
        );
        assert_eq!(
            ordinary["mcp_profile"],
            if mcp { "minimal" } else { "inherit" }
        );
        assert_eq!(
            ordinary["explicit_capability_constraints"],
            json!({"sandbox":sandbox,"mcp_profile":mcp})
        );
    }
}

#[test]
fn old_caller_and_ambiguous_standalone_records_keep_constraints_without_fabricating_owner() {
    for source in [
        json!({"sandbox":"workspace-write","mcp_profile":"minimal","scope_source":"caller_constraint","todo_id":1,"launch_key":"old"}),
        json!({"sandbox":"workspace-write","mcp_profile":"minimal","scope_source":"launcher","todo_id":1}),
    ] {
        let selected = retained(&source);
        assert_eq!(selected["sandbox"], "workspace-write");
        assert_eq!(selected["mcp_profile"], "minimal");
        assert_eq!(selected["scope_source"], "caller_constraint");
        assert_eq!(
            selected["explicit_capability_constraints"],
            json!({"sandbox":true,"mcp_profile":true})
        );
    }
}
