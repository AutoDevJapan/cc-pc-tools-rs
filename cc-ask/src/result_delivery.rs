//! Attach files explicitly selected by an agent's final report to its existing thread.
use crate::{attachments, core::*};
use serde_json::json;
use sha2::{Digest, Sha256};
use std::{collections::HashSet, fs, io::Read, path::PathBuf};

pub enum Delivery {
    Sent(Option<String>),
    PreparationFailed(String),
    OwnerMessagesPending,
}

/// One job can legitimately answer several questions. Keep retries and nonce
/// reconciliation independent per destination, preserving the legacy first ledger.
pub fn ledger_path(ctx: &Context, thread: &str, job: &str) -> Result<PathBuf> {
    if !valid_job(job) || thread.is_empty() || !thread.bytes().all(|c| c.is_ascii_digit()) {
        return Err("invalid result destination".into());
    }
    let directory = ctx.jobs.join("jobs").join(job);
    let scoped = directory.join(format!("attachment-delivery-{thread}.json"));
    if scoped.is_file() {
        return Ok(scoped);
    }
    let legacy = directory.join("attachment-delivery.json");
    if legacy.is_file() {
        let prior = read_json(&legacy)?;
        if !s(&prior["thread_id"]).is_empty() && s(&prior["thread_id"]) != thread {
            return Ok(scoped);
        }
    }
    Ok(legacy)
}

/// Directives are removed from the owner-facing text; examples in code fences stay text.
pub fn extract(text: &str) -> Result<(String, Vec<PathBuf>)> {
    let mut body = Vec::new();
    let mut paths = Vec::new();
    let mut seen = HashSet::new();
    let mut fence = None;
    let mut directive = false;
    for line in text.lines() {
        let trimmed = line.trim();
        let delimiter = if trimmed.starts_with("```") {
            Some('`')
        } else if trimmed.starts_with("~~~") {
            Some('~')
        } else {
            None
        };
        if let Some(delimiter) = delimiter {
            if fence == Some(delimiter) {
                fence = None;
            } else if fence.is_none() {
                fence = Some(delimiter);
            }
        }
        if fence.is_none() && delimiter.is_none() {
            let selected = trimmed
                .strip_prefix("- ")
                .or_else(|| trimmed.strip_prefix("* "))
                .or_else(|| trimmed.strip_prefix("+ "))
                .unwrap_or(trimmed);
            if let Some(rest) = selected.strip_prefix("添付ファイル") {
                let rest = rest.trim_start();
                let Some(value) = rest.strip_prefix(':').or_else(|| rest.strip_prefix('：'))
                else {
                    body.push(line);
                    continue;
                };
                let value = value.trim();
                let value = ['`', '"', '\'']
                    .iter()
                    .find_map(|&quote| {
                        value
                            .strip_prefix(quote)
                            .and_then(|v| v.strip_suffix(quote))
                    })
                    .unwrap_or(value);
                let path = PathBuf::from(value);
                if !path.is_absolute() || value.contains('\0') {
                    return Err("添付ファイルには絶対パスを指定してください".into());
                }
                directive = true;
                // Owner rule (2026-10-05): attach only images/video/audio, never source zips or exes.
                if !is_owner_media(&path) {
                    continue;
                }
                if seen.insert(path.clone()) {
                    paths.push(path);
                }
                continue;
            }
        }
        body.push(line);
    }
    if !directive {
        Ok((text.to_owned(), paths))
    } else {
        Ok((body.join("\n").trim_end().to_owned(), paths))
    }
}

fn is_owner_media(path: &std::path::Path) -> bool {
    const MEDIA: &[&str] = &[
        "png", "jpg", "jpeg", "gif", "webp", "bmp", "svg", "mp4", "mov", "webm", "mkv", "avi",
        "mp3", "wav", "m4a", "ogg", "flac", "aac", "pdf",
    ];
    path.extension()
        .and_then(|e| e.to_str())
        .is_some_and(|e| MEDIA.contains(&e.to_ascii_lowercase().as_str()))
}

fn nonce(thread: &str, job: &str, part: &str) -> String {
    format!("{:x}", Sha256::digest(format!("{thread}/{job}/{part}")))[..24].to_owned()
}

fn permanent_send_error(error: &str) -> bool {
    [400, 401, 403, 404, 413, 415, 422]
        .iter()
        .any(|code| error.contains(&format!("failed: {code}")))
}

