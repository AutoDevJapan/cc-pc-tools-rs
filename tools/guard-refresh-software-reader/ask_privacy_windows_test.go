//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

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
			id := strings.Repeat("2", 40)
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
				case "cat-file":
					if fault == "blob" {
						return []byte("other"), true
					}
					return blob, true
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
