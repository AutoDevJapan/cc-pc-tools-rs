package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestIntakeStatusLegacyContextRetainsTaskAndExplainsNextStep(t *testing.T) {
	for _, todo := range []uint64{0, 42} {
		t.Run(strconv.FormatUint(todo, 10), func(t *testing.T) {
			q := &Request{Message: "600", Thread: "500", Author: "300", Todo: todo, Phase: "origin_pending", Text: "private original owner text"}
			r := &Runner{State: &State{Requests: map[string]*Request{q.Message: q}}}
			before, err := json.Marshal(r.State)
			if err != nil {
				t.Fatal(err)
			}
			loads := 0
			var out strings.Builder
			code := intakeCLIWith([]string{"status"}, &out, func(_ string, readOnly bool) (*Runner, func(), error) {
				loads++
				if !readOnly {
					t.Error("status requested mutable or credential-bearing input")
				}
				return r, func() {}, nil
			}, nil)
			var result struct {
				Metadata bool `json:"metadata_only"`
				Items    []struct {
					Todo             uint64 `json:"todo_id"`
					Phase            string `json:"phase"`
					Owner            string `json:"pending_owner"`
					Next             string `json:"next_action"`
					MessageTypeKnown bool   `json:"message_type_known"`
					AttachmentsKnown bool   `json:"attachments_known"`
				} `json:"items"`
			}
			if code != 0 || json.Unmarshal([]byte(out.String()), &result) != nil || !result.Metadata || len(result.Items) != 1 || loads != 1 {
				t.Fatal("normal read-only status failed", code)
			}
			item := result.Items[0]
			if item.Todo != todo || item.Phase != "origin_pending" || item.Owner != "OMB" || item.Next != "review_original_context_and_existing_task_linkage" {
				t.Fatalf("legacy context lost responsibility or original task: %+v", item)
			}
			if item.MessageTypeKnown || item.AttachmentsKnown {
				t.Fatal("status inferred message type or attachments from an unobserved legacy input")
			}
			after, err := json.Marshal(r.State)
			if err != nil || !bytes.Equal(before, after) || strings.Contains(out.String(), q.Text) {
				t.Fatal("status changed the canonical request or exposed its body")
			}
		})
	}
}

func TestIntakeStatusContextAdvicePreservesExplicitRoutingAndOtherPhases(t *testing.T) {
	for _, tc := range []struct{ phase, owner, next, wantOwner, wantNext string }{
		{"origin_pending", "existing-worker", "existing-context-check", "existing-worker", "existing-context-check"},
		{"origin_pending", "", "existing-context-check", "OMB", "existing-context-check"},
		{"origin_pending", "existing-worker", "", "existing-worker", "review_original_context_and_existing_task_linkage"},
		{"captured", "", "", "", ""},
		{"registered", "", "", "", ""},
		{"reported", "", "", "", ""},
		{"history_watch", "", "", "", ""},
		{"task_binding_pending", "", "", "", ""},
		{"context_pending", "", "", "", ""},
		{"source_context_pending", "", "", "", ""},
	} {
		t.Run(tc.phase+"/"+tc.owner+"/"+tc.next, func(t *testing.T) {
			q := &Request{Message: "600", Todo: 42, Phase: tc.phase, PendingOwner: tc.owner, NextAction: tc.next}
			r := &Runner{State: &State{Requests: map[string]*Request{q.Message: q}}}
			var out strings.Builder
			code := intakeCLIWith([]string{"status"}, &out, func(string, bool) (*Runner, func(), error) { return r, func() {}, nil }, nil)
			var result struct {
				Items []struct {
					Owner string `json:"pending_owner"`
					Next  string `json:"next_action"`
				} `json:"items"`
			}
			if code != 0 || json.Unmarshal([]byte(out.String()), &result) != nil || len(result.Items) != 1 || result.Items[0].Owner != tc.wantOwner || result.Items[0].Next != tc.wantNext {
				t.Fatal("status changed explicit routing or invented work for another phase", out.String())
			}
			if q.PendingOwner != tc.owner || q.NextAction != tc.next || q.Phase != tc.phase {
				t.Fatal("status persisted its explanatory fallback")
			}
		})
	}
}
