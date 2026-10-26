package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func official3387GitFixture(t *testing.T, repository string) (string, string) {
	t.Helper()
	root := t.TempDir()
	branch := "scheduler/todo-3888-synthetic"
	for _, args := range [][]string{
		{"init", "--initial-branch=" + branch, root},
		{"-C", root, "remote", "add", "origin", "https://github.com/" + repository + ".git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = civ6GitEnvironment(os.Environ())
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create synthetic Git metadata: %v %s", err, out)
		}
	}
	return root, branch
}

func TestOfficialCI3387ProviderUsesItsOwnRepository(t *testing.T) {
	root, branch := official3387GitFixture(t, "AutoDevJapan/local-ai-console")
	if !official3894ProviderSource(root, branch) {
		t.Fatal("normal CI provider with its own repository and branch was rejected")
	}
	if skillsActorSource(root, branch) {
		t.Fatal("the public Skills source rule must still reject the private repository")
	}
	if official3894ProviderSource(root, branch+"-different") {
		t.Fatal("a different current branch was admitted")
	}
	if official3894ProviderSource(filepath.Join(root, "missing"), branch) {
		t.Fatal("an unreadable source was admitted")
	}
}

func TestOfficialCI3387ProviderRejectsAnotherRepository(t *testing.T) {
	for _, repo := range []string{"AutoDevJapan/cc-pc-tools-rs", "another-owner/local-ai-console"} {
		t.Run(repo, func(t *testing.T) {
			root, branch := official3387GitFixture(t, repo)
			if official3894ProviderSource(root, branch) {
				t.Fatal("another repository supplied CI provider authority")
			}
		})
	}
}

func TestOfficialCI3387ReaderPreservesPreparedProducer(t *testing.T) {
	reader := strings.Repeat("a", 40)
	saved := official3894Source{Head: "f5f846a8470b544846e4c9ce43306061bf5668f9", Registration: strings.Repeat("b", 64), CreatorJob: "synthetic-original", Workspace: "synthetic", Branch: "original", SpecSHA: strings.Repeat("c", 64)}
	before, _ := json.Marshal(saved)
	producer, ok := official3894ProducerHead(reader, &saved)
	if !ok || producer != saved.Head {
		t.Fatal("a repaired reader cannot verify the original prepared source without rewriting its receipt")
	}
	after, _ := json.Marshal(saved)
	if string(before) != string(after) {
		t.Fatal("reader selection changed the original producer registration")
	}
	if got, ok := official3894ProducerHead(reader, nil); !ok || got != reader {
		t.Fatal("new creation must bind to its own clean reader build")
	}
	if got, ok := official3894ProducerHead(reader, &official3894Source{Head: reader}); !ok || got != reader {
		t.Fatal("same-build receipt was rejected")
	}
	for _, bad := range []string{"", strings.Repeat("d", 40), strings.ToUpper(saved.Head)} {
		if _, ok := official3894ProducerHead(reader, &official3894Source{Head: bad}); ok {
			t.Fatal("unknown producer revision was adopted")
		}
	}
}

func TestOfficialCI3387ProducerAndReaderEvidenceAreIndependent(t *testing.T) {
	reader := strings.Repeat("a", 40)
	producer := "f5f846a8470b544846e4c9ce43306061bf5668f9"
	for _, failure := range []string{"", "reader-unmerged", "producer-unmerged", "producer-input-changed", "unknown-producer", "invalid-reader"} {
		t.Run(failure, func(t *testing.T) {
			current := reader
			saved := official3894Source{Head: producer, Registration: strings.Repeat("b", 64)}
			if failure == "unknown-producer" {
				saved.Head = strings.Repeat("d", 40)
			}
			if failure == "invalid-reader" {
				current = ""
			}
			var compared, merges []string
			got, err := official3894SourceRevision(current, &saved, func(ref string) bool {
				compared = append(compared, ref)
				return failure != "producer-input-changed"
			}, func(ref string) bool {
				merges = append(merges, ref)
				return !(failure == "reader-unmerged" && ref == reader) && !(failure == "producer-unmerged" && ref == producer)
			})
			if failure == "" {
				if err != nil || got != producer || len(compared) != 1 || compared[0] != producer || len(merges) != 2 || merges[0] != reader || merges[1] != producer {
					t.Fatalf("original producer and new reader were conflated: got=%s err=%v compared=%v merges=%v", got, err, compared, merges)
				}
			} else if err == nil || got != "" {
				t.Fatal("invalid source evidence was accepted")
			}
			if (failure == "unknown-producer" || failure == "invalid-reader") && (len(compared) != 0 || len(merges) != 0) {
				t.Fatal("unknown source caused a Git/API probe")
			}
			if saved.Registration != strings.Repeat("b", 64) {
				t.Fatal("the original authenticated registration was replaced")
			}
		})
	}
}

