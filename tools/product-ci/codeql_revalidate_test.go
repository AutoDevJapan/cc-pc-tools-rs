package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodeQLRevalidationPreservesRustAndRejectsActiveRuns(t *testing.T) {
	fixture := func(active bool) api {
		return codeQLFixture(t, func(d map[string]any) {
			s := d["repos/AutoDevJapan/opaque-fixture/code-scanning/default-setup"].(map[string]any)
			s["languages"] = []string{"actions", "javascript", "javascript-typescript", "typescript", "python", "rust"}
			s["query_suite"] = "default"
			s["threat_model"] = "remote"
			for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
				total := 0
				if active && status == "in_progress" {
					total = 1
				}
				d["repos/AutoDevJapan/opaque-fixture/actions/runs?event=dynamic&status="+status+"&per_page=1"] = map[string]any{"total_count": total}
			}
			d["repos/AutoDevJapan/opaque-fixture/actions/runs?event=dynamic&per_page=1"] = map[string]any{"workflow_runs": []map[string]any{{"id": 6}}}
		})
	}
	s, _, e := revalidationPreflight(context.Background(), candidates[0], fixture(false))
	if e != nil || s.DefaultSHA != strings.Repeat("a", 40) {
		t.Fatalf("valid idle setup rejected: %v", e)
	}
	body := revalidationBody(s.Prior)
	if _, ok := body["languages"]; ok {
		t.Fatal("REST language selection could omit unsupported Rust")
	}
	languages := codeQLLanguages(s.Prior.Languages)
	want := []string{"actions", "javascript-typescript", "python", "rust"}
	if strings.Join(languages, ",") != strings.Join(want, ",") {
		t.Fatal("original language coverage changed")
	}
	after := s.Prior
	after.Languages = languages
	if !sameCodeQLSettings(after, s.Prior) {
		t.Fatal("canonical aliases treated as coverage loss")
	}
	after.Languages = want[:3]
	if sameCodeQLSettings(after, s.Prior) {
		t.Fatal("Rust removal accepted")
	}
	if _, _, e = revalidationPreflight(context.Background(), candidates[0], fixture(true)); e == nil {
		t.Fatal("active analysis allowed reconfiguration")
	}
	if body["state"] != "configured" {
		t.Fatal("scanning disabled")
	}
}

func TestInterruptedSetupRecoveryPersistsNewRunBeforePendingCoverage(t *testing.T) {
	c := candidates[0]
	dir := t.TempDir()
	prior := codeQLSetup{State: "configured", Languages: []string{"actions", "javascript-typescript", "python", "rust"}, QuerySuite: "default", ThreatModel: "remote", RunnerType: "labeled", RunnerLabel: codeQLLabel(c.ID)}
	s := codeQLRevalidation{RepoID: c.ID, Candidate: c.SHA, DefaultSHA: strings.Repeat("a", 40), Started: time.Now().Add(time.Hour), Prior: prior, LatestRunID: 6}
	b, _ := json.Marshal(s)
	if e := os.WriteFile(filepath.Join(dir, "codeql-revalidation-intent.private.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	configured := false
	writes := 0
	fixture := codeQLFixture(t, func(d map[string]any) {
		r := d["repos/AutoDevJapan/opaque-fixture/actions/runs/7"].(map[string]any)
		r["head_sha"] = s.DefaultSHA
		jobs := []map[string]any{}
		for i, l := range prior.Languages {
			jobs = append(jobs, map[string]any{"id": i + 1, "run_id": 7, "name": "Analyze (" + l + ")", "labels": []string{prior.RunnerLabel}})
		}
		d["repos/AutoDevJapan/opaque-fixture/actions/runs/7/jobs?per_page=100"] = map[string]any{"total_count": len(jobs), "jobs": jobs}
	})
	read := func(ctx context.Context, p string, out any) error {
		if strings.HasSuffix(p, "/code-scanning/default-setup") {
			now := prior
			if configured {
				now.Languages = nil
			} else {
				now.State = "not-configured"
			}
			b, _ := json.Marshal(now)
			return json.Unmarshal(b, out)
		}
		return fixture(ctx, p, out)
	}
	write := func(_ context.Context, method, p string, body, out any) error {
		writes++
		if method != "PATCH" || !strings.HasSuffix(p, "/code-scanning/default-setup") {
			t.Fatal("unexpected mutation")
		}
		m := body.(map[string]any)
		if m["state"] != "configured" || m["query_suite"] != prior.QuerySuite || m["threat_model"] != prior.ThreatModel {
			t.Fatal("recovery weakened original settings")
		}
		configured = true
		return json.Unmarshal([]byte(`{"run_id":7}`), out)
	}
	if e := recoverCodeQLRevalidationUsing(context.Background(), c, dir, read, write); e != nil {
		t.Fatal(e)
	}
	if writes != 1 {
		t.Fatal("recovery did not issue exactly one re-enable")
	}
	b, e := os.ReadFile(filepath.Join(dir, "codeql-route-response.private.json"))
	if e != nil || !strings.Contains(string(b), `"run_id": 7`) {
		t.Fatal("fresh pending run identity lost")
	}
	if e = recoverCodeQLRevalidationUsing(context.Background(), c, dir, read, write); e != nil || writes != 1 {
		t.Fatal("pending setup issued a duplicate mutation")
	}
}

func TestPendingCodeQLSetupRequiresFreshCompleteDedicatedMatrix(t *testing.T) {
	c := candidates[0]
	start := time.Now().UTC()
	s := codeQLRevalidation{RepoID: c.ID, Candidate: c.SHA, DefaultSHA: strings.Repeat("a", 40), Started: start, LatestRunID: 6, Prior: codeQLSetup{Languages: []string{"actions", "javascript", "typescript", "python", "rust"}}}
	for _, fault := range []string{"", "old", "clock-skew", "missing-rust", "wrong-label", "foreign-source"} {
		t.Run(fault, func(t *testing.T) {
			read := codeQLFixture(t, func(d map[string]any) {
				r := d["repos/AutoDevJapan/opaque-fixture/actions/runs/7"].(map[string]any)
				r["head_sha"] = s.DefaultSHA
				r["created_at"] = start
				if fault == "clock-skew" {
					r["created_at"] = start.Add(-time.Hour)
				}
				if fault == "foreign-source" {
					r["head_sha"] = strings.Repeat("f", 40)
				}
				jobs := []map[string]any{}
				for index, language := range []string{"actions", "javascript-typescript", "python", "rust"} {
					if fault == "missing-rust" && language == "rust" {
						continue
					}
					label := codeQLLabel(c.ID)
					if fault == "wrong-label" && index == 0 {
						label = "ubuntu-latest"
					}
					jobs = append(jobs, map[string]any{"id": index + 1, "run_id": 7, "name": "Analyze (" + language + ")", "labels": []string{label}})
				}
				d["repos/AutoDevJapan/opaque-fixture/actions/runs/7/jobs?per_page=100"] = map[string]any{"total_count": len(jobs), "jobs": jobs}
			})
			bound := s
			if fault == "old" {
				bound.LatestRunID = 7
			}
			e := verifyRevalidationRun(context.Background(), c, bound, "AutoDevJapan/opaque-fixture", 7, read)
			if (e == nil) != (fault == "" || fault == "clock-skew") {
				t.Fatalf("matrix admission fault %s: %v", fault, e)
			}
		})
	}
}
