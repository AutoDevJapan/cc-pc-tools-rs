//go:build windows && amd64

package main

import "testing"

func TestCollectionObservationDoesNotHideUnreadableProcesses(t *testing.T) {
	v := collectionObservation(0, 3)
	if v["queryable_account_process_count"] != 0 || v["unreadable_process_count"] != 3 || v["scm_stopped"] != true {
		t.Fatal(v)
	}
	if _, exists := v["shared_service_idle"]; exists {
		t.Fatal("queryable census claimed global process absence")
	}
}
