package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	home, out, archive string
	raw, spec          []byte
	document           map[string]any
}

func writeFixture(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal("fixture directory failed")
	}
	if os.WriteFile(path, b, 0600) != nil {
		t.Fatal("fixture write failed")
	}
}

func (f *fixture) save(t *testing.T) {
	t.Helper()
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if encoder.Encode(f.document) != nil {
		t.Fatal("fixture encoding failed")
	}
	raw := encoded.Bytes()
	f.raw = append([]byte(" \r\n"), bytes.ReplaceAll(raw, []byte("\n"), []byte("\r\n"))...)
	f.raw = append(f.raw, []byte("\r\n ")...)
	writeFixture(t, filepath.Join(f.home, "todo.json"), f.raw)
}

func newFixture(t *testing.T, large bool) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{home: filepath.Join(base, "home"), out: filepath.Join(base, "export"), spec: []byte{0xef, 0xbb, 0xbf, 'a', '\r', '\n', 0, 0xff}}
	spec := filepath.Join(f.home, "specs", "仕様 #+&%.md")
	specAlias := filepath.Join(f.home, "specs", "同じ内容.md")
	writeFixture(t, spec, f.spec)
	writeFixture(t, specAlias, f.spec)
	writeFixture(t, filepath.Join(base, "outside.md"), []byte("external fixture must stay unread"))
	writeFixture(t, filepath.Join(f.home, "client-projects.txt"), []byte("excluded fixture must stay unread"))
	writeFixture(t, filepath.Join(f.home, "todo.lock"), nil)
	history := []byte("{\r\n  \"domain\":\"" + HistoryDomain + "\",\"todo_id\":1,\"entries\":[{\"note\":\"架空の履歴\",\"updated\":\"2026-01-01\",\"unrecognized\":9007199254740993}]\r\n}")
	hash := digest(history)
	file := "1-" + hash + ".json"
	f.archive = filepath.Join(f.home, "note-history-v1", file)
	writeFixture(t, f.archive, history)
	ref := map[string]any{"domain": HistoryDomain, "todo_id": 1, "file": file, "sha256": hash, "bytes": len(history), "entries": 1}
	item := map[string]any{"id": 1, "title": "架空のテスト項目", "note": "private-fiction-note", "status": "todo", "lastJob": json.Number("9007199254740993"), "lastjob": "different", "LASTJOB": nil,
		"note_history_archives": []any{ref}, "spec": spec, "spec_history": []any{map[string]any{"spec": specAlias, "body": "inline body is never a path"}, spec, "specs/missing.md", filepath.Join(base, "outside.md"), filepath.Join(base, "absent.md"), filepath.Join(f.home, "client-projects.txt"), nil, false},
		"wait_on": []any{2, 3}, "worker_receipt": map[string]any{"job": nil, "generation": 7}, "decimal": json.Number("1.2300e+02")}
	for i := range 576 {
		item[fmt.Sprintf("unknown_%03d", i)] = map[string]any{"upper": nil, "Lower": []any{json.Number("9007199254740995"), false, "value"}}
	}
	if large {
		item["note"] = strings.Repeat("架", ChunkBytes/3+2000)
	}
	items := []any{item}
	for i, status := range []any{"doing", "done", "wait", "block", "recovery_unverified", "future-status", nil, 42, false} {
		items = append(items, map[string]any{"id": i + 2, "status": status, "spec": false})
	}
	items = append(items, map[string]any{"id": 11, "spec": nil})
	f.document = map[string]any{"items": items, "next_id": 100, "projects": []any{"fiction"}, "unknownRoot": map[string]any{"VeryLarge": json.Number("9007199254740999"), "value": nil}, "UnknownRoot": true}
	f.save(t)
	return f
}

func mustExport(t *testing.T, f *fixture) Summary {
	t.Helper()
	summary, err := Export(context.Background(), f.home, f.out)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return summary
}

