package main

import "testing"

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
