//! Capability choices for delegation, independent of the routed role.
use super::{invalid, Result};
use cc_ask::job_policy::capability_scope::{
    constrained, fixed as contract, legacy, origin, retained,
};
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
    job["capability_provenance_version"] = if raw["capability_provenance_version"] == 1 {
        json!(1)
    } else {
        Value::Null
    };
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
    !value[field].is_null()
        && (constrained(value, field)
            || (value["capability_provenance_version"] != 1 && value["scope_source"].is_null()))
}

pub(super) fn read_only(requested: &mut Value) {
    let mcp = current(requested, "mcp_profile");
    requested["sandbox"] = json!("read-only");
    requested["scope_source"] = json!("owner_marker");
    requested["capability_provenance_version"] = json!(1);
    requested["explicit_capability_constraints"] = json!({"sandbox":true,"mcp_profile":mcp});
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
    let fixed_scope = fixed.map(retained);
    let mut scope_source = "launcher";
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
            // Matching explicit inputs establish only that field's durable
            // constraint; the other fixed field remains temporary.
            explicit.or_else(|| (!source[field].is_null()).then_some(source))
        } else {
            explicit.or_else(|| {
                inherited.filter(|source| {
                    valid(field, &source[field])
                        && (constrained(source, field) || legacy(source, field))
                })
            })
        };
        options[field] = chosen.map_or_else(|| json!("inherit"), |value| value[field].clone());
        let durable = chosen.is_some_and(|value| {
            if inherited.is_some_and(|source| std::ptr::eq(source, value)) {
                fixed_scope.as_ref().map_or_else(
                    || constrained(value, field) || legacy(value, field),
                    |scope| scope["explicit_capability_constraints"][field] == true,
                )
            } else {
                true
            }
        });
        options["explicit_capability_constraints"][field] = json!(durable);
        if let Some(value) = chosen.filter(|_| durable) {
            let source = if fixed.is_some_and(|source| std::ptr::eq(source, value)) {
                fixed_scope.as_ref().unwrap()["scope_source"]
                    .as_str()
                    .unwrap()
            } else {
                origin(value)
            };
            if scope_source == "launcher" || matches!(source, "owner_request" | "owner_marker") {
                scope_source = source;
            }
        }
        options["legacy_capability_constraints"][field] = json!(chosen.is_some_and(|value| {
            value["legacy_capability_constraints"][field] == true
                || inherited
                    .is_some_and(|source| std::ptr::eq(source, value) && legacy(value, field))
        }));
    }
    options["scope_source"] = json!(scope_source);
    options["capability_provenance_version"] = json!(1);
    Ok(())
}
