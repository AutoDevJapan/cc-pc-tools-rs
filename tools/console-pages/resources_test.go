package consolepages

import "testing"

const resourceFixture = `
const ctx={Date,Set,Map,Number,Math};vm.createContext(ctx);vm.runInContext(between(input.resources,'function resourceNumber(','function renderResources('),ctx);
const process=(pid,cpu,growth,window=300000)=>({pid,created:String(pid),name:'process-'+pid,cpuPercent:cpu,privateBytes:1073741824,fiveMinuteGrowthBytes:growth,fiveMinuteWindowMs:window});
const sample={schemaVersion:1,state:'ready',currentAvailable:true,sampledAt:1000,host:{cpuPercent:0,usedPhysicalBytes:1073741824,totalPhysicalBytes:2147483648},buildCounts:{executing:1,processing:1,waiting:1,unverified:1},builds:[{pid:1,created:'1',tool:'cargo.exe',state:'executing',elapsedSeconds:65,job:{product:'GoDD MCP α'}},{pid:2,created:'2',tool:'go.exe',state:'waiting'}],processes:[process(1,0,200),process(2,10,null,1000),process(3,2,500)]};
`

func TestResourcesDistinguishVerifiedWorkAndMissingMetrics(t *testing.T) {
	node(t, resourceFixture+`
const result=ctx.resourceModel(sample,1100,'cpu');assert.equal(result.active,2);assert.match(result.html,/0\.0%/);assert.match(result.html,/待機 1/);assert.match(result.html,/状態を確認できない処理 1/);assert.match(result.html,/GoDD MCP α/);assert.match(result.html,/蓄積中/);assert.match(result.html,/私有メモリ/);assert.match(result.html,/合算しません/);
sample.host.cpuPercent=null;assert.match(ctx.resourceModel(sample,1100).html,/取得できません/);
const stale=ctx.resourceModel(sample,47000);assert.equal(stale.active,null);assert(!stale.html.includes('0.0%'));assert.match(stale.html,/古い|取得できません/);
`)
}

func TestResourcesSortFiveMinuteMeasurementsAndEscapeLabels(t *testing.T) {
	node(t, resourceFixture+`
sample.builds[0].job.product='<img src=x onerror=bad>';sample.processes[0].name='<script>bad</script>';
const cpu=ctx.resourceModel(sample,1100,'cpu').html,growth=ctx.resourceModel(sample,1100,'growth').html;
assert(cpu.indexOf('process-2')<cpu.indexOf('process-3'));assert(growth.indexOf('process-3')<growth.indexOf('process-2'));
assert(!growth.includes('<img'));assert(growth.includes('&lt;img'));assert(!growth.includes('<script>bad'));
sample.processes[2].fiveMinuteWindowMs=600000;sample.processes[2].fiveMinuteGrowthBytes=999999999;const limited=ctx.resourceModel(sample,1100,'growth').html;assert(!limited.includes('+953.7 MiB'),'a ten-minute sample must not be relabelled as five minutes');
`)
}

const resourceLifecycle = `
let requests=[],html='',scrolls=[{scrollLeft:0},{scrollLeft:0}];const host={get innerHTML(){return html},set innerHTML(value){html=value;scrolls=[{scrollLeft:0},{scrollLeft:0}]},querySelectorAll:selector=>selector==='.resource-scroll'?scrolls:[],querySelector:()=>null};
const document={hidden:false,activeElement:null,querySelector:selector=>selector==='#systems-resources'?host:{classList:{contains:()=>true}},addEventListener(){}};
const ctx={Date,Map,Set,Number,Math,document,window:{addEventListener(){}},AbortController,setTimeout,clearTimeout,fetch:(_url,options)=>new Promise((resolve,reject)=>requests.push({options,resolve,reject}))};vm.createContext(ctx);vm.runInContext(input.resources.match(/<script>([\s\S]*?)<\/script>/)[1],ctx);
const sample=()=>({schemaVersion:1,state:'ready',sampledAt:Date.now(),currentAvailable:true,host:{cpuPercent:10},buildCounts:{executing:1,processing:1,waiting:0,unverified:0},processes:[],builds:[]});
`

func TestResourceRequestsAreSingleFlightAndCancelledResponsesStayDiscarded(t *testing.T) {
	node(t, resourceLifecycle+`
(async()=>{assert.equal(requests.length,1);const pending=ctx.window.refreshResources();assert.equal(ctx.window.refreshResources(),pending);assert.equal(requests.length,1);ctx.window.cancelResources();assert(requests[0].options.signal.aborted);requests[0].resolve({ok:true,json:async()=>sample()});await pending;assert.equal(host.innerHTML,'');const fresh=ctx.window.refreshResources();requests[1].resolve({ok:true,json:async()=>sample()});await fresh;assert(host.innerHTML.includes('<strong>2件</strong>'));const failed=ctx.window.refreshResources();requests[2].reject(new Error('offline'));await failed;assert(!host.innerHTML.includes('<strong>2件</strong>'));assert(host.innerHTML.includes('現在値を取得できません'));document.hidden=true;await ctx.window.refreshResources();assert.equal(requests.length,3)})().catch(error=>{console.error(error);process.exitCode=1});
`)
}

func TestResourceTableScrollSurvivesTheNextObservation(t *testing.T) {
	node(t, resourceLifecycle+`
(async()=>{const first=ctx.window.refreshResources();requests[0].resolve({ok:true,json:async()=>sample()});await first;scrolls[0].scrollLeft=80;scrolls[1].scrollLeft=150;const next=ctx.window.refreshResources();requests[1].resolve({ok:true,json:async()=>sample()});await next;assert.equal(scrolls[0].scrollLeft,80);assert.equal(scrolls[1].scrollLeft,150)})().catch(error=>{console.error(error);process.exitCode=1});
`)
}

func TestResourceFiveMinuteWindowDoesNotAcceptOutsideBoundaries(t *testing.T) {
	node(t, resourceFixture+`
for(const width of [299900,345100]){sample.processes=[process(1,0,1048576,width)];assert(ctx.resourceModel(sample,1100,'growth').html.includes('蓄積中'))}
for(const width of [300000,345000]){sample.processes=[process(1,0,1048576,width)];assert(ctx.resourceModel(sample,1100,'growth').html.includes('+1.0 MiB'))}
`)
}
