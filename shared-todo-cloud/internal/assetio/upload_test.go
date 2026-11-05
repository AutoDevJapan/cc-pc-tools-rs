package assetio_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/assetio"
)

func hash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

func encode(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("fixture JSON encoding failed")
	}
	return raw
}

func write(t *testing.T, path string, raw []byte) {
	t.Helper()
	if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("fixture file creation failed")
	}
}

func payload(id int64) []byte {
	return []byte(fmt.Sprintf("{\r\n \"domain\":%q, \"todo_id\":%d, \"entries\":[{\"note\":\"架空の履歴\",\"large\":9007199254740993,\"Meta\":null,\"meta\":false},null],\"Unknown\":true\r\n}\r\n", assetio.HistoryDomain, id))
}

func archive(t *testing.T, home string, id int64, raw []byte) (map[string]any, string) {
	t.Helper()
	sha := hash(raw)
	file := fmt.Sprintf("%d-%s.json", id, sha)
	path := filepath.Join(home, "note-history-v1", file)
	write(t, path, raw)
	return map[string]any{"domain": assetio.HistoryDomain, "todo_id": id, "file": file, "sha256": sha, "bytes": len(raw), "entries": 2}, path
}

func requireError(t *testing.T, err error, want assetio.Error) {
	t.Helper()
	if !errors.Is(err, want) || err.Error() != want.Error() {
		t.Fatalf("expected fixed error %s", want)
	}
}

type upload struct {
	sha, path, kind string
	raw             []byte
}

func capture(t *testing.T, calls *[]upload) assetio.PutFunc {
	t.Helper()
	return func(ctx context.Context, sha, path, kind string, raw []byte) error {
		if ctx == nil || hash(raw) != sha {
			t.Error("callback digest or context differs")
		}
		*calls = append(*calls, upload{sha: sha, path: path, kind: kind, raw: bytes.Clone(raw)})
		return nil
	}
}

func TestUploadNewArchiveSpecsAndAllAliases(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	data := []byte{0xef, 0xbb, 0xbf, 'a', '\r', '\n', 0, 0xff}
	pathA, pathB := filepath.Join(home, "specs", "new.bin"), filepath.Join(home, "specs", "alias.bin")
	write(t, pathA, data)
	write(t, pathB, data)
	archiveBytes := payload(11)
	ref, archivePath := archive(t, home, 11, archiveBytes)
	legacy := map[string]any{"domain": assetio.HistoryDomain, "todo_id": 11, "file": "11-" + strings.Repeat("a", 64) + ".json", "sha256": strings.Repeat("a", 64), "bytes": 99, "entries": 1}
	old := map[string]any{"id": 11, "spec": "specs/missing-original.md", "spec_history": []any{map[string]any{"spec": "specs/missing-history.md", "body": "inline content is never a filename"}}, "note_history_archives": []any{legacy}, "progress": 10}
	before := map[int64]json.RawMessage{11: encode(t, old), 99: json.RawMessage(`untouched legacy row must not be decoded`)}
	upserts := []json.RawMessage{encode(t, map[string]any{
		"id": 11, "title": "fictional task", "note": "fictional new note", "progress": 30, "lastJob": nil, "lastjob": false,
		"spec": "specs/new.bin", "spec_history": []any{map[string]any{"spec": "specs/missing-history.md", "body": "changed inline content"}, map[string]any{"spec": "specs/alias.bin"}, pathA, map[string]any{"body": "not a path"}, map[string]any{"Spec": "not a path"}, false, nil},
		"note_history_archives": []any{legacy, ref},
	}), encode(t, map[string]any{"id": 12, "spec_history": "./specs/new.bin"})}
	beforeCopy := bytes.Clone(before[11])
	upsertCopy := []json.RawMessage{bytes.Clone(upserts[0]), bytes.Clone(upserts[1])}
	cache := []byte("fixture cache is untouched\r\n")
	write(t, filepath.Join(home, "todo.json"), cache)
	var calls []upload
	if err := assetio.UploadNew(context.Background(), before, upserts, home, capture(t, &calls)); err != nil {
		t.Fatal("new valid assets failed")
	}
	want := map[string]upload{
		archivePath: {sha: hash(archiveBytes), path: archivePath, kind: "note-history", raw: archiveBytes},
	}
	for _, path := range []string{"specs/new.bin", pathA, "specs/alias.bin", pathB, "./specs/new.bin"} {
		want[path] = upload{sha: hash(data), path: path, kind: "spec", raw: data}
	}
	if len(calls) != len(want) {
		t.Fatalf("uploaded aliases=%d, want %d", len(calls), len(want))
	}
	for _, call := range calls {
		if expected, ok := want[call.path]; !ok || !reflect.DeepEqual(call, expected) {
			t.Fatal("asset bytes, kind or path alias differs")
		}
		delete(want, call.path)
	}
	if len(want) != 0 {
		t.Fatal("alias missing")
	}
	if !bytes.Equal(beforeCopy, before[11]) || !reflect.DeepEqual(upsertCopy, upserts) {
		t.Fatal("input items were mutated")
	}
	current, err := os.ReadFile(filepath.Join(home, "todo.json"))
	if err != nil || !bytes.Equal(current, cache) {
		t.Fatal("local cache was changed")
	}
}

