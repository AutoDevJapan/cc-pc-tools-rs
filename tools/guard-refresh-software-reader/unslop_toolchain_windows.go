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
func consumeUnslopToolchain(ctx context.Context, purpose unslopToolchainPurpose, provider rustToolchainOriginProvider, use func(context.Context, []byte, []byte) bool) (result unslopToolchainResult) {
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
		consumed := use(ctx, held.Cargo.Raw, held.Rustc.Raw)
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
