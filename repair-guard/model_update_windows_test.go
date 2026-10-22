package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestModelUpdateCLIHasPurposeSpecificFailure(t *testing.T) {
	// A synthetic path: the missing update reader must not fall through to
	// general data classification or report ordinary success for an unknown leaf.
	var out, err bytes.Buffer
	code := runGuardCLI([]string{"--verify-model-runtime-update", "C:/synthetic-3177/unknown.exe"}, &out, &err, func(int) object {
		t.Fatal("update purpose reached the ToDo verifier")
		return nil
	})
	if code != 1 || out.String() != "{\"qualified\":false}\n" || err.Len() != 0 {
		t.Fatalf("missing purpose reader: exit=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
}

func TestModelUpdateHelpAddsPurposeWithoutChangingHelpContract(t *testing.T) {
	var out, err bytes.Buffer
	code := runGuardCLI([]string{"--help"}, &out, &err, func(int) object { return nil })
	if code != 0 || out.Len() != 0 || !strings.Contains(err.String(), "Usage of scrub391:") || !strings.Contains(err.String(), "-verify-model-runtime-update string") {
		t.Fatal("model update purpose is missing from existing stderr help")
	}
}

type modelUpdateFixture struct {
	consoleFixture
	candidate, buildReceipt string
	leaf                    modelUpdateLeaf
}

func newModelUpdateFixture(t *testing.T) modelUpdateFixture {
	t.Helper()
	base := newConsoleFixture(t)
	f := modelUpdateFixture{consoleFixture: base, candidate: filepath.Join(base.root, "new.exe"), buildReceipt: filepath.Join(base.root, "build.json")}
	for path, body := range map[string]string{f.candidate: "new model software", f.buildReceipt: "independent reviewed build"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.leaf = modelUpdateLeaf{kind: "fixture", target: f.consoleFixture.leaf, baselines: base.leaves[0].pins, candidate: modelUpdateCandidate{
		root: f.root, path: f.candidate, head: strings.Repeat("a", 40), sha256: hashBytes([]byte("new model software")),
		evidence: []consoleEvidence{{f.root, f.buildReceipt, hashBytes([]byte("independent reviewed build"))}},
	}}
	return f
}

func (f modelUpdateFixture) result(path string) object {
	return modelRuntimeUpdateWith(path, []modelUpdateLeaf{f.leaf}, func(string) bool { return false }, func(root string) bool { return same(root, f.root) }, func(root, head string) bool { return same(root, f.root) && head == strings.Repeat("a", 40) })
}

func requireModelUpdateRefused(t *testing.T, result object) {
	t.Helper()
	if result["qualified"] != false {
		t.Fatalf("unexpected software qualification: %v", result)
	}
}

func TestModelUpdateBindsBaselineCandidateAndIdempotentCurrent(t *testing.T) {
	f := newModelUpdateFixture(t)
	result := f.result(f.leaf.target)
	if result["qualified"] != true || result["todo_id"] != 3081 || result["data_access_granted"] != false ||
		result["current_sha256"] != f.leaf.baselines[0].sha256 || result["new_sha256"] != f.leaf.candidate.sha256 || result["already_current"] != false {
		t.Fatal("known baseline/new candidate/build proof rejected or broadened")
	}
	if err := os.WriteFile(f.leaf.target, []byte("new model software"), 0600); err != nil {
		t.Fatal(err)
	}
	result = f.result(f.leaf.target)
	if result["qualified"] != true || result["already_current"] != true || result["current_sha256"] != f.leaf.candidate.sha256 {
		t.Fatal("already-installed reviewed candidate not distinguished")
	}
	if err := os.WriteFile(f.leaf.target, []byte("parallel unrelated distribution"), 0600); err != nil {
		t.Fatal(err)
	}
	requireModelUpdateRefused(t, f.result(f.leaf.target))
}

func TestModelUpdateUnboundCandidateReportsOnlyObservedBaseline(t *testing.T) {
	f := newModelUpdateFixture(t)
	f.leaf.candidate = modelUpdateCandidate{}
	result := f.result(f.leaf.target)
	if result["qualified"] != false || result["baseline_verified"] != true || result["reason"] != "candidate_unbound" || result["current_sha256"] != f.leaf.baselines[0].sha256 || result["data_access_granted"] != false {
		t.Fatal("unbuilt candidate either granted an update or lost its bounded observation")
	}
	if _, ok := result["new_sha256"]; ok {
		t.Fatal("unbuilt candidate SHA invented")
	}
	var out bytes.Buffer
	if code := runModel3081CLI([]string{"--verify-model-runtime-update", f.leaf.target}, &out, f.result); code != 1 || !strings.Contains(out.String(), `"baseline_verified":true`) {
		t.Fatal("CLI reported update success for baseline-only observation")
	}
	if err := os.WriteFile(f.leaf.target, []byte("parallel distribution"), 0600); err != nil {
		t.Fatal(err)
	}
	result = f.result(f.leaf.target)
	if len(result) != 1 || result["qualified"] != false {
		t.Fatal("unknown current bytes exposed an observed baseline or hash")
	}
}

func TestModelUpdateRejectsScopeOutsideExactLeaf(t *testing.T) {
	f := newModelUpdateFixture(t)
	if f.result(f.leaf.target)["qualified"] != true {
		t.Fatal("positive control rejected")
	}
	for _, path := range []string{f.root, filepath.Dir(f.leaf.target), f.leaf.target + ".bak", f.leaf.target + ":stream", f.candidate, f.receipt, f.buildReceipt, filepath.Join(filepath.Dir(f.leaf.target), "sibling.exe"), filepath.Join(f.root, "private.json")} {
		requireModelUpdateRefused(t, f.result(path))
	}
	if len(model3081UpdateLeaves) != 4 {
		t.Fatal("production update scope expanded")
	}
	bound := 0
	for _, leaf := range model3081UpdateLeaves {
		if len(leaf.baselines) == 0 {
			t.Fatal("independent installed provenance omitted")
		}
		if leaf.kind == "todo" {
			bound++
			if leaf.candidate.root != model3298Source || leaf.candidate.head != model3298Head ||
				!within(leaf.candidate.root, leaf.candidate.path) || filepath.Base(leaf.candidate.path) != "todo.exe" ||
				!modelDigestValid(leaf.candidate.sha256) || len(leaf.candidate.evidence) != 2 ||
				!modelCandidateEvidenceIndependent(leaf.candidate, leaf) {
				t.Fatal("built ToDo candidate lost its fixed source or independent receipts")
			}
		} else if leaf.kind == "jobs" {
			bound++
			if leaf.target != jobsRuntime || leaf.candidate.root != model3309Source ||
				!modelDigestValid(leaf.candidate.sha256) || len(leaf.candidate.evidence) != 2 ||
				!modelCandidateEvidenceIndependent(leaf.candidate, leaf) {
				t.Fatal("launcher repair lost its exact independent provenance")
			}
		} else if leaf.kind == "guard" {
			bound++
			if leaf.candidate.root != model3344GuardSource || !modelDigestValid(leaf.candidate.sha256) ||
				len(leaf.candidate.evidence) != 1 || !modelCandidateEvidenceIndependent(leaf.candidate, leaf) {
				t.Fatal("guard A lost its separate fixed source and build receipt")
			}
		} else if leaf.candidate.path != "" || leaf.candidate.sha256 != "" || leaf.candidate.root != "" || leaf.candidate.head != "" || len(leaf.candidate.evidence) != 0 {
			t.Fatal("another production leaf was bound without this build's provenance")
		}
		if leaf.kind != "todo" && leaf.kind != "discord" && leaf.kind != "guard" && leaf.kind != "jobs" {
			t.Fatal("unknown production kind")
		}
		for _, path := range []string{filepath.Dir(leaf.target), leaf.target + ".bak", leaf.target + ":stream", filepath.Join(filepath.Dir(leaf.target), "state.json")} {
			requireModelUpdateRefused(t, model3081RuntimeReceipt(path))
		}
	}
	if bound != 3 {
		t.Fatal("expected the verified ToDo, launcher and separate guard A candidates")
	}
	requireModelUpdateRefused(t, model3081RuntimeReceipt("D:/CC/Tools/civ6-live/review.exe"))
}

func TestModelUpdateLauncherQualificationIsSoftwareOnlyAndExact(t *testing.T) {
	good := func(string) object {
		return object{"qualified": true, "domain": "model-runtime-update-3081-v1", "todo_id": 3081, "target_kind": "jobs", "data_access_granted": false}
	}
	if !modelLauncherQualified(jobsRuntime, good) {
		t.Fatal("qualified exact launcher refused")
	}
	for _, path := range []string{filepath.Dir(jobsRuntime), jobsRuntime + ".bak", jobsRuntime + ":stream", "D:/CC/Tools/todo/todo.exe"} {
		if modelLauncherQualified(path, good) {
			t.Fatal("software qualification escaped the exact launcher leaf")
		}
	}
	for _, key := range []string{"qualified", "domain", "todo_id", "target_kind", "data_access_granted"} {
		if modelLauncherQualified(jobsRuntime, func(path string) object {
			r := good(path)
			delete(r, key)
			return r
		}) {
			t.Fatalf("missing %s accepted", key)
		}
	}
}

func TestModelUpdateRejectsUnpinnedOrUnverifiedCandidate(t *testing.T) {
	for _, change := range []string{"missing candidate SHA", "uppercase SHA", "invalid SHA", "missing build proof", "missing head", "wrong source head", "outside root", "pin without old proof", "candidate is proof", "baseline is proof", "reused old receipt"} {
		t.Run(change, func(t *testing.T) {
			f := newModelUpdateFixture(t)
			switch change {
			case "missing candidate SHA":
				f.leaf.candidate.sha256 = ""
			case "uppercase SHA":
				f.leaf.candidate.sha256 = strings.ToUpper(f.leaf.candidate.sha256)
			case "invalid SHA":
				f.leaf.candidate.sha256 = strings.Repeat("g", 64)
			case "missing build proof":
				f.leaf.candidate.evidence = nil
			case "missing head":
				f.leaf.candidate.head = ""
			case "wrong source head":
				f.leaf.candidate.head = strings.Repeat("b", 40)
			case "outside root":
				f.leaf.candidate.root = filepath.Join(f.root, "other")
			case "pin without old proof":
				f.leaf.baselines[0].evidence = nil
			case "candidate is proof":
				f.leaf.candidate.evidence = []consoleEvidence{{f.root, f.candidate, f.leaf.candidate.sha256}}
			case "baseline is proof":
				f.leaf.baselines[0].evidence = []consoleEvidence{{f.root, f.leaf.target, f.leaf.baselines[0].sha256}}
			case "reused old receipt":
				f.leaf.candidate.evidence = f.leaf.baselines[0].evidence
			}
			requireModelUpdateRefused(t, f.result(f.leaf.target))
		})
	}
}

func TestModelUpdateRejectsMissingOrChangedReceiptTargetAndCandidate(t *testing.T) {
	for _, name := range []string{"baseline receipt", "build receipt", "target", "candidate"} {
		for _, action := range []string{"missing", "changed"} {
			t.Run(name+"/"+action, func(t *testing.T) {
				f := newModelUpdateFixture(t)
				path := map[string]string{"baseline receipt": f.receipt, "build receipt": f.buildReceipt, "target": f.leaf.target, "candidate": f.candidate}[name]
				var err error
				if action == "missing" {
					err = os.Remove(path)
				} else {
					err = os.WriteFile(path, []byte("changed bytes"), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				requireModelUpdateRefused(t, f.result(f.leaf.target))
			})
		}
	}
}

func TestModelUpdatePreservesOriginalProtectionAndOrigin(t *testing.T) {
	f := newModelUpdateFixture(t)
	for _, path := range []string{f.root, f.leaf.target, filepath.Dir(f.leaf.target), f.receipt, f.buildReceipt, f.candidate} {
		requireModelUpdateRefused(t, modelRuntimeUpdateWith(f.leaf.target, []modelUpdateLeaf{f.leaf}, func(p string) bool { return same(p, path) }, func(string) bool { return true }, func(string, string) bool { return true }))
	}
	requireModelUpdateRefused(t, modelRuntimeUpdateWith(f.leaf.target, []modelUpdateLeaf{f.leaf}, func(string) bool { return false }, func(string) bool { return false }, func(string, string) bool { return true }))
	requireModelUpdateRefused(t, modelRuntimeUpdateWith(f.leaf.target, []modelUpdateLeaf{f.leaf}, func(string) bool { return false }, func(string) bool { return true }, func(string, string) bool { return false }))
}

func TestModelUpdateRequiresActualGitRootOriginAndCandidateHead(t *testing.T) {
	f := newModelUpdateFixture(t)
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", f.root}, args...)...)
		command.Env = civ6GitEnvironment(os.Environ())
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git failed: %v %s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q")
	git("remote", "add", "origin", "https://github.com/AutoDevJapan/cc-pc-tools-rs.git")
	git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=absent-fixture-hooks", "commit", "--allow-empty", "-m", "synthetic build")
	f.leaf.candidate.head = git("rev-parse", "HEAD")
	result := func() object {
		return modelRuntimeUpdateWith(f.leaf.target, []modelUpdateLeaf{f.leaf}, func(string) bool { return false }, consoleSourceVerified, modelSourceHead)
	}
	if result()["qualified"] != true {
		t.Fatal("actual source root/origin/head positive control rejected")
	}
	t.Setenv("GIT_DIR", filepath.Join(f.root, "unrelated-git"))
	if result()["qualified"] != true {
		t.Fatal("inherited Git override redirected the source check")
	}
	f.leaf.candidate.root = filepath.Dir(f.leaf.target)
	f.leaf.candidate.path = filepath.Join(f.leaf.candidate.root, "new.exe")
	if err := os.WriteFile(f.leaf.candidate.path, []byte("new model software"), 0600); err != nil {
		t.Fatal(err)
	}
	requireModelUpdateRefused(t, result())
	f.leaf.candidate.root, f.leaf.candidate.path = f.root, f.candidate
	git("remote", "set-url", "origin", "https://github.com/AutoDevJapan/unrelated.git")
	requireModelUpdateRefused(t, result())
	git("remote", "set-url", "origin", "https://github.com/AutoDevJapan/cc-pc-tools-rs.git")
	git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=absent-fixture-hooks", "commit", "--allow-empty", "-m", "source advanced")
	requireModelUpdateRefused(t, result())
}

func TestModelUpdateAlreadyCurrentStillRequiresOldIndependentProof(t *testing.T) {
	f := newModelUpdateFixture(t)
	if err := os.WriteFile(f.leaf.target, []byte("new model software"), 0600); err != nil {
		t.Fatal(err)
	}
	if f.result(f.leaf.target)["qualified"] != true {
		t.Fatal("idempotent positive control rejected")
	}
	if err := os.Remove(f.receipt); err != nil {
		t.Fatal(err)
	}
	requireModelUpdateRefused(t, f.result(f.leaf.target))
}

func TestModelUpdateRefusesHardlinksSymlinksAndJunction(t *testing.T) {
	for _, name := range []string{"target", "candidate", "baseline receipt", "build receipt"} {
		for _, kind := range []string{"hardlink", "symlink"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				f := newModelUpdateFixture(t)
				path := map[string]string{"target": f.leaf.target, "candidate": f.candidate, "baseline receipt": f.receipt, "build receipt": f.buildReceipt}[name]
				alias := filepath.Join(f.root, "alias")
				if kind == "hardlink" {
					if err := os.Link(path, alias); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Rename(path, alias); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(alias, path); err != nil {
						t.Fatal(err)
					}
				}
				requireModelUpdateRefused(t, f.result(f.leaf.target))
			})
		}
	}
	f := newModelUpdateFixture(t)
	link := filepath.Join(f.root, "junction")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Dir(f.leaf.target))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("junction fixture failed: %v %s", err, out)
	}
	defer os.Remove(link)
	f.leaf.target = filepath.Join(link, filepath.Base(f.leaf.target))
	requireModelUpdateRefused(t, f.result(f.leaf.target))
}

func TestModelUpdateRejectsExistingConcurrentWriters(t *testing.T) {
	for _, name := range []string{"target", "candidate", "baseline receipt", "build receipt"} {
		t.Run(name, func(t *testing.T) {
			f := newModelUpdateFixture(t)
			path := map[string]string{"target": f.leaf.target, "candidate": f.candidate, "baseline receipt": f.receipt, "build receipt": f.buildReceipt}[name]
			ptr, err := syscall.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			h, err := syscall.CreateFile(ptr, syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			requireModelUpdateRefused(t, f.result(f.leaf.target))
			if err := syscall.CloseHandle(h); err != nil {
				t.Fatal(err)
			}
			if f.result(f.leaf.target)["qualified"] != true {
				t.Fatal("positive control after writer close rejected")
			}
		})
	}
}

func TestModelUpdateKeepsAllHandlesAndRechecksSourceAndProtection(t *testing.T) {
	for _, change := range []string{"none", "source", "protection", "head"} {
		t.Run(change, func(t *testing.T) {
			f := newModelUpdateFixture(t)
			seen := false
			locked := true
			sourceCalls := 0
			// One initial check per root, then the final check while every
			// receipt, candidate and target handle remains pinned together.
			source := func(string) bool {
				sourceCalls++
				if sourceCalls == 2 {
					seen = true
					for _, path := range []string{f.receipt, f.buildReceipt, f.candidate, f.leaf.target} {
						if err := os.WriteFile(path, []byte("must not replace pinned bytes"), 0600); err == nil {
							locked = false
						}
					}
				}
				return !(seen && change == "source")
			}
			result := modelRuntimeUpdateWith(f.leaf.target, []modelUpdateLeaf{f.leaf}, func(string) bool { return seen && change == "protection" }, source, func(string, string) bool { return !(seen && change == "head") })
			if !seen || !locked {
				t.Fatal("final recheck not reached with all bytes pinned together")
			}
			if change == "none" {
				if result["qualified"] != true {
					t.Fatal("stable positive control rejected")
				}
			} else {
				requireModelUpdateRefused(t, result)
			}
		})
	}
}

func TestModelUpdateCLIRejectsMixedDuplicateAndArbitraryManifestArguments(t *testing.T) {
	f := newModelUpdateFixture(t)
	for _, args := range [][]string{
		{"--verify-model-runtime-update"}, {"--verify-model-runtime-update", "relative"},
		{"--verify-model-runtime-update", f.leaf.target, "extra"},
		{"--verify-model-runtime-update", f.leaf.target, "--guard-path", f.leaf.target},
		{"--guard-path", f.leaf.target, "--verify-model-runtime-update", f.leaf.target},
		{"--verify-model-runtime-update", f.leaf.target, "--verify-capacity-runtime-update", f.leaf.target},
		{"--verify-model-runtime-update", f.leaf.target, "--verify-current-succession", "3081"},
		{"--verify-model-runtime-update", f.leaf.target, "--verify-product-resume", "3081"},
		{"--verify-model-runtime-update", f.leaf.target, "--verify-model-runtime-update", f.leaf.target},
		{"--verify-model-runtime-update", f.leaf.target, "--manifest", f.buildReceipt},
		{"--verify-model-runtime-update", f.leaf.target, "--sha256", f.leaf.candidate.sha256},
		{"--help", "--verify-model-runtime-update", f.leaf.target},
		{"junk", "--verify-model-runtime-update", f.leaf.target},
	} {
		var out bytes.Buffer
		calls := 0
		code := runModel3081CLI(args, &out, func(string) object { calls++; return object{"qualified": true} })
		if code != 1 || calls != 0 || out.String() != "{\"qualified\":false}\n" {
			t.Fatal("malformed purpose reached verifier or changed failure contract")
		}
		var mainOut, mainErr bytes.Buffer
		if code := runGuardCLI(args, &mainOut, &mainErr, func(int) object {
			t.Fatal("malformed update reached another purpose")
			return nil
		}); code != 1 || mainOut.String() != out.String() || mainErr.Len() != 0 {
			t.Fatal("main dispatch changed malformed-purpose rejection")
		}
	}
	for _, args := range [][]string{{"--verify-model-runtime-update", f.leaf.target}, {"-verify-model-runtime-update", f.leaf.target}, {"--verify-model-runtime-update=" + f.leaf.target}} {
		var out bytes.Buffer
		code := runModel3081CLI(args, &out, f.result)
		var result object
		if code != 0 || json.Unmarshal(out.Bytes(), &result) != nil || result["qualified"] != true || result["data_access_granted"] != false || result["todo_id"] != float64(3081) {
			t.Fatal("valid purpose failed")
		}
	}
	if code := runModel3081CLI([]string{"--verify-model-runtime-update", f.leaf.target}, closedWriter{}, f.result); code != 1 {
		t.Fatal("failed output reported success")
	}
	var out bytes.Buffer
	if code := runModel3081CLI([]string{"--verify-model-runtime-update", f.leaf.target}, &out, func(path string) object {
		result := f.result(path)
		result["private_path"] = "not a software fact"
		return result
	}); code != 0 || strings.Contains(out.String(), "private_path") {
		t.Fatal("non-software verifier metadata leaked")
	}
}

func TestModelUpdateKeepsLegacyCLIErrorAndClassification(t *testing.T) {
	for _, args := range [][]string{{"--guard-path", "relative"}, {"--unknown-option"}, {"--guard-path"}} {
		var out, err bytes.Buffer
		if code := runGuardCLI(args, &out, &err, func(int) object { return nil }); code != 2 {
			t.Fatal("legacy error exit changed")
		}
	}
	f := newModelUpdateFixture(t)
	if f.result(f.leaf.target)["qualified"] != true {
		t.Fatal("purpose positive control rejected")
	}
	if allowed(f.leaf.target) || allowed(f.root) {
		t.Fatal("update purpose changed general data classification")
	}
}
