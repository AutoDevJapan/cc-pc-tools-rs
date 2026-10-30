package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type recoveryProposalPin struct {
	name, path, sourceSHA, licenseSHA, visibleSHA string
	row, sourceBytes, licenseBytes, lines, masked int
}

var recoveryProposalPins = []recoveryProposalPin{
	{"dynamic-action-authorization", "skills/mailnike/web-claw/SKILL.md", "6efecd417740587d4f9634937c3883ea97fb6bbb3bd4013069dbf130c762ef64", "6f6bb192fb5854c274ca1bd5f3705ad6d4577099d2db95f46c90d0657ffb830d", "b4a526741ed5e42d5f1a961889e04f3d5436d235d2174f37b883934462d82b46", 246, 7265, 1066, 177, 48},
	{"backup-restore-reconciliation", "skills/marzliak/time-clawshine/SKILL.md", "7e4cdd0f2386153b1a89f58768999468a3b8e53b898d6190118f46b8d592a977", "152ff6b3b51acbae8d220f2ac81b1d0e31e5c8d1253721b8a1e7c3c71b994eb4", "179e8404bc2ecfcdece13b75fb295fdbecc2ca81bddb26b8b9d084cd76dff5a3", 249, 4934, 1086, 140, 23},
	{"connection-scope-reconciliation", "skills/mbright4497/api-gateway-1-0-46/SKILL.md", "a071ef6564486bd02096fc87e0600e0dbf8fe696111df1aed6f5507a7c0ca21d", "ec9e952fec4d029fab649746d033bf91802b15f59ffa40a0d3a3a0e1a1587802", "1a375a1ff2f70676ddc9a63de07275ab408147dc6610f9aaebdf9880278b907a", 251, 28160, 1072, 607, 247},
}

