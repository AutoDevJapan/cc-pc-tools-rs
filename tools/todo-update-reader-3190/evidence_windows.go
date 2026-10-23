package main

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type receiptInput struct {
	Path   string `json:"path"`
	Blob   string `json:"blob"`
	SHA256 string `json:"sha256"`
}

// buildReceipt is the complete schema of ToDo3190's original build record.
// Unknown fields are refused, so a receipt cannot carry an extra grant.
type buildReceipt struct {
	Domain               string         `json:"domain"`
	TodoID               int64          `json:"todo_id"`
	Job                  string         `json:"job"`
	At                   string         `json:"at"`
	Repository           string         `json:"repository"`
	SourceRoot           string         `json:"source_root"`
	SourceHead           string         `json:"source_head"`
	SourceTree           string         `json:"source_tree"`
	Command              []string       `json:"command"`
	CompilerExitCode     *int64         `json:"compiler_exit_code"`
	LogFile              string         `json:"log_file"`
	LogSHA256            string         `json:"log_sha256"`
	DepInfoFile          string         `json:"dep_info_file"`
	DepInfoSHA256        string         `json:"dep_info_sha256"`
	Artifact             string         `json:"artifact"`
	ArtifactSHA256       string         `json:"artifact_sha256"`
	ArtifactBytes        int64          `json:"artifact_bytes"`
	Inputs               []receiptInput `json:"inputs"`
	BaselineQualifiedBy  string         `json:"baseline_qualified_by"`
	BaselineSHA256       string         `json:"baseline_sha256"`
	FunctionalTestStatus string         `json:"functional_test_status"`
	DataAccessGranted    *bool          `json:"data_access_granted"`
}

type treeEntry struct{ mode, kind, object string }

var (
	relPathPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)
	lowerHex       = regexp.MustCompile(`^[0-9a-f]+$`)
	compilingLine  = regexp.MustCompile(`^ +Compiling ([A-Za-z0-9_-]+) v([0-9][0-9A-Za-z.+-]*)(?: \(([^()]+)\))?$`)
	finishedLine   = regexp.MustCompile("^ +Finished `release` profile \\[optimized\\] target\\(s\\) in (?:[0-9]+(?:\\.[0-9]+)?s|[0-9]+m [0-9]+s)$")
)

func hexDigest(value string, length int) bool {
	return len(value) == length && lowerHex.MatchString(value)
}

// validRelPath accepts one plain repository path: no absolute or parent
// parts, no stream or drive separators, no trailing dot alias, no device name.
func validRelPath(rel string) bool {
	if !relPathPattern.MatchString(rel) {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "." || part == ".." || strings.HasSuffix(part, ".") || reservedDeviceName(part) {
			return false
		}
	}
	return true
}

func reservedDeviceName(part string) bool {
	base, _, _ := strings.Cut(strings.ToUpper(part), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}

// relativeTo converts a native absolute path strictly inside root.
func relativeTo(root, native string) (string, bool) {
	if !filepath.IsAbs(native) || filepath.Clean(native) != native || len(native) <= len(root)+1 ||
		!strings.EqualFold(native[:len(root)], root) || native[len(root)] != '\\' {
		return "", false
	}
	return filepath.ToSlash(native[len(root)+1:]), true
}

// noDuplicateKeys refuses JSON in which any object repeats a key; the
// standard decoder would silently keep only the last value.
func noDuplicateKeys(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value func(depth int) bool
	value = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, err := dec.Token()
		if err != nil {
			return false
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				name, err := dec.Token()
				key, ok := name.(string)
				if err != nil || !ok || seen[key] {
					return false
				}
				seen[key] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := dec.Token()
			return err == nil && end == json.Delim('}')
		case json.Delim('['):
			for dec.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := dec.Token()
			return err == nil && end == json.Delim(']')
		}
		return true
	}
	if !value(0) {
		return false
	}
	_, err := dec.Token()
	return err == io.EOF
}

// decodeSingle decodes exactly one JSON value followed only by whitespace.
func decodeSingle(data []byte, v any, strict bool) bool {
	if !utf8.Valid(data) || !noDuplicateKeys(data) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if strict {
		dec.DisallowUnknownFields()
	}
	if dec.Decode(v) != nil {
		return false
	}
	_, err := dec.Token()
	return err == io.EOF
}

func receiptMatches(r buildReceipt, p profile) bool {
	at, err := time.Parse(time.RFC3339Nano, r.At)
	return err == nil && !at.IsZero() &&
		r.Domain == p.receipt.domain && r.TodoID == p.receipt.todoID && r.Job == p.receipt.job &&
		r.Repository == p.receipt.repository && filepath.IsAbs(r.SourceRoot) &&
		strings.EqualFold(filepath.Clean(r.SourceRoot), p.root) &&
		r.SourceHead == p.head && r.SourceTree == p.tree && slices.Equal(r.Command, p.receipt.command) &&
		r.CompilerExitCode != nil && *r.CompilerExitCode == 0 &&
		r.LogFile == p.logName && r.LogSHA256 == p.logSHA &&
		r.DepInfoFile == p.depInfoName && r.DepInfoSHA256 == p.depInfoSHA &&
		r.Artifact == p.candidateName && r.ArtifactSHA256 == p.candidateSHA && r.ArtifactBytes == p.candidateBytes &&
		r.BaselineQualifiedBy == p.receipt.baselineQualifiedBy && r.BaselineSHA256 == p.baselineSHA &&
		r.DataAccessGranted != nil && !*r.DataAccessGranted &&
		len(r.Inputs) == p.inputs && validInputs(r.Inputs)
}