func TestExistingMissingOrLegacyReferencesAreNotReopened(t *testing.T) {
	for _, oldArchive := range []json.RawMessage{
		json.RawMessage(`[ {"sha":"unretrieved legacy","extra":9007199254740993}, "legacy entry" ]`),
		json.RawMessage(`{"legacy":true}`), json.RawMessage(`null`),
	} {
		old := map[string]any{"id": 1, "spec": "missing.md", "spec_history": []any{"also-missing.md", map[string]any{"spec": "third-missing.md"}}, "note_history_archives": oldArchive, "note": "old"}
		prior := encode(t, old)
		old["note"], old["progress"] = "new", 70
		// Re-encode to exercise semantic reference equality, not just raw bytes.
		if string(oldArchive) == `{"legacy":true}` {
			old["note_history_archives"] = json.RawMessage(`{ "legacy" : true }`)
		}
		home := filepath.Join(t.TempDir(), "home-does-not-exist")
		if err := assetio.UploadNew(context.Background(), map[int64]json.RawMessage{1: prior}, []json.RawMessage{encode(t, old)}, home, nil); err != nil {
			t.Fatal("unchanged legacy reference blocked an ordinary update")
		}
	}
	// A reference moved from current spec to spec_history is still an old path.
	before := map[int64]json.RawMessage{1: json.RawMessage(`{"id":1,"spec":"missing.md"}`)}
	if err := assetio.UploadNew(context.Background(), before, []json.RawMessage{json.RawMessage(`{"id":1,"spec_history":[{"spec":"missing.md","body":"edited"}]} `)}, "", nil); err != nil {
		t.Fatal("moving an unchanged spec reopened it")
	}
}

func TestNewArchiveWithUnchangedMissingLegacyEntry(t *testing.T) {
	home := t.TempDir()
	ref, _ := archive(t, home, 1, payload(1))
	legacy := json.RawMessage(`{"sha":"unretrieved","unknown":null}`)
	before := map[int64]json.RawMessage{1: encode(t, map[string]any{"id": 1, "note_history_archives": []any{legacy}})}
	upserts := []json.RawMessage{encode(t, map[string]any{"id": 1, "note_history_archives": []any{json.RawMessage(`{ "unknown":null, "sha":"unretrieved" }`), ref}})}
	var calls []upload
	if err := assetio.UploadNew(context.Background(), before, upserts, home, capture(t, &calls)); err != nil || len(calls) != 1 {
		t.Fatal("new archive failed beside a missing legacy reference")
	}
}

