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
    if let Some(reviewed) = incoming.and_then(|v| v.get("reviewed_owner_context")) {
        context["reviewed_owner_context"] = reviewed.clone();
    }
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
    original_todo(ctx, context)?.unique()
}

enum OriginalTodo {
    Known(u64),
    Ambiguous(Vec<u64>),
}

impl OriginalTodo {
    fn unique(self) -> Result<u64> {
        match self {
            Self::Known(id) => Ok(id),
            Self::Ambiguous(_) => Err("質問の元ToDoが一意に決まりません".into()),
        }
    }
}

fn original_todo(ctx: &Context, context: &Value) -> Result<OriginalTodo> {
    let direct = n(&context["todo_id"]);
    let job = s(&context["posting_job"]);
    if job.is_empty() || !ctx.todo.exists() {
        return Ok(OriginalTodo::Known(direct));
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
            return Ok(OriginalTodo::Known(direct));
        }
        if direct > 0 {
            return Err("decision original ToDo differs from recorded worker".into());
        }
        let mut candidates: Vec<_> = matches.iter().map(|item| n(&item["id"])).collect();
        // A saved question can retain original/related task evidence no longer
        // present in the live jobs index. That evidence must not be discarded
        // when proving disjointness. Invalid entries keep the set uncertain.
        for related in [
            &context["related_todo_ids"],
            &context["job_info"]["related_todo_ids"],
        ] {
            match related {
                Value::Null => {}
                Value::Array(ids) => candidates.extend(ids.iter().map(n)),
                _ => candidates.push(0),
            }
        }
        let saved = &context["job_info"]["todo_id"];
        if !saved.is_null() {
            candidates.push(n(saved));
        }
        if !context["job_info"].is_null() && !context["job_info"].is_object() {
            candidates.push(0);
        }
        return Ok(OriginalTodo::Ambiguous(candidates));
    }
    let inferred = matches.first().map_or(0, |v| n(&v["id"]));
    if direct > 0 && inferred > 0 && direct != inferred {
        return Err("decision original ToDo differs from recorded worker".into());
    }
    Ok(OriginalTodo::Known(if direct > 0 {
        direct
    } else {
        inferred
    }))
}

