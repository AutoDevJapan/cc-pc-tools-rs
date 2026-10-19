package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSchemaTransferPreservesDefaultsAndRejectsUnrelatedChanges(t *testing.T) {
	old := object{}
	for _, plugin := range []string{"core:app", "core:window"} {
		old[plugin] = object{"default_permission": object{"permissions": []any{"unchanged"}}, "permissions": object{"existing": object{"commands": object{"allow": []any{"existing"}}}}}
	}
	b, err := compactSchema(old)
	if err != nil {
		t.Fatal(err)
	}
	var updated object
	if err := json.Unmarshal(b, &updated); err != nil {
		t.Fatal(err)
	}
	for plugin, names := range map[string][]string{"core:app": {"allow-exit", "deny-exit"}, "core:window": {"allow-set-fullscreen-on-monitor", "deny-set-fullscreen-on-monitor"}} {
		for _, name := range names {
			updated[plugin].(object)["permissions"].(object)[name] = object{"identifier": name}
		}
	}
	n, err := compactSchema(updated)
	if err != nil {
		t.Fatal(err)
	}
	out, err := transferSharedDefinitions(b, n, b)
	if err != nil {
		t.Fatal(err)
	}
	var got object
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, updated) {
		t.Fatal("transferred definitions differ")
	}
	updated["core:app"].(object)["default_permission"] = object{"permissions": []any{"allow-exit"}}
	n, err = compactSchema(updated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transferSharedDefinitions(b, n, b); err == nil {
		t.Fatal("a changed permission grant was accepted")
	}
}
