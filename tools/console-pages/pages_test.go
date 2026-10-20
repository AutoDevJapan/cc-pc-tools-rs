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
const row=(id,state)=>({id:'todo-'+id,primaryJob:'job-'+id,workState:state});
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
