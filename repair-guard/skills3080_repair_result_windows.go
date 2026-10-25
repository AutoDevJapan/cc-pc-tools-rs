package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

// This already completed prose specification is a fixed migration. New
// continuations use the complete typed prelaunch contract below, never a table
// entry for each new job or a classification based only on task 3686.
var skillsLegacyPublicRepair003920 = skillsResultRequest{
	Task: 3686, Job: "20261026-003920-todo-3686",
	SpecSHA:  "43238d03ec3afc3d8173a5354ee55fa6d908dd555c081ac737074fd77f4cca85",
	FinalSHA: "2a8a0768492a6a45281a378843c009c59ed9f80cce489fc441d9889bc76c612e",
}
var skillsRepair003920 = skillsHistoricalResult{
	Job: "20261026-003920-todo-3686", SpecPath: currentTodoSpecsRoot + "/3686-494f53a724cd4cb2b45f912c33c6e273.md",
	SpecSHA: "43238d03ec3afc3d8173a5354ee55fa6d908dd555c081ac737074fd77f4cca85", PromptSHA: "081ed6b932b13f4aefc0f98253230488ac6bd95ef4a7b74a959f8ef4018d2a23",
	ParentJob: "20261025-074042-todo-3080", Workspace: "D:/CC/Work/unattended-operations-20261013-wt/todo-3686-skills-3080-a",
	LaunchKey: "514873a012034310bdaa1b57f3c271e4", Thread: "01a173b7-dd89-7501-96d3-b8d5f5b39117",
	Started: "2026-10-26T00:39:50+09:00", Finished: "2026-10-26T02:05:19+09:00", Model: "gpt-6.1-sol", Effort: "max",
}

func skillsRepairSnapshotPath(r skillsResultRequest) bool {
	return r.Task == 3686 && skillsDigest(r.SpecSHA) && skillsDigest(r.FinalSHA) && within(currentTodoSpecsRoot, r.SpecPath) &&
		strings.HasPrefix(filepath.Base(r.SpecPath), "3686-") && currentSubworkSpecName.MatchString(filepath.Base(r.SpecPath))
}

func skillsTypedRepairBinding(r skillsResultRequest, reg skillsRegistry, job object, raw []byte, c skillsPublicReviewScope) bool {
	row, parent := reg.Tasks[3686], reg.Tasks[3080]
	latest := row.Scheduler.Job
	if latest == "" {
		latest = row.Scheduler.LastJob
	}
	start, e := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	end, e2 := time.Parse(time.RFC3339Nano, str(job["finished_at"]))
	return skillsRepairSnapshotPath(r) && skillsPublicSourceShape(c, true) && hashBytes(raw) == r.SpecSHA &&
		row.ID == 3686 && parent.ID == 3080 && row.Project == "cc-pc-tools-rs" && parent.Project == row.Project && !skillsHeld(row) && !skillsHeld(parent) &&
		skillsWriterValid(reg.writer) && parent.Status == "doing" && skillsLatest(parent, reg.writer.Job) && parent.Scheduler.Job == reg.writer.Job && same(parent.Scheduler.Worktree, reg.writer.Workspace) &&
		skillsLatest(row, latest) && skillsRegisteredGeneration(row, r.Job, latest) && strings.HasPrefix(row.Scheduler.Branch, "scheduler/todo-3686-") &&
		skillsRegisteredGeneration(parent, c.Author.Job, reg.writer.Job) && same(parent.Scheduler.Worktree, c.Author.Workspace) &&
		job["id"] == r.Job && skillsJobTask(job, 3686) && job["status"] == "succeeded" && job["exit_code"] == json.Number("0") &&
		same(str(job["cwd"]), row.Scheduler.Worktree) && skillsModelIdentifier(str(job["model"])) && job["effort"] == "max" &&
		job["parent_job_id"] == nil && job["subwork"] == nil && job["finite_review"] == nil && e == nil && e2 == nil && !end.Before(start)
}

func skillsReceiveTypedRepair(r skillsResultRequest, reg skillsRegistry, s *skillsFS) (skillsReceived, string) {
	var out skillsReceived
	if !skillsRepairSnapshotPath(r) {
		return out, "result_binding"
	}
	spec, ok := s.read(r.SpecPath, 1<<20)
	if !ok || hashBytes(spec) != r.SpecSHA {
		return out, "result_binding"
	}
	c, ok := skillsParsePublicRepair(spec)
	if !ok {
		return out, "result_binding"
	}
	job, ok := skillsReadJob(s, r.Job)
	if !ok || !skillsTypedRepairBinding(r, reg, job, spec, c) {
		return out, "result_binding"
	}
	origin, ok := skillsReadJob(s, c.Author.Job)
	if !ok || !skillsReviewAuthorBound(reg.Tasks[3080], c.Author.Job, job, origin) {
		return out, "result_binding"
	}
	authorSpec, ok := s.read(c.AuthorSpecPath, 1<<20)
	if !ok || hashBytes(authorSpec) != c.Author.SpecSHA {
		return out, "result_binding"
	}
	if _, ok = skillsReviewLaunchPromptAt(s, skillsJobsRoot, origin, authorSpec); !ok {
		return out, "result_binding"
	}
	if !skillsPublicRepairInputs(c, reg.writer.Workspace, s) {
		return out, "result_binding"
	}
	prompt, ok := skillsReviewLaunchPromptAt(s, skillsJobsRoot, job, spec)
	if !ok {
		return out, "result_binding"
	}
	a := skillsHistoricalResult{Job: r.Job, SpecPath: r.SpecPath, SpecSHA: r.SpecSHA, PromptSHA: hashBytes(prompt), ParentJob: c.Author.Job,
		Workspace: str(job["cwd"]), LaunchKey: str(job["launch_key"]), Thread: str(job["thread_id"]), Started: str(job["started_at"]), Finished: str(job["finished_at"]), Model: str(job["model"]), Effort: str(job["effort"])}
	body, native, reason := skillsHistoricalFilesAt(s, skillsJobsRoot, a, spec, job, r.FinalSHA, func(p *skillsNativeResultProof) ([]object, bool) { return skillsHistoricalNative(s, job, p) })
	if reason != "" {
		return out, reason
	}
	return skillsReceived{Body: body, Spec: spec, Native: native, Metadata: object{"id": r.Job, "task": 3686, "status": job["status"], "model": a.Model, "effort": a.Effort, "exit_code": job["exit_code"], "thread_id": a.Thread, "spec_sha256": r.SpecSHA, "final_sha256": hashBytes(body), "spec_binding": "immutable_typed_public_repair", "consumer_job": reg.writer.Job, "public_input_commit": c.Commit}}, ""
}
