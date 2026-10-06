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
fn running(pid:u32,created:u64)->Option<bool> {
    use windows_sys::Win32::{Foundation::*,System::Threading::*};
    unsafe {
        let handle=OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION,0,pid);
        if handle.is_null() {return if GetLastError()==ERROR_INVALID_PARAMETER {Some(false)} else {None};}
        let mut code=0;
        let mut times=[FILETIME{dwLowDateTime:0,dwHighDateTime:0};4];
        let ok=GetExitCodeProcess(handle,&mut code)!=0 && GetProcessTimes(handle,&mut times[0],&mut times[1],&mut times[2],&mut times[3])!=0;
        CloseHandle(handle);
        ok.then_some(code==259 && (((times[0].dwHighDateTime as u64)<<32)|times[0].dwLowDateTime as u64)==created)
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
    for entry in fs::read_dir(r"\\.\pipe\")? {
        let name = entry?.file_name().to_string_lossy().into_owned();
        if !name.starts_with("codex-browser-use\\") || name.contains("cc-job-") {
            continue;
        }
        let pipe = format!(r"\\.\pipe\{name}");
        let Ok(mut rpc) = Rpc::connect(&pipe).await else {
            continue;
        };
        let Ok(info)=rpc.call("getInfo",json!({"session_id":"cc-chrome-discovery","turn_id":"discovery","session_context":"live"})).await else {continue};
        if info["type"] == "extension" && info["family"] == "chrome" {
            return Ok((pipe, info));
        }
    }
    Err(io::Error::other("owner Chrome extension is not connected"))
}
pub struct NativeCloser;
impl Closer for NativeCloser {
    fn owner_running(&mut self, job: &Value) -> Option<bool> {
        let mut unknown = false;
        for key in ["pid", "codex_pid"] {
            let Some(pid) = job[key].as_u64().and_then(|n| u32::try_from(n).ok()) else { continue; };
            let Some(created) = job[format!("{key}_created")].as_u64().filter(|n| *n != 0) else { unknown=true; continue; };
            match running(pid, created) { Some(true)=>return Some(true), None=>unknown=true, _=>{} }
        }
        if unknown { None } else { Some(false) }
    }
    fn close(&mut self, r: &Record) -> io::Result<bool> {
        runtime()?.block_on(async {
            tokio::time::timeout(Duration::from_secs(3), close_record(r)).await.map_err(io::Error::other)?
        })
    }
}
async fn close_record(r: &Record) -> io::Result<bool> {
    // A host can reconnect while Chrome stays open. Check both browser identities.
    let mut rpc = match Rpc::connect(&r.pipe).await {
        Ok(v) => v,
        Err(e) if e.kind() == io::ErrorKind::NotFound => {
            let (pipe, info) = discover().await?;
            if !r.matches_backend(&info) { return Ok(false); }
            Rpc::connect(&pipe).await?
        },
        Err(e) => return Err(e),
    };
    let info = rpc.call("getInfo", r.params()).await?;
    if !r.matches_backend(&info) {
        return Ok(false);
    }
    let tabs = rpc.call("getTabs", r.params()).await?;
    let tabs = tabs
        .as_array()
        .ok_or_else(|| io::Error::other("invalid session tab inventory"))?;
    if !tabs.iter().any(|t| t["id"].as_u64() == Some(r.tab)) {
        // A turn-end hook may release a marked tab from session control while
        // leaving it open. Reclaim only this positively recorded agent-created ID.
        let all=rpc.call("getUserTabs",r.params()).await?;
        if !has_tab(&all,r.tab)? {return Ok(false);}
        let mut p=r.params();p["tabId"]=json!(r.tab);
        rpc.call("claimUserTab",p).await?;
    }
    let mut p = r.params();
    p["tabId"] = json!(r.tab);
    rpc.call("attach", p).await?;
    let mut p = r.params();
    p["target"] = json!({"tabId":r.tab});
    p["method"] = json!("Page.close");
    p["commandParams"] = json!({});
    rpc.call("executeCdp", p).await?;
    let tabs = rpc.call("getUserTabs", r.params()).await?;
    if has_tab(&tabs,r.tab)? {
        return Err(io::Error::other("tab still open after close"));
    }
    Ok(true)
}
fn has_tab(tabs:&Value,id:u64)->io::Result<bool> {
    let tabs=tabs.as_array().ok_or_else(||io::Error::other("invalid full tab inventory"))?;
    Ok(tabs.iter().any(|t|["id","providerTabId","tabId"].iter().any(|key|t[*key].as_u64().or_else(||t[*key].as_str()?.parse().ok())==Some(id))))
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
            let mut server=ServerOptions::new().first_pipe_instance(true).create(&service_pipe)?;
            let _=ready_tx.send(Ok(()));
            let mut tasks=tokio::task::JoinSet::new();tokio::pin!(stop_rx);
            loop {
                tokio::select! {
                    _=&mut stop_rx=>{tasks.shutdown().await;return Ok(());},
                    result=server.connect()=>{
                        result?;
                        let next=ServerOptions::new().create(&service_pipe)?;
                        let connected=std::mem::replace(&mut server,next);
                        let dir=path.clone();let job=job.clone();
                        tasks.spawn(async move{if let Ok((upstream,info))=discover().await{let _=forward(connected,&upstream,&info,&dir,&job).await;}});
                    },
                    _=tasks.join_next(),if !tasks.is_empty()=>{},
                }
            }
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
    upstream: &str, info: &Value, dir: &Path, job: &str,
) -> io::Result<()> {
    let (mut ar, mut aw) = tokio::io::split(server);
    let (mut br, mut bw) = tokio::io::split(client);
    let pending = Arc::new(Mutex::new(HashMap::<String, Value>::new()));
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
                if v["method"] == "createTab" && v.get("id").is_some() {
                    sending
                        .lock()
                        .unwrap()
                        .insert(v["id"].to_string(), v["params"].clone());
                }
            }
            write_frame(&mut bw, &v).await?;
        }
        #[allow(unreachable_code)]
        Ok::<(), io::Error>(())
    };
    let down = async {
        loop {
            let v = read_frame(&mut br).await?;
            if v.get("method").is_none()
                && let Some(params) = pending.lock().unwrap().remove(&v["id"].to_string())
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
                    persist(dir, &record)?; // Durable BEFORE exposing the new tab to the agent.
            }
            write_frame(&mut aw, &v).await?;
        }
        #[allow(unreachable_code)]
        Ok::<(), io::Error>(())
    };
    tokio::select! { r=up=>r,r=down=>r }
}
pub fn open(dir: &Path, job: &str, url: &str) -> io::Result<Record> {
    if !safe_job(job)
        || !(url.starts_with("https://") || url.starts_with("http://") || url == "about:blank")
    {
        return Err(io::Error::other("invalid job or URL"));
    }
    runtime()?.block_on(async {
        let (pipe, info) = discover().await?;
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
            session: format!("cc-chrome-{}", uuid::Uuid::new_v4()),
            turn: "open".into(),
            tab: 0,
            created: epoch(),
        };
        let tab = rpc.call("createTab", r.params()).await?;
        r.tab = tab["id"]
            .as_u64()
            .ok_or_else(|| io::Error::other("invalid createTab reply"))?;
        persist(dir, &r)?;
        let mut p = r.params();
        p["tabId"] = json!(r.tab);
        rpc.call("attach", p).await?;
        let mut p = r.params();
        p["target"] = json!({"tabId":r.tab});
        p["method"] = json!("Page.navigate");
        p["commandParams"] = json!({"url":url});
        rpc.call("executeCdp", p).await?;
        Ok(r)
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rpc_deadline_includes_write_to_a_connected_pipe_that_never_reads() {
        runtime().unwrap().block_on(async {
            let pipe = format!(r"\\.\pipe\cc-chrome-stalled-write-{}", uuid::Uuid::new_v4());
            let server = ServerOptions::new().first_pipe_instance(true).in_buffer_size(4096).create(&pipe).unwrap();
            let (stop, stopped) = tokio::sync::oneshot::channel();
            let connected = async {
                let mut rpc = Rpc::connect(&pipe).await.unwrap();
                let start = std::time::Instant::now();
                let result = rpc.call("fixture", json!({"large":"x".repeat(1024 * 1024)})).await;
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
                    ("claimUserTab",json!({"id":11})),("attach",json!({})),("executeCdp",json!({})),
                    ("getUserTabs",json!([{"id":"99"}])),
                ] {
                    let v=read_frame(&mut server).await.unwrap();assert_eq!(v["method"],method);
                    if ["claimUserTab","attach"].contains(&method) {assert_eq!(v["params"]["tabId"],11);}
                    if method=="executeCdp" {assert_eq!(v["params"]["target"]["tabId"],11);assert_eq!(v["params"]["method"],"Page.close");}
                    write_frame(&mut server,&json!({"jsonrpc":"2.0","id":v["id"],"result":result})).await.unwrap();
                }
            };
            let (closed,_)=tokio::join!(close_record(&record),mock);assert!(closed.unwrap());
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
}
