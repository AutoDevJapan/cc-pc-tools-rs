package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const visiblePrimaryJournalFile = "skills-3080-visible-primary-assessments.json"

func visibleJournalNumber(value int64) json.Number { return json.Number(strconv.FormatInt(value, 10)) }

func visibleJournalGitHubID(id string) bool {
	return strings.HasPrefix(id, "github:") && validHex(strings.TrimPrefix(id, "github:"), 64)
}

// Independent anchor: the accepted journal at 4ebfedab3794ff12e0f0be09243be3fd3544107a,
// file SHA88eb360eb1dccd1055f73fdf3c6b144ef33590428e31b8e5cbafd73efdb2869d.
// Canonical JSON preserves all values and entry order, without checkout spacing.
func visibleAcceptedPrefixMatches(entries []any) bool {
	if len(entries) < 54 {
		return false
	}
	raw, err := json.Marshal(entries[:54])
	return err == nil && travel3Digest(raw) == "b5276adcc6bc7709955f3481d0baf7c570a1577a0f550f1eb16f134950ab492c"
}

// The journal accepted at 379d3daa3bb6940f516b4957d40038a5564921df has 112
// entries and file SHA2395e2b04ba2ec8bdc63f933f886bd7246caabf8daf618e90db4bd034cb5a4de.
// Keep the newer accepted suffix as well as the original 54-entry anchor.
func visibleAccepted112PrefixMatches(entries []any) bool {
	if len(entries) < 112 || !visibleAcceptedPrefixMatches(entries) {
		return false
	}
	raw, err := json.Marshal(entries[:112])
	return err == nil && travel3Digest(raw) == "b9035a43e567eccd8464223d0f00405f6e7f96303c01771effe2046b44233948"
}

// The next 29 entries are anchored to the original received public projection,
// SHA b01feaa0838c25cbeb538e702f74d7281e3b3872892e1dcc9d96dd567a53cb88.
// This anchor is independent of a caller's manifest, row count or summary.
func visibleLatest29DeltaMatches(entries []any, groups, members, latestRecords, latestMembers int) bool {
	if len(entries) != 141 || groups != 141 || members != 162 || latestRecords != 29 || latestMembers != 44 || !visibleAccepted112PrefixMatches(entries) {
		return false
	}
	raw, err := json.Marshal(entries[112:141])
	return err == nil && travel3Digest(raw) == "fb9d6283fc8d300c92587ee4fd3f3ef91fec890d186a9fba49d77aeec3c34ce6"
}

// The accepted 146-entry prefix at main 33b5c4e99b726a170692d78d71d235642d063efd
// has file SHA 1fee59498adc33e309b3733d2353d98986f8e958e7c2da6e14faf0606aabd418.
// It includes the prior 141 entries and the separately received five-entry delta.
// The next 16 entries come from the independently received projection with
// file SHA 8e4c791a7c6edd43dde36d0186603390246363d5292fbcbfe3c5ef484c19ea3b.
// These pins are separate from the mutable publication manifest and counters.
var visibleCurrentSnapshotPin = struct {
	prefix, records, members, latestRecords, latestMembers int
	prefixSHA, deltaSHA                                    string
}{146, 162, 183, 16, 16,
	"5158a41c90663f73f9880c6e4225761b8674510e631bfcdd49f7a16ef44a04d2",
	"dbee42dbc135633f48c35622de64415661b573175430f59a9964157a0d83d2c9"}

func visibleCurrentPrefixMatches(entries []any) bool {
	pin := visibleCurrentSnapshotPin
	if len(entries) < pin.prefix {
		return false
	}
	raw, err := json.Marshal(entries[:pin.prefix])
	return err == nil && travel3Digest(raw) == pin.prefixSHA
}

func visibleCurrentSnapshotMatches(entries []any, groups, members, latestRecords, latestMembers int) bool {
	pin := visibleCurrentSnapshotPin
	if pin.records != pin.prefix+pin.latestRecords || len(entries) != pin.records || groups != pin.records || members != pin.members || latestRecords != pin.latestRecords || latestMembers != pin.latestMembers || !visibleCurrentPrefixMatches(entries) {
		return false
	}
	raw, err := json.Marshal(entries[pin.prefix:])
	return err == nil && travel3Digest(raw) == pin.deltaSHA
}

