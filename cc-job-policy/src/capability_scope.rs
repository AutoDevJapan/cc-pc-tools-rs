//! Capability provenance shared by ordinary launch and recovery entry points.
use serde_json::Value;

pub fn owner(value: &Value) -> bool {
    matches!(
        value["scope_source"].as_str(),
        Some("owner_request" | "owner_marker")
    )
}

pub fn constrained(value: &Value, field: &str) -> bool {
    owner(value)
        || value["scope_source"] == "caller_constraint"
        || value["explicit_capability_constraints"][field] == true
        || value["legacy_capability_constraints"][field] == true
}

/// Retain an unknown standalone restriction without claiming owner authorship.
pub fn legacy(value: &Value, field: &str) -> bool {
    value["explicit_capability_constraints"].is_null()
        && value["scope_source"] != "policy"
        && value["launch_key"].as_str().is_none_or(str::is_empty)
        && match field {
            "sandbox" => matches!(value[field].as_str(), Some("read-only" | "workspace-write")),
            "mcp_profile" => matches!(value[field].as_str(), Some("minimal" | "browser")),
            _ => false,
        }
}

/// These records must still pass their own execution/requalification gates.
/// Recognizing their scope never authorizes an operation or bypasses a gate.
pub fn fixed(value: &Value) -> bool {
    value["capability_contract"] == true
        || crate::launch_context::fixed_execution_contract(value)
        || [
            "initial_review",
            "unstarted_review",
            "finite_research_approval",
            "quota_recovery",
        ]
        .iter()
        .any(|field| value[*field].is_object())
        || value["subwork_continuation"]["scope_sha256"].is_string()
}

pub fn inherited<'a>(value: &'a Value, field: &str) -> Option<&'a str> {
    value[field]
        .as_str()
        .filter(|text| !text.is_empty())
        .filter(|_| fixed(value) || constrained(value, field) || legacy(value, field))
}

pub fn origin(value: &Value) -> &'static str {
    match value["scope_source"].as_str() {
        Some("owner_request") => "owner_request",
        Some("owner_marker") => "owner_marker",
        _ => "caller_constraint",
    }
}

/// Serialize a resolved launch tuple together so relaunchers cannot drop its
/// provenance. Values remain subject to the regular launcher's validation.
pub fn launch_args(scope: &Value) -> Vec<String> {
    [
        ("--sandbox", "sandbox", "inherit"),
        ("--mcp-profile", "mcp_profile", "inherit"),
        ("--scope-source", "scope_source", "launcher"),
    ]
    .into_iter()
    .flat_map(|(flag, field, default)| {
        [
            flag.to_owned(),
            scope[field]
                .as_str()
                .filter(|value| !value.is_empty())
                .unwrap_or(default)
                .to_owned(),
        ]
    })
    .collect()
}
