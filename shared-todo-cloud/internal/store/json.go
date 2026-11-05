package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"
)

func invalid(reason string) error { return &Error{Code: CodeInvalid, Reason: reason} }

// object uses exact keys, unlike unmarshalling JSON into a Go struct. Duplicate
// keys are rejected rather than silently losing one of their values.
func object(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, invalid("invalid_json")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, invalid("expected_object")
	}
	m := make(map[string]json.RawMessage)
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return nil, invalid("invalid_json")
		}
		key, ok := tok.(string)
		if !ok {
			return nil, invalid("invalid_object_key")
		}
		if _, exists := m[key]; exists {
			return nil, invalid("duplicate_json_key")
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, invalid("invalid_json")
		}
		m[key] = value
	}
	if _, err = d.Token(); err != nil {
		return nil, invalid("invalid_json")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, invalid("trailing_json")
	}
	return m, nil
}

// encodeObject/encodeArray copy raw values without float conversion, HTML
// escaping or normalization of field spelling/number lexemes.
func encodeObject(m map[string]json.RawMessage) json.RawMessage {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := bytes.NewBufferString("{")
	for i, key := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(key)
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(m[key])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

func encodeArray(values []json.RawMessage) json.RawMessage {
	buf := bytes.NewBufferString("[")
	for i, v := range values {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(v)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// Canonicalization is only for request fingerprints. Stored JSON remains raw.
// It ignores object order/whitespace but preserves array order and exact numeric
// tokens (including integers beyond float64's precision).
func canonical(raw []byte) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, invalid("invalid_json")
	}
	switch raw[0] {
	case '{':
		m, err := object(raw)
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			m[k], err = canonical(v)
			if err != nil {
				return nil, err
			}
		}
		return encodeObject(m), nil
	case '[':
		var a []json.RawMessage
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, invalid("invalid_array")
		}
		for i := range a {
			v, err := canonical(a[i])
			if err != nil {
				return nil, err
			}
			a[i] = v
		}
		return encodeArray(a), nil
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, invalid("invalid_string")
		}
		return json.Marshal(s)
	default:
		return bytes.Clone(raw), nil
	}
}

func fingerprint(mode string, request any) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", invalid("invalid_request_json")
	}
	raw, err = canonical(raw)
	if err != nil {
		return "", err
	}
	return digest(append([]byte(mode+"\n"), raw...)), nil
}

func digest(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func validDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	v, err := hex.DecodeString(s)
	return err == nil && hex.EncodeToString(v) == s
}

func integer(raw []byte) (int64, error) {
	v, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	if err != nil || v <= 0 {
		return 0, invalid("expected_positive_int64")
	}
	return v, nil
}

func itemFields(raw json.RawMessage) (map[string]json.RawMessage, int64, error) {
	m, err := object(raw)
	if err != nil {
		return nil, 0, err
	}
	id, err := integer(m["id"])
	if err != nil || id == math.MaxInt64 {
		return nil, 0, invalid("invalid_item_id")
	}
	return m, id, nil
}

func summary(m map[string]json.RawMessage, id int64) Summary {
	return Summary{ID: id, Title: m["title"], Status: m["status"],
		Project: m["project"], Priority: m["priority"], Progress: m["progress"]}
}

func normalizedMeta(raw []byte, minimum int64, strict bool) (json.RawMessage, int64, error) {
	m, err := object(raw)
	if err != nil {
		return nil, 0, err
	}
	if _, exists := m["items"]; exists {
		return nil, 0, invalid("meta_contains_items")
	}
	next := minimum
	if value, exists := m["next_id"]; exists {
		n, err := integer(value)
		if err != nil {
			return nil, 0, invalid("invalid_next_id")
		}
		if n < minimum && strict {
			return nil, 0, invalid("next_id_not_above_items")
		}
		if n >= minimum {
			return encodeObject(m), n, nil
		}
	}
	m["next_id"] = json.RawMessage(strconv.FormatInt(next, 10))
	return encodeObject(m), next, nil
}
