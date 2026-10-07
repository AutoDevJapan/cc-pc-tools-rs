import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { addBookmark, canonicalUrl, poll, BASE, FOLDERS } from '../extension/core.js';

function entry(url='https://github.com/example/tool') {
  return { id:createHash('sha256').update(url).digest('hex'), url, name:'Tool – 開発を補助する', folder:'開発ライブラリ・インフラ' };
}
function mock() {
  const root={id:'0',children:[{id:'1',folderType:'bookmarks-bar',children:[{id:'b',title:'ビジネス',children:[{id:'s',title:'副業',parentId:'b',children:[]}]}]},{id:'2',title:'other',children:[]}]};
  const nodes=()=>{const walk=n=>[n,...(n.children??[]).flatMap(walk)];return walk(root)};
  const calls=[];
  return {root,calls,side:root.children[0].children[0].children[0], api:{
    async getTree(){return structuredClone([root])},
    async getChildren(id){return structuredClone(nodes().find(n=>n.id===id)?.children??[])},
    async get(id){return structuredClone(nodes().filter(n=>n.id===id))},
    async create(details){calls.push(details);const parent=nodes().find(n=>n.id===details.parentId);assert.ok(parent);const n={...details,id:`new${calls.length}`,...(!details.url?{children:[]}:{})};parent.children.push(n);return structuredClone(n)}
  }};
}
test('creates category and bookmark only within side folder; replay returns exists',async()=>{
  const m=mock();const result=await addBookmark(m.api,entry());assert.equal(result.status,'created');
  assert.deepEqual(m.calls,[{parentId:'s',title:entry().folder},{parentId:'new1',title:entry().name,url:entry().url}]);
  assert.equal((await addBookmark(m.api,entry())).status,'exists');assert.equal(m.calls.length,2);
});
test('reuses category and leaves unrelated folders untouched',async()=>{
  const m=mock();m.side.children.push({id:'cat',parentId:'s',title:entry().folder,children:[]});
  const outside=structuredClone(m.root.children[1]);await addBookmark(m.api,entry());assert.equal(m.calls[0].parentId,'cat');assert.deepEqual(m.root.children[1],outside);
});
test('same repository URL with case, git suffix, slash or query is not duplicated anywhere',async()=>{
  for(const url of ['https://github.com/Example/Tool.git/','https://github.com/example/tool?tab=readme-ov-file#usage']){
    const m=mock();m.root.children[1].children.push({id:'already',url});assert.deepEqual(await addBookmark(m.api,entry()),{id:entry().id,status:'exists',bookmark_id:'already'});assert.equal(m.calls.length,0);
  }
});
test('missing or ambiguous side path never writes',async()=>{
  for(const change of [m=>m.side.title='別フォルダ',m=>m.root.children[0].children[0].children.push(structuredClone(m.side)),m=>m.root.children[0].folderType='other',m=>m.side.unmodifiable='managed']){
    const m=mock();change(m);await assert.rejects(addBookmark(m.api,entry()));assert.equal(m.calls.length,0);
  }
});
test('ambiguous and managed categories never receive writes',async()=>{
  for(const children of [[{id:'x',title:entry().folder,unmodifiable:'managed'}],[{id:'x',title:entry().folder},{id:'y',title:entry().folder}]]){
    const m=mock();m.side.children=children;await assert.rejects(addBookmark(m.api,entry()));assert.equal(m.calls.length,0);
  }
});
test('moved category is rejected just before bookmark creation',async()=>{
  const m=mock();m.side.children.push({id:'cat',title:entry().folder,parentId:'s',children:[]});const original=m.api.get;
  m.api.get=async id=>{const out=await original(id);out[0].parentId='2';return out};
  await assert.rejects(addBookmark(m.api,entry()),/外/);assert.equal(m.calls.length,0);
});
test('tampered queue ID, unsafe URL, invalid folder or title never writes',async()=>{
  for(const changed of [{id:'a'.repeat(64)},{url:'https://evil.example/x/y'},{folder:'../other'},{name:'Tool'},{name:'Tool – bad\nname'}]){
    const m=mock();await assert.rejects(addBookmark(m.api,{...entry(),...changed}));assert.equal(m.calls.length,0);
  }
});
test('bookmark API failure sends no success receipt and next poll retries',async()=>{
  const m=mock();const create=m.api.create;m.api.create=async()=>{throw Error('Chrome error')};const receipts=[];
  const fetcher=async(url,opts)=>{assert.ok(url.startsWith(BASE));assert.equal(opts.headers.Authorization,'Bearer test');assert.equal(opts.redirect,'error');if(url.endsWith('/pending'))return {ok:true,json:async()=>({entries:[entry()]})};receipts.push(JSON.parse(opts.body));return {ok:true,json:async()=>({ok:true})}};
  await poll(m.api,'test',fetcher);assert.equal(receipts.length,0);m.api.create=create;await poll(m.api,'test',fetcher);assert.equal(receipts[0].status,'created');
});
test('lost acknowledgement is replayed as exists without another bookmark',async()=>{
  const m=mock();let fail=true;const receipts=[];
  const fetcher=async(url,opts)=>{if(url.endsWith('/pending'))return {ok:true,json:async()=>({entries:[entry()]})};receipts.push(JSON.parse(opts.body));if(fail)throw Error('disconnected');return {ok:true,json:async()=>({ok:true})}};
  await poll(m.api,'test',fetcher);fail=false;await poll(m.api,'test',fetcher);assert.deepEqual(receipts.map(r=>r.status),['created','exists']);assert.equal(m.calls.length,2);
});
test('unauthorized server and malformed batch never writes',async()=>{
  for(const response of [{ok:false,status:401},{ok:true,json:async()=>({entries:Array(33).fill(entry())})}]){const m=mock();await assert.rejects(poll(m.api,'test',async()=>response));assert.equal(m.calls.length,0)}
});
test('permissions and CSP restrict network to fixed loopback port',async()=>{
  const manifest=JSON.parse(await readFile(new URL('../extension/manifest.json',import.meta.url),'utf8'));
  assert.deepEqual(manifest.permissions,['bookmarks']);assert.deepEqual(manifest.host_permissions,[BASE+'/*']);assert.ok(manifest.content_security_policy.extension_pages.includes('connect-src '+BASE));assert.equal(manifest.manifest_version,3);assert.equal(FOLDERS.length,22);
});
test('URL canonicalization rejects non-repository destinations',()=>{
  assert.equal(canonicalUrl('https://github.com/Example/Tool.git/#readme'),'https://github.com/example/tool');
  for(const url of ['http://github.com/a/b','https://github.com/a/b/tree/main','https://x@github.com/a/b','https://github.com:8080/a/b','https://github.com/a/.git'])assert.throws(()=>canonicalUrl(url));
});
