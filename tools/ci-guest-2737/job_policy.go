package main

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"cc/product-ci/pressci"
	"time"
)

func jobPolicy(pin sourcepacket.Pin, role string) (pressci.Job, bool) {
	profile, ok := pin.Profile()
	if !ok {
		return pressci.Job{}, false
	}
	if pin.Product == sourcepacket.PressProduct {
		j, ok := pressci.RoleJob(pin.Workflow, role)
		if !ok || (pin.Workflow == "local-checks.yml") != (pin.PullHeadSHA == "") {
			return pressci.Job{}, false
		}
		return j, true
	}
	timeout, osName := 30, "Linux"
	switch role {
	case "verify":
	case "ci-gate":
		timeout = 5
	case "windows-native-check":
		timeout, osName = 20, "Windows"
	default:
		return pressci.Job{}, false
	}
	return pressci.Job{Workflow: profile.Workflow, ID: role, Name: role, OS: osName, TimeoutMinutes: timeout}, true
}
func jobLabel(pin sourcepacket.Pin, role string, run int64, attempt int) string {
	if _, ok := jobPolicy(pin, role); !ok {
		return ""
	}
	if pin.Product == sourcepacket.PressProduct {
		return pressci.RuntimeLabel(pin.Workflow, role, run, attempt)
	}
	return normalci.RuntimeLabel(role, run)
}
func jobEventRef(pin sourcepacket.Pin) (string, string) {
	profile, ok := pin.Profile()
	if !ok {
		return "", ""
	}
	if pin.PullHeadSHA != "" {
		return "pull_request", "refs/pull/175/merge"
	}
	return "workflow_dispatch", "refs/heads/" + profile.HeadRef
}
func liveWorkspaceFor(pin sourcepacket.Pin) string {
	if pin.Product == sourcepacket.PressProduct {
		return "/home/ci2737/work/GoDD-Press/GoDD-Press"
	}
	return liveWorkspace
}
func jobGuestTimeout(pin sourcepacket.Pin, role string) time.Duration {
	if pin.Product == sourcepacket.PressProduct {
		if j, ok := jobPolicy(pin, role); ok {
			return time.Duration(j.TimeoutMinutes+10) * time.Minute
		}
	}
	return 40 * time.Minute
}
