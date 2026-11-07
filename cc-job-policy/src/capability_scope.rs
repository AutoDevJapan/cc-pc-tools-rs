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

/// Recover missing launch fields from the same job's saved selection. Existing
/// source fields and their provenance win; a partial legacy source must not
/// acquire another field's lifetime when the saved selection uses v1 masks.
pub fn restore_missing(source: &mut Value, saved: &Value) {
    let present =
        |value: &Value, field: &str| value[field].as_str().is_some_and(|text| !text.is_empty());
    if ["sandbox", "mcp_profile"]
        .iter()
        .all(|field| present(source, field))
    {
        return;
    }
    let original = source.clone();
    let mut scope_source = if fixed(&original) {
        original["scope_source"].as_str().unwrap_or("launcher")
    } else {
        "launcher"
    };
    for field in ["sandbox", "mcp_profile"] {
        let chosen = if present(&original, field) {
            &original
        } else {
            saved
        };
        source[field] = chosen[field]
            .as_str()
            .filter(|text| !text.is_empty())
            .map_or_else(
                || serde_json::json!("inherit"),
                |text| serde_json::json!(text),
            );
        let durable = present(chosen, field)
            && (constrained(chosen, field) || (!fixed(chosen) && legacy(chosen, field)));
        source["explicit_capability_constraints"][field] = serde_json::json!(durable);
        if durable && (scope_source == "launcher" || owner(chosen)) {
            scope_source = origin(chosen);
        }
    }
    source["capability_provenance_version"] = serde_json::json!(1);
    source["scope_source"] = serde_json::json!(scope_source);
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
