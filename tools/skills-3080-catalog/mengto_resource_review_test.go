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
		Executed bool   `json:"executed"`
	} `json:"supporting_resources"`
}

func validateCodex20ResourceReview(entries []resourceReview) error {
	primary, total, saved, read, scan := 0, 0, 0, 0, 0
	seen := map[string]bool{}
	for _, e := range entries {
		for _, s := range e.Support {
			if s.Executed {
				return errors.New("source execution was claimed in a read-only resource assessment")
			}
		}
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
	for _, kind := range []string{"valid", "missing_scan_flag", "claimed_full_read", "unsaved_scan", "missing_scan_note", "claimed_execution", "non_codex_execution"} {
		t.Run(kind, func(t *testing.T) {
			var a struct {
				Entries  []resourceReview `json:"entries"`
				Executed int              `json:"new_source_commands_executed"`
			}
			loadPublicReceipt(t, "skills-3080-mengto-partial-evaluation.json", &a)
			if a.Executed != 0 {
				t.Fatal("read-only assessment executed upstream commands")
			}
			changed := false
			for i := range a.Entries {
				for j := range a.Entries[i].Support {
					s := &a.Entries[i].Support[j]
					eligible := s.Scan
					if kind == "non_codex_execution" {
						eligible = !strings.HasPrefix(a.Entries[i].Path, "agent-skills/codex/")
					}
					if changed || !eligible {
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
					case "claimed_execution", "non_codex_execution":
						s.Executed = true
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
