package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// One already registered public repair result, whose author received a saved
// specification before the ordinary ToDo specification advanced. These facts
// bind provenance; they grant no general path or historical-job access.
type skillsHistoricalResult struct {
	Job, SpecPath, SpecSHA, PromptSHA, ParentJob, Workspace string
	LaunchKey, Thread, Started, Finished, Model, Effort     string
}

var skillsOriginalRepair = skillsHistoricalResult{
	Job:       "20261025-211205-todo-3686",
	SpecPath:  currentTodoSpecsRoot + "/3686-012ccd41a3f34bf193d32bf1e75622fd.md",
	SpecSHA:   "602f492e3e934d4065da64d4ca803d916d991185cb771597fdff168695ca07f5",
	PromptSHA: "4fcfe139282a56524b1c607d66f33e64dd9367121b777dd4b0a2cbff3dca0c44",
	ParentJob: "20261025-074042-todo-3080", Workspace: "D:/CC/Work/unattended-operations-20261013-wt/todo-3686-skills-3080-a",
	LaunchKey: "1e3e6bd98d31414189bee1687cb2c544", Thread: "01a172fa-1c5d-7030-a92b-4d4aa50b5ee5",
	Started: "2026-10-25T21:12:34+09:00", Finished: "2026-10-25T22:05:32+09:00", Model: "gpt-6.1-sol", Effort: "max",
}

func skillsHistoricalBinding(r skillsResultRequest, reg skillsRegistry, job object, spec []byte, a skillsHistoricalResult) bool {
	row, ok := reg.Tasks[3686]
	parent, parentOK := reg.Tasks[3080]
	w := reg.writer
	latest := row.Scheduler.Job
	if latest == "" {
		latest = row.Scheduler.LastJob
	}
	return ok && parentOK && row.ID == 3686 && parent.ID == 3080 && row.Project == "cc-pc-tools-rs" && parent.Project == row.Project &&
		!skillsHeld(row) && !skillsHeld(parent) && parent.Status == "doing" && skillsWriterValid(w) && parent.Scheduler.Job == w.Job &&
		same(parent.Scheduler.Worktree, w.Workspace) && skillsLatest(parent, w.Job) && skillsRegisteredGeneration(parent, a.ParentJob, w.Job) &&
		skillsLatest(row, latest) && skillsRegisteredGeneration(row, a.Job, latest) && same(row.Scheduler.Worktree, a.Workspace) &&
		r.Task == 3686 && r.Job == a.Job && r.SpecSHA == a.SpecSHA && skillsDigest(r.FinalSHA) && hashBytes(spec) == a.SpecSHA &&
		job["id"] == a.Job && skillsJobTask(job, 3686) && job["status"] == "succeeded" && job["exit_code"] == json.Number("0") &&
		same(str(job["cwd"]), a.Workspace) && job["launch_key"] == a.LaunchKey && job["thread_id"] == a.Thread &&
		job["started_at"] == a.Started && job["finished_at"] == a.Finished && job["model"] == a.Model && job["effort"] == a.Effort &&
		// This root repair really had no runner parent. Its logical original
		// public parent is checked separately; never manufacture parent_job_id.
		job["parent_job_id"] == nil && job["subwork"] == nil && job["finite_review"] == nil
}

func skillsRegisteredGeneration(row skillsTask, author, consumer string) bool {
	a, c := -1, -1
	for i, id := range row.Jobs {
		if id == author {
			if a >= 0 {
				return false
			}
			a = i
		}
		if id == consumer {
			if c >= 0 {
				return false
			}
			c = i
		}
	}
	return a >= 0 && c >= a
}

