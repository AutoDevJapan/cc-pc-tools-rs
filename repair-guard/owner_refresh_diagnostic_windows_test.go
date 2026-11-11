//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func diagnosticHome(t *testing.T) string {
	t.Helper()
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func diagnosticQuery(repo string) func(context.Context, string, ownerSourceAccess) ownerPathVerdict {
	return func(ctx context.Context, _ string, a ownerSourceAccess) ownerPathVerdict {
		a.identity(repo)
		return ownerUnverified()
	}
}

func TestRefreshDiagnosticActualHTTPAndErrorBuffer(t *testing.T) {
	for _, tc := range []struct{ label, stderr, stage string }{
		{"401", "HTTP 401 SECRET_TOKEN", "identity_http_401"},
		{"403", "HTTP 403 SECRET_TOKEN", "identity_http_403"},
		{"generic", "SECRET_TOKEN C:/private", "identity_fetch_failed"},
		{"bounded", strings.Repeat("x", 16384) + "HTTP 403 SECRET_TOKEN", "identity_fetch_failed"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			home := diagnosticHome(t)
			ob := &ownerRefreshObservation{}
			calls := 0
			var held *ownerRefreshErrorBuffer
			fetch := func(ctx context.Context, repo string) (int64, string, string, bool) {
				return ownerGitHubIdentityOutputWith(ctx, repo, func(cmd *exec.Cmd) ([]byte, error) {
					calls++
					held = cmd.Stderr.(*ownerRefreshErrorBuffer)
					if cmd.Path != currentGitHubCLI || cmd.WaitDelay != 20*time.Millisecond || cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
						t.Fatal("production command changed")
					}
					if n, err := held.Write([]byte(tc.stderr)); err != nil || n != len(tc.stderr) {
						t.Fatal("drain")
					}
					cmd.Path = os.Args[0]
					cmd.Args = []string{os.Args[0], "-test.run=^TestRefreshDiagnosticCLIChild$"}
					cmd.Env = append(cmd.Env, "F4431_DIAGNOSTIC_CHILD=1")
					return cmd.Output()
				})
			}
			ok := ownerRefreshIdentityRequestWith("unused", "", home, time.Now, diagnosticQuery("AutoDevJapan/diagnostic-fixture"), fetch, ob)
			d := ownerRefreshProject(context.Background(), ob, ok, time.Now())
			if ok || calls != 1 || d.Stage != tc.stage || !d.Attempted || d.CLIExit == nil || *d.CLIExit != 1 {
				t.Fatal("wrong fixed failure", d, calls)
			}
			if held == nil || len(held.data) > 16384 || strings.Trim(string(held.data), "\x00") != "" {
				t.Fatal("stderr buffer retained")
			}
			b, _ := json.Marshal(d)
			if bytes.Contains(b, []byte("SECRET_TOKEN")) || bytes.Contains(b, []byte("C:/private")) {
				t.Fatal("error exposed")
			}
		})
	}
}

func TestRefreshDiagnosticCLIChild(t *testing.T) {
	if os.Getenv("F4431_DIAGNOSTIC_CHILD") != "1" {
		return
	}
	os.Exit(1)
}

func TestRefreshDiagnosticErasesEveryWrittenBackingArray(t *testing.T) {
	var buffer ownerRefreshErrorBuffer
	var aliases [][]byte
	for _, size := range []int{1, 512, 4096, 8192, 8192} {
		chunk := bytes.Repeat([]byte{0x73}, size)
		n, err := buffer.Write(chunk)
		if n != size || err != nil {
			t.Fatal("stderr drain contract")
		}
		aliases = append(aliases, buffer.data)
	}
	if len(buffer.data) != 16384 || len(aliases) != 5 {
		t.Fatal("counter did not exercise bound")
	}
	buffer.erase()
	for index, old := range aliases {
		for _, value := range old {
			if value != 0 {
				t.Fatalf("stderr retained in prior backing array %d", index)
			}
		}
	}
}

