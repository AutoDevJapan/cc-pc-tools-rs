package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestReleaseSourceWindowRevalidatesAfterConsumption(t *testing.T) {
	fixture := mergedFixture(nil)
	for _, changed := range []bool{false, true} {
		reads, consumed := 0, 0
		read := func(ctx context.Context, endpoint string, out any) error {
			reads++
			if err := fixture(ctx, endpoint, out); err != nil {
				return err
			}
			if changed && consumed > 0 && strings.HasSuffix(endpoint, "/git/ref/heads/main") {
				return json.Unmarshal([]byte(`{"object":{"sha":"unapproved"}}`), out)
			}
			return nil
		}
		err := withMergedReleaseSource(context.Background(), read, func(r repository) error {
			if r.ID != 1285649124 || reads != 5 {
				t.Fatal("source consumed before full fixed receipt")
			}
			consumed++
			return nil
		})
		if consumed != 1 || (!changed && err != nil) || (changed && (err == nil || err.Error() != "current main differs from audited merge")) {
			t.Fatal("post-read verification not required")
		}
		if !changed && reads != 10 {
			t.Fatal("before/after fixed receipt reads incomplete")
		}
	}
}

func TestReleaseSourceTreeFailureStopsBeforeArchive(t *testing.T) {
	fixture := mergedFixture(nil)
	for _, treeUnavailable := range []bool{false, true} {
		archives, trees := 0, 0
		read := func(ctx context.Context, endpoint string, out any) error {
			if endpoint == mergedFixtureRepo+"git/trees/"+mergedTree+"?recursive=1" {
				trees++
				if treeUnavailable {
					return errors.New("opaque-sensitive-provider-error")
				}
				b, _ := json.Marshal(releaseTreeReply{SHA: mergedTree, Truncated: true})
				return json.Unmarshal(b, out)
			}
			return fixture(ctx, endpoint, out)
		}
		r, err := inspectReleaseSource(context.Background(), read, func(context.Context, string) ([]byte, error) {
			archives++
			return nil, errors.New("unexpected archive request")
		})
		want := "release tree envelope rejected"
		if treeUnavailable {
			want = "merged source release tree: GitHub read failed (unclassified)"
		}
		if err == nil || err.Error() != want || archives != 0 || trees != 1 || r.RepoID != 0 || r.ExecutionAuthorized {
			t.Fatalf("unexpected tree rejection: %v", err)
		}
	}
}

func TestReleaseSourceBadIdentityNeverConsumesSource(t *testing.T) {
	read := mergedFixture(func(m map[string]any) { m["repositories/1285649124"].(map[string]any)["private"] = false })
	consumed := 0
	err := withMergedReleaseSource(context.Background(), read, func(repository) error { consumed++; return nil })
	if err == nil || err.Error() != "merged repository identity rejected" || consumed != 0 {
		t.Fatal("unapproved identity crossed source boundary")
	}
}

func TestReleaseSourceDiagnosticsCannotFormatRawBytes(t *testing.T) {
	sensitive := "opaque-sensitive-source"
	values := []any{
		sealedReleaseContext{archive: []byte(sensitive), files: map[string]sealedReleaseFile{sensitive: {data: []byte(sensitive)}}},
		sealedReleaseFile{data: []byte(sensitive)},
		releaseTreeReply{Entries: []releaseTreeEntry{{Path: sensitive}}},
		releaseTreeEntry{Path: sensitive},
		releaseBlobReply{Content: sensitive},
		frozenReleaseBlob{data: []byte(sensitive)},
	}
	for _, v := range values {
		for _, pattern := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			s := fmt.Sprintf(pattern, v)
			if !strings.HasPrefix(s, "<") || strings.Contains(s, sensitive) || strings.Contains(s, "111 112 97 113") {
				t.Fatal("raw source escaped through formatting")
			}
		}
	}
}

func TestReleaseReadPauseStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := releaseReadPause(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled source read kept retrying")
	}
}
