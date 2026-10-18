package main

// GitHub JSON is consumed only in the authenticated controller. Only hash-
// verified Git objects and tracked bytes cross into the new private runtime.
import (
	"bytes"
	"cc/product-ci/pr148scope"
	"compress/zlib"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sourceCommit struct {
	SHA               string
	Tree              struct{ SHA string }
	Parents           []struct{ SHA string }
	Author, Committer struct{ Name, Email, Date string }
	Message           string
	Verification      struct{ Payload, Signature string }
}
type sourceObject struct {
	Kind  string
	Bytes []byte
}
type sourceBundle struct {
	Head, Tree string
	Objects    map[string]sourceObject
	Entries    []pr148scope.TreeEntry
}

func objectID(kind string, b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// Never invent a replacement commit if GitHub has normalized an unavailable
// timezone or header. The exact object hash is the acceptance criterion.
func sourceCommitBytes(c sourceCommit, head string) ([]byte, error) {
	accept := func(s string) bool { return objectID("commit", []byte(s)) == head }
	payload := c.Verification.Payload
	if payload != "" {
		if c.Verification.Signature == "" && accept(payload) {
			return []byte(payload), nil
		}
		if i := strings.Index(payload, "\n\n"); i >= 0 && c.Verification.Signature != "" {
			body := payload[:i+1] + "gpgsig " + strings.ReplaceAll(strings.TrimSuffix(c.Verification.Signature, "\n"), "\n", "\n ") + "\n" + payload[i+1:]
			if accept(body) {
				return []byte(body), nil
			}
		}
	}
	a, ae := time.Parse(time.RFC3339, c.Author.Date)
	b, be := time.Parse(time.RFC3339, c.Committer.Date)
	if ae != nil || be != nil {
		return nil, errors.New("commit dates unavailable")
	}
	prefix := "tree " + c.Tree.SHA + "\n"
	for _, p := range c.Parents {
		prefix += "parent " + p.SHA + "\n"
	}
	for _, atz := range []string{"+0000", "+0900"} {
		for _, btz := range []string{"+0000", "+0900"} {
			for _, tail := range []string{"", "\n"} {
				s := prefix + fmt.Sprintf("author %s <%s> %d %s\ncommitter %s <%s> %d %s\n", c.Author.Name, c.Author.Email, a.Unix(), atz, c.Committer.Name, c.Committer.Email, b.Unix(), btz)
				if c.Verification.Signature != "" {
					s += "gpgsig " + strings.ReplaceAll(strings.TrimSuffix(c.Verification.Signature, "\n"), "\n", "\n ") + "\n"
				}
				s += "\n" + c.Message + tail
				if accept(s) {
					return []byte(s), nil
				}
			}
		}
	}
	return nil, errors.New("exact Git commit bytes unavailable")
}

func sourcePathOK(s string) bool {
	if len(s) == 0 || len(s) > 240 || strings.ContainsAny(s, "\\:\x00\r\n\t<>\"|?*") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimRight(part, " .") != part || strings.EqualFold(part, ".git") || strings.EqualFold(part, "git~1") || strings.EqualFold(part, ".git~1") {
			return false
		}
		x := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if x == "CON" || x == "PRN" || x == "AUX" || x == "NUL" || (len(x) == 4 && (strings.HasPrefix(x, "COM") || strings.HasPrefix(x, "LPT")) && x[3] >= '1' && x[3] <= '9') {
			return false
		}
	}
	return true
}

func fetchSource(head, tree string, read readAPI) (*sourceBundle, error) {
	if !validSHA(head) || !validSHA(tree) {
		return nil, errors.New("source pins required")
	}
	var c sourceCommit
	if e := read("repos/"+repository+"/git/commits/"+head, &c); e != nil {
		return nil, e
	}
	if c.SHA != head || c.Tree.SHA != tree {
		return nil, errors.New("source commit/tree changed")
	}
	cb, e := sourceCommitBytes(c, head)
	if e != nil {
		return nil, e
	}
	var t pr148scope.TreeReply
	if e = read("repos/"+repository+"/git/trees/"+tree+"?recursive=1", &t); e != nil {
		return nil, e
	}
	if t.SHA != tree || t.Truncated || len(t.Entries) == 0 || len(t.Entries) > 20000 {
		return nil, errors.New("bounded complete source tree required")
	}
	s := &sourceBundle{Head: head, Tree: tree, Objects: map[string]sourceObject{head: {"commit", cb}}, Entries: t.Entries}
	seen := map[string]bool{}
	byDir := map[string][]pr148scope.TreeEntry{}
	dirs := map[string]string{"": tree}
	var total int
	for _, x := range t.Entries {
		key := strings.ToLower(x.Path)
		if !sourcePathOK(x.Path) || !validSHA(x.SHA) || seen[key] {
			return nil, errors.New("source path/hash/case collision rejected")
		}
		seen[key] = true
		i := strings.LastIndex(x.Path, "/")
		parent := ""
		if i >= 0 {
			parent = x.Path[:i]
		}
		byDir[parent] = append(byDir[parent], x)
		if x.Type == "tree" && x.Mode == "040000" {
			dirs[x.Path] = x.SHA
			continue
		}
		if x.Type != "blob" || (x.Mode != "100644" && x.Mode != "100755") {
			return nil, errors.New("unsupported source mode; never omit a tracked entry")
		}
		if _, ok := s.Objects[x.SHA]; ok {
			continue
		}
		var v struct {
			SHA, Encoding, Content string
			Size                   int
		}
		if e = read("repos/"+repository+"/git/blobs/"+x.SHA, &v); e != nil {
			return nil, e
		}
		if len(v.Content) > 24<<20 {
			return nil, errors.New("source blob response bound")
		}
		b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
		if err != nil || v.Encoding != "base64" || v.SHA != x.SHA || v.Size != len(b) || len(b) > 16<<20 || objectID("blob", b) != x.SHA {
			return nil, errors.New("source blob bytes rejected")
		}
		total += len(b)
		if total > 256<<20 {
			return nil, errors.New("source RAM bound")
		}
		s.Objects[x.SHA] = sourceObject{"blob", b}
	}
	for dir, entries := range byDir {
		id, ok := dirs[dir]
		if !ok {
			return nil, errors.New("source parent tree missing")
		}
		sort.Slice(entries, func(i, j int) bool {
			name := func(x pr148scope.TreeEntry) string {
				n := filepath.Base(x.Path)
				if x.Type == "tree" {
					n += "/"
				}
				return n
			}
			return name(entries[i]) < name(entries[j])
		})
		var b bytes.Buffer
		for _, x := range entries {
			mode := x.Mode
			if x.Type == "tree" {
				mode = "40000"
			}
			fmt.Fprintf(&b, "%s %s%c", mode, filepath.Base(x.Path), byte(0))
			raw, _ := hex.DecodeString(x.SHA)
			b.Write(raw)
		}
		if objectID("tree", b.Bytes()) != id {
			return nil, errors.New("source tree bytes/hash mismatch")
		}
		s.Objects[id] = sourceObject{"tree", b.Bytes()}
	}
	// Git normally omits empty trees; explicitly support and verify them.
	for dir, id := range dirs {
		if len(byDir[dir]) == 0 {
			if objectID("tree", nil) != id {
				return nil, errors.New("empty source tree hash mismatch")
			}
			s.Objects[id] = sourceObject{"tree", nil}
		}
	}
	return s, nil
}

func materializeSource(p Plan, read readAPI) error {
	if p.Fixture != "" || p.validate() != nil {
		return errors.New("real source plan required")
	}
	if e := verifyRouting(p, read); e != nil {
		return e
	}
	s, e := fetchSource(p.Head, p.Tree, read)
	if e != nil {
		return e
	}
	if e = writeSource(p.source(), s); e != nil {
		return e
	}
	if e = verifySource(p, p.source(), true); e != nil {
		return e
	}
	return saveNew(filepath.Join(p.Root, "source-materialized.json"), map[string]any{"head": p.Head, "tree": p.Tree, "tracked_entries": len(s.Entries), "objects": len(s.Objects), "credential_free": true, "official_ci_claimed": false})
}

// Destination must be absent. No clone/checkout, archive extraction, host
// command or caller-chosen destination is exposed by a public mode.
func writeSource(dest string, s *sourceBundle) error {
	if _, e := os.Lstat(dest); !os.IsNotExist(e) {
		return errors.New("source destination must be new")
	}
	if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return e
	}
	if e := os.Mkdir(dest, 0700); e != nil {
		return e
	}
	git := filepath.Join(dest, ".git")
	for _, sub := range []string{"objects", "refs/heads"} {
		if e := os.MkdirAll(filepath.Join(git, sub), 0700); e != nil {
			return e
		}
	}
	for id, obj := range s.Objects {
		if objectID(obj.Kind, obj.Bytes) != id {
			return errors.New("source object changed before write")
		}
		var b bytes.Buffer
		z := zlib.NewWriter(&b)
		fmt.Fprintf(z, "%s %d%c", obj.Kind, len(obj.Bytes), byte(0))
		z.Write(obj.Bytes)
		if e := z.Close(); e != nil {
			return e
		}
		if e := os.MkdirAll(filepath.Join(git, "objects", id[:2]), 0700); e != nil {
			return e
		}
		if e := createSourceFile(filepath.Join(git, "objects", id[:2], id[2:]), b.Bytes()); e != nil {
			return e
		}
	}
	for _, x := range s.Entries {
		path := filepath.Join(dest, filepath.FromSlash(x.Path))
		if e := beneath(dest, path); e != nil {
			return e
		}
		if x.Type == "tree" {
			if e := os.MkdirAll(path, 0700); e != nil {
				return e
			}
			continue
		}
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		if e := createSourceFile(path, s.Objects[x.SHA].Bytes); e != nil {
			return e
		}
	}
	config := "[core]\n\trepositoryformatversion = 0\n\tbare = false\n\tautocrlf = false\n\tfilemode = false\n[remote \"origin\"]\n\turl = https://github.com/" + repository + "\n"
	for name, b := range map[string]string{"HEAD": s.Head + "\n", "shallow": s.Head + "\n", "config": config} {
		if e := createSourceFile(filepath.Join(git, name), []byte(b)); e != nil {
			return e
		}
	}
	_, e := sourceGit(dest, "read-tree", s.Head)
	return e
}

func createSourceFile(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	return errors.Join(e, f.Close())
}
