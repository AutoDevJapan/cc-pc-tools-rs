package consolepages

import "testing"

const musicControlsFixture = `
const field=(name,type,value,dirty=false)=>({name,type,value,tagName:type.startsWith('select')?'SELECT':'INPUT',checked:false,dataset:dirty?{consoleDirty:'true'}:{}});
const form=(action,fields)=>{const node={fields,getAttribute:key=>key==='action'?action:key==='method'?'post':null,querySelectorAll:()=>node.fields};for(const item of fields)item.replaceWith=replacement=>{node.fields[node.fields.indexOf(item)]=replacement};return node};
const host=forms=>({querySelectorAll:selector=>selector==='form'?forms:[]});
const ctx={document:{activeElement:null}};vm.createContext(ctx);vm.runInContext(between(input.shell,'function musicFields(',"document.addEventListener('input'"),ctx);
`

func TestReview3121CandidateControlsKeepTheirFreshIdentity(t *testing.T) {
	node(t, musicControlsFixture+`
const oldA=field('candidate','radio','A',true),oldB=field('candidate','radio','B',true);oldB.checked=true;
const freshB=field('candidate','radio','B'),freshC=field('candidate','radio','C'),freshA=field('candidate','radio','A');
const next=form('/music/select',[freshB,freshC,freshA]);
ctx.retainMusicState(host([form('/music/select',[oldA,oldB])]),host([next]));
assert.equal(next.fields[0],freshB,'a refreshed candidate must keep its own server value');
assert.equal(next.fields[1],freshC,'a new candidate must not be replaced by an old control');
assert.equal(next.fields[2],freshA);assert.equal(freshB.checked,true);assert.equal(freshA.checked,false);
assert.deepEqual(next.fields.map(x=>x.value),['B','C','A']);
`)
}

func TestReview3121SelectKeepsNewOptionsAndExistingChoice(t *testing.T) {
	node(t, musicControlsFixture+`
const old=field('voice','select-one','B',true);old.options=[{value:'A'},{value:'B'}];
const fresh=field('voice','select-one','C');fresh.options=[{value:'B'},{value:'C'}];
ctx.document.activeElement=old;const next=form('/music/options',[fresh]);
const focused=ctx.retainMusicState(host([form('/music/options',[old])]),host([next]));
assert.equal(next.fields[0],fresh,'the options returned by the server must remain available');
assert.deepEqual(fresh.options.map(x=>x.value),['B','C']);assert.equal(fresh.value,'B');assert.equal(focused,fresh);
const removed=field('voice','select-one','C');removed.options=[{value:'C'}];
ctx.retainMusicState(host([form('/music/options',[old])]),host([form('/music/options',[removed])]));
assert.equal(removed.value,'C','an obsolete choice must not replace a fresh valid default');
`)
}

func TestReview3121DraftSurvivesUnrelatedFormInsertion(t *testing.T) {
	node(t, musicControlsFixture+`
const old=field('title','text','unfinished draft',true),fresh=field('title','text','server value');ctx.document.activeElement=old;
const next=form('/music/drafts',[fresh]);
const focused=ctx.retainMusicState(host([form('/music/drafts',[old])]),host([form('/music/refresh',[]),next]));
assert.equal(next.fields[0].value,'unfinished draft');assert.equal(focused,next.fields[0]);
`)
}

func TestReview3121WorkloadObservationDatesSurviveFailure(t *testing.T) {
	node(t, `
class Element{constructor(tag,cls='',text=''){this.tagName=tag.toUpperCase();this.className=cls;this.textContent=text;this.children=[];this.dataset={};this.style={}}append(...nodes){this.children.push(...nodes)}prepend(node){this.children.unshift(node)}replaceChildren(...nodes){this.children=nodes}setAttribute(key,value){this[key]=value}get lastChild(){return this.children.at(-1)}get childNodes(){return this.children}}
const host=new Element('section');const walk=node=>[node,...node.children.flatMap(walk)],text=node=>[node.textContent,...node.children.map(text)].join(' ');
const document={hidden:false,querySelectorAll:()=>[],createTextNode:value=>new Element('text','',value)};
const ctx={document,ROOT:'#view-workload',root:()=>host,active:()=>true,$:selector=>walk(host).find(x=>'.'+x.className===selector),el:(tag,cls,value)=>new Element(tag,cls,value),Date,Set,Number,URL,Math,AbortController,setTimeout,clearTimeout,API:'/api/agent/workload',window:{},fetch:async()=>{throw new Error('offline')}};
vm.createContext(ctx);vm.runInContext('let timer=null,inFlight=false,lastData=null,selectedDate=null,controller=null,generation=0;\n'+between(input.workload,'function status(','function cancelWorkload(')+between(input.workload,'function cancelWorkload(','window.refreshWorkload='),ctx);
const sample={date:'2026-10-21',updated_at:'2026-10-21T09:16:00+09:00',collecting:false,measurement:{state:'partial',kind:'processing_observations',first_at:'2026-10-21T09:00:00+09:00',last_at:'2026-10-21T09:15:00+09:00'},totals:{active_ms:15000,jobs:1},projects:[],timeline:[],history:[],warnings:[]};
ctx.render(sample);for(const value of [sample.measurement.first_at,sample.measurement.last_at,sample.updated_at])assert(text(host).includes(value),'the observed range and last confirmation must be visible');
(async()=>{await ctx.refreshWorkload();assert(text(host).includes('offline'));for(const value of [sample.measurement.first_at,sample.measurement.last_at,sample.updated_at])assert(text(host).includes(value),'fetch failure must retain the last observed timestamps')})().catch(error=>{console.error(error);process.exitCode=1});
`)
}
