package main

// A received content review supplies the original author, separately from the
// current creator. Keep the public publication tuple at this single boundary.
func skillsBindCreationReview(m skillsManifest, r skillsRequest, review skillsReceived, reg skillsRegistry) (skillsManifest, bool) {
	c, typed := skillsParsePublicReview(review.Spec)
	// Keep the original non-typed legacy contract. Public data requires a
	// qualified typed review, and every typed review binds its complete Git
	// publication, including the project instructions used by consumers.
	if !typed {
		return m, !m.publicV2 && m.Writer == r.Writer
	}
	if c.Comparison != nil || c.Commit != r.ManifestCommit || c.ManifestSHA != r.ManifestSHA || c.ManifestPath != r.ManifestPath || !skillsRegisteredGeneration(reg.Tasks[3080], c.Author.Job, r.Writer.Job) {
		return m, false
	}
	return skillsBindPublicManifest(m, r.Writer, c.Author)
}
