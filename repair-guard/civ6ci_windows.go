package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// The canonical source contract is part of this reviewed guard, not a runtime
// allowlist. Changing a path requires the ordinary source/review/CI process.
//
//go:embed civ6-ci-2962-contract.json
var civ6Contract []byte

const civ6Home = "D:/CC/Tools/civ6-ci-2962"
const civ6Source = "D:/CC/Work/unattended-operations-20261013-wt/todo-2962-ci-pc-actions"
const civ6Floor = "ab45cb5d221077fa65484c706b3353cdc4867c07"
const civ6Receipt = civ6Source + "/docs/civ6-ci-2962-deployment.private.json"
const civ6NotBefore = "2026-10-20T05:50:00Z"
const civ6Owner = "S-1-5-21-3727265621-1182195423-444854664-1001"
const civ6ArchiveSHA = "f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f"
const civ6ArchiveBytes = 104063355

var civ6Leaves = regexp.MustCompile(`^(?:civ6-ci-(?:queue|windows|route)\.exe|civ6-ci-queue\.json|actions-runner-win-x64-2\.338\.0\.zip|deployment-receipt\.json)$`)
var civ6Update = regexp.MustCompile(`^(?:civ6-ci-(?:queue|windows|route)\.exe|civ6-ci-queue\.json|deployment-receipt\.json)\.pr[1-9][0-9]{0,5}\.(?:tmp|backup)$`)
var civ6Key = regexp.MustCompile(`^[1-9][0-9]{0,17}-[1-9][0-9]{0,3}-[1-9][0-9]{0,17}$`)
var civ6Cache = regexp.MustCompile(`^(?:[0-9a-f]{64}\.json|\.c-[0-9]{1,20}\.tmp)$`)
var civ6LedgerTemp = regexp.MustCompile(`^\.ledger-[0-9]{1,20}\.tmp$`)
var civ6ResultTemp = regexp.MustCompile(`^\.r-[0-9]{1,20}\.tmp$`)

// State is restricted to queue-generated names, never an arbitrary subtree.
// Existing state still requires an independently saved identity entry below.
func civ6Name(rel string) (directory, valid bool) {
	p := strings.Split(filepath.ToSlash(rel), "/")
	if rel == "." {
		return true, true
	}
	if len(p) == 1 && (civ6Leaves.MatchString(p[0]) || civ6Update.MatchString(p[0])) {
		return false, true
	}
	if p[0] != "state" {
		return false, false
	}
	if len(p) == 1 {
		return true, true
	}
	if len(p) == 2 {
		switch p[1] {
		case "entries", "evidence", "cache", "locks":
			return true, true
		case "ledger.json", "ledger.json.lock", "tick.lock":
			return false, true
		}
		return false, civ6LedgerTemp.MatchString(p[1])
	}
	if len(p) == 3 && p[1] == "cache" {
		return false, civ6Cache.MatchString(p[2])
	}
	if len(p) == 3 && p[1] == "locks" {
		return false, p[2] == "os-Windows.lock"
	}
	if (p[1] == "entries" || p[1] == "evidence") && civ6Key.MatchString(p[2]) {
		if len(p) == 3 {
			return true, true
		}
		if p[1] == "entries" && len(p) == 4 {
			switch p[3] {
			case "result.json", "plan.json", "admin-prepare.ps1", "admin-collect.ps1", "admin-collect-unstarted.ps1", "prepare.log", "execute.log", "stop.log", "collect.log", "collect-unstarted.log", "run.log":
				return false, true
			}
			return false, civ6ResultTemp.MatchString(p[3])
		}
		// Evidence can contain original diagnostic names. Only its manifest
		// qualifies here; deeper copies stay protected unless separately reviewed.
		return false, len(p) == 4 && p[3] == "manifest.json"
	}
	return false, false
}

