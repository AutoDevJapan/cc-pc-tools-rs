package main

func validServiceID(id string) bool {
	switch id {
	case "operations", "opencodex", "godd_ao", "opencodex_watchdog":
		return true
	default:
		return false
	}
}
