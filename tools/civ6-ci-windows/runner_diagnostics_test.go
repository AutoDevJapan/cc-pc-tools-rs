package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const fakeRunnerSecret = "synthetic-never-save-this-credential-0123456789"

var hideRunnerTestCommand = func(*exec.Cmd) {}

func TestRunnerSignalsBoundedAndSplit(t *testing.T) {
	var out runnerSignals
	for _, b := range []byte("prefix " + fakeRunnerSecret + " Connected to GitHub CryptographicException Status code: 403") {
		_, _ = out.Write([]byte{b})
	}
	if got := strings.Join(out.snapshot(), ","); got != "connected,crypto,http_403" {
		t.Fatalf("unexpected fixed observations: %s", got)
	}
	for range 1000 {
		_, _ = out.Write([]byte(strings.Repeat("x", 32768)))
	}
	if len(out.tail) > runnerTailSize {
		t.Fatal("unbounded output retained")
	}
	out.clear()
	if len(out.tail) != 0 {
		t.Fatal("output tail not cleared")
	}
}

func TestRunnerSignalsConcurrent(t *testing.T) {
	var out runnerSignals
	var group sync.WaitGroup
	for _, s := range []string{"Connected to GitHub", "UnauthorizedAccessException", "No such host is known"} {
		group.Add(1)
		go func() { defer group.Done(); _, _ = out.Write([]byte(s)) }()
	}
	group.Wait()
	if got := strings.Join(out.snapshot(), ","); got != "connected,access,network" {
		t.Fatalf("concurrent observations lost: %s", got)
	}
}

func TestRunnerSignalsOfficialHttpFailure(t *testing.T) {
	// Credential-free output observed from official runner v2.338.0 configure
	// using a deliberately invalid dummy registration token.
	observed := "Response status code does not indicate success: 404 (Not Found).\r\n"
	for split := 0; split <= len(observed); split++ {
		var out runnerSignals
		_, _ = out.Write([]byte(observed[:split]))
		_, _ = out.Write([]byte(observed[split:]))
		if got := strings.Join(out.snapshot(), ","); got != "http_404" {
			t.Fatalf("split %d: %s", split, got)
		}
	}
	var out runnerSignals
	_, _ = out.Write([]byte("HttpRequestException: Response status code does not indicate success: 401 (Unauthorized)."))
	if got := strings.Join(out.snapshot(), ","); got != "http_exception,http_401" {
		t.Fatalf("HTTP response misclassified: %s", got)
	}
}

func assertRunnerReceiptNoSecret(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("unexpected receipt directory")
		}
		b, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), fakeRunnerSecret) {
			t.Fatalf("secret persisted in %s", entry.Name())
		}
	}
}

func TestRunnerReceiptRealChild(t *testing.T) {
	for _, mode := range []string{"success", "exit", "start"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			c := exec.Command(os.Args[0], "-test.run=^TestRunnerReceiptChild$", fakeRunnerSecret, "--", mode)
			if mode == "start" {
				c = exec.Command(filepath.Join(root, "missing-child"))
			}
			c.Env = append(os.Environ(), "CIW_RECEIPT_TEST_CHILD=1", "ACTIONS_RUNNER_INPUT_TOKEN="+fakeRunnerSecret)
			hideRunnerTestCommand(c)
			err := runRunnerCommand(root, "configure", c)
			if (err == nil) != (mode == "success") {
				t.Fatalf("mode %s error %v", mode, err)
			}
			b, readErr := os.ReadFile(filepath.Join(root, "runner-configure-result.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if strings.Contains(string(b), fakeRunnerSecret) {
				t.Fatal("child output leaked")
			}
			var result runnerCommandResult
			if err := json.Unmarshal(b, &result); err != nil {
				t.Fatal(err)
			}
			if result.Started != (mode != "start") || result.OK != (mode == "success") {
				t.Fatalf("wrong outcome: %+v", result)
			}
			if mode == "exit" && (result.Exit == nil || *result.Exit != 41) {
				t.Fatal("original exit lost")
			}
			if mode == "start" && (result.Exit != nil || result.Error != "start") {
				t.Fatal("failed start misreported as exit")
			}
			if mode != "start" && strings.Join(result.Observed, ",") != "connected,http_404" {
				t.Fatal("child observations lost")
			}
			if c.Env != nil {
				t.Fatal("credential environment retained on command")
			}
			assertRunnerReceiptNoSecret(t, root)
		})
	}
}

func TestRunnerReceiptRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "should-not-exist")
	if err := os.WriteFile(filepath.Join(root, "runner-configure-start.json"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(os.Args[0], "-test.run=^TestRunnerReceiptChild$", "--", "marker", marker)
	hideRunnerTestCommand(c)
	c.Env = append(os.Environ(), "CIW_RECEIPT_TEST_CHILD=1")
	if err := runRunnerCommand(root, "configure", c); err == nil {
		t.Fatal("repeated stage accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("child started despite missing start receipt")
	}
	if err := runRunnerCommand(root, "unknown", c); err == nil {
		t.Fatal("caller stage accepted")
	}
}

func TestRunnerReceiptResultWriteFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runner-listener-result.json"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(os.Args[0], "-test.run=^TestRunnerReceiptChild$", "--", "success")
	hideRunnerTestCommand(c)
	c.Env = append(os.Environ(), "CIW_RECEIPT_TEST_CHILD=1")
	if err := runRunnerCommand(root, "listener", c); err == nil {
		t.Fatal("missing result receipt reported success")
	}
	b, err := os.ReadFile(filepath.Join(root, "runner-listener-result.json"))
	if err != nil || string(b) != "original" {
		t.Fatal("old receipt overwritten")
	}
	assertRunnerReceiptNoSecret(t, root)
}

func TestRunnerReceiptChild(t *testing.T) {
	if os.Getenv("CIW_RECEIPT_TEST_CHILD") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if len(os.Args) > 2 && os.Args[len(os.Args)-2] == "marker" {
		_ = os.WriteFile(mode, []byte("ran"), 0600)
		os.Exit(0)
	}
	fmt.Fprint(os.Stdout, fakeRunnerSecret+" Connected to Git")
	fmt.Fprint(os.Stdout, "Hub\n")
	fmt.Fprint(os.Stderr, "HttpResponseException: Not Found (404) "+fakeRunnerSecret)
	if mode == "exit" {
		os.Exit(41)
	}
	os.Exit(0)
}
