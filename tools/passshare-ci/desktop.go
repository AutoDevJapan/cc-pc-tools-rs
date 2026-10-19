package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// The Windows-only baseline is the handed-off, independently reviewed PR721.
// Preserve its Windows checks and fail propagation while avoiding packaging
// unrelated PR changes. The event entry point remains unconditional.
func gateDesktop(w object) error {
	jobs := w["jobs"].(object)
	smoke := jobs["smoke"].(object)
	if smoke["runs-on"] != "windows-latest" {
		return fmt.Errorf("expected reviewed Windows smoke")
	}
	steps := smoke["steps"].([]any)
	if len(steps) == 0 || steps[0].(object)["uses"] != checkout {
		return fmt.Errorf("unexpected desktop checkout")
	}
	steps[0].(object)["with"] = object{"fetch-depth": 0, "persist-credentials": false}
	ordered := []any{steps[0], object{
		"name": "Determine affected desktop inputs", "id": "scope", "shell": "bash",
		"env": object{"BASE_SHA": "${{ github.event.pull_request.base.sha || github.event.before }}", "EVENT_NAME": "${{ github.event_name }}"},
		"run": changedPaths,
	}}
	var native any
	for _, raw := range steps[1:] {
		step := raw.(object)
		if _, exists := step["if"]; exists {
			return fmt.Errorf("unexpected existing Windows smoke condition")
		}
		step["if"] = "steps.scope.outputs.desktop == 'true'"
		if step["name"] == "Run desktop smoke tests" {
			native = step
			continue
		}
		ordered = append(ordered, step)
	}
	if native == nil {
		return fmt.Errorf("reviewed Windows native acceptance missing")
	}
	ordered = append(ordered, native) // Type checking fails before native compilation.
	smoke["steps"] = ordered
	smoke["outputs"] = object{"desktop": "${{ steps.scope.outputs.desktop }}"}
	build := jobs["build"].(object)
	if build["needs"] != "smoke" {
		return fmt.Errorf("unexpected desktop build dependency")
	}
	build["if"] = "needs.smoke.outputs.desktop == 'true'"
	return nil
}

func generateDesktop(input, output string) error {
	b, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	w, err := decode(b)
	if err != nil {
		return err
	}
	if err := gateDesktop(w); err != nil {
		return err
	}
	b, err = encodePreserving(b, w)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "desktop-build.yml"), b, 0644)
}
