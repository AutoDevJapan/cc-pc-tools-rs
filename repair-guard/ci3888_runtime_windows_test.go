package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func ci3888RuntimeFixture() (ci3888RuntimeAccess, skillsRegistry, map[string]object, official3894Owned, string) {
	reg := official3894LineageFixture()
	reg.Root = roots[3]
	row := reg.Tasks[3888]
	caller := "20261028-000001-todo-3888"
	row.Status, row.Scheduler.Worktree = "doing", ci3888RuntimeWorkspace
	row.Scheduler.Branch, row.Scheduler.Job, row.Scheduler.Key = "scheduler/todo-3888-local-ai-console-windows-ci-pc", caller, "synthetic-launch-key"
	row.Jobs = append(row.Jobs, ci3888RuntimeProducer, caller)
	reg.Tasks[3888] = row
	jobs := official3894OriginFixture(reg)
	for _, id := range []string{ci3888RuntimeProducer, caller} {
		jobs[id] = object{"id": id, "todo_id": json.Number("3888"), "project": row.Project, "task_type": "implementation", "scope_source": "launcher", "cwd": row.Scheduler.Worktree,
			"status": "running", "launch_key": row.Scheduler.Key, "work_owner": object{"repository": "autodevjapan/local-ai-console", "git_common": "D:/synthetic/.git", "worktree": row.Scheduler.Worktree, "branch": row.Scheduler.Branch}}
	}
	r := official3894Owned{Version: 3, ID: ci3888RuntimeID, Root: filepath.Dir(ci3888RuntimeRoot), Path: ci3888RuntimeRoot,
		Volume: 2229119083, FileID: 14918173766427707, Birth: 134375814346352521,
		Created: "2026-10-27T22:10:00+09:00", Project: "todo-3888", Content: "official-ci3387-public-runtime",
		ProducerJob: ci3888RuntimeProducer, ProducerTodo: 3888, ProducerKind: "scheduler", State: "owned", Integrity: strings.Repeat("1", 64)}
	for path := r.Root; ; path = filepath.Dir(path) {
		ancestor := struct {
			Path   string `json:"path"`
			Volume uint32 `json:"volume"`
			FileID uint64 `json:"file_id"`
			Birth  uint64 `json:"birth"`
		}{path, r.Volume, 31, 41}
		if len(r.Ancestors) == 0 {
			ancestor.FileID, ancestor.Birth = 562949953422078, 134072793159133344
		}
		r.Ancestors = append(r.Ancestors, ancestor)
		if path == filepath.Dir(path) {
			break
		}
	}
	a := ci3888RuntimeAccess{
		registry: func(context.Context) (skillsRegistry, bool) { return reg, true },
		job:      func(id string) (object, bool) { j, ok := jobs[id]; return j, ok },
		read: func(path string, _ int64) ([]byte, bool) {
			return []byte("synthetic independently pinned creation"), path == ci3888RuntimeCreationOutput || path == ci3888RuntimeIssuerRecord
		},
		creation:  func([]byte) (official3894Owned, bool) { return r, true },
		directory: func(string) bool { return true },
		identity: func(path string) (audio2906Metadata, error) {
			m := audio2906Metadata{Volume: r.Volume, Owner: civ6Owner, Attributes: syscall.FILE_ATTRIBUTE_DIRECTORY}
			if same(path, r.Path) {
				m.ID, m.Created = r.FileID, int64(r.Birth)
				return m, nil
			}
			for _, ancestor := range r.Ancestors {
				if same(path, ancestor.Path) {
					m.ID, m.Created = ancestor.FileID, int64(ancestor.Birth)
					return m, nil
				}
			}
			return m, errors.New("not a declared metadata path")
		},
		common: func(string) (string, bool) { return "D:/synthetic/.git", true },
		source: func(string, string) error { return nil }, live: func(object) bool { return true },
		github: func(context.Context) error { return nil }, merged: func(context.Context) error { return nil }, blocked: func(string) bool { return false },
	}
	return a, reg, jobs, r, caller
}

