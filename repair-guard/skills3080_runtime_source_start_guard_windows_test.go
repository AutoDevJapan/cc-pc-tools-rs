package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkills3080SourceStartRetainsSpecPinThroughNormalCommand(t *testing.T) {
	root := skillsFixtureRoot(t)
	if os.Mkdir(filepath.Join(root, ".ci-work"), 0700) != nil {
		t.Fatal("private fixture")
	}
	path := filepath.Join(root, "prepared-spec.json")
	expected := []byte("independent prepared specification")
	if os.WriteFile(path, expected, 0600) != nil {
		t.Fatal("spec control")
	}
	fs := skillsFS{blocked: func(string) bool { return false }}
	spec, ok := fs.read(path, 1<<20)
	if !ok || !bytes.Equal(spec, expected) {
		fs.close()
		t.Fatal("held exact spec")
	}
	r := skillsRequest{Writer: skillsWriter{Task: 3080, Workspace: root}, RuntimeSource: &skillsRuntimeSourceRequest{PreviousJob: "20300101-000000-todo-6291"}}
	calls := 0
	out := skillsSourceStartGuardedDispatch(&fs, r, skillsTask{ID: 6291, sourceReadyGeneration: 1, sourceReadyGenerationKnown: true}, hashBytes([]byte("request")), strings.Repeat("a", 32), func() skillsSourceCommandResult {
		calls++
		if os.WriteFile(path, []byte("changed in place"), 0600) == nil {
			t.Fatal("dispatch-time overwrite succeeded")
		}
		other := filepath.Join(root, "replacement.json")
		if os.WriteFile(other, []byte("replacement"), 0600) != nil {
			t.Fatal("replacement control")
		}
		if os.Rename(other, path) == nil {
			t.Fatal("dispatch-time replacement succeeded")
		}
		return skillsSourceCommandResult{ok: true, stdout: []byte(`{"id":6291,"launched":"20300101-000001-todo-6291"}`)}
	}, func(string) bool { return true })
	fs.close()
	if calls != 1 || out["qualified"] != true {
		t.Fatal("single controlled command did not run")
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatal("pinned spec changed")
	}
	if os.WriteFile(path, []byte("after close control"), 0600) != nil {
		t.Fatal("overwrite control must succeed after pin release")
	}
}

func TestSkills3080SourceStartRetainsUnknownAttemptAcrossCalls(t *testing.T) {
	for _, kind := range []string{"timeout", "command_failed", "invalid_response", "accepted", "wrong_launch_tuple", "active", "known_nonlaunch"} {
		t.Run(kind, func(t *testing.T) {
			root := skillsFixtureRoot(t)
			if os.Mkdir(filepath.Join(root, ".ci-work"), 0700) != nil {
				t.Fatal("private fixture")
			}
			r := skillsRequest{Writer: skillsWriter{Task: 3080, Job: "20300101-000000-todo-3080", Workspace: root}, RuntimeSource: &skillsRuntimeSourceRequest{PreviousJob: "20300101-000000-todo-6291"}}
			row := skillsTask{ID: 6291, sourceReadyGeneration: 1, sourceReadyGenerationKnown: true}
			calls := 0
			run := func() skillsSourceCommandResult {
				calls++
				switch kind {
				case "timeout", "command_failed":
					return skillsSourceCommandResult{diagnostic: object{"outcome": kind}}
				case "invalid_response":
					return skillsSourceCommandResult{ok: true, stdout: []byte(`{"id":6291}`)}
				case "accepted", "wrong_launch_tuple":
					return skillsSourceCommandResult{ok: true, stdout: []byte(`{"id":6291,"launched":"20300101-000001-todo-6291"}`)}
				case "active":
					return skillsSourceCommandResult{ok: true, stdout: []byte(`{"id":6291,"launched":null,"active":true}`)}
				default:
					return skillsSourceCommandResult{ok: true, stdout: []byte(`{"id":6291,"launched":null,"active":false,"status":"todo","allowance_now":0}`)}
				}
			}
			bind := func(string) bool { return kind == "accepted" }
			first := skillsFS{blocked: func(string) bool { return false }}
			out := skillsSourceStartGuardedDispatch(&first, r, row, hashBytes([]byte("request")), strings.Repeat("a", 32), run, bind)
			first.close() // Models a new process after normal return or a crash.
			if (out["qualified"] == true) != (kind == "accepted") {
				t.Fatal("unbound launch qualified")
			}
			if kind == "wrong_launch_tuple" && out["failure_stage"] != "source_launch_binding" {
				t.Fatal("wrong actual admission accepted")
			}
			// Changing request bytes, nonce, issuer generation, or path spelling
			// must not turn the unknown first attempt into a fresh attempt.
			r.Writer.Job = "20300101-000002-todo-3080"
			r.Writer.Workspace = strings.ToUpper(filepath.ToSlash(root))
			second := skillsFS{blocked: func(string) bool { return false }}
			defer second.close()
			next := skillsSourceStartGuardedDispatch(&second, r, row, hashBytes([]byte("other request")), strings.Repeat("b", 32), run, bind)
			if kind == "known_nonlaunch" {
				if calls != 2 || next["failure_stage"] == "source_start_pending" {
					t.Fatal("known completed non-launch cannot retry")
				}
			} else if calls != 1 || next["failure_stage"] != "source_start_pending" {
				t.Fatal("unknown/accepted attempt dispatched twice")
			}
		})
	}
}

