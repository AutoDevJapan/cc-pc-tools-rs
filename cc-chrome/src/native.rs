use super::*;
use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
    time::Duration,
};
use tokio::{
    io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt},
    net::windows::named_pipe::{ClientOptions, NamedPipeClient, ServerOptions},
};

const MAX_FRAME: usize = 32 * 1024 * 1024;
fn running(pid: u32, created: u64) -> Option<bool> {
    use windows_sys::Win32::{Foundation::*, System::Threading::*};
    unsafe {
        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if handle.is_null() {
            return if GetLastError() == ERROR_INVALID_PARAMETER {
                Some(false)
            } else {
                None
            };
        }
        let mut code = 0;
        let mut times = [FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        }; 4];
        let ok = GetExitCodeProcess(handle, &mut code) != 0
            && GetProcessTimes(
                handle,
                &mut times[0],
                &mut times[1],
                &mut times[2],
                &mut times[3],
            ) != 0;
        CloseHandle(handle);
        ok.then_some(
            code == 259
                && (((times[0].dwHighDateTime as u64) << 32) | times[0].dwLowDateTime as u64)
                    == created,
        )
    }
}
async fn read_frame(r: &mut (impl AsyncRead + Unpin)) -> io::Result<Value> {
    let size = r.read_u32_le().await? as usize;
    if size == 0 || size > MAX_FRAME {
        return Err(io::Error::other("invalid extension frame"));
    }
    let mut data = vec![0; size];
    r.read_exact(&mut data).await?;
    serde_json::from_slice(&data).map_err(io::Error::other)
}
async fn write_frame(w: &mut (impl AsyncWrite + Unpin), v: &Value) -> io::Result<()> {
    let data = serde_json::to_vec(v)?;
    if data.len() > MAX_FRAME {
        return Err(io::Error::other("extension frame too large"));
    }
    w.write_u32_le(data.len() as u32).await?;
    w.write_all(&data).await
}
async fn connect(pipe: &str) -> io::Result<NamedPipeClient> {
    let started = std::time::Instant::now();
    loop {
        match ClientOptions::new().open(pipe) {
            Ok(c) => return Ok(c),
            Err(e)
                if e.raw_os_error() == Some(231) && started.elapsed() < Duration::from_secs(1) =>
            {
                tokio::time::sleep(Duration::from_millis(25)).await
            }
            Err(e) => return Err(e),
        }
    }
}
struct Rpc {
    pipe: NamedPipeClient,
    next: u64,
}
impl Rpc {
    async fn connect(pipe: &str) -> io::Result<Self> {
        Ok(Self {
            pipe: connect(pipe).await?,
            next: 1,
        })
    }
    async fn call(&mut self, method: &str, params: Value) -> io::Result<Value> {
        if registrar::rpc_denied(method, &params) {
            return Err(io::Error::new(
                io::ErrorKind::PermissionDenied,
                registrar::REASON,
            ));
        }
        let id = self.next;
        self.next += 1;
        tokio::time::timeout(Duration::from_secs(3), async {
        write_frame(
            &mut self.pipe,
            &json!({"jsonrpc":"2.0","id":id,"method":method,"params":params}),
        )
        .await?;
            loop { let v=read_frame(&mut self.pipe).await?;
                if v["id"]==id && v.get("method").is_none() {
                    if v.get("error").is_some() { return Err(io::Error::other(v["error"]["message"].as_str().unwrap_or("extension request failed"))); }
                    return Ok(v["result"].clone());
                }
                if v.get("method").is_some() && v.get("id").is_some() {
                    write_frame(&mut self.pipe,&json!({"jsonrpc":"2.0","id":v["id"],"error":{"code":-1,"message":"cleanup client has no interactive handlers"}})).await?;
                }
            }
        }).await.map_err(io::Error::other)?
    }
}
fn runtime() -> io::Result<tokio::runtime::Runtime> {
    tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
}
async fn discover() -> io::Result<(String, Value)> {
    discover_matching(None).await
}
async fn discover_matching(record: Option<&Record>) -> io::Result<(String, Value)> {
    discover_for(record, false).await
}
async fn discover_for(
    record: Option<&Record>,
    include_reconnected: bool,
) -> io::Result<(String, Value)> {
    let mut pipes = vec![];
    for entry in fs::read_dir(r"\\.\pipe\")? {
        let name = entry?.file_name().to_string_lossy().into_owned();
        if !name.starts_with("codex-browser-use\\") || name.contains("cc-job-") {
            continue;
        }
        pipes.push(format!(r"\\.\pipe\{name}"));
    }
    discover_from_mode(pipes, record, include_reconnected).await
}
#[cfg(test)]
async fn discover_from(pipes: Vec<String>, record: Option<&Record>) -> io::Result<(String, Value)> {
    discover_from_mode(pipes, record, false).await
}
async fn discover_from_mode(
    pipes: Vec<String>,
    record: Option<&Record>,
    include_reconnected: bool,
) -> io::Result<(String, Value)> {
    let mut selected: Option<(String, Value)> = None;
    for pipe in pipes {
        let Ok(Ok(info))=tokio::time::timeout(Duration::from_millis(300),async {
            let mut rpc=Rpc::connect(&pipe).await?;
            rpc.call("getInfo",json!({"session_id":"cc-chrome-discovery","turn_id":"discovery","session_context":"live"})).await
        }).await else {continue};
        if chrome_identity(&info).is_some()
            && record.is_none_or(|r| {
                r.matches_backend(&info) || (include_reconnected && same_instance(r, &info))
            })
        {
            if let Some((_, prev)) = &selected {
                if prev["metadata"]["extensionInstanceId"]
                    != info["metadata"]["extensionInstanceId"]
                    || prev["browserTabMentions"]["generation"]
                        != info["browserTabMentions"]["generation"]
                {
                    return Err(io::Error::other(
                        "multiple Chrome profiles are connected; owner connection is ambiguous",
                    ));
                }
            } else {
                selected = Some((pipe, info));
            }
        }
    }
    selected.ok_or_else(|| io::Error::other("owner Chrome extension is not connected"))
}

// Creation, reconnection and cleanup share a complete browser identity.
// An extension response without identity is not permission to create a tab.
fn chrome_identity(info: &Value) -> Option<(&str, &str)> {
    if info["type"] != "extension" || info["family"] != "chrome" {
        return None;
    }
    let instance = info["metadata"]["extensionInstanceId"]
        .as_str()
        .filter(|v| !v.is_empty())?;
    let generation = info["browserTabMentions"]["generation"]
        .as_str()
        .filter(|v| !v.is_empty())?;
    Some((instance, generation))
}
pub fn inventory() -> io::Result<Value> {
    runtime()?.block_on(async {
        let mut result=vec![];
        for entry in fs::read_dir(r"\\.\pipe\")? {
            let name=entry?.file_name().to_string_lossy().into_owned();
            if !name.starts_with("codex-browser-use\\") || name.contains("cc-job-") {continue;}
            let pipe=format!(r"\\.\pipe\{name}");
            let Ok(mut rpc)=Rpc::connect(&pipe).await else {continue};
            let params=json!({"session_id":"cc-chrome-audit","turn_id":"inventory","session_context":"live"});
            let Ok(info)=rpc.call("getInfo",params.clone()).await else {continue};
            if info["type"]!="extension" || info["family"]!="chrome" {continue;}
            let tabs=rpc.call("getUserTabs",params).await?;
            result.push(json!({"pipe":pipe,"instance":info["metadata"]["extensionInstanceId"],"generation":info["browserTabMentions"]["generation"],"tabs":tabs.as_array().ok_or_else(||io::Error::other("invalid inventory"))?.iter().map(|t| t["id"].clone()).collect::<Vec<_>>() }));
        }
        Ok(json!(result))
    })
}
pub struct NativeCloser;
impl Closer for NativeCloser {
    fn absent(&mut self, r: &Record) -> io::Result<bool> {
        runtime()?.block_on(async {
            tokio::time::timeout(Duration::from_secs(3), absent_record(r))
                .await
                .map_err(|_| io::Error::other("tab absence check timed out"))?
        })
    }
    fn owner_running(&mut self, job: &Value) -> Option<bool> {
        let mut unknown = false;
        // pid is the admission supervisor: it remains alive while a worker is queued.
        for key in ["codex_pid"] {
            let Some(pid) = job[key].as_u64().and_then(|n| u32::try_from(n).ok()) else {
                continue;
            };
            let Some(created) = job[format!("{key}_created")].as_u64().filter(|n| *n != 0) else {
                unknown = true;
                continue;
            };
            match running(pid, created) {
                Some(true) => return Some(true),
                None => unknown = true,
                _ => {}
            }
        }
        if unknown { None } else { Some(false) }
    }
    fn close(&mut self, r: &Record) -> io::Result<bool> {
        runtime()?.block_on(async {
            tokio::time::timeout(Duration::from_secs(3), close_record(r))
                .await
                .map_err(io::Error::other)?
        })
    }
}
async fn close_record(r: &Record) -> io::Result<bool> {
    close_record_with_attachment(r, |info| context::attachment_params(r, info)).await
}

async fn close_record_in(dir: &Path, r: &Record) -> io::Result<bool> {
    close_record_with_attachment(r, |info| {
        context::load(dir, &r.job, info)?.attachment_params(r)
    })
    .await
}

