//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRefreshCooldownCLIChild(t *testing.T) {
	code := os.Getenv("F4431_COOLDOWN_CHILD_EXIT")
	if code == "" {
		return
	}
	exit, err := strconv.Atoi(code)
	if err != nil {
		os.Exit(125)
	}
	os.Exit(exit)
}

func cooldownFailureMarker(t *testing.T, home, repo string, failed int64) {
	t.Helper()
	if !ownerWriteSealed(home, ownerRefreshFailureName(repo), ownerRefreshFailure{"owner-source-refresh-failure-v1", repo, hashBytes(ownerDepartmentCatalog), failed}) {
		t.Fatal("failure marker fixture")
	}
}

func TestRefreshCooldownReportsTheFixedFetchFailureCause(t *testing.T) {
	for _, tc := range []struct {
		label, stderr, stage string
		exit, http           int
	}{
		{"http_401", "gh: fixture (HTTP 401) SECRET_TOKEN\n", "identity_http_401", 1, 401},
		{"http_403", "gh: fixture (HTTP 403) C:/private\n", "identity_http_403", 1, 403},
		{"not_signed_in", "SECRET_TOKEN gh auth login C:/private\n", "identity_fetch_failed", 4, 0},
		{"network", "SECRET_TOKEN dial tcp C:/private\n", "identity_fetch_failed", 1, 0},
	} {
		t.Run(tc.label, func(t *testing.T) {
			const repo = "AutoDevJapan/cooldown-fixture"
			home := diagnosticHome(t)
			now := time.Now()
			clock := func() time.Time { return now }
			calls := 0
			fetch := func(ctx context.Context, repository string) (int64, string, string, bool) {
				calls++
				return ownerGitHubIdentityOutputWith(ctx, repository, func(cmd *exec.Cmd) ([]byte, error) {
					_, _ = cmd.Stderr.Write([]byte(tc.stderr))
					cmd.Path = os.Args[0]
					cmd.Args = []string{os.Args[0], "-test.run=^TestRefreshCooldownCLIChild$"}
					cmd.Env = append(cmd.Env, "F4431_COOLDOWN_CHILD_EXIT="+strconv.Itoa(tc.exit))
					return cmd.Output()
				})
			}
			first := &ownerRefreshObservation{}
			if ownerRefreshIdentityRequestWith("unused", "", home, clock, diagnosticQuery(repo), fetch, first) || calls != 1 {
				t.Fatal("failed fetch fixture", calls)
			}
			if d := ownerRefreshProject(context.Background(), first, false, now); !d.Attempted || d.Cooldown != nil || d.Stage != tc.stage {
				t.Fatal("first failure", d)
			}
			now = now.Add(37 * time.Second)
			second := &ownerRefreshObservation{}
			ok := ownerRefreshIdentityRequestWith("unused", "", home, clock, diagnosticQuery(repo), fetch, second)
			d := ownerRefreshProject(context.Background(), second, ok, now)
			if ok || calls != 1 || d.Attempted || d.Stage != "cooldown_no_valid_identity" || d.Cooldown == nil {
				t.Fatal("cooldown cause not reported", ok, calls, d)
			}
			c := d.Cooldown
			if c.Kind != "fetch_failed" || c.Stage != tc.stage || c.HTTP != tc.http || c.CLIExit == nil || *c.CLIExit != tc.exit || c.AgeSeconds != 37 {
				t.Fatalf("wrong fixed cause %+v", c)
			}
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"SECRET_TOKEN", "C:/private", "AutoDevJapan/"} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Fatal("cooldown diagnostic exposed", secret)
				}
			}
			var payload struct {
				Cooldown map[string]any `json:"cooldown"`
			}
			if json.Unmarshal(raw, &payload) != nil || len(payload.Cooldown) == 0 {
				t.Fatal("cooldown object missing")
			}
			for key := range payload.Cooldown {
				switch key {
				case "kind", "stage", "cli_exit", "http", "age_seconds":
				default:
					t.Fatal("unexpected cooldown field", key)
				}
			}
		})
	}
}

