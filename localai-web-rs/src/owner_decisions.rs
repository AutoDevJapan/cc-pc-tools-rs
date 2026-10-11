//! Questions are counted by Discord message generation, never by blocked tasks.
//! Discord access is GET-only; neither receipts nor shared ToDos are mutated here.
use cc_ask::{n, s, truth};
use serde_json::{json, Value};
use std::{
    collections::{BTreeMap, BTreeSet},
    path::Path,
    sync::LazyLock,
};

fn array(v: &Value) -> impl Iterator<Item = &Value> {
    v.as_array().into_iter().flatten()
}
fn identity(q: &Value) -> u64 {
    let first = n(&q["question_message"]);
    if first > 0 {
        first
    } else {
        array(&q["message_ids"])
            .map(n)
            .filter(|id| *id > 0)
            .min()
            .unwrap_or_else(|| {
                let thread = n(&q["thread_id"]);
                if q["ask_title"].is_string()
                    && s(&q["source"]).is_empty()
                    && thread > 0
                    && thread <= n(&q["after"])
                {
                    thread
                } else {
                    n(&q["after"])
                }
            })
    }
}
fn end(q: &Value) -> u64 {
    array(&q["message_ids"])
        .map(n)
        .chain([identity(q)])
        .max()
        .unwrap_or(0)
}
fn valid_id(id: &str) -> bool {
    !id.is_empty() && id.bytes().all(|c| c.is_ascii_digit())
}
fn concrete(body: &str) -> bool {
    concrete_question(body)
}
/// Recognize explicit choices with a recommendation annotation, and inline a/b requests.
pub fn concrete_question(body: &str) -> bool {
    static ANNOTATION: LazyLock<regex::Regex> = LazyLock::new(|| {
        regex::Regex::new(
            r"(?m)^([ ]{0,3}(?:[-*] )?[a-d])(?:（[^）\n]{1,24}）|\([^\)\n]{1,24}\))([:：.)） ]|$)",
        )
        .unwrap()
    });
    let body = ANNOTATION.replace_all(body, "$1$2");
    cc_ask::questions::is_question(&body)
        && (cc_ask::concrete_question_body(&body)
            || cc_ask::choice_question_body(&body)
            || !body
                .lines()
                .next()
                .unwrap_or("")
                .trim()
                .starts_with("状態:"))
}
fn rfc_time(v: &Value) -> Option<u64> {
    chrono::DateTime::parse_from_rfc3339(s(v))
        .ok()
        .and_then(|d| u64::try_from(d.timestamp_millis()).ok())
}
fn recover_generation(q: &Value, history: &Value) -> Value {
    let mut q = q.clone();
    if n(&q["question_message"]) > 0 || array(&q["message_ids"]).next().is_some() {
        return q;
    }
    let Some(at) = rfc_time(&q["asked_at"]) else {
        return q;
    };
    let after = n(&q["after"]);
    let candidates: Vec<_> = array(&history["messages"])
        .filter(|m| m["bot"] == true && n(&m["id"]) <= after)
        .filter(|m| cc_ask::snowflake_time(s(&m["id"])) * 1000. >= at.saturating_sub(2000) as f64)
        .filter(|m| concrete(s(&m["content"])))
        .collect();
    let first = candidates.iter().map(|m| n(&m["id"])).min();
    if let Some(first) = first {
        q["question_message"] = json!(first.to_string());
        q["message_ids"] = json!([first.to_string()]);
        // Two concrete posts may be two questions. Order alone cannot join their generations.
        if candidates.len() > 1 {
            q["generation_unverified"] = json!(true);
            q["legacy_scope_end"] = json!(after);
        }
    }
    q
}
fn thread_from_url(value: &str) -> Option<String> {
    static RE: LazyLock<regex::Regex> = LazyLock::new(|| {
        regex::Regex::new(
            r"https://(?:www\.)?discord\.com/channels/[0-9]+/([0-9]+)(?:/[0-9]+)?(?:\b|/)",
        )
        .unwrap()
    });
    RE.captures(value).map(|c| c[1].to_owned())
}
fn task_threads(item: &Value) -> BTreeSet<String> {
    let mut out = BTreeSet::new();
    for key in [
        "resume_question_thread",
        "question_thread",
        "question_thread_id",
        "thread_id",
    ] {
        if valid_id(s(&item[key])) {
            out.insert(s(&item[key]).to_owned());
        }
    }
    if valid_id(s(&item["decision_wait"]["thread_id"])) {
        out.insert(s(&item["decision_wait"]["thread_id"]).to_owned());
    }
    for key in ["questionUrl", "question_url", "note"] {
        if let Some(thread) = thread_from_url(s(&item[key])) {
            out.insert(thread);
        }
    }
    out
}
struct Bindings<'a> {
    items: &'a [Value],
    threads: BTreeMap<String, BTreeSet<usize>>,
    jobs: BTreeMap<String, BTreeSet<usize>>,
    ids: BTreeMap<u64, BTreeSet<usize>>,
    private: BTreeSet<usize>,
    sessions: BTreeMap<String, BTreeSet<String>>,
    private_jobs: BTreeSet<String>,
}
impl<'a> Bindings<'a> {
    fn new(db: &'a Value, state: &Value, names: &cc_ask::task_title::Names) -> Self {
        static IDS: LazyLock<regex::Regex> =
            LazyLock::new(|| regex::Regex::new(r"[0-9]{16,20}").unwrap());
        static URLS: LazyLock<regex::Regex> = LazyLock::new(|| {
            regex::Regex::new(r"https://(?:www\.)?discord\.com/channels/\d+/(\d+)").unwrap()
        });
        let mut out = Self {
            items: db["items"].as_array().map(Vec::as_slice).unwrap_or(&[]),
            threads: BTreeMap::new(),
            jobs: BTreeMap::new(),
            ids: BTreeMap::new(),
            private: BTreeSet::new(),
            sessions: BTreeMap::new(),
            private_jobs: BTreeSet::new(),
        };
        for (index, item) in out.items.iter().enumerate() {
            let mut threads = task_threads(item);
            for cap in URLS.captures_iter(s(&item["note"])) {
                threads.insert(cap[1].into());
            }
            threads.extend(
                IDS.find_iter(s(&item["note"]))
                    .map(|m| m.as_str().to_owned()),
            );
            for tid in threads {
                out.threads.entry(tid).or_default().insert(index);
            }
            for jid in array(&item["jobs"]).map(s).chain([
                s(&item["job"]),
                s(&item["scheduler"]["job"]),
                s(&item["work_owner_job"]),
            ]) {
                if !jid.is_empty() {
                    out.jobs.entry(jid.into()).or_default().insert(index);
                }
            }
            out.ids.entry(n(&item["id"])).or_default().insert(index);
            if private_task(names, db, item) {
                out.private.insert(index);
            }
        }
        for job in array(&state["operations_jobs"]) {
            let mut linked = BTreeSet::new();
            for id in array(&job["todo_ids"]).map(n).chain([n(&job["todo_id"])]) {
                if id > 0 {
                    linked.extend(out.ids.get(&id).into_iter().flatten().copied());
                }
            }
            if !s(&job["id"]).is_empty() {
                out.jobs
                    .entry(s(&job["id"]).into())
                    .or_default()
                    .extend(&linked);
            }
            if job["private"] == true
                || linked.iter().any(|i| out.private.contains(i))
                || [s(&job["cwd"]), s(&job["project"])]
                    .iter()
                    .any(|value| names.project(value) == "クライアント案件")
            {
                out.private_jobs.insert(s(&job["id"]).into());
            }
            for session in [s(&job["thread_id"]), s(&job["resume_session"])] {
                if !session.is_empty() {
                    out.sessions
                        .entry(session.into())
                        .or_default()
                        .insert(s(&job["id"]).into());
                }
            }
        }
        out
    }
    fn indices(&self, q: &Value) -> BTreeSet<usize> {
        let mut found = self
            .threads
            .get(s(&q["thread_id"]))
            .cloned()
            .unwrap_or_default();
        // Legacy notes often point to a question message inside an existing thread.
        for message in array(&q["message_ids"]).map(n).chain([identity(q)]) {
            found.extend(self.threads.get(&message.to_string()).into_iter().flatten());
        }
        for id in array(&q["todo_ids"]).map(n).chain([n(&q["todo_id"])]) {
            if id > 0 {
                found.extend(self.ids.get(&id).into_iter().flatten());
            }
        }
        for jid in [
            s(&q["posting_job"]),
            s(&q["progress_job"]),
            s(&q["continuation"]["job"]),
        ]
        .into_iter()
        .chain(
            self.sessions
                .get(s(&q["session_id"]))
                .into_iter()
                .flatten()
                .map(String::as_str),
        ) {
            if !jid.is_empty() {
                found.extend(self.jobs.get(jid).into_iter().flatten());
            }
        }
        found.retain(|index| {
            let specific = n(&self.items[*index]["decision_wait"]["message_id"]);
            specific == 0 || specific == identity(q)
        });
        found
    }
    fn related(&self, q: &Value) -> Vec<&'a Value> {
        self.indices(q)
            .iter()
            .map(|i| &self.items[*i])
            .filter(|i| s(&i["status"]) != "done")
            .collect()
    }
    fn private_job(&self, q: &Value) -> bool {
        [
            s(&q["posting_job"]),
            s(&q["progress_job"]),
            s(&q["continuation"]["job"]),
        ]
        .into_iter()
        .chain(
            self.sessions
                .get(s(&q["session_id"]))
                .into_iter()
                .flatten()
                .map(String::as_str),
        )
        .any(|jid| self.private_jobs.contains(jid))
    }
}
fn private_task(names: &cc_ask::task_title::Names, db: &Value, item: &Value) -> bool {
    [
        s(&item["project"]),
        s(&db["projects"][s(&item["project"])]),
        s(&item["cwd"]),
        s(&item["spec"]),
    ]
    .iter()
    .any(|v| names.project(v) == "クライアント案件")
}
fn private_question(
    names: &cc_ask::task_title::Names,
    db: &Value,
    q: &Value,
    bindings: &Bindings<'_>,
) -> bool {
    [
        s(&q["project"]),
        s(&q["cwd"]),
        s(&db["projects"][s(&q["project"])]),
    ]
    .iter()
    .any(|v| names.project(v) == "クライアント案件")
        || bindings
            .indices(q)
            .iter()
            .any(|i| bindings.private.contains(i))
        || bindings.private_job(q)
}
pub(crate) fn job_metadata(
    jobs: &[Value],
    names: &cc_ask::task_title::Names,
    db: &Value,
) -> Vec<Value> {
    let aliases: BTreeSet<_> = db["projects"]
        .as_object()
        .into_iter()
        .flatten()
        .filter(|(name, path)| {
            names.project(name) == "クライアント案件"
                || names.project(s(path)) == "クライアント案件"
        })
        .map(|(name, _)| name.to_ascii_lowercase())
        .collect();
    jobs.iter().map(|j|json!({"id":j["id"],"thread_id":j["thread_id"],"resume_session":j["resume_session"],"todo_id":j["todo_id"],"todo_ids":j["todo_ids"],
        "private":(aliases.contains(&s(&j["project"]).to_ascii_lowercase()) || [s(&j["cwd"]),s(&j["project"])].iter().any(|p|names.project(p)=="クライアント案件"))})).collect()
}
fn time(id: u64) -> Value {
    if id == 0 {
        return Value::Null;
    }
    json!(cc_ask::snowflake_time(&id.to_string()) * 1000.)
}

