//! Match open questions before creating a Discord thread. No model or network is
//! involved in matching, and a dedicated OS lock covers lookup through registration.
use crate::core::*;
use regex::Regex;
use serde_json::{json, Value};
use std::{collections::BTreeSet, path::Path, time::Duration};

pub(crate) fn creation_lock(ctx: &Context) -> Result<ByteLock> {
    ByteLock::acquire(&ctx.here.join("ask-create.lock"), Duration::from_secs(60))
}

pub(crate) fn folder_key(folder: &str) -> String {
    if folder.trim().is_empty() {
        return String::new();
    }
    let resolved = Path::new(folder).canonicalize().ok();
    let text = resolved
        .as_ref()
        .map(|p| p.to_string_lossy().into_owned())
        .unwrap_or_else(|| folder.to_owned());
    folder_spelling(&text)
}

fn folder_spelling(folder: &str) -> String {
    let text = folder.replace('\\', "/");
    let text = text.strip_prefix("//?/").unwrap_or(&text);
    let mut parts: Vec<&str> = Vec::new();
    for part in text.split('/') {
        match part {
            "." => {}
            ".." if parts
                .last()
                .is_some_and(|p| *p != ".." && !p.ends_with(':')) =>
            {
                parts.pop();
            }
            _ => parts.push(part),
        }
    }
    let key = parts.join("/").trim_end_matches('/').to_owned();
    if cfg!(windows) {
        key.to_lowercase()
    } else {
        key
    }
}

fn title_key(title: &str) -> String {
    let agent = Regex::new(r"^\s*\[[^\]]+\]\s*").unwrap();
    let title = agent.replace(title, "");
    // Keep project labels: different products in one working folder are distinct.
    let numbered = Regex::new(r"(?i)\bq\s*\d+\s*[:：.\-]?\s*").unwrap();
    let title = numbered.replace_all(&title, "");
    let title = title
        .to_lowercase()
        .replace("サインイン", "ログイン")
        .replace("sign-in", "ログイン")
        .replace("sign in", "ログイン")
        .replace("signin", "ログイン")
        .replace("login", "ログイン");
    let key: String = title.chars().filter(|c| c.is_alphanumeric()).collect();
    key.replace("へのログイン", "ログイン")
        .replace("のログイン", "ログイン")
}

fn similar(left: &str, right: &str) -> bool {
    let projects = Regex::new(r"【[^】]+】").unwrap();
    if projects
        .find_iter(left)
        .map(|m| m.as_str().to_lowercase())
        .collect::<Vec<_>>()
        != projects
            .find_iter(right)
            .map(|m| m.as_str().to_lowercase())
            .collect::<Vec<_>>()
    {
        return false;
    }
    let (left, right) = (title_key(left), title_key(right));
    if left.is_empty() || right.is_empty() {
        return false;
    }
    if left == right {
        return true;
    }
    // Different prices/versions remain separate; question numbers were removed above.
    let digits = Regex::new(r"[0-9０-９〇零一二三四五六七八九十百千万億兆]+").unwrap();
    if digits
        .find_iter(&left)
        .map(|m| m.as_str())
        .collect::<Vec<_>>()
        != digits
            .find_iter(&right)
            .map(|m| m.as_str())
            .collect::<Vec<_>>()
    {
        return false;
    }
    let latin = Regex::new(r"[a-z][a-z0-9]*").unwrap();
    if latin
        .find_iter(&left)
        .map(|m| m.as_str())
        .collect::<Vec<_>>()
        != latin
            .find_iter(&right)
            .map(|m| m.as_str())
            .collect::<Vec<_>>()
    {
        return false;
    }
    for word in [
        "承認",
        "却下",
        "無料",
        "有料",
        "税込",
        "税抜",
        "禁止",
        "許可",
        "不要",
        "必要",
        "しない",
        "できない",
    ] {
        if left.contains(word) != right.contains(word) {
            return false;
        }
    }
    let grams = |s: &str| {
        let chars: Vec<_> = s.chars().collect();
        chars
            .windows(2)
            .map(|p| (p[0], p[1]))
            .collect::<BTreeSet<_>>()
    };
    let (a, b) = (grams(&left), grams(&right));
    // Require substantial overlap in both directions, not just a shared prefix.
    let shared = a.intersection(&b).count();
    shared >= 4 && shared * 5 >= a.len() * 4 && shared * 5 >= b.len() * 4
}

