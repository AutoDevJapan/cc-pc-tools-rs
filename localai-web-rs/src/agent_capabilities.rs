//! Capability choices for delegation, independent of the routed role.
use super::{invalid, Result};
use cc_ask::job_policy::capability_scope::{constrained, fixed as contract, legacy};
use serde_json::{json, Value};

const FIELDS: [&str; 2] = ["sandbox", "mcp_profile"];

fn valid(field: &str, value: &Value) -> bool {
    match field {
        "sandbox" => matches!(
            value.as_str(),
            Some("inherit" | "read-only" | "workspace-write" | "danger-full-access")
        ),
        _ => matches!(
            value.as_str(),
            Some("inherit" | "minimal" | "browser" | "full")
        ),
    }
}

pub(super) fn validate(value: &Value) -> Result<()> {
    if FIELDS
        .iter()
        .any(|field| !value[*field].is_null() && !valid(field, &value[*field]))
    {
        return Err(invalid());
    }
    Ok(())
}

/// The runtime needs provenance, not the private contents of contract receipts.
pub(super) fn project(job: &mut Value, raw: &Value) {
    job["mcp_profile"] = if valid("mcp_profile", &raw["mcp_profile"]) {
        raw["mcp_profile"].clone()
    } else {
        Value::Null
    };
    job["scope_source"] = match raw["scope_source"].as_str() {
        Some("launcher" | "owner_request" | "owner_marker" | "caller_constraint" | "policy") => {
            raw["scope_source"].clone()
        }
        _ => Value::Null,
    };
    for field in [
        "explicit_capability_constraints",
        "legacy_capability_constraints",
    ] {
        job[field] = if raw[field].is_object() {
            json!({"sandbox":raw[field]["sandbox"] == true, "mcp_profile":raw[field]["mcp_profile"] == true})
        } else {
            Value::Null
        };
    }
    job["capability_contract"] = json!(contract(raw));
}

fn current(value: &Value, field: &str) -> bool {
    !value[field].is_null() && (value["scope_source"].is_null() || constrained(value, field))
}

pub(super) fn apply(
    item: Option<&Value>,
    requested: &Value,
    source: Option<&Value>,
    inherit_scope: bool,
    options: &mut Value,
) -> Result<()> {
    validate(requested)?;
    if let Some(item) = item {
        validate(item)?;
    }
    let inherited = source.filter(|_| inherit_scope);
    let fixed = inherited.filter(|source| contract(source));
    let mut origin = "launcher";
    for field in FIELDS {
        let changed = !requested["_todo_defaults"].is_object()
            || item.is_some_and(|item| item[field] != requested["_todo_defaults"][field]);
        let local = item.filter(|item| current(item, field));
        let explicit = if requested["scope_source"] == "owner_marker" && current(requested, field) {
            Some(requested)
        } else {
            local
                .filter(|_| changed)
                .or_else(|| current(requested, field).then_some(requested))
                .or(local)
        };
        let chosen = if let Some(source) = fixed {
            validate(source)?;
            // A finite contract must be requalified by its own gate before a
            // later delegate input can replace the exact capability tuple.
            if explicit
                .is_some_and(|value| !source[field].is_null() && value[field] != source[field])
            {
                return Err(invalid());
            }
            (!source[field].is_null()).then_some(source).or(explicit)
        } else {
            explicit.or_else(|| {
                inherited.filter(|source| {
                    valid(field, &source[field])
                        && (constrained(source, field) || legacy(source, field))
                })
            })
        };
        options[field] = chosen.map_or_else(|| json!("inherit"), |value| value[field].clone());
        if let Some(value) = chosen {
            let source = match value["scope_source"].as_str() {
                Some("owner_request") => "owner_request",
                Some("owner_marker") => "owner_marker",
                _ => "caller_constraint",
            };
            if origin == "launcher" || matches!(source, "owner_request" | "owner_marker") {
                origin = source;
            }
        }
        options["legacy_capability_constraints"][field] = json!(chosen.is_some_and(|value| {
            value["legacy_capability_constraints"][field] == true
                || inherited
                    .is_some_and(|source| std::ptr::eq(source, value) && legacy(value, field))
        }));
    }
    options["scope_source"] = json!(origin);
    Ok(())
}
