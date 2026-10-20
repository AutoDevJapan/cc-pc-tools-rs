use std::process::Command;

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
