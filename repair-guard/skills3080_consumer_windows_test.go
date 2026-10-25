package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func skillsConsumerFixture() (skillsConsumerScope, skillsWriter, skillsRegistry, object) {
	c := skillsConsumerScope{Domain: "skills-3080-consumer-v1", Instructions: skillsConsumerInstructions, Author: skillsPublicScopeFixture().Author,
		AuthorSpecPath: currentTodoSpecsRoot + "/3080-11111111111111111111111111111111.md", CreatorSpecPath: currentTodoSpecsRoot + "/3080-11111111111111111111111111111111.md",
		ManifestCommit: strings.Repeat("a", 40), ManifestPath: "docs/skills-3080-operations/synthetic.json", ManifestSHA: strings.Repeat("b", 64),
		Review: skillsResultRequest{Task: 3998, Job: "20261025-120000-todo-3998", SpecSHA: strings.Repeat("c", 64), FinalSHA: strings.Repeat("d", 64)}}
	c.Consume = skillsConsumeRequest{Creator: c.Author, RequestPath: skillsParentRoot + "/.ci-work/synthetic-create.json", RequestSHA: strings.Repeat("e", 64), CallID: "call_synthetic", Indices: []int{0, 1}}
	w := skillsWriter{Task: 3999, Job: "20261026-010000-todo-3999", Workspace: "D:/CC/Work/synthetic-readonly-consumer", SpecSHA: strings.Repeat("f", 64)}
	parent := skillsTask{ID: 3080, Project: "cc-pc-tools-rs", Status: "doing", Jobs: []string{c.Author.Job}}
	parent.Scheduler.Job, parent.Scheduler.Worktree = c.Author.Job, c.Author.Workspace
	row := skillsTask{ID: w.Task, Project: parent.Project, Status: "doing", Spec: currentTodoSpecsRoot + "/3999-22222222222222222222222222222222.md", Jobs: []string{w.Job}}
	row.Scheduler.Job, row.Scheduler.Worktree, row.Scheduler.Branch = w.Job, w.Workspace, "scheduler/todo-3999-consumer"
	row.Subwork.Parent, row.Subwork.ParentJob, row.Subwork.Kind = 3080, c.Author.Job, "review"
	row.Subwork.Owner.Worktree = c.Author.Workspace
	row.Scheduler.Admission = &skillsAdmission{Key: "actual-admission", Spec: row.Spec, Worktree: w.Workspace, Subwork: row.Subwork}
	job := object{"id": w.Job, "todo_id": json.Number("3999"), "status": "running", "sandbox": "read-only", "cwd": w.Workspace, "launch_key": "actual-admission", "subwork_validated": true,
		"subwork": object{"parent_todo_id": json.Number("3080"), "origin_job_id": c.Author.Job, "kind": "review", "owner": object{"worktree": c.Author.Workspace}}, "finite_review": object{"spec": row.Spec}, "parent_job_id": c.Author.Job}
	return c, w, skillsRegistry{Tasks: map[int]skillsTask{3080: parent, 3999: row}}, job
}

