package main

import (
	"reflect"
	"regexp"
	"testing"
)

// Compare the retained source declaration, including absence, with every
// emitted policy receipt. A missing true is different from no declaration.
func TestPreparedDockerInvocationReceiptsMatchAllSixteenSources(t *testing.T) {
	var sources []struct {
		Path    string `json:"path"`
		SHA     string `json:"sha256"`
		Content string `json:"content"`
	}
	loadPublicReceipt(t, "skills-3080-docker-invocation.json", &sources)
	var plan struct {
		Targets []struct {
			Policy []struct {
				Path     string `json:"path"`
				SHA      string `json:"sha256"`
				Implicit *bool  `json:"allow_implicit_invocation"`
			} `json:"upstream_agent_policy"`
		} `json:"targets"`
	}
	loadPublicReceipt(t, "skills-3080-batch03.json", &plan)
	index := map[string]int{}
	for i, r := range sources {
		if _, exists := index[r.Path]; exists {
			t.Fatal("source repeated")
		}
		index[r.Path] = i
	}
	pattern := regexp.MustCompile(`(?m)^\s+allow_implicit_invocation:\s*(true|false)\s*$`)
	seen := map[string]bool{}
	for _, target := range plan.Targets {
		if len(target.Policy) != 2 {
			t.Fatal("target lost source policy metadata")
		}
		for _, receipt := range target.Policy {
			i, ok := index[receipt.Path]
			if !ok || seen[receipt.Path] || sources[i].SHA != receipt.SHA {
				t.Fatal("policy source identity differs")
			}
			seen[receipt.Path] = true
			matches := pattern.FindAllStringSubmatch(sources[i].Content, -1)
			if len(matches) > 1 {
				t.Fatal("ambiguous retained policy declaration")
			}
			var actual *bool
			if len(matches) == 1 {
				v := matches[0][1] == "true"
				actual = &v
			}
			if !reflect.DeepEqual(receipt.Implicit, actual) {
				t.Fatalf("%s: retained policy value differs", receipt.Path)
			}
		}
	}
	if len(seen) != len(sources) {
		t.Fatal("not every retained metadata source used exactly once")
	}
}
