package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"testing"
)

const originalSevenDuplicates = "e08f794faa99ab467326dc0e05cc0e9ac5e6b0dde79bb01fd24688dbe1cc7c7f"
const originalOneRights = "e422a04f065da493f0117dc9a54af735fb4d7e180cddfd119148609d438c7a04"
const restrictionExclusionType = "individual_reuse_restriction_permission_not_established"

type restrictedCopyDecision struct {
	individualRightsDecision
	Blob        string `json:"license_git_blob"`
	PrimaryRead *bool  `json:"primary_fulltext_read_confirmed"`
	NoticeBy    string `json:"individual_notice_read_by"`
	Evidence    struct {
		Artifact string `json:"artifact"`
		ID       string `json:"inventory_id"`
		ReadBy   string `json:"read_by"`
		Raw      *bool  `json:"raw_full_read"`
	} `json:"primary_read_evidence"`
}

func validateRestrictedCopyDecisions(rows []restrictedCopyDecision, extra, next20 extra45Assessment, amendment primaryReadingAmendments) error {
	wanted := map[string]bool{
		"github:80e883eeb6770b068abcf5bb5ef1410cb87ddd5e94ba52c2a55c06b6dea6a7df": true,
		"github:26a03a3d50b59afbdef18bf936dffa118faeb3853c50b16f619a223b1acd70f8": true,
		amendedPrimaryID: true,
		"github:741cc025c76afb1abd990520f1cc34337bd14258cd765a44ee3bc43a6256abc2": true,
	}
	if len(rows) != 4 {
		return errors.New("restricted copy decisions: four fixed occurrences are not recorded")
	}
	known := map[string]extra45Entry{}
	artifacts := map[string]string{}
	for i, group := range [][]extra45Entry{extra.Entries, next20.Entries} {
		for _, e := range group {
			known[e.ID] = e
			name := "skills-3080-sixmile-next45-evaluation.json"
			if i == 1 {
				name = "skills-3080-sixmile-next20-evaluation.json"
			}
			artifacts[e.ID] = name
		}
	}
	seen := map[string]bool{}
	for _, d := range rows {
		e, ok := known[d.ID]
		if !wanted[d.ID] || seen[d.ID] || !ok || d.Source != "6mile/skills" || d.Commit != extra.Commit || d.Path != e.Path || d.SHA != e.SHA || !e.Excluded || d.Disposition != "excluded" || d.Type != restrictionExclusionType {
			return errors.New("restricted copy decisions: fixed identity or final source flag differs")
		}
		seen[d.ID] = true
		if d.Reason == "" || d.Scope == "" || !d.NoticeRead || d.NoticeBy != "20261024-123649-todo-3080" || d.LicensePath != path.Dir(e.Path)+"/LICENSE.txt" || d.LicensePath != e.Rights.Path || d.LicenseSHA != e.Rights.SHA || d.LicenseBytes != e.Rights.Bytes || d.LicenseSHA != "79f6d8f5b427252fa3b1c11ecdbdb6bf610b944f7530b4de78f770f38741cfaa" || d.LicenseBytes != 1467 || d.Blob != "c55ab42224874608473643de0a85736b7fec0730" || !x45False(d.CopyRights) || !x45False(d.Copied) || !x45False(d.Executed) {
			return errors.New("restricted copy decisions: notice evidence was lost, copied or treated as permission")
		}
		artifact, actor := artifacts[d.ID], x45String(e.ReadBy)
		if !x45True(e.Read) {
			if d.ID != amendedPrimaryID || len(amendment.Entries) != 1 || amendment.Entries[0].ID != d.ID || !x45True(amendment.Entries[0].New.Read) || !x45False(amendment.Entries[0].Original.Read) {
				return errors.New("restricted copy decisions: unread primary was excluded without its real reading amendment")
			}
			artifact = "skills-3080-primary-reading-amendments.json"
			actor = x45String(amendment.Entries[0].New.ReadBy)
		}
		if !x45True(d.PrimaryRead) || !x45True(d.Evidence.Raw) || d.Evidence.ID != d.ID || d.Evidence.Artifact != artifact || d.Evidence.ReadBy != actor || actor == "" {
			return errors.New("restricted copy decisions: primary reader evidence differs from the original or amendment")
		}
	}
	return nil
}

