// This narrow repair guard is not a reconstruction of the lost private registry.
// Unknown resources remain protected; only verified repair source/explicit receipts qualify.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const base = "D:/CC/Work/runtime-recovery-20261012"

// Explicit 2026-10-13 unattended-operation repair scope; every existing root
// still needs the same independently checked Git origin and physical path.
var roots = []string{base, "D:/CC/Work/operations-owner-replies-2474", "D:/CC/Work/operations-original-records-2467", "D:/CC/Work/unattended-operations-20261013", "C:/Users/makhmeto/Desktop/dev/ADJ/cc-pc-tools-rs"}
var receipts = []string{
	"D:/CC/Work/unattended-week-readiness-20261013.md",
	"D:/CC/Work/operations-organization-request-20261012.md",
	"D:/CC/Work/operations-owner-reply-followup-2474.md",
	"D:/CC/Work/operations-owner-replies-parent-steering-2474.md",
	"D:/CC/Work/operations-parent-2472.active",
	"D:/CC/Work/operations-verified-status-receipt-2472.json",
	"D:/CC/Work/operations-unlisted-question-receipt-2473.json",
	"D:/CC/Work/operations-unlisted-question-2472.txt",
	"D:/CC/Work/operations-unlisted-inventory-2472.json",
	"D:/CC/Work/operations-unlisted-decision-2473.json",
	"D:/CC/Work/operations-owner-replies-2473.json",
	"D:/CC/Work/operations-membership-choice-2473.json",
}

func physical(path string) (string, error) {
	path, err := portable(path)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}
func portable(path string) (string, error) {
	if strings.HasPrefix(path, `\\?\UNC\`) {
		return `\\` + path[8:], nil
	}
	if strings.HasPrefix(path, `\\?\`) {
		rest := path[4:]
		if len(rest) >= 3 && ((rest[0] >= 'A' && rest[0] <= 'Z') || (rest[0] >= 'a' && rest[0] <= 'z')) && rest[1] == ':' && rest[2] == '\\' {
			return rest, nil
		}
		return "", os.ErrInvalid
	}
	if strings.HasPrefix(path, `\\.\`) {
		return "", os.ErrInvalid
	}
	return path, nil
}
func same(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func sourceVerified(root string) bool {
	return sourceOriginVerified(root, "autodevjapan/cc-pc-tools-rs")
}
func sourceOriginVerified(root, repository string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "-C", root, "remote", "get-url", "origin")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		return false
	}
	origin := strings.ToLower(strings.TrimSpace(string(output)))
	return origin == "https://github.com/"+repository+".git" || origin == "https://github.com/"+repository || origin == "git@github.com:"+repository+".git"
}
func boundedSchedulerTree(path string) bool {
	// Only standard scheduler children of these already verified PC-tool sources.
	// This is workspace creation scope, not a substitute private exclusions file.
	namePattern := regexp.MustCompile(`^todo-([1-9][0-9]*)-[A-Za-z0-9_-]+$`)
	for _, root := range roots {
		container := filepath.Clean(root + "-wt")
		rel, err := filepath.Rel(container, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			continue
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		match := namePattern.FindStringSubmatch(parts[0])
		if match == nil {
			continue
		}
		// Git is an external process. Check path membership first so one
		// unrelated root cannot consume every scheduled repair's deadline.
		// The matching root still needs the same origin and physical checks.
		if !sourceVerified(root) {
			continue
		}
		if _, err := os.Stat(container); err == nil {
			resolved, e := physical(container)
			if e != nil || !same(resolved, container) {
				continue
			}
		} else if !os.IsNotExist(err) {
			continue
		}
		parent, e := physical(filepath.Dir(container))
		rootParent, e2 := physical(filepath.Dir(root))
		if e != nil || e2 != nil || !same(parent, rootParent) {
			continue
		}
		workspace := filepath.Join(container, parts[0])
		if _, err := os.Stat(workspace); os.IsNotExist(err) {
			return len(parts) == 1
		}
		resolved, e := physical(workspace)
		if e != nil || !same(resolved, workspace) || !sourceVerified(workspace) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(ctx, "git", "-C", workspace, "branch", "--show-current")
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		command.WaitDelay = time.Second
		branch, e := command.Output()
		cancel()
		if e != nil || !strings.HasPrefix(strings.TrimSpace(string(branch)), "scheduler/todo-"+match[1]+"-") {
			continue
		}
		if target, e := physical(path); e == nil && within(resolved, target) {
			return true
		}
	}
	return false
}
func legacyMatch(path string) bool {
	// Never emit, copy, or return any line from the private registry.
	file, err := os.Open("D:/CC/Tools/client-projects.txt")
	if err != nil {
		return !os.IsNotExist(err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 || !utf8.Valid(data) || len(strings.TrimSpace(string(data))) == 0 {
		return true
	}
	normalized := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		entry := strings.ToLower(strings.TrimSpace(line))
		if entry != "" && !strings.HasPrefix(entry, "#") && strings.Contains(normalized, strings.ReplaceAll(entry, "\\", "/")) {
			return true
		}
	}
	return false
}
func allowed(path string) bool {
	path, e := portable(path)
	if e != nil {
		return false
	}
	absolute, err := filepath.Abs(path)
	if err != nil || legacyMatch(absolute) {
		return false
	}
	if conversationPathAllowed(absolute) {
		return true
	}
	if productPathAllowed(absolute) {
		return true
	}
	if boundedSchedulerTree(absolute) {
		return true
	}
	target, err := physical(absolute)
	if err != nil {
		// The one explicitly authorized new worktree may be created empty.
		planned := same(absolute, roots[1]) || same(absolute, roots[2])
		if !os.IsNotExist(err) || !planned || !sourceVerified(base) {
			return false
		}
		parent, e := physical(filepath.Dir(absolute))
		expected, e2 := physical("D:/CC/Work")
		return e == nil && e2 == nil && same(parent, expected)
	}
	if legacyMatch(target) {
		return false
	}
	if runtimeExecutable(absolute, target, publicationGateRuntime) && sourceVerified(base) {
		return true
	}
	for _, root := range roots {
		resolved, e := physical(root)
		if e == nil && within(resolved, target) && sourceVerified(resolved) {
			return true
		}
	}
	if !sourceVerified(base) {
		return false
	}
	for _, receipt := range receipts {
		resolved, e := physical(receipt)
		if e == nil && same(absolute, receipt) && same(target, resolved) {
			return true
		}
	}
	return false
}
func main() {
	path := flag.String("guard-path", "", "resource absolute path")
	product := flag.Int("verify-product-resume", 0, "verify original ToDo 1626, 2407 or 2475; no launch or mutation")
	flag.Parse()
	if *product != 0 {
		db, err := readObject(productStore, 64<<20)
		if err != nil {
			_ = json.NewEncoder(os.Stdout).Encode(object{"verified": false, "reason": "store_unavailable"})
			os.Exit(1)
		}
		r := verifyProduct(db, *product)
		_ = json.NewEncoder(os.Stdout).Encode(r)
		if r["verified"] != true {
			os.Exit(1)
		}
		return
	}
	ok := *path != "" && filepath.IsAbs(*path)
	verified := ok && allowed(*path)
	result := map[string]any{"ok": ok, "client_resource": !verified, "classification_unverified": !verified, "original_registry_recovered": false, "scope": "verified repair source and explicitly supplied operational receipts only"}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if !ok {
		os.Exit(2)
	}
}
