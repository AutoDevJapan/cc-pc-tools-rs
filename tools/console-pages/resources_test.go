package consolepages

import "testing"

const resourceFixture = `
const ctx={Date,Set,Map,Number,Math};vm.createContext(ctx);vm.runInContext(between(input.resources,'function resourceNumber(','function renderResources('),ctx);
const process=(pid,cpu,growth,window=300000)=>({pid,created:String(pid),name:'process-'+pid,cpuPercent:cpu,privateBytes:1073741824,fiveMinuteGrowthBytes:growth,fiveMinuteWindowMs:window});
const sample={schemaVersion:1,state:'ready',currentAvailable:true,sampledAt:1000,host:{cpuPercent:0,usedPhysicalBytes:1073741824,totalPhysicalBytes:2147483648},buildCounts:{executing:1,processing:1,waiting:1,unverified:1},builds:[{pid:1,created:'1',tool:'cargo.exe',state:'executing',elapsedSeconds:65,job:{product:'GoDD MCP α'}},{pid:2,created:'2',tool:'go.exe',state:'waiting'}],processes:[process(1,0,200),process(2,10,null,1000),process(3,2,500)]};
`

func TestResourcesDistinguishVerifiedWorkAndMissingMetrics(t *testing.T) {
	node(t, resourceFixture+`
const result=ctx.resourceModel(sample,1100,'cpu');assert.equal(result.active,2);assert.match(result.html,/0\.0%/);assert.match(result.html,/待機 1/);assert.match(result.html,/未確認 1/);assert.match(result.html,/GoDD MCP α/);assert.match(result.html,/蓄積中/);assert.match(result.html,/私有メモリ/);assert.match(result.html,/合算しません/);
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
