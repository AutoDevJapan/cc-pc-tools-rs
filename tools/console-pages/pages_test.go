package consolepages

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func page(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "localai-web-rs", "src", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func node(t *testing.T, script string) {
	t.Helper()
	input, err := json.Marshal(map[string]string{"index": page(t, "index.html"), "shell": page(t, "console_shell.html"), "workload": page(t, "workload.html")})
	if err != nil {
		t.Fatal(err)
	}
	prelude := `const assert=require('node:assert/strict'),vm=require('node:vm');
const input=JSON.parse(require('node:fs').readFileSync(0,'utf8'));
const line=(source,prefix)=>{const result=source.split('\n').find(x=>x.trimStart().startsWith(prefix));assert(result,'missing '+prefix);return result};
const between=(source,start,end)=>{const from=source.indexOf(start),to=source.indexOf(end,from);assert(from>=0&&to>from,'missing '+start+' / '+end);return source.slice(from,to)};
`
	cmd := exec.Command("node", "-e", prelude+script)
	windowless(cmd)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
}

func TestRetiredPagesAreAbsent(t *testing.T) {
	html := page(t, "index.html")
	for _, old := range []string{`id="view-chat"`, `id="view-minimax"`, `id="model-summary"`, `id="mini-services"`} {
		if strings.Contains(html, old) {
			t.Errorf("retired content still loaded: %s", old)
		}
	}
	if !strings.Contains(html, `id="view-kanban"`) {
		t.Fatal("Operations must remain available")
	}
}

func TestCompiledPageScriptSyntax(t *testing.T) {
	node(t, `for(const html of Object.values(input))for(const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g))new vm.Script(match[1]);`)
}

func TestNavigationKeepsDistinctProgressAndMusicLinks(t *testing.T) {
	node(t, `
const make=(tag,cls,text)=>({tagName:tag.toUpperCase(),dataset:{},textContent:text,href:null,removeAttribute(){},getAttribute(key){return this[key]}});
const original=['kanban','progress','workload','systems','music'].map(view=>{const node=make(['progress','music'].includes(view)?'a':'button','',view);node.dataset.view=view;node.href=view==='music'?'/music':view==='progress'?'/progress':null;return node});
const nav={querySelectorAll:()=>original,replaceChildren(...nodes){this.children=nodes}};
const ctx={make,nav};vm.createContext(ctx);vm.runInContext(line(input.shell,'const names=')+'\n'+between(input.shell,'const existing=','const toggle=')+'\n globalThis.links=links;',ctx);
assert.deepEqual(Array.from(ctx.links,n=>n.dataset.view),['kanban','progress','workload','systems','music']);assert.equal(new Set(ctx.links).size,5);assert.equal(ctx.links[1].href,'/progress');assert.equal(ctx.links[4].href,'/music');
`)
}

func TestWorkloadCancelsOldDateAndRejectsLateResponse(t *testing.T) {
	node(t, `
let visible=true,pending=[],rendered=[],messages=[];
const ctx={document:{hidden:false},root:()=>({}),active:()=>visible,status:message=>messages.push(message),render:data=>rendered.push(data),AbortController,setTimeout,clearTimeout,API:'/api/agent/workload',fetch:(path,options)=>new Promise(resolve=>pending.push({path,options,resolve})),window:{}};
vm.createContext(ctx);vm.runInContext("let timer=null,inFlight=false,lastData=null,selectedDate=null,controller=null,generation=0;\n"+between(input.workload,'function cancelWorkload(','window.refreshWorkload=')+"\nglobalThis.choose=value=>{selectedDate=value;cancelWorkload()};",ctx);
(async()=>{const old=ctx.refreshWorkload();assert.equal(pending.length,1);ctx.choose('2026-10-19');assert(pending[0].options.signal.aborted);const fresh=ctx.refreshWorkload();assert.equal(pending[1].path,'/api/agent/workload?date=2026-10-19');pending[0].resolve({ok:true,json:async()=>({ok:true,version:1,data:{date:'old'}})});await old;assert.equal(rendered.length,0);pending[1].resolve({ok:true,json:async()=>({ok:true,version:1,data:{date:'new'}})});await fresh;assert.deepEqual(rendered,[{date:'new'}]);visible=false;await ctx.refreshWorkload();ctx.document.hidden=true;visible=true;await ctx.refreshWorkload();assert.equal(pending.length,2)})().catch(e=>{console.error(e);process.exitCode=1});
`)
}

func TestSharedRequestsAbortAndDiscardLateProtectedContent(t *testing.T) {
	node(t, `
let reply,options,rendered=0;const updates=new Map();
const ctx={document:{hidden:false},musicBusy:new Set(),sharedControllers:new Map(),AbortController,setTimeout,clearTimeout,receive:(key,patch)=>updates.set(key,{...updates.get(key),...patch}),sharedResponse:(_key,value)=>value,fetch:(_path,opts)=>{options=opts;return new Promise(resolve=>reply=resolve)}};
vm.createContext(ctx);vm.runInContext(between(input.shell,'function stopShared(','function retrySource('),ctx);
(async()=>{const request=ctx.request('todo','/api/agent/display/todo',()=>rendered++);assert.equal(updates.get('todo').loading,true);ctx.stopShared();assert(options.signal.aborted);assert.equal(updates.get('todo').loading,false);reply({ok:true,json:async()=>({privateFixture:'late'})});await request;assert.equal(rendered,0);assert.equal(updates.get('todo').data,undefined);assert.equal(ctx.sharedControllers.size,0);assert.equal(ctx.musicBusy.size,0)})().catch(e=>{console.error(e);process.exitCode=1});
`)
}

func TestMusicRefreshPreservesDraftFocusAndAudioButRefreshesHiddenFields(t *testing.T) {
	node(t, `
const form=(fields)=>({fields,getAttribute:()=>'/music/drafts',querySelectorAll(){return this.fields}});
const oldTitle={name:'title',type:'text',value:'unfinished song',dataset:{consoleDirty:'true'}},oldSecret={name:'csrf',type:'hidden',value:'old',dataset:{consoleDirty:'true'}},oldAudio={getAttribute:()=>'/music/song/audio',currentTime:17};
const before={querySelectorAll:key=>key==='form'?[form([oldTitle,oldSecret])]:[oldAudio]};
const nextTitle={name:'title',type:'text',value:'server title',dataset:{}},freshSecret={name:'csrf',type:'hidden',value:'fresh',dataset:{}},nextAudio={getAttribute:()=>'/music/song/audio'};
const nextForm=form([nextTitle,freshSecret]),next={audio:[nextAudio],querySelectorAll(key){return key==='form'?[nextForm]:this.audio}};
nextTitle.replaceWith=node=>{nextForm.fields[0]=node};nextAudio.replaceWith=node=>{next.audio[0]=node};
const ctx={document:{activeElement:oldTitle}};vm.createContext(ctx);vm.runInContext(between(input.shell,'function musicFields(','document.addEventListener(\'input\''),ctx);
const focused=ctx.retainMusicState(before,next);assert.equal(focused,oldTitle);assert.equal(nextForm.fields[0],oldTitle);assert.equal(nextForm.fields[0].value,'unfinished song');assert.equal(nextForm.fields[1],freshSecret);assert.equal(nextForm.fields[1].value,'fresh');assert.equal(next.audio[0],oldAudio);assert.equal(next.audio[0].currentTime,17);
`)
}

func TestMusicRefreshFetchesOnlyOnDemandAndCancelsLateLoads(t *testing.T) {
	node(t, `
let requests=[],replaced=0;
const host={dataset:{musicLoaded:'true'},childNodes:[],setAttribute(){},removeAttribute(){},querySelectorAll:()=>[],contains:()=>false,replaceChildren(){replaced++}};
const content={childNodes:[],querySelectorAll:()=>[]};
const ctx={window:{},document:{addEventListener(){},importNode:node=>({...node})},q:()=>host,make:()=>({}),AbortController,setTimeout,clearTimeout,DOMParser:class{parseFromString(){return{querySelector:()=>content}}},fetch:(_path,options)=>new Promise(resolve=>requests.push({options,resolve}))};
vm.createContext(ctx);vm.runInContext(between(input.shell,'let musicLoading;','const names='),ctx);
(async()=>{await ctx.window.loadConsoleMusic();assert.equal(requests.length,0);const pending=ctx.window.loadConsoleMusic({force:true});assert.equal(requests.length,1);assert.equal(ctx.window.loadConsoleMusic({force:true}),pending);ctx.window.cancelConsoleMusic();assert(requests[0].options.signal.aborted);requests[0].resolve({ok:true,text:async()=>''});await pending;assert.equal(replaced,0);const next=ctx.window.loadConsoleMusic({force:true});requests[1].resolve({ok:true,text:async()=>''});await next;assert.equal(replaced,1);assert.equal(host.dataset.musicLoaded,'true')})().catch(e=>{console.error(e);process.exitCode=1});
`)
}

func TestProgressNavigation(t *testing.T) {
	node(t, `
const nodes=new Map(['kanban','progress','systems','workload','music'].map(view=>[view,{id:'view-'+view,dataset:{view},attrs:{},active:false,classList:{toggle(_,on){this.node.active=on}},setAttribute(k,v){this.attrs[k]=v},removeAttribute(k){delete this.attrs[k]}}]));for(const n of nodes.values())n.classList.node=n;
const title={focus(){this.focused=true}},subtitle={},events=[];let ops=0;
const ctx={URL,location:{href:'http://localhost:10012/'},$:s=>s==='#page-title'?title:s==='#page-subtitle'?subtitle:nodes.get(s.replace('#view-','')),$$:()=>[...nodes.values()],refreshOperations(){ops++},refresh(){},refreshCurrentPage(){},cancelPageFetches(){},renderProgress(){},CustomEvent:class{constructor(type,o){this.type=type;this.detail=o.detail}},window:{dispatchEvent:e=>events.push(e)}};
ctx.history={pushState(_a,_b,url){ctx.location.href=String(url)},replaceState(_a,_b,url){ctx.location.href=String(url)}};
vm.createContext(ctx);vm.runInContext(line(input.index,'function switchView('),ctx);
ctx.switchView('progress',{focus:true});assert.deepEqual([...nodes.values()].filter(n=>n.active).map(n=>n.dataset.view),['progress']);assert.equal(title.textContent,'作業進捗');assert(title.focused);assert.equal(new URL(ctx.location.href).pathname,'/progress');
ctx.switchView('systems');assert.equal(new URL(ctx.location.href).pathname,'/');assert.equal(new URL(ctx.location.href).searchParams.get('view'),'systems');
ctx.switchView('minimax');assert(nodes.get('kanban').active);assert(!nodes.get('music').active);
`)
}

func TestProgressExcludesWaitsAndExpiredEvidence(t *testing.T) {
	node(t, `
const ctx={Date,opsList:x=>Array.isArray(x)?x:[],opsState:{}};vm.createContext(ctx);vm.runInContext(between(input.index,'function processingRows(','function renderProgress('),ctx);
const row=(id,state)=>({id:'todo-'+id,primaryJob:'old-'+id,references:{sessions:['job-'+id]},workState:state});
const board={workforce:{processing:{state:'complete',validUntil:200}},departments:[{products:[{workers:[row(1,'processing'),row(2,'resource_wait'),row(3,'dependency_wait'),row(4,'decision_wait'),row(5,'execution_unverified'),row(6,'done'),row(1,'processing')]}]}]};
assert.deepEqual(Array.from(ctx.processingRows(board,100),x=>x.worker.id),['todo-1']);assert.equal(ctx.processingRows(board,201).length,0);board.workforce.processing.state='unavailable';assert.equal(ctx.processingRows(board,100).length,0);
`)
}

func TestFetchesBelongToTheVisiblePage(t *testing.T) {
	node(t, `
let view='systems',calls=[];
const ctx={document:{hidden:false},state:{},$:s=>s==='#view-systems'?{classList:{contains:()=>view==='systems'}}:{classList:{contains:()=>false}},publishConsoleSource(){},renderHealth(){},api:async(path)=>{calls.push(path);return{}},AbortController,setTimeout,clearTimeout};
vm.createContext(ctx);vm.runInContext(line(input.index,'let refreshInFlight=')+'\n'+between(input.index,'async function refresh(){','function esc('),ctx);
(async()=>{await ctx.refresh();assert.deepEqual(calls,['/api/health']);view='kanban';await ctx.refresh();assert.deepEqual(calls,['/api/health']);view='systems';ctx.document.hidden=true;await ctx.refresh();assert.equal(calls.length,1)})().catch(e=>{console.error(e);process.exitCode=1});
`)
}

func TestWorkloadNeverConvertsUnknownToZero(t *testing.T) {
	node(t, `
const ctx={};vm.createContext(ctx);vm.runInContext(line(input.workload,'function number('),ctx);
for(const value of [null,undefined,'',false,'invalid'])assert.equal(ctx.number(value),null);
assert.equal(ctx.number(0),0);assert.equal(ctx.number(12),12);vm.runInContext(line(input.workload,'function hasMeasurement('),ctx);assert.equal(ctx.hasMeasurement({collecting:true,updated_at:null}),false);assert.equal(ctx.hasMeasurement({measurement:{state:'unavailable'}}),false);assert.equal(ctx.hasMeasurement({measurement:{state:'complete'}}),true);assert.equal(ctx.hasMeasurement({updated_at:'2026-10-20T00:00:00Z',collecting:false}),true);
`)
}