type civ6Object struct {
	Volume           uint32
	ID               uint64
	Created          int64
	Owner            string
	Size             uint64
	Written, Changed int64
	SHA256           string
}
type civ6Deployment struct {
	Domain, ContractSHA256, SourceHead string
	Parent, Root                       civ6Object
	Objects                            map[string]civ6Object
}
type civ6Access struct {
	home, receipt string
	owner         string
	notBefore     int64
	at            int64
	protected     func(string) bool
	qualified     func() (string, bool)
	ancestor      func(string, string) bool
	identity      func(string) (audio2906Metadata, error)
	read          func(string, int64) ([]byte, bool)
}

func civ6Filetime(t time.Time) int64 { return t.UnixNano()/100 + 116444736000000000 }

func civ6GitEnvironment(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			out = append(out, entry)
		}
	}
	return out
}

func civ6Git(args ...string) ([]byte, error) {
	return civ6GitAt(civ6Source, args...)
}

func civ6GitAt(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	cmd.Env = civ6GitEnvironment(os.Environ())
	return cmd.Output()
}

func civ6Ancestor(older, newer string) bool {
	valid := regexp.MustCompile(`^[0-9a-f]{40}$`)
	if !valid.MatchString(older) || !valid.MatchString(newer) {
		return false
	}
	_, err := civ6Git("merge-base", "--is-ancestor", older, newer)
	return err == nil
}

func civ6SourceHead() (string, bool) {
	if legacyMatch(base) || !conversationPhysical(base) || !conversationPhysical(civ6Source) || legacyMatch(civ6Source) {
		return "", false
	}
	for _, root := range []string{base, civ6Source} {
		b, err := civ6GitAt(root, "remote", "get-url", "origin")
		origin := strings.ToLower(strings.TrimSpace(string(b)))
		if err != nil || (origin != "https://github.com/autodevjapan/cc-pc-tools-rs.git" && origin != "https://github.com/autodevjapan/cc-pc-tools-rs" && origin != "git@github.com:autodevjapan/cc-pc-tools-rs.git") {
			return "", false
		}
	}
	b, err := civ6Git("rev-parse", "HEAD")
	head := strings.TrimSpace(string(b))
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(head) {
		return "", false
	}
	if !civ6Ancestor(civ6Floor, head) {
		return "", false
	}
	if !currentGitHubIdentityVerified(currentProduct{repository: "autodevjapan/cc-pc-tools-rs", owner: "AutoDevJapan", repositoryID: 1406818713}) {
		return "", false
	}
	return head, true
}

func civ6Runtime(path string) bool {
	cutoff, err := time.Parse(time.RFC3339, civ6NotBefore)
	if err != nil {
		return false
	}
	return civ6Allowed(path, civ6Access{home: civ6Home, receipt: civ6Receipt, protected: legacyMatch,
		owner: civ6Owner, notBefore: civ6Filetime(cutoff), at: civ6Filetime(time.Now()), ancestor: civ6Ancestor,
		qualified: civ6SourceHead, identity: func(p string) (audio2906Metadata, error) { return resourceIdentity(p, true) },
		read: func(p string, n int64) ([]byte, bool) { return conversationBytes(p, n, legacyMatch) }})
}

func civ6Matches(w civ6Object, g audio2906Metadata, directory bool) bool {
	if w.Volume == 0 || w.ID == 0 || w.Created == 0 || w.Owner == "" || w.Volume != g.Volume || w.ID != g.ID || w.Created != g.Created || w.Owner != g.Owner || g.Attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	if directory {
		return g.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
	}
	return g.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 && g.Links == 1 && w.Size == g.Size && w.Written == g.Written && w.Changed == g.Changed
}

