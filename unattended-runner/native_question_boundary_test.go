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
