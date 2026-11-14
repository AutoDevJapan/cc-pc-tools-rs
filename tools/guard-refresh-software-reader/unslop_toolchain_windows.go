//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
)

// This private purpose is made by the normal Unslop controller. It is never
// decoded from a command-line argument, environment value or caller proof.
type unslopToolchainPurpose struct {
	Domain, Product, Source, CommandSHA, Version, Target string
}

type unslopToolchainResult struct {
	Verified bool   `json:"verified"`
	Consumed bool   `json:"consumed"`
	Reason   string `json:"reason"`
}

// Consumed records callback invocation, even when a later check or provider
// cleanup fails. Verified is set only after WithHeld reports complete success.
var errUnslopToolchainConsumerFailed = errors.New("toolchain_consumer_failed")

func fixedUnslopToolchainPurpose() unslopToolchainPurpose {
	command, _ := json.Marshal(unslopCaptureCommand())
	return unslopToolchainPurpose{
		Domain: "godd-unslop-toolchain-input-v1", Product: "godd-unslop",
		Source: unslopCaptureSource, CommandSHA: digest(command),
		Version: "1.95.0", Target: "x86_64-pc-windows-msvc",
	}
}

// Matching bytes are useful only while the normal provider retains its input
// handles. This result is neither a general data grant nor permission to
// execute or install software. A consuming callback must not retain Raw.
func consumeUnslopToolchain(ctx context.Context, purpose unslopToolchainPurpose, provider rustToolchainOriginProvider, use func(context.Context, []byte, []byte) bool) unslopToolchainResult {
	if use == nil {
		return consumeUnslopToolchainHeld(ctx, purpose, provider, nil)
	}
	return consumeUnslopToolchainHeld(ctx, purpose, provider, func(ctx context.Context, held rustToolchainHeld) bool {
		return use(ctx, held.Cargo.Raw, held.Rustc.Raw)
	})
}

// The held view is borrowed only inside the normal provider callback. It lets
// the source-bound capture controller compare identity/ACL metadata without
// reopening paths or retaining tool buffers after WithHeld returns.
func consumeUnslopToolchainHeld(ctx context.Context, purpose unslopToolchainPurpose, provider rustToolchainOriginProvider, use func(context.Context, rustToolchainHeld) bool) (result unslopToolchainResult) {
	result.Reason = "toolchain_origin_provider_unavailable"
	if ctx.Err() != nil {
		result.Reason = "toolchain_cancelled"
		return
	}
	if purpose != fixedUnslopToolchainPurpose() {
		result.Reason = "toolchain_inputs_mismatch"
		return
	}
	if provider == nil {
		return
	}
	if use == nil {
		result.Reason = "toolchain_consumer_failed"
		return
	}
	callbacks := 0
	accepted := false
	result.Reason = "toolchain_origin_unavailable"
	err := provider.WithHeld(ctx, func(held rustToolchainHeld) bool {
		callbacks++
		if callbacks != 1 {
			result.Reason = "toolchain_callback_invalid"
			return false
		}
		if ctx.Err() != nil {
			result.Reason = "toolchain_cancelled"
			return false
		}
		if held.Recheck == nil {
			return false
		}
		if !rustToolchainInputsMatch(held) {
			result.Reason = "toolchain_inputs_mismatch"
			return false
		}
		if err := held.Recheck(ctx); err != nil {
			result.Reason = unslopToolchainFailureReason(ctx, err)
			return false
		}
		if ctx.Err() != nil {
			result.Reason = "toolchain_cancelled"
			return false
		}
		if !rustToolchainInputsMatch(held) {
			result.Reason = "toolchain_inputs_mismatch"
			return false
		}
		result.Consumed = true
		consumed := use(ctx, held)
		// Run both checks even after consumption fails. The provider still
		// owns the same input buffers and is responsible for closing them.
		checkErr := held.Recheck(ctx)
		matches := rustToolchainInputsMatch(held)
		if ctx.Err() != nil {
			result.Reason = "toolchain_cancelled"
			return false
		}
		if checkErr != nil {
			result.Reason = unslopToolchainFailureReason(ctx, checkErr)
			return false
		}
		if !matches {
			result.Reason = "toolchain_inputs_mismatch"
			return false
		}
		if !consumed {
			result.Reason = "toolchain_consumer_failed"
			return false
		}
		accepted = true
		return true
	})
	if ctx.Err() != nil {
		result.Reason = "toolchain_cancelled"
		return
	}
	if callbacks == 0 && err != nil {
		result.Reason = unslopToolchainFailureReason(ctx, err)
		return
	}
	if callbacks != 1 {
		result.Reason = "toolchain_callback_invalid"
		return
	}
	if err != nil {
		if accepted {
			result.Reason = unslopToolchainFailureReason(ctx, err)
		}
		return
	}
	if !accepted {
		return
	}
	result.Verified = true
	result.Reason = "toolchain_inputs_consumed"
	return
}

func unslopToolchainFailureReason(ctx context.Context, err error) string {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "toolchain_cancelled"
	}
	if err != nil {
		reason := err.Error()
		switch reason {
		case "toolchain_origin_provider_unavailable", "toolchain_origin_unavailable",
			"toolchain_inputs_mismatch", "toolchain_cancelled",
			"toolchain_consumer_failed", "toolchain_callback_invalid":
			return reason
		}
	}
	return "toolchain_origin_unavailable"
}

// The shared origin provider's normal adapter is deliberately not connected
// until its own official-payload and current-installation checks are qualified.
// No caller-provided path/hash/boolean can fill this factory in the meantime.
func normalUnslopToolchainConsumer(ctx context.Context, _ func(context.Context, []byte, []byte) bool) unslopToolchainResult {
	if ctx.Err() != nil {
		return unslopToolchainResult{Reason: "toolchain_cancelled"}
	}
	return unslopToolchainResult{Reason: "unslop_toolchain_provider_unavailable"}
}

