//! One admitted attempt's native bytes and collection observations.
//! No rollout lookup, caller-selected source path, or history normalization.
use crate::{Context, Error, Result, Value, json, s};
use serde::Serialize;
use std::{
    fs::{self, File, OpenOptions}, io::{self, Read, Write, Seek, SeekFrom},
    path::{Path, PathBuf}, sync::{Arc, Mutex},
};

const DOMAIN: &str = "owned-native-export/1";
const PURPOSE: &str = "p33-skill-business-v11";
const MAX_STREAM: u64 = 32 << 20;
const MAX_INPUT: u64 = 2 << 20;
const MAX_FRAME: usize = 2 << 20;
const MAX_RECEIPT: u64 = 128 << 10;
const MAX_BINDING: u64 = MAX_STREAM;
const MAX_RPC_PAGES: usize = 64;
pub(crate) const NATIVE_ATTEMPT_BUDGET_SECS:f64=600.;

// Avoid requiring serde's optional proc-macro feature in the existing crate.
macro_rules! export_fields {
    ($ty:ty,$($field:ident),+ $(,)?)=>{
        impl Serialize for $ty {
            fn serialize<S:serde::Serializer>(&self,serializer:S)->std::result::Result<S::Ok,S::Error>{
                use serde::ser::SerializeMap;
                let mut object=serializer.serialize_map(None)?;
                $(object.serialize_entry(stringify!($field),&self.$field)?;)+
                object.end()
            }
        }
    }
}


fn rejected() -> io::Error { io::Error::other("owned_native_export_rejected") }
fn id_ok(s:&str)->bool { !s.is_empty() && s.len()<=160 && s.bytes().all(|b|b.is_ascii_alphanumeric()||b==b'-'||b==b'_') }


#[derive(Debug)]
struct JsonBudgetExceeded {limit:u64}
impl std::fmt::Display for JsonBudgetExceeded {
    fn fmt(&self,f:&mut std::fmt::Formatter<'_>)->std::fmt::Result{write!(f,"owned_native_json_budget_exceeded(limit={})",self.limit)}
}
impl std::error::Error for JsonBudgetExceeded {}

// Producer and consumer share the same byte budgets. Oversize JSON is rejected
// before CreateNew, not written as a receipt its own consumer cannot read.
fn bounded_json<T:Serialize>(value:&T,limit:u64)->io::Result<Vec<u8>> {
    struct Bounded {bytes:Vec<u8>,limit:usize,exceeded:bool}
    impl Write for Bounded {
        fn write(&mut self,raw:&[u8])->io::Result<usize>{
            if self.bytes.len().checked_add(raw.len()).is_none_or(|n|n>self.limit){self.exceeded=true;return Err(rejected());}
            self.bytes.extend_from_slice(raw);Ok(raw.len())
        }
        fn flush(&mut self)->io::Result<()>{Ok(())}
    }
    let mut writer=Bounded{bytes:Vec::new(),limit:usize::try_from(limit).map_err(|_|rejected())?,exceeded:false};
    if serde_json::to_writer(&mut writer,value).is_err(){
        if writer.exceeded{return Err(io::Error::other(JsonBudgetExceeded{limit}));}
        return Err(rejected());
    }
    Ok(writer.bytes)
}
fn save_json<T:Serialize>(path:&Path,value:&T,limit:u64)->io::Result<FileFact> {
    let bytes=bounded_json(value,limit)?;
    save_new_bounded(path,&bytes,limit)
}


fn strict_value(raw:&[u8])->io::Result<Value> {
    use serde::{Deserialize,Deserializer,de::{self,Visitor,SeqAccess,MapAccess}};
    struct Strict(Value);
    struct StrictVisitor;
    impl<'de> Visitor<'de> for StrictVisitor {
        type Value=Strict;
        fn expecting(&self,f:&mut std::fmt::Formatter<'_>)->std::fmt::Result{f.write_str("one unambiguous JSON value")}
        fn visit_bool<E:de::Error>(self,v:bool)->std::result::Result<Strict,E>{Ok(Strict(Value::Bool(v)))}
        fn visit_i64<E:de::Error>(self,v:i64)->std::result::Result<Strict,E>{Ok(Strict(Value::Number(v.into())))}
        fn visit_u64<E:de::Error>(self,v:u64)->std::result::Result<Strict,E>{Ok(Strict(Value::Number(v.into())))}
        fn visit_f64<E:de::Error>(self,v:f64)->std::result::Result<Strict,E>{serde_json::Number::from_f64(v).map(|v|Strict(Value::Number(v))).ok_or_else(||E::custom("nonfinite"))}
        fn visit_str<E:de::Error>(self,v:&str)->std::result::Result<Strict,E>{Ok(Strict(Value::String(v.into())))}
        fn visit_string<E:de::Error>(self,v:String)->std::result::Result<Strict,E>{Ok(Strict(Value::String(v)))}
        fn visit_unit<E:de::Error>(self)->std::result::Result<Strict,E>{Ok(Strict(Value::Null))}
        fn visit_none<E:de::Error>(self)->std::result::Result<Strict,E>{Ok(Strict(Value::Null))}
        fn visit_seq<A:SeqAccess<'de>>(self,mut input:A)->std::result::Result<Strict,A::Error>{
            let mut values=Vec::new();while let Some(Strict(v))=input.next_element()?{values.push(v);}Ok(Strict(Value::Array(values)))
        }
        fn visit_map<A:MapAccess<'de>>(self,mut input:A)->std::result::Result<Strict,A::Error>{
            let mut values=serde_json::Map::new();let mut seen=std::collections::HashSet::new();
            while let Some(key)=input.next_key::<String>()? {
                if !seen.insert(key.to_ascii_lowercase()){return Err(de::Error::custom("duplicate field"));}
                let Strict(value)=input.next_value()?;values.insert(key,value);
            }
            Ok(Strict(Value::Object(values)))
        }
    }
    impl<'de> Deserialize<'de> for Strict {
        fn deserialize<D:Deserializer<'de>>(input:D)->std::result::Result<Self,D::Error>{input.deserialize_any(StrictVisitor)}
    }
    if raw.is_empty()||raw.len()>MAX_STREAM as usize{return Err(rejected());}
    let mut input=serde_json::Deserializer::from_slice(raw);
    let Strict(value)=Strict::deserialize(&mut input).map_err(|_|rejected())?;
    input.end().map_err(|_|rejected())?;
    Ok(value)
}

fn rpc_result(raw:&[u8],id:u64)->io::Result<Value> {
    let value=strict_value(raw)?;
    let object=value.as_object().ok_or_else(rejected)?;
    if object.keys().any(|k|!matches!(k.as_str(),"id"|"result"|"error"))||
        object.get("id").and_then(Value::as_u64)!=Some(id)||
        object.get("error").is_some_and(|v|!v.is_null())||
        !object.get("result").is_some_and(Value::is_object){return Err(rejected());}
    Ok(value["result"].clone())
}
fn page_cursor(page:&Value)->io::Result<Option<String>> {
    match page.as_object().and_then(|v|v.get("nextCursor")) {
        Some(Value::Null)=>Ok(None),
        Some(Value::String(value)) if !value.is_empty()&&value.len()<4096=>Ok(Some(value.clone())),
        _=>Err(rejected()),
    }
}

fn qualified_source(context:&Context,path:&Path)->io::Result<()> {
    let reply=context.rt.call(crate::Call::new(vec!["D:/CC/Tools/client-records/scrub391.exe".into(),"--guard-path".into(),
        path.to_str().ok_or_else(rejected)?.into()],vec![],5.))?;
    if reply.code!=0{return Err(rejected());}
    let v=strict_value(&reply.stdout)?;
    if v["ok"]!=true||v["access_allowed"]!=true||v["classification_unverified"]!=false||
        v["client_resource"]!=false||v["protection_status"]!="clear" {return Err(rejected());}
    Ok(())
}


