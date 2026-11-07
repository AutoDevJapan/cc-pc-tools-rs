package main

import (
	"path/filepath"
	"strings"
)

type skillsConsumerOrigin struct {
	Creation skillsRequest
	Creator  object
	Evidence skillsCreatedEvidence
	Current  skillsProducer
}

// Internal memoization of one real canonical issuance, never a request field
// or caller-created receipt. The exact normally registered consumer scope is
// the key; a different creator/call/request/spec cannot reuse this proof.
func skillsConsumerOriginFor(c skillsConsumerScope, reg skillsRegistry, s *skillsFS) (skillsConsumerOrigin, string) {
	key := skillsProjectionSHA(c)
	if origin, ok := s.consumerOrigins[key]; ok {
		return origin, ""
	}
	var out skillsConsumerOrigin
	creation, creator, reason := skillsConsumerCreationContext(c, reg, s)
	if reason != "" {
		return out, reason
	}
	current, ok := skillsInstalledProducer(s)
	if !ok {
		return out, "source"
	}
	e, ok := skillsReadCreationNative(s, creator, c.Consume)
	if !ok || !skillsCreationHeaderBound(e, creation, c.Consume.RequestSHA, current) {
		return out, "creation_evidence"
	}
	out = skillsConsumerOrigin{creation, creator, e, current}
	if s.consumerOrigins == nil {
		s.consumerOrigins = map[string]skillsConsumerOrigin{}
	}
	s.consumerOrigins[key] = out
	return out, ""
}

// Only called after the accepted original native CreateNew header proved the
// publication of this same commit. Use its exact local immutable Git object;
// readonly use never enables network or consults today's working-tree file.
func skillsConsumerProjectFromCreated(s *skillsFS, e skillsCreatedEvidence) bool {
	return skillsConsumerProjectFromCreatedWith(s, e, permanentGitAt)
}

func skillsConsumerProjectFromCreatedWith(s *skillsFS, e skillsCreatedEvidence, git func(string, ...string) ([]byte, error)) bool {
	if !skillsWriterValid(e.Writer) || !skillsGitObjectID(e.ManifestCommit) || s.blocked(filepath.Join(e.Writer.Workspace, "AGENTS.md")) {
		return false
	}
	tree, err := git(e.Writer.Workspace, "ls-tree", e.ManifestCommit, "--", "AGENTS.md")
	fields := strings.Fields(string(tree))
	if err != nil || len(fields) != 4 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !skillsGitObjectID(fields[2]) || fields[3] != "AGENTS.md" {
		return false
	}
	body, err := git(e.Writer.Workspace, "cat-file", "blob", fields[2])
	if err != nil || len(body) == 0 || len(body) > 256<<10 || skillsGitBlobID(body) != fields[2] {
		return false
	}
	if s.bootstrapProjects == nil {
		s.bootstrapProjects = map[string][]byte{}
	}
	s.bootstrapProjects[e.ManifestCommit] = body
	return true
}