func TestRefreshDiagnosticActualPersistenceFailureAndBindingMismatch(t *testing.T) {
	const repo = "AutoDevJapan/diagnostic-fixture"
	for _, mismatch := range []bool{false, true} {
		t.Run(fmtBool(mismatch), func(t *testing.T) {
			home := diagnosticHome(t)
			ob := &ownerRefreshObservation{}
			calls := 0
			if !mismatch {
				if err := os.Mkdir(filepath.Join(home, hashBytes([]byte(strings.ToLower(repo)))+".dpapi"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			fetch := func(context.Context, string) (int64, string, string, bool) {
				calls++
				if mismatch {
					return 12345, "AutoDevJapan/another", "AutoDevJapan", true
				}
				return 12345, repo, "AutoDevJapan", true
			}
			ok := ownerRefreshIdentityRequestWith("unused", "", home, time.Now, diagnosticQuery(repo), fetch, ob)
			d := ownerRefreshProject(context.Background(), ob, ok, time.Now())
			want := "identity_persistence_failed"
			if mismatch {
				want = "identity_binding_failed"
			}
			if ok || calls != 1 || d.Stage != want {
				t.Fatal(d, calls)
			}
			if _, valid := ownerReadIdentity(home, repo, time.Now()); valid {
				t.Fatal("invalid authority persisted")
			}
		})
	}
}

func fmtBool(v bool) string {
	if v {
		return "binding_mismatch"
	}
	return "persistence_failure"
}

func TestRefreshDiagnosticActualParentAndInnerContext(t *testing.T) {
	for _, inner := range []bool{false, true} {
		label := "parent_canceled_before_query"
		if inner {
			label = "parent_canceled_during_fetch"
		}
		t.Run(label, func(t *testing.T) {
			home := diagnosticHome(t)
			ob := &ownerRefreshObservation{}
			calls := 0
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			query := diagnosticQuery("AutoDevJapan/diagnostic-fixture")
			if !inner {
				cancel()
			} else {
				query = func(ctx context.Context, _ string, a ownerSourceAccess) ownerPathVerdict {
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 8*time.Second || time.Until(deadline) < 7*time.Second {
						t.Fatal("refresh deadline changed")
					}
					a.identity("AutoDevJapan/diagnostic-fixture")
					return ownerUnverified()
				}
			}
			fetch := func(ctx context.Context, _ string) (int64, string, string, bool) {
				calls++
				cancel()
				return 0, "", "", false
			}
			ok := ownerRefreshIdentityRequestContextWith(parent, "unused", "", home, time.Now, query, fetch, ob)
			d := ownerRefreshProject(context.Background(), ob, ok, time.Now())
			wantCalls := 0
			if inner {
				wantCalls = 1
			}
			if ok || calls != wantCalls || !d.Timeout || d.Stage != "deadline_exceeded" {
				t.Fatal(d, calls)
			}
		})
	}
}

func TestRefreshDiagnosticInnerDeadlineIsNotOuterTimeout(t *testing.T) {
	home := diagnosticHome(t)
	ob := &ownerRefreshObservation{}
	calls := 0
	query := func(ctx context.Context, _ string, _ ownerSourceAccess) ownerPathVerdict {
		inner, stop := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer stop()
		if inner.Err() != context.DeadlineExceeded || ctx.Err() != nil {
			t.Fatal("contexts not independent")
		}
		return ownerUnverified()
	}
	ok := ownerRefreshIdentityRequestWith("unused", "", home, time.Now, query, func(context.Context, string) (int64, string, string, bool) { calls++; return 0, "", "", false }, ob)
	d := ownerRefreshProject(context.Background(), ob, ok, time.Now())
	if ok || calls != 0 || d.Timeout || d.Stage != "binding_unavailable" {
		t.Fatal(d, calls)
	}
}

func TestRefreshDiagnosticActualClaimMismatchFetchesNothing(t *testing.T) {
	home := diagnosticHome(t)
	ob := &ownerRefreshObservation{}
	calls := 0
	ok := ownerRefreshIdentityRequestWith("unused", strings.Repeat("a", 32), home, time.Now, diagnosticQuery("AutoDevJapan/diagnostic-fixture"), func(context.Context, string) (int64, string, string, bool) { calls++; return 0, "", "", false }, ob)
	d := ownerRefreshProject(context.Background(), ob, ok, time.Now())
	if ok || calls != 0 || d.Stage != "claim_unavailable" {
		t.Fatal(d, calls)
	}
}

func TestRefreshDiagnosticActualCLIRejectsMixedAndBadFlagsBeforeCallbacks(t *testing.T) {
	cases := [][]string{
		{"--diagnostic"}, {"--diagnostic=false", "--guard-path", "C:/protected"},
		{"--diagnostic", "--refresh-owner-source", "C:/source", "--guard-path", "C:/protected"},
		{"--diagnostic", "--refresh-owner-source", "C:/source", "--verify-current-succession", "1626"},
		{"--diagnostic", "--refresh-owner-source", "C:/source", "--verify-capacity-runtime-update", "C:/protected"},
		{"--diagnostic", "--refresh-owner-source", "C:/source", "--verify-model-runtime-update", "C:/protected"},
		{"--diagnostic", "--refresh-owner-source", "C:/source", "--unknown=SECRET_TOKEN"},
		{"--unknown=SECRET_TOKEN", "--diagnostic", "--refresh-owner-source", "C:/source"},
		{"--diagnostic", "--refresh-owner-source", "relative"},
	}
	for i, args := range cases {
		var out, err bytes.Buffer
		calls := 0
		code := runGuardCLIWithRefresh(args, &out, &err, func(int) object { calls++; return nil }, func(string, string, bool) (bool, ownerRefreshDiagnostic) {
			calls++
			return false, ownerRefreshDiagnostic{}
		})
		if code != 2 || calls != 0 || strings.Contains(out.String()+err.String(), "SECRET_TOKEN") {
			t.Fatal("bad flag consumed", i, code, calls)
		}
	}
}

type diagnosticBrokenWriter struct{}

func (diagnosticBrokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture output failure")
}

func TestRefreshDiagnosticCLIOutputFailureNeverReportsSuccess(t *testing.T) {
	for _, diagnostic := range []bool{false, true} {
		args := []string{"--refresh-owner-source", "C:/source"}
		if diagnostic {
			args = append(args, "--diagnostic")
		}
		code := runGuardCLIWithRefresh(args, diagnosticBrokenWriter{}, &bytes.Buffer{}, func(int) object { return nil }, func(string, string, bool) (bool, ownerRefreshDiagnostic) {
			return true, ownerRefreshDiagnostic{Refreshed: true, Stage: "complete"}
		})
		if code != 1 {
			t.Fatal("unwritten result reported success", code)
		}
	}
}

func TestRefreshDiagnosticFlagHasExplicitScope(t *testing.T) {
	_, err := parseGuardArgs([]string{"--refresh-owner-source", "C:/self-source", "--diagnostic"})
	if err != nil {
		t.Fatal("normal refresh diagnostic flag rejected", err)
	}
}

func TestRefreshDiagnosticActualWithNoFetchBinding(t *testing.T) {
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ob := &ownerRefreshObservation{}
	ok := ownerRefreshIdentityRequestWith("unused", "", home, time.Now, func(context.Context, string, ownerSourceAccess) ownerPathVerdict { return ownerUnverified() }, func(context.Context, string) (int64, string, string, bool) { calls++; return 0, "", "", false }, ob)
	d := ownerRefreshProject(context.Background(), ob, ok, time.Now())
	if ok || calls != 0 || d.Stage != "binding_unavailable" {
		t.Fatal(d, calls)
	}
}
func TestRefreshDiagnosticActualWithFailureAndCooldown(t *testing.T) {
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := "AutoDevJapan/diagnostic-fixture"
	calls := 0
	q := func(ctx context.Context, _ string, a ownerSourceAccess) ownerPathVerdict {
		a.identity(repo)
		return ownerUnverified()
	}
	f := func(context.Context, string) (int64, string, string, bool) { calls++; return 0, "", "", false }
	ob := &ownerRefreshObservation{}
	ok := ownerRefreshIdentityRequestWith("unused", "", home, time.Now, q, f, ob)
	d := ownerRefreshProject(context.Background(), ob, ok, time.Now())
	if ok || calls != 1 || d.Stage != "identity_fetch_failed" || !d.Attempted {
		t.Fatal(d, calls)
	}
	ob = &ownerRefreshObservation{}
	ok = ownerRefreshIdentityRequestWith("unused", "", home, time.Now, q, f, ob)
	d = ownerRefreshProject(context.Background(), ob, ok, time.Now())
	if ok || calls != 1 || d.Stage != "cooldown_no_valid_identity" || d.Attempted {
		t.Fatal(d, calls)
	}
}
func TestRefreshDiagnosticActualPersistence(t *testing.T) {
	home, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	q := func(ctx context.Context, _ string, a ownerSourceAccess) ownerPathVerdict {
		a.identity("AutoDevJapan/diagnostic-fixture")
		return ownerUnverified()
	}
	calls := 0
	f := func(context.Context, string) (int64, string, string, bool) {
		calls++
		return 12345, "AutoDevJapan/diagnostic-fixture", "AutoDevJapan", true
	}
	ob := &ownerRefreshObservation{}
	ok := ownerRefreshIdentityRequestWith("unused", "", home, time.Now, q, f, ob)
	d := ownerRefreshProject(context.Background(), ob, ok, time.Now())
	if !ok || calls != 1 || d.Stage != "complete" {
		t.Fatal(d, calls)
	}
	ob = &ownerRefreshObservation{}
	ok = ownerRefreshIdentityRequestWith("unused", "", home, time.Now, q, f, ob)
	if !ok || calls != 1 {
		t.Fatal("valid cache refetched", calls)
	}
}

func TestRefreshCLIKeepsNormalOutputAndDiagnosticExit(t *testing.T) {
	for _, ok := range []bool{false, true} {
		for _, diag := range []bool{false, true} {
			var stdout, stderr bytes.Buffer
			calls := 0
			args := []string{"--refresh-owner-source", "C:/self-source"}
			if diag {
				args = append(args, "--diagnostic")
			}
			got := runGuardCLIWithRefresh(args, &stdout, &stderr, func(int) object { return nil }, func(path, request string, want bool) (bool, ownerRefreshDiagnostic) {
				calls++
				return ok, ownerRefreshDiagnostic{Refreshed: ok, Stage: "fixture"}
			})
			wantExit := 1
			if ok {
				wantExit = 0
			}
			if got != wantExit || calls != 1 || stderr.Len() != 0 {
				t.Fatal("exit/calls", got, calls)
			}
			var payload map[string]any
			if json.Unmarshal(stdout.Bytes(), &payload) != nil {
				t.Fatal("json")
			}
			if payload["refreshed"] != ok {
				t.Fatal("bool")
			}
			if !diag && len(payload) != 2 {
				t.Fatal("normal output changed", payload)
			}
		}
	}
	var out, err bytes.Buffer
	calls := 0
	got := runGuardCLIWithRefresh([]string{"--diagnostic", "--guard-path", "C:/must-not-read"}, &out, &err, func(int) object { return nil }, func(string, string, bool) (bool, ownerRefreshDiagnostic) {
		calls++
		return false, ownerRefreshDiagnostic{}
	})
	if got != 2 || calls != 0 {
		t.Fatal("bad flag reached refresh")
	}
}