#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Observed { Confirmed, Failed, Unknown }
impl Serialize for Observed {
    fn serialize<S:serde::Serializer>(&self,serializer:S)->std::result::Result<S::Ok,S::Error>{
        serializer.serialize_str(match self{Self::Confirmed=>"confirmed",Self::Failed=>"failed",Self::Unknown=>"unknown"})
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Birth { pid:u32, created:u64 }
#[derive(Clone, Debug, PartialEq, Eq)]
struct FileIdentity { volume:u32, index:u64, created:u64, size:u64 }
#[derive(Clone, Debug)]
struct FileFact { name:String, bytes:u64, sha256:String, identity:FileIdentity }
#[derive(Clone, Debug)]
struct StreamState { bytes:u64, eof:Observed, persisted:Observed, overflow:bool, seal:Option<FileFact>,flushed:Observed,writer_closed:Observed }
impl Default for StreamState {
    fn default()->Self {Self{bytes:0,eof:Observed::Unknown,persisted:Observed::Confirmed,overflow:false,seal:None,flushed:Observed::Unknown,writer_closed:Observed::Unknown}}
}
#[derive(Clone, Debug)]
struct NativeRef { record:u64, pointer:String, family:&'static str, native_id:Option<String> }
#[derive(Clone, Debug, Default)]
struct NativeBinding {
    thread_id:Option<String>, turn_ids:Vec<String>, completed_turn_ids:Vec<String>,
    turn_completed_without_id:bool, malformed:bool, item_ids:Vec<String>,
    records:u64,session_ref:Option<NativeRef>,item_refs:Vec<NativeRef>,turn_refs:Vec<NativeRef>,finish_refs:Vec<NativeRef>,
    tool_started_refs:Vec<NativeRef>,tool_completed_refs:Vec<NativeRef>,assistant_message_refs:Vec<NativeRef>,unsupported_item_types:Vec<String>,
}
export_fields!(Birth,pid,created);
export_fields!(FileIdentity,volume,index,created,size);
export_fields!(FileFact,name,bytes,sha256,identity);
export_fields!(StreamState,bytes,eof,persisted,overflow,seal,flushed,writer_closed);
export_fields!(NativeRef,record,pointer,family,native_id);
export_fields!(NativeBinding,thread_id,turn_ids,completed_turn_ids,turn_completed_without_id,malformed,item_ids,records,session_ref,item_refs,turn_refs,finish_refs,tool_started_refs,tool_completed_refs,assistant_message_refs,unsupported_item_types);
impl NativeBinding {
    fn observe(&mut self,line:&[u8]) {
        self.records+=1;
        let Ok(v)=strict_value(line) else {self.malformed=true;return;};
        match v.get("type").and_then(Value::as_str) {
            Some("thread.started")=>{
                let Some(id)=v.get("thread_id").and_then(Value::as_str).filter(|x|id_ok(x)) else {self.malformed=true;return;};
                if self.thread_id.as_deref().is_some_and(|old|old!=id){self.malformed=true;}else{self.thread_id=Some(id.into());self.session_ref=Some(NativeRef{record:self.records,pointer:"/thread_id".into(),family:"codex-exec-json",native_id:Some(id.into())});}
            }
            Some("turn.started")|Some("turn.completed")=>{
                let turn=match v.get("turn_id") {
                    None=>None,
                    Some(Value::String(id)) if id_ok(id)=>Some(id.as_str()),
                    Some(_)=>{self.malformed=true;return;}
                };
                if let Some(id)=turn {
                    if v["type"]=="turn.started" {
                        if self.turn_ids.iter().any(|old|old==id){self.malformed=true;}else{self.turn_ids.push(id.into());self.turn_refs.push(NativeRef{record:self.records,pointer:"/turn_id".into(),family:"codex-exec-json",native_id:Some(id.into())});}
                    } else {self.completed_turn_ids.push(id.into());self.finish_refs.push(NativeRef{record:self.records,pointer:"/turn_id".into(),family:"codex-exec-json",native_id:Some(id.into())});}
                } else if v["type"]=="turn.completed" {self.turn_completed_without_id=true;self.finish_refs.push(NativeRef{record:self.records,pointer:"".into(),family:"codex-exec-json",native_id:None});}
            }
            Some("item.started")|Some("item.completed")=>{
                let item=&v["item"];
                let Some(id)=item["id"].as_str().filter(|x|id_ok(x)) else {self.malformed=true;return;};
                let reference=NativeRef{record:self.records,pointer:"/item".into(),family:"codex-exec-json",native_id:Some(id.into())};
                if v["type"]=="item.completed"&&!self.item_ids.iter().any(|x|x==id){self.item_ids.push(id.into());self.item_refs.push(reference.clone());}
                match item["type"].as_str() {
                    Some("command_execution")|Some("mcp_tool_call")|Some("web_search")=>{
                        if v["type"]=="item.started"{self.tool_started_refs.push(reference);}else{self.tool_completed_refs.push(reference);}
                    }
                    Some("agent_message")=>{if v["type"]=="item.completed"{self.assistant_message_refs.push(reference);}}
                    Some(kind)=>{if !self.unsupported_item_types.iter().any(|old|old==kind){self.unsupported_item_types.push(kind.into());}}
                    None=>self.malformed=true,
                }
            }
            _=>{}
        }
    }
}


#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
struct CloseState { attempted:bool,failed:bool }
// Every acquired resource is wrapped immediately. Taking the value before the
// finalizer makes normal close, early return, unwind and Drop one close attempt.
struct CloseOnce<T> {
    value:Option<T>,finalize:fn(T)->io::Result<()>,state:Arc<Mutex<CloseState>>,
}
impl<T> CloseOnce<T> {
    fn new(value:T,finalize:fn(T)->io::Result<()>)->Self {
        Self{value:Some(value),finalize,state:Arc::new(Mutex::new(CloseState::default()))}
    }
    fn get(&self)->io::Result<&T>{self.value.as_ref().ok_or_else(rejected)}
    fn get_mut(&mut self)->io::Result<&mut T>{self.value.as_mut().ok_or_else(rejected)}
    fn close(&mut self)->io::Result<()> {
        if let Some(value)=self.value.take() {
            self.state.lock().unwrap_or_else(|p|p.into_inner()).attempted=true;
            let result=(self.finalize)(value);
            if result.is_err(){self.state.lock().unwrap_or_else(|p|p.into_inner()).failed=true;}
            result
        } else if self.state.lock().unwrap_or_else(|p|p.into_inner()).failed {Err(rejected())}else{Ok(())}
    }
}
impl<T> Drop for CloseOnce<T> {fn drop(&mut self){let _=self.close();}}
#[derive(Debug)]
struct PrimaryAndCloseFailure {primary:io::Error,cleanup:io::Error}
impl std::fmt::Display for PrimaryAndCloseFailure {
    fn fmt(&self,f:&mut std::fmt::Formatter<'_>)->std::fmt::Result {
        write!(f,"owned_native_export_primary_and_close_failed(primary={:?},cleanup={:?})",self.primary.kind(),self.cleanup.kind())
    }
}
impl std::error::Error for PrimaryAndCloseFailure {
    fn source(&self)->Option<&(dyn std::error::Error+'static)>{Some(&self.primary)}
}

#[derive(Debug)]
struct ConstructorCleanupFailure {primary:io::Error,retained:usize}
impl std::fmt::Display for ConstructorCleanupFailure {
    fn fmt(&self,f:&mut std::fmt::Formatter<'_>)->std::fmt::Result{
        write!(f,"owned_native_export_constructor_and_close_failed(primary={:?},retained={})",self.primary.kind(),self.retained)
    }
}
impl std::error::Error for ConstructorCleanupFailure {
    fn source(&self)->Option<&(dyn std::error::Error+'static)>{Some(&self.primary)}
}


fn include_retained_cleanup<T>(before:usize,primary:io::Result<T>)->io::Result<T> {
    match primary {
        Err(primary) if native::retained_close_count()>before=>Err(io::Error::other(ConstructorCleanupFailure{
            primary,retained:native::retained_close_count()-before})),
        other=>other,
    }
}

fn finish_owned<T,R>(resource:&mut CloseOnce<T>,primary:io::Result<R>)->io::Result<R> {
    match (primary,resource.close()) {
        (Ok(value),Ok(()))=>Ok(value),(Err(error),Ok(()))=>Err(error),(Ok(_),Err(error))=>Err(error),
        (Err(primary),Err(cleanup))=>Err(io::Error::other(PrimaryAndCloseFailure{primary,cleanup})),
    }
}

fn new_file(path:&Path)->io::Result<CloseOnce<File>> {
    let mut options=OpenOptions::new(); options.read(true).write(true).create_new(true);
    #[cfg(windows)] { use std::os::windows::fs::OpenOptionsExt; options.share_mode(1).custom_flags(0x0020_0000); }
    options.open(path).map(|file|CloseOnce::new(file,native::close_file))
}
fn read_owned<T:Read>(mut file:CloseOnce<T>,limit:u64,name:String,
    identity:fn(&T)->io::Result<FileIdentity>,hash:fn(&[u8])->io::Result<String>)->io::Result<(Vec<u8>,FileFact)> {
    let primary=(||->io::Result<_>{
        let before=identity(file.get()?)?;
        if before.size>limit{return Err(rejected());}
        let mut bytes=Vec::new();
        Read::by_ref(file.get_mut()?).take(limit+1).read_to_end(&mut bytes)?;
        let after=identity(file.get()?)?;
        let digest=hash(&bytes)?;
        if before!=after||bytes.len() as u64!=before.size{return Err(rejected());}
        Ok((bytes,FileFact{name,bytes:before.size,sha256:digest,identity:before}))
    })();
    finish_owned(&mut file,primary)
}
fn stable_read(path:&Path,limit:u64)->io::Result<(Vec<u8>,FileFact)> {
    let name=path.file_name().and_then(|s|s.to_str()).ok_or_else(rejected)?.to_string();
    let mut options=OpenOptions::new();options.read(true);
    #[cfg(windows)] {use std::os::windows::fs::OpenOptionsExt;options.share_mode(1).custom_flags(0x0020_0000);}
    let file=CloseOnce::new(options.open(path)?,native::close_file);
    read_owned(file,limit,name,native::file_identity,native::sha256)
}
fn save_new(path:&Path,bytes:&[u8])->io::Result<FileFact>{save_new_bounded(path,bytes,MAX_STREAM)}
fn save_new_bounded(path:&Path,bytes:&[u8],limit:u64)->io::Result<FileFact> {
    if bytes.len() as u64>limit{return Err(rejected());}
    let mut file=new_file(path)?;
    let primary=(||->io::Result<FileIdentity>{
        file.get_mut()?.write_all(bytes)?;
        file.get()?.sync_all()?;
        native::file_identity(file.get()?)
    })();
    let identity=finish_owned(&mut file,primary)?;
    let (read,fact)=stable_read(path,limit)?;
    if read!=bytes||fact.identity!=identity{return Err(rejected());}
    Ok(fact)
}


#[cfg(test)]
fn load_json_fact(path:&Path,expected:&Value,limit:u64)->io::Result<Value> {
    let (raw,fact)=stable_read(path,limit)?;
    if serde_json::to_value(fact).map_err(|_|rejected())?!=*expected{return Err(rejected());}
    strict_value(&raw)
}

fn sealed_stream_fact(path:&Path,state:&StreamState,limit:u64)->io::Result<FileFact> {
    let expected=state.seal.as_ref().ok_or_else(rejected)?;
    let (_,current)=stable_read(path,limit)?;
    if current.name!=expected.name||current.bytes!=expected.bytes||current.sha256!=expected.sha256||current.identity!=expected.identity {
        return Err(rejected());
    }
    Ok(expected.clone())
}

fn streams_complete(input:&StreamState,expected_input:u64,stdout:&StreamState,stderr:&StreamState,
    preservation_ok:bool,retained:usize)->bool {
    input.bytes==expected_input&&input.flushed==Observed::Confirmed&&input.writer_closed==Observed::Confirmed&&input.persisted==Observed::Confirmed&&
    stdout.eof==Observed::Confirmed&&stderr.eof==Observed::Confirmed&&
    stdout.persisted==Observed::Confirmed&&stderr.persisted==Observed::Confirmed&&
    !stdout.overflow&&!stderr.overflow&&preservation_ok&&retained==0
}

// Frozen input is used by the normal spawn's stdin writer. Observing a prompt
// file later is not a substitute for writing these exact bytes to that child.
pub(crate) struct FrozenInput { bytes:Arc<[u8]>,state:Arc<Mutex<StreamState>> }
impl FrozenInput {
    pub(crate) fn write_to(self,mut stdin:impl Write)->io::Result<()> {
        let mut sent=0;
        while sent<self.bytes.len() {
            match stdin.write(&self.bytes[sent..]) {
                Ok(0)=>{self.state.lock().map_err(|_|rejected())?.persisted=Observed::Failed;return Err(rejected());}
                Ok(n)=>{sent+=n;self.state.lock().map_err(|_|rejected())?.bytes=sent as u64;}
                Err(e)=>{self.state.lock().map_err(|_|rejected())?.persisted=Observed::Failed;return Err(e);}
            }
        }
        if let Err(e)=stdin.flush(){self.state.lock().map_err(|_|rejected())?.persisted=Observed::Failed;return Err(e);}
        self.state.lock().map_err(|_|rejected())?.flushed=Observed::Confirmed;
        // Generic Write/flush proves bytes sent, not closure of the owned
        // child stdin pipe. Its EOF remains unknown until a native writer owns it.
        Ok(())
    }
    // Only this route takes stdin from the exact actual Child. Its local handle
    // Close is not relabelled as peer-observed EOF; that stays unknown.
    #[cfg(all(windows,test))]
    fn write_to_child(self,child:&mut std::process::Child,processes:&native::Processes)->io::Result<()> {
        use std::os::windows::io::AsHandle;
        if !processes.same_child(child.as_handle()){return Err(rejected());}
        let stdin=child.stdin.take().ok_or_else(rejected)?;
        let mut owner=CloseOnce::new(stdin,native::close_child_stdin);
        let state=self.state.clone();
        let primary=owner.get_mut().and_then(|stdin|self.write_to(stdin));
        let result=finish_owned(&mut owner,primary);
        let close=*owner.state.lock().unwrap_or_else(|p|p.into_inner());
        state.lock().map_err(|_|rejected())?.writer_closed=if close.attempted&&!close.failed{Observed::Confirmed}else{Observed::Failed};
        result
    }

}


fn frozen_saved_input(raw:&[u8],saved:&FileFact)->io::Result<FrozenInput> {
    if saved.name!="prompt.as-sent.utf8"||saved.bytes!=raw.len() as u64||saved.sha256!=native::sha256(raw)? {return Err(rejected());}
    let state=StreamState{persisted:Observed::Confirmed,..StreamState::default()};
    Ok(FrozenInput{bytes:Arc::from(raw),state:Arc::new(Mutex::new(state))})
}

// Not Deserialize: constructed only from the actual owned process handles and
// the state-locked normal job. There is no public constructor accepting a hash.

#[derive(Clone, Debug)]
struct WaitFact {called:bool,reaped:Observed,exit_code:Option<i64>}
export_fields!(WaitFact,called,reaped,exit_code);

pub(crate) struct Capture {
    job_id:String,generation:String,directory:PathBuf,root:Birth,child:Birth,fresh_launch:bool,
    processes:native::Processes,prompt:FileFact,wait:WaitFact,
    input:Arc<Mutex<StreamState>>,stdout:Arc<Mutex<StreamState>>,stderr:Arc<Mutex<StreamState>>,
    binding:Arc<Mutex<NativeBinding>>,
}
pub(crate) struct FinishedExport { job_id:String,generation:String,root:Birth,child:Birth,directory:String,receipt:FileFact,collection:FileFact }

#[cfg(windows)]
pub(crate) struct OwnedInputWriter {input:FrozenInput,stdin:CloseOnce<std::process::ChildStdin>}
#[cfg(windows)]
impl OwnedInputWriter {
    pub(crate) fn write(mut self)->io::Result<()> {
        let state=self.input.state.clone();
        let primary=self.stdin.get_mut().and_then(|stdin|self.input.write_to(stdin));
        let result=finish_owned(&mut self.stdin,primary);
        let close=*self.stdin.state.lock().unwrap_or_else(|p|p.into_inner());
        state.lock().map_err(|_|rejected())?.writer_closed=if close.attempted&&!close.failed{Observed::Confirmed}else{Observed::Failed};
        result
    }
}

impl Capture {
    #[cfg(windows)]
    pub(crate) fn resume_owned_child(&self,child:&std::process::Child)->io::Result<()> {
        use std::os::windows::io::AsHandle;
        if !self.processes.same_child(child.as_handle()){return Err(rejected());}
        native::resume_child(child)
    }
    #[cfg(windows)]
    pub(crate) fn owned_input_writer(&self,input:FrozenInput,child:&mut std::process::Child)->io::Result<OwnedInputWriter> {
        use std::os::windows::io::AsHandle;
        if !Arc::ptr_eq(&self.input,&input.state)||!self.processes.same_child(child.as_handle()){return Err(rejected());}
        let stdin=child.stdin.take().ok_or_else(rejected)?;
        Ok(OwnedInputWriter{input,stdin:CloseOnce::new(stdin,native::close_child_stdin)})
    }
    #[cfg(windows)]
    pub(crate) fn observe_child_wait(&mut self,child:&mut std::process::Child)->io::Result<Option<i64>> {
        use std::os::windows::io::AsHandle;
        if !self.processes.same_child(child.as_handle()) {self.wait.reaped=Observed::Failed;return Err(rejected());}
        self.wait.called=true;
        match child.try_wait() {
            Ok(Some(status))=>{let code=status.code().map(|c|c as u32 as i64);self.wait.reaped=Observed::Confirmed;self.wait.exit_code=code;Ok(code)}
            Ok(None)=>{self.wait.reaped=Observed::Failed;Ok(None)}
            Err(e)=>{self.wait.reaped=Observed::Unknown;Err(e)}
        }
    }
    pub(crate) fn stdout<R:Read>(&self,input:R)->io::Result<OriginalStream<R>> {
        OriginalStream::new(input,&self.directory.join("stdout.native.jsonl"),self.stdout.clone(),Some(self.binding.clone()))
    }
    pub(crate) fn stderr<R:Read>(&self,input:R)->io::Result<OriginalStream<R>> {
        OriginalStream::new(input,&self.directory.join("stderr.native.txt"),self.stderr.clone(),None)
    }

    #[cfg(windows)]
    pub(crate) fn reclaim_owned_children(&self,deadline:std::time::Instant)->io::Result<()> {
        self.processes.reclaim_children(deadline)
    }
    #[cfg(windows)]
    pub(crate) fn owned_children_collected(&self)->bool {
        self.processes.children_collected()
    }

    pub(crate) fn finish(self)->io::Result<FinishedExport> {
        let before=native::retained_close_count();
        let directory=self.directory.clone();let job=self.job_id.clone();let generation=self.generation.clone();
        let primary=self.finish_body();
        match primary {
            Ok(result)=>Ok(result),
            Err(primary)=>{
                let failed=json!({"domain":DOMAIN,"purpose":PURPOSE,"job_id":job,"started_at":generation,
                    "complete":false,"phase":"finish","json_budget_exceeded":primary.get_ref().is_some_and(|e|e.is::<JsonBudgetExceeded>()),
                    "retained_close_failures":native::retained_close_count(),"registry_published":false});
                let saved=save_json(&directory.join("finish-failure.private.json"),&failed,MAX_RECEIPT);
                let retained=native::retained_close_count().saturating_sub(before);
                if retained>0{return Err(io::Error::other(ConstructorCleanupFailure{primary,retained}));}
                if let Err(cleanup)=saved{return Err(io::Error::other(PrimaryAndCloseFailure{primary,cleanup}));}
                Err(primary)
            }
        }
    }
    fn finish_body(self)->io::Result<FinishedExport> {
        let input=self.input.lock().map_err(|_|rejected())?.clone();
        let stdout=self.stdout.lock().map_err(|_|rejected())?.clone();
        let stderr=self.stderr.lock().map_err(|_|rejected())?.clone();
        let binding=self.binding.lock().map_err(|_|rejected())?.clone();
        let binding_fact=save_json(&self.directory.join("binding.private.json"),&binding,MAX_BINDING)?;
        let collection=json!({"os_observation":self.processes.finish(),"std_child_wait":self.wait});
        let mut files=Vec::new();
        let mut preservation_errors=Vec::new();
        for (name,state) in [("stdout.native.jsonl",&stdout),("stderr.native.txt",&stderr)] {
            match sealed_stream_fact(&self.directory.join(name),state,MAX_STREAM) {
                Ok(fact)=>files.push(fact),Err(_)=>preservation_errors.push(name),
            }
        }
        let original_complete=streams_complete(&input,self.prompt.bytes,&stdout,&stderr,preservation_errors.is_empty(),native::retained_close_count());
        let receipt=json!({"domain":DOMAIN,"purpose":PURPOSE,"job_id":self.job_id,"started_at":self.generation,
            "root":self.root,"child":self.child,"fresh_launch":self.fresh_launch,"prompt":self.prompt,"files":files,
            "input":input,"stdout":stdout,"stderr":stderr,"binding":binding_fact,"collection":collection,
            "original_complete":original_complete,"preservation_errors":preservation_errors,"retained_close_failures":native::retained_close_count(),
            "native_refs_base":1,"prompt_ref":{"source":"prompt.as-sent.utf8","bytes":self.prompt.bytes,"sha256":self.prompt.sha256},
            "scope":{"native_stdout":"original bytes before old redaction/stamping",
                "prompt":"same frozen stdin bytes with actual write count; not all delivered instructions",
                "commands_results":"only native events actually emitted","model":"unknown unless native metadata exposes it",
                "effort":"unknown unless native metadata exposes it","instructions":"not exposed by exec stdout",
                "private_history_read":false,"scorer_v11_complete":false}});
        let collection=save_json(&self.directory.join("collection.private.json"),&collection,MAX_RECEIPT)?;
        let receipt=save_json(&self.directory.join("export.private.json"),&receipt,MAX_RECEIPT)?;
        Ok(FinishedExport{job_id:self.job_id,generation:self.generation,root:self.root,child:self.child,
            directory:self.directory.file_name().and_then(|s|s.to_str()).ok_or_else(rejected)?.into(),receipt,collection})
    }
}

pub(crate) struct OriginalStream<R> {
    input:R,file:Option<CloseOnce<File>>,state:Arc<Mutex<StreamState>>,binding:Option<Arc<Mutex<NativeBinding>>>,pending:Vec<u8>,
    initial:FileIdentity,name:String,digest:Option<native::StreamHash>,
}

fn same_file_object(before:&FileIdentity,after:&FileIdentity)->bool {
    before.volume==after.volume&&before.index==after.index&&before.created==after.created
}
impl<R> OriginalStream<R> {
    fn new(input:R,path:&Path,state:Arc<Mutex<StreamState>>,binding:Option<Arc<Mutex<NativeBinding>>>)->io::Result<Self> {
        let before=native::retained_close_count();
        let primary=(||->io::Result<Self>{
        let file=new_file(path)?;
        let initial=native::file_identity(file.get()?)?;
        if initial.size!=0{return Err(rejected());}
        let name=path.file_name().and_then(|s|s.to_str()).ok_or_else(rejected)?.to_string();
        let digest=native::StreamHash::new()?;
        Ok(Self{input,file:Some(file),state,binding,pending:Vec::new(),initial,name,digest:Some(digest)})
        })();
        include_retained_cleanup(before,primary)
    }
    fn seal(&mut self,bytes:u64)->io::Result<FileFact> {
        let digest=self.digest.take().ok_or_else(rejected)?.finish()?;
        let file=self.file.as_mut().ok_or_else(rejected)?;
        file.get()?.sync_all()?;
        let before=native::file_identity(file.get()?)?;
        if !same_file_object(&self.initial,&before)||before.size!=bytes||bytes>MAX_STREAM{return Err(rejected());}
        file.get_mut()?.seek(SeekFrom::Start(0))?;
        let mut raw=Vec::new();Read::by_ref(file.get_mut()?).take(MAX_STREAM+1).read_to_end(&mut raw)?;
        let after=native::file_identity(file.get()?)?;
        if before!=after||raw.len() as u64!=bytes||native::sha256(&raw)?!=digest{return Err(rejected());}
        Ok(FileFact{name:self.name.clone(),bytes,sha256:digest,identity:before})
    }
}

impl<R:Read> Read for OriginalStream<R> {
    fn read(&mut self,buf:&mut [u8])->io::Result<usize> {
        if buf.is_empty(){return Ok(0);}
        {
            let state=self.state.lock().map_err(|_|rejected())?;
            if state.eof==Observed::Failed||state.persisted==Observed::Failed||state.overflow{return Err(rejected());}
            if state.eof==Observed::Confirmed{return Ok(0);}
        }
        let n=loop {
            match self.input.read(buf) {
                Ok(n)=>break n,
                Err(e) if e.kind()==io::ErrorKind::Interrupted=>continue,
                Err(e)=>{self.state.lock().map_err(|_|rejected())?.eof=Observed::Failed;return Err(e);}
            }
        };
        if n==0 {
            let state_handle=self.state.clone();
            let mut state=state_handle.lock().map_err(|_|rejected())?;
            let seal=self.seal(state.bytes);
            if let Some(mut file)=self.file.take() {
                match finish_owned(&mut file,seal) {
                    Ok(fact)=>state.seal=Some(fact),
                    Err(error)=>{state.persisted=Observed::Failed;state.eof=Observed::Failed;return Err(error);}
                }
            }else{state.persisted=Observed::Failed;return Err(rejected());}
            if state.eof!=Observed::Failed{state.eof=Observed::Confirmed;}
            if let Some(binding)=&self.binding&&!self.pending.is_empty(){binding.lock().map_err(|_|rejected())?.observe(&self.pending);self.pending.clear();}
            return Ok(0);
        }
        let mut state=self.state.lock().map_err(|_|rejected())?;
        if state.bytes.checked_add(n as u64).is_none_or(|v|v>MAX_STREAM){state.overflow=true;state.persisted=Observed::Failed;return Err(rejected());}
        let Some(file)=self.file.as_mut() else {state.persisted=Observed::Failed;return Err(rejected());};
        if let Err(e)=file.get_mut().and_then(|f|f.write_all(&buf[..n])){state.persisted=Observed::Failed;return Err(e);}
        state.bytes+=n as u64;
        if self.digest.as_mut().ok_or_else(rejected)?.update(&buf[..n]).is_err(){state.persisted=Observed::Failed;return Err(rejected());}
        if let Some(binding)=&self.binding {
            self.pending.extend_from_slice(&buf[..n]);
            while let Some(at)=self.pending.iter().position(|x|*x==b'\n') {
                if at>MAX_FRAME {state.persisted=Observed::Failed;return Err(rejected());}
                let line:Vec<_>=self.pending.drain(..=at).collect();binding.lock().map_err(|_|rejected())?.observe(&line);
            }
            if self.pending.len()>MAX_FRAME{state.persisted=Observed::Failed;return Err(rejected());}
        }
        Ok(n)
    }
}
impl<R> Drop for OriginalStream<R> {
    fn drop(&mut self) {
        if let Some(file)=self.file.take() {
            let mut file=file;
            let sync=file.get().and_then(File::sync_all);
            if finish_owned(&mut file,sync).is_err(){if let Ok(mut s)=self.state.lock(){s.persisted=Observed::Failed;}}
            // Dropping before EOF leaves eof=unknown, even if the child exited0.
        }
    }
}


// A fresh native thread may legitimately omit turn IDs from exec events. Only
// a producer-marked fresh launch may resolve one unique turn from that exact
// thread's normal API entries. Resumed launches retain unknown without an ID.
#[derive(Clone)]
pub(crate) struct ExactThread {job_id:String,generation:String,thread_id:String,fresh_launch:bool,native_item_ids:Vec<String>}

#[derive(Clone)]
pub(crate) struct ExactSession {job_id:String,generation:String,thread_id:String,turn_id:String,required_items:Vec<String>}
pub(crate) struct SelectedExport {directory:PathBuf,receipt:Value,collection:Value,binding:Value}
impl SelectedExport {
    pub(crate) fn metadata(&self)->&Value {&self.receipt}
    pub(crate) fn collection(&self)->&Value {&self.collection}
    pub(crate) fn thread(&self)->io::Result<ExactThread> {
        let binding=&self.binding;
        let thread=binding["thread_id"].as_str().filter(|s|id_ok(s)).ok_or_else(rejected)?;
        if binding["malformed"]!=false{return Err(rejected());}
        let ids=binding["item_ids"].as_array().ok_or_else(rejected)?.iter()
            .map(|id|id.as_str().filter(|x|id_ok(x)).map(str::to_string).ok_or_else(rejected)).collect::<io::Result<Vec<_>>>()?;
        Ok(ExactThread{job_id:s(&self.receipt,"job_id").into(),generation:s(&self.receipt,"started_at").into(),
            thread_id:thread.into(),fresh_launch:self.receipt["fresh_launch"]==true,native_item_ids:ids})
    }
    pub(crate) fn session(&self,turn:&str)->io::Result<ExactSession> {
        let binding=&self.binding;
        let thread=binding["thread_id"].as_str().filter(|s|id_ok(s)).ok_or_else(rejected)?;
        if !id_ok(turn)||binding["malformed"]!=false||
            !binding["turn_ids"].as_array().is_some_and(|a|a.iter().any(|id|id==turn)) {return Err(rejected());}
        Ok(ExactSession{job_id:s(&self.receipt,"job_id").into(),generation:s(&self.receipt,"started_at").into(),thread_id:thread.into(),turn_id:turn.into(),required_items:self.thread()?.native_item_ids})
    }
    pub(crate) fn read_original(&self,context:&Context,name:&str)->io::Result<Vec<u8>> {
        let list=if name=="prompt.as-sent.utf8" {vec![&self.receipt["prompt"]]}else{
            self.receipt["files"].as_array().map(|v|v.iter().collect()).ok_or_else(rejected)?
        };
        let expected=list.into_iter().find(|f|f["name"]==name).ok_or_else(rejected)?;
        if !matches!(name,"prompt.as-sent.utf8"|"stdout.native.jsonl"|"stderr.native.txt"){return Err(rejected());}
        qualified_source(context,&self.directory.join(name))?;
        let (raw,fact)=stable_read(&self.directory.join(name),MAX_STREAM)?;
        if serde_json::to_value(fact).map_err(|_|rejected())?!=*expected{return Err(rejected());}
        Ok(raw)
    }
}

impl Context {
    #[cfg(windows)]
    pub(crate) fn begin_native_capture(&self,jid:&str,input:&[u8],argv:&[String],
        child:std::os::windows::io::BorrowedHandle<'_>,job_handle:Option<std::os::windows::io::BorrowedHandle<'_>>)
        ->Result<(Capture,FrozenInput)> {
        let retained_before=native::retained_close_count();
        let primary=(||->Result<(Capture,FrozenInput)>{
        if !crate::queue::valid_job_id(jid)||input.is_empty()||input.len() as u64>MAX_INPUT{return Err(Error::new("native capture input rejected"));}
        let _lock=self.job_lock(jid,"state.lock")?;
        let row=self.load(jid)?;
        if row["native_evidence_purpose"]!=PURPOSE||s(&row,"status")!="running"||s(&row,"id")!=jid||s(&row,"started_at").is_empty(){return Err(Error::new("native capture not admitted"));}
        let processes=native::Processes::pin(child,job_handle)?;
        let root=processes.root();let child=processes.child();
        if row["pid"]!=root.pid||row["pid_created"]!=root.created||row["codex_pid"]!=child.pid||row["codex_pid_created"]!=child.created{return Err(Error::new("native launch generation mismatch"));}
        let directory=self.job_dir(jid).join(format!("native-{}-{}",root.created,child.created));
        fs::create_dir(&directory)?;
        let prompt=save_new(&directory.join("prompt.as-sent.utf8"),input)?;
        let launch=json!({"domain":DOMAIN,"purpose":PURPOSE,"job_id":jid,"started_at":row["started_at"],
            "root":root,"child":child,"cwd":row["cwd"],"argv":argv,"requested_model":row["model"],"requested_effort":row["effort"],
            "actual_model":null,"actual_effort":null,"instruction_scope":"not exposed by this exec interface"});
        save_json(&directory.join("launch.private.json"),&launch,MAX_RECEIPT)?;
        let frozen=frozen_saved_input(input,&prompt)?;
        let state=frozen.state.clone();
        Ok((Capture{job_id:jid.into(),generation:s(&row,"started_at").into(),directory,root,child,fresh_launch:s(&row,"resume_session").is_empty(),processes,prompt,wait:WaitFact{called:false,reaped:Observed::Unknown,exit_code:None},
            input:state,stdout:Arc::new(Mutex::new(StreamState::default())),stderr:Arc::new(Mutex::new(StreamState::default())),
            binding:Arc::new(Mutex::new(NativeBinding::default()))},frozen))
        })();
        match primary {
            Err(mut error)=>{
                let retained=native::retained_close_count().saturating_sub(retained_before);
                if retained>0{error.message.push_str(&format!("; owned_native_capture_close_failed(retained={retained})"));}
                Err(error)
            }
            other=>other,
        }
    }
    pub(crate) fn publish_native_export(&self,finished:FinishedExport)->Result<()> {
        let _lock=self.job_lock(&finished.job_id,"state.lock")?;let mut row=self.load(&finished.job_id)?;
        if s(&row,"started_at")!=finished.generation||row["pid"]!=finished.root.pid||row["pid_created"]!=finished.root.created||
            row["codex_pid"]!=finished.child.pid||row["codex_pid_created"]!=finished.child.created{return Err(Error::new("native export generation changed"));}
        row["native_export"]=json!({"domain":DOMAIN,"purpose":PURPOSE,"directory":finished.directory,"job_id":finished.job_id,
            "started_at":finished.generation,"root":finished.root,"child":finished.child,"receipt":finished.receipt,"collection":finished.collection});
        self.save(&row)
    }
    pub(crate) fn select_native_export(&self,jid:&str)->Result<SelectedExport> {
        if !crate::queue::valid_job_id(jid){return Err(Error::new("invalid exact native export job"));}
        qualified_source(self,&self.job_dir(jid))?;
        qualified_source(self,&self.job_dir(jid).join("job.json"))?;
        let _lock=self.job_lock(jid,"state.lock")?;let row=self.load(jid)?;let reg=&row["native_export"];
        if row["native_evidence_purpose"]!=PURPOSE||reg["domain"]!=DOMAIN||reg["purpose"]!=PURPOSE||reg["job_id"]!=jid||
            reg["started_at"]!=row["started_at"]||reg["root"]["pid"]!=row["pid"]||reg["root"]["created"]!=row["pid_created"]||
            reg["child"]["pid"]!=row["codex_pid"]||reg["child"]["created"]!=row["codex_pid_created"]{return Err(Error::new("native export origin unavailable"));}
        let name=reg["directory"].as_str().filter(|s|s.starts_with("native-")&&id_ok(s)).ok_or_else(||Error::new("native export namespace unavailable"))?;
        let directory=self.job_dir(jid).join(name);
        Ok(read_export_artifacts(directory,reg,jid,|path|qualified_source(self,path))?)
    }
}


#[derive(Debug)]
struct ExportReadFailure {phase:&'static str,category:&'static str,primary:Option<io::Error>,binding_matches:Option<[bool;6]>}
impl std::fmt::Display for ExportReadFailure {
    fn fmt(&self,f:&mut std::fmt::Formatter<'_>)->std::fmt::Result {write!(f,"native_export_read_failed phase={} category={} binding_matches={:?}",self.phase,self.category,self.binding_matches)}
}
impl std::error::Error for ExportReadFailure {
    fn source(&self)->Option<&(dyn std::error::Error+'static)> {self.primary.as_ref().map(|error|error as &(dyn std::error::Error+'static))}
}
fn export_read_failure(phase:&'static str,category:&'static str,primary:Option<io::Error>)->io::Error {
    io::Error::other(ExportReadFailure{phase,category,primary,binding_matches:None})
}
fn load_export_component(path:&Path,expected:&Value,limit:u64,phase:&'static str)->io::Result<Value> {
    let (raw,fact)=stable_read(path,limit).map_err(|primary|export_read_failure(phase,"stable_read_failed",Some(primary)))?;
    let actual=serde_json::to_value(fact).map_err(|_|export_read_failure(phase,"fact_encoding_failed",None))?;
    if actual!=*expected {return Err(export_read_failure(phase,"registered_fact_mismatch",None));}
    strict_value(&raw).map_err(|primary|export_read_failure(phase,"strict_json_rejected",Some(primary)))
}

// Fixed booleans diagnose the rejected comparison without exporting identities,
// environment values or receipt contents. All six original checks remain exact.
fn export_attempt_binding_matches(receipt:&Value,reg:&Value,jid:&str)->[bool;6] {
    [receipt["domain"]==DOMAIN,receipt["purpose"]==PURPOSE,receipt["job_id"]==jid,
        receipt["started_at"]==reg["started_at"],receipt["root"]==reg["root"],receipt["child"]==reg["child"]]
}
fn read_export_artifacts(directory:PathBuf,reg:&Value,jid:&str,mut qualify:impl FnMut(&Path)->io::Result<()>)->io::Result<SelectedExport> {
qualify(&directory.join("export.private.json"))?;
let receipt=load_export_component(&directory.join("export.private.json"),&reg["receipt"],MAX_RECEIPT,"receipt")?;
let binding_matches=export_attempt_binding_matches(&receipt,reg,jid);
if !binding_matches.iter().all(|matches|*matches) {
    return Err(io::Error::other(ExportReadFailure{phase:"receipt",category:"attempt_binding_mismatch",primary:None,binding_matches:Some(binding_matches)}));
}
qualify(&directory.join("collection.private.json"))?;
let collection=load_export_component(&directory.join("collection.private.json"),&reg["collection"],MAX_RECEIPT,"collection")?;
if collection!=receipt["collection"]{return Err(export_read_failure("collection","receipt_collection_mismatch",None));}
qualify(&directory.join("binding.private.json"))?;
let binding=load_export_component(&directory.join("binding.private.json"),&receipt["binding"],MAX_BINDING,"binding")?;
Ok(SelectedExport{directory,receipt,collection,binding})
}



pub(crate) struct ProgressOwner {
    progress:crate::notify::TaskProgress,required:bool,closed:bool,failed:bool,
    report:Option<(PathBuf,Value)>,reported:bool,
}
impl ProgressOwner {
    pub(crate) fn new(progress:crate::notify::TaskProgress,required:bool)->Self {
        Self{progress,required,closed:false,failed:false,report:None,reported:false}
    }
    pub(crate) fn bind_job(&mut self,directory:&Path,job:&Value) {
        self.report=Some((directory.join("notification-collection.private.json"),json!({"job_id":job["id"],"started_at":job["started_at"],"root_pid":job["pid"],"root_created":job["pid_created"]})));
    }
    pub(crate) fn close(&mut self)->bool {
        use crate::notify::ProgressCompletion;
        let status=self.progress.close_status();
        if status!=ProgressCompletion::Pending {self.closed=true;self.failed|=status==ProgressCompletion::Failed;}
        if self.required&&self.closed&&!self.reported {
            self.reported=true;
            if let Some((path,binding))=&self.report {
                let receipt=json!({"domain":"native-notification-collection/1","binding":binding,"collection":self.progress.collection_facts(),"failed":self.failed});
                if save_json(path,&receipt,MAX_RECEIPT).is_err(){self.failed=true;}
            }
        }
        self.closed&&!self.failed&&status==ProgressCompletion::Done
    }
    pub(crate) fn collect(&mut self)->bool {completion_barrier(||{let _=self.close();self.closed});!self.failed}
    pub(crate) fn deliver_published_terminal(&mut self,context:&Context,job:&Value)->io::Result<()> {
        if job["native_evidence_purpose"]!=PURPOSE||!crate::TERMINAL.contains(&s(job,"status")) {return Err(rejected());}
        let Some((_,binding))=&self.report else {return Err(rejected());};
        if binding["job_id"]!=job["id"]||binding["started_at"]!=job["started_at"]||
            binding["root_pid"]!=job["pid"]||binding["root_created"]!=job["pid_created"] {return Err(rejected());}
        {
            let _state=context.job_lock(s(job,"id"),"state.lock").map_err(|_|rejected())?;
            let durable=context.load(s(job,"id")).map_err(|_|rejected())?;
            if ["id","started_at","pid","pid_created","codex_pid","codex_pid_created","status"]
                .iter().any(|key|durable[*key]!=job[*key]) {return Err(rejected());}
        }
        if !self.closed||self.failed {
            self.progress.suppress_final_delivery("owner_collection_failed");
        }else{
            self.progress.finish_after_collection(job);
        }
        if let Some((path,binding))=&self.report {
            let receipt=json!({"domain":"native-notification-delivery/1","binding":binding,
                "terminal_status":job["status"],"terminal_published":true,
                "collection":self.progress.collection_facts(),"collection_failed":self.failed,
                "scope":"same_owner_delivery_observation_not_runtime_admission"});
            save_json(&path.with_file_name("notification-delivery.private.json"),&receipt,MAX_RECEIPT)?;
        }
        Ok(())
    }
}
impl std::ops::Deref for ProgressOwner {type Target=crate::notify::TaskProgress;fn deref(&self)->&Self::Target{&self.progress}}
impl std::ops::DerefMut for ProgressOwner {fn deref_mut(&mut self)->&mut Self::Target{&mut self.progress}}
fn completion_barrier(mut close:impl FnMut()->bool){while !close(){std::thread::yield_now();}}
impl Drop for ProgressOwner {fn drop(&mut self){if self.required&&!self.closed{let _=self.collect();}}}

#[derive(Clone,Copy,Debug,PartialEq,Eq)]
pub(crate) enum NativeThreadRole { CaptureInput,CaptureStdout,CaptureStderr,OriginInput,OriginStdout,OriginStderr }
impl NativeThreadRole {
    fn name(self)->&'static str {match self {
        Self::CaptureInput=>"native-input",Self::CaptureStdout=>"native-stdout",Self::CaptureStderr=>"native-stderr",
        Self::OriginInput=>"origin-input",Self::OriginStdout=>"origin-stdout",Self::OriginStderr=>"origin-stderr",
    }}
}
#[cfg(test)]
thread_local! {static FAIL_NATIVE_THREAD:std::cell::Cell<Option<NativeThreadRole>>=const {std::cell::Cell::new(None)};}
pub(crate) fn spawn_owned_thread<F,T>(role:NativeThreadRole,work:F)->io::Result<std::thread::JoinHandle<T>>
where F:FnOnce()->T+Send+'static,T:Send+'static {
    #[cfg(test)] if FAIL_NATIVE_THREAD.with(|fail|if fail.get()==Some(role){fail.set(None);true}else{false}) {
        return Err(io::Error::other("owned_thread_setup_failed"));
    }
    // Fixed names have no caller-controlled NUL; OS allocation failure is Err,
    // so an acquired Child stays in the normal attempt's cleanup owner.
    std::thread::Builder::new().name(role.name().into()).spawn(work)
}
#[cfg(test)]
thread_local! {static FULL_ENQUEUE_OBSERVER:std::cell::RefCell<Option<std::sync::mpsc::SyncSender<()>>>=const {std::cell::RefCell::new(None)};}
fn enqueue_before_deadline<T>(sender:&std::sync::mpsc::SyncSender<T>,mut value:T,deadline:std::time::Instant)->io::Result<()> {
    loop {
        if std::time::Instant::now()>=deadline{return Err(io::Error::new(io::ErrorKind::TimedOut,"owned_origin_send_deadline"));}
        match sender.try_send(value) {
            Ok(())=>return Ok(()),
            Err(std::sync::mpsc::TrySendError::Full(pending))=>{
                #[cfg(test)] FULL_ENQUEUE_OBSERVER.with(|hook|{if let Some(observer)=hook.borrow_mut().take(){let _=observer.send(());}});
                value=pending;std::thread::yield_now();
            },
            Err(std::sync::mpsc::TrySendError::Disconnected(_))=>return Err(rejected()),
        }
    }
}

// A normal readonly transport supplies one raw JSON-RPC response frame. This
// codec owns its request IDs and never accepts a caller-selected method/tuple.
#[derive(Clone,Copy,Debug,PartialEq,Eq)]
enum ReplyTimeliness { InTime, DeadlineElapsed }
pub(crate) struct ReceivedReply {raw:Vec<u8>,timeliness:ReplyTimeliness}
impl ReceivedReply {
    fn received(raw:Vec<u8>,timeliness:ReplyTimeliness)->Self{Self{raw,timeliness}}
}
pub(crate) trait ReadonlyTransport { fn exchange(&mut self,request:&[u8],limit:usize)->io::Result<ReceivedReply>; }

// The normal connection owns bounded channels and a finite deadline. A frame
// dequeued before the deadline check remains evidence even when it is too late
// for acceptance. Its bytes pass to the same preservation layer before refusal.
pub(crate) struct ConnectedReadonly {
    requests:std::sync::mpsc::SyncSender<Vec<u8>>,
    replies:std::sync::mpsc::Receiver<io::Result<Vec<u8>>>,
    deadline:std::time::Instant,
    #[cfg(test)] after_receive:Option<fn(std::time::Instant)->std::time::Instant>,
    #[cfg(test)] response_ready:Option<(std::sync::mpsc::Receiver<()>,std::time::Duration)>,
}
impl ConnectedReadonly {
    pub(crate) fn from_owned_connection(
        requests:std::sync::mpsc::SyncSender<Vec<u8>>,
        replies:std::sync::mpsc::Receiver<io::Result<Vec<u8>>>,
        deadline:std::time::Instant,
    )->io::Result<Self>{
        let now=std::time::Instant::now();
        if deadline<=now||deadline.duration_since(now)>std::time::Duration::from_secs(45){return Err(rejected());}
        Ok(Self{requests,replies,deadline,#[cfg(test)] after_receive:None,#[cfg(test)] response_ready:None})
    }
    fn received_at(&self)->std::time::Instant {
        #[cfg(test)] if let Some(clock)=self.after_receive{return clock(self.deadline);}
        std::time::Instant::now()
    }
}
impl ReadonlyTransport for ConnectedReadonly {
    fn exchange(&mut self,request:&[u8],limit:usize)->io::Result<ReceivedReply> {
        let frame=strict_value(request)?;
        if !matches!(s(&frame,"method"),"initialize"|"thread/read"|"thread/turns/list"|"thread/items/list"){return Err(rejected());}
        let _left=self.deadline.checked_duration_since(std::time::Instant::now()).ok_or_else(rejected)?;
        enqueue_before_deadline(&self.requests,request.to_vec(),self.deadline)?;
        // Test-only barrier at the real post-send/pre-receive boundary.
        #[cfg(test)] if let Some((ready,budget))=self.response_ready.take(){
            ready.recv_timeout(std::time::Duration::from_secs(1)).map_err(|_|rejected())?;
            self.deadline=std::time::Instant::now()+budget;
        }
        for _ in 0..=16 {
            let left=self.deadline.checked_duration_since(std::time::Instant::now()).ok_or_else(rejected)?;
            let raw=self.replies.recv_timeout(left).map_err(|_|rejected())??;
            if raw.is_empty()||raw.len()>limit{return Err(rejected());}
            let timeliness=if self.received_at()>=self.deadline{ReplyTimeliness::DeadlineElapsed}else{ReplyTimeliness::InTime};
            if timeliness==ReplyTimeliness::InTime&&let Ok(frame)=strict_value(&raw)&&frame["id"].is_null()&&frame["method"].as_str().is_some(){continue;}
            return Ok(ReceivedReply::received(raw,timeliness));
        }
        Err(rejected())
    }
}

// Resolve an actual turn only when one producer-owned fresh thread has exactly
// one stored turn and every completed native item belongs to that turn. Empty,
// ambiguous, resumed, mismatched, unsupported or truncated replies fail closed.
struct OriginCalls<'a>{transport:&'a mut dyn ReadonlyTransport,directory:&'a Path,id:u64,total:usize,facts:Vec<Value>,prefix:&'static str}
impl OriginCalls<'_> {
    fn call(&mut self,method:&str,params:Value)->io::Result<Value>{
        if !matches!(method,"thread/read"|"thread/items/list"|"thread/turns/list"){return Err(rejected());}
        let mut request=serde_json::to_vec(&json!({"id":self.id,"method":method,"params":params})).map_err(|_|rejected())?;request.push(b'\n');
        let at=self.facts.len();
        self.facts.push(json!({"method":method,"request":null,"send":"not_attempted","transport":"not_attempted",
            "reply":null,"timeliness":"unknown","validation":"not_attempted","preservation":"not_attempted"}));
        let sent=match save_new_bounded(&self.directory.join(format!("{}-request-{}.jsonl",self.prefix,self.id)),&request,MAX_FRAME as u64){
            Ok(sent)=>sent,Err(error)=>{self.facts[at]["preservation"]=json!("request_failed");return Err(error);}
        };
        self.facts[at]["request"]=serde_json::to_value(sent).map_err(|_|rejected())?;
        self.facts[at]["send"]=json!("attempted");self.facts[at]["transport"]=json!("pending");
        let reply=match self.transport.exchange(&request,MAX_FRAME){
            Ok(reply)=>reply,Err(error)=>{self.facts[at]["transport"]=json!("failed_or_timeout");return Err(error);}
        };
        self.facts[at]["transport"]=json!("received");
        let ReceivedReply{raw:reply,timeliness}=reply;
        self.facts[at]["timeliness"]=json!(match timeliness{ReplyTimeliness::InTime=>"in_time",ReplyTimeliness::DeadlineElapsed=>"deadline_elapsed"});
        let next=self.total.checked_add(reply.len()).ok_or_else(rejected)?;
        if reply.len()>MAX_FRAME||next as u64>MAX_STREAM{
            self.facts[at]["preservation"]=json!("rejected_size_limit");return Err(rejected());
        }
        self.total=next;
        let fact=match save_new_bounded(&self.directory.join(format!("{}-response-{}.jsonl",self.prefix,self.id)),&reply,MAX_FRAME as u64){
            Ok(fact)=>fact,Err(error)=>{self.facts[at]["preservation"]=json!("failed");return Err(error);}
        };
        self.facts[at]["reply"]=serde_json::to_value(fact).map_err(|_|rejected())?;
        self.facts[at]["preservation"]=json!("saved");
        if timeliness==ReplyTimeliness::DeadlineElapsed {
            self.facts[at]["validation"]=json!("rejected_deadline");return Err(rejected());
        }
        let value=match rpc_result(&reply,self.id){
            Ok(value)=>{self.facts[at]["validation"]=json!("accepted");value},
            Err(error)=>{self.facts[at]["validation"]=json!("rejected");return Err(error);}
        };
        self.id+=1;Ok(value)
    }
}

