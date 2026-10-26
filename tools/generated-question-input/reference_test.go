package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const testThread = "00000000-0000-4000-8000-000000000001"
const testTurn = "00000000-0000-4000-8000-000000000002"
const testItem = "00000000-0000-4000-8000-000000000003"
const testImage = "exec-00000000-0000-4000-8000-000000000004.png"

func inputHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type fakeInputRPC struct {
	t           *testing.T
	text        string
	calls       []string
	mutate      func(string, map[string]any) map[string]any
	failMethod  string
	closeErr    error
	closed      bool
	wantCursors []string
	turnPages   int
}

func (f *fakeInputRPC) Close() error { f.closed = true; return f.closeErr }

func (f *fakeInputRPC) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	if method == f.failMethod {
		return nil, errors.New("private transport failure must not appear in output")
	}
	p, err := json.Marshal(params)
	if err != nil {
		f.t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal(p, &request); err != nil {
		f.t.Fatal(err)
	}
	if request["threadId"] != testThread {
		f.t.Fatalf("wrong thread requested: %s", p)
	}
	var result any
	switch method {
	case "thread/read":
		if request["includeTurns"] != false {
			f.t.Fatal("full thread history requested")
		}
		result = map[string]any{"thread": map[string]any{"id": testThread, "source": "cli", "turns": []any{}}}
	case "thread/turns/list":
		if request["itemsView"] != "notLoaded" || request["limit"] != float64(20) || request["sortDirection"] != "desc" {
			f.t.Fatal("turn bodies requested")
		}
		if f.wantCursors != nil {
			cursor, _ := request["cursor"].(string)
			if f.turnPages >= len(f.wantCursors) || cursor != f.wantCursors[f.turnPages] {
				f.t.Fatal("wrong turn continuation cursor")
			}
			f.turnPages++
		}
		result = map[string]any{"data": []any{map[string]any{"id": testTurn, "rootTurnId": testTurn, "startedAt": 100, "itemsView": "notLoaded", "items": []any{}}}}
	case "thread/items/list", "thread/items/read":
		if request["turnId"] != testTurn {
			f.t.Fatal("wrong turn requested")
		}
		if method == "thread/items/list" && (request["limit"] != float64(1) || request["sortDirection"] != "asc") {
			f.t.Fatal("more than the first item requested")
		}
		if method == "thread/items/read" {
			ids, ok := request["itemIds"].([]any)
			if !ok || len(ids) != 1 || ids[0] != testItem {
				f.t.Fatal("wrong exact item requested")
			}
		}
		result = map[string]any{"data": []any{map[string]any{"turnId": testTurn, "item": map[string]any{"id": testItem, "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": f.text}}}}}}
	default:
		return nil, fmt.Errorf("unexpected method")
	}
	object := result.(map[string]any)
	if f.mutate != nil {
		object = f.mutate(method, object)
	}
	return json.Marshal(object)
}

func validTestReference(text string) inputReference {
	return inputReference{ThreadID: testThread, TurnID: testTurn, ItemID: testItem, InputSHA256: inputHash(text), ImageName: testImage}
}

