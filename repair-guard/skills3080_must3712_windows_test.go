package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// A real repository name and a preserved caller-chosen digest do not prove
// that these bytes were acquired at that repository's fixed path.
func TestSkills3080RejectsInventedSourceBytes(t *testing.T) {
	f := skillsAcquiredFixture(t, []byte("real acquired source"), []byte("real acquired notice"), false)
	m := skillsTestManifest()
	m.Sources[0].Repository = "6mile/skills"
	m.Sources[0].Commit = skillsPublicSources["6mile/skills"]
	m.Sources[0].SHA = hashBytes([]byte("invented source"))
	m.Sources[0].License.SHA = hashBytes([]byte("invented permission"))
	m.Files = append(m.Files,
		skillsOutput{Target: "sample/upstream.txt", SHA: m.Sources[0].SHA, body: []byte("invented source")},
		skillsOutput{Target: "sample/LICENSE.txt", SHA: m.Sources[0].License.SHA, body: []byte("invented permission")})
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	if skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors) {
		t.Fatal("caller-invented bytes acquired the identity of a real public path")
	}
}

func skillsReviewGenerationFixture() (skillsRegistry, skillsTask, object, object) {
	old, current := "20261025-004702-todo-3686", "20261025-051006-todo-3686"
	w := skillsWriter{Task: 3686, Job: current, Workspace: "D:/CC/Work/todo-3686-test"}
	parent := skillsTask{ID: 3686, Project: "cc-pc-tools-rs", Jobs: []string{old, current}}
	parent.Scheduler.Job, parent.Scheduler.Worktree = current, w.Workspace
	row := skillsTask{ID: 3718, Project: "cc-pc-tools-rs"}
	row.Subwork.Parent, row.Subwork.Kind, row.Subwork.ParentJob = 3686, "review", old
	row.Subwork.Owner.Worktree = w.Workspace
	row.Scheduler.SubworkParent = old
	reg := skillsRegistry{Tasks: map[int]skillsTask{3080: {ID: 3080, Project: "cc-pc-tools-rs"}, 3686: parent}, writer: w}
	child := object{"started_at": "2026-10-24T18:33:55Z"}
	origin := object{"id": old, "todo_id": json.Number("3686"), "cwd": w.Workspace, "status": "failed", "started_at": "2026-10-24T15:47:02Z"}
	return reg, row, child, origin
}

func TestSkills3080PersistedAdmissionBindsTheActualLaunchParent(t *testing.T) {
	reg, row, child, _ := skillsReviewGenerationFixture()
	row.Scheduler.SubworkParent = "" // Normal scheduler persists admission, not this preparation-only field.
	row.Spec = "D:/CC/Tools/todo/specs/3740-a.md"
	row.Scheduler.Worktree = "D:/CC/Work/independent-review"
	row.Scheduler.Admission = &skillsAdmission{Key: "one-real-admitted-launch", Spec: row.Spec, Worktree: row.Scheduler.Worktree, Subwork: row.Subwork}
	child["launch_key"], child["subwork_validated"] = "one-real-admitted-launch", true
	child["subwork"] = object{"kind": "review", "owner": object{"worktree": row.Subwork.Owner.Worktree}}
	if !skillsReviewAdmissionBound(row, child) || !skillsReviewParentBound(reg, row, false, row.Subwork.ParentJob) {
		t.Fatal("normal persisted receipt could not bind the real registered parent")
	}
	if !skillsReviewParentBound(reg, row, false, "") {
		t.Fatal("normal departed launch parent invalidated the registered origin")
	}
	for _, kind := range []string{"missing", "key", "spec", "worktree", "origin", "owner", "validation"} {
		t.Run(kind, func(t *testing.T) {
			copy := row
			a := *row.Scheduler.Admission
			copy.Scheduler.Admission = &a
			j := objClone(child)
			switch kind {
			case "missing":
				copy.Scheduler.Admission = nil
			case "key":
				a.Key = "other-launch"
			case "spec":
				a.Spec = "D:/other-spec"
			case "worktree":
				a.Worktree = "D:/other-worker"
			case "origin":
				a.Subwork.ParentJob = "20261025-000000-todo-3686"
			case "owner":
				a.Subwork.Owner.Worktree = "D:/other-parent"
			case "validation":
				j["subwork_validated"] = false
			}
			if skillsReviewAdmissionBound(copy, j) {
				t.Fatal("another preparation or scope became the admitted review")
			}
		})
	}
}

