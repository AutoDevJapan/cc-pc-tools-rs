package main

import (
	"strings"
	"testing"
)

func TestOriginalCommitBytesNeverInventsAnotherCommit(t *testing.T) {
	var c originalCommit
	c.Tree.SHA = strings.Repeat("a", 40)
	c.Parents = []struct{ SHA string }{{strings.Repeat("b", 40)}}
	c.Author.Name = "Fixture"
	c.Author.Email = "fixture@example.invalid"
	c.Author.Date = "2000-01-01T00:00:00Z"
	c.Committer = c.Author
	c.Message = "fixed fixture"
	body := "tree " + c.Tree.SHA + "\nparent " + c.Parents[0].SHA + "\nauthor Fixture <fixture@example.invalid> 946684800 +0900\ncommitter Fixture <fixture@example.invalid> 946684800 +0000\n\nfixed fixture\n"
	expected := gitObjectID("commit", []byte(body))
	b, e := originalCommitBytes(c, expected)
	if e != nil || string(b) != body {
		t.Fatalf("exact original bytes unavailable: %v", e)
	}
	if _, e = originalCommitBytes(c, strings.Repeat("c", 40)); e == nil {
		t.Fatal("replacement commit accepted")
	}
	c.Verification.Payload = body
	b, e = originalCommitBytes(c, expected)
	if e != nil || string(b) != body {
		t.Fatal("verified payload not retained")
	}
}
