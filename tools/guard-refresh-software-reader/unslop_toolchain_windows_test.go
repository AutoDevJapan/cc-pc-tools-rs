//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type syntheticUnslopToolchainProvider struct {
	held rustToolchainHeld
	mode string
	opens, calls, checks int
}

func newSyntheticUnslopToolchainProvider() *syntheticUnslopToolchainProvider {
	cargo, rustc := []byte("owned-cargo-bytes"), []byte("owned-rustc-bytes")
	p := &syntheticUnslopToolchainProvider{}
	p.held = rustToolchainHeld{
		Origin: rustToolchainOrigin{Version: "1.95.0", Host: "x86_64-pc-windows-msvc", ManifestSHA: "821ff14e4c4a1cbe1e8915f35aff0a3fbbdf8d293ad48ab8f31e3b0440c581f9", Components: [3]rustToolchainComponent{
			{Name: "cargo", URL: "https://static.rust-lang.org/dist/2026-04-16/cargo-1.95.0-x86_64-pc-windows-msvc.tar.gz", ArchiveSHA: "2d68113a00b98f0dec6d0e8473f82e08cec00c392115933a57dbfe9d3c8b2d8c", ExecutableSHA: digest(cargo)},
			{Name: "rustc", URL: "https://static.rust-lang.org/dist/2026-04-16/rustc-1.95.0-x86_64-pc-windows-msvc.tar.gz", ArchiveSHA: "b1101cba184fda0da47658772d04423fdb86cc9ed888cac3b29d0e9f55faec53", ExecutableSHA: digest(rustc)},
			{Name: "rust-std", URL: "https://static.rust-lang.org/dist/2026-04-16/rust-std-1.95.0-x86_64-pc-windows-msvc.tar.gz", ArchiveSHA: "aa56f95b4817f562c0ada0abee3511a802a948303404e8fc872d0371ae0693fc"},
		}},
		Alias: rustToolchainAlias{InstallRoot: "C:/Users/makhmeto/.rustup", PhysicalRoot: "D:/CC/Toolchains/rustup", AliasIdentity: softwareIdentity{"1", "2", "3"}, ResolvedIdentity: softwareIdentity{"1", "4", "5"}, TargetIdentity: softwareIdentity{"1", "4", "5"}, AliasACLSHA: digest([]byte("alias-acl")), TargetACLSHA: digest([]byte("target-acl"))},
		Cargo: rustToolchainHeldFile{InstallPath: "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe", PhysicalPath: "D:/CC/Toolchains/rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe", Identity: softwareIdentity{"1", "6", "7"}, ViaInstallIdentity: softwareIdentity{"1", "6", "7"}, ACLSHA: digest([]byte("cargo-acl")), Raw: cargo},
		Rustc: rustToolchainHeldFile{InstallPath: "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe", PhysicalPath: "D:/CC/Toolchains/rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe", Identity: softwareIdentity{"1", "8", "9"}, ViaInstallIdentity: softwareIdentity{"1", "8", "9"}, ACLSHA: digest([]byte("rustc-acl")), Raw: rustc},
	}
	p.held.Recheck = func(context.Context) error {
		p.checks++
		if p.mode == "before-mutates" && p.checks == 1 { p.held.Cargo.Raw[0] ^= 1 }
		if p.mode == "before-check" || p.mode == "after-check" && p.checks == 2 { return errors.New("private failure path and contents") }
		return nil
	}
	return p
}

func (p *syntheticUnslopToolchainProvider) WithHeld(ctx context.Context, use func(rustToolchainHeld) bool) error {
	p.opens++
	defer func() { clear(p.held.Cargo.Raw); clear(p.held.Rustc.Raw) }()
	if p.mode == "open-error" { return errors.New("private failure path and contents") }
	if p.mode == "no-callback" { return nil }
	p.calls++
	accepted := use(p.held)
	if p.mode == "two-callbacks" { p.calls++; accepted = use(p.held) && accepted }
	if p.mode == "provider-error" || p.mode == "close-error" { return errors.New("private failure path and contents") }
	if p.mode == "context-error" { return context.DeadlineExceeded }
	if !accepted { return errUnslopToolchainConsumerFailed }
	return nil
}

