package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestReadAllowlist(t *testing.T) {
	sha := rep("a", 40)
	for _, ok := range []string{
		"repositories/1392152644",
		"repos/" + repoFull + "/pulls/149",
		"repos/" + repoFull + "/git/commits/" + sha,
		"repos/" + repoFull + "/git/trees/" + sha + "?recursive=1",
		"repos/" + repoFull + "/git/ref/heads/scheduler%2Fbranch",
		"repos/" + repoFull + "/contents/.github/workflows/rust.yml?ref=" + sha,
		"repos/" + repoFull + "/actions/runs/101",
		"repos/" + repoFull + "/actions/runs/101/attempts/2/jobs?per_page=100",
		"repos/" + repoFull + "/actions/runs/101/jobs?per_page=100&page=2",
		"repos/" + repoFull + "/actions/runs?status=queued&per_page=100&page=1",
		"repos/" + repoFull + "/actions/jobs/102",
		"repos/" + repoFull + "/actions/runners?per_page=100",
		"repos/" + repoFull + "/code-scanning/default-setup",
		"repos/" + repoFull + "/compare/" + sha + "..." + rep("b", 40),
	} {
		if e := allowedRead(ok); e != nil {
			t.Errorf("rejected %s: %v", ok, e)
		}
	}
	for _, bad := range []string{
		"repositories/1",
		"repos/AutoDevJapan/other/pulls/1",
		"repos/" + repoFull + "/issues/1",
		"repos/" + repoFull + "/actions/secrets",
		"repos/" + repoFull + "/actions/runners/registration-token",
		"repos/" + repoFull + "/actions/runs/101/logs",
		"repos/" + repoFull + "/contents/README.md",
		"repos/" + repoFull + "/contents/README.md?ref=main",
		"repos/" + repoFull + "/contents/../x?ref=" + sha,
		"repos/" + repoFull + "/pulls/1?x=a;b",
		"repos/" + repoFull + "/pulls/1 ",
		"user",
		"graphql",
		"repos/" + repoFull + "/tarball/" + sha,
	} {
		if e := allowedRead(bad); e == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	called := false
	readOverride = func(context.Context, string, any) error { called = true; return nil }
	t.Cleanup(func() { readOverride = nil })
	if e := githubRead(context.Background(), "user", nil); e == nil || called {
		t.Fatal("disallowed endpoint reached the reader")
	}
}

func TestFiniteReadRetriesOnlyTransientFailures(t *testing.T) {
	calls := 0
	_, e := finiteRead(context.Background(), func() ([]byte, string, error) {
		calls++
		return nil, "HTTP 503", errors.New("x")
	}, func() error { return nil })
	if e == nil || calls != 3 {
		t.Fatalf("transient: %v calls=%d", e, calls)
	}
	calls = 0
	_, e = finiteRead(context.Background(), func() ([]byte, string, error) {
		calls++
		return nil, "HTTP 404 repo AutoDevJapan/secret", errors.New("x")
	}, func() error { return nil })
	if e == nil || calls != 1 || strings.Contains(e.Error(), "AutoDevJapan") {
		t.Fatalf("permanent: %v calls=%d", e, calls)
	}
}

func TestMutationsAreFixed(t *testing.T) {
	var seen []string
	mutateOverride = func(_ context.Context, method, endpoint string, out any) error {
		seen = append(seen, method+" "+endpoint)
		if method == "POST" {
			out.(*struct{ Token string }).Token = "TOKEN"
		}
		return nil
	}
	t.Cleanup(func() { mutateOverride = nil })
	tok, e := registrationToken(context.Background())
	if e != nil || tok != "TOKEN" {
		t.Fatal(e)
	}
	if e = deleteRunner(context.Background(), 77); e != nil {
		t.Fatal(e)
	}
	if e = deleteRunner(context.Background(), 0); e == nil {
		t.Fatal("zero runner id accepted")
	}
	want := fmt.Sprintf("POST repos/%s/actions/runners/registration-token", repoFull)
	if len(seen) != 2 || seen[0] != want || seen[1] != fmt.Sprintf("DELETE repos/%s/actions/runners/77", repoFull) {
		t.Fatalf("unexpected mutations: %v", seen)
	}
}
