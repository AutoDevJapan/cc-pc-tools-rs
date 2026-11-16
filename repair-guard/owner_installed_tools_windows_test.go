package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type installedToolFixture struct {
	plan ownerInstalledToolPlan
	leases []*installedToolFixtureLease
	originCalls, aliasCalls, opens, reads int
	mode string
	cancel context.CancelFunc
	returned []byte
}

type installedToolFixtureLease struct {
	owner *installedToolFixture
	state ownerInstalledToolState
	index, states, closed int
}

func newInstalledToolFixture(mode string, cancel context.CancelFunc) *installedToolFixture {
	raw:=[]byte("MZ synthetic public development tool")
	f:=&installedToolFixture{mode:mode,cancel:cancel,plan:ownerInstalledToolPlan{name:"synthetic",path:"C:/fixed/bin/tool.exe",version:"1",publisher:"Fixed public publisher",sha:hashBytes(raw),size:int64(len(raw)),aliases:[]string{"C:/fixed/bin/alias.exe","C:/fixed/bin/tool.exe"}}}
	for i,path:=range f.plan.aliases {
		f.leases=append(f.leases,&installedToolFixtureLease{owner:f,index:i,state:ownerInstalledToolState{id:ownerInstalledToolIdentity{1,2,3},size:f.plan.size,links:2,acl:"known-acl",resolved:path,aclAllowed:true,parentProof:"known-parent-chain",parentsAllowed:true}})
	}
	return f
}

func (f *installedToolFixture) Origin(context.Context,ownerInstalledToolPlan) error {
	f.originCalls++
	if f.mode=="origin-close" || f.mode=="late-origin-close" && f.originCalls==2 { return errOwnerInstalledToolCleanup }
	if f.mode=="origin" || f.mode=="publisher" || f.mode=="version" || f.mode=="late-origin" && f.originCalls==2 { return errors.New("private origin text") }
	if f.mode=="cancel-origin" { f.cancel() }
	if f.mode=="late-origin-mutates-acl" && f.originCalls==2 { f.leases[0].state.acl="changed" }
	return nil
}

func (f *installedToolFixture) Aliases(context.Context,string) ([]string,error) {
	f.aliasCalls++
	if f.mode=="alias-close" || f.mode=="late-alias-close" && f.aliasCalls==2 { return nil,errOwnerInstalledToolCleanup }
	if f.mode=="list-error" { return nil,errors.New("private alias path") }
	result:=append([]string(nil),f.plan.aliases...)
	if f.mode=="alias-extra" { result=append(result,"C:/other/tool.exe") }
	if f.mode=="alias-missing" { return result[:1],nil }
	if f.mode=="alias-duplicate" { result[1]=result[0] }
	if f.mode=="late-alias" && f.aliasCalls==2 { result[1]="C:/other/tool.exe" }
	return result,nil
}

func (f *installedToolFixture) Open(_ context.Context,path string) (ownerInstalledToolLease,error) {
	i:=f.opens;f.opens++
	lease:=f.leases[i]
	if path!=lease.state.resolved { return nil,errors.New("unexpected path") }
	if f.mode=="open-error" && i==1 { return lease,errors.New("private open error") }
	if f.mode=="cancel-open" && i==1 { f.cancel() }
	return lease,nil
}

func (f *installedToolFixtureLease) State() (ownerInstalledToolState,error) {
	f.states++
	s:=f.state
	if f.owner.mode=="state-error" { return s,errors.New("private metadata text") }
	if f.owner.mode=="identity" && f.index==1 { s.id.file++ }
	if f.owner.mode=="created" && f.index==1 { s.id.created++ }
	if f.owner.mode=="missing-id" { s.id.created=0 }
	if f.owner.mode=="resolved" { s.resolved="C:/foreign/tool.exe" }
	if f.owner.mode=="links" { s.links=3 }
	if f.owner.mode=="length" { s.size++ }
	if f.owner.mode=="acl-policy" { s.aclAllowed=false }
	if f.owner.mode=="alias-acl" && f.index==1 { s.acl="different-acl" }
	if f.owner.mode=="late-id" && f.states>1 { s.id.file++ }
	if f.owner.mode=="late-acl" && f.states>1 { s.acl="changed-acl" }
	if f.owner.mode=="parent-acl" { s.parentsAllowed=false }
	if f.owner.mode=="parent-missing" { s.parentProof="" }
	if f.owner.mode=="late-parent-acl" && f.states>1 { s.parentProof="changed-parent-acl" }
	if f.owner.mode=="late-parent-id" && f.states>1 { s.parentProof="changed-parent-generation" }
	return s,nil
}

func (f *installedToolFixtureLease) Read(ctx context.Context,n int64) ([]byte,error) {
	o:=f.owner;o.reads++
	if n!=o.plan.size || o.opens!=len(o.plan.aliases) { return nil,errors.New("read before all aliases were pinned") }
	for _,lease:=range o.leases { if lease.closed!=0 { return nil,errors.New("read after release") } }
	o.returned=[]byte("MZ synthetic public development tool")
	if o.mode=="bytes" { o.returned[3]^=1 }
	if o.mode=="short" { o.returned=o.returned[:len(o.returned)-1] }
	if o.mode=="cancel-read" { o.cancel() }
	if o.mode=="read-error" { return o.returned,errors.New("private read error") }
	return o.returned,nil
}

