package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

const releaseContextLimit = 128 << 20

type releaseTreeEntry struct{ Path, Mode, Type, SHA string }
type releaseTreeReply struct {
	SHA       string
	Truncated bool
	Entries   []releaseTreeEntry `json:"tree"`
}

// Neither names nor source bytes can be serialized. Only a verified RAM copy
// can become a build input; guest paths and guest-provided digests are not used.
type sealedReleaseContext struct {
	tree, archiveSHA256 string
	archive             []byte
	files               map[string]sealedReleaseFile
	directories         map[string]bool
}

type sealedReleaseFile struct {
	mode, blob, sha256 string
	data               []byte
}

func releaseSourcePath(s string) bool {
	if s == "" || len(s) > 2048 || path.Clean(s) != s || s == "." || s == ".." || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "../") || strings.ContainsAny(s, "\\\x00\r\n:") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

func releaseObjectSHA(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == ""
}

// Rebuild every Git tree object, including Git's directory ordering. Merely
// trusting the SHA field in a recursive tree reply would not bind its entries.
func verifyReleaseTree(t releaseTreeReply, expected string) (map[string]releaseTreeEntry, error) {
	if t.Truncated || t.SHA != expected || !releaseObjectSHA(expected) || len(t.Entries) == 0 || len(t.Entries) > 100000 {
		return nil, errors.New("release tree envelope rejected")
	}
	entries := make(map[string]releaseTreeEntry, len(t.Entries))
	children := map[string][]releaseTreeEntry{}
	for _, e := range t.Entries {
		if !releaseSourcePath(e.Path) || !releaseObjectSHA(e.SHA) {
			return nil, errors.New("release tree entry rejected")
		}
		if _, exists := entries[e.Path]; exists {
			return nil, errors.New("duplicate release tree entry")
		}
		if !(e.Type == "tree" && e.Mode == "040000" || e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755")) {
			return nil, errors.New("release tree mode rejected")
		}
		entries[e.Path] = e
		dir := path.Dir(e.Path)
		if dir == "." {
			dir = ""
		}
		children[dir] = append(children[dir], e)
	}
	for name := range entries {
		dir := path.Dir(name)
		if dir != "." && entries[dir].Type != "tree" {
			return nil, errors.New("release tree parent missing")
		}
	}
	for dir, list := range children {
		sort.Slice(list, func(i, j int) bool {
			a, b := path.Base(list[i].Path), path.Base(list[j].Path)
			if list[i].Type == "tree" {
				a += "/"
			}
			if list[j].Type == "tree" {
				b += "/"
			}
			return a < b
		})
		var raw bytes.Buffer
		for _, e := range list {
			mode := e.Mode
			if mode == "040000" {
				mode = "40000"
			}
			fmt.Fprintf(&raw, "%s %s%c", mode, path.Base(e.Path), 0)
			id, _ := hex.DecodeString(e.SHA) // Validated above.
			raw.Write(id)
		}
		want := expected
		if dir != "" {
			want = entries[dir].SHA
		}
		if gitObjectID("tree", raw.Bytes()) != want {
			return nil, errors.New("release tree object mismatch")
		}
	}
	// Empty directory objects have no children to visit above.
	for name, e := range entries {
		if e.Type == "tree" && len(children[name]) == 0 && e.SHA != gitObjectID("tree", nil) {
			return nil, errors.New("empty release tree object mismatch")
		}
	}
	return entries, nil
}

func sealReleaseContext(tree releaseTreeReply, compressed []byte) (sealedReleaseContext, error) {
	c, err := sealGitContext(tree, compressed, mergedTree)
	if err != nil {
		return sealedReleaseContext{}, err
	}
	for _, pin := range releaseBlobPins {
		found := false
		for _, f := range c.files {
			if f.blob == pin.blob && f.sha256 == pin.sha256 {
				found = true
			}
		}
		if !found {
			return sealedReleaseContext{}, errors.New("audited release blob absent from fixed tree")
		}
	}
	return c, nil
}

