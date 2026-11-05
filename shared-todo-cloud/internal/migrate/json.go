package migrate

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"reflect"
	"strconv"
	"unicode/utf8"
)

func validateJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return failure("invalid_json")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, err := d.Token()
		if err != nil {
			return failure("invalid_json")
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return failure("duplicate_or_invalid_json_key")
				}
				seen[name] = true
				if err := walk(); err != nil {
					return err
				}
			}
			if end, err := d.Token(); err != nil || end != json.Delim('}') {
				return failure("invalid_json")
			}
		case json.Delim('['):
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			if end, err := d.Token(); err != nil || end != json.Delim(']') {
				return failure("invalid_json")
			}
		case json.Delim('}'), json.Delim(']'):
			return failure("invalid_json")
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return failure("invalid_json")
	}
	return nil
}

func object(raw []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, failure("expected_object")
	}
	return m, nil
}

func stringValue(raw []byte) (string, bool) {
	raw = bytes.TrimSpace(raw)
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func intValue(raw []byte) (int64, bool) {
	n, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	return n, err == nil
}

func uintValue(raw []byte) (uint64, bool) {
	n, err := strconv.ParseUint(string(bytes.TrimSpace(raw)), 10, 64)
	return n, err == nil
}

type row struct {
	id     int64
	fields map[string]json.RawMessage
}

func parseDocument(raw []byte) ([]row, Counts, error) {
	if err := validateJSON(raw); err != nil {
		return nil, Counts{}, err
	}
	root, err := object(raw)
	if err != nil {
		return nil, Counts{}, err
	}
	var items []json.RawMessage
	if json.Unmarshal(root["items"], &items) != nil || items == nil {
		return nil, Counts{}, failure("invalid_items")
	}
	counts := Counts{Items: len(items), RootFields: len(root)}
	fields, statuses, ids := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	rows := make([]row, 0, len(items))
	var maxID int64
	for _, item := range items {
		m, err := object(item)
		if err != nil {
			return nil, Counts{}, err
		}
		id, ok := intValue(m["id"])
		if !ok || id <= 0 || id == math.MaxInt64 || ids[id] {
			return nil, Counts{}, failure("invalid_or_duplicate_item_id")
		}
		ids[id] = true
		maxID = max(maxID, id)
		for key := range m {
			fields[key] = true
		}
		// Count all legacy status types, including missing and explicit null.
		statuses[string(bytes.TrimSpace(m["status"]))] = true
		rows = append(rows, row{id: id, fields: m})
	}
	next, ok := intValue(root["next_id"])
	if !ok || next <= maxID || next <= 0 {
		return nil, Counts{}, failure("invalid_next_id")
	}
	counts.ItemFields, counts.StatusKinds = len(fields), len(statuses)
	return rows, counts, nil
}

// UseNumber retains numeric lexemes and large integers. Object key order and
// insignificant whitespace may change in /state, but spelling and types may not.
func equalJSON(a, b []byte) bool {
	decode := func(raw []byte) (any, bool) {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		err := d.Decode(&v)
		return v, err == nil
	}
	x, ok := decode(a)
	if !ok {
		return false
	}
	y, ok := decode(b)
	return ok && reflect.DeepEqual(x, y)
}
