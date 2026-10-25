package main

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

type receiptProjectionField struct {
	Name   string
	Type   reflect.Type
	Index  []int
	Tagged bool
}

var receiptProjectionFieldCache sync.Map // reflect.Type -> immutable field slice

// Resolve exported/promoted fields before checking aliases. Map keys remain
// data: README.md and readme.md are not folded into a single identifier.
func receiptProjectionFields(t reflect.Type) []receiptProjectionField {
	if fields, ok := receiptProjectionFieldCache.Load(t); ok {
		return fields.([]receiptProjectionField)
	}
	var all []receiptProjectionField
	var visit func(reflect.Type, []int, map[reflect.Type]bool)
	visit = func(t reflect.Type, path []int, seen map[reflect.Type]bool) {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		next := map[reflect.Type]bool{}
		for k, v := range seen {
			next[k] = v
		}
		next[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.PkgPath != "" && (!f.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			index := append(append([]int(nil), path...), i)
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				visit(f.Type, index, next)
				continue
			}
			tagged := name != ""
			if name == "" {
				name = f.Name
			}
			all = append(all, receiptProjectionField{name, f.Type, index, tagged})
		}
	}
	visit(t, nil, map[reflect.Type]bool{})
	groups := map[string][]receiptProjectionField{}
	for _, f := range all {
		groups[f.Name] = append(groups[f.Name], f)
	}
	var fields []receiptProjectionField
	for _, group := range groups {
		slices.SortFunc(group, func(a, b receiptProjectionField) int {
			if len(a.Index) != len(b.Index) {
				return len(a.Index) - len(b.Index)
			}
			if a.Tagged != b.Tagged {
				if a.Tagged {
					return -1
				}
				return 1
			}
			return slices.Compare(a.Index, b.Index)
		})
		// Same-depth, same-priority promoted conflicts are ignored by json.
		if len(group) > 1 && len(group[0].Index) == len(group[1].Index) && group[0].Tagged == group[1].Tagged {
			continue
		}
		fields = append(fields, group[0])
	}
	slices.SortFunc(fields, func(a, b receiptProjectionField) int { return slices.Compare(a.Index, b.Index) })
	actual, _ := receiptProjectionFieldCache.LoadOrStore(t, fields)
	return actual.([]receiptProjectionField)
}

type receiptPromotedProjection struct {
	ID string `json:"inventory_id"`
}
type receiptShadowProjection struct {
	receiptPromotedProjection
	ID string `json:"inventory_id"`
}

func TestReceiptProjectionUsesGoFieldDominance(t *testing.T) {
	var v receiptShadowProjection
	if err := decodePublicReceipt([]byte(`{"inventory_id":"outer"}`), &v); err != nil || v.ID != "outer" || v.receiptPromotedProjection.ID != "" {
		t.Fatalf("outer field did not dominate promoted field: %#v %v", v, err)
	}
	var distinct struct {
		Lower string `json:"id"`
		Upper string `json:"ID"`
	}
	if err := decodePublicReceipt([]byte(`{"id":"lower","ID":"upper"}`), &distinct); err != nil || distinct.Lower != "lower" || distinct.Upper != "upper" {
		t.Fatalf("distinct exact tags collapsed: %#v %v", distinct, err)
	}
}

func rejectReceiptProjectionAliases(value any, t reflect.Type, depth int) error {
	if t == nil || value == nil {
		return nil
	}
	if depth > 80 {
		return fmt.Errorf("receipt projection depth exceeded")
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		fields := receiptProjectionFields(t)
		seen := map[string]string{}
		for key, child := range object {
			match := -1
			for i, f := range fields {
				if key == f.Name {
					match = i
					break
				}
			}
			if match < 0 {
				for i, f := range fields {
					if strings.EqualFold(key, f.Name) {
						match = i
						break
					}
				}
			}
			if match < 0 {
				continue
			} // Preserve deliberate partial projections.
			field := fields[match]
			id := fmt.Sprint(field.Index)
			if prior, ok := seen[id]; ok && prior != key {
				return fmt.Errorf("receipt keys %q and %q bind the same projected field", prior, key)
			}
			seen[id] = key
			if err := rejectReceiptProjectionAliases(child, field.Type, depth+1); err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		if array, ok := value.([]any); ok {
			for _, child := range array {
				if err := rejectReceiptProjectionAliases(child, t.Elem(), depth+1); err != nil {
					return err
				}
			}
		}
	case reflect.Map:
		if object, ok := value.(map[string]any); ok {
			for _, child := range object {
				if err := rejectReceiptProjectionAliases(child, t.Elem(), depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func TestReceiptProjectionAliasesPreserveTypedAndMapBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"valid", `{"entries":[{"inventory_id":"expected"}],"files":{"README.md":1,"readme.md":2}}`, true},
		{"unknown", `{"entries":[{"inventory_id":"expected","extra":"ignored"}],"other":{}}`, true},
		{"single_legacy_case", `{"ENTRIES":[{"INVENTORY_ID":"expected"}]}`, true},
		{"promoted_collision", `{"entries":[{"inventory_id":"foreign","INVENTORY_ID":"expected"}]}`, false},
		{"promoted_reverse", `{"entries":[{"INVENTORY_ID":"foreign","inventory_id":"expected"}]}`, false},
		{"root_alias", `{"entries":[],"ENTRIES":[{"inventory_id":"expected"}]}`, false},
		{"typed_map_value", `{"values":{"one":{"inventory_id":"foreign","INVENTORY_ID":"expected"}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var v struct {
				Rows   []purposeAdoptionRow          `json:"entries"`
				Files  map[string]int                `json:"files"`
				Values map[string]purposeAdoptionRow `json:"values"`
			}
			err := decodePublicReceipt([]byte(tc.raw), &v)
			if (err == nil) != tc.valid {
				t.Fatalf("accepted=%v: %v", err == nil, err)
			}
			if !tc.valid && (len(v.Rows) > 0 || len(v.Values) > 0) {
				t.Fatal("ambiguous keys projected before rejection")
			}
			if tc.name == "valid" && len(v.Files) != 2 {
				t.Fatal("case-sensitive data keys collapsed")
			}
		})
	}
}
