package consolepages

import "testing"

func TestNormalZeroProgressIsSimpleAndOneSourceFailureIsSpecific(t *testing.T) {
	node(t, `
const hosts=new Map(),host=id=>{if(!hosts.has(id))hosts.set(id,{textContent:'',innerHTML:'',contains:()=>false,querySelectorAll:()=>[],insertAdjacentHTML(){}});return hosts.get(id)};
const proof={state:'complete',validUntil:200,observedAt:100,verifiedCount:0,sessions:[],scope:'connected_sessions',collectionIssues:[]};
const ctx={Date:{now:()=>100},Map,Set,document:{activeElement:{}},opsState:{snapshot:{workforce:{processing:proof},departments:[],unassignedProducts:[]},items:new Map(),loading:false,error:''},opsNumber:v=>typeof v==='number'?v:null,opsList:v=>Array.isArray(v)?v:[],opsTime:()=> '確認時刻',esc:String,$:host};
vm.createContext(ctx);vm.runInContext(between(input.index,'function processingRows(','function switchView('),ctx);
ctx.renderProgress();assert.match(host('#progress-update').textContent,/稼働0件/);assert.match(host('#progress-jobs').innerHTML,/該当する処理はありません/);assert(!host('#progress-update').textContent.includes('一部'));
proof.state='partial';proof.collectionIssues=[{kind:'native',reason:'read_failed',lastSuccessAt:80}];ctx.renderProgress();assert.match(host('#progress-update').textContent,/会話中の担当.*取得できません/);assert(!host('#progress-update').textContent.includes('一部を取得'));
proof.validUntil=90;ctx.renderProgress();assert(!host('#progress-update').textContent.includes('稼働0件'),'stale proof must not become a fresh zero');
`)
}

func TestResourceNormalEmptyBuildListIsAZeroRatherThanUnobserved(t *testing.T) {
	node(t, resourceFixture+`
sample.builds=[];sample.buildCounts={observed:0,processing:0,executing:0,waiting:0,unverified:0,truncated:false};
const view=ctx.resourceModel(sample,1100);assert.equal(view.active,0);assert.match(view.html,/ビルド0件/);assert(!view.html.includes('実行・CPU処理を確認 · 待機 0 · 未確認 0'));
sample.currentAvailable=false;const failure=ctx.resourceModel(sample,1100);assert.equal(failure.active,null);assert(!failure.html.includes('ビルド0件'));
`)
}

func TestProtectedTextDoesNotMakeHealthyExecutionCollectionFail(t *testing.T) {
	node(t, `
const hosts=new Map(),get=id=>{if(!hosts.has(id))hosts.set(id,{textContent:'',innerHTML:'',hidden:true});return hosts.get(id)};
const data={metadataOk:true,sourceOk:false,protectionMode:'masked_metadata',fetchedAt:100,sources:[{key:'privacy',label:'作業本文',ok:false},...['todos','jobs','scheduler','commander'].map(key=>({key,label:key==='jobs'?'担当の実行状況':key,ok:true}))],workforce:{processing:{state:'complete',validUntil:200,verifiedCount:0,observedAt:100,bySource:{nativeToolReceipts:0}},acquisition:{}}};
const ctx={data,metric:{},Date:{now:()=>100},Set,opsState:{error:'',receivedAt:100},opsList:v=>Array.isArray(v)?v:[],opsValue:v=>v==null?'—':String(v),opsTime:String,esc:String,$:get};vm.createContext(ctx);
vm.runInContext(between(input.index,'function opsProcessingSummary(','function renderProgress('),ctx);
const render=between(input.index,'const sources=opsList(data&&data.sources)','const commander=data&&data.commander;');
vm.runInContext(render,ctx);assert.equal(get('#ops-source-summary').textContent,'4 / 4 取得');assert(!get('#ops-scope').textContent.includes('一部取得'));assert.match(get('#ops-error').textContent,/本文は保護中/);assert(!get('#ops-error').textContent.includes('担当の実行状況を取得できません'));
data.sources.find(s=>s.key==='jobs').ok=false;vm.runInContext('{'+render+'}',ctx);assert.equal(get('#ops-source-summary').textContent,'3 / 4 取得');assert.match(get('#ops-error').textContent,/担当の実行状況を取得できません/);
`)
}
