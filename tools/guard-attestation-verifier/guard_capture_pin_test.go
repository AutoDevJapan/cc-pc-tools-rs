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
	Issued:          true,
	Source:          "c1adc362778aae5270ceacea1214b9d17940589e",
	SourceTree:      "2137e3ff923aa680041d82f642d881305efd0e98",
	RepairTree:      "4a9e710eadbdb78e3842bb88361d9b5ce68b238e",
	ContractTree:    "7278e7e373d0c65ee7446ad54223e85b3fab565f",
	InputCount:      253,
	Producer:        "09a0de57c493e7c96a1fb1133068d3e2a8246806",
	WorkflowBlob:    "48db626c4ec54a0f61590ecb4ee24d9740ed4e70",
	HelperBlob:      "86129b43f10b7fa0c26785746b7c747d4178710b",
	RunID:           44726482600,
	Attempt:         1,
	JobID:           136255100998,
	CheckRunID:      136255100998,
	JobStarted:      "2026-11-20T11:18:17Z",
	JobCompleted:    "2026-11-20T11:19:25Z",
	ArtifactID:      14624362204,
	ArtifactName:    "guard-bootstrap-44726482600-1-136255100998",
	ArtifactCreated: "2026-11-20T11:19:20Z",
	ArtifactExpires: "2026-11-21T11:19:18Z",
	ArchiveBytes:    14659755,
	ArchiveSHA:      "86b6e1ef17657795a9e1ad4d4e3a50360e8f653a6ead43d5501ecdef3152d78a",
	PEBytes:         14360064,
	PESHA:           "9c431c58244a222c0a4b6bf8fd2449ee0f59a1b7653bf34393e4a890ce3a3589",
	ProofBytes:      298487,
	ProofSHA:        "13b7047783a33cd346f78d9a0496a89aeee2a9774bf9f9d9201247bacf9cc312",
	CaptureBytes:    830,
	CaptureSHA:      "6ee51118e681e2c4928cead9d1e4f33ae97b6376f810a1f4b7ed387b932d193d",
	AttestationID:   "70110618",
	RekorIndex:      "4023990479",
	BundleBytes:     11231,
	BundleSHA:       "e7834c6fc21c5bed1d17507c929c7bc25f8c6c7322226383b26d5a037b9d0771",
}
