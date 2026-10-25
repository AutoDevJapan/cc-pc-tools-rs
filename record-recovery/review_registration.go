package main

import (
	"encoding/json"
	"io"
)

const reviewRegistrationThread = "01a169de-626d-7270-9de7-362e2cc8af25"
const reviewRegistrationTurn = "01a16d9d-54fe-77c0-9ce3-1a46594978fd"
const reviewRegistrationItem = "exec-27beecc5-8f7a-47c3-927f-9f0a7208a390"
const reviewRegistrationCommandSHA = "31ad65c65a2f203326add7cd2b1feb438034671fa12255603abdd40312976deb"
const reviewRegistrationOutputSHA = "9eec3fe88e851eaaf196a27033696cee8a5e34bbae59d222c7fa3778c0a0d992"
const reviewRegistrationCWD = "D:/CC/Work/unattended-operations-20261013-wt/todo-3387-discord"

// The normal API is the source. The saved investigation receipt is not read or
// accepted as an attestation. Only this one command's fixed hashes are returned;
// command text, output text, other items, paths and current settings stay private.
func inspectReviewRegistration(call func(string, obj) (obj, error)) bool {
	return inspectReviewRegistrationAgainst(call, reviewRegistrationCommandSHA, reviewRegistrationOutputSHA)
}

// Only synthetic tests vary these hashes. The production call has no caller
// input, file path or flag that can replace the two compiled historical pins.
func inspectReviewRegistrationAgainst(call func(string, obj) (obj, error), commandSHA, outputSHA string) bool {
	root, err := call("thread/read", obj{"threadId": reviewRegistrationThread, "includeTurns": false})
	if err != nil {
		return false
	}
	thread := omb3255Object(root["thread"])
	if omb3255Text(thread["id"]) != reviewRegistrationThread || !omb3255SamePath(omb3255Text(thread["cwd"]), reviewRegistrationCWD) || len(omb3255Array(thread["turns"])) != 0 {
		return false
	}
	page, err := call("thread/turns/list", obj{"threadId": reviewRegistrationThread, "limit": 32, "sortDirection": "asc", "itemsView": "notLoaded"})
	if err != nil || page["nextCursor"] != nil {
		return false
	}
	matched := 0
	for _, raw := range omb3255Array(page["data"]) {
		turn := omb3255Object(raw)
		if omb3255Text(turn["id"]) == reviewRegistrationTurn {
			if turn["status"] != "completed" || turn["error"] != nil || omb3255Int(turn["startedAt"]) != 1792840389 || omb3255Int(turn["completedAt"]) != 1792848579 || turn["itemsView"] != "notLoaded" || len(omb3255Array(turn["items"])) != 0 {
				return false
			}
			matched++
		}
	}
	if matched != 1 {
		return false
	}
	// Opaque cursors come only from the normal API. The historical item is in
	// the first four 100-item pages. A shifted/missing item is unavailable, not
	// a reason to walk an unbounded transcript or read native files.
	var cursor any
	seen := map[string]bool{}
	for index := 0; index < 4; index++ {
		params := obj{"threadId": reviewRegistrationThread, "turnId": reviewRegistrationTurn, "limit": 100, "sortDirection": "asc"}
		if cursor != nil {
			params["cursor"] = cursor
		}
		page, err = call("thread/items/list", params)
		if err != nil {
			return false
		}
		count := 0
		for _, raw := range omb3255Array(page["data"]) {
			entry := omb3255Object(raw)
			item := omb3255Object(entry["item"])
			if item["id"] != reviewRegistrationItem {
				continue
			}
			count++
			if entry["turnId"] != reviewRegistrationTurn || item["type"] != "commandExecution" || item["status"] != "completed" || omb3255Int(item["exitCode"]) != 0 || !omb3255SamePath(omb3255Text(item["cwd"]), reviewRegistrationCWD) || omb3255TextSHA(omb3255Text(item["command"])) != commandSHA || omb3255TextSHA(omb3255Text(item["aggregatedOutput"])) != outputSHA {
				return false
			}
		}
		if count > 1 {
			return false
		}
		if count == 1 {
			// ID equality and the full original command/output hashes bind the
			// one API item, not a global claim about other transcript contents.
			return true
		}
		cursor = page["nextCursor"]
		text, ok := cursor.(string)
		if !ok || text == "" || len(text) > 8192 || seen[text] {
			return false
		}
		seen[text] = true
	}
	return false
}

func runReviewRegistrationCLI(args []string, out io.Writer, acquire func() bool) int {
	if len(args) != 0 {
		_ = json.NewEncoder(out).Encode(obj{"ok": false, "code": "review_registration_invalid_arguments"})
		return 2
	}
	observed := acquire()
	result := obj{"ok": true, "kind": "original-review-registration-3649-v1", "todo_id": 3649, "state": "unavailable", "registration": nil, "producer": reviewAdmissionProducer(), "owner_origin_proved": false, "fallback_authorized": false, "live_state_changed": false, "data_access_granted": false}
	if observed {
		result["state"] = "observed"
		result["registration"] = obj{"source_job": "20261024-201241-todo-3387", "thread": reviewRegistrationThread, "turn": reviewRegistrationTurn, "item": reviewRegistrationItem, "command_sha256": reviewRegistrationCommandSHA, "output_sha256": reviewRegistrationOutputSHA, "completed_successfully": true, "added_3649": true, "omitted_origin_flags": true}
	}
	if json.NewEncoder(out).Encode(result) != nil {
		return 1
	}
	return 0
}
