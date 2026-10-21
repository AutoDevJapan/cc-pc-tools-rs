package main

import (
	"path/filepath"
	"strings"
)

type capacityUpdateLeaf struct{ kind, target, source, oldSHA, newSHA string }
type capacityUpdateProfile struct {
	root, evidence, evidenceSHA string
	leaves                      []capacityUpdateLeaf
}

const capacity3085Source = "D:/CC/Work/unattended-operations-20261013-wt/todo-3085-capacity-decision"

// Filled only from this reviewed build's independently observed software
// ownership and fingerprints. No input manifest, caller identity or directory
// grant can extend these two executable leaves or grant private data access.
var capacity3085UpdateProfile = capacityUpdateProfile{
	root:        capacity3085Source,
	evidence:    capacity3085Source + "/docs/evidence/capacity-3085-runtime.json",
	evidenceSHA: "84fec3eb34176718015e9384c374036f15e32013e15683244b3cb543c5acce76",
	leaves: []capacityUpdateLeaf{
		{"capacity", "D:/CC/Tools/capacity-audit-3h/capacity-audit-3h.exe", capacity3085Source + "/.ci-work/capacity-audit-3h.exe", "efc30235d69e53b7cbcd181db922dc354a1a268a221356ef856e916643790329", "43d2e177ec55a1f3f0a281063105d5bd0ad2c6be89f1ab1f14a078773d724a3d"},
		{"todo", "D:/CC/Tools/todo/todo.exe", capacity3085Source + "/.ci-work/todo-3085.exe", "ba1d65381bd31c38d05cf3798973427e2f0271dbc9d664afbfe91501a8de6811", "3ec3ffb1de995d1f900398386004b69a4aed109fb9f0c91319ac1b32ba467b44"},
	},
}

func capacity3085FlagPresent(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimLeft(arg, "-")
		if arg == "verify-capacity-runtime-update" || strings.HasPrefix(arg, "verify-capacity-runtime-update=") {
			return true
		}
	}
	return false
}

func capacity3085RuntimeReceipt(path string) object {
	return capacity3085RuntimeWith(path, capacity3085UpdateProfile, legacyMatch, consoleSourceVerified)
}

func capacity3085RuntimeWith(path string, profile capacityUpdateProfile, protected func(string) bool, source func(string) bool) object {
	failure := object{"qualified": false}
	for _, leaf := range profile.leaves {
		if !same(path, leaf.target) {
			continue
		}
		evidence := []consoleEvidence{{profile.root, profile.evidence, profile.evidenceSHA}}
		pins := []consoleLeaf{{path: leaf.target, pins: []consolePin{{sha256: leaf.oldSHA, evidence: evidence}, {sha256: leaf.newSHA, evidence: evidence}}}}
		if !consoleRuntimeWith(path, pins, protected, source) || !within(profile.root, leaf.source) || !runtimeLeaf(leaf.source, filepath.Dir(leaf.source), protected, true) || !consolePinnedFile(leaf.source, leaf.newSHA) {
			return failure
		}
		return object{"qualified": true, "domain": "capacity-runtime-update-3085", "todo_id": 3085, "target_kind": leaf.kind, "old_sha256": leaf.oldSHA, "new_sha256": leaf.newSHA, "source_receipt_sha256": profile.evidenceSHA, "data_access_granted": false, "original_registry_recovered": false}
	}
	return failure
}
