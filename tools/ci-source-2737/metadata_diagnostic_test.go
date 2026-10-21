package sourcepacket

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutMetadataDiagnosticsKeepBoundsAndRejectPolicy(t *testing.T) {
	for _, code := range []string{"file_limit", "policy"} {
		t.Run(code, func(t *testing.T) {
			p, root := fixture(t)
			dest := filepath.Join(root, "guest")
			if _, err := Materialize(context.Background(), p, p.Pin, dest); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest); err != nil {
				t.Fatal("positive proof rejected")
			}
			name := filepath.Join(dest, ".git", "objects", "synthetic-sensitive-name")
			if code == "policy" {
				name = filepath.Join(dest, ".git", "objects", "info", "alternates")
				if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if code == "file_limit" {
				err = f.Truncate(128<<20 + 1)
			}
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatal("sparse metadata fixture failed")
			}
			_, err = VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dest)
			d := MetadataFailure(err)
			if err == nil || err.Error() != "checkout proof rejected at metadata" || d == nil || d.Code != code || d.Kind != "objects" || d.Entries == 0 {
				t.Fatalf("missing bounded metadata diagnostic: %+v", d)
			}
			if code == "file_limit" && d.FileBytes != 128<<20+1 {
				t.Fatal("observed size not retained")
			}
			b, err := json.Marshal(d)
			if err != nil || strings.Contains(string(b), "sensitive") || strings.Contains(string(b), dest) || strings.Contains(string(b), "alternates") {
				t.Fatal("metadata content or path escaped")
			}
		})
	}
}

func TestMetadataDiagnosticWhitelistAndDetachedCopy(t *testing.T) {
	want := MetadataDiagnostic{Code: "file_limit", Kind: "objects", Entries: 4, FileBytes: 128<<20 + 1, TotalBytes: 100}
	err := &metadataProofError{diagnostic: want}
	d := MetadataFailure(err)
	if d == nil || *d != want {
		t.Fatal("valid observation rejected")
	}
	d.Code = "modified"
	if MetadataFailure(err).Code != want.Code {
		t.Fatal("returned observation aliases error")
	}
	if MetadataFailure(errors.New("private-path-and-contents")) != nil {
		t.Fatal("raw error admitted")
	}
	for _, bad := range []MetadataDiagnostic{
		{Code: "private-path", Kind: "objects"},
		{Code: "read", Kind: "private-path"},
		{Code: "read", Kind: "index", Entries: -1},
		{Code: "read", Kind: "index", FileBytes: -1},
		{Code: "read", Kind: "index", TotalBytes: 640<<20 + 1},
		{Code: "read", Kind: "index", Entries: maxFiles + 2},
	} {
		if bad.Valid() || MetadataFailure(&metadataProofError{bad}) != nil {
			t.Fatal("invalid observation admitted")
		}
	}
}
