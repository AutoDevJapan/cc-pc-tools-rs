package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOMB3255CLIRejectsArgumentsWithoutGenericFallback(t *testing.T) {
	var out, diagnostic bytes.Buffer
	code := runGuardCLI([]string{"--verify-omb-producer-3255", "C:/unrelated-input"}, &out, &diagnostic, func(int) object {
		t.Fatal("software comparison reached the ToDo verifier")
		return nil
	})
	var got map[string]any
	if code != 2 || diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &got) != nil ||
		got["domain"] != "omb-producer-software-v1" || got["status"] != "invalid_arguments" ||
		got["data_access_granted"] != false || got["software_matches"] != nil {
		t.Fatalf("bounded OMB3255 software purpose is missing: exit=%d stdout=%q stderr=%q", code, out.String(), diagnostic.String())
	}
}

type omb3255Fixture struct {
	spec omb3255Spec
	body map[string][]byte
}

func newOMB3255Fixture(t *testing.T) omb3255Fixture {
	t.Helper()
	root := t.TempDir()
	f := omb3255Fixture{spec: omb3255Spec{root: root}, body: make(map[string][]byte)}
	for _, name := range []string{"signature.json", "public-baseline.json", "original-install.json"} {
		path, body := filepath.Join(root, name), []byte("independently retained "+name)
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		f.spec.evidence = append(f.spec.evidence, consoleEvidence{root, path, hashBytes(body)})
		f.body[path] = body
	}
	for _, name := range []string{"entry.js", "package.json", "command.js", "server.js"} {
		path, body := filepath.Join(root, name), []byte("private-looking fixture body for "+name)
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		f.spec.components = append(f.spec.components, omb3255Component{path, hashBytes(body)})
		f.body[path] = body
	}
	return f
}

func (f omb3255Fixture) verify() omb3255Result {
	return omb3255SoftwareWith(f.spec, func(string) bool { return false }, func(root string) bool {
		return same(root, f.spec.root)
	}, func(path string) bool { return within(f.spec.root, path) })
}

func requireOMB3255Unavailable(t *testing.T, result omb3255Result) {
	t.Helper()
	if result.Status != "unavailable" || result.SoftwareMatches != nil || result.EvidenceVerified ||
		result.DataAccessGranted || result.SoftwareAccessGranted || result.SourceAccessGranted ||
		result.SourceWorktreeVerified || result.Execution4559Verified || result.OriginalRegistryRecovered || result.StateChanged {
		t.Fatalf("unavailable comparison changed a permission or asserted a negative observation: %+v", result)
	}
}

func TestOMB3255BindsIndependentProofsAndAllFourFiles(t *testing.T) {
	f := newOMB3255Fixture(t)
	got := f.verify()
	matched := true
	want := omb3255Unavailable("matched")
	want.SoftwareMatches, want.EvidenceVerified = &matched, true
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fixed result or software comparison changed: %+v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range f.body {
		current, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(current, body) {
			t.Fatal("comparison changed its synthetic inputs")
		}
		if bytes.Contains(raw, body) || strings.Contains(string(raw), path) || strings.Contains(string(raw), hashBytes(body)) {
			t.Fatal("comparison exposed fixture content, path or an observed digest")
		}
	}
	var fields map[string]any
	wantJSON := map[string]any{
		"domain": "omb-producer-software-v1", "todo_id": float64(3255), "status": "matched", "software_matches": true, "evidence_verified": true,
		"reference_version": "0.1.121", "reference_source_commit": "97a4a1cab96ab3d07b0c7dac275f69afc6e7a8dc",
		"data_access_granted": false, "software_access_granted": false, "source_access_granted": false,
		"source_worktree_verified": false, "execution4559_verified": false, "original_registry_recovered": false, "state_changed": false,
	}
	if json.Unmarshal(raw, &fields) != nil || !reflect.DeepEqual(fields, wantJSON) {
		t.Fatal("software-only JSON field names or values changed")
	}
}

func TestOMB3255RejectsChangedMissingAndSharedInputs(t *testing.T) {
	for _, kind := range []string{"proof", "component"} {
		for _, mutation := range []string{"missing", "changed", "hardlink"} {
			t.Run(kind+"/"+mutation, func(t *testing.T) {
				f := newOMB3255Fixture(t)
				path := f.spec.components[2].path
				if kind == "proof" {
					path = f.spec.evidence[1].path
				}
				var err error
				switch mutation {
				case "missing":
					err = os.Remove(path)
				case "changed":
					err = os.WriteFile(path, []byte("different undisclosed input"), 0600)
				case "hardlink":
					err = os.Link(path, filepath.Join(f.spec.root, "second-name"))
				}
				if err != nil {
					t.Fatal(err)
				}
				result := f.verify()
				if kind == "component" && mutation == "changed" {
					if result.Status != "mismatched" || result.SoftwareMatches == nil || *result.SoftwareMatches || !result.EvidenceVerified {
						t.Fatal("a complete and stable actual mismatch became unavailable")
					}
				} else {
					requireOMB3255Unavailable(t, result)
				}
			})
		}
	}
}

func TestOMB3255MismatchStillRequiresAllFourAndFinalEvidence(t *testing.T) {
	for _, failure := range []string{"missing component", "protected component", "proof permission changed"} {
		t.Run(failure, func(t *testing.T) {
			f := newOMB3255Fixture(t)
			if err := os.WriteFile(f.spec.components[0].path, []byte("actual mismatch not disclosed"), 0600); err != nil {
				t.Fatal(err)
			}
			if failure == "missing component" {
				if err := os.Remove(f.spec.components[3].path); err != nil {
					t.Fatal(err)
				}
			}
			revoked := false
			protected := func(path string) bool {
				if same(path, f.spec.components[3].path) {
					revoked = true
					return failure == "protected component"
				}
				return false
			}
			requireOMB3255Unavailable(t, omb3255SoftwareWith(f.spec, protected, func(string) bool { return true },
				func(string) bool { return !(revoked && failure == "proof permission changed") }))
		})
	}
}

