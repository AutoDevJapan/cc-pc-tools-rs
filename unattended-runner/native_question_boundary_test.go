package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeQuestionMemoryRejectsPartialNumericArguments(t *testing.T) {
	e, d, _ := nativeQuestionMemoryFixture(t)
	raw, err := d.Records(e.Rollout, []nativeQuestionEvent{e.Question})
	if err != nil {
		t.Fatal(err)
	}
	var r nativeQuestionRecord
	if json.Unmarshal(raw[0], &r) != nil {
		t.Fatal("fixture record")
	}
	if !nativeQuestionAskMatches(r.Payload.Input, e) {
		t.Fatal("positive control")
	}
	for _, pair := range [][2]string{{"--todo-id 42", "--todo-id 420"}, {"--origin-thread 106", "--origin-thread 1060"}} {
		if nativeQuestionAskMatches(strings.Replace(r.Payload.Input, pair[0], pair[1], 1), e) {
			t.Fatal("partial numeric argument accepted", pair[0])
		}
	}
	commands, _ := nativeQuestionTokenCommands(nativeQuestionMetadata(r.Payload.Input))
	for _, extra := range []string{"Set-Location elsewhere\n", "Push-Location elsewhere\n", commands[0] + "\n  ", "& \"D:/CC/Tools/discord-ask/discord_ask.exe\" recovery-ask other\n"} {
		input := "text(await tools.exec_command({cmd:" + string(mustNativeJSON(extra+commands[0])) + "}));"
		if nativeQuestionAskMatches(input, e) {
			t.Fatal("extra original command accepted")
		}
	}
}

func TestNativeQuestionMemoryPreservesOriginalJobOwnerStops(t *testing.T) {
	if nativeQuestionJobStopped(nativeQuestionOriginJob{}) {
		t.Fatal("positive control")
	}
	for _, job := range []nativeQuestionOriginJob{{Stopped: true}, {OwnerStopped: true}, {Cancelled: true}, {CancelReason: "manual"}, {CancelReason: "person"}} {
		if !nativeQuestionJobStopped(job) {
			t.Fatal("original job owner stop ignored")
		}
	}
}

func TestNativeQuestionMemoryAssignmentRejectsAdjacentTodoIDs(t *testing.T) {
	if !nativeQuestionAssignmentTodo("本人依頼。元42、対象の実装。", 42) {
		t.Fatal("positive control")
	}
	for _, first := range []string{"元42 ToDo43", "元42、ToDo43", "元42 ToDo42", "元420", "元42suffix"} {
		if nativeQuestionAssignmentTodo(first, 42) {
			t.Fatal("ambiguous original task accepted", first)
		}
	}
}

func TestNativeQuestionMemoryAsyncCannotFollowAnotherUserOrTool(t *testing.T) {
	preview, async := `{"type":"response_item","payload":{"type":"custom_tool_call_output"}}`, `{"type":"response_item","payload":{"type":"function_call"}}`
	for _, middle := range []string{`{"type":"response_item","payload":{"type":"reasoning"}}`, `{"type":"response_item","payload":{"type":"message","role":"user"}}`, `{"type":"response_item","payload":{"type":"function_call"}}`, `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution"}}}`} {
		input := preview + "\n" + middle + "\n" + async + "\n"
		err := nativeQuestionAssociationScan(strings.NewReader(input), nativeQuestionEvent{1, bytesHash([]byte(preview))}, nativeQuestionEvent{3, bytesHash([]byte(async))})
		if (err == nil) != strings.Contains(middle, `"reasoning"`) {
			t.Fatal("intervening event association")
		}
	}
}
