package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completedReleaseFixture(change func(map[string]any)) api {
	return mergedFixture(func(m map[string]any) {
		r := m[mergedFixtureRepo+"actions/runs/38941323954"].(map[string]any)
		r["status"], r["conclusion"] = "completed", "success"
		for _, j := range m[mergedFixtureRepo+"actions/runs/38941323954/attempts/1/jobs?per_page=100"].(map[string]any)["jobs"].([]map[string]any) {
			j["status"], j["conclusion"] = "completed", "success"
		}
		if change != nil {
			change(m)
		}
	})
}

func TestReleasePreflightRequiresExactSuccessfulOriginalMatrix(t *testing.T) {
	for _, change := range []func(map[string]any){
		func(m map[string]any) {
			m[mergedFixtureRepo+"actions/runs/38941323954"].(map[string]any)["head_sha"] = mergedPRHead
		},
		func(m map[string]any) {
			m[mergedFixtureRepo+"actions/runs/38941323954"].(map[string]any)["run_attempt"] = 2
		},
		func(m map[string]any) {
			m[mergedFixtureRepo+"actions/runs/38941323954"].(map[string]any)["conclusion"] = "failure"
		},
		func(m map[string]any) {
			m[mergedFixtureRepo+"actions/runs/38941323954/attempts/1/jobs?per_page=100"].(map[string]any)["jobs"].([]map[string]any)[0]["conclusion"] = "skipped"
		},
		func(m map[string]any) {
			m[mergedFixtureRepo+"actions/runs/38941323954/attempts/1/jobs?per_page=100"].(map[string]any)["jobs"].([]map[string]any)[0]["id"] = int64(117109408508)
		},
		func(m map[string]any) {
			m[mergedFixtureRepo+"actions/runs/38941323954/attempts/1/jobs?per_page=100"].(map[string]any)["jobs"].([]map[string]any)[0]["labels"] = []string{"ubuntu-latest"}
		},
		nil,
	} {
		read := completedReleaseFixture(change)
		r, err := verifyMerged(context.Background(), mergedCandidate(), read)
		if err != nil {
			t.Fatal(err)
		}
		err = verifyOriginalReleaseCodeQL(context.Background(), r, read)
		if (err == nil) != (change == nil) {
			t.Fatal("inexact or incomplete original results admitted")
		}
	}
}

func TestReleasePhaseCrashReservationIsExclusiveAndBoundToDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "owned")
	os.Mkdir(dir, 0700)
	path := filepath.Join(root, "admission")
	l, err := acquireReleasePhase(path, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600); err == nil {
		t.Fatal("old candidate mode can overlap")
	}
	if _, err := acquireReleasePhase(path, dir, true); err == nil {
		t.Fatal("recovery stole active lease")
	}
	l.file.Close() // Simulated owner death leaves the durable path.
	other := filepath.Join(root, "other")
	os.Mkdir(other, 0700)
	if _, err := acquireReleasePhase(path, other, true); err == nil {
		t.Fatal("wrong directory recovered reservation")
	}
	l, err = acquireReleasePhase(path, dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.release(); err != nil {
		t.Fatal(err)
	}
	l.file.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("reservation not removed")
	}
}

func TestReleasePhaseRejectsForeignOrCorruptAdmission(t *testing.T) {
	for _, b := range []string{"", "\x00{}", "\x00{broken", "\x00{\"Kind\":\"bootstrap\"}"} {
		root := t.TempDir()
		dir := filepath.Join(root, "owned")
		os.Mkdir(dir, 0700)
		path := filepath.Join(root, "admission")
		os.WriteFile(path, []byte(b), 0600)
		if _, err := acquireReleasePhase(path, dir, true); err == nil {
			t.Fatal("foreign lock accepted")
		}
		got, _ := os.ReadFile(path)
		if string(got) != b {
			t.Fatal("foreign lock edited")
		}
	}
}

func preflightTestDeps(t *testing.T, path string, built *int) releasePreflightDeps {
	t.Helper()
	return releasePreflightDeps{
		read: completedReleaseFixture(nil),
		source: func(context.Context, repository, api, func(context.Context, string) ([]byte, error)) (sealedReleaseContext, error) {
			return sealedReleaseContext{}, nil
		},
		idle:    func(context.Context, repository, api) error { return nil },
		prepare: func(sealedReleaseContext) (sealedReleaseBuildInput, error) { return sealedReleaseBuildInput{}, nil },
		build: func(_ context.Context, _ sealedReleaseBuildInput, dir string) (releaseBuildReceipt, error) {
			*built++
			if _, err := acquireReleasePhase(path, dir, true); err == nil {
				t.Fatal("build ran without kernel exclusion")
			}
			b := []byte("fixture artifact")
			return releaseBuildReceipt{Succeeded: true, PhaseMayRelease: true, artifact: b, ArtifactBytes: int64(len(b)), ArtifactSHA256: digest(b)}, nil
		},
	}
}

