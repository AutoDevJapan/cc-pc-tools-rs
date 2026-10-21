package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func archiveFixture(t *testing.T, name string, kind byte) []byte {
	t.Helper()
	var b bytes.Buffer
	g := gzip.NewWriter(&b)
	w := tar.NewWriter(g)
	h := &tar.Header{Name: name, Typeflag: kind, Mode: 04755}
	if kind == tar.TypeReg {
		h.Size = 4
	}
	if kind == tar.TypeSymlink {
		h.Linkname = "/outside"
	}
	if e := w.WriteHeader(h); e != nil {
		t.Fatal(e)
	}
	if kind == tar.TypeReg {
		_, _ = w.Write([]byte("test"))
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	if e := g.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestArchiveBoundary(t *testing.T) {
	for _, name := range []string{"/absolute/file", "root/../escape", "root/a/../../escape", "root/a\\b"} {
		t.Run(name, func(t *testing.T) {
			if _, e := opaqueArchive(archiveFixture(t, name, tar.TypeReg)); e == nil {
				t.Fatal("unsafe archive admitted")
			}
		})
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeFifo} {
		if _, e := opaqueArchive(archiveFixture(t, "root/link", kind)); e == nil {
			t.Fatal("special file admitted")
		}
	}
	b, e := opaqueArchive(archiveFixture(t, "sensitive-root/scripts/test.mjs", tar.TypeReg))
	if e != nil {
		t.Fatal(e)
	}
	tr := tar.NewReader(bytes.NewReader(b))
	h, e := tr.Next()
	if e != nil {
		t.Fatal(e)
	}
	if h.Name != "scripts/test.mjs" || h.Mode != 0755 || h.Uid != 1001 {
		t.Fatalf("opaque archive header: %+v", h)
	}
	data, e := io.ReadAll(tr)
	if e != nil || string(data) != "test" {
		t.Fatalf("content changed: %q %v", data, e)
	}
	if bytes.Contains(b, []byte("sensitive-root")) {
		t.Fatal("repository root leaked into transferred archive")
	}
}
func TestFailedScriptsFromOriginalEvidence(t *testing.T) {
	b := []byte("not ok 3 - scripts/one.test.mjs\n  not ok 20 - assertion\n  location: '/work/scripts/two.test.mjs:43:2'\n")
	got, e := failedScripts(b)
	if e != nil || len(got) != 2 || got[0] != "scripts/one.test.mjs" || got[1] != "scripts/two.test.mjs" {
		t.Fatalf("selection %q %v", got, e)
	}
	if _, e = failedScripts([]byte("not ok 3 - scripts/../escape.test.mjs\n")); e == nil {
		t.Fatal("traversal admitted")
	}
	if _, e = failedScripts([]byte("# tests 0\n")); e == nil {
		t.Fatal("missing failure evidence admitted")
	}
}
func TestGlobalPAXDoesNotBecomeSourceRoot(t *testing.T) {
	var b bytes.Buffer
	g := gzip.NewWriter(&b)
	w := tar.NewWriter(g)
	if e := w.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "fixed revision"}}); e != nil {
		t.Fatal(e)
	}
	if e := w.WriteHeader(&tar.Header{Name: "source-root/file", Typeflag: tar.TypeReg, Mode: 0644, Size: 1}); e != nil {
		t.Fatal(e)
	}
	_, _ = w.Write([]byte("x"))
	_ = w.Close()
	_ = g.Close()
	if _, e := opaqueArchive(b.Bytes()); e != nil {
		t.Fatal(e)
	}
}
func TestIsolationRejectsHostAccessAndUnboundedResources(t *testing.T) {
	image := "node@sha256:" + strings.Repeat("a", 64)
	valid := `[{"Config":{"User":"1001:1001","Image":"` + image + `","Labels":{"cc.todo":"2648"}},"HostConfig":{"Init":true,"ReadonlyRootfs":true,"NetworkMode":"none","NanoCPUs":2000000000,"Memory":2147483648,"PidsLimit":256,"CapDrop":["ALL"],"SecurityOpt":["no-new-privileges=true"],"Tmpfs":{"/work":"rw,uid=1001","/tmp":"rw,noexec"}},"Mounts":[{"Type":"tmpfs","Destination":"/work"}]}]`
	if e := checkIsolation([]byte(valid), image); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ from, to string }{
		{`"Init":true`, `"Init":false`}, {`"User":"1001:1001"`, `"User":"root"`},
		{`"NetworkMode":"none"`, `"NetworkMode":"host"`}, {`"ReadonlyRootfs":true`, `"ReadonlyRootfs":false`},
		{`"Type":"tmpfs"`, `"Type":"bind"`}, {`"NanoCPUs":2000000000`, `"NanoCPUs":0`},
		{`"PidsLimit":256`, `"PidsLimit":null`}, {`"CapDrop":["ALL"]`, `"CapDrop":[]`},
		{`"Init":true`, `"Init":true,"DeviceRequests":[{"Capabilities":[["gpu"]]}]`},
		{`"Init":true`, `"Init":true,"Binds":["/host:/work"]`},
	} {
		if e := checkIsolation([]byte(strings.Replace(valid, tc.from, tc.to, 1)), image); e == nil {
			t.Fatalf("unsafe isolation admitted: %s", tc.to)
		}
	}
}
func TestOutputLimit(t *testing.T) {
	b := boundedBuffer{limit: 3}
	if _, e := b.Write([]byte("abc")); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Write([]byte("d")); e == nil || b.String() != "abc" {
		t.Fatal("unbounded response admitted")
	}
}

