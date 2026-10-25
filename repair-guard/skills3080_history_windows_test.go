package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func skillsHistoryFixture() (skillsResultRequest, skillsRegistry, object, []byte, skillsHistoricalResult) {
	a := skillsOriginalRepair
	spec := []byte("fixed public repair; original parent " + a.ParentJob)
	a.SpecSHA = hashBytes(spec)
	r := skillsResultRequest{Task: 3686, Job: a.Job, SpecSHA: a.SpecSHA, FinalSHA: hashBytes([]byte("public final"))}
	row := skillsTask{ID: 3686, Project: "cc-pc-tools-rs", Spec: currentTodoSpecsRoot + "/3686-new.md", Jobs: []string{a.Job, "20261025-221258-todo-3686"}}
	row.Scheduler.Job, row.Scheduler.Worktree = row.Jobs[1], a.Workspace
	parent := skillsTask{ID: 3080, Project: "cc-pc-tools-rs", Status: "doing", Jobs: []string{a.ParentJob}}
	parent.Scheduler.Job, parent.Scheduler.Worktree = a.ParentJob, skillsParentRoot
	w := skillsWriter{Task: 3080, Job: a.ParentJob, Workspace: skillsParentRoot, SpecSHA: hashBytes([]byte("current parent spec"))}
	reg := skillsRegistry{Tasks: map[int]skillsTask{3080: parent, 3686: row}, writer: w}
	job := object{"id": a.Job, "todo_id": json.Number("3686"), "cwd": a.Workspace, "status": "succeeded", "exit_code": json.Number("0"), "started_at": a.Started, "finished_at": a.Finished, "thread_id": a.Thread, "launch_key": a.LaunchKey, "model": a.Model, "effort": a.Effort, "parent_job_id": nil}
	return r, reg, job, spec, a
}

