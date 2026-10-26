package main

import (
	"reflect"
	"strings"
)

// encoding/json matches struct tags case-insensitively. Protocol field names
// are exact: a differently cased duplicate must not override the declared
// instructions, author, public tuple or review acceptance in another view.
func skillsCanonicalJSON(value any, target any) bool {
	if target == nil {
		return false
	}
	var check func(any, reflect.Type, int) bool
	check = func(value any, t reflect.Type, depth int) bool {
		if depth > 64 {
			return false
		}
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if value == nil {
			return true
		}
		switch t.Kind() {
		case reflect.Struct:
			m, ok := value.(map[string]any)
			if !ok {
				return true
			}
			fields := map[string]reflect.Type{}
			var collect func(reflect.Type, int) bool
			collect = func(s reflect.Type, level int) bool {
				if level > 64 {
					return false
				}
				for i := 0; i < s.NumField(); i++ {
					f := s.Field(i)
					tag := strings.Split(f.Tag.Get("json"), ",")[0]
					if tag == "-" {
						continue
					}
					child := f.Type
					for child.Kind() == reflect.Pointer {
						child = child.Elem()
					}
					if f.Anonymous && tag == "" && child.Kind() == reflect.Struct {
						if !collect(child, level+1) {
							return false
						}
						continue
					}
					if f.PkgPath != "" {
						continue
					}
					if tag == "" {
						tag = f.Name
					}
					fields[tag] = f.Type
				}
				return true
			}
			if !collect(t, 0) {
				return false
			}
			for name, v := range m {
				field, exists := fields[name]
				if !exists || !check(v, field, depth+1) {
					return false
				}
			}
		case reflect.Slice, reflect.Array:
			if values, ok := value.([]any); ok {
				for _, v := range values {
					if !check(v, t.Elem(), depth+1) {
						return false
					}
				}
			}
		case reflect.Map:
			if values, ok := value.(map[string]any); ok {
				for _, v := range values {
					if !check(v, t.Elem(), depth+1) {
						return false
					}
				}
			}
		}
		return true
	}
	return check(value, reflect.TypeOf(target), 0)
}
