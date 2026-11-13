package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerRuleDiscordOperationalProjectRegistration(t *testing.T) {
	config, access := ownerRuleFixture(t)
	root := filepath.Join(filepath.Dir(config.dev), "communication-work")
	output := filepath.Join(root, "work", "inventory.private.json")
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		t.Fatal("fixture setup")
	}
	// Use the real generated department projection, not an injected self grant.
	access.membership = ownerDepartmentMembership
	projects := object{"Discord チェック担当": root}
	for _, path := range []string{root, filepath.Dir(output), output} {
		v := ownerRuleWith(context.Background(), path, projects, access, config, nil)
		if !v.AccessAllowed || v.Basis != "self_project_registration" {
			t.Fatal("registered non-Git communication work did not retain its department", v.Basis)
		}
	}
	// Neither the declaration alone nor a similarly named registration grants
	// authority over a location outside the actual registered working root.
	other := filepath.Join(filepath.Dir(root), "unattributed-work")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal("fixture setup")
	}
	for _, row := range []struct {
		path     string
		projects object
	}{
		{other, projects},
		{output, object{}},
		{output, object{"Discord チェック担当-other": root}},
	} {
		v := ownerRuleWith(context.Background(), row.path, row.projects, access, config, nil)
		if v.AccessAllowed || v.Basis != "owner_rule_unattributed_location" {
			t.Fatal("operational project declaration widened unrelated work")
		}
	}
	access.denied = func(path string) bool { return same(path, output) }
	if v := ownerRuleWith(context.Background(), output, projects, access, config, nil); v.AccessAllowed || v.Basis != "legacy_exclusion" {
		t.Fatal("project membership overrode existing protection")
	}
}
