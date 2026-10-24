//! Finite delivery for ONE explicitly enrolled new recovery question.
//! No legacy ledger, recovered question inventory, prompts or job-wide scan is read.
//! The existing resident invokes tick; only actual new owner replies can launch AI.
use crate::core::{self, ByteLock, Context, Result};
use crate::recovery_messages::{self, LogArgs, RepliesArgs};
use clap::Subcommand;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    fs,
    io::Write,
    path::{Path, PathBuf},
    time::Duration,
};

const FILE: &str = "recovery-followup.json";
const DOMAIN: &str = "new-owner-recovery-followup";
const LIMIT: usize = 8 * 1024 * 1024;

#[derive(Subcommand, Debug)]
pub enum Command {
    /// Explicitly enroll exactly one verified new question. Does not send or launch.
    Register {
        #[arg(long)]
        config: PathBuf,
    },
    /// One bounded observation; called by the existing 120-second resident.
    Tick,
    /// Read the independent evidence and delivery receipts without effects.
    Inspect,
    /// Record explicit receipt by the assigned live handler; never marks applied.
    Claim {
        #[arg(long)]
        message: String,
        #[arg(long)]
        handler: String,
    },
    /// Handler reports actual implementation and verification, never just acceptance.
    Applied {
        #[arg(long)]
        message: String,
        #[arg(long)]
        result_file: PathBuf,
    },
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Watch {
    pub request_id: String,
    pub thread: String,
    pub question_message: String,
    pub todo_id: u64,
    pub work_todo: u64,
    pub parent_lock: PathBuf,
    pub worker_cwd: PathBuf,
    /// Only these exact replies are already assigned to the original parent.
    pub parent_messages: Vec<String>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Record {
    reply: Value,
    acquired_at: String,
    acquisition_source: String,
    key: String,
    phase: String,
    parent_managed: bool,
    job: Option<String>,
    result: Option<Value>,
    progress_reported: bool,
    #[serde(default)]
    steering_handler: Option<String>,
    #[serde(default)]
    steering_received: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct State {
    domain: String,
    version: u32,
    watch: Watch,
    original_question: String,
    original_decision: Value,
    decision_identity: Value,
    owner: String,
    question_timestamp: String,
    progress_thread: String,
    replies: BTreeMap<String, Record>,
    scheduled_observations: u64,
    last_observed_at: Option<String>,
}

fn read(path: &Path) -> Result<Value> {
    cc_job_policy::todo_store::read_bounded(path, LIMIT)
        .map_err(|_| "followup evidence unavailable or malformed".into())
}
fn publish(path: &Path, value: &impl Serialize) -> Result<()> {
    let bytes = serde_json::to_vec_pretty(value).map_err(|_| "followup encoding failed")?;
    if bytes.len() > LIMIT {
        return Err("followup storage limit exceeded; evidence retained".into());
    }
    let mut temp = tempfile::Builder::new()
        .prefix("recovery-followup.")
        .suffix(".tmp")
        .tempfile_in(path.parent().ok_or("followup storage directory missing")?)
        .map_err(|_| "followup staging failed")?;
    temp.write_all(&bytes)
        .and_then(|_| temp.as_file().sync_all())
        .map_err(|_| "followup flush failed")?;
    core::replace_file(temp.path(), path)?;
    #[cfg(not(windows))]
    fs::File::open(path.parent().unwrap())
        .and_then(|f| f.sync_all())
        .map_err(|_| "followup directory flush failed")?;
    Ok(())
}
fn id(value: &str) -> Result<u64> {
    if value.is_empty() || !value.bytes().all(|c| c.is_ascii_digit()) {
        return Err("followup message identity invalid".into());
    }
    value
        .parse::<u64>()
        .ok()
        .filter(|id| *id > 0)
        .ok_or_else(|| "followup message identity invalid".into())
}
fn key(state: &State, message: &str) -> String {
    let bytes = serde_json::to_vec(&json!([
        DOMAIN,
        state.watch.request_id,
        state.watch.thread,
        state.watch.question_message,
        state.watch.todo_id,
        state.watch.work_todo,
        state.decision_identity,
        message
    ]))
    .unwrap();
    format!("{:x}", Sha256::digest(bytes))[..32].into()
}
fn load(home: &Path) -> Result<Option<State>> {
    match fs::metadata(home.join(FILE)) {
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(_) => return Err("followup state unavailable".into()),
        Ok(_) => (),
    }
    let state: State = serde_json::from_value(read(&home.join(FILE))?)
        .map_err(|_| "followup state schema differs")?;
    if state.domain != DOMAIN || state.version != 1 {
        return Err("followup namespace/version differs".into());
    }
    validate_watch(&state.watch)?;
    for (mid, record) in &state.replies {
        validate_reply(&state, &record.reply)?;
        if core::s(&record.reply["message_id"]) != mid
            || record.key != key(&state, mid)
            || record
                .job
                .as_deref()
                .is_some_and(|jid| !core::valid_job(jid))
            || record
                .steering_handler
                .as_deref()
                .is_some_and(|jid| jid != "parent" && !core::valid_job(jid))
            || !matches!(
                record.phase.as_str(),
                "reserved" | "parent_pending" | "launched" | "result_pending" | "applied"
            )
        {
            return Err("followup record association differs".into());
        }
    }
    Ok(Some(state))
}
fn validate_watch(watch: &Watch) -> Result<()> {
    id(&watch.thread)?;
    id(&watch.question_message)?;
    if watch.request_id.is_empty()
        || watch.todo_id == 0
        || watch.work_todo == 0
        || watch.work_todo == watch.todo_id
        || !watch.worker_cwd.is_absolute()
        || !watch.parent_lock.is_absolute()
        || watch.parent_messages.len() > 10
    {
        return Err("exact followup binding required".into());
    }
    for mid in &watch.parent_messages {
        id(mid)?;
    }
    Ok(())
}
fn acquire(home: &Path) -> Result<ByteLock> {
    ByteLock::acquire(&home.join("recovery-followup.lock"), Duration::ZERO)
}
fn evidence(ctx: &mut Context, watch: &Watch) -> Result<Value> {
    recovery_messages::replies(
        ctx,
        &RepliesArgs {
            thread: watch.thread.clone(),
            message: Some(watch.question_message.clone()),
            request_id: Some(watch.request_id.clone()),
            todo_id: Some(watch.todo_id),
            read_only: true,
        },
    )
}
fn verify_evidence(state: &State, value: &Value) -> Result<()> {
    if value["domain"] != "new-owner-recovery-messages"
        || value["source"] != "discord-api-v10"
        || value["read_only"] != true
        || value["request_id"] != state.watch.request_id
        || value["thread_id"] != state.watch.thread
        || value["question_message_id"] != state.watch.question_message
        || value["owner_id"] != state.owner
        || value["question_timestamp"] != state.question_timestamp
        || value["original_question"] != state.original_question
        || value["original_decision"] != state.original_decision
        || value["decision_identity"] != state.decision_identity
        || value["progress_thread_id"] != state.progress_thread
        || !value["owner_replies"].is_array()
    {
        return Err("followup source/generation/original scope differs".into());
    }
    Ok(())
}
fn validate_reply(state: &State, reply: &Value) -> Result<()> {
    if reply["request_id"] != state.watch.request_id
        || reply["todo_id"] != state.watch.todo_id
        || reply["question_thread_id"] != state.watch.thread
        || reply["question_message_id"] != state.watch.question_message
        || reply["author_id"] != state.owner
        || reply["decision_identity"] != state.decision_identity
        || id(core::s(&reply["message_id"]))? <= id(&state.watch.question_message)?
        || !reply["body"].is_string()
        || core::s(&reply["body"]).len() > 128 * 1024
    {
        return Err("followup reply owner/generation/scope differs".into());
    }
    let time = chrono::DateTime::parse_from_rfc3339(core::s(&reply["timestamp"]))
        .map_err(|_| "followup reply time invalid")?;
    if time
        < chrono::DateTime::parse_from_rfc3339(&state.question_timestamp)
            .map_err(|_| "followup question time invalid")?
    {
        return Err("followup reply predates question".into());
    }
    Ok(())
}
fn observe(state: &mut State, value: &Value, at: &str) -> Result<()> {
    verify_evidence(state, value)?;
    for reply in value["owner_replies"].as_array().unwrap() {
        validate_reply(state, reply)?;
        let mid = core::s(&reply["message_id"]);
        if let Some(old) = state.replies.get(mid) {
            if old.reply != *reply {
                return Err(
                    "followup reply edited; original evidence retained, no automatic redispatch"
                        .into(),
                );
            }
            continue;
        }
        let record = Record {
            reply: reply.clone(),
            acquired_at: at.into(),
            acquisition_source: "discord-api-v10".into(),
            key: key(state, mid),
            phase: "reserved".into(),
            parent_managed: state.watch.parent_messages.iter().any(|id| id == mid),
            job: None,
            result: None,
            progress_reported: false,
            steering_handler: None,
            steering_received: false,
        };
        state.replies.insert(mid.into(), record);
    }
    Ok(())
}
/// Registration cannot silently change the target or forget previously delivered replies.
pub fn register(ctx: &mut Context, config: &Path) -> Result<Value> {
    let watch: Watch =
        serde_json::from_value(read(config)?).map_err(|_| "followup configuration invalid")?;
    validate_watch(&watch)?;
    let _lock = acquire(&ctx.here)?;
    if let Some(old) = load(&ctx.here)? {
        if old.watch != watch {
            return Err("followup already enrolled; target/scope cannot change".into());
        }
        return Ok(report(&old));
    }
    let value = evidence(ctx, &watch)?;
    let replies = value["owner_replies"]
        .as_array()
        .ok_or("followup reply array missing")?;
    if watch
        .parent_messages
        .iter()
        .any(|mid| !replies.iter().any(|r| r["message_id"] == *mid))
    {
        return Err("parent assignment requires exact API-verified owner reply".into());
    }
    let identity = value["original_decision"].clone();
    if identity["todo_id"] != watch.todo_id
        || identity["scope"]["original_work_todo"] != watch.work_todo
    {
        return Err("followup original work scope differs".into());
    }
    let decision_identity = value["decision_identity"].clone();
    if !decision_identity.is_object() {
        return Err("followup normalized scope missing".into());
    }
    let mut state = State {
        domain: DOMAIN.into(),
        version: 1,
        watch,
        original_question: core::s(&value["original_question"]).into(),
        original_decision: identity,
        decision_identity,
        owner: core::s(&value["owner_id"]).into(),
        question_timestamp: core::s(&value["question_timestamp"]).into(),
        progress_thread: core::s(&value["progress_thread_id"]).into(),
        replies: BTreeMap::new(),
        scheduled_observations: 0,
        last_observed_at: None,
    };
    id(&state.owner)?;
    id(&state.progress_thread)?;
    if state.progress_thread == state.watch.thread {
        return Err("separate verified silent progress thread required".into());
    }
    observe(&mut state, &value, &core::timestamp())?;
    publish(&ctx.here.join(FILE), &state)?;
    Ok(report(&state))
}
fn todo_item(ctx: &Context, work: u64) -> Result<Value> {
    let db = core::read_json(&ctx.todo)?;
    let items = db["items"]
        .as_array()
        .ok_or("followup shared ToDo unavailable")?;
    let matches: Vec<_> = items.iter().filter(|v| v["id"] == work).collect();
    let [item] = matches.as_slice() else {
        return Err("followup exact ToDo missing or ambiguous".into());
    };
    Ok((*item).clone())
}
/// Add only this question's receipt, retaining every unrelated field and original note.
fn annotate(ctx: &Context, state: &State, mid: &str) -> Result<()> {
    let _lock = ByteLock::acquire(&ctx.todo.with_extension("lock"), Duration::from_secs(1))?;
    let mut db = core::read_json(&ctx.todo)?;
    let items = db["items"]
        .as_array_mut()
        .ok_or("followup shared ToDo unavailable")?;
    if items
        .iter()
        .filter(|v| v["id"] == state.watch.work_todo)
        .count()
        != 1
    {
        return Err("followup exact work ToDo missing or ambiguous".into());
    }
    let item = items
        .iter_mut()
        .find(|v| v["id"] == state.watch.work_todo)
        .unwrap();
    if item
        .get("recovery_owner_replies")
        .is_some_and(|v| !v.is_object())
    {
        return Err("followup ToDo receipt schema differs".into());
    }
    if !item["recovery_owner_replies"].is_object() {
        item["recovery_owner_replies"] = json!({});
    }
    let record = &state.replies[mid];
    let receipt = json!({"domain":DOMAIN,"request_id":state.watch.request_id,"question_thread_id":state.watch.thread,
        "question_message_id":state.watch.question_message,"reply_id":mid,"owner_id":state.owner,
        "launch_key":record.key,"evidence_file":ctx.here.join(FILE),"phase":record.phase,"job":record.job});
    if item["recovery_owner_replies"][&record.key] == receipt {
        return Ok(());
    }
    item["recovery_owner_replies"][&record.key] = receipt;
    cc_job_policy::todo_store::write(&ctx.todo, &db).map_err(|e| e.to_string())
}
fn parent_active(ctx: &Context, state: &State) -> Result<bool> {
    match fs::metadata(&state.watch.parent_lock) {
        Ok(_) => return Ok(true),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => (),
        Err(_) => return Err("followup parent lock unavailable; launch deferred".into()),
    }
    // Exact original-work associations only. Never scan all jobs or their prompts.
    let item = todo_item(ctx, state.watch.work_todo)?;
    for jid in item["jobs"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .chain(item["scheduler"]["job"].as_str())
    {
        if !core::valid_job(jid) {
            return Err("followup work job identity invalid".into());
        }
        let job = read(&ctx.jobs.join("jobs").join(jid).join("job.json"))?;
        if ["queued", "running", "starting", "yielding"].contains(&core::s(&job["status"])) {
            return Ok(true);
        }
    }
    Ok(false)
}
fn guard_worker(ctx: &mut Context, state: &State) -> Result<()> {
    let root = ctx.here.parent().ok_or("followup tools root unavailable")?;
    let result = ctx.effects.run_json(
        &[
            root.join("client-records/scrub391.exe")
                .display()
                .to_string(),
            "--guard-path".into(),
            state.watch.worker_cwd.display().to_string(),
        ],
        5,
    )?;
    if result["client_resource"] != false || result["ok"] != true {
        return Err("followup worker scope protected or unknown".into());
    }
    Ok(())
}
fn prompt(state: &State, record: &Record) -> String {
    format!("元の作業{}に、正規Discord APIで確認した本人返信を反映してください。取得した履歴を直接チャット入力と偽装せず、元の質問と許可対象を維持してください。a/bは元の質問本文の意味で読みます。\n\
既存担当の成果を保存し、実装・検査・別モデルレビュー・CI・必要な既存実行版反映まで追ってください。受領や起動だけで完了にしません。\n\
原文と元範囲（取得元discord-api-v10）:\n{}\n\
実反映後に discord_ask.exe recovery-followup applied --message {} --result-file <JSON> を実行してください。JSONには handler、revision、verification、summary、applied_at を含めます。handlerはこのジョブIDです。結果は別の通知なし進捗へ返され、判断ToDo{}だけが完了します。\n\
作業の区切りで独立recovery-followup.jsonの新ステアリングを確認し、割当て済みの返信は recovery-followup claim --message <返信ID> --handler <自分のジョブID> で受領して元の作業へ反映してください。受領は実反映の完了ではありません。\n\
旧state.json/question-recovery-required.jsonの変更や消失した質問の再送は禁止。対象外案件と秘密値を取得・出力しない。APIキー従量課金なし。ドメイン事業者はRakkoだけ。\n",
        state.watch.work_todo, serde_json::to_string_pretty(&json!({"original_question":state.original_question,
            "original_decision":state.original_decision,"reply":record.reply,"acquired_at":record.acquired_at,
            "launch_key":record.key,"progress_thread_id":state.progress_thread})).unwrap(),
        core::s(&record.reply["message_id"]), state.watch.todo_id)
}
fn deliver(ctx: &mut Context, state: &mut State, mid: &str) -> Result<()> {
    if matches!(
        state.replies[mid].phase.as_str(),
        "applied" | "result_pending"
    ) {
        return Ok(());
    }
    if state.replies[mid].job.is_some() {
        return annotate(ctx, state, mid);
    }
    if state.replies[mid].parent_managed || parent_active(ctx, state)? {
        return save_steering(ctx, state, mid, None);
    }
    // Another reply assigned to a live worker in this very scope is retained as
    // steering. It cannot create a concurrent application job.
    for record in state.replies.values().filter(|r| r.phase == "launched") {
        if let Some(jid) = &record.job {
            let job = read(&ctx.jobs.join("jobs").join(jid).join("job.json"))?;
            if ["queued", "running", "starting", "yielding"].contains(&core::s(&job["status"])) {
                let target = jid.clone();
                return save_steering(ctx, state, mid, Some(&target));
            }
        }
    }
    guard_worker(ctx, state)?;
    let record = &state.replies[mid];
    let path = ctx
        .here
        .join(format!("recovery-followup-{}.txt", record.key));
    // Reservation and complete source are flushed BEFORE the standard launch key.
    publish(&ctx.here.join(FILE), state)?;
    let mut file = fs::File::create(&path).map_err(|_| "followup prompt staging failed")?;
    file.write_all(prompt(state, record).as_bytes())
        .and_then(|_| file.sync_all())
        .map_err(|_| "followup prompt flush failed")?;
    drop(file);
    // Recheck the current parent's marker immediately before launch. The native
    // launcher's --todo-id recheck/publication is atomic under queue -> ToDo
    // locks, shared with all standard parent/worker registrations and admission.
    if parent_active(ctx, state)? {
        return deliver(ctx, state, mid);
    }
    let args = vec![
        "run".into(),
        "--name".into(),
        format!("owner-reply-{}-{}", state.watch.work_todo, mid),
        "--todo-id".into(),
        state.watch.work_todo.to_string(),
        "--cwd".into(),
        state.watch.worker_cwd.display().to_string(),
        "--prompt-file".into(),
        path.display().to_string(),
        "--launch-key".into(),
        record.key.clone(),
        "--task-type".into(),
        "implementation".into(),
        "--sandbox".into(),
        "danger-full-access".into(),
        "--mcp-profile".into(),
        "minimal".into(),
    ];
    let command = ctx
        .here
        .parent()
        .ok_or("followup tools root unavailable")?
        .join("codex-jobs/codex_jobs.exe");
    let output = ctx.effects.run(&core::command_argv(&command, &args), 15)?;
    if output.code != 0 {
        return Err("followup standard launch failed; same durable key retained".into());
    }
    let jid = output
        .stdout
        .lines()
        .map(str::trim)
        .find(|s| core::valid_job(s) && s.starts_with("20"))
        .ok_or("followup launch receipt missing; same key retained")?
        .to_string();
    let record = state.replies.get_mut(mid).unwrap();
    record.job = Some(jid);
    record.phase = "launched".into();
    publish(&ctx.here.join(FILE), state)?;
    annotate(ctx, state, mid)
}
fn save_steering(ctx: &Context, state: &mut State, mid: &str, handler: Option<&str>) -> Result<()> {
    let record = state.replies.get_mut(mid).unwrap();
    record.phase = "parent_pending".into();
    record.steering_handler = Some(handler.unwrap_or("parent").into());
    publish(&ctx.here.join(FILE), state)?;
    let record = &state.replies[mid];
    let envelope = json!({"domain":DOMAIN,"delivery":"saved_steering","work_todo":state.watch.work_todo,
        "question_todo":state.watch.todo_id,"original_question":state.original_question,
        "original_decision":state.original_decision,"reply":record.reply,"handler":record.steering_handler,
        "acquisition_source":record.acquisition_source,"acquired_at":record.acquired_at,"launch_key":record.key});
    let path = match handler {
        Some(jid) => ctx
            .jobs
            .join("jobs")
            .join(jid)
            .join(format!("recovery-followup-{}.json", record.key)),
        None => ctx
            .here
            .join(format!("recovery-followup-parent-{}.json", record.key)),
    };
    if !path.is_file() || read(&path)? != envelope {
        publish(&path, &envelope)?;
    }
    annotate(ctx, state, mid)
}
pub fn claim(ctx: &mut Context, mid: &str, handler: &str) -> Result<Value> {
    id(mid)?;
    let _lock = acquire(&ctx.here)?;
    let mut state = load(&ctx.here)?.ok_or("followup not enrolled")?;
    let value = evidence(ctx, &state.watch)?;
    verify_evidence(&state, &value)?;
    let record = state
        .replies
        .get_mut(mid)
        .ok_or("followup reply not observed")?;
    if record.steering_handler.as_deref() != Some(handler) || record.phase != "parent_pending" {
        return Err("followup steering receiver differs".into());
    }
    if handler != "parent" && std::env::var("CC_JOB_ID").ok().as_deref() != Some(handler) {
        return Err("followup steering claimant is not the assigned job".into());
    }
    record_claim(record, handler);
    let envelope = json!({"original_question":state.original_question,"original_decision":state.original_decision,
        "reply":record.reply,"acquired_at":record.acquired_at,"acquisition_source":record.acquisition_source,
        "handler":handler,"receipt":"claimed_not_applied"});
    publish(&ctx.here.join(FILE), &state)?;
    annotate(ctx, &state, mid)?;
    Ok(envelope)
}
fn record_claim(record: &mut Record, handler: &str) {
    record.steering_received = true;
    if handler == "parent" {
        // A parent claim transfers ownership durably, even after its active
        // marker disappears. Only that handler's actual result can complete it.
        record.parent_managed = true;
    } else {
        record.job = Some(handler.into());
        record.phase = "launched".into();
    }
}
fn finish(ctx: &mut Context, state: &mut State, mid: &str) -> Result<()> {
    let record = &state.replies[mid];
    if record.phase == "applied" {
        return Ok(());
    }
    let Some(result) = record.result.as_ref() else {
        return Ok(());
    };
    let body = format!(
        "状態: 完了\n{}\n検証: {}\n変更版: {}\n元作業{}・判断{}の本人返信{}を反映しました。",
        core::s(&result["summary"]),
        core::s(&result["verification"]),
        core::s(&result["revision"]),
        state.watch.work_todo,
        state.watch.todo_id,
        mid
    );
    if !record.progress_reported {
        let path = ctx
            .here
            .join(format!("recovery-followup-result-{}.txt", record.key));
        let mut file = fs::File::create(&path).map_err(|_| "followup result staging failed")?;
        file.write_all(body.as_bytes())
            .and_then(|_| file.sync_all())
            .map_err(|_| "followup result flush failed")?;
        drop(file);
        let receipt = recovery_messages::log(
            ctx,
            &LogArgs {
                request_id: format!("followup-result-{}", record.key),
                title: "【情報システム部】本人回答の反映結果".into(),
                body_file: path,
                thread: Some(state.progress_thread.clone()),
                notify: false,
                attach: vec![],
            },
        )?;
        if receipt["delivery"] != "verified" || receipt["thread_id"] != state.progress_thread {
            return Err("followup progress result not verified".into());
        }
        state.replies.get_mut(mid).unwrap().progress_reported = true;
        publish(&ctx.here.join(FILE), state)?;
    }
    // Standard ToDo completion, after actual handler report AND verified silent post.
    // A reply may arrive during result delivery. Perform one final finite GET
    // before ToDo completion, preserving newly arrived replies as reservations.
    let final_evidence = evidence(ctx, &state.watch)?;
    observe(state, &final_evidence, &core::timestamp())?;
    publish(&ctx.here.join(FILE), state)?;
    let all_other_applied = state
        .replies
        .iter()
        .all(|(id, record)| id == mid || record.phase == "applied");
    if all_other_applied && todo_item(ctx, state.watch.todo_id)?["status"] != "done" {
        let command = ctx
            .here
            .parent()
            .ok_or("followup tools root unavailable")?
            .join("todo/todo.exe");
        let output = ctx.effects.run(
            &[
                command.display().to_string(),
                "done".into(),
                state.watch.todo_id.to_string(),
            ],
            5,
        )?;
        if output.code != 0 {
            return Err("followup decision ToDo completion failed".into());
        }
        if todo_item(ctx, state.watch.todo_id)?["status"] != "done" {
            return Err("followup decision ToDo completion unverified".into());
        }
    }
    state.replies.get_mut(mid).unwrap().phase = "applied".into();
    publish(&ctx.here.join(FILE), state)?;
    annotate(ctx, state, mid)
}
pub fn tick(ctx: &mut Context) -> Result<Value> {
    if load(&ctx.here)?.is_none() {
        return Ok(json!({"domain":DOMAIN,"enabled":false}));
    }
    let _lock = acquire(&ctx.here)?;
    let mut state = load(&ctx.here)?.ok_or("followup disappeared")?;
    let value = evidence(ctx, &state.watch)?;
    let at = core::timestamp();
    observe(&mut state, &value, &at)?;
    state.scheduled_observations = state.scheduled_observations.saturating_add(1);
    state.last_observed_at = Some(at);
    publish(&ctx.here.join(FILE), &state)?;
    let mut mids: Vec<_> = state.replies.keys().cloned().collect();
    mids.sort_by_key(|s| s.parse::<u64>().unwrap_or(0));
    for mid in mids {
        crate::tick_runtime::check()?;
        deliver(ctx, &mut state, &mid)?;
        finish(ctx, &mut state, &mid)?;
    }
    Ok(report(&state))
}
pub fn applied(ctx: &mut Context, mid: &str, file: &Path) -> Result<Value> {
    id(mid)?;
    let _lock = acquire(&ctx.here)?;
    let mut state = load(&ctx.here)?.ok_or("followup not enrolled")?;
    let value = evidence(ctx, &state.watch)?;
    // Include replies arriving after the previous tick before considering done.
    observe(&mut state, &value, &core::timestamp())?;
    publish(&ctx.here.join(FILE), &state)?;
    let record = state
        .replies
        .get(mid)
        .ok_or("followup reply not observed")?;
    if !value["owner_replies"]
        .as_array()
        .unwrap()
        .iter()
        .any(|r| r == &record.reply)
    {
        return Err("followup applied reply no longer matches API evidence".into());
    }
    let result = read(file)?;
    recovery_messages::validate_followup_text(
        &serde_json::to_string(&result).map_err(|_| "followup result encoding failed")?,
    )?;
    for field in [
        "handler",
        "revision",
        "verification",
        "summary",
        "applied_at",
    ] {
        if core::s(&result[field]).trim().is_empty() {
            return Err(
                "followup actual handler/revision/verification/result/time required".into(),
            );
        }
    }
    chrono::DateTime::parse_from_rfc3339(core::s(&result["applied_at"]))
        .map_err(|_| "followup application time invalid")?;
    let expected = record.job.as_deref().unwrap_or("parent");
    if core::s(&result["handler"]) != expected
        || (record.job.is_none()
            && !matches!(
                record.phase.as_str(),
                "parent_pending" | "result_pending" | "applied"
            ))
    {
        return Err("followup callback handler does not own this reply".into());
    }
    if let Some(old) = &record.result {
        if old != &result {
            return Err("followup callback changed; first result retained".into());
        }
    } else {
        let record = state.replies.get_mut(mid).unwrap();
        record.result = Some(result);
        record.phase = "result_pending".into();
        record.steering_received = true;
        publish(&ctx.here.join(FILE), &state)?;
    }
    finish(ctx, &mut state, mid)?;
    Ok(report(&state))
}
fn report(state: &State) -> Value {
    json!({"domain":DOMAIN,"request_id":state.watch.request_id,"todo_id":state.watch.todo_id,
        "work_todo":state.watch.work_todo,"scheduled_observations":state.scheduled_observations,
        "last_observed_at":state.last_observed_at,"replies":state.replies.iter().map(|(id,r)|
            json!({"message_id":id,"phase":r.phase,"launch_key":r.key,"job":r.job,"progress_reported":r.progress_reported,
                "steering_handler":r.steering_handler,"steering_received":r.steering_received})).collect::<Vec<_>>(),
        "legacy_state":"unchanged"})
}
pub fn inspect(ctx: &Context) -> Result<Value> {
    Ok(
        load(&ctx.here)?.map_or(json!({"domain":DOMAIN,"enabled":false}), |state| {
            report(&state)
        }),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::{Effects, Output};
    use std::sync::{Arc, Mutex};

    #[derive(Default)]
    struct Calls {
        keys: Vec<String>,
        fail_once: bool,
        reservations: usize,
        accepted: BTreeMap<String, String>,
    }
    struct Mock {
        home: PathBuf,
        jobs: PathBuf,
        calls: Arc<Mutex<Calls>>,
    }
    impl Effects for Mock {
        fn http(&mut self, _: &str, _: &str, _: Option<&Value>, _: bool) -> Result<Value> {
            Err("unexpected HTTP".into())
        }
        fn run(&mut self, args: &[String], _: u64) -> Result<Output> {
            if args.get(1).is_some_and(|arg| arg == "--guard-path") {
                return Ok(Output {
                    code: 0,
                    stdout: json!({"ok":true,"client_resource":false}).to_string(),
                    stderr: String::new(),
                });
            }
            let key = args
                .windows(2)
                .find(|v| v[0] == "--launch-key")
                .map(|v| v[1].clone())
                .ok_or("unexpected subprocess")?;
            let saved = load(&self.home)?.unwrap();
            let mut calls = self.calls.lock().unwrap();
            if saved.replies.values().any(|r| r.key == key) {
                calls.reservations += 1;
            }
            calls.keys.push(key.clone());
            let jid = calls
                .accepted
                .entry(key)
                .or_insert_with(|| "20261013-reply-fixture".into())
                .clone();
            fs::create_dir_all(self.jobs.join("jobs").join(&jid)).unwrap();
            fs::write(
                self.jobs.join("jobs").join(&jid).join("job.json"),
                br#"{"status":"queued"}"#,
            )
            .unwrap();
            if calls.fail_once {
                calls.fail_once = false;
                return Err("accepted launch response lost".into());
            }
            Ok(Output {
                code: 0,
                stdout: format!("{jid}\n"),
                stderr: String::new(),
            })
        }
    }
    fn fixture() -> (tempfile::TempDir, Context, State, Arc<Mutex<Calls>>) {
        let temp = tempfile::tempdir().unwrap();
        let home = temp.path().join("discord-ask");
        fs::create_dir_all(&home).unwrap();
        let jobs = temp.path().join("codex-jobs");
        let calls = Arc::new(Mutex::new(Calls::default()));
        let ctx = Context {
            here: home.clone(),
            client_projects: temp.path().join("never-read.txt"),
            civ6_home: PathBuf::new(),
            jobs: jobs.clone(),
            discord_tasks: PathBuf::new(),
            todo: temp.path().join("todo.json"),
            cfg: Value::Null,
            effects: Box::new(Mock {
                home,
                jobs,
                calls: calls.clone(),
            }),
            codex_command: PathBuf::new(),
            scheduler_command: PathBuf::new(),
            shepherd_command: PathBuf::new(),
            todo_command: PathBuf::new(),
        };
        fs::write(&ctx.todo,serde_json::to_vec(&json!({"items":[{"id":2472,"status":"doing","note":"retain this original note","jobs":[],"unknown":{"keep":true}},
            {"id":2473,"status":"block"},{"id":999,"note":"private fixture text must remain unchanged"}],"unknown_root":true})).unwrap()).unwrap();
        let state = State {
            domain: DOMAIN.into(),
            version: 1,
            watch: Watch {
                request_id: "one-new-question".into(),
                thread: "101".into(),
                question_message: "102".into(),
                todo_id: 2473,
                work_todo: 2472,
                parent_lock: temp.path().join("parent.active"),
                worker_cwd: temp.path().to_path_buf(),
                parent_messages: vec![],
            },
            original_question: "a: retain separately; b: add to department".into(),
            original_decision: json!({"todo_id":2473,"scope":{"original_work_todo":2472}}),
            decision_identity: json!({"scope":"only these registrations"}),
            owner: "100".into(),
            question_timestamp: "2026-10-12T00:00:00+00:00".into(),
            progress_thread: "103".into(),
            replies: BTreeMap::new(),
            scheduled_observations: 0,
            last_observed_at: None,
        };
        (temp, ctx, state, calls)
    }
    fn reply(state: &State, mid: &str) -> Value {
        json!({"request_id":state.watch.request_id,"todo_id":state.watch.todo_id,"question_thread_id":state.watch.thread,
            "question_message_id":state.watch.question_message,"author_id":state.owner,"message_id":mid,
            "timestamp":"2026-10-12T00:00:01.000+00:00","body":"a","decision_identity":state.decision_identity})
    }
    fn evidence_for(state: &State, replies: Vec<Value>) -> Value {
        json!({"domain":"new-owner-recovery-messages","source":"discord-api-v10","read_only":true,
            "request_id":state.watch.request_id,"thread_id":state.watch.thread,"question_message_id":state.watch.question_message,
            "owner_id":state.owner,"question_timestamp":state.question_timestamp,"original_question":state.original_question,
            "original_decision":state.original_decision,"decision_identity":state.decision_identity,"progress_thread_id":state.progress_thread,"owner_replies":replies})
    }
    fn observed(state: &mut State, mid: &str) {
        observe(
            state,
            &evidence_for(state, vec![reply(state, mid)]),
            "2026-10-13T00:00:00+09:00",
        )
        .unwrap();
    }
    #[test]
    fn rejects_non_owner_old_reply_wrong_scope_and_namespace_before_reservation() {
        let (_temp, ctx, state, calls) = fixture();
        for (field, value) in [
            ("author_id", json!("999")),
            ("message_id", json!("102")),
            ("timestamp", json!("2026-10-11T00:00:00Z")),
            ("decision_identity", json!({"scope":"other"})),
            ("question_message_id", json!("101")),
        ] {
            let mut r = reply(&state, "104");
            r[field] = value;
            let mut copy = state.clone();
            assert!(observe(&mut copy, &evidence_for(&state, vec![r]), "now").is_err());
            assert!(copy.replies.is_empty());
        }
        let mut wrong = evidence_for(&state, vec![reply(&state, "104")]);
        wrong["domain"] = json!("lost-old-questions");
        assert!(observe(&mut state.clone(), &wrong, "now").is_err());
        assert!(!ctx.here.join(FILE).exists());
        assert!(calls.lock().unwrap().keys.is_empty());
    }
    #[test]
    fn parent_active_and_already_assigned_answer_never_launch_or_mark_applied() {
        let (_temp, mut ctx, mut state, calls) = fixture();
        state.watch.parent_messages.push("104".into());
        observed(&mut state, "104");
        fs::write(&state.watch.parent_lock, "parent holds scope").unwrap();
        publish(&ctx.here.join(FILE), &state).unwrap();
        let original = core::read_json(&ctx.todo).unwrap();
        deliver(&mut ctx, &mut state, "104").unwrap();
        let once = fs::read(&ctx.todo).unwrap();
        deliver(&mut ctx, &mut state, "104").unwrap();
        assert_eq!(fs::read(&ctx.todo).unwrap(), once);
        fs::remove_file(&state.watch.parent_lock).unwrap();
        deliver(&mut ctx, &mut state, "104").unwrap();
        assert!(calls.lock().unwrap().keys.is_empty());
        assert_eq!(state.replies["104"].phase, "parent_pending");
        let after = core::read_json(&ctx.todo).unwrap();
        assert_eq!(after["items"][2], original["items"][2]);
        assert_eq!(after["items"][0]["note"], original["items"][0]["note"]);
        assert_eq!(
            after["items"][0]["unknown"],
            original["items"][0]["unknown"]
        );
        assert_eq!(after["items"][1]["status"], "block");
        let steering = read(&ctx.here.join(format!(
            "recovery-followup-parent-{}.json",
            state.replies["104"].key
        )))
        .unwrap();
        assert_eq!(steering["reply"], state.replies["104"].reply);
        assert_eq!(steering["original_question"], state.original_question);
    }
    #[test]
    fn parent_claim_is_permanent_ownership_after_active_marker_disappears() {
        let (_temp, mut ctx, mut state, calls) = fixture();
        observed(&mut state, "104");
        fs::write(&state.watch.parent_lock, "parent active").unwrap();
        publish(&ctx.here.join(FILE), &state).unwrap();
        deliver(&mut ctx, &mut state, "104").unwrap();
        assert!(!state.replies["104"].parent_managed);
        record_claim(state.replies.get_mut("104").unwrap(), "parent");
        publish(&ctx.here.join(FILE), &state).unwrap();
        fs::remove_file(&state.watch.parent_lock).unwrap();
        let mut restarted = load(&ctx.here).unwrap().unwrap();
        deliver(&mut ctx, &mut restarted, "104").unwrap();
        assert!(calls.lock().unwrap().accepted.is_empty());
        assert!(restarted.replies["104"].steering_received);
        assert_eq!(restarted.replies["104"].phase, "parent_pending");
    }
    #[test]
    fn lost_launch_response_restart_and_repeated_ticks_keep_one_key_and_one_job() {
        let (_temp, mut ctx, mut state, calls) = fixture();
        observed(&mut state, "104");
        publish(&ctx.here.join(FILE), &state).unwrap();
        calls.lock().unwrap().fail_once = true;
        assert!(deliver(&mut ctx, &mut state, "104").is_err());
        let mut restarted = load(&ctx.here).unwrap().unwrap();
        deliver(&mut ctx, &mut restarted, "104").unwrap();
        deliver(&mut ctx, &mut restarted, "104").unwrap();
        let calls = calls.lock().unwrap();
        assert_eq!(calls.reservations, 2);
        assert_eq!(calls.keys.len(), 2);
        assert_eq!(calls.keys[0], calls.keys[1]);
        assert_eq!(calls.accepted.len(), 1);
        assert_eq!(restarted.replies["104"].phase, "launched");
        assert_eq!(todo_item(&ctx, 2473).unwrap()["status"], "block");
    }
    #[test]
    fn another_reply_while_same_scope_job_runs_is_saved_not_launched() {
        let (_temp, mut ctx, mut state, calls) = fixture();
        observed(&mut state, "104");
        publish(&ctx.here.join(FILE), &state).unwrap();
        deliver(&mut ctx, &mut state, "104").unwrap();
        observed(&mut state, "105");
        publish(&ctx.here.join(FILE), &state).unwrap();
        deliver(&mut ctx, &mut state, "105").unwrap();
        assert_eq!(calls.lock().unwrap().accepted.len(), 1);
        assert_eq!(state.replies["105"].phase, "parent_pending");
        assert_eq!(state.replies["105"].reply["body"], "a");
        let record = &state.replies["105"];
        let inbox = read(
            &ctx.jobs
                .join("jobs")
                .join(record.steering_handler.as_ref().unwrap())
                .join(format!("recovery-followup-{}.json", record.key)),
        )
        .unwrap();
        assert_eq!(inbox["reply"], record.reply);
        assert!(!record.steering_received);
    }
    #[test]
    fn edited_reply_corrupt_storage_and_duplicate_callbacks_retain_first_evidence() {
        let (_temp, ctx, mut state, _) = fixture();
        observed(&mut state, "104");
        let original = state.replies["104"].reply.clone();
        let mut changed = original.clone();
        changed["body"] = json!("b");
        let edited = evidence_for(&state, vec![changed]);
        assert!(observe(&mut state, &edited, "later").is_err());
        assert_eq!(state.replies["104"].reply, original);
        publish(&ctx.here.join(FILE), &state).unwrap();
        let _first = acquire(&ctx.here).unwrap();
        assert!(acquire(&ctx.here).is_err());
        fs::write(ctx.here.join(FILE), "{corrupt").unwrap();
        assert!(load(&ctx.here).is_err());
    }
}