func (f *installedToolFixtureLease) Close() error {
	f.closed++
	if f.owner.mode=="cancel-close" { f.owner.cancel() }
	if f.owner.mode=="close-error" || f.owner.mode=="cancel-and-close-error" { if f.owner.mode=="cancel-and-close-error" { f.owner.cancel() };return errors.New("private cleanup text") }
	return nil
}

func TestInstalledToolControllerUsesSameHeldBytesAndRejectsChanges(t *testing.T) {
	cases:=[]string{"normal","origin","publisher","version","list-error","alias-extra","alias-missing","alias-duplicate","late-alias","late-origin","late-origin-mutates-acl","open-error","state-error","identity","created","missing-id","resolved","links","length","acl-policy","alias-acl","late-id","late-acl","bytes","short","read-error","cancel-before","cancel-origin","cancel-open","cancel-read","cancel-close","close-error","cancel-and-close-error","parent-acl","parent-missing","late-parent-acl","late-parent-id","origin-close","late-origin-close","alias-close","late-alias-close"}
	for _,mode:=range cases {
		t.Run(mode,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			f:=newInstalledToolFixture(mode,cancel)
			if mode=="cancel-before" { cancel() }
			v:=ownerInstalledToolVerify(ctx,f.plan,f,func(string)ownerLegacyState{return ownerLegacyClear})
			if v.AccessAllowed!=(mode=="normal") { t.Fatal("incorrect fixed-purpose qualification",mode,v.Basis) }
			if mode=="normal" && (f.reads!=1 || f.originCalls!=2 || f.aliasCalls!=2 || v.Basis!="verified_installed_development_tool") { t.Fatal("normal verification stages missing") }
			for i,lease:=range f.leases { want:=0;if i<f.opens { want=1 };if lease.closed!=want { t.Fatal("resource not closed exactly once",mode,i,lease.closed) } }
			for _,b:=range f.returned { if b!=0 { t.Fatal("verified or failed buffer retained after classification") } }
			if strings.Contains(v.Basis,"private") || strings.Contains(v.Basis,"C:/") { t.Fatal("raw error leaked") }
			if mode=="origin" || mode=="publisher" || mode=="version" || mode=="cancel-before" || mode=="cancel-origin" { if f.opens!=0 || f.reads!=0 { t.Fatal("origin failure opened installed body") } }
			if mode=="cancel-and-close-error" && v.Basis!="installed_tool_cancelled_cleanup_failed" { t.Fatal("cleanup failure hidden by cancellation") }
			if strings.HasSuffix(mode,"-close") && (strings.HasPrefix(mode,"origin") || strings.HasPrefix(mode,"alias") || strings.HasPrefix(mode,"late-")) && v.Basis!="installed_tool_cleanup_failed" { t.Fatal("native resource close failure was lost") }
			if (mode=="parent-acl" || mode=="parent-missing") && f.reads!=0 { t.Fatal("untrusted parent reached executable content") }
		})
	}
}

func TestInstalledToolDirectoryACLRejectsReplacementRights(t *testing.T) {
	for _,rights:=range []string{"0x2","0x4","0x40","0x10000","0x40000","0x80000","GW","GA"} {
		if ownerInstalledToolACLMode("O:BAG:BAD:(A;;FA;;;BA)(A;;"+rights+";;;BU)","",true) { t.Fatal("untrusted directory replacement right accepted",rights) }
	}
	if !ownerInstalledToolACLMode("O:BAG:BAD:(A;;FA;;;SY)(A;;FA;;;BA)(A;;FRFX;;;BU)","",true) { t.Fatal("trusted parent rejected") }
}

func TestInstalledToolNativeCloseFailureCannotReturnSuccess(t *testing.T) {
	for _,primary:=range []error{nil,errors.New("private operation failure"),context.Canceled,context.DeadlineExceeded} {
		for _,closeFailure:=range []bool{false,true} {
			calls:=0
			operation:=func()(failure error){
				defer func(){calls++;var e error;if closeFailure {e=errors.New("private native close failure")};ownerInstalledCloseResult(&failure,e)}()
				return primary
			}
			err:=operation()
			if calls!=1 || closeFailure && !errors.Is(err,errOwnerInstalledToolCleanup) || !closeFailure && err!=primary { t.Fatal("native close result not preserved once") }
			if closeFailure && primary!=nil && !errors.Is(err,primary) { t.Fatal("primary error lost during native close") }
			if closeFailure && err.Error()!=errOwnerInstalledToolCleanup.Error() { t.Fatal("native text exposed") }
		}
	}
}

type installedToolTokenFixture struct {
	primary error
	cleanup error
	reads,closes int
}
func (t *installedToolTokenFixture) SID() (string,error) { t.reads++;return "S-1-5-21-100-200-300-1001",t.primary }
func (t *installedToolTokenFixture) Close() error { t.closes++;return t.cleanup }

func TestInstalledToolTokenSIDPreservesFailureAndClosesOnce(t *testing.T) {
	for _,primary:=range []error{nil,errOwnerInstalledTool,context.Canceled,context.DeadlineExceeded} {
		for _,closeFail:=range []bool{false,true} {
			fixture:=&installedToolTokenFixture{primary:primary}
			if closeFail { fixture.cleanup=errors.New("private token close failure") }
			sid,err:=ownerInstalledTokenSID(fixture)
			if fixture.reads!=1 || fixture.closes!=1 { t.Fatal("token helper did not own exactly one read/close") }
			if primary==nil && !closeFail { if err!=nil || sid=="" { t.Fatal("normal token identity rejected") };continue }
			if sid!="" || err==nil { t.Fatal("failed token operation returned an identity") }
			if primary!=nil && !errors.Is(err,primary) { t.Fatal("token primary or cancellation disappeared") }
			if closeFail && (!errors.Is(err,errOwnerInstalledToolCleanup) || err.Error()!=errOwnerInstalledToolCleanup.Error()) { t.Fatal("token cleanup was erased or free text escaped") }
		}
	}
}

