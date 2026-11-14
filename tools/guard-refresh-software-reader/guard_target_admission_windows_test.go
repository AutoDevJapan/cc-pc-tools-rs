//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type guardTargetTestFixture struct {
	mode                 string
	checks, borrows      int
	artifactCloses       int
	purposeCloses        int
	rechecks, opens      int
	observes, leafCloses  int
	closed               bool
	cancel               context.CancelFunc
	events               []string
	artifact             *guardTargetTestArtifact
	purpose              *guardTargetTestPurpose
	lease                *guardTargetTestMetadata
}

type guardTargetTestArtifact struct{ fixture *guardTargetTestFixture }
type guardTargetTestPurpose struct {
	fixture  *guardTargetTestFixture
	expected guardTargetArtifact
}
type guardTargetTestMetadata struct{ fixture *guardTargetTestFixture }

func newGuardTargetFixture(mode string, cancel context.CancelFunc) *guardTargetTestFixture {
	f := &guardTargetTestFixture{mode: mode, cancel: cancel}
	f.artifact = &guardTargetTestArtifact{f}
	f.purpose = &guardTargetTestPurpose{fixture: f}
	f.purpose.expected = f.artifact
	f.lease = &guardTargetTestMetadata{f}
	return f
}

func (a *guardTargetTestArtifact) VerifySourceGeneration() bool {
	f := a.fixture
	f.checks++
	return !f.closed && f.mode != "artifact-invalid" && !(f.mode == "artifact-late" && f.checks > 1)
}
func (a *guardTargetTestArtifact) WithPinnedPayload(use func([]byte) error) error {
	f := a.fixture
	f.borrows++
	if f.mode == "no-callback" { return nil }
	payload := []byte("MZ synthetic verified artifact")
	if f.mode == "empty-payload" { payload = nil }
	err := use(payload)
	if f.mode == "ignore-callback-error" { return nil }
	if err != nil { return err }
	if f.mode == "double-callback" { return use(payload) }
	if f.mode == "borrow-return-error" { return errors.New("private artifact source error") }
	if f.mode == "cancel-after-borrow" { f.cancel() }
	return nil
}
func (a *guardTargetTestArtifact) Close() error {
	f := a.fixture
	f.artifactCloses++
	f.events = append(f.events, "artifact-close")
	f.closed = true
	if f.mode == "artifact-close" { return errors.New("private artifact close path") }
	return nil
}
func (p *guardTargetTestPurpose) Recheck(ctx context.Context, actual guardTargetArtifact) error {
	f := p.fixture
	f.rechecks++
	if ctx.Err() != nil { return ctx.Err() }
	if actual != p.expected || f.mode == "purpose-invalid" || f.mode == "purpose-late" && f.rechecks == 3 {
		return errGuardTargetPurposeUnavailable
	}
	return nil
}
func (p *guardTargetTestPurpose) OpenMetadata(ctx context.Context) (guardTargetMetadataLease, error) {
	f := p.fixture
	f.opens++
	if f.mode == "nil-lease" { return nil, nil }
	if f.mode == "open-error" { return f.lease, errors.New("private target metadata path") }
	if f.mode == "open-cancel" { f.cancel(); return f.lease, ctx.Err() }
	return f.lease, nil
}
func (p *guardTargetTestPurpose) Close() error {
	f := p.fixture
	f.purposeCloses++
	f.events = append(f.events, "purpose-close")
	if f.mode == "purpose-close" || f.mode == "cancel-cleanup" {
		return errors.New("private purpose close text")
	}
	return nil
}
func (m *guardTargetTestMetadata) Observe() (guardTargetMetadata, error) {
	f := m.fixture
	f.observes++
	state := guardTargetMetadata{identity: softwareIdentity{Volume:"7",FileID:"8",Created:"9"}, acl:"O:BAG:BAD:P(A;;FA;;;BA)", parent:strings.Repeat("a",64), size:4096}
	if f.mode == "observe-error" || f.mode == "ignore-callback-error" { return state, errors.New("private observe error") }
	if f.mode == "bad-id" { state.identity.Created = "00" }
	if f.mode == "bad-parent" { state.parent = "" }
	if f.observes == 2 {
		switch f.mode {
		case "changed-id": state.identity.FileID = "10"
		case "changed-created": state.identity.Created = "10"
		case "changed-acl": state.acl += "changed"
		case "changed-parent": state.parent = strings.Repeat("b",64)
		case "changed-size": state.size++
		case "late-observe-error": return state, errors.New("private final observation")
		}
	}
	if f.mode == "cancel-observe" || f.mode == "cancel-cleanup" { f.cancel() }
	return state, nil
}
func (m *guardTargetTestMetadata) Close() error {
	f := m.fixture
	f.leafCloses++
	f.events = append(f.events, "metadata-close")
	if f.mode == "metadata-close" { return errors.New("private leaf close text") }
	return nil
}

