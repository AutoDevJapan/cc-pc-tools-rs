package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// A public Python slim image pinned by digest, for the package tool fixture.
const fixturePackageBase = "python@sha256:a6e34c598f2467ed0e9a8d349809fcd8b5c603269512df273a0bb1784edc11b1"

// imageEntryOwners reads uid:gid for every entry of every layer of a Docker
// archive image; later layers override earlier ones, as when the image is run.
func imageEntryOwners(t *testing.T, archive []byte) map[string]string {
	t.Helper()
	files := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			files[h.Name] = b
		}
	}
	var manifest []struct{ Layers []string }
	if json.Unmarshal(files["manifest.json"], &manifest) != nil || len(manifest) != 1 {
		t.Fatal("image manifest unreadable")
	}
	owners := map[string]string{}
	for _, name := range manifest[0].Layers {
		var r io.Reader = bytes.NewReader(files[name])
		if b := files[name]; len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
			zr, err := gzip.NewReader(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			r = zr
		}
		lt := tar.NewReader(r)
		for {
			h, err := lt.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			owners[strings.TrimSuffix(strings.TrimPrefix(h.Name, "./"), "/")] = fmt.Sprintf("%d:%d", h.Uid, h.Gid)
		}
	}
	return owners
}
