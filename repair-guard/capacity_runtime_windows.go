package main

const capacity3085Source = "D:/CC/Work/unattended-operations-20261013-wt/todo-3085-capacity-decision"
const capacity3085Evidence = capacity3085Source + "/docs/evidence/capacity-3085-runtime.json"
const capacity3085EvidenceSHA = "82213a5c855a297f4ebbea7a5d408eb1b9168ca04c6f090b4fa55c21894109a4"

// The owner's exact 3085 repair scope, current task action, canonical ToDo
// source, fresh fingerprints and reviewed build fingerprints identify two
// leaves. This neither recovers old protection nor qualifies old logs/state.
var capacity3085RuntimeLeaves = []consoleLeaf{
	{path: "D:/CC/Tools/capacity-audit-3h/capacity-audit-3h.exe", pins: []consolePin{
		{sha256: "efc30235d69e53b7cbcd181db922dc354a1a268a221356ef856e916643790329", evidence: []consoleEvidence{{capacity3085Source, capacity3085Evidence, capacity3085EvidenceSHA}}},
		{sha256: "4a7e46e81d0ad0c2b0e99babcee1ad0805da0b1c50c024b85638d796758d3403", evidence: []consoleEvidence{{capacity3085Source, capacity3085Evidence, capacity3085EvidenceSHA}}},
	}},
	{path: "D:/CC/Tools/todo/todo.exe", pins: []consolePin{
		{sha256: "ba1d65381bd31c38d05cf3798973427e2f0271dbc9d664afbfe91501a8de6811", evidence: []consoleEvidence{{capacity3085Source, capacity3085Evidence, capacity3085EvidenceSHA}}},
		{sha256: "f5b8d06ad4ecf6a69ceaa8a141bae97eaa464f4d65d75a23d27abe3a14cfe91f", evidence: []consoleEvidence{{capacity3085Source, capacity3085Evidence, capacity3085EvidenceSHA}}},
	}},
}

func capacity3085RuntimeAllowed(path string) bool {
	return consoleRuntimeWith(path, capacity3085RuntimeLeaves, legacyMatch, consoleSourceVerified)
}