func TestRefreshCooldownNamesAnUnfinishedHelper(t *testing.T) {
	const repo, next = "AutoDevJapan/cooldown-unfinished", "AutoDevJapan/cooldown-next"
	home := diagnosticHome(t)
	start := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	if _, ok := ownerClaimRefresh(home, repo, start); !ok {
		t.Fatal("claim fixture")
	}
	if _, ok := ownerClaimRefresh(home, next, start.Add(25*time.Second)); !ok {
		t.Fatal("unfinished claim was not recovered")
	}
	clock := func() time.Time { return start.Add(40 * time.Second) }
	calls := 0
	fetch := func(context.Context, string) (int64, string, string, bool) { calls++; return 0, "", "", false }
	ob := &ownerRefreshObservation{}
	ok := ownerRefreshIdentityRequestWith("unused", "", home, clock, diagnosticQuery(repo), fetch, ob)
	d := ownerRefreshProject(context.Background(), ob, ok, clock())
	if ok || calls != 0 || d.Attempted || d.Stage != "cooldown_no_valid_identity" || d.Cooldown == nil {
		t.Fatal("unfinished helper not reported", ok, calls, d)
	}
	c := d.Cooldown
	if c.Kind != "helper_unfinished" || c.Stage != "" || c.CLIExit != nil || c.HTTP != 0 || c.AgeSeconds != 15 {
		t.Fatalf("wrong unfinished helper cause %+v", c)
	}
}

