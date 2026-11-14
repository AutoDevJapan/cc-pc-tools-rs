package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
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
