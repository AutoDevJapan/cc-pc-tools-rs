package main

import (
	"encoding/json"
	"path"
	"strings"
)

// A renamed masked reference is bound by the exact source.json output already
// inside the same immutable, reviewed manifest. The acquired source/index is
// never renamed and the metadata grants no rights or general file access.
func skillsMaskedBindings(m skillsManifest) (map[string]string, bool) {
	bound := map[string]string{}
	byKey := map[string]skillsSource{}
	for _, s := range m.Sources {
		key := skillsSourceKey(s.Repository, s.Commit, s.Path)
		if prior, ok := byKey[key]; ok && prior != s {
			return nil, false
		}
		byKey[key] = s
	}
	outputs := map[string]skillsOutput{}
	for _, f := range m.Files {
		if _, ok := outputs[f.Target]; ok {
			return nil, false
		}
		outputs[f.Target] = f
	}
	renamed := false
	for _, f := range m.Files {
		parts := strings.Split(f.Target, "/")
		if len(parts) != 3 || parts[1] != "references" || parts[2] != "source.json" {
			continue
		}
		name := parts[0]
		maskedTarget := name + "/references/original-skill.masked.txt"
		if _, masked := outputs[maskedTarget]; !masked {
			continue
		}
		value, ok := conversationJSON(f.body)
		if !ok {
			return nil, false
		}
		_, hasSource := value["source"]
		_, hasWriter := value["reformulation_writer"]
		if !hasWriter {
			continue
		}
		if !hasSource || !hasWriter || !skillsName.MatchString(name) || len(name) > 80 || hashBytes(f.body) != f.SHA || value["reformulation_writer"] != m.Writer.Job {
			return nil, false
		}
		raw, e := json.Marshal(value["source"])
		if e != nil {
			return nil, false
		}
		var source skillsSource
		if !skillsStrict(raw, &source) {
			return nil, false
		}
		key := skillsSourceKey(source.Repository, source.Commit, source.Path)
		selected, ok := byKey[key]
		if !ok || selected != source || path.Base(source.Path) != "SKILL.md" {
			return nil, false
		}
		if _, duplicate := bound[key]; duplicate {
			return nil, false
		}
		entry, entryOK := outputs[name+"/SKILL.md"]
		notice, noticeOK := outputs[name+"/references/LICENSE.txt"]
		masked := outputs[maskedTarget]
		if !entryOK || !noticeOK || len(entry.body) == 0 || hashBytes(entry.body) != entry.SHA || notice.SHA != source.License.SHA || hashBytes(notice.body) != source.License.SHA || len(masked.body) == 0 || hashBytes(masked.body) != masked.SHA {
			return nil, false
		}
		bound[key] = maskedTarget
		legacy, ok := skillsMaskedSourceTarget(source.Path)
		if !ok {
			return nil, false
		}
		renamed = renamed || legacy != maskedTarget
	}
	if renamed {
		for key, s := range byKey {
			if path.Base(s.Path) == "SKILL.md" {
				if _, ok := bound[key]; !ok {
					return nil, false
				}
			}
		}
	}
	// A supplied masked reference is never made optional by also retaining raw
	// source elsewhere. Every such namespace must have exactly one owner.
	byTarget := map[string]string{}
	for key, target := range bound {
		if prior, ok := byTarget[target]; ok && prior != key {
			return nil, false
		}
		byTarget[target] = key
	}
	for target := range outputs {
		if path.Base(target) != "original-skill.masked.txt" {
			continue
		}
		parts := strings.Split(target, "/")
		if len(parts) != 3 || parts[1] != "references" || !skillsName.MatchString(parts[0]) {
			return nil, false
		}
		owner := byTarget[target]
		if owner == "" {
			for key, s := range byKey {
				legacy, ok := skillsMaskedSourceTarget(s.Path)
				if ok && legacy == target {
					if owner != "" && owner != key {
						return nil, false
					}
					owner = key
				}
			}
			if owner == "" {
				return nil, false
			}
			if existing, ok := bound[owner]; ok && existing != target {
				return nil, false
			}
			bound[owner] = target
			byTarget[target] = owner
		}
	}
	return bound, true
}
func skillsBoundMaskedOutput(source skillsSource, outputs []skillsOutput, bound map[string]string) bool {
	target, ok := bound[skillsSourceKey(source.Repository, source.Commit, source.Path)]
	if !ok {
		return skillsHasMaskedSourceOutput(source.Path, outputs)
	}
	for _, f := range outputs {
		if f.Target == target && len(f.body) > 0 && hashBytes(f.body) == f.SHA {
			return true
		}
	}
	return false
}