func TestSkills3080LiveConsumerNativeInputPrecedesUse(t *testing.T) {
	for _, kind := range []string{"valid_without_final", "missing_input", "wrong_input", "posthoc_scope", "later_user", "before_start", "after_observation", "wrong_cwd", "prefixed_prompt", "suffixed_prompt"} {
		t.Run(kind, func(t *testing.T) {
			start := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
			end := start.Add(time.Minute)
			id := "11111111-1111-7111-8111-111111111111"
			cwd := "D:/CC/Work/synthetic"
			prompt := "actual registered launch\n依頼:\ntyped consumer"
			input := prompt
			at := start.Add(time.Second)
			if kind == "wrong_input" {
				input = "private input"
			}
			if kind == "prefixed_prompt" {
				input = "private instruction\n" + prompt
			}
			if kind == "suffixed_prompt" {
				input = prompt + "\nprivate instruction"
			}
			if kind == "before_start" {
				at = start.Add(-time.Second)
			}
			if kind == "after_observation" {
				at = end.Add(time.Second)
			}
			metaCwd := cwd
			if kind == "wrong_cwd" {
				metaCwd = "D:/unrelated"
			}
			records := []object{{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": metaCwd}}, {"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"model": "gpt-6-luna", "effort": "max", "cwd": cwd}}}
			user := func(text string, when time.Time) object {
				return object{"type": "response_item", "timestamp": when.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": text}}}}
			}
			if kind == "posthoc_scope" {
				records = append(records, user("original\n依頼:\nprivate scope", start.Add(time.Millisecond)))
			}
			if kind != "missing_input" {
				records = append(records, user(input, at))
			}
			if kind == "later_user" {
				records = append(records, user("extra private instruction", start.Add(2*time.Second)))
			}
			// A live input proof must not decode or need an assistant result.
			records = append(records, object{"type": "response_item", "timestamp": start.Add(3 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "assistant", "phase": "final_answer", "content": object{"not": "a message body"}}})
			var stream bytes.Buffer
			for _, r := range records {
				b, _ := json.Marshal(r)
				stream.Write(b)
				stream.WriteByte('\n')
			}
			p := &skillsNativeResultProof{prompt: []byte(prompt), inputOnly: true, exactInput: true}
			_, ok := skillsNativeProjectionBound(&stream, id, cwd, start, end, p)
			if ok != (kind == "valid_without_final") {
				t.Fatalf("live native %s accepted=%v", kind, ok)
			}
			if len(p.lastFinal) != 0 || !p.finalAt.IsZero() {
				t.Fatal("live input proof materialized assistant body")
			}
		})
	}
}

func TestSkills3080FreshUsesActualHeldBytesAfterProducerUpdate(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "accepted_old_producer", false: "unknown_producer"}[valid], func(t *testing.T) {
			m, _, e, root := skillsForwardFixture(t)
			e.Producer = skillsProducer{"29118cd151cc9ccec8bb6a371382e2a3cc2507d5b36e3837f6cc87793b5b21fb", "e1a4c21dc6241c140d4d7364d34f42b16d7005be"}
			if !valid {
				e.Producer.SHA = strings.Repeat("c", 64)
			}
			leaf := filepath.Join(root, filepath.FromSlash(m.Files[0].Target))
			touches := 0
			fs := skillsFS{blocked: func(p string) bool {
				if same(p, leaf) {
					touches++
				}
				return false
			}}
			defer fs.close()
			result, reason := skillsConsumeAcceptedCurrent(m, e, root, &fs, []int{0}, skillsProducer{strings.Repeat("a", 64), strings.Repeat("b", 40)})
			if !valid {
				if reason != "creation_evidence" || result != nil || touches != 0 {
					t.Fatal("unknown producer opened output")
				}
				return
			}
			if reason != "" || touches == 0 || result["same_read_buffer_consumed"] != true || result["historical_physical_continuity_proven"] != false || result["current_os_writer"] != "unproved" {
				t.Fatal("fresh output was not the held current buffer")
			}
			if result["files"].([]object)[0]["content_utf8"] != string(m.Files[0].body) {
				t.Fatal("manifest copy replaced actual read")
			}
		})
	}
}