func visibleCurrentQueuePointerMatches(progress, summary map[string]any) bool {
	plan, ok := progress["current_execution_plan"].(map[string]any)
	if !ok || plan["current_queue_rollup_field"] != "current_execution_queue" {
		return false
	}
	current, ok := progress["current_execution_queue"].(map[string]any)
	if !ok {
		return false
	}
	actual, err := json.Marshal(current)
	expected, otherErr := json.Marshal(summary)
	return err == nil && otherErr == nil && string(actual) == string(expected)
}

func TestVisiblePrimaryCurrentQueuePointerMatchesPublishedSummary(t *testing.T) {
	for _, change := range []string{"valid", "legacy pointer with matching value", "missing pointer", "missing target", "target is not an object", "queue hash changed", "record count changed"} {
		t.Run(change, func(t *testing.T) {
			var ignored any
			progress, err := parent19Decode(loadPublicReceipt(t, "skills-3080-progress.json", &ignored))
			if err != nil {
				t.Fatal(err)
			}
			summary, err := parent19Decode(loadPublicReceipt(t, "skills-3080-execution-queue-summary.json", &ignored))
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "legacy pointer with matching value":
				progress["current_execution_plan"].(map[string]any)["current_queue_rollup_field"] = "latest_retained_procedure_queue"
				progress["latest_retained_procedure_queue"] = progress["current_execution_queue"]
			case "missing pointer":
				delete(progress["current_execution_plan"].(map[string]any), "current_queue_rollup_field")
			case "missing target":
				delete(progress, "current_execution_queue")
			case "target is not an object":
				progress["current_execution_queue"] = []any{}
			case "queue hash changed":
				progress["current_execution_queue"].(map[string]any)["queue_sha256"] = strings.Repeat("0", 64)
			case "record count changed":
				progress["current_execution_queue"].(map[string]any)["occurrences_with_assessment_records"] = visibleJournalNumber(0)
			}
			if visibleCurrentQueuePointerMatches(progress, summary) != (change == "valid") {
				t.Fatal("the current queue reference must select the exact published summary")
			}
		})
	}
}

func TestVisiblePrimaryJournalRetainsCurrentIndependentSnapshot(t *testing.T) {
	for _, change := range []string{"valid", "new value changed", "new order changed", "new entry missing", "new entry omitted with adjusted header", "new member metadata changed", "new tail with adjusted header", "new member total changed", "latest count changed", "latest member count changed", "base value changed"} {
		t.Run(change, func(t *testing.T) {
			var ignored any
			raw := loadPublicReceipt(t, visiblePrimaryJournalFile, &ignored)
			v, err := parent19Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			entries := v["entries"].([]any)
			pin := visibleCurrentSnapshotPin
			groups, members, latestRecords, latestMembers := len(entries), pin.members, pin.latestRecords, pin.latestMembers
			last := len(entries) - 1
			switch change {
			case "new value changed":
				entries[last].(map[string]any)["value"] = "Different meaningful text with the same valid shape."
			case "new order changed":
				entries[last-1], entries[last] = entries[last], entries[last-1]
			case "new entry missing":
				entries = entries[:last]
			case "new entry omitted with adjusted header":
				entries = entries[:last]
				groups, latestRecords = groups-1, latestRecords-1
			case "new member metadata changed":
				entries[last].(map[string]any)["member_metadata"].([]any)[0].(map[string]any)["path"] = "skills/fixture/changed-same-shape/SKILL.md"
			case "new tail with adjusted header":
				entries = append(entries, entries[last])
				groups, members, latestRecords, latestMembers = groups+1, members+1, latestRecords+1, latestMembers+1
			case "new member total changed":
				members++
			case "latest count changed":
				latestRecords--
			case "latest member count changed":
				latestMembers--
			case "base value changed":
				entries[0].(map[string]any)["value"] = "A changed field in the previously accepted prefix."
			}
			if strings.HasPrefix(change, "new ") && !visibleCurrentPrefixMatches(entries) {
				t.Fatal("the current delta case must retain the full accepted prefix")
			}
			if visibleCurrentSnapshotMatches(entries, groups, members, latestRecords, latestMembers) != (change == "valid") {
				t.Fatal("the current snapshot differs from the independently pinned received data")
			}
		})
	}
}

