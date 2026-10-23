//go:build windows

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Plan.source() in the reviewed Windows producer, not a caller-supplied tree.
const ci3250Git = "runtime/runner/_work/civ6-banseiki/civ6-banseiki/.git"
const ci3250Worktree = "D:/CC/Work/unattended-operations-20261013-wt/todo-2962-ci-pc-actions"

func ci3250OwnedParent(root string) bool {
	return same(root, `C:\Users\makhmeto\AppData\Local\Temp`) || same(root, ci3250Worktree)
}

func ci3250QuarantineParent(root, configured string) string {
	if same(root, ci3250Worktree) && same(configured, `C:\Users\makhmeto\Downloads\削除希望`) {
		return `D:\削除希望`
	}
	return configured
}

func ci3250PreservedReason(root, reason string) bool {
	return reason == "windows" || same(root, ci3250Worktree) && (reason == "size" || reason == "attributes")
}

func ci3250WinPath(path string) string {
	p, e := filepath.Abs(path)
	if e != nil {
		return path
	}
	p = filepath.Clean(p)
	if strings.HasPrefix(p, `\\?\`) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	return `\\?\` + p
}

// Admission belongs to the normally distributed root-only guard. A caller can
// supply only an original safe-delete ID; no root, manifest, grant or success
// override exists. Keep the collector's existing claim lock until record close.
func ci3250Guard(root string) error {
	b, e := runHidden(90*time.Second, `D:\CC\Tools\client-records\scrub391.exe`, "--guard-path", root)
	if e != nil || !ci3250QualifiedResponse(b) {
		return fail("CI_RETIREMENT_UNQUALIFIED")
	}
	return nil
}
func ci3250QualifiedResponse(b []byte) bool {
	var r struct {
		Client     *bool `json:"client_resource"`
		Unverified *bool `json:"classification_unverified"`
		Retirement *bool `json:"ci_retirement_qualified"`
	}
	return json.Unmarshal(b, &r) == nil && r.Client != nil && !*r.Client && r.Unverified != nil && !*r.Unverified && r.Retirement != nil && *r.Retirement
}

func (c config) ci3250Candidate(id string) bool {
	if id == ci3174ID || !same(c.Store, "D:/CC/Tools/safe-delete/receipts-go") || !same(c.Quarantine, `C:\Users\makhmeto\Downloads\削除希望`) {
		return false
	}
	r, e := c.load(id, true)
	return e == nil && r.Project == "civ6-ci-queue" && strings.HasPrefix(r.Content, "job-") && ci3250OwnedParent(r.Root)
}

func (c config) ci3250Recovery(id string) (*ci3174Scope, func(), error) {
	if !c.ci3250Candidate(id) {
		return nil, nil, fail("CI_SCOPE_MISMATCH")
	}
	r, e := c.load(id, true)
	if e != nil {
		return nil, nil, e
	}
	unlock, e := ci3174Lock()
	if e != nil {
		return nil, nil, e
	}
	failed := true
	defer func() {
		if failed {
			unlock()
		}
	}()
	if e = ci3250Guard(r.Path); e != nil {
		return nil, nil, e
	}
	root, e := ci3174Identity(r.Path)
	if e != nil {
		return nil, nil, e
	}
	s := &ci3174Scope{maxEntries: c.MaxEntries, maxBytes: 16 << 30, identity: ci3174Identity, volume: r.Volume, rootID: r.FileID, rootBirth: ci3174Birth(root), captureCollected: true, internalLinks: true}
	before, _, e := s.snapshot(r.Path)
	if e != nil {
		return nil, nil, e
	}
	s.captureCollected = false
	s.baseline = before
	s.opaque = map[string]ci3174Entry{}
	for name, m := range before {
		if m.Attributes&0x400 != 0 {
			s.opaque[name] = m
		}
	}
	// Freeze the external original record's exact bytes; do not re-register it.
	p, _ := c.recordPath(id)
	b, e := ci3250Original(p)
	if e != nil {
		return nil, nil, e
	}
	s.originalReceipt = func() ([]byte, error) {
		current, e := ci3250Original(p)
		if e != nil || string(current) != string(b) {
			return nil, fail("CI_PROOF_CHANGED")
		}
		return current, nil
	}
	s.settled = func() error {
		if _, e := s.originalReceipt(); e != nil {
			return e
		}
		return ci3250Guard(r.Path)
	}
	failed = false
	return s, unlock, nil
}

func ci3250Original(path string) ([]byte, error) {
	// ci3174ProofBytes checks physical ancestry, single-link metadata and reads
	// only the external producer JSON, never anything in the opaque CI root.
	if _, e := checked(path); e != nil {
		return nil, e
	}
	before, e := fileIdentity(path)
	if e != nil || before.NumberOfLinks != 1 || before.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return nil, fail("CI_PROOF_INVALID")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, fail("CI_PROOF_INVALID")
	}
	b, e := io.ReadAll(io.LimitReader(f, 8193))
	f.Close()
	after, ae := fileIdentity(path)
	if e != nil || ae != nil || len(b) > 8192 || geminiMetadata(before) != geminiMetadata(after) {
		return nil, fail("CI_PROOF_CHANGED")
	}
	return b, nil
}
