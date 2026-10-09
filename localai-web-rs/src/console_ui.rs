//! Shared presentation for the Console and its existing music pages.
const SHELL: &str = include_str!("console_shell.html");
const MARKER: &str = "<meta name=\"local-ai-console-shell\" content=\"1\">";

use cc_ask::task_title::Names;
use serde_json::{json, Value};
use std::sync::LazyLock;

const CLIENT: &str = "クライアント案件";
static REDACTOR: LazyLock<cc_redact::Redactor> =
    LazyLock::new(|| cc_redact::Redactor::with_env(std::env::vars()));
fn text<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key].as_str().unwrap_or("")
}
fn rows<'a>(value: &'a Value, key: &str) -> &'a [Value] {
    value[key].as_array().map(Vec::as_slice).unwrap_or(&[])
}
fn secret_pattern() -> &'static regex::Regex {
    // Same credential patterns as Operations, without its summary truncation.
    static SECRET: LazyLock<regex::Regex> = LazyLock::new(|| {
        regex::Regex::new(
        r#"(?i)(?:(?:api[_ -]?key|(?:access|refresh|id|discord|client)[_ -]?(?:token|secret)|token|password|passwd|secret|authorization|パスワード|認証コード)["']?\s*[:=]\s*["']?\S+|\bBearer\s+\S+|\b(?:sk-|ghp_|gho_|github_pat_)[A-Za-z0-9_-]+)"#
    ).unwrap()
    });
    &SECRET
}
fn clean(names: &Names, value: &str) -> String {
    static LOCATION: LazyLock<regex::Regex> =
        LazyLock::new(|| regex::Regex::new(r"https?://\S+|[A-Za-z]:[/\\]\S+").unwrap());
    let value = cc_ask::task_title::canonical_text(&names.redact(&REDACTOR.text(value)));
    let value = secret_pattern().replace_all(&value, "[非表示]");
    LOCATION.replace_all(&value, "[非表示]").into_owned()
}
fn spec_path(names: &Names, value: &str) -> String {
    let bytes = value.as_bytes();
    if bytes.len() < 3
        || !bytes[0].is_ascii_alphabetic()
        || bytes[1] != b':'
        || !matches!(bytes[2], b'/' | b'\\')
        || value.chars().any(char::is_control)
        || names.project(value) == CLIENT
    {
        return "[非表示]".into();
    }
    secret_pattern()
        .replace_all(&REDACTOR.text(value), "[非表示]")
        .into_owned()
}
fn timestamp(value: &Value) -> Option<String> {
    if let Some(seconds) = value.as_u64() {
        return i64::try_from(seconds)
            .ok()
            .and_then(|n| chrono::DateTime::from_timestamp(n, 0))
            .map(|date| date.to_rfc3339());
    }
    let raw = value.as_str()?;
    if let Ok(date) = chrono::DateTime::parse_from_rfc3339(raw) {
        return Some(date.to_rfc3339());
    }
    ["%Y-%m-%d %H:%M", "%Y-%m-%d %H:%M:%S"]
        .iter()
        .find_map(|format| {
            chrono::NaiveDateTime::parse_from_str(raw, format)
                .ok()?
                .and_local_timezone(chrono::FixedOffset::east_opt(9 * 3600)?)
                .single()
                .map(|date| date.to_rfc3339())
        })
}
fn updated(value: &Value) -> Option<String> {
    ["at", "updated_at", "updatedAt", "timestamp", "updated"]
        .iter()
        .find_map(|key| timestamp(&value[*key]))
}
fn status(value: &Value) -> &str {
    let status = text(value, "status");
    if matches!(
        status,
        "todo"
            | "doing"
            | "block"
            | "wait"
            | "done"
            | "queued"
            | "starting"
            | "running"
            | "waiting"
            | "pending"
            | "succeeded"
            | "completed"
            | "failed"
            | "cancelled"
            | "lost"
    ) {
        status
    } else {
        "unknown"
    }
}
fn job_id(value: &Value) -> String {
    use std::hash::{Hash, Hasher};
    let mut hash = std::collections::hash_map::DefaultHasher::new();
    text(value, "id").hash(&mut hash);
    format!("job-{:016x}", hash.finish())
}
fn linked(item: &Value, job: &Value) -> bool {
    item["id"]
        .as_u64()
        .is_some_and(|id| job["todo_id"].as_u64() == Some(id))
        || ["id", "thread_id", "resume_session"].iter().any(|key| {
            job[*key].as_str().is_some_and(|id| {
                !id.is_empty()
                    && (item["job"].as_str() == Some(id)
                        || item["scheduler"]["job"].as_str() == Some(id)
                        || rows(item, "jobs")
                            .iter()
                            .any(|saved| saved.as_str() == Some(id)))
            })
        })
}
fn private_item(names: &Names, db: &Value, item: &Value) -> bool {
    [
        text(item, "project"),
        text(item, "cwd"),
        text(item, "spec"),
        db["projects"][text(item, "project")].as_str().unwrap_or(""),
    ]
    .iter()
    .any(|value| names.project(value) == CLIENT)
}
/// Read only metadata to establish privacy before considering any job message.
pub fn private_job(names: &Names, db: &Value, job: &Value) -> bool {
    let cwd = text(job, "cwd").replace('\\', "/").to_lowercase();
    [
        text(job, "project"),
        text(job, "cwd"),
        db["projects"][text(job, "project")].as_str().unwrap_or(""),
    ]
    .iter()
    .any(|v| names.project(v) == CLIENT)
        || rows(db, "items")
            .iter()
            .any(|item| linked(item, job) && private_item(names, db, item))
        || db["projects"].as_object().is_some_and(|projects| {
            projects.iter().any(|(project, path)| {
                let base = path
                    .as_str()
                    .unwrap_or("")
                    .replace('\\', "/")
                    .trim_end_matches('/')
                    .to_lowercase();
                !base.is_empty()
                    && (cwd == base
                        || cwd.starts_with(&format!("{base}/"))
                        || cwd.starts_with(&format!("{base}-wt/")))
                    && (names.project(project) == CLIENT
                        || names.project(path.as_str().unwrap_or("")) == CLIENT)
            })
        })
}
fn history(names: &Names, item: &Value) -> Vec<Value> {
    let mut history: Vec<_> = rows(item, "history")
        .iter()
        .map(|entry| {
            let message = entry.as_str().unwrap_or_else(|| {
                ["note", "message", "summary", "status"]
                    .iter()
                    .find_map(|key| entry[*key].as_str())
                    .unwrap_or("内容未記録")
            });
            json!({"at":updated(entry), "note":clean(names, message)})
        })
        .collect();
    history.extend(
        rows(item, "note_history")
            .iter()
            .map(|entry| json!({"at":updated(entry), "note":clean(names, text(entry, "note"))})),
    );
    history.extend(rows(item, "spec_history").iter().map(|entry|
        json!({"at":updated(entry), "note":format!("以前の指示書: {}", spec_path(names, text(entry, "spec")))})));
    history.sort_by_key(|entry| {
        updated(entry).and_then(|raw| chrono::DateTime::parse_from_rfc3339(&raw).ok())
    });
    history
}
fn question_url(item: &Value) -> Option<String> {
    static URL: LazyLock<regex::Regex> = LazyLock::new(|| {
        regex::Regex::new(r"^https://(?:www\.)?discord\.com/channels/\d+/\d+(?:/\d+)?/?$").unwrap()
    });
    ["questionUrl", "question_url"]
        .iter()
        .map(|key| text(item, key))
        .chain(text(item, "note").split_whitespace())
        .find(|value| URL.is_match(value))
        .map(str::to_owned)
}

