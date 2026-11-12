//go:build windows

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type slowAskObject struct {
	*fakeUpdateObject
	delay time.Duration
}

func (o slowAskObject) ACL() (string, error) { time.Sleep(o.delay); return o.fakeUpdateObject.ACL() }

func TestAskExpiredSynchronousACLAndCreationNeverReportSuccess(t *testing.T) {
	for _, prepare := range []bool{false, true} {
		t.Run(strconv.FormatBool(prepare), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			old := &fakeUpdateObject{raw: make([]byte, askOldBytes), id: softwareIdentity{askOldVolume, askOldID, askOldCreated}, acl: "owned-acl"}
			writes := 0
			old.write = func(string, []byte, string) (softwareIdentity, error) {
				writes++
				time.Sleep(30 * time.Millisecond)
				return softwareIdentity{FileID: "owned-created"}, nil
			}
			plan := askSoftwarePlan{Proof: "proof", Candidate: "candidate"}
			b := updateBoundary{open: func(path string) (updateObject, error) {
				if path == askTarget {
					if !prepare {
						return slowAskObject{old, 30 * time.Millisecond}, nil
					}
					return old, nil
				}
				return &fakeUpdateObject{raw: []byte("owned")}, nil
			}, oldMatches: func([]byte) bool { return true }, candidateMatches: func([]byte, []byte) bool { return true }, absent: func(string) bool { return true }}
			r := executeAskSoftwareContext(ctx, prepare, plan, b)
			if ctx.Err() != context.DeadlineExceeded || r.Qualified || r.Prepared {
				t.Fatal("deadline success misreported", r)
			}
			if prepare {
				if writes != 1 || len(r.Created) != 1 || r.BackupIdentity == nil || r.Reason != "prepare_incomplete_preserved" {
					t.Fatal("partial not retained", r)
				}
			} else if writes != 0 {
				t.Fatal("verify wrote")
			}
		})
	}
}