func TestSkills3080HistoricalSavedFilesAndLaunchReceipt(t *testing.T) {
	for _, kind := range []string{"valid", "prompt", "spec_in_prompt", "receipt", "missing_receipt", "body", "metadata_body", "digest", "protected", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			r, _, job, spec, a := skillsHistoryFixture()
			root := skillsFixtureRoot(t)
			dir := filepath.Join(root, a.Job)
			if e := os.MkdirAll(dir, 0700); e != nil {
				t.Fatal(e)
			}
			prompt := []byte("public launch\n依頼:\n" + string(spec) + "\nrunner guidance")
			a.PromptSHA = hashBytes(prompt)
			body := []byte("public final")
			job["last_message"] = string(body)
			receipt := []byte(fmt.Sprintf(`{"id":%q}`, a.Job))
			switch kind {
			case "prompt":
				prompt = append(prompt, '!')
			case "spec_in_prompt":
				prompt = []byte("different spec")
				a.PromptSHA = hashBytes(prompt)
			case "receipt":
				receipt = []byte(`{"id":"invented"}`)
			case "body":
				body = append(body, '!')
			case "metadata_body":
				job["last_message"] = "different final"
			case "digest":
				r.FinalSHA = hashBytes([]byte("different final"))
			}
			for path, raw := range map[string][]byte{filepath.Join(dir, "prompt.md"): prompt, filepath.Join(dir, "last_message.md"): body, filepath.Join(root, ".launch-"+a.LaunchKey+".json"): receipt} {
				if e := os.WriteFile(path, raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "missing_receipt" {
				if e := os.Remove(filepath.Join(root, ".launch-"+a.LaunchKey+".json")); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "hardlink" {
				if e := os.Link(filepath.Join(dir, "last_message.md"), filepath.Join(root, "alias")); e != nil {
					t.Fatal(e)
				}
			}
			fs := skillsFS{blocked: func(p string) bool { return kind == "protected" && filepath.Base(p) == "last_message.md" }}
			defer fs.close()
			_, got, reason := skillsHistoricalFilesAt(&fs, root, a, spec, job, r.FinalSHA)
			if kind == "valid" {
				if reason != "" || string(got) != string(body) {
					t.Fatal("bound original files rejected")
				}
			} else if reason == "" {
				t.Fatal("unbound original files accepted")
			}
		})
	}
}

func TestSkills3080HistoricalNativeBindsPromptFinalAndTime(t *testing.T) {
	a := skillsOriginalRepair
	start, _ := time.Parse(time.RFC3339Nano, a.Started)
	end, _ := time.Parse(time.RFC3339Nano, a.Finished)
	prompt, body := []byte("public saved prompt\nexact spec"), []byte("public final")
	line := func(kind string, at time.Time, payload any) string {
		b, _ := json.Marshal(object{"type": kind, "timestamp": at.Format(time.RFC3339Nano), "payload": payload})
		return string(b) + "\n"
	}
	message := func(role, phase, text string) object {
		return object{"type": "message", "role": role, "phase": phase, "content": []object{{"type": "output_text", "text": text}}}
	}
	base := line("session_meta", start, object{"id": a.Thread, "cwd": a.Workspace}) +
		line("turn_context", start.Add(time.Second), object{"cwd": a.Workspace, "model": "openai/" + a.Model, "effort": a.Effort}) +
		line("response_item", start.Add(2*time.Second), message("user", "", string(prompt)+"\ncurrent saved owner instructions")) +
		line("response_item", end.Add(-time.Second), message("assistant", "final", string(body)))
	for _, kind := range []string{"valid", "later_unrelated_final", "changed_prompt", "changed_native_final", "coaltered_result", "out_of_window", "missing_prompt", "missing_final", "wrong_cwd", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			raw := base
			p := &skillsNativeResultProof{prompt: prompt, body: body}
			switch kind {
			case "later_unrelated_final":
				raw += line("response_item", end.Add(time.Second), message("assistant", "final", "other job"))
			case "changed_prompt":
				p.prompt = []byte("invented saved prompt")
			case "changed_native_final":
				raw = strings.Replace(raw, "public final", "invented final", 1)
			case "coaltered_result":
				p.body = []byte("invented final") // Matching job.json/file alone cannot pass.
			case "out_of_window":
				raw = strings.Replace(raw, end.Add(-time.Second).Format(time.RFC3339Nano), end.Add(time.Second).Format(time.RFC3339Nano), 1)
			case "missing_prompt":
				raw = strings.Replace(raw, `"role":"user"`, `"role":"other"`, 1)
			case "missing_final":
				raw = strings.Replace(raw, `"phase":"final"`, `"phase":"commentary"`, 1)
			case "wrong_cwd":
				raw = strings.ReplaceAll(raw, a.Workspace, "D:/other")
			case "truncated":
				raw += "not JSON"
			}
			native, ok := skillsNativeProjectionBound(strings.NewReader(raw), a.Thread, a.Workspace, start, end, p)
			want := kind == "valid" || kind == "later_unrelated_final"
			if ok != want || (ok && (len(native) != 1 || native[0]["effort"] != "max")) {
				t.Fatalf("native proof %s: accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080HistoricalSpecificationStaysWithItsActualAuthor(t *testing.T) {
	r, reg, job, spec, a := skillsHistoryFixture()
	if !skillsHistoricalBinding(r, reg, job, spec, a) {
		t.Fatal("registered past author was confused with the current specification")
	}
	// A normally resumed current consumer need not impersonate the old parent.
	parent := reg.Tasks[3080]
	parent.Jobs = append(parent.Jobs, "20261025-230000-todo-3080")
	parent.Scheduler.Job = parent.Jobs[1]
	reg.Tasks[3080] = parent
	reg.writer.Job = parent.Jobs[1]
	if !skillsHistoricalBinding(r, reg, job, spec, a) {
		t.Fatal("registered current consumer invalidated its actual past author")
	}
}

func TestSkills3080RegisteredParentReviewCanReturnItsOriginalResponse(t *testing.T) {
	// The parent's 3847 template omitted the acceptance marker. Returning its
	// actual review result must remain separate from accepting it for creation.
	spec := []byte("Read only the parent's fixed public files; return findings")
	r := skillsResultRequest{Task: 3847, Job: "20261025-215831-todo-3847", SpecSHA: hashBytes(spec), FinalSHA: hashBytes([]byte("findings"))}
	parent := skillsTask{ID: 3080, Project: "cc-pc-tools-rs", Jobs: []string{"20261025-074042-todo-3080"}}
	parent.Scheduler.Job, parent.Scheduler.Worktree = parent.Jobs[0], skillsParentRoot
	w := skillsWriter{Task: 3080, Job: parent.Jobs[0], Workspace: skillsParentRoot}
	row := skillsTask{ID: r.Task, Project: parent.Project, Spec: "D:/CC/Tools/todo/specs/3847-fixed.md", Jobs: []string{r.Job}}
	row.Scheduler.Job, row.Scheduler.Worktree = r.Job, "D:/CC/Work/public-review3847"
	row.Subwork.Parent, row.Subwork.ParentJob, row.Subwork.Kind = 3080, parent.Jobs[0], "review"
	row.Subwork.Owner.Worktree = skillsParentRoot
	row.Scheduler.Admission = &skillsAdmission{Key: "real-admission", Spec: row.Spec, Worktree: row.Scheduler.Worktree, Subwork: row.Subwork}
	job := object{"id": r.Job, "todo_id": json.Number("3847"), "status": "succeeded", "exit_code": json.Number("0"), "cwd": row.Scheduler.Worktree, "sandbox": "read-only", "parent_job_id": nil, "launch_key": "real-admission", "subwork_validated": true, "subwork": object{"parent_todo_id": json.Number("3080"), "origin_job_id": parent.Jobs[0], "kind": "review", "owner": object{"worktree": skillsParentRoot}}, "finite_review": object{"spec": row.Spec}}
	check := func(row skillsTask, w skillsWriter) bool {
		reg := skillsRegistry{Tasks: map[int]skillsTask{3080: parent, 3847: row}, writer: w}
		return skillsResultBindingWith(r, reg, job, spec, false, map[int]string{})
	}
	if !check(row, w) {
		t.Fatal("registered public parent review could not return its original response")
	}
	received := skillsReceived{Spec: spec, Body: []byte("findings"), Metadata: object{"model": "gpt-6-luna", "effort": "max"}, Native: []object{{"model": "gpt-6-luna", "effort": "max"}}}
	if skillsReviewAccepted(received, strings.Repeat("a", 64), "gpt-6.1-sol") {
		t.Fatal("raw response became creation acceptance without the correct protocol")
	}
	for _, kind := range []string{"other_parent", "unregistered", "admission", "research", "held", "other_consumer", "no_digest"} {
		t.Run(kind, func(t *testing.T) {
			copy, writer := row, w
			switch kind {
			case "other_parent":
				copy.Subwork.Parent = 3686
			case "unregistered":
				copy.Jobs = nil
			case "admission":
				copy.Scheduler.Admission = nil
			case "research":
				copy.Subwork.Kind = "research"
			case "held":
				parent.OwnerStopped = true
				defer func() { parent.OwnerStopped = false }()
			case "other_consumer":
				writer.Task = 3686
			case "no_digest":
				r.FinalSHA = ""
				defer func() { r.FinalSHA = hashBytes([]byte("findings")) }()
			}
			if check(copy, writer) {
				t.Fatal("unbound review result accepted")
			}
		})
	}
}

func TestSkills3080HistoricalBindingRejectsSubstitution(t *testing.T) {
	for _, kind := range []string{"current_spec", "other_job", "future_job", "unregistered_author", "duplicate_author", "parent", "consumer", "stale_consumer", "held_parent", "held_repair", "cwd", "thread", "start", "finish", "model", "effort", "launch", "runner_parent", "unsucceeded", "no_final_digest", "altered_spec"} {
		t.Run(kind, func(t *testing.T) {
			r, reg, job, spec, a := skillsHistoryFixture()
			row := reg.Tasks[3686]
			parent := reg.Tasks[3080]
			switch kind {
			case "current_spec":
				r.SpecSHA = reg.writer.SpecSHA
			case "other_job":
				r.Job = "20261025-202458-todo-3686"
			case "future_job":
				r.Job = "20261026-000000-todo-3686"
			case "unregistered_author":
				row.Jobs = row.Jobs[1:]
			case "duplicate_author":
				row.Jobs = append(row.Jobs, a.Job)
			case "parent":
				parent.Jobs = []string{"20261025-000000-todo-3080"}
			case "consumer":
				reg.writer.Task = 3686
			case "stale_consumer":
				parent.Jobs = append(parent.Jobs, "20261025-230000-todo-3080")
				parent.Scheduler.Job = parent.Jobs[1]
			case "held_parent":
				parent.OwnerStopped = true
			case "held_repair":
				row.OwnerStopped = true
			case "cwd":
				job["cwd"] = "D:/CC/Work/other"
			case "thread":
				job["thread_id"] = "01234567-1234-1234-1234-0123456789ab"
			case "start":
				job["started_at"] = "2026-10-25T21:00:00+09:00"
			case "finish":
				job["finished_at"] = "2026-10-25T23:00:00+09:00"
			case "model":
				job["model"] = "gpt-6-luna"
			case "effort":
				job["effort"] = "low"
			case "launch":
				job["launch_key"] = "invented"
			case "runner_parent":
				job["parent_job_id"] = a.ParentJob
			case "unsucceeded":
				job["status"] = "running"
			case "no_final_digest":
				r.FinalSHA = ""
			case "altered_spec":
				spec = append(spec, '!')
			}
			reg.Tasks[3686], reg.Tasks[3080] = row, parent
			if skillsHistoricalBinding(r, reg, job, spec, a) {
				t.Fatal("substituted history accepted")
			}
		})
	}
}
