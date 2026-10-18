package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialBindingConsumesProductCISchema(t *testing.T) {
	// OS is the product-ci normalCIRoutes schema ("Linux"), not a GOOS value.
	q := linuxJobRequest{Source: fixedSourcePin(), RunID: 17, Attempt: 1, Role: "verify", RouteLease: strings.Repeat("a", 32)}
	raw := []byte(`{"RepoID":1154066087,"RunID":17,"JobID":29,"PR":3613,"Attempt":1,"TimeoutMinutes":30,"OS":"Linux","Role":"verify","Label":"cc-product-2737-1154066087-linux-verify-17","JobName":"verify"}`)
	var b jobBinding
	if json.Unmarshal(raw, &b) != nil {
		t.Fatal("schema fixture invalid")
	}
	b.SHA = q.Source.HeadSHA
	b.Tree = q.Source.TreeSHA
	b.WorkflowSHA = q.Source.WorkflowSHA256
	if !validOfficialBinding(q, b) {
		t.Fatal("actual product-ci OS schema rejected")
	}
	for _, change := range []func(*jobBinding){func(b *jobBinding) { b.RunID++ }, func(b *jobBinding) { b.OS = "Windows" }, func(b *jobBinding) { b.TimeoutMinutes = 31 }, func(b *jobBinding) { b.SHA = "other" }} {
		other := b
		change(&other)
		if validOfficialBinding(q, other) {
			t.Fatal("substituted binding accepted")
		}
	}
}

func TestLiveRequestRejectsExecutableSelection(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "request.json")
	q := linuxJobRequest{Source: fixedSourcePin(), RunID: 17, Attempt: 1, Role: "verify", RouteLease: strings.Repeat("a", 32)}
	raw, e := json.Marshal(q)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(file, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = readJobRequest(file); e != nil {
		t.Fatal("minimal fixed-code request rejected")
	}
	// Positive input first, then the exact former attack field. A real absolute
	// executable exists, but no request field can select it for execution now.
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"ProductCI", "productci", "Executor", "Command"} {
		var injected map[string]any
		if json.Unmarshal(raw, &injected) != nil {
			t.Fatal("fixture invalid")
		}
		injected[key] = exe
		b, e := json.Marshal(injected)
		if e != nil {
			t.Fatal(e)
		}
		if os.WriteFile(file, b, 0600) != nil {
			t.Fatal("fixture write failed")
		}
		if _, e = readJobRequest(file); e == nil {
			t.Fatal("request-selected host executable accepted")
		}
	}
}