pub(crate) fn resolve_fresh_session(thread:&ExactThread,transport:&mut dyn ReadonlyTransport,directory:&Path)->io::Result<ExactSession> {
    if !thread.fresh_launch||thread.native_item_ids.is_empty(){return Err(rejected());}
    let mut calls=OriginCalls{transport,directory,id:1,total:0,facts:Vec::new(),prefix:"resolve"};
    let result=(||->io::Result<ExactSession>{
        let value=calls.call("thread/turns/list",json!({"threadId":thread.thread_id,"limit":2,"itemsView":"notLoaded","sortDirection":"asc"}))?;
        let turns=value["data"].as_array().filter(|a|a.len()==1).ok_or_else(rejected)?;
        if page_cursor(&value)?.is_some(){return Err(rejected());}
        let turn=turns[0]["id"].as_str().filter(|x|id_ok(x)).ok_or_else(rejected)?;
        Ok(ExactSession{job_id:thread.job_id.clone(),generation:thread.generation.clone(),thread_id:thread.thread_id.clone(),
            turn_id:turn.into(),required_items:thread.native_item_ids.clone()})
    })();
    let index=json!({"domain":"owned-native-resolve/1","job_id":thread.job_id,"started_at":thread.generation,
        "thread_id":thread.thread_id,"complete":result.is_ok(),"files":calls.facts,
        "scope":"only unique-turn resolution for a producer-owned fresh thread; not normal origin authority"});
    save_json(&directory.join("resolve-index.private.json"),&index,MAX_RECEIPT)?;
    result
}

