package contract

import (
	"bytes"
	_ "embed"
	"strings"
	"testing"
)

// Parent candidate 3dec5ab (PR720), not current-main or success evidence.
//
//go:embed testdata/quality-parent.yml
var parentQuality []byte

func TestRemovalRequiresAllMovedChecks(t *testing.T) {
	if e := CheckQualityWorkflow(parentQuality); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"Desktop smoke", "cargo test -p passshare-desktop native_lock::tests", "pnpm --dir apps/desktop exec tsc --noEmit", "go test -mod=readonly ./...", "TEST_SCRATCH"} {
		t.Run(s, func(t *testing.T) {
			b := bytes.ReplaceAll(parentQuality, []byte(s), []byte("missing"))
			if CheckQualityWorkflow(b) == nil {
				t.Fatal("missing removal prerequisite accepted")
			}
		})
	}
	if CheckQualityWorkflow([]byte("jobs:\n  desktop-smoke: {}\n# "+string(parentQuality))) == nil {
		t.Fatal("comments cannot prove acceptance")
	}
}
func TestProposeKeepsLinuxAcceptance(t *testing.T) {
	if e := CheckCandidate(DesktopWorkflow); e != nil {
		t.Fatal(e)
	}
	b, e := Propose(originalDesktop, parentQuality)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(b, DesktopWorkflow) {
		t.Fatal("proposal differs")
	}
	for _, s := range []string{"libwebkit2gtk-4.1-dev", "pnpm --dir apps/desktop install --frozen-lockfile", "retention-days: 7"} {
		if _, e = Propose(bytes.ReplaceAll(originalDesktop, []byte(s), []byte("changed")), parentQuality); e == nil {
			t.Fatalf("unaudited input drift %s accepted", s)
		}
		if CheckCandidate(bytes.ReplaceAll(DesktopWorkflow, []byte(s), []byte("changed"))) == nil {
			t.Fatalf("candidate drift %s accepted", s)
		}
	}
	if bytes.Contains(DesktopWorkflow, []byte("macos-latest")) || bytes.Contains(DesktopWorkflow, []byte("windows-latest")) {
		t.Fatal("cloud Windows/macOS returned")
	}
}
func TestYAMLRejectsAmbiguity(t *testing.T) {
	for _, s := range []string{"name: one\nname: two\n", "name: &x one\nother: *x\n", "name: one\n---\nname: two\n"} {
		if _, e := parse([]byte(s)); e == nil {
			t.Fatalf("ambiguous YAML accepted: %s", s)
		}
	}
}
func TestInputScopeIsConservative(t *testing.T) {
	root, e := parse(DesktopWorkflow)
	if e != nil {
		t.Fatal(e)
	}
	steps := field(field(field(root, "jobs"), "admission"), "steps")
	run := ""
	for _, s := range steps.Content {
		if field(s, "id") != nil && field(s, "id").Value == "scope" {
			run = field(s, "run").Value
		}
	}
	for _, want := range []string{"set -euo pipefail", "git cat-file -e", "git diff --no-renames --name-only -z", "*) build=true", "pull_request"} {
		if !strings.Contains(run, want) {
			t.Fatalf("missing scope rule %s", want)
		}
	}
}
