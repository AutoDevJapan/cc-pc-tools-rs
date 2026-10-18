// Package pr148scope holds the one canonical workflow transformation shared by
// the Linux and Windows finite runners. It has no I/O or registration path.
package pr148scope

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"sort"
	"strings"
)

func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Workflows() []Workflow  { return append([]Workflow(nil), originalWorkflows...) }

const Head = "75a4051305b6f47c70ddb59a8d2bf9015666f12c"
const Tree = "cecb9cd535d135a1eb4407882db532c325f42860"
const Base = "1100676d1ed421794c252fb4b15ed3d954e21a5b"
const Branch = "codex/todo2640-workspace-doctests-20261015"
const Dedicated = "github.repository_id == '1392152644' && ((github.event_name == 'pull_request' && github.event.pull_request.number == 148 && github.event.pull_request.head.repo.id == 1392152644) || (github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/" + Branch + "'))"

type Workflow struct {
	Path, Job, OriginalSHA, OriginalRoute string
	RunID                                 int64
	Jobs                                  int
}

var originalWorkflows = []Workflow{
	{".github/workflows/rust.yml", "test", "841ca7d8515d4ce9802effecf38dbf30052560bac3ecd8c3b679d25a598933e9", "${{ matrix.os }}", 38892356460, 2},
	{".github/workflows/native-driver.yml", "native-driver", "cb8fe6b12866934d101ee186c75db152d5a9ec3552182f91a8580967b59f6e8f", "windows-latest", 38892356435, 1},
	{".github/workflows/live-chain.yml", "live-chain", "b151c7841d615ef3e4583a1e36de6b551e6cfc7421ea625f23b6370fc23513b9", "windows-latest", 38892356407, 1},
}

