package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

// These records only refuse another start. They never qualify source bytes or
// replace the controller's original preparation, admission, or transport proof.
// The key spans nonce/request aliases and resumed issuer jobs for this same
// registered task generation; a new nonce cannot clear an uncertain attempt.
func skillsSourceStartClaim(s *skillsFS, workspace string, task int, previous, requestSHA, nonce string, generation uint64) (string, bool) {
	key := skillsProjectionSHA(object{"task": task, "previous_job": previous, "ready_generation": generation})
	base := filepath.Join(workspace, ".ci-work", "skills3080-source-start-"+key)
	for n := 1; n <= 32; n++ {
		claim := base + "-" + strconv.Itoa(n) + ".json"
		body, _ := json.Marshal(object{"domain": "skills-3080-source-start-attempt-v1", "task": task, "previous_job": previous, "ready_generation": generation, "request_sha256": requestSHA, "nonce": nonce})
		if skillsRuntimeSourceWriteNew(s, claim, body) {
			return claim, true
		}
		// A completed, explicit normal non-launch permits a later independent
		// canonical call. Missing, malformed, or unknown completion never does.
		raw, ok := s.read(claim, 1<<20)
		var old struct {
			Domain     string `json:"domain"`
			Task       int    `json:"task"`
			Previous   string `json:"previous_job"`
			Request    string `json:"request_sha256"`
			Nonce      string `json:"nonce"`
			Generation uint64 `json:"ready_generation"`
		}
		if !ok || !skillsStrict(raw, &old) || old.Domain != "skills-3080-source-start-attempt-v1" || old.Task != task || old.Previous != previous || old.Generation != generation || !skillsDigest(old.Request) || !skillsRuntimeNonce(old.Nonce) {
			return "", false
		}
		done, ok := s.read(claim+".not-launched", 1<<20)
		var completed struct {
			Domain  string `json:"domain"`
			Attempt string `json:"attempt_sha256"`
		}
		if !ok || !skillsStrict(done, &completed) || completed.Domain != "skills-3080-source-start-nonlaunch-v1" || completed.Attempt != hashBytes(raw) {
			return "", false
		}
	}
	return "", false
}

func skillsSourceStartClaimRaw(s *skillsFS, path string) ([]byte, bool) {
	for _, f := range s.files {
		if f.Name() == path {
			info, err := f.Stat()
			if err != nil || info.Size() < 1 || info.Size() > 1<<20 {
				return nil, false
			}
			b, err := io.ReadAll(io.NewSectionReader(f, 0, info.Size()))
			return b, err == nil && int64(len(b)) == info.Size()
		}
	}
	return s.read(path, 1<<20)
}

func skillsSourceStartNonlaunch(s *skillsFS, claim string, result skillsSourceCommandResult, task int) bool {
	if !result.ok {
		return false
	}
	v, _, outcome := skillsSourceDispatchLaunch(result.stdout, task)
	if outcome != "not_launched" || v["active"] != false {
		return false
	}
	raw, ok := skillsSourceStartClaimRaw(s, claim)
	if !ok {
		return false
	}
	body, _ := json.Marshal(object{"domain": "skills-3080-source-start-nonlaunch-v1", "attempt_sha256": hashBytes(raw)})
	return skillsRuntimeSourceWriteNew(s, claim+".not-launched", body)
}

func skillsSourceStartGuardedDispatch(s *skillsFS, r skillsRequest, row skillsTask, requestSHA, nonce string, run func() skillsSourceCommandResult, bound func(string) bool) object {
	fail := func(stage string) object {
		return object{"qualified": false, "reason": "source", "failure_stage": stage, "data_access_granted": false}
	}
	if !row.sourceReadyGenerationKnown {
		return fail("source_start_pending")
	}
	claim, ok := skillsSourceStartClaim(s, r.Writer.Workspace, row.ID, r.RuntimeSource.PreviousJob, requestSHA, nonce, row.sourceReadyGeneration)
	if !ok {
		return fail("source_start_pending")
	}
	result := run()
	out := skillsRuntimeSourceDispatch(result, row.ID, requestSHA, nonce, row.Needs...)
	if out["qualified"] == true {
		if !bound(str(out["normal_job"])) {
			return fail("source_launch_binding")
		}
	} else {
		skillsSourceStartNonlaunch(s, claim, result, row.ID)
	}
	return out
}

