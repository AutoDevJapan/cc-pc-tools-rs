package main

// This file contains data only. A later, reviewed issuance supplies the
// server-assigned capture tuple; no caller input can substitute it.
type guardCapturePin struct {
	Issued bool

	Source        string
	SourceTree    string
	RepairTree    string
	ContractTree  string
	InputCount    int
	Producer      string
	WorkflowBlob  string
	HelperBlob    string

	RunID         int64
	Attempt       int
	JobID         int64
	CheckRunID    int64
	JobStarted    string
	JobCompleted  string
	ArtifactID    int64
	ArtifactName  string
	ArtifactCreated string
	ArtifactExpires string

	ArchiveBytes  int64
	ArchiveSHA    string
	PEBytes       int64
	PESHA         string
	ProofBytes    int64
	ProofSHA      string
	CaptureBytes  int64
	CaptureSHA    string
	AttestationID string
	RekorIndex    string
	BundleBytes   int64
	BundleSHA     string
}

var guardCaptureNext = guardCapturePin{
	Issued:       false,
	Source:       "c1adc362778aae5270ceacea1214b9d17940589e",
	SourceTree:   "2137e3ff923aa680041d82f642d881305efd0e98",
	RepairTree:   "4a9e710eadbdb78e3842bb88361d9b5ce68b238e",
	ContractTree: "7278e7e373d0c65ee7446ad54223e85b3fab565f",
	InputCount:   253,
}
