//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type guardSoftwareTestLease struct {
	snapshot guardSoftwareSnapshot
	mode     string
	closes   int
	observes int
}

func (l *guardSoftwareTestLease) Snapshot(ctx context.Context) (guardSoftwareSnapshot, error) {
	if l.mode == "snapshot-error" { return guardSoftwareSnapshot{}, errors.New("private snapshot error") }
	v := l.snapshot
	if l.mode == "wrong-hash" { v.sha = strings.Repeat("d", 64) }
	if l.mode == "wrong-origin" { v.revision = strings.Repeat("d", 40) }
	if l.mode == "cancel-snapshot" { return guardSoftwareSnapshot{}, context.Canceled }
	return v, nil
}
func (l *guardSoftwareTestLease) Observe() (guardTargetMetadata, error) {
	l.observes++
	v := l.snapshot.metadata
	switch l.mode {
	case "id-change": v.identity.FileID = "900"
	case "acl-change": v.acl += "changed"
	case "parent-change": v.parent = strings.Repeat("f", 64)
	case "size-change": v.size++
	}
	return v, nil
}
func (l *guardSoftwareTestLease) Close() error {
	l.closes++
	if l.mode == "close-failure" { return errors.New("private software close error") }
	return nil
}

type guardSoftwareTestPurpose struct {
	artifact guardTargetArtifact
	lease    guardSoftwareLease
	mode     string
	checks   int
	opens    int
	closes   int
}
func (p *guardSoftwareTestPurpose) Recheck(ctx context.Context, a guardTargetArtifact) error {
	p.checks++
	if ctx.Err() != nil { return ctx.Err() }
	if a != p.artifact || p.mode == "purpose-error" || p.mode == "purpose-late" && p.checks > 2 { return errGuardTargetPurposeUnavailable }
	return nil
}
func (p *guardSoftwareTestPurpose) OpenSoftware(context.Context) (guardSoftwareLease, error) {
	p.opens++
	if p.mode == "partial-open" { return p.lease, errors.New("private partial open") }
	if p.mode == "nil-lease" { return nil, nil }
	return p.lease, nil
}
func (p *guardSoftwareTestPurpose) Close() error {
	p.closes++
	if p.mode == "purpose-close" { return errors.New("private purpose close error") }
	return nil
}

func softwareTestSnapshot() guardSoftwareSnapshot {
	return guardSoftwareSnapshot{
		metadata: guardTargetMetadata{identity: softwareIdentity{Volume: "1", FileID: "2", Created: "3"},
			acl: "owned synthetic owner group dacl", parent: strings.Repeat("a", 64), size: 4096},
		sha: installedGuardSHA, revision: installedGuardRevision,
	}
}

func TestGuardSoftwareControllerBindsHeldIdentityAndKeepsWriteAuthorityFalse(t *testing.T) {
	modes := []string{"normal", "snapshot-error", "wrong-hash", "wrong-origin", "id-change", "acl-change", "parent-change", "size-change", "close-failure", "purpose-error", "purpose-late", "partial-open", "nil-lease", "purpose-close", "cancel-snapshot"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			f := newGuardTargetFixture("normal", nil)
			l := &guardSoftwareTestLease{snapshot: softwareTestSnapshot(), mode: mode}
			p := &guardSoftwareTestPurpose{artifact: f.artifact, lease: l, mode: mode}
			r := inspectGuardSoftware(context.Background(), f.artifact, p)
			if r.Qualified != (mode == "normal") { t.Fatal("incorrect software qualification", mode, r.Reason) }
			if r.SoftwareReplaceAllowed || r.DataAccessGranted || r.UpdatePerformed { t.Fatal("software observation became write/data authority") }
			if p.closes != 1 || f.artifactCloses != 1 { t.Fatal("transferred owners not closed once") }
			wantOpen := 1
			if mode == "purpose-error" { wantOpen = 0 }
			if p.opens != wantOpen { t.Fatal("invalid purpose opened software") }
			wantClose := wantOpen
			if mode == "nil-lease" { wantClose = 0 }
			if l.closes != wantClose { t.Fatal("software partial/normal close count mismatch") }
			if mode == "normal" {
				if !r.SoftwareObserved || !r.MetadataObserved || r.SHA256 != installedGuardSHA || r.SourceRevision != installedGuardRevision || r.CurrentIdentity == nil || r.ACLHash == "" || r.ParentProof == "" {
					t.Fatal("same-buffer software identity absent")
				}
			} else if r.SoftwareObserved || r.SHA256 != "" || r.SourceRevision != "" {
				t.Fatal("unqualified result published software identity")
			}
			if r.CleanupFailed != (mode == "close-failure" || mode == "purpose-close") { t.Fatal("cleanup failure erased") }
			if mode == "purpose-close" && r.Reason != "guard_target_cleanup_failed" { t.Fatal("normal result cleanup failure not classified") }
			if r.Cancelled != (mode == "cancel-snapshot") { t.Fatal("cancellation observation erased") }
			raw, _ := json.Marshal(r)
			if bytes.Contains(raw, []byte("private")) || bytes.Contains(raw, []byte("owned synthetic")) { t.Fatal("raw error or ACL escaped") }
		})
	}
}

