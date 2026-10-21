package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Print only runner setup and checkout semantics, never product commands,
// secrets, source bodies or logs. Original workflow bytes remain in RAM.
func outlineNormalCI(ctx context.Context) error {
	c := candidates[2]
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	var content struct{ Encoding, Content string }
	if get(ctx, "repos/"+r.FullName+"/contents/.github/workflows/ci.yml?ref="+c.SHA, &content) != nil || content.Encoding != "base64" {
		return errors.New("workflow outline unavailable")
	}
	b, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(content.Content), ""))
	if e != nil || digest(b) != normalCIWorkflowSHA {
		return errors.New("workflow outline source changed")
	}
	doc, e := normalCIYAML(b)
	if e != nil {
		return e
	}
	var result []map[string]any
	for _, route := range normalCIRoutes() {
		job := yamlField(yamlField(doc, "jobs"), route.Job)
		steps := yamlField(job, "steps")
		var setup []map[string]string
		if steps != nil {
			for _, step := range steps.Content {
				use := yamlField(step, "uses")
				if use == nil {
					continue
				}
				item := map[string]string{"uses": use.Value}
				with := yamlField(step, "with")
				for _, name := range []string{"ref", "persist-credentials", "submodules", "node-version", "version", "toolchain", "components", "profile"} {
					if value := yamlField(with, name); value != nil {
						item[name] = value.Value
					}
				}
				setup = append(setup, item)
			}
		}
		condition := ""
		if v := yamlField(job, "if"); v != nil {
			condition = v.Value
		}
		result = append(result, map[string]any{"role": route.Job, "setup": setup, "condition": condition})
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	inputs := map[string]map[string]string{}
	declared := yamlField(yamlField(yamlField(doc, "on"), "workflow_dispatch"), "inputs")
	if declared != nil {
		for n := 0; n+1 < len(declared.Content); n += 2 {
			key, item := declared.Content[n].Value, declared.Content[n+1]
			inputs[key] = map[string]string{}
			for _, field := range []string{"type", "required", "default"} {
				if v := yamlField(item, field); v != nil {
					inputs[key][field] = v.Value
				}
			}
		}
	}
	encoded, e := json.MarshalIndent(map[string]any{"jobs": result, "dispatch_inputs": inputs}, "", "  ")
	if e == nil {
		fmt.Println(string(encoded))
	}
	return e
}
