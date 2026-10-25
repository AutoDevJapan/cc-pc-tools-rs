package main

import "strings"

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
