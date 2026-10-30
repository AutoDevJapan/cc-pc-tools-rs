package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

const visiblePrimaryJournalFile = "skills-3080-visible-primary-assessments.json"

func visibleJournalNumber(value int64) json.Number { return json.Number(strconv.FormatInt(value, 10)) }

func visibleJournalGitHubID(id string) bool {
	return strings.HasPrefix(id, "github:") && validHex(strings.TrimPrefix(id, "github:"), 64)
}

func visibleJournalKeys(v map[string]any, keys ...string) bool {
	if len(v) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := v[key]; !ok {
			return false
		}
	}
	return true
}

func validateVisiblePrimaryJournal(v map[string]any, expectedRows int) error {
	if !visibleJournalKeys(v, "domain", "catalog_sha256", "representative_body_groups", "bound_member_metadata", "entries", "all_member_contexts_read", "all_member_rights_confirmed", "new_copy_grants", "new_final_dispositions", "central_operations", "whole16_complete", "scope") {
		return fmt.Errorf("public journal header includes missing or operational fields")
	}
	if v["domain"] != "skills3080-visible-primary-assessment-journal-v1" || v["catalog_sha256"] != "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c" || v["all_member_contexts_read"] != false || v["all_member_rights_confirmed"] != false || v["whole16_complete"] != false {
		return fmt.Errorf("journal identity, coverage or rights boundary changed")
	}
	for _, key := range []string{"new_copy_grants", "new_final_dispositions", "central_operations"} {
		if n, ok := parent19PhysicalNumber(v[key]); !ok || n != 0 {
			return fmt.Errorf("assessment gained authority: %s", key)
		}
	}
	rows, ok := v["entries"].([]any)
	declared, declaredOK := parent19PhysicalNumber(v["representative_body_groups"])
	if !ok || !declaredOK || len(rows) != expectedRows || int64(len(rows)) != declared || len(rows) == 0 {
		return fmt.Errorf("complete fixed journal rows required")
	}
	seen := map[string]bool{}
	seenMembers := map[string]bool{}
	seenBodies := map[string]bool{}
	seenBlobs := map[string]bool{}
	members := int64(0)
	for _, value := range rows {
		r, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("journal entry object required")
		}
		if !visibleJournalKeys(r, "inventory_id", "source_repo", "source_commit", "path", "source_sha256", "git_blob", "bytes", "source_url", "use", "value", "findings", "required_changes", "positive", "negative", "unmet", "read", "member_metadata", "member_occurrences", "status", "each_member_context_and_rights_pending", "new_final_disposition", "support_body_reads", "new_copy_grants", "central_operations") {
			return fmt.Errorf("public entry includes missing or operational fields")
		}
		id, _ := r["inventory_id"].(string)
		repo, _ := r["source_repo"].(string)
		commit, _ := r["source_commit"].(string)
		path, _ := r["path"].(string)
		sha, _ := r["source_sha256"].(string)
		blob, _ := r["git_blob"].(string)
		if !visibleJournalGitHubID(id) || seen[id] || !executionRepositoryName.MatchString(repo) || !validHex(commit, 40) || !validHex(sha, 64) || !validHex(blob, 40) || !evidencePath(path) {
			return fmt.Errorf("fixed unique source tuple required")
		}
		seen[id] = true
		if size, ok := parent19PhysicalNumber(r["bytes"]); !ok || size == 0 {
			return fmt.Errorf("actual primary byte count required")
		}
		if seenBodies[sha] || seenBlobs[blob] {
			return fmt.Errorf("one representative group required per primary body")
		}
		seenBodies[sha] = true
		seenBlobs[blob] = true
		parts := strings.Split(path, "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		if r["source_url"] != "https://github.com/"+repo+"/blob/"+commit+"/"+strings.Join(parts, "/") {
			return fmt.Errorf("public source URL differs from fixed tuple")
		}
		if r["status"] != "visible_primary_evaluated" || r["each_member_context_and_rights_pending"] != true || r["new_final_disposition"] != false {
			return fmt.Errorf("visible meaning became an adoption or full package read")
		}
		for _, key := range []string{"support_body_reads", "new_copy_grants", "central_operations"} {
			if n, ok := parent19PhysicalNumber(r[key]); !ok || n != 0 {
				return fmt.Errorf("entry gained support reading or authority")
			}
		}
		for _, key := range []string{"use", "value", "positive", "negative"} {
			if text, ok := r[key].(string); !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("meaning missing: %s", key)
			}
		}
		for _, key := range []string{"findings", "required_changes", "unmet"} {
			list, ok := r[key].([]any)
			if !ok || len(list) == 0 {
				return fmt.Errorf("complete meaning/limits list required")
			}
			for _, value := range list {
				if text, ok := value.(string); !ok || strings.TrimSpace(text) == "" {
					return fmt.Errorf("empty meaning or limit")
				}
			}
		}
		read, ok := r["read"].(map[string]any)
		if !ok || read["raw_full"] != false || read["unseen_meanings_remain_unknown"] != true || read["mask_version"] != "primaryflow-v4-opaque-lexical-boundaries" {
			return fmt.Errorf("fixed visible read profile required")
		}
		if !visibleJournalKeys(read, "failed_view_calls", "mask_version", "ranges", "raw_full", "total_physical_lines", "unseen_meanings_remain_unknown", "view_calls_returned", "whole_masked_view_sha256") {
			return fmt.Errorf("public read profile includes unsupported fields")
		}
		ranges, ok := read["ranges"].([]any)
		if !ok {
			return fmt.Errorf("physical ranges missing")
		}
		withheld := int64(0)
		for _, value := range ranges {
			span, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("range object required")
			}
			if !visibleJournalKeys(span, "first", "through", "visible_lines", "withheld_physical_lines", "range_sha256") {
				return fmt.Errorf("complete supported physical range required")
			}
			positions, ok := span["withheld_physical_lines"].([]any)
			if !ok {
				return fmt.Errorf("explicit hidden line positions required")
			}
			withheld += int64(len(positions))
			digest, _ := span["range_sha256"].(string)
			if !validHex(digest, 64) {
				return fmt.Errorf("range hash missing")
			}
		}
		noticeShape := map[string]any{"physical_lines": read["total_physical_lines"], "withheld_lines": visibleJournalNumber(withheld), "ranges": ranges, "unseen_meanings_remain_unknown": true, "raw_full": false}
		if err := parent19NoticeRanges(noticeShape); err != nil {
			return err
		}
		if calls, ok := parent19PhysicalNumber(read["view_calls_returned"]); !ok || calls != int64(len(ranges)) {
			return fmt.Errorf("all actual returned ranges required")
		}
		if failures, ok := parent19PhysicalNumber(read["failed_view_calls"]); !ok || failures != 0 {
			return fmt.Errorf("failed view cannot be reported as complete")
		}
		whole, _ := read["whole_masked_view_sha256"].(string)
		if !validHex(whole, 64) {
			return fmt.Errorf("whole masked view hash missing")
		}
		bound, ok := r["member_metadata"].([]any)
		count, countOK := parent19PhysicalNumber(r["member_occurrences"])
		if !ok || !countOK || len(bound) == 0 || int64(len(bound)) != count {
			return fmt.Errorf("complete pending member tuples required")
		}
		memberIDs := map[string]bool{}
		representative := false
		for _, value := range bound {
			m, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("member source tuple missing")
			}
			if !visibleJournalKeys(m, "inventory_id", "repo", "commit", "path", "sha256", "git_blob", "bytes") {
				return fmt.Errorf("member tuple includes missing or private metadata")
			}
			mid, _ := m["inventory_id"].(string)
			mrepo, _ := m["repo"].(string)
			mc, _ := m["commit"].(string)
			mp, _ := m["path"].(string)
			if !visibleJournalGitHubID(mid) || memberIDs[mid] || seenMembers[mid] || !executionRepositoryName.MatchString(mrepo) || !validHex(mc, 40) || !evidencePath(mp) || m["sha256"] != sha || m["git_blob"] != blob || m["bytes"] != r["bytes"] {
				return fmt.Errorf("same primary bytes and fixed member identity required")
			}
			memberIDs[mid] = true
			seenMembers[mid] = true
			if mid == id {
				if mrepo != repo || mc != commit || mp != path {
					return fmt.Errorf("representative member differs")
				}
				representative = true
			}
		}
		if !representative {
			return fmt.Errorf("representative absent from member metadata")
		}
		members += count
		for _, key := range []string{"reader_job", "consumer_job", "publisher_job", "original_packet_sha256", "independent_native_metadata", "independent_parent_ack", "content_path"} {
			if _, exists := r[key]; exists {
				return fmt.Errorf("operational metadata belongs in retained private evidence")
			}
		}
	}
	declaredMembers, ok := parent19PhysicalNumber(v["bound_member_metadata"])
	if !ok || declaredMembers != members {
		return fmt.Errorf("journal member units changed")
	}
	return nil
}