/// Explicit display allowlist. Never clone machine records or reservation state.
pub fn display_todos(names: &Names, db: &Value, jobs: &[Value]) -> Value {
    let items: Vec<_> = rows(db, "items").iter().map(|item| {
        let client = private_item(names, db, item) || jobs.iter().any(|job| linked(item, job) && private_job(names, db, job));
        let linked_jobs: Vec<_> = jobs.iter().filter(|job| linked(item, job)).map(|job|
            json!({"id":job_id(job), "name":if client { "担当".into() } else { clean(names, text(job, "name")) }, "status":status(job)})).collect();
        let waits: Vec<_> = rows(item, "wait_on").iter().filter_map(Value::as_u64).collect();
        json!({"id":item["id"].as_u64(), "project":if client { CLIENT.into() } else { clean(names, &cc_ask::task_title::canonical_project(text(item, "project"))) },
            "title":if client { "クライアント案件の作業（内容は非表示）".into() } else { clean(names, text(item, "title")) },
            "status":status(item), "note":if client { String::new() } else { clean(names, text(item, "note")) },
            "progress":item["progress"].as_u64().filter(|p| *p <= 100),
            "agent":if client { "担当".into() } else { clean(names, text(item, "agent")) },
            "updated_at":updated(item), "linked_jobs":linked_jobs, "wait_on":waits,
            "questionUrl":if client { None } else { question_url(item) },
            "spec":if client || text(item, "spec").is_empty() { None } else { Some(spec_path(names, text(item, "spec"))) },
            "history":if client { Vec::new() } else { history(names, item) }})
    }).collect();
    json!({"items":items})
}
pub fn display_jobs(names: &Names, db: &Value, jobs: &[Value]) -> Value {
    json!({"jobs":jobs.iter().map(|job| {
        let client = private_job(names, db, job);
        let title = job["task_title"].as_str().unwrap_or_else(|| text(job, "name"));
        json!({"id":job_id(job), "task_title":if client { "クライアント案件の作業（内容は非表示）".into() } else { clean(names, title) },
            "status":status(job), "started_at":timestamp(&job["started_at"]).or_else(|| timestamp(&job["created_at"])),
            "finished_at":timestamp(&job["finished_at"]), "summary":if client { String::new() } else { clean(names, text(job, "last_message")) }})
    }).collect::<Vec<_>>()})
}
pub fn display_handoff(
    names: &Names,
    db: &Value,
    jobs: &[Value],
    enabled: bool,
    last_tick: u64,
    pr_count: usize,
    prs_error: bool,
) -> Value {
    let todos = display_todos(names, db, jobs);
    let displayed_jobs = display_jobs(names, db, jobs);
    let mut output = format!(
        "自動委譲: {}\n\n対応中・判断待ちのToDo\n",
        if enabled { "有効" } else { "停止" }
    );
    for item in rows(&todos, "items")
        .iter()
        .filter(|item| matches!(status(item), "doing" | "block" | "wait"))
    {
        output.push_str(&format!(
            "#{} [{}] {}: {}\n  {}\n",
            item["id"],
            status(item),
            text(item, "project"),
            text(item, "title"),
            text(item, "note")
        ));
    }
    output.push_str("\n実行中のジョブ\n");
    for job in rows(&displayed_jobs, "jobs")
        .iter()
        .filter(|job| matches!(status(job), "queued" | "starting" | "running" | "waiting"))
    {
        output.push_str(&format!(
            "{} [{}] {}\n",
            text(job, "id"),
            status(job),
            text(job, "task_title")
        ));
    }
    output.push_str(&format!("\n未完了のPR: {pr_count}件\n"));
    let decisions: Vec<_> = jobs.iter().filter(|job| text(job, "name").starts_with("orch-") && matches!(status(job), "succeeded" | "completed" | "failed" | "cancelled" | "lost"))
        .take(10).map(|job| json!({"id":job_id(job), "summary":if private_job(names, db, job) { "クライアント案件の記録（内容は非表示）".into() } else { clean(names, text(job, "last_message")) }})).collect();
    json!({"text":output, "decisions":decisions, "prs_error":if prs_error { Some("PRの状態を取得できません。") } else { None },
        "delegation":{"enabled":enabled, "last_tick":timestamp(&json!(last_tick))}})
}

