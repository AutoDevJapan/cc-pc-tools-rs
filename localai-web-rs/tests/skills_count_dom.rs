use std::process::Command;

#[test]
fn skills_summary_distinguishes_zero_errors_and_partial_results_beside_jobs() {
    let html = include_str!("../src/index.html");
    let summary = &html[html.find("function opsSkillsSummary(").unwrap()
        ..html.find("function renderOperations(").unwrap()];
    let rendering = &html[html.find("const metric=data&&data.metrics||{};").unwrap()
        ..html.find("renderOpsQuestions(data);").unwrap()];
    let script = format!(
        r#"
const assert=require('node:assert/strict');
const esc=value=>String(value).replace(/[&<>"']/g,c=>({{'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}}[c]));
const opsTime=at=>new Date(at).toISOString();
const opsValue=value=>value===null||value===undefined?'—':String(value);
{summary}
assert.match(opsSkillsSummary({{state:'complete',count:0}}).value,/^0 /);
assert.equal(opsSkillsSummary({{state:'partial',count:0}}).value,'一部未取得');
assert.equal(opsSkillsSummary({{state:'unavailable',count:0}}).value,'取得失敗');
for(const count of [null,undefined,'0',-1,0.5,Infinity,NaN]){{
  assert.equal(opsSkillsSummary({{state:'complete',count}}).value,'未取得');
}}
assert.equal(opsSkillsSummary(null).value,'未取得');
assert.equal(opsSkillsSummary({{state:'complete',count:12,checkedAt:1000}}).caption,'確認 1970-01-01T00:00:01.000Z');
function renderSummary(protectionMode){{
  const data={{protectionMode,metrics:{{activeJobs:2,jobLimit:8}},skillsInventory:{{state:'complete',count:1234,checkedAt:1000}}}};
  const all=[],host={{innerHTML:''}};
  const $=()=>host;
  {rendering}
  return host.innerHTML;
}}
for(const mode of ['normal','masked_metadata']){{
  const result=renderSummary(mode);
  assert.match(result,/<div class="ops-metric-pair"><div class="ops-metric "><span>記録上の実行ジョブ \/ 起動枠<\/span>/);
  assert.match(result,/<\/strong><\/div><div class="ops-metric "><span>Skills<\/span><strong>1,234 <small>件<\/small><\/strong>/);
  assert.match(result,/確認 1970-01-01T00:00:01.000Z/);
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
    let output = command
        .output()
        .expect("Node.js runs the frontend regression");
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
}
