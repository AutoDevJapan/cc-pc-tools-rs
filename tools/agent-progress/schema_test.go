package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Validate the published schema's used keywords independently of Go struct
// tags, including required/additional fields and allowed public vocabulary.
func validateContract(root, rule map[string]any, value any) error {
	if ref, ok := rule["$ref"].(string); ok {
		next := root
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			var ok bool
			next, ok = next[part].(map[string]any)
			if !ok {
				return fmt.Errorf("invalid schema reference")
			}
		}
		return validateContract(root, next, value)
	}
	if constant, ok := rule["const"]; ok && !reflect.DeepEqual(value, constant) {
		return fmt.Errorf("constant mismatch")
	}
	if allowed, ok := rule["enum"].([]any); ok {
		found := false
		for _, candidate := range allowed {
			found = found || reflect.DeepEqual(candidate, value)
		}
		if !found {
			return fmt.Errorf("value outside public enumeration")
		}
	}
	types := []any{}
	switch kind := rule["type"].(type) {
	case string:
		types = append(types, kind)
	case []any:
		types = kind
	}
	if len(types) > 0 {
		valid := false
		for _, kind := range types {
			switch kind {
			case "null":
				valid = valid || value == nil
			case "boolean":
				_, ok := value.(bool)
				valid = valid || ok
			case "string":
				_, ok := value.(string)
				valid = valid || ok
			case "object":
				_, ok := value.(map[string]any)
				valid = valid || ok
			case "array":
				_, ok := value.([]any)
				valid = valid || ok
			case "number", "integer":
				n, ok := value.(float64)
				valid = valid || ok && (kind == "number" || n == math.Trunc(n))
			}
		}
		if !valid {
			return fmt.Errorf("contract type mismatch")
		}
	}
	switch v := value.(type) {
	case map[string]any:
		props, _ := rule["properties"].(map[string]any)
		required, _ := rule["required"].([]any)
		for _, key := range required {
			if _, ok := v[key.(string)]; !ok {
				return fmt.Errorf("required public field absent: %s", key)
			}
		}
		for key, child := range v {
			if prop, ok := props[key].(map[string]any); ok {
				if err := validateContract(root, prop, child); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			} else if rule["additionalProperties"] == false {
				return fmt.Errorf("unexpected output field")
			}
		}
	case []any:
		if max, ok := rule["maxItems"].(float64); ok && len(v) > int(max) {
			return fmt.Errorf("array too long")
		}
		if child, ok := rule["items"].(map[string]any); ok {
			for _, item := range v {
				if err := validateContract(root, child, item); err != nil {
					return err
				}
			}
		}
	case string:
		if pattern, ok := rule["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(v) {
			return fmt.Errorf("pattern mismatch")
		}
		if rule["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
				return fmt.Errorf("invalid timestamp")
			}
		}
		if max, ok := rule["maxLength"].(float64); ok && utf8.RuneCountInString(v) > int(max) {
			return fmt.Errorf("text too long")
		}
	case float64:
		if min, ok := rule["minimum"].(float64); ok && v < min {
			return fmt.Errorf("number below minimum")
		}
		if max, ok := rule["maximum"].(float64); ok && v > max {
			return fmt.Errorf("number above maximum")
		}
	}
	return nil
}

func TestPublishedSchemaAndRejectUnsafeExtensions(t *testing.T) {
	data, err := os.ReadFile("snapshot.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	f := fixtureFor(t, `{"items":[{"id":1,"project":"Skills","status":"doing","jobs":["a"]},{"id":2,"status":"wait","wait_ready":{"condition":"retry-time","not_before":1791885660}}]}`)
	f.job(t, "a", `{"id":"a","status":"running","last_event_at":"2026-10-13T10:00:01Z","activity":"thinking"}`)
	for _, failed := range []bool{false, true} {
		if failed {
			writeFixture(t, f.opts.TodoFile, []byte(`{broken`))
		}
		s := snap(f)
		encoded, _ := json.Marshal(s)
		var value map[string]any
		_ = json.Unmarshal(encoded, &value)
		if err := validateContract(schema, schema, value); err != nil {
			t.Fatal(err)
		}
		value["last_message"] = "private"
		if validateContract(schema, schema, value) == nil {
			t.Fatal("published schema allows a raw message extension")
		}
	}
	defs := schema["$defs"].(map[string]any)
	if validateContract(schema, defs["label"].(map[string]any), "PRIVATE_LABEL") == nil ||
		validateContract(schema, defs["key"].(map[string]any), "private-slug") == nil {
		t.Fatal("published schema allowed arbitrary names or job IDs")
	}
}

func TestChildPriorityAndMixedClientLinks(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"department":"受託部","status":"todo"},{"id":2,"project":"Re:X","status":"todo"}]}`)
	f.job(t, "parent", `{"id":"parent","status":"running","todo_id":1,"related_todo_ids":[2]}`)
	f.job(t, "child", `{"id":"child","status":"queued","parent_job_id":"parent"}`)
	f.job(t, "independent", `{"id":"independent","status":"queued","parent_job_id":"parent","subwork":{}}`)
	s := snap(f)
	byKey := map[string]Job{}
	for _, row := range s.Jobs {
		byKey[row.Key] = row
	}
	if byKey[jobKey("parent")].PublicLabel != "受託案件" || byKey[jobKey("child")].Priority != 10 || byKey[jobKey("independent")].Priority != 100 {
		t.Fatal("mixed client links or independent child priority are incorrect")
	}
}
