package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkills3080TypedRepairHistoryIsNotJobAnAllowlist(t *testing.T) {
	for _, kind := range []string{"valid", "another_registered_continuation", "private_spec", "posthoc_scope", "unregistered", "future_generation", "other_parent", "other_cwd", "other_task", "snapshot_escape", "snapshot_other_task", "failed", "held_parent", "held_repair", "duplicate_job", "inverted_time", "invented_parent"} {
		t.Run(kind, func(t *testing.T) {
			r, reg, job, _, _ := skillsHistoryFixture()
			c := skillsPublicScopeFixture()
			c.Domain = "skills-3080-public-repair-v1"
			c.Instructions = skillsPublicRepairInstructions
			r.SpecPath = currentTodoSpecsRoot + "/3686-33333333333333333333333333333333.md"
			row := reg.Tasks[3686]
			row.Scheduler.Branch = "scheduler/todo-3686-synthetic"
			reg.Tasks[3686] = row
			switch kind {
			case "another_registered_continuation":
				r.Job = "20261025-211400-todo-3686"
				row.Jobs[0] = r.Job
				reg.Tasks[3686] = row
				job["id"] = r.Job
			case "private_spec":
				c.Instructions = "Review private files too"
			case "unregistered":
				row.Jobs = row.Jobs[1:]
				reg.Tasks[3686] = row
			case "future_generation":
				row.Jobs = []string{row.Jobs[1], r.Job}
				reg.Tasks[3686] = row
			case "other_parent":
				c.Author.Job = "20261024-235901-todo-3080"
			case "other_cwd":
				job["cwd"] = "D:/unrelated"
			case "other_task":
				job["todo_id"] = json.Number("3999")
			case "snapshot_escape":
				r.SpecPath = "D:/private.json"
			case "snapshot_other_task":
				r.SpecPath = currentTodoSpecsRoot + "/3999-33333333333333333333333333333333.md"
			case "failed":
				job["status"] = "failed"
			case "held_parent":
				p := reg.Tasks[3080]
				p.OwnerStopped = true
				reg.Tasks[3080] = p
			case "held_repair":
				row.OwnerStopped = true
				reg.Tasks[3686] = row
			case "duplicate_job":
				row.Jobs = append(row.Jobs, r.Job)
				reg.Tasks[3686] = row
			case "inverted_time":
				job["finished_at"] = "2026-10-24T00:00:00Z"
			case "invented_parent":
				job["parent_job_id"] = "20261025-074042-todo-9999"
			}
			raw, _ := json.Marshal(c)
			if kind == "posthoc_scope" {
				raw = append([]byte("private spec quoted: "), raw...)
			}
			r.SpecSHA = hashBytes(raw)
			parsed, valid := skillsParsePublicRepair(raw)
			ok := valid && skillsTypedRepairBinding(r, reg, job, raw, parsed)
			if ok != (kind == "valid" || kind == "another_registered_continuation") {
				t.Fatalf("typed history %s accepted=%v", kind, ok)
			}
		})
	}
	legacy := skillsLegacyPublicRepair003920
	if legacy.SpecPath != "" || legacy.FinalSHA != "2a8a0768492a6a45281a378843c009c59ed9f80cce489fc441d9889bc76c612e" || skillsRepair003920.PromptSHA == "" {
		t.Fatal("legacy migration no longer exact")
	}
}

func TestSkills3080PublicRepairCannotBorrowReviewContract(t *testing.T) {
	c := skillsPublicScopeFixture()
	raw, _ := json.Marshal(c)
	if _, ok := skillsParsePublicRepair(raw); ok {
		t.Fatal("readonly review acquired repair scope")
	}
	c.Domain = "skills-3080-public-repair-v1"
	c.Instructions = skillsPublicRepairInstructions
	f := skillsPublicReviewFile{Path: "repair-guard/main_windows.go", Blob: strings.Repeat("d", 40), SHA: strings.Repeat("e", 64), Bytes: 123}
	c.Files = append(c.Files, f)
	raw, _ = json.Marshal(c)
	if _, ok := skillsParsePublicRepair(raw); !ok {
		t.Fatal("typed fixed public repair source rejected")
	}
	if _, ok := skillsParsePublicReview(raw); ok {
		t.Fatal("repair contract became a readonly content acceptance")
	}
	c.Files[1].Path = "tools/unrelated/main.go"
	raw, _ = json.Marshal(c)
	if _, ok := skillsParsePublicRepair(raw); ok {
		t.Fatal("unrelated product entered repair contract")
	}
}
