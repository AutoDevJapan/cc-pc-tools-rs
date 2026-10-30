package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// All records are synthetic; no real ToDo, job, result or central Skill is read.
func skillsAssignmentFixture(id int) (string, object) {
	job := fmt.Sprintf("20261028-203114-todo-%d", id)
	raw := fmt.Sprintf(`{"id":%d,"project":"cc-pc-tools-rs","status":"doing","jobs":[%q],"resume_generation":7,"worker_receipt":{"job":%q,"generation":7},"scheduler":{"job":%q,"key":null,"phase":"running"}}`, id, job, job, job)
	return raw, object{"id": job, "todo_id": json.Number(fmt.Sprint(id)), "launch_key": "original-launch"}
}

func TestSkills3080CanonicalAssignmentKeepsActorRowGates(t *testing.T) {
	for _, id := range []int{3080, 3686} {
		raw, _ := skillsAssignmentFixture(id)
		registry := fmt.Sprintf(`{"projects":{"cc-pc-tools-rs":%q},"items":[%s]}`, roots[3], raw)
		reg, ok := skillsRegistryProjection(strings.NewReader(registry), []int{id})
		if !ok {
			t.Fatal("synthetic canonical projection")
		}
		w := skillsWriter{Task: id, Job: fmt.Sprintf("20261028-203114-todo-%d", id), SpecSHA: strings.Repeat("a", 64), Workspace: skillsParentRoot}
		if id == 3686 {
			w.Workspace = filepath.Join(filepath.Dir(skillsParentRoot), "todo-3686-synthetic")
		}
		row := reg.Tasks[id]
		row.Spec = currentTodoSpecsRoot + fmt.Sprintf("/%d-", id) + strings.Repeat("a", 32) + ".md"
		row.Scheduler.Worktree = w.Workspace
		row.Scheduler.Branch = fmt.Sprintf("scheduler/todo-%d-synthetic", id)
		check := func(r skillsTask, writer skillsWriter) (bool, int) {
			reads := 0
			fs := skillsFS{blocked: func(string) bool { reads++; return true }}
			defer fs.close()
			reg.Tasks[id] = r
			return skillsActorCheck(writer, reg, &fs, w.Job), reads
		}
		if admitted, reads := check(row, w); admitted || reads != 1 {
			t.Fatal("valid row must reach the deliberately denied spec read")
		}
		for name, change := range map[string]func(*skillsTask){
			"project":        func(r *skillsTask) { r.Project = "other" },
			"status":         func(r *skillsTask) { r.Status = "done" },
			"current_job":    func(r *skillsTask) { r.Scheduler.Job = "other" },
			"history":        func(r *skillsTask) { r.Jobs = []string{"other"} },
			"latest":         func(r *skillsTask) { r.Jobs = append(append([]string{}, r.Jobs...), "other") },
			"worktree":       func(r *skillsTask) { r.Scheduler.Worktree += "-other" },
			"spec_path":      func(r *skillsTask) { r.Spec = "D:/synthetic/outside-spec.md" },
			"stop":           func(r *skillsTask) { r.OwnerStopped = true },
			"postponed":      func(r *skillsTask) { r.Postponed = true },
			"stop_kind":      func(r *skillsTask) { r.StopKind = "owner" },
			"question":       func(r *skillsTask) { r.OwnerQuestion = object{} },
			"decision":       func(r *skillsTask) { r.DecisionWait = object{} },
			"wait":           func(r *skillsTask) { r.WaitReady = object{} },
			"cancel":         func(r *skillsTask) { r.Scheduler.PendingCancel = object{} },
			"scheduler_stop": func(r *skillsTask) { r.Scheduler.OwnerStop = object{} },
		} {
			t.Run(fmt.Sprint(id)+"/"+name, func(t *testing.T) {
				r := row
				change(&r)
				if admitted, reads := check(r, w); admitted || reads != 0 {
					t.Fatal("canonical assignment bypassed the actor row gate")
				}
			})
		}
		wrong := w
		wrong.SpecSHA = "not-a-digest"
		if admitted, reads := check(row, wrong); admitted || reads != 0 {
			t.Fatal("invalid spec identity reached a read")
		}
	}
}