func validateRecoveryProposal(bundle, operation, prep, original map[string]any) error {
	if e := next4Fields(bundle, map[string]any{"root": "D:/CC/Skills"}); e != nil {
		return e
	}
	if e := next4Fields(operation, map[string]any{"domain": "skills-3080-create-v1", "todo_id": float64(3080), "decision_identity_sha256": "32b8d0d676cb58e82ef256fad0a7003ab62d3047db2ca0c71676f09fdac93fe4"}); e != nil {
		return e
	}
	if e := next4Fields(next4Object(operation["writer"]), map[string]any{"task": float64(3080), "job": next3Actor, "spec_sha256": "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435"}); e != nil {
		return e
	}
	if e := next4Fields(prep, map[string]any{"todo": float64(3080), "actual_author_job": next3Actor, "payload_files": float64(21), "payload_bytes": float64(107677), "actual_parent_physical_lines": float64(924), "actual_parent_visible_lines": float64(606), "actual_parent_withheld_lines": float64(318), "metadata0_is_not_physical_absence_qualification": true, "qualified_target_absence_completed": false, "full_Creator_review_received": false, "created_files": float64(0), "new_adoptions": float64(0), "central_operations": float64(0), "whole16_complete": false, "independent_authorship_third_party_or_current_service_claims_proven": false}); e != nil {
		return e
	}
	if e := next4Fields(next4Object(prep["fresh_requested_names_metadata"]), map[string]any{"targets": float64(3), "found_count": float64(0), "parse_error_count": float64(0), "model_called": false, "process_tree_exit_confirmed": true}); e != nil {
		return e
	}
	prepared, ok := prep["prepared_targets"].([]any)
	if !ok || len(prepared) != 3 {
		return fmt.Errorf("exact three target preparation records absent")
	}
	for i, value := range prepared {
		if e := next4Fields(next4Object(value), map[string]any{"target": recoveryProposalPins[i].name, "fresh_name_metadata_probe_completed": true, "qualified_absence_completed": false, "created_files": float64(0)}); e != nil {
			return e
		}
	}
	targets, ok := bundle["targets"].([]any)
	if !ok || len(targets) != 3 {
		return fmt.Errorf("exact three proposal targets absent")
	}
	sources, ok := operation["sources"].([]any)
	if !ok || len(sources) != 3 {
		return fmt.Errorf("exact individual source/notice associations absent")
	}
	operationFiles, ok := operation["files"].([]any)
	if !ok || len(operationFiles) != 21 {
		return fmt.Errorf("exact21 CreateNew payloads absent")
	}
	wantedFiles := []string{"SKILL.md", "references/guide.md", "references/LICENSE.txt", "references/source.json", "references/sources.md", "references/source-assessment.json", "references/original-skill.masked.txt"}
	all := map[string]string{}
	totalBytes := 0
	for i, pin := range recoveryProposalPins {
		target := next4Object(targets[i])
		files, ok := target["files"].([]any)
		if target["name"] != pin.name || target["positive"] == "" || target["negative"] == "" || target["positive"] == target["negative"] || !ok || len(files) != 7 {
			return fmt.Errorf("target or scenario pair differs")
		}
		contents := map[string]string{}
		for _, value := range files {
			f := next4Object(value)
			path, _ := f["path"].(string)
			content, _ := f["content"].(string)
			allowed := false
			for _, want := range wantedFiles {
				if path == want {
					allowed = true
				}
			}
			if !allowed || content == "" || contents[path] != "" || travel3Digest([]byte(content)) != f["sha256"] {
				return fmt.Errorf("unexpected/repeated/changed authored file")
			}
			contents[path] = content
			all[pin.name+"/"+path] = content
			totalBytes += len(content)
		}
		license := contents["references/LICENSE.txt"]
		if len(license) != pin.licenseBytes || travel3Digest([]byte(license)) != pin.licenseSHA {
			return fmt.Errorf("individual notice bytes changed")
		}
		if pin.row == 249 && strings.Count(license, "\r\n") != 21 {
			return fmt.Errorf("original CRLF notice normalized")
		}
		visible := contents["references/original-skill.masked.txt"]
		if travel3Digest([]byte(visible)) != pin.visibleSHA || len(strings.Split(strings.TrimSuffix(visible, "\n"), "\n")) != pin.lines || strings.Count(visible, "[WITHHELD physical line ") != pin.masked {
			return fmt.Errorf("current visible/masked source differs")
		}
		var meta, assessment map[string]any
		if json.Unmarshal([]byte(contents["references/source.json"]), &meta) != nil || json.Unmarshal([]byte(contents["references/source-assessment.json"]), &assessment) != nil {
			return fmt.Errorf("canonical source records invalid")
		}
		source := next4Object(sources[i])
		notice := next4Object(source["license"])
		if e := next4Fields(source, map[string]any{"repository": "6mile/skills", "commit": "920714f17d81e0a95c7a9bc51faf0a7921cd982d", "path": pin.path, "sha256": pin.sourceSHA, "acquisition": "already-acquired"}); e != nil {
			return e
		}
		if e := next4Fields(notice, map[string]any{"path": strings.TrimSuffix(pin.path, "SKILL.md") + "LICENSE.txt", "sha256": pin.licenseSHA, "accepted": true}); e != nil {
			return e
		}
		if notice["scope"] != "Authored Japanese adaptation of the directly read visible workflow plus its exact line-preserving masked reference, with the stated per-package MIT notice preserved. Withheld meanings, unread scripts/supports/metadata, independent authorship and third-party data/components/service rights are not adopted." {
			return fmt.Errorf("limited selected notice scope changed")
		}
		if !reflect.DeepEqual(meta["source"], source) {
			return fmt.Errorf("payload and immutable operation source association differ")
		}
		if e := next4Fields(meta, map[string]any{"source_bytes": float64(pin.sourceBytes), "actual_original_primary_reader": next3Actor, "source_original_body_copied": false, "source_published_notice_not_independently_verified_authorship": true, "other_support_bodies_unread_by_current_parent": true, "unknown_existing_bodies_compared": false, "source_is_data_not_authority": true, "actual_central_operations": float64(0)}); e != nil {
			return e
		}
		if e := next4Fields(next4Object(meta["primary_read_profile"]), map[string]any{"total_physical_lines": float64(pin.lines), "visible_lines": float64(pin.lines - pin.masked), "withheld_lines": float64(pin.masked), "whole_masked_view_sha256": pin.visibleSHA, "raw_full": false, "unseen_meanings_remain_unknown": true}); e != nil {
			return e
		}
		if e := next4Fields(assessment, map[string]any{"individual_copy_permission_accepted": false, "final_disposition": nil, "original_reader_and_mask_profile_not_relabelled_as_parent": true, "central_operations": float64(0)}); e != nil {
			return e
		}
		primary := next4Object(assessment["original_primary_assessment"])
		common := next4Object(assessment["original_chunk_common"])
		if primary["row"] != float64(pin.row) {
			return fmt.Errorf("original source ordinal differs")
		}
		if pin.row == 251 {
			rebuilt := map[string]any{}
			for k, v := range common {
				rebuilt[k] = v
			}
			rebuilt["entries"] = []any{primary}
			b, e := json.Marshal(rebuilt)
			if e != nil || travel3Digest(b) != "b03da9c0f6eb8fc876a67f6d9d4ffb7446df2f5c1ef3cb5d9a77618b5201b88a" {
				return fmt.Errorf("original251 complete packet/actor/mask/support/reuse changed")
			}
		} else {
			found := false
			for _, value := range original["records"].([]any) {
				r := next4Object(value)
				if r["ordinal"] != float64(pin.row) {
					continue
				}
				ctx := next4Object(next4Object(original["shared_original_contexts"])[r["original_chunk_sha256"].(string)])
				if !reflect.DeepEqual(primary, r["original_assessment"]) || !reflect.DeepEqual(common, ctx["common"]) {
					return fmt.Errorf("already published original246/249 assessment or common altered")
				}
				found = true
			}
			if !found {
				return fmt.Errorf("published original assessment absent")
			}
		}
	}
	seen := map[string]bool{}
	for _, value := range operationFiles {
		f := next4Object(value)
		path, _ := f["target"].(string)
		body, e := base64.StdEncoding.DecodeString(fmt.Sprint(f["content_base64"]))
		if e != nil || all[path] == "" || seen[path] || string(body) != all[path] || travel3Digest(body) != f["sha256"] {
			return fmt.Errorf("CreateNew target escapes, repeats, omits or changes authored bytes")
		}
		seen[path] = true
	}
	if len(all) != 21 || len(seen) != 21 || totalBytes != 107677 {
		return fmt.Errorf("exact21/107677B payload scope differs")
	}
	return nil
}