func TestNewOrChangedOwnedReferenceMustExist(t *testing.T) {
	home := t.TempDir()
	before := map[int64]json.RawMessage{1: json.RawMessage(`{"id":1,"spec":"old-missing.md"}`)}
	upserts := []json.RawMessage{json.RawMessage(`{"id":1,"spec":"new-missing.md"}`)}
	var calls []upload
	requireError(t, assetio.UploadNew(context.Background(), before, upserts, home, capture(t, &calls)), assetio.ErrFileMissing)
	if len(calls) != 0 {
		t.Fatal("missing owned asset was uploaded")
	}
	ref, path := archive(t, home, 2, payload(2))
	if os.Remove(path) != nil {
		t.Fatal("fixture removal failed")
	}
	requireError(t, assetio.UploadNew(context.Background(), nil, []json.RawMessage{encode(t, map[string]any{"id": 2, "note_history_archives": []any{ref}})}, home, capture(t, &calls)), assetio.ErrFileMissing)
}

func TestArchiveReferenceAndPayloadValidation(t *testing.T) {
	for _, name := range []string{"domain", "todo_id", "file", "sha256", "bytes-zero", "bytes-limit", "bytes-mismatch", "entries-zero", "entries-mismatch", "corrupt", "payload-domain", "payload-id", "payload-entries", "duplicate-key", "trailing-json", "invalid-utf8", "copied-to-other-task"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			data := payload(1)
			want := assetio.ErrArchiveReference
			switch name {
			case "payload-domain":
				data = bytes.Replace(data, []byte(assetio.HistoryDomain), []byte("other-domain"), 1)
				want = assetio.ErrArchiveDocument
			case "payload-id":
				data = bytes.Replace(data, []byte(`"todo_id":1`), []byte(`"todo_id":2`), 1)
				want = assetio.ErrArchiveDocument
			case "payload-entries":
				data = []byte(`{"domain":"` + assetio.HistoryDomain + `","todo_id":1,"entries":null}`)
				want = assetio.ErrArchiveDocument
			case "duplicate-key":
				data = []byte(`{"domain":"` + assetio.HistoryDomain + `","todo_id":1,"entries":[{"x":1,"x":2},null]}`)
				want = assetio.ErrArchiveDocument
			case "trailing-json":
				data = append(data, []byte(` {}`)...)
				want = assetio.ErrArchiveDocument
			case "invalid-utf8":
				data = []byte{'{', '"', 0xff, '"', ':', '1', '}'}
				want = assetio.ErrArchiveDocument
			}
			ref, path := archive(t, home, 1, data)
			id := int64(1)
			switch name {
			case "domain":
				ref["domain"] = "other"
			case "todo_id":
				ref["todo_id"] = 2
			case "file":
				ref["file"] = "../outside.json"
			case "sha256":
				ref["sha256"] = strings.ToUpper(ref["sha256"].(string))
			case "bytes-zero":
				ref["bytes"] = 0
			case "bytes-limit":
				ref["bytes"] = assetio.MaxAssetBytes + 1
			case "bytes-mismatch":
				ref["bytes"] = len(data) - 1
				want = assetio.ErrArchiveDigest
			case "entries-zero":
				ref["entries"] = 0
			case "entries-mismatch":
				ref["entries"] = 3
				want = assetio.ErrArchiveDocument
			case "corrupt":
				write(t, path, []byte("corrupted fixture"))
				want = assetio.ErrArchiveDigest
			case "copied-to-other-task":
				id = 2
			}
			var calls []upload
			upserts := []json.RawMessage{encode(t, map[string]any{"id": id, "note_history_archives": []any{ref}})}
			requireError(t, assetio.UploadNew(context.Background(), nil, upserts, home, capture(t, &calls)), want)
			if len(calls) != 0 {
				t.Fatal("invalid archive was uploaded")
			}
		})
	}
}

