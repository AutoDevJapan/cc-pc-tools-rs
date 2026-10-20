package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFreshAuditConnectsToOrdinaryQueueAndRemovesOwnedTemporaryInputs(t *testing.T) {
	home := t.TempDir()
	r := report{Schema: 1, Time: time.Now().UTC().Format(time.RFC3339), Categories: []categoryResult{{Category: "owner_data", Status: "protected_unclassified"}}}
	var reportPath, specPath string
	got := queueDecisionWith(r, home, func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] != "capacity-cycle" || args[5] != "--project" || args[6] != "cc-pc-tools-rs" {
			t.Fatal("not the normal queue path")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 26*time.Second {
			t.Fatal("unbounded admission")
		}
		reportPath, specPath = args[2], args[4]
		data, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatal(err)
		}
		var input report
		if json.Unmarshal(data, &input) != nil || input.WholeDriveScan || input.Recovered != 0 || input.Categories[0].Status != "protected_unclassified" || input.Categories[0].Bytes != nil {
			t.Fatal("unknown scan fabricated")
		}
		spec, err := os.ReadFile(specPath)
		if err != nil || !strings.Contains(string(spec), "capacity-result") || !strings.Contains(string(spec), "同じToDo") {
			t.Fatal("finite result contract missing")
		}
		return []byte(`{"todo_id":19,"generation":"` + strings.Repeat("a", 64) + `","action":"queued","worker_launched":false}`), nil
	})
	if got.Status != "queued" || got.TodoID != 19 || got.WorkerLaunched {
		t.Fatal("queue receipt missing")
	}
	for _, path := range []string{reportPath, specPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("owned temporary input remained")
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("unexpected output")
	}
}

func TestQueueFailureIsVisibleWithoutStartingOrClaimingDecision(t *testing.T) {
	for _, test := range []struct {
		out string
		err error
	}{{"", errors.New("unavailable")}, {`{"todo_id":1,"generation":"` + strings.Repeat("a", 64) + `","action":"queued","worker_launched":true}`, nil}, {`{}`, nil}} {
		got := queueDecisionWith(report{}, t.TempDir(), func(context.Context, ...string) ([]byte, error) { return []byte(test.out), test.err })
		if got.Status != "queue_unavailable" || got.WorkerLaunched || got.TodoID != 0 {
			t.Fatal("failed admission reported as decision success")
		}
	}
	if queueDecisionWith(report{}, filepath.Join(t.TempDir(), "missing"), func(context.Context, ...string) ([]byte, error) {
		t.Fatal("called queue outside runtime boundary")
		return nil, nil
	}).Status != "queue_unavailable" {
		t.Fatal("missing runtime accepted")
	}
}
