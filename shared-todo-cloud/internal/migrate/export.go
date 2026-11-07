package migrate

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"autodevjapan/shared-todo-cloud/internal/transport"
)

type exporter struct {
	home    string
	stage   string
	source  *os.Root
	m       Manifest
	aliases map[string]Asset
	blobs   map[string]int64
}

// Export requires an existing todo.lock and a new output directory. The local
// byte lock covers snapshot and asset capture only; no network operation holds it.
func Export(ctx context.Context, home, out string) (Summary, error) {
	if err := cancelled(ctx); err != nil {
		return Summary{}, err
	}
	if strings.TrimSpace(home) == "" {
		return Summary{}, failure("invalid_home")
	}
	if strings.TrimSpace(out) == "" {
		return Summary{}, failure("invalid_export_directory")
	}
	home, err := filepath.Abs(home)
	if err != nil || forbiddenPath(home) {
		return Summary{}, failure("invalid_home")
	}
	out, err = filepath.Abs(out)
	if err != nil || forbiddenPath(out) {
		return Summary{}, failure("invalid_export_directory")
	}
	if _, inside := within(home, out); inside || home == out {
		return Summary{}, failure("export_directory_inside_source")
	}
	if _, err := os.Lstat(out); err == nil {
		return Summary{}, failure("export_directory_exists")
	} else if !os.IsNotExist(err) {
		return Summary{}, failure("export_directory_unavailable")
	}
	source, err := os.OpenRoot(home)
	if err != nil {
		return Summary{}, failure("source_unavailable")
	}
	defer source.Close()
	if err := ordinary(source, "todo.lock"); err != nil {
		return Summary{}, failure("source_lock_unavailable")
	}
	lock, err := source.OpenFile("todo.lock", os.O_RDWR, 0600)
	if err != nil {
		return Summary{}, failure("source_lock_unavailable")
	}
	defer lock.Close()
	if lockFile(lock) != nil {
		return Summary{}, failure("source_lock_busy")
	}
	defer unlockFile(lock)
	raw, err := readUnder(source, "todo.json", transport.MaxDocumentBytes)
	if err != nil {
		return Summary{}, err
	}
	rows, counts, err := parseDocument(raw)
	if err != nil {
		return Summary{}, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(out), ".todo-export-")
	if err != nil {
		return Summary{}, failure("export_directory_unavailable")
	}
	// Only the newly created private staging directory is eligible for cleanup.
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0700); err != nil {
		return Summary{}, failure("export_write_failed")
	}
	if err := os.Mkdir(filepath.Join(stage, "assets"), 0700); err != nil {
		return Summary{}, failure("export_write_failed")
	}
	x := exporter{home: home, stage: stage, source: source, aliases: map[string]Asset{}, blobs: map[string]int64{},
		m: Manifest{Format: Format, CreatedAt: time.Now().UTC(), Counts: counts,
			Root:   File{File: "snapshot.json", SHA256: digest(raw), Bytes: int64(len(raw)), OriginalPath: filepath.Join(home, "todo.json")},
			Assets: []Asset{}, References: []Reference{}}}
	if err := writeNew(filepath.Join(stage, "snapshot.json"), raw); err != nil {
		return Summary{}, err
	}
	for _, row := range rows {
		if err := cancelled(ctx); err != nil {
			return Summary{}, err
		}
		if err := x.histories(row); err != nil {
			return Summary{}, err
		}
		for _, spec := range specPaths(row) {
			if err := x.spec(row.id, spec.field, spec.path); err != nil {
				return Summary{}, err
			}
		}
	}
	for _, a := range x.aliases {
		x.m.Assets = append(x.m.Assets, a)
	}
	sort.Slice(x.m.Assets, func(i, j int) bool { return x.m.Assets[i].OriginalPath < x.m.Assets[j].OriginalPath })
	x.m.Counts.AssetFiles, x.m.Counts.AssetAliases = len(x.blobs), len(x.m.Assets)
	manifest, err := json.MarshalIndent(x.m, "", "  ")
	if err != nil || len(manifest) > maxManifestBytes {
		return Summary{}, failure("manifest_too_large")
	}
	if err := writeNew(filepath.Join(stage, "manifest.json"), append(manifest, '\n')); err != nil {
		return Summary{}, err
	}
	if err := cancelled(ctx); err != nil {
		return Summary{}, err
	}
	if err := publishExport(ctx, stage, out); err != nil {
		return Summary{}, err
	}
	return summarize(x.m), nil
}

func publishExport(ctx context.Context, stage, out string) error {
	for attempt := 0; ; attempt++ {
		err := os.Rename(stage, out)
		if err == nil {
			return nil
		}
		if attempt == 6 || !retryableRename(err) {
			return failure("export_publish_failed")
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			return failure("export_publish_failed")
		}
		// A short-lived Windows reader may have the new directory open without
		// delete sharing. Retry the same publication, never replace an output.
		timer := time.NewTimer((20 * time.Millisecond) << attempt)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failure("cancelled")
		case <-timer.C:
		}
	}
}

func (x *exporter) asset(raw []byte, path, kind string, entries uint64) (Asset, error) {
	hash := digest(raw)
	a := Asset{File: File{File: "assets/" + hash, SHA256: hash, Bytes: int64(len(raw)), OriginalPath: path}, Kind: kind, Entries: entries}
	if previous, ok := x.aliases[path]; ok {
		if previous != a {
			return Asset{}, failure("asset_alias_conflict")
		}
		return a, nil
	}
	if _, ok := x.blobs[hash]; !ok {
		if err := writeNew(filepath.Join(x.stage, filepath.FromSlash(a.File.File)), raw); err != nil {
			return Asset{}, err
		}
		x.blobs[hash] = a.Bytes
		x.m.Counts.AssetBytes += a.Bytes
	}
	x.aliases[path] = a
	return a, nil
}

