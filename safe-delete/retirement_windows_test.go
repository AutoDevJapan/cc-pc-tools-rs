//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRetirementDoesNotPromoteAQuarantinedV2ReceiptWithoutOriginalEvidence(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	r.State = "quarantined"
	r.Destination = root
	modify(t, c, r)
	_, err := c.retiredPlan(r.ID)
	code(t, err, "ORIGINAL_CREATE_UNVERIFIED")
}

func retirementFixture(t *testing.T, legacy bool) (config, receipt, string) {
	t.Helper()
	c, root := fixture(t)
	return retirementFixtureAt(t, c, root, legacy)
}

func retirementFixtureAt(t *testing.T, c config, root string, legacy bool) (config, receipt, string) {
	t.Helper()
	job := retirementJob{ID: "producer-job", Todo: 42, CWD: root, Status: "running"}
	rt := &retirementRuntime{Jobs: filepath.Join(root, "jobs"), Todo: filepath.Join(root, "todo.json"), Tool: filepath.Join(root, "safe-delete.exe"), Until: time.Now().Add(time.Minute), Guard: func(string) error { return nil }, Actor: func() (retirementJob, error) { return job, nil }, SavedSource: func(string) (string, error) { return strings.Repeat("a", 40), nil }}
	if !legacy {
		c.Retirement = rt
	}
	r := owned(t, c, root)
	c.Retirement = rt
	spec := filepath.Join(root, "spec.md")
	write(t, spec, "specific disposable fixture, source preserved")
	retirementPut(t, rt.Todo, map[string]any{"items": []retirementTodo{{ID: 42, Status: "doing", Jobs: []string{job.ID}, Note: "resource work", Spec: spec}}})
	retirementPut(t, filepath.Join(rt.Jobs, job.ID, "job.json"), job)
	retirementEvent(t, c, job.ID, "create --project test --content scratch", r, 0)
	write(t, filepath.Join(r.Path, "cache.bin"), "disposable fixture")
	return c, r, root
}