func TestOMB3255RejectsMissingDuplicateAndSelfServingProofs(t *testing.T) {
	for _, mutation := range []string{"proof-count", "component-count", "duplicate-proof", "duplicate-component", "proof-is-component", "outside-proof-root", "different-proof-root", "unpinned-proof", "unpinned-component"} {
		t.Run(mutation, func(t *testing.T) {
			f := newOMB3255Fixture(t)
			switch mutation {
			case "proof-count":
				f.spec.evidence = f.spec.evidence[:2]
			case "component-count":
				f.spec.components = f.spec.components[:3]
			case "duplicate-proof":
				f.spec.evidence[1] = f.spec.evidence[0]
			case "duplicate-component":
				f.spec.components[2] = f.spec.components[0]
			case "proof-is-component":
				f.spec.evidence[1].path = f.spec.components[0].path
				f.spec.evidence[1].sha256 = f.spec.components[0].sha256
			case "outside-proof-root":
				f.spec.root = filepath.Join(f.spec.root, "not-the-parent")
			case "different-proof-root":
				f.spec.evidence[1].root += "-other"
			case "unpinned-proof":
				f.spec.evidence[1].sha256 = ""
			case "unpinned-component":
				f.spec.components[1].sha256 = ""
			}
			requireOMB3255Unavailable(t, f.verify())
		})
	}
}

func TestOMB3255PreservesOriginalProtectionAndSourceQualification(t *testing.T) {
	f := newOMB3255Fixture(t)
	for _, path := range []string{f.spec.root, f.spec.evidence[0].path, f.spec.components[0].path} {
		requireOMB3255Unavailable(t, omb3255SoftwareWith(f.spec, func(p string) bool { return same(p, path) },
			func(string) bool { return true }, func(string) bool { return true }))
	}
	requireOMB3255Unavailable(t, omb3255SoftwareWith(f.spec, func(string) bool { return false },
		func(string) bool { return false }, func(string) bool { return true }))
	requireOMB3255Unavailable(t, omb3255SoftwareWith(f.spec, func(string) bool { return false },
		func(string) bool { return true }, func(string) bool { return false }))
}

func TestOMB3255ProofFailurePrecedesComponentInspection(t *testing.T) {
	f := newOMB3255Fixture(t)
	inspected := make(map[string]bool)
	protected := func(path string) bool {
		for _, component := range f.spec.components {
			if same(path, component.path) {
				inspected[component.path] = true
			}
		}
		return false
	}
	good := omb3255SoftwareWith(f.spec, protected, func(string) bool { return true }, func(string) bool { return true })
	if good.SoftwareMatches == nil || !*good.SoftwareMatches || len(inspected) != 4 {
		t.Fatal("positive control did not inspect the four synthetic components")
	}
	clear(inspected)
	if err := os.WriteFile(f.spec.evidence[2].path, []byte("changed installation evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	requireOMB3255Unavailable(t, omb3255SoftwareWith(f.spec, protected, func(string) bool { return true }, func(string) bool { return true }))
	if len(inspected) != 0 {
		t.Fatal("unverified original-install evidence reached component inspection")
	}
}

func TestOMB3255RetainsReadHandlesAndRechecksSource(t *testing.T) {
	f := newOMB3255Fixture(t)
	writeBlocked, attempted := true, false
	protected := func(path string) bool {
		if same(path, f.spec.components[3].path) {
			attempted = true
			writer, err := os.OpenFile(f.spec.components[0].path, os.O_WRONLY, 0)
			if err == nil {
				writer.Close()
				writeBlocked = false
			}
		}
		return false
	}
	result := omb3255SoftwareWith(f.spec, protected, func(string) bool { return true }, func(string) bool { return true })
	if !attempted || !writeBlocked || result.SoftwareMatches == nil || !*result.SoftwareMatches {
		t.Fatal("earlier input was not held against writing until the comparison finished")
	}
	revoked := false
	requireOMB3255Unavailable(t, omb3255SoftwareWith(f.spec, func(path string) bool {
		if same(path, f.spec.components[3].path) {
			revoked = true
		}
		return false
	}, func(string) bool { return !revoked }, func(string) bool { return true }))
}

func TestOMB3255CLIPositiveControlAndArgumentRejection(t *testing.T) {
	for _, args := range [][]string{{}, {omb3255Flag, "extra"}, {omb3255Flag + "=true"}, {"-verify-omb-producer-3255"}, {omb3255Flag, "--guard-path", "C:/unrelated"}} {
		var out bytes.Buffer
		calls := 0
		code := runOMB3255CLI(args, &out, func() omb3255Result {
			calls++
			return newOMB3255Fixture(t).verify()
		})
		if code != 2 || calls != 0 || !strings.Contains(out.String(), "invalid_arguments") {
			t.Fatalf("invalid arguments reached software inspection: %q", args)
		}
	}
	var out bytes.Buffer
	calls := 0
	f := newOMB3255Fixture(t)
	if code := runOMB3255CLI([]string{omb3255Flag}, &out, func() omb3255Result { calls++; return f.verify() }); code != 0 || calls != 1 {
		t.Fatal("valid argument control did not perform the synthetic comparison")
	}
	var diagnostic bytes.Buffer
	out.Reset()
	if runGuardCLI([]string{"--help"}, &out, &diagnostic, func(int) object { return nil }) != 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), omb3255Flag) {
		t.Fatal("existing stderr-only help does not describe the new software-only purpose")
	}
}
