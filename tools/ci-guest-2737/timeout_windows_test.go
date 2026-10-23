package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPressInsufficientPreparationBudgetRejectsBeforeRegistration(t *testing.T) {
	pin := pressFixturePin("local-checks.yml")
	if requirePressSessionBudget(context.Background(), pin, "rust-core") == nil {
		t.Fatal("unbounded context admitted")
	}
	short, cancel := context.WithTimeout(context.Background(), 129*time.Minute)
	defer cancel()
	if requirePressSessionBudget(short, pin, "rust-core") == nil {
		t.Fatal("short session admitted")
	}
	long, stop := context.WithTimeout(context.Background(), 150*time.Minute)
	defer stop()
	if requirePressSessionBudget(long, pin, "rust-core") != nil {
		t.Fatal("complete original session refused")
	}
	if requirePressSessionBudget(short, pin, "unknown") == nil {
		t.Fatal("unknown role admitted")
	}
	if requirePressSessionBudget(short, pressFixturePin("rust-v2-https.yml"), "rust-v2-https-windows-2022") == nil {
		t.Fatal("short Windows session admitted")
	}
}

func TestGateDeadlineChild(t *testing.T) {
	if os.Getenv("CC2766_GATE_DEADLINE_TEST") != "1" {
		return
	}
	if e := gateChild(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestRealGateChildRefusesMissingExpiredAndShortPressDeadlines(t *testing.T) {
	for _, mode := range []string{"linux-job", "windows-job"} {
		workflow, role := "local-checks.yml", "rust-core"
		if mode == "windows-job" {
			workflow, role = "windows-hidden-fixtures.yml", "junction-fixtures"
		}
		root := t.TempDir()
		request := linuxJobRequest{Source: pressFixturePin(workflow), RunID: 7, Attempt: 2, Role: role, RouteLease: strings.Repeat("1", 32)}
		b, e := json.Marshal(request)
		if e != nil {
			t.Fatal(e)
		}
		file := filepath.Join(root, "request.json")
		if e = os.WriteFile(file, b, 0600); e != nil {
			t.Fatal(e)
		}
		for _, tc := range []struct {
			name     string
			deadline time.Time
			want     string
		}{
			{"missing", time.Time{}, "gate parent deadline absent"},
			{"expired", time.Now().Add(-time.Minute), "gate parent deadline expired"},
			{"queue-consumed-budget", time.Now().Add(129 * time.Minute), "insufficient remaining Press session budget"},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				var input bytes.Buffer
				enc := json.NewEncoder(&input)
				id := strings.Repeat("a", 32)
				if e := enc.Encode(gatedHello{ID: id, Mode: mode, Root: root, Request: file, Deadline: tc.deadline}); e != nil {
					t.Fatal(e)
				}
				if e := enc.Encode(gatedControl{ID: id, Kind: "start"}); e != nil {
					t.Fatal(e)
				}
				exe, e := os.Executable()
				if e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				c := command(ctx, exe, "-test.run=^TestGateDeadlineChild$")
				c.Env = append(os.Environ(), "CC2766_GATE_DEADLINE_TEST=1", "CARGO_BUILD_JOBS=1", "RUST_TEST_THREADS=1", "CODEX_JOBS_CARGO_LOCK_HELD=1", "CODEX_JOBS_CARGO_TARGET_HELD="+root)
				c.Stdin = &input
				var stdout, stderr bytes.Buffer
				c.Stdout, c.Stderr = &stdout, &stderr
				err := c.Run()
				if ctx.Err() != nil || err == nil || strings.TrimSpace(stderr.String()) != tc.want {
					t.Fatalf("gate child did not reject before executor: error=%v stderr=%q", err, stderr.String())
				}
				var admitted gateFrame
				if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &admitted) != nil || admitted.ID != id || admitted.Stage != "admitted" {
					t.Fatal("actual gate child admission path not exercised")
				}
			})
		}
		deadline := time.Now().Add(150 * time.Minute).Round(0)
		q := gatedHello{Mode: mode, Request: file, Deadline: deadline}
		// The wire representation, rather than time.Duration, carries the same
		// absolute deadline into the queue child and the post-boot check.
		b, e = json.Marshal(q)
		if e != nil || json.Unmarshal(b, &q) != nil {
			t.Fatal("deadline frame unavailable")
		}
		ctx, cancel, e := gatedWorkContext(q)
		if e != nil {
			t.Fatal(e)
		}
		got, limited := ctx.Deadline()
		if !limited || !got.Equal(deadline) || requirePressSessionBudget(ctx, request.Source, role) != nil {
			t.Error("queue child reset or lost the parent's absolute deadline")
		}
		cancel()
	}
}

func TestPressHostDeadlineRetainsOriginalJobTimeoutAndBoundedOverhead(t *testing.T) {
	for _, tc := range []struct {
		workflow, role, mode string
		want                 time.Duration
	}{
		{"windows-hidden-fixtures.yml", "junction-fixtures", "windows-job", 150 * time.Minute},
		{"rust-v2-https.yml", "rust-v2-https-windows-2022", "windows-job", 150 * time.Minute},
		{"local-checks.yml", "rust-core", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "rust-integration-1", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "rust-integration-2", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "rust-integration-3", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "rust-integration-4", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "rust-field-read-signing", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "rust-aux", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "browser-package", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "browser-cold-start", "linux-job", 80 * time.Minute},
		{"rust-v2-https.yml", "rust-v2-https-ubuntu-24.04", "linux-job", 150 * time.Minute},
		{"local-checks.yml", "local-gates", "linux-job", 420 * time.Minute},
	} {
		q := linuxJobRequest{Source: pressFixturePin(tc.workflow), RunID: 7, Attempt: 2, Role: tc.role, RouteLease: strings.Repeat("1", 32)}
		b, e := json.Marshal(q)
		if e != nil {
			t.Fatal(e)
		}
		file := filepath.Join(t.TempDir(), "request.json")
		if e = os.WriteFile(file, b, 0600); e != nil {
			t.Fatal(e)
		}
		args := []string{"host", tc.mode, "root", "helper", file}
		if tc.mode == "linux-job" {
			args = []string{"host", tc.mode, "root", "sdk", "helper", file}
		}
		if hostJobTimeout(args) != tc.want {
			t.Fatal("Press job inherited MCP's shorter deadline")
		}
	}
	if hostJobTimeout([]string{"host", "inspect-executor"}) != 55*time.Minute {
		t.Fatal("old operation deadline changed")
	}
}
