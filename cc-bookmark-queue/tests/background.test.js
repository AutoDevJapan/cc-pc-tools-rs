import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { readFile } from 'node:fs/promises';

test('worker starts immediately and heartbeat continues while a poll is blocked', async()=>{
  const source=(await readFile(new URL('../extension/background.js',import.meta.url),'utf8')).replace(/^import .*;\r?\n/gm,'');
  const handlers={};let timer;let calls=0;let heartbeats=0;let release;
  const context={TOKEN:'test',chrome:{bookmarks:{},runtime:{getPlatformInfo:async()=>{heartbeats++},onInstalled:{addListener:f=>handlers.install=f},onStartup:{addListener:f=>handlers.start=f}}},setInterval:(f,ms)=>{assert.equal(ms,20000);timer=f},poll:()=>{calls++;return new Promise(resolve=>{release=resolve})},console:{warn:()=>{}}};
  vm.runInNewContext(source,context);await new Promise(setImmediate);assert.equal(calls,1);
  timer();handlers.start();handlers.install();await new Promise(setImmediate);assert.equal(heartbeats,4);assert.equal(calls,1);
  release();await new Promise(setImmediate);timer();await new Promise(setImmediate);assert.equal(calls,2);release();
});
test('server outage releases polling guard and next tick retries',async()=>{
  const source=(await readFile(new URL('../extension/background.js',import.meta.url),'utf8')).replace(/^import .*;\r?\n/gm,'');let timer;let calls=0;let warnings=0;
  const context={TOKEN:'test',chrome:{bookmarks:{},runtime:{getPlatformInfo:async()=>{},onInstalled:{addListener:()=>{}},onStartup:{addListener:()=>{}}}},setInterval:f=>{timer=f},poll:async()=>{calls++;throw Error('server offline')},console:{warn:()=>warnings++}};
  vm.runInNewContext(source,context);await new Promise(setImmediate);timer();await new Promise(setImmediate);assert.equal(calls,2);assert.equal(warnings,2);
});
