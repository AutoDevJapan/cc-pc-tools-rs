package main

import (
	"context"
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

func TestPendingCodeQLSetupRequiresFreshCompleteDedicatedMatrix(t *testing.T) {
	c := candidates[0]
	start := time.Now().UTC()
	s := codeQLRevalidation{RepoID: c.ID, Candidate: c.SHA, DefaultSHA: strings.Repeat("a", 40), Started: start, Prior: codeQLSetup{Languages: []string{"actions", "javascript", "typescript", "python", "rust"}}}
	for _, fault := range []string{"", "old", "missing-rust", "wrong-label", "foreign-source"} {
		t.Run(fault, func(t *testing.T) {
			read := codeQLFixture(t, func(d map[string]any) {
				r := d["repos/AutoDevJapan/opaque-fixture/actions/runs/7"].(map[string]any)
				r["head_sha"] = s.DefaultSHA
				r["created_at"] = start
				if fault == "old" {
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
			e := verifyRevalidationRun(context.Background(), c, s, "AutoDevJapan/opaque-fixture", 7, read)
			if (e == nil) != (fault == "") {
				t.Fatalf("matrix admission fault %s: %v", fault, e)
			}
		})
	}
}
