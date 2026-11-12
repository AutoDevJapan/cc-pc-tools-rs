// Package hostedsource checks the finite PR149 merge that retains hosted OS coverage.
// It grants no runner admission, local execution, migration, or cleanup authority.
package hostedsource

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

const OldHead = "2bd3a366f8974ba54df83927102aa5bdad3983e5"
const Main = "f0e993f8dd486620d9b41471456fd106b12c700b"
const MainTree = "08f5d09ac07e071cb773fbd7d8030c1f2b122e1c"
const Generation = "pr149-hosted-main-f0e993f8"
const AssetTestPath = "tools/civ6-assets-rs/tests/cli_contract.rs"
const AssetTestBlob = "18aca67c4a635d8739abfa5a2dc92d21835f8488"

const checkout = "${{ github.event.pull_request.head.sha || github.sha }}"
const concurrency = "concurrency:\n  group: ${{ github.workflow }}-${{ github.event_name }}-${{ github.event.pull_request.number || github.run_id }}\n  cancel-in-progress: ${{ github.event_name == 'pull_request' }}\n\n"
const hashStep = `      - name: Record release binary hashes
        shell: pwsh
        run: |
          $ErrorActionPreference = 'Stop'
          $source = git rev-parse HEAD
          if ($LASTEXITCODE -ne 0 -or $source -notmatch '^[0-9a-f]{40}$') { throw 'Source revision is unavailable' }
          $files = @(Get-ChildItem -LiteralPath './tools/target/release' -File | Where-Object { if ($IsWindows) { $_.Extension -eq '.exe' } else { $_.Extension -eq '' } })
          if ($files.Count -eq 0) { throw 'No release binaries found' }
          $hashes = @($files | ForEach-Object { @{ name = $_.Name; bytes = $_.Length; sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() } })
          @{ source_sha = $source; runner_os = $env:RUNNER_OS; binaries = $hashes } | ConvertTo-Json -Depth 5
`

type Workflow struct{ Path, SHA256 string }

var workflowPins = []Workflow{
	{".github/workflows/rust.yml", "72858e501824f1949b3caa4a298e6610212b27f1382e68b98bd2b7b1a11d077f"},
	{".github/workflows/native-driver.yml", "c2eb4b8e7710e52810e190183c30df40e114a88670357ddb83fc96c772c0414c"},
	{".github/workflows/live-chain.yml", "d1f68680e1be885240d205a4981011a46209dbc27c4fc34a7c54f51ec891f989"},
}

