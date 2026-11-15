//go:build windows

package main

import (
	"context"
	"encoding/json"
	"testing"
)

func historyCollectedTestResult() askChildResult {
	return askChildResult{
		Started: true, Assigned: true, WaitVerified: true, Reaped: true, Exit: 0,
		CollectionReason: "history_job_collected", CollectionObserved: 1, CollectionSignalled: 1,
		CollectionTotal: 1, CollectionTotalKnown: true, CollectionRootPID: 7, CollectionRootCreation: 11,
		Capture: &askChildCaptureResult{historyStdin:historyCompletedStdinFacts(),Complete: true, protocolReason: "history_protocol_complete", protocolWritesComplete: true,
			NativeProcess: &askNativeProcess{PID: 7, Creation: 11}},
	}
}

func TestUnslopHistoryCurrentConsumerRequiresStrictSameRootCollection(t *testing.T) {
	if !unslopHistoryChildComplete(historyCollectedTestResult()) { t.Fatal("same-child strict result rejected") }
	for name, mutate := range map[string]func(*askChildResult){
		"not-started": func(r *askChildResult) { r.Started = false },
		"not-assigned": func(r *askChildResult) { r.Assigned = false },
		"wait-unknown": func(r *askChildResult) { r.WaitVerified = false },
		"uncollected": func(r *askChildResult) { r.Reaped = false },
		"cancelled": func(r *askChildResult) { r.TimedOut = true },
		"failed": func(r *askChildResult) { r.Exit = 1 },
		"legacy-reaped-only": func(r *askChildResult) { r.CollectionReason = "" },
		"unknown-count": func(r *askChildResult) { r.CollectionTotalKnown = false },
		"zero-count": func(r *askChildResult) { r.CollectionTotal = 0 },
		"missing-member": func(r *askChildResult) { r.CollectionTotal = 2 },
		"extra-held": func(r *askChildResult) { r.CollectionObserved = 2 },
		"unsignalled": func(r *askChildResult) { r.CollectionSignalled = 0 },
		"zero-root": func(r *askChildResult) { r.CollectionRootPID = 0 },
		"zero-generation": func(r *askChildResult) { r.CollectionRootCreation = 0 },
		"different-root": func(r *askChildResult) { r.Capture.NativeProcess.PID++ },
		"different-generation": func(r *askChildResult) { r.Capture.NativeProcess.Creation++ },
		"missing-native": func(r *askChildResult) { r.Capture.NativeProcess = nil },
		"missing-capture": func(r *askChildResult) { r.Capture = nil },
		"partial-output": func(r *askChildResult) { r.Capture.Complete = false },
		"overflow": func(r *askChildResult) { r.Capture.Overflow = true },
		"stdin-copy-unknown": func(r *askChildResult) { r.Capture.historyStdin.CopyFinished=false },
		"stdin-close-unknown": func(r *askChildResult) { r.Capture.historyStdin.CloseCompleted=false },
		"stdin-close-failed": func(r *askChildResult) { r.Capture.historyStdin.CloseFailed=true },
		"write-failed": func(r *askChildResult) { r.Capture.protocolWriteFailed = true; r.Capture.protocolIOFailed = true },
		"io-failed": func(r *askChildResult) { r.Capture.protocolIOFailed = true },
		"unconfirmed-write": func(r *askChildResult) { r.Capture.protocolWritesComplete = false },
		"missing-final-response": func(r *askChildResult) { r.Capture.protocolReason = "history_protocol_response_incomplete" },
	} {
		t.Run(name, func(t *testing.T) {
			result := historyCollectedTestResult()
			mutate(&result)
			if unslopHistoryChildComplete(result) { t.Fatal("unproved collection accepted") }
		})
	}
}

func TestUnslopHistoryCollectionProjectionDoesNotRepairNativeFailure(t *testing.T) {
	observation := &unslopHistoryCollectionObservation{rootPID: 7, rootCreation: 11, observed: 1, signalled: 1, total: 1, totalKnown: true, collected: true}
	for _, reaped := range []bool{false, true} {
		result := historyCollectedTestResult()
		result.Reaped = reaped
		publishUnslopHistoryCollection(&result, observation)
		if result.Reaped != reaped { t.Fatal("projection changed native collection") }
		if unslopHistoryChildComplete(result) != reaped { t.Fatal("deferred close failure lost") }
	}
	result := historyCollectedTestResult()
	observation.totalKnown = false
	publishUnslopHistoryCollection(&result, observation)
	if result.CollectionTotalKnown || unslopHistoryChildComplete(result) { t.Fatal("unknown total became proof") }
}

func TestUnslopHistoryCurrentContextRejectsMixedInputBeforeSpawn(t *testing.T) {
	plan, err := buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read", map[string]any{"threadId": "synthetic-owned", "includeTurns": false}}})
	if err != nil { t.Fatal(err) }
	defer plan.erase()
	ctx := context.WithValue(context.Background(), unslopHistoryProtocolKey{}, plan)
	ctx = context.WithValue(ctx, askChildCaptureKey{}, askChildCapture{Stdin: []byte("unrelated")})
	result := askOwnedChild(ctx, "must-not-start")
	if result.Started || result.Reaped || result.CollectionTotalKnown { t.Fatal("invalid context reached native launch") }
	if result.Capture == nil || result.Capture.protocolReason != "history_protocol_plan_invalid" { t.Fatal("missing fixed rejection") }
}

func TestUnslopHistoryCompletedMetadataNeverMintsPacketOrigin(t *testing.T) {
	if !unslopHistoryChildComplete(historyCollectedTestResult()) { t.Fatal("positive control missing") }
	if runtime := (unslopPurposeRuntime{}); runtime.history != nil || runtime.historyThreads != [2]string{} { t.Fatal("normal selectors activated") }
	result := consumeUnslopPacket(context.Background(), unslopPacketAccess{})
	raw, err := json.Marshal(result)
	if err != nil { t.Fatal(err) }
	if result.Qualified || result.IndependentlyAssessed || result.GeneralAccessGranted || len(result.Cases) != 0 { t.Fatal("history became packet authority") }
	if !json.Valid(raw) { t.Fatal("consumer result invalid") }
}
