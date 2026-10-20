// This check reads only explicitly manifested skill entrypoints and added references.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Target struct {
	Name      string   `json:"name"`
	New       bool     `json:"new"`
	BeforeSHA string   `json:"before_sha256"`
	Positive  string   `json:"positive"`
	Negative  string   `json:"negative"`
	Files     []File   `json:"files"`
	Sources   []string `json:"sources"`
}
type File struct {
	Path    string  `json:"path"`
	SHA     string  `json:"sha256"`
	Content *string `json:"content,omitempty"`
}
type Manifest struct {
	Targets []Target `json:"targets"`
}

var nameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var linkRE = regexp.MustCompile(`\[[^\]\n]+\]\(([^)\n]+)\)`)

func relative(root, child string) (string, error) {
	if filepath.IsAbs(child) || strings.Contains(child, ":") {
		return "", errors.New("absolute path")
	}
	p := filepath.Join(root, filepath.FromSlash(child))
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escape")
	}
	return p, nil
}

func checkEntry(name string, content []byte) error {
	s := strings.ReplaceAll(string(content), "\r\n", "\n")
	parts := strings.SplitN(s, "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		return errors.New("frontmatter missing")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(parts[1], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(k, " ") {
			continue
		}
		if _, exists := fields[k]; exists {
			return fmt.Errorf("duplicate field %s", k)
		}
		fields[k] = strings.Trim(strings.TrimSpace(v), "\"'")
	}
	if !nameRE.MatchString(name) || len(name) > 64 || fields["name"] != name {
		return errors.New("name mismatch")
	}
	d := fields["description"]
	if utf8.RuneCountInString(d) > 60 || d == "" || !strings.ContainsFunc(d, func(r rune) bool { return unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) }) {
		return errors.New("description must be Japanese, 1..60 characters")
	}
	if len(content) > 4500 {
		return errors.New("entrypoint exceeds 4500 bytes")
	}
	if strings.Contains(s, "[TODO]") {
		return errors.New("unfinished scaffold")
	}
	return nil
}

func check(root string, m Manifest) []string {
	return checkOptions(root, m, false)
}

func checkOptions(root string, m Manifest, requireHash bool) []string {
	return checkWithRead(root, m, requireHash, os.ReadFile, func(p string) error { _, e := os.Stat(p); return e })
}

// Validate the exact same entrypoints, references and hashes before deployment,
// without creating a second on-disk skill collection or reading central files.
func checkBundle(m Manifest) []string {
	root := "virtual-bundle"
	files := map[string][]byte{}
	for _, t := range m.Targets {
		for _, f := range t.Files {
			if f.Content == nil {
				return []string{"bundle content missing"}
			}
			p, e := relative(filepath.Join(root, t.Name), f.Path)
			if e != nil {
				return []string{"invalid bundle path"}
			}
			files[filepath.Clean(p)] = []byte(*f.Content)
		}
	}
	read := func(p string) ([]byte, error) {
		if b, ok := files[filepath.Clean(p)]; ok {
			return b, nil
		}
		return nil, os.ErrNotExist
	}
	return checkWithRead(root, m, true, read, func(p string) error { _, e := read(p); return e })
}

func checkWithRead(root string, m Manifest, requireHash bool, read func(string) ([]byte, error), exists func(string) error) []string {
	var failures []string
	if len(m.Targets) == 0 {
		return []string{"empty target manifest"}
	}
	seen := map[string]bool{}
	for _, t := range m.Targets {
		if !nameRE.MatchString(t.Name) || seen[t.Name] {
			failures = append(failures, "invalid/duplicate target")
			continue
		}
		seen[t.Name] = true
		if t.Positive == "" || t.Negative == "" || t.Positive == t.Negative {
			failures = append(failures, t.Name+": scenario pair missing")
		}
		if t.New && t.BeforeSHA != "absent" {
			failures = append(failures, t.Name+": absence receipt missing")
		}
		if len(t.Files) == 0 {
			failures = append(failures, t.Name+": no files")
			continue
		}
		entrySeen := false
		fileSeen := map[string]bool{}
		base := filepath.Join(root, t.Name)
		for _, f := range t.Files {
			if fileSeen[f.Path] {
				failures = append(failures, t.Name+": duplicate file")
				continue
			}
			fileSeen[f.Path] = true
			if requireHash && !regexp.MustCompile(`^[a-fA-F0-9]{64}$`).MatchString(f.SHA) {
				failures = append(failures, t.Name+"/"+f.Path+": sha256 missing or invalid")
			}
			p, err := relative(base, f.Path)
			if err != nil {
				failures = append(failures, t.Name+": invalid file path")
				continue
			}
			b, err := read(p)
			if err != nil {
				failures = append(failures, t.Name+"/"+f.Path+": missing/unreadable")
				continue
			}
			h := sha256.Sum256(b)
			if f.SHA != "" && !strings.EqualFold(hex.EncodeToString(h[:]), f.SHA) {
				failures = append(failures, t.Name+"/"+f.Path+": hash mismatch")
			}
			if f.Path == "SKILL.md" {
				entrySeen = true
				if err := checkEntry(t.Name, b); err != nil {
					failures = append(failures, t.Name+": "+err.Error())
				}
			}
			if !strings.HasSuffix(f.Path, ".md") {
				continue
			}
			for _, match := range linkRE.FindAllStringSubmatch(string(b), -1) {
				u := strings.Trim(match[1], "<>")
				if strings.HasPrefix(u, "#") || strings.Contains(u, "://") || strings.Contains(u, ":/") {
					continue
				}
				u, _, _ = strings.Cut(u, "#")
				linkRel, e := filepath.Rel(base, filepath.Join(filepath.Dir(p), filepath.FromSlash(u)))
				if e != nil {
					failures = append(failures, t.Name+": link path escape")
					continue
				}
				q, e := relative(base, linkRel)
				if e != nil {
					failures = append(failures, t.Name+": link path escape")
					continue
				}
				if e = exists(q); e != nil {
					failures = append(failures, t.Name+": broken link "+u)
				}
			}
		}
		if !entrySeen {
			failures = append(failures, t.Name+": SKILL.md not manifested")
		}
	}
	return failures
}

func main() {
	root := flag.String("root", "D:/CC/Skills", "explicit installation root")
	manifest := flag.String("manifest", "docs/skills-3048-manifest.json", "target manifest")
	requireHash := flag.Bool("require-hash", true, "require finalized SHA-256 receipts (false for pre-install structural checks)")
	bundle := flag.Bool("bundle", false, "check embedded bundle bytes in memory; never reads installation root")
	flag.Parse()
	b, err := os.ReadFile(*manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var m Manifest
	if err = json.Unmarshal(b, &m); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var failures []string
	if *bundle {
		failures = checkBundle(m)
	} else {
		failures = checkOptions(*root, m, *requireHash)
	}
	if len(failures) > 0 {
		for _, f := range failures {
			fmt.Fprintln(os.Stderr, f)
		}
		os.Exit(1)
	}
	mode := "installed files"
	if *bundle {
		mode = "in-memory bundle only; no deployment implied"
	}
	fmt.Printf("PASS: %d explicit skills; structure, descriptions, references, hashes, scenario pairs; %s\n", len(m.Targets), mode)
}
