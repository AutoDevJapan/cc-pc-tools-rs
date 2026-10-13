package main

import "testing"

func TestServiceLauncherOnlyAcceptsCompiledNonUIIDs(t *testing.T) {
	for _, id := range []string{"operations", "opencodex", "godd_ao", "opencodex_watchdog"} {
		if !validServiceID(id) {
			t.Fatal("known service rejected")
		}
	}
	for _, id := range []string{"", "chrome", "owner-desktop", "task;anything", "fixture", "../../private"} {
		if validServiceID(id) {
			t.Fatal("arbitrary or GUI service accepted")
		}
	}
}
