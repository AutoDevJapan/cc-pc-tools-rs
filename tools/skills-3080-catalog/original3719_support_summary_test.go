package main

import (
	"fmt"
	"strconv"
	"testing"
)

type original3719SupportCounts struct {
	total, saved, unsaved int
	states                map[string]int
}

// Count the original observations once, including inventory defaults that
// remain saved/unread. Consumers use the same result for expected coverage and
// the publication's declared summary; no body is reopened or credited here.
func original3719SupportSummary(value map[string]any) (original3719SupportCounts, error) {
	out := original3719SupportCounts{states: map[string]int{}}
	records, recordsOK := value["records"].([]any)
	contexts, contextsOK := value["shared_original_contexts"].(map[string]any)
	if !recordsOK || !contextsOK {
		return out, fmt.Errorf("original support records or contexts absent")
	}
	packetRows, packetOverrides := map[string]int{}, map[string]int{}
	for _, item := range records {
		r, ok := item.(map[string]any)
		if !ok {
			return out, fmt.Errorf("original support owner absent")
		}
		sha, shaOK := r["original_chunk_sha256"].(string)
		a, assessmentOK := r["original_assessment"].(map[string]any)
		if !shaOK || !validHex(sha, 64) || !assessmentOK {
			return out, fmt.Errorf("original support packet identity absent")
		}
		supports, ok := a["support"].([]any)
		if !ok {
			return out, fmt.Errorf("original support observations absent")
		}
		packetRows[sha]++
		packetOverrides[sha] += len(supports)
		for _, item := range supports {
			tuple, ok := item.([]any)
			if !ok || len(tuple) != 8 {
				return out, fmt.Errorf("original support tuple differs")
			}
			state, ok := tuple[4].(float64)
			if !ok || state < 0 || state > 7 || state != float64(int(state)) {
				return out, fmt.Errorf("original support state differs")
			}
			out.states[strconv.Itoa(int(state))]++
			out.total++
			if state == 0 {
				out.unsaved++
			} else {
				out.saved++
			}
		}
	}
	for sha, item := range contexts {
		context, ok := item.(map[string]any)
		if !ok {
			return out, fmt.Errorf("original support context differs")
		}
		common, ok := context["common"].(map[string]any)
		if !ok {
			return out, fmt.Errorf("original support context common fields absent")
		}
		if inventory, present := common["support_inventory"]; present {
			inv, ok := inventory.(map[string]any)
			if !ok || packetRows[sha] != 1 {
				return out, fmt.Errorf("inherited support inventory must own exactly one primary record")
			}
			count, err := original3719InheritedUnread(inv, packetOverrides[sha])
			if err != nil {
				return out, err
			}
			if count > 0 {
				out.states["1"] += count
			}
			out.total += count
			out.saved += count
		}
	}
	if value["support_metadata_records"] != float64(out.total) || value["saved_support_metadata"] != float64(out.saved) || value["unsaved_support_metadata"] != float64(out.unsaved) {
		return out, fmt.Errorf("declared support totals differ from the original records and inherited unread inventory")
	}
	declared, ok := value["original_support_state_counts"].(map[string]any)
	if !ok || len(declared) != len(out.states) {
		return out, fmt.Errorf("declared support state membership differs")
	}
	for state, count := range out.states {
		if declared[state] != float64(count) {
			return out, fmt.Errorf("declared support state count differs for state %s", state)
		}
	}
	return out, nil
}

func TestOriginal3719SupportDeclarationsStayBoundToOriginalRecords(t *testing.T) {
	for _, change := range []string{"valid", "total", "saved", "unsaved", "missing total", "fractional total", "string saved", "consistent stale totals", "moved state", "extra state", "missing state", "fractional state", "missing states"} {
		t.Run(change, func(t *testing.T) {
			var v map[string]any
			loadPublicReceipt(t, "skills-3080-original3719-next21-268288-evaluation175645.json", &v)
			switch change {
			case "total":
				v["support_metadata_records"] = float64(229)
			case "saved":
				v["saved_support_metadata"] = float64(95)
			case "unsaved":
				v["unsaved_support_metadata"] = float64(135)
			case "missing total":
				delete(v, "support_metadata_records")
			case "fractional total":
				v["support_metadata_records"] = 228.5
			case "string saved":
				v["saved_support_metadata"] = "94"
			case "consistent stale totals":
				v["support_metadata_records"], v["saved_support_metadata"], v["unsaved_support_metadata"] = float64(230), float64(95), float64(135)
			case "moved state":
				m := v["original_support_state_counts"].(map[string]any)
				m["2"], m["1"] = float64(28), float64(47)
			case "extra state":
				v["original_support_state_counts"].(map[string]any)["8"] = float64(0)
			case "missing state":
				delete(v["original_support_state_counts"].(map[string]any), "5")
			case "fractional state":
				v["original_support_state_counts"].(map[string]any)["2"] = 29.5
			case "missing states":
				delete(v, "original_support_state_counts")
			}
			_, err := original3719SupportSummary(v)
			if (change == "valid" && err != nil) || (change != "valid" && err == nil) {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
