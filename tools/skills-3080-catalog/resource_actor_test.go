package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

const resourceActorError = "support reading actor: "

// A metadata assessor is not a full-text reader. Both reviewed source formats
// use this rule; old rows without actor provenance remain historical snapshots.
func validateResourceActor(reader string, saved, read, scan bool, readBy, scanBy *string) error {
	if reader == "" || (read && scan) || ((read || scan) && !saved) {
		return errors.New(resourceActorError + "primary reader or reading state is invalid")
	}
	switch {
	case scan:
		if readBy != nil || scanBy == nil || *scanBy != reader {
			return errors.New(resourceActorError + "scan-only row needs only its actual scanner")
		}
	case read:
		if scanBy != nil || readBy == nil || *readBy != reader {
			return errors.New(resourceActorError + "full-text row needs only its actual reader")
		}
	default:
		if readBy != nil || scanBy != nil {
			return errors.New(resourceActorError + "unread row acquired a reader or scanner")
		}
	}
	return nil
}

func primaryReaderFingerprint(rows []string) string {
	ordered := append([]string(nil), rows...)
	sort.Strings(ordered)
	h := sha256.Sum256([]byte(strings.Join(ordered, "")))
	return hex.EncodeToString(h[:])
}