func official3387UpdaterFixture() (skillsRegistry, string, object, string) {
	reg := official3894LineageFixture()
	caller := "20261026-114514-todo-3387"
	row := reg.Tasks[3387]
	row.Status = "doing"
	row.Jobs = append(row.Jobs, caller)
	row.Scheduler.Job = caller
	row.Scheduler.LastJob = caller
	row.Scheduler.Key = "synthetic-current-launch"
	reg.Tasks[3387] = row
	common := "D:/synthetic/.git"
	job := object{"id": caller, "todo_id": json.Number("3387"), "project": row.Project, "task_type": "research", "scope_source": "launcher", "status": "running", "cwd": row.Scheduler.Worktree, "launch_key": row.Scheduler.Key,
		"work_owner": object{"repository": "autodevjapan/cc-pc-tools-rs", "git_common": common, "worktree": row.Scheduler.Worktree, "branch": row.Scheduler.Branch}}
	return reg, caller, job, common
}

func TestOfficialCI3387ParentSoftwareUpdaterBound(t *testing.T) {
	reg, caller, job, common := official3387UpdaterFixture()
	if !official3894ParentUpdaterBound(reg, caller, job, common) {
		t.Fatal("the current original parent cannot compare the known software")
	}
	for _, change := range []string{"id", "todo_id", "project", "scope_source", "task_type", "parent_job_id", "subwork", "status", "cwd", "launch_key", "repository", "git_common", "worktree", "branch", "extra-owner-field"} {
		t.Run(change, func(t *testing.T) {
			reg, caller, job, common := official3387UpdaterFixture()
			owner := obj(job["work_owner"])
			switch change {
			case "todo_id":
				job[change] = json.Number("3888")
			case "subwork":
				job[change] = object{"kind": "review"}
			case "repository", "git_common", "worktree", "branch":
				owner[change] = "different"
			case "extra-owner-field":
				owner["Repository"] = owner["repository"]
			default:
				job[change] = "different"
			}
			if official3894ParentUpdaterBound(reg, caller, job, common) {
				t.Fatal("an unrelated, stale or changed current actor was admitted")
			}
		})
	}
	for _, change := range []string{"status", "stop", "postponed", "stop-kind", "latest", "launch", "project", "task", "typed-parent", "scheduler-parent", "admission"} {
		t.Run("row/"+change, func(t *testing.T) {
			reg, caller, job, common := official3387UpdaterFixture()
			row := reg.Tasks[3387]
			switch change {
			case "status":
				row.Status = "wait"
			case "stop":
				row.OwnerStopped = true
			case "postponed":
				row.Postponed = true
			case "stop-kind":
				row.StopKind = "explicit"
			case "latest":
				row.Jobs = append(row.Jobs, "different")
			case "launch":
				row.Scheduler.Job = "different"
			case "project":
				row.Project = "different"
			case "task":
				row.ID = 3888
			case "typed-parent":
				row.Subwork.Parent = 3387
			case "scheduler-parent":
				row.Scheduler.SubworkParent = "different"
			case "admission":
				row.Scheduler.Admission = &skillsAdmission{}
			}
			reg.Tasks[3387] = row
			if official3894ParentUpdaterBound(reg, caller, job, common) {
				t.Fatal("a held, superseded or delegated parent supplied update authority")
			}
		})
	}
}

func TestOfficialCI3387SoftwareUpdaterCannotBecomeSDKActor(t *testing.T) {
	t.Setenv("CC_JOB_ID", "20261026-114514-todo-3387")
	for _, consume := range []bool{false, true} {
		if _, err := official3894SourceCheck(context.Background(), nil, consume); err == nil || err.Error() != "registered_actor_required" {
			t.Fatal("the software updater acquired SDK preparation/verification/consumption authority")
		}
	}
}
