package assetio

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

func object(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, ErrInvalidInput
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if start, err := d.Token(); err != nil || start != json.Delim('{') {
		return nil, ErrInvalidInput
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, ErrInvalidInput
		}
		if _, exists := fields[key]; exists {
			return nil, ErrInvalidInput
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, ErrInvalidInput
		}
		fields[key] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, ErrInvalidInput
	}
	return fields, nil
}

func stringValue(raw []byte) (string, bool) {
	raw = bytes.TrimSpace(raw)
	var value string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

// identity ignores key order/whitespace but preserves exact numeric tokens and
// case. It is only used on reference metadata, never on full items or files.
func identity(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return ""
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return digest(canonical)
}

func equalJSON(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	return identity(a) == identity(b)
}

// Archives must be unambiguous JSON, including their unknown nested fields.
// UseNumber avoids any conversion of large IDs/counts/entry values to float64.
func validateJSON(raw []byte) error {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return ErrArchiveDocument
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := d.Token()
		if err != nil {
			return ErrArchiveDocument
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return ErrArchiveDocument
				}
				seen[name] = true
				if err := walk(); err != nil {
					return err
				}
			}
			if end, err := d.Token(); err != nil || end != json.Delim('}') {
				return ErrArchiveDocument
			}
		case json.Delim('['):
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			if end, err := d.Token(); err != nil || end != json.Delim(']') {
				return ErrArchiveDocument
			}
		case json.Delim('}'), json.Delim(']'):
			return ErrArchiveDocument
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrArchiveDocument
	}
	return nil
}
