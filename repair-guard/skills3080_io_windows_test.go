package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func skillsFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	p, e := ownerPhysicalPath(root)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func skillsParsedFixture(t *testing.T) skillsManifest {
	t.Helper()
	m := skillsTestManifest()
	for i := range m.Files {
		m.Files[i].body, _ = base64Decode(m.Files[i].Content)
	}
	return m
}
func base64Decode(s string) ([]byte, error) { return base64.StdEncoding.Strict().DecodeString(s) }

func TestSkills3080AtomicFileHandle(t *testing.T) {
	root := skillsFixtureRoot(t)
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	parent, ok := fs.directory(root)
	if !ok {
		t.Fatal("parent")
	}
	h, ok := skillsOpenAt(parent, "new.txt", false, true)
	if !ok {
		t.Fatal("atomic file creation")
	}
	f := os.NewFile(uintptr(h), filepath.Join(root, "new.txt"))
	defer f.Close()
	if _, e := f.Write([]byte("fixed bytes")); e != nil {
		t.Fatalf("write: %v", e)
	}
	if e := f.Sync(); e != nil {
		t.Fatalf("sync: %v", e)
	}
	if _, e := f.Seek(0, 0); e != nil {
		t.Fatalf("seek: %v", e)
	}
	b, e := io.ReadAll(f)
	if e != nil || string(b) != "fixed bytes" {
		t.Fatalf("same-handle read: %v %q", e, b)
	}
}