func TestCI3888RuntimePurposePositiveAndBoundary(t *testing.T) {
	a, _, _, _, caller := ci3888RuntimeFixture()
	if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, caller, a); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{ci3888RuntimeRoot + "/child", filepath.Dir(ci3888RuntimeRoot), ci3888RuntimeRoot + "-other", "relative", ci3888RuntimeRoot + ":ads", ci3888RuntimeRoot + "."} {
		if err := ci3888RuntimeVerifyWith(context.Background(), path, caller, a); err == nil {
			t.Fatalf("accepted outside location: %s", path)
		}
		if ci3888RuntimePathAllowed(path) {
			t.Fatal("production directory gate admitted a child or other location")
		}
	}
}

func TestCI3888RuntimeSystemAncestorOwner(t *testing.T) {
	a, _, _, _, caller := ci3888RuntimeFixture()
	original := a.identity
	a.identity = func(path string) (audio2906Metadata, error) {
		m, err := original(path)
		if !same(path, ci3888RuntimeRoot) && !same(path, filepath.Dir(ci3888RuntimeRoot)) {
			m.Owner = "S-1-5-18" // System ancestors need not be owned by the temp owner.
		}
		return m, err
	}
	if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, caller, a); err != nil {
		t.Fatalf("unchanged system ancestors rejected: %v", err)
	}
}

func TestCI3888RuntimeIndependentEvidence(t *testing.T) {
	for _, change := range []string{"registry", "current-job", "producer-job", "original-lineage", "source", "build", "process", "stdout", "creation", "issuer", "issuer-changed", "root-handle", "root-identity", "parent-identity", "owner", "reparse", "file", "protection", "ancestor-protection", "github", "cancelled", "physical-recheck", "source-recheck", "process-recheck"} {
		t.Run(change, func(t *testing.T) {
			a, _, jobs, _, caller := ci3888RuntimeFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch change {
			case "registry":
				a.registry = func(context.Context) (skillsRegistry, bool) { return skillsRegistry{}, false }
			case "current-job":
				delete(jobs, caller)
			case "producer-job":
				delete(jobs, ci3888RuntimeProducer)
			case "original-lineage":
				jobs[official3894OriginalCreator]["scope_source"] = "caller"
			case "source":
				a.source = func(string, string) error { return official3894Error("runtime_provider_source_unverified") }
			case "build":
				a.merged = func(context.Context) error { return official3894Error("runtime_reader_merge_not_merged") }
			case "process":
				a.live = func(object) bool { return false }
			case "stdout", "issuer", "issuer-changed":
				old := a.read
				a.read = func(path string, n int64) ([]byte, bool) {
					if change == "stdout" && path == ci3888RuntimeCreationOutput || change == "issuer" && path == ci3888RuntimeIssuerRecord {
						return nil, false
					}
					if change == "issuer-changed" && path == ci3888RuntimeIssuerRecord {
						return []byte("different"), true
					}
					return old(path, n)
				}
			case "creation":
				a.creation = func([]byte) (official3894Owned, bool) { return official3894Owned{}, false }
			case "root-handle":
				a.directory = func(string) bool { return false }
			case "root-identity", "parent-identity", "owner", "reparse", "file", "physical-recheck":
				old, rootCalls := a.identity, 0
				a.identity = func(path string) (audio2906Metadata, error) {
					m, e := old(path)
					if same(path, ci3888RuntimeRoot) {
						rootCalls++
						switch change {
						case "root-identity":
							m.ID++
						case "owner":
							m.Owner = "different"
						case "reparse":
							m.Attributes |= syscall.FILE_ATTRIBUTE_REPARSE_POINT
						case "file":
							m.Attributes = 0
						case "physical-recheck":
							if rootCalls > 1 {
								m.Created++
							}
						}
					}
					if change == "parent-identity" && same(path, filepath.Dir(ci3888RuntimeRoot)) {
						m.ID++
					}
					return m, e
				}
			case "protection":
				a.blocked = func(string) bool { return true }
			case "ancestor-protection":
				a.blocked = func(path string) bool { return same(path, filepath.Dir(ci3888RuntimeRoot)) }
			case "github":
				a.github = func(context.Context) error { return errors.New("synthetic private metadata") }
			case "cancelled":
				a.github = func(context.Context) error { cancel(); return nil }
			case "source-recheck":
				calls := 0
				a.source = func(string, string) error {
					calls++
					if calls > 1 {
						return official3894Error("runtime_provider_source_unverified")
					}
					return nil
				}
			case "process-recheck":
				calls := 0
				a.live = func(object) bool { calls++; return calls == 1 }
			}
			if err := ci3888RuntimeVerifyWith(ctx, ci3888RuntimeRoot, caller, a); err == nil {
				t.Fatal("incomplete or changed independent evidence admitted")
			}
		})
	}
}

