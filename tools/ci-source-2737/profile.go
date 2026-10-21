package sourcepacket

// Profile selects a compiled finite identity, never an arbitrary repository,
// branch, host path, or command. The zero profile preserves the original MCP α
// protocol. Press is the separately authorized PR175 check owned by ToDo2766.
type Profile struct {
	RepoID                                 int64
	PR                                     int
	Repository, HeadRef, Workflow, BaseSHA string
}

const PressProduct = "press-2766"
const PressBase = "4dc5c332d54d19325f122df1f0e4a52e9dd3a629"

func (p Pin) Profile() (Profile, bool) {
	if p.Product == "" && p.Workflow == "" {
		return Profile{RepoID, PR, "AutoDevJapan/GoDD-MCP-Alpha", HeadRef, workflowPath, ""}, true
	}
	if p.Product != PressProduct {
		return Profile{}, false
	}
	switch p.Workflow {
	case "local-checks.yml", "rust-v2-https.yml", "windows-hidden-fixtures.yml":
		return Profile{1386702183, 175, "AutoDevJapan/GoDD-Press", "fix/todo2642-schedule-predecessor-overflow", ".github/workflows/" + p.Workflow, PressBase}, true
	}
	return Profile{}, false
}
