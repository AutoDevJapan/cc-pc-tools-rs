package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

type fakeJob struct {
	ID, RunID int64
	Status    string
	Labels    []string
}

// fakeQueue serves the run and job inventories checkQueue reads.
func fakeQueue(runs map[string][]int64, jobs map[int64][]fakeJob) ReadAPI {
	runJobs := regexp.MustCompile("actions/runs/([0-9]+)/jobs")
	return func(_ context.Context, endpoint string, out any) error {
		var v any
		if m := runJobs.FindStringSubmatch(endpoint); m != nil {
			var id int64
			fmt.Sscan(m[1], &id)
			var list []map[string]any
			for _, j := range jobs[id] {
				list = append(list, map[string]any{"id": j.ID, "run_id": j.RunID, "status": j.Status, "labels": j.Labels})
			}
			v = map[string]any{"total_count": len(list), "jobs": list}
		} else if strings.Contains(endpoint, "/actions/runs?status=") {
			status := strings.Split(strings.Split(endpoint, "status=")[1], "&")[0]
			var list []map[string]any
			for _, id := range runs[status] {
				list = append(list, map[string]any{"id": id})
			}
			v = map[string]any{"total_count": len(list), "workflow_runs": list}
		} else {
			return fmt.Errorf("unexpected endpoint %s", endpoint)
		}
		b, _ := json.Marshal(v)
		return json.Unmarshal(b, out)
	}
}

func TestCheckQueueAdmitsOnlyThePlannedJob(t *testing.T) {
	p := rustPlan()
	mine := []string{"self-hosted", "Linux", "X64", linuxLabel}
	win := []string{"self-hosted", "Windows", "X64", "civ6-ci-windows"}
	ctx := context.Background()
	runs := map[string][]int64{"queued": {p.RunID}}
	good := map[int64][]fakeJob{p.RunID: {{p.JobID, p.RunID, "queued", mine}, {p.JobID + 1, p.RunID, "queued", win}}}
	if e := checkQueue(ctx, p, linuxLabel, fakeQueue(runs, good)); e != nil {
		t.Fatal(e)
	}
	bad := map[string]struct {
		runs map[string][]int64
		jobs map[int64][]fakeJob
	}{
		"other run holds the label": {map[string][]int64{"queued": {p.RunID, 999}}, map[int64][]fakeJob{p.RunID: good[p.RunID], 999: {{5, 999, "in_progress", mine}}}},
		"planned job absent":        {runs, map[int64][]fakeJob{p.RunID: {{p.JobID + 1, p.RunID, "queued", win}}}},
		"planned job already taken": {runs, map[int64][]fakeJob{p.RunID: {{p.JobID, p.RunID, "in_progress", mine}}}},
		"older queued sibling":      {runs, map[int64][]fakeJob{p.RunID: {{p.JobID, p.RunID, "queued", mine}, {p.JobID - 1, p.RunID, "queued", mine}}}},
		"no active runs":            {map[string][]int64{}, nil},
	}
	for name, c := range bad {
		if e := checkQueue(ctx, p, linuxLabel, fakeQueue(c.runs, c.jobs)); e == nil {
			t.Errorf("accepted: %s", name)
		}
	}
	// A completed job of another run no longer occupies the label.
	done := map[int64][]fakeJob{p.RunID: good[p.RunID], 999: {{5, 999, "completed", mine}}}
	if e := checkQueue(ctx, p, linuxLabel, fakeQueue(map[string][]int64{"queued": {p.RunID, 999}}, done)); e != nil {
		t.Fatal(e)
	}
	// CodeQL matrix: the lower job id goes first; its younger sibling may wait.
	q := codeQLPlan("Analyze (actions)")
	cq := []string{codeQLLabel}
	matrix := map[int64][]fakeJob{q.RunID: {{q.JobID, q.RunID, "queued", cq}, {q.JobID + 1, q.RunID, "queued", cq}}}
	if e := checkQueue(ctx, q, codeQLLabel, fakeQueue(map[string][]int64{"queued": {q.RunID}}, matrix)); e != nil {
		t.Fatal(e)
	}
	younger := q
	younger.JobID++
	if e := checkQueue(ctx, younger, codeQLLabel, fakeQueue(map[string][]int64{"queued": {q.RunID}}, matrix)); e == nil {
		t.Fatal("younger CodeQL job admitted ahead of its older sibling")
	}
}

func TestRunLabelDoesNotBlockOnOtherMatrixOrRun(t *testing.T) {
	p := rustPlan()
	w := p
	w.JobName = "test (windows-latest)"
	other := p
	other.RunID++
	jobs := map[int64][]fakeJob{
		p.RunID: {
			{p.JobID, p.RunID, "queued", []string{linuxLabel, jobLabel(p)}},
			{p.JobID - 1, p.RunID, "queued", []string{"civ6-ci-windows", jobLabel(w)}},
		},
		other.RunID: {{1, other.RunID, "queued", []string{linuxLabel, jobLabel(other)}}},
	}
	runs := map[string][]int64{"queued": {p.RunID, other.RunID}}
	if e := checkQueue(context.Background(), p, jobLabel(p), fakeQueue(runs, jobs)); e != nil {
		t.Fatal(e)
	}
	// Reusing this exact selection label for another run is still refused.
	jobs[other.RunID][0].Labels = []string{linuxLabel, jobLabel(p)}
	if checkQueue(context.Background(), p, jobLabel(p), fakeQueue(runs, jobs)) == nil {
		t.Fatal("a conflicting selection label was admitted")
	}
}
