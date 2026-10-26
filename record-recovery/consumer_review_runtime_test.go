package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func consumerChildFixture() (obj, []omb3255ExpectedRoot) {
	expected := consumerReviewRuntimeRoots()[0]
	body := "synthetic completed delegated consumer review"
	expected.SHA = omb3255TextSHA(body)
	row := obj{"id": expected.ID, "rootId": "0a54f034-1aae-4a1e-b65c-932d89a6b80b", "parentId": "0a54f034-1aae-4a1e-b65c-932d89a6b80b", "depth": json.Number("1"), "botId": omb3255Bot, "threadId": omb3255Task, "status": "completed", "outcome": "done", "executions": json.Number("1"), "startedAt": json.Number(strconv.FormatInt(expected.Start, 10)), "finishedAt": json.Number(strconv.FormatInt(expected.Finish, 10)), "result": body}
	return obj{"threadId": omb3255Task, "nodes": []any{row}}, []omb3255ExpectedRoot{expected}
}

func TestConsumerReviewDelegatedNodeBindsExactParent(t *testing.T) {
	for _, change := range []string{"positive", "parentId", "rootId", "both parents", "depth", "botId", "threadId", "id", "result", "status", "outcome", "executions", "startedAt", "finishedAt", "duplicate"} {
		t.Run(change, func(t *testing.T) {
			job, wanted := consumerChildFixture()
			row := omb3255Object(omb3255Array(job["nodes"])[0])
			switch change {
			case "both parents":
				row["rootId"], row["parentId"] = "11111111-1111-4111-8111-111111111111", "11111111-1111-4111-8111-111111111111"
			case "parentId", "rootId", "botId", "threadId", "id", "result", "status", "outcome":
				row[change] = "different"
			case "depth", "executions", "startedAt", "finishedAt":
				row[change] = json.Number(strconv.FormatInt(omb3255Int(row[change])+1, 10))
			case "duplicate":
				job["nodes"] = []any{row, row}
			}
			got, err := consumerReviewChooseNodes(job, "", 0, wanted)
			if change == "positive" {
				if err != nil || !reflect.DeepEqual(got, wanted) {
					t.Fatal("the actual normal delegated record shape was rejected")
				}
				if _, err := omb3255ChooseRootsFor(job, "", 0, wanted); err == nil {
					t.Fatal("an older command gained delegated-record admission")
				}
			} else if err == nil {
				t.Fatal("changed delegated binding was admitted")
			}
		})
	}
}

func TestConsumerReviewOwnDelegatedBuildMarker(t *testing.T) {
	job, wanted := consumerChildFixture()
	revision := strings.Repeat("b", 40)
	parent := "be6bbc17-a433-4e78-a594-645523a25771"
	own := obj{"id": "96d3ab14-f541-4773-a5d7-c446118cafdb", "rootId": parent, "parentId": parent, "depth": json.Number("1"), "botId": omb3255Bot, "threadId": omb3255Task, "status": "completed", "outcome": "done", "executions": json.Number("1"), "startedAt": json.Number("1792986099371"), "finishedAt": json.Number("1792986239900"), "result": omb3255ReaderMarker + " " + revision}
	job["nodes"] = append(omb3255Array(job["nodes"]), own)
	got, err := consumerReviewChooseNodes(job, revision, 1792986000000, wanted)
	if err != nil || len(got) != 2 || got[1].ID != own["id"] {
		t.Fatal("the exact build's delegated read review was omitted")
	}
	for _, change := range []string{"wrong parent", "self parent", "no parent", "old build", "old time", "duplicate"} {
		t.Run(change, func(t *testing.T) {
			copyNode := obj{}
			for key, value := range own {
				copyNode[key] = value
			}
			job["nodes"] = []any{omb3255Array(job["nodes"])[0], copyNode}
			switch change {
			case "wrong parent":
				copyNode["parentId"] = "11111111-1111-4111-8111-111111111111"
			case "self parent":
				copyNode["rootId"], copyNode["parentId"] = copyNode["id"], copyNode["id"]
			case "no parent":
				delete(copyNode, "parentId")
			case "old build":
				copyNode["result"] = omb3255ReaderMarker + " other"
			case "old time":
				copyNode["startedAt"] = json.Number("1")
			case "duplicate":
				job["nodes"] = append(omb3255Array(job["nodes"]), copyNode)
			}
			got, err := consumerReviewChooseNodes(job, revision, 1792986000000, wanted)
			if change == "duplicate" {
				if err == nil {
					t.Fatal("ambiguous own review was accepted")
				}
			} else if err != nil || len(got) != 1 {
				t.Fatal("invalid own review supplied authority")
			}
		})
	}
}

