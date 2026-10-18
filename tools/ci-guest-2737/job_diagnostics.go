package main

import "strings"

// Closed diagnostics contain neither observed values nor filesystem paths.
type hookDiagnostic struct {
	Version     int
	Stage, Code string
}

func validHookDiagnostic(d *hookDiagnostic) bool {
	if d == nil || d.Version != 1 || (d.Stage != "start" && d.Stage != "complete") {
		return false
	}
	switch d.Code {
	case "hook_plan_unavailable", "hook_plan_invalid", "event_path_outside_temp", "event_unavailable", "event_json_invalid", "event_inputs_invalid", "context_repository", "context_sha", "context_workflow_sha", "context_ref", "context_event", "context_run", "context_attempt", "context_job", "context_runner", "context_inputs", "hook_order", "host_admission", "owner_unavailable", "owner_transfer", "owner_rejected", "source_pin", "source_root", "source_config-open", "source_config-names", "source_metadata", "source_head", "source_index", "source_physical-blobs", "source_path-revalidation", "source_unknown":
		return true
	}
	return false
}

func sourceFailureCode(err error) string {
	code := "source_" + strings.TrimPrefix(err.Error(), "checkout proof rejected at ")
	if validHookDiagnostic(&hookDiagnostic{1, "complete", code}) && strings.HasPrefix(code, "source_") {
		return code
	}
	return "source_unknown"
}

type rustProbeResult struct {
	Name                                      string
	FailureCode                               string `json:",omitempty"`
	Exit                                      int
	StdoutBytes, StderrBytes                  int
	StdoutSHA256, StderrSHA256                string
	CompilerSHA256                            string `json:",omitempty"`
	Version, CommitHash, CommitDate, CacheKey string `json:",omitempty"`
}