func skillsReceiveOriginalRepair(r skillsResultRequest, reg skillsRegistry, s *skillsFS) (skillsReceived, string) {
	var out skillsReceived
	a := skillsOriginalRepair
	if r.Job != a.Job {
		if r == skillsLegacyPublicRepair003920 {
			a = skillsRepair003920
		} else {
			return skillsReceiveTypedRepair(r, reg, s)
		}
	}
	if r.SpecPath != "" {
		return out, "result_binding"
	}
	// No caller-supplied historical path, spec snapshot, receipt or author.
	if r.Task != 3686 || r.Job != a.Job || r.SpecSHA != a.SpecSHA || !skillsDigest(r.FinalSHA) {
		return out, "result_binding"
	}
	spec, ok := s.read(a.SpecPath, 1<<20)
	if !ok || hashBytes(spec) != a.SpecSHA {
		return out, "result_binding"
	}
	job, ok := skillsReadJob(s, a.Job)
	if !ok || !skillsHistoricalBinding(r, reg, job, spec, a) {
		return out, "result_binding"
	}
	origin, ok := skillsReadJob(s, a.ParentJob)
	if !ok || !skillsReviewAuthorBound(reg.Tasks[3080], a.ParentJob, job, origin) {
		return out, "result_binding"
	}
	// The actual saved launch prompt contains the byte-identical original
	// spec. Both independent digests predate the new implementation. The
	// immutable launch receipt also binds the actual registered job ID.
	body, native, reason := skillsHistoricalFilesAt(s, skillsJobsRoot, a, spec, job, r.FinalSHA, func(proof *skillsNativeResultProof) ([]object, bool) { return skillsHistoricalNative(s, job, proof) })
	if reason != "" {
		return out, reason
	}
	if r == skillsLegacyPublicRepair003920 && len(body) != 310 {
		return out, "result_unavailable"
	}
	out = skillsReceived{Body: body, Spec: spec, Native: native, Metadata: object{"id": a.Job, "task": 3686, "status": job["status"], "model": a.Model, "effort": a.Effort, "exit_code": job["exit_code"], "thread_id": a.Thread, "spec_sha256": a.SpecSHA, "final_sha256": hashBytes(body), "spec_binding": "original_registered_author", "consumer_job": reg.writer.Job}}
	if r == skillsLegacyPublicRepair003920 {
		out.Metadata["spec_binding"] = "fixed_legacy_public_repair_003920"
	}
	return out, ""
}

// The alternate root is a synthetic-test seam, never a request field.
func skillsHistoricalFilesAt(s *skillsFS, root string, a skillsHistoricalResult, spec []byte, job object, finalSHA string, nativeProof func(*skillsNativeResultProof) ([]object, bool)) ([]byte, []object, string) {
	prompt, ok := s.read(filepath.Join(root, a.Job, "prompt.md"), 4<<20)
	if !ok || hashBytes(prompt) != a.PromptSHA || !bytes.Contains(prompt, append([]byte("\n依頼:\n"), spec...)) {
		return nil, nil, "result_binding"
	}
	receipt, ok := s.read(filepath.Join(root, ".launch-"+a.LaunchKey+".json"), 1<<20)
	var launch struct {
		ID string `json:"id"`
	}
	if !ok || !skillsStrict(receipt, &launch) || launch.ID != a.Job {
		return nil, nil, "result_binding"
	}
	// The fixed historical route follows the same pre-body order as typed
	// reviews. A wrong native input/digest/model/window must not open its result.
	native, ok := nativeProof(&skillsNativeResultProof{prompt: prompt, finalSHA: finalSHA})
	if !ok || len(native) == 0 {
		return nil, nil, "native_context"
	}
	for _, n := range native {
		if skillsModel(str(n["model"])) != skillsModel(a.Model) || n["effort"] != a.Effort {
			return nil, nil, "native_context"
		}
	}
	body, ok := s.read(filepath.Join(root, a.Job, "last_message.md"), 2<<20)
	if !ok || hashBytes(body) != finalSHA || !bytes.Equal(body, []byte(str(job["last_message"]))) {
		return nil, nil, "result_unavailable"
	}
	return body, native, ""
}