func readManifest(t *testing.T, dir string) Manifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var m Manifest
	if err != nil || json.Unmarshal(b, &m) != nil {
		t.Fatal("manifest read failed")
	}
	return m
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	actual, _ := ErrorCode(err)
	if err == nil || actual != code {
		t.Fatalf("expected code %s, got %v", code, err)
	}
}

func TestExportRetainsExactRootHistoryAndSpecificationBytes(t *testing.T) {
	f := newFixture(t, false)
	summary := mustExport(t, f)
	raw, err := os.ReadFile(filepath.Join(f.out, "snapshot.json"))
	if err != nil || !bytes.Equal(raw, f.raw) || summary.RootSHA256 != digest(f.raw) || summary.RootBytes != int64(len(f.raw)) {
		t.Fatal("root bytes or digest changed")
	}
	if summary.Items != 11 || summary.ItemFields < 576 || summary.RootFields != 5 || summary.StatusKinds != 11 || summary.ArchiveReferences != 1 || summary.ArchiveEntries != 1 || summary.SpecImported != 3 || summary.SpecMissing != 1 || summary.SpecUnverified != 3 || summary.SpecReferences != 7 || summary.AssetFiles != 2 || summary.AssetAliases != 3 {
		t.Fatalf("unexpected aggregate counts: %+v", summary)
	}
	m := readManifest(t, f.out)
	if m.Root.File != "snapshot.json" || len(m.References) != 8 {
		t.Fatal("manifest inventory incomplete")
	}
	for _, ref := range m.References {
		switch ref.Status {
		case "imported":
			if ref.Exists == nil || !*ref.Exists || !validDigest(ref.SHA256) {
				t.Fatal("imported reference is not verified")
			}
		case "missing":
			if ref.Exists == nil || *ref.Exists {
				t.Fatal("missing reference existence differs")
			}
		case "unverified":
			if ref.Exists != nil || ref.SHA256 != "" {
				t.Fatal("external reference was probed or populated")
			}
		}
	}
	for _, a := range m.Assets {
		b, err := os.ReadFile(filepath.Join(f.out, filepath.FromSlash(a.File.File)))
		if err != nil || digest(b) != a.SHA256 || int64(len(b)) != a.Bytes {
			t.Fatal("asset differs from manifest")
		}
		if a.Kind == "spec" && !bytes.Equal(b, f.spec) {
			t.Fatal("specification bytes were transcoded")
		}
	}
	source, _ := os.ReadFile(filepath.Join(f.home, "todo.json"))
	lock, _ := os.ReadFile(filepath.Join(f.home, "todo.lock"))
	if !bytes.Equal(source, f.raw) || len(lock) != 0 {
		t.Fatal("source was edited")
	}
	p, err := loadExport(context.Background(), f.out)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	p.root.Close()
	// Acquiring the legacy byte again proves export released it.
	l, err := os.OpenFile(filepath.Join(f.home, "todo.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("lock reopen failed")
	}
	defer l.Close()
	if lockFile(l) != nil {
		t.Fatal("export retained source lock")
	}
	unlockFile(l)
}

func TestExportRejectsCorruptArchiveReferencesAndDocuments(t *testing.T) {
	for _, name := range []string{"domain", "todo_id", "file", "sha256", "bytes", "entries", "missing", "corrupt", "payload_domain", "payload_id", "payload_entries"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, false)
			ref := f.document["items"].([]any)[0].(map[string]any)["note_history_archives"].([]any)[0].(map[string]any)
			switch name {
			case "domain":
				ref[name] = "wrong-domain"
			case "todo_id":
				ref[name] = 2
			case "file":
				ref[name] = "../escape.json"
			case "sha256":
				ref[name] = strings.Repeat("A", 64)
			case "bytes":
				ref[name] = 1
			case "entries":
				ref[name] = 2
			case "missing":
				if os.Remove(f.archive) != nil {
					t.Fatal("fixture removal failed")
				}
			case "corrupt":
				writeFixture(t, f.archive, []byte("corrupt"))
			default:
				payload := map[string]any{"domain": HistoryDomain, "todo_id": 1, "entries": []any{map[string]any{"note": "fixture"}}}
				switch name {
				case "payload_domain":
					payload["domain"] = "other"
				case "payload_id":
					payload["todo_id"] = 2
				case "payload_entries":
					payload["entries"] = []any{}
				}
				b, _ := json.Marshal(payload)
				hash := digest(b)
				ref["file"], ref["sha256"], ref["bytes"] = "1-"+hash+".json", hash, len(b)
				writeFixture(t, filepath.Join(f.home, "note-history-v1", ref["file"].(string)), b)
			}
			f.save(t)
			if _, err := Export(context.Background(), f.home, f.out); err == nil {
				t.Fatal("corrupt history accepted")
			}
			if _, err := os.Stat(f.out); !os.IsNotExist(err) {
				t.Fatal("failed export published a package")
			}
			stages, _ := filepath.Glob(filepath.Join(filepath.Dir(f.out), ".todo-export-*"))
			if len(stages) != 0 {
				t.Fatal("failed export retained staging files")
			}
		})
	}
}