func TestUnslopToolchainConsumerUsesFixedPurposeAndSameHeldBytes(t *testing.T) {
	p := newSyntheticUnslopToolchainProvider()
	if !rustToolchainInputsMatch(p.held) { t.Fatal("synthetic provider fixture does not satisfy shared matcher") }
	consumed := 0
	result := consumeUnslopToolchain(context.Background(), fixedUnslopToolchainPurpose(), p, func(ctx context.Context, cargo, rustc []byte) bool {
		consumed++
		if ctx.Err() != nil || &cargo[0] != &p.held.Cargo.Raw[0] || &rustc[0] != &p.held.Rustc.Raw[0] || string(cargo) != "owned-cargo-bytes" || string(rustc) != "owned-rustc-bytes" || p.checks != 1 { t.Fatal("consumer did not receive the held verified buffers") }
		return true
	})
	if !result.Verified || !result.Consumed || result.Reason != "toolchain_inputs_consumed" || consumed != 1 || p.opens != 1 || p.calls != 1 || p.checks != 2 { t.Fatal("normal fixed purpose was not fully consumed", result) }
	if purpose := fixedUnslopToolchainPurpose(); purpose.Source != unslopCaptureSource || purpose.Version != "1.95.0" || purpose.Target != "x86_64-pc-windows-msvc" || purpose.CommandSHA == "" { t.Fatal("unbound fixed toolchain purpose") }
}

func TestUnslopToolchainConsumerRejectsBeforeUseAndKeepsLateFailure(t *testing.T) {
	for _, mode := range []string{"open-error", "purpose-source", "purpose-command", "purpose-version", "purpose-target", "purpose-product", "purpose-domain", "no-callback", "two-callbacks", "nil-check", "before-check", "before-mutates", "after-check", "empty-cargo", "wrong-sha", "missing-identity", "consumer-failed", "consumer-mutates", "provider-error", "close-error", "context-error", "cancel-before", "cancel-in-consumer"} {
		t.Run(mode, func(t *testing.T) {
			p := newSyntheticUnslopToolchainProvider()
			p.mode = mode
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			purpose := fixedUnslopToolchainPurpose()
			wantReason := "toolchain_origin_unavailable"
			wantCalls := 1
			switch mode {
			case "open-error": wantCalls = 0
			case "purpose-source": purpose.Source = strings.Repeat("0", 40); wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "purpose-command": purpose.CommandSHA = digest([]byte("other command")); wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "purpose-version": purpose.Version = "other"; wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "purpose-target": purpose.Target = "other"; wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "purpose-product": purpose.Product = "other"; wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "purpose-domain": purpose.Domain = "other"; wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "no-callback": wantReason = "toolchain_callback_invalid"; wantCalls = 0
			case "two-callbacks": wantReason = "toolchain_callback_invalid"
			case "nil-check": p.held.Recheck = nil; wantCalls = 0
			case "before-check": wantCalls = 0
			case "before-mutates": wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "empty-cargo": p.held.Cargo.Raw = nil; wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "wrong-sha": p.held.Origin.Components[1].ExecutableSHA = digest([]byte("different")); wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "missing-identity": p.held.Cargo.Identity.Created = ""; wantReason = "toolchain_inputs_mismatch"; wantCalls = 0
			case "consumer-failed": wantReason = "toolchain_consumer_failed"
			case "consumer-mutates": wantReason = "toolchain_inputs_mismatch"
			case "context-error", "cancel-before", "cancel-in-consumer": wantReason = "toolchain_cancelled"; if mode == "cancel-before" { cancel(); wantCalls = 0 }
			}
			calls := 0
			result := consumeUnslopToolchain(ctx, purpose, p, func(_ context.Context, cargo, _ []byte) bool {
				calls++
				if mode == "consumer-mutates" { cargo[0] ^= 1 }
				if mode == "cancel-in-consumer" { cancel() }
				return mode != "consumer-failed"
			})
			if result.Verified || result.Reason != wantReason || calls != wantCalls || result.Consumed != (calls > 0) { t.Fatalf("failure incorrectly consumed or reported: mode=%s result=%+v calls=%d want=%s", mode, result, calls, wantReason) }
			if (mode == "cancel-before" || strings.HasPrefix(mode, "purpose-")) && p.opens != 0 { t.Fatal("invalid purpose or context reached provider") }
			raw, err := json.Marshal(result)
			if err != nil || strings.Contains(string(raw), "private failure") || strings.Contains(string(raw), "owned-cargo") || strings.Contains(string(raw), "/") { t.Fatal("failure leaked provider text or tool bytes") }
		})
	}
}

func TestUnslopToolchainNormalFactoryHasNoUnverifiedProvider(t *testing.T) {
	calls := 0
	result := normalUnslopToolchainConsumer(context.Background(), func(context.Context, []byte, []byte) bool { calls++; return true })
	if result.Verified || result.Consumed || calls != 0 || result.Reason != "unslop_toolchain_provider_unavailable" { t.Fatal("unconnected factory claimed a toolchain", result) }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = normalUnslopToolchainConsumer(ctx, func(context.Context, []byte, []byte) bool { t.Fatal("cancelled factory consumed input"); return true })
	if result.Verified || result.Consumed || result.Reason != "toolchain_cancelled" { t.Fatal("normal cancellation was discarded", result) }
}

