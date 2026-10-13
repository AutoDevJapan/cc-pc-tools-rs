package main

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCargoBoundary(t *testing.T) {
	for _, args := range [][]string{{"test", "--locked", "-p", "cc-task-title", "-j64"}, {"nextest", "run", "--locked", "--test-threads=32"}, {"--version"}, {"fmt", "--manifest-path", "cc-bookmark-queue/Cargo.toml", "--check"}, {"fmt", "--manifest-path=cc-bookmark-queue/Cargo.toml", "--check"}} {
		if e := validate(request{Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: args}); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []request{{Dir: "/etc", Args: []string{"test"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--config=x"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path=/etc/a"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path", "../Cargo.toml"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path", "D:/repo/Cargo.toml"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path", "other/Cargo.toml"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--target-dir=/etc/a"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"run"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "-C/etc"}}} {
		if validate(q) == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}

func TestNormalJobIdentityUsesPRHeadAndRejectsForkOrStaleHead(t *testing.T) {
	head := strings.Repeat("a", 40)
	h := host{mode: "normal", getHead: func(branch string) (string, error) {
		if branch != "feature/linux-ci" {
			t.Fatalf("unexpected branch lookup %q", branch)
		}
		return head, nil
	}}
	valid := request{Repository: repo, Revision: head, Ref: "refs/pull/17/merge", Event: "pull_request", HeadSHA: head, HeadRef: "feature/linux-ci", HeadRepository: repo, BaseRepository: repo, BaseRef: "main"}
	if err := h.authorizeNormal(valid); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []request{
		{Repository: repo, Revision: strings.Repeat("b", 40), Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: repo, BaseRepository: repo, BaseRef: "main"},
		{Repository: repo, Revision: head, Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: "fork/cc-pc-tools-rs", BaseRepository: repo, BaseRef: "main"},
		{Repository: repo, Revision: head, Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: repo, BaseRepository: "other/repo", BaseRef: "main"},
		{Repository: repo, Revision: head, Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: repo, BaseRepository: repo, BaseRef: "release"},
		{Repository: "other/repo", Revision: head, Ref: valid.Ref, Event: valid.Event, HeadSHA: head, HeadRef: valid.HeadRef, HeadRepository: repo, BaseRepository: repo, BaseRef: "main"},
	} {
		if h.authorizeNormal(invalid) == nil {
			t.Fatalf("accepted invalid identity %+v", invalid)
		}
	}
}

func TestNormalPushRequiresCurrentMainHead(t *testing.T) {
	head := strings.Repeat("c", 40)
	h := host{mode: "normal", getHead: func(branch string) (string, error) {
		if branch != "main" {
			t.Fatalf("unexpected branch lookup %q", branch)
		}
		return head, nil
	}}
	valid := request{Repository: repo, Revision: head, Ref: "refs/heads/main", Event: "push", HeadSHA: head, HeadRef: "main", HeadRepository: repo, BaseRepository: repo}
	if err := h.authorizeNormal(valid); err != nil {
		t.Fatal(err)
	}
	valid.HeadSHA = strings.Repeat("d", 40)
	valid.Revision = valid.HeadSHA
	if h.authorizeNormal(valid) == nil {
		t.Fatal("accepted a non-current main commit")
	}
}

func TestJobRequestFromEnvUsesPRHeadNotMergeSHA(t *testing.T) {
	path := t.TempDir() + "/event.json"
	if err := os.WriteFile(path, []byte(`{"repository":{"full_name":"AutoDevJapan/cc-pc-tools-rs"},"pull_request":{"head":{"sha":"`+strings.Repeat("e", 40)+`","ref":"feature/linux-ci","repo":{"full_name":"AutoDevJapan/cc-pc-tools-rs"}},"base":{"ref":"main","repo":{"full_name":"AutoDevJapan/cc-pc-tools-rs"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_LINUX_RUNNER_MODE", "normal")
	t.Setenv("GITHUB_REPOSITORY", repo)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", path)
	t.Setenv("GITHUB_REF", "refs/pull/17/merge")
	t.Setenv("GITHUB_SHA", strings.Repeat("f", 40))
	r, ok := jobRequestFromEnv()
	if !ok {
		t.Fatal("did not parse pull request identity")
	}
	if r.HeadSHA != strings.Repeat("e", 40) || r.Revision != r.HeadSHA || r.Revision == os.Getenv("GITHUB_SHA") {
		t.Fatalf("used merge revision: %+v", r)
	}
}
func TestUnauthorizedRequestCannotInvokeGate(t *testing.T) {
	h := host{key: "secret"}
	r := httptest.NewRequest("POST", "/cargo", strings.NewReader(`{"Dir":"/work/cc-pc-tools-rs/cc-pc-tools-rs","Args":["test"]}`))
	w := httptest.NewRecorder()
	h.serve(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("POST", "/cargo", strings.NewReader(`{"unknown":1}`))
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.serve(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestJobIdentity(t *testing.T) {
	h := host{revision: strings.Repeat("a", 40), branch: "scheduler/todo-2511-test"}
	for _, q := range []request{{Repository: "other/repo"}, {Repository: repo, Revision: h.revision, Ref: "refs/heads/other"}, {Repository: repo, Revision: h.revision, Ref: "refs/heads/" + h.branch, Event: "pull_request"}} {
		if h.authorize(q) == nil {
			t.Fatal("unexpected authorized job")
		}
	}
}