async fn close_record_with_attachment(
    r: &Record,
    attachment: impl Fn(&Value) -> io::Result<Value>,
) -> io::Result<bool> {
    // A host can reconnect while Chrome stays open. Check both browser identities.
    let mut rpc = match Rpc::connect(&r.pipe).await {
        Ok(v) => v,
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            let (pipe, _) = discover_for(Some(r), true).await?;
            Rpc::connect(&pipe).await?
        }
        Err(e) => return Err(e),
    };
    let mut info = rpc.call("getInfo", r.params()).await?;
    if !r.matches_backend(&info) {
        if !same_instance(r, &info) {
            let (pipe, _) = discover_for(Some(r), true).await?;
            rpc = Rpc::connect(&pipe).await?;
            info = rpc.call("getInfo", r.params()).await?;
        }
        if !r.matches_backend(&info) {
            return retired_record_absent(&mut rpc, r, &info).await;
        }
    }
    if legacy_dedicated_session(r) {
        return close_dedicated_record(&mut rpc, r).await;
    }
    let tabs = rpc.call("getTabs", r.params()).await?;
    let tabs = tabs
        .as_array()
        .ok_or_else(|| io::Error::other("invalid session tab inventory"))?;
    if !tabs.iter().any(|t| t["id"].as_u64() == Some(r.tab)) {
        // A turn-end hook may release a marked tab from session control while
        // leaving it open. Reclaim only this positively recorded agent-created ID.
        let all = rpc.call("getUserTabs", r.params()).await?;
        if !has_tab(&all, r.tab)? {
            verify_close_identity(&mut rpc, r).await?;
            return Ok(false);
        }
        let mut p = r.params();
        p["tabId"] = json!(r.tab);
        rpc.call("claimUserTab", p).await?;
    }
    let p = attachment(&info)?;
    verify_close_identity(&mut rpc, r).await?;
    rpc.call("attach", p).await?;
    if let Err(error) = send_recorded_close(&mut rpc, r).await {
        if !debugger_not_attached(&error, r.tab) {
            return Err(error);
        }
        // Chrome may detach while the bridge still caches an attached session.
        // Reset only this positively recorded target, once. Authorization errors
        // and errors for any other tab do not enter this recovery path.
        verify_close_identity(&mut rpc, r).await?;
        let mut target = r.params();
        target["tabId"] = json!(r.tab);
        if let Err(error) = rpc.call("detach", target.clone()).await
            && !debugger_not_attached(&error, r.tab)
        {
            return Err(error);
        }
        verify_close_identity(&mut rpc, r).await?;
        rpc.call("attach", attachment(&info)?).await?;
        send_recorded_close(&mut rpc, r).await?;
    }
    let deadline = std::time::Instant::now() + Duration::from_millis(500);
    loop {
        let tabs = rpc.call("getUserTabs", r.params()).await?;
        if !has_tab(&tabs, r.tab)? {
            break;
        }
        if std::time::Instant::now() >= deadline {
            return Err(io::Error::other("tab still open after close"));
        }
        tokio::time::sleep(Duration::from_millis(25)).await;
    }
    verify_close_identity(&mut rpc, r).await?;
    Ok(true)
}
async fn verify_close_identity(rpc: &mut Rpc, r: &Record) -> io::Result<()> {
    let info = rpc.call("getInfo", r.params()).await?;
    if !r.matches_backend(&info) {
        return Err(io::Error::other("browser identity changed during close"));
    }
    Ok(())
}
async fn send_recorded_close(rpc: &mut Rpc, r: &Record) -> io::Result<Value> {
    // Every close, including the retry, validates the identity after attach.
    verify_close_identity(rpc, r).await?;
    let mut p = r.params();
    p["target"] = json!({"tabId":r.tab});
    p["method"] = json!("Page.close");
    p["commandParams"] = json!({});
    p["timeoutMs"] = json!(3000);
    rpc.call("executeCdp", p).await
}

fn legacy_dedicated_session(r: &Record) -> bool {
    r.turn == "open"
        && r.session
            .strip_prefix("cc-chrome-")
            .is_some_and(|id| uuid::Uuid::parse_str(id).is_ok())
}

/// Old standalone open created exactly one private-session tab. Complete that
/// exact normal lifecycle instead of attaching without a network policy. A
/// shared/changed session is never finalized by this individual-close path.
async fn close_dedicated_record(rpc: &mut Rpc, r: &Record) -> io::Result<bool> {
    verify_close_identity(rpc, r).await?;
    let tabs = rpc.call("getTabs", r.params()).await?;
    let all = rpc.call("getUserTabs", r.params()).await?;
    if !has_tab(&all, r.tab)? {
        verify_close_identity(rpc, r).await?;
        return Ok(false);
    }
    let Some(tabs) = tabs.as_array() else {
        return Err(io::Error::other("invalid private tab inventory"));
    };
    if tabs.len() != 1 || !has_tab(&json!(tabs), r.tab)? {
        return Err(io::Error::other(
            "individual close cannot finalize a shared or released session",
        ));
    }
    verify_close_identity(rpc, r).await?;
    let mut params = r.params();
    params["keep"] = json!([]);
    rpc.call("finalizeTabs", params).await?;
    let after = rpc.call("getUserTabs", r.params()).await?;
    if has_tab(&after, r.tab)? {
        return Err(io::Error::other("owned tab still open after finalization"));
    }
    verify_close_identity(rpc, r).await?;
    Ok(true)
}
fn debugger_not_attached(error: &io::Error, tab: u64) -> bool {
    error.to_string() == format!("Debugger is not attached to the tab with id: {tab}.")
}
async fn absent_record(r: &Record) -> io::Result<bool> {
    let (pipe, _) = discover_for(Some(r), true).await?;
    let mut rpc = Rpc::connect(&pipe).await?;
    let before = rpc.call("getInfo", r.params()).await?;
    if !r.matches_backend(&before) {
        return retired_record_absent(&mut rpc, r, &before)
            .await
            .map(|gone| !gone);
    }
    let tabs = rpc.call("getUserTabs", r.params()).await?;
    let absent = !has_tab(&tabs, r.tab)?;
    let after = rpc.call("getInfo", r.params()).await?;
    if !r.matches_backend(&after) {
        return Err(io::Error::other(
            "browser identity changed during absence check",
        ));
    }
    Ok(absent)
}
fn same_instance(r: &Record, info: &Value) -> bool {
    !r.instance.is_empty()
        && !r.generation.is_empty()
        && info["type"] == "extension"
        && info["family"] == "chrome"
        && info["metadata"]["extensionInstanceId"] == r.instance
        && info["browserTabMentions"]["generation"]
            .as_str()
            .is_some_and(|g| !g.is_empty())
}
async fn retired_record_absent(rpc: &mut Rpc, r: &Record, info: &Value) -> io::Result<bool> {
    if !same_instance(r, info) || info["browserTabMentions"]["generation"] == r.generation {
        return Err(io::Error::other("recorded browser identity unavailable"));
    }
    // A new generation cannot authorize closing a numeric ID from the old one.
    // It can only prove absence, using the complete inventory of the same instance.
    let inventory = rpc.call("getUserTabs", r.params()).await?;
    let tabs = inventory
        .as_array()
        .ok_or_else(|| io::Error::other("invalid retirement inventory"))?;
    for tab in tabs {
        if !["id", "providerTabId", "tabId"].iter().any(|key| {
            tab[*key]
                .as_u64()
                .or_else(|| tab[*key].as_str()?.parse().ok())
                .is_some_and(|id| id > 0)
        }) {
            return Err(io::Error::other("invalid retirement tab identity"));
        }
    }
    if has_tab(&inventory, r.tab)? {
        return Err(io::Error::other(
            "recorded tab ID present after browser generation changed",
        ));
    }
    let after = rpc.call("getInfo", r.params()).await?;
    if !same_instance(r, &after)
        || after["browserTabMentions"]["generation"] != info["browserTabMentions"]["generation"]
    {
        return Err(io::Error::other(
            "browser identity changed during retirement inventory",
        ));
    }
    Ok(false)
}
fn has_tab(tabs: &Value, id: u64) -> io::Result<bool> {
    let tabs = tabs
        .as_array()
        .ok_or_else(|| io::Error::other("invalid full tab inventory"))?;
    for t in tabs {
        if !["id", "providerTabId", "tabId"].iter().any(|key| {
            t[*key]
                .as_u64()
                .or_else(|| t[*key].as_str()?.parse().ok())
                .is_some_and(|n| n > 0)
        }) {
            return Err(io::Error::other("invalid full tab identity"));
        }
    }
    Ok(tabs.iter().any(|t| {
        ["id", "providerTabId", "tabId"]
            .iter()
            .any(|key| t[*key].as_u64().or_else(|| t[*key].as_str()?.parse().ok()) == Some(id))
    }))
}