type installedToolFaultOverlay struct {
	base *installedToolFixture
	stage string
	primary error
	closeFailure bool
	cancel context.CancelFunc
	closes int
	stateAt int
}

func (o *installedToolFaultOverlay) failure() error {
	err:=o.primary
	if o.cancel!=nil { o.cancel() }
	o.closes++
	var closeErr error
	if o.closeFailure { closeErr=errors.New("private native cleanup") }
	ownerInstalledCloseResult(&err,closeErr)
	return err
}

func (o *installedToolFaultOverlay) Origin(ctx context.Context,plan ownerInstalledToolPlan) error {
	if o.stage=="origin" { return o.failure() }
	return o.base.Origin(ctx,plan)
}
func (o *installedToolFaultOverlay) Aliases(ctx context.Context,path string) ([]string,error) {
	if o.stage=="aliases" { return nil,o.failure() }
	return o.base.Aliases(ctx,path)
}
func (o *installedToolFaultOverlay) Open(ctx context.Context,path string) (ownerInstalledToolLease,error) {
	lease,err:=o.base.Open(ctx,path)
	if err==nil && o.stage=="token" { return &installedToolTokenOverlay{lease:lease,owner:o},nil }
	return lease,err
}
type installedToolTokenOverlay struct { lease ownerInstalledToolLease; owner *installedToolFaultOverlay; states int }
func (o *installedToolTokenOverlay) State() (ownerInstalledToolState,error) {
	state,err:=o.lease.State();o.states++
	if err!=nil || o.owner.stateAt>0 && o.states!=o.owner.stateAt { return state,err }
	return state,o.owner.failure()
}
func (o *installedToolTokenOverlay) Read(ctx context.Context,n int64) ([]byte,error) { return o.lease.Read(ctx,n) }
func (o *installedToolTokenOverlay) Close() error { return o.lease.Close() }

func TestInstalledToolPreservesPrimaryCancellationAndCleanupFacts(t *testing.T) {
	for _,stage:=range []string{"origin","aliases","token"} {
		for _,fault:=range []string{"primary","cleanup","primary-cleanup","cancel-primary-cleanup","returned-cancel-cleanup"} {
			t.Run(stage+"/"+fault,func(t *testing.T){
				ctx,cancel:=context.WithCancel(context.Background());defer cancel()
				base:=newInstalledToolFixture("normal",cancel)
				o:=&installedToolFaultOverlay{base:base,stage:stage}
				primary:="installed_tool_origin_unavailable"
				if stage=="aliases" { primary="installed_tool_alias_mismatch" }
				if stage=="token" { primary="installed_tool_identity_mismatch" }
				want:=primary
				switch fault {
				case "primary":o.primary=errOwnerInstalledTool
				case "cleanup":o.closeFailure=true;if stage!="token" { want="installed_tool" };want+="_cleanup_failed"
				case "primary-cleanup":o.primary=errOwnerInstalledTool;o.closeFailure=true;want+="_cleanup_failed"
				case "cancel-primary-cleanup":o.primary=errOwnerInstalledTool;o.closeFailure=true;o.cancel=cancel;want+="_cancelled_cleanup_failed"
				case "returned-cancel-cleanup":o.primary=context.Canceled;o.closeFailure=true;want+="_cancelled_cleanup_failed"
				}
				got:=ownerInstalledToolVerify(ctx,base.plan,o,func(string)ownerLegacyState{return ownerLegacyClear})
				if got.AccessAllowed || got.Status!="unverified" || got.Basis!=want || o.closes!=1 { t.Fatal("compound failure facts lost",stage,fault,got.Basis,want,o.closes) }
				for i,lease:=range base.leases { expected:=0;if i<base.opens { expected=1 };if lease.closed!=expected { t.Fatal("compound failure close count differs") } }
				if base.reads!=0 { t.Fatal("failed origin or metadata reached installed body") }
			})
		}
	}
	for _,mode:=range []string{"bytes-close","bytes-cancel-close","parent-close"} {
		t.Run(mode,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			f:=newInstalledToolFixture("close-error",cancel)
			want:="installed_tool_bytes_mismatch_cleanup_failed"
			if mode=="parent-close" { f.leases[0].state.parentsAllowed=false;want="installed_tool_parent_untrusted_cleanup_failed" } else { f.plan.sha=strings.Repeat("0",64) }
			if mode=="bytes-cancel-close" { f.mode="cancel-and-close-error";want="installed_tool_bytes_mismatch_cancelled_cleanup_failed" }
			got:=ownerInstalledToolVerify(ctx,f.plan,f,func(string)ownerLegacyState{return ownerLegacyClear})
			if got.AccessAllowed || got.Basis!=want { t.Fatal("controller refusal was erased by late cleanup",got.Basis,want) }
		})
	}
}