func TestNewIndividualReuseRestrictionDecisionsAreScopedAndDoNotCopy(t *testing.T) {
	for _, name := range []string{"valid", "missing_occurrence", "duplicate_occurrence", "duplicate_type", "wrong_notice", "notice_actor_lost", "grant_claimed", "copied_claimed", "execution_claimed", "parent_attributed_to_child", "old_unread_excluded"} {
		t.Run(name, func(t *testing.T) {
			var all struct {
				Entries []restrictedCopyDecision `json:"entries"`
			}
			var extra, next20 extra45Assessment
			var amendment primaryReadingAmendments
			loadPublicReceipt(t, "skills-3080-rights-decisions.json", &all)
			loadPublicReceipt(t, "skills-3080-sixmile-next45-evaluation.json", &extra)
			loadPublicReceipt(t, "skills-3080-sixmile-next20-evaluation.json", &next20)
			loadPublicReceipt(t, "skills-3080-primary-reading-amendments.json", &amendment)
			var rows []restrictedCopyDecision
			for _, r := range all.Entries {
				if r.Type == restrictionExclusionType {
					rows = append(rows, r)
				}
			}
			want := ""
			if name != "valid" && len(rows) != 4 {
				t.Fatal("fixed restriction fixture missing before mutation")
			}
			switch name {
			case "missing_occurrence":
				rows = rows[:3]
				want = "restricted copy decisions: four fixed occurrences are not recorded"
			case "duplicate_occurrence":
				rows[1] = rows[0]
				want = "restricted copy decisions: fixed identity or final source flag differs"
			case "duplicate_type":
				rows[0].Type = "exact_body_duplicate_of_existing_catalog_adoption"
				want = "restricted copy decisions: fixed identity or final source flag differs"
			case "wrong_notice":
				rows[0].LicenseSHA = "wrong"
				want = "restricted copy decisions: notice evidence was lost, copied or treated as permission"
			case "notice_actor_lost":
				rows[0].NoticeBy = ""
				want = "restricted copy decisions: notice evidence was lost, copied or treated as permission"
			case "grant_claimed", "copied_claimed", "execution_claimed":
				yes := true
				switch name {
				case "grant_claimed":
					rows[0].CopyRights = &yes
				case "copied_claimed":
					rows[0].Copied = &yes
				case "execution_claimed":
					rows[0].Executed = &yes
				}
				want = "restricted copy decisions: notice evidence was lost, copied or treated as permission"
			case "parent_attributed_to_child":
				for i := range rows {
					if rows[i].ID == amendedPrimaryID {
						rows[i].Evidence.ReadBy = "old-child"
					}
				}
				want = "restricted copy decisions: primary reader evidence differs from the original or amendment"
			case "old_unread_excluded":
				amendment.Entries = nil
				want = "restricted copy decisions: unread primary was excluded without its real reading amendment"
			}
			err := validateRestrictedCopyDecisions(rows, extra, next20, amendment)
			if want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != want {
				t.Fatalf("%s reached another guard: %v", name, err)
			}
		})
	}
}

func TestOriginalSevenDuplicatesAndSingleRightsDecisionRemainUnchanged(t *testing.T) {
	for _, tc := range []struct {
		Name, Want string
		Count      int
	}{{"skills-3080-duplicate-decisions.json", originalSevenDuplicates, 7}, {"skills-3080-rights-decisions.json", originalOneRights, 1}} {
		var a struct {
			Entries []map[string]any `json:"entries"`
		}
		loadPublicReceipt(t, tc.Name, &a)
		if len(a.Entries) < tc.Count {
			t.Fatal("original decisions disappeared")
		}
		b, err := json.Marshal(a.Entries[:tc.Count])
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != tc.Want {
			t.Fatal("accepted original exclusion rows changed")
		}
	}
}

func TestTwoOldMCPSiblingDecisionsKeepTheirAcceptedCanonicalAndUncopiedStatus(t *testing.T) {
	var d struct {
		Entries []duplicateDecision `json:"entries"`
	}
	var l struct {
		Entries []canonicalAdoption `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-duplicate-decisions.json", &d)
	loadPublicReceipt(t, "skills-3080-adoptions.json", &l)
	wanted := map[string]string{"github:e2d31692e49225ac37aa9b2eac4d04b4db6ed6da177cdea2c1d53c45b7516e32": "skills/pupuking723/mcp-builder-anthropic/SKILL.md", "github:9a4792733ddd9ccbd3883f80adaa44d2f3214af2c971850d106896cf0238c55f": "skills/uniquevme/unique-mcp-builder-test/SKILL.md"}
	var found []duplicateDecision
	for _, r := range d.Entries {
		if p, ok := wanted[r.ID]; ok {
			if r.Path != p || r.SHA != "0f4592dcb53cf2b5d6b7febee6b4152018b565551a1c29e3c612f57b218ab295" || r.Bytes != 9092 || r.OriginalID != "github:e4ade76a7932447efe3dd6e777ab300a5a9a6f1cf13a392614b31c621906a016" {
				t.Fatal("old MCP occurrence identity differs")
			}
			found = append(found, r)
		}
	}
	if len(found) != 2 {
		t.Fatal("two previously evaluated MCP siblings not reflected")
	}
	if err := validateDuplicateDecisions(found, l.Entries); err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Acceptance struct {
			Merge string `json:"merge"`
		} `json:"canonical_public_acceptance"`
		Reused      *bool `json:"old_two_comparisons_reused"`
		Redelegated *bool `json:"old_two_redelegated"`
		Copied      *bool `json:"new_body_copied"`
		Executed    *bool `json:"source_scripts_executed"`
		Official    struct {
			Trees       int    `json:"git_tree_hashes_reconstructed"`
			Support     int    `json:"support_paths"`
			Fingerprint string `json:"support_fingerprint"`
			Equal       *bool  `json:"all_support_mode_path_blob_equal_canonical_hxl"`
			TextFetch   *int   `json:"source_text_body_fetches"`
			HeldFetch   *int   `json:"held647_fetches"`
			Executed    *bool  `json:"source_code_executed"`
		} `json:"official_metadata_support_proof"`
	}
	loadPublicReceipt(t, "skills-3080-mcp-sibling-duplicate-evidence.json", &evidence)
	o := evidence.Official
	if evidence.Acceptance.Merge != "0801af4a04d9fb15b2772a5336f84f66ca2fd175" || !x45True(evidence.Reused) || !x45False(evidence.Redelegated) || !x45False(evidence.Copied) || !x45False(evidence.Executed) || o.Trees != 3 || o.Support != 8 || o.Fingerprint != "484bdc219079d5764d67e22bee92807b0c49da6889c439fbb3ed03e534874448" || !x45True(o.Equal) || o.TextFetch == nil || *o.TextFetch != 0 || o.HeldFetch == nil || *o.HeldFetch != 0 || !x45False(o.Executed) {
		t.Fatal("MCP sibling decision acquired an unverified copy, execution or canonical")
	}
}