func TestVisiblePrimaryJournalRetainsOriginalLatest29(t *testing.T) {
	for _, change := range []string{"valid", "new value changed", "new order changed", "new entry missing", "new entry omitted with adjusted header", "new member metadata changed", "new tail with adjusted header", "new member total changed", "latest count changed", "latest member count changed"} {
		t.Run(change, func(t *testing.T) {
			var ignored any
			raw := loadPublicReceipt(t, visiblePrimaryJournalFile, &ignored)
			v, err := parent19Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			entries := v["entries"].([]any)
			// Keep this historical 29-entry regression fixed after later appends.
			entries = entries[:141]
			groups, members, latestRecords, latestMembers := len(entries), 162, 29, 44
			switch change {
			case "new value changed":
				entries[140].(map[string]any)["value"] = "Different meaningful text that still passes the structural validator."
			case "new order changed":
				entries[139], entries[140] = entries[140], entries[139]
			case "new entry missing":
				entries = entries[:140]
			case "new entry omitted with adjusted header":
				entries = entries[:140]
				groups, latestRecords = 140, 28
			case "new member metadata changed":
				member := entries[140].(map[string]any)["member_metadata"].([]any)[0].(map[string]any)
				member["path"] = "skills/fixture/changed-same-shape/SKILL.md"
			case "new tail with adjusted header":
				entries = append(entries, entries[140])
				groups, members = 142, 164
			case "new member total changed":
				members = 163
			case "latest count changed":
				latestRecords = 28
			case "latest member count changed":
				latestMembers = 43
			}
			if strings.HasPrefix(change, "new ") && !visibleAccepted112PrefixMatches(entries) {
				t.Fatal("the delta regression must leave the older accepted prefix intact")
			}
			if visibleLatest29DeltaMatches(entries, groups, members, latestRecords, latestMembers) != (change == "valid") {
				t.Fatal("the newly received delta was changed, reordered, omitted or counted differently")
			}
		})
	}
}

func TestVisiblePrimaryJournalRetainsNewerAcceptedSuffix(t *testing.T) {
	for _, change := range []string{"valid", "suffix value changed", "suffix order changed", "suffix missing"} {
		t.Run(change, func(t *testing.T) {
			var ignored any
			raw := loadPublicReceipt(t, visiblePrimaryJournalFile, &ignored)
			v, err := parent19Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			entries := v["entries"].([]any)
			switch change {
			case "suffix value changed":
				entries[111].(map[string]any)["value"] = "A different nonempty meaning in the newer accepted suffix."
			case "suffix order changed":
				entries[110], entries[111] = entries[111], entries[110]
			case "suffix missing":
				entries = entries[:111]
			}
			if visibleAccepted112PrefixMatches(entries) != (change == "valid") {
				t.Fatal("newer accepted history value or order drift was not detected")
			}
		})
	}
}

func TestVisiblePrimaryJournalRetainsEveryAcceptedPrefixFieldAndOrder(t *testing.T) {
	for _, change := range []string{"valid", "value changed", "order changed", "entry missing"} {
		t.Run(change, func(t *testing.T) {
			var ignored any
			raw := loadPublicReceipt(t, visiblePrimaryJournalFile, &ignored)
			v, err := parent19Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			entries := v["entries"].([]any)
			switch change {
			case "value changed":
				entries[0].(map[string]any)["value"] = "A different nonempty meaning still passes structural validation."
			case "order changed":
				entries[0], entries[1] = entries[1], entries[0]
			case "entry missing":
				entries = entries[:53]
			}
			if visibleAcceptedPrefixMatches(entries) != (change == "valid") {
				t.Fatal("accepted history value or order drift was not detected")
			}
		})
	}
}