func TestUnslopToolchainRejectsMissingInputsWithoutOpeningProvider(t *testing.T) {
	p := newSyntheticUnslopToolchainProvider()
	result := consumeUnslopToolchain(context.Background(), fixedUnslopToolchainPurpose(), p, nil)
	if result.Verified || result.Consumed || result.Reason != "toolchain_consumer_failed" || p.opens != 0 { t.Fatal("missing consumer opened a provider", result) }
	result = consumeUnslopToolchain(context.Background(), fixedUnslopToolchainPurpose(), nil, func(context.Context, []byte, []byte) bool { t.Fatal("missing provider consumed data"); return true })
	if result.Verified || result.Consumed || result.Reason != "toolchain_origin_provider_unavailable" { t.Fatal("missing provider accepted", result) }
}

func TestUnslopToolchainFailureProjectionOnlyReturnsFixedIDs(t *testing.T) {
	for _, id := range []string{"toolchain_origin_provider_unavailable", "toolchain_origin_unavailable", "toolchain_inputs_mismatch", "toolchain_cancelled", "toolchain_consumer_failed", "toolchain_callback_invalid"} {
		if got := unslopToolchainFailureReason(context.Background(), errors.New(id)); got != id { t.Fatal("fixed provider reason discarded", got, id) }
	}
	for _, message := range []string{"private failure path and contents", "toolchain_inputs_mismatch: C:/private/source", "prefix toolchain_cancelled", ""} {
		if got := unslopToolchainFailureReason(context.Background(), errors.New(message)); got != "toolchain_origin_unavailable" { t.Fatal("unbounded provider text escaped", got) }
	}
}

type changingUnslopProviderError struct { calls int }
func (e *changingUnslopProviderError) Error() string {
	e.calls++
	if e.calls == 1 { return "toolchain_inputs_mismatch" }
	return "private failure path and contents"
}
func TestUnslopToolchainFailureTextIsProjectedOnce(t *testing.T) {
	err := &changingUnslopProviderError{}
	if got := unslopToolchainFailureReason(context.Background(), err); got != "toolchain_inputs_mismatch" || err.calls != 1 { t.Fatal("mutable error text was reused", got, err.calls) }
}

// These probes run the real capture controller inside the toolchain callback.
// Compiler inputs and the publisher stay synthetic; no installed tool is read.
type unslopCaptureLifetimeProvider struct {
	*syntheticUnslopToolchainProvider
	active bool
	events []string
}
func (p *unslopCaptureLifetimeProvider) WithHeld(ctx context.Context, use func(rustToolchainHeld) bool) error {
	p.active = true
	p.events = append(p.events, "held")
	defer func() { p.active = false; p.events = append(p.events, "toolchain-close") }()
	return p.syntheticUnslopToolchainProvider.WithHeld(ctx, use)
}
type unslopCaptureLifetimeSession struct {
	*unslopSyntheticCapture
	provider *unslopCaptureLifetimeProvider
	t *testing.T
}
func (s *unslopCaptureLifetimeSession) event(name string) {
	s.t.Helper()
	if !s.provider.active { s.t.Fatal("toolchain released before capture completed", name) }
	s.provider.events = append(s.provider.events, name)
}
func (s *unslopCaptureLifetimeSession) Before() unslopCapturedInputs { s.event("before"); return s.unslopSyntheticCapture.Before() }
func (s *unslopCaptureLifetimeSession) Build(ctx context.Context) (unslopBuildInvocation, error) { s.event("build"); return s.unslopSyntheticCapture.Build(ctx) }
func (s *unslopCaptureLifetimeSession) After(ctx context.Context) (unslopCapturedInputs, error) { s.event("after"); return s.unslopSyntheticCapture.After(ctx) }
func (s *unslopCaptureLifetimeSession) WithArtifact(use func([]byte, softwareIdentity) bool) bool { s.event("artifact"); return s.unslopSyntheticCapture.WithArtifact(use) }
func (s *unslopCaptureLifetimeSession) Acknowledge(ctx context.Context) error { s.event("ack"); return s.unslopSyntheticCapture.Acknowledge(ctx) }
func (s *unslopCaptureLifetimeSession) AwaitCompletion(ctx context.Context) (unslopProducerCompletion, error) { s.event("done"); return s.unslopSyntheticCapture.AwaitCompletion(ctx) }
func (s *unslopCaptureLifetimeSession) Close() error { s.event("input-close"); return s.unslopSyntheticCapture.Close() }
type unslopCaptureLifetimePending struct {
	*syntheticUnslopPending
	provider *unslopCaptureLifetimeProvider
	t *testing.T
}
func (p *unslopCaptureLifetimePending) event(name string) {
	p.t.Helper()
	if !p.provider.active { p.t.Fatal("toolchain released before receipt completed", name) }
	p.provider.events = append(p.provider.events, name)
}
func (p *unslopCaptureLifetimePending) Seal(r unslopCaptureReceipt) error { p.event("seal"); return p.syntheticUnslopPending.Seal(r) }
func (p *unslopCaptureLifetimePending) Commit(ctx context.Context) error { p.event("commit"); return p.syntheticUnslopPending.Commit(ctx) }
func (p *unslopCaptureLifetimePending) Close() error { p.event("export-close"); return p.syntheticUnslopPending.Close() }

