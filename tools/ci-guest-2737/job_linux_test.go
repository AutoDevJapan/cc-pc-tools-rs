package main

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestLivePermitBindsAcknowledgementAndDrainsCancellation(t *testing.T) {
	for _, kind := range []string{"accepted", "wrong-stage", "closed", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			acks := make(chan liveJobAck, 1)
			w := liveWire{out: json.NewEncoder(a), conn: a, acks: acks, ctx: ctx}
			done := make(chan error, 1)
			go func() { done <- w.permit(ctx, "fixed", liveJobEvent{ID: "fixed", Stage: "start"}) }()
			var observed liveJobEvent
			if json.NewDecoder(b).Decode(&observed) != nil || observed.ID != "fixed" || observed.Stage != "start" {
				t.Fatal("actual event not delivered")
			}
			switch kind {
			case "accepted":
				acks <- liveJobAck{ID: "fixed", Stage: "start", Accepted: true}
			case "wrong-stage":
				acks <- liveJobAck{ID: "fixed", Stage: "complete", Accepted: true}
			case "closed":
				close(acks)
			case "cancelled":
				cancel()
			}
			select {
			case e := <-done:
				if (e == nil) != (kind == "accepted") {
					t.Fatal("unexpected admission result")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("permit failed to drain")
			}
		})
	}
}
