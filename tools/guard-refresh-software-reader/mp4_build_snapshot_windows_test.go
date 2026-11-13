//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func snapshotTestTool(t *testing.T, name string) string {
	t.Helper()
	path, e := exec.LookPath(name)
	if e != nil {
		t.Fatalf("required test tool %s missing", name)
	}
	return path
}

func TestMP4SnapshotExcludesUnlistedBuildScriptAndPinsKnownSource(t *testing.T) {
	root := physicalSoftwareFixture(t)
	build := softwareBuildPlan{Target: root}
	raw := []byte("fn main() {}\n")
	evidence := askBuildEvidence{Inputs: []buildSourceRecord{{Path: "cc-ask/src/main.rs", SHA: digest(raw)}}}
	sources := []updateObject{&fakeUpdateObject{raw: raw}}
	snapshot, err := materializeMP4BuildSnapshot(build, evidence, sources)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	known := filepath.Join(snapshot.Root, "cc-ask", "src", "main.rs")
	if got, e := os.ReadFile(known); e != nil || !bytes.Equal(got, raw) {
		t.Fatal("compiler ordinary read blocked", e)
	}
	if os.WriteFile(known, []byte("changed"), 0600) == nil || os.Rename(known, known+".moved") == nil {
		t.Fatal("heldsource replacement permitted")
	}
	if _, e := os.Stat(filepath.Join(snapshot.Root, "cc-ask", "build.rs")); !os.IsNotExist(e) {
		t.Fatal("unlisted build script materialized")
	}
	if got, e := os.ReadFile(filepath.Join(snapshot.Root, "Cargo.toml")); e != nil || string(got) != mp4CargoWorkspace {
		t.Fatal("workspace recipe differs")
	}
	if !snapshot.exact() {
		t.Fatal("normal snapshot rejected")
	}
	extra := filepath.Join(snapshot.Root, "cc-ask", "build.rs")
	if e := os.WriteFile(extra, []byte("fn main() {}"), 0600); e == nil {
		t.Fatal("sealed namespace admitted unlisted build script")
	}
	if e := os.Mkdir(filepath.Join(snapshot.Root, ".cargo"), 0700); e == nil {
		t.Fatal("sealed namespace admitted compiler config")
	}
}

func TestMP4SelectedGoSnapshotBuildIgnoresUnlistedFilesAndPGO(t *testing.T) {
	plain := physicalSoftwareFixture(t)
	packageDir := filepath.Join(plain, "unattended-runner")
	if e := os.Mkdir(packageDir, 0700); e != nil {
		t.Fatal(e)
	}
	selected := filepath.Join(packageDir, "main.go")
	if os.WriteFile(selected, []byte("package main\nfunc main(){}\n"), 0600) != nil || os.WriteFile(filepath.Join(packageDir, "default.pgo"), []byte("unprovedprofile"), 0600) != nil {
		t.Fatal("owned PGO setup")
	}
	ctxPGO, cancelPGO := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelPGO()
	args := softwareProducerGoArgs(plain, filepath.Join(plain, "nopgo.exe"), []string{selected})
	profileResult := askOwnedChild(ctxPGO, snapshotTestTool(t, "go"), args...)
	defer clear(profileResult.Raw)
	if profileResult.Exit != 0 || !profileResult.Reaped {
		t.Fatal("producer consumed unproved profile", profileResult.Exit)
	}
	root := physicalSoftwareFixture(t)
	build := softwareBuildPlan{Target: root, GoRunner: true}
	code := []byte("package main\nfunc main(){}\n")
	mod := []byte("module github.com/AutoDevJapan/cc-pc-tools-rs/unattended-runner\n\ngo 1.27.0\n")
	evidence := askBuildEvidence{Inputs: []buildSourceRecord{{Path: "unattended-runner/main.go", SHA: digest(code)}, {Path: "unattended-runner/go.mod", SHA: digest(mod)}}}
	snapshot, e := materializeMP4BuildSnapshot(build, evidence, []updateObject{&fakeUpdateObject{raw: code}, &fakeUpdateObject{raw: mod}})
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Close()
	dir := filepath.Join(snapshot.Root, "unattended-runner")
	if os.WriteFile(filepath.Join(dir, "untracked.go"), []byte("package main\nsyntax error"), 0600) == nil || os.WriteFile(filepath.Join(dir, "default.pgo"), []byte("invalid-profile"), 0600) == nil {
		t.Fatal("sealed namespace admitted compiler input")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output := filepath.Join(root, "fixture.exe")
	r := askOwnedChild(ctx, snapshotTestTool(t, "go"), "-C", dir, "build", "-pgo=off", "-mod=readonly", "-trimpath", "-o", output, filepath.Join(dir, "main.go"))
	defer clear(r.Raw)
	if r.Exit != 0 || !r.Reaped || r.TimedOut {
		t.Fatal("selected pinned ordinary source failed", r.Exit)
	}
	if !snapshot.exact() {
		t.Fatal("unchanged pinned namespace lost")
	}
	if strings.Contains(output, "untracked") {
		t.Fatal("invalid fixture")
	}
}

func TestMP4RustSnapshotCargoDoesNotDiscoverLiveBuildScript(t *testing.T) {
	root := physicalSoftwareFixture(t)
	build := softwareBuildPlan{Target: root}
	inputs := []struct{ path, raw string }{
		{"Cargo.lock", "# generated test lock\nversion = 4\n\n[[package]]\nname = \"cc-ask\"\nversion = \"0.1.0\"\n"},
		{"cc-ask/Cargo.toml", "[package]\nname=\"cc-ask\"\nversion=\"0.1.0\"\nedition=\"2021\"\n"},
		{"cc-ask/src/main.rs", "fn main() {}\n"},
	}
	for _, name := range []string{"cc-chrome", "cc-job-policy", "cc-live", "cc-local-llm", "cc-task-title"} {
		inputs = append(inputs, struct{ path, raw string }{name + "/Cargo.toml", "[package]\nname=\"" + name + "\"\nversion=\"0.1.0\"\nedition=\"2021\"\n"}, struct{ path, raw string }{name + "/src/lib.rs", "pub fn owned() {}\n"})
	}
	evidence := askBuildEvidence{}
	var objects []updateObject
	for _, item := range inputs {
		raw := []byte(item.raw)
		evidence.Inputs = append(evidence.Inputs, buildSourceRecord{Path: item.path, SHA: digest(raw)})
		objects = append(objects, &fakeUpdateObject{raw: raw})
	}
	snapshot, e := materializeMP4BuildSnapshot(build, evidence, objects)
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Close()
	if os.WriteFile(filepath.Join(snapshot.Root, "cc-ask", "build.rs"), []byte("fn main(){panic!(\"unproved\")}"), 0600) == nil {
		t.Fatal("unproved Cargo build script admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := askOwnedChildAt(ctx, snapshot.Root, snapshotTestTool(t, "cargo"), "metadata", "--offline", "--no-deps", "--format-version", "1", "--manifest-path", filepath.Join(snapshot.Root, "Cargo.toml"))
	defer clear(r.Raw)
	if r.Exit != 0 || !r.Reaped || r.TimedOut || bytes.Contains(r.Raw, []byte("custom-build")) {
		t.Fatal("snapshot Cargo selection failed", r.Exit)
	}
	if !snapshot.exact() {
		t.Fatal("Cargo metadata changed fixed snapshot")
	}
}
