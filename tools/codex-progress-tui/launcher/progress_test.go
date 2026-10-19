package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const partialFixture = `{"schema_version":1,"heartbeat_at":"2026-10-19T14:00:00Z","status":"degraded","sources":{"jobs":{"complete":false,"failed_reads":123}},"tasks":[{"task_id":3021,"display_name":"PRIVATE_TITLE","execution":"running","phase":"testing","percent":45,"progress_state":"measured"},{"task_id":42,"execution":"queued","percent":null}],"jobs":[{"execution":"running"},{"execution":"queued"},{"execution":"supervising"},{"execution":"unknown"},{"execution":"finished"}]}`

func TestProgressCommandDoesNotInterceptOfficialCommandsOrOptionValues(t *testing.T) {
	for _, args := range [][]string{{"progress"}, {"--no-daemon", "progress", "--task", "3021"}} {
		if _, ok := progressArgs(args); !ok {
			t.Fatalf("progress not recognized: %q", args)
		}
	}
	for _, args := range [][]string{{}, {"exec", "progress"}, {"-m", "progress"}, {"--", "progress"}, {"resume", "progress"}} {
		if _, ok := progressArgs(args); ok {
			t.Fatalf("official command intercepted: %q", args)
		}
	}
}

func TestCollectorProcessHelper(t *testing.T) {
	if os.Getenv("PROGRESS_LAUNCHER_TEST_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "partial":
		fmt.Fprintln(os.Stdout, partialFixture)
		os.Exit(1)
	case "invalid":
		fmt.Fprintln(os.Stdout, `{"schema_version":2,"status":"ok"}`)
	case "bad-json":
		fmt.Fprintln(os.Stdout, "PRIVATE_STDOUT")
		fmt.Fprintln(os.Stderr, "PRIVATE_STDERR")
		os.Exit(1)
	case "oversized":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", snapshotLimit+1))
	case "timeout":
		time.Sleep(10 * time.Second)
	case "exit-two":
		fmt.Fprintln(os.Stdout, partialFixture)
		os.Exit(2)
	default:
		os.Exit(9)
	}
	os.Exit(0)
}

func helperCommand(t *testing.T, ctx context.Context, mode string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestCollectorProcessHelper$", "--", mode)
	cmd.Env = append(os.Environ(), "PROGRESS_LAUNCHER_TEST_HELPER=1")
	return cmd
}

func TestPartialExitOneKeepsMeasurementsAndSeparatesWaitingFromRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, code, err := captureSnapshot(ctx, helperCommand(t, ctx, "partial"))
	if err != nil || code != 1 || len(s.Tasks) != 2 {
		t.Fatalf("partial observation lost: code=%d err=%v", code, err)
	}
	var output bytes.Buffer
	writeProgress(&output, s, nil)
	for _, want := range []string{"担当: 1件", "見張り: 2件", "稼働未確認: 1件", "123件", "部分取得", "ToDo 3021: テスト中、45%"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %q: %s", want, &output)
		}
	}
	if strings.Contains(output.String(), "PRIVATE") || strings.Contains(output.String(), "ToDo 42") {
		t.Fatalf("private or inactive row emitted: %s", &output)
	}
	output.Reset()
	writeProgress(&output, s, map[int64]bool{42: true, 999: true})
	if !strings.Contains(output.String(), "ToDo 42: 起動待ち、進捗未計測") || !strings.Contains(output.String(), "ToDo 999: 今回の取得では確認できません") {
		t.Fatalf("explicit selection missing: %s", &output)
	}
}

func TestCaptureRejectsInvalidAndOversizedOutputAndDrainsTimeout(t *testing.T) {
	for _, mode := range []string{"invalid", "bad-json", "oversized", "exit-two", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			budget := 5 * time.Second
			if mode == "timeout" {
				budget = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			cmd := helperCommand(t, ctx, mode)
			start := time.Now()
			_, code, err := captureSnapshot(ctx, cmd)
			if err == nil || code != 2 || cmd.ProcessState == nil {
				t.Fatalf("bad capture accepted or process not reaped: %s code=%d err=%v", mode, code, err)
			}
			if time.Since(start) > budget+time.Second {
				t.Fatal("capture exceeded its deadline")
			}
		})
	}
	// A timeout does not leave an in-process lock or a collector behind.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, code, err := captureSnapshot(ctx, helperCommand(t, ctx, "partial")); err != nil || code != 1 {
		t.Fatalf("capture after timeout failed: code=%d err=%v", code, err)
	}
}

func TestPartialEmptyDoesNotClaimAllStoppedOrInventPercent(t *testing.T) {
	var output bytes.Buffer
	s := progressSnapshot{HeartbeatAt: "2026-10-19T14:00:00Z", Status: "degraded"}
	writeProgress(&output, s, nil)
	if !strings.Contains(output.String(), "全担当の停止は確認できません") || strings.Contains(output.String(), "処理中のToDoはありません") {
		t.Fatalf("partial treated as empty success: %s", &output)
	}
	s.Tasks = []progressTask{{ID: 1, Execution: "running", Percent: new(int), ProgressState: "invalid", Phase: "PRIVATE_PHASE"}}
	output.Reset()
	writeProgress(&output, s, nil)
	if !strings.Contains(output.String(), "進捗未計測") || strings.Contains(output.String(), "0%") || strings.Contains(output.String(), "PRIVATE") {
		t.Fatalf("invalid percentage or arbitrary phase emitted: %s", &output)
	}
}

func TestCollectorHashRemainsRequiredIndependentlyOfTUIVersion(t *testing.T) {
	root := t.TempDir()
	custom := filepath.Join(root, "codex-progress-tui")
	collectorDir := filepath.Join(root, "agent-progress")
	for _, dir := range []string{custom, collectorDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	collector := filepath.Join(collectorDir, "agent-progress.exe")
	if err := os.WriteFile(collector, []byte("trusted collector"), 0600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(custom, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(fmt.Sprintf(`{"collector_sha256":%q}`, digest(collector))), 0600); err != nil {
		t.Fatal(err)
	}
	if collectorBinary(custom) != collector {
		t.Fatal("valid standalone collector rejected")
	}
	if err := os.WriteFile(collector, []byte("changed collector"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runProgress(nil, custom, "missing-package", &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "ハッシュと一致しません") {
		t.Fatalf("changed collector accepted: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
}

func TestCollectorReceivesOSLocationsWithoutAccountCredentials(t *testing.T) {
	env := collectorEnvironment([]string{"SystemRoot=C:\\Windows", "TEMP=C:\\Temp", "OPENCODEX_API_AUTH_TOKEN=PRIVATE", "OPENAI_API_KEY=PRIVATE", "UNRELATED=PRIVATE"})
	if len(env) != 2 || strings.Contains(strings.Join(env, "\n"), "PRIVATE") {
		t.Fatalf("unexpected collector environment: %q", env)
	}
}
