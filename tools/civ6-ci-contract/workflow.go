package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

type Workflow struct{ Path, Job, OriginalDigest, OriginalRoute string }

var workflows = []Workflow{
	{".github/workflows/rust.yml", "test", "70cf12bf1df1216d596513f8e325d009ccacf8180b7ae801018d5844108debbc", "${{ matrix.os }}"},
	{".github/workflows/native-driver.yml", "native-driver", "909fbf55da313dbc56e2e3bb431bc543e405b3790b859e6af35456d2ebc28404", "windows-latest"},
	{".github/workflows/live-chain.yml", "live-chain", "b9fae3ef7ffe726fe86f81b96cbe60b3b1fb40efb1ba74ffda23fdf69613ccb6", "windows-latest"},
}

func Workflows() []Workflow { return append([]Workflow(nil), workflows...) }
func WorkflowByPath(path string) (Workflow, bool) {
	for _, w := range workflows {
		if w.Path == path {
			return w, true
		}
	}
	return Workflow{}, false
}
func field(n *yaml.Node, key string) *yaml.Node {
	if n != nil && n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return n.Content[i+1]
			}
		}
	}
	return nil
}
func parse(data []byte) (*yaml.Node, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("workflow size rejected")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	var n, extra yaml.Node
	if d.Decode(&n) != nil || len(n.Content) != 1 || n.Content[0].Kind != yaml.MappingNode || d.Decode(&extra) != io.EOF {
		return nil, errors.New("one mapping document required")
	}
	var walk func(*yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return errors.New("workflow aliases rejected")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
					return errors.New("workflow mapping rejected")
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if e := walk(c); e != nil {
				return e
			}
		}
		return nil
	}
	if e := walk(&n); e != nil {
		return nil, e
	}
	return n.Content[0], nil
}
func semantic(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			m[n.Content[i].Value] = semantic(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		a := []any{}
		for _, c := range n.Content {
			a = append(a, semantic(c))
		}
		return a
	default:
		return []string{n.Tag, n.Value}
	}
}

const CheckoutRef = "${{ github.event.pull_request.head.sha || github.sha }}"
const DoctestCommand = "cargo test --locked --manifest-path tools/Cargo.toml --workspace --doc"

func Route(w Workflow) string {
	if w.Job == "test" {
		return `${{ (github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name != github.repository) && matrix.os || fromJSON(matrix.os == 'ubuntu-latest' && '["self-hosted","Linux","X64","civ6-ci-linux"]' || '["self-hosted","Windows","X64","civ6-ci-windows"]') }}`
	}
	return `${{ fromJSON((github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name != github.repository) && '["windows-latest"]' || '["self-hosted","Windows","X64","civ6-ci-windows"]') }}`
}

// Propose changes only routing and checkout source/auth. Byte-level edits keep
// every original command and the parent's concurrency text exactly intact.
func Propose(data []byte, w Workflow) ([]byte, error) {
	canonical, ok := WorkflowByPath(w.Path)
	if !ok || canonical != w || Digest(data) != w.OriginalDigest {
		return nil, errors.New("original workflow fingerprint differs")
	}
	before, e := parse(data)
	if e != nil {
		return nil, e
	}
	oldLine := "    runs-on: " + w.OriginalRoute
	if bytes.Count(data, []byte(oldLine)) != 1 {
		return nil, errors.New("original route span differs")
	}
	out := bytes.Replace(data, []byte(oldLine), []byte("    runs-on: "+Route(w)), 1)
	if w.Job == "test" {
		marker := "      - name: Install Rust\n"
		if bytes.Count(out, []byte(marker)) != 1 {
			return nil, errors.New("original checkout boundary differs")
		}
		out = bytes.Replace(out, []byte(marker), []byte("        with:\n          persist-credentials: false\n          ref: "+CheckoutRef+"\n"+marker), 1)
		// --all-targets excludes doctests; retain that command and add the
		// documentation acceptance explicitly requested by the original task.
		buildMarker := "      - name: Build all tools\n"
		if bytes.Count(out, []byte(buildMarker)) != 1 {
			return nil, errors.New("original build boundary differs")
		}
		out = bytes.Replace(out, []byte(buildMarker), []byte("      - name: Run workspace doctests\n        run: "+DoctestCommand+"\n"+buildMarker), 1)
	} else {
		marker := "          persist-credentials: false\n"
		if bytes.Count(out, []byte(marker)) != 1 {
			return nil, errors.New("original checkout options differ")
		}
		out = bytes.Replace(out, []byte(marker), []byte(marker+"          ref: "+CheckoutRef+"\n"), 1)
	}
	after, e := parse(out)
	if e != nil {
		return nil, e
	}
	a := field(field(after, "jobs"), w.Job)
	b := field(field(before, "jobs"), w.Job)
	if a == nil || b == nil {
		return nil, errors.New("original job missing")
	}
	*field(a, "runs-on") = *field(b, "runs-on")
	if w.Job == "test" {
		steps := field(a, "steps")
		var kept []*yaml.Node
		docSteps := 0
		for _, s := range steps.Content {
			name, run := field(s, "name"), field(s, "run")
			if name != nil && name.Value == "Run workspace doctests" {
				if run == nil || run.Value != DoctestCommand || len(s.Content) != 4 {
					return nil, errors.New("doctest acceptance step differs")
				}
				docSteps++
			} else {
				kept = append(kept, s)
			}
		}
		if docSteps != 1 {
			return nil, errors.New("explicit doctest acceptance required")
		}
		steps.Content = kept
	}
	count := 0
	for _, s := range field(a, "steps").Content {
		u := field(s, "uses")
		if u == nil || !strings.HasPrefix(u.Value, "actions/checkout@") {
			continue
		}
		count++
		with := field(s, "with")
		if field(with, "ref") == nil || field(with, "ref").Value != CheckoutRef {
			return nil, errors.New("fixed source checkout missing")
		}
		var keep []*yaml.Node
		for i := 0; i < len(with.Content); i += 2 {
			k := with.Content[i].Value
			if k != "ref" && !(w.Job == "test" && k == "persist-credentials") {
				keep = append(keep, with.Content[i:i+2]...)
			}
		}
		with.Content = keep
		if len(keep) == 0 {
			var fields []*yaml.Node
			for i := 0; i < len(s.Content); i += 2 {
				if s.Content[i].Value != "with" {
					fields = append(fields, s.Content[i:i+2]...)
				}
			}
			s.Content = fields
		}
	}
	if count != 1 {
		return nil, errors.New("exactly one checkout required")
	}
	x, _ := json.Marshal(semantic(before))
	y, _ := json.Marshal(semantic(after))
	if !bytes.Equal(x, y) {
		return nil, errors.New("acceptance semantics changed")
	}
	return out, nil
}