// A later ordinary retry generation revokes the previous reservation. An old
// unregistered child cannot satisfy admitted_launch with a null scheduler key
// and no worker job. A registered/live child is still caught by the ordinary
// dispatcher's target-active gate. Request/nonce changes do not revoke a key.
func skillsSourceReadyGeneration(metadata object) (uint64, bool) {
	if metadata["status"] != "todo" {
		return 0, false
	}
	scheduler, receipt := obj(metadata["scheduler"]), obj(metadata["worker_receipt"])
	key, keyPresent := scheduler["key"]
	job, jobPresent := scheduler["job"]
	worker, workerPresent := receipt["job"]
	if !keyPresent || key != nil || !jobPresent || (job != nil && job != "") || !workerPresent || (worker != nil && worker != "") {
		return 0, false
	}
	value, valid := metadata["resume_generation"].(json.Number)
	generation, err := strconv.ParseUint(string(value), 10, 64)
	return generation, valid && err == nil && generation > 0 && strconv.FormatUint(generation, 10) == string(value) && receipt["generation"] == value
}

// Admission and spec digest come from the normal scheduler/job registration
// transaction, rather than the returned task/job name alone. The original spec
// and its physical parents stay deny-write/delete pinned across the command.
func skillsSourceLaunchedTuple(r skillsRequest, original, current skillsTask, job object, specSHA string) bool {
	finite := obj(job["finite_review"])
	return current.ID == original.ID && current.Spec == original.Spec && current.Subwork == original.Subwork &&
		slices.Equal(current.Needs, original.Needs) && skillsSourceAssignedNeeds(job["assigned_needs"], original.Needs) &&
		skillsHasJob(current, str(job["id"])) && skillsLatest(current, str(job["id"])) && skillsJobTask(job, original.ID) &&
		job["parent_job_id"] == r.Writer.Job && job["subwork_owner_job_id"] == r.Writer.Job &&
		job["sandbox"] == "read-only" && same(str(job["cwd"]), current.Scheduler.Worktree) &&
		skillsModel(str(job["model"])) != "" && job["effort"] == skillsReviewEffort(str(job["model"])) &&
		finite["spec_sha256"] == specSHA && same(str(finite["spec"]), original.Spec) &&
		job["subwork_spec"] == original.Spec && skillsReviewAdmissionBound(current, job)
}

func skillsSourceAssignedNeeds(value any, expected []string) bool {
	values, ok := value.([]any)
	if !ok || len(values) != len(expected) {
		return false
	}
	for i, value := range values {
		if value != expected[i] {
			return false
		}
	}
	return true
}

func skillsSourceLaunchedBound(s *skillsFS, r skillsRequest, original skillsTask, spec []byte, id string) bool {
	reg, ok := skillsRegistryRead([]int{3080, original.ID})
	if !ok {
		return false
	}
	probe := skillsFS{blocked: skillsBlocked}
	validActor := skillsActorCheck(r.Writer, reg, &probe, os.Getenv("CC_JOB_ID"))
	job, read := skillsReadJob(&probe, id)
	probe.close() // Mutable metadata never spans prompt/receipt reads.
	if !validActor || !read || !skillsSourceLaunchedTuple(r, original, reg.Tasks[original.ID], job, hashBytes(spec)) {
		return false
	}
	_, ok = skillsReviewLaunchPromptAt(s, skillsJobsRoot, job, spec)
	return ok
}
