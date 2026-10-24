package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

type resourceReview struct {
	Path    string `json:"path"`
	Reader  string `json:"body_read_by"`
	Blob    string `json:"git_blob"`
	SHA     string `json:"source_sha256"`
	Bytes   int    `json:"bytes"`
	Support []struct {
		Path     string  `json:"path"`
		Blob     string  `json:"git_blob"`
		SHA      string  `json:"source_sha256"`
		Bytes    *int    `json:"bytes"`
		Saved    bool    `json:"body_saved"`
		Read     bool    `json:"body_read"`
		Scan     bool    `json:"scan_only"`
		ScanNote string  `json:"scan_note"`
		ReadBy   *string `json:"read_by"`
		ScanBy   *string `json:"scan_by"`
		Executed *bool   `json:"executed"`
	} `json:"supporting_resources"`
}

// Taken from the preserved final50-prepared093456.json, verified against the
// fixed tree/index/catalog/gzip before publication; not derived from live JSON.
// Prepared bytes SHA256: f8cbd859ab35afbd438111f3988a6bd26934083251d969071ba0eb50ba5610a3.
const mengtoFinal50Bodies = "8c6ff5d718569088014390946db628878751e818727f1057260cfd8cb3a840ce"
const mengtoFinal50Support = "5a3749b14285aa3dc84435cff8bc005a51246b3a8bc063a99ac551f6809699d4"
const mengtoFinal50Actors = "81316015ba69e6f12f80442f1f184399dd7f0a03cc250366f660895eb9424b14"