// Raw replies stay raw artifacts. They are never recast as native response_item
// records to force the frozen scorer to pass. A connected normal owner, not
// this function, supplies and owns the qualified transport and its deadline.
pub(crate) fn collect_exact_rpc(session:&ExactSession,transport:&mut dyn ReadonlyTransport,directory:&Path)->io::Result<Value> {
    let mut calls=OriginCalls{transport,directory,id:1,total:0,facts:Vec::new(),prefix:"origin"};
    let mut items=std::collections::HashSet::new();
    let result=(||->io::Result<()>{
        let metadata=calls.call("thread/read",json!({"threadId":session.thread_id,"includeTurns":false}))?;
        if metadata["thread"]["id"]!=session.thread_id{return Err(rejected());}
        let mut cursor=Value::Null;let mut cursors=std::collections::HashSet::new();
        for _ in 0..MAX_RPC_PAGES {
            let page=calls.call("thread/items/list",json!({"threadId":session.thread_id,"turnId":session.turn_id,
                "cursor":cursor,"limit":50,"sortDirection":"asc"}))?;
            for entry in page["data"].as_array().ok_or_else(rejected)? {
                if entry["turnId"]!=session.turn_id{return Err(rejected());}
                let id=entry["item"]["id"].as_str().filter(|x|id_ok(x)).ok_or_else(rejected)?;
                if !items.insert(id.to_string()){return Err(rejected());}
            }
            let Some(next)=page_cursor(&page)? else{return Ok(());};
            if !cursors.insert(next.clone()){return Err(rejected());}cursor=Value::String(next);
        }
        Err(rejected())
    })();
    let result=result.and_then(|_|if session.required_items.iter().all(|id|items.contains(id)){Ok(())}else{Err(rejected())});
    let index=json!({"domain":"owned-native-rpc-origin/1","job_id":session.job_id,"started_at":session.generation,
        "thread_id":session.thread_id,"turn_id":session.turn_id,"files":calls.facts,"items":items.len(),"complete":result.is_ok(),
        "scope":"exact raw API replies, not private rollout or normalized original.jsonl",
        "instruction_scope":"unknown unless actually exposed","scorer_v11_complete":false});
    save_json(&directory.join("origin-index.private.json"),&index,MAX_RECEIPT)?;
    result?;Ok(index)
}


