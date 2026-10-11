//! Durable owner decisions. Presentation and worker identity are never permission identity.
use crate::core::*;
use serde_json::{json, Value};
use std::{collections::BTreeMap, time::Duration};

/// A stable action, concrete resources and the exact permission/specification scope.
/// Human wording (including option letters) belongs to the saved original question.
pub fn normalize(ctx: &Context, value: &Value) -> Result<Value> {
    let todo = n(&value["todo_id"]);
    let action = s(&value["decision"]).trim();
    let resources = value["resources"]
        .as_array()
        .ok_or("decision resources required")?;
    let scope = value["scope"]
        .as_object()
        .ok_or("decision scope required")?;
    if value.get("owner").is_some_and(|v| s(v) != ctx.owner()) {
        return Err("decision owner differs from configured owner".into());
    }
    if todo == 0 || action.is_empty() || resources.is_empty() || scope.is_empty() {
        return Err("判断には元ToDo、具体的な対象、判断の種類、権限範囲が必要です".into());
    }
    if ![
        "archive-and-delete",
        "delete",
        "publish",
        "money",
        "rights",
        "customer",
        "direction",
        "irreversible",
        "access",
    ]
    .contains(&action)
    {
        return Err("decision must use a stable action, never a title or paraphrase".into());
    }
    let mut targets = Vec::new();
    for resource in resources {
        let kind = s(&resource["kind"]).trim();
        let id = s(&resource["id"]).trim();
        if kind.is_empty() || id.is_empty() {
            return Err("invalid decision resource".into());
        }
        let id = if kind == "path" {
            crate::ask_dedup::folder_key(id)
        } else {
            id.to_owned()
        };
        targets.push(json!({"kind":kind,"id":id}));
    }
    targets.sort_by_key(Value::to_string);
    targets.dedup();
    fn canonical(v: &Value) -> Value {
        match v {
            Value::Object(m) => Value::Object(
                m.iter()
                    .map(|(k, v)| (k.clone(), canonical(v)))
                    .collect::<BTreeMap<_, _>>()
                    .into_iter()
                    .collect(),
            ),
            Value::Array(a) => Value::Array(a.iter().map(canonical).collect()),
            _ => v.clone(),
        }
    }
    Ok(canonical(
        &json!({"owner":ctx.owner(),"guild":ctx.cfg["guild_id"],"parent":ctx.cfg["task_channel_id"],
        "todo_id":todo,"decision":action,"resources":targets,"scope":scope}),
    ))
}

/// Result writers can carry the same identity without exposing internal metadata on Discord.
pub fn extract(body: &str) -> Result<(String, Option<Value>)> {
    let marker = "<!-- cc-decision ";
    let Some(start) = body.find(marker) else {
        return Ok((body.into(), None));
    };
    let rest = &body[start + marker.len()..];
    let end = rest
        .find("-->")
        .ok_or("unterminated cc-decision identity")?;
    let value = serde_json::from_str(rest[..end].trim()).map_err(|_| "invalid cc-decision JSON")?;
    let clean = format!("{}{}", &body[..start], &rest[end + 3..]);
    if clean.contains(marker) {
        return Err("multiple decision identities".into());
    }
    Ok((clean.trim().into(), Some(value)))
}

/// Bind once at every input boundary, before overwriting original task context.
pub fn bind(ctx: &Context, context: &mut Value, incoming: Option<&Value>) -> Result<Value> {
    let current = if context["decision_identity"].is_object() {
        Some(normalize(ctx, &context["decision_identity"])?)
    } else {
        None
    };
    let incoming = incoming.map(|v| normalize(ctx, v)).transpose()?;
    if let (Some(left), Some(right)) = (&current, &incoming) {
        if left != right {
            return Err("conflicting decision identities".into());
        }
    }
    let decision = incoming.or(current).unwrap_or(Value::Null);
    if decision.is_object() {
        if n(&context["todo_id"]) > 0 && n(&context["todo_id"]) != n(&decision["todo_id"]) {
            return Err("decision identity and original context ToDo differ".into());
        }
        context["todo_id"] = decision["todo_id"].clone();
        context["decision_identity"] = decision.clone();
    }
    Ok(decision)
}

