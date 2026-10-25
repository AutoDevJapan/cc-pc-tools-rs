package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func registrationFixture() (obj, obj, obj) {
	return obj{"thread": obj{"id": reviewRegistrationThread, "cwd": reviewRegistrationCWD}},
		obj{"data": []any{obj{"id": reviewRegistrationTurn, "status": "completed", "startedAt": json.Number("1792840389"), "completedAt": json.Number("1792848579"), "itemsView": "notLoaded"}}},
		obj{"turnId": reviewRegistrationTurn, "item": obj{"id": reviewRegistrationItem, "type": "commandExecution", "status": "completed", "exitCode": json.Number("0"), "cwd": reviewRegistrationCWD, "command": "synthetic command only", "aggregatedOutput": "synthetic result only"}}
}

func TestReviewRegistrationUsesBoundNormalItemAndOpaquePagination(t *testing.T) {
	root, turns, entry := registrationFixture()
	calls := 0
	call := func(method string, args obj) (obj, error) {
		calls++
		if args["threadId"] != reviewRegistrationThread {
			t.Fatal("wrong thread")
		}
		switch method {
		case "thread/read":
			if args["includeTurns"] != false {
				t.Fatal("transcript requested")
			}
			return root, nil
		case "thread/turns/list":
			return turns, nil
		case "thread/items/list":
			if args["turnId"] != reviewRegistrationTurn || args["limit"] != 100 {
				t.Fatal("scope changed")
			}
			if args["cursor"] == nil {
				return obj{"data": []any{obj{"item": obj{"id": "other", "text": "must remain private"}}}, "nextCursor": "opaque::cursor"}, nil
			}
			if args["cursor"] != "opaque::cursor" {
				t.Fatal("cursor was fabricated")
			}
			return obj{"data": []any{entry}}, nil
		}
		t.Fatal("non-read API")
		return nil, errors.New("unreachable")
	}
	if !inspectReviewRegistrationAgainst(call, omb3255TextSHA("synthetic command only"), omb3255TextSHA("synthetic result only")) || calls != 4 {
		t.Fatalf("matching API item rejected; calls=%d", calls)
	}
	if inspectReviewRegistration(call) {
		t.Fatal("synthetic record replaced historical compiled pins")
	}
}

func TestReviewRegistrationRejectsChangedFailedAmbiguousOrUnboundedInputs(t *testing.T) {
	for _, scenario := range []string{"root", "cwd", "turn", "turn_time", "turn_status", "turn_error", "turn_duplicate", "turn_cursor", "item_turn", "item_type", "item_status", "item_exit", "item_cwd", "command", "output", "duplicate", "cursor_cycle", "too_many_pages", "rpc_error"} {
		t.Run(scenario, func(t *testing.T) {
			root, turns, entry := registrationFixture()
			turn := omb3255Object(omb3255Array(turns["data"])[0])
			item := omb3255Object(entry["item"])
			switch scenario {
			case "root":
				omb3255Object(root["thread"])["id"] = "other"
			case "cwd":
				omb3255Object(root["thread"])["cwd"] = "other"
			case "turn":
				turn["id"] = "other"
			case "turn_time":
				turn["startedAt"] = json.Number("1792840390")
			case "turn_status":
				turn["status"] = "running"
			case "turn_error":
				turn["error"] = obj{"message": "private failure"}
			case "turn_duplicate":
				turns["data"] = []any{turn, turn}
			case "turn_cursor":
				turns["nextCursor"] = "more"
			case "item_turn":
				entry["turnId"] = "other"
			case "item_type":
				item["type"] = "agentMessage"
			case "item_status":
				item["status"] = "running"
			case "item_exit":
				item["exitCode"] = json.Number("1")
			case "item_cwd":
				item["cwd"] = "other"
			case "command":
				item["command"] = "changed"
			case "output":
				item["aggregatedOutput"] = "changed"
			}
			pages := 0
			call := func(method string, args obj) (obj, error) {
				switch method {
				case "thread/read":
					return root, nil
				case "thread/turns/list":
					return turns, nil
				case "thread/items/list":
					pages++
					if scenario == "rpc_error" {
						return nil, errors.New("private error text")
					}
					if scenario == "cursor_cycle" || scenario == "too_many_pages" {
						cursor := "same"
						if scenario == "too_many_pages" {
							cursor = strings.Repeat("x", pages)
						}
						return obj{"data": []any{}, "nextCursor": cursor}, nil
					}
					if scenario == "duplicate" {
						return obj{"data": []any{entry, entry}}, nil
					}
					return obj{"data": []any{entry}}, nil
				}
				return nil, errors.New("unexpected method")
			}
			if inspectReviewRegistrationAgainst(call, omb3255TextSHA("synthetic command only"), omb3255TextSHA("synthetic result only")) || pages > 4 {
				t.Fatal("invalid source accepted or page bound exceeded")
			}
		})
	}
}

func TestReviewRegistrationCLIRefusesArgumentsAndEmitsOnlyFixedMetadata(t *testing.T) {
	for _, observed := range []bool{false, true} {
		var output bytes.Buffer
		if runReviewRegistrationCLI(nil, &output, func() bool { return observed }) != 0 {
			t.Fatal(output.String())
		}
		var v obj
		if json.Unmarshal(output.Bytes(), &v) != nil {
			t.Fatal("bad JSON")
		}
		keys := map[string]bool{}
		for key := range v {
			keys[key] = true
		}
		if !reflect.DeepEqual(keys, map[string]bool{"ok": true, "kind": true, "todo_id": true, "state": true, "registration": true, "producer": true, "owner_origin_proved": true, "fallback_authorized": true, "live_state_changed": true, "data_access_granted": true}) {
			t.Fatal("output fields widened")
		}
		if !observed && v["registration"] != nil {
			t.Fatal("unavailable source became evidence")
		}
		for _, key := range []string{"owner_origin_proved", "fallback_authorized", "live_state_changed", "data_access_granted"} {
			if v[key] != false {
				t.Fatal(key)
			}
		}
	}
	var output bytes.Buffer
	if runReviewRegistrationCLI([]string{"--path", "private"}, &output, func() bool { t.Fatal("source called"); return true }) != 2 {
		t.Fatal("arguments accepted")
	}
}
