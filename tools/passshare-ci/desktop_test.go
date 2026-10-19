package main

import (
	"os"
	"reflect"
	"testing"
)

func TestWindowsDistributionKeepsEveryOriginalCheck(t *testing.T) {
	b, err := os.ReadFile("testdata/before/desktop-windows-reviewed.yml")
	if err != nil {
		t.Fatal(err)
	}
	before, err := decode(b)
	if err != nil {
		t.Fatal(err)
	}
	after, err := decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := gateDesktop(after); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"on", "permissions", "concurrency", "name"} {
		if !reflect.DeepEqual(before[key], after[key]) {
			t.Fatalf("desktop setting %s changed", key)
		}
	}
	a, z := before["jobs"].(object), after["jobs"].(object)
	oldSteps := a["smoke"].(object)["steps"].([]any)
	newSteps := z["smoke"].(object)["steps"].([]any)
	if len(newSteps) != len(oldSteps)+1 {
		t.Fatal("Windows check lost or duplicated")
	}
	if newSteps[1].(object)["run"] != changedPaths {
		t.Fatal("desktop selector differs from the tested consumer selector")
	}
	for _, old := range oldSteps[1:] {
		var step object
		for _, candidate := range newSteps[2:] {
			if sameSequenceItem(old, candidate) {
				step = candidate.(object)
				break
			}
		}
		if step == nil {
			t.Fatalf("original Windows step missing: %v", old)
		}
		if step["if"] != "steps.scope.outputs.desktop == 'true'" {
			t.Fatal("Windows step lacks explicit scope")
		}
		copy := object{}
		for k, v := range step {
			if k != "if" {
				copy[k] = v
			}
		}
		if !reflect.DeepEqual(old, copy) {
			t.Fatalf("Windows command/env/config altered: %v", old.(object)["name"])
		}
	}
	if newSteps[len(newSteps)-1].(object)["name"] != "Run desktop smoke tests" {
		t.Fatal("native compilation must follow inexpensive type checks")
	}
	build := z["build"].(object)
	if build["if"] != "needs.smoke.outputs.desktop == 'true'" {
		t.Fatal("unrelated PR would package desktop")
	}
	delete(build, "if")
	if !reflect.DeepEqual(a["build"], build) {
		t.Fatal("reviewed Windows build, matrix or artifact configuration changed")
	}
}