func TestSkills3080SourceStartClaimIsExclusiveWhileHeld(t *testing.T) {
	root := skillsFixtureRoot(t)
	if os.Mkdir(filepath.Join(root, ".ci-work"), 0700) != nil {
		t.Fatal("private fixture")
	}
	first := skillsFS{blocked: func(string) bool { return false }}
	defer first.close()
	claim, ok := skillsSourceStartClaim(&first, root, 6291, "20300101-000000-todo-6291", hashBytes([]byte("a")), strings.Repeat("a", 32), 1)
	if !ok {
		t.Fatal("first claim")
	}
	if os.WriteFile(claim, []byte("replacement"), 0600) == nil || os.Remove(claim) == nil {
		t.Fatal("attempt lost deny-write/delete pin")
	}
	second := skillsFS{blocked: func(string) bool { return false }}
	defer second.close()
	if _, ok := skillsSourceStartClaim(&second, root, 6291, "20300101-000000-todo-6291", hashBytes([]byte("b")), strings.Repeat("b", 32), 1); ok {
		t.Fatal("parallel claim accepted")
	}
}

func TestSkills3080SourceLaunchedTupleRequiresActualAdmission(t *testing.T) {
	for _, kind := range []string{"normal", "spec_path", "spec_digest", "parent", "owner_parent", "key", "admission", "cwd", "subwork", "sandbox", "unlinked", "subwork_spec", "needs", "assigned_needs", "model", "effort"} {
		t.Run(kind, func(t *testing.T) {
			p := skillsSourcePreparationFixture()
			r := skillsRequest{Writer: p.Writer}
			original := skillsTask{ID: p.Task, Spec: p.PreparedSpec}
			original.Subwork.Parent = 3080
			original.Subwork.ParentJob = p.Writer.Job
			original.Subwork.Kind = "review"
			original.Subwork.Owner.Worktree = p.Writer.Workspace
			current := original
			id, key := "20300101-000001-todo-6291", strings.Repeat("a", 32)
			current.Jobs = []string{id}
			current.Scheduler.Job = id
			current.Scheduler.Worktree = filepath.Join(p.Writer.Workspace, "separate-child")
			current.Scheduler.Admission = &skillsAdmission{Key: key, Spec: original.Spec, Worktree: current.Scheduler.Worktree, Subwork: original.Subwork}
			subworkBytes, _ := json.Marshal(original.Subwork)
			subwork, _ := conversationJSON(subworkBytes)
			job := object{"id": id, "todo_id": json.Number("6291"), "parent_job_id": p.Writer.Job, "subwork_owner_job_id": p.Writer.Job, "sandbox": "read-only", "cwd": current.Scheduler.Worktree, "launch_key": key, "subwork_validated": true, "subwork": subwork, "subwork_spec": original.Spec, "assigned_needs": []any{}, "model": "gpt-6-luna", "effort": "max", "finite_review": object{"spec": original.Spec, "spec_sha256": p.SpecSHA}}
			switch kind {
			case "spec_path":
				current.Spec = filepath.Join(p.Root, "other.json")
			case "spec_digest":
				obj(job["finite_review"])["spec_sha256"] = strings.Repeat("f", 64)
			case "parent":
				job["parent_job_id"] = "20300101-000002-todo-3080"
			case "owner_parent":
				job["subwork_owner_job_id"] = "20300101-000002-todo-3080"
			case "key":
				job["launch_key"] = strings.Repeat("b", 32)
			case "admission":
				current.Scheduler.Admission = nil
			case "cwd":
				job["cwd"] = p.Writer.Workspace
			case "subwork":
				current.Subwork.ParentJob = "20300101-000002-todo-3080"
			case "sandbox":
				job["sandbox"] = "workspace-write"
			case "unlinked":
				current.Jobs = nil
			case "subwork_spec":
				job["subwork_spec"] = filepath.Join(p.Root, "other.json")
			case "needs":
				current.Needs = []string{"cargo"}
			case "assigned_needs":
				job["assigned_needs"] = []any{"cargo"}
			case "model":
				job["model"] = "unknown"
			case "effort":
				job["effort"] = "low"
			}
			if skillsSourceLaunchedTuple(r, original, current, job, p.SpecSHA) != (kind == "normal") {
				t.Fatal("actual launch tuple did not bind")
			}
		})
	}
}