func TestExternalReferencesArePreservedWithoutOpeningHome(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "absent-home")
	outside := filepath.Join(base, "outside.md")
	write(t, outside, []byte("outside fixture stays unread"))
	upsert := encode(t, map[string]any{"id": 1, "spec": outside, "spec_history": []any{"../missing-outside.md", filepath.Join(base, "missing-outside.md"), "https://example.invalid/spec.md", `\outside\missing.md`, `C:drive-relative.md`}})
	copy := bytes.Clone(upsert)
	if err := assetio.UploadNew(context.Background(), nil, []json.RawMessage{upsert}, home, nil); err != nil {
		t.Fatal("external reference was accessed or rejected")
	}
	if !bytes.Equal(copy, upsert) {
		t.Fatal("external references were rewritten")
	}
}

func TestProtectedControlFilesAndDirectoriesAreNotAssets(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "client-projects.txt"), []byte("excluded fixture must stay unread"))
	write(t, filepath.Join(home, "todo.json"), []byte("cache fixture"))
	write(t, filepath.Join(home, "todo.lock"), nil)
	for _, path := range []string{"client-projects.txt", "CLIENT-PROJECTS.TXT. ", "todo.json", "todo.lock"} {
		var calls []upload
		upsert := encode(t, map[string]any{"id": 1, "spec": path})
		requireError(t, assetio.UploadNew(context.Background(), nil, []json.RawMessage{upsert}, home, capture(t, &calls)), assetio.ErrPathUnverified)
		if len(calls) != 0 {
			t.Fatal("protected file was uploaded")
		}
	}
	if os.Mkdir(filepath.Join(home, "directory"), 0700) != nil {
		t.Fatal("directory fixture failed")
	}
	requireError(t, assetio.UploadNew(context.Background(), nil, []json.RawMessage{json.RawMessage(`{"id":1,"spec":"directory"}`)}, home, func(context.Context, string, string, string, []byte) error { return nil }), assetio.ErrFileUnavailable)
	// A legitimate child beginning with '..' must remain inside the home.
	write(t, filepath.Join(home, "..cache", "spec.md"), []byte("owned fixture"))
	var calls []upload
	if err := assetio.UploadNew(context.Background(), nil, []json.RawMessage{json.RawMessage(`{"id":1,"spec":"..cache/spec.md"}`)}, home, capture(t, &calls)); err != nil || len(calls) != 2 {
		t.Fatal("legitimate dotted child was mistaken for an escape")
	}
}

func TestSymbolicLinkEscapeIsRejected(t *testing.T) {
	base := t.TempDir()
	home, external := filepath.Join(base, "home"), filepath.Join(base, "external")
	if os.Mkdir(home, 0700) != nil {
		t.Fatal("home fixture failed")
	}
	ref, outsidePath := archive(t, external, 1, payload(1))
	if err := os.Symlink(filepath.Dir(outsidePath), filepath.Join(home, "note-history-v1")); err != nil {
		t.Skip("symbolic link creation unavailable")
	}
	var calls []upload
	upsert := encode(t, map[string]any{"id": 1, "note_history_archives": []any{ref}})
	requireError(t, assetio.UploadNew(context.Background(), nil, []json.RawMessage{upsert}, home, capture(t, &calls)), assetio.ErrPathUnverified)
	if len(calls) != 0 {
		t.Fatal("archive directory link was followed")
	}
}

