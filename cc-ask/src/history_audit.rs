//! Explicit historical audit. No Discord writes, acknowledgements, or job launches.
//! The checkpoint contains only IDs/counts/evidence, never message or thread text.
use crate::core::*;
use serde_json::{json, Value};
use std::{
    collections::{BTreeMap, BTreeSet},
    path::PathBuf,
};

#[derive(clap::Args)]
pub struct Args {
    #[arg(long)]
    pub output: PathBuf,
    #[arg(long, default_value_t = 100)]
    pub limit: usize,
    /// Save missing owner replies in the existing receipt/question store without posting.
    #[arg(long)]
    pub capture: bool,
    /// Rediscover all archives and audit threads whose latest message changed.
    #[arg(long)]
    pub refresh: bool,
    /// Disjoint message-reading partitions. Use separate checkpoint paths.
    #[arg(long, default_value_t = 0)]
    pub partition: usize,
    #[arg(long, default_value_t = 1)]
    pub partitions: usize,
}

pub fn query_component(value: &str) -> String {
    value
        .bytes()
        .map(|b| match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                (b as char).to_string()
            }
            _ => format!("%{b:02X}"),
        })
        .collect()
}

pub fn in_partition(id: &str, partition: usize, partitions: usize) -> bool {
    partitions > 0
        && partition < partitions
        && id
            .parse::<u64>()
            .is_ok_and(|n| n % partitions as u64 == partition as u64)
}

fn guard(ctx: &mut Context, path: &str, cache: &mut BTreeMap<String, bool>) -> Result<bool> {
    if let Some(v) = cache.get(path) {
        return Ok(*v);
    }
    let out = ctx.effects.run(
        &[
            "D:/CC/Tools/client-records/scrub391.exe".into(),
            "--guard-path".into(),
            path.into(),
        ],
        15,
    )?;
    if out.code != 0 {
        return Err("client guard failed".into());
    }
    let v: Value =
        serde_json::from_str(&out.stdout).map_err(|_| "invalid client guard response")?;
    let protected = v["client_resource"]
        .as_bool()
        .ok_or("invalid client guard response")?;
    cache.insert(path.into(), protected);
    Ok(protected)
}

fn excluded(
    ctx: &mut Context,
    state: &Value,
    thread: &Value,
    names: &crate::project::ProjectNames,
    cache: &mut BTreeMap<String, bool>,
) -> Result<bool> {
    let id = s(&thread["id"]);
    let records: Vec<_> = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|q| s(&q["thread_id"]) == id)
        .chain(std::iter::once(&state["requests"][id]))
        .collect();
    for record in std::iter::once(thread).chain(records.iter().copied()) {
        for key in ["name", "title", "project", "cwd"] {
            let text = s(&record[key]);
            if names.redact(text) != text {
                return Ok(true);
            }
        }
    }
    for record in records {
        for candidate in [
            record,
            &record["job_info"],
            &record["continuation"],
            &record["continuation"]["job_info"],
        ] {
            let cwd = s(&candidate["cwd"]);
            if !cwd.is_empty() && guard(ctx, cwd, cache)? {
                return Ok(true);
            }
        }
    }
    Ok(false)
}