func TestDockerEndpointCannotInheritRemoteContext(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://untrusted.invalid:2375")
	t.Setenv("DOCKER_CONTEXT", "remote")
	t.Setenv("DOCKER_CONFIG", "untrusted-config")
	c := cmd(context.Background(), docker, "inspect", "owned")
	if len(c.Args) < 4 || c.Args[1] != "--host" || c.Args[2] != "npipe:////./pipe/dockerDesktopLinuxEngine" {
		t.Fatal("Docker endpoint is not pinned")
	}
	for _, v := range c.Env {
		key, _, _ := strings.Cut(v, "=")
		if strings.HasPrefix(strings.ToUpper(key), "DOCKER_") {
			t.Fatal("Docker routing override inherited")
		}
	}
}
func TestMutableImageRejectedBeforeFixtureLaunch(t *testing.T) {
	for _, s := range []string{"node:latest", "unknown@sha256:" + strings.Repeat("a", 64), "node@sha256:bad"} {
		if e := nodeImage(s); e == nil {
			t.Fatal("mutable or unapproved image admitted")
		}
	}
}

func TestVerifyFixedIdentity(t *testing.T) {
	c := candidates[0]
	validRepo := `{"id":1285649124,"full_name":"AutoDevJapan/opaque","private":true,"fork":false,"owner":{"login":"AutoDevJapan"},"permissions":{"admin":true}}`
	validPull := `{"state":"open","head":{"sha":"` + c.SHA + `","ref":"review/branch","repo":` + validRepo + `},"base":{"repo":` + validRepo + `}}`
	for _, tc := range []struct {
		name, repo, pull, ref string
		want                  bool
	}{
		{"valid", validRepo, validPull, c.SHA, true},
		{"fork", strings.Replace(validRepo, `"fork":false`, `"fork":true`, 1), validPull, c.SHA, false},
		{"other-owner", strings.ReplaceAll(validRepo, "AutoDevJapan", "unknown"), validPull, c.SHA, false},
		{"public", strings.Replace(validRepo, `"private":true`, `"private":false`, 1), validPull, c.SHA, false},
		{"wrong-id", strings.Replace(validRepo, "1285649124", "1392152644", 1), validPull, c.SHA, false},
		{"moved-pr", validRepo, strings.Replace(validPull, c.SHA, strings.Repeat("a", 40), 1), c.SHA, false},
		{"fork-head", validRepo, strings.Replace(validPull, `"fork":false`, `"fork":true`, 1), c.SHA, false},
		{"other-base", validRepo, strings.Replace(validPull, `"base":{"repo":{"id":1285649124`, `"base":{"repo":{"id":1392152644`, 1), c.SHA, false},
		{"moved-ref", validRepo, validPull, strings.Repeat("b", 40), false},
		{"closed", validRepo, strings.Replace(validPull, `"state":"open"`, `"state":"closed"`, 1), c.SHA, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			read := func(ctx context.Context, ep string, v any) error {
				calls++
				b := tc.repo
				if strings.Contains(ep, "/pulls/") {
					b = tc.pull
				}
				if strings.Contains(ep, "/git/ref/") {
					b = `{"object":{"sha":"` + tc.ref + `"}}`
				}
				return json.Unmarshal([]byte(b), v)
			}
			_, e := verify(context.Background(), c, read)
			if (e == nil) != tc.want {
				t.Fatalf("admitted=%v want=%v error=%v", e == nil, tc.want, e)
			}
			if tc.want && calls != 3 {
				t.Fatal("current branch not read")
			}
		})
	}
}

func TestFixedCandidatePins(t *testing.T) {
	want := []candidate{
		{ID: 1285649124, PR: 1002, SHA: "20c441df545a88f4a839d7e906b5aeb4f1708ae3"},
		{ID: 1392152644, PR: 148, SHA: "75a4051305b6f47c70ddb59a8d2bf9015666f12c"},
		{ID: 1154066087, PR: 3613, SHA: "b194d66524ef86330bb73ca3f22f9e5dd6ae0a07"},
		{ID: 1386702183, PR: 175, SHA: "b2b6d47b7f9e17617d16a9079a70de272bc3c04c"},
	}
	if len(candidates) != len(want) {
		t.Fatalf("candidate count = %d, want %d", len(candidates), len(want))
	}
	for i := range want {
		if candidates[i] != want[i] {
			t.Errorf("candidate %d = %+v, want %+v", i, candidates[i], want[i])
		}
	}
}