func civ6Allowed(path string, a civ6Access) bool {
	if !filepath.IsAbs(path) || !within(a.home, path) {
		return false
	}
	rel, err := filepath.Rel(a.home, path)
	directory, valid := civ6Name(rel)
	if err != nil || !valid {
		return false
	}
	// Reject live denies for the complete ancestry, before metadata or reads.
	for p := path; ; p = filepath.Dir(p) {
		if a.protected(p) {
			return false
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if a.owner == "" || a.notBefore <= 0 || a.at < a.notBefore || !conversationPhysical(filepath.Dir(a.home)) {
		return false
	}
	rootInfo, err := os.Lstat(a.home)
	if os.IsNotExist(err) {
		// Only mkdir of the exact root. No nonexistent child traversal, no
		// automatic promotion of a subsequently created or preexisting root.
		if _, e := os.Lstat(a.receipt); !os.IsNotExist(e) {
			return false
		}
		_, ok := a.qualified()
		return rel == "." && ok
	}
	if err != nil || !rootInfo.IsDir() || !conversationPhysical(a.home) || a.protected(a.receipt) {
		return false
	}
	root, err := a.identity(a.home)
	if err != nil || root.Created < a.notBefore || root.Owner != a.owner {
		return false
	}
	head, ok := a.qualified()
	if !ok {
		return false
	}
	receiptIdentity, err := a.identity(a.receipt)
	if err != nil || receiptIdentity.Links != 1 || receiptIdentity.Attributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return false
	}
	b, ok := a.read(a.receipt, 4<<20)
	if !ok {
		return false
	}
	parsed, ok := conversationJSON(b)
	if !ok || !civ6ReceiptKeys(parsed) {
		return false
	}
	var r civ6Deployment
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || r.Domain != "civ6-ci-2962-deployment-v1" || r.ContractSHA256 != hashBytes(civ6Contract) ||
		(!a.ancestor(civ6Floor, r.SourceHead) || !a.ancestor(r.SourceHead, head)) {
		return false
	}
	parent, err := a.identity(filepath.Dir(a.home))
	if err != nil || !civ6Matches(r.Parent, parent, true) {
		return false
	}
	if !civ6Matches(r.Root, root, true) || receiptIdentity.Owner != a.owner {
		return false
	}
	// Every existing ancestor under home must have its own recorded identity.
	for p := path; !same(p, a.home); p = filepath.Dir(p) {
		part, _ := filepath.Rel(a.home, p)
		isDir, nameOK := civ6Name(part)
		if !nameOK {
			return false
		}
		info, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil || !conversationPhysical(p) || (isDir != info.IsDir()) {
			return false
		}
		entry, exists := r.Objects[filepath.ToSlash(part)]
		actual, e := a.identity(p)
		if !exists || e != nil || !civ6Matches(entry, actual, isDir) || actual.Owner != a.owner || actual.Created < root.Created {
			return false
		}
		if !isDir && !strings.HasPrefix(filepath.ToSlash(part), "state/") && !conversationSHA.MatchString(entry.SHA256) {
			return false
		}
		if part == "actions-runner-win-x64-2.338.0.zip" && (entry.SHA256 != civ6ArchiveSHA || entry.Size != civ6ArchiveBytes) {
			return false
		}
	}
	// An absent immediate parent is not an invitation to create a whole tree.
	if !same(path, a.home) && !conversationPhysical(filepath.Dir(path)) {
		return false
	}
	if !same(path, a.home) {
		if info, e := os.Lstat(path); e == nil && (directory != info.IsDir()) {
			return false
		} else if e != nil && !os.IsNotExist(e) {
			return false
		}
	}
	finalRoot, e := a.identity(a.home)
	finalParent, pe := a.identity(filepath.Dir(a.home))
	return e == nil && pe == nil && civ6Matches(r.Root, finalRoot, true) && civ6Matches(r.Parent, finalParent, true) &&
		!a.protected(path) && !a.protected(a.home) && conversationPhysical(a.home)
}

// encoding/json accepts case aliases for struct fields. Require the exact
// schema spelling before decoding so two aliases cannot silently override.
func civ6ReceiptKeys(m object) bool {
	keys := func(v object, allowed string) bool {
		set := map[string]bool{}
		for _, k := range strings.Fields(allowed) {
			set[k] = true
		}
		for k := range v {
			if !set[k] {
				return false
			}
		}
		return true
	}
	fields := "Volume ID Created Owner Size Written Changed SHA256"
	if !keys(m, "Domain ContractSHA256 SourceHead Parent Root Objects") || !keys(obj(m["Parent"]), fields) || !keys(obj(m["Root"]), fields) {
		return false
	}
	for name, v := range obj(m["Objects"]) {
		if _, valid := civ6Name(filepath.FromSlash(name)); !valid || !keys(obj(v), fields) {
			return false
		}
	}
	return true
}