func TestGuardTargetAdmissionRejectsBeforeMetadataOpen(t *testing.T) {
	for _,mode := range []string{"nil-artifact","artifact-invalid","nil-purpose","purpose-invalid","wrong-session","cancel-before","no-callback","empty-payload"} {
		t.Run(mode,func(t *testing.T) {
			ctx,cancel := context.WithCancel(context.Background()); defer cancel()
			f := newGuardTargetFixture(mode,cancel)
			var artifact guardTargetArtifact = f.artifact
			var purpose guardTargetPurposeLease = f.purpose
			if mode == "nil-artifact" { artifact = nil }
			if mode == "nil-purpose" { purpose = nil }
			if mode == "wrong-session" { f.purpose.expected = &guardTargetTestArtifact{newGuardTargetFixture("normal",cancel)} }
			if mode == "cancel-before" { cancel() }
			r := inspectGuardTargetMetadata(ctx,artifact,purpose)
			if r.Qualified || r.MetadataObserved || f.opens != 0 || f.observes != 0 || r.SoftwareReplaceAllowed || r.DataAccessGranted || r.UpdatePerformed {
				t.Fatal("unqualified artifact/purpose opened target metadata",mode,r.Reason)
			}
			wantArtifact,wantPurpose := 1,1
			if artifact == nil { wantArtifact = 0 }
			if purpose == nil { wantPurpose = 0 }
			if f.artifactCloses != wantArtifact || f.purposeCloses != wantPurpose { t.Fatal("pre-open rejection did not close transferred resources once") }
		})
	}
}

func TestGuardTargetAdmissionObservationsAndCleanupRemainSeparate(t *testing.T) {
	modes := []string{"normal","open-error","open-cancel","nil-lease","observe-error","bad-id","bad-parent","changed-id","changed-created","changed-acl","changed-parent","changed-size","late-observe-error","metadata-close","purpose-close","artifact-close","cancel-observe","cancel-cleanup","double-callback","borrow-return-error","cancel-after-borrow","artifact-late","purpose-late","ignore-callback-error"}
	for _,mode := range modes {
		t.Run(mode,func(t *testing.T) {
			ctx,cancel := context.WithCancel(context.Background()); defer cancel()
			f := newGuardTargetFixture(mode,cancel)
			r := inspectGuardTargetMetadata(ctx,f.artifact,f.purpose)
			if r.Qualified != (mode == "normal") { t.Fatal("incorrect metadata qualification",mode,r.Reason) }
			if r.SoftwareReplaceAllowed || r.DataAccessGranted || r.UpdatePerformed { t.Fatal("metadata result became an update/data grant") }
			if f.opens != 1 || f.artifactCloses != 1 || f.purposeCloses != 1 { t.Fatal("ownership boundary opened or closed twice",mode) }
			wantClose := 1; if mode == "nil-lease" { wantClose = 0 }
			if f.leafCloses != wantClose { t.Fatal("metadata lease close not accounted",mode) }
			if mode == "normal" && (!r.MetadataObserved || r.CurrentIdentity == nil || r.ACLHash == "" || r.ParentProof == "" || r.Bytes != 4096 || f.observes != 2) { t.Fatal("normal held metadata was not rechecked") }
			if mode == "changed-id" || mode == "changed-created" || mode == "changed-acl" || mode == "changed-parent" || mode == "changed-size" {
				if r.Reason != "guard_target_generation_changed" || !r.MetadataObserved { t.Fatal("observed generation change was lost") }
			}
			cleanup := mode == "metadata-close" || mode == "purpose-close" || mode == "artifact-close" || mode == "cancel-cleanup"
			if r.CleanupFailed != cleanup { t.Fatal("cleanup fact was lost",mode) }
			cancelled := mode == "open-cancel" || mode == "cancel-observe" || mode == "cancel-cleanup" || mode == "cancel-after-borrow"
			if r.Cancelled != cancelled { t.Fatal("cancellation fact was lost",mode) }
			raw,err := json.Marshal(r)
			if err != nil || bytes.Contains(raw,[]byte("private")) || bytes.Contains(raw,[]byte("C:/")) || bytes.Contains(raw,[]byte("O:BAG:")) { t.Fatal("raw metadata or error text escaped") }
			if len(f.events) < 2 || f.events[len(f.events)-2] != "purpose-close" || f.events[len(f.events)-1] != "artifact-close" { t.Fatal("purpose/artifact lifetime release order changed") }
		})
	}
}