func TestReleasePreflightBuildAndRecoveryKeepLeaseUntilClean(t *testing.T) {
	for _, clean := range []bool{true, false} {
		root := t.TempDir()
		dir := filepath.Join(root, "owned")
		os.Mkdir(dir, 0700)
		path := filepath.Join(root, "admission")
		built := 0
		d := preflightTestDeps(t, path, &built)
		if !clean {
			d.build = func(context.Context, sealedReleaseBuildInput, string) (releaseBuildReceipt, error) {
				built++
				return releaseBuildReceipt{RecoveryRequired: true}, errors.New("fixture interrupted")
			}
		}
		r, err := runReleasePreflight(context.Background(), dir, path, false, d)
		if built != 1 || r.ExecutionAuthorized || r.Build.ExecutionAuthorized || !r.SourceVerifiedAfter || r.Succeeded != clean || r.LockReleased != clean || r.RecoveryRequired == clean || (err == nil) != clean {
			t.Fatalf("unexpected preflight result: %+v %v", r, err)
		}
		if clean {
			continue
		}
		calls := 0
		d.recover = func(context.Context, string) (releaseBuildReceipt, error) {
			calls++
			return releaseBuildReceipt{PhaseMayRelease: calls == 2, RecoveryRequired: calls != 2}, nil
		}
		for attempt := 1; attempt <= 2; attempt++ {
			r, err = runReleasePreflight(context.Background(), dir, path, true, d)
			if (err == nil) != (attempt == 2) || r.Succeeded || r.LockReleased != (attempt == 2) || r.RecoveryRequired != (attempt != 2) || built != 1 {
				t.Fatal("unsafe recovery release or rebuild")
			}
		}
		if calls != 2 {
			t.Fatal("recovery was not performed")
		}
	}
}

func TestReleasePreflightStopsBeforeBuildAndReleasesUnusedPhase(t *testing.T) {
	for _, stage := range []string{"identity", "codeql", "idle", "source", "prepare"} {
		root := t.TempDir()
		dir := filepath.Join(root, "owned")
		os.Mkdir(dir, 0700)
		path := filepath.Join(root, "admission")
		built := 0
		d := preflightTestDeps(t, path, &built)
		switch stage {
		case "identity":
			d.read = completedReleaseFixture(func(m map[string]any) { m["repositories/1285649124"].(map[string]any)["private"] = false })
		case "codeql":
			d.read = completedReleaseFixture(func(m map[string]any) {
				m[mergedFixtureRepo+"actions/runs/38941323954"].(map[string]any)["status"] = "queued"
			})
		case "idle":
			d.idle = func(context.Context, repository, api) error { return errors.New("fixture busy") }
		case "source":
			d.source = func(context.Context, repository, api, func(context.Context, string) ([]byte, error)) (sealedReleaseContext, error) {
				return sealedReleaseContext{}, errors.New("fixture invalid tree")
			}
		case "prepare":
			d.prepare = func(sealedReleaseContext) (sealedReleaseBuildInput, error) {
				return sealedReleaseBuildInput{}, errors.New("fixture invalid context")
			}
		}
		r, err := runReleasePreflight(context.Background(), dir, path, false, d)
		if err == nil || built != 0 || !r.LockReleased || r.RecoveryRequired || r.Succeeded || r.BuildAttempted {
			t.Fatal("rejection did not stop before builder")
		}
	}
}

func TestReleasePreflightChangedSourceAfterBuildIsNotSuccess(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "owned")
	os.Mkdir(dir, 0700)
	path := filepath.Join(root, "admission")
	built := 0
	d := preflightTestDeps(t, path, &built)
	base := d.read
	d.read = func(ctx context.Context, p string, out any) error {
		if built > 0 && strings.HasSuffix(p, "/git/ref/heads/main") {
			return json.Unmarshal([]byte(`{"object":{"sha":"different"}}`), out)
		}
		return base(ctx, p, out)
	}
	r, err := runReleasePreflight(context.Background(), dir, path, false, d)
	if err == nil || built != 1 || r.Succeeded || r.SourceVerifiedAfter || !r.LockReleased || !r.ArtifactVerified {
		t.Fatal("source change was accepted or clean lease retained")
	}
}

func TestReleasePreflightIdleFailsBeforeAnyBuildOnUnknownInventory(t *testing.T) {
	for _, kind := range []string{"runners", "runner-read", "containers", "container-read", "clear"} {
		calls := 0
		read := func(_ context.Context, p string, out any) error {
			if p != mergedFixtureRepo+"actions/runners?per_page=100" {
				t.Fatal("unexpected runner endpoint")
			}
			if kind == "runner-read" {
				return errors.New("opaque-sensitive-provider-error")
			}
			b := []byte(`{"total_count":0,"runners":[]}`)
			if kind == "runners" {
				b = []byte(`{"total_count":1,"runners":[{"id":1}]}`)
			}
			return json.Unmarshal(b, out)
		}
		r := repository{FullName: "AutoDevJapan/opaque-merged"}
		err := verifyReleasePreflightIdleWith(context.Background(), r, read, func(context.Context) ([]byte, error) {
			calls++
			if kind == "container-read" {
				return nil, errors.New("opaque-sensitive-daemon-error")
			}
			if kind == "containers" {
				return []byte("opaque-container-id"), nil
			}
			return nil, nil
		})
		if (err == nil) != (kind == "clear") || (calls == 0) != (kind == "runners" || kind == "runner-read") {
			t.Fatal("unknown or busy phase admitted")
		}
		if err != nil && strings.Contains(err.Error(), "opaque-sensitive") {
			t.Fatal("raw provider output exposed")
		}
	}
}