func TestSkills3080CanonicalAssignmentKeepsProcessAndSourceGates(t *testing.T) {
	h, err := syscall.OpenProcess(0x1000|0x100000, false, uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(h)
	var created, exit, kernel, user syscall.Filetime
	if syscall.GetProcessTimes(h, &created, &exit, &kernel, &user) != nil {
		t.Fatal("own process birth")
	}
	birth := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	job := object{"pid": json.Number(strconv.Itoa(os.Getpid())), "pid_created": json.Number(strconv.FormatUint(birth, 10))}
	if !skillsActorLive(job) {
		t.Fatal("same live process birth rejected")
	}
	wrong := objClone(job)
	wrong["pid_created"] = json.Number(strconv.FormatUint(birth+1, 10))
	if skillsActorLive(wrong) {
		t.Fatal("different birth admitted")
	}
	wrong = objClone(job)
	wrong["pid"] = json.Number("4294967295")
	if skillsActorLive(wrong) {
		t.Fatal("different PID admitted")
	}
	root, branch := official3387GitFixture(t, "AutoDevJapan/cc-pc-tools-rs")
	if !skillsActorSource(root, branch) || skillsActorSource(root, branch+"-other") {
		t.Fatal("actual source branch gate changed")
	}
	other, otherBranch := official3387GitFixture(t, "synthetic-owner/other")
	if skillsActorSource(other, otherBranch) {
		t.Fatal("different source admitted")
	}
}

func skillsAssignmentAdmitted(raw string, job object, id int) bool {
	registry := fmt.Sprintf(`{"projects":{"cc-pc-tools-rs":%q},"items":[%s]}`, roots[3], raw)
	r, ok := skillsRegistryProjection(strings.NewReader(registry), []int{id})
	return ok && skillsActorAssignmentBound(r.Tasks[id], job)
}

func TestSkills3080CanonicalWorkerAssignment(t *testing.T) {
	for _, id := range []int{3080, 3686} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			raw, job := skillsAssignmentFixture(id)
			if !skillsAssignmentAdmitted(raw, job, id) {
				t.Fatal("canonical start --job assignment rejected because its launch key was cleared")
			}
			// u64 is the scheduler's exact storage domain; no float conversion.
			if !skillsAssignmentAdmitted(strings.ReplaceAll(raw, ":7", ":18446744073709551615"), job, id) {
				t.Fatal("positive integer generation lost precision")
			}
			keyed := strings.Replace(raw, `"key":null`, `"key":"original-launch"`, 1)
			if !skillsAssignmentAdmitted(keyed, job, id) {
				t.Fatal("original nonempty exact-key assignment changed")
			}
			keyed = strings.Replace(keyed, `,"resume_generation":7,"worker_receipt":{"job":"`+job["id"].(string)+`","generation":7}`, "", 1)
			if !skillsAssignmentAdmitted(keyed, job, id) {
				t.Fatal("legacy exact-key assignment acquired a new generation requirement")
			}
		})
	}
}