func TestGuardSoftwareControllerRejectsArtifactAndCallbackFaultsBeforeOrAfterRead(t *testing.T) {
	for _, mode := range []string{"artifact-invalid", "no-callback", "double-callback", "empty-payload", "borrow-return-error", "artifact-late", "artifact-close"} {
		t.Run(mode, func(t *testing.T) {
			f := newGuardTargetFixture(mode, nil)
			l := &guardSoftwareTestLease{snapshot: softwareTestSnapshot()}
			p := &guardSoftwareTestPurpose{artifact: f.artifact, lease: l}
			r := inspectGuardSoftware(context.Background(), f.artifact, p)
			if r.Qualified || r.SoftwareObserved || r.SHA256 != "" || r.UpdatePerformed || r.DataAccessGranted { t.Fatal("faulty artifact/callback retained qualification") }
			if f.artifactCloses != 1 || p.closes != 1 || l.closes > 1 { t.Fatal("artifact fault changed cleanup ownership") }
			if (mode == "artifact-invalid" || mode == "no-callback" || mode == "empty-payload") && p.opens != 0 { t.Fatal("preopen rejection read target") }
			switch mode {
			case "borrow-return-error", "double-callback":
				if !r.MetadataObserved || r.Reason != "guard_target_artifact_unavailable" { t.Fatal("outer borrow failure misreported as missing metadata", r.Reason) }
			case "artifact-late":
				if !r.MetadataObserved || r.Reason != "guard_target_generation_changed" { t.Fatal("late source generation failure misreported", r.Reason) }
			case "artifact-close":
				if !r.CleanupFailed || r.Reason != "guard_target_cleanup_failed" { t.Fatal("cleanup primary diagnosis changed", r.Reason) }
			}
		})
	}
}

func TestGuardSoftwarePurposeRequiresSamePrivateCandidateBorrowAndSingleOpen(t *testing.T) {
	ctx := context.Background()
	if p, err := guardSoftwarePurposeForArtifact(ctx, nil); p != nil || err == nil { t.Fatal("nil artifact produced purpose") }
	fake := newGuardTargetFixture("normal", nil)
	if p, err := guardSoftwarePurposeForArtifact(ctx, fake.artifact); p != nil || err == nil { t.Fatal("interface-only artifact produced purpose") }
	a, _ := guardCandidateOwnedFixture(t, ctx)
	defer a.Close()
	value, err := guardSoftwarePurposeForArtifact(ctx, a)
	if err != nil { t.Fatal(err) }
	p := value.(*guardSoftwarePurpose)
	defer p.Close()
	lease := &guardSoftwareTestLease{snapshot: softwareTestSnapshot()}
	opens := 0
	p.open = func(context.Context) (guardSoftwareLease, error) { opens++; return lease, nil }
	if l, err := p.OpenSoftware(ctx); l != nil || err == nil || opens != 0 { t.Fatal("software open outside candidate borrow") }
	other, _ := guardCandidateOwnedFixture(t, ctx)
	defer other.Close()
	if p.Recheck(ctx, other) == nil { t.Fatal("other candidate reused purpose") }
	copy := &guardSoftwarePurpose{self: p.self, artifact: p.artifact, candidate: p.candidate, generation: p.generation, ctx: p.ctx, open: p.open}
	if copy.Recheck(ctx, a) == nil { t.Fatal("copied purpose acquired authority") }
	err = a.WithPinnedPayload(func([]byte) error {
		got, err := p.OpenSoftware(ctx)
		if err != nil || got != lease || opens != 1 { t.Fatal("same candidate borrow not admitted") }
		if again, err := p.OpenSoftware(ctx); again != nil || err == nil || opens != 1 { t.Fatal("purpose opened more than once") }
		return got.Close()
	})
	if err != nil || lease.closes != 1 { t.Fatal("borrow/close mismatch", err) }
	p.Close()
	if p.Recheck(ctx, a) == nil { t.Fatal("closed purpose reused") }
}

