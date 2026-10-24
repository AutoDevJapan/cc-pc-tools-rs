package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const mcpPointerReviewedBundleSHA = "b1eb81bf6ee61c6f2d3bd5a3739c89d85d4141352702108282690e8af8e10dc9"

type directSkillDesign struct {
	Name      string `json:"name"`
	ID        string `json:"inventory_id"`
	Repo      string `json:"source_repo"`
	Commit    string `json:"source_commit"`
	Path      string `json:"source_path"`
	SHA       string `json:"source_sha256"`
	EntrySHA  string `json:"entrypoint_sha256"`
	License   string `json:"primary_license"`
	NoticeSHA string `json:"individual_notice_sha256"`
}

// These two individually reviewed designs use a schema that can express an
// official author's individual grant and a listing/name mismatch. Do not force
// them through the older root-LICENSE-only batch schema or add blanket grants.
func directMcpPointerDesigns(t *testing.T) []directSkillDesign {
	t.Helper()
	var d struct {
		NotCap  bool                `json:"batch_is_not_total_scope_or_count_cap"`
		SHA     string              `json:"bundle_sha256"`
		Targets []directSkillDesign `json:"targets"`
	}
	loadPublicReceipt(t, "skills-3080-mcp-pointer-designs.json", &d)
	want := []directSkillDesign{
		{"mcp-builder", "github:e4ade76a7932447efe3dd6e777ab300a5a9a6f1cf13a392614b31c621906a016", "6mile/skills", "920714f17d81e0a95c7a9bc51faf0a7921cd982d", "skills/aabbcc456aa/hxl-code-reviewer/SKILL.md", "0f4592dcb53cf2b5d6b7febee6b4152018b565551a1c29e3c612f57b218ab295", "0ae88aa71d14ac1aa2505bc1406f5e427cdc8882bf1f2ef67720a49c02f0fedc", "Apache-2.0", "bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362"},
		{"pointer-trail-emitter", "github:a89e967becb119a2e1fde4392aaeb0c034ff9c683290947db8c690624fc9887e", "MengTo/Skills", "398cd41c86ba42fbb30e944ae507f463648f08b7", "agent-skills/web-design/pointer-trail-emitter/SKILL.md", "2cca505af09e4f34a96e1dd08b1d9bc1caca467dba3debfe62fce4756b2f2964", "3e8e5b19aca6e33be1dea2485911554d790651cb6bf02e5d4ffe63563d2d9996", "MIT", "b2a071ba22fefd803f4e9d1f5c777dd79c62d191695586a1a73fd4d0ebd051c6"},
	}
	if !d.NotCap || d.SHA != mcpPointerReviewedBundleSHA || !reflect.DeepEqual(d.Targets, want) {
		t.Fatal("direct individual-source designs differ from reviewed identities")
	}
	return d.Targets
}