/// Lives inside the supervisor: no resident child process, profile restart or debug port.
pub struct Proxy {
    pub pipe: String,
    stop: Option<tokio::sync::oneshot::Sender<()>>,
    thread: Option<std::thread::JoinHandle<()>>,
}
impl Drop for Proxy {
    fn drop(&mut self) {
        if let Some(stop) = self.stop.take() {
            let _ = stop.send(());
        }
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
    }
}
pub fn start(dir: &Path, job: &str) -> io::Result<Proxy> {
    if !safe_job(job) {
        return Err(io::Error::other("invalid job id"));
    }
    let pipe = format!(
        r"\\.\pipe\codex-browser-use\cc-job-{}",
        uuid::Uuid::new_v4()
    );
    let (ready_tx, ready_rx) = std::sync::mpsc::sync_channel(1);
    let (stop_tx, stop_rx) = tokio::sync::oneshot::channel();
    let path = dir.to_path_buf();
    let job = job.to_owned();
    let service_pipe = pipe.clone();
    let thread = std::thread::spawn(move || {
        let result = (|| -> io::Result<()> {
            runtime()?.block_on(async {
                let server = ServerOptions::new()
                    .first_pipe_instance(true)
                    .create(&service_pipe)?;
                let _ = ready_tx.send(Ok(()));
                accept_loop(server, &service_pipe, stop_rx, move |connected| {
                    let dir = path.clone();
                    let job = job.clone();
                    async move {
                        if let Ok((upstream, info)) = discover().await {
                            let _ = forward(connected, &upstream, &info, &dir, &job).await;
                        }
                    }
                })
                .await;
                Ok(())
            })
        })();
        if let Err(e) = result {
            let _ = ready_tx.send(Err(e.to_string()));
        }
    });
    match ready_rx.recv_timeout(Duration::from_secs(5)) {
        Ok(Ok(())) => Ok(Proxy {
            pipe,
            stop: Some(stop_tx),
            thread: Some(thread),
        }),
        other => {
            let _ = stop_tx.send(());
            let _ = thread.join();
            Err(io::Error::other(format!(
                "Chrome tracking setup failed: {other:?}"
            )))
        }
    }
}
/// Listener failures never drop active responders. Recreate the listener independently
/// and drain responders only on explicit supervisor shutdown.
async fn accept_loop<F, Fut>(
    server: tokio::net::windows::named_pipe::NamedPipeServer,
    pipe: &str,
    stop: tokio::sync::oneshot::Receiver<()>,
    mut handler: F,
) where
    F: FnMut(tokio::net::windows::named_pipe::NamedPipeServer) -> Fut,
    Fut: std::future::Future<Output = ()> + Send + 'static,
{
    let mut server = Some(server);
    let mut tasks = tokio::task::JoinSet::new();
    tokio::pin!(stop);
    loop {
        if server.is_none() {
            // Yield on resource errors while existing responders continue to journal replies.
            tokio::select! {
                _=&mut stop=>break,
                _=tokio::time::sleep(Duration::from_millis(25))=>{},
            }
            server = ServerOptions::new().create(pipe).ok();
            continue;
        }
        tokio::select! {
            _=&mut stop=>break,
            result=server.as_mut().unwrap().connect()=>{
                let connected=server.take().unwrap();
                // Bind the next instance while this handle still keeps the pipe name alive.
                // Backoff is only for an actual create failure, never for normal connections.
                server=ServerOptions::new().create(pipe).ok();
                if result.is_ok() {tasks.spawn(handler(connected));}
                // ERROR_NO_DATA from an immediately disconnected probing client is recoverable.
            },
            _=tasks.join_next(),if !tasks.is_empty()=>{},
        }
    }
    let _ = tokio::time::timeout(Duration::from_secs(5), async {
        while tasks.join_next().await.is_some() {}
    })
    .await;
    tasks.shutdown().await;
}
async fn forward(
    server: tokio::net::windows::named_pipe::NamedPipeServer,
    upstream: &str,
    info: &Value,
    dir: &Path,
    job: &str,
) -> io::Result<()> {
    let client = connect(upstream).await?;
    forward_streams(server, client, upstream, info, dir, job).await
}
async fn forward_streams(
    server: impl AsyncRead + AsyncWrite + Unpin,
    client: impl AsyncRead + AsyncWrite + Unpin,
    upstream: &str,
    info: &Value,
    dir: &Path,
    job: &str,
) -> io::Result<()> {
    let (mut ar, aw) = tokio::io::split(server);
    let aw = Arc::new(tokio::sync::Mutex::new(aw));
    let (mut br, mut bw) = tokio::io::split(client);
    let pending = Arc::new(Mutex::new(HashMap::<String, (String, Value)>::new()));
    let sending = pending.clone();
    let up = async {
        loop {
            let v = read_frame(&mut ar).await?;
            if v.get("method").is_some() {
                // Other sessions may discover our pipe. Reject them rather than attributing their tabs to this job.
                let own: Value = serde_json::from_slice(&fs::read(dir.join("job.json"))?)
                    .map_err(io::Error::other)?;
                if own["thread_id"].as_str().is_none()
                    || own["thread_id"] != v["params"]["session_id"]
                {
                    return Err(io::Error::other("browser session does not own this job"));
                }
                if registrar::rpc_denied(v["method"].as_str().unwrap_or(""), &v["params"]) {
                    if let Some(id) = v.get("id") {
                        write_frame(&mut *aw.lock().await, &json!({"jsonrpc":"2.0","id":id,"error":{"code":-32000,"message":registrar::REASON}})).await?;
                    }
                    continue;
                }
                if matches!(
                    v["method"].as_str(),
                    Some("turnEnded" | "finalizeTabs" | "detach")
                ) {
                    context::invalidate(dir, &v["params"]["session_id"], &v["params"]["turn_id"]);
                }
                lifecycle::activity(dir)?;
                if matches!(v["method"].as_str(), Some("createTab" | "attach"))
                    && v.get("id").is_some()
                {
                    sending.lock().unwrap().insert(
                        v["id"].to_string(),
                        (v["method"].as_str().unwrap().into(), v["params"].clone()),
                    );
                }
            }
            write_frame(&mut bw, &v).await?;
        }
        #[allow(unreachable_code)]
        Ok::<(), io::Error>(())
    };
    let down = async {
        loop {
            let mut v = read_frame(&mut br).await?;
            let request = { pending.lock().unwrap().get(&v["id"].to_string()).cloned() };
            if v.get("method").is_none()
                && v.get("error").is_none()
                && let Some((method, params)) = &request
                && method == "attach"
            {
                // Observe the unmodified normal CUA policy only after the
                // extension accepted it. Do not inject a fallback into CUA.
                let _ = context::observe_attachment(dir, job, upstream, info, params);
            }
            if v.get("method").is_none()
                && let Some((method, params)) = request
                && method == "createTab"
                && v.get("error").is_none()
            {
                let tab = v["result"]["id"]
                    .as_u64()
                    .ok_or_else(|| io::Error::other("invalid createTab reply"))?;
                let record = Record {
                    job: job.into(),
                    pipe: upstream.into(),
                    instance: info["metadata"]["extensionInstanceId"]
                        .as_str()
                        .unwrap_or("")
                        .into(),
                    generation: info["browserTabMentions"]["generation"]
                        .as_str()
                        .unwrap_or("")
                        .into(),
                    session: params["session_id"].as_str().unwrap_or("").into(),
                    turn: params["turn_id"].as_str().unwrap_or("").into(),
                    tab,
                    created: epoch(),
                };
                if persist_or_close(dir, &record).await.is_err() {
                    v = json!({"jsonrpc":"2.0","id":v["id"],"error":{"code":-32000,"message":"Chrome tab could not be recorded; creation failed and cleanup was attempted"}});
                }
            }
            if v.get("method").is_none() {
                pending.lock().unwrap().remove(&v["id"].to_string());
            }
            if let Err(e) = write_frame(&mut *aw.lock().await, &v).await {
                // The worker can exit before its createTab response. Persist all pending replies.
                if pending.lock().unwrap().is_empty() {
                    return Err(e);
                }
            }
        }
        #[allow(unreachable_code)]
        Ok::<(), io::Error>(())
    };
    tokio::pin!(up, down);
    tokio::select! {
        r=&mut up=>{
            if !pending.lock().unwrap().is_empty() {
                let _=tokio::time::timeout(Duration::from_secs(4),&mut down).await;
            }
            r
        },
        r=&mut down=>r,
    }
}
async fn persist_or_close(dir: &Path, record: &Record) -> io::Result<()> {
    if let Err(error) = persist(dir, record) {
        // Keep a second journal if only chrome-tabs is inaccessible. This also survives
        // cancellation while the rollback RPC is in flight.
        let emergency = dir
            .join("chrome-emergency-tabs")
            .join(format!("{}.json", uuid::Uuid::new_v4()));
        let recorded = lifecycle::atomic_json(&emergency, record).is_ok();
        let closed =
            tokio::time::timeout(Duration::from_secs(3), close_record_in(dir, record)).await;
        if let Ok(Ok(closed)) = closed {
            // A successful rollback still has a lifecycle history. Keep the
            // emergency journal until that history is durably published.
            let history = dir
                .join("chrome-tab-history")
                .join(emergency.file_name().unwrap());
            if lifecycle::atomic_json(
                &history,
                &json!({"record":record,"at":epoch(),"closed":closed,"completion_reason":"creation rolled back after tab record write failed"}),
            )
            .is_ok() && recorded
            {
                let _ = fs::remove_file(emergency);
            }
        }
        return Err(io::Error::new(error.kind(), "tab record write failed"));
    }
    Ok(())
}
pub fn open(dir: &Path, job: &str, url: &str) -> io::Result<Record> {
    runtime()?.block_on(open_inner(dir, job, url))
}

async fn open_inner(dir: &Path, job: &str, url: &str) -> io::Result<Record> {
    if registrar::restricted_url(url) {
        return Err(io::Error::new(
            io::ErrorKind::PermissionDenied,
            registrar::REASON,
        ));
    }
    if !safe_job(job)
        || !(url.starts_with("https://") || url.starts_with("http://") || url == "about:blank")
    {
        return Err(io::Error::other("invalid job or URL"));
    }
    {
        lifecycle::activity(dir)?;
        let (pipe, info) = discover().await?;
        open_with_backend(dir, job, url, pipe, info).await
    }
}