/// Resolve the original ToDo even when an older launcher did not stamp job.todo_id.
pub fn todo(ctx: &Context, context: &Value) -> Result<u64> {
    let direct = n(&context["todo_id"]);
    let job = s(&context["posting_job"]);
    if job.is_empty() || !ctx.todo.exists() {
        return Ok(direct);
    }
    let state = read_json(&ctx.todo)?;
    let matches: Vec<_> = state["items"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|item| {
            item["jobs"]
                .as_array()
                .is_some_and(|jobs| jobs.iter().any(|j| s(j) == job))
        })
        .collect();
    if matches.len() > 1 {
        // A product parent may legitimately retain multiple original ToDos.
        // An explicit decision can select only one of this worker's records.
        if direct > 0 && matches.iter().any(|item| n(&item["id"]) == direct) {
            return Ok(direct);
        }
        return Err("質問の元ToDoが一意に決まりません".into());
    }
    let inferred = matches.first().map_or(0, |v| n(&v["id"]));
    if direct > 0 && inferred > 0 && direct != inferred {
        return Err("decision original ToDo differs from recorded worker".into());
    }
    Ok(if direct > 0 { direct } else { inferred })
}

pub(crate) fn same_work(
    ctx: &Context,
    record: &Value,
    context: &Value,
    work: &str,
) -> Result<bool> {
    let left = todo(ctx, &record["context"])?;
    let right = todo(ctx, context)?;
    if left > 0 && right > 0 {
        return Ok(left == right);
    }
    if !work.is_empty() && work == s(&record["work_thread_id"]) {
        return Ok(true);
    }
    for field in ["posting_job", "session_id"] {
        if !s(&context[field]).is_empty() && context[field] == record["context"][field] {
            return Ok(true);
        }
    }
    // No original task context: never assume a changed target in the same folder is approved.
    Ok(crate::ask_dedup::folder_key(s(&context["cwd"]))
        == crate::ask_dedup::folder_key(s(&record["context"]["cwd"]))
        && context["project"] == record["context"]["project"])
}

pub(crate) fn path(ctx: &Context, record: &Value) -> Result<std::path::PathBuf> {
    let key = s(&record["key"]);
    if key.len() != 64 || !key.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err("invalid decision route key".into());
    }
    Ok(ctx
        .here
        .join("question-progress")
        .join(format!("{key}.json")))
}

/// Return current owner evidence, including closed/consumed replies, without claiming delivery.
/// Persist before returning so a restart cannot forget the answer or its actual author.
pub(crate) fn reuse(
    ctx: &mut Context,
    records: &[Value],
    primary: &Value,
    uncertain: bool,
) -> Result<Value> {
    let tid = s(&primary["question_thread_id"]);
    let mut out = json!({"thread_id":tid,"after":primary["question"]["starter"],"url":format!("https://discord.com/channels/{}/{tid}",s(&ctx.cfg["guild_id"])),
        "progress_thread_id":primary["progress_thread_id"],"work_thread_id":primary["work_thread_id"],
        "reused":true,"suppressed":if uncertain {"decision_scope_required"} else {"same_decision"},
        "question_body":primary["body"],"question_attachments":primary["files"],"decision_identity":primary["decision_identity"]});
    if uncertain {
        out["next_action"] = json!("元の質問の対象と権限範囲を確認し、同じ判断の記録を使ってください。新しい対象・仕様・危険範囲だけ別の判断として登録できます。審査拒否は再質問の理由になりません。");
        return Ok(out);
    }
    let owner = ctx.owner();
    let mut all = BTreeMap::new();
    let mut snapshots = Vec::new();
    for original in records {
        let id = s(&original["question_thread_id"]);
        if id.is_empty() {
            continue;
        }
        // Malformed/failed history is an error, never an empty "unanswered" snapshot.
        let mut history = Vec::new();
        let mut before = String::new();
        loop {
            let suffix = if before.is_empty() {
                String::new()
            } else {
                format!("&before={before}")
            };
            let page = ctx.call(
                "GET",
                &format!("/channels/{id}/messages?limit=100{suffix}"),
                None,
            )?;
            let page = page.as_array().ok_or("invalid decision reply history")?;
            history.extend(page.iter().cloned());
            if page.len() < 100 {
                break;
            }
            let oldest = page.iter().map(|m| n(&m["id"])).min().unwrap_or(0);
            if oldest == 0 || (!before.is_empty() && oldest >= before.parse().unwrap_or(0)) {
                return Err("decision history did not advance".into());
            }
            before = oldest.to_string();
        }
        let mut record = original.clone();
        let answers: Vec<_> = history.iter().filter(|m| s(&m["author"]["id"])==owner
            && !truth(&m["author"]["bot"]) && n(&m["id"])>n(&record["question"]["starter"])
            && !crate::snooze::control_only(m)).map(|m|json!({"id":m["id"],"thread_id":id,
                "author_id":m["author"]["id"],"timestamp":m["timestamp"],"content":m["content"],"attachments":m["attachments"]})).collect();
        for answer in &answers {
            all.insert(n(&answer["id"]), answer.clone());
        }
        let mut archive = record["owner_answer_history"]
            .as_array()
            .cloned()
            .unwrap_or_default();
        for answer in record["owner_answers"]
            .as_array()
            .into_iter()
            .flatten()
            .chain(answers.iter())
        {
            if !archive.contains(answer) {
                archive.push(answer.clone());
            }
        }
        record["owner_answer_history"] = json!(archive);
        record["owner_answers"] = json!(answers);
        record["answers_checked_at"] = json!(ctx.now());
        snapshots.push(record);
    }
    for record in snapshots {
        atomic_json(&path(ctx, &record)?, &record, 2)?;
    }
    out["owner_answers"] = json!(all.values().collect::<Vec<_>>());
    if let Some((_, latest)) = all.last_key_value() {
        out["suppressed"] = json!("already_answered");
        out["latest_owner_reply"] = latest.clone();
    }
    Ok(out)
}