func TestUnslopToolchainCaptureRetainsInputsThroughReceiptAndLateClose(t *testing.T) {
	for _, mode := range []string{"normal", "provider-close", "capture-failed", "cancel-before", "no-provider", "no-factory", "factory-error", "duplicate-callback"} {
		t.Run(mode, func(t *testing.T) {
			provider := &unslopCaptureLifetimeProvider{syntheticUnslopToolchainProvider:newSyntheticUnslopToolchainProvider()}
			if mode == "provider-close" { provider.mode = "close-error" }
			if mode == "duplicate-callback" { provider.mode = "two-callbacks" }
			fixture := syntheticUnslopCapture()
			if mode == "capture-failed" { fixture.fault = "worker_failed" }
			session := &unslopCaptureLifetimeSession{fixture,provider,t}
			pending := &unslopCaptureLifetimePending{&syntheticUnslopPending{},provider,t}
			factories := 0
			factory := func(_ context.Context, held rustToolchainHeld, binding unslopRustToolchainBinding) (unslopCaptureAccess,error) {
				factories++
				if !provider.active || &held.Cargo.Raw[0] != &provider.held.Cargo.Raw[0] || !unslopHex(binding.SHA,64) { t.Fatal("factory not inside original toolchain lease") }
				if mode == "factory-error" { return unslopCaptureAccess{},errors.New("private factory error") }
				return unslopCaptureAccess{
					begin:func(context.Context)(unslopProducerSession,error){ session.event("begin"); return session,nil },
					stage:func(context.Context,unslopCaptureReceipt,[]byte)(unslopPendingCapture,error){ pending.event("stage"); return pending,nil },
				},nil
			}
			ctx,cancel := context.WithCancel(context.Background()); defer cancel()
			var origin rustToolchainOriginProvider = provider
			if mode == "cancel-before" { cancel() }
			if mode == "no-provider" { origin = nil }
			if mode == "no-factory" { factory = nil }
			r := captureUnslopReleaseWithToolchain(ctx,origin,factory)
			wantPublished := mode == "normal" || mode == "provider-close" || mode == "duplicate-callback"
			if r.Qualified != (mode == "normal") || r.ReceiptPublished != wantPublished || provider.active { t.Fatal("capture result lost late failure or published state",r,provider.events) }
			if mode != "normal" && r.Receipt != nil { t.Fatal("failed capture returned usable receipt") }
			if mode == "provider-close" && (pending.commits != 1 || r.Reason != "toolchain_origin_unavailable") { t.Fatal("late close replayed or hid publication",r,pending.commits) }
			if mode == "duplicate-callback" && (pending.commits != 1 || factories != 1 || r.Reason != "toolchain_callback_invalid") { t.Fatal("duplicate callback repeated capture",r,factories,pending.commits) }
			if (mode == "cancel-before" || mode == "no-provider" || mode == "no-factory") && (factories != 0 || provider.opens != 0) { t.Fatal("invalid call reached provider") }
			if mode == "capture-failed" && (pending.commits != 0 || r.Reason != "capture_producer_ack_or_exit_unverified") { t.Fatal("capture failure was overwritten",r) }
			if mode == "normal" {
				want := "held,begin,before,build,after,artifact,stage,ack,done,input-close,seal,commit,export-close,toolchain-close"
				if strings.Join(provider.events,",") != want { t.Fatal("input lifetime did not cover complete capture",provider.events) }
			}
		})
	}
}

