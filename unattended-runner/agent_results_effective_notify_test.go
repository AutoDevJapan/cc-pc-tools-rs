package main

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

func TestAgentResultOldNotifyIntentAcceptsQuietSenderReceipt(t *testing.T) {
	r, _ := fixture(t)
	p, err := r.recordAgentResult(resultEvent())
	if err != nil {
		t.Fatal(err)
	}
	p.NotifyOwner = true // The exact old reservation remains unchanged.
	if err := publish(r.agentResultPath(p.ID), p); err != nil {
		t.Fatal(err)
	}
	calls := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		calls++
		if !slices.Contains(args, "--notify") {
			t.Error("original retry input changed")
		}
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735","requested_notify":true,"effective_notify":false}`, nil
	}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		m := resultReadback(r, p, false)
		m["channel_id"] = "610"
		respond(w, m)
	})
	r.agentResultPhase(context.Background(), &Health{})
	saved := r.mustAgentResult(t, p.ID)
	if saved.Verified == "" || !saved.NotifyOwner || saved.EffectiveNotify == nil || *saved.EffectiveNotify {
		t.Fatal("quiet actual delivery or original notify input was not preserved")
	}
	r.agentResultPhase(context.Background(), &Health{})
	if calls != 1 {
		t.Fatal("verified old intent was resent")
	}
}

func TestAgentResultConcreteOwnerQuestionCannotBeVerifiedAsSilent(t *testing.T) {
	r, _ := fixture(t)
	p, err := r.recordAgentResult(actionRequiredResultEvent())
	if err != nil {
		t.Fatal(err)
	}
	r.Run = func(context.Context, string, ...string) (string, error) {
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735","effective_notify":false}`, nil
	}
	reads := 0
	server(t, r, func(w http.ResponseWriter, q *http.Request) { reads++; respond(w, resultReadback(r, p, false)) })
	if r.deliverAgentResult(context.Background(), &p) == nil || p.Verified != "" {
		t.Fatal("required owner action was verified without its mention")
	}
	if reads != 0 {
		t.Fatal("an inconsistent sender receipt reached final GET verification")
	}
}