// Internal primitive shared by synthetic fixtures and the single fixed wrapper.
// There is no command/API that accepts an arbitrary expected tree or context.
func sealGitContext(tree releaseTreeReply, compressed []byte, expected string) (sealedReleaseContext, error) {
	entries, err := verifyReleaseTree(tree, expected)
	if err != nil {
		return sealedReleaseContext{}, err
	}
	if len(compressed) == 0 || len(compressed) > releaseContextLimit {
		return sealedReleaseContext{}, errors.New("release context compressed size rejected")
	}
	input := bytes.NewReader(compressed)
	gz, err := gzip.NewReader(input)
	if err != nil {
		return sealedReleaseContext{}, errors.New("release context gzip rejected")
	}
	defer gz.Close()
	gz.Multistream(false)
	// A malformed archive cannot expand without bound, including its headers.
	bounded := &io.LimitedReader{R: gz, N: releaseContextLimit + (16 << 20)}
	tr := tar.NewReader(bounded)
	files := map[string]sealedReleaseFile{}
	seen := map[string]bool{}
	root := ""
	total, headers := int64(0), 0
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return sealedReleaseContext{}, errors.New("release context tar rejected")
		}
		headers++
		if headers > 100000 {
			return sealedReleaseContext{}, errors.New("release context header count rejected")
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue // GitHub's non-file PAX commit metadata.
		}
		name := strings.TrimSuffix(h.Name, "/")
		if !releaseSourcePath(name) || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) || h.Linkname != "" {
			return sealedReleaseContext{}, errors.New("release context path or mode rejected")
		}
		prefix, relative, nested := strings.Cut(name, "/")
		if root == "" {
			root = prefix
		}
		if prefix != root || seen[name] {
			return sealedReleaseContext{}, errors.New("release context root or duplicate rejected")
		}
		seen[name] = true
		if !nested {
			if h.Typeflag != tar.TypeDir || h.Size != 0 {
				return sealedReleaseContext{}, errors.New("release context root entry rejected")
			}
			continue
		}
		original, ok := entries[relative]
		if !ok {
			return sealedReleaseContext{}, errors.New("untracked release context entry")
		}
		if h.Typeflag == tar.TypeDir {
			if original.Type != "tree" || h.Size != 0 {
				return sealedReleaseContext{}, errors.New("release context directory mismatch")
			}
			continue
		}
		if original.Type != "blob" || h.Size < 0 || h.Size > 64<<20 || total+h.Size > releaseContextLimit {
			return sealedReleaseContext{}, errors.New("release context file size rejected")
		}
		mode := int64(0644)
		if original.Mode == "100755" {
			mode = 0755
		}
		// Git stores only the executable bit. GitHub may use 0664/0775
		// according to tar umask; rebuild canonical 0644/0755 below.
		if h.Mode < 0 || h.Mode&^0777 != 0 || (h.Mode&0111 != 0) != (mode == 0755) {
			return sealedReleaseContext{}, errors.New("release context file mode mismatch")
		}
		b, e := io.ReadAll(io.LimitReader(tr, h.Size+1))
		if e != nil || int64(len(b)) != h.Size || gitBlobHash(b) != original.SHA {
			return sealedReleaseContext{}, errors.New("release context file bytes mismatch")
		}
		total += h.Size
		files[relative] = sealedReleaseFile{original.Mode, original.SHA, digest(b), b}
	}
	// Drain padding to verify the gzip checksum and reject appended tar payloads.
	tail, e := io.ReadAll(io.LimitReader(bounded, 1<<20))
	if e != nil || len(tail) == 1<<20 || bytes.Count(tail, []byte{0}) != len(tail) || input.Len() != 0 || bounded.N <= 0 {
		return sealedReleaseContext{}, errors.New("release context trailer rejected")
	}
	for name, e := range entries {
		if e.Type == "blob" {
			if _, ok := files[name]; !ok {
				return sealedReleaseContext{}, errors.New("release context tracked file missing")
			}
		}
	}
	if len(files) == 0 {
		return sealedReleaseContext{}, errors.New("empty release context rejected")
	}
	c := sealedReleaseContext{tree: expected, files: files, directories: map[string]bool{}}
	for name, e := range entries {
		if e.Type == "tree" {
			c.directories[name] = true
		}
	}
	c.archive, err = c.makeArchive()
	if err != nil {
		return sealedReleaseContext{}, err
	}
	c.archiveSHA256 = digest(c.archive)
	return c, nil
}

func (c sealedReleaseContext) makeArchive() ([]byte, error) {
	names := make([]string, 0, len(c.files))
	for name := range c.files {
		names = append(names, name)
	}
	for name := range c.directories {
		if _, exists := c.files[name]; exists || !releaseSourcePath(name) {
			return nil, errors.New("sealed release directory changed")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for _, name := range names {
		if c.directories[name] {
			if tw.WriteHeader(&tar.Header{Name: name + "/", Mode: 0755, Typeflag: tar.TypeDir, Uid: 1001, Gid: 1001, Format: tar.FormatPAX}) != nil {
				return nil, errors.New("sealed release directory header failed")
			}
			continue
		}
		f := c.files[name]
		// Hash and consume a private copy. Do not reopen a mutable guest path.
		b := bytes.Clone(f.data)
		if !releaseSourcePath(name) || gitBlobHash(b) != f.blob || digest(b) != f.sha256 {
			return nil, errors.New("sealed release file changed")
		}
		mode := int64(0644)
		if f.mode == "100755" {
			mode = 0755
		} else if f.mode != "100644" {
			return nil, errors.New("sealed release file mode changed")
		}
		h := &tar.Header{Name: name, Mode: mode, Size: int64(len(b)), Typeflag: tar.TypeReg, Uid: 1001, Gid: 1001, Format: tar.FormatPAX}
		if tw.WriteHeader(h) != nil {
			return nil, errors.New("sealed release header failed")
		}
		if _, err := tw.Write(b); err != nil {
			return nil, errors.New("sealed release archive failed")
		}
	}
	if tw.Close() != nil {
		return nil, errors.New("sealed release archive failed")
	}
	return raw.Bytes(), nil
}

func (c sealedReleaseContext) verifiedArchive() (io.Reader, error) {
	b := bytes.Clone(c.archive)
	if len(b) == 0 || digest(b) != c.archiveSHA256 {
		return nil, errors.New("sealed release archive changed")
	}
	return bytes.NewReader(b), nil
}