func softwareTestChild(t *testing.T) askChildResult {
	t.Helper()
	v := guardSoftwareResult{guardTargetMetadataResult: guardTargetMetadataResult{
		Qualified: true, Reason: "verified_guard_target_software", Domain: guardSoftwareDomain, MetadataObserved: true,
		CurrentIdentity: &softwareIdentity{Volume: "1", FileID: "2", Created: "3"}, ACLHash: strings.Repeat("a", 64), ParentProof: strings.Repeat("b", 64), Bytes: 4096},
		SoftwareObserved: true, SHA256: installedGuardSHA, SourceRevision: installedGuardRevision, CredentialCleanupState: "completed"}
	raw, err := json.Marshal(v)
	if err != nil { t.Fatal(err) }
	return askChildResult{Raw: raw, Exit: 0, Started: true, Assigned: true, WaitVerified: true, Reaped: true,
		ResultStdoutComplete: true, ResultStderrComplete: true}
}

func TestGuardSoftwareWorkerOutputCannotMintGrantOrBypassLifecycle(t *testing.T) {
	normal := softwareTestChild(t)
	if r := guardSoftwareInspectionFromChild(normal); !r.Qualified || !r.WorkerResultObserved || r.UpdatePerformed || r.DataAccessGranted || r.SoftwareReplaceAllowed { t.Fatal("normal software observation failed") }
	for _, change := range []func(*askChildResult){
		func(c *askChildResult) { c.Started = false }, func(c *askChildResult) { c.Assigned = false },
		func(c *askChildResult) { c.WaitVerified = false }, func(c *askChildResult) { c.Reaped = false },
		func(c *askChildResult) { c.TimedOut = true }, func(c *askChildResult) { c.Exit = 2 },
		func(c *askChildResult) { c.ResultStdoutComplete = false }, func(c *askChildResult) { c.ResultStderrComplete = false },
	} {
		c := softwareTestChild(t); change(&c)
		if r := guardSoftwareInspectionFromChild(c); r.Qualified || r.SHA256 != "" { t.Fatal("unreaped/unbound worker accepted") }
	}
	for name, change := range map[string]func(*guardSoftwareResult){
		"hash": func(r *guardSoftwareResult) { r.SHA256 = strings.Repeat("f", 64) },
		"origin": func(r *guardSoftwareResult) { r.SourceRevision = strings.Repeat("f", 40) },
		"replace": func(r *guardSoftwareResult) { r.SoftwareReplaceAllowed = true },
		"data": func(r *guardSoftwareResult) { r.DataAccessGranted = true },
		"updated": func(r *guardSoftwareResult) { r.UpdatePerformed = true },
		"cleanup": func(r *guardSoftwareResult) { r.CleanupFailed = true },
		"credential": func(r *guardSoftwareResult) { r.CredentialCleanupState = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			c := softwareTestChild(t); var v guardSoftwareResult
			if json.Unmarshal(c.Raw, &v) != nil { t.Fatal("fixture") }; change(&v)
			c.Raw, _ = json.Marshal(v)
			if guardSoftwareInspectionFromChild(c).Qualified { t.Fatal("invalid worker JSON became qualified") }
		})
	}
	c := softwareTestChild(t)
	c.Raw = append(append([]byte(nil), bytes.TrimSuffix(c.Raw, []byte("}"))...), []byte(`,"qualified":true}`)...)
	if guardSoftwareInspectionFromChild(c).Qualified { t.Fatal("duplicate field accepted") }
	fail := guardSoftwareResult{guardTargetMetadataResult: guardTargetMetadataResult{Domain: guardSoftwareDomain, Reason: "guard_target_cleanup_failed", CleanupFailed: true}, CredentialCleanupState: "failed"}
	c = softwareTestChild(t); c.Exit = 1; c.Raw, _ = json.Marshal(fail)
	if r := guardSoftwareInspectionFromChild(c); r.Qualified || !r.WorkerResultObserved || !r.CleanupFailed { t.Fatal("fixed cleanup rejection lost") }
}