// Read only this exact native session at its initial length. Do not infer
// continuity with another job's thread; preserve the original strict window.
func skillsHistoricalNative(s *skillsFS, job object, proof *skillsNativeResultProof) ([]object, bool) {
	end, e := time.Parse(time.RFC3339Nano, str(job["finished_at"]))
	if e != nil {
		return nil, false
	}
	return skillsNativeWindow(s, job, proof, end)
}

// A live consumer has no finished_at. Its immutable input is checked only up
// to an actual observation time, without inventing or changing job metadata.
func skillsNativeWindow(s *skillsFS, job object, proof *skillsNativeResultProof, end time.Time) ([]object, bool) {
	id := str(job["thread_id"])
	start, e := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	if e != nil || end.Before(start) || !skillsUUID.MatchString(id) {
		return nil, false
	}
	var input io.Reader
	var ok bool
	if proof != nil && proof.frozen {
		input, ok = skillsNativeFrozenSnapshot(s, job)
	} else {
		input, ok = skillsNativeSnapshot(s, job)
	}
	if !ok {
		return nil, false
	}
	if proof != nil && (proof.inputOnly || proof.exactInput) {
		// Every typed caller, including live consumers and repair results, gets
		// its environment from the real job here rather than an optional caller
		// initializer. This cannot be supplied in an operation request.
		proof.sandbox = str(job["sandbox"])
		snapshot, rewindable := input.(*skillsNativeSnapshotReader)
		if !rewindable {
			return nil, false
		}
		if proof.requireRuntimeSource || len(proof.runtimeProject) > 0 {
			source, modern, valid := skillsRuntimeBootstrapProjection(input, job, proof.runtimeProject, start, end)
			if !valid || !snapshot.rewind() {
				return nil, false
			}
			if modern {
				proof.runtimeBootstrap = source
			}
		}
		// Session bootstrap is independent of the job's task-input window.
		body, valid := skillsBootstrapBodyProjection(input, id, str(job["cwd"]), proof.sandbox)
		if !valid || !proof.bootstrapMatchesBody(body) || !rewindable || !snapshot.rewind() {
			return nil, false
		}
		proof.runtimeSourceUsed = len(proof.runtimeBootstrap) > 0
		proof.bootstrapBound = true
	}
	return skillsNativeProjectionBound(input, id, str(job["cwd"]), start, end, proof)
}

// One exact append-log snapshot. Bootstrap identity is separate session
// metadata; task input/final projection still uses the original job window.
func skillsNativeSnapshot(s *skillsFS, job object) (io.Reader, bool) {
	return skillsNativeSnapshotWithSharing(s, job, true)
}

// Completed ordinary software reviews retain the old deny-write guarantee.
// Live and resumed historical append-log paths keep their existing sharing
// contract; no public request can choose the mode for a completed code review.
func skillsNativeFrozenSnapshot(s *skillsFS, job object) (io.Reader, bool) {
	if job["status"] != "succeeded" {
		return nil, false
	}
	return skillsNativeSnapshotWithSharing(s, job, false)
}

