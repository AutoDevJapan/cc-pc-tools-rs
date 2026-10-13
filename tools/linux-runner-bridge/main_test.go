package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCargoBoundary(t *testing.T) {
	for _, args := range [][]string{{"test", "--locked", "-p", "cc-task-title", "-j64"}, {"nextest", "run", "--locked", "--test-threads=32"}, {"--version"}} {
		if e := validate(request{Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: args}); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []request{{Dir: "/etc", Args: []string{"test"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--config=x"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--manifest-path=/etc/a"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "--target-dir=/etc/a"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"run"}}, {Dir: "/work/cc-pc-tools-rs/cc-pc-tools-rs", Args: []string{"test", "-C/etc"}}} {
		if validate(q) == nil {
			t.Fatalf("accepted %+v", q)
		}
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
