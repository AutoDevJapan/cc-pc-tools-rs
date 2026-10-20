package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Self-consistent hashes and success booleans do not establish the independent
// original task's authority. This bundle has no original scope or provenance.
func TestCurrentPRRejectsUnqualifiedEvidenceBundle(t *testing.T) {
	b, _, _, _ := currentPRFixture(t, "secret", nil)
	dir := t.TempDir()
	input := currentPRInput{currentPRBinding: b, ReviewFile: filepath.Join(dir, "review.json"), AcceptanceFile: filepath.Join(dir, "acceptance.json")}
	for _, kind := range []string{"review", "acceptance"} {
		original := []byte("synthetic self-assertion; no independent original task receipt")
		v := currentPREvidence{OriginTodo: 3056, RepoID: b.RepoID, PR: b.PR, Head: b.Head, Tree: b.Tree, Kind: kind, Success: true, Must: 0, Original: original, OriginalSHA256: digest(original)}
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		file := input.ReviewFile
		if kind == "review" {
			input.ReviewSHA256 = digest(data)
		} else {
			file = input.AcceptanceFile
			input.AcceptanceSHA256 = digest(data)
		}
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "pin.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCurrentPRInput(file); err == nil {
		t.Fatal("self-authored evidence bundle was admitted without independent original scope; a copied or reserialized input must not create purpose qualification")
	}
}
