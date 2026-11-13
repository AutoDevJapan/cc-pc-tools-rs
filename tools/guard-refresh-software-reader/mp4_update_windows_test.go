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
	"strings"
	"testing"
)

func TestMP4AskReviewedMergeRemainsBoundWhenMainAdvances(t *testing.T) {
	for _, fault := range []string{"normal", "merge_not_ancestor", "source_not_ancestor", "history_tree_changed", "archive_changed", "wrong_fixed_artifact", "wrong_history_merge", "legacy_main_changed"} {
		t.Run(fault, func(t *testing.T) {
			plan := mp4AskPlan
			body := []byte("owned immutable source\n")
			h := sha1.New()
			fmt.Fprintf(h, "blob %d\x00", len(body))
			h.Write(body)
			blob := hex.EncodeToString(h.Sum(nil))
			rawArtifact := []byte("owned artifact")
			proof := askSoftwareProof{Head: plan.Source, Candidate: plan.Candidate, SHA: digest(rawArtifact), Bytes: len(rawArtifact), Sources: []buildSourceRecord{{Path: "cc-ask/src/main.rs", Blob: blob, SHA: digest(body)}}}
			encoded, _ := json.Marshal(proof)
			sourcePlan := plan
			sourcePlan.SHA, sourcePlan.ProofSHA, sourcePlan.Bytes = digest([]byte("owned artifact")), digest(encoded), len("owned artifact")
			// Exercise the immutable comparison selector separately with its exact
			// completed-artifact pins; the shared validator still checks all bytes.
			if fault == "wrong_fixed_artifact" {
				plan.SHA = strings.Repeat("0", 64)
			}
			if fault == "wrong_history_merge" {
				plan.VerifiedMerge = strings.Repeat("f", 40)
			}
			if fault == "legacy_main_changed" {
				plan.VerifiedMerge = ""
			}
			calls := 0
			comparison, ok := askSourceComparison(plan, func(args ...string) ([]byte, bool) {
				calls++
				if strings.Join(args, "\x00") == strings.Join([]string{"merge-base", "--is-ancestor", plan.Source, plan.VerifiedMerge}, "\x00") {
					return nil, fault != "source_not_ancestor"
				}
				if strings.Join(args, "\x00") == strings.Join([]string{"merge-base", "--is-ancestor", plan.VerifiedMerge, "refs/remotes/origin/main"}, "\x00") {
					return nil, fault != "merge_not_ancestor"
				}
				t.Fatal("unexpected history command")
				return nil, false
			})
			want := fault == "normal" || fault == "history_tree_changed" || fault == "archive_changed" || fault == "legacy_main_changed"
			if ok != want {
				t.Fatal("history selector outcome", fault, ok)
			}
			if !ok {
				return
			}
			if fault == "legacy_main_changed" {
				if comparison != "refs/remotes/origin/main" || calls != 0 {
					t.Fatal("legacy current-main contract changed")
				}
			} else if comparison != "60bf5f02db14813a8fe6a1c16ca780b9e9048cef" || calls != 2 {
				t.Fatal("history merge not pinned")
			}
			var archive bytes.Buffer
			w := tar.NewWriter(&archive)
			_ = w.WriteHeader(&tar.Header{Name: "cc-ask/src/main.rs", Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(body))})
			archivedBody := append([]byte(nil), body...)
			if fault == "archive_changed" {
				archivedBody[0] = 'X'
			}
			_, _ = w.Write(archivedBody)
			_ = w.Close()
			trees := 0
			archiveCalls := 0
			valid := askCandidateMatchesWithReference(context.Background(), rawArtifact, encoded, sourcePlan, func(args ...string) ([]byte, bool) {
				switch args[0] {
				case "merge-base":
					if strings.Join(args, "\x00") != strings.Join([]string{"merge-base", "--is-ancestor", sourcePlan.Source, "refs/remotes/origin/main"}, "\x00") {
						t.Fatal("source ancestry arguments changed")
					}
					return nil, true
				case "ls-tree":
					trees++
					if trees == 2 && args[3] != comparison {
						t.Fatal("validator did not consume selected reference")
					}
					if trees == 2 && (fault == "history_tree_changed" || fault == "legacy_main_changed") {
						return []byte("changed tree"), true
					}
					return []byte("100644 blob " + blob + "\tcc-ask/src/main.rs\x00"), true
				case "archive":
					archiveCalls++
					return archive.Bytes(), true
				}
				t.Fatal("unexpected proof command")
				return nil, false
			}, func(askSoftwarePlan, func(...string) ([]byte, bool)) (string, bool) { return comparison, true })
			if valid != (fault == "normal") {
				t.Fatal("history tree/archive accepted mismatched source", fault, valid)
			}
			if fault == "normal" && archiveCalls != 1 || (fault == "history_tree_changed" || fault == "legacy_main_changed") && archiveCalls != 0 {
				t.Fatal("source stage order changed")
			}
		})
	}
}