async fn open_with_backend(
    dir: &Path,
    job: &str,
    url: &str,
    pipe: String,
    info: Value,
) -> io::Result<Record> {
    // A standalone CLI has no getNetworkPolicy implementation. Reuse
    // only this job's current successful normal CUA attachment context.
    // Missing policy or another session fails before createTab.
    let normal = context::load(dir, job, &info)?;
    let mut rpc = Rpc::connect(&pipe).await?;
    let mut r = Record {
        job: job.into(),
        pipe,
        instance: info["metadata"]["extensionInstanceId"]
            .as_str()
            .unwrap_or("")
            .into(),
        generation: info["browserTabMentions"]["generation"]
            .as_str()
            .unwrap_or("")
            .into(),
        session: normal.session.clone(),
        turn: normal.turn.clone(),
        tab: 0,
        created: epoch(),
    };
    let tab = rpc.call("createTab", normal.params()).await?;
    r.tab = tab["id"]
        .as_u64()
        .ok_or_else(|| io::Error::other("invalid createTab reply"))?;
    persist_or_close(dir, &r).await?;
    let mut p = normal.params();
    p["tabId"] = json!(r.tab);
    p["networkPolicy"] = normal.network_policy.clone();
    let attached = rpc.call("attach", p).await;
    if let Err(e) = attached {
        let _ = tokio::time::timeout(Duration::from_secs(3), close_record_in(dir, &r)).await;
        return Err(e);
    }
    let mut p = normal.params();
    p["target"] = json!({"tabId":r.tab});
    p["method"] = json!("Page.navigate");
    p["commandParams"] = json!({"url":url});
    p["timeoutMs"] = json!(3000);
    if let Err(e) = rpc.call("executeCdp", p).await {
        let _ = tokio::time::timeout(Duration::from_secs(3), close_record_in(dir, &r)).await;
        return Err(e);
    }
    Ok(r)
}

fn remaining(deadline: std::time::Instant) -> io::Result<Duration> {
    deadline
        .checked_duration_since(std::time::Instant::now())
        .filter(|d| !d.is_zero())
        .ok_or_else(|| io::Error::new(io::ErrorKind::TimedOut, "browser deadline reached"))
}

/// The stored ownership record is also used by the caller to retry cleanup.
pub fn open_until(
    dir: &Path,
    job: &str,
    url: &str,
    deadline: std::time::Instant,
) -> io::Result<Record> {
    let budget = remaining(deadline)?;
    runtime()?.block_on(async {
        tokio::time::timeout(budget, open_inner(dir, job, url))
            .await
            .map_err(|_| {
                io::Error::new(io::ErrorKind::TimedOut, "opening owner Chrome timed out")
            })?
    })
}

/// Execute only page operations on a positively recorded tab in the same Chrome generation.
pub fn page_command(
    record: &Record,
    method: &str,
    params: Value,
    deadline: std::time::Instant,
) -> io::Result<Value> {
    if !matches!(
        method,
        "Runtime.evaluate" | "Input.insertText" | "Page.navigate"
    ) {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "unsupported page operation",
        ));
    }
    let budget = remaining(deadline)?;
    runtime()?.block_on(async {
        tokio::time::timeout(budget, async {
            let mut rpc = Rpc::connect(&record.pipe).await?;
            let info = rpc.call("getInfo", record.params()).await?;
            if !record.matches_backend(&info) {
                return Err(io::Error::other("Chrome generation changed"));
            }
            let tabs = rpc.call("getTabs", record.params()).await?;
            if !has_tab(&tabs, record.tab)? {
                return Err(io::Error::other("owned tab is no longer attached"));
            }
            let mut p = record.params();
            p["target"] = json!({"tabId":record.tab});
            p["method"] = json!(method);
            p["commandParams"] = params;
            p["timeoutMs"] = json!(3000);
            rpc.call("executeCdp", p).await
        })
        .await
        .map_err(|_| io::Error::new(io::ErrorKind::TimedOut, "owner Chrome operation timed out"))?
    })
}