fn inventory(
    ctx: &mut Context,
    state: &Value,
    names: &crate::project::ProjectNames,
    cache: &mut BTreeMap<String, bool>,
) -> Result<Value> {
    let guild = s(&ctx.cfg["guild_id"]).to_owned();
    let task = s(&ctx.cfg["task_channel_id"]).to_owned();
    let me = ctx.call("GET", "/users/@me", None)?;
    if me["bot"] != true || s(&me["id"]).is_empty() {
        return Err("bot identity unavailable".into());
    }
    let known: BTreeSet<String> = state["questions"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|q| s(&q["source"]) != "routing")
        .map(|q| s(&q["thread_id"]).into())
        .collect();
    let logs_path = ctx.here.join("log_threads.json");
    let logs = if logs_path.is_file() {
        read_json(&logs_path)?
    } else {
        json!({})
    };
    let log_ids: BTreeSet<String> = logs
        .as_object()
        .into_iter()
        .flatten()
        .filter(|(label, _)| names.redact(label) == **label)
        .map(|(_, id)| s(id).into())
        .collect();
    let mut threads = BTreeMap::new();
    let active = ctx.call("GET", &format!("/guilds/{guild}/threads/active"), None)?;
    for t in active["threads"]
        .as_array()
        .ok_or("invalid active inventory")?
    {
        threads.insert(s(&t["id"]).to_owned(), t.clone());
    }
    let mut pages = 0;
    for channel in [REQUEST_CHANNEL_ID, task.as_str()] {
        if channel.is_empty() {
            return Err("task channel not configured".into());
        }
        let mut before = String::new();
        loop {
            let suffix = if before.is_empty() {
                String::new()
            } else {
                format!("&before={}", query_component(&before))
            };
            let page = ctx.call(
                "GET",
                &format!("/channels/{channel}/threads/archived/public?limit=100{suffix}"),
                None,
            )?;
            pages += 1;
            let ts = page["threads"]
                .as_array()
                .ok_or("invalid archived inventory")?;
            for t in ts {
                threads.insert(s(&t["id"]).into(), t.clone());
            }
            if page["has_more"] == false {
                break;
            }
            if page["has_more"] != true {
                return Err("missing archive completion flag".into());
            }
            let oldest = ts
                .last()
                .map(|t| s(&t["thread_metadata"]["archive_timestamp"]))
                .unwrap_or("");
            if oldest.is_empty() || (!before.is_empty() && oldest >= before.as_str()) {
                return Err("archive pagination did not advance".into());
            }
            before = oldest.into();
        }
    }
    let mut ids = known.clone();
    ids.extend(log_ids.iter().cloned());
    ids.extend(
        state["requests"]
            .as_object()
            .into_iter()
            .flatten()
            .map(|(id, _)| id.clone()),
    );
    let mut unavailable = vec![];
    for id in ids {
        if threads.contains_key(&id) {
            continue;
        }
        if excluded(ctx, state, &json!({"id":id}), names, cache)? {
            continue;
        }
        match ctx.call("GET", &format!("/channels/{id}"), None) {
            Ok(t) => {
                threads.insert(id, t);
            }
            Err(_) => unavailable.push(json!({"thread_id":id,"status":"metadata_unavailable"})),
        }
    }
    let mut selected = serde_json::Map::new();
    let mut clients = 0;
    let mut outside = 0;
    for (id, t) in threads {
        if ![10, 11, 12].contains(&n(&t["type"]))
            || t["guild_id"] != ctx.cfg["guild_id"]
            || !(known.contains(&id)
                || log_ids.contains(&id)
                || s(&t["parent_id"]) == REQUEST_CHANNEL_ID
                || (s(&t["parent_id"]) == task && t["owner_id"] == me["id"]))
        {
            outside += 1;
            continue;
        }
        if excluded(ctx, state, &t, names, cache)? {
            clients += 1;
            continue;
        }
        selected.insert(id.clone(), json!({"id":id,"last_message_id":t["last_message_id"],"archived":t["thread_metadata"]["archived"]}));
    }
    Ok(
        json!({"complete":unavailable.is_empty(),"archive_pages":pages,"threads":selected,"unavailable":unavailable,"excluded_clients":clients,"outside_scope":outside}),
    )
}

