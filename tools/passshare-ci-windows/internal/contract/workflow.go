package contract

import (
	"bytes"
	_ "embed"
	"errors"
	"reflect"
	"strings"
)

// DesktopWorkflow is the only executable workflow admitted by this version.
//
//go:embed desktop-build.yml
var desktopWorkflow []byte

var DesktopWorkflow = bytes.ReplaceAll(desktopWorkflow, []byte("\r\n"), []byte("\n"))

//go:embed original-desktop.yml
var originalDesktop []byte

// Propose refuses drift outside the explicitly audited workflow. Callers must
// obtain qg from the current main, then verify its actual success by API.
func Propose(before, qg []byte) ([]byte, error) {
	old, e := parse(before)
	if e != nil {
		return nil, e
	}
	original, e := parse(originalDesktop)
	if e != nil {
		return nil, e
	}
	if !reflect.DeepEqual(semantic(old), semantic(original)) {
		return nil, errors.New("Desktop Build changed since audit; inspect drift before proposing")
	}
	if e = CheckQualityWorkflow(qg); e != nil {
		return nil, e
	}
	if e = CheckCandidate(DesktopWorkflow); e != nil {
		return nil, e
	}
	return append([]byte(nil), DesktopWorkflow...), nil
}

// Require the parent's actual named jobs and commands, not a comment containing
// a search string or a different job. This preserves removal prerequisites.
func CheckQualityWorkflow(raw []byte) error {
	root, e := parse(raw)
	if e != nil {
		return e
	}
	jobs := field(root, "jobs")
	desktop := field(jobs, "desktop-smoke")
	if desktop == nil || field(desktop, "name") == nil || field(desktop, "name").Value != "Desktop smoke" {
		return errors.New("Quality Gate Desktop smoke missing")
	}
	if field(desktop, "runs-on") == nil || field(desktop, "runs-on").Value != "ubuntu-latest" {
		return errors.New("Linux desktop acceptance must remain")
	}
	for _, command := range []string{"cargo test -p passshare-desktop smoke_", "cargo test -p passshare-desktop native_lock::tests", "pnpm --dir apps/desktop exec tsc --noEmit", "pnpm --dir apps/desktop run test"} {
		if !hasCommand(desktop, command, "", "", "") {
			return errors.New("Quality Gate desktop command missing: " + command)
		}
	}
	pre := field(jobs, "preflight")
	if !hasCommand(pre, "go test -mod=readonly ./...", "tools/desktop-release-check", "TEST_SCRATCH", "${{ runner.temp }}") {
		return errors.New("candidate verifier has not moved to Quality Gate")
	}
	gate := field(jobs, "gate")
	if gate == nil || field(gate, "needs") == nil || !sequenceContains(field(gate, "needs"), "desktop-smoke") || !sequenceContains(field(gate, "needs"), "preflight") {
		return errors.New("Quality Gate must aggregate the required jobs")
	}
	return nil
}

// CheckCandidate also compares Linux's substantive actions/commands to the old
// matrix row so an unrelated acceptance change cannot hide in a route edit.
func CheckCandidate(raw []byte) error {
	candidate, e := parse(raw)
	if e != nil {
		return e
	}
	canonical, e := parse(DesktopWorkflow)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(semantic(candidate), semantic(canonical)) {
		return errors.New("candidate differs from reviewed Desktop Build structure")
	}
	before, e := parse(originalDesktop)
	if e != nil {
		return e
	}
	oldBuild := field(field(before, "jobs"), "build")
	linux := field(field(candidate, "jobs"), "linux")
	// Every original build step except macOS-only storage remains, with its
	// matrix-dependent values resolved to the original ubuntu-22.04 row.
	oldSteps := field(oldBuild, "steps")
	newSteps := field(linux, "steps")
	if oldSteps == nil || newSteps == nil {
		return errors.New("Linux build steps absent")
	}
	var expected []any
	for _, step := range oldSteps.Content {
		if condition := field(step, "if"); condition != nil && condition.Value == "matrix.platform == 'macos-latest'" {
			continue
		}
		var m map[string]any
		if e = step.Decode(&m); e != nil {
			return e
		}
		delete(m, "if")
		if uses, _ := m["uses"].(string); strings.HasPrefix(uses, "actions/checkout@") {
			m["with"] = map[string]any{"persist-credentials": false, "ref": "${{ github.sha }}"}
		}
		if uses, _ := m["uses"].(string); strings.HasPrefix(uses, "tauri-apps/tauri-action@") {
			m["with"].(map[string]any)["args"] = ""
			m["env"].(map[string]any)["GITHUB_TOKEN"] = "${{ github.token }}"
		}
		if name, _ := m["name"].(string); name == "Upload artifacts" {
			with := m["with"].(map[string]any)
			with["name"] = "desktop-ubuntu-22.04"
			with["path"] = "target/release/bundle/**/*"
		}
		expected = append(expected, m)
	}
	var actual []any
	for _, step := range newSteps.Content {
		var m map[string]any
		if e = step.Decode(&m); e != nil {
			return e
		}
		actual = append(actual, m)
	}
	if !reflect.DeepEqual(expected, actual) {
		return errors.New("Linux acceptance changed outside approved matrix resolution")
	}
	return nil
}