func validateMengtoFinal50Resources(entries []resourceReview) error {
	var selected []resourceReview
	for _, e := range entries {
		if e.Reader == "20261024-081950-todo-3535" || e.Reader == "20261024-082550-todo-3536" {
			selected = append(selected, e)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
	var actors []string
	for _, e := range selected {
		actors = append(actors, e.Path+"\t"+e.Reader+"\n")
	}
	if len(selected) != 50 || primaryReaderFingerprint(actors) != mengtoFinal50Actors {
		return errors.New("final50 primary reader attribution differs from the independent prepared roster")
	}
	bodies := sha256.New()
	var supportRows []string
	seen := map[string]bool{}
	saved, read, scanned := 0, 0, 0
	for _, e := range selected {
		fmt.Fprintf(bodies, "%s\t%s\t%d\t%s\n", e.Path, e.Blob, e.Bytes, e.SHA)
		for _, s := range e.Support {
			if !strings.HasPrefix(s.Path, strings.TrimSuffix(e.Path, "SKILL.md")) || seen[s.Path] || s.Executed == nil || *s.Executed || s.Bytes == nil || *s.Bytes < 1 || !validHex(s.Blob, 40) || (s.Saved && !validHex(s.SHA, 64)) || (!s.Saved && s.SHA != "") || (s.Read && !s.Saved) || s.Scan != (s.Saved && !s.Read) || (s.Scan && s.ScanNote == "") {
				return errors.New("final50 support identity or reading scope is unverified")
			}
			if err := validateResourceActor(e.Reader, s.Saved, s.Read, s.Scan, s.ReadBy, s.ScanBy); err != nil {
				return err
			}
			seen[s.Path] = true
			supportRows = append(supportRows, fmt.Sprintf("%s\t%s\t%d\t%s\t%t\t%t\t%t\n", s.Path, s.Blob, *s.Bytes, s.SHA, s.Saved, s.Read, s.Scan))
			if s.Saved {
				saved++
			}
			if s.Read {
				read++
			}
			if s.Scan {
				scanned++
			}
		}
	}
	sort.Strings(supportRows)
	support := sha256.Sum256([]byte(strings.Join(supportRows, "")))
	if len(selected) != 50 || len(supportRows) != 349 || saved != 73 || read != 43 || scanned != 30 || hex.EncodeToString(bodies.Sum(nil)) != mengtoFinal50Bodies || hex.EncodeToString(support[:]) != mengtoFinal50Support {
		return errors.New("final50 identities or reading states differ from the independent saved anchor")
	}
	return nil
}

func TestMengtoFinal50ReadersAndScannersRemainDistinct(t *testing.T) {
	for _, kind := range []string{"valid", "wrong_scanner", "scan_claims_reader", "read_loses_actor", "read_claims_scanner", "unsaved_claims_actor"} {
		t.Run(kind, func(t *testing.T) {
			var a struct {
				Entries []resourceReview `json:"entries"`
			}
			loadPublicReceipt(t, "skills-3080-mengto-partial-evaluation.json", &a)
			changed := kind == "valid"
			for i := range a.Entries {
				e := &a.Entries[i]
				if changed || (e.Reader != "20261024-081950-todo-3535" && e.Reader != "20261024-082550-todo-3536") {
					continue
				}
				for j := range e.Support {
					s := &e.Support[j]
					if ((kind == "wrong_scanner" || kind == "scan_claims_reader") && !s.Scan) || ((kind == "read_loses_actor" || kind == "read_claims_scanner") && !s.Read) || (kind == "unsaved_claims_actor" && s.Saved) {
						continue
					}
					actor := e.Reader
					switch kind {
					case "wrong_scanner":
						actor = "different-job"
						s.ScanBy = &actor
					case "scan_claims_reader", "unsaved_claims_actor":
						s.ReadBy = &actor
					case "read_loses_actor":
						s.ReadBy = nil
					case "read_claims_scanner":
						s.ScanBy = &actor
					}
					changed = true
					break
				}
			}
			if !changed {
				t.Fatal("fixture has no matching provenance to mutate")
			}
			if err := validateMengtoFinal50Resources(a.Entries); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			} else if kind != "valid" && !strings.Contains(err.Error(), resourceActorError) {
				t.Fatalf("%s failed for a different reason: %v", kind, err)
			}
		})
	}
}

func TestMengtoFinal50ResourcesRemainBoundToThePreservedAssessment(t *testing.T) {
	for _, kind := range []string{"valid", "body_changed", "support_changed", "missing_bytes", "changed_read_scope", "missing_execution", "duplicate_support"} {
		t.Run(kind, func(t *testing.T) {
			var a struct {
				Entries []resourceReview `json:"entries"`
			}
			loadPublicReceipt(t, "skills-3080-mengto-partial-evaluation.json", &a)
			changed := false
			for i := range a.Entries {
				e := &a.Entries[i]
				if e.Reader != "20261024-081950-todo-3535" && e.Reader != "20261024-082550-todo-3536" {
					continue
				}
				if len(e.Support) == 0 {
					continue
				}
				switch kind {
				case "body_changed":
					e.SHA = strings.Repeat("e", 64)
				case "support_changed":
					e.Support[0].Blob = strings.Repeat("e", 40)
				case "missing_bytes":
					e.Support[0].Bytes = nil
				case "changed_read_scope":
					e.Support[0].Read = !e.Support[0].Read
				case "missing_execution":
					e.Support[0].Executed = nil
				case "duplicate_support":
					e.Support = append(e.Support, e.Support[0])
				}
				changed = true
				break
			}
			if !changed {
				t.Fatal("fixture has no selected final50 support")
			}
			if err := validateMengtoFinal50Resources(a.Entries); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

func validateMengtoResourceReview(entries []resourceReview) error {
	primary, total, saved, read, scan := 0, 0, 0, 0, 0
	seen := map[string]bool{}
	for _, e := range entries {
		for _, s := range e.Support {
			if s.Executed == nil || *s.Executed {
				return errors.New("source execution was claimed or left unverified in a read-only resource assessment")
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

func TestMengtoResourcesKeepReadScanAndExecutionClaimsDistinct(t *testing.T) {
	for _, kind := range []string{"valid", "missing_scan_flag", "claimed_full_read", "unsaved_scan", "missing_scan_note", "claimed_execution", "non_codex_execution", "missing_execution_flag"} {
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
						claimed := true
						s.Executed = &claimed
					case "missing_execution_flag":
						s.Executed = nil
					}
					changed = true
				}
			}
			if !changed {
				t.Fatal("fixture has no scan-only resource to mutate")
			}
			if err := validateMengtoResourceReview(a.Entries); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