func TestGuardTargetNormalFactoriesRemainUnavailable(t *testing.T) {
	ctx := context.Background()
	a,err := guardTargetOpenNormalArtifact(ctx)
	if a != nil || !errors.Is(err,errGuardTargetArtifactUnavailable) { t.Fatal("missing genuine artifact factory granted a candidate") }
	p,err := guardTargetOpenNormalPurpose(ctx,nil)
	if p != nil || !errors.Is(err,errGuardTargetPurposeUnavailable) { t.Fatal("missing fixed target purpose factory granted access") }
	r := observeNormalGuardTargetMetadata(ctx)
	if r.Qualified || r.MetadataObserved || r.CurrentIdentity != nil || r.SoftwareReplaceAllowed || r.DataAccessGranted || r.UpdatePerformed { t.Fatal("normal fixed failure touched target") }
	var out bytes.Buffer
	if exit := runGuardTargetMetadata([]string{"--inspect-guard-target-update"},&out); exit != 1 { t.Fatal("normal missing factory did not fail") }
	for _,args := range [][]string{nil,{"--inspect-guard-target-update","C:/arbitrary"},{"--arbitrary"}} {
		out.Reset()
		if exit := runGuardTargetMetadata(args,&out); exit != 2 || out.Len() != 0 { t.Fatal("extra selector or path accepted") }
	}
}

func TestGuardTargetNativeOwnedMetadataHandleCannotReadContents(t *testing.T) {
	dir := physicalSoftwareFixture(t)
	path := filepath.Join(dir,"owned.exe")
	if err := os.WriteFile(path,[]byte("MZ owned metadata fixture"),0600); err != nil { t.Fatal(err) }
	plain,err := filepath.EvalSymlinks(path)
	if err != nil { t.Fatal(err) }
	lease,err := openGuardTargetMetadataAt(context.Background(),plain)
	if lease != nil { defer lease.Close() }
	if err != nil { t.Fatal("owned metadata open",err) }
	first,err := lease.Observe()
	if err != nil || !guardTargetMetadataShape(first) { t.Fatal("owned metadata observation",err) }
	second,err := lease.Observe()
	if err != nil || first != second { t.Fatal("same held metadata changed",err) }
	native := lease.(*guardTargetNativeMetadata)
	var raw [1]byte
	var count uint32
	err = syscall.ReadFile(syscall.Handle(native.leaf.Fd()),raw[:],&count,nil)
	if err != syscall.ERROR_ACCESS_DENIED || count != 0 { t.Fatal("metadata-only handle acquired file-data access") }
	if err = lease.Close(); err != nil { t.Fatal(err) }
	if _,err = lease.Observe(); err == nil { t.Fatal("closed metadata lease reused") }
	if err = lease.Close(); err != nil { t.Fatal("second close was not inert") }
}

func TestGuardTargetNativeInitialCancellationOpensNothing(t *testing.T) {
	ctx,cancel := context.WithCancel(context.Background()); cancel()
	lease,err := openGuardTargetMetadataAt(ctx,"not-an-installed-program")
	if err != context.Canceled { t.Fatal("initial context cancellation changed") }
	native := lease.(*guardTargetNativeMetadata)
	if native.leaf != nil || len(native.parents) != 0 { t.Fatal("cancelled metadata opener touched namespace") }
	if native.Close() != nil { t.Fatal("empty lease cleanup failed") }
}

type guardTargetFactoryPurpose struct {
	base *guardTargetTestPurpose
	artifactWasClosed bool
	closeFailure bool
}
func (p *guardTargetFactoryPurpose) Recheck(ctx context.Context, artifact guardTargetArtifact) error {
	return p.base.Recheck(ctx, artifact)
}
func (p *guardTargetFactoryPurpose) OpenMetadata(ctx context.Context) (guardTargetMetadataLease,error) {
	return p.base.OpenMetadata(ctx)
}
func (p *guardTargetFactoryPurpose) Close() error {
	p.artifactWasClosed = p.base.fixture.closed
	err := p.base.Close()
	if p.closeFailure { return errors.Join(err, errors.New("private partial purpose cleanup")) }
	return err
}