func TestProjectDistributionPinsMergedHistoryWithoutReusingOldBuild(t *testing.T) {
	const source = "3a69da420e2797c501eb40dd3c660394f8b7cb3b"
	if mp4Source != source || privacyBuildPlan().Source == source {
		t.Fatal("new project source replaced the prior privacy purpose")
	}
	for _, kind := range []string{"ask", "runner"} {
		plan, baseline, ok := mp4Software(kind)
		build, built := mp4BuildPlan(kind)
		if !ok || !built || plan.Source != source || build.Source != source || build.Export != plan.Candidate || !strings.Contains(build.Target, "-project-3a69-build") || kind == "ask" && !strings.HasSuffix(build.Target, "-build-v7") || strings.Contains(build.Export, "cfecca") || !strings.Contains(baseline.Stage, ".project3a69.stage.exe") || !strings.Contains(baseline.Backup, ".project3a69.before.exe") {
			t.Fatal("old generation was adopted or fixed namespaces diverged")
		}
		wantInputs, wantProof := 209, "956ad08d964b9cdf710698e429c606279e0f93cf766a52a58a989a9baf9fba7f"
		if kind == "runner" {
			wantInputs, wantProof = 183, "3863066939e5110b0014029c5559ad1c11e6a094695ca34620d2c825803598c7"
		}
		if build.Inputs != wantInputs || build.ProofSHA != wantProof {
			t.Fatal("reviewed source set does not match its purpose")
		}
		if mp4PlanReady(plan, baseline) != (kind == "ask") {
			t.Fatal("only the completed ask producer may enable preparation")
		}
		if kind == "ask" && (plan.SHA != "0a693fa7013d733b3e7fbb009227cb37f4e2705e1d087ab40a4f5bf8857e9747" || plan.ProofSHA != "85dfadb0b549d4a9970c24ee4c1b11c7cd2d4b59019ce3c8bdbc92709ac9ddeb" || plan.Bytes != 13560320) {
			t.Fatal("completed artifact or source proof changed")
		}
	}
}

func TestMP4FixedNamespaceAndOldPrivacyRemainSeparate(t *testing.T) {
	for _, kind := range []string{"ask", "runner"} {
		plan, baseline, ok := mp4Software(kind)
		if !ok || plan.Source != mp4Source || baseline.Stage == askStage || baseline.Backup == askBackup || !sameParentPath(baseline.Target, baseline.Stage) || !sameParentPath(baseline.Target, baseline.Backup) {
			t.Fatal("wrong fixed software namespace")
		}
		if kind == "runner" && (!plan.GoRunner || askPlanSourcePath("cc-ask/src/main.rs", plan)) {
			t.Fatal("Go source admits Rust or wrong compiled scope")
		}
		if askPlanSourcePath("../records/private.json", plan) || askPlanSourcePath("unattended-runner/../records/private.json", plan) {
			t.Fatal("data path admitted")
		}
	}
	if _, _, ok := mp4Software("other"); ok {
		t.Fatal("caller selected third target")
	}
	var out bytes.Buffer
	if code := runMP4Software("--verify-mp4-other-update", &out); code != 2 || out.Len() != 0 {
		t.Fatal("unknown flag admitted")
	}
}

