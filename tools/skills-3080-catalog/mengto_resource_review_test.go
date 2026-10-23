package main

import (
	"errors"
	"strings"
	"testing"
)

type resourceReview struct {
	Path    string `json:"path"`
	Support []struct {
		Path     string `json:"path"`
		Saved    bool   `json:"body_saved"`
		Read     bool   `json:"body_read"`
		Scan     bool   `json:"scan_only"`
		ScanNote string `json:"scan_note"`
	} `json:"supporting_resources"`
}

func validateCodex20ResourceReview(entries []resourceReview) error {
	primary, total, saved, read, scan := 0, 0, 0, 0, 0
	seen := map[string]bool{}
	for _, e := range entries {
		if !strings.HasPrefix(e.Path, "agent-skills/codex/") {
			continue
		}
		primary++
		prefix := strings.TrimSuffix(e.Path, "SKILL.md")
		for _, s := range e.Support {
			if !strings.HasPrefix(s.Path, prefix) || seen[s.Path] || (s.Read && !s.Saved) || s.Scan != (s.Saved && !s.Read) || (s.Scan && s.ScanNote == "") {
				return errors.New("saved, fully read and scan-only resources were conflated")
			}
			seen[s.Path] = true
			total++
			if s.Saved {
				saved++
			}
			if s.Read {
				read++
			}
			if s.Scan {
				scan++
			}
		}
	}
	if primary != 20 || total != 109 || saved != 53 || read != 13 || scan != 40 {
		return errors.New("codex20 resource review differs from its received fixed scope")
	}
	return nil
}

func TestCodex20ResourcesKeepScanOnlyDistinctFromFullRead(t *testing.T) {
	for _, kind := range []string{"valid", "missing_scan_flag", "claimed_full_read", "unsaved_scan", "missing_scan_note"} {
		t.Run(kind, func(t *testing.T) {
			var a struct {
				Entries []resourceReview `json:"entries"`
			}
			loadPublicReceipt(t, "skills-3080-mengto-partial-evaluation.json", &a)
			changed := false
			for i := range a.Entries {
				for j := range a.Entries[i].Support {
					s := &a.Entries[i].Support[j]
					if changed || !s.Scan {
						continue
					}
					switch kind {
					case "missing_scan_flag":
						s.Scan = false
					case "claimed_full_read":
						s.Read = true
					case "unsaved_scan":
						s.Saved = false
					case "missing_scan_note":
						s.ScanNote = ""
					}
					changed = true
				}
			}
			if !changed {
				t.Fatal("fixture has no scan-only resource to mutate")
			}
			if err := validateCodex20ResourceReview(a.Entries); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
