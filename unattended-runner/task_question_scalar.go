package main

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

// This contract is only for prepared scope/input scalars. Original question,
// decision, request, signature and media byte identities keep their encoders.
func taskQuestionPreparedDigest(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	var v any
	if decoder.Decode(&v) != nil {
		return ""
	}
	var out bytes.Buffer
	quote := func(s string) {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		_ = e.Encode(s)
		out.Write(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
	}
	var write func(any) bool
	write = func(v any) bool {
		switch v := v.(type) {
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
				quote(key)
				out.WriteByte(':')
				if !write(v[key]) {
					return false
				}
			}
			out.WriteByte('}')
		case []any:
			out.WriteByte('[')
			for i, x := range v {
				if i > 0 {
					out.WriteByte(',')
				}
				if !write(x) {
					return false
				}
			}
			out.WriteByte(']')
		case json.Number:
			n, ok := taskQuestionPreparedNumber(string(v))
			if !ok {
				return false
			}
			out.WriteString(n)
		case string:
			quote(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return false
			}
			out.Write(b)
		}
		return true
	}
	if !write(v) {
		return ""
	}
	return bytesHash(out.Bytes())
}

func taskQuestionPreparedNumber(text string) (string, bool) {
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign = "-"
		text = text[1:]
	}
	exponent := int64(0)
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.ParseInt(text[i+1:], 10, 64)
		if err != nil {
			return "", false
		}
		text = text[:i]
	}
	fraction := 0
	if i := strings.IndexByte(text, '.'); i >= 0 {
		fraction = len(text) - i - 1
		text = text[:i] + text[i+1:]
	}
	digits := strings.TrimLeft(text, "0")
	if digits == "" {
		return "0", true
	}
	coefficient := strings.TrimRight(digits, "0")
	adjustment := int64(len(digits) - len(coefficient) - fraction)
	if (adjustment > 0 && exponent > math.MaxInt64-adjustment) || (adjustment < 0 && exponent < math.MinInt64-adjustment) {
		return "", false
	}
	exponent += adjustment
	if exponent == 0 {
		return sign + coefficient, true
	}
	return sign + coefficient + "e" + strconv.FormatInt(exponent, 10), true
}