func TestGuardTargetFactoriesReleasePartialResourcesInDependencyOrder(t *testing.T) {
	for _,factoryStage := range []string{"artifact","purpose"} {
		cleanupModes := []string{"none","artifact"}
		if factoryStage=="purpose" { cleanupModes=[]string{"none","purpose","artifact","both"} }
		for _,fault := range []string{"primary","cancel-only","primary-and-cancel"} {
			for _,cleanup := range cleanupModes {
				t.Run(factoryStage+"/"+fault+"/"+cleanup,func(t *testing.T) {
					ctx,cancel := context.WithCancel(context.Background());defer cancel()
					mode:="normal"
					if cleanup=="artifact" || cleanup=="both" {mode="artifact-close"}
					f:=newGuardTargetFixture(mode,cancel)
					partial:=&guardTargetFactoryPurpose{base:f.purpose,closeFailure:cleanup=="purpose" || cleanup=="both"}
					artifactCalls,purposeCalls:=0,0
					factoryError:=func()error {
						if fault=="cancel-only" { cancel();return context.Canceled }
						if fault=="primary-and-cancel" { cancel() }
						return errors.New("private acquisition failure")
					}
					factories:=guardTargetMetadataFactories{
						artifact:func(context.Context)(guardTargetArtifact,error){
							artifactCalls++
							if factoryStage=="artifact" { return f.artifact,factoryError() }
							return f.artifact,nil
						},
						purpose:func(_ context.Context, actual guardTargetArtifact)(guardTargetPurposeLease,error){
							purposeCalls++
							if actual!=f.artifact {t.Fatal("factory received a different artifact")}
							return partial,factoryError()
						},
					}
					r:=observeGuardTargetMetadataWith(ctx,factories)
					want:="guard_target_"+factoryStage+"_unavailable"
					if fault=="cancel-only" {want="guard_target_cancelled"}
					if r.Qualified || r.MetadataObserved || r.Reason!=want || f.opens!=0 || f.observes!=0 || f.borrows!=0 {t.Fatal("partial acquisition inspected or qualified resources",r.Reason,want)}
					if artifactCalls!=1 || purposeCalls!=map[bool]int{true:1,false:0}[factoryStage=="purpose"] {t.Fatal("factory call boundary changed")}
					wantPurpose:=0;if factoryStage=="purpose" {wantPurpose=1}
					if f.artifactCloses!=1 || f.purposeCloses!=wantPurpose || partial.artifactWasClosed {t.Fatal("partial resource dependency closed in the wrong order")}
					if factoryStage=="purpose" && (len(f.events)!=2 || f.events[0]!="purpose-close" || f.events[1]!="artifact-close") {t.Fatal("partial purpose did not release before artifact")}
					if r.Cancelled!=(fault!="primary") || r.CleanupFailed!=(cleanup!="none") {t.Fatal("partial acquisition failure facts were lost")}
					if r.SoftwareReplaceAllowed || r.DataAccessGranted || r.UpdatePerformed {t.Fatal("partial metadata acquisition granted writes")}
					raw,_:=json.Marshal(r);if bytes.Contains(raw,[]byte("private")) {t.Fatal("factory free error escaped")}
				})
			}
		}
	}
}

func TestGuardTargetFactoryNormalAndEmptyResultsStayWithinBoundary(t *testing.T) {
	for _,mode:=range []string{"normal","artifact-nil","purpose-nil","artifact-invalid","cancel-before"} {
		t.Run(mode,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			fixtureMode:="normal";if mode=="artifact-invalid" {fixtureMode=mode}
			f:=newGuardTargetFixture(fixtureMode,cancel)
			aCalls,pCalls:=0,0
			if mode=="cancel-before" {cancel()}
			r:=observeGuardTargetMetadataWith(ctx,guardTargetMetadataFactories{
				artifact:func(context.Context)(guardTargetArtifact,error){aCalls++;if mode=="artifact-nil" {return nil,nil};return f.artifact,nil},
				purpose:func(context.Context,guardTargetArtifact)(guardTargetPurposeLease,error){pCalls++;if mode=="purpose-nil" {return nil,nil};return f.purpose,nil},
			})
			if r.Qualified!=(mode=="normal") || r.SoftwareReplaceAllowed || r.DataAccessGranted || r.UpdatePerformed {t.Fatal("normal factory boundary changed")}
			wantArtifact:=1;if mode=="artifact-nil" || mode=="cancel-before" {wantArtifact=0}
			wantPurpose:=0;if mode=="normal" {wantPurpose=1}
			if f.artifactCloses!=wantArtifact || f.purposeCloses!=wantPurpose {t.Fatal("factory empty result closed unacquired resources")}
			if mode!="normal" && (f.opens!=0 || f.borrows!=0) {t.Fatal("empty or invalid factory result opened metadata")}
			if mode=="cancel-before" && (aCalls!=0 || pCalls!=0) {t.Fatal("initial cancellation called factories")}
		})
	}
}
