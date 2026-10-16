package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestGitHubMutationPayloadStaysOffArguments(t *testing.T) {
	c, e := githubMutationCommand(context.Background(), "POST", "repos/AutoDevJapan/opaque/actions/runners/registration-token", map[string]string{"token": "synthetic-registration-value"})
	if e != nil {
		t.Fatal(e)
	}
	for _, arg := range c.Args {
		if strings.Contains(arg, "synthetic-registration-value") {
			t.Fatal("credential entered process arguments")
		}
	}
	b, e := io.ReadAll(c.Stdin)
	if e != nil || !strings.Contains(string(b), "synthetic-registration-value") {
		t.Fatal("structured stdin payload missing")
	}
	d, e := githubMutationCommand(context.Background(), "DELETE", "repos/AutoDevJapan/opaque/actions/runners/1", nil)
	if e != nil {
		t.Fatal(e)
	}
	if d.Stdin != nil {
		t.Fatal("DELETE unexpectedly has a payload")
	}
	for _, arg := range d.Args {
		if arg == "--input" {
			t.Fatal("DELETE unexpectedly reads JSON stdin")
		}
	}
}

func TestGitHubMutationFailureIsOpaqueButClassified(t *testing.T) {
	detail := []byte("HTTP 422: private-repo synthetic-registration-value")
	e := classifiedGitHubError(errors.New("private-repo path"), detail).Error()
	if !strings.Contains(e, "http=422") || !strings.Contains(e, digest(detail)) {
		t.Fatal("HTTP classification or fingerprint missing")
	}
	if strings.Contains(e, "private-repo") || strings.Contains(e, "synthetic-registration-value") {
		t.Fatal("private diagnostic exposed")
	}
}