fn failure(
    ctx: &mut Context,
    thread: &str,
    job: &str,
    ledger: &std::path::Path,
    progress: &mut serde_json::Value,
    error: &str,
) -> Result<Delivery> {
    progress["error"] = json!(safe_text(error));
    atomic_json(ledger, progress, 2)?;
    if !truth(&progress["error_notice_after"]) {
        let target = if let Some(target) = progress["destination_thread_id"].as_str() {
            target.to_owned()
        } else {
            crate::question_progress::result_target(ctx, thread, job)?
        };
        progress["destination_thread_id"] = json!(target);
        atomic_json(ledger, progress, 2)?;
        let content = crate::question_progress::silent_text(&format!(
            "依頼の添付ファイルを送れませんでした。\n理由: {}\n受付が作業を自動で再開して修復します。",
            safe_text(error)
        ));
        let notice_nonce = nonce(thread, job, "error");
        // History also prevents duplicates beyond Discord's short nonce retention window.
        let prior = thread_history(ctx, &target)
            .unwrap_or_default()
            .into_iter()
            .find(|m| s(&m["nonce"]) == notice_nonce);
        let response = match prior {
            Some(message) => message,
            None => match ctx.call(
                "POST",
                &format!("/channels/{target}/messages"),
                Some(&json!({
                    "content":content, "allowed_mentions":{"parse":[],"users":[],"replied_user":false},
                    "flags":SILENT | crate::question_progress::SUPPRESS_EMBEDS,
                    "nonce":notice_nonce,"enforce_nonce":true
                })),
            ) {
                Ok(response) => response,
                Err(error) => {
                    progress["error_notice_error"] = json!(safe_text(error));
                    atomic_json(ledger, progress, 2)?;
                    return Ok(Delivery::PreparationFailed(
                        s(&progress["error"]).to_owned(),
                    ));
                }
            },
        };
        if s(&response["id"]).is_empty() {
            return Err("添付エラーの通知結果がありません".into());
        }
        progress["error_notice_after"] = response["id"].clone();
        atomic_json(ledger, progress, 2)?;
    }
    Ok(Delivery::PreparationFailed(
        s(&progress["error"]).to_owned(),
    ))
}

pub fn post(
    ctx: &mut Context,
    thread: &str,
    text: &str,
    mention: bool,
    job: &str,
) -> Result<Delivery> {
    post_with_cursor(ctx, thread, text, mention, job, None)
}