// YAML is parsed before byte-span edits; aliases, duplicate keys and multiple
// documents are rejected rather than selecting an executable interpretation.
func YAML(data []byte) (*yaml.Node, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("workflow size rejected")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	var root, extra yaml.Node
	if d.Decode(&root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode || d.Decode(&extra) != io.EOF {
		return nil, errors.New("workflow document rejected")
	}
	var walk func(*yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return errors.New("workflow alias rejected")
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
	if e := walk(&root); e != nil {
		return nil, e
	}
	return root.Content[0], nil
}
func Field(n *yaml.Node, k string) *yaml.Node {
	if n != nil && n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == k {
				return n.Content[i+1]
			}
		}
	}
	return nil
}
func Offset(data []byte, n *yaml.Node) (int, error) {
	line, col := 1, 1
	for i, ch := range string(data) {
		if line == n.Line && col == n.Column {
			return i, nil
		}
		if ch == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return 0, errors.New("workflow position unavailable")
}

type Edit struct {
	Start, End int
	Text       string
}

func Route(w Workflow) string {
	label := "'cc-product-2736-1392152644-windows-latest'"
	fallback := "'windows-latest'"
	if w.Job == "test" {
		label = "format('cc-product-2736-1392152644-{0}', matrix.os)"
		fallback = "matrix.os"
	}
	return "${{ (" + Dedicated + ") && " + label + " || " + fallback + " }}"
}

// Preserve merge-result checkout for main and every unrelated PR. Only this
// finite candidate uses its explicitly pinned head rather than a PR merge SHA.
func CheckoutRef() string {
	return "${{ (" + Dedicated + ") && (github.event.pull_request.head.sha || github.sha) || github.sha }}"
}

// Preserve all original bytes except runs-on and the checkout ref/auth options.
// Main and unrelated PRs keep their original hosted route. No mutation is sent.
func Propose(data []byte, w Workflow) ([]byte, error) {
	if Digest(data) != w.OriginalSHA {
		return nil, errors.New("fixed workflow digest changed")
	}
	root, e := YAML(data)
	if e != nil {
		return nil, e
	}
	if Field(Field(root, "on"), "workflow_dispatch") == nil {
		return nil, errors.New("original full workflow dispatch trigger required")
	}
	jobs := Field(root, "jobs")
	job := Field(jobs, w.Job)
	if jobs == nil || len(jobs.Content) != 2 || job == nil {
		return nil, errors.New("fixed job set changed")
	}
	route := Field(job, "runs-on")
	if route == nil || route.Kind != yaml.ScalarNode || route.Style != 0 || route.Value != w.OriginalRoute {
		return nil, errors.New("original route rejected")
	}
	off, e := Offset(data, route)
	if e != nil {
		return nil, e
	}
	if off+len(route.Value) > len(data) || string(data[off:off+len(route.Value)]) != route.Value {
		return nil, errors.New("route span rejected")
	}
	edits := []Edit{{off, off + len(route.Value), Route(w)}}
	steps := Field(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return nil, errors.New("steps unavailable")
	}
	checkouts := 0
	for _, s := range steps.Content {
		u := Field(s, "uses")
		if u == nil || !strings.HasPrefix(u.Value, "actions/checkout@") {
			continue
		}
		checkouts++
		if u.Style != 0 {
			return nil, errors.New("checkout token style rejected")
		}
		with := Field(s, "with")
		if with != nil && Field(with, "ref") != nil {
			return nil, errors.New("unexpected original checkout ref")
		}
		off, e := Offset(data, u)
		if e != nil {
			return nil, e
		}
		if !bytes.HasPrefix(data[off:], []byte(u.Value)) {
			return nil, errors.New("checkout span rejected")
		}
		end := bytes.IndexByte(data[off:], '\n')
		if end < 0 {
			return nil, errors.New("checkout line unavailable")
		}
		end += off + 1
		insert := "        with:\n          persist-credentials: false\n          ref: " + CheckoutRef() + "\n"
		if with != nil {
			persist := Field(with, "persist-credentials")
			if persist == nil || persist.Value != "false" || len(with.Content) != 2 {
				return nil, errors.New("checkout options changed")
			}
			off, e = Offset(data, with.Content[0])
			if e != nil {
				return nil, e
			}
			end = bytes.IndexByte(data[off:], '\n')
			if end < 0 {
				return nil, errors.New("checkout option line unavailable")
			}
			end += off + 1
			insert = "          ref: " + CheckoutRef() + "\n"
		}
		edits = append(edits, Edit{end, end, insert})
	}
	if checkouts != 1 {
		return nil, errors.New("exactly one checkout required")
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].Start < edits[j].Start })
	var out bytes.Buffer
	last := 0
	for _, edit := range edits {
		if edit.Start < last {
			return nil, errors.New("overlapping workflow edits")
		}
		out.Write(data[last:edit.Start])
		out.WriteString(edit.Text)
		last = edit.End
	}
	out.Write(data[last:])
	after, e := YAML(out.Bytes())
	if e != nil {
		return nil, e
	}
	// Independently remove the intended changes and compare all parsed values.
	afterJob := Field(Field(after, "jobs"), w.Job)
	Field(afterJob, "runs-on").Value = route.Value
	for stepIndex, s := range Field(afterJob, "steps").Content {
		u := Field(s, "uses")
		if u == nil || !strings.HasPrefix(u.Value, "actions/checkout@") {
			continue
		}
		with := Field(s, "with")
		hadWith := Field(steps.Content[stepIndex], "with") != nil
		var kept []*yaml.Node
		for i := 0; i < len(with.Content); i += 2 {
			if with.Content[i].Value != "ref" && !(!hadWith && with.Content[i].Value == "persist-credentials") {
				kept = append(kept, with.Content[i:i+2]...)
			}
		}
		with.Content = kept
		if len(kept) == 0 {
			var fields []*yaml.Node
			for i := 0; i < len(s.Content); i += 2 {
				if s.Content[i].Value != "with" {
					fields = append(fields, s.Content[i:i+2]...)
				}
			}
			s.Content = fields
		}
	}
	beforeSemantic, _ := json.Marshal(Semantic(root))
	afterSemantic, _ := json.Marshal(Semantic(after))
	if !bytes.Equal(beforeSemantic, afterSemantic) {
		return nil, errors.New("acceptance semantics changed")
	}
	return out.Bytes(), nil
}
func Semantic(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			m[n.Content[i].Value] = Semantic(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		a := []any{}
		for _, c := range n.Content {
			a = append(a, Semantic(c))
		}
		return a
	default:
		return []string{n.Tag, n.Value}
	}
}