func TestSkills3080CreateUsesValidatedBytes(t *testing.T) {
	root := skillsFixtureRoot(t)
	m := skillsParsedFixture(t)
	fs := skillsFS{blocked: func(string) bool { return false }}
	count, reason := skillsCreate(m, root, &fs)
	fs.close()
	if reason != "" || count != len(m.Files) {
		t.Fatalf("create failed %q/%d", reason, count)
	}
	for _, f := range m.Files {
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Target)))
		if e != nil || !bytes.Equal(b, f.body) {
			t.Fatal("created bytes differ")
		}
	}
	fs = skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	if _, reason = skillsCreate(m, root, &fs); reason != "existing_target" {
		t.Fatal("existing unknown target admitted")
	}
}
func TestSkills3080ExistingAndAliasedTargetsNeverOpened(t *testing.T) {
	root := skillsFixtureRoot(t)
	target := filepath.Join(root, "sample")
	if e := os.Mkdir(target, 0700); e != nil {
		t.Fatal(e)
	}
	original := []byte("unclassified content")
	if e := os.WriteFile(filepath.Join(target, "SKILL.md"), original, 0600); e != nil {
		t.Fatal(e)
	}
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	if reason := skillsTargetsAbsent(skillsParsedFixture(t), root, &fs); reason != "existing_target" || len(fs.files) != 0 {
		t.Fatal("existing body opened or admitted")
	}
	if n, reason := skillsCreate(skillsParsedFixture(t), root, &fs); n != 0 || reason != "existing_target" {
		t.Fatal("collision not held")
	}
	b, _ := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if !bytes.Equal(b, original) {
		t.Fatal("existing content changed")
	}
}
func TestSkills3080HardlinkAndHashChange(t *testing.T) {
	root := skillsFixtureRoot(t)
	one := filepath.Join(root, "one.txt")
	two := filepath.Join(root, "two.txt")
	if e := os.WriteFile(one, []byte("body"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(one, two); e != nil {
		t.Fatal(e)
	}
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	if _, ok := fs.read(one, 100); ok {
		t.Fatal("hardlink original accepted")
	}
	if _, ok := fs.read(two, 100); ok {
		t.Fatal("hardlink alias accepted")
	}
	m := skillsParsedFixture(t)
	m.Files[0].body = []byte("changed after validation")
	if _, reason := skillsCreate(m, root, &fs); reason != "manifest" {
		t.Fatal("unvalidated bytes consumed")
	}
}
func TestSkills3080PinsParentAndCreatedObject(t *testing.T) {
	root := skillsFixtureRoot(t)
	child := filepath.Join(root, "sample")
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	h, ok := fs.directory(root)
	if !ok {
		t.Fatal("physical parent rejected")
	}
	created, ok := skillsOpenAt(h, "sample", true, true)
	if !ok {
		t.Fatal("atomic dir create failed")
	}
	fs.dirs[strings.ToLower(child)] = created
	if e := os.Rename(child, filepath.Join(root, "moved")); e == nil {
		t.Fatal("held directory could be substituted")
	}
	if _, ok = skillsOpenAt(h, "sample", true, true); ok {
		t.Fatal("CreateNew allowed existing directory")
	}
}
func TestSkills3080JunctionParent(t *testing.T) {
	root := skillsFixtureRoot(t)
	real := filepath.Join(root, "real")
	alias := filepath.Join(root, "alias")
	if e := os.Mkdir(real, 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, real)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("junction fixture: %v %s", e, output)
	}
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	if _, ok := fs.directory(alias); ok {
		t.Fatal("junction parent accepted")
	}
	if n, reason := skillsCreate(skillsParsedFixture(t), alias, &fs); n != 0 || reason != "physical_parent" {
		t.Fatal("junction write admitted")
	}
}

func TestSkills3080CompleteManifestReviewAndCreate(t *testing.T) {
	m := skillsTestManifest()
	original, notice := []byte("fixed synthetic public source\n"), []byte("fixed individual grant\n")
	m.Sources[0].Repository = "6mile/skills"
	m.Sources[0].Commit = skillsPublicSources["6mile/skills"]
	m.Sources[0].SHA = hashBytes(original)
	m.Sources[0].License.SHA = hashBytes(notice)
	for _, file := range []struct {
		path string
		body []byte
	}{{"sample/references/upstream.txt", original}, {"sample/references/LICENSE.txt", notice}} {
		m.Files = append(m.Files, skillsOutput{Target: file.path, SHA: hashBytes(file.body), Content: base64.StdEncoding.EncodeToString(file.body)})
	}
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	manifestSHA := hashBytes(raw)
	parsed, ok := skillsParseManifest(raw)
	evidence := skillsAcquiredFixture(t, original, notice, true)
	sourceFS := skillsFS{blocked: func(string) bool { return false }}
	defer sourceFS.close()
	if !ok || !skillsSourcesBoundAt(parsed, &sourceFS, evidence.root, evidence.catalogSHA, evidence.anchors) {
		t.Fatal("complete positive manifest rejected")
	}
	review := skillsReceived{Spec: []byte("SKILLS3080_MANIFEST_SHA256 " + manifestSHA), Metadata: object{"model": "google-antigravity/gemini-3.1-pro", "effort": "high"}, Native: []object{{"model": "google-antigravity/gemini-3.1-pro", "effort": "high"}}, Body: []byte("SKILLS3080_ACCEPTANCE " + `{"domain":"skills-3080-review-v1","manifest_sha256":"` + manifestSHA + `","accepted":true,"must":0}`)}
	if !skillsReviewAccepted(review, manifestSHA, "gpt-6.1-sol") {
		t.Fatal("review did not bind same manifest")
	}
	// Replacing the caller's raw buffer cannot replace the already verified and
	// parsed output bytes actually consumed by the creation boundary.
	for i := range raw {
		raw[i] = ' '
	}
	root := skillsFixtureRoot(t)
	fs := skillsFS{blocked: func(string) bool { return false }}
	count, reason := skillsCreate(parsed, root, &fs)
	fs.close()
	if reason != "" || count != 3 {
		t.Fatalf("complete operation failed: %s/%d", reason, count)
	}
	for _, file := range parsed.Files {
		actual, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Target)))
		if e != nil || hashBytes(actual) != file.SHA {
			t.Fatal("consumed bytes were not the reviewed output")
		}
	}
}