// The explicit index lets later individually reviewed batches extend the
// authored set without changing an installation ceiling in these tests.
// Legacy plans retain their original stricter schema and checks.
func directAuthoredDesigns(t *testing.T) []directSkillDesign {
	t.Helper()
	var index struct {
		NotCap    bool `json:"not_an_adoption_cap"`
		Documents []struct {
			File  string `json:"file"`
			SHA   string `json:"sha256"`
			Count int    `json:"target_count"`
		} `json:"documents"`
	}
	loadPublicReceipt(t, "skills-3080-direct-design-index.json", &index)
	if !index.NotCap || len(index.Documents) == 0 {
		t.Fatal("direct design index scope absent")
	}
	var progress struct {
		Repos []struct {
			Repo   string `json:"requested"`
			Commit string `json:"commit"`
		} `json:"fixed_public_repos"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &progress)
	repos := map[string]string{}
	for _, r := range progress.Repos {
		repos[strings.ToLower(r.Repo)] = r.Commit
	}
	documents, names, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var result []directSkillDesign
	for _, ref := range index.Documents {
		if documents[ref.File] || !strings.HasPrefix(ref.File, "skills-3080-") || !strings.HasSuffix(ref.File, ".json") || strings.ContainsAny(ref.File, "/\\") || ref.Count < 1 {
			t.Fatal("invalid or repeated direct design document")
		}
		documents[ref.File] = true
		var d struct {
			NotCap  bool `json:"batch_is_not_total_scope_or_count_cap"`
			Targets []struct {
				directSkillDesign
				Rights string `json:"rights_scope"`
			} `json:"targets"`
		}
		raw := loadPublicReceipt(t, ref.File, &d)
		h := sha256.Sum256(raw)
		if hex.EncodeToString(h[:]) != ref.SHA || !d.NotCap || len(d.Targets) != ref.Count {
			t.Fatal("direct design document hash/count/scope differs")
		}
		for _, row := range d.Targets {
			r := row.directSkillDesign
			if names[r.Name] || ids[r.ID] || !candidateNameRE.MatchString(r.Name) || r.ID == "" || r.Repo == "" || r.Path == "" || !validHex(r.Commit, 40) || repos[strings.ToLower(r.Repo)] != r.Commit || !validHex(r.SHA, 64) || !validHex(r.EntrySHA, 64) || !validHex(r.NoticeSHA, 64) || row.Rights == "" || r.License == "" {
				t.Fatal("direct source identity or individual rights scope missing")
			}
			names[r.Name], ids[r.ID] = true, true
			result = append(result, r)
		}
	}
	return result
}

type newSkillMetadata struct {
	Name           string `json:"name"`
	Central        *bool  `json:"selected_central_path"`
	EnabledPresent *bool  `json:"enabled_present"`
	Enabled        *bool  `json:"enabled"`
	Description    *bool  `json:"description_matches"`
}
type mcpPointerDeployment struct {
	Done        *bool  `json:"overall_complete"`
	Created     int    `json:"new_targets_created"`
	Files       int    `json:"files_readback_verified"`
	Changed     *bool  `json:"guard_or_registration_changed"`
	OldWrites   int    `json:"old_target_writes"`
	Bundle      string `json:"bundle"`
	BundleSHA   string `json:"bundle_sha256"`
	Manifest    string `json:"manifest"`
	ManifestSHA string `json:"manifest_sha256"`
	Design      string `json:"design"`
	DesignSHA   string `json:"design_sha256"`
	JournalSHA  string `json:"apply_journal_sha256"`
	Discovery   struct {
		Version      string             `json:"codex_version"`
		ModelCalled  *bool              `json:"model_called"`
		Matches      int                `json:"target_matches"`
		Enabled      int                `json:"enabled_count"`
		Errors       int                `json:"parse_errors"`
		Exited       *bool              `json:"process_tree_exit_confirmed"`
		Drained      *bool              `json:"pending_stdout_read_drained"`
		WarningCause *bool              `json:"warning_cause_linked"`
		FullBudget   *bool              `json:"full_context_budget_proven"`
		Implicit     *bool              `json:"implicit_selection_proven"`
		Found        []newSkillMetadata `json:"found"`
	} `json:"discovery"`
	Use struct {
		Core      *bool `json:"core_passed"`
		Server    *bool `json:"server_executed"`
		Rendering *bool `json:"product_rendering_executed"`
		PostTurn  *bool `json:"postdeployment_model_turn_executed"`
	} `json:"explicit_application"`
	Retained struct {
		IDs         []string `json:"adoption_identity_snapshot"`
		Expected    int      `json:"expected_count"`
		Found       int      `json:"found_count"`
		Enabled     int      `json:"enabled_count"`
		Errors      int      `json:"parse_errors"`
		NamesSHA    string   `json:"known_target_names_sha256"`
		Exited      *bool    `json:"process_tree_exit_confirmed"`
		Drained     *bool    `json:"pending_stdout_read_drained"`
		ModelCalled *bool    `json:"model_called"`
		NamesSaved  *bool    `json:"raw_existing_names_or_paths_saved"`
		AllBytes    *bool    `json:"all_disk_hash_or_prechange_equality_proven"`
	} `json:"retained_known_metadata"`
}

func trueReceipt(v *bool) bool  { return v != nil && *v }
func falseReceipt(v *bool) bool { return v != nil && !*v }
func validateMcpPointerDeployment(e mcpPointerDeployment) error {
	if !falseReceipt(e.Done) || e.Created != 2 || e.Files != 10 || !falseReceipt(e.Changed) || e.OldWrites != 0 || e.BundleSHA != mcpPointerReviewedBundleSHA || !validHex(e.ManifestSHA, 64) || !validHex(e.DesignSHA, 64) || !validHex(e.JournalSHA, 64) {
		return errors.New("new-only deployment identity/count/scope differs")
	}
	d := e.Discovery
	if d.Version != "0.163.0" || d.Matches != 2 || d.Enabled != 2 || d.Errors != 0 || !trueReceipt(d.Exited) || !trueReceipt(d.Drained) || !falseReceipt(d.ModelCalled) || !falseReceipt(d.WarningCause) || !falseReceipt(d.FullBudget) || !falseReceipt(d.Implicit) {
		return errors.New("metadata discovery was absent or overclaimed")
	}
	seen := map[string]bool{}
	for _, r := range d.Found {
		if (r.Name != "mcp-builder" && r.Name != "pointer-trail-emitter") || seen[r.Name] || !trueReceipt(r.Central) || !trueReceipt(r.EnabledPresent) || !trueReceipt(r.Enabled) || !trueReceipt(r.Description) {
			return errors.New("requested metadata disabled, repeated, missing or mismatched")
		}
		seen[r.Name] = true
	}
	if len(seen) != 2 || !trueReceipt(e.Use.Core) || !falseReceipt(e.Use.Server) || !falseReceipt(e.Use.Rendering) || !falseReceipt(e.Use.PostTurn) {
		return errors.New("explicit design use or runtime limit differs")
	}
	r := e.Retained
	if len(r.IDs) != 33 || r.Expected != 46 || r.Found != r.Expected || r.Enabled != r.Expected || r.Errors != 0 || r.NamesSHA != "1eae60ec3874c620b3460222562d644df98e5ac91664a0138ada98a89394da9c" || !trueReceipt(r.Exited) || !trueReceipt(r.Drained) || !falseReceipt(r.ModelCalled) || !falseReceipt(r.NamesSaved) || !falseReceipt(r.AllBytes) {
		return errors.New("managed metadata retention missing or overclaimed")
	}
	return nil
}

func TestRetainedKnownMetadataFingerprintsOnlyItsHistoricalManagedSubset(t *testing.T) {
	var e mcpPointerDeployment
	loadPublicReceipt(t, "skills-3080-mcp-pointer-installation-evidence.json", &e)
	var ledger struct {
		Entries []struct {
			ID     string `json:"inventory_id"`
			Target string `json:"target"`
		} `json:"entries"`
	}
	var original struct {
		Targets []struct {
			Name string `json:"name"`
		} `json:"targets"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	loadPublicReceipt(t, "skills-3048-manifest.json", &original)
	byID := map[string]string{}
	for _, r := range ledger.Entries {
		if byID[r.ID] != "" {
			t.Fatal("repeated adoption identity")
		}
		byID[r.ID] = r.Target
	}
	seenIDs, known := map[string]bool{}, map[string]bool{}
	for _, id := range e.Retained.IDs {
		if seenIDs[id] || byID[id] == "" {
			t.Fatal("historical metadata identity is absent or repeated")
		}
		seenIDs[id] = true
		known[byID[id]] = true
	}
	for _, r := range original.Targets {
		if r.Name == "" {
			t.Fatal("original3048 target absent")
		}
		known[r.Name] = true
	}
	var names []string
	for n := range known {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.Sum256([]byte(strings.Join(names, "\n") + "\n"))
	if len(original.Targets) != 14 || len(names) != e.Retained.Expected || hex.EncodeToString(h[:]) != e.Retained.NamesSHA {
		t.Fatal("managed metadata fingerprint differs from its public snapshot sources")
	}
}
func TestMcpPointerInstalledBundleMatchesActualManifestAndIndividualSources(t *testing.T) {
	var e mcpPointerDeployment
	loadPublicReceipt(t, "skills-3080-mcp-pointer-installation-evidence.json", &e)
	if err := validateMcpPointerDeployment(e); err != nil {
		t.Fatal(err)
	}
	if e.Bundle != "skills-3080-mcp-pointer-reviewed-bundle.json" || e.Manifest != "skills-3080-installed-mcp-pointer-manifest.json" || e.Design != "skills-3080-mcp-pointer-designs.json" {
		t.Fatal("unexpected public artifact path")
	}
	type file struct {
		Path    string `json:"path"`
		SHA     string `json:"sha256"`
		Content string `json:"content"`
		Bytes   int    `json:"bytes"`
	}
	type target struct {
		Name   string `json:"name"`
		New    bool   `json:"new"`
		Before string `json:"before_sha256"`
		Files  []file `json:"files"`
	}
	var b struct {
		Root    string   `json:"root"`
		Targets []target `json:"targets"`
	}
	raw := loadPublicReceipt(t, e.Bundle, &b)
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != mcpPointerReviewedBundleSHA || b.Root != "D:/CC/Skills" || len(b.Targets) != 2 {
		t.Fatal("fixed reviewed bundle bytes differ")
	}
	var m struct {
		SHA     string   `json:"bundle_sha256"`
		Targets []target `json:"targets"`
	}
	raw = loadPublicReceipt(t, e.Manifest, &m)
	h = sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != e.ManifestSHA || m.SHA != mcpPointerReviewedBundleSHA || len(m.Targets) != 2 {
		t.Fatal("actual manifest hash/bundle differs")
	}
	var designs any
	raw = loadPublicReceipt(t, e.Design, &designs)
	h = sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != e.DesignSHA {
		t.Fatal("direct design bytes differ")
	}
	d := directMcpPointerDesigns(t)
	order := []string{"references/upstream-original.txt", "references/LICENSE.txt", "references/source.json", "references/sources.md", "SKILL.md"}
	for i, r := range d {
		bt, mt := b.Targets[i], m.Targets[i]
		if bt.Name != r.Name || mt.Name != r.Name || !mt.New || mt.Before != "absent" || len(bt.Files) != 5 || len(mt.Files) != 5 {
			t.Fatal("new target identity/order/absence differs")
		}
		for j, bf := range bt.Files {
			mf := mt.Files[j]
			hash := sha256.Sum256([]byte(bf.Content))
			if bf.Path != order[j] || mf.Path != bf.Path || bf.SHA != mf.SHA || bf.SHA != hex.EncodeToString(hash[:]) || mf.Bytes != len([]byte(bf.Content)) {
				t.Fatal("installed file metadata differs from exact reviewed content")
			}
		}
		if bt.Files[0].SHA != r.SHA || bt.Files[1].SHA != r.NoticeSHA || bt.Files[4].SHA != r.EntrySHA {
			t.Fatal("primary/license/modified entrypoint identities conflated")
		}
	}
	model, err := os.ReadFile("../../docs/skills-3080-pointer-emitter-model.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	h = sha256.Sum256(model)
	if len(model) != 4964 || bytes.ContainsRune(model, '\r') || hex.EncodeToString(h[:]) != "8fd48a4354f90ed6816b81f47f7101cba228ae57bc4a180331029edfc1e89042" {
		t.Fatal("documented self-authored numerical model changed")
	}
}
func TestMcpPointerDiscoveryRejectsDisabledMissingAndOverclaimedEvidence(t *testing.T) {
	for _, kind := range []string{"valid", "disabled", "enabled_missing", "wrong_path", "one_missing", "duplicated", "model_called", "server_claim", "budget_claim", "warning_cause_missing", "incomplete_write"} {
		t.Run(kind, func(t *testing.T) {
			var e mcpPointerDeployment
			loadPublicReceipt(t, "skills-3080-mcp-pointer-installation-evidence.json", &e)
			yes, no := true, false
			switch kind {
			case "disabled":
				e.Discovery.Found[0].Enabled = &no
			case "enabled_missing":
				e.Discovery.Found[0].Enabled = nil
			case "wrong_path":
				e.Discovery.Found[0].Central = &no
			case "one_missing":
				e.Discovery.Found = e.Discovery.Found[:1]
			case "duplicated":
				e.Discovery.Found[1] = e.Discovery.Found[0]
			case "model_called":
				e.Discovery.ModelCalled = &yes
			case "server_claim":
				e.Use.Server = &yes
			case "budget_claim":
				e.Discovery.FullBudget = &yes
			case "warning_cause_missing":
				e.Discovery.WarningCause = nil
			case "incomplete_write":
				e.Files = 9
			}
			if err := validateMcpPointerDeployment(e); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

type next10Projection struct {
	Batch struct {
		Historical *bool `json:"historical"`
	} `json:"license_batch"`
	Next           []int `json:"next_research_todos"`
	NextHistorical *bool `json:"next_research_todos_historical"`
	Proof          struct {
		Primary      int      `json:"primary"`
		Bytes        int      `json:"primary_bytes"`
		Notices      int      `json:"individual_notices"`
		Support      int      `json:"support_metadata"`
		Saved        int      `json:"support_saved"`
		Read         int      `json:"support_full_read"`
		Scan         int      `json:"support_scan_only"`
		Unsaved      int      `json:"support_unsaved"`
		ContextCount int      `json:"outside_package_context_count"`
		Context      []string `json:"outside_package_context_paths"`
		Verified     int      `json:"saved_source_path_occurrences_verified"`
		Reran        *bool    `json:"helper_reexecuted"`
	} `json:"next10_fixed_verification"`
	Actors struct {
		Fingerprint string `json:"primary_path_reader_fingerprint"`
		Read        int    `json:"full_read_by_preserved"`
		Scan        int    `json:"scan_by"`
		Unread      int    `json:"unread_with_no_actor"`
		Removed     int    `json:"old_ambiguous_read_by_removed"`
	} `json:"actor_attribution_correction"`
	Entries []struct {
		Path     string `json:"path"`
		Evidence struct {
			Job   string `json:"job"`
			SHA   string `json:"source_sha256"`
			Bytes int    `json:"bytes"`
		} `json:"parent_official_mcp_primary_reread_evidence"`
	} `json:"entries"`
}

func validateNext10Projection(a sixmileAssessment, p next10Projection) error {
	primary, bodyBytes, notices, support, saved, read, scan, unread := 0, 0, 0, 0, 0, 0, 0, 0
	for _, e := range a.Entries {
		if e.Reader != "20261024-095453-todo-3554" {
			for _, s := range e.Support {
				if s.ReadBy != nil || s.ScanBy != nil {
					return errors.New("next10 projection: historical legacy actors were invented")
				}
			}
			continue
		}
		primary++
		bodyBytes += e.Bytes
		if e.License.Read && e.License.Path != "" {
			notices++
		}
		for _, s := range e.Support {
			support++
			if s.Saved {
				saved++
			}
			if s.Read {
				read++
			}
			if s.Scan {
				scan++
			}
			if !s.Saved {
				unread++
			}
		}
		if e.Path == "skills/aabbcc456aa/hxl-code-reviewer/SKILL.md" {
			matches := 0
			for _, r := range p.Entries {
				if r.Path == e.Path {
					matches++
					if r.Evidence.Job != "20261024-093456-todo-3080" || r.Evidence.SHA != e.SHA || r.Evidence.Bytes != e.Bytes {
						return errors.New("next10 projection: parent official primary evidence differs")
					}
				}
			}
			if matches != 1 {
				return errors.New("next10 projection: parent primary evidence missing or repeated")
			}
		}
	}
	f := p.Proof
	if !falseReceipt(f.Reran) || !trueReceipt(p.Batch.Historical) || !trueReceipt(p.NextHistorical) || !reflect.DeepEqual(p.Next, []int{3565, 3566}) || f.Primary != primary || f.Bytes != bodyBytes || f.Notices != notices || f.Support != support || f.Saved != saved || f.Read != read || f.Scan != scan || f.Unsaved != unread || f.ContextCount != len(f.Context) || !reflect.DeepEqual(f.Context, []string{"README.md", "skills/ai-chen2050/moltrade/_meta.json"}) || f.Verified != primary+saved+len(f.Context) {
		return errors.New("next10 projection: fixed source-path and read-scope counters differ")
	}
	r := p.Actors
	if r.Fingerprint != sixmileNext10Actors || r.Read != read || r.Scan != scan || r.Unread != unread || r.Removed != scan+unread {
		return errors.New("next10 projection: actor correction counters differ")
	}
	return nil
}

func TestNext10HistoricalProjectionsDeriveFromTheFixedReadingCohort(t *testing.T) {
	for _, kind := range []string{"valid", "saved_counter", "verified_occurrences", "actor_removed", "legacy_actor", "primary_evidence", "historical_missing"} {
		t.Run(kind, func(t *testing.T) {
			a, _ := loadSixmileProvenanceFixture(t)
			var p next10Projection
			loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &p)
			switch kind {
			case "saved_counter":
				p.Proof.Saved++
			case "verified_occurrences":
				p.Proof.Verified++
			case "actor_removed":
				p.Actors.Removed--
			case "historical_missing":
				p.NextHistorical = nil
			case "primary_evidence":
				for i := range p.Entries {
					if p.Entries[i].Path == "skills/aabbcc456aa/hxl-code-reviewer/SKILL.md" {
						p.Entries[i].Evidence.SHA = strings.Repeat("e", 64)
						break
					}
				}
			case "legacy_actor":
				changed := false
				for i := range a.Entries {
					e := &a.Entries[i]
					if e.Reader != "20261024-095453-todo-3554" && len(e.Support) > 0 {
						actor := "invented"
						e.Support[0].ReadBy = &actor
						changed = true
						break
					}
				}
				if !changed {
					t.Fatal("legacy fixture has no support")
				}
			}
			err := validateNext10Projection(a, p)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if kind != "valid" && !strings.Contains(err.Error(), "next10 projection:") {
				t.Fatalf("mutation failed for another reason: %v", err)
			}
		})
	}
}
