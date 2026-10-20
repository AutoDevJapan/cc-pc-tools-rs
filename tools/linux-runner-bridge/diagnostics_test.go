package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticArchivePreservesEndedBytesAndRejectsUnfinished(t *testing.T) {
	root := t.TempDir()
	cycle := filepath.Join(root, "cycle-probe")
	if err := os.Mkdir(cycle, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"owner": cycleOwner, "complete": cycleOwner, "Runner.log": "exact diagnostic bytes\n"} {
		if err := os.WriteFile(filepath.Join(cycle, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := archiveEndedDiagnostics(root, &output); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "cycle-probe/Runner.log" {
			body, err := io.ReadAll(tr)
			if err != nil || string(body) != "exact diagnostic bytes\n" {
				t.Fatal("archive changed diagnostic bytes")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("diagnostic log missing")
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cycle, "complete")); err != nil {
		t.Fatal(err)
	}
	if archiveEndedDiagnostics(root, io.Discard) == nil {
		t.Fatal("unfinished evidence archived as ended")
	}
}