func TestInstalledToolInheritOnlyIsNotAnEffectiveCurrentObjectGrant(t *testing.T) {
	for _,directory:=range []bool{false,true} {
		for _,flags:=range []string{"IO","OIIO","CIIO","OICIIO"} {
			sddl:="O:BAG:BAD:(A;;FA;;;BA)(A;"+flags+";FA;;;BU)"
			if !ownerInstalledToolACLMode(sddl,"",directory) { t.Fatal("inherit-only entry treated as effective write",flags,directory) }
		}
		if ownerInstalledToolACLMode("O:BAG:BAD:(A;;FA;;;BA)(A;OI;FA;;;BU)","",directory) { t.Fatal("effective inheritable write was ignored") }
	}
	// A child with an effective inherited grant is still rejected by its own
	// descriptor, even if its parent's grant was inherit-only there.
	if ownerInstalledToolACLMode("O:BAG:BAD:(A;;FA;;;BA)(A;ID;FA;;;BU)","",false) { t.Fatal("effective child ACL was ignored") }
}

func TestInstalledToolNormalTraceAndExplicitFalseSharePolicy(t *testing.T) {
	for _,plan:=range ownerInstalledToolPlans() {
		for _,mode:=range []string{"clear","unknown","cleanup","client","unavailable","ordinary"} {
			t.Run(plan.name+"/"+mode,func(t *testing.T){
				path:=plan.path;if mode=="ordinary" { path="C:/ordinary/fixture.txt" }
				providerCalls,physicalCalls:=0,0
				makeAccess:=func()ownerPolicyAccess {
					a:=ownerNormalPolicyAccess(func(string)ownerPathVerdict{return ownerRuleUnknown("ordinary")})
					if a.installed==nil { t.Fatal("normal provider set omitted installed callback") }
					a.legacy=func(string)ownerLegacyState{if mode=="client" { return ownerLegacyClient };if mode=="unavailable" {return ownerLegacyUnavailable};return ownerLegacyClear}
					a.physical=func(string)(string,error){physicalCalls++;return "",errors.New("synthetic ordinary physical failure")}
					a.installed=func(string)ownerPathVerdict{providerCalls++;switch mode {case "clear":return ownerRuleClear("verified_installed_development_tool");case "cleanup":return ownerInstalledFailureVerdict("installed_tool_origin_unavailable",true,true);default:return ownerRuleUnknown("installed_tool_origin_unavailable")}}
					return a
				}
				ordinary:=ownerPolicyClassify(path,makeAccess())
				trace:=&ownerClassificationTrace{}
				traced:=ownerPolicyClassifyTraced(path,makeAccess(),trace)
				if ordinary.AccessAllowed!=traced.AccessAllowed || ordinary.Basis!=traced.Basis || ordinary.Status!=traced.Status { t.Fatal("trace changes fixed tool verdict") }
				wantProviders,wantPhysical:=2,0
				if mode=="client" || mode=="unavailable" {wantProviders=0}
				if mode=="ordinary" {wantProviders=0;wantPhysical=2}
				if providerCalls!=wantProviders || physicalCalls!=wantPhysical { t.Fatal("trace or exclusion bypass changed") }
				for _,option:=range []string{"--classification-diagnostic","--classification-diagnostic=false"} {
					var out,stderr bytes.Buffer
					code:=runClassificationDiagnosticCLI([]string{"--guard-path",path,option},&out,&stderr,func(input string)(ownerPathVerdict,*ownerClassificationTrace){tr:=&ownerClassificationTrace{};return ownerPolicyClassifyTraced(input,makeAccess(),tr),tr})
					var got map[string]any
					if code!=0 || stderr.Len()!=0 || json.Unmarshal(out.Bytes(),&got)!=nil || got["access_allowed"]!=ordinary.AccessAllowed || got["classification_basis"]!=ordinary.Basis { t.Fatal("diagnostic CLI diverged from ordinary classifier") }
					_,has:=got["classification_diagnostic"];if has!=(option=="--classification-diagnostic") {t.Fatal("explicit false emitted diagnostic metadata")}
				}
			})
		}
	}
}

func TestInstalledToolClosedPathsAndLegacyExclusions(t *testing.T) {
	for _,plan:=range ownerInstalledToolPlans() {
		if got,ok:=ownerInstalledToolPlanFor(plan.path);!ok || got.sha!=plan.sha || len(got.archiveSHA)!=64 || got.size<=0 { t.Fatal("incomplete compiled vendor pin") }
		for _,bad:=range []string{filepath.Dir(plan.path),plan.path+".bak",plan.path+":stream",filepath.Join(filepath.Dir(plan.path),"config.json")} {
			if _,ok:=ownerInstalledToolPlanFor(bad);ok { t.Fatal("neighbor or data path admitted") }
		}
	}
	if _,ok:=ownerInstalledToolPlanFor("C:/Program Files/Git/cmd/git-lfs.exe");ok { t.Fatal("secondary alias gained a general grant") }
	for _,state:=range []ownerLegacyState{ownerLegacyClient,ownerLegacyUnavailable} {
		f:=newInstalledToolFixture("normal",func(){})
		v:=ownerInstalledToolVerify(context.Background(),f.plan,f,func(string)ownerLegacyState{return state})
		if v.AccessAllowed || f.originCalls!=0 || f.opens!=0 { t.Fatal("legacy exclusion bypassed") }
	}
}

const (
	installedToolTestUser="S-1-5-21-100-200-300-1001"
	installedToolTestInstaller="S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
)

