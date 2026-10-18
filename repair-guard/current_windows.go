package main

// Current, explicitly requested succession is separate from recovery of the
// original registry and original task IDs. This scope admits one new actor.
import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const currentAORoot = "C:/Users/makhmeto/Desktop/dev/ADJ/GoDD-AO"
const currentAORepository = "autodevjapan/godd-ao"
const currentAOTask = "2839"
const currentAOSpec = "D:/CC/Tools/todo/specs/2839-ccac08e67b3b4a4488f3f0241c59b32c.md"
const currentAOSpecSHA = "b44e6c70614089c50908901e7d60a902f9e2f0793c813f9a0c2595f03714fd33"
const currentHandoff = "D:/CC/Work/worker-resume-20261018-2103/hand-off.private.md"

func currentAOQualification() bool {
	db, err := readObject(productStore, 64<<20)
	if err != nil {
		return false
	}
	return currentQualification(db, 2839, currentAORoot, currentAOSpec, currentAOSpecSHA, legacyMatch)
}

func currentQualification(db object, task int, root, spec, expectedHash string, protected func(string) bool) bool {
	row := itemAt(db, task)
	if row == nil || row["project"] != "GoDD-AO" || !same(str(obj(db["projects"])["GoDD-AO"]), root) || !same(str(row["spec"]), spec) {
		return false
	}
	p, err := physical(spec)
	if err != nil || !same(p, spec) || protected(p) || !currentPlain(spec, false) {
		return false
	}
	b, err := readBounded(p, 1<<20)
	return err == nil && hashBytes(b) == expectedHash
}

func currentGit(root string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err == nil
}

func currentPlain(path string, directory bool) bool {
	info, err := os.Lstat(path)
	if err != nil || !plainEntry(info) || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return false
	}
	p, err := physical(path)
	return err == nil && same(path, p) && currentNoReparse(path)
}

// Recent Go versions do not resolve every Windows junction in EvalSymlinks.
// Inspect each actual ancestor instead of inferring absence of reparse points
// from an unchanged result. This does not traverse directory contents.
func currentNoReparse(path string) bool {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || !plainEntry(info) {
			return false
		}
		if filepath.Dir(p) == p {
			return true
		}
	}
}

func currentProductPath(path, root, repository, task string, protected func(string) bool) bool {
	// Test path membership before Git or private metadata reads. Neither sibling
	// worktrees nor an old actor's original ID are covered by this scope.
	container := filepath.Clean(root + "-wt")
	rel, err := filepath.Rel(container, path)
	child := ""
	if err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		child = strings.Split(rel, string(os.PathSeparator))[0]
	}
	inRoot := within(root, path)
	if !inRoot && !regexp.MustCompile(`^todo-`+regexp.QuoteMeta(task)+`-[A-Za-z0-9_-]+$`).MatchString(child) {
		return false
	}
	if protected(path) || protected(root) || !currentPlain(root, true) || !sourceOriginVerified(root, repository) {
		return false
	}
	if inRoot {
		target, e := physical(path)
		return e == nil && within(root, target) && !protected(target) && currentNoReparse(path)
	}
	// The existing container must not be a junction. A planned child is
	// admitted only at its root, so ordinary scheduler creation can proceed.
	if protected(container) || !currentPlain(container, true) {
		return false
	}
	workspace := filepath.Join(container, child)
	if _, e := os.Lstat(workspace); os.IsNotExist(e) {
		return same(path, workspace)
	}
	if protected(workspace) || !currentPlain(workspace, true) || !sourceOriginVerified(workspace, repository) {
		return false
	}
	branch, ok := currentGit(workspace, "branch", "--show-current")
	if !ok || !strings.HasPrefix(branch, "scheduler/todo-"+task+"-") {
		return false
	}
	common, ok := currentGit(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	other, ok2 := currentGit(workspace, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if !ok || !ok2 || !same(common, other) {
		return false
	}
	target, e := physical(path)
	return e == nil && within(workspace, target) && !protected(target) && currentNoReparse(path)
}

func currentScopePathAllowed(path string) bool {
	if same(path, currentHandoff) {
		return !legacyMatch(path) && currentPlain(path, false) && sourceVerified(base)
	}
	// Only this exact current product and actor qualify. Existing denials
	// remain authoritative; no lost original row is changed or restored.
	if !within(currentAORoot, path) && !within(currentAORoot+"-wt", path) {
		return false
	}
	return currentAOQualification() && currentProductPath(path, currentAORoot, currentAORepository, currentAOTask, legacyMatch)
}
