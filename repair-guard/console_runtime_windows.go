package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type consoleEvidence struct{ root, path, sha256 string }
type consolePin struct {
	sha256   string
	evidence []consoleEvidence
}
type consoleLeaf struct {
	path string
	pins []consolePin
}

const console3050Source = "D:/CC/Work/unattended-operations-20261013-wt/todo-3050-console"
const console3020Receipts = model2903Source
const console3103Source = "D:/CC/Work/unattended-operations-20261013-wt/todo-3103-godd-mcp-godd-mcp"

// The source and independent distribution receipts identify these two exact
// leaves. Neither a directory nor an arbitrary new executable is admitted.
// All pins are compiled in; no caller-supplied manifest grants access.
var consoleRuntimeLeaves = []consoleLeaf{
	{path: "D:/CC/Tools/localai-web-rs/target/release/localai-web-rs.exe", pins: []consolePin{
		// The media-only Console at fixed source 264a13f. Build provenance,
		// successful CI and the independently received model review are frozen
		// outside the installed executable; no caller manifest is trusted.
		{sha256: "b69ec9003f62668150584689e7e34304f4469e070d6f413752dba91b0d5a8ef3", evidence: []consoleEvidence{
			{console3103Source, console3103Source + "/.ci-work/media-first-build-provenance.private.json", "e79b319e2f49a38a4b1968fe64fbd09bf2d29e44a18be5083ce9c7274f1787b4"},
			{console3103Source, console3103Source + "/.ci-work/media-first-ci-receipt.private.json", "651ef43ef4119c95228276710a65746fd6cc57d554a649611de98ce9859ba564"},
			{console3103Source, console3103Source + "/.ci-work/media-first-review-receipt.private.json", "9eca876f2bf8e62ddfe254439c55c4e1b2da7631becbb31234943b7bf06d7759"},
		}},
		{sha256: "c63a5e8028e90ee720d3d5cc0350c48361cefa8336233a466a909cb833c30b61", evidence: []consoleEvidence{
			{console3020Receipts, console3020Receipts + "/.ci-work/progress3020-cache-deployed.private.json", "1251a100281b7f2a69ed9004cf20e4c224a7a80f6734eaeb8a3f791252761b91"},
			{console3020Receipts, console3020Receipts + "/.ci-work/deploy-console-progress3020-cache.ps1", "2deb577bf786332c38f075f1aaa1366ac302849bb3524e51f5dcd1286cadf606"},
		}},
		{sha256: "0e40ef0ffa4bfc87c42ad682942f450f79e37299426c896f5c708720b38e8437", evidence: []consoleEvidence{
			{console3050Source, console3050Source + "/docs/console-workforce-3050-artifacts.json", "9a8828787ab2652f2786f5391ff4b5810460d303b190f8005bca8bc34c3946cc"},
		}},
	}},
	{path: "D:/CC/Tools/agent-progress/agent-progress.exe", pins: []consolePin{
		{sha256: "581c813d3321c47580d21f07dd7ef7a3b654bd5d4d761cdbf7bb275cd8e7f8d1", evidence: []consoleEvidence{
			{console3050Source, console3050Source + "/tools/codex-progress-tui/activation/verification-3021.json", "1139589976351bb4749099cabb594dd2acc766289907c47cc8bb6c35dc2fbde5"},
		}},
		{sha256: "ec3d2c15e0a3e9d3f0a615b1dc40cbcf4e74c8373aee36d5dcc8926e2c187d46", evidence: []consoleEvidence{
			{console3050Source, console3050Source + "/docs/console-workforce-3050-artifacts.json", "9a8828787ab2652f2786f5391ff4b5810460d303b190f8005bca8bc34c3946cc"},
		}},
	}},
}

func consoleRuntimeAllowed(path string) bool {
	return consoleRuntimeWith(path, consoleRuntimeLeaves, legacyMatch, consoleSourceVerified)
}

func consoleSourceVerified(root string) bool {
	// Reuse the existing bounded, hidden Git invocation with GIT_* overrides
	// removed. An inherited parent repository is not the declared source.
	if !civ6GitRoot(root) {
		return false
	}
	value, err := civ6GitAt(root, "remote", "get-url", "origin")
	origin := strings.ToLower(strings.TrimSpace(string(value)))
	return err == nil && (origin == "https://github.com/autodevjapan/cc-pc-tools-rs.git" || origin == "https://github.com/autodevjapan/cc-pc-tools-rs" || origin == "git@github.com:autodevjapan/cc-pc-tools-rs.git")
}

func consoleRuntimeWith(path string, leaves []consoleLeaf, protected func(string) bool, source func(string) bool) bool {
	for _, leaf := range leaves {
		if !same(path, leaf.path) || !runtimeLeaf(path, filepath.Dir(leaf.path), protected, true) {
			continue
		}
		for _, pin := range leaf.pins {
			if len(pin.evidence) == 0 {
				continue
			}
			valid := true
			for _, evidence := range pin.evidence {
				resolved, err := physical(evidence.root)
				if err != nil || !same(resolved, evidence.root) || protected(evidence.root) || !source(evidence.root) || !within(evidence.root, evidence.path) ||
					!runtimeLeaf(evidence.path, filepath.Dir(evidence.path), protected, true) || !consolePinnedFile(evidence.path, evidence.sha256) {
					valid = false
					break
				}
			}
			// Contents are inspected only inside the normal classifier, after the
			// exact leaf, physical path, protection and independent origin checks.
			if valid && consolePinnedFile(path, pin.sha256) {
				return true
			}
		}
	}
	return false
}

func consolePinnedFile(path, expected string) bool {
	if len(expected) != 64 || strings.ToLower(expected) != expected {
		return false
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return false
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	// Deny writes and replacement while hashing this same handle. Opening the
	// reparse point itself avoids following a link swapped after runtimeLeaf.
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000|0x08000000, 0)
	if err != nil {
		return false
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	var before, after syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &before) != nil || before.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 || before.NumberOfLinks != 1 {
		return false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 64<<20 {
		return false
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(f, (64<<20)+1))
	if err != nil || n != info.Size() || syscall.GetFileInformationByHandle(h, &after) != nil {
		return false
	}
	// The read itself may update access time. Identity, contents' write time,
	// size, link count and attributes must remain identical.
	before.LastAccessTime = after.LastAccessTime
	if before != after {
		return false
	}
	resolved, err := physical(path)
	return err == nil && same(path, resolved) && hex.EncodeToString(digest.Sum(nil)) == expected
}