// Structural copies of descriptors read on a stock Windows PC and on a per-user
// rustup installation. Account identifiers are synthetic.
var installedToolStockDescriptors=map[string]string{
	"system volume root":"O:"+installedToolTestInstaller+"G:"+installedToolTestInstaller+"D:PAI(A;;LC;;;AU)(A;OICIIO;SDGXGWGR;;;AU)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)(A;;0x1000a1;;;S-1-15-3-65536-1-2-3-4-5-6-7-8)",
	"data volume root":"O:SYG:SYD:(A;OICIIO;SDGXGWGR;;;AU)(A;;0x1301bf;;;AU)(A;;FA;;;SY)(A;OICIIO;GA;;;SY)(A;OICIIO;GA;;;BA)(A;;FA;;;BA)(A;;0x1200a9;;;BU)(A;OICIIO;GXGR;;;BU)",
	"profile directory":"O:SYG:SYD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;"+installedToolTestUser+")(A;;0x100020;;;S-1-15-3-65536-1-2-3-4-5-6-7-8)",
	"program files directory":"O:"+installedToolTestInstaller+"G:"+installedToolTestInstaller+"D:PAI(A;OICIIO;GA;;;CO)(A;OICIIO;GA;;;SY)(A;;0x1301bf;;;SY)(A;OICIIO;GA;;;BA)(A;;0x1301bf;;;BA)(A;OICIIO;GXGR;;;BU)(A;;0x1200a9;;;BU)(A;CIIO;GA;;;"+installedToolTestInstaller+")(A;;FA;;;"+installedToolTestInstaller+")(A;;0x1200a9;;;AC)(A;OICIIO;GXGR;;;AC)(A;;0x1200a9;;;S-1-15-2-2)(A;OICIIO;GXGR;;;S-1-15-2-2)",
	"go install directory":"O:SYG:SYD:AI(A;ID;FA;;;"+installedToolTestInstaller+")(A;CIIOID;GA;;;"+installedToolTestInstaller+")(A;ID;FA;;;SY)(A;OICIIOID;GA;;;SY)(A;ID;FA;;;BA)(A;OICIIOID;GA;;;BA)(A;ID;0x1200a9;;;BU)(A;OICIIOID;GXGR;;;BU)(A;OICIIOID;GA;;;CO)(A;ID;0x1200a9;;;AC)(A;OICIIOID;GXGR;;;AC)(A;ID;0x1200a9;;;S-1-15-2-2)(A;OICIIOID;GXGR;;;S-1-15-2-2)",
	"go executable":"O:SYG:SYD:AI(A;ID;FA;;;SY)(A;ID;FA;;;BA)(A;ID;0x1200a9;;;BU)(A;ID;0x1200a9;;;AC)(A;ID;0x1200a9;;;S-1-15-2-2)",
	"user tool directory":"O:"+installedToolTestUser+"G:S-1-5-21-100-200-300-513D:AI(A;OICIID;0x1200a9;;;S-1-5-21-100-200-300-1003)(D;OICIID;CC;;;S-1-5-80-1-2-3-4-5)(A;OICIID;FA;;;SY)(A;OICIID;FA;;;BA)(A;OICIID;FA;;;"+installedToolTestUser+")",
	"user tool executable":"O:"+installedToolTestUser+"G:S-1-5-21-100-200-300-513D:AI(A;ID;0x1200a9;;;S-1-5-21-100-200-300-1003)(D;ID;CC;;;S-1-5-80-1-2-3-4-5)(A;ID;FA;;;SY)(A;ID;FA;;;BA)(A;ID;FA;;;"+installedToolTestUser+")",
}

func TestInstalledToolGoFamilyPlansShareOneAuthenticatedArchive(t *testing.T) {
	var goPlan,gofmtPlan ownerInstalledToolPlan
	declared:=0
	for _,plan:=range ownerInstalledToolPlans() {
		switch plan.name {
		case "go": goPlan=plan;declared++
		case "gofmt": gofmtPlan=plan;declared++
		}
	}
	if declared!=2 || goPlan.name=="" || gofmtPlan.name=="" { t.Fatal("go and gofmt must each be declared exactly once") }
	if gofmtPlan.path!="C:/Program Files/Go/bin/gofmt.exe" || len(gofmtPlan.aliases)!=1 || gofmtPlan.aliases[0]!=gofmtPlan.path { t.Fatal("gofmt is not one fixed single-name installation") }
	if gofmtPlan.version!=goPlan.version || gofmtPlan.publisher!=goPlan.publisher || gofmtPlan.registry!=goPlan.registry || gofmtPlan.archiveURL!=goPlan.archiveURL || gofmtPlan.archiveSHA!=goPlan.archiveSHA { t.Fatal("go family does not share one registration and one public archive") }
	if goPlan.entry!="go/bin/go.exe" || gofmtPlan.entry!="go/bin/gofmt.exe" || gofmtPlan.sha==goPlan.sha || gofmtPlan.size==goPlan.size || len(gofmtPlan.sha)!=64 || gofmtPlan.size<=0 { t.Fatal("gofmt does not have its own public archive entry pin") }
	if got,ok:=ownerInstalledToolPlanFor("C:/Program Files/Go/bin/gofmt.exe");!ok || got.name!="gofmt" { t.Fatal("gofmt installation was not delegated") }
}

func TestInstalledToolOriginSpecSelectsHiveAndValuesFromTheCompiledPlan(t *testing.T) {
	hive,fields:=ownerInstalledToolOriginSpec(ownerInstalledToolPlan{version:"1.0",publisher:"Fixed publisher"})
	if hive!=syscall.Handle(syscall.HKEY_LOCAL_MACHINE) || len(fields)!=2 || fields["DisplayVersion"]!="1.0" || fields["Publisher"]!="Fixed publisher" { t.Fatal("system installer origin changed") }
	hive,fields=ownerInstalledToolOriginSpec(ownerInstalledToolPlan{userRegistry:true,originFields:map[string]string{"DisplayName":"Fixed display name"}})
	if hive!=syscall.Handle(syscall.HKEY_CURRENT_USER) || len(fields)!=1 || fields["DisplayName"]!="Fixed display name" { t.Fatal("per-user installer origin was not selected from the plan") }
}

