package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func skillsRuntimeSourcePreparationForJob(s *skillsFS, job object, spec []byte, start, end time.Time) (skillsRuntimeSourcePreparation, string) {
	var empty skillsRuntimeSourcePreparation
	nonce, ok := skillsRuntimeNonceForSpec(spec)
	if !ok {
		return empty, "source_nonce_missing"
	}
	if !skillsFreshRuntimeThread(job, start, end) {
		return empty, "source_generation"
	}
	if job["sandbox"] != "read-only" || skillsModel(str(job["model"])) == "" || str(job["effort"]) != skillsReviewEffort(str(job["model"])) {
		return empty, "source_registration"
	}
	parentID, task := str(job["parent_job_id"]), int(conversationNumber(job["todo_id"]))
	if !skillsResultID(skillsResultRequest{Task: 3080, Job: parentID}) || task <= 0 || task == 3080 || task == 3686 {
		return empty, "source_registration"
	}
	reg, ok := skillsRegistryRead([]int{3080, task})
	row, root := reg.Tasks[task], reg.Tasks[3080]
	if !ok || row.Project != "cc-pc-tools-rs" || row.Subwork.Parent != 3080 || row.Subwork.Kind != "review" || !skillsHasJob(row, str(job["id"])) || !skillsHasJob(root, parentID) || skillsHeld(row) || skillsHeld(root) {
		return empty, "source_registration"
	}
	var author skillsWriter
	if c, valid := skillsParsePublicReview(spec); valid {
		author = c.Author
	} else if c, valid := skillsParseConsumer(spec); valid {
		author = c.Author
	}
	if !skillsWriterValid(author) || !skillsRegisteredGeneration(root, author.Job, parentID) || !same(row.Subwork.Owner.Worktree, author.Workspace) {
		return empty, "source_registration"
	}
	probe := skillsFS{blocked: skillsBlocked}
	parent, ok := skillsReadJob(&probe, parentID)
	probe.close()
	if !ok || !skillsJobTask(parent, 3080) || !same(str(parent["cwd"]), skillsParentRoot) || !skillsUUID.MatchString(str(parent["thread_id"])) {
		return empty, "source_issuer"
	}
	parentStart, err := time.Parse(time.RFC3339Nano, str(parent["started_at"]))
	parentEnd := time.Now()
	if err != nil {
		return empty, "source_issuer"
	}
	if parent["status"] != "running" {
		parentEnd, err = time.Parse(time.RFC3339Nano, str(parent["finished_at"]))
		if err != nil {
			return empty, "source_issuer"
		}
	}
	stream, ok := skillsNativeSnapshot(s, parent)
	if !ok {
		return empty, "source_issuer_native"
	}
	birth, _ := skillsRuntimeThreadBirth(str(job["thread_id"]))
	p, ok := skillsRuntimeSourceNativeProjection(stream, str(parent["thread_id"]), str(parent["cwd"]), parentID, nonce, task, hashBytes(spec), parentStart, parentEnd, birth)
	if !ok {
		return empty, "source_preparation"
	}
	previous := ""
	found := false
	for _, id := range row.Jobs {
		if id == str(job["id"]) {
			found = true
			break
		}
		previous = id
	}
	if !found || previous != p.PreviousJob {
		return empty, "source_generation"
	}
	rootSpec, ok := s.read(p.WriterSpec, 1<<20)
	if !ok || hashBytes(rootSpec) != p.Writer.SpecSHA {
		return empty, "source_issuer_spec"
	}
	if _, ok := skillsReviewLaunchPromptAt(s, skillsJobsRoot, parent, rootSpec); !ok {
		return empty, "source_issuer_spec"
	}
	current, ok := skillsInstalledProducer(s)
	if !ok || !skillsAcceptedProducer(p.Producer, current) {
		return empty, "source_producer"
	}
	rootID, ok := skillsSourceDirectoryID(s, p.Root)
	if !ok || rootID != p.RootID {
		return empty, "source_directory"
	}
	return p, ""
}

func skillsRuntimeSourceForJob(s *skillsFS, job object, spec, prompt []byte, start, end time.Time) (skillsRecordedTransport, string) {
	p, stage := skillsRuntimeSourcePreparationForJob(s, job, spec, start, end)
	if stage != "" {
		return skillsRecordedTransport{}, stage
	}
	trace, stage := skillsReadTransportTraceAt(s, p.TraceRoot, p.TraceID, job, prompt, start, end)
	if stage == "" {
		if s.sourcePreparations == nil {
			s.sourcePreparations = map[string]skillsRuntimeSourcePreparation{}
		}
		s.sourcePreparations[str(job["id"])] = p
	}
	return trace, stage
}

