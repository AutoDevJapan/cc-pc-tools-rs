package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func skillsRuntimeSourceSpec(raw []byte, nonce string) ([]byte, bool) {
	if !skillsRuntimeNonce(nonce) {
		return nil, false
	}
	if c, ok := skillsParsePublicReview(raw); ok {
		c.RuntimeSourceNonce = nonce
		b, err := json.MarshalIndent(c, "", "  ")
		return b, err == nil
	}
	if c, ok := skillsParseConsumer(raw); ok {
		c.RuntimeSourceNonce = nonce
		b, err := json.MarshalIndent(c, "", "  ")
		return b, err == nil
	}
	return nil, false
}

func skillsRuntimeNonceForSpec(raw []byte) (string, bool) {
	if c, ok := skillsParsePublicReview(raw); ok {
		return c.RuntimeSourceNonce, skillsRuntimeNonce(c.RuntimeSourceNonce)
	}
	if c, ok := skillsParseConsumer(raw); ok {
		return c.RuntimeSourceNonce, skillsRuntimeNonce(c.RuntimeSourceNonce)
	}
	return "", false
}

func skillsRuntimeSourceTarget(r skillsRequest, reg skillsRegistry, s *skillsFS) (skillsTask, []byte, string) {
	row, stage := skillsRuntimeSourceTargetState(r, reg)
	if stage != "" {
		return skillsTask{}, nil, stage
	}
	x := r.RuntimeSource
	if x.PreviousJob != "" {
		probe := skillsFS{blocked: skillsBlocked}
		job, valid := skillsReadJob(&probe, x.PreviousJob)
		live := valid && skillsActorLive(job)
		probe.close()
		if !valid || live || !skillsJobTask(job, row.ID) {
			return skillsTask{}, nil, "source_active"
		}
		switch str(job["status"]) {
		case "succeeded", "failed", "cancelled", "canceled", "lost":
		default:
			return skillsTask{}, nil, "source_active"
		}
	}
	raw, ok := s.read(row.Spec, 1<<20)
	if !ok || hashBytes(raw) != x.SpecSHA {
		return skillsTask{}, nil, "source_spec"
	}
	_, review := skillsParsePublicReview(raw)
	_, consumer := skillsParseConsumer(raw)
	if !review && !consumer {
		return skillsTask{}, nil, "source_spec"
	}
	return row, raw, ""
}

func skillsRuntimeSourceTargetState(r skillsRequest, reg skillsRegistry) (skillsTask, string) {
	var empty skillsTask
	x := r.RuntimeSource
	if x == nil || r.Writer.Task != 3080 || x.Task <= 0 || x.Task == 3080 || x.Task == 3686 || !skillsDigest(x.SpecSHA) {
		return empty, "source_request"
	}
	row, ok := reg.Tasks[x.Task]
	parent := reg.Tasks[3080]
	if !ok || skillsHeld(row) || skillsHeld(parent) || parent.Status != "doing" || row.Project != "cc-pc-tools-rs" || row.Subwork.Parent != 3080 || row.Subwork.Kind != "review" || !same(row.Subwork.Owner.Worktree, r.Writer.Workspace) || !skillsRegisteredGeneration(parent, row.Subwork.ParentJob, r.Writer.Job) {
		return empty, "source_registration"
	}
	latest := row.Scheduler.Job
	if latest == "" {
		latest = row.Scheduler.LastJob
	}
	if latest == "" && len(row.Jobs) != 0 {
		latest = row.Jobs[len(row.Jobs)-1]
	}
	if latest != x.PreviousJob || (row.Status != "done" && row.Status != "todo" && row.Status != "doing") || !within(currentTodoSpecsRoot, row.Spec) || !strings.HasPrefix(filepath.Base(row.Spec), strconv.Itoa(row.ID)+"-") || !currentSubworkSpecName.MatchString(filepath.Base(row.Spec)) {
		return empty, "source_generation"
	}
	return row, ""
}

func skillsRuntimeSourceWriteNew(s *skillsFS, path string, body []byte) bool {
	if s.blocked(path) || len(body) == 0 {
		return false
	}
	ph, ok := s.directory(filepath.Dir(path))
	if !ok {
		return false
	}
	h, ok := skillsOpenAt(ph, filepath.Base(path), false, true)
	if !ok {
		return false
	}
	f := os.NewFile(uintptr(h), path)
	s.files = append(s.files, f)
	n, err := f.Write(body)
	if err != nil || n != len(body) || f.Sync() != nil {
		return false
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(len(body))+1))
	return err == nil && bytes.Equal(body, b)
}