/// Explicit structured controls take precedence over legacy blocked status.
pub fn task_control(item: &Value) -> Option<&'static str> {
    let explicit = match s(&item["decision_wait"]["kind"]) {
        "deferred" => Some("deferred"),
        "owner_stopped" => Some("owner_stopped"),
        "technical" => Some("technical"),
        _ => None,
    };
    if explicit.is_some() {
        return explicit;
    }
    for line in s(&item["note"]).lines() {
        match line.trim() {
            "停止区分: 明示延期" => return Some("deferred"),
            "停止区分: オーナー停止" => return Some("owner_stopped"),
            "停止区分: 技術復旧待ち" => return Some("technical"),
            _ => {}
        }
    }
    if s(&item["status"]) == "wait" || array(&item["wait_on"]).next().is_some() {
        Some("dependency")
    } else {
        None
    }
}

/// Project each generation independently. A reply before its message cannot answer it.
pub fn project(
    db: &Value,
    state: &Value,
    live: &Value,
    names: &cc_ask::task_title::Names,
    now: u64,
) -> Value {
    let known = state["questions"].is_array()
        && db["items"].is_array()
        && db["projects"].is_object()
        && state["jobs_source_ok"] != false;
    let mut linked_state = state.clone();
    if linked_state.is_object()
        && !linked_state["operations_jobs"].is_array()
        && live["operations_jobs"].is_array()
    {
        linked_state["operations_jobs"] = live["operations_jobs"].clone();
    }
    let bindings = Bindings::new(db, &linked_state, names);
    let mut records: BTreeMap<(String, u64), Value> = BTreeMap::new();
    let generations: Vec<_> = array(&state["questions"])
        .map(|q| recover_generation(q, &live["threads"][s(&q["thread_id"])]))
        .collect();
    for q in &generations {
        let tid = s(&q["thread_id"]);
        if valid_id(tid) && identity(q) > 0 && s(&q["source"]) != "routing" {
            let key = (tid.to_owned(), identity(q));
            let relevant = truth(&q["open"]) || !bindings.related(q).is_empty();
            if relevant {
                records
                    .entry(key)
                    .and_modify(|old| {
                        // Duplicate ledger rows are the same question; prefer its latest cursor/answer.
                        if end(q) >= end(old) {
                            *old = q.clone();
                        }
                    })
                    .or_insert_with(|| q.clone());
            }
        }
    }
    for (id, creation) in state["ask_creations"].as_object().into_iter().flatten() {
        let tid = s(&creation["thread_id"]);
        let history = &live["threads"][tid];
        if history["ok"] != true {
            continue;
        }
        if let Some(m) = array(&history["messages"])
            .filter(|m| {
                m["bot"] == true
                    && m["content"] == creation["first"]
                    && n(&m["id"]) > n(&creation["before"])
            })
            .max_by_key(|m| n(&m["id"]))
        {
            let mut q = creation["question"].clone();
            if !q.is_object() {
                q = json!({});
            }
            q["thread_id"] = json!(tid);
            q["question_message"] = m["id"].clone();
            q["message_ids"] = json!([m["id"]]);
            q["open"] = json!(true);
            q["creation_id"] = json!(id);
            records.entry((tid.into(), n(&m["id"]))).or_insert(q);
        }
    }
    // Include concrete follow-ups discovered on Discord even if the ledger closed early.
    for (tid, history) in live["threads"].as_object().into_iter().flatten() {
        let earliest = records
            .keys()
            .filter(|(t, _)| t == tid)
            .map(|(_, id)| *id)
            .min()
            .unwrap_or_else(|| {
                array(&state["questions"])
                    .filter(|q| s(&q["thread_id"]) == tid)
                    .map(identity)
                    .max()
                    .unwrap_or(u64::MAX)
                    .saturating_add(1)
            });
        for message in array(&history["messages"]) {
            if n(&message["id"]) >= earliest
                && message["bot"] == true
                && concrete(s(&message["content"]))
            {
                let mid = n(&message["id"]);
                let previous = records
                    .iter()
                    .rfind(|((t, id), _)| t == tid && *id <= mid)
                    .map(|(_, q)| q.clone());
                let mut q = previous
                    .or_else(|| {
                        array(&state["questions"])
                            .filter(|q| s(&q["thread_id"]) == tid)
                            .max_by_key(|q| identity(q))
                            .cloned()
                    })
                    .unwrap_or_else(|| json!({"thread_id":tid}));
                // Multipart continuation messages belong to the recorded first message.
                if array(&q["message_ids"]).any(|id| n(id) == mid) {
                    continue;
                }
                q["question_message"] = message["id"].clone();
                q["after"] = message["id"].clone();
                q["message_ids"] = json!([message["id"]]);
                q["open"] = json!(true);
                if mid > n(&q["legacy_scope_end"]) {
                    q.as_object_mut().unwrap().remove("generation_unverified");
                }
                for key in [
                    "owner_reply",
                    "owner_replies",
                    "answered_reply",
                    "seen_reply",
                    "remind_not_before",
                    "owner_stopped",
                    "stopped",
                    "cancelled",
                    "resume_status_reply",
                    "last_resume_error",
                ] {
                    q.as_object_mut().unwrap().remove(key);
                }
                records.entry((tid.clone(), mid)).or_insert(q);
            }
        }
    }
    let mut questions = Vec::new();
    let mut task_states: BTreeMap<u64, BTreeSet<String>> = BTreeMap::new();
    for ((tid, mid), q) in &records {
        let history = &live["threads"][tid];
        let checked = history["ok"] == true
            && q["generation_unverified"] != true
            && history["completeAfter"]
                .as_u64()
                .is_none_or(|lower| lower <= *mid)
            && history["checkedAt"]
                .as_u64()
                .is_some_and(|at| at <= now + 60_000 && now.saturating_sub(at) < 600_000);
        let message = array(&history["messages"]).find(|m| n(&m["id"]) == *mid);
        let posted_parts: Vec<_> = array(&history["messages"])
            .filter(|m| {
                n(&m["id"]) == *mid || array(&q["message_ids"]).any(|id| n(id) == n(&m["id"]))
            })
            .map(|m| s(&m["content"]))
            .collect();
        let posted_body = posted_parts.join("\n");
        // A cursor or a generic judgment-needed result is not a delivered concrete question.
        let sent = message.is_some_and(|m| m["bot"] == true) && concrete(&posted_body);
        let boundary = *mid;
        let edited_at = array(&history["messages"])
            .filter(|m| {
                n(&m["id"]) == *mid || array(&q["message_ids"]).any(|id| n(id) == n(&m["id"]))
            })
            .filter_map(|m| rfc_time(&m["editedAt"]))
            .max()
            .unwrap_or(0);
        let next = records
            .keys()
            .filter(|(t, id)| t == tid && *id > boundary)
            .map(|(_, id)| *id)
            .min()
            .unwrap_or(u64::MAX);
        let answers = array(&q["owner_replies"]).chain(std::iter::once(&q["owner_reply"]));
        let recorded_answer = answers
            .filter(|m| n(&m["id"]) > boundary && n(&m["id"]) < next)
            .max_by_key(|m| n(&m["id"]));
        let live_answer = array(&history["messages"])
            .filter(|m| m["owner"] == true && m["controlOnly"] != true)
            .filter(|m| cc_ask::snowflake_time(s(&m["id"])) * 1000. >= edited_at as f64)
            .filter(|m| {
                n(&m["id"]) > boundary
                    && (n(&m["id"]) < next
                        || n(&m["replyTo"]) == *mid
                        || array(&q["message_ids"]).any(|id| n(id) == n(&m["replyTo"])))
            })
            .filter(|m| {
                n(&m["replyTo"]) == 0
                    || n(&m["replyTo"]) == *mid
                    || array(&q["message_ids"]).any(|id| n(id) == n(&m["replyTo"]))
            })
            .max_by_key(|m| n(&m["id"]));
        let later_unscoped_answer = next < u64::MAX
            && array(&history["messages"]).any(|m| {
                m["owner"] == true
                    && m["controlOnly"] != true
                    && n(&m["id"]) > next
                    && n(&m["replyTo"]) == 0
            });
        // Saved thread-wide answers omit reply references. They are receipt hints only.
        let answer = if checked && sent { live_answer } else { None };
        let rid = answer.map(|a| n(&a["id"])).unwrap_or(0);
        let receipt = &state["answer_receipts"][rid.to_string()];
        let delivered = rid > 0
            && s(&receipt["thread"]) == tid
            && cc_ask::reply_delivered(state, &rid.to_string());
        let deferred = q["remind_not_before"]
            .as_f64()
            .is_some_and(|at| at * 1000. > now as f64);
        let stopped = [q, &state["requests"][tid]].iter().any(|r| {
            ["owner_stopped", "stopped", "cancelled"]
                .iter()
                .any(|key| truth(&r[*key]))
        });
        let status = if !checked || !sent {
            if recorded_answer.is_some() {
                "answer_received_unverified"
            } else {
                "delivery_unverified"
            }
        } else if stopped {
            "owner_stopped"
        } else if deferred && rid == 0 {
            "deferred"
        } else if rid > 0 && delivered && q["open"] == false {
            "answered"
        } else if rid > 0 && delivered {
            "applying"
        } else if rid > 0
            && (q["resume_status_reply"] == json!(rid.to_string())
                && truth(&q["last_resume_error"])
                || (state["requests"][tid]["resume_status_reply"] == json!(rid.to_string())
                    && truth(&state["requests"][tid]["last_resume_error"]))
                || (s(&receipt["thread"]) == tid && truth(&receipt["last_error"])))
        {
            "answer_delivery_failed"
        } else if rid > 0 {
            "answer_received"
        } else if later_unscoped_answer {
            "answer_scope_unverified"
        } else if checked && !sent {
            "delivery_unverified"
        } else if checked && sent {
            "unanswered"
        } else {
            "delivery_unverified"
        };
        let related = bindings.related(q);
        let ids: Vec<_> = related.iter().map(|i| n(&i["id"])).collect();
        for item in &related {
            task_states
                .entry(n(&item["id"]))
                .or_default()
                .insert(status.into());
        }
        let protected =
            !known || private_question(names, db, q, &bindings) || history["protected"] == true;
        let body = if protected {
            "クライアント案件の質問（内容は非表示）".to_owned()
        } else {
            crate::console_ui::clean(
                names,
                if posted_parts.is_empty() {
                    s(&q["title"])
                } else {
                    &posted_body
                },
            )
        };
        let guild = s(&history["guild"]);
        let url = if protected {
            None
        } else if valid_id(guild) {
            Some(format!("https://discord.com/channels/{guild}/{tid}/{mid}"))
        } else {
            related
                .iter()
                .find_map(|i| crate::console_ui::question_url(i))
        };
        questions.push(json!({"id":format!("{tid}:{mid}"),"threadId":if protected {Value::Null}else{json!(tid)},"messageId":if protected {Value::Null}else{json!(mid.to_string())},
            "generation":if protected {Value::Null}else{json!(format!("{mid}:{edited_at}"))},"question":body,"questionUrl":url,"state":status,
            "sent":sent,"deliveryVerified":checked && sent,"verificationComplete":checked && sent,"checkedAt":history["checkedAt"],
            "askedAt":time(*mid),"replyAt":time(rid),"replyId":if protected || rid==0 {Value::Null}else{json!(rid.to_string())},
            "answer":if protected{String::new()}else{answer.map(|a|crate::console_ui::clean(names,s(&a["content"]))).unwrap_or_default()},
            "answerDelivered":delivered,"taskIds":ids,"taskCount":ids.len(),"protected":protected,"closed":q["open"]==false}));
    }
    for (id, creation) in state["ask_creations"].as_object().into_iter().flatten() {
        let q = &creation["question"];
        let mid = n(&creation["message_id"]);
        if records.contains_key(&(s(&creation["thread_id"]).to_owned(), mid))
            || records.values().any(|q| q["creation_id"] == *id)
            || !concrete(s(&creation["first"]))
        {
            continue;
        }
        let protected = !known || private_question(names, db, q, &bindings);
        let ids: Vec<_> = bindings
            .related(q)
            .into_iter()
            .map(|i| n(&i["id"]))
            .collect();
        for id in &ids {
            task_states
                .entry(*id)
                .or_default()
                .insert("unverified".into());
        }
        questions.push(json!({"id":format!("creation:{id}"),"question":if protected {"クライアント案件の質問（内容は非表示）".into()}else{crate::console_ui::clean(names,s(&creation["first"]))},
            "state":"send_unknown","sent":false,"deliveryVerified":false,"verificationComplete":false,"questionUrl":null,"taskIds":ids,"taskCount":ids.len(),"protected":protected}));
    }
    // A concrete unsent question needs its own durable identity and body. A vague block is unknown.
    for item in array(&db["items"]).filter(|i| s(&i["status"]) == "block") {
        let pending = &item["decision_wait"];
        if !s(&pending["id"]).is_empty()
            && concrete(s(&pending["question"]))
            && !task_states.contains_key(&n(&item["id"]))
        {
            let key = format!("unsent:{}", s(&pending["id"]));
            let id = n(&item["id"]);
            task_states.entry(id).or_default().insert("unsent".into());
            if let Some(q) = questions.iter_mut().find(|q| q["id"] == key) {
                q["taskIds"].as_array_mut().unwrap().push(json!(id));
                q["taskCount"] = json!(q["taskIds"].as_array().unwrap().len());
            } else {
                let private = !known || private_task(names, db, item);
                questions.push(json!({"id":key,"question":if private {"クライアント案件の質問（内容は非表示）".into()}else{crate::console_ui::clean(names,s(&pending["question"]))},
                    "state":if s(&pending["delivery_status"])=="failed"{"send_failed"}else{"unsent"},"sent":false,"deliveryVerified":false,"questionUrl":null,"taskIds":[id],"taskCount":1,"protected":private}));
            }
        }
    }
    let mut tasks = Vec::new();
    for item in array(&db["items"]).filter(|i| s(&i["status"]) != "done") {
        let states = task_states.get(&n(&item["id"]));
        let kind = task_control(item).unwrap_or_else(|| {
            if states.is_some_and(|s| s.contains("unanswered")) {
                "owner"
            } else if states.is_some_and(|s| s.contains("unsent")) {
                "unsent"
            } else if states.is_some_and(|s| {
                s.contains("answer_received")
                    || s.contains("applying")
                    || s.contains("answer_delivery_failed")
                    || s.contains("answer_received_unverified")
            }) {
                "answer_received"
            } else if states.is_some_and(|s| s.contains("owner_stopped")) {
                "owner_stopped"
            } else if states.is_some_and(|s| s.contains("deferred")) {
                "deferred"
            } else if states.is_some_and(|s| s.contains("answered")) {
                "answered"
            } else if s(&item["status"]) == "block" {
                "unverified"
            } else {
                "none"
            }
        });
        if kind != "none" {
            tasks.push(json!({"id":n(&item["id"]),"kind":kind}));
        }
    }
    let count = |status: &str| questions.iter().filter(|q| q["state"] == status).count();
    let blocked_task_states: Vec<_> = array(&db["items"])
        .filter(|i| s(&i["status"]) == "block")
        .map(|i| {
            tasks
                .iter()
                .find(|t| t["id"] == i["id"])
                .cloned()
                .unwrap_or_else(|| json!({"id":i["id"],"kind":"unverified"}))
        })
        .collect();
    let mut stopped_counts = BTreeMap::<String, usize>::new();
    for t in &blocked_task_states {
        *stopped_counts.entry(s(&t["kind"]).into()).or_default() += 1;
    }
    let pending_threads = array(&state["questions"])
        .map(|q| s(&q["thread_id"]))
        .filter(|id| valid_id(id))
        .collect::<BTreeSet<_>>()
        .iter()
        .filter(|id| {
            let h = &live["threads"][**id];
            h["ok"] != true
                || n(&h["completeAfter"]) > 0
                || h["checkedAt"]
                    .as_u64()
                    .is_none_or(|at| now.saturating_sub(at) >= 600_000)
        })
        .count();
    let complete = known
        && live["checkedAt"].is_number()
        && pending_threads == 0
        && count("answer_scope_unverified") == 0
        && questions
            .iter()
            .filter(|q| q["askedAt"].is_number() || q["state"] == "send_unknown")
            .all(|q| q["verificationComplete"] == true);
    json!({"sourceOk":complete,"checkedAt":live["checkedAt"],"questions":questions,"tasks":tasks,"blockedTasks":blocked_task_states,
        "metrics":{"ownerDecisions":if complete {json!(count("unanswered"))}else{Value::Null},"confirmedUnansweredQuestions":if known {json!(count("unanswered"))}else{Value::Null},"ownerDecisionsComplete":complete,
        "ownerDecisionTasks":if known {json!(tasks.iter().filter(|t|t["kind"]=="owner").count())}else{Value::Null},
        "unsentQuestions":if known {json!(count("unsent")+count("send_failed"))}else{Value::Null},
        "sendFailedQuestions":count("send_failed"),"answerReceivedQuestions":count("answer_received"),"applyingQuestions":count("applying"),
        "answerDeliveryFailedQuestions":count("answer_delivery_failed"),"answeredQuestions":count("answered"),"deferredQuestions":count("deferred"),"ownerStoppedQuestions":count("owner_stopped"),
        "answerScopeUnverifiedQuestions":count("answer_scope_unverified"),"historyInspectionPendingThreads":pending_threads,"unverifiedQuestions":count("delivery_unverified")+count("answer_received_unverified")+count("send_unknown")+count("answer_scope_unverified"),"unclassifiedBlockedTasks":tasks.iter().filter(|t|t["kind"]=="unverified").count(),
        "blockedTasksByKind":stopped_counts,"blockedTasks":array(&db["items"]).filter(|i|s(&i["status"])=="block").count()}})
}