func TestInputReferenceRejectsOtherSourcesVersionsAndShapes(t *testing.T) {
	validText := "Please check " + testImage + "."
	cases := []struct {
		name, code string
		mutate     func(string, map[string]any) map[string]any
		fail       string
	}{
		{"subagent", "source_thread_kind_rejected", func(m string, v map[string]any) map[string]any {
			if m == "thread/read" {
				v["thread"].(map[string]any)["source"] = map[string]any{"subAgent": "spawn"}
			}
			return v
		}, ""},
		{"exec", "source_thread_kind_rejected", func(m string, v map[string]any) map[string]any {
			if m == "thread/read" {
				v["thread"].(map[string]any)["source"] = "exec"
			}
			return v
		}, ""},
		{"wrong_thread", "thread_metadata_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/read" {
				v["thread"].(map[string]any)["id"] = testItem
			}
			return v
		}, ""},
		{"history_body", "thread_metadata_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/read" {
				v["thread"].(map[string]any)["turns"] = []any{"unexpected"}
			}
			return v
		}, ""},
		{"wrong_root", "root_turn_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/turns/list" {
				v["data"].([]any)[0].(map[string]any)["rootTurnId"] = testThread
			}
			return v
		}, ""},
		{"future_root", "root_turn_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/turns/list" {
				v["data"].([]any)[0].(map[string]any)["startedAt"] = 201
			}
			return v
		}, ""},
		{"unknown_root_time", "root_turn_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/turns/list" {
				v["data"].([]any)[0].(map[string]any)["startedAt"] = nil
			}
			return v
		}, ""},
		{"turn_body", "turn_metadata_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/turns/list" {
				v["data"].([]any)[0].(map[string]any)["itemsView"] = "summary"
			}
			return v
		}, ""},
		{"duplicate_root", "root_turn_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/turns/list" {
				v["data"] = append(v["data"].([]any), v["data"].([]any)[0])
			}
			return v
		}, ""},
		{"missing_root", "root_turn_not_found", func(m string, v map[string]any) map[string]any {
			if m == "thread/turns/list" {
				v["data"] = []any{}
			}
			return v
		}, ""},
		{"wrong_first_item", "first_input_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/items/list" {
				v["data"].([]any)[0].(map[string]any)["item"].(map[string]any)["id"] = testThread
			}
			return v
		}, ""},
		{"assistant_item", "first_input_mismatch", func(m string, v map[string]any) map[string]any {
			if m == "thread/items/list" {
				v["data"].([]any)[0].(map[string]any)["item"].(map[string]any)["type"] = "agentMessage"
			}
			return v
		}, ""},
		{"extra_content", "input_shape_rejected", func(m string, v map[string]any) map[string]any {
			if m == "thread/items/list" {
				item := v["data"].([]any)[0].(map[string]any)["item"].(map[string]any)
				item["content"] = append(item["content"].([]any), map[string]any{"type": "image", "url": "unused"})
			}
			return v
		}, ""},
		{"item_version_changed", "input_version_changed", func(m string, v map[string]any) map[string]any {
			if m == "thread/items/read" {
				v["data"].([]any)[0].(map[string]any)["item"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"] = validText + " changed"
			}
			return v
		}, ""},
		{"read_error", "source_unavailable", nil, "thread/items/read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeInputRPC{t: t, text: validText, mutate: tc.mutate, failMethod: tc.fail}
			r := inspectInputReference(context.Background(), f, validTestReference(validText), 200)
			if r.SourceMatched || r.AssetQualified || r.Code != tc.code {
				t.Fatalf("got %+v, expected %s", r, tc.code)
			}
		})
	}
}

func TestInputReferenceKeepsTextVersionAndImageIdentityDistinct(t *testing.T) {
	cases := []struct{ name, text, code string }{
		{"no_image", "Keep the structure and adjust its colors.", "image_reference_missing"},
		{"other_image", "Review exec-00000000-0000-4000-8000-000000000005.png", "image_reference_mismatch"},
		{"two_images", testImage + " and exec-00000000-0000-4000-8000-000000000005.png", "ambiguous_image_reference"},
		{"scheduler", "この PC の共有スケジューラから割り当てられた作業です。\n" + testImage, "automation_input_rejected"},
		{"todo_assignment", "ToDo1234 " + testImage, "automation_input_rejected"},
		{"environment", "<environment_context> " + testImage, "automation_input_rejected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeInputRPC{t: t, text: tc.text}
			r := inspectInputReference(context.Background(), f, validTestReference(tc.text), 200)
			if r.Code != tc.code || r.SourceMatched || r.AssetQualified {
				t.Fatalf("got %+v", r)
			}
		})
	}
	text := "Review " + testImage
	f := &fakeInputRPC{t: t, text: text}
	ref := validTestReference(text)
	ref.InputSHA256 = inputHash(text + "\n")
	if r := inspectInputReference(context.Background(), f, ref, 200); r.Code != "input_version_mismatch" {
		t.Fatalf("newline normalization accepted: %+v", r)
	}
}