func TestConsumerReviewRuntimeExactNormalScope(t *testing.T) {
	want := []omb3255ExpectedRoot{{"c998d376-5fe2-4832-bac8-5e8a049fc61d", 1792985405622, 1792985551232, "125df4c40339e3a344925e437c2162d29256d5d6be03888e0e019caa39aa29d6"}}
	if !reflect.DeepEqual(consumerReviewRuntimeRoots(), want) {
		t.Fatal("consumer review's exact completed normal-result tuple changed")
	}
	old := append(append(append(omb3255ExpectedRoots(), reviewAdmissionExpectedRoots()...), unstartedReviewRuntimeRoots()...), privateReviewRuntimeRoots()...)
	for _, r := range old {
		if r.ID == want[0].ID {
			t.Fatal("a previously completed old review was reacquired")
		}
	}
}

func TestConsumerReviewRuntimeRejectsChangedNormalEvidence(t *testing.T) {
	for _, change := range []string{"positive", "result", "id", "rootId", "botId", "threadId", "status", "outcome", "depth", "startedAt", "finishedAt", "executions", "duplicate"} {
		t.Run(change, func(t *testing.T) {
			expected := consumerReviewRuntimeRoots()[0]
			body := "synthetic completed consumer review"
			expected.SHA = omb3255TextSHA(body)
			row := obj{"id": expected.ID, "rootId": expected.ID, "depth": json.Number("0"), "botId": omb3255Bot, "threadId": omb3255Task, "status": "completed", "outcome": "done", "executions": json.Number("1"), "startedAt": json.Number(strconv.FormatInt(expected.Start, 10)), "finishedAt": json.Number(strconv.FormatInt(expected.Finish, 10)), "result": body}
			switch change {
			case "result", "id", "rootId", "botId", "threadId", "status", "outcome":
				row[change] = "different"
			case "depth", "executions", "startedAt", "finishedAt":
				row[change] = json.Number(strconv.FormatInt(omb3255Int(row[change])+1, 10))
			}
			nodes := []any{row}
			if change == "duplicate" {
				nodes = append(nodes, row)
			}
			got, err := omb3255ChooseRootsFor(obj{"threadId": omb3255Task, "nodes": nodes}, "", 0, []omb3255ExpectedRoot{expected})
			if change == "positive" {
				if err != nil || !reflect.DeepEqual(got, []omb3255ExpectedRoot{expected}) {
					t.Fatal("the independently fixed normal binding was rejected")
				}
			} else if err == nil {
				t.Fatal("changed/duplicate normal evidence supplied model authority")
			}
		})
	}
}

func TestConsumerReviewRuntimeMainHelper(t *testing.T) {
	if os.Getenv("CC_CONSUMER_REVIEW_RUNTIME_MAIN_HELPER") != "1" {
		return
	}
	os.Args = []string{"record-recovery", "consumer-review-runtime-metadata", "--source=forbidden"}
	main()
	t.Fatal("main did not exit")
}

func TestConsumerReviewRuntimeMainRejectsOverrides(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestConsumerReviewRuntimeMainHelper$")
	cmd.Env = append(os.Environ(), "CC_CONSUMER_REVIEW_RUNTIME_MAIN_HELPER=1")
	hideCommand(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	exit, ok := err.(*exec.ExitError)
	var result obj
	if !ok || exit.ExitCode() != 2 || stderr.Len() != 0 || json.Unmarshal(raw, &result) != nil || result["code"] != "omb_runtime_metadata_invalid_arguments" {
		t.Fatal("a caller scope override reached a metadata source")
	}
}
