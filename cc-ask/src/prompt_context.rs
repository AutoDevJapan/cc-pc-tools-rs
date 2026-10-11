//! Keep automatic jobs scoped and bounded. Full sources stay on disk for selective reads.
use crate::core::*;
use serde_json::{json, Value};
use std::{fs, path::Path};

pub const MAX_PROMPT_BYTES: usize = 60 * 1024;
pub const REDUCED_PROMPT_BYTES: usize = 24 * 1024;
pub const MIN_PROMPT_BYTES: usize = 8 * 1024;

pub fn limit(selection: &Value) -> usize {
    selection["prompt_limit_bytes"]
        .as_u64()
        .map(|n| n.clamp(MIN_PROMPT_BYTES as u64, MAX_PROMPT_BYTES as u64) as usize)
        .unwrap_or(MAX_PROMPT_BYTES)
}

pub fn reduce(selection: &mut Value) {
    selection["prompt_limit_bytes"] =
        json!((limit(selection) / 2).clamp(MIN_PROMPT_BYTES, REDUCED_PROMPT_BYTES));
}

fn boundary(text: &str, mut index: usize) -> usize {
    index = index.min(text.len());
    while !text.is_char_boundary(index) {
        index -= 1;
    }
    index
}

/// Preserve instructions at the beginning and the newest material at the end.
pub fn bounded(text: &str, bytes: usize, reference: &str) -> String {
    if text.len() <= bytes {
        return text.into();
    }
    let note = format!("\n[古い文脈の一部を省略。参照先: {reference}。必要な箇所だけ読むこと。]\n");
    if note.len() >= bytes {
        return note[..boundary(&note, bytes)].into();
    }
    let available = bytes - note.len();
    let head = boundary(text, available / 3);
    let mut tail = boundary(text, text.len() - (available - head));
    if tail < text.len() {
        tail += text[tail..].chars().next().unwrap().len_utf8();
    }
    format!("{}{note}{}", &text[..head], &text[tail..])
}

fn snapshot(ctx: &Context, name: &str, text: &str) -> Result<String> {
    let root = ctx.here.join("prompt-context");
    fs::create_dir_all(&root).map_err(|e| e.to_string())?;
    let name = Path::new(name);
    let digest = crate::automation::key(text);
    let path = root.join(format!(
        "{}-{}.{}",
        name.file_stem().unwrap().to_string_lossy(),
        &digest[..12],
        name.extension().unwrap().to_string_lossy()
    ));
    if !path.is_file() {
        fs::write(&path, text).map_err(|e| e.to_string())?;
    }
    Ok(path.to_string_lossy().into_owned())
}

/// These are console-wide aggregates, not the work assigned by this thread.
fn scoped(text: &str) -> (String, bool) {
    let mut skip = false;
    let mut in_message = false;
    let mut omitted = false;
    let mut out = String::new();
    for line in text.lines() {
        let trimmed = line.trim();
        if trimmed.starts_with("### オーナー（")
            || trimmed.starts_with("### エージェント（")
            || trimmed.starts_with("オーナー投稿 ")
        {
            in_message = true;
            skip = false;
        } else if [
            "元の依頼・作業指示:",
            "元の作業指示:",
            "前回の作業指示:",
            "前回の結果:",
            "前のジョブの最後のメッセージ:",
            "スレッドの冒頭を含む会話:",
            "オーナーの全投稿（古い順）:",
        ]
        .contains(&trimmed)
            || trimmed.starts_with("## スレッド「")
        {
            in_message = false;
            skip = false;
        }
        if !in_message
            && [
                "共有ToDoの文脈:",
                "共有 ToDoの文脈:",
                "## 引き継ぎ",
                "未完了のPR",
                "未完了の PR",
            ]
            .contains(&trimmed)
        {
            skip = true;
            omitted = true;
            continue;
        }
        if skip
            && (trimmed.starts_with("スレッドの冒頭を含む会話:")
                || trimmed.starts_with("## スレッド「")
                || trimmed.starts_with("オーナーの全投稿（古い順）:")
                || trimmed.starts_with("前のジョブの最後のメッセージ:")
                || trimmed.starts_with("元の作業指示:")
                || trimmed.starts_with("前回の結果:")
                || trimmed.starts_with("## 作業の依頼"))
        {
            skip = false;
        }
        if !skip {
            out.push_str(line);
            out.push('\n');
        }
    }
    if omitted {
        (out, true)
    } else {
        (text.into(), false)
    }
}