func archiveReferences(row row, field string) ([]json.RawMessage, error) {
	raw, present := row.fields[field]
	if !present {
		return nil, nil
	}
	var refs []json.RawMessage
	if json.Unmarshal(raw, &refs) != nil || refs == nil {
		return nil, failure("invalid_archive_references")
	}
	return refs, nil
}

func validateArchiveRef(id int64, raw json.RawMessage) (file, hash string, size int64, entries uint64, err error) {
	r, err := object(raw)
	if err != nil {
		return "", "", 0, 0, err
	}
	domain, _ := stringValue(r["domain"])
	refID, validID := intValue(r["todo_id"])
	file, _ = stringValue(r["file"])
	hash, _ = stringValue(r["sha256"])
	size, validSize := intValue(r["bytes"])
	entries, validCount := uintValue(r["entries"])
	if domain != HistoryDomain || !validID || refID != id || !validDigest(hash) || file != strconv.FormatInt(id, 10)+"-"+hash+".json" || !validSize || size <= 0 || size > MaxAssetBytes || !validCount || entries == 0 {
		return "", "", 0, 0, failure("invalid_archive_reference")
	}
	return file, hash, size, entries, nil
}

func validateArchive(raw []byte, id int64, entries uint64) error {
	if err := validateJSON(raw); err != nil {
		return failure("invalid_archive_document")
	}
	m, err := object(raw)
	if err != nil {
		return failure("invalid_archive_document")
	}
	domain, _ := stringValue(m["domain"])
	refID, ok := intValue(m["todo_id"])
	var values []json.RawMessage
	if domain != HistoryDomain || !ok || refID != id || json.Unmarshal(m["entries"], &values) != nil || values == nil || uint64(len(values)) != entries {
		return failure("archive_identity_or_count_mismatch")
	}
	return nil
}

func (x *exporter) histories(row row) error {
	refs, err := archiveReferences(row, "note_history_archives")
	if err != nil {
		return err
	}
	for _, ref := range refs {
		file, hash, size, entries, err := validateArchiveRef(row.id, ref)
		if err != nil {
			return err
		}
		rel := filepath.Join("note-history-v1", file)
		raw, err := readUnder(x.source, rel, MaxAssetBytes)
		if err != nil {
			return failure("archive_unavailable")
		}
		if int64(len(raw)) != size || digest(raw) != hash {
			return failure("archive_digest_or_size_mismatch")
		}
		if err := validateArchive(raw, row.id, entries); err != nil {
			return err
		}
		path := filepath.Join(x.home, rel)
		if _, err := x.asset(raw, path, "note-history", entries); err != nil {
			return err
		}
		if math.MaxUint64-x.m.Counts.ArchiveEntries < entries {
			return failure("archive_count_overflow")
		}
		x.m.Counts.ArchiveReferences++
		x.m.Counts.ArchiveEntries += entries
		x.m.References = append(x.m.References, Reference{ItemID: row.id, Field: "note_history_archives", Kind: "note-history", Path: path, Status: "imported", Exists: boolPtr(true), SHA256: hash, Entries: entries})
	}
	return nil
}

func boolPtr(v bool) *bool { return &v }

type specPath struct{ field, path string }

func specPaths(row row) []specPath {
	var paths []specPath
	if path, ok := stringValue(row.fields["spec"]); ok {
		paths = append(paths, specPath{"spec", path})
	}
	if path, ok := stringValue(row.fields["spec_history"]); ok {
		return append(paths, specPath{"spec_history", path})
	}
	var history []json.RawMessage
	if json.Unmarshal(row.fields["spec_history"], &history) == nil {
		for _, raw := range history {
			if path, ok := stringValue(raw); ok {
				paths = append(paths, specPath{"spec_history", path})
			} else if m, err := object(raw); err == nil {
				if path, ok := stringValue(m["spec"]); ok {
					paths = append(paths, specPath{"spec_history", path})
				}
			}
		}
	}
	return paths
}

func (x *exporter) spec(id int64, field, path string) error {
	r := Reference{ItemID: id, Field: field, Kind: "spec", Path: path, Status: "unverified", Reason: "outside_home"}
	x.m.Counts.SpecReferences++
	if rel, ok := localReference(x.home, path); ok {
		raw, err := readUnder(x.source, rel, MaxAssetBytes)
		if err == nil {
			a, err := x.asset(raw, path, "spec", 0)
			if err != nil {
				return err
			}
			// Keep exact legacy strings as well as a native absolute path alias.
			absolute := filepath.Join(x.home, rel)
			if absolute != path {
				if _, err := x.asset(raw, absolute, "spec", 0); err != nil {
					return err
				}
			}
			r.Status, r.Exists, r.SHA256, r.Reason = "imported", boolPtr(true), a.SHA256, ""
		} else {
			code, _ := ErrorCode(err)
			r.Reason = code
			switch code {
			case "file_missing":
				r.Status, r.Exists = "missing", boolPtr(false)
			case "path_unverified":
				r.Status = "unverified"
			default:
				r.Status = "unavailable"
				if info, err := x.source.Lstat(rel); err == nil && !linked(info) {
					r.Exists = boolPtr(true)
				}
			}
		}
	} else if forbiddenPath(path) {
		r.Reason = "excluded_path"
	}
	switch r.Status {
	case "imported":
		x.m.Counts.SpecImported++
	case "missing":
		x.m.Counts.SpecMissing++
	case "unavailable":
		x.m.Counts.SpecUnavailable++
	default:
		x.m.Counts.SpecUnverified++
	}
	x.m.References = append(x.m.References, r)
	return nil
}