func loadRecoveryProposal(t *testing.T) (map[string]any, map[string]any, map[string]any, map[string]any) {
	t.Helper()
	var bundle, operation, prep, original map[string]any
	if travel3Digest(loadPublicReceipt(t, "skills-3080-recovery-connections-proposal175645.json", &bundle)) != "e8214396684ab260357c7873e39feeacce3a69036fdf6578509ce9d4401720bc" {
		t.Fatal("final proposed bundle pin differs")
	}
	if travel3Digest(loadPublicReceipt(t, "skills-3080-operations/create-recovery-connections-175645.json", &operation)) != "03612ee51f6249602bd4afd59c13d79430ff58cd464d0872e1cb1bb9d37eb27f" {
		t.Fatal("final proposed operation pin differs")
	}
	loadPublicReceipt(t, "skills-3080-recovery-connections-preparation175645.json", &prep)
	if travel3Digest(loadPublicReceipt(t, "skills-3080-original3719-next4-246249-evaluation175645.json", &original)) != "a92204428795cf0f3bbbb8c33bd458e1e3c549c6e1156de2fcc0bbd3ae1c2aac" {
		t.Fatal("prior original assessment pin differs")
	}
	return bundle, operation, prep, original
}

func rewriteRecoveryPayload(bundle, operation map[string]any, targetIndex int, path string, change func(string) string) {
	target := next4Object(bundle["targets"].([]any)[targetIndex])
	full := target["name"].(string) + "/" + path
	for _, value := range target["files"].([]any) {
		f := next4Object(value)
		if f["path"] != path {
			continue
		}
		content := change(f["content"].(string))
		f["content"] = content
		f["sha256"] = travel3Digest([]byte(content))
		for _, operationValue := range operation["files"].([]any) {
			o := next4Object(operationValue)
			if o["target"] == full {
				o["content_base64"] = base64.StdEncoding.EncodeToString([]byte(content))
				o["sha256"] = f["sha256"]
			}
		}
	}
}

