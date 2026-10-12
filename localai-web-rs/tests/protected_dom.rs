use std::process::Command;

fn function<'a>(html: &'a str, start: &str, next: &str) -> &'a str {
    let text = &html[html.find(start).expect("UI function exists")..];
    &text[..text.find(next).expect("next UI function exists")]
}

#[test]
fn protected_transition_erases_focused_content_and_pending_details() {
    let html = include_str!("../src/index.html");
    let functions = [
        function(
            html,
            "function renderOpsQuestions(data)",
            "function renderOpsDepartments",
        ),
        function(
            html,
            "function applyOpsProtection(snapshot)",
            "async function refreshOperations",
        ),
        function(
            html,
            "async function loadOpsDetail(worker",
            "function openOpsDetail",
        ),
    ]
    .join("\n");
    let prelude = r#"
const assert = require('node:assert/strict');
const hosts = new Map(['ops-questions','ops-departments','ops-dialog-body'].map(id => [id, {
    innerHTML:'fixture-private-content', hidden:false,
    contains(){return true}, replaceChildren(){this.innerHTML=''}
}]));
const $ = selector => hosts.get(selector.slice(1));
const document = {activeElement:{}};
const window = {getSelection(){return {isCollapsed:false,anchorNode:{}}}};
const opsState = {snapshot:{protectionMode:'normal'}, details:new Map([['old',{data:{note:'fixture-private-note'}}]]),
    items:new Map([['old',{task:'fixture-private-task'}]]), detailController:null, selected:'old'};
let closed = false, renderCount = 0, resolveResponse;
const closeOpsDetail = () => {closed=true};
const renderOpsDetail = () => {renderCount++};
const api = () => new Promise(resolve => {resolveResponse=resolve});
    "#;
    let assertions = r#"
(async () => {
    renderOpsQuestions({protectionMode:'masked_metadata'});
    assert.equal($('#ops-questions').innerHTML,'');
    assert.equal($('#ops-questions').hidden,true);
    const worker = {id:'new',detailAvailable:true,detailVersion:'v1'};
    const request = loadOpsDetail(worker);
    const pending = opsState.details.get(worker.id);
    applyOpsProtection({protectionMode:'masked_metadata'});
    opsState.snapshot={protectionMode:'masked_metadata'};
    assert.equal(opsState.details.size,0);
    assert.equal(opsState.items.size,0);
    assert.equal($('#ops-departments').innerHTML,'');
    assert.equal($('#ops-dialog-body').innerHTML,'');
    assert.equal(closed,true);
    resolveResponse({ok:true,id:'new',history:[],note:'fixture-private-note',nextOffset:null,detailVersion:'v1'});
    await request;
    assert.equal(pending.data,null);
    assert.equal(opsState.details.size,0);
    console.log('protected transition: focused DOM, caches and late response cleared');
})().catch(error => {console.error(error);process.exitCode=1});
    "#;
    let script = [prelude, &functions, assertions].join("\n");
    let mut command = Command::new("node");
    command.args(["-e", &script]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let output = command
        .output()
        .expect("installed Node.js runs the UI regression");
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
}
