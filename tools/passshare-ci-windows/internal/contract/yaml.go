package contract

import (
	"bytes"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"strings"
)

func sequenceContains(n *yaml.Node, value string) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value == value
	}
	for _, v := range n.Content {
		if v.Value == value {
			return true
		}
	}
	return false
}
func hasCommand(job *yaml.Node, command, dir, envKey, envValue string) bool {
	steps := field(job, "steps")
	if steps == nil {
		return false
	}
	for _, step := range steps.Content {
		// A disabled or optional step cannot establish a removal prerequisite.
		if field(step, "if") != nil || field(step, "continue-on-error") != nil {
			continue
		}
		run := field(step, "run")
		if run == nil {
			continue
		}
		if dir != "" && (field(step, "working-directory") == nil || field(step, "working-directory").Value != dir) {
			continue
		}
		if envKey != "" {
			env := field(field(step, "env"), envKey)
			if env == nil || env.Value != envValue {
				continue
			}
		}
		for _, line := range strings.Split(run.Value, "\n") {
			if strings.TrimSpace(line) == command {
				return true
			}
		}
	}
	return false
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