func TestSkills3080ReviewRegisteredContinuation(t *testing.T) {
	reg, row, child, origin := skillsReviewGenerationFixture()
	if !skillsReviewParentBound(reg, row, false, row.Scheduler.SubworkParent) || !skillsReviewOriginBound(reg.Tasks[3686], row, child, origin) {
		t.Fatal("a genuine prior author and the registered resumed actor were conflated")
	}
	row.Scheduler.SubworkParent = reg.writer.Job
	if !skillsReviewParentBound(reg, row, false, row.Scheduler.SubworkParent) {
		t.Fatal("normal scheduler rebinding erased the registered original author")
	}
	launch := objClone(origin)
	launch["id"], launch["started_at"], launch["status"] = reg.writer.Job, "2026-10-24T20:10:06Z", "running"
	child["started_at"] = "2026-10-24T20:11:00Z"
	if !skillsReviewAuthorBound(reg.Tasks[3686], row.Scheduler.SubworkParent, child, launch) {
		t.Fatal("a real resumed launch parent was confused with the original author")
	}
	row.Scheduler.SubworkParent = ""
	if !skillsReviewParentBound(reg, row, false, row.Scheduler.SubworkParent) {
		t.Fatal("removing a departed launch parent erased the registered author")
	}
	// A review requested by this current author is also valid. Source/head and
	// exact response checks remain separate mandatory gates at the consumer.
	row.Subwork.ParentJob, row.Scheduler.SubworkParent = reg.writer.Job, reg.writer.Job
	origin["id"], origin["status"] = reg.writer.Job, "running"
	if !skillsReviewParentBound(reg, row, false, row.Scheduler.SubworkParent) || !skillsReviewOriginBound(reg.Tasks[3686], row, child, origin) {
		t.Fatal("current registered author rejected")
	}
	for _, kind := range []string{"fictional", "wrong-parent-task", "stale-writer", "future-generation", "duplicate-origin", "disagreeing-parent", "wrong-origin-task", "wrong-origin-workspace", "wrong-origin-time"} {
		t.Run(kind, func(t *testing.T) {
			reg, row, child, origin := skillsReviewGenerationFixture()
			parent := reg.Tasks[3686]
			switch kind {
			case "fictional":
				row.Subwork.ParentJob = "20261025-000000-todo-3686"
				row.Scheduler.SubworkParent = row.Subwork.ParentJob
			case "wrong-parent-task":
				row.Subwork.Parent = 3080
			case "stale-writer":
				reg.writer.Job = row.Subwork.ParentJob
			case "future-generation":
				parent.Jobs = []string{reg.writer.Job, row.Subwork.ParentJob}
			case "duplicate-origin":
				parent.Jobs = []string{row.Subwork.ParentJob, row.Subwork.ParentJob, reg.writer.Job}
			case "disagreeing-parent":
				row.Scheduler.SubworkParent = "20261025-000000-todo-3686"
			case "wrong-origin-task":
				origin["todo_id"] = json.Number("3080")
			case "wrong-origin-workspace":
				origin["cwd"] = "D:/unrelated"
			case "wrong-origin-time":
				origin["started_at"] = "2026-10-25T00:00:00Z"
			}
			reg.Tasks[3686] = parent
			if skillsReviewParentBound(reg, row, false, row.Scheduler.SubworkParent) && skillsReviewOriginBound(parent, row, child, origin) {
				t.Fatal("unregistered or mismatched author generation admitted")
			}
		})
	}
}

func TestSkills3080NativeUsesClosedAttemptWindow(t *testing.T) {
	id, cwd := "01234567-1234-1234-1234-0123456789ab", "D:/CC/Work/public"
	start := time.Date(2026, 10, 24, 23, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	meta := fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"cwd\":%q}}\n", id, cwd)
	turn := func(at time.Time, model string) string {
		return fmt.Sprintf("{\"type\":\"turn_context\",\"timestamp\":%q,\"payload\":{\"model\":%q,\"effort\":\"max\"}}\n", at.Format(time.RFC3339Nano), model)
	}
	raw := meta + turn(start.Add(-time.Nanosecond), "gpt-6-astra") + turn(start, "gpt-6-luna") + turn(end, "gpt-6-luna") + turn(end.Add(time.Nanosecond), "gpt-6-astra")
	projected, ok := skillsNativeProjection(strings.NewReader(raw), id, cwd, start, end)
	if !ok || len(projected) != 2 || projected[0]["model"] != "gpt-6-luna" || projected[1]["model"] != "gpt-6-luna" {
		t.Fatal("native observation crossed the exact attempt boundary")
	}
	if _, ok := skillsNativeProjection(strings.NewReader(meta+turn(end.Add(time.Nanosecond), "gpt-6-luna")), id, cwd, start, end); ok {
		t.Fatal("an exclusively post-completion context qualified this attempt")
	}
}