pub fn source(ctx: &Context, key: &str, text: &str, path: &Path, bytes: usize) -> Result<String> {
    let (scoped, omitted) = scoped(&safe_text(text));
    let reference = if path.is_file() {
        path.to_string_lossy().into_owned()
    } else {
        snapshot(ctx, &format!("source-{key}.md"), &safe_text(text))?
    };
    let note = if omitted {
        format!("\n[共有ToDoの全件・全作業の引き継ぎ・PR一覧を省略。参照先: {reference}。この作業に必要な箇所だけ読むこと。]\n")
    } else {
        String::new()
    };
    Ok(format!(
        "{}{}",
        bounded(&scoped, bytes.saturating_sub(note.len()), &reference),
        note
    ))
}

/// Keep the opening question and newest messages, dropping old messages first.
pub fn conversation(
    ctx: &Context,
    thread: &Value,
    messages: &[Value],
    owner: &str,
    bytes: usize,
) -> Result<String> {
    let full = crate::core::conversation(thread, messages, owner);
    if full.len() <= bytes {
        return Ok(full);
    }
    let tid = s(&thread["id"]);
    if !valid_job(tid) {
        return Err("invalid context thread ID".into());
    }
    let last = messages.iter().map(|m| n(&m["id"])).max().unwrap_or(0);
    let reference = snapshot(
        ctx,
        &format!("thread-{tid}-{last}.json"),
        &safe_text(serde_json::to_string_pretty(messages).map_err(|e| e.to_string())?),
    )?;
    let notice =
        format!("\n[古い会話を省略。全会話の参照先: {reference}。必要な投稿だけ読むこと。]\n");
    let budget = bytes.saturating_sub(notice.len());
    let mut selected = Vec::new();
    let mut used = 0;
    // Give the opening question some space, but reserve most of it for current replies.
    if let Some(first) = messages.first() {
        let text = bounded(
            &crate::core::conversation(thread, std::slice::from_ref(first), owner),
            budget / 5,
            &reference,
        );
        used += text.len();
        selected.push((0, text));
    }
    if let Some((index, message)) = messages
        .iter()
        .enumerate()
        .rev()
        .find(|(index, m)| *index != 0 && s(&m["author"]["id"]) == owner)
    {
        let text = bounded(
            &crate::core::conversation(&json!({}), std::slice::from_ref(message), owner),
            budget / 3,
            &reference,
        );
        used += text.len();
        selected.push((index, text));
        // A short answer such as "a" is meaningless without the exact question.
        // Prefer an explicit same-thread reply; otherwise retain the latest registered
        // question before this answer. Selection preserves evidence, never grants access.
        let explicit = s(&message["message_reference"]["message_id"]);
        let channel = s(&message["message_reference"]["channel_id"]);
        let question_id = if !explicit.is_empty() {
            (channel.is_empty() || channel == tid).then(|| explicit.to_owned())
        } else {
            let state = ctx.load()?;
            state["questions"]
                .as_array()
                .into_iter()
                .flatten()
                .filter(|q| s(&q["thread_id"]) == tid)
                .flat_map(|q| {
                    let mut ids = vec![q["question_message"].clone(), q["after"].clone()];
                    ids.extend(q["message_ids"].as_array().into_iter().flatten().cloned());
                    ids
                })
                .filter(|id| {
                    n(id) > 0
                        && n(id) < n(&message["id"])
                        && messages.iter().any(|m| {
                            m["id"] == *id
                                && s(&m["author"]["id"]) != owner
                                && crate::questions::is_question(s(&m["content"]))
                        })
                })
                .max_by_key(n)
                .map(|id| s(&id).to_owned())
        };
        if let Some((question_index, question)) = question_id.and_then(|id| {
            messages
                .iter()
                .enumerate()
                .find(|(i, m)| *i < index && s(&m["id"]) == id && s(&m["author"]["id"]) != owner)
        }) {
            if !selected.iter().any(|(i, _)| *i == question_index) {
                let text = bounded(
                    &crate::core::conversation(thread, std::slice::from_ref(question), owner),
                    (budget - used).min(budget / 3),
                    &reference,
                );
                used += text.len();
                selected.push((question_index, text));
            }
        }
    }
    for (index, message) in messages.iter().enumerate().skip(1).rev() {
        if selected.iter().any(|(i, _)| *i == index) {
            continue;
        }
        let text = crate::core::conversation(&json!({}), std::slice::from_ref(message), owner);
        let text = text.strip_prefix("## スレッド「」\n\n").unwrap_or(&text);
        if used + text.len() <= budget {
            used += text.len();
            selected.push((index, text.to_owned()));
        } else if budget.saturating_sub(used) > notice.len() {
            let text = bounded(text, (budget - used).min(budget / 3), &reference);
            used += text.len();
            selected.push((index, text));
        } else {
            break;
        }
    }
    selected.sort_by_key(|(i, _)| *i);
    Ok(format!(
        "{}{notice}",
        selected
            .into_iter()
            .map(|(_, text)| text)
            .collect::<String>()
    ))
}

