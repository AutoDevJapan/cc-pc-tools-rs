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
	prompt, body, reason := skillsHistoricalFilesAt(s, skillsJobsRoot, a, spec, job, r.FinalSHA)
	if reason != "" {
		return out, reason
	}
	native, ok := skillsHistoricalNative(s, job, &skillsNativeResultProof{prompt: prompt, body: body})
	if !ok {
		return out, "native_context"
	}
	for _, n := range native {
		if skillsModel(str(n["model"])) != skillsModel(a.Model) || n["effort"] != a.Effort {
			return out, "native_context"
		}
	}
	out = skillsReceived{Body: body, Spec: spec, Native: native, Metadata: object{"id": a.Job, "task": 3686, "status": job["status"], "model": a.Model, "effort": a.Effort, "exit_code": job["exit_code"], "thread_id": a.Thread, "spec_sha256": a.SpecSHA, "final_sha256": hashBytes(body), "spec_binding": "original_registered_author", "consumer_job": reg.writer.Job}}
	return out, ""
}

// The alternate root is a synthetic-test seam, never a request field.
func skillsHistoricalFilesAt(s *skillsFS, root string, a skillsHistoricalResult, spec []byte, job object, finalSHA string) ([]byte, []byte, string) {
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
	body, ok := s.read(filepath.Join(root, a.Job, "last_message.md"), 2<<20)
	if !ok || hashBytes(body) != finalSHA || !bytes.Equal(body, []byte(str(job["last_message"]))) {
		return nil, nil, "result_unavailable"
	}
	return prompt, body, ""
}

// Read only this exact native session at its initial length. Do not infer
// continuity with another job's thread; preserve the original strict window.
func skillsHistoricalNative(s *skillsFS, job object, proof *skillsNativeResultProof) ([]object, bool) {
	id := str(job["thread_id"])
	start, e := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	end, e2 := time.Parse(time.RFC3339Nano, str(job["finished_at"]))
	if e != nil || e2 != nil || end.Before(start) || !skillsUUID.MatchString(id) {
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
	h, ok := skillsOpenAtSharing(parent, filepath.Base(p), false, false, true)
	if !ok {
		return nil, false
	}
	f := os.NewFile(uintptr(h), p)
	s.files = append(s.files, f)
	info, e := f.Stat()
	actual, e2 := ownerHandlePath(h)
	if e != nil || e2 != nil || !same(actual, p) || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 256<<20 {
		return nil, false
	}
	return skillsNativeProjectionBound(io.LimitReader(f, info.Size()), id, str(job["cwd"]), start, end, proof)
}

type skillsNativeResultProof struct {
	prompt, body, lastFinal []byte
	promptAt, finalAt       time.Time
}

func (p *skillsNativeResultProof) observe(raw []byte, at time.Time) bool {
	var envelope struct {
		Payload struct {
			Type, Role, Phase string
			Content           []struct{ Type, Text string }
		}
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	v := envelope.Payload
	// codex-cli 0.163.0's generated MessagePhase schema and the original
	// job's native metadata both use final_answer. Unknown/partial/commentary
	// phases cannot prove a terminal response. No timestamp tolerance is added.
	if v.Type != "message" || (v.Role != "user" && (v.Role != "assistant" || v.Phase != "final_answer")) {
		return true
	}
	if _, ok := conversationJSON(raw); !ok {
		return false
	}
	var text strings.Builder
	for _, block := range v.Content {
		if block.Type == "input_text" || block.Type == "output_text" {
			text.WriteString(block.Text)
		}
	}
	if v.Role == "user" && strings.Contains(text.String(), string(p.prompt)) {
		p.promptAt = at
	}
	if v.Role == "assistant" {
		p.lastFinal = []byte(text.String())
		p.finalAt = at
	}
	return true
}

func (p *skillsNativeResultProof) bound() bool {
	return len(p.prompt) > 0 && len(p.body) > 0 && !p.promptAt.IsZero() && !p.finalAt.Before(p.promptAt) && bytes.Equal(p.lastFinal, p.body)
}