fn job_children_observation(root_current:bool,root_member:Observed,launch_root:Observed,
    launch_child:Observed,breakaway:Observed,signalled:Observed,active:Option<u32>)->Observed {
    if !root_current||root_member!=Observed::Confirmed||launch_root!=Observed::Confirmed||
        launch_child!=Observed::Confirmed||breakaway!=Observed::Confirmed||signalled!=Observed::Confirmed {
        return Observed::Unknown;
    }
    match active{Some(1)=>Observed::Confirmed,Some(_)=>Observed::Failed,None=>Observed::Unknown}
}



// The synchronous owner cannot return to process::exit while it still owns an
// uncollected child/pipe. Each settle call is finite; receipt IO never discards
// the next owning state or turns a failed business operation into success.
fn drive_owned_collection<S,R>(mut owner:S,mut settle:impl FnMut(S)->(Option<S>,R),mut observe:impl FnMut(&R,bool))->(R,bool) {
    let mut deferred=false;
    loop{
        let (pending,receipt)=settle(owner);let complete=pending.is_none();
        observe(&receipt,complete);
        match pending{None=>return (receipt,deferred),Some(next)=>{deferred=true;owner=next;}}
    }
}

#[derive(Clone,Debug,Default)]
struct StopFact {attempted:bool,accepted:bool,failed:bool}
impl StopFact {
    fn call(&mut self,operation:impl FnOnce(&Self)->io::Result<()>){
        self.attempted=true;
        match operation(self){Ok(())=>self.accepted=true,Err(_)=>self.failed=true}
    }
}
export_fields!(StopFact,attempted,accepted,failed);
#[derive(Clone,Debug,Default)]
struct OriginFacts {
    child_wait:bool,exit_code:Option<i32>,child_wait_failed:bool,job_active:Option<u32>,last_observed_job_active:Option<u32>,
    latest_job_query_succeeded:bool,job_query_failed:bool,thread_error:bool,pipe_close_failed:bool,
    child_terminate:StopFact,job_terminate:StopFact,deferred:bool,
}
impl OriginFacts {
    fn job_query(&mut self,result:io::Result<u32>){
        self.job_active=None;self.latest_job_query_succeeded=false;
        match result {Ok(active)=>{self.job_active=Some(active);self.last_observed_job_active=Some(active);self.latest_job_query_succeeded=true;},
            Err(_)=>self.job_query_failed=true}
    }
    fn collected(&self,threads_done:bool)->bool{
        self.child_wait&&self.latest_job_query_succeeded&&self.job_active==Some(0)&&threads_done
    }
    fn unforced_success(&self)->bool {
        self.collected(true)&&self.exit_code==Some(0)&&!self.child_wait_failed&&!self.job_query_failed&&!self.thread_error&&!self.pipe_close_failed&&
            !self.child_terminate.attempted&&!self.job_terminate.attempted&&!self.deferred
    }
}
export_fields!(OriginFacts,child_wait,exit_code,child_wait_failed,job_active,last_observed_job_active,latest_job_query_succeeded,job_query_failed,
    thread_error,pipe_close_failed,child_terminate,job_terminate,deferred);

#[cfg(windows)]
mod native {
    use super::*;
    use std::{mem::{size_of,zeroed},os::windows::io::{AsRawHandle,AsHandle,BorrowedHandle,OwnedHandle,FromRawHandle}};
    use windows_sys::Win32::{
        Foundation::{CloseHandle,DuplicateHandle,DUPLICATE_SAME_ACCESS,FILETIME,HANDLE,WAIT_OBJECT_0,WAIT_TIMEOUT},
        Storage::FileSystem::{GetFileInformationByHandle,BY_HANDLE_FILE_INFORMATION},
        System::{Threading::{GetCurrentProcess,GetCurrentProcessId,GetProcessId,GetProcessTimes,GetExitCodeProcess,WaitForSingleObject},
            JobObjects::{IsProcessInJob,QueryInformationJobObject,JOBOBJECT_BASIC_ACCOUNTING_INFORMATION,JOBOBJECT_EXTENDED_LIMIT_INFORMATION,
                JobObjectBasicAccountingInformation,JobObjectExtendedLimitInformation}},
    };
    #[link(name="bcrypt")]
    unsafe extern "system" {
        fn BCryptOpenAlgorithmProvider(handle:*mut *mut core::ffi::c_void,name:*const u16,implementation:*const u16,flags:u32)->i32;
        fn BCryptHash(handle:*mut core::ffi::c_void,secret:*const u8,secret_len:u32,input:*const u8,input_len:u32,output:*mut u8,output_len:u32)->i32;
        fn BCryptCloseAlgorithmProvider(handle:*mut core::ffi::c_void,flags:u32)->i32;
        fn BCryptCreateHash(provider:*mut core::ffi::c_void,hash:*mut *mut core::ffi::c_void,object:*mut u8,object_len:u32,secret:*const u8,secret_len:u32,flags:u32)->i32;
        fn BCryptHashData(hash:*mut core::ffi::c_void,input:*const u8,input_len:u32,flags:u32)->i32;
        fn BCryptFinishHash(hash:*mut core::ffi::c_void,output:*mut u8,output_len:u32,flags:u32)->i32;
        fn BCryptDestroyHash(hash:*mut core::ffi::c_void)->i32;
    }
    // Strong owning values stay here after a failed explicit Close. The same
    // supervisor owns them; neither Drop nor another attempt retries closure.
    enum RetainedResource {
        File(std::mem::ManuallyDrop<File>),Kernel(std::mem::ManuallyDrop<OwnedHandle>),
        Stdin(std::mem::ManuallyDrop<std::process::ChildStdin>),Algorithm(AlgorithmHandle),Hash(HashHandle),
    }
    struct AlgorithmHandle {raw:usize}
    static RETAINED_CLOSES:Mutex<Vec<RetainedResource>>=Mutex::new(Vec::new());
    fn retain(resource:RetainedResource){RETAINED_CLOSES.lock().unwrap_or_else(|p|p.into_inner()).push(resource);}
    pub(super) fn retained_close_count()->usize{
        RETAINED_CLOSES.lock().unwrap_or_else(|p|p.into_inner()).iter().map(|resource|{
            match resource {
                RetainedResource::File(owner)=>{let _=owner.as_raw_handle();},
                RetainedResource::Kernel(owner)=>{let _=owner.as_raw_handle();},
                RetainedResource::Stdin(owner)=>{let _=owner.as_raw_handle();},
                RetainedResource::Algorithm(owner)=>{let _=owner.raw;},
                RetainedResource::Hash(owner)=>{let _=owner.raw;},
            }1usize
        }).sum()
    }
    pub(super) fn close_owned(handle:OwnedHandle)->io::Result<()> {
        let owner=std::mem::ManuallyDrop::new(handle);let raw=owner.as_raw_handle() as HANDLE;
        if unsafe{CloseHandle(raw)}==0 {
            let error=io::Error::last_os_error();
            retain(RetainedResource::Kernel(owner));Err(error)
        }else{Ok(())}
    }
    fn close_algorithm(handle:AlgorithmHandle)->io::Result<()> {
        if unsafe{BCryptCloseAlgorithmProvider(handle.raw as *mut core::ffi::c_void,0)}<0 {
            retain(RetainedResource::Algorithm(handle));Err(rejected())
        }else{Ok(())}
    }
    pub(super) fn sha256(raw:&[u8])->io::Result<String> {
        let n=u32::try_from(raw.len()).map_err(|_|rejected())?;
        let name:Vec<u16>="SHA256\0".encode_utf16().collect();let mut handle=std::ptr::null_mut();
        if unsafe{BCryptOpenAlgorithmProvider(&mut handle,name.as_ptr(),std::ptr::null(),0)}<0{return Err(rejected());}
        let mut owner=CloseOnce::new(AlgorithmHandle{raw:handle as usize},close_algorithm);
        let mut digest=[0u8;32];
        let hashed=unsafe{BCryptHash(owner.get()?.raw as *mut core::ffi::c_void,std::ptr::null(),0,raw.as_ptr(),n,digest.as_mut_ptr(),32)};
        let primary=if hashed<0{Err(rejected())}else{Ok(digest.iter().map(|b|format!("{b:02x}")).collect())};
        finish_owned(&mut owner,primary)
    }
    pub(super) fn close_file(file:File)->io::Result<()> {
        let owner=std::mem::ManuallyDrop::new(file);let raw=owner.as_raw_handle() as HANDLE;
        if unsafe{CloseHandle(raw)}==0 {
            let error=io::Error::last_os_error();
            retain(RetainedResource::File(owner));Err(error)
        }else{Ok(())}
    }

    struct HashHandle {raw:usize}
    fn close_hash(handle:HashHandle)->io::Result<()> {
        if unsafe{BCryptDestroyHash(handle.raw as *mut core::ffi::c_void)}<0 {
            retain(RetainedResource::Hash(handle));Err(rejected())
        }else{Ok(())}
    }
    pub(super) struct StreamHash {hash:CloseOnce<HashHandle>,provider:CloseOnce<AlgorithmHandle>}
    impl StreamHash {
        pub(super) fn new()->io::Result<Self> {
            let before=retained_close_count();
            let primary=(||->io::Result<Self>{
            let name:Vec<u16>="SHA256\0".encode_utf16().collect();let mut algorithm=std::ptr::null_mut();
            if unsafe{BCryptOpenAlgorithmProvider(&mut algorithm,name.as_ptr(),std::ptr::null(),0)}<0{return Err(rejected());}
            let provider=CloseOnce::new(AlgorithmHandle{raw:algorithm as usize},close_algorithm);
            let mut hash=std::ptr::null_mut();
            if unsafe{BCryptCreateHash(provider.get()?.raw as *mut core::ffi::c_void,&mut hash,std::ptr::null_mut(),0,std::ptr::null(),0,0)}<0{return Err(rejected());}
            Ok(Self{hash:CloseOnce::new(HashHandle{raw:hash as usize},close_hash),provider})
            })();
            include_retained_cleanup(before,primary)
        }
        pub(super) fn update(&mut self,raw:&[u8])->io::Result<()> {
            let size=u32::try_from(raw.len()).map_err(|_|rejected())?;
            if unsafe{BCryptHashData(self.hash.get()?.raw as *mut core::ffi::c_void,raw.as_ptr(),size,0)}<0{Err(rejected())}else{Ok(())}
        }
        pub(super) fn finish(mut self)->io::Result<String> {
            let mut digest=[0u8;32];
            let status=unsafe{BCryptFinishHash(self.hash.get()?.raw as *mut core::ffi::c_void,digest.as_mut_ptr(),32,0)};
            let primary=if status<0{Err(rejected())}else{Ok(digest.iter().map(|byte|format!("{byte:02x}")).collect())};
            let result=finish_owned(&mut self.hash,primary);
            finish_owned(&mut self.provider,result)
        }
    }


    pub(super) fn close_child_stdin(stdin:std::process::ChildStdin)->io::Result<()> {
        let owner=std::mem::ManuallyDrop::new(stdin);let raw=owner.as_raw_handle() as HANDLE;
        if unsafe{CloseHandle(raw)}==0 {
            let error=io::Error::last_os_error();
            retain(RetainedResource::Stdin(owner));Err(error)
        }else{Ok(())}
    }

    pub(super) fn file_identity(file:&File)->io::Result<FileIdentity> {
        let mut info:BY_HANDLE_FILE_INFORMATION=unsafe{zeroed()};
        if unsafe{GetFileInformationByHandle(file.as_raw_handle() as HANDLE,&mut info)}==0{return Err(io::Error::last_os_error());}
        // Refuse directories, reparse leaves and aliasing. These files were
        // CreateNew in this producer's per-attempt directory, not external inputs.
        if info.dwFileAttributes&(0x10|0x400)!=0||info.nNumberOfLinks!=1{return Err(rejected());}
        Ok(FileIdentity{volume:info.dwVolumeSerialNumber,index:((info.nFileIndexHigh as u64)<<32)|info.nFileIndexLow as u64,
            created:((info.ftCreationTime.dwHighDateTime as u64)<<32)|info.ftCreationTime.dwLowDateTime as u64,
            size:((info.nFileSizeHigh as u64)<<32)|info.nFileSizeLow as u64})
    }



    #[link(name="userenv")]
    unsafe extern "system" {fn GetUserProfileDirectoryW(token:HANDLE,path:*mut u16,size:*mut u32)->i32;}
    fn normal_origin_environment()->io::Result<Vec<(String,String)>> {
        use windows_sys::Win32::{Security::TOKEN_QUERY,System::{Threading::OpenProcessToken,SystemInformation::GetWindowsDirectoryW}};
        let mut raw=std::ptr::null_mut();
        if unsafe{OpenProcessToken(GetCurrentProcess(),TOKEN_QUERY,&mut raw)}==0{return Err(io::Error::last_os_error());}
        let mut token=CloseOnce::new(unsafe{OwnedHandle::from_raw_handle(raw as _)},close_owned);
        let primary=(||->io::Result<Vec<(String,String)>>{
            let mut size=32768u32;let mut profile=vec![0u16;size as usize];
            if unsafe{GetUserProfileDirectoryW(token.get()?.as_raw_handle() as HANDLE,profile.as_mut_ptr(),&mut size)}==0||size==0||size>32768{return Err(rejected());}
            let end=profile.iter().position(|c|*c==0).ok_or_else(rejected)?;
            let profile=String::from_utf16(&profile[..end]).map_err(|_|rejected())?;
            let mut windows=vec![0u16;32768];let n=unsafe{GetWindowsDirectoryW(windows.as_mut_ptr(),windows.len() as u32)};
            if n==0||n as usize>=windows.len(){return Err(rejected());}
            let windows=String::from_utf16(&windows[..n as usize]).map_err(|_|rejected())?;
            if !Path::new(&profile).is_absolute()||!Path::new(&windows).is_absolute(){return Err(rejected());}
            let local=Path::new(&profile).join("AppData/Local");
            Ok(vec![("SYSTEMROOT".into(),windows.clone()),("WINDIR".into(),windows.clone()),
                ("PATH".into(),format!("{};{}",Path::new(&windows).join("System32").display(),windows)),
                ("USERPROFILE".into(),profile.clone()),("HOME".into(),profile.clone()),
                ("APPDATA".into(),Path::new(&profile).join("AppData/Roaming").to_string_lossy().into()),
                ("LOCALAPPDATA".into(),local.to_string_lossy().into()),("TEMP".into(),local.join("Temp").to_string_lossy().into()),
                ("TMP".into(),local.join("Temp").to_string_lossy().into()),("CODEX_HOME".into(),Path::new(&profile).join(".codex").to_string_lossy().into())])
        })();finish_owned(&mut token,primary)
    }

    struct OriginOwned {
        child:std::process::Child,job:CloseOnce<OwnedHandle>,birth:Option<Birth>,
        writer:Option<std::thread::JoinHandle<io::Result<()>>>,
        stdout:Option<std::thread::JoinHandle<io::Result<()>>>,stderr:Option<std::thread::JoinHandle<io::Result<()>>>,
        facts:OriginFacts,grace_at:Option<std::time::Instant>,shutdown:Arc<std::sync::atomic::AtomicBool>,
        stdout_state:Arc<Mutex<StreamState>>,stderr_state:Arc<Mutex<StreamState>>,
    }
    impl OriginOwned {