/// Check once more after attachment preparation, immediately before starting the final reply.
pub fn post_with_cursor(
    ctx: &mut Context,
    thread: &str,
    text: &str,
    mention: bool,
    job: &str,
    after: Option<&str>,
) -> Result<Delivery> {
    // These files belong to the finished job, never to a source attachment or credentials.
    if !valid_job(job) {
        return Err("添付結果のジョブIDが不正です".into());
    }
    let ledger = ledger_path(ctx, thread, job)?;
    let mut progress = if ledger.is_file() {
        read_json(&ledger)?
    } else {
        json!({})
    };
    if !s(&progress["thread_id"]).is_empty() && s(&progress["thread_id"]) != thread {
        return Err("result delivery is already bound to another thread".into());
    }
    progress["thread_id"] = json!(thread);
    if truth(&progress["complete"]) {
        return Ok(Delivery::Sent(progress["last"].as_str().map(str::to_owned)));
    }
    if truth(&progress["error"]) {
        let error = s(&progress["error"]).to_owned();
        return failure(ctx, thread, job, &ledger, &mut progress, &error);
    }
    let (body, paths) = match extract(text) {
        Ok(selected) => selected,
        Err(error) => return failure(ctx, thread, job, &ledger, &mut progress, &error),
    };
    let (body, decision) = crate::decision_identity::extract(&body)?;
    let typed_decision = decision.is_some();
    // Validate every file before posting any part or recording successful delivery.
    let prepared = match attachments::prepare(ctx, &paths) {
        Ok(Some(prepared)) => Some(prepared),
        Ok(None) if paths.is_empty() => None,
        Ok(None) => {
            return failure(
                ctx,
                thread,
                job,
                &ledger,
                &mut progress,
                "添付ファイルがありません",
            );
        }
        Err(error) => return failure(ctx, thread, job, &ledger, &mut progress, &error),
    };
    let needs_question = result_state(text) == "状態: 要判断"
        || (text.starts_with("判断をお願いしたいです！")
            && (concrete_question_body(&body) || choice_question_body(&body)));
    let owner = ctx.owner();
    let mention =
        mention && !needs_question && !crate::question_progress::is_separated(ctx, thread)?;
    let mut body = safe_text(body);
    if mention {
        let (first, rest) = body.split_once('\n').unwrap_or((&body, ""));
        body = if body.contains('\n') {
            format!("{first}\n<@{owner}>\n{rest}")
        } else {
            format!("{first}\n<@{owner}>")
        };
    }
    // Bind routing before fingerprinting so retries cannot switch destinations.
    // The ledger and owner cursor always belong to the origin, not this target.
    let target = if let Some(target) = progress["destination_thread_id"].as_str() {
        target.to_owned()
    } else if needs_question {
        // ask_result must be allowed to recover a question whose progress bridge
        // has not yet been created; result_target deliberately rejects that state.
        thread.to_owned()
    } else {
        crate::question_progress::result_target(ctx, thread, job)?
    };
    progress["destination_thread_id"] = json!(target);
    atomic_json(&ledger, &progress, 2)?;
    if needs_question && !truth(&progress["question_thread_id"]) {
        if unseen(ctx, thread, job, after)? {
            return Ok(Delivery::OwnerMessagesPending);
        }
        if !typed_decision
            && crate::questions::answered_repost(
                ctx,
                thread,
                text,
                Some(job),
                &attachments::fingerprints(prepared.as_ref())?,
            )?
            .is_some()
        {
            return Ok(Delivery::OwnerMessagesPending);
        }
        // The parent API registers a new generation with the original work context.
        // It must reconcile the same origin/job on retry after a lost response.
        let question = crate::question_progress::ask_result(ctx, thread, text, job)?;
        if question["suppressed"] == "decision_scope_required" {
            return Err(format!(
                "DecisionScopeRequired: {} {}",
                s(&question["next_action"]),
                s(&question["url"])
            ));
        }
        if s(&question["thread_id"]).is_empty()
            || s(&question["after"]).is_empty()
            || s(&question["progress_thread_id"]).is_empty()
        {
            return Err("新しい質問の配送先がありません".into());
        }
        progress["question_thread_id"] = question["thread_id"].clone();
        progress["question_after"] = question["after"].clone();
        progress["destination_thread_id"] = question["progress_thread_id"].clone();
        progress["decision_reuse"] = if truth(&question["reused"]) {
            question
        } else {
            serde_json::Value::Null
        };
        atomic_json(&ledger, &progress, 2)?;
    }
    let target = s(&progress["destination_thread_id"]).to_owned();
    let separated = target != thread || crate::question_progress::is_separated(ctx, thread)?;
    let mention = mention && target == thread;
    if needs_question {
        body = format!(
            "{}\nhttps://discord.com/channels/{}/{}",
            if truth(&progress["decision_reuse"]) {
                "同じ判断の質問と回答を保持しています。"
            } else {
                "判断をお願いする質問を新しい投稿にしました。"
            },
            s(&ctx.cfg["guild_id"]),
            s(&progress["question_thread_id"])
        );
    }
    if target != thread || crate::question_progress::is_separated(ctx, thread)? {
        body = crate::question_progress::silent_text(&body);
    }
    let parts = chunks(&body, MAX_CONTENT);
    // ask_result owns attachments of a concrete question; the progress post links it.
    let batches = prepared
        .as_ref()
        .filter(|_| !needs_question)
        .map(|p| attachments::batches(&p.files))
        .unwrap_or_default();
    let count = parts.len().max(batches.len());
    let mut digest = Sha256::new();
    digest.update(body.as_bytes());
    if needs_question {
        digest.update(text.as_bytes());
    }
    for file in prepared.as_ref().into_iter().flat_map(|p| &p.files) {
        digest.update(file.name.as_bytes());
        digest.update(file.size.to_le_bytes());
        let mut input = fs::File::open(&file.path).map_err(|_| "添付用のコピーを読み取れません")?;
        let mut buffer = [0u8; 65536];
        loop {
            let size = input
                .read(&mut buffer)
                .map_err(|_| "添付用のコピーを読み取れません")?;
            if size == 0 {
                break;
            }
            digest.update(&buffer[..size]);
        }
    }
    let fingerprint = format!("{:x}", digest.finalize());
    if truth(&progress["fingerprint"]) && s(&progress["fingerprint"]) != fingerprint {
        return failure(
            ctx,
            thread,
            job,
            &ledger,
            &mut progress,
            "送信途中に本文または添付が変わりました",
        );
    }
    progress["fingerprint"] = json!(fingerprint);
    progress["attachment_hashes"] = json!(attachments::fingerprints(prepared.as_ref())?);
    atomic_json(&ledger, &progress, 2)?;
    let first = n(&progress["next"]) as usize;
    if first == 0 && unseen(ctx, thread, job, after)? {
        return Ok(Delivery::OwnerMessagesPending);
    }
    if first == 0
        && !typed_decision
        && crate::questions::answered_repost(
            ctx,
            thread,
            text,
            Some(job),
            &attachments::fingerprints(prepared.as_ref())?,
        )?
        .is_some()
    {
        return Ok(Delivery::OwnerMessagesPending);
    }
    let mut last = progress["last"].as_str().map(str::to_owned);
    for index in first..count {
        let content = parts.get(index).map(String::as_str).unwrap_or("");
        let users = if mention && index == 0 && content.contains(&format!("<@{owner}>")) {
            vec![owner.clone()]
        } else {
            vec![]
        };
        let mut payload = json!({
            "content": content,
            "allowed_mentions": {"parse": [], "users": users},
            "flags": if mention && index == 0 { 0 } else if separated {SILENT | crate::question_progress::SUPPRESS_EMBEDS} else {SILENT},
            "nonce": nonce(thread, job, &index.to_string()),
            "enforce_nonce": true
        });
        if separated {
            payload["allowed_mentions"]["replied_user"] = json!(false);
        }
        // An interrupted save or lost response may already have delivered this
        // part. Match nonce or exact text plus the durable pre-send cursor.
        let part_key = index.to_string();
        let batch = batches.get(index).copied().unwrap_or(&[]);
        let history = thread_history(ctx, &target)?;
        let existing = history
            .iter()
            .find(|m| {
                truth(&m["author"]["bot"])
                    && (m["nonce"] == payload["nonce"]
                        || (progress["attempts"][&part_key].is_object()
                            && n(&m["id"]) > n(&progress["attempts"][&part_key]["after"])
                            && m["content"] == payload["content"]
                            && batch.iter().all(|file| {
                                m["attachments"].as_array().is_some_and(|files| {
                                    files.iter().any(|f| {
                                        s(&f["filename"]) == file.name && n(&f["size"]) == file.size
                                    })
                                })
                            })))
            })
            .cloned();
        if !progress["attempts"].is_object() {
            progress["attempts"] = json!({});
        }
        if !progress["attempts"][&part_key].is_object() {
            progress["attempts"][&part_key] =
                json!({"after":history.iter().map(|m|n(&m["id"])).max().unwrap_or(0).to_string()});
            atomic_json(&ledger, &progress, 2)?;
        }
        let response = if let Some(existing) = existing {
            existing
        } else {
            match attachments::send(
                ctx,
                &format!("/channels/{target}/messages"),
                &payload,
                batch,
                prepared
                    .as_ref()
                    .map_or(attachments::BASE_LIMIT, |p| p.limit),
            ) {
                Ok(response) => response,
                Err(error) if permanent_send_error(&error) => {
                    return failure(ctx, thread, job, &ledger, &mut progress, &error);
                }
                Err(error) => return Err(error),
            }
        };
        let id = s(&response["id"]);
        if id.is_empty() {
            return Err("添付の送信結果にメッセージIDがありません".into());
        }
        last = Some(id.to_owned());
        if !truth(&progress["first"]) {
            progress["first"] = json!(id);
        }
        progress["next"] = json!(index + 1);
        progress["last"] = json!(id);
        progress["complete"] = json!(index + 1 == count);
        atomic_json(&ledger, &progress, 2)?;
    }
    Ok(Delivery::Sent(last))
}

fn unseen(ctx: &mut Context, thread: &str, job: &str, after: Option<&str>) -> Result<bool> {
    let Some(after) = after else {
        return Ok(false);
    };
    let owner = ctx.owner();
    let state = ctx.load()?;
    for message in thread_history(ctx, thread)?.iter().filter(|m| {
        n(&m["id"]) > after.parse::<u64>().unwrap_or(0)
            && s(&m["author"]["id"]) == owner
            && !crate::snooze::control_only(m)
    }) {
        if reply_delivered_to_job(ctx, &state, thread, job, message)? {
            continue;
        }
        // Receipt alone, another worker, or a read reservation cannot permit
        // this finished job to publish a result that predates the correction.
        return Ok(true);
    }
    Ok(false)
}
