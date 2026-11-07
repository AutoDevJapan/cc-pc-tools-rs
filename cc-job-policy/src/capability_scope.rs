//! Capability provenance shared by ordinary launch and recovery entry points.
use serde_json::Value;

pub fn owner(value: &Value) -> bool {
    matches!(
        value["scope_source"].as_str(),
        Some("owner_request" | "owner_marker")
    )
}

pub fn constrained(value: &Value, field: &str) -> bool {
    if value["capability_provenance_version"] == 1 {
        return value["explicit_capability_constraints"][field] == true;
    }
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

/// Resolve the retained tuple before replacing a source's launch identity.
/// Per-field provenance must not let an unrelated old hint become a constraint.
pub fn retained(value: &Value) -> Value {
    let sandbox = inherited(value, "sandbox");
    let mcp = inherited(value, "mcp_profile");
    let origin = if sandbox.is_some() || mcp.is_some() {
        if fixed(value) {
            value["scope_source"].as_str().unwrap_or("launcher")
        } else {
            origin(value)
        }
    } else {
        "launcher"
    };
    serde_json::json!({"sandbox":sandbox.unwrap_or("inherit"),
        "mcp_profile":mcp.unwrap_or("inherit"),"scope_source":origin,
        "capability_provenance_version":1,
        "explicit_capability_constraints":{
            "sandbox":constrained(value,"sandbox") || (!fixed(value) && legacy(value,"sandbox")),
            "mcp_profile":constrained(value,"mcp_profile") || (!fixed(value) && legacy(value,"mcp_profile"))}})
}

/// Serialize a resolved launch tuple together so relaunchers cannot drop its
/// provenance. Values remain subject to the regular launcher's validation.
pub fn launch_args(scope: &Value) -> Vec<String> {
    let mut args: Vec<String> = [
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
    .collect();
    if scope["capability_provenance_version"] == 1 {
        let mask = match (
            scope["explicit_capability_constraints"]["sandbox"] == true,
            scope["explicit_capability_constraints"]["mcp_profile"] == true,
        ) {
            (true, true) => "both",
            (true, false) => "sandbox",
            (false, true) => "mcp_profile",
            _ => "none",
        };
        args.extend(["--capability-constraints".into(), mask.into()]);
    }
    args
}
