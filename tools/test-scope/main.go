// test-scope selects Rust packages; it never builds, tests, or changes source.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type metadata struct {
	Root     string    `json:"workspace_root"`
	Members  []string  `json:"workspace_members"`
	Packages []rustPkg `json:"packages"`
}

type rustPkg struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Manifest string `json:"manifest_path"`
	Deps     []struct {
		Path string `json:"path"`
	} `json:"dependencies"`
	Targets []struct {
		Kind    []string `json:"kind"`
		Doctest bool     `json:"doctest"`
	} `json:"targets"`
}

type plan struct {
	Mode        string   `json:"mode"`
	Head        string   `json:"head"`
	Base        string   `json:"base"`
	Packages    []string `json:"packages"`
	DocPackages []string `json:"doc_packages"`
	Reasons     []string `json:"reasons"`
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
var nameRE = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_-]*$`)

func relative(root, file string) (string, error) {
	rel, err := filepath.Rel(root, file)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("workspace member is outside the source root")
	}
	return filepath.ToSlash(rel), nil
}

func selectPlan(m metadata, changed []string, fullReason string) (plan, error) {
	p := plan{Mode: "none", Packages: []string{}, DocPackages: []string{}, Reasons: []string{}}
	if !filepath.IsAbs(m.Root) || len(m.Members) == 0 {
		return p, errors.New("workspace metadata is incomplete")
	}
	members := map[string]bool{}
	for _, id := range m.Members {
		if id == "" || members[id] {
			return p, errors.New("workspace member identity is invalid")
		}
		members[id] = true
	}
	packages, directories := map[string]rustPkg{}, map[string]string{}
	for _, pkg := range m.Packages {
		if !members[pkg.ID] {
			continue
		}
		dir, err := relative(m.Root, filepath.Dir(pkg.Manifest))
		if err != nil || !filepath.IsAbs(pkg.Manifest) || !nameRE.MatchString(pkg.Name) || packages[pkg.Name].Name != "" || directories[dir] != "" {
			return p, errors.New("workspace package metadata is invalid")
		}
		packages[pkg.Name], directories[dir] = pkg, pkg.Name
	}
	if len(packages) != len(members) {
		return p, errors.New("workspace member metadata is missing")
	}
	selected := map[string]bool{}
	reasons := map[string]bool{}
	if fullReason != "" {
		reasons[fullReason] = true
	}
	for _, file := range changed {
		// Git -z is literal: renames supply both old and new paths (--no-renames).
		if file == "" || strings.Contains(file, "\\") || path.IsAbs(file) || path.Clean(file) != file || file == ".." || strings.HasPrefix(file, "../") {
			return p, errors.New("changed path is invalid")
		}
		switch {
		case file == "Cargo.toml", file == "Cargo.lock", file == "rust-toolchain", file == "rust-toolchain.toml", strings.HasPrefix(file, ".cargo/"), file == ".config/nextest.toml":
			reasons["shared_cargo_configuration"] = true
		case file == ".github/workflows/build-speed.yml", strings.HasPrefix(file, "tools/test-scope/"):
			reasons["test_selection_changed"] = true
		case strings.HasPrefix(file, "tools/linux-runner-bridge/"), strings.HasPrefix(file, "tools/product-ci/"):
			reasons["shared_runner_changed"] = true
		default:
			matched := false
			for dir, name := range directories {
				if dir == "." || strings.HasPrefix(file, dir+"/") {
					selected[name], matched = true, true
				}
			}
			if !matched && (strings.HasSuffix(file, ".rs") || path.Base(file) == "Cargo.toml") {
				// A removed member is absent from current metadata, not harmless.
				reasons["unmapped_rust_input"] = true
			}
		}
	}
	if len(reasons) != 0 {
		p.Mode = "full"
		for name := range packages {
			selected[name] = true
		}
		for reason := range reasons {
			p.Reasons = append(p.Reasons, reason)
		}
	} else {
		// Includes normal, build, dev and target-specific local dependencies.
		// Read dependency.path, not its (possibly renamed) dependency name.
		for added := true; added; {
			added = false
			for name, pkg := range packages {
				if selected[name] {
					continue
				}
				for _, dep := range pkg.Deps {
					if dep.Path == "" {
						continue
					}
					dir, err := relative(m.Root, dep.Path)
					if err == nil && selected[directories[dir]] {
						selected[name], added = true, true
						break
					}
				}
			}
		}
		if len(selected) != 0 {
			p.Mode, p.Reasons = "affected", []string{"changed_packages_and_reverse_dependencies"}
		}
	}
	for name := range selected {
		p.Packages = append(p.Packages, name)
		for _, target := range packages[name].Targets {
			if target.Doctest && library(target.Kind) {
				p.DocPackages = append(p.DocPackages, name)
				break
			}
		}
	}
	sort.Strings(p.Packages)
	sort.Strings(p.DocPackages)
	sort.Strings(p.Reasons)
	return p, nil
}

func library(kinds []string) bool {
	for _, kind := range kinds {
		switch kind {
		case "lib", "rlib", "dylib", "cdylib", "staticlib", "proc-macro":
			return true
		}
	}
	return false
}

func command(dir, exe string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, exe, args...)
	c.Dir, c.WaitDelay = dir, time.Second
	hideWindow(c)
	return c.Output() // Never forward metadata, original source or stderr to the log.
}

func collect(root, base, head, event string) (plan, error) {
	if !shaRE.MatchString(head) || (base != "" && !shaRE.MatchString(base)) || (event != "push" && event != "pull_request") {
		return plan{}, errors.New("source revisions must be fixed commit hashes")
	}
	actual, err := command(root, "git", "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(actual)) != head {
		return plan{}, errors.New("checkout does not match the fixed head")
	}
	raw, err := command(root, "cargo", "metadata", "--locked", "--no-deps", "--format-version", "1")
	if err != nil {
		return plan{}, errors.New("Cargo metadata failed; no tests were skipped")
	}
	var m metadata
	if json.Unmarshal(raw, &m) != nil || !samePath(m.Root, root) {
		return plan{}, errors.New("Cargo metadata does not describe the checked-out workspace")
	}
	comparison, fullReason, err := comparisonBase(root, base, head, event)
	if err != nil {
		return plan{}, err
	}
	changed := []string{}
	if comparison != "" {
		diff, err := command(root, "git", "diff", "--name-only", "-z", "--no-renames", comparison, head, "--")
		if err != nil {
			return plan{}, errors.New("source comparison failed; no tests were skipped")
		}
		if len(diff) != 0 {
			if diff[len(diff)-1] != 0 {
				return plan{}, errors.New("changed path list is incomplete")
			}
			changed = strings.Split(string(diff[:len(diff)-1]), "\x00")
		}
	}
	p, err := selectPlan(m, changed, fullReason)
	p.Head, p.Base = head, comparison
	return p, err
}

func comparisonBase(root, base, head, event string) (comparison, fullReason string, err error) {
	if base == "" || base == strings.Repeat("0", 40) {
		fullReason = "comparison_base_unavailable"
	} else if _, err := command(root, "git", "cat-file", "-e", base+"^{commit}"); err != nil {
		fullReason = "comparison_base_unavailable"
	} else if event == "push" {
		// A forced push can remove changes. The previous tip, rather than its
		// merge-base with the new tip, is the source that must be compared.
		comparison = base
	} else {
		mergeBase, err := command(root, "git", "merge-base", base, head)
		if err != nil {
			fullReason = "comparison_ancestry_unavailable"
		} else {
			comparison = strings.TrimSpace(string(mergeBase))
			if !shaRE.MatchString(comparison) {
				return "", "", errors.New("comparison revision is invalid")
			}
		}
	}
	return comparison, fullReason, nil
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func main() {
	rootFlag := flag.String("root", ".", "checked-out workspace root")
	event := flag.String("event", "pull_request", "GitHub event: pull_request or push")
	base := flag.String("base", "", "fixed PR base or previous push revision")
	head := flag.String("head", "", "fixed checked-out source revision")
	flag.Parse()
	root, err := filepath.Abs(*rootFlag)
	if err == nil {
		var p plan
		p, err = collect(root, *base, *head, *event)
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(p)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "test scope:", err)
		os.Exit(1)
	}
}