func TestInstalledToolRustupIsOneFixedUserInstallationOfTheReleasedSetupFile(t *testing.T) {
	plan,ok:=ownerInstalledToolPlanFor("C:/Users/makhmeto/.cargo/bin/rustup.exe")
	if !ok || plan.name!="rustup" || !plan.userRegistry || plan.originFields["DisplayName"]=="" || len(plan.aliases)!=1 || plan.aliases[0]!=plan.path { t.Fatal("rustup must be one fixed per-user single-name installation") }
	if plan.archiveSHA!=plan.sha || len(plan.sha)!=64 || plan.size<=0 || !strings.HasPrefix(plan.archiveURL,"https://static.rust-lang.org/rustup/archive/"+plan.version+"/") { t.Fatal("rustup pin is not the released setup file of its own version") }
	for _,neighbor:=range []string{"cargo","rustc","rustfmt","cargo-clippy","clippy-driver","rustdoc","cargo-fmt"} {
		if _,granted:=ownerInstalledToolPlanFor("C:/Users/makhmeto/.cargo/bin/"+neighbor+".exe");granted { t.Fatal("proxy name gained a general grant",neighbor) }
	}
}

func TestInstalledToolStockDescriptorsAreAcceptedInTheirRole(t *testing.T) {
	user:=installedToolTestUser
	for _,tc:=range []struct{name string;install,root,want bool}{
		{"system volume root",false,true,true},
		{"system volume root",true,true,false},
		{"data volume root",false,true,true},
		{"data volume root",false,false,false},
		{"profile directory",false,false,true},
		{"program files directory",false,false,true},
		{"go install directory",true,false,true},
		{"user tool directory",true,false,true},
		{"user tool directory",false,false,true},
	} {
		sddl,known:=installedToolStockDescriptors[tc.name]
		if !known { t.Fatal("missing descriptor",tc.name) }
		if got:=ownerInstalledToolParentAllowed(sddl,user,tc.install,tc.root);got!=tc.want { t.Fatal("descriptor role changed",tc.name,tc.install,tc.root,got) }
	}
	for _,name:=range []string{"go executable","user tool executable"} {
		if !ownerInstalledToolACL(installedToolStockDescriptors[name],user) { t.Fatal("stock executable descriptor rejected",name) }
	}
}

func TestInstalledToolRightNamesKeepTheirFileSystemBitMeaning(t *testing.T) {
	for _,tc:=range []struct{name,ace string;directory,want bool}{
		{"write","(A;;FW;;;BU)",false,false},
		{"read","(A;;FR;;;BU)",false,true},
		{"read data by its directory service name","(A;;CC;;;BU)",false,true},
		{"append data by its directory service name","(A;;LC;;;BU)",false,false},
		{"delete child is meaningless on a file","(A;;DT;;;BU)",false,true},
		{"delete child on a directory","(A;;DT;;;BU)",true,false},
		{"add subdirectory on a directory","(A;;LC;;;BU)",true,false},
		{"write extended attributes by its directory service name","(A;;RP;;;BU)",true,false},
		{"deny entry with directory service names","(D;;CCLC;;;BU)",false,true},
		{"unknown name","(A;;ZZ;;;BU)",false,false},
		{"unknown name in a deny entry","(D;;ZZ;;;BU)",false,false},
	} {
		if got:=ownerInstalledToolACLMode("O:BAG:BAD:(A;;FA;;;BA)"+tc.ace,"",tc.directory);got!=tc.want { t.Fatal("right name semantics changed",tc.name,got) }
	}
}

func TestInstalledToolAncestorsOnlyCountRightsThatCanReplaceAComponent(t *testing.T) {
	for _,tc:=range []struct{rights string;install,root,want bool}{
		{"0x40",false,false,false},{"0x10000",false,false,false},{"0x40000",false,false,false},{"0x80000",false,false,false},{"GW",false,false,false},{"GA",false,false,false},{"DT",false,false,false},
		{"0x2",false,false,true},{"0x4",false,false,true},{"0x10",false,false,true},{"0x100",false,false,true},{"LC",false,false,true},{"CC",false,false,true},{"DC",false,false,true},
		{"0x40",false,true,false},{"0x40000",false,true,false},{"0x80000",false,true,false},{"GW",false,true,false},{"DT",false,true,false},
		{"0x10000",false,true,true},{"SD",false,true,true},{"0x2",false,true,true},{"LC",false,true,true},
		{"0x2",true,false,false},{"LC",true,false,false},{"0x10000",true,false,false},{"0x40",true,false,false},
	} {
		sddl:="O:BAG:BAD:(A;;FA;;;BA)(A;;"+tc.rights+";;;AU)"
		if got:=ownerInstalledToolParentAllowed(sddl,"",tc.install,tc.root);got!=tc.want { t.Fatal("ancestor rule changed",tc.rights,tc.install,tc.root,got) }
	}
	const trustedOnly="O:BAG:BAD:(A;;FA;;;SY)(A;;FA;;;BA)(A;;FRFX;;;BU)"
	for _,role:=range [][2]bool{{false,false},{false,true},{true,false}} {
		if !ownerInstalledToolParentAllowed(trustedOnly,"",role[0],role[1]) { t.Fatal("trusted directory rejected",role) }
	}
}

