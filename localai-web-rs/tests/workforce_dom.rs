//! Exercises the retained index.html candidate, not the selected Prd page.
use std::process::Command;

#[test]
fn registered_department_counts_do_not_claim_actual_product_membership_or_pc_totals() {
    let html = include_str!("../src/index.html");
    let start = html.find("if(data&&data.workforce){").unwrap();
    let end = html[start..].find("if(data&&data.previousDataAt)").unwrap() + start;
    let rendering = &html[start..end];
    let script = format!(
        r#"
const assert=require('node:assert/strict');
const host={{textContent:''}},$=()=>host;
const opsTime=value=>value==null?'未確認':String(value),opsValue=opsTime;
const data={{metadataOk:true,sourceOk:false,protectionMode:'masked_metadata',departments:[{{products:[]}}],
 unassignedProducts:[{{workers:[{{id:'todo-88',priority:40,status:'waiting'}}]}}],
 workforce:{{processing:{{state:'partial',verifiedCount:1,total:null}},responsibility:{{productRoles:29}}}}}};
{rendering}
assert.match(host.textContent,/登録上の所属/);
assert.match(host.textContent,/登録0件は全PCの仕事0件を意味しません/);
assert.match(host.textContent,/元の製品への所属は未確認/);
assert.match(host.textContent,/指定外の登録.*別欄/);
assert.match(host.textContent,/一部取得/);
assert.match(host.textContent,/Native Codex・Claude 未観測/);
assert(!host.textContent.includes('全PC 0'));
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
fn acquisition_completion_and_worker_observation_use_their_distinct_clocks() {
    let html = include_str!("../src/index.html");
    let start = html.find("if(data&&data.workforce){").unwrap();
    let end = html[start..].find("if(data&&data.previousDataAt)").unwrap() + start;
    let rendering = &html[start..end];
    let script = format!(
        r#"
const assert=require('node:assert/strict');
const host={{textContent:''}},$=()=>host;
const opsTime=value=>value==null?'未確認':String(value),opsValue=opsTime;
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
{functions}
const row={{id:'todo-1',recordedStatus:'doing',status:'working',workState:'processing'}};
const data={{workforce:{{processing:{{validUntil:1000,verifiedCount:1,total:1,state:'complete'}}}},departments:[{{products:[{{workers:[row]}}]}}]}};
expireOpsEvidence(data,1000);assert.equal(data.workforce.processing.verifiedCount,1);
assert.match(opsCount([row]),/実作業 1件/);
expireOpsEvidence(data,1001);assert.equal(data.workforce.processing.verifiedCount,null);
assert.equal(data.workforce.processing.total,null);assert.equal(row.status,'unknown');
assert.equal(row.recordedStatus,'doing');assert.match(opsCount([row]),/実作業 0件 \/ 作業記録 1件/);
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
