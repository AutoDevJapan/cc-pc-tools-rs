package main

import (
	"bytes"
	"reflect"
	"sort"

	"gopkg.in/yaml.v3"
)

// Retain comments, original key order and action version annotations while
// changing only the explicitly generated fields.
func encodePreserving(original []byte, value object) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(original, &doc); err != nil {
		return nil, err
	}
	if err := overlay(doc.Content[0], value); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	e := yaml.NewEncoder(&out)
	e.SetIndent(2)
	if err := e.Encode(&doc); err != nil {
		return nil, err
	}
	return out.Bytes(), e.Close()
}

func overlay(n *yaml.Node, value any) error {
	var previous any
	if err := n.Decode(&previous); err != nil {
		return err
	}
	if reflect.DeepEqual(previous, value) {
		return nil
	}
	switch v := value.(type) {
	case map[string]any:
		if n.Kind == yaml.MappingNode {
			var content []*yaml.Node
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				key, child := n.Content[i], n.Content[i+1]
				if val, exists := v[key.Value]; exists {
					if err := overlay(child, val); err != nil {
						return err
					}
					content = append(content, key, child)
					seen[key.Value] = true
				}
			}
			var extra []string
			for key := range v {
				if !seen[key] {
					extra = append(extra, key)
				}
			}
			sort.Strings(extra)
			for _, key := range extra {
				var child yaml.Node
				if err := child.Encode(v[key]); err != nil {
					return err
				}
				content = append(content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &child)
			}
			n.Content = content
			return nil
		}
	case []any:
		if n.Kind == yaml.SequenceNode {
			old := n.Content
			used := map[int]bool{}
			n.Content = nil
			for _, val := range v {
				var selected *yaml.Node
				for i, child := range old {
					if used[i] {
						continue
					}
					var prior any
					if err := child.Decode(&prior); err != nil {
						return err
					}
					if sameSequenceItem(prior, val) {
						selected = child
						used[i] = true
						break
					}
				}
				if selected == nil {
					selected = &yaml.Node{}
					if err := selected.Encode(val); err != nil {
						return err
					}
				} else if err := overlay(selected, val); err != nil {
					return err
				}
				n.Content = append(n.Content, selected)
			}
			return nil
		}
	}
	var replacement yaml.Node
	if err := replacement.Encode(value); err != nil {
		return err
	}
	replacement.HeadComment, replacement.LineComment, replacement.FootComment = n.HeadComment, n.LineComment, n.FootComment
	*n = replacement
	return nil
}

func sameSequenceItem(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if !aok || !bok {
		return false
	}
	for _, key := range []string{"name", "uses"} {
		if av, ok := am[key]; ok {
			if bv, ok := bm[key]; ok {
				return av == bv
			}
		}
	}
	return false
}
