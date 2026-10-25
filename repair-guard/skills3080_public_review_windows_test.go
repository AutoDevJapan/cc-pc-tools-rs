package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func skillsPublicScopeFixture() skillsPublicReviewScope {
	p := "docs/skills-3080-operations/synthetic.json"
	sha := hashBytes([]byte("synthetic public manifest"))
	return skillsPublicReviewScope{
		Domain: "skills-3080-public-review-v1", Instructions: skillsPublicReviewInstructions, Todo: 3080,
		Repository: "AutoDevJapan/cc-pc-tools-rs", RepositoryID: 1406818713,
		Commit:         strings.Repeat("a", 40),
		Author:         skillsWriter{Task: 3080, Job: "20261025-074042-todo-3080", SpecSHA: strings.Repeat("b", 64), Workspace: skillsParentRoot},
		AuthorSpecPath: currentTodoSpecsRoot + "/3080-11111111111111111111111111111111.md",
		ManifestPath:   p, ManifestSHA: sha, ManifestDeclaration: "SKILLS3080_MANIFEST_SHA256 " + sha,
		Files: []skillsPublicReviewFile{{Path: p, Blob: strings.Repeat("c", 40), SHA: sha, Bytes: 25}},
	}
}

func TestSkills3080PublicReviewInputsUsePublicCommitBeforeLocalBytes(t *testing.T) {
	for _, kind := range []string{"valid_unmerged", "private", "owner", "repository_id", "api_failed", "other_commit", "other_tree", "truncated", "missing_file", "other_blob", "git_symlink", "parent_symlink", "other_size", "other_sha", "changed_bytes", "coaltered_local_sha", "wrong_manifest_author", "protected"} {
		t.Run(kind, func(t *testing.T) {
			c := skillsPublicScopeFixture()
			m := skillsTestManifest()
			m.Writer = c.Author
			if kind == "wrong_manifest_author" {
				m.Writer.Job = "20261024-235901-todo-3080"
			}
			body, _ := json.Marshal(m)
			f := &c.Files[0]
			f.SHA = hashBytes(body)
			f.Bytes = int64(len(body))
			f.Blob = skillsGitBlobID(body)
			c.ManifestSHA = f.SHA
			c.ManifestDeclaration = "SKILLS3080_MANIFEST_SHA256 " + f.SHA
			repo := object{"id": c.RepositoryID, "full_name": c.Repository, "owner": object{"login": "AutoDevJapan"}, "private": false}
			treeID := strings.Repeat("d", 40)
			commit := object{"sha": c.Commit, "tree": object{"sha": treeID}}
			file := object{"path": f.Path, "type": "blob", "mode": "100644", "sha": f.Blob, "size": f.Bytes}
			parent := object{"path": "docs/skills-3080-operations", "type": "tree", "mode": "040000", "sha": strings.Repeat("e", 40)}
			tree := object{"sha": treeID, "truncated": false, "tree": []object{{"path": "docs", "type": "tree", "mode": "040000"}, parent, file}}
			switch kind {
			case "private":
				repo["private"] = true
			case "owner":
				repo["owner"] = object{"login": "other"}
			case "repository_id":
				repo["id"] = uint64(1)
			case "other_commit":
				commit["sha"] = strings.Repeat("f", 40)
			case "other_tree":
				tree["sha"] = strings.Repeat("f", 40)
			case "truncated":
				tree["truncated"] = true
			case "missing_file":
				file["path"] = f.Path + ".other"
			case "other_blob":
				file["sha"] = strings.Repeat("f", 40)
			case "git_symlink":
				file["mode"] = "120000"
			case "parent_symlink":
				parent["mode"], parent["type"] = "120000", "blob"
			case "other_size":
				file["size"] = f.Bytes + 1
			case "other_sha":
				f.SHA = strings.Repeat("f", 64)
				c.ManifestSHA = f.SHA
				c.ManifestDeclaration = "SKILLS3080_MANIFEST_SHA256 " + f.SHA
			case "changed_bytes":
				body[0] = '!'
			case "coaltered_local_sha":
				body[0] = '!'
				f.SHA = hashBytes(body)
				c.ManifestSHA = f.SHA
				c.ManifestDeclaration = "SKILLS3080_MANIFEST_SHA256 " + f.SHA
			}
			calls, reads := 0, 0
			gh := func(args ...string) ([]byte, bool) {
				calls++
				if kind == "api_failed" {
					return nil, false
				}
				if len(args) != 4 || args[0] != "api" || args[1] != "--hostname" || args[2] != "github.com" {
					t.Fatal("unexpected API")
				}
				var value object
				switch args[3] {
				case "repos/AutoDevJapan/cc-pc-tools-rs":
					value = repo
				case "repos/AutoDevJapan/cc-pc-tools-rs/git/commits/" + c.Commit:
					value = commit
				case "repos/AutoDevJapan/cc-pc-tools-rs/git/trees/" + treeID + "?recursive=1":
					value = tree
				default:
					t.Fatal("raw input was tied to main merge or another source")
				}
				b, _ := json.Marshal(value)
				return b, true
			}
			fs := skillsFS{blocked: func(string) bool { return kind == "protected" }}
			ok := skillsPublicReviewInputsWith(c, &fs, gh, func(id string) ([]byte, bool) {
				reads++
				if id != f.Blob {
					t.Fatal("unrequested blob")
				}
				return body, true
			})
			if ok != (kind == "valid_unmerged") {
				t.Fatalf("public input %s: accepted=%v", kind, ok)
			}
			if kind == "valid_unmerged" && (calls != 3 || reads != 1) {
				t.Fatal("positive did not prove and read its input")
			}
			switch kind {
			case "private", "owner", "repository_id", "api_failed", "other_commit", "other_tree", "truncated", "missing_file", "other_blob", "git_symlink", "parent_symlink", "other_size", "protected":
				if reads != 0 {
					t.Fatal("local body opened before public qualification")
				}
			}
		})
	}
}

