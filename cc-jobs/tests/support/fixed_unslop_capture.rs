use super::*;

#[test]
fn fixed_capture_dispatch_returns_failure_before_model_or_job_creation() {
    let t=Test::new();
    let (result,out,err)=t.cli(&["capture-unslop","--key","11111111111111111111111111111111"]);
    assert_eq!(result.unwrap(),1);
    let value: Value=serde_json::from_str(&out).unwrap();
    assert_eq!(value["reason"],"capture_owner_unavailable");
    assert_eq!(value["started"],false);
    assert_eq!(value["qualified"],false);
    assert!(err.is_empty());
    assert_eq!(t.rt.spawns.load(Ordering::SeqCst),0);
    assert_eq!(t.rt.supervisors.load(Ordering::SeqCst),0);
    assert!(t.rt.calls.lock().unwrap().is_empty());
    assert!(!t.ctx.jobs.exists());
}

#[test]
fn fixed_capture_dispatch_does_not_echo_unknown_paths() {
    let t=Test::new();
    let (result,out,err)=t.cli(&["capture-unslop","--key","11111111111111111111111111111111","--root","C:/private"]);
    let error=result.unwrap_err();
    assert_eq!(error.code,2);
    assert_eq!(error.message,"invalid_capture_request");
    assert!(out.is_empty());assert!(err.is_empty());
    assert_eq!(t.rt.spawns.load(Ordering::SeqCst),0);
    assert!(t.rt.calls.lock().unwrap().is_empty());
}

#[test]
fn saved_native_marker_cannot_start_codex_or_enter_generic_cleanup() {
    let t=Test::new();
    let saved=t.put(job(json!({"id":"native-owner","status":"queued","native_capture":true,"model":"invalid","kind":"unslop_owned_capture_v1"})));
    let before=t.rt.calls.lock().unwrap().len();
    let error=t.ctx.supervise(s(&saved,"id")).unwrap_err();
    assert_eq!(error.message,"capture_owner_unavailable");
    assert_eq!(t.rt.spawns.load(Ordering::SeqCst),0);
    assert_eq!(t.rt.calls.lock().unwrap().len(),before);
    assert_eq!(t.ctx.load(s(&saved,"id")).unwrap()["status"],"queued");
    assert!(t.ctx.execution_spawn(&saved).is_err());
    assert_eq!(t.rt.spawns.load(Ordering::SeqCst),0);
    assert_eq!(t.rt.calls.lock().unwrap().len(),before);
}

#[test]
fn fixed_capture_help_and_normal_status_keep_existing_routes() {
    let t=Test::new();
    let (result,out,err)=t.cli(&["capture-unslop","--help"]);
    assert_eq!(result.unwrap(),0);
    assert!(out.starts_with("usage: codex_jobs.exe capture-unslop"));
    assert!(err.is_empty());
    let (result,out,err)=t.cli(&["status","--all","--json"]);
    assert_eq!(result.unwrap(),0);
    assert!(serde_json::from_str::<Value>(&out).is_ok());
    assert!(err.is_empty());
    assert_eq!(t.rt.spawns.load(Ordering::SeqCst),0);
    assert_eq!(t.rt.supervisors.load(Ordering::SeqCst),0);
}