func TestInstalledToolACLDoesNotTrustRegistryPublisherAlone(t *testing.T) {
	user:="S-1-5-21-100-200-300-1001"
	for _,tc:=range []struct{s string;ok bool}{
		{"O:BAG:BAD:AI(A;;FA;;;SY)(A;;FA;;;BA)(A;;0x1200a9;;;BU)",true},
		{"O:"+user+"G:BAD:P(A;;FA;;;"+user+")(A;;FRFX;;;BU)",true},
		{"O:BAG:BAD:(A;;FA;;;WD)",false},
		{"O:BAG:BAD:(A;;GW;;;BU)",false},
		{"O:BUG:BAD:(A;;FR;;;BU)",false},
		{"O:BAG:BAD:NO_ACCESS_CONTROL",false},
		{"O:BAG:BAD:(XA;;FR;;;BU)",false},
		{"O:BAG:BAD:(A;unknown;FR;;;BU)",false},
		{"O:BAG:BAD:(A;;ZZ;;;BU)",false},
		{"O:BAG:BAD:(A;;FR;;;BU)trailing",false},
	} { if ownerInstalledToolACL(tc.s,user)!=tc.ok { t.Fatal("ACL policy differs",tc.ok) } }
}

func TestInstalledToolNormalPolicyCallbackIsExactAndPreservesExclusions(t *testing.T) {
	path:=ownerInstalledToolPlans()[0].path
	for _,mode:=range []string{"normal","client","unavailable","unknown-origin","ordinary-file"} {
		calls,physicalCalls:=0,0
		input:=path;if mode=="ordinary-file" { input="C:/other/readme.md" }
		v:=ownerPolicyClassify(input,ownerPolicyAccess{
			legacy:func(string)ownerLegacyState{if mode=="client" {return ownerLegacyClient};if mode=="unavailable" {return ownerLegacyUnavailable};return ownerLegacyClear},
			physical:func(string)(string,error){physicalCalls++;return "",errors.New("ordinary hardlink or metadata rejection")},
			installed:func(string)ownerPathVerdict{calls++;if mode=="unknown-origin" {return ownerRuleUnknown("installed_tool_origin_unavailable")};return ownerRuleClear("verified_installed_development_tool")},
		})
		if v.AccessAllowed!=(mode=="normal") { t.Fatal("normal classifier connection incorrect",mode) }
		if mode=="normal" && (calls!=1 || physicalCalls!=0) { t.Fatal("fixed installation still uses generic hardlink path") }
		if (mode=="client" || mode=="unavailable" || mode=="ordinary-file") && calls!=0 { t.Fatal("special provider used outside fixed excluded scope") }
		if mode=="ordinary-file" && (physicalCalls!=1 || v.Basis!="physical_metadata_unavailable") { t.Fatal("generic hardlink policy weakened") }
	}
}

func TestInstalledToolTokenCleanupAtEachStateCannotBecomeClear(t *testing.T) {
	for _,at:=range []int{1,2,3} {
		for _,fault:=range []string{"cleanup","primary-cleanup","cancel-primary-cleanup","returned-cancel-cleanup"} {
			t.Run([]string{"initial","after-read","final"}[at-1]+"/"+fault,func(t *testing.T){
				ctx,cancel:=context.WithCancel(context.Background());defer cancel()
				base:=newInstalledToolFixture("normal",cancel)
				o:=&installedToolFaultOverlay{base:base,stage:"token",stateAt:at,closeFailure:true}
				want:="installed_tool_identity_mismatch"
				if at>1 {want="installed_tool_identity_changed"}
				switch fault {
				case "primary-cleanup":o.primary=errOwnerInstalledTool
				case "cancel-primary-cleanup":o.primary=errOwnerInstalledTool;o.cancel=cancel
				case "returned-cancel-cleanup":o.primary=context.Canceled
				}
				if strings.Contains(fault,"cancel") {want+="_cancelled"};want+="_cleanup_failed"
				v:=ownerInstalledToolVerify(ctx,base.plan,o,func(string)ownerLegacyState{return ownerLegacyClear})
				if v.AccessAllowed || v.Status!="unverified" || v.Basis!=want || o.closes!=1 {t.Fatal("state cleanup/cancellation facts disappeared",at,fault,v.Basis,want)}
				wantReads:=1;if at==1 {wantReads=0}
				if base.reads!=wantReads {t.Fatal("state fault reached wrong body boundary")}
				wantOrigins:=1;if at==3 {wantOrigins=2}
				if base.originCalls!=wantOrigins {t.Fatal("state fault hit wrong recheck phase")}
				for i,lease:=range base.leases {wantClose:=0;if i<base.opens {wantClose=1};if lease.closed!=wantClose {t.Fatal("state fault leaked or repeated a lease close")}}
				for _,b:=range base.returned {if b!=0 {t.Fatal("state fault retained installed content")}}
			})
		}
	}
}

func TestInstalledToolLegacyProtectionSurvivesCancellation(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	fixture:=newInstalledToolFixture("normal",cancel)
	v:=ownerInstalledToolVerify(ctx,fixture.plan,fixture,func(string)ownerLegacyState{cancel();return ownerLegacyClient})
	if v.AccessAllowed || v.Status!="protected" || v.Basis!="legacy_exclusion" || fixture.originCalls!=0 || fixture.opens!=0 || fixture.reads!=0 {t.Fatal("cancellation erased a confirmed client exclusion or opened content")}
}

