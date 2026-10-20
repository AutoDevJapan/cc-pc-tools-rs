package main

import (
	"reflect"
	"testing"
)

// Runs only with -tags civ6contract against the parent's module: the JSON
// interface between the two modules must stay identical.
func TestBridgeSharesThePlanInterface(t *testing.T) {
	var c Contract = bridge{}
	if e := c.Validate(rustPlan()); e != nil {
		t.Fatal(e)
	}
	if e := c.Validate(codeQLPlan("Analyze (rust)")); e != nil {
		t.Fatal(e)
	}
	if got := c.Labels(rustPlan()); !reflect.DeepEqual(got, []string{"self-hosted", "Linux", "X64", linuxLabel, jobLabel(rustPlan())}) {
		t.Fatalf("labels %v", got)
	}
	if got := c.Labels(codeQLPlan("Analyze (actions)")); !reflect.DeepEqual(got, []string{codeQLLabel}) {
		t.Fatalf("labels %v", got)
	}
	bad := rustPlan()
	bad.RepoID++
	if c.Validate(bad) == nil {
		t.Fatal("foreign repository accepted")
	}
	if c.CheckEvent(rustPlan(), map[string]string{}, nil) == nil {
		t.Fatal("empty job context accepted")
	}
}
