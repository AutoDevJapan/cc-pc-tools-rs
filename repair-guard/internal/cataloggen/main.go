// cataloggen projects normal department declarations to opaque membership hashes.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func generate(source []byte, departmentSources ...[]byte) ([]byte, error) {
	source = bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
	departments := source
	if len(departmentSources) > 0 {
		departments = bytes.ReplaceAll(departmentSources[0], []byte("\r\n"), []byte("\n"))
	}
	declarations := regexp.MustCompile(`(?s)const DEPARTMENTS:\s*\[\(&str,\s*&str\);\s*([0-9]+)\]\s*=\s*\[(.*?)\];`).FindAllSubmatch(departments, -1)
	if len(declarations) != 1 {
		return nil, fmt.Errorf("department declaration missing or ambiguous")
	}
	decl := declarations[0]
	rows := regexp.MustCompile(`\(\s*("[^"\\]*(?:\\.[^"\\]*)*")\s*,\s*"[^"\\]*(?:\\.[^"\\]*)*"\s*\)`).FindAllSubmatch(decl[2], -1)
	count, err := strconv.Atoi(string(decl[1]))
	if err != nil || len(rows) != count {
		return nil, fmt.Errorf("department declaration shape changed")
	}
	contracted := -1
	for index, row := range rows {
		var name string
		if json.Unmarshal(row[1], &name) != nil {
			return nil, fmt.Errorf("department label invalid")
		}
		if name == "受託部" {
			if contracted != -1 {
				return nil, fmt.Errorf("contracted department is ambiguous")
			}
			contracted = index
		}
	}
	if contracted < 0 {
		return nil, fmt.Errorf("contracted department missing")
	}
	sections := regexp.MustCompile(`(?s)pub const PRODUCTS: &\[Product\] = &\[(.*?)\n\];`).FindAllSubmatch(source, -1)
	if len(sections) != 1 {
		return nil, fmt.Errorf("catalogue section missing or ambiguous")
	}
	section := sections[0]
	type group struct {
		Kind    string   `json:"kind"`
		Aliases []string `json:"aliases"`
	}
	groups := []group{}
	for _, block := range regexp.MustCompile(`(?s)Product\s*\{(.*?)\}`).FindAllSubmatch(section[1], -1) {
		name := regexp.MustCompile(`name:\s*("[^"\\]*(?:\\.[^"\\]*)*")`).FindSubmatch(block[1])
		dept := regexp.MustCompile(`department:\s*([0-9]+)`).FindSubmatch(block[1])
		aliases := regexp.MustCompile(`(?s)aliases:\s*&\[(.*?)\]`).FindSubmatch(block[1])
		if len(name) != 2 || len(dept) != 2 || len(aliases) != 2 {
			return nil, fmt.Errorf("catalogue product shape changed")
		}
		literals := [][]byte{name[1]}
		combined := []string{}
		for _, value := range regexp.MustCompile(`concat!\([^)]*\)|"[^"\\]*(?:\\.[^"\\]*)*"`).FindAll(aliases[1], -1) {
			if bytes.HasPrefix(value, []byte("concat!")) {
				joined := ""
				for _, literal := range regexp.MustCompile(`"[^"\\]*(?:\\.[^"\\]*)*"`).FindAll(value, -1) {
					var part string
					if json.Unmarshal(literal, &part) != nil {
						return nil, fmt.Errorf("unsupported catalogue string")
					}
					joined += part
				}
				combined = append(combined, joined)
			} else {
				literals = append(literals, value)
			}
		}
		for _, literal := range literals {
			var value string
			if json.Unmarshal(literal, &value) != nil {
				return nil, fmt.Errorf("unsupported catalogue string")
			}
			combined = append(combined, value)
		}
		set := map[string]bool{}
		for _, value := range combined {
			value = strings.TrimPrefix(strings.ToLower(value), "resume-")
			normalized := strings.Map(func(r rune) rune {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					return r
				}
				return -1
			}, value)
			set[digest([]byte(normalized))] = true
		}
		hashes := []string{}
		for value := range set {
			hashes = append(hashes, value)
		}
		sort.Strings(hashes)
		kind := "self"
		department, err := strconv.Atoi(string(dept[1]))
		if err != nil || department < 0 || department >= count {
			return nil, fmt.Errorf("product department is outside declaration")
		}
		if department == contracted {
			kind = "client_project"
		}
		groups = append(groups, group{kind, hashes})
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("empty catalogue")
	}
	result := struct {
		Domain             string  `json:"domain"`
		Source             string  `json:"source"`
		SHA                string  `json:"source_declaration_sha256"`
		DepartmentSource   string  `json:"department_source"`
		DepartmentSHA      string  `json:"department_declaration_sha256"`
		ContractDepartment int     `json:"contract_department"`
		Groups             []group `json:"groups"`
	}{"owner-department-classification-v1", "localai-web-rs/src/operations_catalog.rs", digest(section[0]), "localai-web-rs/src/operations.rs", digest(decl[0]), contracted, groups}
	b, err := json.MarshalIndent(result, "", "  ")
	return append(b, '\n'), err
}

func main() {
	check := flag.Bool("check", false, "check projection without writing")
	flag.Parse()
	source, err := os.ReadFile("../localai-web-rs/src/operations_catalog.rs")
	if err != nil {
		fmt.Fprintln(os.Stderr, "catalogue source unavailable")
		os.Exit(1)
	}
	departments, err := os.ReadFile("../localai-web-rs/src/operations.rs")
	if err != nil {
		fmt.Fprintln(os.Stderr, "department declaration unavailable")
		os.Exit(1)
	}
	want, err := generate(source, departments)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		got, err := os.ReadFile("owner_catalog.json")
		if err != nil || !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			fmt.Fprintln(os.Stderr, "department projection needs go generate")
			os.Exit(1)
		}
		return
	}
	if os.WriteFile("owner_catalog.json", want, 0600) != nil {
		fmt.Fprintln(os.Stderr, "catalogue projection write failed")
		os.Exit(1)
	}
}
