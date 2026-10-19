package sourcepacket

import "errors"

// MetadataDiagnostic exposes only closed categories and numeric observations.
// It never carries a path, Git object ID, configuration key or OS error text.
type MetadataDiagnostic struct {
	Code, Kind string
	Entries    int
	FileBytes  int64
	TotalBytes int64
}

func (d MetadataDiagnostic) Valid() bool {
	if d.Entries < 0 || d.Entries > maxFiles+1 || d.FileBytes < 0 || d.TotalBytes < 0 || d.TotalBytes > 640<<20 {
		return false
	}
	switch d.Code {
	case "context", "directory", "enumerate", "entry_limit", "path", "policy", "open", "stat", "file_limit", "read", "total_limit", "write":
	default:
		return false
	}
	switch d.Kind {
	case "directory", "config", "head", "index", "refs", "objects", "other":
		return true
	}
	return false
}

type metadataProofError struct{ diagnostic MetadataDiagnostic }

func (e *metadataProofError) Error() string { return "checkout proof rejected at metadata" }

// MetadataFailure returns a detached, validated observation, never an arbitrary
// error's text. Callers may serialize it alongside the existing rejection code.
func MetadataFailure(err error) *MetadataDiagnostic {
	var e *metadataProofError
	if !errors.As(err, &e) || e == nil || !e.diagnostic.Valid() {
		return nil
	}
	d := e.diagnostic
	return &d
}

func metadataKind(slash string) string {
	switch slash {
	case ".git/config":
		return "config"
	case ".git/HEAD":
		return "head"
	case ".git/index":
		return "index"
	}
	if len(slash) >= 10 && slash[:10] == ".git/refs/" {
		return "refs"
	}
	if len(slash) >= 13 && slash[:13] == ".git/objects/" {
		return "objects"
	}
	return "other"
}

type secureReadError struct {
	code string
	size int64
}

func (*secureReadError) Error() string { return "secure file read rejected" }