func skillsNativeSnapshotWithSharing(s *skillsFS, job object, appendLog bool) (io.Reader, bool) {
	id := str(job["thread_id"])
	start, e := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	if e != nil || !skillsUUID.MatchString(id) {
		return nil, false
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, false
	}
	paths := []string{}
	for _, day := range skillsCreationSessionDays(id, start) {
		dir := filepath.Join(home, ".codex/sessions", day)
		if _, ok := s.directory(dir); !ok {
			continue
		}
		entries, e := os.ReadDir(dir)
		if e != nil {
			return nil, false
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "rollout-") && strings.HasSuffix(entry.Name(), "-"+id+".jsonl") {
				paths = append(paths, filepath.Join(dir, entry.Name()))
			}
		}
	}
	if len(paths) != 1 || s.blocked(paths[0]) {
		return nil, false
	}
	p := paths[0]
	parent, ok := s.directory(filepath.Dir(p))
	if !ok {
		return nil, false
	}
	h, ok := skillsOpenAtSharing(parent, filepath.Base(p), false, false, appendLog)
	if !ok {
		return nil, false
	}
	f := os.NewFile(uintptr(h), p)
	s.files = append(s.files, f)
	info, e := f.Stat()
	actual, e2 := ownerHandlePath(h)
	if e != nil || e2 != nil || !same(actual, p) || !info.Mode().IsRegular() || info.Size() < 1 {
		return nil, false
	}
	// A long, otherwise valid session does not change its identity or scope.
	// Freeze the observed length through this same handle. Bootstrap extraction
	// separately stops at 4 MiB; full projections retain per-record bounds and the
	// original task window, exact input/final/call checks and duplicate detection.
	return &skillsNativeSnapshotReader{reader: f, remaining: info.Size(), length: info.Size()}, true
}

// Unlike LimitReader, an early underlying EOF must not authenticate a shortened
// prefix that hides a duplicate proof from the frozen snapshot. Bootstrap may
// deliberately stop at its separate 4MiB prefix bound; full projections drain
// this reader and retain ErrUnexpectedEOF through their scanner error check.
type skillsNativeSnapshotReader struct {
	reader    io.Reader
	remaining int64
	length    int64
}

func (s *skillsNativeSnapshotReader) rewind() bool {
	r, ok := s.reader.(io.Seeker)
	if !ok {
		return false
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return false
	}
	s.remaining = s.length
	return true
}