pub(crate) fn find_open(state: &Value, cwd: &str, title: &str, raw_title: &str) -> Option<Value> {
    let folder = folder_key(cwd);
    if folder.is_empty() {
        return None;
    }
    state["questions"]
        .as_array()?
        .iter()
        .rev()
        .find(|q| {
            truth(&q["open"])
                && !s(&q["thread_id"]).is_empty()
                && if let Some(original) = q["ask_title"].as_str().filter(|t| !t.is_empty()) {
                    similar(original, raw_title)
                } else {
                    similar(s(&q["title"]), title)
                        // Legacy records have only the displayed title. An
                        // explicit product label from the caller remains valid
                        // when its working folder is a shared orchestration root.
                        || (raw_title.contains('【') && similar(s(&q["title"]), raw_title))
                }
                && question_folder_matches(state, q, cwd, &folder)
        })
        .cloned()
}

fn question_folder_matches(state: &Value, question: &Value, cwd: &str, folder: &str) -> bool {
    let recorded = s(&question["cwd"]);
    let key = s(&question["cwd_key"]);
    if !recorded.is_empty() || !key.is_empty() {
        // A question's own context takes precedence over its parent request.
        return key == folder
            || (!recorded.is_empty()
                && (folder_spelling(recorded) == folder_spelling(cwd)
                    || folder_spelling(recorded) == folder));
    }
    // Older recovered questions may lack cwd. Use only context explicitly
    // attached to this thread; project-title guesses and broad default folders
    // from infer_question_cwd are not sufficient evidence for deduplication.
    let request = &state["requests"][s(&question["thread_id"])];
    [
        &question["continuation"],
        &question["continuation"]["job_info"],
        request,
        &request["job_info"],
    ]
    .iter()
    .map(|context| s(&context["cwd"]))
    .find(|path| !path.is_empty())
    .is_some_and(|path| {
        folder_spelling(path) == folder_spelling(cwd) || folder_spelling(path) == folder
    })
}

fn inventory(ctx: &mut Context, parent: &str) -> Result<Vec<Value>> {
    let guild = s(&ctx.cfg["guild_id"]).to_owned();
    let active = ctx.call("GET", &format!("/guilds/{guild}/threads/active"), None)?;
    let archived = ctx.call(
        "GET",
        &format!("/channels/{parent}/threads/archived/public?limit=25"),
        None,
    )?;
    if !active["threads"].is_array() || !archived["threads"].is_array() {
        return Err("Discord thread inventory is invalid".into());
    }
    Ok(active["threads"]
        .as_array()
        .into_iter()
        .flatten()
        .chain(archived["threads"].as_array().into_iter().flatten())
        .filter(|t| s(&t["parent_id"]) == parent)
        .cloned()
        .collect())
}

/// Durable intent is committed before an irreversible new-thread POST. If the
/// response is lost, the next caller recovers this intent rather than sending again.
pub(crate) fn reserve(
    ctx: &mut Context,
    question: &Value,
    first: &str,
    files: &[crate::attachments::File],
    forum: bool,
) -> Result<String> {
    let parent = s(&ctx.cfg["task_channel_id"]).to_owned();
    let me = ctx.call("GET", "/users/@me", None)?;
    if !truth(&me["bot"]) || s(&me["id"]).is_empty() {
        return Err("question sender unavailable".into());
    }
    let before = if forum {
        inventory(ctx, &parent)?
            .iter()
            .map(|t| n(&t["id"]))
            .max()
            .unwrap_or(0)
    } else {
        ctx.call(
            "GET",
            &format!("/channels/{parent}/messages?limit=100"),
            None,
        )?
        .as_array()
        .into_iter()
        .flatten()
        .map(|m| n(&m["id"]))
        .max()
        .unwrap_or(0)
    };
    let id = uuid::Uuid::new_v4().simple().to_string();
    let _state = ByteLock::acquire(&ctx.here.join("state.lock"), Duration::from_secs(60))?;
    let mut state = ctx.load_fresh()?;
    state["ask_creations"][&id] = json!({"question":question,"first":first,"files":files.iter().map(|f| json!({"filename":f.name,"size":f.size})).collect::<Vec<_>>(),
        "sender":me["id"],"parent":parent,"guild":ctx.cfg["guild_id"],"before":before.to_string(),"forum":forum,"starter_only":!forum});
    atomic_json(&ctx.here.join("state.json"), &state, 2)?;
    Ok(id)
}

