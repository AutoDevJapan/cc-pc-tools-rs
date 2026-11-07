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
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := canonicalNode(decoder)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, invalid("trailing_json")
	}
	var out bytes.Buffer
	writeCanonicalNode(&out, value)
	return out.Bytes(), nil

}

// Parse each byte once, then serialize into one buffer. Re-decoding and copying
// every nested RawMessage makes a small deeply nested document quadratic.
func canonicalNode(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, invalid("invalid_json")
	}
	switch token {
	case json.Delim('{'):
		values := map[string]any{}
		for d.More() {
			keyToken, e := d.Token()
			if e != nil {
				return nil, invalid("invalid_json")
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, invalid("invalid_object_key")
			}
			if _, exists := values[key]; exists {
				return nil, invalid("duplicate_json_key")
			}
			value, e := canonicalNode(d)
			if e != nil {
				return nil, e
			}
			values[key] = value
		}
		if close, e := d.Token(); e != nil || close != json.Delim('}') {
			return nil, invalid("invalid_json")
		}
		return values, nil
	case json.Delim('['):
		values := []any{}
		for d.More() {
			value, e := canonicalNode(d)
			if e != nil {
				return nil, e
			}
			values = append(values, value)
		}
		if close, e := d.Token(); e != nil || close != json.Delim(']') {
			return nil, invalid("invalid_json")
		}
		return values, nil
	default:
		return token, nil
	}
}

func writeCanonicalNode(out *bytes.Buffer, value any) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			out.Write(encoded)
			out.WriteByte(':')
			writeCanonicalNode(out, v[key])
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			writeCanonicalNode(out, item)
		}
		out.WriteByte(']')
	case json.Number:
		out.WriteString(v.String())
	default:
		encoded, _ := json.Marshal(v)
		out.Write(encoded)
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