func (s *skillsNativeSnapshotReader) Read(p []byte) (int, error) {
	if s.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > s.remaining {
		p = p[:s.remaining]
	}
	n, err := s.reader.Read(p)
	s.remaining -= int64(n)
	if err == io.EOF && s.remaining > 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

type skillsNativeResultProof struct {
	prompt, body, lastFinal       []byte
	promptAt, finalAt             time.Time
	inputEventAt                  time.Time
	finalSHA                      string
	inputOnly                     bool
	exactInput                    bool
	bootstrapSHA, cwd             string
	sandbox                       string
	runtimeBootstrap              []byte
	runtimeProject                []byte
	requireRuntimeSource          bool
	runtimeSourceUsed             bool
	userSeen                      bool
	runtimePrefixClosed           bool
	bootstrapSeen, bootstrapBound bool
	frozen                        bool
}

func (p *skillsNativeResultProof) observeEvent(raw []byte, at time.Time) bool {
	if !p.exactInput && !p.inputOnly {
		return true
	}
	var e struct {
		Payload struct {
			Type     string            `json:"type"`
			Message  string            `json:"message"`
			Images   []json.RawMessage `json:"images"`
			Local    []json.RawMessage `json:"local_images"`
			Elements []json.RawMessage `json:"text_elements"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return false
	}
	v := e.Payload
	if v.Type != "user_message" {
		return true
	}
	if !p.inputEventAt.IsZero() || v.Message != string(p.prompt) || len(v.Images) != 0 || len(v.Local) != 0 || len(v.Elements) != 0 {
		return false
	}
	p.inputEventAt = at
	return true
}

func (p *skillsNativeResultProof) bootstrapMatchesBody(body string) bool {
	if len(p.runtimeBootstrap) > 0 {
		return skillsBootstrapSourceMatches(body, p.runtimeBootstrap)
	}
	return skillsDigest(p.bootstrapSHA) && hashBytes([]byte(body)) == p.bootstrapSHA
}

func (p *skillsNativeResultProof) observe(raw []byte, at time.Time) bool {
	if _, ok := conversationJSON(raw); !ok {
		return false
	}
	var envelope struct {
		Payload struct {
			Type, Role, Phase string
			Content           json.RawMessage
		}
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	v := envelope.Payload
	if (p.exactInput || p.inputOnly) && v.Type != "message" && v.Role != "" {
		return v.Type == "additional_tools" && v.Role == "developer" && !p.runtimePrefixClosed && !p.userSeen
	}
	if (p.exactInput || p.inputOnly) && v.Type == "message" && v.Role != "user" && v.Role != "assistant" {
		return v.Role == "developer" && !p.runtimePrefixClosed && !p.userSeen
	}
	if v.Role == "user" && v.Type != "message" {
		return false
	}
	if p.inputOnly && v.Role == "assistant" {
		return true
	}
	// codex-cli 0.163.0's generated MessagePhase schema and the original
	// job's native metadata both use final_answer. Unknown/partial/commentary
	// phases cannot prove a terminal response. No timestamp tolerance is added.
	if v.Type != "message" || (v.Role != "user" && (v.Role != "assistant" || v.Phase != "final_answer")) {
		return true
	}
	// A terminal payload is relevant only after the exact prelaunch input has
	// classified this turn. Do not materialize an unbound assistant's text.
	if v.Role == "assistant" && (p.promptAt.IsZero() || at.Before(p.promptAt)) {
		return true
	}
	var blocks []json.RawMessage
	if json.Unmarshal(v.Content, &blocks) != nil {
		return false
	}
	var text strings.Builder
	for _, raw := range blocks {
		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if !skillsCreationHeaderKeys(raw, "type", "text") || json.Unmarshal(raw, &block) != nil {
			return false
		}
		if v.Role == "user" && (p.exactInput || p.inputOnly) && block.Type != "input_text" {
			return false
		}
		if block.Type == "input_text" || block.Type == "output_text" {
			text.WriteString(block.Text)
		}
	}
	if v.Role == "user" && (p.exactInput || p.inputOnly) {
		p.runtimePrefixClosed = true
		first := !p.userSeen
		p.userSeen = true
		if first {
			if body, ok := skillsBootstrapBodyWithSandbox(text.String(), p.cwd, p.sandbox); ok && p.bootstrapMatchesBody(body) {
				p.bootstrapSeen = true
				return true
			}
		}
	}
	matched := text.String() == string(p.prompt)
	if !p.exactInput {
		matched = strings.Contains(text.String(), string(p.prompt))
	}
	if v.Role == "user" && matched {
		if !p.promptAt.IsZero() {
			return false
		}
		p.promptAt = at
	} else if v.Role == "user" && ((p.finalSHA != "" && !p.promptAt.IsZero()) || p.inputOnly || p.exactInput) {
		// The typed contract fixes the review's complete user input. An unknown
		// later user message cannot silently expand that public classification.
		return false
	}
	if v.Role == "assistant" {
		if !p.finalAt.IsZero() {
			return false
		}
		p.lastFinal = []byte(text.String())
		p.finalAt = at
	}
	return true
}

func (p *skillsNativeResultProof) bound() bool {
	// Some native versions mirror a user message as an event. It must be the
	// same exact input at the same recorded instant, never a second turn.
	if !p.inputEventAt.IsZero() && !p.inputEventAt.Equal(p.promptAt) {
		return false
	}
	if (p.inputOnly || p.exactInput) && !p.bootstrapBound && !p.bootstrapSeen {
		return false
	}
	if p.inputOnly {
		return len(p.prompt) > 0 && !p.promptAt.IsZero()
	}
	if len(p.prompt) == 0 || p.promptAt.IsZero() || p.finalAt.IsZero() || p.finalAt.Before(p.promptAt) {
		return false
	}
	if len(p.body) > 0 {
		return bytes.Equal(p.lastFinal, p.body)
	}
	// Public review preflight proves the immutable prompt and the expected
	// native digest before opening last_message.md. Its same-buffer digest and
	// job metadata are then checked independently by skillsReceive.
	return skillsDigest(p.finalSHA) && hashBytes(p.lastFinal) == p.finalSHA
}