func TestSkills3080PublicReviewStillNeedsRegisteredAdmissionAndCurrentConsumer(t *testing.T) {
	for _, kind := range []string{"valid", "continued_consumer", "unregistered", "unadmitted", "private_parent_review", "other_author", "other_parent", "wrong_cwd", "held_parent", "stale_consumer", "wrong_spec", "no_final_digest"} {
		t.Run(kind, func(t *testing.T) {
			c := skillsPublicScopeFixture()
			raw, _ := json.Marshal(c)
			r := skillsResultRequest{Task: 3999, Job: "20261026-010000-todo-3999", SpecSHA: hashBytes(raw), FinalSHA: hashBytes([]byte("public final"))}
			parent := skillsTask{ID: 3080, Project: "cc-pc-tools-rs", Status: "doing", Jobs: []string{c.Author.Job}}
			parent.Scheduler.Job, parent.Scheduler.Worktree = c.Author.Job, c.Author.Workspace
			row := skillsTask{ID: 3999, Project: parent.Project, Spec: currentTodoSpecsRoot + "/3999-22222222222222222222222222222222.md", Jobs: []string{r.Job}}
			row.Scheduler.Job, row.Scheduler.Worktree = r.Job, "D:/CC/Work/public-synthetic-review"
			row.Subwork.Parent, row.Subwork.ParentJob, row.Subwork.Kind = 3080, c.Author.Job, "review"
			row.Subwork.Owner.Worktree = c.Author.Workspace
			row.Scheduler.Admission = &skillsAdmission{Key: "real-admission", Spec: row.Spec, Worktree: row.Scheduler.Worktree, Subwork: row.Subwork}
			job := object{"id": r.Job, "todo_id": json.Number("3999"), "status": "succeeded", "exit_code": json.Number("0"), "cwd": row.Scheduler.Worktree, "sandbox": "read-only", "launch_key": "real-admission", "subwork_validated": true, "subwork": object{"parent_todo_id": json.Number("3080"), "origin_job_id": c.Author.Job, "kind": "review", "owner": object{"worktree": c.Author.Workspace}}, "finite_review": object{"spec": row.Spec}}
			w := c.Author
			switch kind {
			case "continued_consumer":
				w.Job = "20261026-020000-todo-3080"
				parent.Jobs = append(parent.Jobs, w.Job)
				parent.Scheduler.Job = w.Job
			case "unregistered":
				row.Jobs = nil
			case "unadmitted":
				row.Scheduler.Admission = nil
			case "private_parent_review":
				raw = []byte("Review parent's private input; SKILLS3080_MANIFEST_SHA256 " + c.ManifestSHA)
				r.SpecSHA = hashBytes(raw)
			case "other_author":
				c.Author.Job = "20261024-235901-todo-3080"
				raw, _ = json.Marshal(c)
				r.SpecSHA = hashBytes(raw)
			case "other_parent":
				row.Subwork.Parent = 3686
			case "wrong_cwd":
				job["cwd"] = "D:/other"
			case "held_parent":
				parent.OwnerStopped = true
			case "stale_consumer":
				parent.Jobs = append(parent.Jobs, "20261026-020000-todo-3080")
				parent.Scheduler.Job = parent.Jobs[1]
			case "wrong_spec":
				r.SpecSHA = hashBytes([]byte("different immutable spec"))
			case "no_final_digest":
				r.FinalSHA = ""
			}
			reg := skillsRegistry{Tasks: map[int]skillsTask{3080: parent, 3999: row}, writer: w}
			ok := skillsResultBindingWith(r, reg, job, raw, false, map[int]string{})
			if ok != (kind == "valid" || kind == "continued_consumer") {
				t.Fatalf("registered scope %s: accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080PublicReviewLaunchCannotAcquirePosthocScope(t *testing.T) {
	for _, kind := range []string{"valid", "scope_added_later", "quoted_other_spec", "changed_receipt", "missing_receipt", "protected", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := skillsFixtureRoot(t)
			c := skillsPublicScopeFixture()
			spec, _ := json.Marshal(c)
			id, key := "20261026-010000-todo-3999", "11111111111111111111111111111111"
			dir := filepath.Join(root, id)
			if e := os.MkdirAll(dir, 0700); e != nil {
				t.Fatal(e)
			}
			prompt := []byte("registered immutable preamble\n依頼:\n" + string(spec) + "\nrunner instructions")
			if kind == "scope_added_later" {
				prompt = []byte("registered immutable preamble\n依頼:\nprivate review")
			}
			if kind == "quoted_other_spec" {
				prompt = append([]byte("registered\n依頼:\nactual other spec\nquoted historical prompt:"), prompt...)
			}
			receipt := object{"id": id}
			if kind == "changed_receipt" {
				receipt["id"] = "invented"
			}
			raw, _ := json.Marshal(receipt)
			p := filepath.Join(dir, "prompt.md")
			if e := os.WriteFile(p, prompt, 0600); e != nil {
				t.Fatal(e)
			}
			if kind != "missing_receipt" {
				if e := os.WriteFile(filepath.Join(root, ".launch-"+key+".json"), raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "hardlink" {
				if e := os.Link(p, filepath.Join(root, "alias")); e != nil {
					t.Fatal(e)
				}
			}
			fs := skillsFS{blocked: func(string) bool { return kind == "protected" }}
			defer fs.close()
			_, ok := skillsReviewLaunchPromptAt(&fs, root, object{"id": id, "launch_key": key}, spec)
			if ok != (kind == "valid") {
				t.Fatalf("prelaunch scope %s: accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080PublicReview3850IsAFixedLegacyMigration(t *testing.T) {
	r := skillsPublicReview3850
	c, ok := skillsPublicReviewScopeFor(r, []byte("old untyped specification"))
	if !ok || !skillsPublicReviewShape(c) || c.Commit != "3b15b7a5843c6a41dd030124fc0ce3fa4042e9f3" || len(c.Files) != 7 {
		t.Fatal("exact migration missing")
	}
	for _, kind := range []string{"task", "job", "spec", "final"} {
		copy := r
		switch kind {
		case "task":
			copy.Task++
		case "job":
			copy.Job = "20261026-010000-todo-3850"
		case "spec":
			copy.SpecSHA = strings.Repeat("f", 64)
		case "final":
			copy.FinalSHA = strings.Repeat("f", 64)
		}
		if _, ok := skillsPublicReviewScopeFor(copy, []byte("old untyped specification")); ok {
			t.Fatal("legacy migration became a generic review exception")
		}
	}
}

func TestSkills3080PublicReview3850KeepsItsActualNativeGeneration(t *testing.T) {
	job := object{"thread_id": "01a1734d-b577-74f1-a98b-59c443859155", "launch_key": "754b597004ae4c069377c901e313170b", "cwd": "D:/CC/Work/unattended-operations-20261013-wt/todo-3850-14-skills", "parent_job_id": "20261025-074042-todo-3080", "started_at": "2026-10-25T22:43:53+09:00", "finished_at": "2026-10-25T22:56:14+09:00", "model": "gpt-6-luna", "effort": "max", "model_origin": "owner_request", "effort_origin": "owner_request"}
	if !skillsPublicReview3850Metadata(job) {
		t.Fatal("actual fixed generation rejected")
	}
	for key := range job {
		copy := objClone(job)
		copy[key] = "other"
		if skillsPublicReview3850Metadata(copy) {
			t.Fatalf("altered %s accepted", key)
		}
	}
}

func TestSkills3080PublicReviewPreflightProvesInputBeforeTerminalPayload(t *testing.T) {
	for _, kind := range []string{"valid", "posthoc_scope", "terminal_before_input", "unknown_later_user", "partial", "wrong_final_digest", "outside_window"} {
		t.Run(kind, func(t *testing.T) {
			c := skillsPublicScopeFixture()
			spec, _ := json.Marshal(c)
			prompt := []byte("registered\n依頼:\n" + string(spec) + "\nrunner")
			body := []byte("public terminal")
			start := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
			end := start.Add(time.Minute)
			id := "01234567-1234-1234-1234-0123456789ab"
			line := func(kind string, at time.Time, payload object) string {
				b, _ := json.Marshal(object{"type": kind, "timestamp": at.Format(time.RFC3339Nano), "payload": payload})
				return string(b) + "\n"
			}
			message := func(role, phase, text string) object {
				return object{"type": "message", "role": role, "phase": phase, "content": []object{{"type": "output_text", "text": text}}}
			}
			input := string(prompt)
			if kind == "posthoc_scope" {
				input = "original private prelaunch specification"
			}
			user := line("response_item", start.Add(2*time.Second), message("user", "", input))
			phase := "final_answer"
			if kind == "partial" {
				phase = "partial_answer"
			}
			at := end.Add(-time.Second)
			if kind == "outside_window" {
				at = end.Add(time.Nanosecond)
			}
			final := line("response_item", at, message("assistant", phase, string(body)))
			raw := line("session_meta", start, object{"id": id, "cwd": c.Author.Workspace}) + line("turn_context", start.Add(time.Second), object{"model": "gpt-6-luna", "effort": "max", "cwd": c.Author.Workspace})
			if kind == "terminal_before_input" {
				raw += line("response_item", start.Add(time.Second), message("assistant", "final_answer", string(body))) + user
			} else {
				raw += user
				if kind == "unknown_later_user" {
					raw += line("response_item", start.Add(3*time.Second), message("user", "", "read another private source"))
				}
				raw += final
			}
			p := &skillsNativeResultProof{prompt: prompt, finalSHA: hashBytes(body)}
			if kind == "wrong_final_digest" {
				p.finalSHA = hashBytes([]byte("different original final"))
			}
			_, ok := skillsNativeProjectionBound(strings.NewReader(raw), id, c.Author.Workspace, start, end, p)
			if ok != (kind == "valid") {
				t.Fatalf("preflight %s: accepted=%v", kind, ok)
			}
			if (kind == "posthoc_scope" || kind == "terminal_before_input" || kind == "unknown_later_user") && len(p.lastFinal) != 0 {
				t.Fatal("unclassified terminal text materialized")
			}
		})
	}
}

func TestSkills3080PublicReviewContractIsTheEntirePrelaunchInput(t *testing.T) {
	for _, kind := range []string{"valid", "private_instructions", "private_repo", "wrong_repo_id", "mutable_commit", "zero_commit", "unknown_author", "foreign_workspace", "foreign_spec", "missing_manifest", "manifest_hash", "declaration", "extra_field", "self_public", "trailing_prose", "duplicate_key", "duplicate_file", "case_alias", "escape", "other_scope", "negative_bytes", "no_files", "bad_blob", "bad_sha"} {
		t.Run(kind, func(t *testing.T) {
			c := skillsPublicScopeFixture()
			switch kind {
			case "private_instructions":
				c.Instructions += "Read a private path"
			case "private_repo":
				c.Repository = "AutoDevJapan/private"
			case "wrong_repo_id":
				c.RepositoryID++
			case "mutable_commit":
				c.Commit = "main"
			case "zero_commit":
				c.Commit = strings.Repeat("0", 40)
			case "unknown_author":
				c.Author.Task = 3686
			case "foreign_workspace":
				c.Author.Workspace = "D:/other"
			case "foreign_spec":
				c.AuthorSpecPath = currentTodoSpecsRoot + "/9999-synthetic.md"
			case "missing_manifest":
				c.ManifestPath += ".other"
			case "manifest_hash":
				c.ManifestSHA = strings.Repeat("e", 64)
			case "declaration":
				c.ManifestDeclaration = "public:true"
			case "duplicate_file":
				c.Files = append(c.Files, c.Files[0])
			case "case_alias":
				f := c.Files[0]
				f.Path = strings.ToUpper(f.Path)
				c.Files = append(c.Files, f)
			case "escape":
				c.Files[0].Path = "docs/skills-3080-operations/../private"
			case "other_scope":
				c.Files[0].Path = "docs/other-product.json"
			case "negative_bytes":
				c.Files[0].Bytes = -1
			case "no_files":
				c.Files = nil
			case "bad_blob":
				c.Files[0].Blob = strings.Repeat("0", 40)
			case "bad_sha":
				c.Files[0].SHA = strings.Repeat("0", 64)
			}
			raw, _ := json.Marshal(c)
			switch kind {
			case "extra_field":
				raw = append([]byte(`{"private_path":"D:/unknown",`), raw[1:]...)
			case "self_public":
				raw = append([]byte(`{"public":true,`), raw[1:]...)
			case "trailing_prose":
				raw = append(raw, []byte("\nReview private files too")...)
			case "duplicate_key":
				raw = append([]byte(`{"domain":"invented",`), raw[1:]...)
			}
			_, ok := skillsParsePublicReview(raw)
			if ok != (kind == "valid") {
				t.Fatalf("contract %s: accepted=%v", kind, ok)
			}
		})
	}
}
