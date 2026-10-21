package pressci

import (
	sourcepacket "cc/ci-source-2737"
	"context"
	"errors"
)

type SourceOutline struct {
	Source                                                       sourcepacket.Pin
	RepoID                                                       int64
	PR                                                           int
	ArchiveBytes                                                 int
	ArchiveSHA256                                                string
	TreeEntries, Blobs                                           int
	ArchiveBlobIDsVerified, CommitIDVerified                     bool
	PhysicalCheckoutVerified, RunnerAdmission, OfficialCISuccess bool
}

// InspectSource fetches and validates source only in RAM. It never calls the
// materializer or exposes an entry name, command, commit author or source body.
func InspectSource(ctx context.Context, pin sourcepacket.Pin) (SourceOutline, error) {
	if pin.Product != sourcepacket.PressProduct {
		return SourceOutline{}, errors.New("Press inspection profile rejected")
	}
	p, e := sourcepacket.Fetch(ctx, pin)
	if e != nil {
		return SourceOutline{}, e
	}
	defer clear(p.Archive)
	defer clear(p.CommitPayload)
	out := SourceOutline{Source: pin, RepoID: RepoID, PR: PR, ArchiveBytes: len(p.Archive), ArchiveSHA256: p.ArchiveSHA256, TreeEntries: len(p.TreeEntries), ArchiveBlobIDsVerified: true, CommitIDVerified: true}
	for _, entry := range p.TreeEntries {
		if entry.Type == "blob" {
			out.Blobs++
		}
	}
	return out, nil
}
