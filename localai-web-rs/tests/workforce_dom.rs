//! Exercises the Operations functions from the selected Console page.
use std::process::Command;

#[test]
fn acquisition_completion_and_worker_observation_use_their_distinct_clocks() {
    let html = include_str!("../src/index.html");
    let start = html.find("if(data&&data.workforce){").unwrap();
    let end = html[start..].find("if(data&&data.previousDataAt)").unwrap() + start;
    let rendering = &html[start..end];
    let helper_start = html.find("function opsProcessingSummary(").unwrap();
    let helper_end = html[helper_start..]
        .find("function renderProgress(")
        .unwrap()
        + helper_start;
    let helper = &html[helper_start..helper_end];
    let script = format!(
        r#"
const assert=require('node:assert/strict');
const host={{textContent:''}},$=()=>host;
const opsTime=value=>value==null?'未確認':String(value),opsValue=opsTime;
const opsList=value=>Array.isArray(value)?value:[];
{helper}
const data={{workforce:{{acquisition:{{startedAt:1000,completedAt:3000}},processing:{{startedAt:1500,observedAt:2500,state:'complete'}},responsibility:{{productRoles:29}}}}}};
{rendering}
assert.match(host.textContent,/取得開始 1000/);
assert.match(host.textContent,/取得完了 3000/);
assert.match(host.textContent,/担当観測 2500/);
assert(!host.textContent.includes('取得完了 2500'));
"#
    );
    let mut command = Command::new("node");
    command.args(["-e", &script]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let result = command.output().unwrap();
    assert!(
        result.status.success(),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
}

#[test]
fn expired_proof_is_not_displayed_as_processing_and_wait_filters_remain_distinct() {
    let html = include_str!("../src/index.html");
    let functions = [
        &html[html.find("function expireOpsEvidence(").unwrap()
            ..html.find("function opsKey(").unwrap()],
        &html
            [html.find("function opsWaitKind(").unwrap()..html.find("function opsShort(").unwrap()],
        &html[html.find("function opsCount(").unwrap()
            ..html.find("function expireOpsEvidence(").unwrap()],
    ]
    .join("\n");
    let script = format!(
        r#"
const assert=require('node:assert/strict');
const opsList=v=>Array.isArray(v)?v:[];
let now=1000;Date.now=()=>now;
const opsState={{snapshot:null}};
{functions}
const row={{id:'todo-1',recordedStatus:'doing',status:'working',workState:'processing',references:{{sessions:['live-worker']}}}};
const data={{workforce:{{processing:{{validUntil:1000,verifiedCount:1,total:1,state:'complete',sessions:[{{key:'live-worker',validUntil:1000}}]}}}},departments:[{{products:[{{workers:[row]}}]}}]}};
opsState.snapshot=data;
expireOpsEvidence(data,1000);assert.equal(data.workforce.processing.verifiedCount,1);
assert.match(opsCount([row]),/確認した実作業 1件 \/ 作業記録 1件/);
now=1001;
expireOpsEvidence(data,1001);assert.equal(data.workforce.processing.verifiedCount,null);
assert.equal(data.workforce.processing.total,null);assert.equal(row.status,'unknown');
assert.equal(row.recordedStatus,'doing');assert.equal(row.references.sessions.length,0);
assert.match(opsCount([row]),/実作業は未確認 \/ 作業記録 1件/);
for(const state of ['dependency_wait','resource_wait','decision_wait','job_queued','intake_wait','recovery_unverified']){{
 const task={{workState:state,status:'waiting'}};
 assert(opsMatchesStatus(task,state));
 assert(!opsMatchesStatus(task,'working'));
 assert(!opsMatchesStatus(task,state==='resource_wait'?'decision_wait':'resource_wait'));
}}
"#
    );
    let mut command = Command::new("node");
    command.args(["-e", &script]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let result = command.output().unwrap();
    assert!(
        result.status.success(),
        "{}",
        String::from_utf8_lossy(&result.stderr)
    );
}