/// Fetch histories only on trusted question threads, through the existing bot adapter.
/// Client paths are guarded before accessing Discord. The cache stays in memory.
pub fn inspect(root: &Path, previous: &Value) -> Value {
    let _deadline = cc_ask::tick_runtime::Deadline::new(std::time::Duration::from_secs(90));
    let _pool = cc_ask::ScopedHttpPool::new();
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64;
    let Ok(names) = cc_ask::task_title::Names::load(&root.join("client-projects.txt")) else {
        return Value::Null;
    };
    let Ok(db) = cc_ask::read_json(&root.join("todo/todo.json")) else {
        return Value::Null;
    };
    if !db["items"].is_array() || !db["projects"].is_object() {
        return Value::Null;
    }
    let Ok(mut state) = cc_ask::read_json(&root.join("discord-ask/state.json")) else {
        return Value::Null;
    };
    let Some(home) = std::env::var_os("USERPROFILE").or_else(|| std::env::var_os("HOME")) else {
        return Value::Null;
    };
    let Ok(cfg) =
        cc_ask::read_json(&std::path::PathBuf::from(home).join(".godd/discord-notify.json"))
    else {
        return Value::Null;
    };
    let mut ctx = cc_ask::Context {
        here: root.join("discord-ask"),
        client_projects: root.join("client-projects.txt"),
        civ6_home: root.join("civ6-live"),
        jobs: root.join("codex-jobs"),
        discord_tasks: cc_ask::discord_tasks_dir().unwrap_or_default(),
        todo: root.join("todo/todo.json"),
        cfg,
        effects: Box::new(cc_ask::RealEffects),
        codex_command: "".into(),
        scheduler_command: "".into(),
        shepherd_command: "".into(),
        todo_command: "".into(),
    };
    let mut threads = serde_json::Map::new();
    let Ok(jobs) = cc_ask::automation::jobs(&ctx) else {
        return Value::Null;
    };
    let metadata = job_metadata(&jobs, &names, &db);
    state["operations_jobs"] = json!(metadata);
    let bindings = Bindings::new(&db, &state, &names);
    let mut candidates: BTreeMap<String, Value> = BTreeMap::new();
    for q in array(&state["questions"]) {
        if valid_id(s(&q["thread_id"])) && s(&q["source"]) != "routing" && identity(q) > 0 {
            let tid = s(&q["thread_id"]);
            let active = truth(&q["open"]) || !bindings.related(q).is_empty();
            candidates
                .entry(tid.into())
                .and_modify(|old| {
                    let was = truth(&old["inspection_active"]);
                    if identity(q) > identity(old) {
                        *old = q.clone();
                    }
                    old["inspection_active"] = json!(was || active);
                })
                .or_insert_with(|| {
                    let mut q = q.clone();
                    q["inspection_active"] = json!(active);
                    q
                });
        }
    }
    for creation in state["ask_creations"]
        .as_object()
        .into_iter()
        .flat_map(|m| m.values())
    {
        let tid = s(&creation["thread_id"]);
        if !valid_id(tid) {
            continue;
        }
        let mut q = creation["question"].clone();
        if !q.is_object() {
            q = json!({});
        }
        q["thread_id"] = json!(tid);
        q["question_message"] = creation["message_id"].clone();
        q["inspection_active"] = json!(true);
        candidates.entry(tid.into()).or_insert(q);
    }
    let mut candidates: Vec<_> = candidates.into_iter().collect();
    candidates.sort_by_key(|(tid, q)| {
        (
            !truth(&q["inspection_active"]),
            previous["threads"][tid]["lastAttemptAt"]
                .as_u64()
                .unwrap_or(0),
        )
    });
    let started = std::time::Instant::now();
    for (tid, q) in candidates {
        if cc_ask::tick_runtime::check().is_err() {
            break;
        }
        if truth(&q["inspection_active"]) && started.elapsed() > std::time::Duration::from_secs(45)
        {
            continue;
        }
        if private_question(&names, &db, &q, &bindings)
            || array(&state["questions"])
                .filter(|other| s(&other["thread_id"]) == tid)
                .any(|other| private_question(&names, &db, other, &bindings))
        {
            threads.insert(tid, json!({"protected":true,"lastAttemptAt":now}));
            continue;
        }
        let _thread_deadline =
            cc_ask::tick_runtime::Deadline::new(std::time::Duration::from_secs(8));
        let cwd = s(&q["cwd"]);
        if !cwd.is_empty() {
            let argv = vec![
                root.join("client-records/scrub391.exe")
                    .to_string_lossy()
                    .into_owned(),
                "--guard-path".into(),
                cwd.into(),
            ];
            let allowed = cc_ask::run_hidden(&argv, 5)
                .ok()
                .filter(|r| r.code == 0)
                .and_then(|r| serde_json::from_str::<Value>(&r.stdout).ok())
                .is_some_and(|v| v["client_resource"] == false);
            if !allowed {
                threads.insert(tid, json!({"protected":true,"lastAttemptAt":now}));
                continue;
            }
        }
        let result = (|| -> cc_ask::Result<Value> {
            let channel = ctx.call("GET", &format!("/channels/{tid}"), None)?;
            if channel["guild_id"] != ctx.cfg["guild_id"]
                || ![10, 11, 12].contains(&n(&channel["type"]))
            {
                return Err("untrusted thread".into());
            }
            let owner = ctx.owner();
            let lower = state["questions"]
                .as_array()
                .into_iter()
                .flatten()
                .filter(|q| s(&q["thread_id"]) == tid)
                .filter(|q| truth(&q["open"]) || !bindings.related(q).is_empty())
                .map(|q| {
                    if identity(q) == n(&q["after"])
                        && n(&q["question_message"]) == 0
                        && array(&q["message_ids"]).next().is_none()
                    {
                        rfc_time(&q["asked_at"])
                            .map(|at| at.saturating_sub(1420070402000) << 22)
                            .unwrap_or(0)
                    } else {
                        identity(q)
                    }
                })
                .min()
                .unwrap_or(identity(&q));
            let (history, complete_after) = bounded_history(&mut ctx, &tid, lower)?;
            let messages:Vec<_> = history.into_iter().map(|m|json!({"id":m["id"],"content":m["content"],
                "owner":s(&m["author"]["id"])==owner,"bot":m["author"]["bot"]==true,
                "replyTo":m["message_reference"]["message_id"],"editedAt":m["edited_timestamp"],"controlOnly":cc_ask::snooze::control_only(&m)})).collect();
            Ok(
                json!({"ok":true,"checkedAt":now,"lastAttemptAt":now,"guild":channel["guild_id"],"completeAfter":complete_after,"messages":messages}),
            )
        })();
        let value = result.unwrap_or_else(|_| {
            let mut old = previous["threads"][&tid].clone();
            if !old.is_object() {
                old = json!({});
            }
            old["ok"] = json!(false);
            old["lastAttemptAt"] = json!(now);
            old
        });
        threads.insert(tid, value);
    }
    for (tid, old) in previous["threads"].as_object().into_iter().flatten() {
        threads.entry(tid.clone()).or_insert_with(|| old.clone());
    }
    json!({"checkedAt":now,"threads":threads,"operations_jobs":metadata})
}

