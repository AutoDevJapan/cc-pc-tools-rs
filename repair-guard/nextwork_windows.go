package main

// The 2026-10-15 direct request grants one new engineering unit, not recovery
// of the lost registry or a state transition for any of the original 192 rows.
import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const nextWorkOwnerSpec = "D:/CC/Tools/todo/specs/2636-959549e314bd449198b5fa7675409c86.md"
const nextWorkOwnerHash = "d9882975ddadfd4126bc429911be92b0f3e4c7d39f208b161319eb7e1789464a"
const nextWorkSpec = "D:/CC/Tools/todo/specs/2637-16aed37d951048e4b1ba002c0158bbfb.md"
const nextWorkHash = "9fb375dcc3a8cbbf34e679e6d31f661b2bb9271d9a6ca6127d173f0b1a5d3024"
const nextWorkOriginalSpec = "D:/CC/Tools/todo/specs/880-77693cd060c34779b732423febb6dfa7.md"
const nextWorkOriginalHash = "8954d32a53a9f91a7fead3d2dd83c49b17d88666cc80e12477750c819cd80776"
const nextWorkRoot = "C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki"
const nextWorkOriginalTree = "C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki-wt/todo-880-pr86"
const nextWorkTree = "C:/Users/makhmeto/Desktop/dev/ADJ/civ6-banseiki-wt/todo-2637-prepare-880-2636"
const nextWorkOrigin = "git@github.com:AutoDevJapan/civ6-banseiki.git"

type nextWorkIO struct {
	readObject func(string, int64) (object, error)
	read       func(string, int64) ([]byte, error)
	physical   func(string) (string, error)
	origin     func(string) string
	branch     func(string) string
	protected  func(string) bool
	digest     func([]byte) string
	at         time.Time
}

func nextWorkPathAllowed(path string) bool {
	return nextWorkPathWith(path, nextWorkIO{readObject, readBounded, physical, exactOrigin, exactBranch, legacyMatch, hashBytes, time.Now()})
}

func exactOrigin(root string) string {
	return exactGitRead(root, "remote", "get-url", "origin")
}

func exactBranch(root string) string {
	return exactGitRead(root, "branch", "--show-current")
}

func exactGitRead(root string, arguments ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, arguments...)...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func nextWorkPathWith(path string, access nextWorkIO) bool {
	// Determine membership BEFORE reading any evidence or invoking Git.
	root := ""
	for _, candidate := range []string{nextWorkRoot, nextWorkOriginalTree, nextWorkTree} {
		if within(candidate, path) {
			root = candidate
			break
		}
	}
	receipt := same(path, nextWorkOwnerSpec) || same(path, nextWorkSpec) || same(path, nextWorkOriginalSpec)
	if root == "" && !receipt {
		return false
	}
	start := time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC) // October 15 JST
	expiry := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	if access.at.Before(start) || !access.at.Before(expiry) || access.protected(path) {
		return false
	}
	db, err := access.readObject(productStore, 64<<20)
	if err != nil {
		return false
	}
	owner, worker, original := itemAt(db, 2636), itemAt(db, 2637), itemAt(db, 880)
	if owner == nil || worker == nil || original == nil || owner["project"] != "cc-pc-tools-rs" ||
		worker["project"] != "civ6-banseiki" || original["project"] != "civ6-banseiki" ||
		!same(str(owner["spec"]), nextWorkOwnerSpec) || !same(str(worker["spec"]), nextWorkSpec) ||
		!same(str(original["spec"]), nextWorkOriginalSpec) ||
		!same(str(obj(db["projects"])["civ6-banseiki"]), nextWorkRoot) {
		return false
	}
	allocation := obj(worker["scheduler"])
	if (str(allocation["worktree"]) != "" && !same(str(allocation["worktree"]), nextWorkTree)) ||
		(str(allocation["branch"]) != "" && str(allocation["branch"]) != "scheduler/todo-2637-prepare-880-2636") {
		return false
	}
	// Hashes are independent compiled anchors, never taken from the mutable
	// evidence file or the current ledger. Check the exact bytes read here.
	for _, anchor := range []struct{ path, hash string }{
		{nextWorkOwnerSpec, nextWorkOwnerHash}, {nextWorkSpec, nextWorkHash}, {nextWorkOriginalSpec, nextWorkOriginalHash},
	} {
		p, e := access.physical(anchor.path)
		if e != nil || !same(p, anchor.path) || access.protected(p) {
			return false
		}
		b, e := access.read(anchor.path, 1<<20)
		if e != nil || access.digest(b) != anchor.hash {
			return false
		}
	}
	resolvedRoot, err := access.physical(nextWorkRoot)
	if err != nil || !same(resolvedRoot, nextWorkRoot) || access.protected(resolvedRoot) || access.origin(resolvedRoot) != nextWorkOrigin {
		return false
	}
	if receipt {
		return true // exact anchored receipt only
	}
	resolved, err := access.physical(root)
	if err != nil {
		// Only this one scheduler allocation may create its as-yet absent tree.
		// The physical parent must exist and stay in the specified container.
		if !os.IsNotExist(err) || !same(root, nextWorkTree) || !same(path, nextWorkTree) {
			return false
		}
		parent := filepath.Dir(nextWorkTree)
		p, e := access.physical(parent)
		return e == nil && same(p, parent) && !access.protected(p)
	}
	if !same(resolved, root) || access.protected(resolved) || access.origin(resolved) != nextWorkOrigin {
		return false
	}
	target, err := access.physical(path)
	if same(root, nextWorkTree) && access.branch(resolved) != "scheduler/todo-2637-prepare-880-2636" {
		return false
	}
	return err == nil && within(resolved, target) && !access.protected(target)
}
