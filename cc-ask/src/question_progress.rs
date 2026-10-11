//! One owner question per post, with a separate durable, silent work destination.
use crate::{attachments, core::*};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{fs, path::Path, time::Duration};

pub const SUPPRESS_EMBEDS: u64 = 4;

pub fn silent_text(text: &str) -> String {
    let text = safe_text(text);
    let mentions = regex::Regex::new(r"<@(?:!|&)?[^>]+>|@everyone|@here").unwrap();
    let text = mentions.replace_all(&text, "");
    let links = regex::Regex::new(r"<https?://[^\s<>]+>|https?://[^\s<>]+").unwrap();
    links
        .replace_all(&text, |c: &regex::Captures<'_>| {
            if c[0].starts_with('<') {
                c[0].to_owned()
            } else {
                format!("<{}>", &c[0])
            }
        })
        .into_owned()
}

fn url(ctx: &Context, thread: &str) -> String {
    format!(
        "https://discord.com/channels/{}/{thread}",
        s(&ctx.cfg["guild_id"])
    )
}

fn directory(ctx: &Context) -> std::path::PathBuf {
    ctx.here.join("question-progress")
}

pub(crate) fn routes(ctx: &Context) -> Result<Vec<Value>> {
    let dir = directory(ctx);
    if !dir.exists() {
        return Ok(vec![]);
    }
    let mut routes = Vec::new();
    for entry in fs::read_dir(dir).map_err(|e| e.to_string())? {
        let path = entry.map_err(|e| e.to_string())?.path();
        if path.extension().is_some_and(|v| v == "json") {
            let route = read_json(&path)?;
            if route["guild"] == ctx.cfg["guild_id"]
                && route["parent"] == ctx.cfg["task_channel_id"]
            {
                routes.push(route);
            }
        }
    }
    routes.sort_by_key(|r| n(&r["question_thread_id"]));
    Ok(routes)
}

pub fn route(ctx: &Context, origin: &str) -> Result<Option<Value>> {
    let records = routes(ctx)?;
    // An exact question keeps its own bridge even when the same work has a later question.
    if let Some(r) = records
        .iter()
        .find(|r| s(&r["question_thread_id"]) == origin || s(&r["progress_thread_id"]) == origin)
    {
        return Ok(Some(r.clone()));
    }
    Ok(records
        .into_iter()
        .rev()
        .find(|r| !origin.is_empty() && s(&r["work_thread_id"]) == origin))
}

pub fn is_separated(ctx: &Context, origin: &str) -> Result<bool> {
    Ok(route(ctx, origin)?.is_some())
}

pub fn result_target(ctx: &mut Context, origin: &str, _job: &str) -> Result<String> {
    match route(ctx, origin)? {
        Some(r) if !s(&r["progress_thread_id"]).is_empty() => {
            Ok(s(&r["progress_thread_id"]).into())
        }
        Some(r) => {
            recover_pending_for(ctx, Some(s(&r["key"])))?;
            match route(ctx, origin)? {
                Some(r) if !s(&r["progress_thread_id"]).is_empty() => {
                    Ok(s(&r["progress_thread_id"]).into())
                }
                _ => Err("質問の進捗投稿を回収中です。質問投稿へ結果を送信しません".into()),
            }
        }
        None => Ok(origin.into()),
    }
}

fn inventory(ctx: &mut Context, parent: &str, forum: bool) -> Result<Vec<Value>> {
    if !forum {
        let messages = ctx.call(
            "GET",
            &format!("/channels/{parent}/messages?limit=100"),
            None,
        )?;
        return messages
            .as_array()
            .cloned()
            .ok_or("invalid message inventory".into());
    }
    let guild = s(&ctx.cfg["guild_id"]).to_owned();
    let active = ctx.call("GET", &format!("/guilds/{guild}/threads/active"), None)?;
    let archived = ctx.call(
        "GET",
        &format!("/channels/{parent}/threads/archived/public?limit=25"),
        None,
    )?;
    let mut out = active["threads"]
        .as_array()
        .cloned()
        .ok_or("invalid thread inventory")?;
    out.extend(
        archived["threads"]
            .as_array()
            .cloned()
            .ok_or("invalid archive inventory")?,
    );
    out.retain(|t| s(&t["parent_id"]) == parent);
    Ok(out)
}