func TestSkills3080CanonicalWorkerAssignmentRejectsAmbiguity(t *testing.T) {
	for _, id := range []int{3080, 3686} {
		raw, job := skillsAssignmentFixture(id)
		mutations := map[string]string{
			"missing_key":              strings.Replace(raw, `,"key":null`, "", 1),
			"empty_key":                strings.Replace(raw, `"key":null`, `"key":""`, 1),
			"wrong_key":                strings.Replace(raw, `"key":null`, `"key":"other"`, 1),
			"missing_generation":       strings.Replace(raw, `"resume_generation":7,`, "", 1),
			"missing_receipt":          strings.Replace(raw, `"worker_receipt":{"job":"`+job["id"].(string)+`","generation":7},`, "", 1),
			"different_receipt_job":    strings.Replace(raw, `"worker_receipt":{"job":"`+job["id"].(string)+`"`, `"worker_receipt":{"job":"20261028-000000-todo-3080"`, 1),
			"old_receipt_generation":   strings.Replace(raw, `"generation":7`, `"generation":6`, 1),
			"duplicate_key":            strings.Replace(raw, `"key":null`, `"key":null,"key":null`, 1),
			"key_case_alias":           strings.Replace(raw, `"key":null`, `"key":null,"Key":null`, 1),
			"key_alias_only":           strings.Replace(raw, `"key":null`, `"Key":null`, 1),
			"scheduler_alias":          strings.Replace(raw, `"scheduler":`, `"Scheduler":`, 1),
			"scheduler_job_alias":      strings.Replace(raw, `"scheduler":{"job":`, `"scheduler":{"Job":`, 1),
			"generation_alias":         strings.Replace(raw, `"resume_generation":`, `"Resume_Generation":`, 1),
			"receipt_alias":            strings.Replace(raw, `"worker_receipt":`, `"Worker_Receipt":`, 1),
			"receipt_job_alias":        strings.Replace(raw, `"worker_receipt":{"job":`, `"worker_receipt":{"Job":`, 1),
			"receipt_generation_alias": strings.Replace(raw, `"generation":`, `"Generation":`, 1),
			"duplicate_generation":     strings.Replace(raw, `"resume_generation":7`, `"resume_generation":7,"resume_generation":7`, 1),
			"duplicate_receipt_job":    strings.Replace(raw, `"worker_receipt":{"job":`, `"worker_receipt":{"job":"duplicate","job":`, 1),
			"status_alias":             strings.Replace(raw, `"status":"doing"`, `"status":"doing","Status":"doing"`, 1),
			"stop_alias":               strings.Replace(raw, `"status":"doing"`, `"status":"doing","owner_stopped":true,"Owner_Stopped":false`, 1),
			"wait_alias":               strings.Replace(raw, `"status":"doing"`, `"status":"doing","wait_ready":{},"Wait_Ready":null`, 1),
			"workspace_alias":          strings.Replace(raw, `"scheduler":{`, `"scheduler":{"Worktree":"other",`, 1),
			"branch_alias":             strings.Replace(raw, `"scheduler":{`, `"scheduler":{"Branch":"other",`, 1),
		}
		for _, bad := range []string{"0", "-1", "7.0", "7.5", "7e0", `"7"`, "null", "true", "[]", "{}", "18446744073709551616"} {
			mutations["generation_"+bad] = strings.ReplaceAll(raw, ":7", ":"+bad)
			mutations["receipt_generation_"+bad] = strings.Replace(raw, `"generation":7`, `"generation":`+bad, 1)
		}
		for name, changed := range mutations {
			t.Run(fmt.Sprint(id)+"/"+name, func(t *testing.T) {
				if skillsAssignmentAdmitted(changed, job, id) {
					t.Fatal("ambiguous or unbound canonical assignment admitted")
				}
			})
		}
		for _, key := range []string{"id", "todo_id"} {
			t.Run(fmt.Sprint(id)+"/job_"+key, func(t *testing.T) {
				changed := objClone(job)
				changed[key] = "other"
				if skillsAssignmentAdmitted(raw, changed, id) {
					t.Fatal("another job or task admitted")
				}
			})
		}
		for _, invalid := range []any{nil, "", json.Number("7"), false} {
			changed := objClone(job)
			changed["launch_key"] = invalid
			if skillsAssignmentAdmitted(raw, changed, id) {
				t.Fatal("missing, empty or non-string original launch key admitted")
			}
		}
		// A case alias must not turn a bad keyed assignment into a good one.
		keyedAlias := strings.Replace(raw, `"key":null`, `"key":"other","Key":"original-launch"`, 1)
		if skillsAssignmentAdmitted(keyedAlias, job, id) {
			t.Fatal("case-aliased launch key admitted")
		}
	}
}
