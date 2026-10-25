package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func omb3255ReferenceFixture() (map[string]obj, map[string]obj, []omb3255ExpectedRoot) {
	text := "synthetic final review"
	expected := omb3255ExpectedRoot{"synthetic-root", 1792848636233, 1792848928582, omb3255TextSHA(text)}
	root := obj{"id": expected.ID, "rootId": expected.ID, "depth": json.Number("0"), "botId": omb3255Bot, "threadId": omb3255Task, "status": "completed", "outcome": "done", "executions": json.Number("1"), "startedAt": json.Number("1792848636233"), "finishedAt": json.Number("1792848928582"), "result": text}
	get := map[string]obj{
		"/api/bots?messages=0": {"bots": []any{obj{"id": omb3255Bot, "tasks": []any{obj{"threadId": omb3255Task, "busy": false, "activity": "idle", "cwd": "C:/synthetic-project"}}}}},
		"/api/threads/" + omb3255Task + "/job?scope=job&since=1792848636233&full=1": {"threadId": omb3255Task, "nodes": []any{root}},
	}
	call := map[string]obj{
		"thread/read":       {"thread": obj{"id": omb3255NativeThread, "createdAt": json.Number("1792848636"), "cwd": "C:/synthetic-project", "path": omb3255ExpectedNativePath(), "turns": []any{}}},
		"thread/turns/list": {"data": []any{obj{"id": "synthetic-turn", "startedAt": json.Number("1792848641"), "completedAt": json.Number("1792848928"), "status": "completed", "itemsView": "notLoaded", "items": []any{}, "error": nil}}},
		"thread/items/list": {"data": []any{obj{"turnId": "synthetic-turn", "item": obj{"type": "agentMessage", "phase": "final_answer", "text": text}}}},
	}
	return get, call, []omb3255ExpectedRoot{expected}
}

func TestOMB3255RuntimeReferenceAdmissionIsLiveAndSpecific(t *testing.T) {
	for _, name := range []string{"positive", "busy", "busy absent", "wrong task", "duplicate task", "wrong job", "duplicate root", "wrong actor", "wrong result", "rerun", "wrong native", "wrong cwd", "wrong path", "ambiguous turns", "items loaded", "wrong final", "wrong phase", "wrong entry turn", "duplicate final", "get error", "rpc error"} {
		t.Run(name, func(t *testing.T) {
			get, call, wanted := omb3255ReferenceFixture()
			profile := omb3255Object(omb3255Array(get["/api/bots?messages=0"]["bots"])[0])
			task := omb3255Object(omb3255Array(profile["tasks"])[0])
			job := get["/api/threads/"+omb3255Task+"/job?scope=job&since=1792848636233&full=1"]
			root := omb3255Object(omb3255Array(job["nodes"])[0])
			native := omb3255Object(call["thread/read"]["thread"])
			turn := omb3255Object(omb3255Array(call["thread/turns/list"]["data"])[0])
			entry := omb3255Object(omb3255Array(call["thread/items/list"]["data"])[0])
			final := omb3255Object(entry["item"])
			switch name {
			case "busy":
				task["busy"] = true
			case "busy absent":
				delete(task, "busy")
			case "wrong task":
				task["threadId"] = "other-task"
			case "duplicate task":
				profile["tasks"] = []any{task, task}
			case "wrong job":
				job["threadId"] = "other-task"
			case "duplicate root":
				job["nodes"] = []any{root, root}
			case "wrong actor":
				root["botId"] = "other-bot"
			case "wrong result":
				root["result"] = "private different result"
			case "rerun":
				root["executions"] = json.Number("2")
			case "wrong native":
				native["id"] = "other-native"
			case "wrong cwd":
				native["cwd"] = "C:/other-project"
			case "wrong path":
				native["path"] = "C:/other-private-file"
			case "ambiguous turns":
				call["thread/turns/list"]["data"] = []any{turn, turn}
			case "items loaded":
				turn["itemsView"] = "full"
			case "wrong final":
				final["text"] = "private different result"
			case "wrong phase":
				final["phase"] = "commentary"
			case "wrong entry turn":
				entry["turnId"] = "other-turn"
			case "duplicate final":
				call["thread/items/list"]["data"] = []any{entry, entry}
			}
			gets, rpcs := 0, 0
			refs, path, err := omb3255CollectRefsFrom(func(endpoint string) (obj, error) {
				gets++
				if name == "get error" {
					return nil, errors.New("private network error")
				}
				return get[endpoint], nil
			}, func(method string, params obj) (obj, error) {
				rpcs++
				if name == "rpc error" {
					return nil, errors.New("private RPC error")
				}
				return call[method], nil
			}, "", 0, wanted)
			if name == "positive" {
				if err != nil || len(refs) != 1 || refs[0].AssignmentID != "synthetic-root" || path != omb3255ExpectedNativePath() || gets != 2 || rpcs != 3 {
					t.Fatalf("positive qualification failed refs=%+v path=%s get=%d rpc=%d err=%v", refs, path, gets, rpcs, err)
				}
			} else if err == nil || refs != nil || path != "" {
				t.Fatalf("bad normal source admitted: %+v %s", refs, path)
			}
		})
	}
}

func TestOMB3255RuntimeOwnReviewNeedsExactRevisionAndMarker(t *testing.T) {
	get, _, wanted := omb3255ReferenceFixture()
	job := get["/api/threads/"+omb3255Task+"/job?scope=job&since=1792848636233&full=1"]
	revision := strings.Repeat("b", 40)
	newRoot := obj{"id": "current-reader-review", "rootId": "current-reader-review", "depth": json.Number("0"), "botId": omb3255Bot, "threadId": omb3255Task, "status": "completed", "outcome": "done", "executions": json.Number("1"), "startedAt": json.Number("1792850000000"), "finishedAt": json.Number("1792850010000"), "result": omb3255ReaderMarker + " head " + revision}
	job["nodes"] = append(omb3255Array(job["nodes"]), newRoot)
	refs, err := omb3255ChooseRootsFor(job, revision, 1792849990000, wanted)
	if err != nil || len(refs) != 2 || refs[1].ID != "current-reader-review" {
		t.Fatal("own normal review was not included")
	}
	for _, field := range []string{"revision", "marker", "date"} {
		newRoot["result"] = omb3255ReaderMarker + " head " + revision
		newRoot["startedAt"] = json.Number("1792850000000")
		switch field {
		case "revision":
			newRoot["result"] = omb3255ReaderMarker + " head other"
		case "marker":
			newRoot["result"] = revision
		case "date":
			newRoot["startedAt"] = json.Number("1")
		}
		refs, err = omb3255ChooseRootsFor(job, revision, 1792849990000, wanted)
		if err != nil || len(refs) != 1 {
			t.Fatalf("unbound current review included: %s", field)
		}
	}
}

func TestOMB3255RuntimeMetadataRejectsShadowedAndMalformedJSON(t *testing.T) {
	for _, raw := range []string{`{"id":1,"id":2}`, `{"x":{"a":1,"a":2}}`, "{} {}", string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
		if _, err := omb3255Decode([]byte(raw)); err == nil {
			t.Fatal("bad protocol JSON admitted")
		}
	}
	for _, value := range []any{nil, "1", true, json.Number("1e2"), json.Number("-1")} {
		if omb3255Int(value) != -1 {
			t.Fatal("non-integral protocol number admitted")
		}
	}
}