func TestInputReferenceAllowsOneSHAOrRepeatedSameImageButNoConflictingAlias(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, text := range []string{"Review " + sha, testImage + " then " + testImage, testImage + " " + sha} {
		ref := validTestReference(text)
		ref.ImageSHA256 = sha
		if r := inspectInputReference(context.Background(), &fakeInputRPC{t: t, text: text}, ref, 200); !r.SourceMatched || r.AssetQualified {
			t.Fatalf("consistent identity rejected: %+v", r)
		}
	}
	text := testImage + " " + strings.Repeat("b", 64)
	ref := validTestReference(text)
	ref.ImageSHA256 = sha
	if r := inspectInputReference(context.Background(), &fakeInputRPC{t: t, text: text}, ref, 200); r.Code != "image_reference_mismatch" {
		t.Fatalf("conflicting alias accepted: %+v", r)
	}
}

func TestInputReferencePaginationIsBoundedAndDoesNotLoadItems(t *testing.T) {
	text := "Review " + testImage
	page := 0
	f := &fakeInputRPC{t: t, text: text, wantCursors: []string{"", "same-cursor"}, mutate: func(m string, v map[string]any) map[string]any {
		if m == "thread/turns/list" {
			page++
			v["data"] = []any{}
			v["nextCursor"] = "same-cursor"
		}
		return v
	}}
	if r := inspectInputReference(context.Background(), f, validTestReference(text), 200); r.Code != "turn_metadata_mismatch" {
		t.Fatalf("cursor cycle accepted: %+v", r)
	}
	if page != 2 {
		t.Fatalf("cursor cycle was not bounded: %d", page)
	}
	for _, call := range f.calls {
		if call == "thread/items/list" || call == "thread/items/read" {
			t.Fatal("read item content without a root")
		}
	}
}

func TestInputReferenceFindsSecondPageAndCapsUniqueCursors(t *testing.T) {
	text := "Review " + testImage
	page := 0
	f := &fakeInputRPC{t: t, text: text, wantCursors: []string{"", "second-page"}, mutate: func(m string, v map[string]any) map[string]any {
		if m == "thread/turns/list" {
			page++
			if page == 1 {
				v["data"] = []any{}
				v["nextCursor"] = "second-page"
			}
		}
		return v
	}}
	if r := inspectInputReference(context.Background(), f, validTestReference(text), 200); !r.SourceMatched || page != 2 {
		t.Fatalf("second page was not accepted: %+v, pages=%d", r, page)
	}
	page = 0
	f = &fakeInputRPC{t: t, text: text, wantCursors: []string{"", "1", "2", "3", "4", "5", "6", "7"}, mutate: func(m string, v map[string]any) map[string]any {
		if m == "thread/turns/list" {
			page++
			v["data"] = []any{}
			v["nextCursor"] = fmt.Sprint(page)
		}
		return v
	}}
	if r := inspectInputReference(context.Background(), f, validTestReference(text), 200); r.Code != "root_turn_not_found" || page != 8 {
		t.Fatalf("unique cursors were not bounded: %+v, pages=%d", r, page)
	}
}

func TestInputReferenceMatchesLiveFirstItemAndReacquiredVersion(t *testing.T) {
	text := "Please confirm the colors of " + testImage + "."
	f := &fakeInputRPC{t: t, text: text}
	r := inspectInputReference(context.Background(), f, inputReference{ThreadID: testThread, TurnID: testTurn, ItemID: testItem, InputSHA256: inputHash(text), ImageName: testImage}, 200)
	if !r.SourceMatched || r.AssetQualified || r.Stage != "complete" || r.Code != "source_reference_matched" {
		t.Fatalf("expected source match without asset permission, got %+v", r)
	}
	if got := fmt.Sprint(f.calls); got != "[thread/read thread/turns/list thread/items/list thread/items/read]" {
		t.Fatalf("normal bounded reacquisition was not used: %s", got)
	}
}