pub(crate) fn checkpoint(ctx: &Context, id: &str, thread: &str, first: &str) -> Result<()> {
    let _state = ByteLock::acquire(&ctx.here.join("state.lock"), Duration::from_secs(60))?;
    let mut state = ctx.load_fresh()?;
    state["ask_creations"][id]["thread_id"] = json!(thread);
    state["ask_creations"][id]["message_id"] = json!(first);
    atomic_json(&ctx.here.join("state.json"), &state, 2)
}

pub(crate) fn text_starter(ctx: &Context, id: &str, first: &str) -> Result<()> {
    let _state = ByteLock::acquire(&ctx.here.join("state.lock"), Duration::from_secs(60))?;
    let mut state = ctx.load_fresh()?;
    state["ask_creations"][id]["message_id"] = json!(first);
    state["ask_creations"][id]["starter_only"] = json!(false);
    atomic_json(&ctx.here.join("state.json"), &state, 2)
}

fn matches_starter(message: &Value, record: &Value) -> bool {
    let files = message["attachments"]
        .as_array()
        .into_iter()
        .flatten()
        .map(|a| json!({"filename":a["filename"],"size":a["size"]}))
        .collect::<Vec<_>>();
    message["author"]["id"] == record["sender"]
        && message["content"] == record["first"]
        && json!(files) == record["files"]
}

pub(crate) fn clear(state: &mut Value, id: &str) {
    if let Some(records) = state["ask_creations"].as_object_mut() {
        records.remove(id);
    }
}

pub(crate) fn creation_result(ctx: &Context, id: &str, response: Result<Value>) -> Result<Value> {
    if let Err(error) = &response {
        // An explicit rejection proves this POST did not create anything. Transport
        // failures and server errors remain uncertain and must be recovered.
        if definite_rejection(error) {
            let _state = ByteLock::acquire(&ctx.here.join("state.lock"), Duration::from_secs(60))?;
            let mut state = ctx.load_fresh()?;
            clear(&mut state, id);
            atomic_json(&ctx.here.join("state.json"), &state, 2)?;
        }
    }
    response
}

fn definite_rejection(error: &str) -> bool {
    Regex::new(r"^(Discord API POST \S+ failed|Discord upload failed): (400|401|403|404|413|429)\b")
        .unwrap()
        .is_match(error)
        || error == "Discord API rate limited"
        || error.starts_with("Discord upload rate limited")
}

fn recover_text(ctx: &mut Context, id: &str, record: &Value) -> Result<(String, String)> {
    let first = if !s(&record["message_id"]).is_empty() {
        s(&record["message_id"]).to_owned()
    } else {
        let messages = ctx.call(
            "GET",
            &format!("/channels/{}/messages?limit=100", s(&record["parent"])),
            None,
        )?;
        let matches: Vec<_> = messages
            .as_array()
            .ok_or("Discord message inventory is invalid")?
            .iter()
            .filter(|m| n(&m["id"]) > n(&record["before"]) && matches_starter(m, record))
            .collect();
        if matches.len() != 1 {
            return Err(
                "text starter outcome is unconfirmed; no duplicate starter was sent".into(),
            );
        }
        s(&matches[0]["id"]).to_owned()
    };
    text_starter(ctx, id, &first)?;
    // Discord gives a message-derived thread the source message ID, and permits
    // exactly one thread from that message. Its type-21 starter has no content.
    // https://github.com/discord/discord-api-docs/blob/main/developers/topics/threads.mdx
    let thread = match ctx.call("GET", &format!("/channels/{first}"), None) {
        Ok(thread) => thread,
        Err(error)
            if Regex::new(r"^Discord API GET \S+ failed: 404\b")
                .unwrap()
                .is_match(&error) =>
        {
            ctx.call(
                "POST",
                &format!(
                    "/channels/{}/messages/{first}/threads",
                    s(&record["parent"])
                ),
                Some(&json!({"name":record["question"]["title"]})),
            )?
        }
        Err(error) => return Err(error),
    };
    if thread["id"] != first
        || thread["guild_id"] != record["guild"]
        || thread["parent_id"] != record["parent"]
        || ![10, 11, 12].contains(&n(&thread["type"]))
    {
        return Err("recovered text thread is outside the original message context".into());
    }
    checkpoint(ctx, id, &first, &first)?;
    Ok((first.clone(), first))
}