fn bounded_history(
    ctx: &mut cc_ask::Context,
    thread: &str,
    lower: u64,
) -> cc_ask::Result<(Vec<Value>, u64)> {
    let mut messages = BTreeMap::new();
    let mut before = 0;
    for _ in 0..5 {
        let suffix = if before > 0 {
            format!("&before={before}")
        } else {
            String::new()
        };
        let result = ctx.call(
            "GET",
            &format!("/channels/{thread}/messages?limit=100{suffix}"),
            None,
        );
        let page = match result {
            Ok(v) => v.as_array().cloned().ok_or("invalid history")?,
            Err(e) if messages.is_empty() => return Err(e),
            Err(_) => break,
        };
        let oldest = page.iter().map(|m| n(&m["id"])).min().unwrap_or(0);
        if oldest > 0 && before > 0 && oldest >= before {
            return Err("history cursor did not advance".into());
        }
        let length = page.len();
        for m in page {
            messages.insert(n(&m["id"]), m);
        }
        if length < 100 || oldest <= lower {
            if lower == thread.parse::<u64>().unwrap_or(0) && !messages.contains_key(&lower) {
                if let Ok(starter) = ctx.call(
                    "GET",
                    &format!("/channels/{thread}/messages/{thread}"),
                    None,
                ) {
                    if n(&starter["id"]) == lower {
                        messages.insert(lower, starter);
                    }
                }
            }
            return Ok((messages.into_values().collect(), 0));
        }
        before = oldest;
    }
    Ok((messages.into_values().collect(), before))
}