func TestCI3888RuntimeActorAndOriginalScope(t *testing.T) {
	for _, change := range []string{"foreign-caller", "foreign-task", "stop", "parent-stop", "creator-stop", "wait-ready", "assignment", "latest", "workspace", "branch", "key", "origin", "parent-job", "subwork", "repository", "git-common", "job-worktree", "job-branch", "job-status", "job-key", "producer-status", "producer-todo", "producer-origin", "producer-repository"} {
		t.Run(change, func(t *testing.T) {
			a, reg, jobs, _, caller := ci3888RuntimeFixture()
			row := reg.Tasks[3888]
			job, producer := jobs[caller], jobs[ci3888RuntimeProducer]
			switch change {
			case "foreign-caller":
				caller = "20261028-000001-todo-4143"
			case "foreign-task":
				row.ID = 4143
			case "stop":
				row.OwnerStopped = true
			case "parent-stop", "creator-stop":
				id := 3387
				if change == "creator-stop" {
					id = 3894
				}
				other := reg.Tasks[id]
				other.OwnerStopped = true
				reg.Tasks[id] = other
			case "wait-ready":
				row.WaitReady = true
			case "assignment":
				row.Scheduler.Job = ci3888RuntimeProducer
			case "latest":
				row.Jobs = append(row.Jobs, "20261028-000002-todo-3888")
			case "workspace":
				row.Scheduler.Worktree += "-other"
			case "branch":
				row.Scheduler.Branch = "scheduler/todo-4143-other"
			case "key":
				row.Scheduler.Key = ""
			case "origin":
				job["scope_source"] = "caller"
			case "parent-job":
				job["parent_job_id"] = ci3888RuntimeProducer
			case "subwork":
				job["subwork"] = object{"kind": "check"}
			case "repository":
				obj(job["work_owner"])["repository"] = "different/owner"
			case "git-common":
				obj(job["work_owner"])["git_common"] = "D:/different/.git"
			case "job-worktree":
				obj(job["work_owner"])["worktree"] = "D:/different"
			case "job-branch":
				obj(job["work_owner"])["branch"] = "different"
			case "job-status":
				job["status"] = "succeeded"
			case "job-key":
				job["launch_key"] = "different"
			case "producer-status":
				producer["status"] = "failed"
			case "producer-todo":
				producer["todo_id"] = json.Number("4143")
			case "producer-origin":
				producer["scope_source"] = "caller"
			case "producer-repository":
				obj(producer["work_owner"])["repository"] = "different/owner"
			}
			reg.Tasks[3888] = row
			if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, caller, a); err == nil {
				t.Fatal("foreign/held caller or original scope admitted")
			}
		})
	}
}