func TestAskReconciliationFixedNamesAbsentPresentUnknownNeverGrant(t *testing.T) {
	opened := 0
	closed := []*fakeUpdateObject{}
	r := reconcileAskObjects(context.Background(), func(path string) (updateObject, error) {
		opened++
		if path == askBackup {
			return nil, errUpdateObject
		}
		o := &fakeUpdateObject{raw: []byte("owned"), id: softwareIdentity{FileID: "5"}, acl: "owned"}
		closed = append(closed, o)
		return o, nil
	}, func(path string) (os.FileInfo, error) {
		if path == askStage {
			return nil, os.ErrNotExist
		}
		return nil, nil
	})
	if r.SwitchAllowed || r.Target.State != "present" || r.Stage.State != "absent" || r.Backup.State != "unknown" || opened != 2 {
		t.Fatal(r)
	}
	for _, o := range closed {
		if !o.closed {
			t.Fatal("reconcile handle leak")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = reconcileAskObjects(ctx, func(string) (updateObject, error) { t.Fatal("expired open"); return nil, nil }, func(string) (os.FileInfo, error) { t.Fatal("expired stat"); return nil, nil })
	if r.Target.State != "unknown" || r.Stage.State != "unknown" || r.Backup.State != "unknown" || r.SwitchAllowed {
		t.Fatal(r)
	}
}

func TestAskPrivacySoftwarePurposeEntryNeverFallsBackToGuardData(t *testing.T) {
	var out bytes.Buffer
	calls := 0
	code := run([]string{"--verify-ask-privacy-update"}, &out, func() softwareObservation {
		t.Fatal("ask purpose reached old guard observation")
		return softwareObservation{}
	}, updateOperations{verify: func() updateResult {
		calls++
		return updateResult{Qualified: true}
	}})
	if code != 0 || calls != 1 {
		t.Fatalf("fixed ask software purpose is unavailable: exit=%d calls=%d", code, calls)
	}
}

// Explicit opt-in local measurement. The source is not merged yet: only the
// local-main comparison is mapped to the pinned source for timing. This never
// calls the installed target or creates its two software leaves.
func TestAskFull207SourceFlightOwnedTiming(t *testing.T) {
	if os.Getenv("FOURBROS_ASK_FLIGHT_TIMING") != "1" {
		t.Skip("explicit owned full-flight measurement")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	raw, err := os.ReadFile(compiledAskPlan.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	proof, err := os.ReadFile(compiledAskPlan.Proof)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(proof)
	var parsed askSoftwareProof
	if json.Unmarshal(proof, &parsed) != nil || len(parsed.Sources) != 207 {
		t.Fatal("fixed proof differs")
	}
	calls := 0
	git := func(args ...string) ([]byte, bool) {
		args = append([]string(nil), args...)
		for i := range args {
			if args[i] == "refs/remotes/origin/main" {
				args[i] = compiledAskPlan.Source
			}
		}
		calls++
		return askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", append([]string{"-C", updateSourceRoot}, args...)...)
	}
	old := &fakeUpdateObject{raw: make([]byte, askOldBytes), id: softwareIdentity{askOldVolume, askOldID, askOldCreated}, acl: "owned-acl"}
	candidate := &fakeUpdateObject{raw: raw}
	p := &fakeUpdateObject{raw: proof}
	writes := 0
	old.write = func(string, []byte, string) (softwareIdentity, error) {
		writes++
		return softwareIdentity{}, errors.New("forbidden timing write")
	}
	b := updateBoundary{open: func(path string) (updateObject, error) {
		switch path {
		case askTarget:
			return old, nil
		case compiledAskPlan.Candidate:
			return candidate, nil
		case compiledAskPlan.Proof:
			return p, nil
		}
		return nil, errUpdateObject
	}, absent: func(string) bool { return true }, oldMatches: func([]byte) bool { return true }, candidateMatches: func(a, b []byte) bool { return askCandidateMatchesContext(ctx, a, b, compiledAskPlan, git) }}
	r := executeAskSoftware(false, compiledAskPlan, b)
	elapsed := time.Since(started)
	if !r.Qualified || r.Prepared || writes != 0 || calls != 4 || elapsed >= 10*time.Second || !old.closed || !candidate.closed || !p.closed {
		t.Fatalf("flight failed calls=%d writes=%d elapsed=%v result=%+v", calls, writes, elapsed, r)
	}
	t.Logf("records=207 actual_git_children=%d full_flight_ms=%.3f source_local_parity_only=true real_target_open=0 fixed_leaf_create=0 handles_closed=true", calls, float64(elapsed)/float64(time.Millisecond))
}

func TestAskGitDeadlineChild(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "ask-owned-deadline-child" && i+1 < len(os.Args) {
			if os.WriteFile(os.Args[i+1], []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
				os.Exit(3)
			}
			time.Sleep(10 * time.Second)
			os.Exit(0)
		}
	}
}

func TestAskOwnedSupervisorChild(t *testing.T) {
	if os.Getenv("ASK_OWNED_SUPERVISOR_CHILD") != "1" {
		return
	}
	mode := os.Getenv("ASK_OWNED_MODE")
	if mode == "leaf" {
		if os.WriteFile(filepath.Join(os.Getenv("ASK_OWNED_ROOT"), "backup.exe"), []byte("owned partial"), 0600) != nil {
			os.Exit(3)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestAskOwnedSupervisorChild$")
		child.Env = append(os.Environ(), "ASK_OWNED_MODE=descendant")
		if child.Start() != nil {
			os.Exit(4)
		}
		if os.WriteFile(filepath.Join(os.Getenv("ASK_OWNED_ROOT"), "descendant.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0600) != nil {
			os.Exit(5)
		}
	}
	if mode != "normal" {
		time.Sleep(20 * time.Second)
	}
	fmt.Fprintln(os.Stdout, `{"owned":true}`)
	os.Exit(0)
}

func TestAskOwnedSupervisorCollectsTreeAndPreservesPartial(t *testing.T) {
	for _, mode := range []string{"normal", "leaf"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			t.Setenv("ASK_OWNED_SUPERVISOR_CHILD", "1")
			t.Setenv("ASK_OWNED_MODE", mode)
			t.Setenv("ASK_OWNED_ROOT", root)
			budget := 300 * time.Millisecond
			if mode == "normal" {
				budget = 2 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			got := askOwnedChild(ctx, os.Args[0], "-test.run=^TestAskOwnedSupervisorChild$")
			defer clear(got.Raw)
			if !got.Reaped {
				t.Fatal("job not reaped")
			}
			if mode == "normal" {
				if got.TimedOut || got.Exit != 0 || !bytes.Contains(got.Raw, []byte(`"owned":true`)) {
					t.Fatal(got)
				}
				return
			}
			if !got.TimedOut || len(got.Raw) != 0 {
				t.Fatal("timeout mistaken for result", got)
			}
			if b, e := os.ReadFile(filepath.Join(root, "backup.exe")); e != nil || string(b) != "owned partial" {
				t.Fatal("partial lost")
			}
			if _, e := os.Stat(filepath.Join(root, "stage.exe")); !os.IsNotExist(e) {
				t.Fatal("unknown stage")
			}
			pid, e := os.ReadFile(filepath.Join(root, "descendant.pid"))
			if e != nil {
				t.Fatal(e)
			}
			n, e := strconv.Atoi(string(pid))
			if e != nil {
				t.Fatal(e)
			}
			h, e := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(n))
			if e == nil {
				defer syscall.CloseHandle(h)
				state, e := syscall.WaitForSingleObject(h, 1000)
				if e != nil || state != syscall.WAIT_OBJECT_0 {
					t.Fatal("descendant alive")
				}
			}
			t.Log("actual_owned_job_deadline=true root_descendant_reaped=true partial_backup_retained=true stage_absent=true real_target=0")
		})
	}
}

func TestAskSharedDeadlineReapsChildBeforeAnyLeaf(t *testing.T) {
	root := physicalSoftwareFixture(t)
	marker := filepath.Join(root, "owned-child-pid")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, ok := askGitCommand(ctx, os.Args[0], "-test.run=^TestAskGitDeadlineChild$", "--", "ask-owned-deadline-child", marker)
	if ok || ctx.Err() != context.DeadlineExceeded || time.Since(start) > 2*time.Second {
		t.Fatal("common deadline not enforced")
	}
	raw, e := os.ReadFile(marker)
	if e != nil {
		t.Fatal("owned child did not start", e)
	}
	pid, e := strconv.Atoi(string(raw))
	if e != nil {
		t.Fatal(e)
	}
	h, e := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if e == nil {
		defer syscall.CloseHandle(h)
		state, e := syscall.WaitForSingleObject(h, 0)
		if e != nil || state != syscall.WAIT_OBJECT_0 {
			t.Fatal("child still active")
		}
	}
	b := askSoftwareBoundaryContext(ctx, compiledAskPlan)
	r := executeAskSoftware(true, compiledAskPlan, b)
	if r.Qualified || r.Prepared || len(r.Created) != 0 {
		t.Fatal("expired operation created leaves")
	}
	for _, leaf := range []string{"stage.exe", "backup.exe"} {
		if _, e := os.Stat(filepath.Join(root, leaf)); !os.IsNotExist(e) {
			t.Fatal("owned leaf state unknown")
		}
	}
	t.Logf("deadline_ms=300 elapsed_ms=%.3f child_started=1 child_reaped=true qualified=false prepared=false created=0 owned_stage_absent=true owned_backup_absent=true", float64(time.Since(start))/float64(time.Millisecond))
}

func TestAskBoundedGitOutputAndCanceledHashWork(t *testing.T) {
	b := askGitOutput{limit: 4}
	if n, e := b.Write([]byte("abcd")); n != 4 || e != nil {
		t.Fatal(n, e)
	}
	if n, e := b.Write([]byte("e")); n != 0 || e == nil || b.Len() != 4 {
		t.Fatal("output limit failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if askCandidateMatchesContext(ctx, nil, nil, askSoftwarePlan{}, func(...string) ([]byte, bool) { t.Fatal("canceled work reached git"); return nil, false }) {
		t.Fatal("canceled source accepted")
	}
}

func TestAskPurposeFixedObjectsPartialFailureAndNoDataGrant(t *testing.T) {
	for _, fault := range []string{"verify", "prepare", "old", "identity", "candidate", "proof", "acl", "collision", "backup_write", "stage_write"} {
		t.Run(fault, func(t *testing.T) {
			old := &fakeUpdateObject{raw: make([]byte, askOldBytes), id: softwareIdentity{askOldVolume, askOldID, askOldCreated}, acl: "owned-acl"}
			proof := &fakeUpdateObject{raw: []byte("proof")}
			candidate := &fakeUpdateObject{raw: []byte("candidate")}
			plan := askSoftwarePlan{Candidate: "owned-candidate", Proof: "owned-proof", Source: "fixed-source", SHA: "candidate-sha", Bytes: 9}
			writes := []string{}
			objects := []*fakeUpdateObject{}
			old.write = func(path string, raw []byte, acl string) (softwareIdentity, error) {
				writes = append(writes, path)
				want := old.raw
				if path == askStage {
					want = candidate.raw
				}
				if path != askStage && path != askBackup || !bytes.Equal(raw, want) || acl != old.acl {
					t.Fatal("purpose or exact bytes changed")
				}
				id := softwareIdentity{"owned", path, "1"}
				if fault == "backup_write" && path == askBackup || fault == "stage_write" && path == askStage {
					return id, errors.New("owned IO fault")
				}
				return id, nil
			}
			b := updateBoundary{open: func(path string) (updateObject, error) {
				var o *fakeUpdateObject
				switch path {
				case askTarget:
					o = old
				case plan.Proof:
					if fault == "proof" {
						return nil, errors.New("unavailable")
					}
					o = proof
				case plan.Candidate:
					o = candidate
				default:
					t.Fatal("purpose outside fixed objects")
				}
				objects = append(objects, o)
				return o, nil
			}, absent: func(string) bool { return fault != "collision" }, oldMatches: func([]byte) bool { return fault != "old" }, candidateMatches: func(a, b []byte) bool {
				return fault != "candidate" && bytes.Equal(a, candidate.raw) && bytes.Equal(b, proof.raw)
			}}
			if fault == "identity" {
				old.id.FileID = "other"
			}
			old.failACL = fault == "acl"
			r := executeAskSoftware(fault != "verify", plan, b)
			if r.DataAccessGranted || r.UpdatePerformed || r.SoftwareReplaceAllowed {
				t.Fatal("data/replacement grant")
			}
			for _, o := range objects {
				if !o.closed {
					t.Fatal("handle leaked")
				}
			}
			switch fault {
			case "verify":
				if !r.Qualified || r.Prepared || len(writes) != 0 {
					t.Fatal(r)
				}
			case "prepare":
				if !r.Qualified || !r.Prepared || len(writes) != 2 {
					t.Fatal(r)
				}
			case "backup_write", "stage_write":
				if r.Qualified || r.Prepared || r.Reason != "prepare_incomplete_preserved" || len(r.Created) == 0 {
					t.Fatal(r)
				}
			default:
				if r.Qualified || len(writes) != 0 {
					t.Fatal("invalid source reached creation")
				}
			}
		})
	}
}

func TestAskCandidateRequiresExactProofSourceAndNoCallerPath(t *testing.T) {
	for _, path := range []string{"other/data", "cc-ask/../secret", "cc-ask//file", "C:/other", "cc-ask\\file"} {
		if askSourcePath(path) {
			t.Fatal(path)
		}
	}
	if askCandidateMatches([]byte("unverified"), nil, askSoftwarePlan{}, func(...string) ([]byte, bool) { t.Fatal("bad bytes reached git"); return nil, false }) {
		t.Fatal("unfinished pins accepted")
	}
	for _, args := range [][]string{{"--verify-ask-privacy-update", "D:/caller"}, {"--prepare-ask-privacy-update=true"}, {"--guard-path", askTarget}} {
		calls := 0
		op := func() updateResult { calls++; return updateResult{Qualified: true, Prepared: true} }
		var out bytes.Buffer
		if run(args, &out, func() softwareObservation { calls++; return softwareObservation{} }, updateOperations{verify: op, prepare: op}) != 2 || calls != 0 {
			t.Fatal("caller changed purpose")
		}
	}
	var out bytes.Buffer
	r := updateResult{Qualified: false, Prepared: false, Reason: "prepare_incomplete_preserved", Created: []string{askBackup}, BackupIdentity: &softwareIdentity{FileID: "unknown_created"}}
	if runAskSoftware([]string{"--prepare-ask-privacy-update"}, &out, updateOperations{prepare: func() updateResult { return r }}) != 1 {
		t.Fatal("partial falsely succeeded")
	}
	var got updateResult
	if json.Unmarshal(out.Bytes(), &got) != nil || got.Qualified || got.BackupIdentity.FileID != "unknown_created" {
		t.Fatal("partial state lost")
	}
}

func TestAskCandidateProofMatchesMergedInputBlobsAndRejectsChanges(t *testing.T) {
	for _, fault := range []string{"ok", "hash", "proof", "source", "main", "blob", "sha", "duplicate", "missing", "unsafe"} {
		t.Run(fault, func(t *testing.T) {
			raw := []byte("owned PE fixture")
			blob := []byte("owned source\n")
			source := strings.Repeat("1", 40)
			h := sha1.New()
			fmt.Fprintf(h, "blob %d\x00", len(blob))
			h.Write(blob)
			id := hex.EncodeToString(h.Sum(nil))
			proof := askSoftwareProof{Head: source, Candidate: "candidate", SHA: digest(raw), Bytes: len(raw), Sources: []buildSourceRecord{{Path: "cc-ask/src/main.rs", Blob: id, SHA: digest(blob)}}}
			switch fault {
			case "source":
				proof.Head = strings.Repeat("3", 40)
			case "sha":
				proof.Sources[0].SHA = strings.Repeat("0", 64)
			case "duplicate":
				proof.Sources = append(proof.Sources, proof.Sources[0])
			case "missing":
				proof.Sources = nil
			case "unsafe":
				proof.Sources[0].Path = "cc-ask/../other"
			}
			encoded, _ := json.Marshal(proof)
			plan := askSoftwarePlan{Candidate: "candidate", Source: source, SHA: digest(raw), ProofSHA: digest(encoded), Bytes: len(raw)}
			if fault == "hash" {
				plan.SHA = strings.Repeat("0", 64)
			}
			if fault == "proof" {
				plan.ProofSHA = strings.Repeat("0", 64)
			}
			git := func(args ...string) ([]byte, bool) {
				switch args[0] {
				case "merge-base":
					return nil, true
				case "ls-tree":
					if fault == "main" && args[3] == "refs/remotes/origin/main" {
						return []byte("changed"), true
					}
					return []byte("100644 blob " + id + "\tcc-ask/src/main.rs\x00"), true
				case "archive":
					if fault == "blob" {
						blob = []byte("other")
					}
					var out bytes.Buffer
					writer := tar.NewWriter(&out)
					_ = writer.WriteHeader(&tar.Header{Name: "cc-ask/src/main.rs", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(blob))})
					_, _ = writer.Write(blob)
					_ = writer.Close()
					return out.Bytes(), true
				}
				t.Fatal("unexpected git command")
				return nil, false
			}
			if got := askCandidateMatches(raw, encoded, plan, git); got != (fault == "ok") {
				t.Fatalf("accepted=%v fault=%s", got, fault)
			}
		})
	}
}
