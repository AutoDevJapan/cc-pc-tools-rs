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

var projectionDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

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

// Check the fields before Go's decoder collapses duplicates or accepts
// missing or null numbers as zero.
func readProjectionFields(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	invalid := errors.New("todo_remote_invalid_response")
	if !utf8.Valid(raw) {
		return nil, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, invalid
	}
	fields := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || fields[name] != nil || len(fields) == len(names) {
			return nil, invalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, invalid
		}
		fields[name] = value
	}
	if _, err = decoder.Token(); err != nil {
		return nil, invalid
	}
	if _, err = decoder.Token(); err != io.EOF || len(fields) != len(names) {
		return nil, invalid
	}
	for _, name := range names {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, invalid
		}
	}
	return fields, nil
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
	fields, err := readProjectionFields(raw, "id", "note", "noteSha256", "revision", "stateId")
	if err != nil {
		return CurrentNote{}, err
	}
	var result CurrentNote
	// Go's decoder replaces unpaired surrogate escapes with U+FFFD. Reject
	// them before decoding so the value remains faithful to the Rust editor.
	if !validJSONSurrogates(fields["note"]) || json.Unmarshal(raw, &result) != nil {
		return CurrentNote{}, errors.New("todo_remote_invalid_response")
	}
	if result.ID != id || len(result.Note) > MaxDocumentBytes || !projectionDigestPattern.MatchString(result.StateID) || !projectionDigestPattern.MatchString(result.NoteSHA256) || result.NoteSHA256 != digest([]byte(result.Note)) {
		return CurrentNote{}, errors.New("todo_remote_invalid_response")
	}
	return result, nil
}
