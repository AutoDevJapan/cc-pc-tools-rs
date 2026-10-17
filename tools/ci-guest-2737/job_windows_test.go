package main

import (
	"encoding/json"
	"testing"
)

func TestOfficialBindingConsumesProductCISchema(t *testing.T) {
	// OS is the product-ci normalCIRoutes schema ("Linux"), not a GOOS value.
	q := linuxJobRequest{Source: fixedSourcePin(), RunID: 17, Attempt: 1, Role: "verify"}
	raw := []byte(`{"RepoID":1154066087,"RunID":17,"JobID":29,"PR":3613,"Attempt":1,"TimeoutMinutes":30,"OS":"Linux","Role":"verify","Label":"cc-product-2737-1154066087-linux","JobName":"verify"}`)
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