#[cfg(test)]
mod tests {
    use super::*;
    fn names() -> cc_ask::task_title::Names {
        cc_ask::task_title::Names::parse("SecretClient\n")
    }
    fn db() -> Value {
        json!({"projects":{"demo":"D:/public"},"items":[
        {"id":1,"project":"demo","status":"block","resume_question_thread":"123"},
        {"id":2,"project":"demo","status":"block","resume_question_thread":"123"}]})
    }
    fn history(messages: Value) -> Value {
        json!({"checkedAt":1000,"threads":{"123":{"ok":true,"checkedAt":1000,"guild":"99","messages":messages}}})
    }
    #[test]
    fn counts_one_delivered_question_and_two_tasks_without_duplicate_ledger_rows() {
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":true},{"thread_id":"123","after":"100","open":true}]});
        let live = history(
            json!([{"id":"100","bot":true,"content":"公開してよいか決めてください。a 公開 b 非公開"}]),
        );
        let out = project(&db(), &state, &live, &names(), 1000);
        assert_eq!(out["metrics"]["ownerDecisions"], 1);
        assert_eq!(out["metrics"]["ownerDecisionTasks"], 2);
        assert_eq!(
            out["questions"][0]["questionUrl"],
            "https://discord.com/channels/99/123/100"
        );
    }
    #[test]
    fn message_reference_in_note_links_task_to_question_inside_thread() {
        let mut db = db();
        db["items"] = json!([{"id":7,"project":"demo","status":"block","note":"質問123456789012345678を待ちます"}]);
        let state = json!({"questions":[{"thread_id":"123","question_message":"123456789012345678","after":"123456789012345678","open":true}]});
        let live = history(
            json!([{"id":"123456789012345678","bot":true,"content":"公開を決めてください。a 公開 b 非公開"}]),
        );
        let out = project(&db, &state, &live, &names(), 1000);
        assert_eq!(out["metrics"]["ownerDecisionTasks"], 1);
        assert_eq!(out["questions"][0]["taskIds"], json!([7]));
    }
    #[test]
    fn recommendation_annotation_and_inline_choice_are_concrete_without_generic_status() {
        assert!(concrete("状態: 要判断\na（推奨）: 公開\nb: 非公開"));
        assert!(concrete(
            "上記1と2について、それぞれa/bで返信してください。"
        ));
        assert!(!concrete("状態: 要判断\nジョブの結果に判断が必要です。"));
        assert!(!concrete("状態: 完了\na（推奨）: 公開\nb: 非公開"));
    }
    #[test]
    fn old_reply_does_not_answer_new_generation_even_if_receipt_or_closed_marker_remains() {
        let state = json!({"questions":[{"thread_id":"123","question_message":"300","after":"300","open":false,"owner_reply":{"id":"200"},"answered_reply":"200"}],"answer_receipts":{"200":{"thread":"123","handled_by":"old"}}});
        let live = history(
            json!([{"id":"200","owner":true,"content":"a"},{"id":"300","bot":true,"content":"新しい案を決めてください。a 公開 b 非公開"}]),
        );
        let out = project(&db(), &state, &live, &names(), 1000);
        assert_eq!(out["metrics"]["ownerDecisions"], 1);
        assert!(out["questions"][0]["replyId"].is_null());
        assert_eq!(out["questions"][0]["state"], "unanswered");
    }
    #[test]
    fn discovers_unregistered_followup_after_answer_and_keeps_it_visible() {
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":false}],"answer_receipts":{"200":{"thread":"123","read_by":"worker"}}});
        let live = history(
            json!([{"id":"100","bot":true,"content":"公開を決めてください。a 公開 b 非公開"},
            {"id":"200","owner":true,"content":"a"},{"id":"300","bot":true,"content":"追加予算を決めてください。a 使う b 使わない"}]),
        );
        let out = project(&db(), &state, &live, &names(), 1000);
        assert_eq!(out["metrics"]["ownerDecisions"], 1);
        assert!(
            array(&out["questions"]).any(|q| q["messageId"] == "300" && q["state"] == "unanswered")
        );
    }
    #[test]
    fn missing_or_stale_delivery_evidence_is_unknown_not_an_invented_zero() {
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":true}]});
        let out = project(&db(), &state, &Value::Null, &names(), 1000);
        assert!(out["metrics"]["ownerDecisions"].is_null());
        assert_eq!(out["metrics"]["unverifiedQuestions"], 1);
        assert_eq!(out["metrics"]["unclassifiedBlockedTasks"], 2);
        let stale = project(
            &db(),
            &state,
            &history(json!([{"id":"100","bot":true}])),
            &names(),
            700000,
        );
        assert_eq!(stale["questions"][0]["state"], "delivery_unverified");
    }
    #[test]
    fn reply_received_delivery_failed_and_applied_are_separate_from_unanswered() {
        let mut state = json!({"questions":[{"thread_id":"123","after":"100","open":true,"resume_status_reply":"200","last_resume_error":"failed"}]});
        let live = history(
            json!([{"id":"100","bot":true,"content":"予算を決めてください。a 使う b 使わない"},{"id":"200","owner":true,"content":"a"}]),
        );
        let out = project(&db(), &state, &live, &names(), 1000);
        assert_eq!(out["questions"][0]["state"], "answer_delivery_failed");
        assert_eq!(out["metrics"]["ownerDecisions"], 0);
        state["answer_receipts"] = json!({"200":{"thread":"123","handled_by":"worker"}});
        let out = project(&db(), &state, &live, &names(), 1000);
        assert_eq!(out["questions"][0]["state"], "applying");
        state["questions"][0]["open"] = json!(false);
        assert_eq!(
            project(&db(), &state, &live, &names(), 1000)["questions"][0]["state"],
            "answered"
        );
    }
    #[test]
    fn pending_concrete_questions_deduplicate_and_vague_blocks_remain_unclassified() {
        let db = json!({"projects":{},"items":[
            {"id":1,"status":"block","decision_wait":{"id":"budget","question":"予算を決めてください。a 使う b 使わない"}},
            {"id":2,"status":"block","decision_wait":{"id":"budget","question":"予算を決めてください。a 使う b 使わない"}},
            {"id":3,"status":"block","note":"ジョブの結果に判断が必要"},
            {"id":4,"status":"block","wait_on":[5]},
            {"id":6,"status":"block","decision_wait":{"kind":"technical"}},
            {"id":7,"status":"block","decision_wait":{"kind":"deferred"}}]});
        let out = project(
            &db,
            &json!({"questions":[]}),
            &json!({"checkedAt":1000}),
            &names(),
            1000,
        );
        assert_eq!(out["metrics"]["unsentQuestions"], 1);
        assert_eq!(out["questions"][0]["taskCount"], 2);
        assert_eq!(out["metrics"]["ownerDecisions"], 0);
        assert_eq!(out["metrics"]["unclassifiedBlockedTasks"], 1);
    }
    #[test]
    fn protected_questions_have_no_content_reply_id_or_link() {
        let db = json!({"projects":{"alias":"D:/SecretClient"},"items":[{"id":1,"project":"alias","status":"block","resume_question_thread":"123"}]});
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":true,"cwd":"D:/SecretClient","title":"do-not-display"}]});
        let out = project(
            &db,
            &state,
            &history(json!([{"id":"100","bot":true,"content":"do-not-display"}])),
            &names(),
            1000,
        );
        assert!(!out.to_string().contains("do-not-display"));
        assert!(out["questions"][0]["questionUrl"].is_null());
        assert!(out["questions"][0]["messageId"].is_null());
    }
    #[test]
    fn missing_todo_source_hides_question_body_and_link() {
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":true,"title":"private-body"}]});
        let out = project(
            &Value::Null,
            &state,
            &history(
                json!([{"id":"100","bot":true,"content":"private-body を選んでください。a 公開 b 非公開"}]),
            ),
            &names(),
            1000,
        );
        assert!(!out.to_string().contains("private-body"));
        assert!(out["questions"][0]["questionUrl"].is_null());
        assert!(out["metrics"]["ownerDecisions"].is_null());
    }
    #[test]
    fn multipart_reply_between_parts_belongs_to_first_post() {
        let state = json!({"questions":[{"thread_id":"123","after":"300","message_ids":["100","300"],"open":true}]});
        let live = history(
            json!([{"id":"100","bot":true,"content":"予算を決めてください。a 使う b 使わない"},{"id":"200","owner":true,"content":"a"},{"id":"300","bot":true,"content":"補足です"}]),
        );
        let out = project(&db(), &state, &live, &names(), 1000);
        assert_eq!(out["questions"][0]["messageId"], "100");
        assert_eq!(out["questions"][0]["replyId"], "200");
        assert_eq!(out["metrics"]["confirmedUnansweredQuestions"], 0);
    }
    #[test]
    fn closed_thread_without_open_todos_still_discovers_followup() {
        let mut db = db();
        for i in db["items"].as_array_mut().unwrap() {
            i["status"] = json!("done");
        }
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":false}]});
        let live = history(
            json!([{"id":"200","owner":true,"content":"a"},{"id":"300","bot":true,"content":"新しい公開範囲を決めてください。a 広く b 狭く"}]),
        );
        let out = project(&db, &state, &live, &names(), 1000);
        assert_eq!(out["metrics"]["confirmedUnansweredQuestions"], 1);
        assert_eq!(out["questions"][0]["taskCount"], 0);
    }
    #[test]
    fn reference_lost_in_saved_answers_is_unverified_not_another_generations_answer() {
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":true,"owner_reply":{"id":"400"}},
            {"thread_id":"123","after":"300","open":true,"owner_reply":{"id":"400"}}],"answer_receipts":{"400":{"thread":"123","read_by":"worker"}}});
        let live = history(
            json!([{"id":"100","bot":true,"content":"公開を決めてください。a 公開 b 非公開"},{"id":"300","bot":true,"content":"追加予算を決めてください。a 使う b 使わない"},{"id":"400","owner":true,"replyTo":"100","content":"a"}]),
        );
        let checked = project(&db(), &state, &live, &names(), 1000);
        assert_eq!(checked["metrics"]["confirmedUnansweredQuestions"], 1);
        let mut failed = live;
        failed["threads"]["123"]["ok"] = json!(false);
        let unknown = project(&db(), &state, &failed, &names(), 1000);
        assert!(unknown["metrics"]["ownerDecisions"].is_null());
        assert!(array(&unknown["questions"])
            .all(|q| q["state"] != "answered" && q["state"] != "applying"));
    }
    #[test]
    fn edit_after_old_reply_and_deleted_reply_do_not_answer_current_question() {
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":true,"owner_reply":{"id":"200"}}]});
        let live = history(
            json!([{"id":"100","bot":true,"content":"変更した予算を決めてください。a 使う b 使わない","editedAt":"2015-01-01T00:00:01Z"},{"id":"200","owner":true,"content":"a"}]),
        );
        assert_eq!(
            project(&db(), &state, &live, &names(), 1000)["metrics"]
                ["confirmedUnansweredQuestions"],
            1
        );
        let deleted = history(
            json!([{"id":"100","bot":true,"content":"予算を決めてください。a 使う b 使わない"}]),
        );
        assert_eq!(
            project(&db(), &state, &deleted, &names(), 1000)["metrics"]
                ["confirmedUnansweredQuestions"],
            1
        );
    }
    #[test]
    fn creation_with_lost_response_is_delivery_unknown_not_unsent() {
        let state = json!({"questions":[],"ask_creations":{"one":{"question":{"todo_id":1},"first":"予算を決めてください。a 使う b 使わない"}}});
        let out = project(&db(), &state, &json!({"checkedAt":1000}), &names(), 1000);
        assert_eq!(out["metrics"]["unsentQuestions"], 0);
        assert_eq!(out["questions"][0]["state"], "send_unknown");
        assert!(out["metrics"]["ownerDecisions"].is_null());
    }
    #[test]
    fn legacy_restore_never_merges_independent_followup_into_answered_question() {
        let state = json!({"questions":[{"thread_id":"100","after":"300","open":true,"asked_at":"2015-01-01T00:00:00Z"}]});
        let live = json!({"checkedAt":1000,"threads":{"100":{"ok":true,"checkedAt":1000,"guild":"99","messages":[
            {"id":"100","bot":true,"content":"公開を決めてください。a 公開 b 非公開"},{"id":"200","owner":true,"content":"a"},{"id":"300","bot":true,"content":"追加予算を決めてください。a 使う b 使わない"}]}}});
        let out = project(
            &json!({"projects":{},"items":[]}),
            &state,
            &live,
            &names(),
            1000,
        );
        assert!(array(&out["questions"]).any(|q| q["messageId"] == "300"
            && q["state"] != "answered"
            && q["state"] != "applying"));
        assert!(out["metrics"]["ownerDecisions"].is_null());
    }
    #[test]
    fn job_alias_and_job_side_todo_reference_protect_question() {
        let db = json!({"projects":{"alias":"D:/SecretClient"},"items":[{"id":7,"project":"alias","status":"block"}]});
        let metadata = job_metadata(
            &[
                json!({"id":"j","project":"ALIAS","cwd":"D:/public","todo_id":7,"thread_id":"session"}),
            ],
            &names(),
            &db,
        );
        assert_eq!(metadata[0]["private"], true);
        let state = json!({"questions":[{"thread_id":"123","after":"100","open":true,"session_id":"session"}],"operations_jobs":metadata});
        let out = project(
            &db,
            &state,
            &history(
                json!([{"id":"100","bot":true,"content":"private-bodyを決めてください。a 公開 b 非公開"}]),
            ),
            &names(),
            1000,
        );
        assert_eq!(out["questions"][0]["taskCount"], 1);
        assert!(!out.to_string().contains("private-body"));
    }
}
