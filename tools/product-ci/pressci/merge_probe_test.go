package pressci

// This opt-in diagnostic concerns only workflow blobs and merge-file behavior.
// It is neither full-product physical Git proof nor official CI acceptance.
type workflowMergeInput struct {
	Name                              string
	Base, Candidate, Main             []byte
	BaseBlob, CandidateBlob, MainBlob string
}
type workflowMergeResult struct {
	Name                              string
	BaseBlob, CandidateBlob, MainBlob string
	Exit, ConflictRegions             int
}
