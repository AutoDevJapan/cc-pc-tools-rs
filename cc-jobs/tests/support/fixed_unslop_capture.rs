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
