package migrate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"autodevjapan/shared-todo-cloud/internal/transport"
)

type exportPackage struct {
	root     *os.Root
	manifest Manifest
	snapshot []byte
}

func loadExport(ctx context.Context, dir string) (*exportPackage, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, failure("export_unavailable")
	}
	return loadExportRoot(ctx, root)
}

// The caller transfers ownership of the already bounded directory handle.
func loadExportRoot(ctx context.Context, root *os.Root) (*exportPackage, error) {
	p := &exportPackage{root: root}
	success := false
	defer func() {
		if !success {
			root.Close()
		}
	}()
	raw, err := readUnder(root, "manifest.json", maxManifestBytes)
	if err != nil {
		return nil, failure("manifest_unavailable")
	}
	if err := validateJSON(raw); err != nil {
		return nil, failure("invalid_manifest")
	}
	if json.Unmarshal(raw, &p.manifest) != nil || p.manifest.Format != Format || p.manifest.Root.File != "snapshot.json" || !validDigest(p.manifest.Root.SHA256) || p.manifest.Root.Bytes <= 0 || p.manifest.Root.Bytes > transport.MaxDocumentBytes || p.manifest.Root.OriginalPath == "" || forbiddenPath(p.manifest.Root.OriginalPath) {
		return nil, failure("invalid_manifest")
	}
	p.snapshot, err = p.read(p.manifest.Root, transport.MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	rows, counts, err := parseDocument(p.snapshot)
	if err != nil {
		return nil, err
	}
	if err := p.validateInventory(rows, counts); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, a := range p.manifest.Assets {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		if seen[a.SHA256] {
			continue
		}
		if _, err := p.read(a.File, MaxAssetBytes); err != nil {
			return nil, err
		}
		seen[a.SHA256] = true
	}
	// Archive identity and entry count are checked again, on the exported bytes.
	aliases := map[string]Asset{}
	for _, a := range p.manifest.Assets {
		aliases[a.OriginalPath] = a
	}
	for _, ref := range p.manifest.References {
		if ref.Kind != "note-history" {
			continue
		}
		b, err := p.read(aliases[ref.Path].File, MaxAssetBytes)
		if err != nil {
			return nil, err
		}
		if err := validateArchive(b, ref.ItemID, ref.Entries); err != nil {
			return nil, err
		}
	}
	success = true
	return p, nil
}

// VerifyExport validates only local bytes. It requires neither credentials nor
// a running cloud ledger and does not resolve any original source reference.
func VerifyExport(ctx context.Context, dir string) (Summary, error) {
	if err := cancelled(ctx); err != nil {
		return Summary{}, err
	}
	p, err := loadExport(ctx, dir)
	if err != nil {
		return Summary{}, err
	}
	defer p.root.Close()
	summary := summarize(p.manifest)
	summary.VerifiedBlobs = p.manifest.Counts.AssetFiles + 1
	return summary, nil
}

func (p *exportPackage) read(f File, limit int64) ([]byte, error) {
	b, err := readUnder(p.root, filepath.FromSlash(f.File), limit)
	if err != nil {
		return nil, failure("export_file_unavailable")
	}
	if int64(len(b)) != f.Bytes || digest(b) != f.SHA256 {
		return nil, failure("export_digest_or_size_mismatch")
	}
	return b, nil
}

func (p *exportPackage) validateInventory(rows []row, counts Counts) error {
	m := p.manifest
	aliases, blobs, used := map[string]Asset{}, map[string]int64{}, map[string]bool{}
	for _, a := range m.Assets {
		if !validDigest(a.SHA256) || a.File.File != "assets/"+a.SHA256 || a.Bytes < 0 || a.Bytes > MaxAssetBytes || a.OriginalPath == "" || forbiddenPath(a.OriginalPath) || a.Kind != "spec" && a.Kind != "note-history" || a.Kind == "spec" && a.Entries != 0 || a.Kind == "note-history" && a.Entries == 0 {
			return failure("invalid_manifest_asset")
		}
		if _, exists := aliases[a.OriginalPath]; exists {
			return failure("duplicate_manifest_alias")
		}
		aliases[a.OriginalPath] = a
		if size, ok := blobs[a.SHA256]; ok {
			if size != a.Bytes {
				return failure("invalid_manifest_asset")
			}
		} else {
			blobs[a.SHA256] = a.Bytes
			counts.AssetBytes += a.Bytes
		}
	}
	home := filepath.Dir(m.Root.OriginalPath)
	index := 0
	nextRef := func(id int64, field, kind, path string) (Reference, error) {
		if index >= len(m.References) {
			return Reference{}, failure("manifest_reference_mismatch")
		}
		r := m.References[index]
		index++
		if r.ItemID != id || r.Field != field || r.Kind != kind || r.Path != path {
			return Reference{}, failure("manifest_reference_mismatch")
		}
		return r, nil
	}
	for _, row := range rows {
		refs, err := archiveReferences(row, "note_history_archives")
		if err != nil {
			return err
		}
		for _, raw := range refs {
			file, hash, size, entries, err := validateArchiveRef(row.id, raw)
			if err != nil {
				return err
			}
			path := filepath.Join(home, "note-history-v1", file)
			r, err := nextRef(row.id, "note_history_archives", "note-history", path)
			if err != nil {
				return err
			}
			a, ok := aliases[path]
			if !ok || a.Kind != "note-history" || a.SHA256 != hash || a.Bytes != size || a.Entries != entries || r.Status != "imported" || r.SHA256 != hash || r.Entries != entries || r.Exists == nil || !*r.Exists {
				return failure("manifest_reference_mismatch")
			}
			used[path] = true
			counts.ArchiveReferences++
			counts.ArchiveEntries += entries
		}
		for _, spec := range specPaths(row) {
			r, err := nextRef(row.id, spec.field, "spec", spec.path)
			if err != nil || r.Entries != 0 {
				return failure("manifest_reference_mismatch")
			}
			counts.SpecReferences++
			switch r.Status {
			case "imported":
				rel, local := localReference(home, r.Path)
				a, ok := aliases[r.Path]
				if !local || !ok || a.Kind != "spec" || a.SHA256 != r.SHA256 || r.Exists == nil || !*r.Exists {
					return failure("manifest_reference_mismatch")
				}
				used[r.Path] = true
				absolute := filepath.Join(home, rel)
				if other, ok := aliases[absolute]; !ok || other.Kind != "spec" || other.SHA256 != a.SHA256 || other.Bytes != a.Bytes {
					return failure("manifest_reference_mismatch")
				}
				used[absolute] = true
				counts.SpecImported++
			case "missing":
				if r.Exists == nil || *r.Exists || r.SHA256 != "" {
					return failure("manifest_reference_mismatch")
				}
				counts.SpecMissing++
			case "unavailable":
				if r.SHA256 != "" {
					return failure("manifest_reference_mismatch")
				}
				counts.SpecUnavailable++
			case "unverified":
				if r.Exists != nil || r.SHA256 != "" {
					return failure("manifest_reference_mismatch")
				}
				counts.SpecUnverified++
			default:
				return failure("manifest_reference_mismatch")
			}
		}
	}
	if index != len(m.References) || len(used) != len(aliases) {
		return failure("manifest_reference_mismatch")
	}
	counts.AssetFiles, counts.AssetAliases = len(blobs), len(aliases)
	if counts != m.Counts {
		return failure("manifest_count_mismatch")
	}
	return nil
}