/// Keep existing forms, CSRF values, routes, media and scripts intact.
pub fn decorate_html(html: &str) -> String {
    if html.contains(MARKER) {
        return html.to_owned();
    }
    let at = html
        .as_bytes()
        .windows(7)
        .rposition(|candidate| candidate.eq_ignore_ascii_case(b"</body>"))
        .unwrap_or(html.len());
    let mut output = String::with_capacity(html.len() + SHELL.len() + MARKER.len());
    output.push_str(&html[..at]);
    output.push_str(MARKER);
    output.push_str(SHELL);
    output.push_str(&html[at..]);
    output
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn quoted_multiline_secrets_and_basic_auth_are_removed_from_notes_and_history() {
        let names = Names::parse("");
        let note = "password=\"artificial alpha beta\"\nsecret='artificial gamma\ndelta'\nAuthorization: Basic dXNlcjpwYXNzd29yZA==\n正常な本文";
        let db = json!({"projects":{"demo":"D:/demo"},"items":[
            {"id":1,"project":"demo","status":"doing","note":note,
             "note_history":[{"note":note}]}]});
        let response = display_todos(&names, &db, &[]);
        let output = response.to_string();
        for forbidden in ["alpha", "beta", "gamma", "delta", "dXNlcjpwYXNzd29yZA=="] {
            assert!(!output.contains(forbidden), "secret residue in display");
        }
        assert!(response["items"][0]["note"]
            .as_str()
            .unwrap()
            .contains("正常な本文"));
        assert!(response["items"][0]["history"][0]["note"]
            .as_str()
            .unwrap()
            .contains("正常な本文"));
    }

    #[test]
    fn display_projects_allowlisted_records_and_merges_actual_old_contents() {
        let names = Names::parse("SecretClient\n");
        let db = json!({"projects":{"demo":"D:/CC/Work/demo","alias":"D:/CC/Work/SecretClient"},"items":[
            {"id":1,"project":"demo","title":"保存を確認","note":"現在のメモ password=artificial-password\n次の行","status":"doing","progress":60,"agent":"担当A","updated":"2026-10-10 09:15","jobs":["public-job"],"spec":"D:/CC/Tools/todo/specs/1-new.md",
                "history":[{"message":"着手した","timestamp":"2026-10-10T08:00:00+09:00","unexpected":"artificial-unlisted"}],
                "note_history":[{"note":"旧メモの具体的な本文\n再開条件を残す token=artificial-token","updated":"2026-10-10 08:30"}],
                "spec_history":[{"spec":"D:/CC/Tools/todo/specs/1-old.md","updated":"2026-10-10 08:45","body":"artificial-spec-body"}],"worker_receipt":{"prompt":"artificial-reservation"}},
            {"id":2,"project":"alias","title":"artificial-client-title","note":"artificial-client-note","status":"block","agent":"SecretClient担当","jobs":["SecretClient-job"],"spec":"D:/SecretClient/new.md",
                "history":["artificial-client-history"],"note_history":[{"note":"artificial-client-old-note"}],"spec_history":[{"spec":"D:/SecretClient/old.md"}],"questionUrl":"https://discord.com/channels/1/2"},
            {"id":3,"project":"demo","cwd":"D:/SecretClient-wt/agent","status":"doing","title":"artificial-path-title","note":"artificial-path-note"}
        ]});
        let jobs = vec![
            json!({"id":"public-job","name":"公開ジョブ token=artificial-job-token","status":"running","cwd":"D:/CC/Work/demo","created_at":"2026-10-10T09:00:00+09:00","launch_key":"artificial-reservation"}),
            json!({"id":"SecretClient-job","name":"orch-artificial-client-job","status":"completed","cwd":"D:/CC/Work/SecretClient","last_message":"artificial-client-decision"}),
            json!({"id":"public-decision","name":"orch-public","status":"completed","cwd":"D:/CC/Work/demo","last_message":"次の作業を再開する secret=artificial-decision-secret"}),
        ];
        let before = (db.clone(), jobs.clone());
        let todos = display_todos(&names, &db, &jobs);
        let item = &todos["items"][0];
        assert_eq!(item["updated_at"], "2026-10-10T09:15:00+09:00");
        assert_eq!(item["note"], "現在のメモ [非表示]\n次の行");
        assert_eq!(
            item["history"],
            json!([
                {"at":"2026-10-10T08:00:00+09:00","note":"着手した"},
                {"at":"2026-10-10T08:30:00+09:00","note":"旧メモの具体的な本文\n再開条件を残す [非表示]"},
                {"at":"2026-10-10T08:45:00+09:00","note":"以前の指示書: D:/CC/Tools/todo/specs/1-old.md"}
            ])
        );
        assert_eq!(item["spec"], "D:/CC/Tools/todo/specs/1-new.md");
        for item in &todos["items"].as_array().unwrap()[1..] {
            assert_eq!(item["project"], CLIENT);
            assert_eq!(item["note"], "");
            assert_eq!(item["history"], json!([]));
            assert!(item["spec"].is_null());
            assert!(item["questionUrl"].is_null());
        }
        let displayed_jobs = display_jobs(&names, &db, &jobs);
        assert_eq!(
            displayed_jobs["jobs"][0]["task_title"],
            "公開ジョブ [非表示]"
        );
        assert_eq!(displayed_jobs["jobs"][1]["summary"], "");
        let handoff = display_handoff(&names, &db, &jobs, true, 1791591300, 2, true);
        assert!(text(&handoff, "text").contains("保存を確認\n  現在のメモ [非表示]\n次の行"));
        assert_eq!(
            handoff["decisions"][1]["summary"],
            "次の作業を再開する [非表示]"
        );
        let response = json!({"todo":todos,"jobs":displayed_jobs,"handoff":handoff}).to_string();
        for forbidden in [
            "SecretClient",
            "artificial-password",
            "artificial-token",
            "artificial-job-token",
            "artificial-decision-secret",
            "artificial-unlisted",
            "artificial-spec-body",
            "artificial-reservation",
            "artificial-client",
            "artificial-path",
        ] {
            assert!(
                !response.contains(forbidden),
                "unexpected display content: {forbidden}"
            );
        }
        assert_eq!((db, jobs), before);
    }

    #[test]
    fn privacy_follows_linked_todos_registered_paths_and_client_worktrees() {
        let names = Names::parse("SecretClient");
        let db = json!({"projects":{"alias":"D:/SecretClient","SecretClient":"D:/neutral"},"items":[
            {"id":12,"project":"alias","jobs":["linked"],"status":"doing","note":"artificial-private"},
            {"id":13,"project":"public","jobs":["path-job"],"status":"doing","note":"artificial-associated"}
        ]});
        for job in [
            json!({"id":"linked","cwd":"D:/neutral-other"}),
            json!({"id":"todo-job","todo_id":12,"cwd":"D:/neutral-other"}),
            json!({"id":"path-job","cwd":"D:/neutral-wt/agent"}),
            json!({"id":"other-job","thread_id":"linked","cwd":"D:/common"}),
            json!({"id":"session-job","resume_session":"linked","cwd":"D:/common"}),
        ] {
            assert!(private_job(&names, &db, &job));
            assert_eq!(display_jobs(&names, &db, &[job])["jobs"][0]["summary"], "");
        }
        let jobs = vec![json!({"id":"path-job","cwd":"D:/neutral-wt/agent"})];
        assert_eq!(display_todos(&names, &db, &jobs)["items"][1]["note"], "");
    }

    // Run the actual request/render functions with artificial responses and a minimal DOM.
    // No browser, network, PC records, JS source-string assertions or Cargo subprocess.
    const SHELL_RESPONSE_TEST: &str = r#"
const assert = require('node:assert/strict');
const input = JSON.parse(require('node:fs').readFileSync(0, 'utf8'));
class Element {
  constructor(tag='div', cls='', text='') { this.tagName=tag.toUpperCase(); this.className=cls; this.ownText=text; this.childNodes=[]; this.dataset={}; this.value=''; this.open=false; }
  get textContent() { return this.ownText+this.childNodes.map(node=>node.textContent).join(''); }
  set textContent(text) { this.ownText=String(text); this.childNodes=[]; }
  append(...nodes) { this.childNodes.push(...nodes); }
  replaceChildren(...nodes) { this.ownText=''; this.childNodes=nodes; }
  get firstChild() { return this.childNodes[0]; }
  contains(node) { return node!=null && (this===node || this.childNodes.some(child=>child.contains(node))); }
  querySelectorAll(selector) { const result=[]; for(const child of this.childNodes){if(child.tagName==='DETAILS' && child.dataset.record && (!selector.includes('[open]') || child.open))result.push(child);result.push(...child.querySelectorAll(selector));}return result; }
}
const document={activeElement:null};
const window={getSelection:()=>null};
const make=(tag,cls,text)=>new Element(tag,cls,text);
const todoList=make('div'),jobsHost=make('div'),handoffHost=make('div');
const todoSearch=make('input'),todoProject=make('select'),todoStatus=make('select');
todoStatus.value='open';
let sharedData={},sharedSignature={},pendingShared={},sharedComposing=false;
const musicBusy=new Set(),sources=new Map(),calls=[];
const receive=(key,patch)=>sources.set(key,{...(sources.get(key)||{}),...patch});
const shared={open:true},operationsView={classList:{contains:()=>true}};
const refreshAgents=()=>{throw new Error('unredacted fallback called');};
let nextResponse=null;
const fetch=async path=>{calls.push(path);const key=path.split('/').pop();return {ok:true,status:200,json:async()=>nextResponse===null?input.responses[key]:nextResponse};};
function production(start,end){const at=input.shell.indexOf(start);const stop=input.shell.indexOf(end,at+start.length);assert(at>=0&&stop>at);return input.shell.slice(at,stop);}
const functions=production('    const array=value=>','    const waiting=')+
  production('    function sharedResponse(', '    function retrySource(')+
  production('    const short=value=>','    if(operationsView){');
const run=new Function('assert','document','window','make','todoList','jobsHost','handoffHost','todoSearch','todoProject','todoStatus','musicBusy','sources','receive','shared','operationsView','refreshAgents','fetch','input',
  'let sharedData={},sharedSignature={},pendingShared={},sharedComposing=false;'+functions+`
return (async()=>{
  loadShared(); await new Promise(resolve=>setImmediate(resolve));
  for(const key of ['todo','jobs','handoff']) {
    assert.equal(sources.get(key).error,'');
    assert.deepEqual(sources.get(key).data,input.responses[key].data);
    assert(sources.get(key).receivedAt>0);
  }
  assert(todoList.textContent.includes('保存を確認'));
  assert(todoList.textContent.includes('現在のメモの本文'));
  assert(todoList.textContent.includes('旧メモの具体的な本文'));
  assert(todoList.textContent.includes('以前の指示書: D:/CC/Tools/todo/specs/1-old.md'));
  assert(todoList.textContent.includes('更新 '+time('2026-10-10T09:15:00+09:00')));
  assert(todoList.textContent.includes(time('2026-10-10T08:30:00+09:00')+' · 旧メモの具体的な本文'));
  assert(jobsHost.textContent.includes('接続を確認する · 実行中'));
  assert(handoffHost.textContent.includes('対応中・判断待ちのToDo'));
  assert(handoffHost.textContent.includes('次の作業を再開する'));
})();`);
// Error cases run separately below with the same real functions and retained DOM.
const checks=functions+`
return (async()=>{
  for(const key of ['todo','jobs','handoff']) {
    await request(key,'/api/agent/display/'+key,data=>applyShared(key,data));
    const good=sources.get(key),host=key==='todo'?todoList:key==='jobs'?jobsHost:handoffHost,rendered=host.textContent;
    for(const bad of [{ok:false,version:1,data:input.responses[key].data},{ok:true,version:2,data:input.responses[key].data},{ok:true,version:1,data:{}},{ok:true,version:1,data:[]},input.responses[key].data,{ok:true,version:1,data:{items:[null],jobs:[null],decisions:[null],text:'bad'}},{ok:true,version:1,data:{items:[{}],jobs:[{}],decisions:[{}],text:'bad'}}]) {
      setResponse(bad);
      await request(key,'/api/agent/display/'+key,data=>applyShared(key,data));
      assert(sources.get(key).error);
      assert.equal(sources.get(key).data,good.data);
      assert.equal(sources.get(key).receivedAt,good.receivedAt);
      assert.equal(host.textContent,rendered);
    }
    setResponse(null);
    await request(key,'/api/agent/display/'+key,data=>applyShared(key,data));
    assert.equal(sources.get(key).error,'');
  }
  const raw={ok:false,departments:[]};setResponse(raw);
  await request('operations','/api/operations',data=>assert.equal(data,raw));
  assert.equal(sources.get('operations').error,'');
})();`;
(async()=>{
  await run(assert,document,window,make,todoList,jobsHost,handoffHost,todoSearch,todoProject,todoStatus,musicBusy,sources,receive,shared,operationsView,refreshAgents,fetch,input);
  assert.deepEqual(calls.slice(0,3),['/api/agent/display/todo','/api/agent/display/jobs','/api/agent/display/handoff']);
  const verify=new Function('assert','document','window','make','todoList','jobsHost','handoffHost','todoSearch','todoProject','todoStatus','musicBusy','sources','receive','shared','operationsView','refreshAgents','fetch','input','setResponse','sharedData','sharedSignature','pendingShared','sharedComposing',checks);
  await verify(assert,document,window,make,todoList,jobsHost,handoffHost,todoSearch,todoProject,todoStatus,musicBusy,sources,receive,shared,operationsView,refreshAgents,fetch,input,value=>nextResponse=value,{}, {}, {}, false);
  process.stdout.write('shared display response/render regression passed\n');
})().catch(error=>{console.error(error);process.exitCode=1;});
"#;

    #[test]
    fn shell_renders_three_shared_envelopes_and_retains_last_success_on_invalid_response() {
        use std::{
            io::Write,
            process::{Command, Stdio},
        };
        let names = Names::parse("");
        let db = json!({"projects":{"demo":"D:/CC/Work/demo"},"items":[{"id":1,"project":"demo","title":"保存を確認","note":"現在のメモの本文","status":"doing","updated":"2026-10-10 09:15",
            "note_history":[{"note":"旧メモの具体的な本文","updated":"2026-10-10 08:30"}],
            "spec_history":[{"spec":"D:/CC/Tools/todo/specs/1-old.md","updated":"2026-10-10 08:45"}]}]});
        let jobs = vec![
            json!({"id":"public-job","name":"接続を確認する","status":"running","cwd":"D:/CC/Work/demo"}),
            json!({"id":"public-decision","name":"orch-public","status":"succeeded","cwd":"D:/CC/Work/demo","last_message":"次の作業を再開する"}),
        ];
        let responses = json!({"todo":{"ok":true,"version":1,"data":display_todos(&names,&db,&jobs)},
            "jobs":{"ok":true,"version":1,"data":display_jobs(&names,&db,&jobs)},
            "handoff":{"ok":true,"version":1,"data":display_handoff(&names,&db,&jobs,true,1791591300,0,false)}});
        let mut command = Command::new("node");
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let mut child = command
            .args(["-e", SHELL_RESPONSE_TEST])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .expect("Node is required for the shell response regression");
        child
            .stdin
            .take()
            .unwrap()
            .write_all(
                json!({"shell":SHELL,"responses":responses})
                    .to_string()
                    .as_bytes(),
            )
            .unwrap();
        let output = child.wait_with_output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        assert_eq!(
            String::from_utf8_lossy(&output.stdout),
            "shared display response/render regression passed\n"
        );
    }

    #[test]
    fn preserves_music_form_and_media_and_decorates_only_once() {
        let html = "<!doctype html><BoDy><form action='/music/drafts' method='post'><input name='csrf' value='test-only'></form><audio src='/music/example/audio'></audio></BoDy>";
        let rendered = decorate_html(html);
        assert!(rendered.contains("<form action='/music/drafts' method='post'><input name='csrf' value='test-only'></form>"));
        assert!(rendered.contains("<audio src='/music/example/audio'></audio>"));
        assert!(rendered.ends_with("</BoDy>"));
        assert_eq!(rendered.matches(MARKER).count(), 1);
        assert_eq!(decorate_html(&rendered), rendered);
    }

    #[test]
    fn fragment_gets_shell_without_changing_existing_message() {
        let rendered = decorate_html("<h1>音楽制作</h1><p>接続を確認してください。</p>");
        assert!(rendered.starts_with("<h1>音楽制作</h1><p>接続を確認してください。</p>"));
        assert_eq!(rendered.matches(MARKER).count(), 1);
    }
}
