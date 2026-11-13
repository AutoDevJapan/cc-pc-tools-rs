package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// Projection responses may contain a note as large as the existing store.
// JSON escaping can expand each byte up to sixfold; no full snapshot is read.
const maxNoteWireBytes = 6*MaxDocumentBytes + 4096

var noteDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type CurrentNote struct {
	ID         int64  `json:"id"`
	Note       string `json:"note"`
	NoteSHA256 string `json:"noteSha256"`
	Revision   uint64 `json:"revision"`
	StateID    string `json:"stateId"`
}

func validJSONSurrogates(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
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

func (c *Client) currentNote(ctx context.Context, id int64) (CurrentNote, error) {
	if id <= 0 {
		return CurrentNote{}, errors.New("todo_remote_invalid_note_request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/items/%d/note", c.base, id), nil)
	if err != nil {
		return CurrentNote{}, errors.New("todo_remote_request_failed")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return CurrentNote{}, errors.New("todo_remote_network_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CurrentNote{}, &HTTPError{resp.StatusCode}
	}
	if resp.ContentLength > maxNoteWireBytes {
		return CurrentNote{}, errResponseTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxNoteWireBytes+1))
	if err != nil {
		return CurrentNote{}, errors.New("todo_remote_response_read_failed")
	}
	if len(raw) > maxNoteWireBytes {
		return CurrentNote{}, errResponseTooLarge
	}
	if !utf8.Valid(raw) {
		return CurrentNote{}, errors.New("todo_remote_invalid_response")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return CurrentNote{}, errors.New("todo_remote_invalid_response")
	}
	fields := make(map[string]json.RawMessage, 5)
	for decoder.More() {
		key, e := decoder.Token()
		name, ok := key.(string)
		if e != nil || !ok || fields[name] != nil {
			return CurrentNote{}, errors.New("todo_remote_invalid_response")
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return CurrentNote{}, errors.New("todo_remote_invalid_response")
		}
		fields[name] = value
	}
	if _, err = decoder.Token(); err != nil {
		return CurrentNote{}, errors.New("todo_remote_invalid_response")
	}
	if _, err = decoder.Token(); err != io.EOF || len(fields) != 5 {
		return CurrentNote{}, errors.New("todo_remote_invalid_response")
	}
	var result CurrentNote
	for _, name := range []string{"id", "note", "noteSha256", "revision", "stateId"} {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return CurrentNote{}, errors.New("todo_remote_invalid_response")
		}
	}
	// Go's decoder replaces unpaired surrogate escapes with U+FFFD. Reject
	// them before decoding so the value remains faithful to the Rust editor.
	if !validJSONSurrogates(fields["note"]) || json.Unmarshal(raw, &result) != nil {
		return CurrentNote{}, errors.New("todo_remote_invalid_response")
	}
	if result.ID != id || len(result.Note) > MaxDocumentBytes || !noteDigestPattern.MatchString(result.StateID) || !noteDigestPattern.MatchString(result.NoteSHA256) || result.NoteSHA256 != digest([]byte(result.Note)) {
		return CurrentNote{}, errors.New("todo_remote_invalid_response")
	}
	return result, nil
}
