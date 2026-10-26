package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type recordingInput struct{ bytes.Buffer }

func (*recordingInput) Close() error { return nil }

func TestInputTransportCorrelatesIDsAndIgnoresOnlyBoundedNotifications(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := &recordingInput{}
	frames := make(chan frameResult, 3)
	frames <- frameResult{frame: protocolFrame{Method: "account/updated"}}
	id := int64(1)
	frames <- frameResult{frame: protocolFrame{ID: &id, Result: json.RawMessage(`{"safe":true}`)}}
	rpc := &appServerRPC{ctx: ctx, cancel: cancel, in: in, frames: frames}
	raw, err := rpc.Call(ctx, "thread/read", map[string]any{"threadId": testThread, "includeTurns": false})
	if err != nil || string(raw) != `{"safe":true}` {
		t.Fatalf("valid response not correlated: %s %v", raw, err)
	}
	var request map[string]any
	if json.Unmarshal(in.Bytes(), &request) != nil || request["id"] != float64(1) || request["method"] != "thread/read" {
		t.Fatal("wrong wire request")
	}
}

func TestInputTransportRejectsMismatchedIDsErrorsAndNotificationFlood(t *testing.T) {
	for _, which := range []string{"wrong_id", "private_error", "notification_flood", "malformed", "closed"} {
		t.Run(which, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			frames := make(chan frameResult, 18)
			id := int64(1)
			switch which {
			case "wrong_id":
				id = 2
				frames <- frameResult{frame: protocolFrame{ID: &id, Result: json.RawMessage(`{}`)}}
			case "private_error":
				frames <- frameResult{frame: protocolFrame{ID: &id, Error: json.RawMessage(`{"message":"PRIVATE-MARKER"}`)}}
			case "notification_flood":
				for i := 0; i < 18; i++ {
					frames <- frameResult{frame: protocolFrame{Method: "account/updated"}}
				}
			case "malformed":
				frames <- frameResult{err: errInputSource}
			case "closed":
				close(frames)
			}
			rpc := &appServerRPC{ctx: ctx, cancel: cancel, in: &recordingInput{}, frames: frames}
			if _, err := rpc.Call(ctx, "thread/read", map[string]any{}); err != errInputSource {
				t.Fatal("invalid response was accepted or leaked a raw error")
			}
		})
	}
}

func TestInputTransportDisallowsModelMethodsAndAlreadyCancelledCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := &recordingInput{}
	rpc := &appServerRPC{ctx: ctx, cancel: cancel, in: in}
	if _, err := rpc.Call(ctx, "turn/start", map[string]any{}); err != errInputSource || in.Len() != 0 {
		t.Fatal("model method was written")
	}
	cancel()
	if _, err := rpc.Call(ctx, "thread/read", map[string]any{}); err != errInputSource || in.Len() != 0 {
		t.Fatal("cancelled method was written")
	}
}

func TestInputDecoderRejectsOversizedOrInvalidFramesWithoutRetainingBody(t *testing.T) {
	for _, input := range []string{strings.Repeat("x", maxProtocolFrame+1) + "\n", "not-json\n"} {
		ctx, cancel := context.WithCancel(context.Background())
		frames := make(chan frameResult, 1)
		decodeInputFrames(ctx, strings.NewReader(input), frames)
		result := <-frames
		cancel()
		if result.err != errInputSource || result.frame.ID != nil || len(result.frame.Result) != 0 {
			t.Fatal("bad frame accepted or retained")
		}
	}
}

func TestInputDecoderStopsWhenConnectionIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	frames := make(chan frameResult)
	done := make(chan struct{})
	go func() { decodeInputFrames(ctx, reader, frames); close(done) }()
	cancel()
	_ = writer.Close()
	<-done
	_ = reader.Close()
}
