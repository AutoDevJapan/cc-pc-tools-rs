package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executionRepositoryFixture(t *testing.T) (string, ExecutionManifest, ExecutionEvidence) {
	t.Helper()
	input, m, e := executionFixture()
	var out bytes.Buffer
	dec := json.NewDecoder(strings.NewReader(input))
	for i := 0; i < m.Records; i++ {
		var r Record
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.ID == "first" {
			r.Repo = "example/skills"
		}
		if err := json.NewEncoder(&out).Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	h := sha256.Sum256(out.Bytes())
	m.CatalogSHA = hex.EncodeToString(h[:])
	a := e.Assessments["first"]
	a.SourceRepo = "example/skills"
	e.Assessments["first"] = a
	return out.String(), m, e
}

func TestExecutionAssessmentRequiresCatalogRepository(t *testing.T) {
	for _, repo := range []string{"example/skills", "different/skills", ""} {
		t.Run("repository="+repo, func(t *testing.T) {
			input, m, e := executionRepositoryFixture(t)
			a := e.Assessments["first"]
			a.SourceRepo = repo
			e.Assessments["first"] = a
			_, err := executionQueue(strings.NewReader(input), &bytes.Buffer{}, m, e)
			if (err == nil) != (repo == "example/skills") {
				t.Fatalf("known catalog repository matched unverified assessment repository %q: %v", repo, err)
			}
		})
	}
}

func TestExecutionRepeatedAssessmentRepositoryCannotChange(t *testing.T) {
	for _, repo := range []string{"example/skills", "different/skills", ""} {
		t.Run("repository="+repo, func(t *testing.T) {
			_, manifest, root, _ := executionDiskFixture(t)
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var m ExecutionManifest
			if err = json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			_, _, e := executionRepositoryFixture(t)
			a := e.Assessments["first"]
			for i, source := range []string{"example/skills", repo} {
				a.SourceRepo = source
				data, err = json.Marshal(struct {
					Entries []executionIdentity `json:"entries"`
				}{[]executionIdentity{a}})
				if err != nil {
					t.Fatal(err)
				}
				file := fmt.Sprintf("skills-3080-assessment-%d.json", i)
				if err = os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
					t.Fatal(err)
				}
				h := sha256.Sum256(data)
				m.Artifacts = append(m.Artifacts, ExecutionArtifact{File: file, SHA: hex.EncodeToString(h[:]), Rows: 1})
			}
			loaded, err := loadExecutionEvidence(root, m)
			if repo == "example/skills" {
				if err != nil || len(loaded.References["first"]) != 2 {
					t.Fatalf("matching independent references lost: %v", err)
				}
			} else if err == nil {
				t.Fatalf("conflicting assessment repository %q accepted", repo)
			}
		})
	}
}