func TestSkills3080RootReviewMigrationIsExact(t *testing.T) {
	// The production hash is independent of this fixture; injecting it here
	// verifies that a lookalike or new root job is never a generic review grant.
	r := skillsResultRequest{Task: 3709, Job: skillsReplacementReviewJob, SpecSHA: skillsReplacementReviewSpec}
	row := skillsTask{ID: 3709, Project: "cc-pc-tools-rs", Jobs: []string{r.Job}}
	row.Scheduler.Worktree = "D:/CC/Work/fixed-review"
	reg := skillsRegistry{Tasks: map[int]skillsTask{3080: {ID: 3080, Project: "cc-pc-tools-rs"}, 3709: row}}
	job := object{"id": r.Job, "todo_id": json.Number("3709"), "status": "succeeded", "exit_code": json.Number("0"), "cwd": row.Scheduler.Worktree, "model": "gpt-6-luna", "effort": "max", "model_origin": "owner_request", "effort_origin": "owner_request"}
	if !skillsReplacementReviewBound(r, job) {
		t.Fatal("designated replacement identity rejected")
	}
	for _, field := range []string{"model", "effort", "model_origin", "effort_origin"} {
		copy := objClone(job)
		copy[field] = "different"
		if skillsReplacementReviewBound(r, copy) {
			t.Fatalf("replacement %s was not fixed", field)
		}
	}
	// Exact saved-spec bytes are required by the outer binding check, so these
	// invented bytes must fail even with the designated task/job/model names.
	if skillsResultBindingWith(r, reg, job, []byte("lookalike 3669 review"), false, map[int]string{}) {
		t.Fatal("invented scope became the fixed replacement review")
	}
	changed := r
	changed.Job = "20261025-060000-todo-3709"
	if skillsReplacementReviewBound(changed, job) || skillsResultBindingWith(changed, reg, job, nil, false, map[int]string{}) {
		t.Fatal("new root generation reused the replacement grant")
	}
}

func TestSkills3080ReviewParentMustBeRegistered(t *testing.T) {
	spec := []byte("SKILLS3080_IMPLEMENTATION_REVIEW " + strings.Repeat("a", 40))
	r := skillsResultRequest{Task: 3718, Job: "20261025-033355-todo-3718", SpecSHA: hashBytes(spec)}
	row := skillsTask{ID: r.Task, Project: "cc-pc-tools-rs", Spec: "D:/CC/Tools/todo/specs/3718-a.md", Jobs: []string{r.Job}}
	row.Scheduler.Worktree = "D:/CC/Work/public-review"
	row.Subwork.Parent, row.Subwork.Kind = 3686, "review"
	row.Subwork.ParentJob = "20261025-000000-todo-3686" // not in the real parent's history
	row.Scheduler.SubworkParent = row.Subwork.ParentJob
	parent := skillsTask{ID: 3686, Project: "cc-pc-tools-rs", Jobs: []string{"20261025-051006-todo-3686"}}
	parent.Scheduler.Job = parent.Jobs[0]
	reg := skillsRegistry{Tasks: map[int]skillsTask{3080: {ID: 3080, Project: "cc-pc-tools-rs"}, 3686: parent, 3718: row}}
	job := object{"id": r.Job, "todo_id": json.Number("3718"), "status": "succeeded", "exit_code": json.Number("0"), "cwd": row.Scheduler.Worktree,
		"sandbox": "read-only", "parent_job_id": row.Subwork.ParentJob,
		"subwork": object{"parent_todo_id": json.Number("3686"), "origin_job_id": row.Subwork.ParentJob}, "finite_review": object{"spec": row.Spec}}
	if skillsResultBindingWith(r, reg, job, spec, false, map[int]string{}) {
		t.Fatal("a child record's self-consistent fictional parent was accepted")
	}
}
