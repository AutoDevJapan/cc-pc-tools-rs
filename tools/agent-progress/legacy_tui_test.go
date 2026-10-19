package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyTUIArgumentsFilterOnlyItsCanonicalOutput(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","progress":83}]}`)
	legacy := filepath.Join(f.root, "output", "snapshot-v1.json")
	for _, tc := range []struct {
		name, path string
		extra      []string
		want       int
	}{
		{"old TUI without new flags", legacy, nil, 0},
		{"explicit diagnostic override", legacy, []string{"--active-only=false"}, 1},
		{"other diagnostic output", filepath.Join(f.root, "output", "diagnostic.json"), nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exact old acquisition arguments plus isolated metadata inputs.
			args := []string{"--once", "--deadline", "4s", "--max-rows", "1000", "--output", tc.path,
				"--todo", f.opts.TodoFile, "--jobs", f.opts.JobsDir, "--priority-file", "", "--resource-queue", f.opts.ResourceQueueDir}
			args = append(args, tc.extra...)
			var stdout, stderr bytes.Buffer
			if runWithLegacyOutput(args, &stdout, &stderr, legacy) != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatal("legacy-compatible CLI failed")
			}
			data, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			var s Snapshot
			if json.Unmarshal(data, &s) != nil || s.SchemaVersion != 1 || s.Status != "ok" || len(s.Tasks) != tc.want || len(s.Jobs) != 0 {
				t.Fatal("wrong compatible snapshot projection")
			}
			if tc.want == 1 && (s.Tasks[0].Percent == nil || *s.Tasks[0].Percent != 83) {
				t.Fatal("diagnostic measured progress changed")
			}
		})
	}
}

func TestLegacyOutputIdentityRequiresTheSameResolvedFile(t *testing.T) {
	f := fixtureFor(t, `{"items":[]}`)
	path := filepath.Join(f.root, "output", "snapshot-v1.json")
	if !sameOutput(path, path) || sameOutput("", path) || sameOutput(path, "") || sameOutput(filepath.Join(f.root, "other", "snapshot-v1.json"), path) {
		t.Fatal("legacy consumer identity is too broad")
	}
}
