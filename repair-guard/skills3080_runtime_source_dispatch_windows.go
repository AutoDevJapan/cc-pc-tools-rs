package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Drain both pipes, retain only bounded prefixes, and hash every observed byte.
// Only fixed error codes and selected admission metadata leave this process.
type skillsSourceCapture struct {
	prefix []byte
	limit  int
	bytes  uint64
	hash   hash.Hash
}

func (c *skillsSourceCapture) Write(p []byte) (int, error) {
	if c.hash == nil {
		c.hash = sha256.New()
	}
	c.hash.Write(p)
	c.bytes += uint64(len(p))
	if remaining := c.limit - len(c.prefix); remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		c.prefix = append(c.prefix, p[:remaining]...)
	}
	return len(p), nil
}

func (c *skillsSourceCapture) digest() string {
	if c.hash == nil {
		return hashBytes(nil)
	}
	return hex.EncodeToString(c.hash.Sum(nil))
}

type skillsSourceCommandResult struct {
	stdout     []byte
	diagnostic object
	ok         bool
}

func skillsSourceNormalError(stderr []byte) string {
	line := strings.TrimSpace(string(stderr))
	if _, _, ok := skillsSourceSubworkError(line); ok {
		return "InvalidSubwork"
	}
	for _, code := range []string{"InputRejected", "TimeoutError", "TimeoutExpired", "ResourceWait", "OSError", "PermissionError", "FileNotFoundError", "ValueError", "TypeError", "AttributeError", "RuntimeError", "InvalidSubwork"} {
		if line == "subwork dispatch failed "+code {
			return code
		}
	}
	return ""
}

func skillsSourceSubworkFields(stage, reason string) bool {
	if stage != "before_workspace" && stage != "after_workspace" {
		return false
	}
	switch reason {
	case "parent_missing", "relation_invalid", "parent_invalid", "repository_mismatch", "pr_mismatch", "dependency_cycle", "checkout_not_independent", "checkout_missing", "multiple_live_parents", "validation_failed":
		return true
	}
	return false
}

// The normal dispatcher prints this two-line grammar. Match the complete
// bounded stderr; an arbitrary prefix is not a diagnostic classification.
func skillsSourceSubworkError(stderr string) (string, string, bool) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(stderr), "\r\n", "\n"), "\n")
	if len(lines) != 2 || lines[1] != "subwork dispatch failed InvalidSubwork" {
		return "", "", false
	}
	const prefix = "subwork validation failed stage="
	if !strings.HasPrefix(lines[0], prefix) {
		return "", "", false
	}
	stage, reason, ok := strings.Cut(strings.TrimPrefix(lines[0], prefix), " reason=")
	return stage, reason, ok && skillsSourceSubworkFields(stage, reason)
}

func skillsRuntimeSourceRunHidden(command string, args []string, cwd string, env []string, seconds time.Duration) skillsSourceCommandResult {
	ctx, cancel := context.WithTimeout(context.Background(), seconds)
	defer cancel()
	c := exec.CommandContext(ctx, command, args...)
	c.Dir, c.Env = cwd, env
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	c.WaitDelay = time.Second
	stdout, stderr := &skillsSourceCapture{limit: 1 << 20}, &skillsSourceCapture{limit: 64 << 10}
	c.Stdout, c.Stderr = stdout, stderr
	err := c.Run()
	d := object{"outcome": "command_failed", "stdout_bytes": stdout.bytes, "stdout_sha256": stdout.digest(), "stderr_bytes": stderr.bytes, "stderr_sha256": stderr.digest()}
	if c.ProcessState != nil {
		d["exit_code"] = c.ProcessState.ExitCode()
	}
	if uint64(len(stderr.prefix)) == stderr.bytes {
		if code := skillsSourceNormalError(stderr.prefix); code != "" {
			d["command_error"] = code
			if stage, reason, ok := skillsSourceSubworkError(string(stderr.prefix)); ok {
				d["subwork_stage"], d["subwork_reason"] = stage, reason
			}
		}
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		d["win32_error"] = uint32(errno)
	}
	result := skillsSourceCommandResult{stdout: stdout.prefix, diagnostic: d}
	switch {
	case ctx.Err() != nil:
		d["outcome"] = "timeout"
	case errors.Is(err, exec.ErrWaitDelay):
		d["outcome"] = "drain_timeout"
	case c.ProcessState == nil:
		d["outcome"] = "start_failed"
	case stdout.bytes > uint64(stdout.limit):
		d["outcome"] = "output_limit"
	case err == nil:
		d["outcome"], result.ok = "completed", true
	}
	return result
}