func TestCI3888RuntimeCreationMetadataAndUint64(t *testing.T) {
	_, _, _, original, _ := ci3888RuntimeFixture()
	if !ci3888RuntimeCreationFields(original) {
		t.Fatal("complete creation metadata refused")
	}
	raw, _ := json.Marshal(original)
	var decoded official3894Owned
	if !skillsStrict(raw, &decoded) || decoded.FileID != 14918173766427707 || decoded.Birth != 134375814346352521 {
		t.Fatal("uint64 rounded")
	}
	if _, ok := ci3888RuntimeCreation(raw); ok {
		t.Fatal("unobserved caller bytes supplied creation authority")
	}
	for _, change := range []string{"version", "id", "state", "path", "root", "project", "content", "job", "todo", "kind", "volume", "fileid", "birth", "integrity", "created", "ancestor-missing", "ancestor-chain", "ancestor-id", "ancestor-birth"} {
		t.Run(change, func(t *testing.T) {
			r := decoded
			r.Ancestors = append(r.Ancestors[:0:0], decoded.Ancestors...)
			switch change {
			case "version":
				r.Version = 2
			case "id":
				r.ID = strings.Repeat("0", 32)
			case "state":
				r.State = "deleted"
			case "path":
				r.Path += "/child"
			case "root":
				r.Root += "/other"
			case "project":
				r.Project = "todo-4143"
			case "content":
				r.Content = "temp"
			case "job":
				r.ProducerJob = "20261028-000001-todo-4143"
			case "todo":
				r.ProducerTodo = 4143
			case "kind":
				r.ProducerKind = "caller"
			case "volume":
				r.Volume++
			case "fileid":
				r.FileID++
			case "birth":
				r.Birth++
			case "integrity":
				r.Integrity = ""
			case "created":
				r.Created = "invalid"
			case "ancestor-missing":
				r.Ancestors = r.Ancestors[:1]
			case "ancestor-chain":
				r.Ancestors[1].Path += "/other"
			case "ancestor-id":
				r.Ancestors[0].FileID++
			case "ancestor-birth":
				r.Ancestors[0].Birth++
			}
			if ci3888RuntimeCreationFields(r) {
				t.Fatal("changed creation metadata admitted")
			}
		})
	}
}

func TestCI3888RuntimeCLIMetadataOnly(t *testing.T) {
	for _, args := range [][]string{{}, {ci3888RuntimeFlag}, {ci3888RuntimeFlag, ci3888RuntimeRoot, "extra"}, {ci3888RuntimeFlag, ci3888RuntimeRoot, "--guard-path", ci3888RuntimeRoot}, {ci3888RuntimeFlag + "=" + ci3888RuntimeRoot}, {ci3888RuntimeFlag, ci3888RuntimeRoot + "/child"}} {
		var out bytes.Buffer
		calls := 0
		code := runCI3888RuntimeCLI(args, &out, func(context.Context, string) error { calls++; return nil })
		if code != 2 || calls != 0 || !strings.Contains(out.String(), `"reason":"arguments"`) {
			t.Fatal("invalid/mixed arguments admitted")
		}
	}
	for _, success := range []bool{false, true} {
		var out bytes.Buffer
		code := runCI3888RuntimeCLI([]string{ci3888RuntimeFlag, ci3888RuntimeRoot}, &out, func(context.Context, string) error {
			if success {
				return nil
			}
			return errors.New("synthetic secret must not escape")
		})
		got, ok := conversationJSON(out.Bytes())
		if !ok || got["purpose_verified"] != success || (code == 0) != success || strings.Contains(out.String(), "synthetic secret") {
			t.Fatal("wrong result or private error leaked")
		}
		for _, key := range []string{"access_allowed", "child_data_access", "sdk_qualified", "sdk_consumed", "launch_performed", "original_registry_recovered"} {
			if got[key] != false {
				t.Fatalf("unexpected authority: %s", key)
			}
		}
		if got["files"] != nil {
			t.Fatal("metadata query returned payload")
		}
	}
}
