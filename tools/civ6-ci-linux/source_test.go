package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func archive(t *testing.T, entries []tar.Header, data string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		h := h
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(data))
		}
		if e := tw.WriteHeader(&h); e != nil {
			t.Fatal(e)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write([]byte(data))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestOpaqueArchiveKeepsOnlyRegularFilesWithSafeModes(t *testing.T) {
	good := archive(t, []tar.Header{
		{Name: "root-abc/", Typeflag: tar.TypeDir, Mode: 0755},
		{Name: "root-abc/tools/run.sh", Typeflag: tar.TypeReg, Mode: 04755},
	}, "echo")
	out, e := opaqueArchive(good)
	if e != nil {
		t.Fatal(e)
	}
	tr := tar.NewReader(bytes.NewReader(out))
	h, e := tr.Next()
	if e != nil || h.Name != "tools/run.sh" || h.Mode&07000 != 0 || h.Uid != 1001 {
		t.Fatalf("%+v %v", h, e)
	}
	for name, entries := range map[string][]tar.Header{
		"symlink":     {{Name: "r/a", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		"hardlink":    {{Name: "r/a", Typeflag: tar.TypeLink, Linkname: "r/b"}},
		"dotdot":      {{Name: "r/../x", Typeflag: tar.TypeReg, Mode: 0644}},
		"second root": {{Name: "r/a", Typeflag: tar.TypeReg, Mode: 0644}, {Name: "s/b", Typeflag: tar.TypeReg, Mode: 0644}},
		"backslash":   {{Name: "r/a\\b", Typeflag: tar.TypeReg, Mode: 0644}},
	} {
		if _, e := opaqueArchive(archive(t, entries, "x")); e == nil {
			t.Errorf("accepted %s", name)
		}
	}
	if _, e := opaqueArchive([]byte("not gzip")); e == nil {
		t.Fatal("invalid archive accepted")
	}
	if _, e := opaqueArchive(archive(t, nil, "")); e == nil {
		t.Fatal("empty archive accepted")
	}
}

func TestOriginalCommitBytesReproducesOnlyTheRealCommit(t *testing.T) {
	payload := "tree " + rep("2", 40) + "\nauthor A <a@example.invalid> 1 +0000\ncommitter A <a@example.invalid> 1 +0000\n\nmsg\n"
	id := gitObjectID("commit", []byte(payload))
	var c originalCommit
	c.Verification.Payload = payload
	if b, e := originalCommitBytes(c, id); e != nil || string(b) != payload {
		t.Fatal(e)
	}
	if _, e := originalCommitBytes(c, rep("9", 40)); e == nil {
		t.Fatal("different commit ID accepted")
	}
}

func TestFetchWorkflowPinsBytesToThePlanDigest(t *testing.T) {
	wf := []byte("name: x\n")
	p := rustPlan()
	p.WorkflowDigest = digest(wf)
	reply := map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(wf), "sha": gitObjectID("blob", wf)}
	var asked string
	readOverride = func(_ context.Context, endpoint string, out any) error {
		asked = endpoint
		b, _ := json.Marshal(reply)
		return json.Unmarshal(b, out)
	}
	t.Cleanup(func() { readOverride = nil })
	got, e := fetchWorkflow(context.Background(), p)
	if e != nil || string(got) != string(wf) || !strings.HasSuffix(asked, "?ref="+p.WorkflowSHA) || !strings.Contains(asked, "/contents/"+rustWorkflow) {
		t.Fatalf("%v %s", e, asked)
	}
	p.WorkflowDigest = rep("0", 64)
	if _, e = fetchWorkflow(context.Background(), p); e == nil {
		t.Fatal("workflow with a different digest accepted")
	}
	p.WorkflowDigest = digest(wf)
	reply["sha"] = rep("0", 40)
	if _, e = fetchWorkflow(context.Background(), p); e == nil {
		t.Fatal("workflow differing from its Git blob accepted")
	}
	if _, e = fetchTarball(context.Background(), "main"); e == nil {
		t.Fatal("non-commit archive reference accepted")
	}
}