func TestGuardSoftwarePublicEntryRejectsPathsAndWorkerForgery(t *testing.T) {
	t.Setenv("ASK_SOFTWARE_PARENT_HANDLE", "")
	for _, args := range [][]string{nil, {guardSoftwareInspectionFlag, "C:/other"}, {"--other"}} {
		var out bytes.Buffer
		calls := 0
		exit := runGuardSoftwareInspectionWith(args, &out, func() guardSoftwareInspectionResult { calls++; return guardSoftwareInspectionResult{} })
		if exit != 2 || calls != 0 || out.Len() != 0 { t.Fatal("arbitrary selector reached inspection") }
	}
	var out bytes.Buffer
	calls := 0
	if exit := runGuardSoftwareInspectionWith([]string{guardSoftwareInspectionFlag}, &out, func() guardSoftwareInspectionResult {
		calls++
		return guardSoftwareInspectionFromChild(softwareTestChild(t))
	}); exit != 0 || calls != 1 || !bytes.Contains(out.Bytes(), []byte(installedGuardSHA)) {
		t.Fatal("one fixed public command did not return its qualified observation")
	}
	out.Reset()
	if exit := runGuardSoftwareWorker([]string{"wrong"}, &out); exit != 2 || out.Len() != 0 { t.Fatal("worker accepted wrong args") }
	if exit := run([]string{guardSoftwareInspectionFlag, "extra"}, &out, func() softwareObservation { t.Fatal("old observer called"); return softwareObservation{} }); exit != 2 { t.Fatal("public dispatcher accepted extra path") }
	// This test process is not an owned-child invocation and has no inherited parent pipe.
	if exit := runAskWorker([]string{guardSoftwareWorkerFlag}, &out); exit != 2 { t.Fatal("direct forged worker accepted") }
}

func TestGuardSoftwareNativeOwnedReadKeepsSameIdentityAndReadOnlyAccess(t *testing.T) {
	dir := physicalSoftwareFixture(t)
	path := filepath.Join(dir, "owned.exe")
	raw := []byte("MZ owned software-only fixture")
	if err := os.WriteFile(path, raw, 0600); err != nil { t.Fatal(err) }
	actual, err := filepath.EvalSymlinks(path)
	if err != nil { t.Fatal(err) }
	verifyCalls := 0
	if verifyInstalledGuardSoftware(raw) { t.Fatal("synthetic software fixture matched the strict real Guard pin") }
	lease, err := openGuardTargetSoftwareAt(context.Background(), actual, func(b []byte) bool { verifyCalls++; return bytes.Equal(b, raw) })
	if lease != nil { defer lease.Close() }
	if err != nil { t.Fatal(err) }
	first, err := lease.Snapshot(context.Background())
	if err != nil || first.sha != digest(raw) || verifyCalls != 1 || !guardTargetMetadataShape(first.metadata) { t.Fatal("same held snapshot failed", err) }
	after, err := lease.Observe()
	if err != nil || after != first.metadata { t.Fatal("held metadata changed") }
	native := lease.(*guardSoftwareNativeLease)
	var written uint32
	if err := syscall.WriteFile(syscall.Handle(native.data.Fd()), []byte("x"), &written, nil); err != syscall.ERROR_ACCESS_DENIED || written != 0 { t.Fatal("software inspection gained write access") }
	if err := lease.Close(); err != nil { t.Fatal(err) }
	if _, err := lease.Snapshot(context.Background()); err == nil { t.Fatal("closed lease read software") }
	if err := lease.Close(); err != nil { t.Fatal("second close changed cleanup", err) }
	if got, err := os.ReadFile(actual); err != nil || !bytes.Equal(got, raw) { t.Fatal("owned original changed") }
	denied, err := openGuardTargetSoftwareAt(context.Background(), actual, func([]byte) bool { return false })
	if denied != nil { defer denied.Close() }
	if err != nil { t.Fatal(err) }
	if _, err = denied.Snapshot(context.Background()); err == nil { t.Fatal("mismatching original was accepted") }
	ctx, cancel := context.WithCancel(context.Background()); cancel()
	empty, err := openGuardTargetSoftwareAt(ctx, "not-opened", func([]byte) bool { t.Fatal("verifier after initial cancel"); return true })
	if empty != nil { defer empty.Close() }
	if !errors.Is(err, context.Canceled) { t.Fatal("initial cancellation lost") }
}