        pub(super) fn reclaim_child_if_job_assignment_failed(&mut self)->io::Result<()> {
            if self.birth.is_none(){self.birth=Some(birth(self.child.as_handle())?);}
            if self.child.try_wait()?.is_none(){
                let child=&mut self.child;self.facts.child_terminate.call(|_|child.kill());
            }
            let until=std::time::Instant::now()+std::time::Duration::from_secs(2);
            while self.child.try_wait()?.is_none(){if std::time::Instant::now()>=until{return Err(rejected());}std::thread::sleep(std::time::Duration::from_millis(5));}
            Ok(())
        }
    }
    pub(super) struct OriginReader {owned:Option<OriginOwned>,directory:PathBuf}
    #[link(name="ntdll")]
    unsafe extern "system" {fn NtResumeProcess(process:HANDLE)->i32;}
    pub(super) fn resume_child(child:&std::process::Child)->io::Result<()> {
        if unsafe{NtResumeProcess(child.as_raw_handle() as HANDLE)}<0{Err(rejected())}else{Ok(())}
    }
    fn origin_pipe(file:impl std::os::windows::io::IntoRawHandle)->CloseOnce<File> {
        CloseOnce::new(unsafe{File::from_raw_handle(file.into_raw_handle())},close_file)
    }
    #[cfg(test)]
    thread_local! {pub(super) static ORIGIN_FORWARD_FULL_OBSERVER:std::cell::RefCell<Option<std::sync::mpsc::SyncSender<()>>>=const {std::cell::RefCell::new(None)};}
    pub(super) fn origin_read_lines<R:Read>(mut file:CloseOnce<R>,path:PathBuf,send:std::sync::mpsc::SyncSender<io::Result<Vec<u8>>>,
        stop_forward:Arc<std::sync::atomic::AtomicBool>,state:Arc<Mutex<StreamState>>)->io::Result<()> {
        use std::io::BufRead;
        let primary=(||->io::Result<()>{
            let stream=OriginalStream::new(file.get_mut()?,&path,state.clone(),None)?;
            let mut reader=std::io::BufReader::new(stream);let mut forwarding=true;
            loop {
                let mut line=Vec::new();
                let count=Read::take(&mut reader,MAX_FRAME as u64+1).read_until(b'\n',&mut line)?;
                if count==0 {break;}
                if line.len()>MAX_FRAME {return Err(rejected());}
                if stop_forward.load(std::sync::atomic::Ordering::Acquire) {forwarding=false;}
                if forwarding {
                    let mut pending=Ok(line);
                    loop {
                        if stop_forward.load(std::sync::atomic::Ordering::Acquire) {forwarding=false;break;}
                        match send.try_send(pending) {
                            Ok(())=>break,
                            Err(std::sync::mpsc::TrySendError::Full(value))=>{
                                #[cfg(test)] ORIGIN_FORWARD_FULL_OBSERVER.with(|hook|{if let Some(tx)=hook.borrow_mut().take(){let _=tx.send(());}});
                                pending=value;std::thread::yield_now();
                            }
                            Err(std::sync::mpsc::TrySendError::Disconnected(_))=>{forwarding=false;break;}
                        }
                    }
                }
                // Shutdown only stops forwarding. The same held input continues
                // through OriginalStream until real EOF, with a raw identity seal.
            }
            Ok(())
        })();
        if primary.is_err() {let _=send.try_send(Err(rejected()));}
        let result=finish_owned(&mut file,primary);
        if result.is_err() {state.lock().unwrap_or_else(|p|p.into_inner()).persisted=Observed::Failed;}
        result
    }
    fn origin_stderr(mut file:CloseOnce<File>,path:PathBuf,state:Arc<Mutex<StreamState>>)->io::Result<()> {
        let primary=(||->io::Result<()>{
            let mut stream=OriginalStream::new(file.get_mut()?,&path,state.clone(),None)?;
            let mut chunk=[0u8;8192];while stream.read(&mut chunk)?!=0 {} Ok(())
        })();
        let result=finish_owned(&mut file,primary);
        if result.is_err() {state.lock().unwrap_or_else(|p|p.into_inner()).persisted=Observed::Failed;}
        result
    }
    fn origin_settle(mut state:OriginOwned,deadline:std::time::Instant)->(Option<OriginOwned>,Value) {
        state.shutdown.store(true,std::sync::atomic::Ordering::Release);
        let grace=*state.grace_at.get_or_insert_with(||std::time::Instant::now()+std::time::Duration::from_millis(500));
        for pipe in [state.child.stdin.take().map(std::os::windows::io::IntoRawHandle::into_raw_handle),
            state.child.stdout.take().map(std::os::windows::io::IntoRawHandle::into_raw_handle),
            state.child.stderr.take().map(std::os::windows::io::IntoRawHandle::into_raw_handle)].into_iter().flatten(){
            let mut owner=CloseOnce::new(unsafe{File::from_raw_handle(pipe)},close_file);
            if owner.close().is_err(){state.facts.pipe_close_failed=true;}
        }
        loop {
            match state.child.try_wait(){Ok(Some(exit))=>{state.facts.child_wait=true;state.facts.exit_code=exit.code();},
                Ok(None)=>(),Err(_)=>state.facts.child_wait_failed=true}
            let query=(||->io::Result<u32>{
                let mut info:JOBOBJECT_BASIC_ACCOUNTING_INFORMATION=unsafe{zeroed()};
                let job=state.job.get()?;
                if unsafe{QueryInformationJobObject(job.as_raw_handle() as HANDLE,JobObjectBasicAccountingInformation,
                    (&mut info as *mut JOBOBJECT_BASIC_ACCOUNTING_INFORMATION).cast(),size_of::<JOBOBJECT_BASIC_ACCOUNTING_INFORMATION>() as u32,std::ptr::null_mut())}==0{
                    return Err(io::Error::last_os_error());
                }
                Ok(info.ActiveProcesses)
            })();
            state.facts.job_query(query);
            let threads_done=[&state.writer,&state.stdout,&state.stderr].iter().all(|handle|handle.as_ref().is_none_or(|h|h.is_finished()));
            if state.facts.collected(threads_done){
                for handle in [state.writer.take(),state.stdout.take(),state.stderr.take()].into_iter().flatten(){
                    if !matches!(handle.join(),Ok(Ok(()))){state.facts.thread_error=true;}
                }
                let child_handle_close=if super::close_owned_child(state.child).is_ok(){Observed::Confirmed}else{Observed::Failed};
                let job_close=if state.job.close().is_ok(){Observed::Confirmed}else{Observed::Failed};
                let stdout=state.stdout_state.lock().unwrap_or_else(|p|p.into_inner()).clone();
                let stderr=state.stderr_state.lock().unwrap_or_else(|p|p.into_inner()).clone();
                let raw_complete=stdout.eof==Observed::Confirmed&&stdout.persisted==Observed::Confirmed&&stderr.eof==Observed::Confirmed&&stderr.persisted==Observed::Confirmed;
                let success=raw_complete&&state.facts.unforced_success()&&child_handle_close==Observed::Confirmed&&job_close==Observed::Confirmed&&retained_close_count()==0;
                let receipt=json!({"domain":"owned-readonly-origin-collection/1","child":state.birth,"facts":state.facts,
                    "child_wait":state.facts.child_wait,"exit_code":state.facts.exit_code,"job_active":state.facts.job_active,
                    "thread_error":state.facts.thread_error,"terminate_attempted":state.facts.child_terminate.attempted||state.facts.job_terminate.attempted,
                    "pipe_threads_complete":true,"stdout":stdout,"stderr":stderr,"raw_streams_complete":raw_complete,"job_close":job_close,"child_handle_close":child_handle_close,
                    "retained_close_failures":retained_close_count(),"collected":true,"operation_success":success,"AI_turn_started":false});
                return (None,receipt);
            }
            if std::time::Instant::now()>=deadline{state.facts.deferred=true;break;}
            if std::time::Instant::now()>=grace{
                if !state.facts.child_wait&&!state.facts.child_terminate.attempted{
                    let child=&mut state.child;state.facts.child_terminate.call(|_|child.kill());
                }
                // Attempt is recorded before the native call, including an
                // error. A later natural exit cannot erase this event.
                if !state.facts.job_terminate.attempted&&let Ok(job)=state.job.get(){
                        let raw=job.as_raw_handle() as HANDLE;
                        state.facts.job_terminate.call(|_|{
                            if unsafe{windows_sys::Win32::System::JobObjects::TerminateJobObject(raw,1)}==0{
                                Err(io::Error::last_os_error())
                            }else{Ok(())}
                        });
                }
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        let receipt=json!({"domain":"owned-readonly-origin-collection/1","child":state.birth,"facts":state.facts,
            "child_wait":state.facts.child_wait,"exit_code":state.facts.exit_code,"job_active":state.facts.job_active,
            "thread_error":state.facts.thread_error,"terminate_attempted":state.facts.child_terminate.attempted||state.facts.job_terminate.attempted,
            "pipe_threads_complete":false,"stdout":state.stdout_state.lock().unwrap_or_else(|p|p.into_inner()).clone(),
            "stderr":state.stderr_state.lock().unwrap_or_else(|p|p.into_inner()).clone(),"raw_streams_complete":false,"collected":false,"operation_success":false,"retained_owner":true,
            "retained_close_failures":retained_close_count(),"AI_turn_started":false});
        (Some(state),receipt)
    }
    fn origin_collect_owned(state:OriginOwned,directory:&Path,first_budget:std::time::Duration)->io::Result<Value>{
        let mut first=true;let mut initial_saved=false;let mut last_error=None;
        let (receipt,deferred)=drive_owned_collection(state,|state|{
            let budget=if first{first=false;first_budget}else{std::time::Duration::from_millis(100)};
            origin_settle(state,std::time::Instant::now()+budget)
        },|receipt,complete|{
            let filename=if !initial_saved{initial_saved=true;Some("connection-collection.private.json")}
                else if complete{Some("connection-collection-drained.private.json")}else{None};
            if let Some(filename)=filename&&let Err(error)=save_json(&directory.join(filename),receipt,MAX_RECEIPT){
                last_error=Some(match last_error.take(){None=>error,Some(primary)=>io::Error::other(PrimaryAndCloseFailure{primary,cleanup:error})});
            }
        });
        if let Some(error)=last_error{return Err(error);}
        if deferred{return Err(rejected());}
        Ok(receipt)
    }
    impl Drop for OriginReader {
        fn drop(&mut self){if let Some(state)=self.owned.take(){
            let _=origin_collect_owned(state,&self.directory,std::time::Duration::from_secs(3));
        }}
    }
    impl OriginReader {
        pub(super) fn connect(launcher:&Path,directory:&Path)->io::Result<(ConnectedReadonly,Self)> {
            use std::os::windows::process::CommandExt;
            use windows_sys::Win32::System::JobObjects::{CreateJobObjectW,SetInformationJobObject,AssignProcessToJobObject};
            let raw=unsafe{CreateJobObjectW(std::ptr::null(),std::ptr::null())};if raw.is_null(){return Err(io::Error::last_os_error());}
            let job=CloseOnce::new(unsafe{OwnedHandle::from_raw_handle(raw as _)},close_owned);
            let mut limits:JOBOBJECT_EXTENDED_LIMIT_INFORMATION=unsafe{zeroed()};limits.BasicLimitInformation.LimitFlags=0x2000;
            if unsafe{SetInformationJobObject(raw,JobObjectExtendedLimitInformation,(&limits as *const JOBOBJECT_EXTENDED_LIMIT_INFORMATION).cast(),size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32)}==0{return Err(io::Error::last_os_error());}
            let mut command=crate::hidden_command(launcher.to_str().ok_or_else(rejected)?);
            command.args(["app-server","--listen","stdio://"]).stdin(std::process::Stdio::piped()).stdout(std::process::Stdio::piped()).stderr(std::process::Stdio::piped())
                .creation_flags(crate::NO_WINDOW|0x4).env_clear().envs(normal_origin_environment()?);
            // Current token and Windows directories identify the normal profile;
            // caller API-key/endpoint/config environment is not inherited.
            let child=command.spawn()?;
            let mut owner=Self{owned:Some(OriginOwned{child,job,birth:None,writer:None,stdout:None,stderr:None,facts:OriginFacts::default(),grace_at:None,shutdown:Arc::new(std::sync::atomic::AtomicBool::new(false)),stdout_state:Arc::new(Mutex::new(StreamState::default())),stderr_state:Arc::new(Mutex::new(StreamState::default()))}),directory:directory.to_path_buf()};
            let state=owner.owned.as_mut().ok_or_else(rejected)?;
            state.birth=Some(birth(state.child.as_handle())?);
            if unsafe{AssignProcessToJobObject(raw,state.child.as_raw_handle() as HANDLE)}==0{
                let primary=io::Error::last_os_error();let _=state.reclaim_child_if_job_assignment_failed();return Err(primary);
            }
            let stdin=origin_pipe(state.child.stdin.take().ok_or_else(rejected)?);
            let stdout=origin_pipe(state.child.stdout.take().ok_or_else(rejected)?);
            let stderr=origin_pipe(state.child.stderr.take().ok_or_else(rejected)?);
            let (request_tx,request_rx)=std::sync::mpsc::sync_channel::<Vec<u8>>(1);
            let (reply_tx,reply_rx)=std::sync::mpsc::sync_channel::<io::Result<Vec<u8>>>(8);
            let shutdown=owner.owned.as_ref().ok_or_else(rejected)?.shutdown.clone();
            let writer=spawn_owned_thread(NativeThreadRole::OriginInput,move||{
                let mut stdin=stdin;let work=(||->io::Result<()>{loop{
                    if shutdown.load(std::sync::atomic::Ordering::Acquire){break;}
                    match request_rx.recv_timeout(std::time::Duration::from_millis(25)){
                        Ok(raw)=>{stdin.get_mut()?.write_all(&raw)?;stdin.get_mut()?.flush()?;},
                        Err(std::sync::mpsc::RecvTimeoutError::Timeout)=>continue,Err(std::sync::mpsc::RecvTimeoutError::Disconnected)=>break,
                    }
                }Ok(())})();
                finish_owned(&mut stdin,work)
            })?;
            owner.owned.as_mut().ok_or_else(rejected)?.writer=Some(writer);
            let stdout_path=directory.join("connection.stdout.native.jsonl");
            let stderr_path=directory.join("connection.stderr.native.txt");
            let shutdown=owner.owned.as_ref().ok_or_else(rejected)?.shutdown.clone();
            let stream_state=owner.owned.as_ref().ok_or_else(rejected)?.stdout_state.clone();
            let output=spawn_owned_thread(NativeThreadRole::OriginStdout,move||origin_read_lines(stdout,stdout_path,reply_tx,shutdown,stream_state))?;
            owner.owned.as_mut().ok_or_else(rejected)?.stdout=Some(output);
            let stream_state=owner.owned.as_ref().ok_or_else(rejected)?.stderr_state.clone();
            let errors=spawn_owned_thread(NativeThreadRole::OriginStderr,move||origin_stderr(stderr,stderr_path,stream_state))?;
            let state=owner.owned.as_mut().ok_or_else(rejected)?;state.stderr=Some(errors);
            if unsafe{NtResumeProcess(state.child.as_raw_handle() as HANDLE)}<0{return Err(rejected());}
            let mut transport=ConnectedReadonly::from_owned_connection(request_tx,reply_rx,std::time::Instant::now()+std::time::Duration::from_secs(40))?;
            let request=b"{\"id\":0,\"method\":\"initialize\",\"params\":{\"clientInfo\":{\"name\":\"cc_jobs_native_export\",\"version\":\"1\"},\"capabilities\":{\"experimentalApi\":true}}}\n";
            save_new_bounded(&directory.join("initialize-request.jsonl"),request,MAX_FRAME as u64)?;
            let ReceivedReply{raw:reply,timeliness}=transport.exchange(request,MAX_FRAME)?;
            save_new_bounded(&directory.join("initialize-response.jsonl"),&reply,MAX_FRAME as u64)?;
            if timeliness!=ReplyTimeliness::InTime{return Err(rejected());}
            rpc_result(&reply,0)?;
            enqueue_before_deadline(&transport.requests,b"{\"method\":\"initialized\",\"params\":{}}\n".to_vec(),transport.deadline)?;
            Ok((transport,owner))
        }
        pub(super) fn finish(mut self,directory:&Path)->io::Result<()> {
            let state=self.owned.take().ok_or_else(rejected)?;
            let receipt=origin_collect_owned(state,directory,std::time::Duration::from_secs(5))?;
            if receipt["operation_success"]==true{Ok(())}else{Err(rejected())}
        }
    }


    #[cfg(test)]
    pub(super) fn origin_collection_fixture(directory:&Path,delay_pipe:bool)->io::Result<Value>{
        use std::os::windows::process::CommandExt;
        use windows_sys::Win32::System::JobObjects::{CreateJobObjectW,SetInformationJobObject,AssignProcessToJobObject};
        let raw=unsafe{CreateJobObjectW(std::ptr::null(),std::ptr::null())};if raw.is_null(){return Err(io::Error::last_os_error());}
        let job=CloseOnce::new(unsafe{OwnedHandle::from_raw_handle(raw as _)},close_owned);
        let mut limits:JOBOBJECT_EXTENDED_LIMIT_INFORMATION=unsafe{zeroed()};limits.BasicLimitInformation.LimitFlags=0x2000;
        if unsafe{SetInformationJobObject(raw,JobObjectExtendedLimitInformation,(&limits as *const JOBOBJECT_EXTENDED_LIMIT_INFORMATION).cast(),size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32)}==0{return Err(io::Error::last_os_error());}
        let mut command=crate::hidden_command(std::env::current_exe()?.to_str().ok_or_else(rejected)?);
        command.args(["--exact","native_export::tests::owned_stdin_fixture_helper","--nocapture"])
            .env("CC_NATIVE_EXPORT_STDIN_FIXTURE","1").env("CC_NATIVE_EXPORT_STDIN_RESULT",directory.join("owned-reader-fixture.txt"))
            .current_dir(directory).stdin(std::process::Stdio::piped()).stdout(std::process::Stdio::piped()).stderr(std::process::Stdio::piped())
            .creation_flags(crate::NO_WINDOW|0x4);
        let child=command.spawn()?;
        let mut owner=OriginReader{owned:Some(OriginOwned{child,job,birth:None,writer:None,stdout:None,stderr:None,
            facts:OriginFacts::default(),grace_at:None,shutdown:Arc::new(std::sync::atomic::AtomicBool::new(false)),stdout_state:Arc::new(Mutex::new(StreamState::default())),stderr_state:Arc::new(Mutex::new(StreamState::default()))}),directory:directory.to_path_buf()};
        let state=owner.owned.as_mut().ok_or_else(rejected)?;state.birth=Some(birth(state.child.as_handle())?);
        if unsafe{AssignProcessToJobObject(raw,state.child.as_raw_handle() as HANDLE)}==0{return Err(io::Error::last_os_error());}
        let mut stdin=origin_pipe(state.child.stdin.take().ok_or_else(rejected)?);stdin.close()?;
        let stdout=origin_pipe(state.child.stdout.take().ok_or_else(rejected)?);
        let stderr=origin_pipe(state.child.stderr.take().ok_or_else(rejected)?);
        let (release,wait)=std::sync::mpsc::sync_channel::<()>(1);
        let (send,receive)=std::sync::mpsc::sync_channel(1);drop(receive);
        let shutdown=state.shutdown.clone();let stream_state=state.stdout_state.clone();let path=directory.join("fixture-stdout.native.jsonl");
        state.stdout=Some(spawn_owned_thread(NativeThreadRole::OriginStdout,move||{
            if delay_pipe{wait.recv_timeout(std::time::Duration::from_secs(5)).map_err(|_|rejected())?;}
            origin_read_lines(stdout,path,send,shutdown,stream_state)
        })?);
        let path=directory.join("fixture-stderr.native.txt");let stream_state=state.stderr_state.clone();
        state.stderr=Some(spawn_owned_thread(NativeThreadRole::OriginStderr,move||origin_stderr(stderr,path,stream_state))?);
        resume_child(&state.child)?;
        if !delay_pipe{
            let until=std::time::Instant::now()+std::time::Duration::from_secs(5);
            loop{let done=state.child.try_wait()?.is_some()&&state.stdout.as_ref().is_some_and(std::thread::JoinHandle::is_finished)&&state.stderr.as_ref().is_some_and(std::thread::JoinHandle::is_finished);
                if done{break;}if std::time::Instant::now()>=until{return Err(rejected());}std::thread::yield_now();}
        }
        let state=owner.owned.take().ok_or_else(rejected)?;let mut first=true;
        let (receipt,deferred)=drive_owned_collection(state,|state|{
            let budget=if first{first=false;if delay_pipe{std::time::Duration::ZERO}else{std::time::Duration::from_secs(3)}}else{std::time::Duration::from_millis(100)};
            origin_settle(state,std::time::Instant::now()+budget)
        },|_,complete|{if !complete{let _=release.try_send(());}});
        if deferred!=delay_pipe{return Err(rejected());}Ok(receipt)
    }


    #[cfg(test)]
    pub(super) fn capture_fixture_job(flags:u32)->io::Result<CloseOnce<OwnedHandle>> {
        use windows_sys::Win32::System::JobObjects::{CreateJobObjectW,SetInformationJobObject,AssignProcessToJobObject};
        let raw=unsafe{CreateJobObjectW(std::ptr::null(),std::ptr::null())};
        if raw.is_null(){return Err(io::Error::last_os_error());}
        let owner=CloseOnce::new(unsafe{OwnedHandle::from_raw_handle(raw as _)},close_owned);
        let mut limits:JOBOBJECT_EXTENDED_LIMIT_INFORMATION=unsafe{zeroed()};limits.BasicLimitInformation.LimitFlags=flags;
        if unsafe{SetInformationJobObject(raw,JobObjectExtendedLimitInformation,(&limits as *const JOBOBJECT_EXTENDED_LIMIT_INFORMATION).cast(),size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32)}==0{return Err(io::Error::last_os_error());}
        if unsafe{AssignProcessToJobObject(raw,GetCurrentProcess())}==0{return Err(io::Error::last_os_error());}
        Ok(owner)
    }


    #[cfg(test)]
    pub(super) fn empty_fixture_job(flags:u32)->io::Result<CloseOnce<OwnedHandle>> {
        use windows_sys::Win32::System::JobObjects::{CreateJobObjectW,SetInformationJobObject};
        let raw=unsafe{CreateJobObjectW(std::ptr::null(),std::ptr::null())};
        if raw.is_null(){return Err(io::Error::last_os_error());}
        let owner=CloseOnce::new(unsafe{OwnedHandle::from_raw_handle(raw as _)},close_owned);
        let mut limits:JOBOBJECT_EXTENDED_LIMIT_INFORMATION=unsafe{zeroed()};limits.BasicLimitInformation.LimitFlags=flags;
        if unsafe{SetInformationJobObject(raw,JobObjectExtendedLimitInformation,(&limits as *const JOBOBJECT_EXTENDED_LIMIT_INFORMATION).cast(),size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32)}==0{return Err(io::Error::last_os_error());}
        Ok(owner)
    }
    #[cfg(test)]
    pub(super) fn assign_fixture_child(job:&CloseOnce<OwnedHandle>,child:&std::process::Child)->io::Result<()> {
        if unsafe{windows_sys::Win32::System::JobObjects::AssignProcessToJobObject(job.get()?.as_raw_handle() as HANDLE,child.as_raw_handle() as HANDLE)}==0 {
            Err(io::Error::last_os_error())
        }else{Ok(())}
    }

    pub(super) fn current_process()->io::Result<CloseOnce<OwnedHandle>> {
        // Duplicate the kernel pseudo handle into a real owned handle before
        // retaining a stable observed kernel object across collection.
        let current=unsafe{GetCurrentProcess()};let mut out=std::ptr::null_mut();
        if unsafe{DuplicateHandle(current,current,current,&mut out,0,0,DUPLICATE_SAME_ACCESS)}==0{return Err(io::Error::last_os_error());}
        Ok(CloseOnce::new(unsafe{OwnedHandle::from_raw_handle(out as _)},close_owned))
    }

    fn clone_handle(handle:BorrowedHandle<'_>)->io::Result<CloseOnce<OwnedHandle>> {
        let current=unsafe{GetCurrentProcess()};let mut out=std::ptr::null_mut();
        if unsafe{DuplicateHandle(current,handle.as_raw_handle() as HANDLE,current,&mut out,0,0,DUPLICATE_SAME_ACCESS)}==0 {
            return Err(io::Error::last_os_error());
        }
        Ok(CloseOnce::new(unsafe{OwnedHandle::from_raw_handle(out as _)},close_owned))
    }
    fn birth(handle:BorrowedHandle<'_>)->io::Result<Birth> {
        let raw=handle.as_raw_handle() as HANDLE;let pid=unsafe{GetProcessId(raw)};
        let mut created:FILETIME=unsafe{zeroed()};let mut exit=created;let mut kernel=created;let mut user=created;
        if pid==0||unsafe{GetProcessTimes(raw,&mut created,&mut exit,&mut kernel,&mut user)}==0{return Err(io::Error::last_os_error());}
        let created=((created.dwHighDateTime as u64)<<32)|created.dwLowDateTime as u64;
        if created==0{return Err(rejected());}Ok(Birth{pid,created})
    }
    fn member(process:BorrowedHandle<'_>,job:BorrowedHandle<'_>)->Observed {
        let mut answer=0;
        if unsafe{IsProcessInJob(process.as_raw_handle() as HANDLE,job.as_raw_handle() as HANDLE,&mut answer)}==0 {
            Observed::Unknown
        }else if answer==0{Observed::Failed}else{Observed::Confirmed}
    }
        pub(crate) struct Collection {
        root:Birth,child:Birth,child_signalled:Observed,exit_code:Option<u32>,
        root_in_owned_job:Observed,child_in_owned_job_at_launch:Observed,
        job_active_processes:Option<u32>,job_breakaway_disabled:Observed,
        all_job_children_collected:Observed,os_wait_error:bool,handle_release:Observed,retained_handles:usize,
        scope:&'static str,
    }
    export_fields!(Collection,root,child,child_signalled,exit_code,root_in_owned_job,child_in_owned_job_at_launch,job_active_processes,job_breakaway_disabled,all_job_children_collected,os_wait_error,handle_release,retained_handles,scope);
    pub(crate) struct Processes {
        controller:CloseOnce<OwnedHandle>,child:CloseOnce<OwnedHandle>,job:Option<CloseOnce<OwnedHandle>>,root:Birth,child_birth:Birth,
        root_member:Observed,child_member:Observed,breakaway_disabled:Observed,
    }
    impl Processes {
        pub(super) fn pin(child:BorrowedHandle<'_>,job:Option<BorrowedHandle<'_>>)->io::Result<Self> {
            let before=retained_close_count();
            let primary=(||->io::Result<Self>{
            let controller=current_process()?;let root_handle=controller.get()?.as_handle();
            let root=birth(root_handle)?;let child_birth=birth(child)?;
            if root.pid!=unsafe{GetCurrentProcessId()}||root.pid==child_birth.pid{return Err(rejected());}
            let child_handle=clone_handle(child)?;
            let (root_member,child_member,breakaway_disabled,job)=if let Some(job)=job {
                let root_member=member(root_handle,job);let child_member=member(child,job);
                let mut limits:JOBOBJECT_EXTENDED_LIMIT_INFORMATION=unsafe{zeroed()};
                let ok=unsafe{QueryInformationJobObject(job.as_raw_handle() as HANDLE,JobObjectExtendedLimitInformation,
                    (&mut limits as *mut JOBOBJECT_EXTENDED_LIMIT_INFORMATION).cast(),size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32,std::ptr::null_mut())};
                let breakaway_disabled=if ok==0{Observed::Unknown}else if limits.BasicLimitInformation.LimitFlags&(0x800|0x1000)!=0{Observed::Failed}else{Observed::Confirmed};
                (root_member,child_member,breakaway_disabled,Some(clone_handle(job)?))
            }else{(Observed::Unknown,Observed::Unknown,Observed::Unknown,None)};
            Ok(Self{controller,child:child_handle,job,root,child_birth,root_member,child_member,breakaway_disabled})
            })();
            if let Err(error)=primary {
                let added=retained_close_count().saturating_sub(before);
                if added>0{return Err(io::Error::other(ConstructorCleanupFailure{primary:error,retained:added}));}
                return Err(error);
            }
            primary
        }

        #[cfg(test)]
        pub(super) fn borrowed_actual_child_for_fixture(child:BorrowedHandle<'_>)->io::Result<Self>{Self::pin(child,None)}


        pub(super) fn reclaim_children(&self,deadline:std::time::Instant)->io::Result<()> {
            use windows_sys::Win32::System::Threading::{OpenProcess,TerminateProcess,PROCESS_TERMINATE,PROCESS_QUERY_LIMITED_INFORMATION,PROCESS_SYNCHRONIZE};
            let job=self.job.as_ref().ok_or_else(rejected)?.get()?;
            if self.breakaway_disabled!=Observed::Confirmed{return Err(rejected());}
            let mut pinned:Vec<CloseOnce<OwnedHandle>>=Vec::new();
            loop {
                let list_start=8/size_of::<usize>();
                let mut ids=vec![0usize;4096+list_start];
                if unsafe{QueryInformationJobObject(job.as_raw_handle() as HANDLE,windows_sys::Win32::System::JobObjects::JobObjectBasicProcessIdList,
                    ids.as_mut_ptr().cast(),(ids.len()*size_of::<usize>()) as u32,std::ptr::null_mut())}==0{return Err(io::Error::last_os_error());}
                let header=unsafe{std::slice::from_raw_parts(ids.as_ptr().cast::<u32>(),2)};
                let count=header[1] as usize;if count>4096||header[0] as usize>count{return Err(rejected());}
                let mut other=0;
                for pid in &ids[list_start..list_start+count] {
                    let pid=u32::try_from(*pid).map_err(|_|rejected())?;
                    if pid==self.root.pid{continue;}
                    other+=1;
                    let handle=unsafe{OpenProcess(PROCESS_TERMINATE|PROCESS_QUERY_LIMITED_INFORMATION|PROCESS_SYNCHRONIZE,0,pid)};
                    if handle.is_null(){return Err(io::Error::last_os_error());}
                    let owner=CloseOnce::new(unsafe{OwnedHandle::from_raw_handle(handle as _)},close_owned);
                    if member(owner.get()?.as_handle(),job.as_handle())!=Observed::Confirmed{return Err(rejected());}
                    let actual=birth(owner.get()?.as_handle())?;if actual.pid==self.root.pid{return Err(rejected());}
                    let raw=owner.get()?.as_raw_handle() as HANDLE;
                    if unsafe{WaitForSingleObject(raw,0)}!=WAIT_OBJECT_0&&unsafe{TerminateProcess(raw,1)}==0&&unsafe{WaitForSingleObject(raw,0)}!=WAIT_OBJECT_0{return Err(io::Error::last_os_error());}
                    if pinned.len()>=4096{return Err(rejected());}
                    pinned.push(owner);
                }
                if other==0&&pinned.iter().all(|h|h.get().is_ok_and(|h|unsafe{WaitForSingleObject(h.as_raw_handle() as HANDLE,0)}==WAIT_OBJECT_0)) {
                    for mut owner in pinned{owner.close()?;}return Ok(());
                }
                if std::time::Instant::now()>=deadline{return Err(io::Error::new(io::ErrorKind::TimedOut,"owned Job collection deadline"));}
                std::thread::sleep(std::time::Duration::from_millis(10));
            }
        }

        pub(super) fn children_collected(&self)->bool{self.collect().all_job_children_collected==Observed::Confirmed}
        pub(super) fn root(&self)->Birth{self.root.clone()}
        pub(super) fn child(&self)->Birth{self.child_birth.clone()}
        pub(super) fn same_child(&self,handle:BorrowedHandle<'_>)->bool{birth(handle).is_ok_and(|actual|actual==self.child_birth)}
        pub(super) fn finish(mut self)->Collection {
            let mut result=self.collect();
            let child_close=self.child.close();let job_close=self.job.as_mut().map(CloseOnce::close).transpose();let root_close=self.controller.close();
            result.handle_release=if child_close.is_ok()&&job_close.is_ok()&&root_close.is_ok(){Observed::Confirmed}else{Observed::Failed};
            result.retained_handles=retained_close_count();result
        }
        pub(super) fn collect(&self)->Collection {
            let wait=unsafe{WaitForSingleObject(self.child.get().map(|h|h.as_raw_handle() as HANDLE).unwrap_or(std::ptr::null_mut()),0)};
            let child_signalled=if wait==WAIT_OBJECT_0{Observed::Confirmed}else if wait==WAIT_TIMEOUT{Observed::Failed}else{Observed::Unknown};
            let mut code=0;let exit_code=if child_signalled==Observed::Confirmed&&unsafe{GetExitCodeProcess(self.child.get().map(|h|h.as_raw_handle() as HANDLE).unwrap_or(std::ptr::null_mut()),&mut code)}!=0{Some(code)}else{None};
            let Some(current)=self.controller.get().ok().map(AsHandle::as_handle) else {return Collection{root:self.root.clone(),child:self.child_birth.clone(),child_signalled,exit_code,root_in_owned_job:Observed::Unknown,child_in_owned_job_at_launch:self.child_member,job_active_processes:None,job_breakaway_disabled:self.breakaway_disabled,all_job_children_collected:Observed::Unknown,os_wait_error:true,handle_release:Observed::Unknown,retained_handles:retained_close_count(),scope:"controller handle unavailable"};};
            let root_current=birth(current).is_ok_and(|root|root==self.root);
            let mut root_member=Observed::Unknown;let mut active=None;
            if let Some(job)=&self.job {
                root_member=job.get().map(|handle|member(current,handle.as_handle())).unwrap_or(Observed::Unknown);
                let mut info:JOBOBJECT_BASIC_ACCOUNTING_INFORMATION=unsafe{zeroed()};
                if unsafe{QueryInformationJobObject(job.get().map(|h|h.as_raw_handle() as HANDLE).unwrap_or(std::ptr::null_mut()),JobObjectBasicAccountingInformation,
                    (&mut info as *mut JOBOBJECT_BASIC_ACCOUNTING_INFORMATION).cast(),size_of::<JOBOBJECT_BASIC_ACCOUNTING_INFORMATION>() as u32,std::ptr::null_mut())}!=0 {active=Some(info.ActiveProcesses);}
            }
            let collected=job_children_observation(root_current,root_member,self.root_member,self.child_member,self.breakaway_disabled,child_signalled,active);
            Collection{root:self.root.clone(),child:self.child_birth.clone(),child_signalled,exit_code,
                root_in_owned_job:root_member,child_in_owned_job_at_launch:self.child_member,job_active_processes:active,
                job_breakaway_disabled:self.breakaway_disabled,all_job_children_collected:collected,
                os_wait_error:wait!=WAIT_OBJECT_0&&wait!=WAIT_TIMEOUT,handle_release:Observed::Unknown,retained_handles:retained_close_count(),
                scope:"read-only actual held process/Job observation; root supervisor remains live, no peer or external process claim"}
        }
    }
}
#[cfg(not(windows))]
mod native {
    use super::*;
    pub(super) fn retained_close_count()->usize{0}
    pub(super) fn sha256(_:&[u8])->io::Result<String>{Err(io::Error::new(io::ErrorKind::Unsupported,"Windows native export observation required"))}
    pub(super) fn close_file(file:File)->io::Result<()>{drop(file);Ok(())}

    pub(super) struct StreamHash;
    impl StreamHash {
        pub(super) fn new()->io::Result<Self>{Err(rejected())}
        pub(super) fn update(&mut self,_:&[u8])->io::Result<()>{Err(rejected())}
        pub(super) fn finish(self)->io::Result<String>{Err(rejected())}
    }

    pub(super) fn file_identity(_:&File)->io::Result<FileIdentity>{Err(io::Error::new(io::ErrorKind::Unsupported,"Windows held-file identity required"))}
        pub(crate) struct Collection {scope:&'static str}
    export_fields!(Collection,scope);
    pub(crate) struct Processes;
    impl Processes {pub(super) fn finish(self)->Collection{Collection{scope:"unsupported; process/Job unknown"}}}
}

#[cfg(test)]
#[path="../tests/support/native_export.rs"]
mod tests;

pub(crate) fn validate_request_purpose(purpose:&str)->Result<()> {
    if purpose!=PURPOSE{return Err(Error::new("unsupported native evidence purpose"));}
    #[cfg(not(windows))] {return Err(Error::new("native evidence requires the normal Windows producer"));}
    #[cfg(windows)] {Ok(())}
}
impl Context {
    pub(crate) fn cmd_native_export(&self,jid:&str,part:&str,out:&mut dyn Write)->Result<i32> {
        if !matches!(part,"metadata"|"prompt"|"stdout"|"stderr"|"origin"){return Err(Error::new("unsupported native export part"));}
        let _raw=crate::platform::native_raw_output();
        let selected=self.select_native_export(jid)?;
        if selected.collection()!=&selected.metadata()["collection"]{return Err(Error::new("native collection metadata changed"));}
        let _retained=retained_attempt_count();
        match part {
            "metadata"=>{serde_json::to_writer(out,selected.metadata())?;},
            "prompt"|"stdout"|"stderr"=>{
                let name=match part{"prompt"=>"prompt.as-sent.utf8","stdout"=>"stdout.native.jsonl",_=>"stderr.native.txt"};
                out.write_all(&selected.read_original(self,name)?)?;
            }
            "origin"=>{self.export_normal_origin(&selected,out)?;},
            _=>return Err(Error::new("native export part must be metadata, prompt, stdout, stderr or origin")),
        }
        Ok(0)
    }
}

static RETAINED_READERS:Mutex<Vec<std::thread::JoinHandle<()>>>=Mutex::new(Vec::new());
static RETAINED_INPUTS:Mutex<Vec<std::thread::JoinHandle<io::Result<()>>>>=Mutex::new(Vec::new());
pub(crate) fn retain_reader(reader:std::thread::JoinHandle<()>){RETAINED_READERS.lock().unwrap_or_else(|p|p.into_inner()).push(reader);}
pub(crate) fn retain_input(input:std::thread::JoinHandle<io::Result<()>>){RETAINED_INPUTS.lock().unwrap_or_else(|p|p.into_inner()).push(input);}


// No public/bool/JSON constructor. A normal software owner can issue this only
// after its actual held executable admission; the current System has no issuer.
pub struct NormalOriginLauncher {path:PathBuf}
impl NormalOriginLauncher {
    fn fixed_path(&self)->io::Result<&Path>{
        if self.path!=Path::new("D:/CC/Tools/codex-progress-tui/codex-launch.exe"){return Err(rejected());}Ok(&self.path)
    }
}

#[cfg(windows)]
impl Context {
    fn export_normal_origin(&self,selected:&SelectedExport,out:&mut dyn Write)->Result<()> {
        let thread=selected.thread()?;
        let turn=selected.binding["turn_ids"].as_array().filter(|a|a.len()==1).and_then(|a|a[0].as_str());
        let directory=selected.directory.join("normal-origin");
        qualified_source(self,&directory)?;
        // This fixed normal entry is an existing installed readonly operation.
        // Source classification never supplies executable software provenance.
        // Actual H1 deployment/host acceptance remains an external condition.
        let admission=self.rt.native_origin_launcher()?;
        let launcher=admission.fixed_path()?;
        qualified_source(self,launcher)?;
        fs::create_dir(&directory)?;
        let (mut transport,owner)=native::OriginReader::connect(launcher,&directory)?;
        let result=(||->io::Result<Value>{
            let session=if let Some(turn)=turn {selected.session(turn)?}else{resolve_fresh_session(&thread,&mut transport,&directory)?};
            collect_exact_rpc(&session,&mut transport,&directory)
        })();
        drop(transport);
        let cleanup=owner.finish(&directory);
        match (result,cleanup) {
            (Ok(index),Ok(()))=>{serde_json::to_writer(out,&index)?;Ok(())},
            (Err(error),Ok(()))=>Err(error.into()),(Ok(_),Err(error))=>Err(error.into()),
            (Err(primary),Err(cleanup))=>Err(io::Error::other(PrimaryAndCloseFailure{primary,cleanup}).into()),
        }
    }
}
#[cfg(not(windows))]
impl Context {
    fn export_normal_origin(&self,_:&SelectedExport,_:&mut dyn Write)->Result<()> {
        Err(Error::new("normal readonly origin requires the Windows owned controller"))
    }
}

struct RetainedNativeAttempt {
    job_id:String,generation:String,root_created:Value,child_created:Value,process:Box<dyn crate::Running>,capture:Option<Capture>,readers:Vec<std::thread::JoinHandle<()>>,
    input:Option<std::thread::JoinHandle<io::Result<()>>>,
}
static RETAINED_NATIVE_ATTEMPTS:Mutex<Vec<RetainedNativeAttempt>>=Mutex::new(Vec::new());
pub(crate) fn retain_native_attempt(context:&Context,row:&Value,process:Box<dyn crate::Running>,capture:Option<Capture>,
    readers:Vec<std::thread::JoinHandle<()>>,input:Option<std::thread::JoinHandle<io::Result<()>>>)->io::Result<()> {
    let job=s(row,"id").to_string();let generation=s(row,"started_at").to_string();
    let path=capture.as_ref().map(|c|c.directory.join("uncollected.private.json"))
        .unwrap_or_else(||context.job_dir(&job).join(format!("native-uncollected-{}.private.json",process.pid())));
    let value=json!({"domain":DOMAIN,"purpose":PURPOSE,"job_id":job,"started_at":generation,
        "root":{"pid":row["pid"],"created":row["pid_created"]},"child":{"pid":row["codex_pid"],"created":row["codex_pid_created"]},
        "complete":false,"phase":"owned_collection_unconfirmed",
        "stdout":capture.as_ref().and_then(|c|c.stdout.lock().ok().map(|s|s.clone())),
        "stderr":capture.as_ref().and_then(|c|c.stderr.lock().ok().map(|s|s.clone())),
        "retained_reader_threads":readers.len(),"retained_input_thread":input.is_some(),"retained_process":true,
        "retained_job_clone":capture.is_some(),"retained_close_failures":native::retained_close_count(),"scorer_v11_complete":false});
    // Transfer every owner before any fallible IO/lock. Missing evidence cannot
    // drop an unreaped process or make the unknown state become successful.
    RETAINED_NATIVE_ATTEMPTS.lock().unwrap_or_else(|p|p.into_inner())
        .push(RetainedNativeAttempt{job_id:job.clone(),generation:generation.clone(),root_created:row["pid_created"].clone(),child_created:row["codex_pid_created"].clone(),process,capture,readers,input});
    let fact=save_json(&path,&value,MAX_RECEIPT)?;
    let _lock=context.job_lock(&job,"state.lock").map_err(|_|rejected())?;
    let mut current=context.load(&job).map_err(|_|rejected())?;
    if current["started_at"]!=generation{return Err(rejected());}
    current["native_export_pending"]=json!({"receipt":fact,"job_id":job,"started_at":generation,
        "receipt_relative":path.strip_prefix(context.job_dir(s(row,"id"))).ok().and_then(|p|p.to_str())});
    context.save(&current).map_err(|_|rejected())
}
fn retained_attempt_count()->usize {
    RETAINED_NATIVE_ATTEMPTS.lock().unwrap_or_else(|p|p.into_inner()).iter().map(|a|{
        let _=a.process.pid();let _=a.capture.as_ref().map(|c|&c.job_id);let _=a.readers.len();let _=a.input.is_some();1usize
    }).sum()
}

#[cfg(windows)]
pub(crate) fn close_owned_child(mut child:std::process::Child)->io::Result<()> {
    use std::os::windows::io::{IntoRawHandle,FromRawHandle,OwnedHandle};
    let mut error=None;
    for pipe in [child.stdin.take().map(IntoRawHandle::into_raw_handle),child.stdout.take().map(IntoRawHandle::into_raw_handle),child.stderr.take().map(IntoRawHandle::into_raw_handle)].into_iter().flatten(){
        let mut owner=CloseOnce::new(unsafe{File::from_raw_handle(pipe)},native::close_file);
        if let Err(close)=owner.close(){error=Some(match error{None=>close,Some(primary)=>io::Error::other(PrimaryAndCloseFailure{primary,cleanup:close})});}
    }
    let mut owner=CloseOnce::new(OwnedHandle::from(child),native::close_owned);
    finish_owned(&mut owner,error.map_or(Ok(()),Err))
}

pub(crate) fn has_retained_attempt(jid:&str)->bool {
    RETAINED_NATIVE_ATTEMPTS.lock().unwrap_or_else(|p|p.into_inner()).iter()
        .any(|attempt|attempt.job_id==jid)
}

// Called by the existing supervisor only while it owns this exact attempt.
// One finite drain slice preserves incomplete entries rather than detaching them.
pub(crate) fn drain_retained_attempts(context:&Context,jid:&str) {
    let mut selected=Vec::new();
    {let mut all=RETAINED_NATIVE_ATTEMPTS.lock().unwrap_or_else(|p|p.into_inner());let mut at=0;
        while at<all.len(){if all[at].job_id==jid{selected.push(all.remove(at));}else{at+=1;}}}
    for mut attempt in selected {
        let until=std::time::Instant::now()+std::time::Duration::from_millis(100);
        #[cfg(windows)]
        if let Some(capture)=&attempt.capture{let _=capture.reclaim_owned_children(until);}
        if attempt.capture.is_none()&&!matches!(attempt.process.poll(),Ok(Some(_))){let _=attempt.process.kill();}
        let ready=matches!(attempt.process.poll(),Ok(Some(_)))&&attempt.readers.iter().all(std::thread::JoinHandle::is_finished)&&
            attempt.input.as_ref().is_none_or(std::thread::JoinHandle::is_finished)&&{
                #[cfg(windows)]{attempt.capture.as_ref().is_none_or(Capture::owned_children_collected)}
                #[cfg(not(windows))]{false}
            };
        if !ready{RETAINED_NATIVE_ATTEMPTS.lock().unwrap_or_else(|p|p.into_inner()).push(attempt);continue;}
        #[cfg(windows)]
        if let (Some(capture),Some(child))=(attempt.capture.as_mut(),attempt.process.native_child_mut()){
            let _=capture.observe_child_wait(child);
        }
        let mut failed=false;
        for reader in attempt.readers.drain(..){if reader.join().is_err(){failed=true;}}
        if let Some(input)=attempt.input.take(){if !matches!(input.join(),Ok(Ok(()))){failed=true;}}
        #[cfg(windows)] if attempt.process.close_owned_process().is_err(){failed=true;}
        if let Some(capture)=attempt.capture.take(){
            match capture.finish(){Ok(finished)=>{if context.publish_native_export(finished).is_err(){failed=true;}},Err(_)=>failed=true}
        }
        if let Ok(_lock)=context.job_lock(jid,"state.lock")&&let Ok(mut row)=context.load(jid){
                if row["started_at"]!=attempt.generation||row["pid_created"]!=attempt.root_created||row["codex_pid_created"]!=attempt.child_created{continue;}
                row["native_retention_drained"]=json!(true);row["native_retention_drain_failed"]=json!(failed);
                // Preserve lost/failure and the original incomplete receipt. A
                // later drain is not a successful rerun of the business task.
                let _=context.save(&row);
        }
    }
}
