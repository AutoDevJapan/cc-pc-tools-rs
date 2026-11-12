package hostedsource

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtures(t *testing.T) (map[string][]byte, map[string][]byte) {
	t.Helper()
	a, b := map[string][]byte{}, map[string][]byte{}
	for _, w := range Workflows() {
		v, e := os.ReadFile(filepath.Join("testdata", filepath.Base(w.Path)))
		if e != nil {
			t.Fatal(e)
		}
		a[w.Path] = v
		b[w.Path], e = Propose(w.Path, v)
		if e != nil {
			t.Fatal(e)
		}
	}
	return a, b
}
func TestCurrentMainKeepsLinuxCoverage(t *testing.T) {
	a, b := fixtures(t)
	for file, v := range b {
		if err := parse(v); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(v), "persist-credentials: false") {
			t.Fatal("credentials persisted")
		}
		if strings.Contains(string(v), "self-hosted") {
			t.Fatal("unprovided runner selected")
		}
		if file == ".github/workflows/rust.yml" {
			if !strings.Contains(string(v), "os: [ubuntu-latest, windows-latest]") {
				t.Fatal("Linux-only acceptance lost")
			}
			for _, command := range []string{"--workspace --all-targets", "--workspace --doc", "--workspace --bins"} {
				if strings.Count(string(v), command) != strings.Count(string(a[file]), command) {
					t.Fatalf("command changed: %s", command)
				}
			}
			if strings.Count(string(v), "Record release binary hashes") != 1 {
				t.Fatal("artifact proof step missing")
			}
		}
		if !bytes.Equal(bytes.ReplaceAll(bytes.ReplaceAll(v, []byte(concurrency), nil), []byte(checkout), []byte("${{ github.sha }}")), a[file]) && file != ".github/workflows/rust.yml" {
			t.Fatal("unrelated workflow bytes changed")
		}
	}
}
func TestRejectChangedMainAndInvalidWorkflow(t *testing.T) {
	for _, w := range Workflows() {
		b, e := os.ReadFile(filepath.Join("testdata", filepath.Base(w.Path)))
		if e != nil {
			t.Fatal(e)
		}
		for _, bad := range [][]byte{nil, append(bytes.Clone(b), []byte("\npermissions: write-all\n")...), bytes.Replace(b, []byte("contents: read"), []byte("contents: write"), 1)} {
			if _, err := Propose(w.Path, bad); err == nil {
				t.Fatal("changed input accepted")
			}
		}
	}
	for _, bad := range []string{"jobs: {}\njobs: {}\n", "jobs: &j {}\nother: *j\n", "jobs: {}\n---\njobs: {}\n"} {
		if parse([]byte(bad)) == nil {
			t.Fatal("ambiguous YAML accepted")
		}
	}
}
func sourceFixture(t *testing.T) Snapshot {
	t.Helper()
	a, b := fixtures(t)
	s := Snapshot{Old: Commit{OldHead, "a4e37a064e0f2ea316d0f15d9aaf710abf9b65f6", []string{"f3e26f45682ac04b8a9bf07821ce5b96d83ce012"}},
		Main: Commit{Main, MainTree, nil}, Candidate: Commit{strings.Repeat("a", 40), strings.Repeat("b", 40), []string{OldHead, Main}}, MainFiles: a, CandidateFiles: b}
	for _, w := range Workflows() {
		s.MainTree = append(s.MainTree, Entry{w.Path, "100644", "blob", Blob(a[w.Path])})
		s.CandidateTree = append(s.CandidateTree, Entry{w.Path, "100644", "blob", Blob(b[w.Path])})
	}
	s.MainTree = append(s.MainTree, Entry{AssetTestPath, "100644", "blob", strings.Repeat("c", 40)}, Entry{"Cargo.lock", "100644", "blob", strings.Repeat("d", 40)})
	s.CandidateTree = append(s.CandidateTree, Entry{AssetTestPath, "100644", "blob", AssetTestBlob}, Entry{"Cargo.lock", "100644", "blob", strings.Repeat("d", 40)})
	fixture, err := os.ReadFile("testdata/cli_contract.rs")
	if err != nil {
		t.Fatal(err)
	}
	if Blob(fixture) != AssetTestBlob {
		t.Fatal("canonical existing fixture blob mismatch")
	}
	s.CandidateFiles[AssetTestPath] = fixture
	return s
}
func TestFixedHostedMerge(t *testing.T) {
	if err := Verify(sourceFixture(t)); err != nil {
		t.Fatal(err)
	}
}
func TestHostedMergeRejectsUnrelatedOrStaleChanges(t *testing.T) {
	for _, kind := range []string{"base", "parentorder", "extra-parent", "oldtip", "leaf", "tree", "mode", "missing", "duplicate", "fixture", "workflow", "unrelated"} {
		t.Run(kind, func(t *testing.T) {
			s := sourceFixture(t)
			switch kind {
			case "base":
				s.Main.SHA = strings.Repeat("e", 40)
			case "parentorder":
				s.Candidate.Parents = []string{Main, OldHead}
			case "extra-parent":
				s.Candidate.Parents = append(s.Candidate.Parents, Main)
			case "oldtip":
				s.Old.SHA = Main
			case "leaf":
				s.CandidateTree[0].SHA = strings.Repeat("e", 40)
			case "tree":
				s.CandidateTree[0].Path = "../" + s.CandidateTree[0].Path
			case "mode":
				s.CandidateTree[0].Mode = "100755"
			case "missing":
				s.CandidateTree = s.CandidateTree[1:]
			case "duplicate":
				s.CandidateTree[1] = s.CandidateTree[0]
			case "fixture":
				s.CandidateFiles[AssetTestPath] = []byte("test result: ok. 0 passed")
			case "workflow":
				s.CandidateFiles[".github/workflows/rust.yml"] = bytes.Replace(s.CandidateFiles[".github/workflows/rust.yml"], []byte("[ubuntu-latest, windows-latest]"), []byte("[windows-latest]"), 1)
			case "unrelated":
				s.CandidateTree[len(s.CandidateTree)-1].SHA = strings.Repeat("e", 40)
			}
			if Verify(s) == nil {
				t.Fatal("modified generation accepted")
			}
		})
	}
}
