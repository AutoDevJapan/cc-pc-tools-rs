package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPressHostDeadlineRetainsOriginalJobTimeoutAndBoundedOverhead(t *testing.T) {
	for _, tc := range []struct {
		workflow, role, mode string
		want                 time.Duration
	}{
		{"windows-hidden-fixtures.yml", "junction-fixtures", "windows-job", 110 * time.Minute},
		{"rust-v2-https.yml", "rust-v2-https-windows-2022", "windows-job", 110 * time.Minute},
		{"local-checks.yml", "rust-core", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "rust-integration-1", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "rust-integration-2", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "rust-integration-3", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "rust-integration-4", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "rust-field-read-signing", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "rust-aux", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "browser-package", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "browser-cold-start", "linux-job", 40 * time.Minute},
		{"rust-v2-https.yml", "rust-v2-https-ubuntu-24.04", "linux-job", 110 * time.Minute},
		{"local-checks.yml", "local-gates", "linux-job", 380 * time.Minute},
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
