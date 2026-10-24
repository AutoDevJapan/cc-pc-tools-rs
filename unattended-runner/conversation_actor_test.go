package main

import "testing"

func TestConversationActorCLIRequiresCompletePair(t *testing.T) {
	base := conversationAskArgs()
	args := append(append([]string{}, base...), "--scheduler-item", "3277", "--scheduler-job", "20261025-063120-todo-3277")
	if _, err := parseConversationCLI(args); err != nil {
		t.Fatal("a complete actor pair must reach scope verification")
	}
	for _, tail := range [][]string{{"--scheduler-item", "3277"}, {"--scheduler-job", "job-3277"}, {"--scheduler-item", "0", "--scheduler-job", "job-3277"}, {"--scheduler-item", "3277", "--scheduler-job", "../private"}} {
		if _, err := parseConversationCLI(append(append([]string{}, base...), tail...)); err == nil {
			t.Fatal("incomplete or malformed actor accepted")
		}
	}
}