func TestVisiblePrimaryJournalPreservesHiddenLinesPendingRightsAndMemberUnits(t *testing.T) {
	var manifest ExecutionManifest
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &manifest)
	var pin ExecutionArtifact
	for _, candidate := range manifest.Artifacts {
		if candidate.File == visiblePrimaryJournalFile {
			if pin.File != "" {
				t.Fatal("duplicate public journal input")
			}
			pin = candidate
		}
	}
	if pin.File == "" {
		t.Fatal("public journal missing from actual execution inputs")
	}
	load := func() map[string]any {
		var ignored any
		raw := loadPublicReceipt(t, visiblePrimaryJournalFile, &ignored)
		if travel3Digest(raw) != pin.SHA {
			t.Fatal("journal bytes differ from manifest")
		}
		if _, e := uniqueEvidenceJSON(raw); e != nil {
			t.Fatal(e)
		}
		v, e := parent19Decode(raw)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	for _, change := range []string{"valid", "raw credit", "range gap", "repeated hidden line", "hidden count changed", "rights granted", "member omitted", "member rights transfer", "source URL changed", "source identity repeated", "member repeated across entries", "representative body repeated", "representative ID malformed", "member ID malformed", "blob reused with different SHA", "operational actor exposed", "member actor exposed", "header publisher exposed"} {
		t.Run(change, func(t *testing.T) {
			v := load()
			r := next4Object(v["entries"].([]any)[0])
			read := next4Object(r["read"])
			span := next4Object(read["ranges"].([]any)[0])
			switch change {
			case "raw credit":
				read["raw_full"] = true
			case "range gap":
				span["first"] = visibleJournalNumber(2)
			case "repeated hidden line":
				positions := span["withheld_physical_lines"].([]any)
				span["withheld_physical_lines"] = append(positions, positions[0])
			case "hidden count changed":
				span["visible_lines"] = visibleJournalNumber(0)
			case "rights granted":
				r["each_member_context_and_rights_pending"] = false
			case "member omitted":
				r["member_metadata"] = []any{}
			case "member rights transfer":
				r["new_copy_grants"] = visibleJournalNumber(1)
			case "source URL changed":
				r["source_url"] = "https://example.invalid/same-primary"
			case "source identity repeated":
				rows := v["entries"].([]any)
				rows[1] = rows[0]
			case "member repeated across entries":
				other := next4Object(v["entries"].([]any)[1])
				member := map[string]any{}
				for key, value := range next4Object(other["member_metadata"].([]any)[0]) {
					member[key] = value
				}
				member["inventory_id"] = r["inventory_id"]
				other["member_metadata"] = append(other["member_metadata"].([]any), member)
				count, _ := parent19PhysicalNumber(other["member_occurrences"])
				other["member_occurrences"] = visibleJournalNumber(count + 1)
				total, _ := parent19PhysicalNumber(v["bound_member_metadata"])
				v["bound_member_metadata"] = visibleJournalNumber(total + 1)
			case "representative body repeated":
				other := next4Object(v["entries"].([]any)[1])
				for _, key := range []string{"source_sha256", "git_blob", "bytes", "read"} {
					other[key] = r[key]
				}
				for _, value := range other["member_metadata"].([]any) {
					member := next4Object(value)
					member["sha256"] = r["source_sha256"]
					member["git_blob"] = r["git_blob"]
					member["bytes"] = r["bytes"]
				}
			case "representative ID malformed":
				id := r["inventory_id"]
				r["inventory_id"] = "github:not-a-digest"
				for _, value := range r["member_metadata"].([]any) {
					member := next4Object(value)
					if member["inventory_id"] == id {
						member["inventory_id"] = r["inventory_id"]
					}
				}
			case "member ID malformed":
				changed := false
				for _, value := range v["entries"].([]any) {
					entry := next4Object(value)
					for _, value := range entry["member_metadata"].([]any) {
						member := next4Object(value)
						if member["inventory_id"] != entry["inventory_id"] {
							member["inventory_id"] = "not-a-catalog-id"
							changed = true
						}
					}
				}
				if !changed {
					t.Fatal("nonrepresentative member fixture required")
				}
			case "blob reused with different SHA":
				other := next4Object(v["entries"].([]any)[1])
				other["git_blob"], other["bytes"] = r["git_blob"], r["bytes"]
				for _, value := range other["member_metadata"].([]any) {
					member := next4Object(value)
					member["git_blob"], member["bytes"] = r["git_blob"], r["bytes"]
				}
			case "operational actor exposed":
				r["reader_job"] = "internal-run"
			case "member actor exposed":
				next4Object(r["member_metadata"].([]any)[0])["reader_job"] = "internal-run"
			case "header publisher exposed":
				v["publisher_job"] = "internal-run"
			}
			err := validateVisiblePrimaryJournal(v, pin.Rows)
			if (err == nil) != (change == "valid") {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
