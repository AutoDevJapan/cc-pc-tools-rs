package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourceAdmission(t *testing.T) {
	tests := []struct {
		name, kind, sha, ref string
		e                    event
		accept               bool
	}{
		{"current push", "push", "abc", "", event{Repository: repo{repository}, Ref: "refs/heads/main", After: "abc"}, true},
		{"other repo", "push", "abc", "", event{Repository: repo{"other/repo"}, Ref: "refs/heads/main", After: "abc"}, false},
		{"tag", "push", "abc", "", event{Repository: repo{repository}, Ref: "refs/tags/v1", After: "abc"}, false},
		{"wrong sha", "push", "abc", "", event{Repository: repo{repository}, Ref: "refs/heads/main", After: "old"}, false},
		{"missing pr", "pull_request", "abc", "", event{Repository: repo{repository}}, false},
		{"privileged pr event", "pull_request_target", "abc", "", event{Repository: repo{repository}}, false},
		{"branch dispatch", "workflow_dispatch", "abc", "refs/heads/main", event{Repository: repo{repository}}, true},
		{"tag dispatch", "workflow_dispatch", "abc", "refs/tags/v1", event{Repository: repo{repository}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := source(tt.e, tt.kind, tt.sha, tt.ref)
			if (err == nil) != tt.accept {
				t.Fatalf("accepted=%v error=%v", err == nil, err)
			}
		})
	}
}

func TestCurrentHeadRejectsStaleOrUnavailable(t *testing.T) {
	for _, tt := range []struct {
		name, response, sha string
		status              int
		accept              bool
	}{
		{"current", `{"object":{"sha":"current"}}`, "current", 200, true},
		{"obsolete", `{"object":{"sha":"current"}}`, "obsolete", 200, false},
		{"missing", `{"object":{}}`, "", 200, false},
		{"unavailable", `{}`, "current", 403, false},
		{"invalid response", `{`, "current", 200, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var auth string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth = r.Header.Get("Authorization")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer s.Close()
			err := currentHead(s.Client(), s.URL, "test-only-token", tt.sha)
			if auth != "Bearer test-only-token" {
				t.Fatal("missing job-scoped authorization")
			}
			if (err == nil) != tt.accept {
				t.Fatalf("accepted=%v error=%v", err == nil, err)
			}
		})
	}
}

func TestPullRequestRejectsFork(t *testing.T) {
	var e event
	// The checkout SHA is permitted only when the head repository is exactly this repo.
	if err := json.Unmarshal([]byte(`{"repository":{"full_name":"AutoDevJapan/cc-pc-tools-rs"},"pull_request":{"head":{"repo":{"full_name":"fork/cc-pc-tools-rs"},"ref":"main","sha":"abc"}}}`), &e); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source(e, "pull_request", "merge", ""); err == nil {
		t.Fatal("fork accepted")
	}
	e.PullRequest.Head.Repo.FullName = repository
	branch, sha, err := source(e, "pull_request", "merge", "")
	if err != nil || branch != "main" || sha != "abc" {
		t.Fatalf("wrong pinned PR source: %q %q %v", branch, sha, err)
	}
}
