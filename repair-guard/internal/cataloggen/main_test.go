package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProjectionKeepsDepartmentAndConcatAliasesWithoutNames(t *testing.T) {
	source := []byte("pub const PRODUCTS: &[Product] = &[\nProduct { name: \"self-fixture\", department: 1, aliases: &[concat!(\"self-\", \"alias\")] },\nProduct { name: \"client-fixture\", department: 0, aliases: &[\"client-alias\"] },\n];")
	b, err := generate(source)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("client-fixture")) || bytes.Contains(b, []byte("client-alias")) {
		t.Fatal("projection disclosed contracted names")
	}
	var result struct {
		Groups []struct {
			Kind    string
			Aliases []string
		}
	}
	if json.Unmarshal(b, &result) != nil || len(result.Groups) != 2 || result.Groups[1].Kind != "client_project" {
		t.Fatal("department classification lost")
	}
	match := false
	for _, alias := range result.Groups[0].Aliases {
		match = match || alias == digest([]byte("selfalias"))
	}
	if !match {
		t.Fatal("concat alias lost")
	}
	if _, err := generate([]byte("unrecognized")); err == nil {
		t.Fatal("unsupported source silently produced classification")
	}
}