// Invoked only by the Rust integration test's normally compiled Go test binary.
// These injections are not flags or environment overrides in the production CLI.
func TestRetirementLeaseProcess(t *testing.T) {
	root := os.Getenv("CC_RETIREMENT_FIXTURE_ROOT")
	if root == "" {
		return
	}
	c := config{Store: filepath.Join(root, "receipts"), Quarantine: filepath.Join(root, "quarantine"), Temp: root, MaxEntries: 10000, MaxBytes: 512 << 20}
	c, r, _ := retirementFixtureAt(t, c, root, false)
	retirementReleased(t, c, r)
	r = retirementQuarantined(t, c, r)
	unlock, e := c.retirementLock(r.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	p, e := c.retiredPlan(r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.NewEncoder(os.Stdout).Encode(p); e != nil {
		t.Fatal(e)
	}
	line, e := bufio.NewReader(os.Stdin).ReadString('\n')
	if e != nil || line != "finish\n" {
		os.Exit(3)
	}
	if e = c.finishRetirement(p); e != nil {
		t.Fatal(e)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"id": r.ID, "state": "quarantine_removed"})
	os.Exit(0)
}
func retirementPut(t *testing.T, path string, v any) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	write(t, path, string(b))
}
func retirementEvent(t *testing.T, c config, job, action string, v any, exit int) {
	t.Helper()
	rt := c.retirementRuntime()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	row := map[string]any{"type": "item.completed", "item": map[string]any{"type": "command_execution", "status": "completed", "command": rt.Tool + " " + action, "aggregated_output": string(b), "exit_code": exit}}
	b, e = json.Marshal(row)
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(rt.Jobs, job, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e = f.Write(append(b, '\n')); e != nil {
		t.Fatal(e)
	}
}
func retirementReleased(t *testing.T, c config, r receipt) retiredRelease {
	t.Helper()
	rel, e := c.releaseRetirement(r.ID, "producer-job", "producer-job", true)
	if e != nil {
		t.Fatal(e)
	}
	retirementEvent(t, c, "producer-job", "release --id "+r.ID+" --producer-job producer-job --move-job producer-job --disposable", rel, 0)
	return rel
}
func retirementQuarantined(t *testing.T, c config, r receipt) receipt {
	t.Helper()
	q, e := c.quarantine(r.ID, "size")
	if e != nil {
		t.Fatal(e)
	}
	retirementEvent(t, c, "producer-job", "quarantine --id "+r.ID+" --reason size", q, 0)
	r, e = c.retiredReceipt(r.ID)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestRetirementOriginalReleaseMoveAndCanonicalFinish(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-v3", true: "original-v2"}[legacy], func(t *testing.T) {
			c, r, _ := retirementFixture(t, legacy)
			if legacy && r.Version != 2 || !legacy && r.Version != 3 {
				t.Fatal(r.Version)
			}
			retirementReleased(t, c, r)
			q := retirementQuarantined(t, c, r)
			if legacy {
				q.MoveJob = ""
				modify(t, c, q)
			}
			id, e := c.retirementID(q.Destination)
			if e != nil || id != r.ID {
				t.Fatal(id, e)
			}
			p, e := c.retiredPlan(id)
			if e != nil {
				t.Fatal(e)
			}
			if p.Action != "candidate" || p.Bytes != uint64(len("disposable fixture")) || len(p.Entries) != 2 {
				t.Fatal(p)
			}
			if _, e = c.plan(id); e == nil {
				t.Fatal("ordinary plan accepted quarantined record")
			}
			code(t, c.finishRetirement(p), "TARGET_STILL_EXISTS")
			// The consumer, not safe-delete, executes its own deletion. This is only
			// a synthetic t.TempDir fixture; production uses Rust pinned handles.
			if e = os.Remove(filepath.Join(p.Path, "cache.bin")); e != nil {
				t.Fatal(e)
			}
			if e = os.Remove(p.Path); e != nil {
				t.Fatal(e)
			}
			if e = c.finishRetirement(p); e != nil {
				t.Fatal(e)
			}
			after, e := c.retiredReceipt(id)
			if e != nil || after.State != "quarantine_removed" {
				t.Fatal(after, e)
			}
			if !exists(filepath.Join(q.Destination, "理由.txt")) {
				t.Fatal("quarantine evidence wrapper lost")
			}
			p, e = c.retiredPlan(id)
			if e != nil || p.Action != "completed" {
				t.Fatal(p, e)
			}
		})
	}
}
func TestRetirementOriginalEvidenceFailuresRetainData(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*testing.T, config, receipt)
	}{
		{"missing-create", "ORIGINAL_CREATE_UNVERIFIED", func(t *testing.T, c config, r receipt) {
			write(t, filepath.Join(c.retirementRuntime().Jobs, "producer-job", "events.jsonl"), "")
		}},
		{"new-self-checksum", "ORIGINAL_CREATE_UNVERIFIED", func(t *testing.T, c config, r receipt) {
			r.Content = "rewritten"
			r.Integrity = digest(r)
			modify(t, c, r)
		}},
		{"different-todo", "ORIGINAL_PRODUCER_REQUIRED", func(t *testing.T, c config, r receipt) {
			c.Retirement.Actor = func() (retirementJob, error) { return retirementJob{ID: "collector-job", Todo: 99}, nil }
		}},
		{"disallowed-source", "SOURCE_OR_RECOVERY_RETAINED", func(t *testing.T, c config, r receipt) { write(t, filepath.Join(r.Path, "main.go"), "source") }},
		{"unclassified", "GUARD_RETAINED", func(t *testing.T, c config, r receipt) {
			c.Retirement.Guard = func(string) error { return fail("GUARD_RETAINED") }
		}},
		{"guard-failed", "GUARD_UNAVAILABLE", func(t *testing.T, c config, r receipt) {
			c.Retirement.Guard = func(string) error { return fail("GUARD_UNAVAILABLE") }
		}},
		{"unsaved", "SOURCE_NOT_SAVED", func(t *testing.T, c config, r receipt) {
			c.Retirement.SavedSource = func(string) (string, error) { return "", fail("SOURCE_NOT_SAVED") }
		}},
		{"unpushed", "SOURCE_NOT_PUSHED", func(t *testing.T, c config, r receipt) {
			c.Retirement.SavedSource = func(string) (string, error) { return "", fail("SOURCE_NOT_PUSHED") }
		}},
		{"expired", "ORIGINAL_CREATE_UNVERIFIED", func(t *testing.T, c config, r receipt) { c.Retirement.Until = time.Now().Add(-time.Second) }},
		{"external-hardlink", "HARD_LINK_OR_VOLUME_CHANGED", func(t *testing.T, c config, r receipt) {
			outside := filepath.Join(r.Root, "outside.bin")
			write(t, outside, "keep")
			if e := os.Link(outside, filepath.Join(r.Path, "link.bin")); e != nil {
				t.Fatal(e)
			}
		}},
		{"link", "LINK_OR_REPARSE_POINT", func(t *testing.T, c config, r receipt) {
			outside := filepath.Join(r.Root, "outside")
			if e := os.Mkdir(outside, 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(outside, filepath.Join(r.Path, "link")); e != nil {
				t.Skip(e)
			}
		}},
		{"readonly", "PROTECTED_ATTRIBUTES_RETAINED", func(t *testing.T, c config, r receipt) {
			p, _ := syscall.UTF16PtrFromString(filepath.Join(r.Path, "cache.bin"))
			if e := syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_READONLY); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_NORMAL) })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, r, _ := retirementFixture(t, false)
			tc.change(t, c, r)
			_, e := c.releaseRetirement(r.ID, "producer-job", "", true)
			code(t, e, tc.want)
			if !exists(filepath.Join(r.Path, "cache.bin")) {
				t.Fatal("retention changed data")
			}
		})
	}
}
func TestRetirementCurrentStateChangesAndAmbiguousReleaseRetain(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*testing.T, config, receipt, retiredRelease)
	}{
		{"no-release-event", "ORIGINAL_RELEASE_UNVERIFIED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			p := filepath.Join(c.retirementRuntime().Jobs, "producer-job", "events.jsonl")
			lines := strings.Split(strings.TrimSpace(string(mustRetirementBytes(t, p))), "\n")
			write(t, p, lines[0]+"\n"+lines[2]+"\n")
		}},
		{"self-edited-release", "ORIGINAL_RELEASE_UNVERIFIED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			rel.SourceHead = strings.Repeat("b", 40)
			p, _ := c.retirementPath(r.ID, ".release.json")
			retirementPut(t, p, rel)
		}},
		{"retention", "RESOURCE_DISPOSITION_UNVERIFIED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			rel.Retention = true
			p, _ := c.retirementPath(r.ID, ".release.json")
			retirementPut(t, p, rel)
		}},
		{"dependency", "RESOURCE_DISPOSITION_UNVERIFIED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			rel.Dependencies = true
			p, _ := c.retirementPath(r.ID, ".release.json")
			retirementPut(t, p, rel)
		}},
		{"unfinished-resource", "RESOURCE_DISPOSITION_UNVERIFIED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			rel.ResourceFinished = false
			p, _ := c.retirementPath(r.ID, ".release.json")
			retirementPut(t, p, rel)
		}},
		{"not-reproducible", "RESOURCE_DISPOSITION_UNVERIFIED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			rel.Reproducible = false
			p, _ := c.retirementPath(r.ID, ".release.json")
			retirementPut(t, p, rel)
		}},
		{"original-reappeared", "ORIGINAL_PATH_REAPPEARED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			if e := os.Mkdir(r.Path, 0700); e != nil {
				t.Fatal(e)
			}
		}},
		{"content-replacement", "RESOURCE_CONTENTS_CHANGED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			write(t, filepath.Join(r.Destination, "files", "cache.bin"), "replacement")
		}},
		{"unknown-job-state", "JOB_STATE_UNVERIFIED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			j, _ := c.retirementRuntime().job("producer-job")
			j.Status = "unknown"
			retirementPut(t, filepath.Join(c.retirementRuntime().Jobs, j.ID, "job.json"), j)
		}},
		{"active-resource", "RESOURCE_STILL_ACTIVE", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			j, _ := c.retirementRuntime().job("producer-job")
			j.ID = "current-resource-job"
			j.CWD = filepath.Join(r.Destination, "files")
			retirementPut(t, filepath.Join(c.retirementRuntime().Jobs, j.ID, "job.json"), j)
			rt := c.retirementRuntime()
			row, _ := rt.todo(42)
			row.Jobs = append(row.Jobs, j.ID)
			retirementPut(t, rt.Todo, map[string]any{"items": []retirementTodo{row}})
		}},
		{"new-owner-hold", "RETENTION_OR_TODO_CHANGED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			rt := c.retirementRuntime()
			row, _ := rt.todo(42)
			row.Note = "keep the resource"
			retirementPut(t, rt.Todo, map[string]any{"items": []retirementTodo{row}})
		}},
		{"new-spec", "RETENTION_OR_SPEC_CHANGED", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			row, _ := c.retirementRuntime().todo(42)
			write(t, row.Spec, "new requirement")
		}},
		{"cross-drive-receipt", "QUARANTINE_BOUNDARY_MISMATCH", func(t *testing.T, c config, r receipt, rel retiredRelease) {
			r.Destination = "Z:\\削除希望\\other"
			modify(t, c, r)
			p := filepath.Join(c.retirementRuntime().Jobs, "producer-job", "events.jsonl")
			lines := strings.Split(strings.TrimSpace(string(mustRetirementBytes(t, p))), "\n")
			write(t, p, strings.Join(lines[:2], "\n")+"\n")
			retirementEvent(t, c, "producer-job", "quarantine --id "+r.ID+" --reason size", map[string]any{"id": r.ID, "state": "quarantined", "destination": r.Destination}, 0)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, r, _ := retirementFixture(t, false)
			rel := retirementReleased(t, c, r)
			q := retirementQuarantined(t, c, r)
			tc.change(t, c, q, rel)
			_, e := c.retiredPlan(r.ID)
			code(t, e, tc.want)
			if !exists(filepath.Join(q.Destination, "files", "cache.bin")) {
				t.Fatal("retained payload lost")
			}
		})
	}
}
func mustRetirementBytes(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestRetirementDirectCommandEvidenceAndDuplicateKeys(t *testing.T) {
	tool := "D:/CC/Tools/safe-delete/safe-delete.exe"
	for _, s := range []string{tool + " create --project test", `"C:\\Program Files\\PowerShell\\7\\pwsh.exe" -Command '` + tool + " create --project test'"} {
		if !retiredDirect(s, tool, "create") {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"echo " + tool + " create", tool + " create; echo fake", tool + " create | Tee-Object x", tool + " create $(echo x)", "other.exe create", "Get-Content record.json", tool + " create --bad yes"} {
		if retiredDirect(s, tool, "create") {
			t.Fatal(s)
		}
	}
	for _, b := range []string{`{"id":"a","id":"b"}`, `{"v":{"x":1,"x":2}}`, `{} {}`, `{"v":1`} {
		if retiredUniqueJSON([]byte(b)) == nil {
			t.Fatal(b)
		}
	}
}
func TestRetirementLockAndMissingDisposition(t *testing.T) {
	c, r, _ := retirementFixture(t, false)
	unlock, e := c.retirementLock(r.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.retirementLock(r.ID)
	code(t, e, "RETIREMENT_BUSY")
	_, e = c.quarantine(r.ID, "size")
	code(t, e, "RETIREMENT_BUSY")
	unlock()
	_, e = c.releaseRetirement(r.ID, "producer-job", "", false)
	code(t, e, "RESOURCE_DISPOSITION_REQUIRED")
}

func TestRetirementActualPayloadReplacementAndAncestorJunctionRetain(t *testing.T) {
	c, r, root := retirementFixture(t, false)
	retirementReleased(t, c, r)
	q := retirementQuarantined(t, c, r)
	payload := filepath.Join(q.Destination, "files")
	old := filepath.Join(root, "original-retained")
	if e := os.Rename(payload, old); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(payload, 0700); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(payload, "cache.bin"), "disposable fixture")
	_, e := c.retiredPlan(r.ID)
	code(t, e, "RESOURCE_CONTENTS_CHANGED")
	if !exists(filepath.Join(old, "cache.bin")) {
		t.Fatal("original fixture lost")
	}
	// Replace only our synthetic quarantine ancestor with an owned junction.
	if e = os.Rename(c.Quarantine, filepath.Join(root, "quarantine-kept")); e != nil {
		t.Fatal(e)
	}
	if _, e = runHidden(5*time.Second, "cmd.exe", "/c", "mklink", "/J", c.Quarantine, filepath.Join(root, "quarantine-kept")); e != nil {
		t.Fatal(e)
	}
	_, e = c.retiredPlan(r.ID)
	code(t, e, "LINK_OR_REPARSE_POINT")
	if e = os.Remove(c.Quarantine); e != nil {
		t.Fatal(e)
	}
}

func TestRetirementFinishRetainsCanonicalStateOnPartialDeletionOrChangedReceipt(t *testing.T) {
	c, r, _ := retirementFixture(t, false)
	retirementReleased(t, c, r)
	q := retirementQuarantined(t, c, r)
	p, e := c.retiredPlan(r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(p.Path, "cache.bin")); e != nil {
		t.Fatal(e)
	}
	code(t, c.finishRetirement(p), "TARGET_STILL_EXISTS")
	q.MoveJob = "another-job"
	modify(t, c, q)
	code(t, c.finishRetirement(p), "CANONICAL_RECEIPT_CHANGED")
	current, e := c.retiredReceipt(r.ID)
	if e != nil || current.State != "quarantined" {
		t.Fatal(current, e)
	}
}

func TestRetirementReadOnlyInspectionDoesNotCreateLocks(t *testing.T) {
	c, r, _ := retirementFixture(t, false)
	p, _ := c.retirementPath(r.ID, ".lock")
	if exists(p) {
		t.Fatal("fixture already has a lock")
	}
	_, close, e := c.openRetiredPlan(r.ID, true)
	close()
	code(t, e, "ORIGINAL_CREATE_UNVERIFIED")
	if exists(p) {
		t.Fatal("read-only inspection wrote a canonical lock")
	}
}

func TestRetirementLiveSchedulerActorAncestry(t *testing.T) {
	if os.Getenv("CC_RETIREMENT_LIVE_ACTOR_TEST") != "1" {
		t.Skip("opt-in live actor check; canonical fixture tests run normally")
	}
	rt := defaultConfig().retirementRuntime()
	j, je := rt.job(os.Getenv("CC_JOB_ID"))
	row, te := rt.todo(j.Todo)
	t.Logf("job_valid=%v ancestry=%v todo_valid=%v actor_linked=%v", je == nil, retirementProcessAncestor(j.CodexPID), te, retiredHas(row.Jobs, j.ID))
	actor, e := rt.actor()
	if e != nil {
		t.Fatal(e)
	}
	if actor.ID != os.Getenv("CC_JOB_ID") || actor.Todo == 0 || actor.Status != "running" {
		t.Fatal("current scheduler actor not verified")
	}
}

func TestRetirementTodoDecodesOnlyTheOriginalOwnerRow(t *testing.T) {
	c, _, _ := retirementFixture(t, false)
	rt := c.retirementRuntime()
	row, e := rt.todo(42)
	if e != nil {
		t.Fatal(e)
	}
	retirementPut(t, rt.Todo, map[string]any{"items": []any{map[string]any{"id": 7, "note": map[string]any{"legacy": true}, "jobs": "legacy-shape"}, row}})
	selected, e := rt.todo(42)
	if e != nil || selected.ID != 42 || selected.Note != row.Note {
		t.Fatal(selected, e)
	}
	retirementPut(t, rt.Todo, map[string]any{"items": []any{map[string]any{"id": 42, "jobs": "broken-selected-owner"}}})
	_, e = rt.todo(42)
	code(t, e, "PRODUCER_TODO_UNVERIFIED")
}

func TestRetirementActorRejectsReusedOrMissingProcessBirth(t *testing.T) {
	c, _, _ := retirementFixture(t, false)
	rt := c.retirementRuntime()
	rt.Actor = nil
	j, e := rt.job("producer-job")
	if e != nil {
		t.Fatal(e)
	}
	j.CodexPID = uint32(os.Getpid())
	j.CodexCreated = retirementProcessBirth(j.CodexPID)
	if j.CodexCreated == 0 {
		t.Fatal("process identity unavailable")
	}
	path := filepath.Join(rt.Jobs, j.ID, "job.json")
	retirementPut(t, path, j)
	t.Setenv("CC_JOB_ID", j.ID)
	if _, e = rt.actor(); e != nil {
		t.Fatal(e)
	}
	j.CodexCreated++
	retirementPut(t, path, j)
	_, e = rt.actor()
	code(t, e, "PRODUCER_ACTOR_UNVERIFIED")
	j.CodexCreated = 0
	retirementPut(t, path, j)
	_, e = rt.actor()
	code(t, e, "PRODUCER_ACTOR_UNVERIFIED")
}
