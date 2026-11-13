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