/// Cleanup has its own bounded grace; it never closes the user's browser.
pub fn close_page(record: &Record) -> io::Result<bool> {
    runtime()?.block_on(async {
        tokio::time::timeout(Duration::from_secs(3), close_record(record))
            .await
            .map_err(|_| io::Error::new(io::ErrorKind::TimedOut, "owned tab cleanup timed out"))?
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    fn fixture_attachment(record: &Record) -> impl Fn(&Value) -> io::Result<Value> {
        let mut params = record.params();
        params["tabId"] = json!(record.tab);
        params["networkPolicy"] = json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]});
        move |_| Ok(params.clone())
    }
    #[test]
    fn malformed_inventory_cannot_prove_a_recorded_tab_absent() {
        for tabs in [
            json!([{}]),
            json!([{"id":0}]),
            json!([{"id":"invalid"}]),
            json!({}),
        ] {
            assert!(has_tab(&tabs, 11).is_err());
        }
        assert!(has_tab(&json!([{"providerTabId":"11"},{"tabId":99}]), 11).unwrap());
        assert!(!has_tab(&json!([]), 11).unwrap());
    }
    #[test]
    fn incomplete_connection_identity_is_rejected_before_creation() {
        let valid = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
        assert_eq!(chrome_identity(&valid), Some(("instance", "generation")));
        for invalid in [
            json!({"type":"extension","family":"chrome"}),
            json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":""},"browserTabMentions":{"generation":"generation"}}),
            json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":""}}),
        ] {
            assert!(chrome_identity(&invalid).is_none());
        }
    }
    #[test]
    fn missing_or_wrong_normal_context_sends_no_create_or_attach() {
        runtime().unwrap().block_on(async {
            for scenario in ["missing", "wrong-generation", "wrong-session", "no-policy"] {
                let root=tempfile::tempdir().unwrap();
                let owner=json!({"id":"job","thread_id":"session","status":"running"});
                fs::write(root.path().join("job.json"),owner.to_string()).unwrap();
                let info=json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
                let pipe=format!(r"\\.\pipe\cc-chrome-missing-context-{}",uuid::Uuid::new_v4());
                let server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
                if scenario!="missing" {
                    context::observe_attachment(root.path(),"job",&pipe,&info,&json!({"session_id":"session","turn_id":"turn","session_context":"live","networkPolicy":{"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]}})).unwrap();
                    if scenario=="wrong-session" {fs::write(root.path().join("job.json"),json!({"id":"job","thread_id":"other","status":"running"}).to_string()).unwrap();}
                    if scenario=="no-policy" {
                        let path=root.path().join("chrome-browser-context.json");
                        let mut value:Value=serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
                        value["network_policy"]=Value::Null;fs::write(path,value.to_string()).unwrap();
                    }
                }
                let mut backend=info;
                if scenario=="wrong-generation" {backend["browserTabMentions"]["generation"]=json!("other");}
                let result=open_with_backend(root.path(),"job","https://allowed.invalid",pipe,backend).await;
                assert!(result.is_err(),"{scenario}");
                assert!(!root.path().join("chrome-tabs").exists());
                assert!(tokio::time::timeout(Duration::from_millis(20),server.connect()).await.is_err(),"no create/attach connection before context");
            }
        });
    }
    #[test]
    fn open_reuses_successful_normal_policy_and_real_session_without_fallback() {
        runtime().unwrap().block_on(async {
            let root=tempfile::tempdir().unwrap();
            fs::write(root.path().join("job.json"),json!({"id":"job","thread_id":"real-session","status":"running"}).to_string()).unwrap();
            let info=json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
            let policy=json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]},{"kind":"origins","mode":"allowlist","patterns":["https://allowed.invalid"]}],"timeoutMs":2000});
            let pipe=format!(r"\\.\pipe\cc-chrome-normal-open-{}",uuid::Uuid::new_v4());
            let mut server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
            context::observe_attachment(root.path(),"job",&pipe,&info,&json!({"session_id":"real-session","turn_id":"real-turn","session_context":"live","networkPolicy":policy})).unwrap();
            let mock=async {
                server.connect().await.unwrap();
                for method in ["createTab","attach","executeCdp"] {
                    let request=read_frame(&mut server).await.unwrap();
                    assert_eq!(request["method"],method);
                    assert_eq!(request["params"]["session_id"],"real-session");
                    assert_eq!(request["params"]["turn_id"],"real-turn");
                    if method=="attach" {assert_eq!(request["params"]["networkPolicy"],policy);}
                    if method=="executeCdp" {assert_eq!(request["params"]["target"]["tabId"],42);assert_eq!(request["params"]["commandParams"]["url"],"https://allowed.invalid");}
                    write_frame(&mut server,&json!({"id":request["id"],"result":if method=="createTab" {json!({"id":42})}else{json!({})}})).await.unwrap();
                }
            };
            let (result,())=tokio::join!(open_with_backend(root.path(),"job","https://allowed.invalid",pipe,info.clone()),mock);
            let record=result.unwrap();assert_eq!(record.session,"real-session");assert_eq!(record.turn,"real-turn");assert_eq!(record.tab,42);
            assert_eq!(fs::read_dir(root.path().join("chrome-tabs")).unwrap().count(),1);
        });
    }
    #[test]
    fn proxy_publishes_context_only_after_successful_normal_attachment() {
        runtime().unwrap().block_on(async {
            for accepted in [false, true] {
                let root = tempfile::tempdir().unwrap();
                fs::write(root.path().join("job.json"), json!({"id":"job","thread_id":"session","status":"running"}).to_string()).unwrap();
                let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
                let policy = json!({"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]},{"kind":"origins","mode":"allowlist","patterns":["https://allowed.invalid"]}],"timeoutMs":2000});
                let params = json!({"session_id":"session","turn_id":"turn","session_context":"live","tabId":42,"networkPolicy":policy,"request_headers":{"fixture-header":"fixture-value"},"agent_request_header_enabled":true,"root_session_id":"root"});
                let (mut agent, proxy_agent) = tokio::io::duplex(65536);
                let (proxy_backend, mut backend) = tokio::io::duplex(65536);
                let proxy = forward_streams(proxy_agent, proxy_backend, "fixture-pipe", &info, root.path(), "job");
                let check = async {
                    write_frame(&mut agent, &json!({"id":1,"method":"attach","params":params})).await.unwrap();
                    let forwarded = read_frame(&mut backend).await.unwrap();
                    assert_eq!(forwarded["params"], params, "normal policy and session must be unchanged");
                    assert!(context::load(root.path(), "job", &info).is_err(), "request alone cannot publish context");
                    let reply = if accepted { json!({"id":1,"result":{}}) } else { json!({"id":1,"error":{"message":"controlled attachment rejection"}}) };
                    write_frame(&mut backend, &reply).await.unwrap();
                    assert_eq!(read_frame(&mut agent).await.unwrap(), reply);
                    let saved = context::load(root.path(), "job", &info);
                    if accepted {
                        let saved = saved.unwrap();
                        assert_eq!(saved.network_policy, policy);
                        let normal = saved.params();
                        for key in ["session_id", "turn_id", "session_context", "request_headers", "agent_request_header_enabled", "root_session_id"] {
                            assert_eq!(normal[key], params[key], "{key}");
                        }
                    } else {
                        assert!(saved.is_err());
                        assert!(!root.path().join("chrome-browser-context.json").exists());
                    }
                    agent.shutdown().await.unwrap();
                };
                let (result, ()) = tokio::time::timeout(Duration::from_secs(2), async {tokio::join!(proxy, check)}).await.unwrap();
                assert!(result.is_err());
            }
        });
    }
    #[test]
    fn discovery_refuses_two_distinct_chrome_profiles() {
        runtime().unwrap().block_on(async {
            let mut servers = Vec::new();
            let mut pipes = Vec::new();
            for _ in 0..2 {
                let pipe = format!(r"\\.\pipe\cc-chrome-profile-test-{}", uuid::Uuid::new_v4());
                servers.push(ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap());
                pipes.push(pipe);
            }
            let reply = |mut server: tokio::net::windows::named_pipe::NamedPipeServer, instance: &'static str| async move {
                server.connect().await.unwrap();
                let request = read_frame(&mut server).await.unwrap();
                assert_eq!(request["method"], "getInfo");
                write_frame(&mut server, &json!({"id":request["id"],"result":{"type":"extension","family":"chrome","metadata":{"extensionInstanceId":instance},"browserTabMentions":{"generation":"generation"}}})).await.unwrap();
            };
            let second = servers.pop().unwrap();
            let first = servers.pop().unwrap();
            let (result, _, _) = tokio::join!(discover_from(pipes, None), reply(first, "owner"), reply(second, "other"));
            assert!(result.unwrap_err().to_string().contains("ambiguous"));
        });
    }

    #[test]
    fn page_commands_require_matching_generation_and_recorded_tab() {
        runtime().unwrap().block_on(async {
            for (generation, tabs, succeeds) in [("generation", json!([{"id":42}]), true), ("different", json!([{"id":42}]), false), ("generation", json!([{"id":99}]), false)] {
                let pipe = format!(r"\\.\pipe\cc-chrome-page-test-{}", uuid::Uuid::new_v4());
                let mut server = ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
                let record = Record { job:"chatgpt-web".into(), pipe, instance:"instance".into(), generation:"generation".into(), session:"session".into(), turn:"turn".into(), tab:42, created:epoch() };
                let call = tokio::task::spawn_blocking(move || page_command(&record, "Runtime.evaluate", json!({"expression":"1+1","returnByValue":true}), std::time::Instant::now()+Duration::from_secs(2)));
                let mock = async {
                    server.connect().await.unwrap();
                    let request=read_frame(&mut server).await.unwrap();
                    assert_eq!(request["method"],"getInfo");
                    write_frame(&mut server,&json!({"id":request["id"],"result":{"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":generation}}})).await.unwrap();
                    if generation=="generation" {
                        let request=read_frame(&mut server).await.unwrap();assert_eq!(request["method"],"getTabs");
                        write_frame(&mut server,&json!({"id":request["id"],"result":tabs})).await.unwrap();
                        if succeeds {
                            let request=read_frame(&mut server).await.unwrap();assert_eq!(request["method"],"executeCdp");
                            assert_eq!(request["params"]["target"]["tabId"],42);
                            assert_eq!(request["params"]["method"],"Runtime.evaluate");
                            write_frame(&mut server,&json!({"id":request["id"],"result":{"result":{"value":2}}})).await.unwrap();
                        }
                    }
                };
                let (result, _) = tokio::join!(call, mock);
                assert_eq!(result.unwrap().is_ok(), succeeds);
            }
        });
    }
    #[test]
    fn rpc_deadline_includes_write_to_a_connected_pipe_that_never_reads() {
        runtime().unwrap().block_on(async {
            let pipe = format!(r"\\.\pipe\cc-chrome-stalled-write-{}", uuid::Uuid::new_v4());
            let server = ServerOptions::new()
                .first_pipe_instance(true)
                .in_buffer_size(4096)
                .create(&pipe)
                .unwrap();
            let (stop, stopped) = tokio::sync::oneshot::channel();
            let connected = async {
                let mut rpc = Rpc::connect(&pipe).await.unwrap();
                let start = std::time::Instant::now();
                let result = rpc
                    .call("fixture", json!({"large":"x".repeat(1024 * 1024)}))
                    .await;
                assert!(result.is_err());
                assert!(start.elapsed() < Duration::from_secs(4));
                let _ = stop.send(());
            };
            let stalled = async {
                server.connect().await.unwrap();
                tokio::select! {
                    _ = stopped => {},
                    _ = tokio::time::sleep(Duration::from_secs(6)) => {},
                }
                drop(server);
            };
            tokio::join!(connected, stalled);
        });
    }
    #[test]
    fn retirement_only_reads_and_retires_absent_tabs_of_the_same_instance() {
        runtime().unwrap().block_on(async {
            for (tabs, after_instance, after_generation, accepted) in [
                (json!([{"id":"99"}]), "extension", "new", true),
                (json!([{"id":"11"},{"id":"99"}]), "extension", "new", false),
                (json!([null]), "extension", "new", false),
                (json!({}), "extension", "new", false),
                (json!([]), "other", "new", false),
                (json!([]), "extension", "newer", false),
            ] {
                let pipe=format!(r"\\.\pipe\cc-chrome-retirement-{}",uuid::Uuid::new_v4());
                let mut server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
                let record=Record{job:"j".into(),pipe:pipe.clone(),instance:"extension".into(),generation:"old".into(),session:"session".into(),turn:"turn".into(),tab:11,created:100};
                let info=json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"new"}});
                let mock=async {
                    server.connect().await.unwrap();
                    let v=read_frame(&mut server).await.unwrap();assert_eq!(v["method"],"getUserTabs");
                    write_frame(&mut server,&json!({"id":v["id"],"result":tabs})).await.unwrap();
                    if tabs==json!([]) || tabs==json!([{"id":"99"}]) {
                        let v=read_frame(&mut server).await.unwrap();assert_eq!(v["method"],"getInfo");
                        write_frame(&mut server,&json!({"id":v["id"],"result":{"type":"extension","family":"chrome","metadata":{"extensionInstanceId":after_instance},"browserTabMentions":{"generation":after_generation}}})).await.unwrap();
                    }
                    // EOF proves no claim/attach/close command was sent, including rejection cases.
                    assert!(read_frame(&mut server).await.is_err());
                };
                let client=async {
                    let mut rpc=Rpc::connect(&pipe).await.unwrap();
                    let result=retired_record_absent(&mut rpc,&record,&info).await;
                    if accepted {assert!(!result.unwrap());} else {assert!(result.is_err());}
                };
                tokio::time::timeout(Duration::from_secs(5),async {tokio::join!(client,mock)}).await.unwrap();
            }
        });
    }
    #[test]
    fn retirement_rejects_other_instances_and_incomplete_identities() {
        let record = Record {
            job: "j".into(),
            pipe: "unused".into(),
            instance: "extension".into(),
            generation: "old".into(),
            session: "session".into(),
            turn: "turn".into(),
            tab: 11,
            created: 100,
        };
        for (instance, generation) in [("other", "new"), ("extension", ""), ("", "new")] {
            let info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":instance},"browserTabMentions":{"generation":generation}});
            assert!(!same_instance(&record, &info));
        }
    }
    #[test]
    fn cleanup_reclaims_a_released_recorded_tab_and_keeps_user_tabs() {
        runtime().unwrap().block_on(async {
            let pipe=format!(r"\\.\pipe\cc-chrome-fixture-{}",uuid::Uuid::new_v4());
            let server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
            let record=Record{job:"j".into(),pipe,instance:"extension".into(),generation:"generation".into(),session:"session".into(),turn:"turn".into(),tab:11,created:100};
            let mock=async move{
                server.connect().await.unwrap();let mut server=server;
                for (method,result) in [
                    ("getInfo",json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"generation"}})),
                    ("getTabs",json!([])),("getUserTabs",json!([{"id":"11"},{"id":"99"}])),
                    ("claimUserTab",json!({"id":11})),
                    ("getInfo",json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"generation"}})),
                    ("attach",json!({})),
                    ("getInfo",json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"generation"}})),
                    ("executeCdp",json!({})),
                    ("getUserTabs",json!([{"id":"99"}])),
                    ("getInfo",json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"generation"}})),
                ] {
                    let v=read_frame(&mut server).await.unwrap();assert_eq!(v["method"],method);
                    if ["claimUserTab","attach"].contains(&method) {assert_eq!(v["params"]["tabId"],11);}
                    if method=="executeCdp" {assert_eq!(v["params"]["target"]["tabId"],11);assert_eq!(v["params"]["method"],"Page.close");}
                    write_frame(&mut server,&json!({"jsonrpc":"2.0","id":v["id"],"result":result})).await.unwrap();
                }
            };
            let (closed,_)=tokio::join!(close_record_with_attachment(&record,fixture_attachment(&record)),mock);assert!(closed.unwrap());
        });
    }
    #[test]
    fn debugger_recovery_matches_only_the_exact_recorded_tab_error() {
        let matches = |message| debugger_not_attached(&io::Error::other(message), 11);
        assert!(matches("Debugger is not attached to the tab with id: 11."));
        assert!(!matches("Debugger is not attached to the tab with id: 11"));
        assert!(!matches(
            "Debugger is not attached to the tab with id: 11.."
        ));
        assert!(!matches("Debugger is not attached to the tab with id: 99."));
        assert!(!matches("Permission denied"));
        assert!(!matches("Debugger unattached"));
        assert!(!matches(
            "Debugger is not attached to the tab with id: 11; permission denied"
        ));
    }
    #[test]
    fn legacy_individual_finalization_never_mutates_shared_or_changed_identity() {
        runtime().unwrap().block_on(async {
            for scenario in ["owned", "shared", "released", "gone", "changed-generation", "finalize-failed", "still-open"] {
                let pipe=format!(r"\\.\pipe\cc-chrome-legacy-{}",uuid::Uuid::new_v4());
                let mut server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
                let record=Record{job:"fixture".into(),pipe,instance:"instance".into(),generation:"generation".into(),session:format!("cc-chrome-{}",uuid::Uuid::new_v4()),turn:"open".into(),tab:42,created:100};
                let info=json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
                let mut changed=info.clone();changed["browserTabMentions"]["generation"]=json!("other");
                let expected_session=record.session.clone();
                let mock=async move {
                    server.connect().await.unwrap();
                    let tabs=match scenario {"shared"=>json!([{"id":42},{"id":99}]),"released"|"gone"=>json!([]),_=>json!([{"id":42}])};
                    let all=if scenario=="gone" {json!([{"id":99}])}else{json!([{"id":42},{"id":99}])};
                    let mut steps=vec![("getInfo",info.clone(),false),("getInfo",info.clone(),false),("getTabs",tabs,false),("getUserTabs",all,false)];
                    if scenario=="gone" {steps.push(("getInfo",info.clone(),false));}
                    if !["shared","released","gone"].contains(&scenario) {
                        steps.push(("getInfo",if scenario=="changed-generation" {changed}else{info.clone()},false));
                        if scenario!="changed-generation" {
                            steps.push(("finalizeTabs",if scenario=="finalize-failed" {json!({"message":"controlled finalization failure"})}else{json!({})},scenario=="finalize-failed"));
                            if scenario!="finalize-failed" {
                                steps.push(("getUserTabs",if scenario=="still-open" {json!([{"id":42},{"id":99}])}else{json!([{"id":99}])},false));
                                if scenario!="still-open" {steps.push(("getInfo",info,false));}
                            }
                        }
                    }
                    for (method,result,error) in steps {
                        let request=read_frame(&mut server).await.unwrap();
                        assert_eq!(request["method"],method);
                        assert_eq!(request["params"]["session_id"],expected_session);
                        if method=="finalizeTabs" {assert_eq!(request["params"]["keep"],json!([]));}
                        let reply=if error{json!({"id":request["id"],"error":result})}else{json!({"id":request["id"],"result":result})};
                        write_frame(&mut server,&reply).await.unwrap();
                    }
                    assert!(read_frame(&mut server).await.is_err(),"no extra close/claim/attach operation");
                };
                let close=async {close_record_with_attachment(&record, |_| panic!("legacy cleanup must not request a policy attachment")).await};
                let (result,())=tokio::time::timeout(Duration::from_secs(5),async{tokio::join!(close,mock)}).await.unwrap();
                match scenario {"owned"=>assert!(result.unwrap()),"gone"=>assert!(!result.unwrap()),_=>assert!(result.is_err(),"{scenario}")}
            }
        });
    }
    #[test]
    fn cleanup_recovers_a_stale_debugger_once_without_touching_user_tabs() {
        runtime().unwrap().block_on(async {
            let pipe=format!(r"\\.\pipe\cc-chrome-stale-{}",uuid::Uuid::new_v4());
            let mut server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
            let record=Record{job:"j".into(),pipe,instance:"extension".into(),generation:"generation".into(),session:"session".into(),turn:"turn".into(),tab:11,created:100};
            let info=json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"generation"}});
            let stale=json!({"code":1,"message":"Debugger is not attached to the tab with id: 11."});
            let mock=async move {
                server.connect().await.unwrap();
                for (method, result, error) in [
                    ("getInfo",info.clone(),false),("getTabs",json!([{"id":11},{"id":99}]),false),
                    ("getInfo",info.clone(),false),("attach",json!({}),false),("getInfo",info.clone(),false),
                    ("executeCdp",stale.clone(),true),("getInfo",info.clone(),false),
                    ("detach",stale,true),("getInfo",info.clone(),false),
                    ("attach",json!({}),false),("getInfo",info.clone(),false),("executeCdp",json!({}),false),
                    ("getUserTabs",json!([{"id":99}]),false),("getInfo",info,false),
                ] {
                    let v=read_frame(&mut server).await.unwrap();
                    assert_eq!(v["method"],method);
                    if ["attach","detach"].contains(&method) {assert_eq!(v["params"]["tabId"],11);}
                    if method=="executeCdp" {assert_eq!(v["params"]["target"]["tabId"],11);assert_eq!(v["params"]["method"],"Page.close");}
                    let response=if error {json!({"id":v["id"],"error":result})} else {json!({"id":v["id"],"result":result})};
                    write_frame(&mut server,&response).await.unwrap();
                }
            };
            let (closed,())=tokio::time::timeout(Duration::from_secs(5),async {tokio::join!(close_record_with_attachment(&record,fixture_attachment(&record)),mock)}).await.unwrap();
            assert!(closed.unwrap());
        });
    }
    #[test]
    fn cleanup_stops_if_generation_changes_before_stale_debugger_recovery() {
        runtime().unwrap().block_on(async {
            let pipe=format!(r"\\.\pipe\cc-chrome-stale-generation-{}",uuid::Uuid::new_v4());
            let mut server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
            let record=Record{job:"j".into(),pipe,instance:"extension".into(),generation:"generation".into(),session:"session".into(),turn:"turn".into(),tab:11,created:100};
            let info=json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"generation"}});
            let mut changed=info.clone(); changed["browserTabMentions"]["generation"]=json!("new-generation");
            let mock=async move {
                server.connect().await.unwrap();
                for (method, result, error) in [
                    ("getInfo",info.clone(),false),("getTabs",json!([{"id":11},{"id":99}]),false),
                    ("getInfo",info.clone(),false),("attach",json!({}),false),("getInfo",info,false),
                    ("executeCdp",json!({"code":1,"message":"Debugger is not attached to the tab with id: 11."}),true),
                    ("getInfo",changed,false),
                ] {
                    let v=read_frame(&mut server).await.unwrap(); assert_eq!(v["method"],method);
                    let response=if error {json!({"id":v["id"],"error":result})} else {json!({"id":v["id"],"result":result})};
                    write_frame(&mut server,&response).await.unwrap();
                }
                assert!(read_frame(&mut server).await.is_err(), "no reconnect or close request after generation change");
            };
            let (closed,())=tokio::time::timeout(Duration::from_secs(5),async {tokio::join!(close_record_with_attachment(&record,fixture_attachment(&record)),mock)}).await.unwrap();
            assert!(closed.is_err());
        });
    }
    #[test]
    fn cleanup_never_mutates_after_unsafe_generation_or_command_error() {
        runtime().unwrap().block_on(async {
            for scenario in ["absent", "absent-new-generation", "reattached-new-generation", "permission", "other-tab"] {
                let pipe=format!(r"\\.\pipe\cc-chrome-close-guard-{}",uuid::Uuid::new_v4());
                let mut server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
                let record=Record{job:"j".into(),pipe,instance:"extension".into(),generation:"generation".into(),session:"session".into(),turn:"turn".into(),tab:11,created:100};
                let info=json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"generation"}});
                let mut changed=info.clone(); changed["browserTabMentions"]["generation"]=json!("new-generation");
                let mut steps=vec![("getInfo",info.clone(),false)];
                if scenario.starts_with("absent") {
                    steps.extend([
                        ("getTabs",json!([]),false),("getUserTabs",json!([]),false),
                        ("getInfo",if scenario=="absent" {info.clone()} else {changed.clone()},false),
                    ]);
                } else {
                    let message=match scenario {
                        "permission"=>"Permission denied",
                        "other-tab"=>"Debugger is not attached to the tab with id: 99.",
                        _=>"Debugger is not attached to the tab with id: 11.",
                    };
                    steps.extend([
                        ("getTabs",json!([{"id":11},{"id":99}]),false),
                        ("getInfo",info.clone(),false),("attach",json!({}),false),
                        ("getInfo",info.clone(),false),("executeCdp",json!({"code":1,"message":message}),true),
                    ]);
                    if scenario=="reattached-new-generation" {
                        steps.extend([
                            ("getInfo",info.clone(),false),("detach",json!({}),false),
                            ("getInfo",info.clone(),false),("attach",json!({}),false),
                            ("getInfo",changed,false),
                        ]);
                    }
                }
                let mock=async move {
                    server.connect().await.unwrap();
                    for (method,result,error) in steps {
                        let v=read_frame(&mut server).await.unwrap(); assert_eq!(v["method"],method);
                        if ["attach","detach"].contains(&method) {assert_eq!(v["params"]["tabId"],11);}
                        if method=="executeCdp" {assert_eq!(v["params"]["target"]["tabId"],11);}
                        let response=if error {json!({"id":v["id"],"error":result})} else {json!({"id":v["id"],"result":result})};
                        write_frame(&mut server,&response).await.unwrap();
                    }
                    assert!(read_frame(&mut server).await.is_err(), "no additional requests after guard result");
                };
                let (closed,())=tokio::time::timeout(Duration::from_secs(5),async {tokio::join!(close_record_with_attachment(&record,fixture_attachment(&record)),mock)}).await.unwrap();
                if scenario=="absent" {assert!(!closed.unwrap());} else {assert!(closed.is_err(), "{scenario}");}
            }
        });
    }
    #[test]
    fn registrar_rejection_never_reaches_backend_and_connection_remains_usable() {
        runtime().unwrap().block_on(async {
            let root = tempfile::tempdir().unwrap();
            fs::write(root.path().join("job.json"), json!({"thread_id":"session"}).to_string()).unwrap();
            let (mut agent, proxy_agent) = tokio::io::duplex(65536);
            let (proxy_backend, mut backend) = tokio::io::duplex(65536);
            let info = json!({});
            let proxy = forward_streams(proxy_agent, proxy_backend, "fixture", &info, root.path(), "job");
            let check = async {
                for id in [1, 2] {
                    let (method, params) = if id == 1 {
                        ("createTab", json!({"session_id":"session","url":"https://onamae.com"}))
                    } else {
                        ("executeCdp", json!({"session_id":"session","method":"Page.navigate","commandParams":{"url":"https://navi.onamae.com"}}))
                    };
                    write_frame(&mut agent, &json!({"id":id,"method":method,"params":params})).await.unwrap();
                    let reply = read_frame(&mut agent).await.unwrap();
                    assert_eq!(reply["id"], id);
                    assert_eq!(reply["error"]["code"], -32000);
                }
                // The first frame reaching the backend must be this allowed request.
                let allowed = json!({"id":3,"method":"getUserTabs","params":{"session_id":"session"}});
                write_frame(&mut agent, &allowed).await.unwrap();
                assert_eq!(read_frame(&mut backend).await.unwrap(), allowed);
                write_frame(&mut backend, &json!({"id":3,"result":[]})).await.unwrap();
                assert_eq!(read_frame(&mut agent).await.unwrap()["id"], 3);
                assert!(!root.path().join("chrome-tabs").exists());
            };
            tokio::pin!(proxy);
            tokio::select! {
                result = &mut proxy => panic!("proxy ended before checks: {result:?}"),
                // This is a deadlock watchdog for the complete fixture, not a
                // production request deadline. Parallel Windows CI can spend
                // several seconds scheduling the otherwise immediate frames.
                result = tokio::time::timeout(Duration::from_secs(30), check) => { result.expect("registrar checks timed out"); },
            }
        });
    }
    #[test]
    fn simultaneous_policy_rejection_and_backend_reply_preserve_frame_boundaries() {
        runtime().unwrap().block_on(async {
            let root = tempfile::tempdir().unwrap();
            fs::write(root.path().join("job.json"), json!({"thread_id":"session"}).to_string()).unwrap();
            let (mut agent, proxy_agent) = tokio::io::duplex(65536);
            let (proxy_backend, mut backend) = tokio::io::duplex(65536);
            let info = json!({});
            let proxy = forward_streams(proxy_agent, proxy_backend, "fixture", &info, root.path(), "job");
            let check = async {
                write_frame(&mut agent, &json!({"id":1,"method":"getUserTabs","params":{"session_id":"session"}})).await.unwrap();
                assert_eq!(read_frame(&mut backend).await.unwrap()["id"], 1);
                let denied = json!({"id":2,"method":"executeCdp","params":{"session_id":"session","method":"Page.navigate","commandParams":{"url":"https://onamae.com"}}});
                let response = json!({"id":1,"result":[]});
                let (sent, replied) = tokio::join!(write_frame(&mut agent, &denied), write_frame(&mut backend, &response));
                sent.unwrap();
                replied.unwrap();
                let first = read_frame(&mut agent).await.unwrap();
                let second = read_frame(&mut agent).await.unwrap();
                let mut ids = [first["id"].as_u64().unwrap(), second["id"].as_u64().unwrap()];
                ids.sort();
                assert_eq!(ids, [1, 2]);
                for reply in [first, second] {
                    if reply["id"] == 2 {
                        assert_eq!(reply["error"]["code"], -32000);
                    } else {
                        assert_eq!(reply["result"], json!([]));
                    }
                }
                write_frame(&mut agent, &json!({"id":3,"method":"getInfo","params":{"session_id":"session"}})).await.unwrap();
                assert_eq!(read_frame(&mut backend).await.unwrap()["id"], 3);
            };
            tokio::pin!(proxy);
            tokio::select! {
                result = &mut proxy => panic!("proxy ended before checks: {result:?}"),
                result = tokio::time::timeout(Duration::from_secs(3), check) => { result.expect("concurrent replies timed out"); },
            }
        });
    }
    #[test]
    fn proxy_records_only_successful_creation_before_returning_it() {
        runtime().unwrap().block_on(async {
            let root=tempfile::tempdir().unwrap();
            fs::write(root.path().join("job.json"),json!({"id":"job","thread_id":"session"}).to_string()).unwrap();
            let (mut agent,proxy_agent)=tokio::io::duplex(65536);
            let (proxy_backend,mut backend)=tokio::io::duplex(65536);
            let info=json!({"metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
            let proxy=forward_streams(proxy_agent,proxy_backend,"upstream",&info,root.path(),"job");
            let check=async {
                for (id,method,result) in [(1,"getTabs",json!([{"id":999}])),(2,"claimUserTab",json!({"id":999})),(3,"createTab",json!({"id":10}))] {
                    let v=json!({"jsonrpc":"2.0","id":id,"method":method,"params":{"session_id":"session","turn_id":"turn"}});
                    write_frame(&mut agent,&v).await.unwrap();assert_eq!(read_frame(&mut backend).await.unwrap(),v);
                    write_frame(&mut backend,&json!({"jsonrpc":"2.0","id":id,"result":result})).await.unwrap();
                    read_frame(&mut agent).await.unwrap();
                    if id<3 {assert!(!root.path().join("chrome-tabs").exists());}
                }
                let files:Vec<_>=fs::read_dir(root.path().join("chrome-tabs")).unwrap().collect();assert_eq!(files.len(),1);
                let r:Record=serde_json::from_slice(&fs::read(files[0].as_ref().unwrap().path()).unwrap()).unwrap();
                assert_eq!((r.tab,r.job.as_str(),r.session.as_str()),(10,"job","session"));
                write_frame(&mut agent,&json!({"id":4,"method":"createTab","params":{"session_id":"session","turn_id":"turn"}})).await.unwrap();read_frame(&mut backend).await.unwrap();
                write_frame(&mut backend,&json!({"id":4,"error":{"message":"refused"}})).await.unwrap();read_frame(&mut agent).await.unwrap();
                assert_eq!(fs::read_dir(root.path().join("chrome-tabs")).unwrap().count(),1);
                // Another job/session is refused before reaching the extension.
                write_frame(&mut agent,&json!({"id":5,"method":"createTab","params":{"session_id":"other","turn_id":"turn"}})).await.unwrap();
                tokio::time::sleep(Duration::from_millis(10)).await;
            };
            let (p,_)=tokio::join!(proxy,check);assert!(p.unwrap_err().to_string().contains("does not own"));
        });
    }
    #[test]
    fn creation_reply_is_recorded_after_agent_disconnects() {
        runtime().unwrap().block_on(async {
            let root=tempfile::tempdir().unwrap();
            fs::write(root.path().join("job.json"),json!({"id":"job","thread_id":"session"}).to_string()).unwrap();
            let (mut agent,proxy_agent)=tokio::io::duplex(65536);
            let (proxy_backend,mut backend)=tokio::io::duplex(65536);
            let info=json!({"metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
            let proxy=forward_streams(proxy_agent,proxy_backend,"upstream",&info,root.path(),"job");
            let disconnect=async move {
                write_frame(&mut agent,&json!({"id":1,"method":"createTab","params":{"session_id":"session","turn_id":"turn"}})).await.unwrap();
                let v=read_frame(&mut backend).await.unwrap();
                drop(agent);
                tokio::time::sleep(Duration::from_millis(30)).await;
                write_frame(&mut backend,&json!({"id":v["id"],"result":{"id":42}})).await.unwrap();
            };
            let _=tokio::join!(proxy,disconnect);
            let paths:Vec<_>=fs::read_dir(root.path().join("chrome-tabs")).unwrap().collect();
            assert_eq!(paths.len(),1);
            let r:Record=serde_json::from_slice(&fs::read(paths[0].as_ref().unwrap().path()).unwrap()).unwrap();
            assert_eq!(r.tab,42);
        });
    }
    #[test]
    fn discovery_skips_another_profile_instead_of_discarding_the_record() {
        runtime().unwrap().block_on(async {
            let mut pipes=vec![];let mut servers=vec![];
            for _ in 0..2 {
                let pipe=format!(r"\\.\pipe\cc-chrome-discovery-{}",uuid::Uuid::new_v4());
                servers.push(ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap());pipes.push(pipe);
            }
            let record=Record{job:"job".into(),pipe:pipes[1].clone(),instance:"wanted".into(),generation:"generation".into(),session:"session".into(),turn:"turn".into(),tab:42,created:100};
            let backend=async move {
                for (mut server,instance) in servers.into_iter().zip(["other","wanted"]) {
                    server.connect().await.unwrap();let v=read_frame(&mut server).await.unwrap();
                    write_frame(&mut server,&json!({"id":v["id"],"result":{"type":"extension","family":"chrome","metadata":{"extensionInstanceId":instance},"browserTabMentions":{"generation":"generation"}}})).await.unwrap();
                }
            };
            let (result,_)=tokio::join!(discover_from(pipes,Some(&record)),backend);
            let (pipe,info)=result.unwrap();assert_eq!(pipe,record.pipe);assert!(record.matches_backend(&info));
        });
    }
    #[test]
    fn supervisor_without_worker_does_not_protect_tabs() {
        let job = json!({"pid":std::process::id(),"pid_created":1,"codex_pid":null});
        assert_eq!(NativeCloser.owner_running(&job), Some(false));
        let job = json!({"codex_pid":std::process::id(),"codex_pid_created":null});
        assert_eq!(NativeCloser.owner_running(&job), None);
    }
    #[test]
    fn proxy_rolls_back_unrecordable_creation_and_returns_error_instead_of_success() {
        runtime().unwrap().block_on(async {
          for history_writable in [false, true] {
            let root=tempfile::tempdir().unwrap();
            fs::write(root.path().join("job.json"),json!({"id":"job","thread_id":"session","status":"running"}).to_string()).unwrap();
            fs::write(root.path().join("chrome-tabs"),"not a directory").unwrap();
            if !history_writable { fs::write(root.path().join("chrome-tab-history"), "not a directory").unwrap(); }
            let pipe=format!(r"\\.\pipe\cc-chrome-rollback-{}",uuid::Uuid::new_v4());
            let mut server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
            let (mut agent,proxy_agent)=tokio::io::duplex(65536);
            let (proxy_backend,mut backend)=tokio::io::duplex(65536);
            let info=json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
            context::observe_attachment(root.path(),"job",&pipe,&info,&json!({"session_id":"session","turn_id":"turn","session_context":"live","networkPolicy":{"constraints":[{"kind":"domains","mode":"denylist","patterns":["blocked.invalid"]}]}})).unwrap();
            let proxy=forward_streams(proxy_agent,proxy_backend,&pipe,&info,root.path(),"job");
            let check=async {
                write_frame(&mut agent,&json!({"id":1,"method":"createTab","params":{"session_id":"session","turn_id":"turn"}})).await.unwrap();
                let v=read_frame(&mut backend).await.unwrap();
                write_frame(&mut backend,&json!({"id":v["id"],"result":{"id":42}})).await.unwrap();
                let v=read_frame(&mut agent).await.unwrap();assert!(v.get("error").is_some());assert!(v.get("result").is_none());
                let pending=fs::read_dir(root.path().join("chrome-emergency-tabs")).unwrap().map(|entry|entry.unwrap().path()).collect::<Vec<_>>();
                if history_writable {
                    assert!(pending.is_empty());
                    let histories=fs::read_dir(root.path().join("chrome-tab-history")).unwrap().map(|entry|entry.unwrap().path()).collect::<Vec<_>>();
                    assert_eq!(histories.len(),1);
                    let history:Value=serde_json::from_slice(&fs::read(&histories[0]).unwrap()).unwrap();
                    assert_eq!(history["record"]["tab"],42);
                    assert_eq!(history["record"]["session"],"session");
                    assert_eq!(history["record"]["generation"],"generation");
                    assert_eq!(history["closed"],true);
                    assert_eq!(history["completion_reason"],"creation rolled back after tab record write failed");
                } else {
                    assert_eq!(pending.len(),1,"unpublished history must retain emergency ownership");
                    let record:Record=serde_json::from_slice(&fs::read(&pending[0]).unwrap()).unwrap();
                    assert_eq!(record.tab,42);
                    assert_eq!(record.session,"session");
                }
                agent.shutdown().await.unwrap();
            };
            let rollback=async {
                server.connect().await.unwrap();
                for (method,result) in [("getInfo",info.clone()),("getTabs",json!([{"id":42}])),("getInfo",info.clone()),("attach",json!({})),("getInfo",info.clone()),("executeCdp",json!({})),("getUserTabs",json!([{"id":99}])),("getInfo",info.clone())] {
                    let v=read_frame(&mut server).await.unwrap();assert_eq!(v["method"],method);
                    if method=="executeCdp" {assert_eq!(v["params"]["method"],"Page.close");assert_eq!(v["params"]["target"]["tabId"],42);}
                    write_frame(&mut server,&json!({"id":v["id"],"result":result})).await.unwrap();
                }
            };
            let (result,(),())=tokio::time::timeout(Duration::from_secs(5),async {tokio::join!(proxy,check,rollback)}).await.unwrap();
            assert!(result.is_err());
          }
        });
    }
    #[test]
    fn failed_rollback_keeps_emergency_record_for_the_next_sweep() {
        runtime().unwrap().block_on(async {
            let root = tempfile::tempdir().unwrap();
            let dir = root.path().join("job");
            fs::create_dir(&dir).unwrap();
            fs::write(
                dir.join("job.json"),
                json!({"id":"job","status":"failed"}).to_string(),
            )
            .unwrap();
            fs::write(dir.join("chrome-tabs"), "not a directory").unwrap();
            let pipe = format!(
                r"\\.\pipe\cc-chrome-rollback-offline-{}",
                uuid::Uuid::new_v4()
            );
            let mut server = ServerOptions::new()
                .first_pipe_instance(true)
                .create(&pipe)
                .unwrap();
            let record = Record {
                job: "job".into(),
                pipe,
                instance: "instance".into(),
                generation: "generation".into(),
                session: "session".into(),
                turn: "turn".into(),
                tab: 42,
                created: 100,
            };
            let offline = async {
                server.connect().await.unwrap();
                let v = read_frame(&mut server).await.unwrap();
                write_frame(
                    &mut server,
                    &json!({"id":v["id"],"error":{"message":"offline"}}),
                )
                .await
                .unwrap();
            };
            let (result, _) = tokio::join!(persist_or_close(&dir, &record), offline);
            assert!(result.is_err());
            assert_eq!(
                fs::read_dir(dir.join("chrome-emergency-tabs"))
                    .unwrap()
                    .count(),
                1
            );
            struct Closed;
            impl Closer for Closed {
                fn close(&mut self, r: &Record) -> io::Result<bool> {
                    assert_eq!(r.tab, 42);
                    Ok(true)
                }
            }
            assert_eq!(
                sweep_with(root.path(), None, 101, false, &mut Closed)
                    .unwrap()
                    .closed,
                1
            );
            assert_eq!(
                fs::read_dir(dir.join("chrome-emergency-tabs"))
                    .unwrap()
                    .count(),
                0
            );
        });
    }
    #[test]
    fn immediately_disconnected_probes_do_not_stop_recording_or_drain() {
        runtime().unwrap().block_on(async {
            let root=tempfile::tempdir().unwrap();
            fs::write(root.path().join("job.json"),json!({"id":"job","thread_id":"session"}).to_string()).unwrap();
            let pipe=format!(r"\\.\pipe\cc-chrome-probe-{}",uuid::Uuid::new_v4());
            let server=ServerOptions::new().first_pipe_instance(true).create(&pipe).unwrap();
            // Force a probe to disconnect before the listener starts connecting.
            let probe=connect(&pipe).await.unwrap();drop(probe);
            let (stop,stopped)=tokio::sync::oneshot::channel();
            let dir=root.path().to_path_buf();
            let serve=accept_loop(server,&pipe,stopped,move |client| {
                let dir=dir.clone();
                async move {
                    let (proxy_backend,mut backend)=tokio::io::duplex(65536);
                    let info=json!({"metadata":{"extensionInstanceId":"instance"},"browserTabMentions":{"generation":"generation"}});
                    let proxy=forward_streams(client,proxy_backend,"upstream",&info,&dir,"job");
                    let fixture=async {
                        if let Ok(v)=read_frame(&mut backend).await {
                            let _=write_frame(&mut backend,&json!({"id":v["id"],"result":{"id":42}})).await;
                        }
                    };
                    let _=tokio::join!(proxy,fixture);
                }
            });
            let probes=async {
                for _ in 0..12 {let client=connect(&pipe).await.unwrap();drop(client);}
                let mut client=connect(&pipe).await.unwrap();
                write_frame(&mut client,&json!({"id":1,"method":"createTab","params":{"session_id":"session","turn_id":"turn"}})).await.unwrap();
                let v=read_frame(&mut client).await.unwrap();assert_eq!(v["result"]["id"],42);
                assert_eq!(fs::read_dir(root.path().join("chrome-tabs")).unwrap().count(),1);
                drop(client);let _=stop.send(());
            };
            tokio::time::timeout(Duration::from_secs(5),async {tokio::join!(serve,probes)}).await.unwrap();
        });
    }
}
