package assetio

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"math"
	"path/filepath"
	"strconv"
)

type reference struct {
	path    string
	kind    string
	archive *archiveRef
}

type archiveRef struct {
	id      int64
	sha     string
	bytes   int64
	entries uint64
}

func newReferences(ctx context.Context, before map[int64]json.RawMessage, upserts []json.RawMessage) ([]reference, error) {
	var total int64
	for _, raw := range upserts {
		if int64(len(raw)) > MaxDocumentBytes-total {
			return nil, ErrDocumentTooLarge
		}
		total += int64(len(raw))
	}
	var refs []reference
	seen := map[int64]bool{}
	for _, raw := range upserts {
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		fields, id, err := item(raw)
		if err != nil || seen[id] {
			return nil, ErrInvalidInput
		}
		seen[id] = true
		var old map[string]json.RawMessage
		if prior, exists := before[id]; exists {
			if len(prior) > MaxDocumentBytes {
				return nil, ErrDocumentTooLarge
			}
			var priorID int64
			old, priorID, err = item(prior)
			if err != nil || priorID != id {
				return nil, ErrInvalidInput
			}
		}
		oldPaths := map[string]bool{}
		for _, path := range specPaths(old) {
			oldPaths[path] = true
		}
		for _, path := range specPaths(fields) {
			if !oldPaths[path] {
				refs = append(refs, reference{path: path, kind: "spec"})
			}
		}
		current := fields["note_history_archives"]
		if len(current) == 0 || bytes.Equal(bytes.TrimSpace(current), []byte("null")) || equalJSON(current, old["note_history_archives"]) {
			continue
		}
		var incoming, previous []json.RawMessage
		if json.Unmarshal(current, &incoming) != nil || incoming == nil {
			return nil, ErrArchiveReference
		}
		_ = json.Unmarshal(old["note_history_archives"], &previous)
		priorRefs := map[string]bool{}
		for _, prior := range previous {
			priorRefs[identity(prior)] = true
		}
		for _, rawRef := range incoming {
			if priorRefs[identity(rawRef)] {
				continue
			}
			file, expected, err := parseArchiveRef(id, rawRef)
			if err != nil {
				return nil, err
			}
			refs = append(refs, reference{path: filepath.Join("note-history-v1", file), kind: "note-history", archive: &expected})
		}
	}
	return refs, nil
}

func item(raw []byte) (map[string]json.RawMessage, int64, error) {
	fields, err := object(raw)
	if err != nil {
		return nil, 0, ErrInvalidInput
	}
	id, err := strconv.ParseInt(string(bytes.TrimSpace(fields["id"])), 10, 64)
	if err != nil || id <= 0 || id == math.MaxInt64 {
		return nil, 0, ErrInvalidInput
	}
	return fields, id, nil
}

// Match the legacy export schema, without treating inline bodies/unknown fields
// as paths: spec is a string, spec_history is a string or an array containing
// strings and objects with an exact lower-case spec field.
func specPaths(fields map[string]json.RawMessage) []string {
	var paths []string
	if path, ok := stringValue(fields["spec"]); ok {
		paths = append(paths, path)
	}
	if path, ok := stringValue(fields["spec_history"]); ok {
		return append(paths, path)
	}
	var history []json.RawMessage
	if json.Unmarshal(fields["spec_history"], &history) == nil {
		for _, raw := range history {
			if path, ok := stringValue(raw); ok {
				paths = append(paths, path)
			} else if fields, err := object(raw); err == nil {
				if path, ok := stringValue(fields["spec"]); ok {
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

func parseArchiveRef(id int64, raw []byte) (string, archiveRef, error) {
	fields, err := object(raw)
	if err != nil {
		return "", archiveRef{}, ErrArchiveReference
	}
	domain, _ := stringValue(fields["domain"])
	refID, idErr := strconv.ParseInt(string(bytes.TrimSpace(fields["todo_id"])), 10, 64)
	sha, _ := stringValue(fields["sha256"])
	file, _ := stringValue(fields["file"])
	size, sizeErr := strconv.ParseInt(string(bytes.TrimSpace(fields["bytes"])), 10, 64)
	entries, countErr := strconv.ParseUint(string(bytes.TrimSpace(fields["entries"])), 10, 64)
	decoded, hashErr := hex.DecodeString(sha)
	if domain != HistoryDomain || idErr != nil || refID != id || len(decoded) != 32 || hashErr != nil || hex.EncodeToString(decoded) != sha ||
		file != strconv.FormatInt(id, 10)+"-"+sha+".json" || sizeErr != nil || size <= 0 || size > MaxAssetBytes || countErr != nil || entries == 0 {
		return "", archiveRef{}, ErrArchiveReference
	}
	return file, archiveRef{id: id, sha: sha, bytes: size, entries: entries}, nil
}

func validateArchive(raw []byte, id int64, entries uint64) error {
	if err := validateJSON(raw); err != nil {
		return ErrArchiveDocument
	}
	fields, err := object(raw)
	if err != nil {
		return ErrArchiveDocument
	}
	domain, _ := stringValue(fields["domain"])
	refID, idErr := strconv.ParseInt(string(bytes.TrimSpace(fields["todo_id"])), 10, 64)
	var values []json.RawMessage
	if domain != HistoryDomain || idErr != nil || refID != id || json.Unmarshal(fields["entries"], &values) != nil || values == nil || uint64(len(values)) != entries {
		return ErrArchiveDocument
	}
	return nil
}
