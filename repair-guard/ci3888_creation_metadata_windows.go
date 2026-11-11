package main

// A separate, non-granting query validates only the independently observed
// public create stdout. It never opens the private issuer record, SDK or root.
import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const ci3888CreationFlag = "--verify-ci3888-runtime-creation"

func ci3888CreationFlagPresent(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimLeft(arg, "-")
		if arg == "verify-ci3888-runtime-creation" || strings.HasPrefix(arg, "verify-ci3888-runtime-creation=") {
			return true
		}
	}
	return false
}

func ci3888CreationMetadata() error {
	raw, ok := ci3888RuntimeRead(ci3888RuntimeCreationOutput, 8192)
	if !ok {
		return official3894Error("runtime_creation_output_unavailable")
	}
	if hashBytes(raw) != ci3888RuntimeCreationSHA {
		return official3894Error("runtime_creation_output_changed")
	}
	if _, ok := ci3888RuntimeCreation(raw); !ok {
		return official3894Error("runtime_creation_fields_mismatch")
	}
	return nil
}

func runCI3888CreationCLI(args []string, out io.Writer, verify func() error) int {
	result := object{"creation_verified": false, "purpose_verified": false, "access_allowed": false,
		"child_data_access": false, "sdk_qualified": false, "sdk_consumed": false,
		"launch_performed": false, "original_registry_recovered": false}
	code := 0
	if len(args) != 1 || args[0] != ci3888CreationFlag {
		result["reason"], code = "arguments", 2
	} else if err := verify(); err != nil {
		result["reason"], code = "purpose_or_io", 1
		var stage official3894Error
		if errors.As(err, &stage) {
			result["reason"] = string(stage)
		}
	} else {
		result["creation_verified"] = true
		result["stdout_sha256"] = ci3888RuntimeCreationSHA
	}
	if json.NewEncoder(out).Encode(result) != nil {
		return 1
	}
	return code
}
