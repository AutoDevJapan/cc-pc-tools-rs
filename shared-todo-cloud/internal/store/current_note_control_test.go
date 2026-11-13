package store

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestCurrentNoteInvalidRevisionReturnsErrorWithoutPanic(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
		ok   bool
	}{
		{"missing", nil, false},
		{"short", []byte{0, 0, 0}, false},
		{"long", make([]byte, 9), false},
		{"zero", make([]byte, 8), true},
		{"valid", key(1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := stateFixture(t)
			if _, err := s.Update(UpdateRequest{RequestID: "revision-fixture", Upserts: []json.RawMessage{json.RawMessage(`{"id":7,"note":"fixture"}`)}}); err != nil {
				t.Fatal(err)
			}
			if err := s.write(func(tx *bolt.Tx) error {
				if tc.raw == nil {
					return tx.Bucket(bControl).Delete(kRevision)
				}
				return tx.Bucket(bControl).Put(kRevision, tc.raw)
			}); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if recover() != nil {
					t.Fatal("malformed revision panicked instead of returning an error")
				}
			}()
			got, err := s.CurrentNote(7)
			if tc.ok {
				if err != nil || got.Revision != binary.BigEndian.Uint64(tc.raw) || got.Note != "fixture" {
					t.Fatal("valid revision rejected")
				}
			} else if !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid revision did not return a typed error")
			}
		})
	}
}
