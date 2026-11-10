//! Native control transitions of an already answered question, not actor rights.
//! Both callers hold todo.lock; no answer, owner, generation or hold is changed here.
use serde_json::{json, Value};

pub const RECORDS: &str = "owner_question_continuation_receipts";

pub fn metadata(row: &Value, root: &Value) -> Value {
    json!({"id":row["id"],"project":row["project"],"root":root,
        "title":row["title"],"created_s":row["created_s"],"spec":row["spec"]})
}

pub fn control(row: &Value) -> Value {
    json!({"status":row["status"],"resume_generation":row["resume_generation"],
        "worker_receipt":row["worker_receipt"],"jobs":row["jobs"],
        "scheduler":{"job":row["scheduler"]["job"],"key":row["scheduler"]["key"],
            "last_job":row["scheduler"]["last_job"],"phase":row["scheduler"]["phase"],
            "resume":row["scheduler"]["resume"]}})
}

/// This native ledger is useful only with the original signed purpose, canonical
/// answer/preserved delivery, terminal owner and independently checked successor job.
/// It cannot establish an owner request or grant access to the registered root.
pub fn record(before: &Value, row: &mut Value, root: &Value, operation: &str) {
    let (from, to) = (control(before), control(row));
    let scope = metadata(before, root);
    if from == to || scope != metadata(row, root) {
        return;
    }
    let Some(answers) = before["owner_question_answer_receipts"].as_array() else {
        return;
    };
    let mut records = match row.get(RECORDS) {
        None | Some(Value::Null) => Vec::new(),
        Some(Value::Array(v)) => v.clone(),
        _ => return, // Never replace a malformed ledger or rewrite old evidence.
    };
    for answer in answers {
        if records.len() >= 256 {
            return; // Bounded evidence; missing continuity fails closed at consumption.
        }
        if answer["domain"] != "todo-owner-question-answer-receipt-v1"
            || !answer["answer_file_sha256"].is_string()
        {
            continue;
        }
        records.push(json!({"domain":"todo-owner-question-continuation-v1",
            "request_id":answer["answer"]["request_id"],
            "question_receipt_sha256":answer["answer"]["question_receipt_sha256"],
            "answer_file_sha256":answer["answer_file_sha256"],
            "metadata":scope,"operation":operation,"before":from,"after":to,
            "recorded_at":chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Micros,true)}));
    }
    if !records.is_empty() {
        row[RECORDS] = json!(records);
    }
}