func validInputs(inputs []receiptInput) bool {
	seen := map[string]bool{}
	for _, in := range inputs {
		folded := strings.ToLower(in.Path)
		if !validRelPath(in.Path) || seen[folded] || !hexDigest(in.SHA256, 64) || !hexDigest(in.Blob, 40) {
			return false
		}
		seen[folded] = true
	}
	return true
}

// parseDepInfo reads Cargo's one-rule dep-info for artifact. Every compiler
// input must be a plain path inside root; escapes and variables are refused.
func parseDepInfo(data []byte, root, artifact string) ([]string, bool) {
	text := string(data)
	if !utf8.ValidString(text) || !strings.HasSuffix(text, "\n") {
		return nil, false
	}
	line := strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r")
	if line == "" || strings.ContainsAny(line, "\n\r\t$#") || strings.Contains(line, `\ `) {
		return nil, false
	}
	target, list, ok := strings.Cut(line, ": ")
	if !ok || !strings.EqualFold(target, artifact) {
		return nil, false
	}
	seen := map[string]bool{}
	var deps []string
	for _, dep := range strings.Split(list, " ") {
		rel, ok := relativeTo(root, dep)
		if !ok || !validRelPath(rel) || seen[strings.ToLower(rel)] {
			return nil, false
		}
		seen[strings.ToLower(rel)] = true
		deps = append(deps, rel)
	}
	return deps, len(deps) > 0
}

// parseBuildLog accepts only Cargo's Compiling lines and a final release
// Finished line. It returns the source-relative directories of local crates.
func parseBuildLog(data []byte, root string) ([]string, bool) {
	text := string(data)
	if !utf8.ValidString(text) {
		return nil, false
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 2 || !finishedLine.MatchString(lines[len(lines)-1]) {
		return nil, false
	}
	seen := map[string]bool{}
	var crates []string
	for _, line := range lines[:len(lines)-1] {
		match := compilingLine.FindStringSubmatch(line)
		if match == nil {
			return nil, false
		}
		if match[3] == "" {
			continue // a registry crate pinned by Cargo.lock
		}
		rel, ok := relativeTo(root, match[3])
		if !ok || !validRelPath(rel) || seen[rel] {
			return nil, false
		}
		seen[rel] = true
		crates = append(crates, rel)
	}
	return crates, len(crates) > 0
}

func crateOf(dep string, inputs map[string]bool) (string, bool) {
	for dir := path.Dir(dep); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if inputs[dir+"/Cargo.toml"] {
			return dir, true
		}
	}
	return "", false
}

func declaresBuildScript(manifest []byte) bool {
	for _, line := range strings.Split(string(manifest), "\n") {
		trimmed := strings.TrimSpace(line)
		for _, key := range []string{"build", `"build"`, "'build'"} {
			if rest, ok := strings.CutPrefix(trimmed, key); ok && strings.HasPrefix(strings.TrimSpace(rest), "=") {
				return true
			}
		}
	}
	return false
}

// closureComplete checks that the receipt's inputs are exactly the compiler's
// local inputs plus the manifests and lock file that define the build, and
// that the fixed tree has no build script, Cargo configuration or toolchain
// file that the receipt would not cover. It returns the local crate count.
func closureComplete(inputs []receiptInput, deps, logCrates []string, tree map[string]treeEntry, manifests map[string][]byte, p profile) (int, bool) {
	inputSet := map[string]bool{}
	for _, in := range inputs {
		inputSet[in.Path] = true
	}
	depSet := map[string]bool{}
	crates := map[string]bool{}
	for _, dep := range deps {
		crate, ok := crateOf(dep, inputSet)
		if !inputSet[dep] || !ok {
			return 0, false
		}
		depSet[dep] = true
		crates[crate] = true
	}
	expected := map[string]bool{"Cargo.lock": true, "Cargo.toml": true}
	for crate := range crates {
		expected[crate+"/Cargo.toml"] = true
	}
	for name := range inputSet {
		if depSet[name] {
			continue
		}
		if !expected[name] {
			return 0, false
		}
		delete(expected, name)
	}
	if len(expected) != 0 || len(logCrates) != len(crates) {
		return 0, false
	}
	for _, crate := range logCrates {
		if !crates[crate] {
			return 0, false
		}
	}
	folded := map[string]bool{}
	for name := range tree {
		folded[strings.ToLower(name)] = true
	}
	dirs := []string{""}
	for crate := range crates {
		dirs = append(dirs, crate)
	}
	for _, dir := range dirs {
		for _, name := range []string{"build.rs", ".cargo/config", ".cargo/config.toml"} {
			if folded[strings.ToLower(path.Join(dir, name))] {
				return 0, false
			}
		}
	}
	if folded["rust-toolchain"] || folded["rust-toolchain.toml"] {
		return 0, false
	}
	for _, manifest := range manifests {
		if declaresBuildScript(manifest) {
			return 0, false
		}
	}
	if len(inputSet) != p.inputs || len(deps) != p.depEntries || len(crates) != p.localCrates {
		return 0, false
	}
	return len(crates), true
}
