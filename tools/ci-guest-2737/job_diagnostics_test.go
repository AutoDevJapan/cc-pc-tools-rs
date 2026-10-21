package main

import (
	"errors"
	"strings"
	"testing"

	sourcepacket "cc/ci-source-2737"
)

func TestHookDiagnosticsRejectUnknownValuesAndNeverExposeProofErrors(t *testing.T) {
	for _, d := range []*hookDiagnostic{nil, {Version: 2, Stage: "start", Code: "context_sha"}, {Version: 1, Stage: "other", Code: "context_sha"}, {Version: 1, Stage: "complete", Code: "/private/SENSITIVE"}} {
		if validHookDiagnostic(d) {
			t.Fatal("unbounded diagnostic admitted")
		}
	}
	if got := sourceFailureCode(errors.New("checkout proof rejected at physical-blobs")); got != "source_physical-blobs" {
		t.Fatal(got)
	}
	if got := sourceFailureCode(errors.New("/private/SENSITIVE")); got != "source_unknown" || strings.Contains(got, "SENSITIVE") {
		t.Fatal("raw proof error escaped")
	}
}

func TestHookMetadataDiagnosticsAreClosedAndSourceOnly(t *testing.T) {
	d := &hookDiagnostic{Version: 1, Stage: "complete", Code: "source_metadata", Metadata: &sourcepacket.MetadataDiagnostic{Code: "file_limit", Kind: "objects", Entries: 3, FileBytes: 128<<20 + 1}}
	if !validHookDiagnostic(d) {
		t.Fatal("valid numeric metadata diagnostic rejected")
	}
	d.Code = "source_index"
	if validHookDiagnostic(d) {
		t.Fatal("metadata on unrelated failure admitted")
	}
	d.Code = "source_metadata"
	d.Metadata.Kind = "/private/SENSITIVE"
	if validHookDiagnostic(d) {
		t.Fatal("unbounded metadata kind admitted")
	}
}