func TestRefreshCooldownCauseIsFixedVocabularyOrUnknown(t *testing.T) {
	const repo = "AutoDevJapan/cooldown-vocabulary"
	catalog := hashBytes(ownerDepartmentCatalog)
	stored := func(mutate func(*ownerRefreshCause)) func(int64) *ownerRefreshCause {
		return func(failed int64) *ownerRefreshCause {
			c := &ownerRefreshCause{Domain: ownerRefreshCauseDomain, Repository: repo, Catalog: catalog, Failed: failed, Kind: "fetch_failed", Stage: "identity_http_403", CLIExit: 4, HTTP: 403}
			mutate(c)
			return c
		}
	}
	unknown := ownerRefreshCooldown{Kind: "unknown", CLIExit: -1, AgeSeconds: 5}
	for _, tc := range []struct {
		label string
		cause func(int64) *ownerRefreshCause
		want  ownerRefreshCooldown
	}{
		{"no_cause_file", nil, unknown},
		{"outside_the_vocabulary", stored(func(c *ownerRefreshCause) { c.Kind, c.Stage, c.CLIExit, c.HTTP = "SECRET_TOKEN", "C:/private", 999, 500 }), unknown},
		{"other_failure_second", stored(func(c *ownerRefreshCause) { c.Failed-- }), unknown},
		{"other_repository", stored(func(c *ownerRefreshCause) { c.Repository = "AutoDevJapan/another" }), unknown},
		{"other_catalog", stored(func(c *ownerRefreshCause) { c.Catalog = "not-the-catalog" }), unknown},
		{"other_domain", stored(func(c *ownerRefreshCause) { c.Domain = "owner-source-refresh-cause-v0" }), unknown},
		{"fixed_values", stored(func(*ownerRefreshCause) {}), ownerRefreshCooldown{Kind: "fetch_failed", Stage: "identity_http_403", CLIExit: 4, HTTP: 403, AgeSeconds: 5}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			home := diagnosticHome(t)
			now := time.Now()
			failed := now.Unix()
			cooldownFailureMarker(t, home, repo, failed)
			if tc.cause != nil && !ownerWriteSealed(home, ownerRefreshCauseName(repo), tc.cause(failed)) {
				t.Fatal("cause fixture")
			}
			got := ownerRefreshCooldownCause(home, repo, now.Add(5*time.Second))
			if got == nil || *got != tc.want {
				t.Fatalf("cause %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestRefreshCooldownCauseFollowsTheFailureMarkerWindow(t *testing.T) {
	const repo = "AutoDevJapan/cooldown-window"
	home := diagnosticHome(t)
	now := time.Now()
	failed := now.Unix()
	for _, kind := range []string{ownerRefreshKindFetchFailed, ownerRefreshKindHelperUnfinished, ownerRefreshKindHelperStartFailed} {
		ownerRecordRefreshCause(home, repo, failed, kind, "identity_http_403", 4, 403)
		cooldownFailureMarker(t, home, repo, failed)
		got := ownerRefreshCooldownCause(home, repo, now.Add(299*time.Second))
		if got == nil || got.Kind != kind || got.Stage != "identity_http_403" || got.CLIExit != 4 || got.HTTP != 403 || got.AgeSeconds != 299 {
			t.Fatalf("kind %s reported as %+v", kind, got)
		}
		if ownerRefreshCooldownCause(home, repo, now.Add(300*time.Second)) != nil || ownerRefreshCooling(home, repo, now.Add(300*time.Second)) {
			t.Fatal("cause outlived the 300 second marker window")
		}
	}
	if ownerRefreshCooldownCause(home, repo, now.Add(-time.Second)) != nil {
		t.Fatal("cause reported before its failure second")
	}
}

func TestRefreshCooldownIgnoresCauseFilesThatAreNotValidSealedCauses(t *testing.T) {
	const repo = "AutoDevJapan/cooldown-invalid-files"
	valid := func(failed int64) ownerRefreshCause {
		return ownerRefreshCause{Domain: ownerRefreshCauseDomain, Repository: repo, Catalog: hashBytes(ownerDepartmentCatalog), Failed: failed, Kind: "fetch_failed", Stage: "identity_http_403", CLIExit: 4, HTTP: 403}
	}
	// A padded record is valid in every other respect, so only its size can make
	// the reader refuse it.
	padded := func(failed int64, size int) any {
		return struct {
			ownerRefreshCause
			Padding string `json:"padding"`
		}{valid(failed), strings.Repeat("x", size)}
	}
	fixed := ownerRefreshCooldown{Kind: "fetch_failed", Stage: "identity_http_403", CLIExit: 4, HTTP: 403, AgeSeconds: 5}
	unknown := ownerRefreshCooldown{Kind: "unknown", CLIExit: -1, AgeSeconds: 5}
	for _, tc := range []struct {
		label string
		write func(t *testing.T, home, path string, failed int64)
		want  ownerRefreshCooldown
	}{
		{"valid_record_with_an_extra_field", func(t *testing.T, home, path string, failed int64) {
			if !ownerWriteSealed(home, ownerRefreshCauseName(repo), padded(failed, 100)) {
				t.Fatal("cause fixture")
			}
		}, fixed},
		{"sealed_record_over_the_8192_byte_read_limit", func(t *testing.T, home, path string, failed int64) {
			if !ownerWriteSealed(home, ownerRefreshCauseName(repo), padded(failed, 9000)) {
				t.Fatal("cause fixture")
			}
		}, unknown},
		{"garbage_bytes", func(t *testing.T, home, path string, failed int64) {
			if os.WriteFile(path, []byte("not a sealed cause \x00\xff"), 0600) != nil {
				t.Fatal("cause fixture")
			}
		}, unknown},
		{"valid_json_that_is_not_sealed", func(t *testing.T, home, path string, failed int64) {
			raw, err := json.Marshal(valid(failed))
			if err != nil || os.WriteFile(path, raw, 0600) != nil {
				t.Fatal("cause fixture")
			}
		}, unknown},
		{"directory_instead_of_a_file", func(t *testing.T, home, path string, failed int64) {
			if os.Mkdir(path, 0700) != nil {
				t.Fatal("cause fixture")
			}
		}, unknown},
	} {
		t.Run(tc.label, func(t *testing.T) {
			home := diagnosticHome(t)
			now := time.Now()
			failed := now.Unix()
			cooldownFailureMarker(t, home, repo, failed)
			tc.write(t, home, filepath.Join(home, ownerRefreshCauseName(repo)), failed)
			got := ownerRefreshCooldownCause(home, repo, now.Add(5*time.Second))
			if got == nil || *got != tc.want {
				t.Fatalf("cause %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestRefreshCooldownKeepsTheStageOfTheFailureThatStartedIt(t *testing.T) {
	const repo = "AutoDevJapan/cooldown-stages"
	for _, tc := range []struct{ label, stage string }{
		{"persistence_failure", "identity_persistence_failed"},
		{"binding_mismatch", "identity_binding_failed"},
		{"deadline", "deadline_exceeded"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			home := diagnosticHome(t)
			now := time.Now()
			clock := func() time.Time { return now }
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			fetch := func(context.Context, string) (int64, string, string, bool) {
				calls++
				switch tc.stage {
				case "identity_binding_failed":
					return 12345, "AutoDevJapan/another", "AutoDevJapan", true
				case "deadline_exceeded":
					cancel()
					return 0, "", "", false
				}
				return 12345, repo, "AutoDevJapan", true
			}
			if tc.stage == "identity_persistence_failed" {
				if err := os.Mkdir(filepath.Join(home, hashBytes([]byte(strings.ToLower(repo)))+".dpapi"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			first := &ownerRefreshObservation{}
			if ownerRefreshIdentityRequestContextWith(parent, "unused", "", home, clock, diagnosticQuery(repo), fetch, first) || calls != 1 {
				t.Fatal("failure fixture", calls)
			}
			if d := ownerRefreshProject(context.Background(), first, false, now); d.Stage != tc.stage {
				t.Fatal("first failure", d)
			}
			now = now.Add(11 * time.Second)
			second := &ownerRefreshObservation{}
			ok := ownerRefreshIdentityRequestWith("unused", "", home, clock, diagnosticQuery(repo), fetch, second)
			d := ownerRefreshProject(context.Background(), second, ok, now)
			if ok || calls != 1 || d.Stage != "cooldown_no_valid_identity" || d.Cooldown == nil {
				t.Fatal("cause not reported", ok, calls, d)
			}
			if c := d.Cooldown; c.Kind != "fetch_failed" || c.Stage != tc.stage || c.CLIExit != nil || c.HTTP != 0 || c.AgeSeconds != 11 {
				t.Fatalf("wrong cause %+v", c)
			}
		})
	}
}

// cooldownIdentity leaves the repository cooling with the stored cause, as a
// failed refresh would have left it.
func cooldownIdentity(t *testing.T, home, repo string, failed int64, kind, stage string, cliExit, httpStatus int) {
	t.Helper()
	ownerRecordRefreshCause(home, repo, failed, kind, stage, cliExit, httpStatus)
	cooldownFailureMarker(t, home, repo, failed)
}

// cooldownServedIdentity leaves a valid cached identity, so the repository is
// still answered while its failure marker applies.
func cooldownServedIdentity(t *testing.T, home, repo string, checked int64) {
	t.Helper()
	record := ownerIdentityRecord{Domain: "owner-source-identity-v1", Catalog: hashBytes(ownerDepartmentCatalog), Repository: repo, Owner: "AutoDevJapan", ID: 12345, Checked: checked}
	if !ownerWriteSealed(home, hashBytes([]byte(strings.ToLower(repo)))+".dpapi", record) {
		t.Fatal("cached identity fixture")
	}
}

// cooldownRefresh runs one refresh over the repositories in the given order. The
// fetch counter shows whether a repository reached the network.
func cooldownRefresh(home string, now time.Time, repos ...string) (bool, ownerRefreshDiagnostic, int) {
	calls := 0
	fetch := func(context.Context, string) (int64, string, string, bool) {
		calls++
		return 0, "", "", false
	}
	query := func(ctx context.Context, _ string, a ownerSourceAccess) ownerPathVerdict {
		for _, repo := range repos {
			a.identity(repo)
		}
		return ownerUnverified()
	}
	ob := &ownerRefreshObservation{}
	ok := ownerRefreshIdentityRequestWith("unused", "", home, func() time.Time { return now }, query, fetch, ob)
	return ok, ownerRefreshProject(context.Background(), ob, ok, now), calls
}

func TestRefreshCooldownReportsTheNewestAgeOfOneSharedCause(t *testing.T) {
	const a, b = "AutoDevJapan/cooldown-a", "AutoDevJapan/cooldown-b"
	for _, order := range [][]string{{a, b}, {b, a}} {
		home := diagnosticHome(t)
		now := time.Now()
		cooldownIdentity(t, home, a, now.Unix()-9, "fetch_failed", "identity_http_403", 4, 403)
		cooldownIdentity(t, home, b, now.Unix()-10, "fetch_failed", "identity_http_403", 4, 403)
		ok, d, calls := cooldownRefresh(home, now, order...)
		if ok || calls != 0 || d.Stage != "cooldown_no_valid_identity" || d.Cooldown == nil {
			t.Fatal("shared cause not reported", order, ok, calls, d)
		}
		if c := d.Cooldown; c.Kind != "fetch_failed" || c.Stage != "identity_http_403" || c.CLIExit == nil || *c.CLIExit != 4 || c.HTTP != 403 || c.AgeSeconds != 9 {
			t.Fatalf("wrong shared cause %+v", c)
		}
	}
}

func TestRefreshCooldownIgnoresAServedIdentityThatIsAlsoCooling(t *testing.T) {
	const failedRepo, served = "AutoDevJapan/cooldown-failed", "AutoDevJapan/cooldown-served"
	for _, order := range [][]string{{failedRepo, served}, {served, failedRepo}} {
		home := diagnosticHome(t)
		now := time.Now()
		cooldownServedIdentity(t, home, served, now.Unix())
		cooldownIdentity(t, home, served, now.Unix()-5, "helper_unfinished", "", -1, 0)
		cooldownIdentity(t, home, failedRepo, now.Unix()-9, "fetch_failed", "identity_http_403", 4, 403)
		ok, d, calls := cooldownRefresh(home, now, order...)
		if !ok || calls != 0 || d.Stage != "partial_identity_failure" || d.IdentityFailure != "cooldown_no_valid_identity" || d.Cooldown == nil {
			t.Fatal("served identity changed the report", order, ok, calls, d)
		}
		if c := d.Cooldown; c.Kind != "fetch_failed" || c.Stage != "identity_http_403" || c.CLIExit == nil || *c.CLIExit != 4 || c.HTTP != 403 || c.AgeSeconds != 9 {
			t.Fatalf("cause of the served identity was reported %+v", c)
		}
	}
}

func TestRefreshCooldownLeavesTheObjectOutWhenFailedCausesDiffer(t *testing.T) {
	const a, b = "AutoDevJapan/cooldown-a", "AutoDevJapan/cooldown-b"
	for _, order := range [][]string{{a, b}, {b, a}} {
		home := diagnosticHome(t)
		now := time.Now()
		cooldownIdentity(t, home, a, now.Unix()-9, "fetch_failed", "identity_http_403", 4, 403)
		cooldownIdentity(t, home, b, now.Unix()-9, "helper_unfinished", "", -1, 0)
		ok, d, calls := cooldownRefresh(home, now, order...)
		if ok || calls != 0 || d.Stage != "cooldown_no_valid_identity" || d.Cooldown != nil {
			t.Fatal("differing causes were reported as one", order, ok, calls, d)
		}
	}
}

func TestRefreshCooldownIsNotReportedForAnotherFailureStage(t *testing.T) {
	const cooling, fresh = "AutoDevJapan/cooldown-cooling", "AutoDevJapan/cooldown-fresh"
	for _, order := range [][]string{{cooling, fresh}, {fresh, cooling}} {
		home := diagnosticHome(t)
		now := time.Now()
		cooldownIdentity(t, home, cooling, now.Unix()-9, "fetch_failed", "identity_http_403", 4, 403)
		ok, d, calls := cooldownRefresh(home, now, order...)
		if ok || calls != 1 || d.Stage != "multiple_identity_failures" || d.Cooldown != nil {
			t.Fatal("cooldown cause shown for another failure stage", order, ok, calls, d)
		}
	}
}
