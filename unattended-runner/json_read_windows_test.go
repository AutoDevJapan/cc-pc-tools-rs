package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type publishOnJSONDecode struct{ path string }

func (v *publishOnJSONDecode) UnmarshalJSON([]byte) error {
	return publish(v.path, map[string]int{"generation": 2})
}

func TestReadJSONClosesBeforeDecoderCallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.json")
	if err := publish(path, map[string]int{"generation": 1}); err != nil {
		t.Fatal(err)
	}
	if err := readJSON(path, 1024, &publishOnJSONDecode{path: path}); err != nil {
		t.Fatalf("decoder callback could not publish after read: %v", err)
	}
	var result map[string]int
	if err := readJSON(path, 1024, &result); err != nil || result["generation"] != 2 {
		t.Fatal("new record unavailable")
	}
}

func TestReplaceRetriesTransientReadHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.json")
	if err := publish(path, map[string]int{"generation": 1}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	newPath := filepath.Join(filepath.Dir(path), "replacement.json")
	if err := publish(newPath, map[string]int{"generation": 2}); err != nil {
		t.Fatal(err)
	}
	attempts, waits := 0, 0
	err = retryReplace(func() error { attempts++; return replaceOnce(newPath, path) }, func(time.Duration) {
		waits++
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil || attempts != 2 || waits != 1 {
		t.Fatalf("did not recover after first real sharing failure: %v attempts=%d waits=%d", err, attempts, waits)
	}
	var current map[string]int
	if err := readJSON(path, 1024, &current); err != nil || current["generation"] != 2 {
		t.Fatal("published replacement unavailable")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal("read handles leaked")
	}
}

func TestReplaceRetryHasFiniteDeadlineAndRetainsFailedInputs(t *testing.T) {
	dir := t.TempDir()
	path, next := filepath.Join(dir, "current.json"), filepath.Join(dir, "next.json")
	if err := publish(path, map[string]int{"generation": 1}); err != nil {
		t.Fatal(err)
	}
	if err := publish(next, map[string]int{"generation": 2}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	attempts, totalWait := 0, time.Duration(0)
	err = retryReplace(func() error { attempts++; return replaceOnce(next, path) }, func(delay time.Duration) { totalWait += delay })
	if err == nil || attempts != 4 || totalWait != 70*time.Millisecond {
		t.Fatal("permanent read handle was ignored or retry unbounded")
	}
	for name, generation := range map[string]int{path: 1, next: 2} {
		b, e := os.ReadFile(name)
		var value map[string]int
		if e != nil || json.Unmarshal(b, &value) != nil || value["generation"] != generation {
			t.Fatal("failed retry changed complete inputs")
		}
	}
	attempts = 0
	err = retryReplace(func() error { attempts++; return syscall.Errno(3) }, func(time.Duration) { t.Fatal("unrelated error retried") })
	if err != syscall.Errno(3) || attempts != 1 {
		t.Fatal("non-sharing failure was hidden")
	}
}
