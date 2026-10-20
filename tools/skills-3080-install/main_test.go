package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureBundle() Bundle {
	t := BundleTarget{Name: "sample", Positive: "public test example", Negative: "production change", Sources: []string{"public/source"}}
	for _, p := range fileOrder {
		s := "source evidence"
		if p == "SKILL.md" {
			s = "---\nname: sample\ndescription: 検査の例を扱う。\n---\n\n確認する。\n"
		}
		t.Files = append(t.Files, BundleFile{p, digest([]byte(s)), s})
	}
	return Bundle{Version: 1, Root: centralRoot, Targets: []BundleTarget{t}}
}
func TestNewOnlyApplyAndImmutableReadback(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "skills")
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	b := fixtureBundle()
	receipt := filepath.Join(d, "receipt.jsonl")
	if e := apply(b, root, receipt); e != nil {
		t.Fatal(e)
	}
	for _, f := range b.Targets[0].Files {
		got, e := os.ReadFile(filepath.Join(root, "sample", filepath.FromSlash(f.Path)))
		if e != nil || digest(got) != f.SHA {
			t.Fatal("installed evidence differs")
		}
	}
	raw, e := os.ReadFile(receipt)
	if e != nil {
		t.Fatal(e)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	var end struct {
		Phase   string          `json:"phase"`
		Targets []ReceiptTarget `json:"targets"`
	}
	if e = json.Unmarshal(lines[len(lines)-1], &end); e != nil || end.Phase != "complete" || len(end.Targets) != 1 {
		t.Fatal("missing complete manifest")
	}
	before := append([]byte(nil), raw...)
	if e = apply(b, root, filepath.Join(d, "another-receipt")); e == nil {
		t.Fatal("overwrote existing target")
	}
	after, _ := os.ReadFile(receipt)
	if !bytes.Equal(before, after) {
		t.Fatal("old receipt overwritten")
	}
}
func TestRejectWholeBundleBeforeAnyWrite(t *testing.T) {
	for _, which := range []string{"hash", "path", "name", "duplicate"} {
		t.Run(which, func(t *testing.T) {
			b := fixtureBundle()
			switch which {
			case "hash":
				b.Targets[0].Files[0].Content = "changed"
			case "path":
				b.Targets[0].Files[0].Path = "../escape"
			case "name":
				b.Targets[0].Name = "../escape"
			case "duplicate":
				b.Targets = append(b.Targets, b.Targets[0])
			}
			d := t.TempDir()
			if e := apply(b, d, filepath.Join(d, "receipt")); e == nil {
				t.Fatal("accepted unsafe/corrupt bundle")
			}
			entries, _ := os.ReadDir(d)
			if len(entries) != 0 {
				t.Fatal("invalid preflight mutated directory")
			}
		})
	}
}
func TestRejectExistingOwnerData(t *testing.T) {
	d := t.TempDir()
	b := fixtureBundle()
	if e := os.Mkdir(filepath.Join(d, "sample"), 0700); e != nil {
		t.Fatal(e)
	}
	sentinel := []byte("owner data")
	if e := os.WriteFile(filepath.Join(d, "sample", "SKILL.md"), sentinel, 0600); e != nil {
		t.Fatal(e)
	}
	if e := apply(b, d, filepath.Join(d, "receipt")); e == nil {
		t.Fatal("existing directory accepted")
	}
	got, _ := os.ReadFile(filepath.Join(d, "sample", "SKILL.md"))
	if !bytes.Equal(got, sentinel) {
		t.Fatal("owner data changed")
	}
}
func TestRenderPinsSourceNoticeAndInvocation(t *testing.T) {
	d := t.TempDir()
	body := []byte("---\nname: sample\ndescription: source\ndisable-model-invocation: true\n---\nPublic source data.\n")
	notice := []byte("MIT License\nCopyright test fixture\n")
	var gz bytes.Buffer
	z := gzip.NewWriter(&gz)
	z.Write(body)
	z.Close()
	os.WriteFile(filepath.Join(d, "body.gz"), gz.Bytes(), 0600)
	os.WriteFile(filepath.Join(d, "notice.txt"), notice, 0600)
	r := SourceRecord{Repo: "public/source", Commit: strings.Repeat("a", 40), Path: "skills/sample/SKILL.md", SHA: digest(body), ContentPath: "body.gz"}
	records, _ := json.Marshal([]SourceRecord{r})
	os.WriteFile(filepath.Join(d, "records.json"), records, 0600)
	explicit := true
	target := Target{Name: "sample", Description: "指定された例を確認するときに使う。", Positive: "test", Negative: "production", Path: r.Path, SHA: r.SHA, URL: "https://github.com/public/source/blob/" + r.Commit + "/" + r.Path, Guidance: []string{"必要な例だけを確認する。"}, Status: "fixed", Changes: "Adapted for this workflow.", AdoptionReason: "Maintain this fixed entrypoint.", UpstreamExplicit: &explicit, LicenseReview: LicenseReview{Tree: strings.Repeat("b", 40), Complete: true, Paths: []string{"LICENSE"}, Nested: []string{}, Scope: "entrypoint only", Checked: "2026-10-20T00:00:00Z"}}
	target.Invocation.Mode = "implicit_with_narrow_trigger"
	target.Invocation.Reason = "fixture narrow workflow"
	p := Plan{Repo: r.Repo, Commit: r.Commit, Tree: strings.Repeat("b", 40), Targets: []Target{target}}
	p.License.Path = "notice.txt"
	p.License.SHA = digest(notice)
	p.License.ID = "MIT"
	write := func() {
		b, _ := json.Marshal(p)
		if e := os.WriteFile(filepath.Join(d, "plan.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write()
	b, e := render([]string{filepath.Join(d, "plan.json")}, filepath.Join(d, "records.json"), d)
	if e != nil || len(b.Targets) != 1 {
		t.Fatalf("render: %v", e)
	}
	p.Targets[0].SHA = strings.Repeat("0", 64)
	write()
	if _, e = render([]string{filepath.Join(d, "plan.json")}, filepath.Join(d, "records.json"), d); e == nil {
		t.Fatal("source mismatch accepted")
	}
	p.Targets[0].SHA = r.SHA
	p.Targets[0].UpstreamExplicit = new(bool)
	write()
	if _, e = render([]string{filepath.Join(d, "plan.json")}, filepath.Join(d, "records.json"), d); e == nil {
		t.Fatal("invocation receipt mismatch accepted")
	}
}