// Persist the intent before POST. Once attempted, uncertainty is recovered by
// inventory, never by retrying a forum creation (Discord has no forum nonce).
fn create_thread(
    ctx: &mut Context,
    path: &Path,
    record: &mut Value,
    phase: &str,
    title: &str,
    payload: &Value,
    upload: (&[attachments::File], u64),
) -> Result<String> {
    let (files, limit) = upload;
    let parent = s(&record["parent"]).to_owned();
    let forum = truth(&record["forum"]);
    if !s(&record[phase]["id"]).is_empty() {
        return Ok(s(&record[phase]["id"]).into());
    }
    if truth(&record[phase]["attempted"]) {
        let mut matches = Vec::new();
        for candidate in inventory(ctx, &parent, forum)? {
            if n(&candidate["id"]) <= n(&record[phase]["before"]) {
                continue;
            }
            if forum && (candidate["name"] != title || candidate["owner_id"] != record["sender"]) {
                continue;
            }
            let starter = if forum {
                let id = s(&candidate["id"]);
                ctx.call("GET", &format!("/channels/{id}/messages/{id}"), None)?
            } else {
                candidate.clone()
            };
            let actual_files = starter["attachments"]
                .as_array()
                .into_iter()
                .flatten()
                .map(|f| json!({"filename":f["filename"],"size":f["size"]}))
                .collect::<Vec<_>>();
            let expected = record[phase]["files"]
                .as_array()
                .cloned()
                .unwrap_or_else(|| {
                    files
                        .iter()
                        .map(|f| json!({"filename":f.name,"size":f.size}))
                        .collect()
                });
            if starter["author"]["id"] == record["sender"]
                && starter["content"] == payload["content"]
                && actual_files == expected
            {
                matches.push(s(&candidate["id"]).to_owned());
            }
        }
        if matches.len() != 1 {
            return Err("投稿結果が未確定です。同じ質問や進捗投稿は再送しません".into());
        }
        record[phase]["starter"] = json!(matches[0]);
    } else {
        let before = inventory(ctx, &parent, forum)?
            .iter()
            .map(|v| n(&v["id"]))
            .max()
            .unwrap_or(0);
        let body = if forum {
            json!({"name":title,"message":payload})
        } else {
            payload.clone()
        };
        let prepared = attachments::prepare_send(&body, files, limit)?;
        record[phase] = json!({"attempted":true,"before":before.to_string(),"payload":payload,"title":title,
            "files":files.iter().map(|f|json!({"filename":f.name,"size":f.size})).collect::<Vec<_>>()});
        atomic_json(path, record, 2)?;
        let response = attachments::send_prepared(
            ctx,
            &format!(
                "/channels/{parent}/{}",
                if forum { "threads" } else { "messages" }
            ),
            &prepared,
        );
        let response = match response {
            Ok(v) => v,
            Err(e) => {
                // These explicit Discord rejections prove no post was created.
                if regex::Regex::new(r"^(Discord API POST \S+ failed|Discord upload failed): (400|401|403|404|413|429)\b").unwrap().is_match(&e)
                    || e == "Discord API rate limited" || e.starts_with("Discord upload rate limited") {
                    record[phase] = json!({}); atomic_json(path, record, 2)?;
                }
                return Err(e);
            }
        };
        if s(&response["id"]).is_empty() {
            return Err("投稿にDiscord IDがありません".into());
        }
        record[phase]["starter"] = response["id"].clone();
        // A failure here leaves the attempted intent, which inventory recovers.
        atomic_json(path, record, 2)?;
    }
    let starter = s(&record[phase]["starter"]).to_owned();
    let id = if forum {
        starter.clone()
    } else {
        match ctx.call("GET", &format!("/channels/{starter}"), None) {
            Ok(t) if t["id"] == starter && t["parent_id"] == parent => starter.clone(),
            Err(e) if e.contains("404") => {
                let t = ctx.call(
                    "POST",
                    &format!("/channels/{parent}/messages/{starter}/threads"),
                    Some(&json!({"name":title})),
                )?;
                if t["id"] != starter {
                    return Err("text thread ID mismatch".into());
                }
                starter.clone()
            }
            _ => return Err("text thread context is unconfirmed".into()),
        }
    };
    record[phase]["id"] = json!(id);
    atomic_json(path, record, 2)?;
    Ok(id)
}

