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

func TestReviewRegistrationModuleIdentityDoesNotUseWholeRepositoryHEAD(t *testing.T) {
	hash := strings.Repeat("a", 40)
	if !reviewRegistrationModuleMatch([]byte(hash + "\n" + hash + "\n")) {
		t.Fatal("identical module identities were refused")
	}
	for _, raw := range []string{hash, hash + "\n" + strings.Repeat("b", 40), hash + "\n" + hash + "\n" + hash, "unknown\nunknown", strings.Repeat("z", 40) + "\n" + strings.Repeat("z", 40)} {
		if reviewRegistrationModuleMatch([]byte(raw)) {
			t.Fatal("missing, changed or invalid module identity accepted")
		}
	}
}

func TestReviewRegistrationContinuationIsTheParentReplyAndNeverAnOwnerClaim(t *testing.T) {
	for _, scenario := range []string{"positive", "wrong_thread", "user_role", "changed", "duplicate", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			get := func(endpoint string) (obj, error) {
				if endpoint == "/api/bots?messages=0" {
					thread := reviewContinuationThread
					if scenario == "wrong_thread" {
						thread = "other"
					}
					return obj{"bots": []any{obj{"id": "2e2cb951-341e-4a2b-89f6-0ad084845edb", "tasks": []any{obj{"threadId": thread}}}}}, nil
				}
				if scenario == "unavailable" {
					return nil, errors.New("unavailable")
				}
				m := obj{"id": reviewContinuationMessage, "role": "bot", "kind": "text", "text": "synthetic continuation"}
				if scenario == "user_role" {
					m["role"] = "user"
				}
				if scenario == "changed" {
					m["text"] = "other"
				}
				messages := []any{m}
				if scenario == "duplicate" {
					messages = append(messages, m)
				}
				return obj{"messages": messages}, nil
			}
			if got := inspectReviewContinuationAgainst(get, omb3255TextSHA("synthetic continuation")); got != (scenario == "positive") {
				t.Fatal(scenario, got)
			}
		})
	}
}

func TestReviewRegistrationRejectsDuplicatesAcrossBoundedPages(t *testing.T) {
	root, turns, entry := registrationFixture()
	pages := 0
	call := func(method string, args obj) (obj, error) {
		switch method {
		case "thread/read":
			return root, nil
		case "thread/turns/list":
			return turns, nil
		case "thread/items/list":
			pages++
			if pages == 1 {
				return obj{"data": []any{entry}, "nextCursor": "second"}, nil
			}
			return obj{"data": []any{entry}}, nil
		}
		return nil, errors.New("unexpected")
	}
	if inspectReviewRegistrationAgainst(call, omb3255TextSHA("synthetic command only"), omb3255TextSHA("synthetic result only")) {
		t.Fatal("duplicate across pages accepted")
	}
}

func TestUnstartedReviewRuntimeRootsDoNotReacquireCompletedOldReviews(t *testing.T) {
	refs := unstartedReviewRuntimeRoots()
	if len(refs) != 2 || refs[0].ID != "837aab36-0c9d-4867-8b9f-db9952bc3931" || refs[0].Start != 1792942185190 || refs[0].Finish != 1792943381103 || refs[1].ID != "6d821a13-a98a-4c7b-ac81-ca82eb8012cd" {
		t.Fatal("new review binding widened or changed")
	}
	for _, old := range append(omb3255ExpectedRoots(), reviewAdmissionExpectedRoots()...) {
		if refs[0].ID == old.ID || refs[1].ID == old.ID {
			t.Fatal("old completed review reintroduced")
		}
	}
}
