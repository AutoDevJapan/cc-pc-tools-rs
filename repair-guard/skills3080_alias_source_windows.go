package main

import (
	"encoding/json"
	"strings"
)

// This resolves a preserved reference inside the already-qualified manifest.
// It grants no access: the acquired catalog/index/tree/raw-object verification
// and exact physical-line comparison still run in skillsSourceObject.
func skillsManifestMaskedSourceTarget(source skillsSource, m skillsManifest) (string, object, bool) {
	legacy, valid := skillsMaskedSourceTarget(source.Path)
	if !valid {
		return "", nil, false
	}
	if skillsHasMaskedSourceOutput(source.Path, m.Files) {
		return legacy, nil, true
	}
	if !skillsWriterValid(m.Writer) {
		return "", nil, false
	}
	target, metadata := "", object(nil)
	for _, output := range m.Files {
		parts := strings.Split(output.Target, "/")
		if len(parts) != 3 || parts[1] != "references" || parts[2] != "source.json" {
			continue
		}
		if !skillsRelative(output.Target) || !skillsName.MatchString(parts[0]) || len(parts[0]) > 80 || hashBytes(output.body) != output.SHA {
			return "", nil, false
		}
		meta, ok := conversationJSON(output.body)
		if !ok {
			return "", nil, false
		}
		for key := range meta {
			for _, canonical := range []string{"source", "reformulation_writer", "source_bytes", "source_git_blob"} {
				if strings.EqualFold(key, canonical) && key != canonical {
					return "", nil, false
				}
			}
		}
		encoded, err := json.Marshal(meta["source"])
		var declared skillsSource
		if err != nil || !skillsStrict(encoded, &declared) {
			return "", nil, false
		}
		if declared.Repository != source.Repository || declared.Commit != source.Commit || declared.Path != source.Path {
			continue
		}
		count, countOK := meta["source_bytes"].(json.Number)
		bytes, countErr := count.Int64()
		blob, blobOK := meta["source_git_blob"].(string)
		if declared != source || meta["reformulation_writer"] != m.Writer.Job || !countOK || countErr != nil || bytes <= 0 || !blobOK || !civ6Commit.MatchString(blob) || blob == strings.Repeat("0", 40) || target != "" {
			return "", nil, false
		}
		name := parts[0]
		target = name + "/references/original-skill.masked.txt"
		if !skillsAliasOutputPresent(m.Files, target, "") || !skillsAliasOutputPresent(m.Files, name+"/SKILL.md", "") || !skillsAliasOutputPresent(m.Files, name+"/references/LICENSE.txt", source.License.SHA) {
			return "", nil, false
		}
		metadata = meta
	}
	return target, metadata, target != ""
}

func skillsAliasOutputPresent(files []skillsOutput, target, sha string) bool {
	for _, output := range files {
		if output.Target == target && len(output.body) > 0 && hashBytes(output.body) == output.SHA && (sha == "" || output.SHA == sha) {
			return true
		}
	}
	return false
}

func skillsManifestHasMaskedSourceOutput(source skillsSource, m skillsManifest) bool {
	_, _, ok := skillsManifestMaskedSourceTarget(source, m)
	return ok
}

func skillsManifestMaskedSourceObject(row skillsSavedSource, raw []byte, m skillsManifest) bool {
	if skillsMaskedSourceObject(row, raw, m.Files) {
		return true
	}
	if row.Role != "skill_entry" {
		return false
	}
	for _, source := range m.Sources {
		if source.Repository != row.Repo || source.Commit != row.Commit || source.Path != row.Path || source.SHA != row.SHA {
			continue
		}
		target, meta, ok := skillsManifestMaskedSourceTarget(source, m)
		if !ok || meta == nil || meta["source_git_blob"] != row.Blob {
			return false
		}
		count, ok := meta["source_bytes"].(json.Number)
		bytes, err := count.Int64()
		if !ok || err != nil || bytes != row.Bytes {
			return false
		}
		for _, output := range m.Files {
			if output.Target == target && hashBytes(output.body) == output.SHA && skillsMaskedSourceBytes(raw, output.body) {
				return true
			}
		}
	}
	return false
}
