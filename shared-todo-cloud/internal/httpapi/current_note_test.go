package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCurrentNoteRequiresExistingAPIAuthorityAndProjectsOneItem(t *testing.T) {
	s := testServer(t)
	const path = "/api/v1/items/201/note"
	for _, token := range []string{"", "invalid-fixture-token"} {
		w := perform(s, http.MethodGet, path, "", token, nil, "")
		if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "private detail") {
			t.Fatal("unauthenticated projection did not fail closed")
		}
	}
	for _, token := range []string{testSync, testAdmin} {
		w := perform(s, http.MethodGet, path, "", token, nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("authorized projection status = %d", w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("projection is cacheable")
		}
		var got struct {
			ID         int64  `json:"id"`
			Note       string `json:"note"`
			NoteSHA256 string `json:"noteSha256"`
			Revision   uint64 `json:"revision"`
			StateID    string `json:"stateId"`
		}
		dec := json.NewDecoder(w.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&got); err != nil {
			t.Fatal("projection response shape is invalid")
		}
		digest := sha256.Sum256([]byte("private detail"))
		revision, stateID, err := s.Store.Version()
		if err != nil || got.ID != 201 || got.Note != "private detail" || got.NoteSHA256 != hex.EncodeToString(digest[:]) || got.Revision != revision || got.StateID != stateID || stateID == "" {
			t.Fatal("projection does not match the selected synthetic note and snapshot")
		}
	}
}

func TestCurrentNoteRejectsInvalidAndMissingIDs(t *testing.T) {
	s := testServer(t)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/items/0/note", http.StatusBadRequest},
		{"/api/v1/items/not-an-id/note", http.StatusBadRequest},
		{"/api/v1/items/999999/note", http.StatusNotFound},
	} {
		w := perform(s, http.MethodGet, tc.path, "", testSync, nil, "")
		if w.Code != tc.want || strings.Contains(w.Body.String(), "private detail") {
			t.Fatalf("unexpected status or data for %s: %d", tc.path, w.Code)
		}
	}
}
