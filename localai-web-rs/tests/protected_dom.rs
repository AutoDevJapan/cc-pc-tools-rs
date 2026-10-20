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
const hosts = new Map(['ops-questions','ops-departments','ops-dialog-body','ops-unassigned-list'].map(id => [id, {
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
    assert.equal($('#ops-unassigned-list').innerHTML,'');
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

#[test]
fn product_rows_remain_collapsed_and_keep_owner_order_after_refresh() {
    let html = include_str!("../src/index.html");
    let render = function(
        html,
        "function renderOpsDepartments",
        "function renderOpsDetail",
    );
    let prelude = r#"
const assert = require('node:assert/strict');
const host = {innerHTML:''},results={textContent:''},search={value:''},status={value:''};
const $ = selector => ({'#ops-departments':host,'#ops-results':results,'#ops-search':search,'#ops-status':status})[selector];
const document={activeElement:null},matchMedia=()=>({matches:false});
const opsList=x=>Array.isArray(x)?x:[],opsWorkers=d=>opsList(d.products).flatMap(p=>opsList(p.workers));
const esc=x=>String(x).replaceAll('&','&amp;').replaceAll('"','&quot;').replaceAll('<','&lt;');
const unesc=x=>x.replaceAll('&quot;','"').replaceAll('&lt;','<').replaceAll('&amp;','&');
const opsKey=(...parts)=>JSON.stringify(parts),opsColor=()=> '#3B73C4';
const opsRobot=()=>'',opsCount=rows=>String(rows.length),opsIsOpen=w=>w.status==='working';
const opsPriority=()=>0,opsMatchesStatus=()=>true,opsDependencies=()=>'',opsShort=x=>x;
const opsProgress=()=>'',opsStatus=x=>x,opsFacts=()=>'',opsPreview=()=>'',opsNumber=x=>Number.isFinite(x)?x:null,opsDecimal=x=>String(x);
const opsRestoreBoardFocus=()=>{},openOpsDetail=()=>{};
let productNodes=[];
const $$=selector=>{
  if(selector==='[data-op-product]'){
    productNodes=[...host.innerHTML.matchAll(/<details class="ops-product" data-op-product="([^"]*)"\s*(open)?/g)].map(m=>{
      const node={dataset:{opProduct:unesc(m[1])},open:Boolean(m[2]),handler:null};
      node.querySelector=()=>({addEventListener(_,handler){node.handler=handler}});return node;
    });return productNodes;
  } return [];
};
const opsState={snapshot:{metrics:{}},filter:'',filterProducts:new Map(),filterDepartments:new Map(),products:new Map(),expanded:new Map(),limits:new Map(),items:new Map(),selected:null};
const names=['GoDD 13a13e1','GoDD AO','GoDD Agent','GoDD Creators','GoDD Genesis','GoDD MCP α','GoDD Press','GoDD Rosetta','GoDD Virtual Streamer'];
const departments=[{id:'department-1',name:'GoDD事業部',products:names.map((name,index)=>({id:'p'+index,name,workers:[{id:'todo-'+index,name:'作業',task:'確認',status:index===7?'working':'waiting'}]}))}];
opsState.snapshot.departments=departments;
    "#;
    let assertions = r#"
renderOpsDepartments(departments);
assert.equal(productNodes.length,9);
assert(productNodes.every(node=>!node.open),'Every product must start collapsed');
const positions=names.map(name=>host.innerHTML.indexOf('<h4>'+name+'</h4>'));
assert(positions.every((position,index)=>index===0||position>positions[index-1]),'Activity must not change owner order');
productNodes[1].handler({preventDefault(){}});
assert.equal(productNodes.filter(node=>node.open).length,1);
assert.equal(productNodes[1].open,true);
assert.equal(opsState.items.size,1);
departments[0].products[8].workers[0].status='working';
renderOpsDepartments(departments);
assert.equal(productNodes[1].open,true,'Refresh must preserve the chosen fold state');
assert.equal(productNodes.filter(node=>node.open).length,1);
console.log('Owner order and fold state retained');
    "#;
    let script = [prelude, render, assertions].join("\n");
    let mut command = Command::new("node");
    command.args(["-e", &script]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let output = command
        .output()
        .expect("installed Node.js runs the rendering regression");
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
}