func TestSkills3080FreshConsumerNeedsActualReadonlyAdmission(t *testing.T) {
	for _, kind := range []string{"valid", "creator_finished", "continued_parent", "unregistered", "unadmitted", "writer_impersonated", "write_enabled", "wrong_parent", "wrong_launch_parent", "wrong_owner", "wrong_cwd", "stale_consumer", "held_parent", "held_child", "other_project", "changed_admission"} {
		t.Run(kind, func(t *testing.T) {
			c, w, reg, job := skillsConsumerFixture()
			row, parent := reg.Tasks[w.Task], reg.Tasks[3080]
			switch kind {
			case "creator_finished":
				parent.Status = "done"
			case "continued_parent":
				parent.Jobs = append(parent.Jobs, "20261026-020000-todo-3080")
				parent.Scheduler.Job = parent.Jobs[1]
			case "unregistered":
				row.Jobs = nil
			case "unadmitted":
				row.Scheduler.Admission = nil
			case "writer_impersonated":
				w.Task = 3080
				w.Job = c.Author.Job
				w.Workspace = c.Author.Workspace
			case "write_enabled":
				job["sandbox"] = "danger-full-access"
			case "wrong_parent":
				row.Subwork.Parent = 3686
			case "wrong_launch_parent":
				job["parent_job_id"] = "20261024-020000-todo-3080"
			case "wrong_owner":
				row.Subwork.Owner.Worktree = "D:/unrelated"
			case "wrong_cwd":
				job["cwd"] = "D:/unrelated"
			case "stale_consumer":
				row.Jobs = append(row.Jobs, "20261026-030000-todo-3999")
				row.Scheduler.Job = row.Jobs[1]
			case "held_parent":
				parent.OwnerStopped = true
			case "held_child":
				row.OwnerStopped = true
			case "other_project":
				row.Project = "other"
			case "changed_admission":
				row.Scheduler.Admission.Key = "invented"
			}
			reg.Tasks[row.ID], reg.Tasks[3080] = row, parent
			ok := skillsConsumerActorBound(w, reg, job, c)
			if ok != (kind == "valid" || kind == "creator_finished" || kind == "continued_parent") {
				t.Fatalf("fresh consumer %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080FreshConsumerContractIsExact(t *testing.T) {
	for _, kind := range []string{"valid", "arbitrary_prose", "caller_job", "private_instruction", "unknown_field", "snapshot_escape", "other_creator", "other_manifest", "duplicate_index", "negative_index", "no_final", "posthoc_prose"} {
		t.Run(kind, func(t *testing.T) {
			c, _, _, _ := skillsConsumerFixture()
			switch kind {
			case "private_instruction":
				c.Instructions += " read private input"
			case "snapshot_escape":
				c.CreatorSpecPath = "D:/private.json"
			case "other_creator":
				c.Consume.Creator.Task = 3999
			case "other_manifest":
				c.ManifestPath = "docs/another.json"
			case "duplicate_index":
				c.Consume.Indices = []int{0, 0}
			case "negative_index":
				c.Consume.Indices = []int{-1}
			case "no_final":
				c.Review.FinalSHA = ""
			}
			raw, _ := json.Marshal(c)
			if kind == "unknown_field" || kind == "caller_job" {
				var v object
				json.Unmarshal(raw, &v)
				v[kind] = true
				raw, _ = json.Marshal(v)
			}
			if kind == "arbitrary_prose" {
				raw = []byte("public consumer true")
			}
			if kind == "posthoc_prose" {
				raw = append(raw, []byte("\nother instructions")...)
			}
			_, ok := skillsParseConsumer(raw)
			if ok != (kind == "valid") {
				t.Fatalf("consumer spec %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080FreshProducerAcceptsProvenOldRuntimeOnly(t *testing.T) {
	current := skillsProducer{strings.Repeat("a", 64), strings.Repeat("b", 40)}
	old := skillsProducer{"29118cd151cc9ccec8bb6a371382e2a3cc2507d5b36e3837f6cc87793b5b21fb", "e1a4c21dc6241c140d4d7364d34f42b16d7005be"}
	if !skillsAcceptedProducer(old, current) || !skillsAcceptedProducer(current, current) {
		t.Fatal("accepted historical or current producer rejected")
	}
	for _, bad := range []skillsProducer{{strings.Repeat("c", 64), old.Revision}, {old.SHA, strings.Repeat("d", 40)}, {"", old.Revision}, {strings.Repeat("0", 64), old.Revision}} {
		if skillsAcceptedProducer(bad, current) {
			t.Fatal("receipt self declaration admitted unknown producer")
		}
	}
}

func TestSkills3080FreshConsumerCLIHasNoCallerSuppliedRequest(t *testing.T) {
	for _, args := range [][]string{{"--consume-skills3080-admitted"}, {"--consume-skills3080-admitted", "D:/request.json"}, {"--consume-skills3080-admitted", "--consume-skills3080"}} {
		calls := 0
		var out bytes.Buffer
		code := runSkillsCLI(args, &out, func(op, path string) object {
			calls++
			if op != "consume-skills3080-admitted" || path != "" {
				t.Fatal("invented request path")
			}
			return object{"qualified": true}
		})
		if len(args) == 1 {
			if code != 0 || calls != 1 || !skillsFlag(args) {
				t.Fatal("registered caller's own spec did not select operation")
			}
		} else if code != 1 || calls != 0 {
			t.Fatal("fresh caller accepted arbitrary request")
		}
	}
}
