package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var candidateNameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// CI checks the published design, not the protected central skill directory or
// private original bodies. Their SHA correspondence is checked on this PC.
func TestCandidateReceiptsAndUsageBoundaries(t *testing.T) {
	files, err := filepath.Glob("../../docs/skills-3080-batch*.json")
	if err != nil || len(files) == 0 {
		t.Fatal("no adoption candidate batches")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) { checkCandidatePlan(t, file) })
	}
}

func checkCandidatePlan(t *testing.T, file string) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Status      string `json:"status"`
		Repo        string `json:"source_repo"`
		Commit      string `json:"source_commit"`
		Tree        string `json:"source_tree"`
		BatchNotCap bool   `json:"batch_is_not_total_scope_or_count_cap"`
		License     struct {
			Path        string `json:"preserved_notice"`
			SHA         string `json:"source_sha256"`
			ID          string `json:"identifier"`
			Attribution string `json:"attribution"`
		} `json:"license"`
		Targets []struct {
			Name             string   `json:"name"`
			Path             string   `json:"source_path"`
			SHA              string   `json:"source_sha256"`
			URL              string   `json:"source_url"`
			Description      string   `json:"description"`
			Positive         string   `json:"positive"`
			Negative         string   `json:"negative"`
			Guidance         []string `json:"guidance"`
			Adaptations      string   `json:"adaptations"`
			Distinct         string   `json:"distinct_from_existing"`
			UpstreamExplicit *bool    `json:"upstream_disable_model_invocation"`
			Invocation       struct {
				Mode   string `json:"mode"`
				Reason string `json:"reason"`
			} `json:"derivative_invocation"`
			Status        string `json:"upstream_status"`
			Reason        string `json:"upstream_status_adoption_reason"`
			LicenseReview struct {
				Tree     string   `json:"source_tree"`
				Complete bool     `json:"tree_complete"`
				Paths    []string `json:"license_paths"`
				Nested   []string `json:"nested_license_notice_copying_paths"`
				Scope    string   `json:"scope"`
				Checked  string   `json:"checked_at"`
			} `json:"individual_license_review"`
		} `json:"targets"`
	}
	if err = json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	commit, commitErr := hex.DecodeString(p.Commit)
	if !p.BatchNotCap || p.Status != "authored_candidates_pending_review_and_scoped_install" || commitErr != nil || len(commit) != 20 || len(p.Targets) == 0 {
		t.Fatal("candidate state/scope not explicit")
	}
	notice, err := os.ReadFile(filepath.Join(filepath.Dir(file), p.License.Path))
	if err != nil {
		t.Fatal(err)
	}
	noticeSHA := sha256.Sum256(notice)
	if (p.License.ID != "MIT" && p.License.ID != "Apache-2.0") || hex.EncodeToString(noticeSHA[:]) != p.License.SHA || p.License.Attribution == "" || !strings.Contains(string(notice), p.License.Attribution) {
		t.Fatal("fixed notice identity/hash/attribution differ")
	}
	if p.License.ID == "Apache-2.0" && (p.Repo != "docker/skills" || p.License.SHA != "3bb33f65872155f0801efc9f6732e2868eb338c5ed6a81eec5c775c1382761cb") {
		t.Fatal("unreviewed Apache notice")
	}
	var progress struct {
		Repos []struct {
			Repo     string `json:"requested"`
			FullName string `json:"full_name"`
			Tree     string `json:"tree_sha"`
		} `json:"fixed_public_repos"`
	}
	raw, err := os.ReadFile("../../docs/skills-3080-progress.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &progress); err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, r := range progress.Repos {
		// The API's fixed canonical owner spelling may differ from the requested URL.
		// Accept either recorded identity, still requiring the same exact tree.
		if (r.Repo == p.Repo || r.FullName == p.Repo) && r.Tree == p.Tree {
			matched = true
		}
	}
	if !matched {
		t.Fatal("plan source tree differs from pinned complete source")
	}
	seen := map[string]bool{}
	for _, c := range p.Targets {
		if !candidateNameRE.MatchString(c.Name) || len(c.Name) > 64 || seen[c.Name] || filepath.Base(filepath.Dir(c.Path)) != c.Name {
			t.Fatal("missing/repeated candidate name")
		}
		seen[c.Name] = true
		d, err := hex.DecodeString(c.SHA)
		if err != nil || len(d) != 32 {
			t.Fatalf("%s: missing fixed source SHA", c.Name)
		}
		if c.URL != "https://github.com/"+p.Repo+"/blob/"+p.Commit+"/"+c.Path || !strings.HasSuffix(c.Path, "/SKILL.md") {
			t.Fatalf("%s: source URL/path/commit differ", c.Name)
		}
		if n := utf8.RuneCountInString(c.Description); n < 1 || n > 60 {
			t.Fatalf("%s: description length %d", c.Name, n)
		}
		if c.Positive == "" || c.Negative == "" || c.Positive == c.Negative || c.Adaptations == "" || c.Distinct == "" || len(c.Guidance) == 0 {
			t.Fatalf("%s: incomplete use/license-policy review metadata", c.Name)
		}
		if c.UpstreamExplicit == nil || c.Invocation.Mode != "implicit_with_narrow_trigger" || c.Invocation.Reason == "" || c.Status == "" || c.Reason == "" {
			t.Fatalf("%s: invocation/support status is not explicit", c.Name)
		}
		r := c.LicenseReview
		if r.Tree != p.Tree || !r.Complete || len(r.Paths) != 1 || r.Paths[0] != "LICENSE" || r.Nested == nil || len(r.Nested) != 0 || r.Scope == "" {
			t.Fatal("individual license scope incomplete")
		}
		if _, err := time.Parse(time.RFC3339Nano, r.Checked); err != nil {
			t.Fatal("invalid license review date")
		}
	}
}
