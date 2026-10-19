package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnstartedCollectionRejectsWorkerAndCredentialArtifacts(t *testing.T) {
	for _, tc := range []string{"", "runtime/started.json", "runtime/service.json", "runtime/worker.json", "runtime/hook-start.json", "runtime/verify-source.json", "runtime/hook-completed.json", "runtime/runner/.runner", "runtime/runner/.credentials", "runtime/runner/.credentials_rsaparams", "report-changed", "report-missing"} {
		t.Run(tc, func(t *testing.T) {
			root := t.TempDir()
			runner := filepath.Join(root, "runtime", "runner")
			if err := os.MkdirAll(runner, 0700); err != nil {
				t.Fatal(err)
			}
			report := filepath.Join(root, "launcher-result.json")
			if tc != "report-missing" {
				data := []byte("{}")
				if tc == "report-changed" {
					data = []byte(`{"Drained":true}`)
				}
				if err := os.WriteFile(report, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc != "" && tc != "report-changed" && tc != "report-missing" {
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(tc)), []byte("must not be consumed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := checkUnstartedFiles(root, runner)
			if (err == nil) != (tc == "") {
				t.Fatalf("unstarted artifacts %q: %v", tc, err)
			}
		})
	}
}

func TestUnstartedCollectionRequiresExactQueuedJob(t *testing.T) {
	p := fixturePlan()
	j := apiJob{ID: p.JobID, RunID: p.RunID, Attempt: p.Attempt, Name: p.JobName, Labels: p.requiredLabels(), Status: "queued"}
	if err := checkUnstartedJob(p, j, 0); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []string{"id", "run", "attempt", "runner", "running", "completed", "steps"} {
		q, steps := j, 0
		switch tc {
		case "id":
			q.ID++
		case "run":
			q.RunID++
		case "attempt":
			q.Attempt++
		case "runner":
			q.Runner = p.name()
		case "running":
			q.Status = "in_progress"
		case "completed":
			q.Status = "completed"
		case "steps":
			steps = 1
		}
		if checkUnstartedJob(p, q, steps) == nil {
			t.Fatalf("accepted %s", tc)
		}
	}
}
