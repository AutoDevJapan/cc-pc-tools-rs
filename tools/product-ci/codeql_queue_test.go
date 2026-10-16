package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCodeQLQueueAdmitsSameRunMatrixOnly(t *testing.T) {
	p := codeQLPlan{Repo: "AutoDevJapan/opaque-fixture", RunID: 7, Label: "dedicated"}
	for _, tc := range []struct {
		name                      string
		foreign, matching, target bool
		want                      bool
	}{{"serial matrix", false, false, true, true}, {"foreign dedicated", true, true, true, false}, {"other hosted", true, false, true, true}, {"old hosted target", false, false, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			read := func(_ context.Context, path string, out any) error {
				var value any
				switch {
				case strings.Contains(path, "/actions/runs?status=queued&"):
					runs := []map[string]int{{"id": 7}}
					if tc.foreign {
						runs = append(runs, map[string]int{"id": 8})
					}
					value = map[string]any{"total_count": len(runs), "workflow_runs": runs}
				case strings.Contains(path, "/actions/runs?status="):
					value = map[string]any{"total_count": 0, "workflow_runs": []any{}}
				case strings.Contains(path, "/actions/runs/7/jobs?"):
					label := "ubuntu-latest"
					if tc.target {
						label = p.Label
					}
					value = map[string]any{"total_count": 2, "jobs": []map[string]any{{"id": 1, "run_id": 7, "status": "queued", "labels": []string{label}}, {"id": 2, "run_id": 7, "status": "queued", "labels": []string{label}}}}
				case strings.Contains(path, "/actions/runs/8/jobs?"):
					label := "ubuntu-latest"
					if tc.matching {
						label = p.Label
					}
					value = map[string]any{"total_count": 1, "jobs": []map[string]any{{"id": 3, "run_id": 8, "status": "queued", "labels": []string{label}}}}
				default:
					return fmt.Errorf("unexpected test endpoint")
				}
				b, _ := json.Marshal(value)
				return json.Unmarshal(b, out)
			}
			if got := checkCodeQLQueue(context.Background(), p, read) == nil; got != tc.want {
				t.Fatalf("admission %t, want %t", got, tc.want)
			}
		})
	}
}

func TestCodeQLSettingsOrderAndAcceptance(t *testing.T) {
	a := codeQLSetup{State: "configured", Languages: []string{"rust", "actions"}, QuerySuite: "default", ThreatModel: "remote"}
	b := a
	b.Languages = []string{"actions", "rust"}
	if !sameCodeQLSettings(a, b) {
		t.Fatal("order-only change rejected")
	}
	b.Languages = []string{"actions"}
	if sameCodeQLSettings(a, b) {
		t.Fatal("removed Rust accepted")
	}
	b = a
	b.QuerySuite = "extended"
	if sameCodeQLSettings(a, b) {
		t.Fatal("query change accepted")
	}
}