func TestExportRefusesMalformedRootBeforePublishing(t *testing.T) {
	for _, raw := range []string{
		`{"items":null,"next_id":1}`,
		`{"items":[]}`,
		`{"items":[],"items":[],"next_id":1}`,
		`{"items":[{"id":1,"nested":{"x":1,"x":2}}],"next_id":2}`,
		`{"items":[{"id":1},{"id":1}],"next_id":2}`,
		`{"items":[{"id":1}],"next_id":1}`,
		`{"items":[],"next_id":1} true`,
	} {
		f := newFixture(t, false)
		writeFixture(t, filepath.Join(f.home, "todo.json"), []byte(raw))
		if _, err := Export(context.Background(), f.home, f.out); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}

func TestExportDoesNotCreateSourceLockOrOverwriteOutput(t *testing.T) {
	f := newFixture(t, false)
	if os.Remove(filepath.Join(f.home, "todo.lock")) != nil {
		t.Fatal("fixture removal failed")
	}
	_, err := Export(context.Background(), f.home, f.out)
	requireCode(t, err, "source_lock_unavailable")
	if _, err := os.Stat(filepath.Join(f.home, "todo.lock")); !os.IsNotExist(err) {
		t.Fatal("missing source lock was created")
	}
	writeFixture(t, filepath.Join(f.home, "todo.lock"), nil)
	writeFixture(t, filepath.Join(f.out, "sentinel"), []byte("keep"))
	_, err = Export(context.Background(), f.home, f.out)
	requireCode(t, err, "export_directory_exists")
	_, err = Export(context.Background(), f.home, filepath.Join(f.home, "new-export"))
	requireCode(t, err, "export_directory_inside_source")
}

func TestExportLeavesLinkedSpecificationsUnverified(t *testing.T) {
	f := newFixture(t, false)
	link := filepath.Join(f.home, "specs", "link.md")
	if err := os.Symlink(filepath.Join(filepath.Dir(f.home), "outside.md"), link); err != nil {
		t.Skip("symbolic link creation unavailable")
	}
	f.document["items"].([]any)[0].(map[string]any)["spec"] = link
	f.save(t)
	mustExport(t, f)
	m := readManifest(t, f.out)
	for _, ref := range m.References {
		if ref.Path == link && (ref.Status != "unverified" || ref.Exists != nil) {
			t.Fatal("linked path was read")
		}
	}
}

func TestExportCancellationAndRootSizeLimit(t *testing.T) {
	f := newFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Export(ctx, f.home, f.out)
	requireCode(t, err, "cancelled")
	file, err := os.OpenFile(filepath.Join(f.home, "todo.json"), os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("fixture open failed")
	}
	err = file.Truncate(64<<20 + 1)
	file.Close()
	if err != nil {
		t.Fatal("fixture truncate failed")
	}
	_, err = Export(context.Background(), f.home, f.out)
	requireCode(t, err, "file_too_large")
}
