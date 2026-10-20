package contract

import (
	"context"
	"fmt"
	"testing"
)

func TestRegistrationSeesLegacyBaseLabelJobs(t *testing.T) {
	p, _ := nativeFixture(t)
	for _, tc := range []struct {
		name     string
		labels   []string
		status   string
		accepted bool
	}{
		{"other unique run", append(Labels(p)[:4:4], "another-run-selection"), "queued", true},
		{"legacy base labels", Labels(p)[:4], "queued", false},
		{"generic Windows job", []string{"self-hosted", "Windows", "X64"}, "queued", false},
		{"case insensitive legacy labels", []string{"SELF-HOSTED", "windows", "x64"}, "queued", false},
		{"completed legacy", Labels(p)[:4], "completed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := map[string]any{}
			for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
				f[fmt.Sprintf("repos/%s/actions/runs?status=%s&per_page=100&page=1", Repository, status)] = map[string]any{"total_count": 0, "workflow_runs": []any{}}
			}
			f[fmt.Sprintf("repos/%s/actions/runs?status=queued&per_page=100&page=1", Repository)] = map[string]any{"total_count": 2, "workflow_runs": []any{map[string]any{"id": p.RunID}, map[string]any{"id": 99}}}
			f[fmt.Sprintf("repos/%s/actions/runs/%d/jobs?per_page=100&page=1", Repository, p.RunID)] = map[string]any{"total_count": 1, "jobs": []Job{{ID: p.JobID, RunID: p.RunID, Status: "queued", Labels: Labels(p)}}}
			f[fmt.Sprintf("repos/%s/actions/runs/99/jobs?per_page=100&page=1", Repository)] = map[string]any{"total_count": 1, "jobs": []Job{{ID: 991, RunID: 99, Status: tc.status, Labels: tc.labels}}}
			e := CheckRegistrationQueue(context.Background(), p, Labels(p), fixtureRead(f))
			if (e == nil) != tc.accepted {
				t.Fatalf("accepted=%t error=%v", tc.accepted, e)
			}
		})
	}
}

func TestCompleteInventoryRejectsMissingLastPageEntries(t *testing.T) {
	for _, x := range [][3]int{{101, 2, 0}, {200, 2, 99}, {1001, 1, 100}, {-1, 1, 0}} {
		if completePage(x[0], x[1], x[2]) {
			t.Fatalf("incomplete page accepted: %v", x)
		}
	}
	if !completePage(101, 2, 1) || !completePage(0, 1, 0) {
		t.Fatal("complete final/empty page rejected")
	}
}
