package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"testing"
)

func TestPrivateReviewRuntimeFixedNewScope(t *testing.T) {
	want := []omb3255ExpectedRoot{{"e3069b1f-0901-4def-b70f-a140a39d7f4b", 1792959872006, 1792959954969, "261a6eafa25997cd62c63d68b7a069cfc34ee765f679812f0bc2f76732aa9399"}}
	if !reflect.DeepEqual(privateReviewRuntimeRoots(), want) {
		t.Fatal("new review's exact normal-result binding changed")
	}
	for _, old := range append(append(omb3255ExpectedRoots(), reviewAdmissionExpectedRoots()...), unstartedReviewRuntimeRoots()...) {
		if old.ID == want[0].ID {
			t.Fatal("completed old review was reacquired")
		}
	}
}

func TestPrivateReviewRuntimeRootRejectsChangedNormalEvidence(t *testing.T) {
	roots := privateReviewRuntimeRoots()
	if len(roots) != 1 {
		t.Fatal("missing fixed root")
	}
	// The fixed production tuple is checked above. These synthetic fixtures
	// isolate normal-API admission without opening any production source.
	for _, field := range []string{"positive", "result", "id", "startedAt", "finishedAt", "executions", "duplicate"} {
		t.Run(field, func(t *testing.T) {
			expected := roots[0]
			row := obj{"id": expected.ID, "rootId": expected.ID, "depth": json.Number("0"), "botId": omb3255Bot, "threadId": omb3255Task, "status": "completed", "outcome": "done", "executions": json.Number("1"), "startedAt": json.Number(strconv.FormatInt(expected.Start, 10)), "finishedAt": json.Number(strconv.FormatInt(expected.Finish, 10)), "result": "synthetic mismatching response"}
			if field != "result" {
				// Use an independent synthetic expected digest to isolate the field.
				expected.SHA = omb3255TextSHA("synthetic mismatching response")
			}
			switch field {
			case "id":
				row["id"] = "different-root"
			case "startedAt":
				row[field] = json.Number(strconv.FormatInt(expected.Start+1, 10))
			case "finishedAt":
				row[field] = json.Number(strconv.FormatInt(expected.Finish+1, 10))
			case "executions":
				row[field] = json.Number("2")
			}
			nodes := []any{row}
			if field == "duplicate" {
				nodes = append(nodes, row)
			}
			got, err := omb3255ChooseRootsFor(obj{"threadId": omb3255Task, "nodes": nodes}, "", 0, []omb3255ExpectedRoot{expected})
			if field == "positive" {
				if err != nil || !reflect.DeepEqual(got, []omb3255ExpectedRoot{expected}) {
					t.Fatal("synthetic positive control rejected")
				}
			} else if err == nil {
				t.Fatal("changed normal evidence accepted")
			}
		})
	}
}

func TestPrivateReviewRuntimeMainHelper(t *testing.T) {
	if os.Getenv("CC_PRIVATE_REVIEW_RUNTIME_MAIN_HELPER") != "1" {
		return
	}
	os.Args = []string{"record-recovery", "private-review-runtime-metadata", "--source=forbidden"}
	main()
	t.Fatal("main did not exit")
}

func TestPrivateReviewRuntimeMainRefusesArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestPrivateReviewRuntimeMainHelper$")
	cmd.Env = append(os.Environ(), "CC_PRIVATE_REVIEW_RUNTIME_MAIN_HELPER=1")
	hideCommand(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	exit, ok := err.(*exec.ExitError)
	var response obj
	if !ok || exit.ExitCode() != 2 || stderr.Len() != 0 || json.Unmarshal(output, &response) != nil || response["code"] != "omb_runtime_metadata_invalid_arguments" {
		t.Fatal("main did not select the argument-refusing runtime command")
	}
}
