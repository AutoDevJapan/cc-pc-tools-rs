package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceScopedCallIdentity(t *testing.T) {
	a := Receipt{Source: "fixture-A", Call: "same"}
	b := Receipt{Source: "fixture-B", Call: "same"}
	if receiptKey(a) == receiptKey(b) {
		t.Fatal("cross-source call collision")
	}
}

func TestSyntheticJoinProtectsRawOutputAndSourceBoundaries(t *testing.T) {
	private := t.TempDir()
	sentinel := "SYNTHETIC_CUSTOMER_SENTINEL"
	zero := 0
	a := Receipt{Source: "fixture-A/" + sentinel, Call: "shared", Command: "go build scrub391 " + sentinel, Output: sentinel, Exit: &zero, Matched: true}
	b := Receipt{Source: "fixture-B/" + sentinel, Call: "shared", Command: "Set-Content client-projects.txt $dynamic", Output: sentinel, Matched: true}
	selected, _ := os.Create(filepath.Join(private, "selected.private.jsonl"))
	json.NewEncoder(selected).Encode(a)
	json.NewEncoder(selected).Encode(b)
	selected.Close()
	input := filepath.Join(private, "fixture.jsonl")
	f, _ := os.Create(input)
	json.NewEncoder(f).Encode(a)
	json.NewEncoder(f).Encode(b)
	f.Close()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	previous := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = previous }()
	joinAll(input, private)
	w.Close()
	os.Stdout = previous
	public, _ := io.ReadAll(r)
	r.Close()
	if strings.Contains(string(public), sentinel) {
		t.Fatal("raw output leaked")
	}
	raw, e := os.ReadFile(filepath.Join(private, "joined-summary.json"))
	if e != nil {
		t.Fatal(e)
	}
	var summary struct {
		Counts map[string]int `json:"counts"`
		Write  string         `json:"actual_file_write"`
	}
	if json.Unmarshal(raw, &summary) != nil || summary.Counts["groups"] != 2 || summary.Counts["completed_zero"] != 1 || summary.Counts["write_unproven"] != 1 || summary.Write != "unproven" {
		t.Fatal("cross-source completion or zero exit promoted")
	}
}
func TestNoQuotedExitPromotion(t *testing.T) {
	for _, s := range []string{`prompt: Process exited with code 0`, `{"text":"exit_code:0"}`, `{"output":"exit_code:0","session_id":71}`, `{"exit_code":0,"session_id":71}`, "Script running with cell ID fixture"} {
		if _, ok := terminalExit(Receipt{Output: s}); ok {
			t.Fatalf("nonterminal fixture accepted: %q", s)
		}
	}
	for _, s := range []string{"Process exited with code 0\n", `{"exit_code":0,"session_id":null}`} {
		n, ok := terminalExit(Receipt{Output: s})
		if !ok || n != 0 {
			t.Fatal("terminal envelope lost")
		}
	}
}
func TestToolOutputContentArrays(t *testing.T) {
	for _, kind := range []string{"text", "input_text", "output_text"} {
		b, _ := json.Marshal([]map[string]string{{"type": kind, "text": "Script completed"}})
		if enclosingState(toolTexts(b)) != "enclosing_completed" {
			t.Fatal("output variant lost")
		}
	}
	if enclosingState([]string{"stdout", "Script completed"}) != "unproven" {
		t.Fatal("stdout promoted")
	}
	if enclosingState([]string{"Script running with cell ID fixture-7"}) != "pending" {
		t.Fatal("pending lost")
	}
}
func TestRejectQuotedEvents(t *testing.T) {
	for _, b := range []string{`{"type":"message","content":"function_call_output"}`, `{"type":"function_call_output","call_id":""}`, `{"type":"response_item","payload":{"type":"message","call_id":"fixture"}}`} {
		if _, ok := unwrapEvent([]byte(b)); ok {
			t.Fatal("quoted or unidentified event promoted")
		}
	}
	if _, ok := unwrapEvent([]byte(`{"type":"response_item","call_id":"forged-carrier","payload":{"type":"function_call"}}`)); ok {
		t.Fatal("carrier identity inherited")
	}
	b := []byte(`{"type":"response_item","payload":{"type":"custom_tool_call","call_id":"fixture","input":"data"}}`)
	e, ok := unwrapEvent(b)
	if !ok || eventCommand(e) != "data" {
		t.Fatal("custom call lost")
	}
}
func TestLiteralExtractionAndUnresolvedVariables(t *testing.T) {
	s := "@'\nfixture-A\nfixture-B\n'@ | Set-Content -LiteralPath 'D:/CC/Tools/client-projects.txt'"
	got := literals(s)
	if len(got) != 1 || string(got[0]) != "fixture-A\nfixture-B" {
		t.Fatal("literal lost")
	}
	for _, s := range []string{"Set-Content -LiteralPath $path -Value $value", `Set-Content -LiteralPath 'D:/CC/Tools/client-projects.txt' -Value $value`, `os.WriteFile(path, content, 0600)`} {
		if len(literals(s)) != 0 {
			t.Fatal("dynamic value invented")
		}
	}
}
func TestPublicFindingContainsNoRawStrings(t *testing.T) {
	secret := "SYNTHETIC_CUSTOMER_SENTINEL"
	zero := 0
	r := Receipt{Call: secret, Source: "Z:/" + secret, Command: secret, Output: secret, Exit: &zero}
	b, e := json.Marshal(classify(19, []byte(secret), r))
	if e != nil || strings.Contains(string(b), secret) || strings.Contains(string(b), "Z:/") {
		t.Fatal("raw source leaked")
	}
	// A zero exit remains metadata: no original-file proof field can be produced.
	if strings.Contains(string(b), "original_proven") {
		t.Fatal("shell result promoted to original proof")
	}
}
func TestSourceAllowlist(t *testing.T) {
	for _, p := range []string{"D:/unrelated/source.jsonl", "C:/Users/makhmeto/AppData/Local/Google/Chrome/profile.jsonl", "D:/CC/Tools/client-projects.txt", "D:/CC/Tools/codex-jobs/jobs/../../outside.jsonl"} {
		if allowedSource(p) {
			t.Fatal("scope expanded")
		}
	}
	if !allowedSource("D:/CC/Tools/codex-jobs/jobs/fixture/session.jsonl") {
		t.Fatal("operational source lost")
	}
}