pub(crate) fn same_work(
    ctx: &Context,
    record: &Value,
    context: &Value,
    work: &str,
) -> Result<bool> {
    let right = todo(ctx, context)?;
    let original = original_todo(ctx, &record["context"])?;
    if let OriginalTodo::Ambiguous(ids) = &original {
        // Exclude only a complete, non-overlapping candidate set. Never select
        // one legacy original, infer an approval or ignore conflicting evidence.
        if right > 0 && ids.iter().all(|id| *id > 0 && *id != right) {
            return Ok(false);
        }
    }
    let left = original.unique()?;
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
    out["approval_matches_requested_scope"] = json!(!uncertain);
    out["delivery"] = json!("inspection_only");
    if uncertain {
        out["next_action"] = json!("元の質問と本人回答を読み、元ToDoの指示を反映してください。この回答を別の対象の承認に読み替えないでください。AIが選べる技術的詳細は自分で決めて進めてください。審査拒否は再質問の理由になりません。");
    }
    let owner = ctx.owner();
    let mut all = BTreeMap::new();
    let mut snapshots = Vec::new();
    let mut questions = Vec::new();
    for original in records {
        let id = s(&original["question_thread_id"]);
        if id.is_empty() {
            continue;
        }
        let channel = ctx.call("GET", &format!("/channels/{id}"), None)?;
        if channel["guild_id"] != ctx.cfg["guild_id"]
            || channel["parent_id"] != ctx.cfg["task_channel_id"]
            || channel["owner_id"] != original["sender"]
        {
            return Err("decision source outside configured bot forum".into());
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
        let starter = if truth(&record["forum"]) {
            history
                .iter()
                .find(|m| m["id"] == record["question"]["starter"])
                .cloned()
                .ok_or("original decision message unavailable")?
        } else {
            ctx.call(
                "GET",
                &format!(
                    "/channels/{}/messages/{}",
                    s(&record["parent"]),
                    s(&record["question"]["starter"])
                ),
                None,
            )?
        };
        if starter["author"]["id"] != record["sender"]
            || starter["content"] != record["question"]["payload"]["content"]
        {
            return Err("original decision changed".into());
        }
        let answers: Vec<_> = history.iter().filter(|m| s(&m["author"]["id"])==owner
            && !truth(&m["author"]["bot"]) && n(&m["id"])>n(&record["question"]["starter"])
            && !crate::snooze::control_only(m)).map(|m|json!({"id":m["id"],"thread_id":id,
                "author_id":m["author"]["id"],"timestamp":m["timestamp"],"edited_timestamp":m["edited_timestamp"],
                "acquisition_source":"discord-api-v10","content":m["content"],"attachments":m["attachments"]})).collect();
        questions.push(
            json!({"thread_id":id,"question_message_id":record["question"]["starter"],
            "todo_id":record["context"]["todo_id"],"question_body":record["body"],
            "decision_identity":record["decision_identity"],"owner_answers":answers}),
        );
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
    out["prior_questions"] = json!(questions);
    if let Some((_, latest)) = all.last_key_value() {
        if !uncertain {
            out["suppressed"] = json!("already_answered");
        }
        out["latest_owner_reply"] = latest.clone();
    }
    // Preserve original evidence privately, but never emit credentials or client names.
    redact(ctx, &out)
}

pub(crate) fn redact(ctx: &Context, out: &Value) -> Result<Value> {
    let names = crate::project::ProjectNames::load(&ctx.client_projects)?;
    serde_json::from_str(&names.redact(&safe_text(out.to_string())))
        .map_err(|_| "invalid redacted decision output".into())
}

/// Keep task instructions and independently verified sibling answers across sessions.
/// Old receipt flags are deliberately absent: this is context, not completion evidence.
pub fn task_handoff(ctx: &mut Context, context: &Value) -> Result<String> {
    let mut context = context.clone();
    if s(&context["posting_job"]).is_empty() && valid_job(s(&context["id"])) {
        context["posting_job"] = context["id"].clone();
    }
    let id = todo(ctx, &context)?;
    if id == 0 {
        return Ok(String::new());
    }
    let db = if ctx.todo.exists() {
        read_json(&ctx.todo)?
    } else {
        Value::Null
    };
    let item = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|v| n(&v["id"]) == id);
    let _guard = ByteLock::acquire(
        &ctx.here.join("question-progress.lock"),
        Duration::from_secs(5),
    )?;
    let mut records: Vec<_> = crate::question_progress::routes(ctx)?
        .into_iter()
        .filter(|r| n(&r["context"]["todo_id"]) == id && truth(&r["complete"]))
        .filter(|r| {
            r["decision_identity"]["owner"]
                .as_str()
                .is_none_or(|owner| owner == ctx.owner())
        })
        .collect();
    records.reverse();
    let older_question_ids: Vec<_> = records
        .iter()
        .skip(10)
        .map(|r| r["question_thread_id"].clone())
        .collect();
    records.truncate(10);
    let questions = if let Some(first) = records.first() {
        // Cached provenance is an index, never evidence of the current message text.
        // reuse validates the guild, parent, bot starter and fetches actual owner replies.
        reuse(ctx, &records, first, true)?["prior_questions"].clone()
    } else {
        json!([])
    };
    let evidence = redact(
        ctx,
        &json!({"todo_id":id,"saved_instruction":item.map(|v|json!({"spec":v["spec"],"note":v["note"]})),
        "questions":questions,"older_question_ids_require_lookup":older_question_ids,"delivery":"context_only"}),
    )?;
    let full = format!("元ToDoの保存済み指示と本人回答（回答は対応する元質問の範囲で読む。AIが選べる技術的詳細は自分で決め、同じ判断を再質問しない。受領や配送だけで実反映済みにしない）:\n{evidence}\n");
    crate::prompt_context::owner_context(ctx, &format!("todo-{id}"), &full, 8 * 1024)
}

/// Append verified input to the original ToDo's existing spec before any worker wait.
/// Keep the ToDo state, ownership, dependencies and progress unchanged.
pub fn persist_task_reply(ctx: &Context, question: &Value, message: &Value) -> Result<bool> {
    if n(&question["todo_id"]) == 0
        || s(&message["author"]["id"]) != ctx.owner()
        || truth(&message["author"]["bot"])
        || n(&message["id"]) <= n(&question["after"])
    {
        return Ok(false);
    }
    let home = ctx.todo.parent().ok_or("ToDo home unavailable")?;
    let _guard = ByteLock::acquire(&home.join("todo.lock"), Duration::from_secs(5))?;
    let db = read_json(&ctx.todo)?;
    let Some(item) = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|v| v["id"] == question["todo_id"])
    else {
        return Ok(false);
    };
    let path = std::path::PathBuf::from(s(&item["spec"]));
    if path.as_os_str().is_empty() {
        return Ok(false);
    }
    let path = path
        .canonicalize()
        .map_err(|_| "original ToDo spec unavailable")?;
    if path.parent()
        != Some(
            home.join("specs")
                .canonicalize()
                .map_err(|_| "ToDo specs unavailable")?
                .as_path(),
        )
    {
        return Err("original ToDo spec outside managed specs".into());
    }
    let evidence = redact(
        ctx,
        &json!({"todo_id":question["todo_id"],"thread_id":question["thread_id"],
        "question_message_id":question["after"],"question_body":question["question_body"],
        "decision_identity":question["decision_identity"],"author_id":message["author"]["id"],
        "message_id":message["id"],"timestamp":message["timestamp"],"edited_timestamp":message["edited_timestamp"],
        "acquisition_source":"discord-api-v10","content":message["content"],"attachments":message["attachments"],
        "delivery":"saved_to_original_todo_spec","application":"unverified"}),
    )?;
    let marker = format!(
        "<!-- cc-owner-answer {} -->",
        crate::automation::key(&evidence.to_string())
    );
    let original = read_text(&path)?;
    if original.contains(&marker) {
        return Ok(true);
    }
    let text = format!("{original}\n\n{marker}\n元ToDoへ届いた本人のDiscord回答です。対応する元質問の範囲で反映し、AIが選べる技術的詳細は自分で決めてください。同じ判断を再質問しないでください。配送は実操作の成功を証明しません。\n{evidence}\n");
    let temp = path.with_file_name(format!(
        "owner-answer-{}.tmp",
        uuid::Uuid::new_v4().simple()
    ));
    use std::io::Write;
    let mut file = std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&temp)
        .map_err(|_| "cannot stage owner task context")?;
    file.write_all(text.as_bytes())
        .and_then(|_| file.sync_all())
        .map_err(|_| "cannot stage owner task context")?;
    drop(file);
    replace_file(&temp, &path)?;
    Ok(true)
}

