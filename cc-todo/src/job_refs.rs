//! Read job references without rewriting recovered evidence or trusting unknown shapes.
use serde_json::Value;

pub fn valid_id(id: &str) -> bool {
    !id.is_empty()
        && id
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}

pub fn ids(value: &Value) -> impl DoubleEndedIterator<Item = &str> {
    value
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .filter(|id| valid_id(id))
}

pub fn unverified_count(value: &Value) -> usize {
    match value.as_array() {
        Some(values) => values.len() - ids(value).count(),
        None => 1,
    }
}
