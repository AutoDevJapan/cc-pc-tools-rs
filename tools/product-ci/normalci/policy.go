package normalci

import (
	"bytes"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"strings"
)

const RepoID int64 = 1154066087
const RouteVariable = "CC_PRODUCT_2737_CI"

func RunnerExpression(r Route) string {
	return "${{ vars." + RouteVariable + " == 'enabled' && github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/codex/todo2641-http-response-pseudo-20261015' && '" + r.Proposed + "' || '" + r.Previous + "' }}"
}

type Route struct {
	Job      string `json:"job"`
	OS       string `json:"required_os"`
	Previous string `json:"previous_label"`
	Proposed string `json:"proposed_label"`
}

func Routes() []Route {
	return []Route{
		{"verify", "Linux", "ubuntu-latest", "cc-product-2737-1154066087-linux"},
		{"windows-native-check", "Windows", "windows-2025", "cc-product-2737-1154066087-windows"},
		{"pr-tree-receipt", "Linux", "ubuntu-latest", "cc-product-2737-1154066087-linux"},
		{"ci-gate", "Linux", "ubuntu-latest", "cc-product-2737-1154066087-linux"},
	}
}

func ParseYAML(data []byte) (*yaml.Node, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("normal CI workflow size rejected")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	var root, extra yaml.Node
	if d.Decode(&root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("normal CI workflow structure rejected")
	}
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("normal CI multiple documents rejected")
	}
	var visit func(*yaml.Node) error
	visit = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return errors.New("normal CI aliases rejected")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
					return errors.New("normal CI mapping key rejected")
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if e := visit(c); e != nil {
				return e
			}
		}
		return nil
	}
	if e := visit(&root); e != nil {
		return nil, e
	}
	return root.Content[0], nil
}

func Field(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func CountWindowsCargo(root *yaml.Node) (int, error) {
	return countWindowsCargo(root, 13)
}

// The new sixteen-command contract is admitted only for the explicitly
// delivered workflow bytes. Plan verifies their digest before calling this.
// Legacy proposals and fixtures retain their original thirteen-command check.
func CountWindowsCargoForWorkflow(root *yaml.Node, workflowSHA string) (int, error) {
	want := 13
	if workflowSHA == "fa5a017fca45710ebccae6a1ca7dfc70070199d9eacb8d94ddb6797d93320220" {
		want = 16
	}
	return countWindowsCargo(root, want)
}

func countWindowsCargo(root *yaml.Node, want int) (int, error) {
	job := Field(Field(root, "jobs"), "windows-native-check")
	steps := Field(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return 0, errors.New("normal CI Windows steps unavailable")
	}
	count := 0
	for _, step := range steps.Content {
		if run := Field(step, "run"); run != nil && run.Kind == yaml.ScalarNode {
			for _, line := range strings.Split(run.Value, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "cargo ") {
					count++
				}
			}
		}
	}
	if count != want {
		return count, errors.New("fixed normal CI Windows command contract changed")
	}
	return count, nil
}
