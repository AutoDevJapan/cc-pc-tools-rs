package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePlanStrict(t *testing.T) {
	good, _ := json.Marshal(rustPlan())
	if _, e := ParsePlan(good); e != nil {
		t.Fatal(e)
	}
	for name, p := range map[string]Plan{"actions": codeQLPlan("Analyze (actions)"), "rust": codeQLPlan("Analyze (rust)")} {
		b, _ := json.Marshal(p)
		if k, e := mustPlan(t, b).kind(); e != nil || k != KindCodeQL {
			t.Fatalf("%s: %v %v", name, k, e)
		}
	}
	bad := map[string]string{
		"unknown field":   strings.Replace(string(good), "{", "{\"command\":\"evil\",", 1),
		"trailing doc":    string(good) + "{}",
		"empty":           "",
		"wrong repo":      strings.Replace(string(good), "1392152644", "1", 1),
		"short head":      strings.Replace(string(good), rep("1", 40), "abc", 1),
		"windows job":     strings.Replace(string(good), "test (ubuntu-latest)", "test (windows-latest)", 1),
		"native workflow": strings.Replace(string(good), ".github/workflows/rust.yml", ".github/workflows/native-driver.yml", -1),
		"no digest":       strings.Replace(string(good), rep("4", 64), "", 1),
		"zero attempt":    strings.Replace(string(good), "\"attempt\":1", "\"attempt\":0", 1),
	}
	for name, s := range bad {
		if _, e := ParsePlan([]byte(s)); e == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	dyn := codeQLPlan("Analyze (rust)")
	dyn.WorkflowDigest = rep("4", 64)
	b, _ := json.Marshal(dyn)
	if _, e := ParsePlan(b); e == nil {
		t.Fatal("generated CodeQL with a workflow digest accepted")
	}
	other := codeQLPlan("Adjust Configuration")
	b, _ = json.Marshal(other)
	if _, e := ParsePlan(b); e == nil {
		t.Fatal("non-analysis CodeQL job accepted")
	}
}

func mustPlan(t *testing.T, b []byte) Plan {
	t.Helper()
	p, e := ParsePlan(b)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestRegistrationLabels(t *testing.T) {
	c, d, e := registrationLabels(rustPlan(), []string{"self-hosted", "Linux", "X64", linuxLabel, jobLabel(rustPlan())})
	if e != nil || c != linuxLabel+","+jobLabel(rustPlan()) || !d {
		t.Fatalf("%v %v %v", c, d, e)
	}
	c, d, e = registrationLabels(codeQLPlan("Analyze (rust)"), []string{codeQLLabel})
	if e != nil || c != codeQLLabel || d {
		t.Fatalf("%v %v %v", c, d, e)
	}
	for _, bad := range [][]string{nil, {"self-hosted", "Linux", "X64"}, {"self-hosted", "Windows", "X64", "civ6-ci-windows"}, {"self-hosted", "Linux", "X64", linuxLabel, "extra"}, {linuxLabel}} {
		if _, _, e = registrationLabels(rustPlan(), bad); e == nil {
			t.Fatalf("accepted Rust labels %v", bad)
		}
	}
	for _, bad := range [][]string{nil, {linuxLabel}, {codeQLLabel, "extra"}, {"self-hosted", "Linux", "X64", codeQLLabel}} {
		if _, _, e = registrationLabels(codeQLPlan("Analyze (rust)"), bad); e == nil {
			t.Fatalf("accepted CodeQL labels %v", bad)
		}
	}
}

func TestRegistrationRejectsAnotherRunOrAttemptLabel(t *testing.T) {
	p := rustPlan()
	for _, change := range []func(*Plan){func(q *Plan) { q.RunID++ }, func(q *Plan) { q.Attempt++ }} {
		other := p
		change(&other)
		labels := []string{"self-hosted", "Linux", "X64", linuxLabel, jobLabel(other)}
		if _, _, e := registrationLabels(p, labels); e == nil {
			t.Fatal("another queued run/attempt could be registered")
		}
	}
}