pub(crate) fn recover(ctx: &mut Context, cwd: &str, raw_title: &str) -> Result<()> {
    let state = ctx.load_fresh()?;
    let pending: Vec<_> = state["ask_creations"]
        .as_object()
        .into_iter()
        .flat_map(|m| m.iter())
        .filter(|(_, r)| {
            similar(s(&r["question"]["ask_title"]), raw_title)
                && s(&r["question"]["cwd_key"]) == folder_key(cwd)
        })
        .map(|(id, r)| (id.clone(), r.clone()))
        .collect();
    for (id, record) in pending {
        if record["guild"] != ctx.cfg["guild_id"] || record["parent"] != ctx.cfg["task_channel_id"]
        {
            return Err("pending question belongs to different Discord settings".into());
        }
        let (thread, first) = if !s(&record["thread_id"]).is_empty() {
            (
                s(&record["thread_id"]).to_owned(),
                s(&record["message_id"]).to_owned(),
            )
        } else if !truth(&record["forum"]) {
            recover_text(ctx, &id, &record)?
        } else {
            let mut found = Vec::new();
            for t in inventory(ctx, s(&record["parent"]))? {
                if n(&t["id"]) <= n(&record["before"])
                    || t["owner_id"] != record["sender"]
                    || t["name"] != record["question"]["title"]
                {
                    continue;
                }
                let starter = ctx.call(
                    "GET",
                    &format!("/channels/{0}/messages/{0}", s(&t["id"])),
                    None,
                )?;
                if matches_starter(&starter, &record) {
                    found.push((s(&t["id"]).to_owned(), s(&starter["id"]).to_owned()));
                }
            }
            if found.len() != 1 {
                return Err(format!("question creation outcome is unconfirmed (reservation {id}, asked {}); inspect this reservation before retrying recovery without sending another question", s(&record["question"]["asked_at"])));
            }
            found.pop().unwrap()
        };
        if thread.is_empty() || first.is_empty() {
            return Err("pending question has no Discord ID".into());
        }
        let _state = ByteLock::acquire(&ctx.here.join("state.lock"), Duration::from_secs(60))?;
        let mut state = ctx.load_fresh()?;
        if !state["questions"]
            .as_array()
            .ok_or("questions missing")?
            .iter()
            .any(|q| s(&q["thread_id"]) == thread)
        {
            let mut q = record["question"].clone();
            q["thread_id"] = json!(thread);
            q["after"] = json!(first);
            state["questions"].as_array_mut().unwrap().push(q);
        }
        clear(&mut state, &id);
        atomic_json(&ctx.here.join("state.json"), &state, 2)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn production_discord_rejections_are_distinct_from_uncertain_delivery() {
        for e in [
            "Discord API POST /channels/T/threads failed: 403 Forbidden",
            "Discord API POST /channels/T/threads failed: 400 Bad request",
            "Discord upload failed: 413 Too big",
            "Discord API rate limited",
            "Discord upload rate limited; retry next tick",
        ] {
            assert!(definite_rejection(e), "{e}");
        }
        for e in [
            "Discord API POST /channels/T/threads failed: 500 Internal",
            "Discord upload failed: 502 Gateway",
            "Discord upload connection failed; check thread before retrying",
            "TimeoutExpired: HTTP request",
        ] {
            assert!(!definite_rejection(e), "{e}");
        }
    }
    #[test]
    fn titles_tolerate_agent_number_and_login_spelling() {
        assert!(similar(
            "[Codex] 【Game】Q12 Steam のサインイン",
            "【Game】Q13 Steam ログイン"
        ));
        assert!(similar(
            "【Genesis】税・返金・問い合わせの担当と方針",
            "【Genesis】税、返金、問い合わせの担当と方針の確認"
        ));
        assert!(!similar("【Game】Steamのサインイン", "【Game】Steamの購入"));
        assert!(!similar(
            "【Game】料金1000円の確認",
            "【Game】料金2000円の確認"
        ));
        assert!(!similar(
            "【GameA】Steamのサインイン",
            "【GameB】Steamの購入"
        ));
        assert!(!similar(
            "【GameA】Steamのサインイン",
            "【GameB】Steamのサインイン"
        ));
        assert!(!similar(
            "Steamの新しい料金と利用方法を無料で公開",
            "Steamの新しい料金と利用方法を有料で公開"
        ));
        assert!(!similar(
            "Steamの新しい料金と利用方法を千円にする",
            "Steamの新しい料金と利用方法を二千円にする"
        ));
    }
}