// Starts only an already registered finite public task, through the normal
// shared scheduler. A Root-issued prepared specification is registered first.
func skillsRuntimeSourceStartBound(path string, raw []byte, r skillsRequest, reg skillsRegistry, s *skillsFS, row skillsTask, spec []byte) object {
	fail := func(stage string) object {
		return object{"qualified": false, "reason": "source", "failure_stage": stage, "data_access_granted": false}
	}
	if r.RuntimeSource == nil || !skillsRuntimeNonce(r.RuntimeSource.Nonce) {
		return fail("source_request")
	}
	nonce, ok := skillsRuntimeNonceForSpec(spec)
	if !ok || nonce != r.RuntimeSource.Nonce {
		return fail("source_nonce_missing")
	}
	probe := skillsFS{blocked: skillsBlocked}
	parent, ok := skillsReadJob(&probe, r.Writer.Job)
	probe.close()
	if !ok {
		return fail("source_issuer")
	}
	start, err := time.Parse(time.RFC3339Nano, str(parent["started_at"]))
	if err != nil {
		return fail("source_issuer")
	}
	stream, ok := skillsNativeSnapshot(s, parent)
	if !ok {
		return fail("source_issuer_native")
	}
	preparation, ok := skillsRuntimeSourceNativeProjection(stream, str(parent["thread_id"]), r.Writer.Workspace, r.Writer.Job, nonce, row.ID, hashBytes(spec), start, time.Now(), time.Now())
	if !ok || preparation.PreviousJob != r.RuntimeSource.PreviousJob {
		return fail("source_preparation")
	}
	current, ok := skillsInstalledProducer(s)
	if !ok || preparation.Producer != current {
		return fail("source_producer")
	}
	id, ok := skillsSourceDirectoryID(s, preparation.Root)
	if !ok || id != preparation.RootID {
		return fail("source_directory")
	}
	id, ok = skillsSourceDirectoryID(s, preparation.TraceRoot)
	if !ok || id != preparation.TraceID {
		return fail("source_directory")
	}
	entries, err := os.ReadDir(preparation.TraceRoot)
	if err != nil || len(entries) != 0 {
		return fail("source_not_empty")
	}
	if _, ok := skillsRuntimeSourcePending(s, r.Writer, path, "--start-skills3080-runtime-source"); !ok {
		return fail("native_launch")
	}
	fresh, ok := skillsRegistryRead([]int{3080, row.ID})
	if !ok {
		return fail("source_registration")
	}
	auth := skillsFS{blocked: skillsBlocked}
	validActor := skillsActorCheck(r.Writer, fresh, &auth, os.Getenv("CC_JOB_ID"))
	auth.close()
	currentRow, stateStage := skillsRuntimeSourceTargetState(r, fresh)
	if !validActor || stateStage != "" || currentRow.Spec != row.Spec || currentRow.Subwork != row.Subwork {
		return fail("source_inputs_changed")
	}
	// Releasing a finished task remains the parent's normal todo CLI action.
	// The existing subwork dispatcher retains its admission and stop checks.
	if currentRow.Status == "done" {
		return fail("source_task_finished")
	}
	env := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.EqualFold(key, "CODEX_ROLLOUT_TRACE_ROOT") {
			env = append(env, value)
		}
	}
	env = append(env, "CODEX_ROLLOUT_TRACE_ROOT="+preparation.TraceRoot)
	result, ok := skillsRuntimeSourceHidden("D:/CC/Tools/scheduler/scheduler.exe", []string{"dispatch-subwork", "--id", strconv.Itoa(row.ID)}, r.Writer.Workspace, env, 45*time.Second)
	if !ok {
		return fail("source_normal_dispatch")
	}
	return skillsRuntimeSourceDispatchReceipt(result, row.ID, hashBytes(raw), nonce)
}

func skillsRuntimeSourceDispatchReceipt(raw []byte, task int, requestSHA, nonce string) object {
	out := object{"qualified": false, "reason": "source", "failure_stage": "source_normal_dispatch", "launch_accepted": false, "normal_source_recorded": false, "data_access_granted": false}
	if !skillsNativeUniqueJSON(raw) || !skillsCreationHeaderKeys(raw, "id", "launched") {
		return out
	}
	v, ok := conversationJSON(raw)
	if !ok {
		return out
	}
	job := ""
	if id, present := v["id"]; present && conversationNumber(id) != uint64(task) {
		return out
	}
	if value, text := v["launched"].(string); text {
		if conversationNumber(v["id"]) != uint64(task) {
			return out
		}
		job = value
	} else {
		launched := obj(v["launched"])
		if conversationNumber(launched["item"]) != uint64(task) {
			return out
		}
		job = str(launched["job"])
	}
	if !skillsResultID(skillsResultRequest{Task: task, Job: job}) {
		return out
	}
	return object{"qualified": true, "operation": "start_runtime_source", "request_sha256": requestSHA, "nonce": nonce, "launch_accepted": true, "normal_job": job, "normal_source_recorded": false, "data_access_granted": false, "general_access_granted": false, "original_registry_recovered": false}
}

func skillsRuntimeSourceSpecPath(path string, task int) bool {
	return within(currentTodoSpecsRoot, path) && strings.HasPrefix(filepath.Base(path), strconv.Itoa(task)+"-") && currentSubworkSpecName.MatchString(filepath.Base(path))
}