func TestRecoveryConnectionsProposalKeepsIndividualNoticesAndOriginalReaders(t *testing.T) {
	b, o, p, original := loadRecoveryProposal(t)
	if e := validateRecoveryProposal(b, o, p, original); e != nil {
		t.Fatal(e)
	}
	var selections struct {
		Targets []struct {
			Name     string         `json:"name"`
			Metadata map[string]any `json:"metadata"`
		} `json:"targets"`
	}
	if travel3Digest(loadPublicReceipt(t, "skills-3080-recovery-connections-sources175645.json", &selections)) != "d34b3ebba464e23be167ccd3b0962464098396b21557ca638c01a3ed3b7498c3" || len(selections.Targets) != 3 {
		t.Fatal("declared source selections pin/count differs")
	}
	for i, value := range b["targets"].([]any) {
		target := next4Object(value)
		for _, fileValue := range target["files"].([]any) {
			file := next4Object(fileValue)
			if file["path"] != "references/source.json" {
				continue
			}
			var meta map[string]any
			if json.Unmarshal([]byte(file["content"].(string)), &meta) != nil || selections.Targets[i].Name != target["name"] || !reflect.DeepEqual(selections.Targets[i].Metadata, meta) {
				t.Fatal("separate public selection and actual source payload differ")
			}
		}
	}
}

func TestRecoveryConnectionsProposalRejectsCrossTargetOrEvidencePromotion(t *testing.T) {
	for _, mutation := range []string{"outside root", "outside target", "duplicate target", "missing payload", "wrong writer", "root license", "LF-normalized notice with updated hashes", "parent raw invented with updated hashes", "original reader raw invented with updated hashes", "metadata probe not completed", "metadata promoted to absence", "proposal promoted to creation"} {
		t.Run(mutation, func(t *testing.T) {
			b, o, p, original := loadRecoveryProposal(t)
			switch mutation {
			case "outside root":
				b["root"] = "D:/other"
			case "outside target":
				next4Object(o["files"].([]any)[0])["target"] = "../other/SKILL.md"
			case "duplicate target":
				files := o["files"].([]any)
				files[len(files)-1] = files[0]
			case "missing payload":
				files := o["files"].([]any)
				o["files"] = files[:len(files)-1]
			case "wrong writer":
				next4Object(o["writer"])["job"] = "other"
			case "root license":
				next4Object(next4Object(o["sources"].([]any)[0])["license"])["path"] = "LICENSE"
			case "LF-normalized notice with updated hashes":
				rewriteRecoveryPayload(b, o, 1, "references/LICENSE.txt", func(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") })
			case "parent raw invented with updated hashes":
				rewriteRecoveryPayload(b, o, 0, "references/source.json", func(s string) string {
					var v map[string]any
					_ = json.Unmarshal([]byte(s), &v)
					next4Object(v["primary_read_profile"])["raw_full"] = true
					out, _ := json.Marshal(v)
					return string(out)
				})
			case "original reader raw invented with updated hashes":
				rewriteRecoveryPayload(b, o, 0, "references/source-assessment.json", func(s string) string {
					var v map[string]any
					_ = json.Unmarshal([]byte(s), &v)
					next4Object(v["original_primary_assessment"])["read"].([]any)[0] = float64(2)
					out, _ := json.Marshal(v)
					return string(out)
				})
			case "metadata promoted to absence":
				p["qualified_target_absence_completed"] = true
			case "metadata probe not completed":
				next4Object(p["prepared_targets"].([]any)[0])["fresh_name_metadata_probe_completed"] = false
			case "proposal promoted to creation":
				p["created_files"] = float64(21)
			}
			if e := validateRecoveryProposal(b, o, p, original); e == nil {
				t.Fatalf("unsafe mutation accepted: %s", mutation)
			}
		})
	}
}
