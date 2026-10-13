package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIStatusAndArgumentPrivacy(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":2547,"status":"doing","progress":40}]}`)
	args := []string{"--once", "--todo", f.opts.TodoFile, "--jobs", f.opts.JobsDir, "--priority-file", "", "--task", "2547"}
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("CLI code=%d, stderr=%s", code, stderr.String())
	}
	var s Snapshot
	if json.Unmarshal(stdout.Bytes(), &s) != nil || s.SchemaVersion != 1 || len(s.Tasks) != 1 || *s.Tasks[0].Percent != 40 || s.HeartbeatAt == "" {
		t.Fatal("CLI did not emit its supported snapshot contract")
	}
	for _, bad := range [][]string{
		{"--task", "PRIVATE@example.invalid"}, {"--PRIVATE"}, {"--once=false"},
		{"--deadline", "2h"}, {"--max-rows", "0"}, {"--max-jobs", "9999999"},
	} {
		stdout.Reset()
		stderr.Reset()
		if run(bad, &stdout, &stderr) != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "PRIVATE") {
			t.Fatal("invalid arguments must fail without repeating their private values")
		}
	}
	if err := os.Remove(f.opts.TodoFile); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if run(args, &stdout, &stderr) != 1 || json.Unmarshal(stdout.Bytes(), &s) != nil || s.Status != "degraded" {
		t.Fatal("source failure should return nonzero with an explicit failure snapshot")
	}
}

func TestOutputCannotReplaceSourcesOrTheirAliases(t *testing.T) {
	f := fixtureFor(t, `{"items":[]}`)
	f.job(t, "job", `{"id":"job","status":"running"}`)
	alias := filepath.Join(f.root, "output", "alias.json")
	if err := os.Link(f.opts.TodoFile, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{f.opts.TodoFile, alias, filepath.Join(f.opts.JobsDir, "job", "job.json"), filepath.Join(filepath.Dir(f.opts.TodoFile), "other.json")} {
		if safeOutput(path, f.opts) {
			t.Fatal("output accepted a protected source location or hardlink")
		}
		if publish(path, []byte(`{"changed":true}`), f.opts) == nil {
			t.Fatal("source overwrite was not rejected")
		}
	}
	data, err := os.ReadFile(f.opts.TodoFile)
	if err != nil || string(data) != `{"items":[]}` {
		t.Fatal("failed output altered source bytes")
	}
	if inside(filepath.Join(f.root, "jobs-other"), f.opts.JobsDir) {
		t.Fatal("a path prefix is not containment")
	}
}

func TestAtomicPublicationAndFailedReplacement(t *testing.T) {
	f := fixtureFor(t, `{"items":[]}`)
	path := filepath.Join(f.root, "output", "snapshot.json")
	a := []byte(`{"schema_version":1,"generation":"a","payload":"` + strings.Repeat("a", 32000) + `"}`)
	b := []byte(`{"schema_version":1,"generation":"b","payload":"` + strings.Repeat("b", 48000) + `"}`)
	if err := publish(path, a, f.opts); err != nil {
		t.Fatal(err)
	}
	// Readers run across real replacements and may only see complete generations.
	result := make(chan string, 1)
	go func() {
		for i := 0; i < 200; i++ {
			data, err := os.ReadFile(path)
			if err != nil {
				result <- "published file could not be read"
				return
			}
			if !bytes.Equal(data, a) && !bytes.Equal(data, b) {
				result <- "partial generation exposed to reader"
				return
			}
		}
		result <- ""
	}()
	for i := 0; i < 20; i++ {
		data := a
		if i%2 == 0 {
			data = b
		}
		if err := publish(path, data, f.opts); err != nil {
			t.Fatal(err)
		}
	}
	if message := <-result; message != "" {
		t.Fatal(message)
	}
	old, _ := os.ReadFile(path)
	// An invalid target cannot erase the existing published snapshot.
	if publish(filepath.Join(f.root, "output", "missing", "snapshot.json"), b, f.opts) == nil {
		t.Fatal("publication with a missing parent should fail")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(old, after) {
		t.Fatal("failed publication changed the previous generation")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".agent-progress-") {
			t.Fatal("staging file leaked after publication")
		}
	}
}