func TestMP4BuildPurposeRejectsUnboundSourceBeforeAnyOutput(t *testing.T) {
	for _, kind := range []string{"ask", "runner"} {
		build, ok := mp4BuildPlan(kind)
		if !ok || build.Source != mp4Source || build.Export == "" || build.Inputs < 1 || build.GoRunner != (kind == "runner") || build.Target == askBuildOwnedTarget || build.Receipt == askBuildReceipt {
			t.Fatal("wrong compiled producer scope")
		}
		calls := 0
		if softwareBuildSourceMatches(context.Background(), []byte(`{"Head":"forged"}`), build, func(...string) ([]byte, bool) { calls++; return nil, false }) || calls != 0 {
			t.Fatal("unbound proof reached Git or production outputs")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if softwareBuildSourceMatches(ctx, nil, build, func(...string) ([]byte, bool) { t.Fatal("canceled source lookup"); return nil, false }) {
			t.Fatal("expired proof admitted")
		}
		if mp4PlanReady(askSoftwarePlan{}, softwareBaseline{}) {
			t.Fatal("unbound final candidate permitted prepare")
		}
	}
	if _, ok := mp4BuildPlan("other"); ok {
		t.Fatal("unknown producer")
	}
}

func TestMP4SoftwareControllerKeepsSameBytesACLAndPartialFailure(t *testing.T) {
	for _, kind := range []string{"ask", "runner"} {
		for _, fault := range []string{"normal", "wrong_old_id", "bad_candidate", "collision", "stage_failure"} {
			t.Run(kind+"/"+fault, func(t *testing.T) {
				plan, baseline, _ := mp4Software(kind)
				baseline.Bytes, baseline.SHA = 3, digest([]byte("old"))
				baseline.Identity = softwareIdentity{"1", "2", "3"}
				plan.SHA = digest([]byte("new"))
				old := &fakeUpdateObject{raw: []byte("old"), id: baseline.Identity, acl: "owned-acl"}
				if fault == "wrong_old_id" {
					old.id.FileID = "other"
				}
				var writes []string
				old.write = func(path string, data []byte, acl string) (softwareIdentity, error) {
					writes = append(writes, path)
					if acl != "owned-acl" || path == baseline.Backup && !bytes.Equal(data, []byte("old")) || path == baseline.Stage && !bytes.Equal(data, []byte("new")) {
						t.Fatal("held bytes or ACL changed")
					}
					id := softwareIdentity{"1", "4", "5"}
					if path == baseline.Stage && fault == "stage_failure" {
						return id, errors.New("owned partial failure")
					}
					return id, nil
				}
				b := updateBoundary{open: func(path string) (updateObject, error) {
					if path == baseline.Target {
						return old, nil
					}
					if path == plan.Candidate {
						return &fakeUpdateObject{raw: []byte("new")}, nil
					}
					return &fakeUpdateObject{raw: []byte("owned-proof")}, nil
				}, absent: func(string) bool { return fault != "collision" }, oldMatches: func(raw []byte) bool { return bytes.Equal(raw, []byte("old")) }, candidateMatches: func([]byte, []byte) bool { return fault != "bad_candidate" }}
				r := executeSoftwareContext(context.Background(), true, plan, baseline, b)
				if fault == "normal" {
					if !r.Qualified || !r.Prepared || len(writes) != 2 || r.Target != baseline.Target || r.Stage != baseline.Stage || r.Backup != baseline.Backup || r.CurrentSHA != baseline.SHA {
						t.Fatal("normal prepare failed", r)
					}
				} else if fault == "stage_failure" {
					if r.Qualified || r.Prepared || len(r.Created) != 2 || r.StageIdentity == nil || r.BackupIdentity == nil || r.Reason != "prepare_incomplete_preserved" {
						t.Fatal("partial preparation lost", r)
					}
				} else if r.Qualified || len(writes) != 0 {
					t.Fatal("unqualified prepare mutated", r)
				}
			})
		}
	}
}

func TestMP4BaselineReadbackOnlyWhenExplicitlyRequested(t *testing.T) {
	if os.Getenv("FOURBROS_MP4_BASELINE_READBACK") != "1" {
		t.Skip("explicit fixed-software metadata readback only")
	}
	for _, kind := range []string{"ask", "runner"} {
		_, baseline, _ := mp4Software(kind)
		o, err := openUpdateObject(baseline.Target)
		if err != nil {
			t.Fatal("cannot pin fixed current software")
		}
		id := o.ID()
		acl, err := o.ACL()
		if err != nil || digest(o.Raw()) != baseline.SHA || len(o.Raw()) != baseline.Bytes {
			o.Close()
			t.Fatal("fixed current software changed")
		}
		t.Logf("kind=%s volume=%s file_id=%s created=%s bytes=%d sha=%s acl_sha=%s", kind, id.Volume, id.FileID, id.Created, len(o.Raw()), digest(o.Raw()), digest([]byte(acl)))
		if err := o.Close(); err != nil {
			t.Fatal("current software close failed")
		}
	}
}