func skillsRuntimeSourceHidden(command string, args []string, cwd string, env []string, seconds time.Duration) ([]byte, bool) {
	r := skillsRuntimeSourceRunHidden(command, args, cwd, env, seconds)
	return r.stdout, r.ok
}

func skillsRuntimeSourceOwnedRoot(w skillsWriter) (string, string, bool) {
	b, ok := skillsRuntimeSourceHidden("D:/CC/Tools/safe-delete/safe-delete.exe", []string{"create", "--project", "cc-pc-tools-rs", "--content", "skills3080-runtime-source", "--root", w.Workspace}, w.Workspace, os.Environ(), 30*time.Second)
	v, parsed := conversationJSON(b)
	id, path := str(v["id"]), str(v["path"])
	valid := parsed && skillsRuntimeNonce(id) && v["state"] == "owned" && v["producer_job"] == w.Job && conversationNumber(v["producer_todo"]) == 3080 && same(path, filepath.Join(w.Workspace, "codex-owned-"+id))
	if !valid {
		return "", "", false
	}
	return path, id, ok
}

// Claim the request object before starting any external allocation. An unknown
// outcome (timeout, crash, malformed or missing creator output) leaves this
// CreateNew record in place, so retries cannot allocate a second root.
func skillsRuntimeSourceAttempt(s *skillsFS, w skillsWriter, requestID skillsObjectID, task int, requestSHA string) bool {
	if !skillsSourceObjectShape(requestID) || !skillsResultID(skillsResultRequest{Task: w.Task, Job: w.Job}) || !skillsDigest(requestSHA) || task <= 0 {
		return false
	}
	key := skillsProjectionSHA(object{"writer_job": w.Job, "request_object": requestID})
	path := filepath.Join(w.Workspace, ".ci-work", "skills3080-source-attempt-"+key+".json")
	body, err := json.Marshal(object{"domain": "skills-3080-source-attempt-v1", "writer_job": w.Job, "task": task, "request_object": requestID, "request_sha256": requestSHA})
	return err == nil && skillsRuntimeSourceWriteNew(s, path, body)
}

func skillsRuntimeSourceAllocateOnce(s *skillsFS, w skillsWriter, requestID skillsObjectID, task int, requestSHA string, create func() (string, string, bool)) (string, string, bool, bool) {
	if !skillsRuntimeSourceAttempt(s, w, requestID, task, requestSHA) {
		return "", "", false, false
	}
	root, id, ok := create()
	return root, id, ok, true
}

func skillsRuntimeSourcePending(s *skillsFS, w skillsWriter, path, flag string) (string, bool) {
	f, opened := s.open(path, 1<<20)
	if !opened {
		return "", false
	}
	requestID, identified := skillsHandleID(syscall.Handle(f.Fd()))
	if !identified {
		return "", false
	}
	probe := skillsFS{blocked: skillsBlocked}
	job, ok := skillsReadJob(&probe, w.Job)
	probe.close()
	if !ok {
		return "", false
	}
	start, err := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	if err != nil {
		return "", false
	}
	stream, ok := skillsNativeSnapshot(s, job)
	if !ok {
		return "", false
	}
	return skillsRuntimeSourcePendingCall(stream, str(job["thread_id"]), w.Workspace, path, flag, start, time.Now(), requestID)
}

