package store

import (
	"bytes"
	"encoding/json"
	"strconv"

	bolt "go.etcd.io/bbolt"
)

// CurrentNote is the read-only projection used by clients preparing an edit.
type CurrentNote struct {
	ID         int64  `json:"id"`
	Note       string `json:"note"`
	NoteSHA256 string `json:"noteSha256"`
	Revision   uint64 `json:"revision"`
	StateID    string `json:"stateId"`
}

func (s *Store) CurrentNote(id int64) (CurrentNote, error) {
	if id <= 0 {
		return CurrentNote{}, invalid("invalid_item_id")
	}
	var result CurrentNote
	err := s.view(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bItems).Get(key(uint64(id)))
		if raw == nil {
			return &Error{Code: CodeNotFound, Reason: "item"}
		}
		fields, err := object(raw)
		if err != nil {
			return err
		}
		var note string
		value := bytes.TrimSpace(fields["note"])
		if len(value) != 0 && !bytes.Equal(value, []byte("null")) {
			if value[0] != '"' || !validNoteSurrogates(value) || json.Unmarshal(value, &note) != nil {
				return invalid("invalid_note")
			}
		}
		result = CurrentNote{ID: id, Note: note, NoteSHA256: digest([]byte(note)), Revision: revision(tx), StateID: stateID(tx)}
		if !validDigest(result.StateID) {
			return invalid("invalid_state_id")
		}
		return nil
	})
	if err != nil {
		return CurrentNote{}, err
	}
	return result, nil
}

// encoding/json replaces unpaired Unicode surrogate escapes with U+FFFD.
// Reject those before decoding so the projection does not silently change a
// note that the Rust editor cannot read. object already checked JSON syntax.
func validNoteSurrogates(raw []byte) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw)-1 {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		value, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value < 0xd800 || value > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
