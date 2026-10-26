package main

import "strings"

// Both creator and admitted readers reuse the exact native CreateNew proof.
// Publication and content-review acceptance were checked by that accepted
// issuer; a later read must not acquire a new dependency on remote availability
// or reopen the original review as though the reader were its author.
type skillsCreatedChecks struct {
	installed func(*skillsFS) (skillsProducer, bool)
	native    func(*skillsFS, object, skillsConsumeRequest) (skillsCreatedEvidence, bool)
	manifest  func(skillsCreatedEvidence, skillsRequest, string, skillsProducer) ([]byte, bool)
	sources   func(skillsManifest, *skillsFS) bool
}

func skillsCreatedProvenance(creation skillsRequest, c skillsConsumeRequest, creator object, s *skillsFS) (skillsManifest, skillsCreatedEvidence, skillsProducer, string) {
	return skillsCreatedProvenanceWith(creation, c, creator, s, skillsCreatedChecks{skillsInstalledProducer, skillsReadCreationNative, skillsCreatedPublicManifest, skillsSourcesBound})
}

// Internal test seams are never request fields. No central leaf is opened here.
func skillsCreatedProvenanceWith(creation skillsRequest, c skillsConsumeRequest, creator object, s *skillsFS, checks skillsCreatedChecks) (skillsManifest, skillsCreatedEvidence, skillsProducer, string) {
	var m skillsManifest
	var e skillsCreatedEvidence
	var current skillsProducer
	if creation.Writer != c.Creator || !skillsDigest(c.RequestSHA) {
		return m, e, current, "creation_request"
	}
	current, ok := checks.installed(s)
	if !ok {
		return m, e, current, "installed_producer"
	}
	e, ok = checks.native(s, creator, c)
	if !ok {
		return m, e, current, "creation_native"
	}
	if !skillsCreationHeaderBound(e, creation, c.RequestSHA, current) {
		return m, e, current, "creation_header"
	}
	raw, ok := checks.manifest(e, creation, c.RequestSHA, current)
	if !ok {
		return m, e, current, "created_manifest"
	}
	m, ok = skillsParseManifest(raw)
	if !ok || m.Writer != c.Creator {
		return m, e, current, "manifest_shape"
	}
	s.failureStage = ""
	if !checks.sources(m, s) {
		stage := s.failureStage
		if stage == "" {
			stage = "source_provenance"
		}
		return m, e, current, stage
	}
	if !skillsCreatedBound(e, creation, c.RequestSHA, m, e.Producer) {
		return m, e, current, "creation_binding"
	}
	return m, e, current, ""
}

func skillsCreatedFailureReason(stage string) string {
	switch {
	case stage == "":
		return ""
	case stage == "installed_producer" || stage == "manifest_shape" || strings.HasPrefix(stage, "source_"):
		return "source"
	case stage == "created_manifest":
		return "manifest"
	default:
		return "creation_evidence"
	}
}

// This header is only received from skillsReadCreationNative, after the exact
// canonical tool issuer/call/request/time/regular-session checks. It is not a
// request-supplied receipt or an independent bool that classifies source data.
func skillsCreationHeaderBound(e skillsCreatedEvidence, r skillsRequest, sha string, current skillsProducer) bool {
	return e.Domain == "skills-3080-created-v2" && r.Domain == "skills-3080-operation-v1" && r.Review != nil && e.Review == *r.Review && e.Writer == r.Writer &&
		skillsWriterValid(e.Writer) && skillsDigest(sha) && e.RequestSHA == sha && e.ManifestCommit == r.ManifestCommit && e.ManifestPath == r.ManifestPath && e.ManifestSHA == r.ManifestSHA &&
		skillsGitObjectID(e.ManifestCommit) && skillsDigest(e.ManifestSHA) && skillsRelative(e.ManifestPath) && strings.HasPrefix(e.ManifestPath, "docs/skills-3080-operations/") && strings.HasSuffix(e.ManifestPath, ".json") &&
		len(e.Files) > 0 && len(e.Files) <= 128 && len(e.Directories) > 0 && skillsAcceptedProducer(e.Producer, current)
}

func skillsCreatedPublicManifest(e skillsCreatedEvidence, r skillsRequest, sha string, current skillsProducer) ([]byte, bool) {
	return skillsCreatedPublicManifestWith(e, r, sha, current, sourceVerified, func(root, commit, path string) ([]byte, bool) {
		b, err := permanentGitAt(root, "cat-file", "blob", commit+":"+path)
		return b, err == nil && len(b) <= 16<<20
	})
}

// The accepted original producer verified PUBLIC main/notice/review before
// issuing CreateNew. Reuse that proven publication, the exact immutable Git
// object and SHA256; a network-disabled reader does not reclassify publication
// or silently enable network. Ordinary new creation still uses skillsMerged.
func skillsCreatedPublicManifestWith(e skillsCreatedEvidence, r skillsRequest, sha string, current skillsProducer, source func(string) bool, blob func(string, string, string) ([]byte, bool)) ([]byte, bool) {
	if !skillsCreationHeaderBound(e, r, sha, current) || !source(e.Writer.Workspace) {
		return nil, false
	}
	b, ok := blob(e.Writer.Workspace, e.ManifestCommit, e.ManifestPath)
	return b, ok && len(b) > 0 && len(b) <= 16<<20 && hashBytes(b) == e.ManifestSHA
}