func TestGuardSoftwareAcquisitionRejectsNonPrivateArtifactWithoutOpeningTarget(t *testing.T) {
	cases := []struct { name string; err error; audit string }{
		{"partial", errors.New("private acquisition"), "completed"},
		{"cancel", context.Canceled, "unknown"},
		{"credential-close", errGHAGuardCredentialCleanup, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newGuardTargetFixture("normal", nil)
			audit := &ghaFreshCredentialAudit{state: tc.audit}
			r := guardSoftwareInspectWith(context.Background(), audit, func(context.Context) (guardTargetArtifact, error) { return f.artifact, tc.err })
			if r.Qualified || r.SoftwareObserved || f.artifactCloses != 1 { t.Fatal("partial source became software authority") }
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f := newGuardTargetFixture("normal", nil)
	r := guardSoftwareInspectWith(ctx, &ghaFreshCredentialAudit{state: "completed"}, func(context.Context) (guardTargetArtifact, error) { return f.artifact, nil })
	if r.Qualified || r.Reason != "guard_target_purpose_unavailable" || f.artifactCloses != 1 { t.Fatal("interface-only candidate minted purpose") }
}

func TestGuardSoftwareCleanupKeepsPrimaryAndCancellationSeparate(t *testing.T) {
	f := newGuardTargetFixture("normal", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &guardSoftwareTestPurpose{artifact: f.artifact, mode: "purpose-close"}
	r := guardSoftwareResult{guardTargetMetadataResult: guardTargetMetadataResult{
		Domain: guardSoftwareDomain, Reason: "guard_target_software_unverified",
		Qualified: false, MetadataObserved: true}, SoftwareObserved: true,
		SHA256: installedGuardSHA, SourceRevision: installedGuardRevision}
	releaseGuardSoftwareResult(&r, ctx, f.artifact, p)
	if r.Qualified || r.Reason != "guard_target_software_unverified" ||
		!r.Cancelled || !r.CleanupFailed || r.SoftwareObserved || r.SHA256 != "" ||
		p.closes != 1 || f.artifactCloses != 1 {
		t.Fatal("primary software refusal/cancellation/cleanup facts were conflated")
	}
}

func TestGuardSoftwareSupervisorRequiresBoundedCompleteStreams(t *testing.T) {
	called := 0
	result := guardSoftwareSupervisedInspectionWith(func(ctx context.Context, executable string, args ...string) askChildResult {
		called++
		strict, _ := ctx.Value(askStrictJobKey{}).(bool)
		complete, _ := ctx.Value(askResultCompleteKey{}).(bool)
		if !strict || !complete || executable == "" || len(args) != 1 || args[0] != guardSoftwareWorkerFlag {
			t.Fatal("normal software supervisor did not select its bounded complete-stream worker")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 {
			t.Fatal("worker deadline missing")
		}
		return softwareTestChild(t)
	})
	if called != 1 || !result.Qualified || !result.WorkerResultObserved {
		t.Fatal("normal complete-stream worker not accepted")
	}
	for _, stdout := range []bool{false, true} {
		child := softwareTestChild(t)
		child.Exit = 1
		failure := guardSoftwareResult{guardTargetMetadataResult: guardTargetMetadataResult{
			Domain: guardSoftwareDomain, Reason: "guard_target_cleanup_failed", CleanupFailed: true}, CredentialCleanupState: "failed"}
		child.Raw, _ = json.Marshal(failure)
		if stdout { child.ResultStdoutComplete = false } else { child.ResultStderrComplete = false }
		if got := guardSoftwareInspectionFromChild(child); got.Qualified || got.WorkerResultObserved || got.SHA256 != "" {
			t.Fatal("exit-one result masked incomplete stream collection")
		}
	}
}

// Use an actual os.File pipe so os.File.WriteTo/io.Copy must dispatch through
// the destination's ReaderFrom, rather than testing only direct Write calls.
func guardSoftwarePipeResult(t *testing.T, payload []byte) (*askResultStream, int64, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil { t.Fatal(err) }
	readClosed := false
	defer func() { if !readClosed { _ = reader.Close() } }()
	written := make(chan error, 1)
	go func() {
		_, writeErr := io.Copy(writer, bytes.NewReader(payload))
		written <- errors.Join(writeErr, writer.Close())
	}()
	stream := newAskResultStream()
	n, copyErr := io.Copy(stream, reader)
	// An overflow may intentionally stop before EOF. Release this exact pipe
	// before joining its writer, including on the expected error path.
	readCloseErr := reader.Close()
	readClosed = true
	if readCloseErr != nil { t.Error("owned pipe read close", readCloseErr) }
	writeErr := <-written
	if copyErr == nil && writeErr != nil { t.Fatal("normal pipe writer failed", writeErr) }
	return stream, n, copyErr
}

func TestGuardSoftwareResultCollectorUsesBoundedPipeReaderFrom(t *testing.T) {
	for _, tc := range []struct { name string; size int; overflow bool }{
		{"empty", 0, false}, {"exact-limit", 65536, false}, {"limit-plus-one", 65537, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte("x"), tc.size)
			stream, n, err := guardSoftwarePipeResult(t, payload)
			defer clear(stream.output.Bytes())
			if stream.output.Len() > 65536 { t.Fatal("ReadFrom bypassed the result write limit") }
			if tc.overflow {
				if err == nil || stream.complete() || !stream.overflow || n > 65536 {
					t.Fatal("oversized pipe result was not bounded and refused")
				}
			} else if err != nil || n != int64(tc.size) || !stream.complete() || !bytes.Equal(stream.output.Bytes(), payload) {
				t.Fatal("complete boundary pipe result lost bytes or EOF", err)
			}
		})
	}
}

type guardSoftwareReadFailure struct {
	data []byte
	err error
}

func (r *guardSoftwareReadFailure) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 { return n, r.err }
	return n, nil
}

func TestGuardSoftwareResultCollectorRetainsReadFailureWithExitOne(t *testing.T) {
	child := softwareTestChild(t)
	child.Exit = 1
	failure := guardSoftwareResult{guardTargetMetadataResult: guardTargetMetadataResult{
		Domain: guardSoftwareDomain, Reason: "guard_target_software_unverified"}, CredentialCleanupState: "completed"}
	child.Raw, _ = json.Marshal(failure)
	readErr := errors.New("owned result read failed")
	stream := newAskResultStream()
	n, err := io.Copy(stream, &guardSoftwareReadFailure{data: child.Raw, err: readErr})
	defer clear(stream.output.Bytes())
	if !errors.Is(err, readErr) || n != int64(len(child.Raw)) || !stream.readFailed || stream.complete() {
		t.Fatal("full JSON bytes hid a terminal reader error", err)
	}
	child.Raw = append([]byte(nil), stream.output.Bytes()...)
	defer clear(child.Raw)
	child.ResultStdoutComplete = stream.complete()
	if got := guardSoftwareInspectionFromChild(child); got.WorkerResultObserved || got.Qualified || got.SHA256 != "" {
		t.Fatal("exit-one child masked copier failure")
	}
	// Clean EOF containing bytes is complete as a stream but stderr is not
	// silent; the supervisor must withhold Raw for this separate condition.
	stderr, _, err := guardSoftwarePipeResult(t, []byte("diagnostic"))
	defer clear(stderr.output.Bytes())
	if err != nil || !stderr.complete() || stderr.output.Len() == 0 {
		t.Fatal("non-empty stderr fixture did not reach the silent-stderr boundary", err)
	}
}

func TestGuardSoftwareOwnedChildStreamBoundary(t *testing.T) {
	const fixtureEnv = "GUARD_SOFTWARE_OWNED_STREAM_FIXTURE"
	if mode := os.Getenv(fixtureEnv); mode != "" {
		if !askWorkerParent() { os.Exit(2) }
		// The test child emits synthetic bytes only. It never enters the
		// production reader, source issuer or installed-target dispatcher.
		exit := 0
		switch mode {
		case "exact-stdout":
			_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 65536))
		case "overflow-stdout":
			_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 65537))
			exit = 1
		case "stderr":
			_, _ = os.Stdout.Write([]byte("owned result"))
			_, _ = os.Stderr.Write([]byte("diagnostic"))
		case "overflow-stderr":
			_, _ = os.Stdout.Write([]byte("owned result"))
			_, _ = os.Stderr.Write(bytes.Repeat([]byte("x"), 65537))
			exit = 1
		case "exit-one":
			_, _ = os.Stdout.Write([]byte("owned refusal"))
			exit = 1
		default:
			exit = 2
		}
		os.Exit(exit)
	}
	executable, err := os.Executable()
	if err != nil { t.Fatal(err) }
	for _, tc := range []struct { name string; wantBytes int; wantExit int; complete bool }{
		{"exact-stdout", 65536, 0, true}, {"overflow-stdout", 0, 1, false},
		{"stderr", 0, 0, true}, {"overflow-stderr", 0, 1, false}, {"exit-one", len("owned refusal"), 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ctx = context.WithValue(ctx, askStrictJobKey{}, true)
			ctx = context.WithValue(ctx, askResultCompleteKey{}, true)
			ctx = context.WithValue(ctx, askChildEnvironment{}, map[string]string{fixtureEnv: tc.name})
			child := askOwnedChild(ctx, executable, "-test.run=^TestGuardSoftwareOwnedChildStreamBoundary$")
			defer clear(child.Raw)
			if !child.Started || !child.Assigned || !child.WaitVerified || !child.Reaped || child.TimedOut || child.Exit != tc.wantExit || len(child.Raw) != tc.wantBytes {
				t.Fatal("owned worker stream outcome differs from bounded/silent policy")
			}
			if tc.complete {
				if !child.ResultStdoutComplete || !child.ResultStderrComplete || !child.WaitVerified || !child.Reaped {
					t.Fatal("normal complete streams lost EOF or actual lifecycle")
				}
			} else if child.ResultStdoutComplete && child.ResultStderrComplete {
				t.Fatal("overflow became a complete result")
			}
		})
	}
}