func Workflows() []Workflow  { return append([]Workflow(nil), workflowPins...) }
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Blob(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func parse(b []byte) error {
	if len(b) == 0 || len(b) > 1<<20 {
		return errors.New("bounded workflow required")
	}
	d := yaml.NewDecoder(bytes.NewReader(b))
	var n, extra yaml.Node
	if d.Decode(&n) != nil || len(n.Content) != 1 || n.Content[0].Kind != yaml.MappingNode || d.Decode(&extra) != io.EOF {
		return errors.New("single YAML mapping required")
	}
	var walk func(*yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return errors.New("YAML aliases rejected")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
					return errors.New("duplicate or invalid YAML key")
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(&n)
}

// Propose uses immutable main bytes. Commands, two OS entries, routes, action
// versions, permissions, doctests and error handling remain byte-identical.
func Propose(file string, b []byte) ([]byte, error) {
	known := false
	for _, w := range workflowPins {
		if w.Path == file && w.SHA256 == digest(b) {
			known = true
		}
	}
	if !known {
		return nil, errors.New("fixed main workflow fingerprint differs")
	}
	if err := parse(b); err != nil {
		return nil, err
	}
	out := bytes.Clone(b)
	for _, r := range [][2]string{
		{"jobs:\n", concurrency + "jobs:\n"},
		{"          ref: ${{ github.sha }}\n", "          ref: " + checkout + "\n"},
	} {
		if bytes.Count(out, []byte(r[0])) != 1 {
			return nil, errors.New("fixed main span differs")
		}
		out = bytes.Replace(out, []byte(r[0]), []byte(r[1]), 1)
	}
	if file == ".github/workflows/rust.yml" {
		marker := []byte("      - name: Check existing scenario JSON\n")
		if bytes.Count(out, marker) != 1 {
			return nil, errors.New("postbuild marker differs")
		}
		out = bytes.Replace(out, marker, append([]byte(hashStep), marker...), 1)
	}
	if err := parse(out); err != nil {
		return nil, err
	}
	return out, nil
}

type Commit struct {
	SHA, Tree string
	Parents   []string
}
type Entry struct{ Path, Mode, Type, SHA string }
type Snapshot struct {
	Old, Main, Candidate      Commit
	MainTree, CandidateTree   []Entry
	MainFiles, CandidateFiles map[string][]byte
}

func validSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Verify keeps this hosted source proof disjoint from all prior self-hosted
// generations. No queue, token, budget, source publication or OS state is read.
func Verify(s Snapshot) error {
	if s.Old.SHA != OldHead || s.Old.Tree != "a4e37a064e0f2ea316d0f15d9aaf710abf9b65f6" ||
		len(s.Old.Parents) != 1 || s.Old.Parents[0] != "f3e26f45682ac04b8a9bf07821ce5b96d83ce012" {
		return errors.New("historical tip identity differs")
	}
	if s.Main.SHA != Main || s.Main.Tree != MainTree {
		return errors.New("fixed main identity differs")
	}
	c := s.Candidate
	if !validSHA(c.SHA) || !validSHA(c.Tree) || c.SHA == OldHead || c.SHA == Main || len(c.Parents) != 2 || c.Parents[0] != OldHead || c.Parents[1] != Main {
		return errors.New("exact old-tip/current-main merge parents required")
	}
	expected := map[string]string{AssetTestPath: AssetTestBlob}
	for _, w := range workflowPins {
		want, err := Propose(w.Path, s.MainFiles[w.Path])
		if err != nil {
			return err
		}
		got, ok := s.CandidateFiles[w.Path]
		if !ok || !bytes.Equal(want, got) {
			return errors.New("reviewed hosted workflow differs")
		}
		expected[w.Path] = Blob(got)
	}
	if Blob(s.CandidateFiles[AssetTestPath]) != AssetTestBlob {
		return errors.New("Linux executable-inode fixture differs")
	}
	if len(s.MainTree) == 0 || len(s.MainTree) != len(s.CandidateTree) || len(s.MainTree) > 100000 {
		return errors.New("complete equal tree inventory required")
	}
	prior := map[string]Entry{}
	for _, e := range s.MainTree {
		if !validEntry(e) || prior[e.Path].Path != "" {
			return errors.New("main tree inventory ambiguous")
		}
		prior[e.Path] = e
	}
	seen := map[string]bool{}
	changed := map[string]bool{}
	for _, n := range s.CandidateTree {
		p, ok := prior[n.Path]
		if !ok || !validEntry(n) || seen[n.Path] || n.Mode != p.Mode || n.Type != p.Type {
			return errors.New("path/mode/type inventory differs")
		}
		seen[n.Path] = true
		if want, ok := expected[n.Path]; ok {
			if n.Type != "blob" || n.SHA != want || n.SHA == p.SHA {
				return errors.New("fixed changed leaf differs")
			}
			changed[n.Path] = true
		} else if n.SHA != p.SHA {
			ancestor := false
			for leaf := range expected {
				if n.Type == "tree" && strings.HasPrefix(leaf, n.Path+"/") {
					ancestor = true
				}
			}
			if !ancestor {
				return errors.New("unrelated source changed")
			}
		}
	}
	if len(changed) != len(expected) {
		return errors.New("incomplete expected leaf set")
	}
	return nil
}
func validEntry(e Entry) bool {
	if e.Path == "" || e.Path == "." || strings.HasPrefix(e.Path, "/") || strings.Contains(e.Path, "\\") || path.Clean(e.Path) != e.Path || strings.HasPrefix(e.Path, "../") || !validSHA(e.SHA) {
		return false
	}
	return e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755") || e.Type == "tree" && e.Mode == "040000" || e.Type == "commit" && e.Mode == "160000"
}