/// Explicit bounded repair of reviewed duplicate threads. Never posts, launches or deletes replies.
pub fn lookup(ctx: &mut Context, value: &Value) -> Result<Value> {
    let _guard = ByteLock::acquire(
        &ctx.here.join("question-progress.lock"),
        Duration::from_secs(60),
    )?;
    let identity = normalize(ctx, value)?;
    let records: Vec<_> = crate::question_progress::routes(ctx)?
        .into_iter()
        .filter(|r| r["decision_identity"] == identity && truth(&r["complete"]))
        .collect();
    match records.first() {
        Some(primary) => reuse(ctx, &records, primary, false),
        None => Ok(json!({"found":false,"decision_identity":identity})),
    }
}

/// Explicit bounded repair of reviewed duplicate threads. Never posts, launches or deletes replies.
pub fn reconcile(ctx: &mut Context, manifest: &Value) -> Result<Value> {
    let _guard = ByteLock::acquire(
        &ctx.here.join("question-progress.lock"),
        Duration::from_secs(60),
    )?;
    let identity = normalize(ctx, &manifest["identity"])?;
    let ids = manifest["threads"]
        .as_array()
        .ok_or("explicit threads required")?;
    if ids.is_empty() || ids.len() > 10 || s(&manifest["evidence"]).trim().is_empty() {
        return Err("bounded reviewed evidence required".into());
    }
    let routes = crate::question_progress::routes(ctx)?;
    let mut selected = Vec::new();
    for id in ids {
        let mut record = routes
            .iter()
            .find(|r| r["question_thread_id"] == *id)
            .cloned()
            .ok_or("unregistered decision thread")?;
        if !truth(&record["complete"]) {
            return Err("cannot reconcile incomplete question".into());
        }
        if !record["decision_identity"].is_null() && record["decision_identity"] != identity {
            return Err("decision scope already differs".into());
        }
        // Validate that the recorded original belongs to our bot and configured server.
        let channel = ctx.call("GET", &format!("/channels/{}", s(id)), None)?;
        if channel["guild_id"] != ctx.cfg["guild_id"]
            || channel["parent_id"] != ctx.cfg["task_channel_id"]
        {
            return Err("foreign decision thread".into());
        }
        let starter = ctx.call(
            "GET",
            &format!(
                "/channels/{}/messages/{}",
                s(id),
                s(&record["question"]["starter"])
            ),
            None,
        )?;
        if starter["author"]["id"] != record["sender"]
            || starter["content"] != record["question"]["payload"]["content"]
        {
            return Err("original decision changed".into());
        }
        record["decision_identity"] = identity.clone();
        record["context"]["todo_id"] = identity["todo_id"].clone();
        record["decision_reconciliation"] =
            json!({"evidence":manifest["evidence"],"threads":ids,"at":ctx.now()});
        selected.push(record);
    }
    selected.sort_by_key(|r| n(&r["question_thread_id"]));
    let result = reuse(ctx, &selected, &selected[0], false)?;
    // reuse saved every exact identity and answer snapshot; no global history migration.
    Ok(result)
}
