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
			Lengths []int `json:"normalized_lengths"`
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
	if len(result.Groups[0].Lengths) != 2 || result.Groups[0].Lengths[0] != 9 || result.Groups[0].Lengths[1] != 11 || len(result.Groups[1].Lengths) != 2 || result.Groups[1].Lengths[0] != 11 || result.Groups[1].Lengths[1] != 13 {
		t.Fatal("normalized alias character lengths lost")
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

func TestInternalProjectsRetainDepartmentWithoutBecomingProducts(t *testing.T) {
	products := fixtureDepartments + "pub const PRODUCTS: &[Product] = &[\nProduct { name: \"fixture\", department: 0, aliases: &[] },\n];\n"
	internal := `pub const INTERNAL_PROJECTS: &[(&str, usize)] = &[("internal-fixture", 5)];`
	b, err := generate([]byte(products + internal))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Internal string `json:"internal_project_declaration_sha256"`
		Groups   []struct {
			Kind    string
			Aliases []string
		}
	}
	if json.Unmarshal(b, &result) != nil || result.Internal != digest([]byte(internal)) || len(result.Groups) != 2 || result.Groups[1].Kind != "self" || len(result.Groups[1].Aliases) != 1 || result.Groups[1].Aliases[0] != digest([]byte("internalfixture")) {
		t.Fatal("internal membership was not independently projected")
	}
	for _, bad := range []string{internal + internal, strings.Replace(internal, "5)", "6)", 1), strings.Replace(internal, "5)", "-1)", 1), strings.Replace(internal, `("internal-fixture", 5)`, `("internal-fixture", 5), ("internal-fixture", 5)`, 1)} {
		if _, err := generate([]byte(products + bad)); err == nil {
			t.Fatal("ambiguous internal declaration accepted")
		}
	}
}

func TestInternalDeclarationWhitespaceAndSeparators(t *testing.T) {
	products := fixtureDepartments + "pub const PRODUCTS: &[Product] = &[\nProduct { name: \"fixture\", department: 0, aliases: &[] },\n];\n"
	valid := `pub const INTERNAL_PROJECTS: &[(&str, usize)] = &[("first", 5), ("second", 4),];`
	compact := strings.Replace(valid, "&str, usize", "&str,usize", 1)
	multiline := strings.ReplaceAll(strings.Replace(valid, "pub const", "pub\nconst", 1), "&str, usize", "&str,\nusize")
	for _, declaration := range []string{valid, compact, multiline} {
		if _, err := generate([]byte(products + declaration)); err != nil {
			t.Fatal("valid internal list rejected", err)
		}
	}
	for _, declaration := range []string{
		valid + compact, valid + multiline,
		strings.Replace(valid, "), (", ") (", 1),
		strings.Replace(valid, "), (", "),, (", 1),
		strings.Replace(valid, "&[(", "&[,(", 1),
		strings.Replace(valid, "),];", "),,];", 1),
		strings.Replace(valid, "usize", "u64", 1),
	} {
		if _, err := generate([]byte(products + declaration)); err == nil {
			t.Fatal("ambiguous or malformed internal list accepted")
		}
	}
}