fn install(ctx: &Context, record: &Value) -> Result<()> {
    let _guard = if crate::state_work::snapshot(ctx).is_some() {
        None
    } else {
        Some(ByteLock::acquire(
            &ctx.here.join("state.lock"),
            Duration::from_secs(60),
        )?)
    };
    let mut state = ctx.load()?;
    let tid = s(&record["question_thread_id"]);
    let mut question = record["context"].clone();
    question["thread_id"] = json!(tid);
    question["after"] = record["question"]["starter"].clone();
    question["question_message"] = question["after"].clone();
    question["question_body"] = record["body"].clone();
    question["open"] = json!(true);
    question["separated_question"] = json!(true);
    question["posting_complete"] = record["complete"].clone();
    question["work_thread_id"] = record["work_thread_id"].clone();
    question["progress_thread_id"] = record["progress_thread_id"].clone();
    question["question_url"] = json!(url(ctx, tid));
    question["work_url"] = json!(url(ctx, s(&record["work_thread_id"])));
    question["progress_url"] = json!(url(ctx, s(&record["progress_thread_id"])));
    if !state["questions"].is_array() {
        state["questions"] = json!([]);
    }
    if !state["questions"]
        .as_array()
        .unwrap()
        .iter()
        .any(|q| s(&q["thread_id"]) == tid)
    {
        state["questions"].as_array_mut().unwrap().push(question);
    } else if let Some(q) = state["questions"]
        .as_array_mut()
        .unwrap()
        .iter_mut()
        .find(|q| s(&q["thread_id"]) == tid)
    {
        // Preserve answer cursors and closed status on a retry.
        q["progress_thread_id"] = record["progress_thread_id"].clone();
        q["posting_complete"] = record["complete"].clone();
        q["progress_url"] = json!(url(ctx, s(&record["progress_thread_id"])));
    }
    state["question_routes"][tid] = json!({"question_thread_id":tid,"work_thread_id":record["work_thread_id"],
        "progress_thread_id":record["progress_thread_id"],"posting_job":record["context"]["posting_job"],
        "todo_id":record["context"]["todo_id"],"session_id":record["context"]["session_id"]});
    ctx.save(&state)?;
    let job = s(&record["context"]["posting_job"]);
    let job_dir = ctx.jobs.join("jobs").join(job);
    if valid_job(job) && job_dir.join("job.json").is_file() {
        let _job_lock = ByteLock::acquire(&job_dir.join("state.lock"), Duration::from_secs(45))?;
        let path = job_dir.join("discord-threads.json");
        let mut association = if path.is_file() {
            read_json(&path)?
        } else {
            json!({"thread_ids":[]})
        };
        if !association.is_object() {
            return Err("invalid Discord question associations".into());
        }
        if association.get("thread_ids").is_none() {
            association["thread_ids"] = json!([]);
        }
        let mut ids = std::collections::BTreeSet::new();
        ids.extend(
            association["question_thread_ids"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(Value::as_str)
                .map(str::to_owned),
        );
        ids.insert(tid.to_owned());
        association["question_thread_ids"] = json!(ids);
        association["question_routes"][tid] = state["question_routes"][tid].clone();
        atomic_json(&path, &association, 2)?;
    }
    Ok(())
}

/// PR108 wrote question-only sidecars without request ownership. Their question
/// IDs must never be promoted to request IDs or prevent unrelated intake.
pub(crate) fn request_thread_ids(association: &Value, job: &str) -> Result<Value> {
    if let Some(ids) = association.get("thread_ids") {
        if ids.is_array() {
            return Ok(ids.clone());
        }
        return Err("invalid Discord thread associations".into());
    }
    let questions = association["question_thread_ids"]
        .as_array()
        .ok_or("invalid Discord thread associations")?;
    if questions.is_empty()
        || !association["question_routes"].is_object()
        || questions.iter().any(|id| {
            let text = s(id);
            text.is_empty()
                || !text.bytes().all(|c| c.is_ascii_digit())
                || association["question_routes"][text]["question_thread_id"] != *id
                || s(&association["question_routes"][text]["posting_job"]) != job
        })
    {
        return Err("invalid Discord question associations".into());
    }
    Ok(json!([]))
}

pub fn ask(
    ctx: &mut Context,
    context: &Value,
    origin: &str,
    body: &str,
    paths: &[std::path::PathBuf],
) -> Result<Value> {
    let _guard = ByteLock::acquire(
        &ctx.here.join("question-progress.lock"),
        Duration::from_secs(60),
    )?;
    // One boundary for every producer: direct ask, post and automatic result.
    // Keeping extraction here prevents a new caller from leaking metadata or
    // falling back to a body/session key instead of permission identity.
    let (clean_body, embedded) = crate::decision_identity::extract(body)?;
    let body = clean_body.as_str();
    let mut context = context.clone();
    let decision = crate::decision_identity::bind(ctx, &mut context, embedded.as_ref())?;
    let prepared = attachments::prepare(ctx, paths)?;
    let hashes = attachments::fingerprints(prepared.as_ref())?;
    let work = route(ctx, origin)?
        .map(|r| s(&r["work_thread_id"]).to_owned())
        .unwrap_or_else(|| origin.into());
    let todo = crate::decision_identity::todo(ctx, &context)?;
    if todo > 0 {
        context["todo_id"] = json!(todo);
    }
    // All callers share this lock, including automatic result delivery. A new job,
    // title or session is never evidence of a new permission scope.
    let records = routes(ctx)?;
    let mut previous = None;
    let mut duplicates = Vec::new();
    let mut uncertain = None;
    for record in &records {
        let attachments_same = record["attachment_hashes"] == json!(hashes)
            || (record["attachment_hashes"].is_null() && hashes.is_empty());
        let same = if decision.is_object() && record["decision_identity"].is_object() {
            decision == record["decision_identity"]
        } else if decision.is_object() {
            false
        } else {
            crate::decision_identity::same_work(ctx, record, &context, &work)?
                && s(&record["body"]) == body
                && attachments_same
        };
        if same {
            previous.get_or_insert_with(|| record.clone());
            duplicates.push(record.clone());
        }
        if (!decision.is_object() || !record["decision_identity"].is_object())
            && crate::decision_identity::same_work(ctx, record, &context, &work)?
        {
            uncertain.get_or_insert(record);
        }
    }
    if let Some(record) = &previous {
        if truth(&record["complete"]) {
            // Untyped callers may recover exact text, but can never consume an approval
            // for a paraphrased/different target merely because the work matches.
            return crate::decision_identity::reuse(ctx, &duplicates, record, false);
        }
    } else if let Some(record) = uncertain {
        return crate::decision_identity::reuse(ctx, &[], record, true);
    }
    let identity = if decision.is_object() {
        json!({"decision":decision})
    } else {
        json!({"guild":ctx.cfg["guild_id"],"parent":ctx.cfg["task_channel_id"],"owner":ctx.owner(),
            "todo":todo,"work":work,"cwd":context["cwd"],"body":body,"hashes":hashes})
    };
    let key = format!("{:x}", Sha256::digest(identity.to_string().as_bytes()));
    let dir = directory(ctx);
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let path = if let Some(record) = &previous {
        crate::decision_identity::path(ctx, record)?
    } else {
        dir.join(format!("{key}.json"))
    };
    let mut record = if path.exists() {
        read_json(&path)?
    } else {
        let parent = s(&ctx.cfg["task_channel_id"]).to_owned();
        let channel = ctx.call("GET", &format!("/channels/{parent}"), None)?;
        let me = ctx.call("GET", "/users/@me", None)?;
        if !truth(&me["bot"]) || s(&me["id"]).is_empty() {
            return Err("question sender unavailable".into());
        }
        let record = json!({"guild":ctx.cfg["guild_id"],"parent":parent,"forum":n(&channel["type"])==15,"sender":me["id"],
            "context":context,"body":body,"work_thread_id":work,"origin_thread_id":origin,"key":key,
            "decision_identity":decision,"attachment_hashes":hashes});
        atomic_json(&path, &record, 2)?;
        record
    };
    let context = record["context"].clone();
    let body = s(&record["body"]);
    let limit = prepared
        .as_ref()
        .map_or(attachments::BASE_LIMIT, |p| p.limit);
    let owner = ctx.owner();
    let body = if s(&context["project"]).is_empty() {
        format!("<@{owner}>\n{}", silent_text(body))
    } else {
        crate::project::question_body(s(&context["project"]), &owner, &silent_text(body))
    };
    // A long second line must not strand the project header in a separate
    // starter without its one owner mention (the generic line chunker can).
    let chars: Vec<_> = body.chars().collect();
    let parts: Vec<String> = chars
        .chunks(MAX_CONTENT)
        .map(|p| p.iter().collect())
        .collect();
    if !record["text_parts"].is_array() {
        record["text_parts"] = json!(parts);
        record["upload_limit"] = json!(limit);
        let mut snapshots = vec![];
        if let Some(prepared) = &prepared {
            let media = dir.join(format!("{key}-media"));
            fs::create_dir_all(&media).map_err(|e| e.to_string())?;
            for (index, file) in prepared.files.iter().enumerate() {
                let copy = media.join(index.to_string());
                fs::copy(&file.path, &copy).map_err(|e| e.to_string())?;
                snapshots.push(json!({"path":copy.to_string_lossy(),"name":file.name,"mime":file.mime,"size":file.size,
                    "hash":format!("{:x}",Sha256::digest(fs::read(&copy).map_err(|e|e.to_string())?))}));
            }
        }
        record["files"] = json!(snapshots);
        atomic_json(&path, &record, 2)?;
    }
    // Retrying the same scope uses the saved original assets, even if this caller
    // has renamed or re-encoded its files. New asset conditions must change scope.
    let stored_files = saved_files(&record)?;
    let batches = attachments::batches(&stored_files);
    let limit = n(&record["upload_limit"]);
    let tid = create_thread(
        ctx,
        &path,
        &mut record,
        "question",
        s(&context["title"]),
        &json!({"content":parts[0],"flags":SUPPRESS_EMBEDS,"allowed_mentions":{"parse":[],"users":[owner],"replied_user":false}}),
        (batches.first().copied().unwrap_or(&[]), limit),
    )?;
    record["question_thread_id"] = json!(tid);
    atomic_json(&path, &record, 2)?;
    // Register before any continuation, retaining reply/recovery even if an upload fails.
    install(ctx, &record)?;
    deliver_parts(ctx, &path, &mut record)?;
    let progress = finish_bridge(ctx, &path, &mut record)?;
    record["complete"] = json!(true);
    atomic_json(&path, &record, 2)?;
    install(ctx, &record)?;
    Ok(
        json!({"thread_id":tid,"after":record["question"]["starter"],"url":url(ctx,&tid),"progress_thread_id":progress,
        "progress_url":url(ctx,&progress),"work_thread_id":work}),
    )
}

fn saved_files(record: &Value) -> Result<Vec<attachments::File>> {
    record["files"]
        .as_array()
        .into_iter()
        .flatten()
        .map(|v| {
            let p = std::path::PathBuf::from(s(&v["path"]));
            let bytes = fs::read(&p).map_err(|e| e.to_string())?;
            if bytes.len() as u64 != n(&v["size"])
                || format!("{:x}", Sha256::digest(&bytes)) != s(&v["hash"])
            {
                return Err("saved question attachment changed".into());
            }
            let mime = match s(&v["mime"]) {
                "image/png" => "image/png",
                "image/jpeg" => "image/jpeg",
                "image/webp" => "image/webp",
                "image/gif" => "image/gif",
                "video/mp4" => "video/mp4",
                "audio/mpeg" => "audio/mpeg",
                "audio/ogg" => "audio/ogg",
                "audio/wav" => "audio/wav",
                _ => "application/octet-stream",
            };
            Ok(attachments::File {
                path: p,
                name: s(&v["name"]).into(),
                mime,
                size: n(&v["size"]),
            })
        })
        .collect()
}

fn deliver_parts(ctx: &mut Context, path: &Path, record: &mut Value) -> Result<()> {
    let parts = record["text_parts"].as_array().cloned().unwrap_or_default();
    let files = saved_files(record)?;
    let batches = attachments::batches(&files);
    let tid = s(&record["question_thread_id"]).to_owned();
    for index in 1..parts.len().max(batches.len()) {
        let key = index.to_string();
        if truth(&record["parts"][&key]["id"]) {
            continue;
        }
        let payload = json!({"content":parts.get(index).and_then(Value::as_str).unwrap_or(""),"flags":SILENT|SUPPRESS_EMBEDS,
            "allowed_mentions":{"parse":[],"users":[],"replied_user":false},"nonce":format!("{:x}",Sha256::digest(format!("{}:{index}",record["key"]).as_bytes()))[..24].to_owned(),"enforce_nonce":true});
        let history = thread_history(ctx, &tid)?;
        let batch = batches.get(index).copied().unwrap_or(&[]);
        let existing = history
            .iter()
            .find(|m| {
                m["author"]["id"] == record["sender"]
                    && (m["nonce"] == payload["nonce"]
                        || (record["parts"][&key]["attempted"] == true
                            && n(&m["id"]) > n(&record["parts"][&key]["before"])
                            && m["content"] == payload["content"]
                            && batch.iter().all(|f| {
                                m["attachments"].as_array().is_some_and(|a| {
                                    a.iter().any(|a| {
                                        s(&a["filename"]) == f.name && n(&a["size"]) == f.size
                                    })
                                })
                            })))
            })
            .cloned();
        if !record["parts"][&key].is_object() {
            record["parts"][&key] = json!({"attempted":true,"before":history.iter().map(|m|n(&m["id"])).max().unwrap_or(0).to_string()});
            atomic_json(path, record, 2)?;
        } else if existing.is_none() {
            return Err("質問の続きの送信結果が未確定です".into());
        }
        let message = match existing {
            Some(m) => m,
            None => attachments::send(
                ctx,
                &format!("/channels/{tid}/messages"),
                &payload,
                batch,
                n(&record["upload_limit"]),
            )?,
        };
        if s(&message["id"]).is_empty() {
            return Err("question continuation has no ID".into());
        }
        record["parts"][&key]["id"] = message["id"].clone();
        atomic_json(path, record, 2)?;
    }
    record["parts_complete"] = json!(true);
    atomic_json(path, record, 2)
}

fn finish_bridge(ctx: &mut Context, path: &Path, record: &mut Value) -> Result<String> {
    let tid = s(&record["question_thread_id"]).to_owned();
    let work = s(&record["work_thread_id"]);
    let bridge = if work.is_empty() {
        format!(
            "質問: <{}>\n以後の進捗と結果はこの投稿へ送ります。",
            url(ctx, &tid)
        )
    } else {
        format!(
            "元の作業: <{}>\n質問: <{}>\n以後の進捗と結果はこの投稿へ送ります。",
            url(ctx, work),
            url(ctx, &tid)
        )
    };
    let title = format!("{} 進捗", s(&record["context"]["title"]))
        .chars()
        .take(100)
        .collect::<String>();
    let progress = create_thread(
        ctx,
        path,
        record,
        "progress",
        &title,
        &json!({"content":bridge,"flags":SILENT|SUPPRESS_EMBEDS,"allowed_mentions":{"parse":[],"users":[],"replied_user":false}}),
        (&[], attachments::BASE_LIMIT),
    )?;
    record["progress_thread_id"] = json!(progress);
    atomic_json(path, record, 2)?;
    install(ctx, record)?;
    Ok(progress)
}

/// Repair at most one new-format interrupted creation. Never migrate legacy
/// questions or emit another question while its first POST is uncertain.
pub fn recover_pending(ctx: &mut Context) -> Result<()> {
    recover_pending_for(ctx, None)
}
fn recover_pending_for(ctx: &mut Context, requested: Option<&str>) -> Result<()> {
    let _guard = ByteLock::acquire(&ctx.here.join("question-progress.lock"), Duration::ZERO)?;
    for mut record in routes(ctx)? {
        if requested.is_some_and(|key| s(&record["key"]) != key) {
            continue;
        }
        if truth(&record["complete"]) || !truth(&record["question"]["attempted"]) {
            continue;
        }
        let key = s(&record["key"]);
        if key.len() != 64 || !key.bytes().all(|b| b.is_ascii_hexdigit()) {
            return Err("invalid question creation key".into());
        }
        let path = directory(ctx).join(format!("{key}.json"));
        let payload = record["question"]["payload"].clone();
        let title = s(&record["question"]["title"]).to_owned();
        let tid = create_thread(
            ctx,
            &path,
            &mut record,
            "question",
            &title,
            &payload,
            (&[], attachments::BASE_LIMIT),
        )?;
        record["question_thread_id"] = json!(tid);
        atomic_json(&path, &record, 2)?;
        install(ctx, &record)?;
        finish_bridge(ctx, &path, &mut record)?;
        deliver_parts(ctx, &path, &mut record)?;
        record["complete"] = json!(true);
        atomic_json(&path, &record, 2)?;
        install(ctx, &record)?;
        break;
    }
    Ok(())
}

pub fn ask_result(ctx: &mut Context, origin: &str, text: &str, job: &str) -> Result<Value> {
    let state = ctx.load()?;
    let original = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .find(|q| s(&q["thread_id"]) == origin)
        .cloned()
        .unwrap_or_else(|| state["requests"][origin].clone());
    let info = job_info(ctx, job);
    let mut context = original;
    if !context.is_object() {
        context = json!({});
    }
    if s(&context["title"]).is_empty() {
        let channel = ctx.call("GET", &format!("/channels/{origin}"), None)?;
        context["title"] = channel["name"].clone();
    }
    if s(&context["project"]).is_empty() && !s(&info["project"]).is_empty() {
        context["project"] = info["project"].clone();
    }
    context["title"] = json!(format!("{} 判断をお願いします", s(&context["title"]))
        .chars()
        .take(100)
        .collect::<String>());
    if s(&context["title"]).trim() == "判断をお願いします" {
        context["title"] = json!("作業の判断をお願いします");
    }
    context["posting_job"] = json!(job);
    // Worker generation is delivery context, never decision identity.
    context["request_key"] = json!(job);
    context["job"] = json!(job);
    context["job_info"] = info.clone();
    if !s(&info["cwd"]).is_empty() {
        context["cwd"] = info["cwd"].clone();
    }
    if !s(&info["thread_id"]).is_empty() {
        context["session_id"] = info["thread_id"].clone();
    }
    if !info["todo_id"].is_null() {
        context["todo_id"] = info["todo_id"].clone();
    }
    context["source"] = json!("posted");
    context["asked_at"] = json!(timestamp());
    for key in [
        "thread_id",
        "after",
        "question_message",
        "continuation",
        "restart",
        "recovery_closed",
        "seen_reply",
    ] {
        context.as_object_mut().unwrap().remove(key);
    }
    let (body, paths) = crate::result_delivery::extract(text)?;
    let (body, decision) = crate::decision_identity::extract(&body)?;
    // A new result must explicitly state its scope; inheriting a prior decision
    // would apply an old a/b to the new proposal.
    context.as_object_mut().unwrap().remove("decision_identity");
    crate::decision_identity::bind(ctx, &mut context, decision.as_ref())?;
    ask(ctx, &context, origin, &body, &paths)
}