func TestGuardSoftwareAncestorCloseFailureRetainsOriginalOwnedHandle(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "complete-acquisition"
		if partial { name = "partial-acquisition" }
		t.Run(name, func(t *testing.T) {
			dir := physicalSoftwareFixture(t)
			path := filepath.Join(dir, "owned-ancestor-close.exe")
			raw := []byte("MZ owned ancestor close fixture")
			if err := os.WriteFile(path, raw, 0600); err != nil { t.Fatal(err) }
			actual, err := filepath.EvalSymlinks(path)
			if err != nil { t.Fatal(err) }
			lease, err := openGuardTargetSoftwareAt(context.Background(), actual, func(b []byte) bool { return bytes.Equal(b, raw) })
			if lease != nil { defer lease.Close() }
			if err != nil { t.Fatal(err) }
			native := lease.(*guardSoftwareNativeLease)
			if len(native.meta.parents) == 0 { t.Fatal("missing owned ancestor") }
			parent := native.meta.parents[len(native.meta.parents)-1]
			parentHandle := syscall.Handle(parent.Fd())
			// Direct same-held directory observation is the oracle. The
			// ordinary executable identity verifier must continue to reject it.
			if _, err := fileSoftwareIdentity(parent); err == nil { t.Fatal("ordinary software verifier admitted a directory") }
			var parentInfo syscall.ByHandleFileInformation
			if err := syscall.GetFileInformationByHandle(parentHandle, &parentInfo); err != nil { t.Fatal(err) }
			if parentInfo.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 || parentInfo.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				t.Fatal("fixture ancestor is not a plain directory")
			}
			identity := softwareIdentity{
				Volume: strconv.FormatUint(uint64(parentInfo.VolumeSerialNumber), 10),
				FileID: strconv.FormatUint(uint64(parentInfo.FileIndexHigh)<<32|uint64(parentInfo.FileIndexLow), 10),
				Created: strconv.FormatUint(uint64(parentInfo.CreationTime.HighDateTime)<<32|uint64(parentInfo.CreationTime.LowDateTime), 10),
			}
			if partial {
				if err := native.data.Close(); err != nil { t.Fatal(err) }
				if err := native.meta.leaf.Close(); err != nil { t.Fatal(err) }
				native.data, native.meta.leaf = nil, nil
			}
			wantAttempted := len(native.meta.parents)
			if native.meta.leaf != nil { wantAttempted++ }
			if native.data != nil { wantAttempted++ }
			var handles []syscall.Handle
			for _, file := range native.meta.parents { handles = append(handles, syscall.Handle(file.Fd())) }
			if native.meta.leaf != nil { handles = append(handles, syscall.Handle(native.meta.leaf.Fd())) }
			if native.data != nil { handles = append(handles, syscall.Handle(native.data.Fd())) }
			softwareUnconfirmedCleanup.Lock()
			priorOwners := len(softwareUnconfirmedCleanup.owners)
			softwareUnconfirmedCleanup.Unlock()
			if err := syscall.SetHandleInformation(parentHandle, 2, 2); err != nil { t.Fatal(err) }
			// This test alone restores and closes its exact pre-captured owner.
			// Production never retries an unconfirmed raw handle.
			defer func() {
				if err := syscall.SetHandleInformation(parentHandle, 2, 0); err != nil { t.Error("restore owned ancestor flags", err); return }
				if err := syscall.CloseHandle(parentHandle); err != nil { t.Error("recover exact owned ancestor", err) }
			}()
			closeErr := lease.Close()
			facts := softwareCleanupFacts(closeErr)
			if closeErr == nil || !errors.Is(closeErr, errSoftwareCleanupUnconfirmed) ||
				facts.Attempted != wantAttempted || facts.Failed != 1 || !native.closed {
				t.Fatal("ancestor cleanup outcome lost", closeErr, facts)
			}
			var failedOwner *softwareOwnedFile
			for _, resource := range native.pending {
				owner, ok := resource.(*softwareOwnedFile)
				if !ok { t.Fatal("raw ancestor escaped the existing owned wrapper") }
				if !owner.closeAttempted || !owner.closeCompleted || owner.closeCalls != 1 { t.Fatal("close not attempted exactly once") }
				if owner.handle == parentHandle { failedOwner = owner }
			}
			if failedOwner == nil || failedOwner.generation == 0 || !failedOwner.identityKnown ||
				failedOwner.identity != identity || failedOwner.closeErr == nil || failedOwner.file != parent {
				t.Fatal("original ancestor handle generation/identity not retained")
			}
			softwareUnconfirmedCleanup.Lock()
			newOwners := append([][]io.Closer(nil), softwareUnconfirmedCleanup.owners[priorOwners:]...)
			softwareUnconfirmedCleanup.Unlock()
			found := false
			for _, owners := range newOwners {
				for _, owner := range owners { if owner == failedOwner { found = true } }
			}
			if !found { t.Fatal("failed owner missing from unconfirmed cleanup retention") }
			for _, handle := range handles {
				var info syscall.ByHandleFileInformation
				err := syscall.GetFileInformationByHandle(handle, &info)
				if handle == parentHandle {
					if err != nil { t.Fatal("protected test ancestor unexpectedly closed", err) }
				} else if err == nil { t.Fatal("other owned handle was not closed") }
			}
			if again := lease.Close(); again == nil || failedOwner.closeCalls != 1 { t.Fatal("second lease close retried a native owner") }
			if got, err := os.ReadFile(actual); err != nil || !bytes.Equal(got, raw) { t.Fatal("owned source bytes changed") }
		})
	}
}