func skillsRuntimeSourceOperation(op, path string, raw []byte, r skillsRequest, reg skillsRegistry, s *skillsFS) object {
	ownedRoot, ownedID := "", ""
	requestIdentity := skillsObjectID{}
	attemptRecorded := false
	fail := func(stage string) object {
		return skillsRuntimeSourceFailure(stage, ownedRoot, ownedID, attemptRecorded, requestIdentity)
	}
	if r.RuntimeSource == nil || r.Writer.Task != 3080 || r.ManifestCommit != "" || r.ManifestPath != "" || r.ManifestSHA != "" || r.Review != nil || r.Result != nil || r.Runtime != nil || r.Consume != nil || r.Revision != nil {
		return fail("source_request")
	}
	if !skillsRuntimeSourceInvocationPaths(path, r.Writer.Workspace, "--prepare-skills3080-runtime-source") {
		return fail("source_request")
	}
	row, template, stage := skillsRuntimeSourceTarget(r, reg, s)
	if stage != "" {
		return fail(stage)
	}
	if op == "render-skills3080-runtime-source" {
		return object{"qualified": true, "operation": "render_runtime_source_invocation", "data_access_granted": false, "general_access_granted": false, "original_registry_recovered": false,
			"prepare_invocation": skillsRuntimeSourceInvocationText(path, r.Writer.Workspace, "--prepare-skills3080-runtime-source"), "start_invocation": skillsRuntimeSourceInvocationText(path, r.Writer.Workspace, "--start-skills3080-runtime-source")}
	}
	if op == "start-skills3080-runtime-source" {
		return skillsRuntimeSourceStart(path, raw, r, reg, s, row, template)
	}
	if op != "prepare-skills3080-runtime-source" || r.RuntimeSource.Nonce != "" {
		return fail("source_request")
	}
	call, ok := skillsRuntimeSourcePending(s, r.Writer, path, "--prepare-skills3080-runtime-source")
	if !ok {
		return fail("native_launch")
	}
	requestFile, ok := s.open(path, 1<<20)
	if !ok {
		return fail("source_request")
	}
	requestID, ok := skillsHandleID(syscall.Handle(requestFile.Fd()))
	if !ok {
		return fail("source_request")
	}
	requestIdentity = requestID
	producer, ok := skillsProducerObservation(s)
	if !ok {
		return fail("runtime")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fail("source_nonce")
	}
	nonce := hex.EncodeToString(random[:])
	prepared, ok := skillsRuntimeSourceSpec(template, nonce)
	if !ok {
		return fail("source_spec")
	}
	root, ownership, ok, attempted := skillsRuntimeSourceAllocateOnce(s, r.Writer, requestID, row.ID, hashBytes(raw), func() (string, string, bool) { return skillsRuntimeSourceOwnedRoot(r.Writer) })
	attemptRecorded = attempted
	if !attempted {
		return fail("source_attempt_record")
	}
	ownedRoot, ownedID = root, ownership
	if !ok {
		return fail("source_owned_root")
	}
	rootID, ok := skillsSourceDirectoryID(s, root)
	if !ok {
		return fail("source_owned_root")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return fail("source_owned_root")
	}
	h, ok := skillsOpenAt(s.dirs[strings.ToLower(filepath.Clean(root))], "trace", true, true)
	if !ok {
		return fail("source_directory")
	}
	trace := filepath.Join(root, "trace")
	s.dirs[strings.ToLower(filepath.Clean(trace))] = h
	traceID, ok := skillsHandleID(h)
	if !ok {
		return fail("source_directory")
	}
	p := skillsRuntimeSourcePreparation{Domain: skillsRuntimeSourceDomain, Writer: r.Writer, WriterSpec: reg.Tasks[3080].Spec, Task: row.ID, PreviousJob: r.RuntimeSource.PreviousJob, TemplateSHA: hashBytes(template), SpecSHA: hashBytes(prepared), PreparedSpec: filepath.Join(root, "scope.json"), Nonce: nonce, OwnershipID: ownership, Root: root, RootID: rootID, TraceRoot: trace, TraceID: traceID, RequestID: requestID,
		PreparedAt: time.Now().UTC().Format(time.RFC3339Nano), RequestPath: path, RequestSHA: hashBytes(raw), CallID: call, Producer: producer, SourceMeaning: "first_recorded_model_visible_request_object"}
	if !skillsRuntimeSourcePreparationShape(p) || !skillsRuntimeSourceWriteNew(s, p.PreparedSpec, prepared) {
		return fail("source_prepare_write")
	}
	return object{"qualified": true, "operation": "prepare_runtime_source", "request_sha256": p.RequestSHA, "runtime_source_preparation": p, "normal_source_recorded": false, "data_access_granted": false, "general_access_granted": false, "original_registry_recovered": false}
}

func skillsRuntimeSourceFailure(stage, root, id string, attempted bool, requestID skillsObjectID) object {
	v := object{"qualified": false, "reason": "source", "failure_stage": stage, "data_access_granted": false}
	if root != "" && skillsRuntimeNonce(id) {
		v["ownership_id"], v["partial_preparation_possible"] = id, true
	}
	if attempted && skillsSourceObjectShape(requestID) {
		v["source_attempt_recorded"], v["request_object"] = true, requestID
	}
	return v
}

func skillsRuntimeSourceStart(path string, raw []byte, r skillsRequest, reg skillsRegistry, s *skillsFS, row skillsTask, spec []byte) object {
	return skillsRuntimeSourceStartBound(path, raw, r, reg, s, row, spec)
}