type installedToolBoundaryFixture struct {
	*installedToolFixture
	ctx context.Context
	cancel context.CancelFunc
	boundary string
	data []byte
	readerCalls int
	views [][]byte
}

func (f *installedToolBoundaryFixture) Open(ctx context.Context,path string) (ownerInstalledToolLease,error) {
	lease,err:=f.installedToolFixture.Open(ctx,path)
	if err!=nil { return lease,err }
	if strings.HasPrefix(f.boundary,"open-") {
		f.cancel()
		if f.boundary=="open-error" { return lease,errOwnerInstalledTool }
		return lease,ctx.Err()
	}
	return &installedToolBoundaryLease{owner:f,lease:lease},nil
}

type installedToolBoundaryLease struct {
	owner *installedToolBoundaryFixture
	lease ownerInstalledToolLease
}
func (l *installedToolBoundaryLease) State() (ownerInstalledToolState,error) { return l.lease.State() }
func (l *installedToolBoundaryLease) Close() error { return l.lease.Close() }
func (l *installedToolBoundaryLease) Read(ctx context.Context,size int64) ([]byte,error) {
	l.owner.reads++
	if l.owner.boundary=="read-before" { l.owner.cancel() }
	raw,err:=ownerInstalledToolRead(ctx,l.owner,size)
	l.owner.returned=raw
	return raw,err
}

func (f *installedToolBoundaryFixture) ReadAt(out []byte,offset int64) (int,error) {
	f.readerCalls++
	f.views=append(f.views,out)
	if offset<0 || offset>=int64(len(f.data)) { return 0,io.EOF }
	if f.boundary=="read-error" {
		n:=copy(out[:len(out)/2],f.data[int(offset):]);f.cancel()
		return n,io.ErrUnexpectedEOF
	}
	n:=copy(out,f.data[int(offset):])
	if f.boundary=="read-mid" { f.cancel() }
	if int(offset)+n==len(f.data) && (f.boundary=="read-complete" || f.boundary=="read-mismatch") {
		if f.boundary=="read-mismatch" { out[n-1]^=1 }
		f.cancel()
	}
	return n,nil
}

func TestInstalledToolNativeReadBoundaryDistinguishesCancelIOAndMismatch(t *testing.T) {
	for _,boundary:=range []string{"normal","open-cancel","open-error","read-before","read-mid","read-complete","read-error","read-mismatch"} {
		for _,closeFail:=range []bool{false,true} {
			name:=boundary;if closeFail {name+="/close-failure"}
			t.Run(name,func(t *testing.T){
				ctx,cancel:=context.WithCancel(context.Background());defer cancel()
				base:=newInstalledToolFixture("normal",cancel)
				data:=bytes.Repeat([]byte{'a'},65537);copy(data,[]byte("MZ"))
				base.plan.size=int64(len(data));base.plan.sha=hashBytes(data)
				for _,lease:=range base.leases {lease.state.size=base.plan.size}
				if closeFail {base.mode="close-error"}
				fixture:=&installedToolBoundaryFixture{installedToolFixture:base,ctx:ctx,cancel:cancel,boundary:boundary,data:data}
				got:=ownerInstalledToolVerify(ctx,base.plan,fixture,func(string)ownerLegacyState{return ownerLegacyClear})
				want:="verified_installed_development_tool"
				switch boundary {
				case "open-cancel","read-before","read-mid","read-complete":want="installed_tool_cancelled"
				case "open-error":want="installed_tool_identity_unavailable_cancelled"
				case "read-error":want="installed_tool_read_failed_cancelled"
				case "read-mismatch":want="installed_tool_bytes_mismatch_cancelled"
				}
				if closeFail {if boundary=="normal" {want="installed_tool_cleanup_failed"} else {want+="_cleanup_failed"}}
				if got.Basis!=want || got.AccessAllowed!=(boundary=="normal" && !closeFail) {t.Fatal("Open/Read cancellation invented or erased a primary fact",boundary,got.Basis,want)}
				wantReads:=1;wantReaderCalls:=2
				if strings.HasPrefix(boundary,"open-") {wantReads=0;wantReaderCalls=0}
				if boundary=="read-before" {wantReaderCalls=0}
				if boundary=="read-mid" || boundary=="read-error" {wantReaderCalls=1}
				if base.reads!=wantReads || fixture.readerCalls!=wantReaderCalls {t.Fatal("wrong bounded native-read phase count")}
				for i,lease:=range base.leases {wantCloses:=0;if i<base.opens {wantCloses=1};if lease.closed!=wantCloses {t.Fatal("Open/Read cancellation did not close each acquired lease once")}}
				for _,view:=range fixture.views {for _,b:=range view {if b!=0 {t.Fatal("native read success/failure retained buffer")}}}
				if boundary!="normal" && (base.originCalls!=1 || base.aliasCalls!=1) {t.Fatal("failure continued beyond observed Open/Read boundary")}
				if strings.Contains(got.Basis,"C:/") || strings.Contains(got.Basis,"private") {t.Fatal("native error text escaped fixed classification")}
			})
		}
	}
}

func TestInstalledToolNativeOpenAndReadInitialCancellationDoNotTouchAPath(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	lease,err:=(ownerInstalledToolNative{}).Open(ctx,"not-an-installed-path")
	if lease!=nil || err!=context.Canceled {t.Fatal("native Open initial cancellation lost or allocated a lease")}
	raw,err:=(&ownerInstalledToolFile{}).Read(ctx,64)
	if raw!=nil || err!=context.Canceled {t.Fatal("native Read initial cancellation lost before file validation")}
}