// One interpretation serves both qualification and diagnostics. Only an
// explicit null with the exact task is a known non-launch; malformed or absent
// launch information never means that a new launch is safe to retry.
func skillsSourceDispatchLaunch(raw []byte, task int) (object, string, string) {
	if task <= 0 || !skillsNativeUniqueJSON(raw) || !skillsCreationHeaderKeys(raw, "id", "launched", "active", "busy", "busy_resources", "status", "phase", "priority_wait", "allowance_now", "preparation_reason") {
		return nil, "", "invalid_response"
	}
	v, ok := conversationJSON(raw)
	if !ok {
		return nil, "", "invalid_response"
	}
	if id, present := v["id"]; present && conversationNumber(id) != uint64(task) {
		return v, "", "invalid_response"
	}
	value, present := v["launched"]
	if !present {
		return v, "", "invalid_response"
	}
	if value == nil {
		if conversationNumber(v["id"]) == uint64(task) {
			return v, "", "not_launched"
		}
		return v, "", "invalid_response"
	}
	job := ""
	if text, ok := value.(string); ok {
		if conversationNumber(v["id"]) != uint64(task) {
			return v, "", "invalid_response"
		}
		job = text
	} else {
		b, err := json.Marshal(value)
		var launch struct {
			Item int    `json:"item"`
			Job  string `json:"job"`
		}
		if err != nil || !skillsStrict(b, &launch) || launch.Item != task {
			return v, "", "invalid_response"
		}
		job = launch.Job
	}
	if !skillsResultID(skillsResultRequest{Task: task, Job: job}) {
		return v, "", "invalid_response"
	}
	if v["active"] == true || v["busy"] == true || v["priority_wait"] != nil || v["preparation_reason"] != nil {
		return v, "", "invalid_response"
	}
	return v, job, "launched"
}

func skillsSourceDispatchAdmission(raw []byte, task int) object {
	v, _, outcome := skillsSourceDispatchLaunch(raw, task)
	return skillsSourceDispatchAdmissionValue(raw, v, outcome, task)
}

func skillsSourceDispatchAdmissionValue(raw []byte, v object, outcome string, task int, needs ...string) object {
	d := object{"outcome": "invalid_response", "stdout_bytes": uint64(len(raw)), "stdout_sha256": hashBytes(raw), "stderr_bytes": uint64(0), "stderr_sha256": hashBytes(nil), "exit_code": 0}
	if outcome == "invalid_response" {
		return d
	}
	d["outcome"] = outcome
	for _, status := range []string{"todo", "doing", "done", "block", "blocked", "wait"} {
		if v["status"] == status {
			d["status"] = status
		}
	}
	if outcome == "launched" {
		return d
	}
	for _, phase := range []string{"queued", "running", "retry", "finished", "preparing", "resumed", "cancelled", "waiting"} {
		if v["phase"] == phase {
			d["phase"] = phase
		}
	}
	reason := "unavailable"
	switch {
	case v["active"] == true:
		reason = "active"
	case d["status"] != nil && d["status"] != "todo":
		reason = "status_not_todo"
	case v["busy"] == true || skillsSourceBusyNeeds(v["busy_resources"], needs):
		reason = "busy"
	case conversationNumber(v["priority_wait"]) == uint64(task):
		reason = "priority_wait"
	case skillsSourcePreparationReason(str(v["preparation_reason"])):
		reason, d["preparation_reason"] = "preparation", v["preparation_reason"]
	case v["allowance_now"] == json.Number("0"):
		reason = "capacity"
	}
	d["admission_reason"] = reason
	return d
}