func TestVisiblePrimaryMarkdownCountsMatchTheExecutionSnapshot(t *testing.T) {
	var summary ExecutionSummary
	loadPublicReceipt(t, "skills-3080-execution-queue-summary.json", &summary)
	var progress struct {
		Issue       string `json:"issue"`
		SourceCount int    `json:"source_count"`
		CentralHold struct {
			Count int `json:"count"`
		} `json:"central_target_holds_current"`
		LatestDelta struct {
			Records int `json:"new_assessment_records"`
			Members int `json:"additional_member_metadata"`
		} `json:"latest_visible_primary_delta"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &progress)
	var journal struct {
		Groups  int `json:"representative_body_groups"`
		Members int `json:"bound_member_metadata"`
	}
	loadPublicReceipt(t, visiblePrimaryJournalFile, &journal)
	var baseline any
	journalRaw := loadPublicReceipt(t, visiblePrimaryJournalFile, &baseline)
	journalValue, decodeErr := parent19Decode(journalRaw)
	if decodeErr != nil || !visibleCurrentSnapshotMatches(journalValue["entries"].([]any), journal.Groups, journal.Members, progress.LatestDelta.Records, progress.LatestDelta.Members) {
		t.Fatal("the current latest delta differs from the independently received records and member metadata")
	}
	if progress.LatestDelta.Records <= 0 || progress.LatestDelta.Records > journal.Groups || progress.LatestDelta.Members < progress.LatestDelta.Records || progress.LatestDelta.Members > journal.Members {
		t.Fatal("the current increment must be a nonempty subset of the cumulative journal")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "skills-3080-visible-primary-assessments.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		fmt.Sprintf("固定した出典に対する%d件の評価", journal.Groups),
		fmt.Sprintf("今回の追加は%d件", progress.LatestDelta.Records),
		fmt.Sprintf("今回%d件増えた", progress.LatestDelta.Members),
		fmt.Sprintf("初回54件からの追加は%d件", journal.Groups-54),
		fmt.Sprintf("候補メタデータは計%d件", journal.Members),
	} {
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("Markdown count differs from the current JSON snapshot: %s", expected)
		}
	}
	issue := strings.TrimPrefix(progress.Issue, "https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/")
	if _, err := strconv.Atoi(issue); err != nil || issue == progress.Issue {
		t.Fatal("the public issue reference is not a numbered repository issue")
	}
	digits := visibleMarkdownCount(summary.Records, true)
	expected := fmt.Sprintf("全件処理表は%s出現、評価%d、最終%d、新規%d・既存統合%d・旧除外%d。評価と最終の交差%d、和集合%d、評価済み未採否%d、意味と個別権利の確認が必要な候補%dを区別する。本文未取得%d・取得審査保留%d・未知の中央%d・旧除外%dの再確認と、指定%d資料の残作業を保持する。全体のIssue%sは継続する。",
		digits, summary.Assessments, summary.Final, summary.Decisions["installed_new_adaptation"], summary.Decisions["integrated_existing"], summary.Decisions["excluded"], summary.AssessmentAndFinal, summary.AssessmentOrFinal, summary.Stages["assessed_needs_current_disposition"], summary.Stages["needs_semantic_and_individual_rights_review"], summary.Stages["held_body_unavailable"], summary.Stages["held_execution_review"], progress.CentralHold.Count, summary.Decisions["excluded"], progress.SourceCount, issue)
	if !visibleMarkdownSummaryMatches(string(raw), expected) {
		t.Fatal("the complete Markdown summary differs from the JSON snapshot")
	}
	spans := regexp.MustCompile(`[0-9][0-9,]*`).FindAllStringIndex(expected, -1)
	if len(spans) != 16 {
		t.Fatal("the complete summary must expose exactly sixteen numeric fields")
	}
	for index, span := range spans {
		t.Run(fmt.Sprintf("numeric field %02d", index+1), func(t *testing.T) {
			original := expected[span[0]:span[1]]
			number, err := strconv.Atoi(strings.ReplaceAll(original, ",", ""))
			if err != nil {
				t.Fatal(err)
			}
			// Keep the original grouping style and every other byte unchanged.
			changed := visibleMarkdownCount(number+1, strings.Contains(original, ","))
			wrong := expected[:span[0]] + changed + expected[span[1]:]
			if visibleMarkdownSummaryMatches(wrong, expected) {
				t.Fatal("a changed numeric value passed with unchanged grouping style")
			}
		})
	}
	for _, change := range []string{"missing sentence", "duplicate paragraph"} {
		t.Run(change, func(t *testing.T) {
			wrong := expected
			switch change {
			case "missing sentence":
				start := strings.Index(wrong, "本文未取得")
				if start < 0 {
					t.Fatal("the hold sentence is absent from the baseline")
				}
				end := strings.Index(wrong[start:], "。")
				if end < 0 {
					t.Fatal("the hold sentence has no end")
				}
				wrong = wrong[:start] + wrong[start+end+len("。"):]
			case "duplicate paragraph":
				wrong += "\n\n" + expected
			}
			if visibleMarkdownSummaryMatches(wrong, expected) {
				t.Fatal("missing, stale or duplicated summary passed")
			}
		})
	}
}

func visibleMarkdownCount(number int, grouped bool) string {
	digits := strconv.Itoa(number)
	if grouped {
		for i := len(digits) - 3; i > 0; i -= 3 {
			digits = digits[:i] + "," + digits[i:]
		}
	}
	return digits
}

func visibleMarkdownSummaryMatches(markdown, expected string) bool {
	count := 0
	for _, paragraph := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n\n") {
		paragraph = strings.TrimSpace(paragraph)
		if strings.HasPrefix(paragraph, "全件処理表は") || strings.HasPrefix(paragraph, "全件queueは") {
			count++
			if paragraph != expected {
				return false
			}
		}
	}
	return count == 1
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