func TestReleasePreflightRechecksSourceBeforeStartingBuilder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "owned")
	os.Mkdir(dir, 0700)
	path := filepath.Join(root, "admission")
	built := 0
	d := preflightTestDeps(t, path, &built)
	base := d.read
	prepared := false
	d.prepare = func(sealedReleaseContext) (sealedReleaseBuildInput, error) {
		prepared = true
		return sealedReleaseBuildInput{}, nil
	}
	d.read = func(ctx context.Context, p string, out any) error {
		if prepared && strings.HasSuffix(p, "/git/ref/heads/main") {
			return json.Unmarshal([]byte(`{"object":{"sha":"different"}}`), out)
		}
		return base(ctx, p, out)
	}
	r, err := runReleasePreflight(context.Background(), dir, path, false, d)
	if err == nil || built != 0 || !prepared || !r.LockReleased || r.BuildAttempted {
		t.Fatal("source change during preparation crossed builder boundary")
	}
}

func TestReleasePreflightDoesNotConfuseArtifactOrCleanupWithSuccess(t *testing.T) {
	for _, bad := range []string{"artifact", "cleanup"} {
		root := t.TempDir()
		dir := filepath.Join(root, "owned")
		os.Mkdir(dir, 0700)
		path := filepath.Join(root, "admission")
		built := 0
		d := preflightTestDeps(t, path, &built)
		build := d.build
		d.build = func(ctx context.Context, in sealedReleaseBuildInput, dir string) (releaseBuildReceipt, error) {
			r, e := build(ctx, in, dir)
			if bad == "artifact" {
				r.ArtifactSHA256 = strings.Repeat("0", 64)
			} else {
				r.PhaseMayRelease = false
			}
			return r, e
		}
		r, err := runReleasePreflight(context.Background(), dir, path, false, d)
		if err == nil || built != 1 || r.Succeeded || r.LockReleased != (bad == "artifact") || r.RecoveryRequired != (bad == "cleanup") {
			t.Fatal("unverified artifact or uncertain cleanup reported success")
		}
	}
}

func TestReleasePreflightRejectsMissingNullOrInconsistentRunnerInventory(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `{"total_count":null,"runners":[]}`, `{"total_count":0}`,
		`{"total_count":0,"runners":null}`, `{"total_count":-1,"runners":[]}`,
		`{"total_count":0,"runners":[{"id":1}]}`, `{"total_count":1,"runners":[]}`,
		`{"total_count":"0","runners":[]}`, `{"total_count":0,"runners":[]}`,
	} {
		calls := 0
		read := func(_ context.Context, _ string, out any) error { return json.Unmarshal([]byte(body), out) }
		err := verifyReleasePreflightIdleWith(context.Background(), repository{}, read, func(context.Context) ([]byte, error) { calls++; return nil, nil })
		valid := body == `{"total_count":0,"runners":[]}`
		if (err == nil) != valid || (calls == 1) != valid {
			t.Fatal("malformed inventory passed as empty or valid empty was rejected")
		}
	}
}

func TestReleasePreflightSecondIdleFailureStopsBeforeIntent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "owned")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "admission")
	built, calls := 0, 0
	d := preflightTestDeps(t, path, &built)
	d.idle = func(context.Context, repository, api) error {
		calls++
		if calls == 2 {
			return errors.New("fixture phase became busy")
		}
		return nil
	}
	r, err := runReleasePreflight(context.Background(), dir, path, false, d)
	if err == nil || calls != 2 || built != 0 || r.BuildAttempted || !r.LockReleased || r.RecoveryRequired {
		t.Fatal("second idle failure persisted build intent or retained an unused lease")
	}
}

func TestReleasePreflightReservedRecoveryDoesNotBuildOrReadSource(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "owned")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "admission")
	l, err := acquireReleasePhase(path, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.file.Close(); err != nil {
		t.Fatal(err)
	}
	// Nil dependencies deliberately panic if recovery enters source/build code.
	r, err := runReleasePreflight(context.Background(), dir, path, true, releasePreflightDeps{})
	if err != nil || !r.LockReleased || r.RecoveryRequired || r.Succeeded || r.BuildAttempted || r.SourceVerifiedBefore || r.ExecutionAuthorized {
		t.Fatal("reserved-only recovery used dependencies or reported build success")
	}
}