#[test]
fn transient_initial_read_retry_releases_state_lock_and_reloads_cancellation() {
    for cancel in [false,true] {
        let t=Test::new();
        let jid="initial-read";
        t.put(job(json!({"id":jid,"name":jid,"pid":std::process::id(),"pid_created":123,"codex":"codex.CMD","cwd":t.ctx.root})));
        {let _queue=t.ctx.queue_lock().unwrap();t.ctx.track_queued_job(jid).unwrap();}
        fs::write(t.ctx.job_dir(jid).join("prompt.md"),"work").unwrap();
        let path=t.ctx.job_dir(jid).join("job.json");
        let loaded_path=path.clone();
        let injected=Arc::new(AtomicBool::new(false));let inject=injected.clone();
        let runtime=Arc::downgrade(&t.rt);
        *t.rt.load_hook.lock().unwrap()=Some(Box::new(move |loaded,_|{
            if loaded==loaded_path&&!inject.swap(true,Ordering::Relaxed) {
                runtime.upgrade().unwrap().load_failures.store(50,Ordering::Relaxed);
            }
        }));
        let waited=Arc::new(AtomicBool::new(false));let wait_seen=waited.clone();
        let lock_free=Arc::new(AtomicBool::new(false));let lock_seen=lock_free.clone();
        let still_queued=Arc::new(AtomicBool::new(false));let queued_seen=still_queued.clone();
        let cancelled=Arc::new(AtomicBool::new(false));let cancel_seen=cancelled.clone();
        let state_path=t.ctx.job_dir(jid).join("state.lock");
        *t.rt.sleep_hook.lock().unwrap()=Some(Box::new(move |seconds|{
            if seconds!=3.||wait_seen.swap(true,Ordering::Relaxed) {return;}
            let Ok(_state)=FileLock::try_acquire(&state_path) else {return;};
            lock_seen.store(true,Ordering::Relaxed);
            let Ok(bytes)=fs::read(&path) else {return;};
            let Ok(mut value)=serde_json::from_slice::<Value>(&bytes) else {return;};
            queued_seen.store(value["status"]=="queued"&&value["last_error"].is_null(),Ordering::Relaxed);
            if cancel {
                value["status"]=json!("cancelled");value["cancel_requested"]=json!(true);
                cancel_seen.store(fs::write(&path,serde_json::to_vec(&value).unwrap()).is_ok(),Ordering::Relaxed);
            }
        }));
        t.ctx.supervise(jid).unwrap();
        assert!(injected.load(Ordering::Relaxed),"initial read fault was not injected");
        assert!(waited.load(Ordering::Relaxed),"initial transient read was not retried");
        assert!(lock_free.load(Ordering::Relaxed),"state.lock stayed held during retry wait");
        assert!(still_queued.load(Ordering::Relaxed),"transient read finalized the waiting job");
        let final_job=t.ctx.load(jid).unwrap();
        assert_eq!(final_job["status"],if cancel {"cancelled"} else {"succeeded"});
        assert_eq!(t.rt.spawns.load(Ordering::Relaxed),usize::from(!cancel));
        if cancel {assert!(cancelled.load(Ordering::Relaxed));assert_eq!(final_job["cancel_requested"],true);}
        assert!(t.rt.diagnostics.lock().unwrap().iter().any(|line|line.starts_with("順番待ちの状態読取りを再試行")));
    }
}

#[test]
fn invalid_initial_record_is_not_retried_or_overwritten_as_success() {
    let t=Test::new();let jid="invalid-initial-read";
    t.put(job(json!({"id":jid,"status":"queued","cwd":t.ctx.root})));
    let path=t.ctx.job_dir(jid).join("job.json");let loaded_path=path.clone();
    let reads=Arc::new(AtomicUsize::new(0));let read_seen=reads.clone();
    let corrupted=Arc::new(AtomicBool::new(false));let corruption_seen=corrupted.clone();
    *t.rt.load_hook.lock().unwrap()=Some(Box::new(move |loaded,_|{
        if loaded==loaded_path&&read_seen.fetch_add(1,Ordering::Relaxed)==1 {
            corruption_seen.store(fs::write(&loaded_path,b"{").is_ok(),Ordering::Relaxed);
        }
    }));
    let error=t.ctx.supervise(jid).unwrap_err();
    assert!(corrupted.load(Ordering::Relaxed));
    assert_eq!(error.code,1);
    assert_eq!(t.rt.spawns.load(Ordering::Relaxed),0);
    assert_eq!(fs::read(path).unwrap(),b"{");
    assert!(!t.rt.sleeps.lock().unwrap().contains(&3.));
    assert!(!t.rt.diagnostics.lock().unwrap().iter().any(|line|line.starts_with("順番待ちの状態読取りを再試行")));
}
