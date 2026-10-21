package pressci

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPressWorkflowMergeGuest(t *testing.T) {
	if os.Getenv("CC2766_MERGE_GUEST") != "1" {
		t.Skip("isolated workflow input not supplied")
	}
	var input []workflowMergeInput
	d := json.NewDecoder(io.LimitReader(os.Stdin, 512<<10))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || len(input) != 3 {
		t.Fatal("workflow frame rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var results []workflowMergeResult
	for _, item := range input {
		if item.Name != "local-checks.yml" && item.Name != "rust-v2-https.yml" && item.Name != "windows-hidden-fixtures.yml" {
			t.Fatal("workflow rejected")
		}
		var paths []string
		for i, raw := range [][]byte{item.Base, item.Candidate, item.Main} {
			want := []string{item.BaseBlob, item.CandidateBlob, item.MainBlob}[i]
			c := exec.CommandContext(ctx, "/usr/bin/git", "hash-object", "--stdin")
			c.Stdin = bytes.NewReader(raw)
			b, e := c.Output()
			if e != nil || strings.TrimSpace(string(b)) != want {
				t.Fatal("physical workflow blob mismatch")
			}
			path := filepath.Join("/work", item.Name+[]string{"-base", "-candidate", "-main"}[i])
			f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				t.Fatal(e)
			}
			_, e = f.Write(raw)
			closeErr := f.Close()
			if e != nil || closeErr != nil {
				t.Fatal("private workflow write failed")
			}
			paths = append(paths, path)
		}
		c := exec.CommandContext(ctx, "/usr/bin/git", "merge-file", "--stdout", paths[1], paths[0], paths[2])
		b, e := c.Output()
		code := 0
		if e != nil {
			ee, ok := e.(*exec.ExitError)
			if !ok || ee.ExitCode() < 1 || ee.ExitCode() > 127 {
				t.Fatal("physical merge execution failed")
			}
			code = ee.ExitCode()
		}
		results = append(results, workflowMergeResult{item.Name, item.BaseBlob, item.CandidateBlob, item.MainBlob, code, bytes.Count(b, []byte("<<<<<<<"))})
		clear(b) // Never return private merged workflow text to the host.
	}
	b, e := json.Marshal(results)
	if e != nil {
		t.Fatal(e)
	}
	t.Log("workflow_merge_metadata=" + string(b))
}
