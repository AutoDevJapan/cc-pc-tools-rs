package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSkills3080SourceSpecIssueChangesOnlyCorrelation(t *testing.T) {
	for _, consumer := range []bool{false, true} {
		var original []byte
		if consumer {
			c, _, _, _ := skillsConsumerFixture()
			original, _ = json.Marshal(c)
		} else {
			original, _ = json.Marshal(skillsPublicScopeFixture())
		}
		nonce := strings.Repeat("a1", 16)
		prepared, ok := skillsRuntimeSourceSpec(original, nonce)
		if !ok {
			t.Fatal("known registered public scope")
		}
		var a, b object
		json.Unmarshal(original, &a)
		json.Unmarshal(prepared, &b)
		if b["runtime_source_nonce"] != nonce {
			t.Fatal("controller correlation was not applied")
		}
		delete(b, "runtime_source_nonce")
		before, _ := json.Marshal(a)
		after, _ := json.Marshal(b)
		if !bytes.Equal(before, after) {
			t.Fatal("source issue changed author, creator, public inputs or instructions")
		}
		if got, ok := skillsRuntimeNonceForSpec(prepared); !ok || got != nonce {
			t.Fatal("prepared scope did not retain its exact correlation")
		}
	}
	if _, ok := skillsRuntimeSourceSpec([]byte(`{"domain":"private-scope"}`), strings.Repeat("a1", 16)); ok {
		t.Fatal("unknown scope gained a source context")
	}
	c := skillsPublicScopeFixture()
	c.Domain, c.Instructions = "skills-3080-public-repair-v1", skillsPublicRepairInstructions
	raw, _ := json.Marshal(c)
	if _, ok := skillsRuntimeSourceSpec(raw, strings.Repeat("a1", 16)); ok {
		t.Fatal("source correlation expanded repair scope")
	}
}

func TestSkills3080SourceTargetRetainsNormalTaskAndParentControls(t *testing.T) {
	for _, kind := range []string{"terminal", "new_task", "repair_actor", "wrong_task", "wrong_project", "wrong_parent", "wrong_kind", "wrong_workspace", "held_child", "held_parent", "wrong_previous", "wrong_spec_root", "wrong_spec_id", "owner_wait", "unregistered_origin", "wrong_spec_sha"} {
		t.Run(kind, func(t *testing.T) {
			c, _, reg, _ := skillsConsumerFixture()
			row := reg.Tasks[3999]
			row.Status = "done"
			r := skillsRequest{Writer: c.Author, RuntimeSource: &skillsRuntimeSourceRequest{Task: row.ID, PreviousJob: row.Scheduler.Job, SpecSHA: strings.Repeat("a", 64)}}
			parent := reg.Tasks[3080]
			switch kind {
			case "new_task":
				row.Status, row.Scheduler.Job, row.Scheduler.LastJob, row.Jobs = "todo", "", "", nil
				r.RuntimeSource.PreviousJob = ""
			case "repair_actor":
				r.Writer.Task = 3686
			case "wrong_task":
				r.RuntimeSource.Task = 3080
			case "wrong_project":
				row.Project = "different-project"
			case "wrong_parent":
				row.Subwork.Parent = 1
			case "wrong_kind":
				row.Subwork.Kind = "implementation"
			case "wrong_workspace":
				row.Subwork.Owner.Worktree = "D:/different"
			case "held_child":
				row.Status = "blocked"
			case "held_parent":
				parent.Status = "blocked"
			case "wrong_previous":
				r.RuntimeSource.PreviousJob = "20300101-000000-todo-3999"
			case "wrong_spec_root":
				row.Spec = "D:/outside/3999-22222222222222222222222222222222.md"
			case "wrong_spec_id":
				row.Spec = currentTodoSpecsRoot + "/4000-22222222222222222222222222222222.md"
			case "owner_wait":
				row.Status = "owner"
			case "unregistered_origin":
				row.Subwork.ParentJob = "20300101-000000-todo-3080"
			case "wrong_spec_sha":
				r.RuntimeSource.SpecSHA = "not a digest"
			}
			reg.Tasks[3999], reg.Tasks[3080] = row, parent
			_, stage := skillsRuntimeSourceTargetState(r, reg)
			if (stage == "") != (kind == "terminal" || kind == "new_task") {
				t.Fatalf("normal source target %s accepted=%v stage=%s", kind, stage == "", stage)
			}
		})
	}
}

func TestSkills3080RuntimeSourceCLIIsPurposeScopedAndReportsOwnedHold(t *testing.T) {
	for _, flag := range []string{"render-skills3080-runtime-source", "prepare-skills3080-runtime-source", "start-skills3080-runtime-source"} {
		for _, valid := range []bool{false, true} {
			args := []string{"--" + flag, "D:/synthetic/request.json"}
			if !valid {
				args = append(args, "--guard-path", "D:/unknown")
			}
			calls := 0
			var out bytes.Buffer
			code := runSkillsCLI(args, &out, func(op, path string) object {
				calls++
				return object{"qualified": true}
			})
			if valid && (code != 0 || calls != 1 || !skillsFlag(args)) {
				t.Fatal("source purpose was not dispatched once")
			}
			if !valid && (code != 1 || calls != 0) {
				t.Fatal("source purpose mixed with general classifier")
			}
		}
	}
	for _, owned := range []string{strings.Repeat("a1", 16), "D:/private/path"} {
		var out bytes.Buffer
		code := runSkillsCLI([]string{"--prepare-skills3080-runtime-source", "D:/synthetic/request.json"}, &out, func(string, string) object {
			return object{"qualified": false, "reason": "source", "failure_stage": "source_prepare_write", "ownership_id": owned, "partial_preparation_possible": true, "source_owned_root": "D:/private/path", "body": "private data"}
		})
		var value object
		json.Unmarshal(out.Bytes(), &value)
		if code != 1 || strings.Contains(out.String(), "private") || value["failure_stage"] != "source_prepare_write" {
			t.Fatal("source refusal leaked a pathname or lost its fixed stage")
		}
		if skillsRuntimeNonce(owned) != (value["ownership_id"] == owned && value["partial_preparation_possible"] == true) {
			t.Fatal("source hold did not retain only its validated ownership identifier")
		}
	}
}