func TestUnslopToolchainBindingChangesForEveryIdentityAndOriginField(t *testing.T) {
	provider := newSyntheticUnslopToolchainProvider()
	purpose := fixedUnslopToolchainPurpose()
	original,ok := bindUnslopRustToolchain(purpose,provider.held)
	if !ok || !unslopHex(original.SHA,64) { t.Fatal("valid binding fixture") }
	again,ok := bindUnslopRustToolchain(purpose,provider.held)
	if !ok || original != again { t.Fatal("binding is not deterministic") }
	for _,mode := range []string{"origin","components","alias-id","alias-resolved","alias-acl","target-acl","cargo-id","cargo-acl","cargo-bytes","rustc-id","rustc-acl","rustc-bytes","purpose","nil-check"} {
		t.Run(mode,func(t *testing.T){
			p := newSyntheticUnslopToolchainProvider()
			request := purpose
			switch mode {
			case "origin": p.held.Origin.ManifestSHA = strings.Repeat("a",64)
			case "components": p.held.Origin.Components[0],p.held.Origin.Components[1]=p.held.Origin.Components[1],p.held.Origin.Components[0]
			case "alias-id": p.held.Alias.AliasIdentity.Created = "20"
			case "alias-resolved": p.held.Alias.ResolvedIdentity.Created="40";p.held.Alias.TargetIdentity=p.held.Alias.ResolvedIdentity
			case "alias-acl": p.held.Alias.AliasACLSHA = digest([]byte("changed acl"))
			case "target-acl": p.held.Alias.TargetACLSHA=digest([]byte("changed target acl"))
			case "cargo-id": p.held.Cargo.Identity.Created = "30"; p.held.Cargo.ViaInstallIdentity = p.held.Cargo.Identity
			case "cargo-acl": p.held.Cargo.ACLSHA = digest([]byte("changed file acl"))
			case "cargo-bytes": p.held.Cargo.Raw[0] ^= 1; p.held.Origin.Components[0].ExecutableSHA = digest(p.held.Cargo.Raw)
			case "rustc-id": p.held.Rustc.Identity.Created="50";p.held.Rustc.ViaInstallIdentity=p.held.Rustc.Identity
			case "rustc-acl": p.held.Rustc.ACLSHA=digest([]byte("changed rustc acl"))
			case "rustc-bytes": p.held.Rustc.Raw[0]^=1;p.held.Origin.Components[1].ExecutableSHA=digest(p.held.Rustc.Raw)
			case "purpose": request.CommandSHA = digest([]byte("different command"))
			case "nil-check": p.held.Recheck = nil
			}
			got,valid := bindUnslopRustToolchain(request,p.held)
			if valid && got.SHA == original.SHA { t.Fatal("binding omitted changed field",mode) }
			mustReject := mode == "origin" || mode == "components" || mode == "purpose" || mode == "nil-check"
			if mustReject && valid { t.Fatal("invalid binding accepted",mode) }
			if !mustReject && !valid { t.Fatal("consistent changed generation fixture rejected before binding",mode) }
		})
	}
}

func TestUnslopWorkerRequiresItsOwnMatchingHeldBinding(t *testing.T) {
	for _,mode := range []string{"matching","mismatch","provider-close","worker-failure","cancelled","no-provider"} {
		t.Run(mode,func(t *testing.T){
			p := newSyntheticUnslopToolchainProvider()
			binding,ok := bindUnslopRustToolchain(fixedUnslopToolchainPurpose(),p.held)
			if !ok { t.Fatal("binding fixture") }
			expected := binding.SHA
			if mode == "mismatch" { expected = strings.Repeat("a",64) }
			if mode == "provider-close" { p.mode = "close-error" }
			ctx,cancel := context.WithCancel(context.Background()); defer cancel()
			if mode == "cancelled" { cancel() }
			var origin rustToolchainOriginProvider = p
			if mode == "no-provider" { origin = nil }
			calls:=0
			exit:=runUnslopWorkerWithToolchain(ctx,origin,expected,func(_ context.Context,h rustToolchainHeld,got unslopRustToolchainBinding)int {
				calls++
				if got.SHA != expected || &h.Rustc.Raw[0] != &p.held.Rustc.Raw[0] { t.Fatal("worker used other generation") }
				if mode == "worker-failure" { return 7 }; return 0
			})
			if (exit==0)!=(mode=="matching") || ((mode=="mismatch" || mode=="cancelled" || mode=="no-provider") && calls!=0) || calls>1 { t.Fatal("worker binding/exit contract",mode,exit,calls) }
			if mode=="worker-failure" && exit!=7 { t.Fatal("worker failure exit lost",exit) }
		})
	}
}
