use super::*;
use std::io::{Cursor,Write};

#[test]
fn strict_native_json_rejects_ambiguous_records() {
    for raw in [r#"{"id":1,"id":2}"#,r#"{"id":1,"ID":2}"#,r#"{"x":{"a":1,"a":2}}"#,r#"{"id":1}{}"#] {
        assert!(strict_value(raw.as_bytes()).is_err());
    }
    assert_eq!(strict_value(br#"{"id":9007199254740993}"#).unwrap()["id"],9007199254740993u64);
}
#[test]
fn native_binding_never_invents_turn_model_or_effort() {
    let mut binding=NativeBinding::default();
    binding.observe(br#"{"type":"thread.started","thread_id":"thread-owned"}"#);
    binding.observe(br#"{"type":"turn.started"}"#);
    binding.observe(br#"{"type":"item.completed","item":{"id":"item-own","type":"agent_message","text":"done"}}"#);
    binding.observe(br#"{"type":"turn.completed"}"#);
    assert_eq!(binding.thread_id.as_deref(),Some("thread-owned"));
    assert!(binding.turn_ids.is_empty());assert!(binding.completed_turn_ids.is_empty());
    assert!(binding.turn_completed_without_id);assert!(!binding.malformed);
    assert_eq!(binding.item_ids,["item-own"]);
    assert_eq!(binding.records,4);assert_eq!(binding.session_ref.as_ref().unwrap().record,1);
    assert_eq!(binding.item_refs[0].record,3);assert_eq!(binding.item_refs[0].pointer,"/item");
    assert_eq!(binding.finish_refs[0].record,4);assert!(binding.finish_refs[0].native_id.is_none());
    binding.observe(br#"{"type":"thread.started","thread_id":"thread-other"}"#);
    assert!(binding.malformed);
}
#[test]
fn raw_stdin_writer_preserves_bytes_and_reports_partial_failure() {
    let bytes:Arc<[u8]>=Arc::from(&b"prompt\r\n\0raw\n"[..]);
    let state=Arc::new(Mutex::new(StreamState::default()));let mut out=Vec::new();
    FrozenInput{bytes:bytes.clone(),state:state.clone()}.write_to(&mut out).unwrap();
    assert_eq!(&*bytes,out);assert_eq!(state.lock().unwrap().bytes,bytes.len() as u64);
    assert_eq!(state.lock().unwrap().eof,Observed::Unknown);
    assert_eq!(state.lock().unwrap().flushed,Observed::Confirmed);
    assert_eq!(state.lock().unwrap().writer_closed,Observed::Unknown);
    struct Partial{bytes:Vec<u8>}
    impl Write for Partial {
        fn write(&mut self,b:&[u8])->io::Result<usize>{
            if !self.bytes.is_empty(){return Err(io::Error::other("fixture write failed"));}
            let n=b.len().min(2);self.bytes.extend_from_slice(&b[..n]);Ok(n)
        }
        fn flush(&mut self)->io::Result<()>{Ok(())}
    }
    let state=Arc::new(Mutex::new(StreamState::default()));let mut output=Partial{bytes:vec![]};
    assert!(FrozenInput{bytes,state:state.clone()}.write_to(&mut output).is_err());
    let state=state.lock().unwrap();assert_eq!(state.bytes,2);assert_eq!(state.persisted,Observed::Failed);
    assert_eq!(state.eof,Observed::Unknown);
}
fn thread(fresh:bool)->ExactThread {ExactThread{job_id:"job-owned".into(),generation:"2026-11-20T13:30:00Z".into(),
    thread_id:"thread-owned".into(),fresh_launch:fresh,native_item_ids:vec!["item-own".into()]}}
fn session()->ExactSession {ExactSession{job_id:"job-owned".into(),generation:"2026-11-20T13:30:00Z".into(),
    thread_id:"thread-owned".into(),turn_id:"turn-owned".into(),required_items:vec!["item-own".into()]}}
struct FixtureRPC{replies:std::collections::VecDeque<Vec<u8>>,requests:Vec<Vec<u8>>}
impl FixtureRPC {
    fn new(replies:Vec<Vec<u8>>)->Self {Self{replies:replies.into(),requests:Vec::new()}}
}
impl ReadonlyTransport for FixtureRPC {
    fn exchange(&mut self,request:&[u8],limit:usize)->io::Result<ReceivedReply> {
        let v=strict_value(request)?;
        assert!(matches!(s(&v,"method"),"thread/read"|"thread/turns/list"|"thread/items/list"));
        assert_eq!(v["params"]["threadId"],"thread-owned");
        if v["method"]=="thread/items/list" {assert_eq!(v["params"]["turnId"],"turn-owned");}
        self.requests.push(request.to_vec());let reply=self.replies.pop_front().ok_or_else(rejected)?;
        if reply.len()>limit{return Err(rejected());}Ok(ReceivedReply::received(reply,ReplyTimeliness::InTime))
    }
}
#[test]
fn read_only_connection_keeps_reply_and_enforces_deadline_and_methods() {
    let raw=b" {\"id\":1,\"result\": {\"x\":1}}\r\n".to_vec();
    let (reply_tx,reply_rx)=std::sync::mpsc::channel();reply_tx.send(Ok(raw.clone())).unwrap();
    let (request_tx,request_rx)=std::sync::mpsc::sync_channel(1);
    let mut transport=ConnectedReadonly::from_owned_connection(request_tx,reply_rx,
        std::time::Instant::now()+std::time::Duration::from_secs(1)).unwrap();
    let request=br#"{"id":1,"method":"thread/read","params":{"threadId":"thread-owned"}}"#;
    let received=transport.exchange(request,MAX_FRAME).unwrap();
    assert_eq!(received.raw,raw);assert_eq!(received.timeliness,ReplyTimeliness::InTime);
    assert_eq!(request_rx.try_recv().unwrap(),request);
    assert!(transport.exchange(br#"{"id":2,"method":"turn/start"}"#,MAX_FRAME).is_err());
    let (_reply_tx,reply_rx)=std::sync::mpsc::channel();let (request_tx,_request_rx)=std::sync::mpsc::sync_channel(1);
    let mut short=ConnectedReadonly::from_owned_connection(request_tx,reply_rx,
        std::time::Instant::now()+std::time::Duration::from_millis(2)).unwrap();
    assert!(short.exchange(request,MAX_FRAME).is_err());
}
#[cfg(windows)]
#[test]
fn owned_file_tee_keeps_original_bytes_before_redaction_and_stamp() {
    let dir=tempfile::tempdir().unwrap();let path=dir.path().join("stdout.native.jsonl");
    let raw=b"{\"type\":\"thread.started\",\"thread_id\":\"thread-owned\"}\r\nnot-json\n";
    let state=Arc::new(Mutex::new(StreamState::default()));let binding=Arc::new(Mutex::new(NativeBinding::default()));
    let mut stream=OriginalStream::new(Cursor::new(raw),&path,state.clone(),Some(binding.clone())).unwrap();
    let mut seen=Vec::new();stream.read_to_end(&mut seen).unwrap();drop(stream);
    assert_eq!(&seen,raw);assert_eq!(fs::read(&path).unwrap(),raw);
    assert_eq!(state.lock().unwrap().eof,Observed::Confirmed);assert!(binding.lock().unwrap().malformed);
    assert_eq!(binding.lock().unwrap().thread_id.as_deref(),Some("thread-owned"));
}
#[cfg(windows)]
#[test]
fn tee_drop_is_not_eof_and_create_new_collision_preserves_owner_bytes() {
    let dir=tempfile::tempdir().unwrap();let path=dir.path().join("raw.txt");let state=Arc::new(Mutex::new(StreamState::default()));
    let stream=OriginalStream::new(Cursor::new(b"never read"),&path,state.clone(),None).unwrap();
    drop(stream);assert_eq!(state.lock().unwrap().eof,Observed::Unknown);
    fs::write(&path,b"prior evidence").unwrap();assert!(save_new(&path,b"replacement").is_err());
    assert_eq!(fs::read(&path).unwrap(),b"prior evidence");
}
#[cfg(windows)]
#[test]
fn native_sha256_and_same_handle_readback_match_known_bytes() {
    assert_eq!(native::sha256(b"abc").unwrap(),"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    let dir=tempfile::tempdir().unwrap();let path=dir.path().join("evidence.txt");let fact=save_new(&path,b"abc").unwrap();
    let (raw,read)=stable_read(&path,16).unwrap();assert_eq!(raw,b"abc");assert_eq!(fact.identity,read.identity);
    assert_eq!(fact.sha256,read.sha256);assert_eq!(fact.bytes,3);
}
#[cfg(windows)]
#[test]
fn fresh_exact_turn_resolution_keeps_reply_and_refuses_ambiguous_or_resumed() {
    let raw=b" {\"id\":1,\"result\":{\"data\":[{\"id\":\"turn-owned\"}],\"nextCursor\":null}}\n";
    let dir=tempfile::tempdir().unwrap();let mut rpc=FixtureRPC::new(vec![raw.to_vec()]);
    let resolved=resolve_fresh_session(&thread(true),&mut rpc,dir.path()).unwrap();
    assert_eq!(resolved.thread_id,"thread-owned");assert_eq!(resolved.turn_id,"turn-owned");
    assert_eq!(fs::read(dir.path().join("resolve-response-1.jsonl")).unwrap(),raw);
    let mut rpc=FixtureRPC::new(vec![]);assert!(resolve_fresh_session(&thread(false),&mut rpc,dir.path()).is_err());assert!(rpc.requests.is_empty());
    for raw in [br#"{"id":1,"result":{"data":[],"nextCursor":null}}"#.to_vec(),
        br#"{"id":1,"result":{"data":[{"id":"a"},{"id":"b"}],"nextCursor":null}}"#.to_vec(),
        br#"{"id":1,"result":{"data":[{"id":"turn-owned"}],"nextCursor":"more"}}"#.to_vec(),
        br#"{"id":2,"result":{"data":[{"id":"turn-owned"}],"nextCursor":null}}"#.to_vec()] {
        let dir=tempfile::tempdir().unwrap();let mut rpc=FixtureRPC::new(vec![raw]);
        assert!(resolve_fresh_session(&thread(true),&mut rpc,dir.path()).is_err());
    }
}
#[cfg(windows)]
#[test]
fn exact_rpc_page_scope_and_raw_bytes_are_not_normalized_native_records() {
    let metadata=b" {\"id\":1,\"result\": {\"thread\": {\"id\":\"thread-owned\"}}}\r\n".to_vec();
    let page=b"{\"id\":2,\"result\":{\"data\":[{\"turnId\":\"turn-owned\",\"item\":{\"id\":\"item-own\",\"type\":\"agentMessage\",\"text\":\"done\"}}],\"nextCursor\":null}}\n".to_vec();
    let dir=tempfile::tempdir().unwrap();let mut rpc=FixtureRPC::new(vec![metadata.clone(),page.clone()]);
    let index=collect_exact_rpc(&session(),&mut rpc,dir.path()).unwrap();assert_eq!(index["complete"],true);
    assert_eq!(fs::read(dir.path().join("origin-response-1.jsonl")).unwrap(),metadata);
    assert_eq!(fs::read(dir.path().join("origin-response-2.jsonl")).unwrap(),page);
    assert_eq!(index["scorer_v11_complete"],false);assert_eq!(index["turn_id"],"turn-owned");
    for kind in ["foreign_turn","missing_native_item","duplicate_item","duplicate_key","wrong_response_id","repeated_cursor","unsupported"] {
        let mut bad=strict_value(&page).unwrap();
        match kind {
            "foreign_turn"=>bad["result"]["data"][0]["turnId"]=json!("turn-other"),
            "missing_native_item"=>bad["result"]["data"][0]["item"]["id"]=json!("item-other"),
            "duplicate_item"=>{let item=bad["result"]["data"][0].clone();bad["result"]["data"].as_array_mut().unwrap().push(item);},
            "wrong_response_id"=>bad["id"]=json!(3),
            "repeated_cursor"=>bad["result"]["nextCursor"]=json!("same"),
            "unsupported"=>bad=json!({"id":2,"error":{"code":-32601}}),_=>{}
        }
        let raw=if kind=="duplicate_key"{br#"{"id":2,"id":2,"result":{}}"#.to_vec()}else{serde_json::to_vec(&bad).unwrap()};
        let mut replies=vec![metadata.clone(),raw.clone()];if kind=="repeated_cursor"{
            let mut next=bad.clone();next["id"]=json!(3);next["result"]["data"]=json!([]);replies.push(serde_json::to_vec(&next).unwrap());
        }
        let dir=tempfile::tempdir().unwrap();let mut rpc=FixtureRPC::new(replies);
        assert!(collect_exact_rpc(&session(),&mut rpc,dir.path()).is_err(),"{kind}");
        let index:Value=serde_json::from_slice(&fs::read(dir.path().join("origin-index.private.json")).unwrap()).unwrap();
        assert_eq!(index["complete"],false,"{kind}");
    }
}

#[test]
fn rpc_contract_rejects_missing_null_alias_and_unrequested_methods() {
    for raw in [br#"{"id":1}"#.as_slice(),br#"{"id":1,"result":null}"#.as_slice(),
        br#"{"id":1,"result":{},"Error":null}"#.as_slice(),
        br#"{"id":1,"result":{},"extra":true}"#.as_slice(),
        br#"{"id":1,"result":{},"error":{"code":-32601}}"#.as_slice(),
        br#"{"id":2,"result":{}}"#.as_slice()] {assert!(rpc_result(raw,1).is_err());}
    assert_eq!(rpc_result(br#"{"id":1,"result":{},"error":null}"#,1).unwrap(),json!({}));
    for page in [json!({}),json!({"nextCursor":0}),json!({"nextCursor":""})]{assert!(page_cursor(&page).is_err());}
    assert_eq!(page_cursor(&json!({"nextCursor":null})).unwrap(),None);
}

#[cfg(windows)]
#[test]
fn actual_current_process_handle_observation_does_not_claim_reap_or_job_collection() {
    use std::os::windows::io::AsHandle;
    let current=native::current_process().unwrap();
    assert!(native::Processes::pin(current.get().unwrap().as_handle(),None).is_err());
}

#[test]
fn collection_observation_never_turns_exit_or_missing_job_into_collected() {
    use Observed::*;
    assert_eq!(job_children_observation(true,Confirmed,Confirmed,Confirmed,Confirmed,Confirmed,Some(1)),Confirmed);
    assert_eq!(job_children_observation(true,Confirmed,Confirmed,Confirmed,Confirmed,Confirmed,Some(2)),Failed);
    assert_eq!(job_children_observation(true,Confirmed,Confirmed,Confirmed,Confirmed,Confirmed,None),Unknown);
    assert_eq!(job_children_observation(false,Confirmed,Confirmed,Confirmed,Confirmed,Confirmed,Some(1)),Unknown);
    assert_eq!(job_children_observation(true,Unknown,Confirmed,Confirmed,Confirmed,Confirmed,Some(1)),Unknown);
    assert_eq!(job_children_observation(true,Confirmed,Unknown,Confirmed,Confirmed,Confirmed,Some(1)),Unknown);
    assert_eq!(job_children_observation(true,Confirmed,Confirmed,Unknown,Confirmed,Confirmed,Some(1)),Unknown);
    assert_eq!(job_children_observation(true,Confirmed,Confirmed,Confirmed,Unknown,Confirmed,Some(1)),Unknown);
    assert_eq!(job_children_observation(true,Confirmed,Confirmed,Confirmed,Confirmed,Failed,Some(1)),Unknown);
    assert_eq!(job_children_observation(true,Confirmed,Confirmed,Confirmed,Confirmed,Unknown,Some(1)),Unknown);
}

#[test]
fn no_wait_call_remains_unknown_and_has_no_success_exit() {
    let wait=WaitFact{called:false,reaped:Observed::Unknown,exit_code:None};
    let value=serde_json::to_value(wait).unwrap();
    assert_eq!(value["called"],false);assert_eq!(value["reaped"],"unknown");assert!(value["exit_code"].is_null());
}

#[cfg(windows)]
#[test]
fn eof_without_terminal_lf_keeps_one_complete_native_record() {
    let dir=tempfile::tempdir().unwrap();let path=dir.path().join("stdout.native.jsonl");
    let raw=br#"{"type":"thread.started","thread_id":"thread-owned"}"#;
    let state=Arc::new(Mutex::new(StreamState::default()));let binding=Arc::new(Mutex::new(NativeBinding::default()));
    let mut stream=OriginalStream::new(Cursor::new(raw),&path,state.clone(),Some(binding.clone())).unwrap();
    let mut actual=Vec::new();stream.read_to_end(&mut actual).unwrap();drop(stream);
    assert_eq!(actual,raw);assert_eq!(fs::read(&path).unwrap(),raw);
    assert_eq!(binding.lock().unwrap().records,1);assert!(!binding.lock().unwrap().malformed);
    assert_eq!(binding.lock().unwrap().thread_id.as_deref(),Some("thread-owned"));
}

#[test]
fn command_and_assistant_refs_point_only_to_actual_native_records() {
    let mut binding=NativeBinding::default();
    binding.observe(br#"{"type":"item.started","item":{"id":"command1","type":"command_execution","command":"fixture-only"}}"#);
    binding.observe(br#"{"type":"item.completed","item":{"id":"command1","type":"command_execution","aggregated_output":"fixture result","exit_code":0}}"#);
    binding.observe(br#"{"type":"item.completed","item":{"id":"answer1","type":"agent_message","text":"fixture final candidate"}}"#);
    assert_eq!(binding.tool_started_refs[0].record,1);assert_eq!(binding.tool_completed_refs[0].record,2);
    assert_eq!(binding.tool_completed_refs[0].native_id.as_deref(),Some("command1"));
    assert_eq!(binding.assistant_message_refs[0].record,3);assert_eq!(binding.assistant_message_refs[0].pointer,"/item");
    assert!(binding.finish_refs.is_empty());assert!(!binding.malformed);
    binding.observe(br#"{"type":"item.completed","item":{"id":"new1","type":"unknown-native-type"}}"#);
    assert_eq!(binding.unsupported_item_types,["unknown-native-type"]);
}

#[cfg(windows)]
#[test]
fn binding_sidecar_and_compact_receipt_share_consumer_limits() {
    let mut binding=NativeBinding::default();
    let mut raw=Vec::new();
    for n in 0..1000 {
        let line=format!("{{\"type\":\"item.completed\",\"item\":{{\"id\":\"item_{n}\",\"type\":\"agent_message\",\"text\":\"ok\"}}}}\n");
        binding.observe(line.as_bytes());raw.extend_from_slice(line.as_bytes());
    }
    assert!(raw.len()<MAX_RECEIPT as usize);
    let binding_bytes=bounded_json(&binding,MAX_BINDING).unwrap();
    assert!(binding_bytes.len()>MAX_RECEIPT as usize);
    let dir=tempfile::tempdir().unwrap();
    let binding_path=dir.path().join("binding.private.json");
    let fact=save_json(&binding_path,&binding,MAX_BINDING).unwrap();
    let receipt=json!({"domain":DOMAIN,"binding":fact,"original_complete":false});
    let receipt_path=dir.path().join("export.private.json");
    let receipt_fact=save_json(&receipt_path,&receipt,MAX_RECEIPT).unwrap();
    let consumed=load_json_fact(&receipt_path,&serde_json::to_value(receipt_fact).unwrap(),MAX_RECEIPT).unwrap();
    let rebound=load_json_fact(&binding_path,&consumed["binding"],MAX_BINDING).unwrap();
    assert_eq!(rebound["item_ids"].as_array().unwrap().len(),1000);
    assert_eq!(rebound,strict_value(&binding_bytes).unwrap());
    for (name,n,accepted) in [("below",MAX_RECEIPT-1,true),("at",MAX_RECEIPT,true),("above",MAX_RECEIPT+1,false)] {
        let value=json!("x".repeat(n as usize-2));let path=dir.path().join(name);
        let result=save_json(&path,&value,MAX_RECEIPT);
        assert_eq!(result.is_ok(),accepted,"{name}");
        if let Ok(fact)=result {assert_eq!(fact.bytes,n);assert_eq!(load_json_fact(&path,&serde_json::to_value(fact).unwrap(),MAX_RECEIPT).unwrap(),value);}
        else {assert!(!path.exists());}
    }
}

#[cfg(windows)]
#[test]
fn fatal_read_failure_is_sticky_but_interrupted_is_retried() {
    struct Steps {step:usize,fatal:bool}
    impl Read for Steps {
        fn read(&mut self,buf:&mut [u8])->io::Result<usize>{
            self.step+=1;
            match self.step {
                1=>{buf[0]=b'x';Ok(1)},
                2=>Err(io::Error::new(if self.fatal{io::ErrorKind::BrokenPipe}else{io::ErrorKind::Interrupted},"fixture-only")),
                _=>Ok(0),
            }
        }
    }
    for fatal in [false,true] {
        let dir=tempfile::tempdir().unwrap();let state=Arc::new(Mutex::new(StreamState::default()));
        let mut stream=OriginalStream::new(Steps{step:0,fatal},&dir.path().join("stdout.native.jsonl"),state.clone(),None).unwrap();
        let mut buf=[0u8;4];assert_eq!(stream.read(&mut buf).unwrap(),1);
        if fatal {
            assert!(stream.read(&mut buf).is_err());assert!(stream.read(&mut buf).is_err());
            assert_eq!(stream.input.step,2);assert_eq!(state.lock().unwrap().eof,Observed::Failed);
            let output=state.lock().unwrap().clone();let mut input=StreamState::default();
            input.bytes=1;input.flushed=Observed::Confirmed;input.writer_closed=Observed::Confirmed;
            let mut err=StreamState::default();err.eof=Observed::Confirmed;
            assert!(!streams_complete(&input,1,&output,&err,true,0));
        }else{assert_eq!(stream.read(&mut buf).unwrap(),0);assert_eq!(state.lock().unwrap().eof,Observed::Confirmed);}
    }
}

#[cfg(windows)]
#[test]
fn rejected_rpc_frames_and_timeout_keep_original_request_reply_refs() {
    let metadata=br#"{"id":1,"result":{"thread":{"id":"thread-owned"}}}"#.to_vec();
    for (name,reply) in [
        ("error",Some(b" {\"id\":2,\"error\":{\"code\":-32601,\"message\":\"fixture-only\"}}\r\n".to_vec())),
        ("wrong_id",Some(br#"{"id":9,"result":{}}"#.to_vec())),
        ("duplicate",Some(br#"{"id":2,"id":2,"result":{}}"#.to_vec())),
        ("malformed",Some(b"not json\n".to_vec())),
        ("timeout",None),
    ] {
        let dir=tempfile::tempdir().unwrap();let mut replies=vec![metadata.clone()];
        if let Some(raw)=&reply{replies.push(raw.clone());}
        let mut rpc=FixtureRPC::new(replies);
        assert!(collect_exact_rpc(&session(),&mut rpc,dir.path()).is_err(),"{name}");
        let index:Value=strict_value(&fs::read(dir.path().join("origin-index.private.json")).unwrap()).unwrap();
        assert_eq!(index["complete"],false);assert_eq!(index["files"].as_array().unwrap().len(),2);
        let attempt=&index["files"][1];
        let request=fs::read(dir.path().join("origin-request-2.jsonl")).unwrap();
        assert_eq!(request,rpc.requests[1]);assert_eq!(attempt["request"]["sha256"],native::sha256(&request).unwrap());
        match reply {
            Some(raw)=>{
                assert_eq!(fs::read(dir.path().join("origin-response-2.jsonl")).unwrap(),raw);
                assert_eq!(attempt["reply"]["sha256"],native::sha256(&raw).unwrap());
                assert_eq!(attempt["preservation"],"saved");assert_eq!(attempt["validation"],"rejected");
            }
            None=>{assert!(attempt["reply"].is_null());assert!(!dir.path().join("origin-response-2.jsonl").exists());assert_eq!(attempt["transport"],"failed_or_timeout");}
        }
    }
}

#[cfg(windows)]
#[test]
fn stream_writer_seal_rejects_truncate_content_change_and_replacement() {
    for change in ["none","truncate","same_size","replace"] {
        let dir=tempfile::tempdir().unwrap();let path=dir.path().join("stdout.native.jsonl");
        let state=Arc::new(Mutex::new(StreamState::default()));
        let raw=b"source-original\n";
        let mut stream=OriginalStream::new(Cursor::new(raw),&path,state.clone(),None).unwrap();
        let mut returned=Vec::new();stream.read_to_end(&mut returned).unwrap();drop(stream);
        let state=state.lock().unwrap().clone();let writer=state.seal.as_ref().unwrap();
        assert_eq!(writer.bytes,raw.len() as u64);assert_eq!(writer.sha256,native::sha256(raw).unwrap());
        match change {
            "truncate"=>fs::write(&path,b"").unwrap(),
            "same_size"=>fs::write(&path,b"source-changed!\n").unwrap(),
            "replace"=>{
                fs::rename(&path,dir.path().join("old-owned-stream")).unwrap();
                fs::write(&path,raw).unwrap();
            }
            _=>{}
        }
        let result=sealed_stream_fact(&path,&state,MAX_STREAM);
        assert_eq!(result.is_ok(),change=="none","{change}");
        if let Ok(fact)=result{assert_eq!(fact.identity,writer.identity);assert_eq!(fact.sha256,writer.sha256);}
    }
}

#[derive(Default)]
struct CloseTestLog {calls:Mutex<Vec<&'static str>>,retained:Mutex<Vec<CloseTestResource>>}
struct CloseTestResource {
    name:&'static str,fail:bool,token:Arc<()>,log:std::sync::Weak<CloseTestLog>,
}
fn close_test_resource(resource:CloseTestResource)->io::Result<()> {
    let log=resource.log.upgrade().unwrap();let _=Arc::strong_count(&resource.token);log.calls.lock().unwrap().push(resource.name);
    if resource.fail {log.retained.lock().unwrap().push(resource);Err(io::Error::other("close fixture"))}else{Ok(())}
}
fn resource(log:&Arc<CloseTestLog>,name:&'static str,fail:bool,token:&Arc<()>)->CloseOnce<CloseTestResource> {
    CloseOnce::new(CloseTestResource{name,fail,token:token.clone(),log:Arc::downgrade(log)},close_test_resource)
}
#[test]
fn once_finalizer_keeps_failed_owner_and_never_retries_on_drop_or_reclose() {
    for fail in [false,true] {
        let log=Arc::new(CloseTestLog::default());let token=Arc::new(());
        let mut owner=resource(&log,"resource",fail,&token);
        assert_eq!(owner.close().is_err(),fail);assert_eq!(owner.close().is_err(),fail);drop(owner);
        assert_eq!(*log.calls.lock().unwrap(),["resource"]);
        assert_eq!(log.retained.lock().unwrap().len(),usize::from(fail));
        assert_eq!(Arc::strong_count(&token),if fail{2}else{1});
        log.retained.lock().unwrap().clear();assert_eq!(Arc::strong_count(&token),1);
        assert_eq!(*log.calls.lock().unwrap(),["resource"]);
    }
    let log=Arc::new(CloseTestLog::default());let token=Arc::new(());
    let _=std::panic::catch_unwind(std::panic::AssertUnwindSafe(||{
        let _owner=resource(&log,"unwind",true,&token);panic!("fixture-only unwind");
    }));
    assert_eq!(*log.calls.lock().unwrap(),["unwind"]);assert_eq!(Arc::strong_count(&token),2);
    log.retained.lock().unwrap().clear();assert_eq!(Arc::strong_count(&token),1);
}

#[test]
fn partial_constructor_and_before_finish_drop_finalize_each_acquired_owner_once() {
    for stop in 0..=3 {
        for failed_close in [None,Some(0usize),Some(1),Some(2)] {
            let log=Arc::new(CloseTestLog::default());let token=Arc::new(());
            let result=(||->io::Result<Vec<CloseOnce<CloseTestResource>>>{
                let mut acquired=Vec::new();
                for (n,name) in ["root","child","job"].into_iter().enumerate(){
                    if n==stop{return Err(io::Error::new(io::ErrorKind::PermissionDenied,"primary fixture"));}
                    acquired.push(resource(&log,name,failed_close==Some(n),&token));
                }
                Ok(acquired)
            })();
            if stop<3{assert_eq!(result.err().unwrap().kind(),io::ErrorKind::PermissionDenied);}else{drop(result.unwrap());}
            let calls=log.calls.lock().unwrap();assert_eq!(calls.len(),stop.min(3));
            for name in ["root","child","job"].into_iter().take(stop.min(3)){assert_eq!(calls.iter().filter(|got|**got==name).count(),1);}
            let retained=usize::from(failed_close.is_some_and(|n|n<stop));
            assert_eq!(log.retained.lock().unwrap().len(),retained);assert_eq!(Arc::strong_count(&token),1+retained);
            drop(calls);log.retained.lock().unwrap().clear();assert_eq!(Arc::strong_count(&token),1);
        }
    }
}

struct FailingReadOwner {
    input:Cursor<Vec<u8>>,fault:&'static str,identity_calls:std::cell::Cell<usize>,owner:CloseTestResource,
}
impl Read for FailingReadOwner {
    fn read(&mut self,out:&mut [u8])->io::Result<usize>{
        if self.fault=="read"{return Err(io::Error::new(io::ErrorKind::BrokenPipe,"read fixture"));}
        self.input.read(out)
    }
}
fn failing_read_identity(file:&FailingReadOwner)->io::Result<FileIdentity> {
    let n=file.identity_calls.get()+1;file.identity_calls.set(n);
    if (n==1&&file.fault=="first_identity")||(n==2&&file.fault=="second_identity"){
        return Err(io::Error::new(io::ErrorKind::InvalidData,"identity fixture"));
    }
    Ok(FileIdentity{volume:1,index:if n==2&&file.fault=="changed"{2}else{1},created:3,
        size:if file.fault=="oversize"{99}else{file.input.get_ref().len() as u64}})
}
fn close_failing_reader(file:FailingReadOwner)->io::Result<()>{close_test_resource(file.owner)}
fn fixture_digest(_:&[u8])->io::Result<String>{Ok("fixture-digest".into())}
fn failed_digest(_:&[u8])->io::Result<String>{Err(io::Error::new(io::ErrorKind::InvalidData,"hash fixture"))}
#[test]
fn read_owned_early_failures_keep_primary_close_fact_and_strong_owner() {
    for fault in ["none","first_identity","oversize","read","second_identity","hash","changed"] {
        for close_fail in [false,true] {
            let log=Arc::new(CloseTestLog::default());let token=Arc::new(());
            let file=FailingReadOwner{input:Cursor::new(b"abc".to_vec()),fault,identity_calls:std::cell::Cell::new(0),
                owner:CloseTestResource{name:"file",fail:close_fail,token:token.clone(),log:Arc::downgrade(&log)}};
            let result=read_owned(CloseOnce::new(file,close_failing_reader),16,"file".into(),failing_read_identity,
                if fault=="hash"{failed_digest}else{fixture_digest});
            assert_eq!(result.is_ok(),fault=="none"&&!close_fail,"{fault}/{close_fail}");
            if fault!="none"&&close_fail{
                assert!(result.as_ref().unwrap_err().get_ref().unwrap().downcast_ref::<PrimaryAndCloseFailure>().is_some());
            }
            assert_eq!(*log.calls.lock().unwrap(),["file"]);assert_eq!(Arc::strong_count(&token),if close_fail{2}else{1});
            assert_eq!(log.retained.lock().unwrap().len(),usize::from(close_fail));log.retained.lock().unwrap().clear();
        }
    }
}

#[cfg(windows)]
#[test]
fn owned_stdin_fixture_helper() {
    if std::env::var("CC_NATIVE_EXPORT_STDIN_FIXTURE").as_deref()!=Ok("1"){return;}
    let path=std::env::var_os("CC_NATIVE_EXPORT_STDIN_RESULT").expect("fixture output path");
    let mut bytes=Vec::new();std::io::stdin().take(4097).read_to_end(&mut bytes).unwrap();
    assert!(bytes.len()<=4096);fs::write(path,&bytes).unwrap();
}
#[cfg(windows)]
struct OwnedFixtureChild {child:std::process::Child}
#[cfg(windows)]
impl Drop for OwnedFixtureChild {
    fn drop(&mut self){if self.child.try_wait().ok().flatten().is_none(){let _=self.child.kill();let _=self.child.wait();}}
}
#[cfg(windows)]
fn spawn_stdin_fixture(dir:&Path,name:&str)->OwnedFixtureChild {
    use std::os::windows::process::CommandExt;
    let mut command=std::process::Command::new(std::env::current_exe().unwrap());
    command.args(["--exact","native_export::tests::owned_stdin_fixture_helper","--nocapture"])
        .env("CC_NATIVE_EXPORT_STDIN_FIXTURE","1").env("CC_NATIVE_EXPORT_STDIN_RESULT",dir.join(name))
        .current_dir(dir).stdin(std::process::Stdio::piped()).stdout(std::process::Stdio::null()).stderr(std::process::Stdio::null())
        .creation_flags(0x0800_0000);
    OwnedFixtureChild{child:command.spawn().unwrap()}
}
#[cfg(windows)]
#[test]
fn same_owned_child_stdin_closes_and_child_observes_exact_input() {
    use std::os::windows::io::AsHandle;
    let dir=tempfile::tempdir().unwrap();let mut owned=spawn_stdin_fixture(dir.path(),"result.txt");
    let processes=native::Processes::pin(owned.child.as_handle(),None).unwrap();
    let bytes:Arc<[u8]>=Arc::from(&b"same-child\r\nraw\0bytes"[..]);let state=Arc::new(Mutex::new(StreamState::default()));
    FrozenInput{bytes:bytes.clone(),state:state.clone()}.write_to_child(&mut owned.child,&processes).unwrap();
    assert!(owned.child.stdin.is_none());
    {let actual=state.lock().unwrap();assert_eq!(actual.bytes,bytes.len() as u64);assert_eq!(actual.flushed,Observed::Confirmed);
        assert_eq!(actual.writer_closed,Observed::Confirmed);assert_eq!(actual.eof,Observed::Unknown);}
    let deadline=std::time::Instant::now()+std::time::Duration::from_secs(3);
    loop {
        if let Some(exit)=owned.child.try_wait().unwrap(){assert!(exit.success());break;}
        assert!(std::time::Instant::now()<deadline,"owned stdin fixture deadline");std::thread::sleep(std::time::Duration::from_millis(5));
    }
    assert_eq!(fs::read(dir.path().join("result.txt")).unwrap(),&*bytes);
    let collection=serde_json::to_value(processes.finish()).unwrap();
    assert_eq!(collection["child_signalled"],"confirmed");assert_eq!(collection["all_job_children_collected"],"unknown");
}
#[cfg(windows)]
#[test]
fn other_child_cannot_consume_same_attempt_frozen_input() {
    use std::os::windows::io::AsHandle;
    let dir=tempfile::tempdir().unwrap();let owned=spawn_stdin_fixture(dir.path(),"owner.txt");
    let processes=native::Processes::pin(owned.child.as_handle(),None).unwrap();
    let mut other=spawn_stdin_fixture(dir.path(),"other.txt");let state=Arc::new(Mutex::new(StreamState::default()));
    assert!(FrozenInput{bytes:Arc::from(&b"owned-only"[..]),state:state.clone()}.write_to_child(&mut other.child,&processes).is_err());
    assert_eq!(state.lock().unwrap().bytes,0);assert_eq!(state.lock().unwrap().writer_closed,Observed::Unknown);
    assert!(other.child.stdin.is_some());assert!(!dir.path().join("other.txt").exists());
}

#[cfg(windows)]
#[test]
fn resolve_rpc_error_and_timeout_keep_raw_frames_and_failure_index() {
    for (name,reply) in [("error",Some(br#"{"id":1,"error":{"code":-32601}}"#.to_vec())),("timeout",None)] {
        let dir=tempfile::tempdir().unwrap();let replies=reply.iter().cloned().collect();let mut rpc=FixtureRPC::new(replies);
        assert!(resolve_fresh_session(&thread(true),&mut rpc,dir.path()).is_err(),"{name}");
        let index=strict_value(&fs::read(dir.path().join("resolve-index.private.json")).unwrap()).unwrap();
        assert_eq!(index["complete"],false);let request=fs::read(dir.path().join("resolve-request-1.jsonl")).unwrap();
        assert_eq!(request,rpc.requests[0]);
        if let Some(raw)=reply {
            assert_eq!(fs::read(dir.path().join("resolve-response-1.jsonl")).unwrap(),raw);
            assert_eq!(index["files"][0]["validation"],"rejected");
        }else{assert!(!dir.path().join("resolve-response-1.jsonl").exists());assert!(index["files"][0]["reply"].is_null());}
    }
}

#[cfg(windows)]
#[test]
fn thousand_items_finish_and_same_artifact_consumer_remain_readable() {
    use std::os::windows::io::AsHandle;
    let dir=tempfile::tempdir().unwrap();let owned=spawn_stdin_fixture(dir.path(),"stdin-result.txt");
    let processes=native::Processes::borrowed_actual_child_for_fixture(owned.child.as_handle()).unwrap();
    let root=processes.root();let child=processes.child();
    let prompt=save_new(&dir.path().join("prompt.as-sent.utf8"),b"test-owned prompt").unwrap();
    let mut input=StreamState::default();input.bytes=prompt.bytes;input.flushed=Observed::Confirmed;
    // No actual stdin was sent by this parser/capacity fixture. Keep it unknown.
    let capture=Capture{job_id:"job-owned".into(),generation:"fixture-generation".into(),directory:dir.path().to_path_buf(),
        root:root.clone(),child:child.clone(),fresh_launch:true,processes,prompt,wait:WaitFact{called:false,reaped:Observed::Unknown,exit_code:None},
        input:Arc::new(Mutex::new(input)),stdout:Arc::new(Mutex::new(StreamState::default())),stderr:Arc::new(Mutex::new(StreamState::default())),
        binding:Arc::new(Mutex::new(NativeBinding::default()))};
    let mut raw=b"{\"type\":\"thread.started\",\"thread_id\":\"thread-owned\"}\n".to_vec();
    for n in 0..1000 {raw.extend_from_slice(format!("{{\"type\":\"item.completed\",\"item\":{{\"id\":\"item_{n}\",\"type\":\"agent_message\",\"text\":\"ok\"}}}}\n").as_bytes());}
    {let mut stdout=capture.stdout(Cursor::new(&raw)).unwrap();stdout.read_to_end(&mut Vec::new()).unwrap();}
    {let mut stderr=capture.stderr(Cursor::new(b"")).unwrap();stderr.read_to_end(&mut Vec::new()).unwrap();}
    let finished=capture.finish().unwrap();assert!(finished.receipt.bytes<=MAX_RECEIPT);
    let reg=json!({"job_id":"job-owned","started_at":"fixture-generation","root":root,"child":child,
        "receipt":finished.receipt,"collection":finished.collection});
    let selected=read_export_artifacts(dir.path().to_path_buf(),&reg,"job-owned",|_|Ok(())).unwrap();
    assert_eq!(selected.binding["item_ids"].as_array().unwrap().len(),1000);
    assert_eq!(selected.metadata()["original_complete"],false);assert_eq!(selected.metadata()["input"]["writer_closed"],"unknown");
    let state=&selected.metadata()["stdout"];let expected=&selected.metadata()["files"][0];
    assert_eq!(state["seal"]["sha256"],native::sha256(&raw).unwrap());assert_eq!(state["seal"],*expected);
    let (read,fact)=stable_read(&dir.path().join("stdout.native.jsonl"),MAX_STREAM).unwrap();
    assert_eq!(read,raw);assert_eq!(serde_json::to_value(fact).unwrap(),*expected);
    // Test uses the exact consumer helper with an explicit non-authoritative
    // fixture qualification seam. It cannot publish a normal job registry.
    drop(owned);
}

#[cfg(windows)]
#[test]
fn request_preservation_failure_is_not_parse_failure_or_network_attempt() {
    let dir=tempfile::tempdir().unwrap();let path=dir.path().join("origin-request-1.jsonl");fs::write(&path,b"old evidence").unwrap();
    let mut rpc=FixtureRPC::new(vec![]);
    assert!(collect_exact_rpc(&session(),&mut rpc,dir.path()).is_err());assert!(rpc.requests.is_empty());
    let index=strict_value(&fs::read(dir.path().join("origin-index.private.json")).unwrap()).unwrap();
    assert_eq!(index["complete"],false);assert_eq!(index["files"][0]["preservation"],"request_failed");
    assert_eq!(index["files"][0]["validation"],"not_attempted");assert_eq!(index["files"][0]["send"],"not_attempted");
    assert_eq!(fs::read(&path).unwrap(),b"old evidence");
}

#[cfg(windows)]
#[test]
fn connected_receive_deadline_preserves_raw_before_rejection() {
    use std::sync::mpsc::{sync_channel,Receiver,SyncSender};
    use std::time::{Duration,Instant};
    fn at_deadline(deadline:Instant)->Instant{deadline}
    // Start only this fixture's response budget after OriginCalls has saved the
    // actual request. The same responder receives that frame before replying.
    // The cfg(test) post-enqueue barrier waits for response-ready (or deliberate
    // noReply) before the same 1-second/40-ms receive budget. Production timing
    // and raw preservation are unchanged; this is not a total-operation timer.
    struct ReadyTransport {
        inner:ConnectedReadonly,requests:Option<Receiver<Vec<u8>>>,responses:SyncSender<io::Result<Vec<u8>>>,
        replies:std::collections::VecDeque<Option<Vec<u8>>>,observed:Vec<Vec<u8>>,
    }
    impl ReadyTransport {
        fn new(replies:Vec<Option<Vec<u8>>>)->Self {
            let (request_tx,request_rx)=sync_channel(8);let (reply_tx,reply_rx)=sync_channel(8);
            let inner=ConnectedReadonly::from_owned_connection(request_tx,reply_rx,Instant::now()+Duration::from_secs(1)).unwrap();
            Self{inner,requests:Some(request_rx),responses:reply_tx,replies:replies.into(),observed:Vec::new()}
        }
    }
    impl ReadonlyTransport for ReadyTransport {
        fn exchange(&mut self,request:&[u8],limit:usize)->io::Result<ReceivedReply> {
            let requests=self.requests.take().ok_or_else(rejected)?;
            let reply=self.replies.pop_front().ok_or_else(rejected)?;let timeout=reply.is_none();
            let responses=self.responses.clone();let (ready_tx,ready_rx)=sync_channel(1);let (response_tx,response_rx)=sync_channel(1);let (release_tx,release_rx)=sync_channel::<()>(1);
            let responder=std::thread::Builder::new().name("native-rpc-fixture".into()).spawn(move|| {
                let result=(||->io::Result<Vec<u8>> {
                    ready_tx.send(()).map_err(|_|rejected())?;
                    let raw=requests.recv_timeout(Duration::from_secs(1)).map_err(|_|rejected())?;
                    if let Some(reply)=reply{responses.send(Ok(reply)).map_err(|_|rejected())?;}
                    response_tx.send(()).map_err(|_|rejected())?;
                    let _=release_rx.recv_timeout(Duration::from_secs(1));Ok(raw)
                })();
                (requests,result)
            })?;
            let result=match ready_rx.recv_timeout(Duration::from_secs(1)) {
                Ok(())=>{
                    self.inner.deadline=Instant::now()+Duration::from_secs(1);
                    self.inner.response_ready=Some((response_rx,if timeout{Duration::from_millis(40)}else{Duration::from_secs(1)}));
                    self.inner.exchange(request,limit)
                },
                Err(_)=>Err(rejected()),
            };
            let _=release_tx.send(());
            let (requests,observed)=responder.join().map_err(|_|rejected())?;self.requests=Some(requests);
            self.observed.push(observed?);result
        }
    }
    let good=br#"{"id":1,"result":{"thread":{"id":"thread-owned"}}}"#.to_vec();
    let error=b" {\"id\":1,\"error\":{\"code\":-32601}}\r\n".to_vec();
    let cases:[(&str,Option<Vec<u8>>,bool,&str);7]=[
        ("late_valid",Some(good.clone()),true,"rejected_deadline"),
        ("late_error",Some(error.clone()),true,"rejected_deadline"),
        ("in_time_error",Some(error),false,"rejected"),
        ("wrong_id",Some(br#"{"id":900,"result":{}}"#.to_vec()),false,"rejected"),
        ("real_timeout",None,false,"not_attempted"),
        ("oversize",Some(vec![b'x';MAX_FRAME+1]),false,"not_attempted"),
        ("empty",Some(vec![]),false,"not_attempted"),
    ];
    for (name,reply,late,validation) in cases {
        let dir=tempfile::tempdir().unwrap();let mut transport=ReadyTransport::new(vec![reply.clone()]);
        if late{transport.inner.after_receive=Some(at_deadline);}
        assert!(collect_exact_rpc(&session(),&mut transport,dir.path()).is_err(),"{name}");
        let index=strict_value(&fs::read(dir.path().join("origin-index.private.json")).unwrap()).unwrap();
        assert_eq!(index["complete"],false,"{name}");
        let fact=&index["files"][0];assert_eq!(fact["validation"],validation,"{name}");
        let request=fs::read(dir.path().join("origin-request-1.jsonl")).unwrap();
        assert_eq!(transport.observed,vec![request],"{name}");assert!(transport.replies.is_empty(),"{name}");
        let bounded=reply.as_ref().filter(|raw|!raw.is_empty()&&raw.len()<=MAX_FRAME);
        if let Some(raw)=bounded {
            let (saved,saved_fact)=stable_read(&dir.path().join("origin-response-1.jsonl"),MAX_FRAME as u64).unwrap();
            assert_eq!(&saved,raw,"{name}");assert_eq!(serde_json::to_value(saved_fact).unwrap(),fact["reply"],"{name}");
            assert_eq!(fact["preservation"],"saved","{name}");
            assert_eq!(fact["timeliness"],if late{"deadline_elapsed"}else{"in_time"},"{name}");
        } else {
            assert!(fact["reply"].is_null(),"{name}");
            assert!(!dir.path().join("origin-response-1.jsonl").exists(),"{name}");
            assert_eq!(fact["transport"],"failed_or_timeout","{name}");
        }
    }
    let dir=tempfile::tempdir().unwrap();
    let page=br#"{"id":2,"result":{"data":[{"turnId":"turn-owned","item":{"id":"item-own"}}],"nextCursor":null}}"#.to_vec();
    let mut transport=ReadyTransport::new(vec![Some(good.clone()),Some(page.clone())]);
    let index=collect_exact_rpc(&session(),&mut transport,dir.path()).unwrap();
    assert_eq!(index["complete"],true);assert_eq!(index["files"].as_array().unwrap().len(),2);
    for (offset,raw) in [good,page].iter().enumerate(){
        assert_eq!(fs::read(dir.path().join(format!("origin-response-{}.jsonl",offset+1))).unwrap(),*raw);
        assert_eq!(index["files"][offset]["timeliness"],"in_time");assert_eq!(index["files"][offset]["validation"],"accepted");
        assert_eq!(transport.observed[offset],fs::read(dir.path().join(format!("origin-request-{}.jsonl",offset+1))).unwrap());
    }
    assert_eq!(transport.observed.len(),2);assert!(transport.replies.is_empty());
    let dir=tempfile::tempdir().unwrap();let (tx,rx)=sync_channel(1);drop(rx);let (_reply,answers)=sync_channel(1);
    let mut disconnected=ConnectedReadonly::from_owned_connection(tx,answers,Instant::now()+Duration::from_secs(1)).unwrap();
    assert!(collect_exact_rpc(&session(),&mut disconnected,dir.path()).is_err());
    let index=strict_value(&fs::read(dir.path().join("origin-index.private.json")).unwrap()).unwrap();
    assert_eq!(index["complete"],false);assert!(index["files"][0]["reply"].is_null());assert_eq!(index["files"][0]["transport"],"failed_or_timeout");
}

#[test]
fn public_native_export_parser_accepts_only_exact_id_and_fixed_parts() {
    let args=crate::cli::parse(&["export-native".into(),"job-owned".into(),"--part".into(),"stdout".into()]).unwrap();
    assert_eq!(args.id,"job-owned");assert_eq!(args.value("part"),Some("stdout"));
    for args in [vec!["export-native","--source-path","C:/other"],vec!["export-native"],vec!["export-native","job-owned","--hash","00"]] {
        let args:Vec<_>=args.into_iter().map(str::to_string).collect();assert!(crate::cli::parse(&args).is_err());
    }
    assert!(validate_request_purpose("caller-hash-grant").is_err());
}
#[test]
fn native_evidence_keeps_normal_model_policy_and_user_capabilities() {
    let normal=json!({"codex":"codex.cmd","model":"gpt-6.1-sol","effort":"max","sandbox":"inherit","mcp_profile":"inherit","cwd":"C:/fixture"});
    let mut evidence=normal.clone();evidence["native_evidence_purpose"]=json!(PURPOSE);
    assert_eq!(crate::codex_args(&normal),crate::codex_args(&evidence));
}
#[cfg(windows)]
#[test]
fn public_exact_selector_rejects_invalid_selectors_without_starting_host() {
    let dir=tempfile::tempdir().unwrap();
    let rt:crate::SharedRuntime=Arc::new(crate::System::default());
    let context=Context{root:dir.path().into(),jobs:dir.path().join("jobs"),home:dir.path().into(),codex_home:dir.path().join("codex"),
        gto:dir.path().join("gto.exe"),executable:std::env::current_exe().unwrap(),rt};
    let mut out=Vec::new();
    assert!(context.cmd_native_export("../outside","stdout",&mut out).is_err());assert!(out.is_empty());
    assert!(context.cmd_native_export("job-owned","arbitrary-path",&mut out).is_err());assert!(out.is_empty());
}

#[test]
fn public_native_output_mode_keeps_exact_lf_crlf_and_restores_legacy_writer() {
    let bytes=b"raw\nraw\r\n\0";let mut output=crate::TextWriter(Vec::new());
    {let _raw=crate::platform::native_raw_output();output.write_all(bytes).unwrap();}
    assert_eq!(output.0,bytes);
    let mut normal=crate::TextWriter(Vec::new());normal.write_all(b"a\n").unwrap();
    #[cfg(windows)] assert_eq!(normal.0,b"a\r\n");
    #[cfg(not(windows))] assert_eq!(normal.0,b"a\n");
}

#[test]
fn present_invalid_turn_identity_is_sticky_and_never_resolved() {
    for kind in ["turn.started","turn.completed"] {
        for invalid in [Value::Null,json!(42),json!(""),json!("bad/turn"),json!([]),json!({})] {
            let raw=serde_json::to_vec(&json!({"type":kind,"turn_id":invalid})).unwrap();
            let mut binding=NativeBinding::default();
            binding.observe(br#"{"type":"thread.started","thread_id":"thread-owned"}"#);
            binding.observe(br#"{"type":"item.completed","item":{"id":"item-own","type":"agent_message"}}"#);
            binding.observe(&raw);
            assert!(binding.malformed,"{kind}: {invalid}");
            assert!(!binding.turn_completed_without_id);
            // A later well-formed completion cannot clear the malformed fact.
            binding.observe(br#"{"type":"turn.completed"}"#);
            assert!(binding.malformed);
            let selected=SelectedExport{directory:PathBuf::from("unused-fixture"),
                receipt:json!({"job_id":"job-owned","started_at":"generation","fresh_launch":true}),
                collection:Value::Null,binding:serde_json::to_value(&binding).unwrap()};
            let mut rpc=FixtureRPC::new(vec![]);
            let result=selected.thread().and_then(|thread|resolve_fresh_session(&thread,&mut rpc,Path::new("unused-fixture")));
            assert!(result.is_err());assert!(rpc.requests.is_empty());
            assert!(selected.session("turn-owned").is_err());
        }
    }
    let mut missing=NativeBinding::default();
    missing.observe(br#"{"type":"turn.started"}"#);missing.observe(br#"{"type":"turn.completed"}"#);
    assert!(!missing.malformed);assert!(missing.turn_ids.is_empty());assert!(missing.turn_completed_without_id);
    let mut valid=NativeBinding::default();
    valid.observe(br#"{"type":"turn.started","turn_id":"turn-owned"}"#);
    valid.observe(br#"{"type":"turn.completed","turn_id":"turn-owned"}"#);
    assert!(!valid.malformed);assert_eq!(valid.turn_ids,["turn-owned"]);assert_eq!(valid.completed_turn_ids,["turn-owned"]);
}
#[cfg(windows)]
#[test]
fn invalid_turn_record_keeps_its_original_tee_bytes() {
    let dir=tempfile::tempdir().unwrap();let raw=b"{\"type\":\"turn.completed\",\"turn_id\":42}\r\n";
    let state=Arc::new(Mutex::new(StreamState::default()));let binding=Arc::new(Mutex::new(NativeBinding::default()));
    let path=dir.path().join("stdout.native.jsonl");
    {let mut stream=OriginalStream::new(Cursor::new(raw),&path,state.clone(),Some(binding.clone())).unwrap();
        let mut result=Vec::new();stream.read_to_end(&mut result).unwrap();assert_eq!(result,raw);}
    assert_eq!(fs::read(&path).unwrap(),raw);assert!(binding.lock().unwrap().malformed);
    assert_eq!(state.lock().unwrap().seal.as_ref().unwrap().sha256,native::sha256(raw).unwrap());
}

#[cfg(windows)]
#[test]
fn actual_saved_prompt_fact_is_required_for_persisted_input() {
    use std::os::windows::io::AsHandle;
    let dir=tempfile::tempdir().unwrap();let bytes=b"sent-exact\r\nraw\0bytes";
    let saved=save_new(&dir.path().join("prompt.as-sent.utf8"),bytes).unwrap();
    let frozen=frozen_saved_input(bytes,&saved).unwrap();let state=frozen.state.clone();let mut sent=Vec::new();
    frozen.write_to(&mut sent).unwrap();assert_eq!(sent,bytes);
    assert_eq!(state.lock().unwrap().persisted,Observed::Confirmed);
    assert_eq!(state.lock().unwrap().writer_closed,Observed::Unknown);
    assert!(frozen_saved_input(b"other",&saved).is_err());
    let mut changed=saved.clone();changed.bytes+=1;assert!(frozen_saved_input(bytes,&changed).is_err());
    let mut changed=saved.clone();changed.sha256="0".repeat(64);assert!(frozen_saved_input(bytes,&changed).is_err());
    let mut changed=saved.clone();changed.name="other-input".into();assert!(frozen_saved_input(bytes,&changed).is_err());
    let mut owned=spawn_stdin_fixture(dir.path(),"fact-child.txt");
    let processes=native::Processes::pin(owned.child.as_handle(),None).unwrap();
    let frozen=frozen_saved_input(bytes,&saved).unwrap();let state=frozen.state.clone();
    frozen.write_to_child(&mut owned.child,&processes).unwrap();
    let mut stdout=StreamState::default();stdout.eof=Observed::Confirmed;stdout.persisted=Observed::Confirmed;
    let stderr=stdout.clone();
    assert!(streams_complete(&state.lock().unwrap(),bytes.len() as u64,&stdout,&stderr,true,0));
    let until=std::time::Instant::now()+std::time::Duration::from_secs(3);
    while owned.child.try_wait().unwrap().is_none(){assert!(std::time::Instant::now()<until);std::thread::sleep(std::time::Duration::from_millis(5));}
    assert_eq!(fs::read(dir.path().join("fact-child.txt")).unwrap(),bytes);
}

// Production JobObjectBasicProcessIdList parsing is exercised below by
// actual_capture_job_reclaim_retention_and_membership_controls.
#[cfg(windows)]
#[test]
fn real_owned_child_primary_handle_closes_after_observed_wait() {
    use std::os::windows::io::AsHandle;
    let dir=tempfile::tempdir().unwrap();let mut owned=spawn_stdin_fixture(dir.path(),"closed.txt");
    let processes=native::Processes::pin(owned.child.as_handle(),None).unwrap();
    let state=Arc::new(Mutex::new(StreamState::default()));
    FrozenInput{bytes:Arc::from(&b"close-control"[..]),state}.write_to_child(&mut owned.child,&processes).unwrap();
    let until=std::time::Instant::now()+std::time::Duration::from_secs(3);
    loop{if let Some(status)=owned.child.try_wait().unwrap(){assert!(status.success());break;}
        assert!(std::time::Instant::now()<until);std::thread::sleep(std::time::Duration::from_millis(5));}
    let collection=processes.finish();let _=collection;
    let child=unsafe{std::ptr::read(&owned.child)};std::mem::forget(owned);
    assert!(close_owned_child(child).is_ok());
}

#[test]
fn missing_normal_origin_software_issuer_refuses_before_spawn() {
    let system=crate::System::default();
    assert!(matches!(crate::Runtime::native_origin_launcher(&system),Err(error) if error.kind()==io::ErrorKind::Unsupported));
}

#[cfg(windows)]
#[test]
fn suspended_normal_capture_child_is_recordable_before_resume() {
    use std::os::windows::{io::AsHandle,process::CommandExt};
    let dir=tempfile::tempdir().unwrap();let target=dir.path().join("suspended-result.txt");
    let mut command=std::process::Command::new(std::env::current_exe().unwrap());
    command.args(["--exact","native_export::tests::owned_stdin_fixture_helper","--nocapture"])
        .env("CC_NATIVE_EXPORT_STDIN_FIXTURE","1").env("CC_NATIVE_EXPORT_STDIN_RESULT",&target)
        .stdin(std::process::Stdio::piped()).stdout(std::process::Stdio::null()).stderr(std::process::Stdio::null())
        .current_dir(dir.path()).creation_flags(crate::NO_WINDOW|0x4);
    let mut owned=OwnedFixtureChild{child:command.spawn().unwrap()};
    let processes=native::Processes::pin(owned.child.as_handle(),None).unwrap();
    let bytes=b"frozen-before-resume";let saved=save_new(&dir.path().join("prompt.as-sent.utf8"),bytes).unwrap();
    let input=frozen_saved_input(bytes,&saved).unwrap();input.write_to_child(&mut owned.child,&processes).unwrap();
    assert!(owned.child.try_wait().unwrap().is_none());assert!(!target.exists());
    native::resume_child(&owned.child).unwrap();
    let deadline=std::time::Instant::now()+std::time::Duration::from_secs(3);
    while owned.child.try_wait().unwrap().is_none(){assert!(std::time::Instant::now()<deadline);std::thread::sleep(std::time::Duration::from_millis(5));}
    assert_eq!(fs::read(target).unwrap(),bytes);let _=processes.finish();
}

#[test]
fn owned_thread_factory_reports_each_setup_failure_without_unwind() {
    use std::sync::atomic::{AtomicUsize,Ordering};
    struct OwnedToken(Arc<AtomicUsize>);impl Drop for OwnedToken{fn drop(&mut self){self.0.fetch_add(1,Ordering::SeqCst);}}
    for role in [NativeThreadRole::CaptureInput,NativeThreadRole::CaptureStdout,NativeThreadRole::CaptureStderr,
        NativeThreadRole::OriginInput,NativeThreadRole::OriginStdout,NativeThreadRole::OriginStderr] {
        let drops=Arc::new(AtomicUsize::new(0));let held=OwnedToken(drops.clone());
        FAIL_NATIVE_THREAD.with(|fail|fail.set(Some(role)));
        let result=spawn_owned_thread(role,move||{let _owner=held;});
        assert!(result.is_err(),"{role:?}");assert_eq!(drops.load(Ordering::SeqCst),1);
        assert!(FAIL_NATIVE_THREAD.with(|fail|fail.get()).is_none());
        let normal=spawn_owned_thread(role,||37usize).unwrap();assert_eq!(normal.join().unwrap(),37);
    }
}
#[test]
fn initialized_capacity_one_precedes_first_read_without_full_refusal() {
    let initialized=b"{\"method\":\"initialized\",\"params\":{}}\n".to_vec();
    let request=b"{\"id\":1,\"method\":\"thread/read\",\"params\":{\"threadId\":\"thread-owned\"}}\n".to_vec();
    let reply=b"{\"id\":1,\"result\":{\"thread\":{\"id\":\"thread-owned\"}}}\n".to_vec();
    let (tx,rx)=std::sync::mpsc::sync_channel(1);tx.send(initialized.clone()).unwrap();
    let (started_tx,started_rx)=std::sync::mpsc::sync_channel(1);let (release_tx,release_rx)=std::sync::mpsc::sync_channel(1);
    let (reply_tx,reply_rx)=std::sync::mpsc::channel();let (seen_tx,seen_rx)=std::sync::mpsc::sync_channel(1);
    let mut transport=ConnectedReadonly::from_owned_connection(tx,reply_rx,std::time::Instant::now()+std::time::Duration::from_secs(3)).unwrap();
    let expected_reply=reply.clone();
    let reader=std::thread::Builder::new().spawn(move||{
        release_rx.recv_timeout(std::time::Duration::from_secs(2)).unwrap();
        let first=rx.recv_timeout(std::time::Duration::from_secs(2)).unwrap();
        let second=rx.recv_timeout(std::time::Duration::from_secs(2)).unwrap();
        seen_tx.send((first,second)).unwrap();reply_tx.send(Ok(reply)).unwrap();
    }).unwrap();
    let sent=request.clone();
    let consumer=std::thread::Builder::new().spawn(move||{FULL_ENQUEUE_OBSERVER.with(|hook|*hook.borrow_mut()=Some(started_tx));transport.exchange(&sent,MAX_FRAME)}).unwrap();
    started_rx.recv_timeout(std::time::Duration::from_secs(2)).unwrap();
    // The real request queue already contains initialized; first RPC starts
    // before the writer is released. Neither capacity nor deadline is expanded.
    release_tx.send(()).unwrap();
    let received=consumer.join().unwrap().unwrap();assert_eq!(received.raw,expected_reply);
    assert_eq!(received.timeliness,ReplyTimeliness::InTime);
    let (first,second)=seen_rx.recv_timeout(std::time::Duration::from_secs(2)).unwrap();
    assert_eq!(first,initialized);assert_eq!(second,request);reader.join().unwrap();
}
#[test]
fn bounded_enqueue_still_refuses_expiry_and_disconnect() {
    let (tx,rx)=std::sync::mpsc::sync_channel(1);tx.send(vec![1u8]).unwrap();
    let result=enqueue_before_deadline(&tx,vec![2],std::time::Instant::now()+std::time::Duration::from_millis(5));
    assert!(matches!(result,Err(error) if error.kind()==io::ErrorKind::TimedOut));assert_eq!(rx.try_recv().unwrap(),[1]);
    assert!(rx.try_recv().is_err());drop(rx);
    assert!(enqueue_before_deadline(&tx,vec![3],std::time::Instant::now()+std::time::Duration::from_secs(1)).is_err());
}
#[test]
fn readonly_query_latest_and_sticky_failure_have_separate_meaning() {
    let mut facts=OriginFacts::default();facts.child_wait=true;facts.exit_code=Some(0);
    facts.job_query(Ok(0));assert!(!facts.collected(false));assert!(facts.collected(true));assert!(facts.unforced_success());
    facts.job_query(Err(rejected()));assert_eq!(facts.job_active,None);assert_eq!(facts.last_observed_job_active,Some(0));
    assert!(!facts.latest_job_query_succeeded);assert!(facts.job_query_failed);assert!(!facts.collected(true));assert!(!facts.unforced_success());
    facts.job_query(Ok(0));assert!(facts.collected(true));assert!(facts.latest_job_query_succeeded);assert!(facts.job_query_failed);
    assert!(!facts.unforced_success());
}
#[test]
fn failed_or_accepted_stop_attempt_is_never_natural_success() {
    for child_stop in [false,true]{for accepted in [false,true]{
        let mut facts=OriginFacts::default();facts.child_wait=true;facts.exit_code=Some(0);facts.job_query(Ok(0));
        let target=if child_stop{&mut facts.child_terminate}else{&mut facts.job_terminate};
        target.call(|_|if accepted{Ok(())}else{Err(rejected())});
        assert!(target.attempted);assert_eq!(target.accepted,accepted);assert_eq!(target.failed,!accepted);
        assert!(facts.collected(true));assert!(!facts.unforced_success());
        facts.child_wait=false;assert!(!facts.collected(true));
    }}
    let mut normal=OriginFacts::default();normal.child_wait=true;normal.exit_code=Some(0);normal.job_query(Ok(0));
    assert!(normal.unforced_success());normal.thread_error=true;assert!(!normal.unforced_success());
}
#[test]
fn same_owner_collection_barrier_waits_and_keeps_failed_business_result() {
    struct Owner {phase:usize,token:Arc<()>,log:Arc<Mutex<Vec<&'static str>>>}
    let token=Arc::new(());let log=Arc::new(Mutex::new(Vec::new()));let original=Err::<(),_>(rejected());
    let owner=Owner{phase:0,token:token.clone(),log:log.clone()};let observations=std::cell::RefCell::new(Vec::new());
    let (receipt,deferred)=drive_owned_collection(owner,|mut state|{
        assert_eq!(Arc::strong_count(&state.token),2);state.phase+=1;
        state.log.lock().unwrap().push(if state.phase<3{"pending"}else{"wait_join_close"});
        let value=json!({"phase":state.phase,"operation_success":false});
        if state.phase<3{(Some(state),value)}else{(None,value)}
    },|receipt,complete|{observations.borrow_mut().push((receipt["phase"].clone(),complete));});
    assert!(deferred);assert_eq!(receipt["phase"],3);assert_eq!(Arc::strong_count(&token),1);assert!(original.is_err());
    assert_eq!(*log.lock().unwrap(),["pending","pending","wait_join_close"]);
    assert_eq!(observations.borrow().iter().map(|(_,done)|*done).collect::<Vec<_>>(),[false,false,true]);
}
#[test]
fn collection_observer_failure_does_not_drop_the_pending_owner() {
    let token=Arc::new(());let mut phase=0;let mut io_failed=false;
    let (result,deferred)=drive_owned_collection(token.clone(),|owner|{
        phase+=1;assert_eq!(Arc::strong_count(&owner),2);
        if phase==1{(Some(owner),false)}else{(None,true)}
    },|_,complete|{if !complete{io_failed=true;}});
    assert!(result&&deferred&&io_failed);assert_eq!(Arc::strong_count(&token),1);
    // The real observer records save_json Err and continues the same owning
    // collector; no claim of native IO-fault execution is made by this seam.
}
#[test]
fn early_error_progress_completion_is_joined_without_restarting_worker() {
    struct Guard<'a>{attempts:&'a std::cell::Cell<usize>}
    impl Drop for Guard<'_>{fn drop(&mut self){completion_barrier(||{let n=self.attempts.get()+1;self.attempts.set(n);n==3});}}
    let count=std::cell::Cell::new(0);let result=(||->io::Result<()>{let _guard=Guard{attempts:&count};Err(rejected())})();
    assert!(result.is_err());assert_eq!(count.get(),3);
    let mut normal=0;completion_barrier(||{normal+=1;true});assert_eq!(normal,1);
}

#[cfg(windows)]
#[test]
fn each_fallible_setup_stage_keeps_the_suspended_child_until_cleanup() {
    use std::os::windows::{io::AsHandle,process::CommandExt};
    for fail in [Some(NativeThreadRole::CaptureInput),Some(NativeThreadRole::CaptureStdout),Some(NativeThreadRole::CaptureStderr),None] {
        let dir=tempfile::tempdir().unwrap();let output=dir.path().join("setup-result.txt");
        let mut command=std::process::Command::new(std::env::current_exe().unwrap());
        command.args(["--exact","native_export::tests::owned_stdin_fixture_helper","--nocapture"])
            .env("CC_NATIVE_EXPORT_STDIN_FIXTURE","1").env("CC_NATIVE_EXPORT_STDIN_RESULT",&output)
            .current_dir(dir.path()).stdin(std::process::Stdio::piped()).stdout(std::process::Stdio::piped()).stderr(std::process::Stdio::piped())
            .creation_flags(crate::NO_WINDOW|0x4);
        let mut owned=OwnedFixtureChild{child:command.spawn().unwrap()};
        let processes=native::Processes::pin(owned.child.as_handle(),None).unwrap();
        let root=processes.root();let child=processes.child();let raw=b"same-normal-input";
        let prompt=save_new(&dir.path().join("prompt.as-sent.utf8"),raw).unwrap();
        let input=frozen_saved_input(raw,&prompt).unwrap();
        let mut capture=Capture{job_id:"job-fixture".into(),generation:"generation".into(),directory:dir.path().to_path_buf(),
            root,child,fresh_launch:true,processes,prompt,wait:WaitFact{called:false,reaped:Observed::Unknown,exit_code:None},
            input:input.state.clone(),stdout:Arc::new(Mutex::new(StreamState::default())),stderr:Arc::new(Mutex::new(StreamState::default())),binding:Arc::new(Mutex::new(NativeBinding::default()))};
        let mut readers=Vec::new();let mut writer=None;FAIL_NATIVE_THREAD.with(|fault|fault.set(fail));
        let setup=(||->io::Result<()>{
            let input=capture.owned_input_writer(input,&mut owned.child)?;
            writer=Some(spawn_owned_thread(NativeThreadRole::CaptureInput,move||input.write())?);
            let mut stdout=capture.stdout(owned.child.stdout.take().ok_or_else(rejected)?)?;
            readers.push(spawn_owned_thread(NativeThreadRole::CaptureStdout,move||{let _=stdout.read_to_end(&mut Vec::new());})?);
            let mut stderr=capture.stderr(owned.child.stderr.take().ok_or_else(rejected)?)?;
            readers.push(spawn_owned_thread(NativeThreadRole::CaptureStderr,move||{let _=stderr.read_to_end(&mut Vec::new());})?);
            capture.resume_owned_child(&owned.child)?;Ok(())
        })();
        assert_eq!(setup.is_err(),fail.is_some());
        if fail.is_some(){assert!(owned.child.try_wait().unwrap().is_none());assert!(!output.exists());owned.child.kill().unwrap();}
        let deadline=std::time::Instant::now()+std::time::Duration::from_secs(5);
        loop{if capture.observe_child_wait(&mut owned.child).unwrap().is_some(){break;}
            assert!(std::time::Instant::now()<deadline);std::thread::sleep(std::time::Duration::from_millis(5));}
        for thread in &readers{while !thread.is_finished(){assert!(std::time::Instant::now()<deadline);std::thread::sleep(std::time::Duration::from_millis(5));}}
        for thread in readers{thread.join().unwrap();}
        if let Some(thread)=writer{while !thread.is_finished(){assert!(std::time::Instant::now()<deadline);std::thread::sleep(std::time::Duration::from_millis(5));}let _=thread.join().unwrap();}
        assert!(capture.wait.called);assert_eq!(capture.wait.reaped,Observed::Confirmed);
        if fail.is_none(){assert_eq!(fs::read(&output).unwrap(),raw);}
        // Same factory/owner objects as jobs.rs. This isolated fixture cannot
        // assert the actual normal supervisor's queue or H1 admission.
        let finished=capture.finish().unwrap();
        let receipt:Value=serde_json::from_slice(&fs::read(dir.path().join("export.private.json")).unwrap()).unwrap();
        assert_eq!(receipt["original_complete"],fail.is_none());
        if fail.is_none(){assert_eq!(receipt["input"]["writer_closed"],"confirmed");assert_eq!(receipt["stdout"]["eof"],"confirmed");assert_eq!(receipt["stderr"]["eof"],"confirmed");assert_eq!(receipt["collection"]["std_child_wait"]["reaped"],"confirmed");}
        assert!(finished.receipt.bytes>0);
    }
}

#[cfg(windows)]
#[test]
fn actual_readonly_owner_barrier_collects_normal_and_delayed_pipe() {
    for delayed in [false,true]{let dir=tempfile::tempdir().unwrap();
        let receipt=native::origin_collection_fixture(dir.path(),delayed).unwrap();
        assert_eq!(receipt["collected"],true);assert_eq!(receipt["child_wait"],true);
        assert_eq!(receipt["job_active"],0);assert_eq!(receipt["pipe_threads_complete"],true);
        assert_eq!(receipt["child_handle_close"],"confirmed");assert_eq!(receipt["job_close"],"confirmed");
        assert_eq!(receipt["operation_success"],!delayed);assert_eq!(receipt["facts"]["deferred"],delayed);
        assert_eq!(receipt["facts"]["job_query_failed"],false);
    }
}

#[test]
fn actual_progress_guard_closes_on_early_error_without_worker_restart() {
    let dir=tempfile::tempdir().unwrap();let rt:crate::SharedRuntime=Arc::new(crate::System::default());
    let result=(||->io::Result<()>{
        let mut progress=ProgressOwner::new(crate::notify::TaskProgress::new(None,dir.path().to_path_buf(),rt),true);
        progress.start(&json!({"id":"fixture-progress"}));assert!(!progress.closed);
        Err(rejected())
    })();
    assert!(result.is_err());
    // None means no notification child or network call. Partial-close ordering
    // is exercised separately by completion_barrier's finite callback seam.
}

#[test]
fn stop_attempt_fact_exists_before_the_native_callback_returns() {
    let mut fact=StopFact::default();let called=std::cell::Cell::new(false);
    fact.call(|during|{assert!(during.attempted);assert!(!during.accepted&&!during.failed);called.set(true);Err(io::Error::new(io::ErrorKind::PermissionDenied,"fixed_failure"))});
    assert!(called.get());assert!(fact.attempted&&fact.failed&&!fact.accepted);
    fact.call(|_|Ok(()));assert!(fact.attempted&&fact.accepted&&fact.failed);
    let mut state=OriginFacts::default();state.child_wait=true;state.exit_code=Some(0);state.job_query(Ok(0));
    state.job_terminate=fact;assert!(state.collected(true));assert!(!state.unforced_success());
}

#[cfg(windows)]
#[test]
fn actual_origin_reader_forward_stop_keeps_all_raw_and_eof() {
    struct Slices{input:Cursor<Vec<u8>>}
    impl Read for Slices {fn read(&mut self,b:&mut [u8])->io::Result<usize>{let size=b.len().min(7);self.input.read(&mut b[..size])}}
    let raw=b"{\"id\":2,\"result\":{}}\nnotice-A\nnotice-B\nlast-no-LF";
    for dropped in [false,true] {let dir=tempfile::tempdir().unwrap();let path=dir.path().join("connection.stdout.native.jsonl");
        let state=Arc::new(Mutex::new(StreamState::default()));let stop=Arc::new(std::sync::atomic::AtomicBool::new(true));
        let (tx,rx)=std::sync::mpsc::sync_channel(1);if dropped {drop(rx);}else{tx.send(Ok(vec![1])).unwrap();}
        native::origin_read_lines(CloseOnce::new(Slices{input:Cursor::new(raw.to_vec())},|_|Ok(())),path.clone(),tx,stop,state.clone()).unwrap();
        assert_eq!(fs::read(&path).unwrap(),raw);let actual=state.lock().unwrap();
        assert_eq!(actual.eof,Observed::Confirmed);assert_eq!(actual.persisted,Observed::Confirmed);assert_eq!(actual.seal.as_ref().unwrap().sha256,native::sha256(raw).unwrap());
    }
}
#[cfg(windows)]
#[test]
fn actual_origin_reader_full_queue_then_shutdown_does_not_cut_tail() {
    let dir=tempfile::tempdir().unwrap();let path=dir.path().join("connection.stdout.native.jsonl");
    let raw=b"{\"id\":2,\"result\":{}}\ntail-one\ntail-two\n".to_vec();let expected=raw.clone();
    let state=Arc::new(Mutex::new(StreamState::default()));let checked=state.clone();
    let stop=Arc::new(std::sync::atomic::AtomicBool::new(false));let later=stop.clone();let (tx,rx)=std::sync::mpsc::sync_channel(1);
    let (full_tx,full_rx)=std::sync::mpsc::sync_channel(1);let destination=path.clone();
    let worker=std::thread::Builder::new().spawn(move || {
        native::ORIGIN_FORWARD_FULL_OBSERVER.with(|h|*h.borrow_mut()=Some(full_tx));
        native::origin_read_lines(CloseOnce::new(Cursor::new(raw),|_|Ok(())),destination,tx,stop,state)
    }).unwrap();
    full_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();
    assert_eq!(rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap().unwrap(),b"{\"id\":2,\"result\":{}}\n");
    later.store(true,std::sync::atomic::Ordering::Release);drop(rx);
    assert!(worker.join().unwrap().is_ok());assert_eq!(fs::read(path).unwrap(),expected);assert_eq!(checked.lock().unwrap().eof,Observed::Confirmed);
}
#[cfg(windows)]
#[test]
fn actual_origin_reader_fault_overflow_and_exact_budget_are_separate() {
    struct FailingRead{step:bool}
    impl Read for FailingRead {fn read(&mut self,b:&mut [u8])->io::Result<usize>{if self.step{return Err(io::Error::other("owned fixture read failure"));}self.step=true;b[..2].copy_from_slice(b"a\n");Ok(2)}}
    for overflow in [false,true] {let dir=tempfile::tempdir().unwrap();let state=Arc::new(Mutex::new(StreamState::default()));let stop=Arc::new(std::sync::atomic::AtomicBool::new(true));let (tx,rx)=std::sync::mpsc::sync_channel(1);drop(rx);
        let result=if overflow {native::origin_read_lines(CloseOnce::new(Cursor::new(vec![b'a';MAX_FRAME+1]),|_|Ok(())),dir.path().join("raw"),tx,stop,state.clone())}
            else{native::origin_read_lines(CloseOnce::new(FailingRead{step:false},|_|Ok(())),dir.path().join("raw"),tx,stop,state.clone())};
        assert!(result.is_err());assert_ne!(state.lock().unwrap().eof,Observed::Confirmed);assert_eq!(state.lock().unwrap().persisted,Observed::Failed);
    }
    let dir=tempfile::tempdir().unwrap();let path=dir.path().join("raw");let mut raw=vec![b'a';MAX_FRAME];raw[MAX_FRAME-1]=b'\n';
    let state=Arc::new(Mutex::new(StreamState::default()));let (tx,rx)=std::sync::mpsc::sync_channel(1);drop(rx);
    native::origin_read_lines(CloseOnce::new(Cursor::new(raw.clone()),|_|Ok(())),path.clone(),tx,Arc::new(std::sync::atomic::AtomicBool::new(true)),state.clone()).unwrap();
    assert_eq!(fs::read(path).unwrap(),raw);assert_eq!(state.lock().unwrap().eof,Observed::Confirmed);assert_eq!(state.lock().unwrap().seal.as_ref().unwrap().bytes,MAX_FRAME as u64);
}

#[cfg(windows)]
#[test]
fn capture_fixture_child_stream() {
    if std::env::var("CC_NATIVE_CAPTURE_CHILD").as_deref()!=Ok("1") {return;}
    let mut bytes=Vec::new();std::io::stdin().take(4097).read_to_end(&mut bytes).unwrap();
    assert_eq!(bytes,b"captured-owned-input");
    println!("{{\"type\":\"thread.started\",\"thread_id\":\"owned-fixture-thread\"}}");
    if std::env::var("CC_NATIVE_CAPTURE_HOLD").as_deref()==Ok("1") {
        loop {std::thread::sleep(std::time::Duration::from_millis(20));}
    }
    println!("{{\"type\":\"turn.completed\"}}");
}

#[cfg(windows)]
struct CaptureFixtureDiagnostic {path:PathBuf,phase:&'static str,category:&'static str,finished:bool,facts:Value}
#[cfg(windows)]
impl CaptureFixtureDiagnostic {
    fn new(directory:&Path)->Self {
        let state=Self{path:directory.join("controller-diagnostic.json"),phase:"entered",category:"none",finished:false,facts:Value::Null};
        state.save();state
    }
    fn step(&mut self,phase:&'static str) {self.phase=phase;self.save();}
    fn save(&self) {
        let value=json!({"schema":"owned-capture-fixture-diagnostic/1","phase":self.phase,"category":self.category,
            "finished":self.finished,"unwinding":std::thread::panicking(),"facts":self.facts,"source_scope":"test_owned_controller_only"});
        assert!(fs::write(&self.path,serde_json::to_vec(&value).unwrap()).is_ok(),"fixture_diagnostic_write_failed");
    }
    fn io<T>(&mut self,result:io::Result<T>)->T {
        match result {Ok(value)=>value,Err(error)=>{
            if let Some(failure)=error.get_ref().and_then(|value|value.downcast_ref::<ExportReadFailure>()) {
                self.phase=match failure.phase {"receipt"=>"read_registered_receipt","collection"=>"read_registered_collection",_=>"read_registered_binding"};
                self.category=failure.category;self.facts=json!({"binding_matches":failure.binding_matches});self.save();panic!("owned_capture_fixture_read_failed");
            }
            self.category=match error.raw_os_error() {Some(5)=>"win32_access_denied",Some(6)=>"win32_invalid_handle",Some(32)=>"win32_sharing_violation",Some(87)=>"win32_invalid_parameter",
                _=>match error.kind(){io::ErrorKind::PermissionDenied=>"permission_denied",io::ErrorKind::TimedOut=>"timed_out",io::ErrorKind::NotFound=>"not_found",io::ErrorKind::InvalidData=>"invalid_data",_=>"io_failure"}};
            self.save();panic!("owned_capture_fixture_io_failed");
        }}
    }
    fn app<T>(&mut self,result:crate::Result<T>)->T {
        match result {Ok(value)=>value,Err(error)=>{
            self.category=match error.message.as_str() {
                "native capture input rejected"=>"native_input_rejected",
                "native capture not admitted"=>"native_capture_not_admitted",
                "native launch generation mismatch"=>"native_generation_mismatch",
                "native export generation changed"=>"native_export_generation_changed",
                _=>if error.code==75 {"job_io_temporarily_unavailable"}else{"job_api_failure"},
            };
            self.save();panic!("owned_capture_fixture_api_failed");
        }}
    }
}
#[cfg(windows)]
impl Drop for CaptureFixtureDiagnostic {
    fn drop(&mut self) {
        if std::thread::panicking()&&self.category=="none" {self.category="assertion_or_panic";}
        // This fixture owns the directory. Never project panic messages, paths,
        // environment, raw stdout/stderr or arbitrary OS error strings.
        let value=json!({"schema":"owned-capture-fixture-diagnostic/1","phase":self.phase,"category":self.category,
            "finished":self.finished,"unwinding":std::thread::panicking(),"facts":self.facts,"source_scope":"test_owned_controller_only"});
        let _=fs::write(&self.path,serde_json::to_vec(&value).unwrap_or_default());
    }
}

#[cfg(windows)]
#[test]
fn capture_job_controller_fixture() {
    use std::os::windows::{io::AsHandle,process::CommandExt};
    let Ok(mode)=std::env::var("CC_NATIVE_CAPTURE_CONTROLLER_MODE")else{return;};
    let directory=PathBuf::from(std::env::var_os("CC_NATIVE_CAPTURE_CONTROLLER_DIR").unwrap());
    let mut diagnostic=CaptureFixtureDiagnostic::new(&directory);
    diagnostic.step("create_capture_job");
    let flags=if mode=="breakaway" {0x800}else{0};
    let mut job=diagnostic.io(if mode=="outside"{native::empty_fixture_job(flags)}else{native::capture_fixture_job(flags)});
    let mut command=crate::hidden_command(std::env::current_exe().unwrap().to_str().unwrap());
    command.args(["--exact","native_export::tests::capture_fixture_child_stream","--nocapture"])
        .env("CC_NATIVE_CAPTURE_CHILD","1").env("CC_NATIVE_CAPTURE_HOLD",if mode=="reclaim"||mode=="retention"{"1"}else{"0"})
        .current_dir(&directory).stdin(std::process::Stdio::piped()).stdout(std::process::Stdio::piped()).stderr(std::process::Stdio::piped())
        .creation_flags(crate::NO_WINDOW|0x4);
    diagnostic.step("spawn_suspended_child");
    let mut child=OwnedFixtureChild{child:diagnostic.io(command.spawn())};
    diagnostic.step("pin_same_child_job");
    let processes=diagnostic.io(native::Processes::pin(child.child.as_handle(),Some(job.get().unwrap().as_handle())));
    diagnostic.step("observe_launch_membership");
    let before=serde_json::to_value(processes.collect()).unwrap();
    if mode=="breakaway"||mode=="outside" {
        if mode=="breakaway" {
            assert_eq!(before["job_breakaway_disabled"],"failed");
            assert!(processes.reclaim_children(std::time::Instant::now()+std::time::Duration::from_secs(1)).is_err());
        }else{assert_eq!(before["root_in_owned_job"],"failed");assert_eq!(before["all_job_children_collected"],"unknown");}
        child.child.kill().unwrap();child.child.wait().unwrap();let collection=serde_json::to_value(processes.finish()).unwrap();
        diagnostic.step("publish_rejected_membership_control");
        diagnostic.io(fs::write(directory.join("controller-result.json"),serde_json::to_vec(&collection).unwrap()));
        diagnostic.step("close_rejected_control_job");diagnostic.io(job.close());diagnostic.finished=true;return;
    }
    let root=processes.root();let child_birth=processes.child();
    let rt:crate::SharedRuntime=Arc::new(crate::System::default());
    let context=Context{root:directory.join("root"),jobs:directory.join("jobs"),home:directory.join("home"),codex_home:directory.join("codex"),gto:directory.join("gto.exe"),executable:std::env::current_exe().unwrap(),rt};
    diagnostic.step("create_owned_job_record");
    let jid="owned-capture-job";diagnostic.io(fs::create_dir_all(context.job_dir(jid)));
    let mut row=json!({"id":jid,"name":"owned fixture","status":"running","native_evidence_purpose":PURPOSE,"started_at":"owned-fixture-generation",
        "pid":root.pid,"pid_created":root.created,"codex_pid":child_birth.pid,"codex_pid_created":child_birth.created,"cwd":directory});
    diagnostic.step("save_owned_running_record");diagnostic.app(context.save(&row));let _=processes.finish();
    diagnostic.step("begin_native_capture");
    let (mut capture,input)=diagnostic.app(context.begin_native_capture(jid,b"captured-owned-input",&[],child.child.as_handle(),Some(job.get().unwrap().as_handle())));
    diagnostic.step("owned_input_writer");
    let writer=diagnostic.io(capture.owned_input_writer(input,&mut child.child));
    let input=diagnostic.io(spawn_owned_thread(NativeThreadRole::CaptureInput,move||writer.write()));
    diagnostic.step("same_owned_tee_setup");
    let mut stdout=diagnostic.io(capture.stdout(child.child.stdout.take().unwrap()));
    let mut stderr=diagnostic.io(capture.stderr(child.child.stderr.take().unwrap()));
    let (release,wait)=std::sync::mpsc::sync_channel::<()>(1);let (entered_tx,entered_rx)=std::sync::mpsc::sync_channel(1);
    let retained=mode=="retention";
    let readers=vec![spawn_owned_thread(NativeThreadRole::CaptureStdout,move||{
        if retained {entered_tx.send(()).unwrap();wait.recv_timeout(std::time::Duration::from_secs(10)).unwrap();}
        stdout.read_to_end(&mut Vec::new()).unwrap();
    }).unwrap(),spawn_owned_thread(NativeThreadRole::CaptureStderr,move||{stderr.read_to_end(&mut Vec::new()).unwrap();}).unwrap()];
    diagnostic.step("resume_same_child");diagnostic.io(capture.resume_owned_child(&child.child));
    let until=std::time::Instant::now()+std::time::Duration::from_secs(8);
    if mode=="reclaim" {
        while !input.is_finished(){assert!(std::time::Instant::now()<until);std::thread::sleep(std::time::Duration::from_millis(5));}
        diagnostic.step("reclaim_same_job_children");diagnostic.io(capture.reclaim_owned_children(until));
    }
    if retained {
        diagnostic.step("retention_pending_and_drain");
        entered_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();
        while !input.is_finished(){assert!(std::time::Instant::now()<until);std::thread::sleep(std::time::Duration::from_millis(5));}
        assert!(child.child.try_wait().unwrap().is_none());
        row["status"]=json!("failed");context.save(&row).unwrap();
        let owned_child=unsafe{std::ptr::read(&child.child)};std::mem::forget(child);
        retain_native_attempt(&context,&row,Box::new(owned_child),Some(capture),readers,Some(input)).unwrap();
        assert!(has_retained_attempt(jid));drain_retained_attempts(&context,jid);assert!(has_retained_attempt(jid));
        assert!(context.load(jid).unwrap()["native_export"].is_null());
        release.send(()).unwrap();
        while has_retained_attempt(jid) {assert!(std::time::Instant::now()<until);drain_retained_attempts(&context,jid);std::thread::sleep(std::time::Duration::from_millis(5));}
        let row=context.load(jid).unwrap();assert_eq!(row["status"],"failed");assert_eq!(row["native_retention_drained"],true);assert_eq!(row["native_retention_drain_failed"],false);
        let selected=read_export_artifacts(context.job_dir(jid).join(row["native_export"]["directory"].as_str().unwrap()),&row["native_export"],jid,|_|Ok(())).unwrap();
        assert_eq!(selected.metadata()["original_complete"],true);assert_eq!(selected.collection()["std_child_wait"]["reaped"],"confirmed");
        let before_repeat=row["native_export"].clone();drain_retained_attempts(&context,jid);assert!(!has_retained_attempt(jid));assert_eq!(context.load(jid).unwrap()["native_export"],before_repeat);
        fs::write(directory.join("controller-result.json"),serde_json::to_vec(selected.metadata()).unwrap()).unwrap();
    }else{
        diagnostic.step("wait_same_child");
        loop {if capture.observe_child_wait(&mut child.child).unwrap().is_some(){break;}assert!(std::time::Instant::now()<until);std::thread::sleep(std::time::Duration::from_millis(5));}
        diagnostic.step("join_raw_readers");
        for reader in readers {while !reader.is_finished(){assert!(std::time::Instant::now()<until);std::thread::sleep(std::time::Duration::from_millis(5));}reader.join().unwrap();}
        diagnostic.step("join_owned_input");input.join().unwrap().unwrap();
        diagnostic.step("collect_same_job_active_one");
        while serde_json::to_value(capture.processes.collect()).unwrap()["job_active_processes"]!=1 {assert!(std::time::Instant::now()<until);std::thread::sleep(std::time::Duration::from_millis(5));}
        let collection=serde_json::to_value(capture.processes.collect()).unwrap();
        assert_eq!(collection["job_active_processes"],1);assert_eq!(collection["all_job_children_collected"],"confirmed");
        assert_eq!(collection["job_breakaway_disabled"],"confirmed");
        diagnostic.step("finish_original_export");let finished=diagnostic.io(capture.finish());
        diagnostic.step("publish_same_generation_export");diagnostic.app(context.publish_native_export(finished));
        diagnostic.step("read_same_export_registration");let row=diagnostic.app(context.load(jid));
        let selected=diagnostic.io(read_export_artifacts(context.job_dir(jid).join(row["native_export"]["directory"].as_str().unwrap()),&row["native_export"],jid,|_|Ok(())));
        diagnostic.facts=json!({
            "original_complete":selected.metadata()["original_complete"].as_bool(),
            "stdin_closed":selected.metadata()["input"]["writer_closed"]=="confirmed",
            "stdout_eof":selected.metadata()["stdout"]["eof"]=="confirmed",
            "stderr_eof":selected.metadata()["stderr"]["eof"]=="confirmed",
            "std_child_reaped":selected.collection()["std_child_wait"]["reaped"]=="confirmed",
            "job_active_processes":selected.collection()["os_observation"]["job_active_processes"].as_u64()});
        diagnostic.step("assert_original_completion");
        assert_eq!(selected.metadata()["original_complete"],true);assert_eq!(selected.metadata()["input"]["writer_closed"],"confirmed");
        assert_eq!(selected.metadata()["stdout"]["eof"],"confirmed");assert_eq!(selected.metadata()["stderr"]["eof"],"confirmed");
        assert_eq!(selected.collection()["std_child_wait"]["reaped"],"confirmed");
        fs::write(directory.join("controller-result.json"),serde_json::to_vec(selected.metadata()).unwrap()).unwrap();
    }
    diagnostic.step("close_controller_capture_job");diagnostic.io(job.close());
    diagnostic.step("finished");diagnostic.finished=true;
}
#[cfg(windows)]
#[test]
fn actual_capture_job_reclaim_retention_and_membership_controls() {
    use std::os::windows::process::CommandExt;
    for mode in ["normal","reclaim","retention","breakaway","outside"] {
        let directory=tempfile::tempdir().unwrap();let mut guard=native::empty_fixture_job(0x2000).unwrap();
        let mut command=crate::hidden_command(std::env::current_exe().unwrap().to_str().unwrap());
        command.args(["--exact","native_export::tests::capture_job_controller_fixture","--nocapture"])
            .env("CC_NATIVE_CAPTURE_CONTROLLER_MODE",mode).env("CC_NATIVE_CAPTURE_CONTROLLER_DIR",directory.path())
            .current_dir(directory.path()).stdin(std::process::Stdio::null()).stdout(std::process::Stdio::null()).stderr(std::process::Stdio::null())
            .creation_flags(crate::NO_WINDOW|0x4);
        let mut child=OwnedFixtureChild{child:command.spawn().unwrap()};
        native::assign_fixture_child(&guard,&child.child).unwrap();native::resume_child(&child.child).unwrap();
        let until=std::time::Instant::now()+std::time::Duration::from_secs(20);
        loop {if let Some(status)=child.child.try_wait().unwrap(){
            if !status.success() {
                let raw=fs::File::open(directory.path().join("controller-diagnostic.json")).ok().and_then(|file| {
                    let mut bytes=Vec::new();file.take(4097).read_to_end(&mut bytes).ok()?;(bytes.len()<=4096).then_some(bytes)
                });
                let record=raw.as_deref().and_then(|raw|serde_json::from_slice::<Value>(raw).ok());
                let phases=["entered","create_capture_job","spawn_suspended_child","pin_same_child_job","observe_launch_membership","publish_rejected_membership_control","close_rejected_control_job","create_owned_job_record","save_owned_running_record","begin_native_capture","owned_input_writer","same_owned_tee_setup","resume_same_child","reclaim_same_job_children","retention_pending_and_drain","wait_same_child","join_raw_readers","join_owned_input","collect_same_job_active_one","finish_original_export","publish_same_generation_export","read_same_export_registration","assert_original_completion","close_controller_capture_job","finished","read_registered_receipt","read_registered_collection","read_registered_binding"];
                let categories=["none","win32_access_denied","win32_invalid_handle","win32_sharing_violation","win32_invalid_parameter","permission_denied","timed_out","not_found","invalid_data","io_failure","job_api_failure","job_io_temporarily_unavailable","native_input_rejected","native_capture_not_admitted","native_generation_mismatch","native_export_generation_changed","assertion_or_panic","stable_read_failed","fact_encoding_failed","registered_fact_mismatch","strict_json_rejected","attempt_binding_mismatch","receipt_collection_mismatch"];
                let phase=record.as_ref().map(|r|s(r,"phase")).filter(|p|phases.contains(p)).unwrap_or("unavailable");
                let category=record.as_ref().map(|r|s(r,"category")).filter(|p|categories.contains(p)).unwrap_or("unavailable");
                let facts=record.as_ref().map(|r|&r["facts"]);
                let original_complete=facts.and_then(|f|f["original_complete"].as_bool());
                let stdin_closed=facts.and_then(|f|f["stdin_closed"].as_bool());
                let stdout_eof=facts.and_then(|f|f["stdout_eof"].as_bool());
                let stderr_eof=facts.and_then(|f|f["stderr_eof"].as_bool());
                let std_child_reaped=facts.and_then(|f|f["std_child_reaped"].as_bool());
                let job_active=facts.and_then(|f|f["job_active_processes"].as_u64());
                let binding_matches:[Option<bool>;6]=std::array::from_fn(|at|facts.and_then(|f|f["binding_matches"].get(at)).and_then(Value::as_bool));
                panic!("owned_capture_fixture_failed mode={mode} phase={phase} category={category} exit_nonzero=true original_complete={original_complete:?} stdin_closed={stdin_closed:?} stdout_eof={stdout_eof:?} stderr_eof={stderr_eof:?} std_child_reaped={std_child_reaped:?} job_active={job_active:?} binding_matches={binding_matches:?}");
            }
            break;}
            assert!(std::time::Instant::now()<until,"{mode}");std::thread::sleep(std::time::Duration::from_millis(10));}
        let value:Value=serde_json::from_slice(&fs::read(directory.path().join("controller-result.json")).unwrap()).unwrap();
        if mode=="breakaway" {assert_eq!(value["job_breakaway_disabled"],"failed");}
        else if mode=="outside" {assert_eq!(value["root_in_owned_job"],"failed");}
        else {assert_eq!(value["original_complete"],true);assert_eq!(value["collection"]["os_observation"]["job_active_processes"],1);}
        guard.close().unwrap();
    }
}

#[test]
fn task_progress_real_start_and_drop_collects_the_same_notification_worker() {
    struct LocalRuntime {start:std::time::Instant,tools:PathBuf}
    impl crate::Runtime for LocalRuntime {
        fn mono(&self)->f64 {self.start.elapsed().as_secs_f64()}
        fn env(&self)->std::collections::BTreeMap<String,String> {std::collections::BTreeMap::new()}
        fn task_title_tools(&self)->PathBuf {self.tools.clone()}
        fn call(&self,_:crate::Call)->io::Result<crate::Reply> {panic!("no external process is allowed by fixture")}
        fn spawn(&self,_:&crate::Spawn)->io::Result<Box<dyn crate::Running>> {Err(rejected())}
        fn spawn_supervisor(&self,_:&Path,_:&Path,_:&Path,_:&str)->io::Result<bool> {Err(rejected())}
    }
    let dir=tempfile::tempdir().unwrap();let rt:crate::SharedRuntime=Arc::new(LocalRuntime{start:std::time::Instant::now(),tools:dir.path().join("missing-owned-names")});
    let mut task=crate::notify::TaskProgress::new(Some("test-notifier-no-process".into()),dir.path().into(),rt);task.backoff=0.;task.shutdown_cap=3.;
    let mut owner=ProgressOwner::new(task,true);owner.start(&json!({"id":"test-progress","status":"running"}));
    assert_eq!(owner.progress.collection_facts()["worker_started"],true);
    assert!(owner.collect());let facts=owner.progress.collection_facts();assert_eq!(facts["worker_joined"],true);assert_eq!(facts["done_received"],true);
    // Failed delivery is existing business data. This assertion is worker
    // termination/collection only, not proof that any notice was delivered.
}
#[test]
fn same_notification_worker_pending_terminal_and_panic_are_distinct() {
    use crate::notify::{ProgressCompletion,TaskProgress};
    for mode in ["normal","sender_drop","panic","pending_done","done_before_return"] {
        let dir=tempfile::tempdir().unwrap();let rt:crate::SharedRuntime=Arc::new(crate::System::default());
        let mut task=TaskProgress::new(None,dir.path().into(),rt);task.shutdown_cap=0.01;
        let (entered_tx,entered_rx)=std::sync::mpsc::sync_channel(1);let (release_tx,release_rx)=std::sync::mpsc::sync_channel(1);let chosen=mode.to_string();
        task.completion_test_worker(move |done| {
            if chosen=="done_before_return" {done.send(()).unwrap();}
            if chosen=="sender_drop" {drop(done);entered_tx.send(()).unwrap();return;}
            entered_tx.send(()).unwrap();
            if chosen=="pending_done"||chosen=="done_before_return" {release_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();}
            assert_ne!(chosen,"panic","notification fixture panic");
            if chosen!="done_before_return" {done.send(()).unwrap();}
        });
        entered_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();let mut owner=ProgressOwner::new(task,true);
        if mode=="pending_done"||mode=="done_before_return" {assert!(!owner.close());assert!(!owner.closed);assert_eq!(owner.progress.completion(),ProgressCompletion::Pending);release_tx.send(()).unwrap();}
        let success=owner.collect();assert!(owner.closed);assert_eq!(success,matches!(mode,"normal"|"pending_done"|"done_before_return"));
        let facts=owner.progress.collection_facts();assert_eq!(facts["worker_joined"],true);assert_eq!(facts["close_requested"],true);assert_eq!(facts["done_received"],success);
        assert_eq!(owner.close(),success);assert_eq!(owner.progress.collection_facts(),facts);
    }
}
#[test]
fn disconnected_notification_sender_with_live_worker_remains_pending_until_join() {
    use crate::notify::{ProgressCompletion,TaskProgress};
    let dir=tempfile::tempdir().unwrap();let rt:crate::SharedRuntime=Arc::new(crate::System::default());let mut task=TaskProgress::new(None,dir.path().into(),rt);task.shutdown_cap=0.01;
    let (entered_tx,entered_rx)=std::sync::mpsc::sync_channel(1);let (release_tx,release_rx)=std::sync::mpsc::sync_channel(1);
    task.completion_test_worker(move |done| {drop(done);entered_tx.send(()).unwrap();release_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();});
    entered_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();assert_eq!(task.close_status(),ProgressCompletion::Pending);assert_eq!(task.collection_facts()["worker_joined"],false);
    release_tx.send(()).unwrap();let mut owner=ProgressOwner::new(task,true);assert!(!owner.collect());assert_eq!(owner.progress.completion(),ProgressCompletion::Failed);assert_eq!(owner.progress.collection_facts()["worker_joined"],true);
}
#[test]
fn ordinary_notification_close_bool_and_timeout_remain_compatible() {
    use crate::notify::{ProgressCompletion,TaskProgress};
    let dir=tempfile::tempdir().unwrap();let rt:crate::SharedRuntime=Arc::new(crate::System::default());let mut task=TaskProgress::new(None,dir.path().into(),rt);assert!(task.close());
    let (tx,rx)=std::sync::mpsc::sync_channel::<()>(1);task.completion_test_worker(move |done| {rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();done.send(()).unwrap();});task.shutdown_cap=0.01;
    assert!(!task.close());tx.send(()).unwrap();task.shutdown_cap=3.;assert!(task.close());assert!(task.close());assert_eq!(task.completion(),ProgressCompletion::Done);assert_eq!(task.collection_facts()["worker_joined"],true);
}

#[test]
fn progress_owner_drop_waits_for_real_worker_before_returning_failed_business() {
    let dir=tempfile::tempdir().unwrap();let rt:crate::SharedRuntime=Arc::new(crate::System::default());
    let mut task=crate::notify::TaskProgress::new(None,dir.path().into(),rt);task.shutdown_cap=0.01;
    let (ready_tx,ready_rx)=std::sync::mpsc::sync_channel(1);let (release_tx,release_rx)=std::sync::mpsc::sync_channel(1);
    task.completion_test_worker(move |done| {ready_tx.send(()).unwrap();release_rx.recv_timeout(std::time::Duration::from_secs(5)).unwrap();done.send(()).unwrap();});
    ready_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();
    let (attempt_tx,attempt_rx)=std::sync::mpsc::sync_channel(1);let (returned_tx,returned_rx)=std::sync::mpsc::sync_channel(1);
    let worker=std::thread::Builder::new().spawn(move || {
        let result=(||->io::Result<()> {let mut owner=ProgressOwner::new(task,true);assert!(!owner.close());attempt_tx.send(()).unwrap();Err(rejected())})();
        returned_tx.send(result.is_err()).unwrap();
    }).unwrap();
    attempt_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();assert!(returned_rx.try_recv().is_err());
    release_tx.send(()).unwrap();assert!(returned_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap());worker.join().unwrap();
}


#[test]
fn progress_timeout_join_observes_the_same_workers_last_done() {
    use crate::notify::{ProgressCompletion,TaskProgress};
    for mode in ["normal","sender_drop","panic"] {
        let dir=tempfile::tempdir().unwrap();
        let rt:crate::SharedRuntime=Arc::new(crate::System::default());
        let mut task=TaskProgress::new(None,dir.path().into(),rt);
        task.shutdown_cap=0.;
        let (release,wait)=std::sync::mpsc::sync_channel(1);
        let chosen=mode.to_string();
        task.completion_test_worker(move|done| {
            wait.recv_timeout(std::time::Duration::from_secs(3)).unwrap();
            if chosen=="sender_drop" {drop(done);return;}
            assert_ne!(chosen,"panic","fixed_worker_panic");
            done.send(()).unwrap();
        });
        let returned=Arc::new(std::sync::atomic::AtomicBool::new(false));
        let entered=returned.clone();
        crate::notify::PROGRESS_AFTER_TIMEOUT.with(|hook|*hook.borrow_mut()=Some(Box::new(move|worker_thread:&std::thread::JoinHandle<()>| {
            release.send(()).unwrap();
            let until=std::time::Instant::now()+std::time::Duration::from_secs(3);
            while !worker_thread.is_finished() {
                assert!(std::time::Instant::now()<until,"worker_did_not_return");
                std::thread::yield_now();
            }
            entered.store(true,std::sync::atomic::Ordering::Release);
        })));
        let actual=task.close_status();
        assert!(returned.load(std::sync::atomic::Ordering::Acquire));
        assert_eq!(actual,if mode=="normal"{ProgressCompletion::Done}else{ProgressCompletion::Failed});
        let facts=task.collection_facts();
        assert_eq!(facts["worker_joined"],true);
        assert_eq!(facts["done_received"],mode=="normal");
        assert_eq!(task.close_status(),actual);
        assert_eq!(task.collection_facts(),facts);
    }
}

#[cfg(windows)]
#[test]
fn native_terminal_publication_waits_for_notification_and_never_reverses_success() {
    struct WaitRuntime {start:std::time::Instant,seen:Mutex<Vec<String>>,terminal_saves:std::sync::atomic::AtomicUsize}
    impl crate::Runtime for WaitRuntime {
        fn mono(&self)->f64 {self.start.elapsed().as_secs_f64()}
        fn env(&self)->std::collections::BTreeMap<String,String> {std::collections::BTreeMap::new()}
        fn diagnostic(&self,_:&str) {}
        fn proc_created_checked(&self,pid:u32)->io::Result<Option<u64>> {Ok(if pid==std::process::id(){Some(1)}else{None})}
        fn before_save(&self,path:&Path,row:&Value)->io::Result<()> {
            if path.file_name().is_some_and(|name|name=="job.json") {
                let status=s(row,"status").to_string();self.seen.lock().unwrap().push(status.clone());
                if !crate::LIVE.contains(&status.as_str()) {self.terminal_saves.fetch_add(1,std::sync::atomic::Ordering::SeqCst);}
            }
            Ok(())
        }
        fn call(&self,_:crate::Call)->io::Result<crate::Reply> {panic!("fixture_external_call_forbidden")}
        fn spawn(&self,_:&crate::Spawn)->io::Result<Box<dyn crate::Running>> {Err(rejected())}
        fn spawn_supervisor(&self,_:&Path,_:&Path,_:&Path,_:&str)->io::Result<bool> {Err(rejected())}
    }
    for mode in ["normal","panic","sender_drop","business_failed","cancelled","replaced"] {
        let dir=tempfile::tempdir().unwrap();
        let observed=Arc::new(WaitRuntime{start:std::time::Instant::now(),seen:Mutex::new(Vec::new()),terminal_saves:std::sync::atomic::AtomicUsize::new(0)});
        let rt:crate::SharedRuntime=observed.clone();
        let context=Context{root:dir.path().join("root"),jobs:dir.path().join("jobs"),home:dir.path().join("home"),codex_home:dir.path().join("codex"),gto:dir.path().join("gto.exe"),executable:dir.path().join("no-execution"),rt};
        let jid="native-notification-final";
        fs::create_dir_all(context.job_dir(jid)).unwrap();
        let row=json!({"id":jid,"name":"owned notification fixture","status":"running","native_evidence_purpose":PURPOSE,
            "started_at":"generation-a","pid":std::process::id(),"pid_created":1,"codex_pid":1234,"codex_pid_created":2,"gto":{"fixture":true}});
        context.save(&row).unwrap();
        let mut proposed=row.clone();proposed["status"]=json!(if mode=="business_failed"{"failed"}else{"succeeded"});
        let mut task=crate::notify::TaskProgress::new(None,context.jobs.clone(),context.rt.clone());task.shutdown_cap=0.01;
        let (ready_tx,ready_rx)=std::sync::mpsc::sync_channel(1);let (release_tx,release_rx)=std::sync::mpsc::sync_channel(1);let chosen=mode.to_string();
        task.completion_test_worker(move|done| {
            ready_tx.send(()).unwrap();release_rx.recv_timeout(std::time::Duration::from_secs(5)).unwrap();
            if chosen=="sender_drop" {drop(done);return;}
            assert_ne!(chosen,"panic","fixed_notification_failure");done.send(()).unwrap();
        });
        ready_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap();
        let mut owner=ProgressOwner::new(task,true);owner.bind_job(&context.job_dir(jid),&row);
        let publish_context=context.clone();
        let (done_tx,done_rx)=std::sync::mpsc::sync_channel(1);
        let publisher=std::thread::Builder::new().spawn(move|| {
            let value=publish_context.publish_native_terminal_after_notification(&proposed,&mut Some(owner));
            done_tx.send(value).unwrap();
        }).unwrap();
        let until=std::time::Instant::now()+std::time::Duration::from_secs(3);
        loop {if context.load(jid).unwrap()["native_notification_pending"]==true {break;}
            assert!(std::time::Instant::now()<until);std::thread::yield_now();}
        let args=crate::cli::Args{model_explicit:false,sandbox_explicit:false,mcp_profile_explicit:false,cmd:"wait".into(),id:jid.into(),
            values:std::collections::BTreeMap::from([("timeout".into(),"0".into())]),flags:std::collections::HashSet::new(),add_dirs:vec![]};
        assert_eq!(context.cmd_wait(&args,&mut Vec::new()).unwrap(),2,"pending_must_not_exit_success");
        assert_eq!(context.load(jid).unwrap()["status"],"running");assert!(done_rx.try_recv().is_err());
        assert!(!observed.seen.lock().unwrap().iter().any(|s|s=="succeeded"));
        if mode=="cancelled"||mode=="replaced" {
            let _lock=context.job_lock(jid,"state.lock").unwrap();let mut changed=context.load(jid).unwrap();
            if mode=="cancelled" {changed["status"]=json!("cancelled");changed["cancel_requested"]=json!(true);}
            else {changed["started_at"]=json!("generation-b");}
            context.save(&changed).unwrap();
        }
        release_tx.send(()).unwrap();let publication=done_rx.recv_timeout(std::time::Duration::from_secs(3)).unwrap().unwrap();publisher.join().unwrap();
        if mode=="replaced" {assert!(publication.is_none());}else{assert!(publication.is_some());}
        let final_job=publication.unwrap_or_else(||context.load(jid).unwrap());
        let expected=if mode=="normal"{"succeeded"}else if mode=="cancelled"{"cancelled"}else if mode=="replaced"{"running"}else{"failed"};
        assert_eq!(final_job["status"],expected);assert_eq!(context.load(jid).unwrap()["status"],expected);
        if mode=="replaced" {assert_eq!(final_job["started_at"],"generation-b");assert_eq!(context.cmd_wait(&args,&mut Vec::new()).unwrap(),2);}
        else {assert_eq!(context.cmd_wait(&args,&mut Vec::new()).unwrap(),if mode=="normal"{0}else{1});}
        if mode!="cancelled"&&mode!="replaced" {assert_eq!(observed.terminal_saves.load(std::sync::atomic::Ordering::SeqCst),1);}
        if mode!="normal" {assert!(!observed.seen.lock().unwrap().iter().any(|s|s=="succeeded"));}
        let snapshot=context.load(jid).unwrap();std::thread::yield_now();assert_eq!(context.load(jid).unwrap(),snapshot);
    }
}

#[cfg(windows)]
#[test]
fn controller_failure_diagnostic_has_only_fixed_phase_and_category() {
    for mode in ["io","job","panic"] {
        let dir=tempfile::tempdir().unwrap();let path=dir.path().join("controller-diagnostic.json");
        let result=std::panic::catch_unwind(|| {
            let mut diagnostic=CaptureFixtureDiagnostic::new(dir.path());diagnostic.step("begin_native_capture");
            match mode {
                "io"=>diagnostic.io(Err::<(),_>(io::Error::new(io::ErrorKind::PermissionDenied,"private-path-secret-not-to-project"))),
                "job"=>diagnostic.app(Err::<(),_>(Error::new("private-path-secret-not-to-project"))),
                _=>panic!("fixed_fixture_panic"),
            }
        });
        assert!(result.is_err());let bytes=fs::read(path).unwrap();assert!(bytes.len()<=4096);
        assert!(!String::from_utf8_lossy(&bytes).contains("private-path-secret-not-to-project"));
        let value:Value=serde_json::from_slice(&bytes).unwrap();assert_eq!(value["phase"],"begin_native_capture");assert_eq!(value["unwinding"],true);
        assert_eq!(value["category"],match mode {"io"=>"permission_denied","job"=>"job_api_failure",_=>"assertion_or_panic"});
    }
}

struct NotificationLifecycleRuntime {
    clock:Mutex<f64>,tools:PathBuf,calls:Mutex<Vec<String>>,statuses:Mutex<Vec<String>>,row:PathBuf,
    start_fails:bool,finish_fails:bool,update_fails:bool,panic_update:bool,
}
impl crate::Runtime for NotificationLifecycleRuntime {
    fn mono(&self)->f64 {*self.clock.lock().unwrap()}
    fn sleep(&self,seconds:f64) {*self.clock.lock().unwrap()+=seconds;std::thread::yield_now();}
    fn env(&self)->std::collections::BTreeMap<String,String> {std::collections::BTreeMap::new()}
    fn task_title_tools(&self)->PathBuf {self.tools.clone()}
    fn task_job_title(&self,_:&Value,_:&str)->io::Result<String> {Ok("owned notification fixture".into())}
    fn call(&self,call:crate::Call)->io::Result<crate::Reply> {
        assert_eq!(call.argv.get(1).map(String::as_str),Some("notify"));
        assert_eq!(call.argv.get(2).map(String::as_str),Some("task"));
        let action=call.argv.get(3).unwrap().clone();assert!(["start","update","finish"].contains(&action.as_str()));
        self.calls.lock().unwrap().push(action.clone());
        let row:Value=serde_json::from_slice(&fs::read(&self.row).unwrap()).unwrap();
        self.statuses.lock().unwrap().push(s(&row,"status").to_string());
        if action=="update"&&self.panic_update {panic!("fixed_notification_fixture_panic");}
        let accepted=match action.as_str() {"start"=>!self.start_fails,"update"=>!self.update_fails,_=>!self.finish_fails};
        Ok(crate::Reply{code:0,stdout:json!({"ok":accepted}).to_string().into_bytes(),stderr:vec![]})
    }
    fn spawn(&self,_:&crate::Spawn)->io::Result<Box<dyn crate::Running>> {Err(rejected())}
    fn spawn_supervisor(&self,_:&Path,_:&Path,_:&Path,_:&str)->io::Result<bool> {Err(rejected())}
}
fn notification_lifecycle_fixture(directory:&Path,mode:&str)->(Context,Value,Arc<NotificationLifecycleRuntime>) {
    let tools=directory.join("title-tools");fs::create_dir_all(&tools).unwrap();
    fs::write(tools.join("client-projects.txt"),b"SyntheticFixture\n").unwrap();
    let jobs=directory.join("jobs");let jid="owned-notice-lifecycle";fs::create_dir_all(jobs.join(jid)).unwrap();
    let observed=Arc::new(NotificationLifecycleRuntime{clock:Mutex::new(100.),tools,calls:Mutex::new(Vec::new()),statuses:Mutex::new(Vec::new()),row:jobs.join(jid).join("job.json"),
        start_fails:mode=="start_failed",finish_fails:mode=="finish_failed",update_fails:mode=="update_failed",panic_update:mode=="worker_panicked"});
    let rt:crate::SharedRuntime=observed.clone();
    let context=Context{root:directory.join("root"),jobs,home:directory.join("home"),codex_home:directory.join("codex"),gto:directory.join("gto.exe"),executable:directory.join("no-launch"),rt};
    let row=json!({"id":jid,"name":"owned notification fixture","status":"running","native_evidence_purpose":PURPOSE,
        "started_at":"owned-generation","pid":std::process::id(),"pid_created":1,"codex_pid":1234,"codex_pid_created":2,"gto":{"fixture":true}});
    context.save(&row).unwrap();fs::write(context.job_dir(jid).join("prompt.md"),b"owned fixture").unwrap();
    (context,row,observed)
}
#[cfg(windows)]
#[test]
fn native_notification_start_gate_delivery_and_collection_have_one_lifecycle() {
    for mode in ["normal","cancelled","start_failed","finish_failed","update_failed","worker_panicked"] {
        let dir=tempfile::tempdir().unwrap();let (context,row,observed)=notification_lifecycle_fixture(dir.path(),mode);
        let mut task=crate::notify::TaskProgress::new(Some("fixture-notifier-no-process".into()),context.jobs.clone(),context.rt.clone());
        task.shutdown_cap=3.;let mut owner=ProgressOwner::new(task,true);owner.bind_job(&context.job_dir(s(&row,"id")),&row);owner.start(&row);
        let mut proposed=row.clone();proposed["status"]=json!(if mode=="cancelled"{"cancelled"}else{"succeeded"});
        if mode=="update_failed"||mode=="worker_panicked" {
            *observed.clock.lock().unwrap()=116.;owner.update(&row,false);
        }
        let mut notifier=Some(owner);
        assert!(!notifier.as_ref().unwrap().notify_with_retry("finish",&proposed,context.rt.mono()+35.));
        let published=context.publish_native_terminal_after_notification(&proposed,&mut notifier).unwrap().unwrap();
        let joined_facts=notifier.as_ref().unwrap().collection_facts();assert_eq!(joined_facts["worker_joined"],true);
        assert_eq!(joined_facts["delivery"]["finish_attempts"],0);
        assert_eq!(published["status"],if mode=="worker_panicked"{"failed"}else if mode=="cancelled"{"cancelled"}else{"succeeded"});
        let mut wrong_attempt=published.clone();wrong_attempt["started_at"]=json!("not-the-owned-generation");
        let before_wrong=observed.calls.lock().unwrap().clone();
        assert!(notifier.as_mut().unwrap().deliver_published_terminal(&context,&wrong_attempt).is_err());
        assert_eq!(*observed.calls.lock().unwrap(),before_wrong);
        notifier.as_mut().unwrap().deliver_published_terminal(&context,&published).unwrap();
        let collection=notifier.as_ref().unwrap().collection_facts();let delivery=notifier.as_ref().unwrap().delivery_facts();
        let calls=observed.calls.lock().unwrap().clone();
        if mode=="start_failed" {
            assert_eq!(calls,["start";4]);assert_eq!(delivery["start"],"failed");assert_eq!(delivery["start_attempts"],4);
            assert_eq!(delivery["finish_attempts"],0);assert_eq!(delivery["finish"],"suppressed");assert_eq!(delivery["finish_claimed"],false);
            assert_eq!(collection["state"],"done");
        }else if mode=="worker_panicked" {
            assert_eq!(calls,["start","update"]);assert_eq!(collection["state"],"failed");assert_eq!(delivery["finish_attempts"],0);
        }else{
            let finishes=calls.iter().filter(|action|*action=="finish").count();assert_eq!(finishes,if mode=="finish_failed"{4}else{1});
            assert_eq!(delivery["start"],"accepted");assert_eq!(delivery["start_attempts"],1);assert_eq!(delivery["finish_claimed"],true);
            assert_eq!(delivery["finish"],if mode=="finish_failed"{"failed"}else{"accepted"});
            if mode=="update_failed" {assert_eq!(delivery["updates_failed"],1);}
        }
        let statuses=observed.statuses.lock().unwrap().clone();
        for (action,status) in calls.iter().zip(statuses) {
            if action=="finish" {assert_eq!(status,s(&published,"status"));}else{assert_eq!(status,"running");}
        }
        let report:Value=serde_json::from_slice(&fs::read(context.job_dir(s(&row,"id")).join("notification-delivery.private.json")).unwrap()).unwrap();
        assert_eq!(report["terminal_published"],true);assert_eq!(report["collection"]["delivery"],delivery);
        assert_eq!(report["binding"]["started_at"],row["started_at"]);assert_eq!(context.load(s(&row,"id")).unwrap()["status"],published["status"]);
        let calls_before=observed.calls.lock().unwrap().clone();let facts_before=notifier.as_ref().unwrap().collection_facts();
        let _=notifier.as_mut().unwrap().close();let _=notifier.as_mut().unwrap().close();
        assert_eq!(notifier.as_ref().unwrap().collection_facts(),facts_before);
        assert!(notifier.as_mut().unwrap().deliver_published_terminal(&context,&published).is_err());
        assert_eq!(*observed.calls.lock().unwrap(),calls_before);
    }
}
#[test]
fn ordinary_and_cancellation_notifications_keep_the_actual_start_gate() {
    for mode in ["normal","start_failed","cancelled","cancelled_start_failed"] {
        let dir=tempfile::tempdir().unwrap();let failed=mode.ends_with("start_failed");
        let (context,mut row,observed)=notification_lifecycle_fixture(dir.path(),if failed{"start_failed"}else{"normal"});
        row["status"]=json!(if mode.starts_with("cancelled"){"cancelled"}else{"succeeded"});context.save(&row).unwrap();
        if mode.starts_with("cancelled") {
            crate::notify::task_cancel(&row,Some("fixture-notifier-no-process".into()),context.jobs.clone(),context.rt.clone());
        }else{
            let mut task=crate::notify::TaskProgress::new(Some("fixture-notifier-no-process".into()),context.jobs.clone(),context.rt.clone());
            task.shutdown_cap=3.;task.start(&row);task.update(&row,true);assert!(task.close());
            assert_eq!(task.delivery_facts()["start"],if failed{"failed"}else{"accepted"});
            let before=task.collection_facts();assert!(task.close());assert_eq!(task.collection_facts(),before);
        }
        let calls=observed.calls.lock().unwrap().clone();assert_eq!(calls,if failed{vec!["start";4]}else{vec!["start","finish"]});
    }
}

#[cfg(windows)]
#[test]
fn registered_export_components_keep_exact_facts_and_fixed_failure_categories() {
    let dir=tempfile::tempdir().unwrap();let path=dir.path().join("component.json");
    let expected=save_json(&path,&json!({"value":"owned"}),MAX_RECEIPT).unwrap();
    let expected=serde_json::to_value(expected).unwrap();
    assert_eq!(load_export_component(&path,&expected,MAX_RECEIPT,"receipt").unwrap()["value"],"owned");
    for key in ["name","bytes","sha256","identity"] {
        let mut changed=expected.clone();changed[key]=Value::Null;
        let error=load_export_component(&path,&changed,MAX_RECEIPT,"receipt").unwrap_err();
        let failure=error.get_ref().and_then(|e|e.downcast_ref::<ExportReadFailure>()).unwrap();
        assert_eq!(failure.phase,"receipt");assert_eq!(failure.category,"registered_fact_mismatch");
        assert!(!error.to_string().contains(&path.to_string_lossy().into_owned()));
    }
    let error=load_export_component(&dir.path().join("missing"),&expected,MAX_RECEIPT,"binding").unwrap_err();
    let failure=error.get_ref().and_then(|e|e.downcast_ref::<ExportReadFailure>()).unwrap();
    assert_eq!(failure.phase,"binding");assert_eq!(failure.category,"stable_read_failed");
    let malformed=dir.path().join("malformed.json");let bad=save_new(&malformed,b"{bad-json").unwrap();
    let error=load_export_component(&malformed,&serde_json::to_value(bad).unwrap(),MAX_RECEIPT,"collection").unwrap_err();
    let failure=error.get_ref().and_then(|e|e.downcast_ref::<ExportReadFailure>()).unwrap();assert_eq!(failure.category,"strict_json_rejected");
}

#[cfg(windows)]
#[test]
fn registered_export_binding_roundtrip_rejects_each_field_independently() {
    let directory=tempfile::tempdir().unwrap();
    let context=Context{root:directory.path().join("root"),jobs:directory.path().join("jobs"),home:directory.path().join("home"),
        codex_home:directory.path().join("codex"),gto:directory.path().join("unused-gto"),executable:std::env::current_exe().unwrap(),rt:Arc::new(crate::System::default())};
    let jid="owned-binding-fixture";let generation="fixture-generation";
    // Large integer identities exercise the actual JSON save/load route without
    // minting an OS launch: this is an internal artifact/registration fixture.
    let root=Birth{pid:201,created:134000000000000001};let child=Birth{pid:202,created:134000000000000003};
    let path=context.job_dir(jid).join("native-binding-fixture");fs::create_dir_all(&path).unwrap();
    let binding=save_json(&path.join("binding.private.json"),&json!({"fixture_only":true}),MAX_BINDING).unwrap();
    let collection=json!({"fixture_only":true});
    let collection_fact=save_json(&path.join("collection.private.json"),&collection,MAX_RECEIPT).unwrap();
    let receipt=json!({"domain":DOMAIN,"purpose":PURPOSE,"job_id":jid,"started_at":generation,
        "root":root,"child":child,"binding":binding,"collection":collection});
    let receipt_fact=save_json(&path.join("export.private.json"),&receipt,MAX_RECEIPT).unwrap();
    let row=json!({"id":jid,"status":"running","started_at":generation,"native_evidence_purpose":PURPOSE,
        "pid":root.pid,"pid_created":root.created,"codex_pid":child.pid,"codex_pid_created":child.created});
    context.save(&row).unwrap();
    context.publish_native_export(FinishedExport{job_id:jid.into(),generation:generation.into(),root:root.clone(),child:child.clone(),
        directory:"native-binding-fixture".into(),receipt:receipt_fact,collection:collection_fact}).unwrap();
    let persisted=context.load(jid).unwrap();let reg=&persisted["native_export"];
    assert_eq!(reg["root"]["created"],root.created);assert_eq!(reg["child"]["created"],child.created);
    let mut reads=Vec::new();
    let selected=read_export_artifacts(path,reg,jid,|path|{reads.push(path.file_name().unwrap().to_str().unwrap().to_string());Ok(())}).unwrap();
    assert_eq!(reads,["export.private.json","collection.private.json","binding.private.json"]);
    assert_eq!(export_attempt_binding_matches(selected.metadata(),reg,jid),[true;6]);
    for (at,key) in ["domain","purpose","job_id","started_at","root","child"].into_iter().enumerate() {
        let path=directory.path().join(format!("rejected-{at}"));fs::create_dir(&path).unwrap();
        let mut changed=receipt.clone();changed[key]=Value::Null;
        let fact=save_json(&path.join("export.private.json"),&changed,MAX_RECEIPT).unwrap();
        let mut registration=reg.clone();registration["receipt"]=serde_json::to_value(fact).unwrap();
        let mut reads=0;
        let result=read_export_artifacts(path,&registration,jid,|_|{reads+=1;Ok(())});
        let error=match result {Ok(_)=>panic!("mismatched_binding_was_accepted"),Err(error)=>error};
        let failure=error.get_ref().and_then(|error|error.downcast_ref::<ExportReadFailure>()).unwrap();
        let mut expected=[true;6];expected[at]=false;
        assert_eq!(failure.phase,"receipt");assert_eq!(failure.category,"attempt_binding_mismatch");
        assert_eq!(failure.binding_matches,Some(expected));assert_eq!(reads,1);
        let public=error.to_string();assert!(!public.contains(jid));assert!(!public.contains(generation));
        assert!(!public.contains(&directory.path().to_string_lossy().into_owned()));
    }
}

#[cfg(windows)]
#[test]
fn production_origin_deadline_is_not_reset_by_request_preservation() {
    let directory=tempfile::tempdir().unwrap();
    let (requests,observed)=std::sync::mpsc::sync_channel(1);
    let (reply,answers)=std::sync::mpsc::sync_channel(1);
    reply.send(Ok(br#"{"id":1,"result":{"thread":{"id":"thread-owned"}}}"#.to_vec())).unwrap();
    let mut transport=ConnectedReadonly::from_owned_connection(requests,answers,
        std::time::Instant::now()+std::time::Duration::from_secs(1)).unwrap();
    transport.deadline=std::time::Instant::now();
    assert!(collect_exact_rpc(&session(),&mut transport,directory.path()).is_err());
    assert!(matches!(observed.try_recv(),Err(std::sync::mpsc::TryRecvError::Empty)));
    let index=strict_value(&fs::read(directory.path().join("origin-index.private.json")).unwrap()).unwrap();
    assert_eq!(index["complete"],false);assert_eq!(index["files"][0]["transport"],"failed_or_timeout");
    assert_eq!(index["files"][0]["validation"],"not_attempted");assert!(index["files"][0]["reply"].is_null());
    assert!(!directory.path().join("origin-response-1.jsonl").exists());
}
