//! Bind a normal prepared prompt to its pending owner input, not actor authority.
use serde_json::{Value, json};

pub fn digest(value: &Value) -> String {
    fn number(text: &str) -> String {
        let (sign, rest) = text.strip_prefix('-').map_or(("", text), |s| ("-", s));
        let (mantissa, exponent) = rest
            .split_once(['e', 'E'])
            .map_or((rest, 0), |(a, b)| (a, b.parse::<i64>().unwrap()));
        let fraction = mantissa.split_once('.').map_or(0, |(_, s)| s.len());
        let digits = mantissa.replace('.', "");
        let digits = digits.trim_start_matches('0');
        if digits.is_empty() {
            return "0".into();
        }
        let coefficient = digits.trim_end_matches('0');
        let exponent = exponent - fraction as i64 + (digits.len() - coefficient.len()) as i64;
        if exponent == 0 {
            format!("{sign}{coefficient}")
        } else {
            format!("{sign}{coefficient}e{exponent}")
        }
    }
    fn write(v: &Value, out: &mut String) {
        match v {
            Value::Object(m) => {
                let mut keys: Vec<_> = m.keys().collect();
                keys.sort();
                out.push('{');
                for (index, key) in keys.into_iter().enumerate() {
                    if index > 0 {
                        out.push(',');
                    }
                    out.push_str(&serde_json::to_string(key).unwrap());
                    out.push(':');
                    write(&m[key], out);
                }
                out.push('}');
            }
            Value::Array(a) => {
                out.push('[');
                for (index, v) in a.iter().enumerate() {
                    if index > 0 {
                        out.push(',');
                    }
                    write(v, out);
                }
                out.push(']');
            }
            Value::Number(n) => out.push_str(&number(&n.to_string())),
            v => out.push_str(&serde_json::to_string(v).unwrap()),
        }
    }
    // JSON number values share an exact decimal coefficient/exponent spelling;
    // no float conversion or integer rounding is performed by this encoder.
    let mut encoded = String::new();
    write(value, &mut encoded);
    let encoded = encoded
        .replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029");
    crate::product_retry::digest(encoded.as_bytes())
}
pub fn version(row: &Value) -> String {
    digest(&json!({"instruction":row["scheduler"]["owner_instruction"],
        "answers":row["owner_question_answer_receipts"],"deliveries":row["owner_question_delivery_receipts"]}))
}
fn scope(row: &Value, root: &Value) -> String {
    digest(&json!({"id":row["id"],"project":row["project"],"root":root,
        "spec":row["spec"],"title":row["title"],"created_s":row["created_s"]}))
}
pub fn receipt(row: &Value, root: &Value, prompt: &[u8]) -> Value {
    json!({"domain":"todo-prepared-input-v1","todo_id":row["id"],"project":row["project"],
        "launch_key":row["scheduler"]["key"],"scope_sha256":scope(row,root),
        "input_sha256":version(row),"source_prompt_sha256":crate::product_retry::digest(prompt)})
}
fn hash(v: &Value) -> bool {
    v.as_str().is_some_and(|s| {
        s.len() == 64
            && s.bytes()
                .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
    })
}
/// Called while holding the canonical ToDo lock at registration and running
/// publication. Later edits are pending input, not injections into a live worker.
pub fn validate(db: &Value, job: &Value) -> Result<(), &'static str> {
    let proof = &job["prepared_input"];
    let row = db["items"]
        .as_array()
        .and_then(|rows| rows.iter().find(|r| r["id"] == job["todo_id"]))
        .ok_or("prepared_input_task_missing")?;
    if proof["domain"] != "todo-prepared-input-v1"
        || proof["todo_id"] != row["id"]
        || proof["project"] != row["project"]
        || job["project"] != row["project"]
        || proof["launch_key"] != job["launch_key"]
        || row["scheduler"]["key"] != job["launch_key"]
        || !["input_sha256", "scope_sha256", "source_prompt_sha256"]
            .iter()
            .all(|k| hash(&proof[*k]))
        || row["scheduler"]["prepared_input"] != *proof
        || row["scheduler"]["job"]
            .as_str()
            .is_some_and(|id| !id.is_empty() && job["id"] != id)
    {
        return Err("prepared_input_receipt_changed");
    }
    if proof["scope_sha256"] != scope(row, &db["projects"][row["project"].as_str().unwrap_or("")]) {
        return Err("prepared_input_scope_changed");
    }
    if !matches!(row["status"].as_str(), Some("todo" | "doing"))
        || crate::cancellation::owner_stopped(row, job)
        || ["owner_question_wait", "decision_wait", "wait_ready"]
            .iter()
            .any(|k| !row[*k].is_null())
        || [
            "paused",
            "owner_stopped",
            "stopped",
            "owner_postponement_recorded",
            "cancel_requested",
        ]
        .iter()
        .any(|k| row[*k] == true)
        || ["owner_stop_job", "pending_cancel", "question_close_pending"]
            .iter()
            .any(|k| {
                let v = &row["scheduler"][*k];
                !v.is_null() && *v != false && *v != ""
            })
        || row["stop_kind"].as_str().is_some_and(|s| !s.is_empty())
        || !crate::todo_wait::job_pending_on(db, job).is_empty()
    {
        return Err("prepared_input_hold");
    }
    if proof["input_sha256"] != version(row) {
        return Err("prepared_input_changed");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn canonical_scope_matches_go_without_html_escaping() {
        assert_eq!(
            digest(&json!({"b":2,"a":"<&>\u{2028}\u{2029}"})),
            crate::product_retry::digest(br#"{"a":"<&>\u2028\u2029","b":2}"#)
        );
    }
    #[test]
    fn canonical_numbers_keep_exact_decimal_values_and_scalar_types() {
        for (raw, canonical) in [
            ("1700000000", "17e8"),
            ("1700000000.0", "17e8"),
            ("1.7e9", "17e8"),
            ("1700000000.125", "1700000000125e-3"),
            ("0.00012345", "12345e-8"),
            ("-0.0", "0"),
            ("18446744073709551615", "18446744073709551615"),
            ("null", "null"),
            ("\"1700000000\"", "\"1700000000\""),
        ] {
            let value: Value = serde_json::from_str(raw).unwrap();
            assert_eq!(
                digest(&value),
                crate::product_retry::digest(canonical.as_bytes()),
                "{raw}"
            );
        }
        assert_ne!(
            digest(&json!(9007199254740993u64)),
            digest(&json!(9007199254740992u64))
        );
        assert_ne!(digest(&Value::Null), digest(&json!("null")));
    }
}