pub fn captured(state: &Value, routing: &Value, tid: &str, mid: &str) -> bool {
    !state["answer_receipts"][mid].is_null()
        || !routing["messages"][mid].is_null()
        || state["questions"]
            .as_array()
            .into_iter()
            .flatten()
            .filter(|q| s(&q["thread_id"]) == tid)
            .any(|q| {
                s(&q["owner_reply"]["id"]) == mid
                    || q["owner_replies"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .any(|r| s(&r["id"]) == mid)
            })
}

fn evidence(
    ctx: &mut Context,
    state: &Value,
    tid: &str,
    message: &Value,
    history: &[Value],
    names: &crate::project::ProjectNames,
    cache: &mut BTreeMap<String, bool>,
) -> Result<Value> {
    let mid = s(&message["id"]);
    let receipt = &state["answer_receipts"][mid];
    let mut candidates = BTreeSet::<String>::new();
    for key in ["handled_by", "read_by"] {
        if valid_job(s(&receipt[key])) {
            candidates.insert(s(&receipt[key]).into());
        }
    }
    let req = &state["requests"][tid];
    for r in std::iter::once(req).chain(
        state["questions"]
            .as_array()
            .into_iter()
            .flatten()
            .filter(|q| s(&q["thread_id"]) == tid),
    ) {
        for v in [
            &r["job"],
            &r["posting_job"],
            &r["scheduler_job"],
            &r["continuation"]["job"],
            &r["owner_update"]["job"],
        ] {
            if valid_job(s(v)) {
                candidates.insert(s(v).into());
            }
        }
    }
    let footer = regex::Regex::new(r"(?m)^-# ジョブ ([A-Za-z0-9_-]+)。").unwrap();
    for m in history.iter().filter(|m| truth(&m["author"]["bot"])) {
        for c in footer.captures_iter(s(&m["content"])) {
            candidates.insert(c[1].into());
        }
    }
    let mut workers = vec![];
    for jid in candidates {
        let root = ctx.jobs.join("jobs").join(&jid);
        if guard(ctx, &root.to_string_lossy(), cache)? {
            continue;
        }
        let info = job_info(ctx, &jid);
        let cwd = s(&info["cwd"]);
        if names.redact(cwd) != cwd || (!cwd.is_empty() && guard(ctx, cwd, cache)?) {
            continue;
        }
        let path = root.join("prompt.md");
        let prompt = if path.is_file() {
            read_text(&path)?
        } else {
            String::new()
        };
        let minute: String = s(&message["timestamp"]).chars().take(16).collect();
        let quote = format!(
            "### オーナー（{minute}）\n{}",
            safe_text(s(&message["content"]))
        );
        let explicit_quote = format!("オーナー投稿 {mid}（{}）の原文:", s(&message["timestamp"]));
        let input = thread_owner(&info, &prompt, tid)
            && crate::automation::epoch(&info["created_at"]) >= snowflake_time(mid)
            && (prompt.contains(&explicit_quote)
                || (!s(&message["content"]).is_empty() && prompt.contains(&quote)));
        let read = crate::steering::recorded_read(ctx, &jid, tid, message)?;
        let events_path = root.join("events.jsonl");
        let events = if events_path.is_file() {
            use std::io::{BufRead, BufReader};
            let reader = BufReader::new(
                std::fs::File::open(&events_path).map_err(|_| "worker events unavailable")?,
            );
            let mut executed = false;
            for line in reader.lines() {
                let line = line.map_err(|_| "worker events unavailable")?;
                if let Ok(event) = serde_json::from_str::<Value>(&line) {
                    if ["command_execution", "agent_message", "mcp_tool_call"]
                        .contains(&s(&event["item"]["type"]))
                    {
                        executed = true;
                        break;
                    }
                }
            }
            executed
        } else {
            false
        };
        let final_path = root.join("last_message.md");
        let final_text = if final_path.is_file() {
            read_text(&final_path)?
        } else {
            String::new()
        };
        let accepted = input || read;
        if accepted || s(&receipt["handled_by"]) == jid {
            workers.push(json!({"job":jid,"status":info["status"],"exact_input":input,"successful_reply_read":read,"events_present":events,"completed_result":s(&info["status"]) == "succeeded" && final_text.starts_with("状態: 完了")}));
        }
    }
    Ok(
        json!({"claimed_delivery":reply_delivered(state,mid),"proven_delivery":workers.iter().any(|w| (w["exact_input"] == true || w["successful_reply_read"] == true) && w["events_present"] == true),"worker_finished_after_reply":workers.iter().any(|w| (w["exact_input"] == true || w["successful_reply_read"] == true) && w["completed_result"] == true),"workers":workers}),
    )
}

pub fn summary(report: &Value) -> Value {
    let rows: Vec<_> = report["rows"]
        .as_object()
        .into_iter()
        .flatten()
        .filter(|(id, _)| report["inventory"]["threads"].get(id.as_str()).is_some())
        .map(|(_, v)| v)
        .collect();
    let replies: Vec<_> = rows
        .iter()
        .flat_map(|r| r["owner_messages"].as_array().into_iter().flatten())
        .collect();
    json!({"inventory_complete":report["inventory"]["complete"],"target_threads":report["inventory"]["threads"].as_object().map_or(0,|m|m.len()),"inspected_threads":rows.len(),"messages":rows.iter().map(|r|n(&r["message_count"])).sum::<u64>(),"owner_messages":replies.len(),"unacquired":replies.iter().filter(|m|m["captured_before"] == false).count(),"recovered":replies.iter().filter(|m|m["recovered"] == true).count(),"unproven_delivery":replies.iter().filter(|m|m["evidence"]["proven_delivery"] != true).count(),"worker_finished_after_reply":replies.iter().filter(|m|m["evidence"]["worker_finished_after_reply"] == true).count(),"excluded_clients":report["inventory"]["excluded_clients"],"unavailable":report["inventory"]["unavailable"].as_array().map_or(0,|v|v.len()),"errors":report["errors"].as_object().map_or(0,|m|m.len())})
}

pub fn run(ctx: &mut Context, args: &Args) -> Result<Value> {
    let _connections = ScopedHttpPool::new();
    if args.limit == 0
        || args.partitions == 0
        || args.partitions > 8
        || args.partition >= args.partitions
    {
        return Err("invalid audit limit or partition".into());
    }
    let state = ctx.load()?;
    let names = crate::project::ProjectNames::load(&ctx.client_projects)?;
    let mut cache = BTreeMap::new();
    let mut report = if args.output.is_file() {
        read_json(&args.output)?
    } else {
        json!({"version":1,"rows":{},"errors":{}})
    };
    if report["version"] != 1 {
        return Err("invalid audit checkpoint".into());
    }
    if report["partition"].is_number()
        && (n(&report["partition"]) != args.partition as u64
            || n(&report["partitions"]) != args.partitions as u64)
    {
        return Err("checkpoint belongs to a different audit partition".into());
    }
    report["partition"] = json!(args.partition);
    report["partitions"] = json!(args.partitions);
    if args.refresh || !report["inventory"].is_object() || report["inventory"]["complete"] != true {
        report["inventory"] = inventory(ctx, &state, &names, &mut cache)?;
        report["snapshot_at"] = json!(ctx.now());
        atomic_json(&args.output, &report, 2)?;
    }
    let routing_path = ctx.here.join("routing_state.json");
    let routing = if routing_path.is_file() {
        read_json(&routing_path)?
    } else {
        json!({})
    };
    let mut ids: Vec<_> = report["inventory"]["threads"]
        .as_object()
        .ok_or("invalid audit inventory")?
        .keys()
        .filter(|id| in_partition(id, args.partition, args.partitions))
        .cloned()
        .collect();
    ids.sort_by_key(|id| std::cmp::Reverse(id.parse::<u64>().unwrap_or(0)));
    let mut count = 0;
    for tid in ids {
        if count >= args.limit {
            break;
        }
        if report["rows"][&tid]["last_message_id"]
            == report["inventory"]["threads"][&tid]["last_message_id"]
            && report["rows"][&tid].is_object()
            && (!args.capture || report["rows"][&tid]["capture_requested"] == true)
        {
            continue;
        }
        count += 1;
        // Revalidate protection before any message read, even with a resumed checkpoint.
        let current = ctx.load()?;
        let thread = match ctx.call("GET", &format!("/channels/{tid}"), None) {
            Ok(v) => v,
            Err(_) => {
                report["errors"][&tid] = json!("metadata_unavailable");
                atomic_json(&args.output, &report, 2)?;
                continue;
            }
        };
        if excluded(ctx, &current, &thread, &names, &mut cache)? {
            report["errors"][&tid] = json!("protected_after_inventory");
            atomic_json(&args.output, &report, 2)?;
            continue;
        }
        let history = match thread_history(ctx, &tid) {
            Ok(v) => v,
            Err(_) => {
                report["errors"][&tid] = json!("history_unavailable");
                atomic_json(&args.output, &report, 2)?;
                continue;
            }
        };
        let owner = ctx.owner();
        let messages: Vec<_> = history
            .iter()
            .filter(|m| s(&m["author"]["id"]) == owner && !crate::snooze::control_only(m))
            .filter(|m| {
                let attachments = m["attachments"].to_string();
                names.redact(s(&m["content"])) == s(&m["content"])
                    && names.redact(&attachments) == attachments
            })
            .cloned()
            .collect();
        let mut owners = vec![];
        for message in &messages {
            let mid = s(&message["id"]);
            let previous = report["rows"][&tid]["owner_messages"]
                .as_array()
                .into_iter()
                .flatten()
                .find(|m| s(&m["id"]) == mid);
            let captured_before = previous
                .and_then(|m| m["captured_before"].as_bool())
                .unwrap_or_else(|| captured(&current, &routing, &tid, mid));
            let recovered = previous.is_some_and(|m| m["recovered"] == true)
                || s(&current["answer_receipts"][mid]["received_via"]) == "history-audit";
            owners.push(json!({"id":mid,"captured_before":captured_before,"recovered":recovered,"evidence":evidence(ctx,&current,&tid,message,&history,&names,&mut cache)?}));
        }
        if args.capture && !messages.is_empty() {
            let _work = crate::state_work::StateWork::begin(ctx)?;
            let mut fresh = ctx.load()?;
            // Protection can change during network I/O or while waiting for the writer.
            cache.clear();
            if excluded(ctx, &fresh, &thread, &names, &mut cache)? {
                report["errors"][&tid] = json!("protected_before_capture");
                atomic_json(&args.output, &report, 2)?;
                continue;
            }
            let missing: BTreeSet<String> = messages
                .iter()
                .filter(|m| !captured(&fresh, &routing, &tid, s(&m["id"])))
                .map(|m| s(&m["id"]).to_owned())
                .collect();
            record_owner_answers(&mut fresh, &tid, &messages, &owner);
            for (message, row) in messages.iter().zip(owners.iter_mut()) {
                let mid = s(&message["id"]);
                if missing.contains(mid) && fresh["answer_receipts"][mid].is_null() {
                    if !fresh["answer_receipts"].is_object() {
                        fresh["answer_receipts"] = json!({});
                    }
                    // Acquisition only: do not set handled_by, read_by, posted, or advance delivery cursors.
                    fresh["answer_receipts"][mid] = json!({"thread":tid,"received_at":ctx.now(),"received_via":"history-audit","owner_reply":{"id":message["id"],"timestamp":message["timestamp"],"content":safe_text(s(&message["content"])),"attachments":message["attachments"]}});
                }
                row["recovered"] = json!(
                    row["recovered"] == true
                        || (row["captured_before"] == false
                            && missing.contains(mid)
                            && captured(&fresh, &routing, &tid, mid))
                );
            }
            ctx.save(&fresh)?;
        }
        report["rows"][&tid] = json!({"last_message_id":thread["last_message_id"],"message_count":history.len(),"oldest_id":history.first().map(|m|&m["id"]),"newest_id":history.last().map(|m|&m["id"]),"owner_messages":owners,"capture_requested":args.capture,"audited_at":ctx.now()});
        if let Some(errors) = report["errors"].as_object_mut() {
            errors.remove(&tid);
        }
        report["summary"] = summary(&report);
        atomic_json(&args.output, &report, 2)?;
        if count % 10 == 0 {
            println!("{}", report["summary"]);
        }
    }
    report["summary"] = summary(&report);
    atomic_json(&args.output, &report, 2)?;
    Ok(report["summary"].clone())
}
