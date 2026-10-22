package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const fixtureDepartments = "const DEPARTMENTS: [(&str, &str); 6] = [(\"a\",\"a\"),(\"b\",\"b\"),(\"c\",\"c\"),(\"d\",\"d\"),(\"受託部\",\"e\"),(\"f\",\"f\")];\n"

func TestProjectionKeepsDepartmentAndConcatAliasesWithoutNames(t *testing.T) {
	source := []byte(fixtureDepartments + "pub const PRODUCTS: &[Product] = &[\nProduct { name: \"self-fixture\", department: 1, aliases: &[concat!(\"self-\", \"alias\")] },\nProduct { name: \"client-fixture\", department: 4, aliases: &[\"client-alias\"] },\n];")
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

func TestProjectionRejectsMissingOrAmbiguousContractDepartment(t *testing.T) {
	products := []byte("pub const PRODUCTS: &[Product] = &[\nProduct { name: \"fixture\", department: 0, aliases: &[] },\n];")
	for _, departments := range []string{strings.ReplaceAll(fixtureDepartments, "受託部", "other"), strings.Replace(fixtureDepartments, "\"a\"", "\"受託部\"", 1)} {
		if _, err := generate(products, []byte(departments)); err == nil {
			t.Fatal("undefined/ambiguous department accepted")
		}
	}
}

func TestProjectionUsesDeclaredContractDepartment(t *testing.T) {
	source := []byte("const DEPARTMENTS: [(&str, &str); 6] = [(\"a\",\"a\"),(\"b\",\"b\"),(\"c\",\"c\"),(\"d\",\"d\"),(\"受託部\",\"e\"),(\"f\",\"f\")];\npub const PRODUCTS: &[Product] = &[\nProduct { name: \"self-zero\", department: 0, aliases: &[] },\nProduct { name: \"client-four\", department: 4, aliases: &[] },\n];")
	b, err := generate(source)
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Groups []struct{ Kind string } }
	if json.Unmarshal(b, &result) != nil || len(result.Groups) != 2 || result.Groups[0].Kind != "self" || result.Groups[1].Kind != "client_project" {
		t.Fatal("normal declaration's contracted department was inverted")
	}
}

func TestProjectionRejectsDuplicateAuthorityDeclarations(t *testing.T) {
	products := "pub const PRODUCTS: &[Product] = &[\nProduct { name: \"fixture\", department: 0, aliases: &[] },\n];\n"
	for _, source := range []string{fixtureDepartments + fixtureDepartments + products, fixtureDepartments + products + products} {
		if _, err := generate([]byte(source)); err == nil {
			t.Fatal("duplicate authority declaration accepted")
		}
	}
}