/// A different scope still needs the original task's answers before asking again.
/// The digest acknowledges inspection, never grants approval for the new scope.
pub(crate) fn preflight(
    ctx: &mut Context,
    records: &[Value],
    context: &Value,
    work: &str,
) -> Result<Option<Value>> {
    let mut prior = Vec::new();
    for record in records.iter().filter(|r| truth(&r["complete"])) {
        if record["decision_identity"]["owner"]
            .as_str()
            .is_some_and(|id| id != ctx.owner())
        {
            continue;
        }
        if same_work(ctx, record, context, work)? {
            prior.push(record.clone());
        }
    }
    if prior.is_empty() {
        return Ok(None);
    }
    let mut out = reuse(ctx, &prior, &prior[0], true)?;
    if out["owner_answers"].as_array().is_none_or(|a| a.is_empty()) {
        return Ok(None);
    }
    let id = todo(ctx, context)?;
    let db = if ctx.todo.exists() {
        read_json(&ctx.todo)?
    } else {
        Value::Null
    };
    let item = db["items"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|v| n(&v["id"]) == id);
    out["original_todo"] = item
        .map(|v| json!({"id":id,"note":v["note"],"spec":v["spec"]}))
        .unwrap_or(json!({"id":id}));
    let digest = crate::automation::key(
        &json!({"todo":out["original_todo"],"questions":out["prior_questions"],
        "requested_scope":context["decision_identity"]})
        .to_string(),
    );
    if s(&context["reviewed_owner_context"]) == digest {
        return Ok(None);
    }
    out["suppressed"] = json!("owner_context_required");
    out["owner_context_digest"] = json!(digest);
    out["next_action"] = json!("元ToDoの指示・元の質問・本人回答を読み、回答に沿って進めてください。技術的な詳細はAIが選びます。本当に新しい対象・仕様・危険範囲の判断が必要な場合だけ、同じdecision JSONにreviewed_owner_contextとしてこのdigestを添えて再実行してください。本人への確認は不要です。");
    redact(ctx, &out).map(Some)
}

/// Explicit bounded repair of reviewed duplicate threads. Never posts, launches or deletes replies.
pub fn lookup(ctx: &mut Context, value: &Value) -> Result<Value> {
    if crate::conversation_bridge::enabled(&ctx.here)? {
        return crate::recovery_messages::lookup_decision(ctx, value);
    }
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
