//go:build windows

package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCompletedBuildGenerationUsesTheSameCASACLAndNoReplace(t *testing.T) {
	l := fixture(t)
	p, e := prepareAt(l, "todo3942")
	if e != nil {
		t.Fatal(e)
	}
	defer p.close()
	if e = p.install(hooks{}); e != nil {
		t.Fatal(e)
	}
	if !p.state.Published || !p.state.Verified || p.state.Restored {
		t.Fatal("completed update did not verify")
	}
	p.close() // ordinary readers do not share DELETE with the pinned updater
	if b, e := os.ReadFile(p.backupPath()); e != nil || sha(b) != l.Old {
		t.Fatal("old software was not preserved")
	}
	if b, e := os.ReadFile(l.Target); e != nil || sha(b) != l.New {
		t.Fatal("published software differs")
	}
}

func TestCompletedBuildIdentityDoesNotAdoptUnknownOrOtherSoftware(t *testing.T) {
	base := completedManifest{BuildOutput: completedRoot + "/target/release/cleanup-8h.exe", OutputSHA: completedLeaf.New, CopyVerified: true, Schema: 1, Todo: 3942, Job: completedBuildJob, Head: completedHead, Root: completedRoot, Command: "cargo build --release --locked -p cleanup-8h", Candidate: completedLeaf.Source, New: completedLeaf.New, Old: completedLeaf.Old}
	for _, kind := range []string{"valid", "other-todo", "other-job", "other-root", "other-head", "debug-command", "other-candidate", "unknown-current", "unknown-new", "schema"} {
		t.Run(kind, func(t *testing.T) {
			m := base
			switch kind {
			case "other-todo":
				m.Todo = 3757
			case "other-job":
				m.Job = "20261026-000000-todo-1"
			case "other-root":
				m.Root += "-sibling"
			case "other-head":
				m.Head = "unknown"
			case "debug-command":
				m.Command = "cargo build -p cleanup-8h"
			case "other-candidate":
				m.Candidate = nativeLeaf.Source
			case "unknown-current":
				m.Old = "unknown"
			case "unknown-new":
				m.New = "unknown"
			case "schema":
				m.Schema = 2
			}
			if (completedManifestIdentity(m) == nil) != (kind == "valid") {
				t.Fatal("manifest identity result differs")
			}
		})
	}
	if completedLeaf.Target != leaves[1].Target || completedLeaf.Old != leaves[1].New || completedLeaf.Kind != "cleanup-8h" || completedLeaf.Target == nativeLeaf.Target {
		t.Fatal("cleanup-only scope changed")
	}
}

func TestCompletedBuildNormalTodoBindingRequiresDonePriorAndExactCurrentOwner(t *testing.T) {
	for _, kind := range []string{"valid", "prior-live", "current-done", "waiting", "unknown-job", "other-root", "other-project", "duplicate", "missing", "unrelated-legacy"} {
		t.Run(kind, func(t *testing.T) {
			current := map[string]any{"id": 3942, "project": "cc-pc-tools-rs", "status": "doing", "jobs": []string{completedBuildJob}, "scheduler": map[string]any{"worktree": completedRoot, "branch": "scheduler/todo-3942-work"}}
			prior := map[string]any{"id": 3757, "project": "cc-pc-tools-rs", "status": "done"}
			items := []any{current, prior}
			switch kind {
			case "prior-live":
				prior["status"] = "doing"
			case "current-done":
				current["status"] = "done"
			case "waiting":
				current["wait_on"] = []int{9999}
			case "unknown-job":
				current["jobs"] = []string{"unknown"}
			case "other-root":
				current["scheduler"] = map[string]any{"worktree": completedRoot + "-sibling", "branch": "scheduler/todo-3942-work"}
			case "other-project":
				current["project"] = "other"
			case "duplicate":
				items = append(items, current)
			case "missing":
				items = items[:1]
			case "unrelated-legacy":
				items = append(items, map[string]any{"id": 99, "project": 123, "jobs": "old type"})
			}
			b, _ := json.Marshal(map[string]any{"projects": map[string]any{"cc-pc-tools-rs": "D:/CC/Work/unattended-operations-20261013"}, "items": items})
			want := kind == "valid" || kind == "unrelated-legacy"
			if (completedTodoReference(b) == nil) != want {
				t.Fatal("normal todo binding result differs")
			}
		})
	}
}