func skillsSourceBusyNeeds(value any, needs []string) bool {
	resources, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range resources {
		if _, ok := raw.(string); !ok {
			return false
		}
	}
	for _, raw := range resources {
		resource := raw.(string)
		for _, need := range needs {
			if resource == need {
				return true
			}
		}
	}
	return false
}

func skillsSourcePreparationReason(s string) bool {
	switch s {
	case "cwd_missing", "non_git", "origin_missing", "branch_mismatch", "repository_mismatch", "tool_missing", "resource_wait", "input_rejected", "subwork_parent_continuation_required", "excluded_resource", "tool_failed":
		return true
	}
	return false
}

// A failed CLI callback cannot use diagnostics as an arbitrary-data channel.
func skillsSourceDispatchDiagnostic(value any) (object, bool) {
	var d struct {
		Outcome       string  `json:"outcome"`
		StdoutBytes   uint64  `json:"stdout_bytes"`
		StdoutSHA     string  `json:"stdout_sha256"`
		StderrBytes   uint64  `json:"stderr_bytes"`
		StderrSHA     string  `json:"stderr_sha256"`
		Exit          *int    `json:"exit_code,omitempty"`
		WindowsError  *uint32 `json:"win32_error,omitempty"`
		CommandError  string  `json:"command_error,omitempty"`
		Admission     string  `json:"admission_reason,omitempty"`
		Status        string  `json:"status,omitempty"`
		Phase         string  `json:"phase,omitempty"`
		Preparation   string  `json:"preparation_reason,omitempty"`
		SubworkStage  string  `json:"subwork_stage,omitempty"`
		SubworkReason string  `json:"subwork_reason,omitempty"`
	}
	b, err := json.Marshal(value)
	if err != nil || !skillsStrict(b, &d) || !skillsDigest(d.StdoutSHA) || !skillsDigest(d.StderrSHA) || d.StdoutBytes > 1<<40 || d.StderrBytes > 1<<40 || (d.Exit != nil && (*d.Exit < -1 || *d.Exit > 0xffffffff)) {
		return nil, false
	}
	switch d.Outcome {
	case "completed", "command_failed", "timeout", "drain_timeout", "start_failed", "output_limit", "not_launched", "invalid_response", "launched":
	default:
		return nil, false
	}
	if d.CommandError != "" && skillsSourceNormalError([]byte("subwork dispatch failed "+d.CommandError)) == "" {
		return nil, false
	}
	switch d.Admission {
	case "", "active", "busy", "priority_wait", "preparation", "capacity", "status_not_todo", "unavailable":
	default:
		return nil, false
	}
	switch d.Status {
	case "", "todo", "doing", "done", "block", "blocked", "wait":
	default:
		return nil, false
	}
	switch d.Phase {
	case "", "queued", "running", "retry", "finished", "preparing", "resumed", "cancelled", "waiting":
	default:
		return nil, false
	}
	if d.Preparation != "" && !skillsSourcePreparationReason(d.Preparation) {
		return nil, false
	}
	if (d.SubworkStage != "" || d.SubworkReason != "") && (d.CommandError != "InvalidSubwork" || !skillsSourceSubworkFields(d.SubworkStage, d.SubworkReason)) {
		return nil, false
	}
	var out object
	if json.Unmarshal(b, &out) != nil {
		return nil, false
	}
	return out, true
}

func skillsRuntimeSourceDispatch(result skillsSourceCommandResult, task int, requestSHA, nonce string, needs ...string) object {
	if !result.ok {
		return object{"qualified": false, "reason": "source", "failure_stage": "source_normal_dispatch", "normal_dispatch": result.diagnostic, "data_access_granted": false}
	}
	out := skillsRuntimeSourceDispatchReceipt(result.stdout, task, requestSHA, nonce, needs...)
	d := obj(out["normal_dispatch"])
	for _, key := range []string{"stdout_bytes", "stdout_sha256", "stderr_bytes", "stderr_sha256", "exit_code"} {
		if v, exists := result.diagnostic[key]; exists {
			d[key] = v
		}
	}
	return out
}