func TestCallbackFailureIsSanitizedAndRetryIsIdempotent(t *testing.T) {
	home := t.TempDir()
	data := []byte("identical bytes at two fixture paths\r\n")
	write(t, filepath.Join(home, "a.md"), data)
	write(t, filepath.Join(home, "b.md"), data)
	upserts := []json.RawMessage{json.RawMessage(`{"id":1,"spec":"a.md","spec_history":[{"spec":"b.md"}]}`)}
	before := map[int64]json.RawMessage{1: json.RawMessage(`{"id":1,"progress":10}`)}
	beforeCopy, upsertCopy := bytes.Clone(before[1]), bytes.Clone(upserts[0])
	remote := map[string]upload{}
	attempts := 0
	sensitive := errors.New("fixture-only-private-text-and-auth-marker")
	put := func(ctx context.Context, sha, path, kind string, raw []byte) error {
		attempts++
		if attempts == 3 {
			return sensitive
		}
		got := upload{sha: sha, path: path, kind: kind, raw: bytes.Clone(raw)}
		if prior, exists := remote[path]; exists && !reflect.DeepEqual(prior, got) {
			return errors.New("fixture alias changed")
		}
		remote[path] = got
		return nil
	}
	err := assetio.UploadNew(context.Background(), before, upserts, home, put)
	requireError(t, err, assetio.ErrUpload)
	if strings.Contains(fmt.Sprintf("%+v %#v", err, err), sensitive.Error()) || errors.Is(err, sensitive) {
		t.Fatal("callback failure leaked through returned error")
	}
	if len(remote) != 2 {
		t.Fatal("fixture did not fail after a partial upload")
	}
	if err := assetio.UploadNew(context.Background(), before, upserts, home, put); err != nil {
		t.Fatal("idempotent retry failed")
	}
	if len(remote) != 4 || attempts != 7 {
		t.Fatal("retry lost a same-digest path alias")
	}
	if !bytes.Equal(beforeCopy, before[1]) || !bytes.Equal(upsertCopy, upserts[0]) {
		t.Fatal("partial failure mutated input or cache values")
	}
}

func TestSameValidatedBytesAreUsedForEveryAlias(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "spec.md")
	original := []byte("original fixture\r\n\x00\xff")
	write(t, path, original)
	calls := 0
	put := func(_ context.Context, sha, _, kind string, raw []byte) error {
		calls++
		if sha != hash(original) || kind != "spec" || !bytes.Equal(raw, original) {
			t.Error("validated bytes were replaced by a later file read")
		}
		if calls == 1 {
			write(t, path, []byte("changed after first alias PUT"))
		}
		return nil
	}
	if err := assetio.UploadNew(context.Background(), nil, []json.RawMessage{json.RawMessage(`{"id":1,"spec":"spec.md"}`)}, home, put); err != nil || calls != 2 {
		t.Fatal("same-file aliases failed")
	}
}

func TestSizeBoundsAndCancellation(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "large.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal("large fixture create failed")
	}
	err = f.Truncate(assetio.MaxAssetBytes + 1)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatal("large fixture truncate failed")
	}
	put := func(context.Context, string, string, string, []byte) error {
		t.Error("invalid asset reached callback")
		return nil
	}
	requireError(t, assetio.UploadNew(context.Background(), nil, []json.RawMessage{json.RawMessage(`{"id":1,"spec":"large.bin"}`)}, home, put), assetio.ErrAssetTooLarge)
	tooLarge := bytes.Repeat([]byte{' '}, assetio.MaxDocumentBytes+1)
	requireError(t, assetio.UploadNew(context.Background(), nil, []json.RawMessage{tooLarge}, home, put), assetio.ErrDocumentTooLarge)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requireError(t, assetio.UploadNew(ctx, nil, nil, home, put), assetio.ErrCancelled)
	write(t, filepath.Join(home, "cancel.md"), []byte("cancel fixture"))
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err = assetio.UploadNew(ctx, nil, []json.RawMessage{json.RawMessage(`{"id":1,"spec":"cancel.md"}`)}, home, func(context.Context, string, string, string, []byte) error { calls++; cancel(); return nil })
	requireError(t, err, assetio.ErrCancelled)
	if calls != 1 {
		t.Fatal("cancellation did not stop the next alias")
	}
}

func TestInvalidItemsAreRejectedWithoutLeakingTheirContent(t *testing.T) {
	home := t.TempDir()
	for _, upserts := range [][]json.RawMessage{
		{json.RawMessage(`{"id":0,"title":"fixture private title"}`)},
		{json.RawMessage(`{"id":1,"id":2}`)},
		{json.RawMessage(`{"id":1}`), json.RawMessage(`{"id":1}`)},
		{json.RawMessage(`{"id":1} trailing-private-text`)},
	} {
		requireError(t, assetio.UploadNew(context.Background(), nil, upserts, home, nil), assetio.ErrInvalidInput)
	}
}
