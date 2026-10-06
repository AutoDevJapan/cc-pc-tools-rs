//! Track only successful agent createTab replies; never infer ownership from URLs or groups.
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    fs, io,
    path::{Path, PathBuf},
};

#[cfg(windows)]
pub mod native;

pub fn needs_browser(task: &str) -> bool {
    let task = task.to_lowercase();
    [
        "http://",
        "https://",
        "website",
        "web site",
        "webpage",
        "browser",
        "chrome",
        "login",
        "log in",
        "sign in",
        "screen check",
        "screenshot",
        "ブラウザ",
        "サイト",
        "ログイン",
        "画面確認",
        "画面の確認",
        "画面を確認",
        "画面チェック",
        "スクリーンショット",
        "表示確認",
    ]
    .iter()
    .any(|s| task.contains(s))
}
pub fn profile<'a>(requested: &'a str, task: &str) -> &'a str {
    if requested != "full" && needs_browser(task) {
        "browser"
    } else {
        requested
    }
}
pub fn epoch() -> i64 {
    chrono::Utc::now().timestamp()
}
/// Let the bundled CUA server inherit the per-job native-pipe route. Do not replace
/// its transport table with a partial user override (Codex rejects that table).
pub fn install_plugin_hook(home: &Path) -> io::Result<usize> {
    let root=home.join(".codex/plugins/cache/openai-bundled/unified-computer-use");
    if !root.exists() {return Ok(0);}
    let mut count=0;
    let mut seen=std::collections::HashSet::new();
    for entry in fs::read_dir(root)? {
        let path=entry?.path().join(".mcp.json");
        if !path.is_file() || !seen.insert(fs::canonicalize(&path)?) {continue;}
        let bytes=fs::read(&path)?;
        let mut value:Value=serde_json::from_slice(&bytes)?;
        let server=&mut value["mcpServers"]["cua_repl"];
        if !server.is_object(){continue;}
        if server["env_vars"].is_null(){server["env_vars"]=json!([]);}
        let vars=server["env_vars"].as_array_mut().ok_or_else(||io::Error::other("plugin env_vars is invalid"))?;
        let mut changed=false;
        for key in ["CC_JOB_ID","CDP_BROWSER_BACKEND_PIPE_PATH","BROWSER_AUTH_EVAL_EXACT_CDP_BACKEND_SOCKET"] {
            if !vars.iter().any(|v|v==key){vars.push(json!(key));changed=true;}
        }
        if changed {
            let backup=path.with_extension("chrome-hygiene.before.json");
            if !backup.exists(){fs::write(backup,bytes)?;}
            let mut temp=tempfile::NamedTempFile::new_in(path.parent().unwrap())?;
            serde_json::to_writer_pretty(&mut temp,&value)?;
            temp.as_file().sync_all()?;temp.persist(&path).map_err(|e|e.error)?;
            count+=1;
        }
    }
    Ok(count)
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq)]
pub struct Record {
    pub job: String,
    pub pipe: String,
    pub instance: String,
    pub generation: String,
    pub session: String,
    pub turn: String,
    pub tab: u64,
    pub created: i64,
}
impl Record {
    pub fn params(&self) -> Value {
        json!({"session_id":self.session,"turn_id":self.turn,"session_context":"live"})
    }
    pub fn matches_backend(&self, info: &Value) -> bool {
        info["type"] == "extension"
            && info["family"] == "chrome"
            && info["metadata"]["extensionInstanceId"] == self.instance
            && info["browserTabMentions"]["generation"] == self.generation
            && !self.instance.is_empty()
            && !self.generation.is_empty()
    }
}
pub fn safe_job(job: &str) -> bool {
    !job.is_empty()
        && job.len() <= 160
        && job
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}
pub fn persist(dir: &Path, record: &Record) -> io::Result<PathBuf> {
    if !safe_job(&record.job) || record.tab == 0 {
        return Err(io::Error::other("invalid tab identity"));
    }
    let folder = dir.join("chrome-tabs");
    fs::create_dir_all(&folder)?;
    let path = folder.join(format!("{}.json", uuid::Uuid::new_v4()));
    let mut temp = tempfile::NamedTempFile::new_in(&folder)?;
    serde_json::to_writer(&mut temp, record)?;
    temp.as_file().sync_all()?;
    temp.persist(&path).map_err(|e| e.error)?;
    Ok(path)
}
/// A queued/running owner is protected even at >30 minutes. Unreadable state is protected.
pub fn eligible(job: Option<&Value>, record: &Record, now: i64) -> bool {
    match job {
        Some(j)
            if ["succeeded", "failed", "cancelled", "lost"]
                .contains(&j["status"].as_str().unwrap_or("")) =>
        {
            true
        }
        Some(j)
            if ["queued", "starting", "running"].contains(&j["status"].as_str().unwrap_or("")) =>
        {
            false
        }
        Some(_) => false,
        None => now.saturating_sub(record.created) >= 30 * 60,
    }
}
#[derive(Default, Debug)]
pub struct Sweep {
    pub closed: usize,
    pub gone: usize,
    pub kept: usize,
    pub failed: usize,
    pub deferred: bool,
}
pub trait Closer {
    fn owner_running(&mut self, _job: &Value) -> Option<bool> { None }
    /// True means closed, false means already gone or backend identity changed.
    fn close(&mut self, record: &Record) -> io::Result<bool>;
}
pub fn sweep_with(
    jobs: &Path,
    only: Option<&str>,
    now: i64,
    dry_run: bool,
    closer: &mut dyn Closer,
) -> io::Result<Sweep> {
    let mut result = Sweep::default();
    let started = std::time::Instant::now();
    for dir in fs::read_dir(jobs)? {
        let dir = dir?;
        if !dir.file_type()?.is_dir() {
            continue;
        }
        let name = dir.file_name().to_string_lossy().into_owned();
        if !safe_job(&name) || only.is_some_and(|id| id != name) {
            continue;
        }
        let folder = dir.path().join("chrome-tabs");
        let entries = match fs::read_dir(folder) {
            Ok(v) => v,
            Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
            Err(e) => return Err(e),
        };
        for entry in entries {
            if result.closed + result.gone + result.failed >= 64 || started.elapsed() >= std::time::Duration::from_secs(8) { return Ok(result); }
            let entry = entry?;
            if entry.path().extension().and_then(|s| s.to_str()) != Some("json") {
                continue;
            }
            let record: Record = match fs::read(entry.path())
                .and_then(|b| serde_json::from_slice::<Record>(&b).map_err(io::Error::other))
            {
                Ok(v) if v.job == name => v,
                _ => {
                    result.failed += 1;
                    continue;
                }
            };
            // Re-read the job before every close; do not let a corrupt/mid-write file authorize cleanup.
            let job = match fs::read(dir.path().join("job.json")) {
                Ok(b) => match serde_json::from_slice::<Value>(&b) {
                    Ok(j) if j["id"] == name => Some(j),
                    _ => {
                        result.kept += 1;
                        continue;
                    }
                },
                Err(e) if e.kind() == io::ErrorKind::NotFound => None,
                Err(_) => {
                    result.kept += 1;
                    continue;
                }
            };
            let abandoned = job.as_ref().is_some_and(|j| ["running", "starting", "queued"].contains(&j["status"].as_str().unwrap_or("")) && closer.owner_running(j) == Some(false))
                && now.saturating_sub(record.created) >= 30 * 60;
            let live_or_unknown_lost = job.as_ref().is_some_and(|j| j["status"] == "lost" && closer.owner_running(j) != Some(false));
            if live_or_unknown_lost || (!eligible(job.as_ref(), &record, now) && !abandoned) || dry_run {
                result.kept += 1;
                continue;
            }
            match closer.close(&record) {
                Ok(closed) => {
                    if closed {
                        result.closed += 1;
                    } else {
                        result.gone += 1;
                    }
                    match fs::remove_file(entry.path()) {Ok(())=>{},Err(e) if e.kind()==io::ErrorKind::NotFound=>{},Err(e)=>return Err(e)};
                }
                Err(_) => result.failed += 1,
            }
        }
    }
    Ok(result)
}
pub fn sweep(jobs: &Path, only: Option<&str>, dry_run: bool) -> io::Result<Sweep> {
    let lock = fs::OpenOptions::new().create(true).truncate(false).read(true).write(true).open(jobs.join("chrome-cleanup.lock"))?;
    if lock.try_lock().is_err() {return Ok(Sweep {deferred:true,..Default::default()});}
    #[cfg(windows)]
    {
        sweep_with(jobs, only, epoch(), dry_run, &mut native::NativeCloser)
    }
    #[cfg(not(windows))]
    {
        let _ = (jobs, only, dry_run);
        Err(io::Error::other("Chrome extension requires Windows"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn plugin_hook_preserves_transport_and_is_idempotent() {
        let root=tempfile::tempdir().unwrap();
        let dir=root.path().join(".codex/plugins/cache/openai-bundled/unified-computer-use/version");
        fs::create_dir_all(&dir).unwrap();
        let path=dir.join(".mcp.json");
        let initial=json!({"mcpServers":{"cua_repl":{"command":"node.exe","args":["cua.mjs"],"env":{"NODE_REPL_TRUSTED_SERVICES":"service"},"env_vars":["EXISTING"]}}});
        fs::write(&path,initial.to_string()).unwrap();
        assert_eq!(install_plugin_hook(root.path()).unwrap(),1);
        assert_eq!(install_plugin_hook(root.path()).unwrap(),0);
        let value:Value=serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
        for key in ["command","args","env"] {assert_eq!(value["mcpServers"]["cua_repl"][key],initial["mcpServers"]["cua_repl"][key]);}
        assert_eq!(value["mcpServers"]["cua_repl"]["env_vars"],json!(["EXISTING","CC_JOB_ID","CDP_BROWSER_BACKEND_PIPE_PATH","BROWSER_AUTH_EVAL_EXACT_CDP_BACKEND_SOCKET"]));
    }
    #[cfg(windows)]
    #[test]
    fn sweep_and_job_completion_share_one_cleanup_lock() {
        let root=tempfile::tempdir().unwrap();
        let guard=fs::OpenOptions::new().create(true).truncate(false).read(true).write(true).open(root.path().join("chrome-cleanup.lock")).unwrap();
        guard.lock().unwrap();assert!(sweep(root.path(),None,true).unwrap().deferred);
        drop(guard);assert!(!sweep(root.path(),None,true).unwrap().deferred);
    }
    struct Fake(Vec<u64>, bool);
    impl Closer for Fake {
        fn close(&mut self, r: &Record) -> io::Result<bool> {
            if self.1 {
                return Err(io::Error::other("offline"));
            }
            self.0.push(r.tab);
            Ok(true)
        }
    }
    fn record(job: &str, tab: u64) -> Record {
        Record {
            job: job.into(),
            tab,
            pipe: "pipe".into(),
            instance: "extension".into(),
            generation: "boot".into(),
            session: "session".into(),
            turn: "turn".into(),
            created: 100,
        }
    }
    fn job(root: &Path, name: &str, status: &str) {
        let dir = root.join(name);
        fs::create_dir_all(&dir).unwrap();
        fs::write(
            dir.join("job.json"),
            json!({"id":name,"status":status}).to_string(),
        )
        .unwrap();
    }
    #[test]
    fn sweep_only_recorded_terminal_tabs_and_retry_offline() {
        let root = tempfile::tempdir().unwrap();
        job(root.path(), "done", "succeeded");
        job(root.path(), "live", "running");
        let p = persist(&root.path().join("done"), &record("done", 11)).unwrap();
        persist(&root.path().join("live"), &record("live", 22)).unwrap();
        let mut fake = Fake(vec![], true);
        let r = sweep_with(root.path(), None, 5000, false, &mut fake).unwrap();
        assert_eq!((r.failed, r.kept), (1, 1));
        assert!(p.exists());
        fake.1 = false;
        let r = sweep_with(root.path(), None, 5000, false, &mut fake).unwrap();
        assert_eq!((r.closed, r.kept), (1, 1));
        assert_eq!(fake.0, vec![11]);
        assert!(!p.exists());
        // Owner tab 33 is deliberately absent from the ledger and can never reach close().
    }
    #[test]
    fn orphan_grace_corruption_and_dry_run() {
        let root = tempfile::tempdir().unwrap();
        fs::create_dir(root.path().join("orphan")).unwrap();
        let p = persist(&root.path().join("orphan"), &record("orphan", 7)).unwrap();
        let mut fake = Fake(vec![], false);
        assert_eq!(
            sweep_with(root.path(), None, 1899, false, &mut fake)
                .unwrap()
                .kept,
            1
        );
        assert_eq!(
            sweep_with(root.path(), None, 1900, true, &mut fake)
                .unwrap()
                .kept,
            1
        );
        fs::write(root.path().join("orphan/job.json"), "broken").unwrap();
        assert_eq!(
            sweep_with(root.path(), None, 9999, false, &mut fake)
                .unwrap()
                .kept,
            1
        );
        fs::remove_file(root.path().join("orphan/job.json")).unwrap();
        assert_eq!(
            sweep_with(root.path(), None, 1900, false, &mut fake)
                .unwrap()
                .closed,
            1
        );
        assert!(!p.exists());
    }
    #[test]
    fn profiles_and_restart_identity() {
        assert_eq!(profile("minimal", "ログインして画面確認"), "browser");
        assert_eq!(profile("minimal", "画面を確認して"), "browser");
        assert_eq!(profile("full", "website check"), "full");
        assert_eq!(profile("minimal", "compile Rust"), "minimal");
        let r = record("j", 1);
        let mut info = json!({"type":"extension","family":"chrome","metadata":{"extensionInstanceId":"extension"},"browserTabMentions":{"generation":"boot"}});
        assert!(r.matches_backend(&info));
        info["browserTabMentions"]["generation"] = json!("new-boot");
        assert!(!r.matches_backend(&info));
    }
    #[test]
    fn abandoned_job_requires_thirty_minutes_and_proven_owner_exit() {
        struct Owner(Option<bool>,Vec<u64>);
        impl Closer for Owner {
            fn owner_running(&mut self,_:&Value)->Option<bool>{self.0}
            fn close(&mut self,r:&Record)->io::Result<bool>{self.1.push(r.tab);Ok(true)}
        }
        let root=tempfile::tempdir().unwrap();job(root.path(),"stale","running");
        persist(&root.path().join("stale"),&record("stale",123)).unwrap();
        let mut fake=Owner(Some(false),vec![]);
        assert_eq!(sweep_with(root.path(),None,1899,false,&mut fake).unwrap().kept,1);
        for state in [None,Some(true)] {fake.0=state;assert_eq!(sweep_with(root.path(),None,1900,false,&mut fake).unwrap().kept,1);}
        fake.0=Some(false);assert_eq!(sweep_with(root.path(),None,1900,false,&mut fake).unwrap().closed,1);
        assert_eq!(fake.1,vec![123]);
        job(root.path(),"lost","lost");persist(&root.path().join("lost"),&record("lost",124)).unwrap();
        for state in [Some(true),None] {fake.0=state;assert_eq!(sweep_with(root.path(),None,5000,false,&mut fake).unwrap().kept,1);}
        fake.0=Some(false);assert_eq!(sweep_with(root.path(),None,5000,false,&mut fake).unwrap().closed,1);
        assert_eq!(fake.1,vec![123,124]);
    }
}
