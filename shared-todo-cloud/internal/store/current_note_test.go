package store_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/store"
)

func TestCurrentNoteReturnsOnlyTargetAndCASDigest(t *testing.T) {
	cases := []struct {
		name    string
		note    any
		present bool
		want    string
		invalid bool
	}{
		{"ascii", "synthetic note", true, "synthetic note", false},
		{"unicode-and-newlines", "日本語と😀\r\nline\tend", true, "日本語と😀\r\nline\tend", false},
		{"long", strings.Repeat("試験😀", 3000), true, strings.Repeat("試験😀", 3000), false},
		{"empty", "", true, "", false},
		{"missing", nil, false, "", false},
		{"null", nil, true, "", false},
		{"number", 42, true, "", true},
		{"object", map[string]any{"unexpected": true}, true, "", true},
		{"array", []any{"unexpected"}, true, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := openStore(t)
			selected := map[string]any{"id": 7, "privateOtherField": "fixture-field-secret"}
			if tc.present {
				selected["note"] = tc.note
			}
			selectedJSON, err := json.Marshal(selected)
			if err != nil {
				t.Fatal(err)
			}
			seed, err := s.Update(store.UpdateRequest{
				RequestID: "synthetic-seed",
				Upserts: []json.RawMessage{
					selectedJSON,
					raw(`{"id":8,"note":"fixture-other-row-secret"}`),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, s)
			got, err := s.CurrentNote(7)
			if tc.invalid {
				requireError(t, err, store.ErrInvalid)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if got.ID != 7 || got.Note != tc.want || got.NoteSHA256 != hash([]byte(tc.want)) {
					t.Fatal("target note or UTF-8 digest mismatch")
				}
				if got.Revision != seed.Revision || got.StateID != seed.StateID || got.StateID == "" {
					t.Fatal("note and revision/state identity not bound")
				}
				encoded, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 5 {
					t.Fatal("projection response has an unexpected shape")
				}
				for _, key := range []string{"id", "note", "noteSha256", "revision", "stateId"} {
					if _, ok := fields[key]; !ok {
						t.Fatalf("projection is missing %s", key)
					}
				}
				if bytes.Contains(encoded, []byte("fixture-field-secret")) || bytes.Contains(encoded, []byte("fixture-other-row-secret")) {
					t.Fatal("projection leaked an unrelated field or row")
				}
			}
			after := snapshot(t, s)
			if before.Revision != after.Revision || before.StateID != after.StateID || !bytes.Equal(before.Document, after.Document) {
				t.Fatal("read operation mutated the ledger")
			}
		})
	}
}

func TestCurrentNoteRejectsMissingOrInvalidID(t *testing.T) {
	s, _, _ := openStore(t)
	_, err := s.CurrentNote(1)
	requireError(t, err, store.ErrNotFound)
	_, err = s.CurrentNote(0)
	requireError(t, err, store.ErrInvalid)
}

func TestCurrentNoteRejectsUnpairedSurrogatesWithoutChangingLiteralText(t *testing.T) {
	for _, tc := range []struct {
		name, rawNote, want string
		invalid             bool
	}{
		{"high", `"\ud800"`, "", true},
		{"low", `"\udc00"`, "", true},
		{"high-then-text", `"\ud800x"`, "", true},
		{"wrong-order", `"\udc00\ud800"`, "", true},
		{"pair", `"\ud83d\ude00"`, "😀", false},
		{"literal-escape", `"\\ud800"`, `\ud800`, false},
		{"replacement-character", `"�"`, "�", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := openStore(t)
			item := raw(`{"id":7,"note":` + tc.rawNote + `}`)
			if _, err := s.Update(store.UpdateRequest{RequestID: "unicode-fixture", Upserts: []json.RawMessage{item}}); err != nil {
				t.Fatal(err)
			}
			got, err := s.CurrentNote(7)
			if tc.invalid {
				requireError(t, err, store.ErrInvalid)
				return
			}
			if err != nil || got.Note != tc.want || got.NoteSHA256 != hash([]byte(tc.want)) {
				t.Fatal("unicode note or digest mismatch")
			}
		})
	}
}
