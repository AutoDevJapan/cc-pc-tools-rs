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

func TestPublicRunnerSDKArchive(t *testing.T) {
	if os.Getenv("CC2737_PUBLIC_SDK_STDIN") != "1" {
		t.Skip("public SDK fixture not supplied")
	}
	b, e := io.ReadAll(io.LimitReader(os.Stdin, 768<<20))
	if e != nil || hash(b) != "c2063dcd969ac4cecb76a6fc81e476f78e331e7bed0a7eefc5c4304b56be5578" {
		t.Fatal("pinned public SDK fixture rejected")
	}
	if e = extractRunnerSDK(b, filepath.Join(t.TempDir(), "runner")); e != nil {
		t.Fatal(e)
	}
}

func sdkArchive(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	w := tar.NewWriter(z)
	for _, h := range entries {
		if e := w.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if h.Typeflag == tar.TypeReg && h.Size > 0 {
			if _, e := w.Write(bytes.Repeat([]byte{'x'}, int(h.Size))); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestSDKPathsRejectTraversalAndSpecialMembers(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", "a/../../outside", "a\\b", "a\nb"} {
		if _, e := safeSDKPath("/owned-sdk", name); e == nil {
			t.Fatal("SDK path escape admitted")
		}
	}
	for _, h := range []*tar.Header{{Name: "../outside", Typeflag: tar.TypeReg, Size: 1, Mode: 0644}, {Name: ".credentials", Typeflag: tar.TypeReg, Size: 1, Mode: 0600}, {Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/outside", Mode: 0777}} {
		if e := extractRunnerSDK(sdkArchive(t, h), filepath.Join(t.TempDir(), "sdk")); e == nil {
			t.Fatal("configured or special SDK member admitted")
		}
	}
}
func TestSDKExtractionRetainsOnlyExpectedPublicExecutables(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runner")
	if e := extractRunnerSDK(sdkArchive(t,
		&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0755},
		&tar.Header{Name: "./bin/", Typeflag: tar.TypeDir, Mode: 0755},
		&tar.Header{Name: "./bin/listener", Typeflag: tar.TypeReg, Size: 2, Mode: 0755},
		&tar.Header{Name: "./bin/listener-alias", Typeflag: tar.TypeSymlink, Linkname: "listener", Mode: 0777},
		&tar.Header{Name: "./config.sh", Typeflag: tar.TypeReg, Size: 1, Mode: 0755}), root); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(root, "config.sh"))
	if e != nil || info.Mode().Perm() != 0755 {
		t.Fatal("SDK executable mode lost")
	}
	if b, e := os.ReadFile(filepath.Join(root, "bin/listener-alias")); e != nil || len(b) != 2 {
		t.Fatal("internal SDK link lost")
	}
	dest := filepath.Join(t.TempDir(), "sccache")
	data := sdkArchive(t, &tar.Header{Name: "sdk/sccache", Typeflag: tar.TypeReg, Size: 2, Mode: 0755}, &tar.Header{Name: "sdk/README.md", Typeflag: tar.TypeReg, Size: 2, Mode: 0644}, &tar.Header{Name: "sdk/LICENSE", Typeflag: tar.TypeReg, Size: 2, Mode: 0644})
	if e = extractSccacheSDK(data, dest); e != nil {
		t.Fatal(e)
	}
	if e = extractSccacheSDK(sdkArchive(t, &tar.Header{Name: "sdk/extra-executable", Typeflag: tar.TypeReg, Size: 1, Mode: 0755}), filepath.Join(t.TempDir(), "sccache")); e == nil {
		t.Fatal("unknown public archive executable admitted")
	}
}