fn related_todos(ctx: &Context, thread: &Value, selection: &Value, cwd: &str) -> Result<String> {
    if !ctx.todo.is_file() {
        return Ok(String::new());
    }
    let db = read_json(&ctx.todo)?;
    let question = json!({"thread_id":thread["id"],"title":thread["name"]});
    let items: Vec<_> = db["items"].as_array().into_iter().flatten().filter(|item| {
        let linked = item["jobs"].as_array().into_iter().flatten().any(|id|
            !s(id).is_empty() && ["id", "thread_id", "resume_session"].iter().any(|field| id == &selection[*field]));
        let same_project = db["projects"][s(&item["project"])].as_str()
            .is_some_and(|base| crate::resume::in_project(cwd, base));
        linked || (truth(&selection["todo_id"]) && item["id"] == selection["todo_id"])
            || (same_project && crate::resume::task_matches(&question, &[],
                &format!("{}\n{}", s(&item["title"]), s(&item["note"]))))
    }).map(|item| json!({"id":item["id"],"title":item["title"],"status":item["status"],"note":item["note"]})).collect();
    if items.is_empty() {
        return Ok(String::new());
    }
    Ok(format!(
        "\nこの作業に関連する共有ToDo（参照先: {}）:\n{}\n",
        ctx.todo.display(),
        bounded(
            &serde_json::to_string(&items).map_err(|e| e.to_string())?,
            (limit(selection) / 12).min(4096),
            &ctx.todo.to_string_lossy()
        )
    ))
}

/// Final cap also covers reservations persisted by an older executable.
pub fn prepare(
    ctx: &Context,
    thread: &Value,
    prompt: &str,
    selection: &Value,
    cwd: &str,
    key: &str,
) -> Result<String> {
    if !valid_job(key) {
        return Err("invalid context launch key".into());
    }
    let (mut scoped, omitted) = scoped(prompt);
    if !scoped.contains("この作業に関連する共有ToDo（参照先:") {
        scoped.push_str(&related_todos(ctx, thread, selection, cwd)?);
    }
    let bytes = limit(selection);
    if !omitted && scoped.len() <= bytes {
        return Ok(scoped);
    }
    let reference = snapshot(ctx, &format!("input-{key}.md"), &safe_text(prompt))?;
    let note = format!("\n[プロンプト上限 {bytes} bytes。古い文脈・全作業の一覧を省略。参照先: {reference} / {}。このスレッドに関係する箇所だけ読むこと。]\n", ctx.todo.display());
    Ok(format!(
        "{}{}",
        bounded(&scoped, bytes.saturating_sub(note.len()), &reference),
        note
    ))
}
