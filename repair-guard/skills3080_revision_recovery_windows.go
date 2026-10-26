package main

import (
	"bufio"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const skillsRevisionRecoveryCall = "call_31fa406e37984d8c8b532a62dd25b922"
const skillsRevisionRecoveryRequest = "f05d30623e76c8cc886017877311340143a5ef1e3bc4f938051dbb83f91fb802"
const skillsRevisionRecoveryInput = "f49660100a01d8ad27abe051042f11d3edaf63b427465ffa3bd18ef2d6b25628"
const skillsRevisionRecoveryCanonical = "e8fbaf8ef72e16902e70f98e75120280f8393c9e083de4f48341d0c8ff529588"
const skillsRevisionRecoveryBlocks = "37b20cca9ffa6a1c274951695a13c2dca49dc83a58cc73d2ca5cc456b9154049"
const skillsRevisionRecoveryStdout = "c68b627e8b2c3f802c385152f1eb1b3f0392f7215ecf2fb30e1c17ae02e2a4fa"

// An audit of one historical mismatch, never a caller-supplied receipt or a
// path-normalization rule. Original native bytes remain unchanged.
type skillsRevisionRecoveryProof struct {
	CallID         string `json:"original_native_call_id"`
	InputSHA       string `json:"original_input_sha256"`
	CanonicalSHA   string `json:"canonical_input_sha256"`
	BlocksSHA      string `json:"original_tool_blocks_sha256"`
	StdoutSHA      string `json:"original_stdout_sha256"`
	CanonicalMatch bool   `json:"original_call_was_canonical"`
}

func skillsRevisionRecoveryAudit() skillsRevisionRecoveryProof {
	return skillsRevisionRecoveryProof{skillsRevisionRecoveryCall, skillsRevisionRecoveryInput, skillsRevisionRecoveryCanonical, skillsRevisionRecoveryBlocks, skillsRevisionRecoveryStdout, false}
}

func skillsRevisionRecoveryIdentity(c skillsConsumeRequest) bool {
	const workspace = `D:\CC\Work\unattended-operations-20261013-wt\todo-3080-16-skills-3048`
	return c.Creator == (skillsWriter{Task: 3080, Job: "20261026-090101-todo-3080", SpecSHA: "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435", Workspace: workspace}) &&
		c.RequestSHA == skillsRevisionRecoveryRequest && c.CallID == skillsRevisionRecoveryCall && len(c.Indices) == 0 &&
		c.RequestPath == workspace+`\.ci-work\todo3080\design-reference-own-revision1-prepare-request090101.json`
}

func skillsRevisionRecoveryInvocation(input, op, cwd string, at time.Time, c skillsConsumeRequest) bool {
	if op != "prepare-skills3080-revision" || cwd != c.Creator.Workspace || !skillsRevisionRecoveryIdentity(c) || at.Format(time.RFC3339Nano) != "2026-10-26T05:40:15.63Z" || hashBytes([]byte(input)) != skillsRevisionRecoveryInput {
		return false
	}
	// Even the fixed digest must decode as the same six-field static command.
	// No variable, syntax suffix, short name, ADS, alternate exe or path alias
	// is interpreted. Only this historical string has doubled separators.
	const prefix, suffix = "text(await tools.exec_command(", "));"
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, prefix) || !strings.HasSuffix(input, suffix) {
		return false
	}
	var got skillsCreationCommand
	want := skillsRevisionCommand(op, c.RequestPath, cwd)
	want.Cmd = strings.ReplaceAll(want.Cmd, `\`, `\\`)
	want.Workdir = strings.ReplaceAll(want.Workdir, `\`, `\\`)
	return skillsStrict([]byte(strings.TrimSuffix(strings.TrimPrefix(input, prefix), suffix)), &got) && got == want &&
		hashBytes([]byte(skillsRevisionInvocation(op, c.RequestPath, cwd))) == skillsRevisionRecoveryCanonical
}

func skillsRevisionRecoveryOutput(raw json.RawMessage, stdout string, at time.Time, e skillsRevisionEvidence) bool {
	return at.Format(time.RFC3339Nano) == "2026-10-26T05:40:24.854Z" && hashBytes(raw) == skillsRevisionRecoveryBlocks && hashBytes([]byte(stdout)) == skillsRevisionRecoveryStdout &&
		e.Recovery == nil && e.RequestSHA == skillsRevisionRecoveryRequest && e.ManifestSHA == skillsRevisionManifestSHA &&
		e.ManifestCommit == "53fcf35c8fc58161e739cb2b9c543607b63b6420" && e.Producer == (skillsProducer{"3a0f5065eb6ae56fdcc0a1f0ef78131f999e2331a0412b01faea91260adffeaa", "a067ff63db64a88fc218fc4c8400552454d5d691"})
}

// Before a new prepare writes anything, prove that its actual in-flight native
// call can later be received. A shell script, nested call, doubled path or
// completed/replayed call cannot leave unreceivable staging behind.
func skillsRevisionPendingCall(input io.Reader, thread, cwd, path string, start, end time.Time) bool {
	if end.Before(start) {
		return false
	}
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	session, pending := false, false
	seen := map[string]bool{}
	for scan.Scan() {
		var env struct {
			Type, Timestamp string
			Payload         json.RawMessage
		}
		if json.Unmarshal(scan.Bytes(), &env) != nil {
			return false
		}
		if env.Type != "session_meta" && env.Type != "response_item" {
			continue
		}
		var p struct {
			Type, ID, Cwd, Name, Input string
			CallID                     string `json:"call_id"`
		}
		if json.Unmarshal(env.Payload, &p) != nil {
			return false
		}
		if env.Type == "session_meta" {
			if session || p.ID != thread || !same(p.Cwd, cwd) {
				return false
			}
			session = true
			continue
		}
		pending = false
		if p.Type != "custom_tool_call" || p.Name != "exec" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, env.Timestamp)
		if err == nil && at.Before(start) {
			continue
		}
		if !session || err != nil || at.After(end) || !skillsCallID.MatchString(p.CallID) || seen[p.CallID] {
			return false
		}
		seen[p.CallID] = true
		pending = skillsRevisionCall(p.Input, "prepare-skills3080-revision", path, cwd)
	}
	return scan.Err() == nil && session && pending
}

func skillsRevisionPrepareNative(s *skillsFS, w skillsWriter, path string) bool {
	job, ok := skillsReadJob(s, w.Job)
	if !ok || job["status"] != "running" || !same(str(job["cwd"]), w.Workspace) {
		return false
	}
	start, err := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	if err != nil {
		return false
	}
	input, ok := skillsNativeSnapshot(s, job)
	return ok && skillsRevisionPendingCall(input, str(job["thread_id"]), w.Workspace, path, start, time.Now())
}

// Read-only validation for the recovered receipt in any legitimate transaction
// state. A separate handle set is closed before apply reopens and revalidates
// its mutation handles. All 51 unaffected files stay locked by the caller.
func skillsRevisionRecoveredState(e skillsRevisionEvidence, original skillsCreatedEvidence, root string, blocked func(string) bool) string {
	s := skillsFS{blocked: blocked}
	defer s.close()
	if reason := skillsRevisionDirectories(original, root, &s); reason != "" {
		return reason
	}
	hpath := filepath.Join(root, filepath.FromSlash(e.History.Relative))
	if blocked(hpath) {
		return "protection"
	}
	h, ok := s.directory(hpath)
	id, valid := skillsHandleID(h)
	if !ok || !valid || id != e.History.ID {
		return "current_identity"
	}
	source, history, pending := filepath.Join(root, filepath.FromSlash(skillsRevisionTarget)), filepath.Join(root, filepath.FromSlash(skillsRevisionHistory)), filepath.Join(root, filepath.FromSlash(skillsRevisionPending))
	for _, p := range []string{source, history, pending} {
		if blocked(p) || ownerWindowsAlias(p) {
			return "protection"
		}
	}
	oldpath := source
	if !skillsRevisionAbsent(history) {
		oldpath = history
	}
	if _, reason := skillsRevisionOpen(&s, oldpath, e.Original, false); reason != "" {
		return reason
	}
	newpath := pending
	if !skillsRevisionAbsent(history) && !skillsRevisionAbsent(source) {
		if !skillsRevisionAbsent(pending) {
			return "existing_target"
		}
		newpath = source
	}
	if _, reason := skillsRevisionOpen(&s, newpath, e.Pending, false); reason != "" {
		return reason
	}
	return ""
}

func skillsRevisionRecoveryFiles(e skillsRevisionEvidence, original skillsCreatedEvidence, root string, s *skillsFS) string {
	if e.Recovery == nil {
		return ""
	}
	if *e.Recovery != skillsRevisionRecoveryAudit() {
		return "creation_evidence"
	}
	if reason := skillsRevisionDirectories(original, root, s); reason != "" {
		return reason
	}
	for i, f := range original.Files {
		if i != 27 {
			if _, reason := skillsRevisionOpen(s, filepath.Join(root, filepath.FromSlash(f.Target)), f, false); reason != "" {
				return reason
			}
		}
	}
	return skillsRevisionRecoveredState(e, original, root, s.blocked)
}

// Retain both the strict canonical input and the historical raw input in
// receipts. No copy of this proof is accepted as an input request.
func skillsRevisionRecoveryEqual(a, b *skillsRevisionRecoveryProof) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b && *a == skillsRevisionRecoveryAudit()
}