// ToolchainSHA keeps its existing ordered four-buffer hash meaning. This
// distinct versioned digest covers only the two Rust executables, their fixed
// origin and install-alias metadata. It does not prove gate/sccache inputs or
// the standard-library, DLL or linker execution closure.
// A digest is a comparison value, never permission to construct a provider.
type unslopRustToolchainBinding struct { SHA string }
type unslopRustToolchainBoundFile struct {
	InstallPath string `json:"install_path"`
	PhysicalPath string `json:"physical_path"`
	Identity softwareIdentity `json:"identity"`
	ViaInstallIdentity softwareIdentity `json:"via_install_identity"`
	ACLSHA string `json:"acl_sha256"`
	SHA string `json:"sha256"`
	Bytes int `json:"bytes"`
}
type unslopRustToolchainBindingRecord struct {
	Domain string `json:"domain"`
	Purpose unslopToolchainPurpose `json:"purpose"`
	Origin rustToolchainOrigin `json:"origin"`
	Alias rustToolchainAlias `json:"alias"`
	Files [2]unslopRustToolchainBoundFile `json:"files"`
}

func bindUnslopRustToolchain(purpose unslopToolchainPurpose, held rustToolchainHeld) (unslopRustToolchainBinding, bool) {
	if purpose != fixedUnslopToolchainPurpose() || !rustToolchainInputsMatch(held) {
		return unslopRustToolchainBinding{}, false
	}
	record := unslopRustToolchainBindingRecord{Domain:"godd-unslop-rust-toolchain-binding-v1", Purpose:purpose, Origin:held.Origin, Alias:held.Alias}
	for i, file := range [2]rustToolchainHeldFile{held.Cargo, held.Rustc} {
		record.Files[i] = unslopRustToolchainBoundFile{InstallPath:file.InstallPath, PhysicalPath:file.PhysicalPath, Identity:file.Identity, ViaInstallIdentity:file.ViaInstallIdentity, ACLSHA:file.ACLSHA, SHA:digest(file.Raw), Bytes:len(file.Raw)}
	}
	raw,err:=json.Marshal(record)
	if err!=nil { return unslopRustToolchainBinding{},false }
	defer clear(raw)
	return unslopRustToolchainBinding{SHA:digest(raw)},true
}

// The normal controller supplies this code-bound factory. There is no input
// parser for constructing a capture access or a software-origin provider.
type unslopToolchainCaptureFactory func(context.Context, rustToolchainHeld, unslopRustToolchainBinding) (unslopCaptureAccess,error)

// Keep both layers' outcomes: cancellation or provider cleanup must not erase
// the capture's own cleanup failure. No worker collection fact is inferred.
type unslopToolchainCaptureResult struct {
	unslopCaptureResult
	CaptureAttempted bool `json:"capture_attempted"`
	CaptureReason string `json:"capture_reason,omitempty"`
	ToolchainReason string `json:"toolchain_reason,omitempty"`
}

func captureUnslopReleaseWithToolchain(ctx context.Context, provider rustToolchainOriginProvider, factory unslopToolchainCaptureFactory) unslopToolchainCaptureResult {
	result := unslopToolchainCaptureResult{unslopCaptureResult:unslopCaptureResult{Reason:"missing_producer_provenance"}}
	if ctx.Err()!=nil { result.Reason="unslop_cancelled"; result.ToolchainReason="toolchain_cancelled"; return result }
	if factory==nil { return result }
	called:=false
	outcome:=consumeUnslopToolchainHeld(ctx,fixedUnslopToolchainPurpose(),provider,func(ctx context.Context, held rustToolchainHeld)bool {
		binding,valid:=bindUnslopRustToolchain(fixedUnslopToolchainPurpose(),held)
		if !valid { return false }
		access,err:=factory(ctx,held,binding)
		if err!=nil { return false }
		called=true
		// Normal and collected-worker capture paths finish inside this lease.
		// The uncollected-worker path still needs a retained owner before
		// normal wiring; returning from this callback alone cannot provide it.
		result.CaptureAttempted=true
		result.unslopCaptureResult=captureUnslopRelease(ctx,access)
		result.CaptureReason=result.unslopCaptureResult.Reason
		return true
	})
	result.ToolchainReason=outcome.Reason
	if !outcome.Verified {
		result.Qualified=false
		result.Receipt=nil
		result.Reason=outcome.Reason
		// ReceiptPublished describes the real Commit, including when provider
		// cleanup fails later. Do not undo it or retry the same receipt here.
		return result
	}
	if !called { result.Reason="toolchain_callback_invalid";return result }
	return result
}

// Worker authorization is independently constructed by its normal factory.
// The expected binding is only a correlation check inside that qualified lease.
func runUnslopWorkerWithToolchain(ctx context.Context, provider rustToolchainOriginProvider, expected string, run func(context.Context,rustToolchainHeld,unslopRustToolchainBinding)int) int {
	if ctx.Err()!=nil || !unslopHex(expected,64) || run==nil { return 1 }
	workerExit:=1
	called:=false
	outcome:=consumeUnslopToolchainHeld(ctx,fixedUnslopToolchainPurpose(),provider,func(ctx context.Context, held rustToolchainHeld)bool {
		binding,valid:=bindUnslopRustToolchain(fixedUnslopToolchainPurpose(),held)
		if !valid || binding.SHA!=expected { return false }
		called=true
		workerExit=run(ctx,held,binding)
		return true
	})
	if called && workerExit!=0 { return workerExit }
	if !called || !outcome.Verified { return 1 }
	return 0
}