func TestSkills3080SourceBusyUsesDeclaredNeedsAndNormalResponse(t *testing.T) {
	for _, c := range []struct {
		status string
		needs  []string
		want   string
	}{
		{"todo", []string{"cargo"}, "busy"}, {"todo", nil, "capacity"}, {"todo", []string{"gpu"}, "capacity"}, {"doing", []string{"cargo"}, "status_not_todo"},
	} {
		raw := []byte(`{"id":6291,"launched":null,"active":false,"status":"` + c.status + `","busy_resources":["cargo"],"allowance_now":0}`)
		v, _, outcome := skillsSourceDispatchLaunch(raw, 6291)
		d := skillsSourceDispatchAdmissionValue(raw, v, outcome, 6291, c.needs...)
		if d["admission_reason"] != c.want {
			t.Fatal("global busy resource or status misclassified")
		}
	}
}

func TestSkills3080SourceReadyGenerationRequiresRevokedReservation(t *testing.T) {
	for _, kind := range []string{"normal", "doing", "key", "missing_key", "job", "worker", "receipt_generation", "number_string", "number_exponent", "zero", "alias"} {
		t.Run(kind, func(t *testing.T) {
			m, ok := conversationJSON([]byte(`{"status":"todo","resume_generation":13,"worker_receipt":{"job":null,"generation":13},"scheduler":{"job":null,"key":null}}`))
			if !ok {
				t.Fatal("canonical normal ready fixture")
			}
			switch kind {
			case "doing":
				m["status"] = "doing"
			case "key":
				obj(m["scheduler"])["key"] = strings.Repeat("a", 32)
			case "missing_key":
				delete(obj(m["scheduler"]), "key")
			case "job":
				obj(m["scheduler"])["job"] = "20300101-000001-todo-6291"
			case "worker":
				obj(m["worker_receipt"])["job"] = "20300101-000001-todo-6291"
			case "receipt_generation":
				obj(m["worker_receipt"])["generation"] = json.Number("12")
			case "number_string":
				m["resume_generation"] = "13"
			case "number_exponent":
				m["resume_generation"] = json.Number("13e0")
			case "zero":
				m["resume_generation"] = json.Number("0")
				obj(m["worker_receipt"])["generation"] = json.Number("0")
			case "alias":
				m["Resume_generation"] = m["resume_generation"]
				delete(m, "resume_generation")
			}
			generation, valid := skillsSourceReadyGeneration(m)
			if valid != (kind == "normal") || (valid && generation != 13) {
				t.Fatal("unrevoked or unbound generation reused")
			}
		})
	}
}

func TestSkills3080SourceStartResumesOnlyAfterNormalReadyGenerationChanges(t *testing.T) {
	root := skillsFixtureRoot(t)
	if os.Mkdir(filepath.Join(root, ".ci-work"), 0700) != nil {
		t.Fatal("private fixture")
	}
	r := skillsRequest{Writer: skillsWriter{Task: 3080, Workspace: root}, RuntimeSource: &skillsRuntimeSourceRequest{PreviousJob: "20300101-000000-todo-6291"}}
	row := skillsTask{ID: 6291, sourceReadyGeneration: 13, sourceReadyGenerationKnown: true}
	calls := 0
	unknown := func() skillsSourceCommandResult {
		calls++
		return skillsSourceCommandResult{diagnostic: object{"outcome": "timeout"}}
	}
	bind := func(string) bool { return false }
	fs := skillsFS{blocked: func(string) bool { return false }}
	skillsSourceStartGuardedDispatch(&fs, r, row, hashBytes([]byte("request")), strings.Repeat("a", 32), unknown, bind)
	fs.close()
	fs = skillsFS{blocked: func(string) bool { return false }}
	blocked := skillsSourceStartGuardedDispatch(&fs, r, row, hashBytes([]byte("changed request")), strings.Repeat("b", 32), unknown, bind)
	fs.close()
	if calls != 1 || blocked["failure_stage"] != "source_start_pending" {
		t.Fatal("nonce/request cleared uncertainty")
	}
	m, ok := conversationJSON([]byte(`{"status":"todo","resume_generation":14,"worker_receipt":{"job":null,"generation":14},"scheduler":{"job":null,"key":null}}`))
	if !ok {
		t.Fatal("normal revoked generation")
	}
	row.sourceReadyGeneration, row.sourceReadyGenerationKnown = skillsSourceReadyGeneration(m)
	fs = skillsFS{blocked: func(string) bool { return false }}
	next := skillsSourceStartGuardedDispatch(&fs, r, row, hashBytes([]byte("request")), strings.Repeat("a", 32), func() skillsSourceCommandResult {
		calls++
		return skillsSourceCommandResult{ok: true, stdout: []byte(`{"id":6291,"launched":null,"active":false}`)}
	}, bind)
	fs.close()
	if calls != 2 || next["failure_stage"] == "source_start_pending" {
		t.Fatal("normal revocation did not allow the reconciled generation")
	}
	entries, err := os.ReadDir(filepath.Join(root, ".ci-work"))
	if err != nil || len(entries) != 3 {
		t.Fatal("uncertain original attempt was removed or overwritten")
	}
}