func TestExecutionLegacyRepositoryFieldsPreserveExplicitProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		bad             bool
	}{
		{"source_repo", `{"source_repo":"example/skills"}`, "example/skills", false},
		{"repo", `{"repo":"example/skills"}`, "example/skills", false},
		{"source", `{"source":"example/skills"}`, "example/skills", false},
		{"equal_aliases", `{"source_repo":"example/skills","repo":"example/skills","source":"example/skills"}`, "example/skills", false},
		{"conflicting_repo", `{"source_repo":"example/skills","repo":"different/skills"}`, "", true},
		{"conflicting_source", `{"source_repo":"example/skills","source":"different/skills"}`, "", true},
		{"label_is_not_repository", `{"source":"saved public source"}`, "", false},
		{"url_is_not_repository", `{"source":"https://github.com/example/skills"}`, "", false},
		{"missing_is_unknown", `{}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r executionIdentity
			if err := json.Unmarshal([]byte(tc.raw), &r); err != nil {
				t.Fatal(err)
			}
			err := normalizeExecutionRepository(&r)
			if (err != nil) != tc.bad || !tc.bad && r.SourceRepo != tc.want {
				t.Fatalf("provenance changed or conflicting fields accepted: %+v / %v", r, err)
			}
		})
	}
}

func TestExecutionFinalDecisionRequiresKnownCatalogRepository(t *testing.T) {
	for _, repo := range []string{"example/skills", "different/skills", ""} {
		t.Run("repository="+repo, func(t *testing.T) {
			input, m, e := executionRepositoryFixture(t)
			d := e.Assessments["first"]
			d.SourceRepo = repo
			d.Disposition = "integrated_existing"
			e.Decisions["first"] = d
			_, err := executionQueue(strings.NewReader(input), &bytes.Buffer{}, m, e)
			if (err == nil) != (repo == "example/skills") {
				t.Fatalf("final decision repository %q accepted without equality: %v", repo, err)
			}
		})
	}
}

func TestSiteAssessmentKeepsOnlyActuallyUnknownGitTuple(t *testing.T) {
	for _, change := range []string{"valid", "assessment repo known", "assessment commit known", "catalog path known", "catalog repo known", "non-site unit", "non-site identity"} {
		t.Run(change, func(t *testing.T) {
			_, manifest, root, _ := executionDiskFixture(t)
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var m ExecutionManifest
			if err = json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			r := Record{ID: "site:fixed-canonical", Unit: "site_index_entry", SHA: strings.Repeat("a", 64), BodyRead: true}
			a := executionIdentity{ID: r.ID, SHA: r.SHA}
			switch change {
			case "assessment repo known":
				a.SourceRepo = "example/skills"
			case "assessment commit known":
				a.SourceCommit = strings.Repeat("c", 40)
			case "catalog path known":
				r.Path = "actual/SKILL.md"
			case "catalog repo known":
				r.Repo = "example/skills"
			case "non-site unit":
				r.Unit = "github_skill_entry"
			case "non-site identity":
				r.ID = "github:fixed"
				a.ID = r.ID
			}
			data, err = json.Marshal(struct {
				Entries []executionIdentity `json:"entries"`
			}{[]executionIdentity{a}})
			if err != nil {
				t.Fatal(err)
			}
			file := "skills-3080-site.json"
			if err = os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(data)
			m.Artifacts = []ExecutionArtifact{{File: file, SHA: hex.EncodeToString(h[:]), Rows: 1}}
			catalog, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			catalog = append(catalog, '\n')
			h = sha256.Sum256(catalog)
			m.CatalogSHA = hex.EncodeToString(h[:])
			m.Records = 1
			e, err := loadExecutionEvidence(root, m)
			var output bytes.Buffer
			if err == nil {
				_, err = executionQueue(bytes.NewReader(catalog), &output, m, e)
			}
			if change != "valid" {
				if err == nil {
					t.Fatal("unknown site tuple exception accepted known or mismatched Git provenance")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result ExecutionRow
			if err = json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Repo != "" || result.Commit != "" || result.Path != "" || result.Stage != "assessed_needs_current_disposition" || len(result.References) != 1 {
				t.Fatal("unknown site tuple was invented or assessment reference lost")
			}
		})
	}
}

func TestSiteOccurrenceCannotInheritGitTupleFromAnotherRecord(t *testing.T) {
	for _, change := range []string{"valid unknown", "assessment path", "assessment repo", "assessment commit", "final path", "final repo", "final commit", "non-site unit with valid path", "valid known"} {
		t.Run(change, func(t *testing.T) {
			r := Record{ID: "site:canonical", Unit: "site_index_entry", SHA: strings.Repeat("a", 64), BodyRead: true}
			a := executionIdentity{ID: r.ID, SHA: r.SHA}
			d := a
			d.Disposition = "integrated_existing"
			switch change {
			case "assessment path":
				a.Path = "upstream/SKILL.md"
			case "assessment repo":
				a.Path = "upstream/SKILL.md"
				a.SourceRepo = "different/source"
			case "assessment commit":
				a.Path = "upstream/SKILL.md"
				a.Commit = strings.Repeat("c", 40)
			case "final path":
				d.Path = "upstream/SKILL.md"
			case "final repo":
				d.SourceRepo = "different/source"
			case "final commit":
				d.Commit = strings.Repeat("c", 40)
			case "non-site unit with valid path":
				r.Unit = "github_skill_entry"
				a.Path = "upstream/SKILL.md"
			case "valid known":
				r.Repo = "example/skills"
				r.Commit = strings.Repeat("c", 40)
				r.Path = "actual/SKILL.md"
				a.SourceRepo = r.Repo
				a.Commit = r.Commit
				a.Path = r.Path
				d = a
				d.Disposition = "integrated_existing"
			}
			catalog, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			catalog = append(catalog, '\n')
			h := sha256.Sum256(catalog)
			m := ExecutionManifest{CatalogSHA: hex.EncodeToString(h[:]), Records: 1}
			e := ExecutionEvidence{Assessments: map[string]executionIdentity{r.ID: a}, Decisions: map[string]executionIdentity{r.ID: d}, References: map[string][]ExecutionReference{}}
			_, err = executionQueue(bytes.NewReader(catalog), &bytes.Buffer{}, m, e)
			valid := change == "valid unknown" || change == "valid known"
			if (err == nil) != valid {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}

func TestExplicitHistoricalSiteIdentityRequiresPinnedRowAndCatalogKind(t *testing.T) {
	for _, change := range []string{"valid archive", "valid legacy claim", "valid historical exclusion", "missing interpretation", "wrong recorded path", "missing occurrence", "occurrence filled", "duplicate row", "wrong row", "wrong id", "wrong hash", "wrong binding unit", "wrong role", "wrong catalog unit", "catalog tuple known", "legacy claim cannot install"} {
		t.Run(change, func(t *testing.T) {
			_, manifest, root, _ := executionDiskFixture(t)
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var m ExecutionManifest
			if err = json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			r := Record{ID: "site:historical", Unit: "site_index_entry", SHA: strings.Repeat("a", 64), BodyRead: true}
			a := executionIdentity{ID: r.ID, SHA: r.SHA, Path: "site/objects/" + r.SHA + ".gz", PathKind: "fixed_saved_archive_relative_path_not_upstream_path"}
			b := ExecutionSiteBinding{Row: 1, ID: r.ID, SHA: r.SHA, Unit: "site_index_entry", Role: "saved_archive_location_not_upstream_git", Occurrence: &executionGitIdentity{}}
			final := change == "valid historical exclusion" || change == "legacy claim cannot install"
			if change == "valid legacy claim" || final {
				a.SourceRepo = "example/skills"
				a.Commit = strings.Repeat("c", 40)
				a.Path = "skills/example/SKILL.md"
				a.PathKind = ""
				b.Role = "legacy_upstream_source_claim_unverified"
			}
			if final {
				a.Disposition = "excluded"
			}
			if change == "legacy claim cannot install" {
				a.Disposition = "installed_new_adaptation"
			}
			b.Recorded = executionGitIdentity{a.SourceRepo, a.Commit, a.Path}
			switch change {
			case "wrong recorded path":
				b.Recorded.Path = "other/SKILL.md"
			case "missing occurrence":
				b.Occurrence = nil
			case "occurrence filled":
				b.Occurrence.Repo = "example/skills"
			case "wrong row":
				b.Row = 2
			case "wrong id":
				b.ID = "site:another"
			case "wrong hash":
				b.SHA = strings.Repeat("b", 64)
			case "wrong binding unit":
				b.Unit = "github_skill_entry"
			case "wrong role":
				b.Role = "grant_all_sources"
			case "wrong catalog unit":
				r.Unit = "github_skill_entry"
			case "catalog tuple known":
				r.Path = "known/SKILL.md"
			}
			if final {
				data, err = json.Marshal(struct {
					Entries  []executionIdentity `json:"entries"`
					Excluded []executionIdentity `json:"excluded_entries"`
				}{[]executionIdentity{}, []executionIdentity{a}})
			} else {
				data, err = json.Marshal(struct {
					Entries []executionIdentity `json:"entries"`
				}{[]executionIdentity{a}})
			}
			if err != nil {
				t.Fatal(err)
			}
			file := "skills-3080-historical-site.json"
			if err = os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(data)
			pin := ExecutionArtifact{File: file, SHA: hex.EncodeToString(h[:]), Rows: 1, SiteBindings: []ExecutionSiteBinding{b}}
			if change == "missing interpretation" {
				pin.SiteBindings = nil
			}
			if change == "duplicate row" {
				pin.SiteBindings = append(pin.SiteBindings, b)
			}
			if final {
				m.Ledger = pin
			} else {
				m.Artifacts = []ExecutionArtifact{pin}
			}
			catalog, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			catalog = append(catalog, '\n')
			h = sha256.Sum256(catalog)
			m.CatalogSHA = hex.EncodeToString(h[:])
			m.Records = 1
			e, err := loadExecutionEvidence(root, m)
			var out bytes.Buffer
			if err == nil {
				_, err = executionQueue(bytes.NewReader(catalog), &out, m, e)
			}
			valid := strings.HasPrefix(change, "valid ")
			if (err == nil) != valid {
				t.Fatalf("%s: %v", change, err)
			}
			if valid {
				var row ExecutionRow
				if err = json.Unmarshal(out.Bytes(), &row); err != nil {
					t.Fatal(err)
				}
				var observed *ExecutionSiteBinding
				if final {
					observed = row.FinalIdentity
				} else if len(row.References) == 1 {
					observed = row.References[0].Identity
				}
				if row.Repo != "" || row.Commit != "" || row.Path != "" || observed == nil || observed.Role != b.Role || observed.Recorded.Path != a.Path {
					t.Fatal("original observation lost or unknown occurrence Git fields invented")
				}
				actual, err := os.ReadFile(filepath.Join(root, file))
				if err != nil || !bytes.Equal(actual, data) {
					t.Fatal("historical artifact mutated")
				}
			}
		})
	}
}

func TestPublishedSiteInterpretationsKeepOriginalArtifactsAndUnverifiedClaims(t *testing.T) {
	var proof struct {
		OriginalLedger string                 `json:"original_ledger_sha256"`
		Artifacts      []ExecutionArtifact    `json:"assessment_artifact_interpretations"`
		Final          []ExecutionSiteBinding `json:"ledger_occurrence_interpretations"`
		Catalog        []Record               `json:"actual_catalog_occurrences"`
		Unverified     bool                   `json:"unverified_legacy_source_claims_not_validated_or_promoted"`
	}
	if travel3Digest(loadPublicReceipt(t, "skills-3080-site-identity-interpretation095653.json", &proof)) != "8e22d4ecfd06cf973732cd7bc31c7e790b5b44182437016b31dc1bffe64c9a0e" {
		t.Fatal("independent original interpretation proof changed")
	}
	var manifest ExecutionManifest
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &manifest)
	var originalLedger, currentLedger map[string]any
	if travel3Digest(loadPublicReceipt(t, "skills-3080-adoptions-before-deploy4-095653.json", &originalLedger)) != proof.OriginalLedger {
		t.Fatal("independent original ledger bytes changed")
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &currentLedger)
	if err := validateLedgerAppendScope(originalLedger, currentLedger, loadPublishedLedgerExtensions(t)); err != nil {
		t.Fatal(err)
	}
	if !proof.Unverified || len(proof.Artifacts) != 4 || len(proof.Final) != 6 || len(proof.Catalog) != 14 {
		t.Fatal("original source claim or ledger preservation boundary changed")
	}
	catalog := map[string]Record{}
	for _, r := range proof.Catalog {
		if r.Unit != "site_index_entry" || r.Repo != "" || r.Commit != "" || r.Path != "" || !r.BodyRead || catalog[r.ID].ID != "" {
			t.Fatal("recorded actual catalog tuple or identity changed")
		}
		catalog[r.ID] = r
	}
	check := func(bindings []ExecutionSiteBinding) {
		for _, b := range bindings {
			r := catalog[b.ID]
			if r.ID == "" || r.SHA != b.SHA || b.Occurrence == nil || *b.Occurrence != (executionGitIdentity{}) {
				t.Fatal("recorded source identity filled actual unknown catalog Git fields")
			}
		}
	}
	count := 0
	for _, pin := range proof.Artifacts {
		found := 0
		for _, actual := range manifest.Artifacts {
			if actual.File != pin.File {
				continue
			}
			found++
			a, _ := json.Marshal(actual)
			b, _ := json.Marshal(pin)
			if !bytes.Equal(a, b) {
				t.Fatal("original pinned artifact interpretation omitted or changed")
			}
		}
		if found != 1 {
			t.Fatal("original pinned source absent or repeated")
		}
		check(pin.SiteBindings)
		count += len(pin.SiteBindings)
	}
	if count != 9 {
		t.Fatal("nine historical assessment references required")
	}
	// Appending accepted entries moves the later excluded rows. Preserve the
	// original identities and source claims, and derive only their new position.
	positions := map[string]int{}
	rows := append(append([]any{}, currentLedger["entries"].([]any)...), currentLedger["excluded_entries"].([]any)...)
	for i, value := range rows {
		positions[next4Object(value)["inventory_id"].(string)] = i + 1
	}
	expected := append([]ExecutionSiteBinding{}, proof.Final...)
	for i := range expected {
		position := positions[expected[i].ID]
		if position == 0 {
			t.Fatal("historical final occurrence missing")
		}
		expected[i].Row = position
	}
	a, _ := json.Marshal(manifest.Ledger.SiteBindings)
	b, _ := json.Marshal(expected)
	if !bytes.Equal(a, b) {
		t.Fatal("historical final interpretations changed")
	}
	check(proof.Final)
}

func validateAppendOnlyAdoptionLedger(original, current map[string]any) error {
	oldEntries, oldOK := original["entries"].([]any)
	entries, ok := current["entries"].([]any)
	if !oldOK || !ok || len(entries) < len(oldEntries) || len(current) != len(original) {
		return fmt.Errorf("original adoption ledger structure or prefix removed")
	}
	equal := func(a, b any) bool {
		x, e := json.Marshal(a)
		y, f := json.Marshal(b)
		return e == nil && f == nil && bytes.Equal(x, y)
	}
	if !equal(oldEntries, entries[:len(oldEntries)]) {
		return fmt.Errorf("historical accepted entries changed")
	}
	mutable := map[string]bool{"entries": true, "installed_new": true, "integrated": true, "pending_occurrences": true, "pending_available_body_occurrences": true}
	for key, value := range original {
		if !mutable[key] && !equal(value, current[key]) {
			return fmt.Errorf("historical ledger field changed: %s", key)
		}
	}
	installed, integrated := 0, 0
	for _, value := range entries {
		switch next4Object(value)["disposition"] {
		case "installed_new_adaptation":
			installed++
		case "integrated_existing":
			integrated++
		default:
			return fmt.Errorf("unfinished entry in accepted ledger")
		}
	}
	excluded, ok := current["excluded_entries"].([]any)
	if !ok {
		return fmt.Errorf("historical exclusions missing")
	}
	pending := 73220 - len(entries) - len(excluded)
	if current["installed_new"] != float64(installed) || current["integrated"] != float64(integrated) || current["pending_occurrences"] != float64(pending) || current["pending_available_body_occurrences"] != float64(pending-428-647) {
		return fmt.Errorf("appended ledger counters differ from actual rows and retained holds")
	}
	return nil
}

// New ledger entries must come from explicitly listed, hash-pinned result
// artifacts. Later accepted cohorts extend data in this list rather than
// loosening the original prefix or adding another hard-coded count to Go.
func loadPublishedLedgerExtensions(t *testing.T) map[string][2]string {
	t.Helper()
	var descriptor struct {
		Artifacts    []ExecutionArtifact `json:"assessment_artifacts"`
		Publications []ExecutionArtifact `json:"append_only_ledger_publications"`
	}
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &descriptor)
	if len(descriptor.Publications) == 0 {
		t.Fatal("explicit published ledger extensions required")
	}
	allowed := map[string][2]string{}
	files := map[string]bool{}
	for _, pin := range descriptor.Publications {
		if files[pin.File] || pin.Rows < 1 {
			t.Fatal("duplicate or empty ledger extension artifact")
		}
		files[pin.File] = true
		matched := 0
		for _, actual := range descriptor.Artifacts {
			if actual.File == pin.File {
				if actual.SHA != pin.SHA || actual.Rows != pin.Rows {
					t.Fatal("ledger extension differs from independently pinned assessment artifact")
				}
				matched++
			}
		}
		if matched != 1 {
			t.Fatal("ledger extension not uniquely registered in assessment evidence")
		}
		var publication struct {
			Entries []struct {
				ID          string `json:"inventory_id"`
				Target      string `json:"target"`
				Disposition string `json:"disposition"`
				Final       bool   `json:"final_disposition_recorded"`
			} `json:"entries"`
		}
		if travel3Digest(loadPublicReceipt(t, pin.File, &publication)) != pin.SHA || len(publication.Entries) != pin.Rows {
			t.Fatal("complete published ledger extension bytes/count changed")
		}
		for _, entry := range publication.Entries {
			if entry.ID == "" || entry.Target == "" || !entry.Final || (entry.Disposition != "installed_new_adaptation" && entry.Disposition != "integrated_existing") || allowed[entry.ID] != ([2]string{}) {
				t.Fatal("ledger extension identity missing, repeated or unfinished")
			}
			allowed[entry.ID] = [2]string{entry.Target, entry.Disposition}
		}
	}
	return allowed
}

func validateLedgerAppendScope(original, current map[string]any, allowed map[string][2]string) error {
	if err := validateAppendOnlyAdoptionLedger(original, current); err != nil {
		return err
	}
	prior := original["entries"].([]any)
	tail := current["entries"].([]any)[len(prior):]
	if len(tail) != len(allowed) {
		return fmt.Errorf("ledger extension includes missing or unreviewed occurrences")
	}
	seen := map[string]bool{}
	for _, value := range tail {
		row := next4Object(value)
		id, _ := row["inventory_id"].(string)
		want, ok := allowed[id]
		if !ok || seen[id] || row["target"] != want[0] || row["disposition"] != want[1] {
			return fmt.Errorf("ledger extension differs from fixed published result scope")
		}
		seen[id] = true
	}
	return nil
}

func TestAdoptionAppendKeepsHistoricalEntriesExclusionsAndActualCounters(t *testing.T) {
	var original map[string]any
	if travel3Digest(loadPublicReceipt(t, "skills-3080-adoptions-before-deploy4-095653.json", &original)) != "f2dddb6f978ef1b1690e54d12e855dac0df6e0b3ab40cfad2f572c8208ada4c7" {
		t.Fatal("fixed original ledger changed")
	}
	allowed := loadPublishedLedgerExtensions(t)
	for _, change := range []string{"valid", "historical accepted target changed", "historical exclusion removed", "invented pending count", "additional held source with matching counters"} {
		t.Run(change, func(t *testing.T) {
			var current map[string]any
			loadPublicReceipt(t, "skills-3080-adoptions.json", &current)
			switch change {
			case "historical accepted target changed":
				next4Object(current["entries"].([]any)[0])["target"] = "other"
			case "historical exclusion removed":
				current["excluded_entries"] = current["excluded_entries"].([]any)[1:]
			case "invented pending count":
				current["pending_occurrences"] = float64(0)
			case "additional held source with matching counters":
				row := map[string]any{}
				for key, value := range next4Object(current["entries"].([]any)[179]) {
					row[key] = value
				}
				row["inventory_id"] = "site:notion-meeting-intelligence-88fc92"
				row["target"] = "notion-meeting-intelligence"
				current["entries"] = append(current["entries"].([]any), row)
				current["integrated"] = current["integrated"].(float64) + 1
				current["pending_occurrences"] = current["pending_occurrences"].(float64) - 1
				current["pending_available_body_occurrences"] = current["pending_available_body_occurrences"].(float64) - 1
			}
			err := validateLedgerAppendScope(original, current, allowed)
			if (err == nil) != (change == "valid") {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}

func TestExecutionAssessmentAndLedgerRejectConflictingPathAndCommitAliases(t *testing.T) {
	for _, stage := range []string{"assessment", "ledger"} {
		for _, change := range []string{"valid primary", "valid equal aliases", "valid source aliases", "wrong path alias", "wrong commit alias", "wrong both aliases"} {
			t.Run(stage+"/"+change, func(t *testing.T) {
				_, manifest, root, _ := executionDiskFixture(t)
				raw, err := os.ReadFile(manifest)
				if err != nil {
					t.Fatal(err)
				}
				var m ExecutionManifest
				if err = json.Unmarshal(raw, &m); err != nil {
					t.Fatal(err)
				}
				catalog, _, e := executionFixture()
				a := e.Assessments["first"]
				switch change {
				case "valid equal aliases":
					a.SourcePath = a.Path
					a.SourceCommit = a.Commit
				case "valid source aliases":
					a.SourcePath = a.Path
					a.SourceCommit = a.Commit
					a.Path = ""
					a.Commit = ""
				case "wrong path alias":
					a.SourcePath = "different/SKILL.md"
				case "wrong commit alias":
					a.SourceCommit = strings.Repeat("d", 40)
				case "wrong both aliases":
					a.SourcePath = "different/SKILL.md"
					a.SourceCommit = strings.Repeat("d", 40)
				}
				if stage == "ledger" {
					a.Disposition = "excluded"
				}
				raw, err = json.Marshal(struct {
					Entries []executionIdentity `json:"entries"`
				}{[]executionIdentity{a}})
				if err != nil {
					t.Fatal(err)
				}
				name := "skills-3080-aliases.json"
				if err = os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
				h := sha256.Sum256(raw)
				pin := ExecutionArtifact{File: name, SHA: hex.EncodeToString(h[:]), Rows: 1}
				if stage == "ledger" {
					m.Ledger = pin
				} else {
					m.Artifacts = []ExecutionArtifact{pin}
				}
				loaded, err := loadExecutionEvidence(root, m)
				if err == nil {
					_, err = executionQueue(strings.NewReader(catalog), &bytes.Buffer{}, m, loaded)
				}
				if (err == nil) != strings.HasPrefix(change, "valid ") {
					t.Fatalf("%s/%s: %v", stage, change, err)
				}
			})
		}
	}
}

func TestSameSiteOccurrenceCanRetainArchiveAndDirectUnknownReferences(t *testing.T) {
	for _, change := range []string{"same occurrence", "different repo"} {
		t.Run(change, func(t *testing.T) {
			_, manifest, root, _ := executionDiskFixture(t)
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var m ExecutionManifest
			if err = json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			r := Record{ID: "site:same", Unit: "site_index_entry", SHA: strings.Repeat("a", 64), BodyRead: true}
			a := executionIdentity{ID: r.ID, SHA: r.SHA}
			for i := 0; i < 2; i++ {
				row := a
				pin := ExecutionArtifact{File: fmt.Sprintf("skills-3080-same-site-%d.json", i), Rows: 1}
				if i == 0 {
					row.Path = "site/objects/" + r.SHA + ".gz"
					row.PathKind = "fixed_saved_archive_relative_path_not_upstream_path"
					pin.SiteBindings = []ExecutionSiteBinding{{Row: 1, ID: r.ID, SHA: r.SHA, Unit: r.Unit, Role: "saved_archive_location_not_upstream_git", Recorded: executionGitIdentity{Path: row.Path}, Occurrence: &executionGitIdentity{}}}
				} else if change == "different repo" {
					row.SourceRepo = "other/source"
					row.Path = "upstream/SKILL.md"
				}
				raw, err = json.Marshal(struct {
					Entries []executionIdentity `json:"entries"`
				}{[]executionIdentity{row}})
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(root, pin.File), raw, 0600); err != nil {
					t.Fatal(err)
				}
				h := sha256.Sum256(raw)
				pin.SHA = hex.EncodeToString(h[:])
				m.Artifacts = append(m.Artifacts, pin)
			}
			catalog, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			catalog = append(catalog, '\n')
			h := sha256.Sum256(catalog)
			m.CatalogSHA = hex.EncodeToString(h[:])
			m.Records = 1
			e, err := loadExecutionEvidence(root, m)
			var output bytes.Buffer
			if err == nil {
				_, err = executionQueue(bytes.NewReader(catalog), &output, m, e)
			}
			if change != "same occurrence" {
				if err == nil {
					t.Fatal("known repository was treated as unknown site identity")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result ExecutionRow
			if err = json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.References) != 2 || result.References[0].Identity == nil || result.References[1].Identity != nil || result.References[0].Identity.Recorded.Path == "" || result.Path != "" {
				t.Fatal("different observations of the same occurrence were erased or conflated")
			}
		})
	}
}
