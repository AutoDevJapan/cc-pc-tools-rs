// Package sourcepacket transfers one fixed GitHub source revision in memory and
// materializes it in a fresh guest root after proving its Git object identities.
package sourcepacket

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	RepoID       int64 = 1154066087
	PR                 = 3613
	HeadRef            = "codex/todo2641-http-response-pseudo-20261015"
	maxArchive         = 128 << 20
	maxExpanded        = 128 << 20
	maxPacket          = 256 << 20
	maxFiles           = 200000
	maxFileBytes       = 64 << 20
	ghPath             = `D:\CC\Tools\codex-jobs\gh_gate.exe`
	workflowPath       = ".github/workflows/ci.yml"
)

var errInvalid = errors.New("source packet rejected")
var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Pin is the caller-selected revision identity. Repository, PR and ref identity
// are fixed by this package; head/tree/workflow digest are explicitly pinned.
type Pin struct {
	HeadSHA        string `json:"head_sha"`
	TreeSHA        string `json:"tree_sha"`
	WorkflowSHA256 string `json:"workflow_sha256"`
}

// TreeEntry is an entry from GitHub's recursive Git tree API.
type TreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// Packet contains source bytes only in memory. Do not log or persist it.
type Packet struct {
	Pin           Pin         `json:"pin"`
	CommitPayload []byte      `json:"commit_payload"`
	Archive       []byte      `json:"archive"`
	ArchiveSHA256 string      `json:"archive_sha256"`
	TreeEntries   []TreeEntry `json:"tree_entries"`
}
type repo struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	Fork     bool   `json:"fork"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Permissions struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}
type pull struct {
	State string `json:"state"`
	Head  struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
		Repo repo   `json:"repo"`
	} `json:"head"`
	Base struct {
		Repo repo `json:"repo"`
	} `json:"base"`
}
type commitResponse struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
	Verification struct {
		Payload   string `json:"payload"`
		Signature string `json:"signature"`
	} `json:"verification"`
}

func validPin(p Pin) bool {
	return hex40.MatchString(p.HeadSHA) && hex40.MatchString(p.TreeSHA) && hex64.MatchString(p.WorkflowSHA256)
}
func gitID(kind string, b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(b))
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func commitTreeID(payload []byte) string {
	line, _, ok := bytes.Cut(payload, []byte{'\n'})
	if !ok || !bytes.HasPrefix(line, []byte("tree ")) || len(line) != 45 {
		return ""
	}
	return string(line[5:])
}
func fixedErr() error { return errInvalid }

// Fetch retrieves exactly the fixed private repository and open PR head. It is
// intentionally not exercised against GitHub by this module's tests.
func Fetch(ctx context.Context, pin Pin) (Packet, error) {
	if !validPin(pin) {
		return Packet{}, fixedErr()
	}
	verify := func() (repo, error) {

		var r repo

		if api(ctx, fmt.Sprintf("repositories/%d", RepoID), &r) != nil || r.ID != RepoID || r.Owner.Login != "AutoDevJapan" || r.FullName != "AutoDevJapan/GoDD-MCP-Alpha" || !r.Private || r.Fork || !r.Permissions.Admin {
			return repo{}, fixedErr()
		}

		var p pull

		if api(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, PR), &p) != nil || p.State != "open" || p.Head.SHA != pin.HeadSHA || p.Head.Ref != HeadRef || p.Head.Repo.ID != RepoID || p.Base.Repo.ID != RepoID || p.Head.Repo.FullName != r.FullName || p.Base.Repo.FullName != r.FullName || p.Head.Repo.Fork {
			return repo{}, fixedErr()
		}

		var ref struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}

		if api(ctx, "repos/"+r.FullName+"/git/ref/heads/"+url.PathEscape(HeadRef), &ref) != nil || ref.Object.SHA != pin.HeadSHA {
			return repo{}, fixedErr()
		}

		return r, nil
	}
	r, err := verify()
	if err != nil {
		return Packet{}, err
	}

	var c commitResponse
	if api(ctx, "repos/"+r.FullName+"/git/commits/"+pin.HeadSHA, &c) != nil || c.SHA != pin.HeadSHA || c.Tree.SHA != pin.TreeSHA || c.Verification.Signature != "" || gitID("commit", []byte(c.Verification.Payload)) != pin.HeadSHA {
		return Packet{}, fixedErr()
	}

	var t struct {
		Truncated bool        `json:"truncated"`
		Tree      []TreeEntry `json:"tree"`
	}
	if api(ctx, "repos/"+r.FullName+"/git/trees/"+pin.TreeSHA+"?recursive=1", &t) != nil || t.Truncated || len(t.Tree) > maxFiles {
		return Packet{}, fixedErr()
	}
	workflow, err := readAPI(ctx, "repos/"+r.FullName+"/contents/"+workflowPath+"?ref="+pin.HeadSHA)
	if err != nil {
		return Packet{}, fixedErr()
	}

	var content struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if json.Unmarshal(workflow, &content) != nil || content.Encoding != "base64" {
		return Packet{}, fixedErr()
	}
	wf, err := decodeBase64(content.Content)
	if err != nil || digest(wf) != pin.WorkflowSHA256 {
		return Packet{}, fixedErr()
	}
	workflowFound := false
	for _, entry := range t.Tree {
		if entry.Path == workflowPath && entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755") && gitID("blob", wf) == entry.SHA {
			workflowFound = true
		}
	}
	if !workflowFound {
		return Packet{}, fixedErr()
	}
	archive, err := runGH(ctx, "repos/"+r.FullName+"/tarball/"+pin.HeadSHA)
	if err != nil || len(archive) > maxArchive {
		return Packet{}, fixedErr()
	}
	if _, err = verify(); err != nil {
		return Packet{}, err
	}
	p := Packet{Pin: pin, CommitPayload: []byte(c.Verification.Payload), Archive: archive, ArchiveSHA256: digest(archive), TreeEntries: t.Tree}
	if validatePacket(p) != nil {
		return Packet{}, fixedErr()
	}
	return p, nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
}
func runGH(ctx context.Context, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, ghPath, append([]string{"api"}, args...)...)
	configureProcess(c)
	c.WaitDelay = 3 * time.Second

	var out bounded
	out.limit = maxArchive
	c.Stdout = &out
	c.Stderr = io.Discard
	if err := c.Run(); err != nil {
		return nil, fixedErr()
	}
	return out.Bytes(), nil
}

type bounded struct {
	bytes.Buffer
	limit int
}

func (b *bounded) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("limit")
	}
	return b.Buffer.Write(p)
}
func api(ctx context.Context, endpoint string, v any) error {
	b, e := readAPI(ctx, endpoint)
	if e != nil {
		return e
	}
	if json.Unmarshal(b, v) != nil {
		return fixedErr()
	}
	return nil
}
func readAPI(ctx context.Context, endpoint string) ([]byte, error) {
	b, e := runGH(ctx, endpoint)
	if e != nil {
		return nil, e
	}
	return b, nil
}

// Encode returns a strict, bounded JSON envelope for RAM/pipe transport.
func Encode(p Packet) ([]byte, error) {
	if err := validatePacket(p); err != nil {
		return nil, fixedErr()
	}
	b, e := json.Marshal(p)
	if e != nil || len(b) > maxPacket {
		return nil, fixedErr()
	}
	return b, nil
}

// Decode rejects duplicate/unknown fields and requires the caller's exact pin.
func Decode(raw []byte, expectedPin Pin) (Packet, error) {
	if len(raw) == 0 || len(raw) > maxPacket || !validPin(expectedPin) || hasDuplicateJSONKeys(raw) {
		return Packet{}, fixedErr()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var p Packet
	if d.Decode(&p) != nil {
		return Packet{}, fixedErr()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Packet{}, fixedErr()
	}
	if p.Pin != expectedPin || validatePacket(p) != nil {
		return Packet{}, fixedErr()
	}
	return p, nil
}
func validatePacket(p Packet) error {
	if !validPin(p.Pin) || len(p.Archive) == 0 || len(p.Archive) > maxArchive || digest(p.Archive) != p.ArchiveSHA256 || len(p.CommitPayload) == 0 || gitID("commit", p.CommitPayload) != p.Pin.HeadSHA || commitTreeID(p.CommitPayload) != p.Pin.TreeSHA || len(p.TreeEntries) > maxFiles {
		return fixedErr()
	}
	if err := validateEntries(p.TreeEntries); err != nil {
		return err
	}
	files, err := archiveFiles(p.Archive)
	if err != nil {
		return err
	}
	workflow, ok := files.data[workflowPath]
	if !ok || digest(workflow) != p.Pin.WorkflowSHA256 {
		return fixedErr()
	}
	workflowFound := false
	regular := make(map[string]string)
	for _, entry := range p.TreeEntries {
		if entry.Type == "blob" {
			regular[entry.Path] = entry.SHA
		}
		if entry.Path == workflowPath {
			workflowFound = entry.Type == "blob" && gitID("blob", workflow) == entry.SHA
		}
	}
	if !workflowFound || len(regular) != len(files.data) {
		return fixedErr()
	}
	for name, content := range files.data {
		if regular[name] != gitID("blob", content) {
			return fixedErr()
		}
	}
	return nil
}
func validateEntries(es []TreeEntry) error {
	seen := make(map[string]bool, len(es))
	folded := make(map[string]bool, len(es))
	for _, e := range es {
		key := strings.ToLower(e.Path)
		if !safePath(e.Path) || seen[e.Path] || folded[key] || !hex40.MatchString(e.SHA) {
			return fixedErr()
		}
		seen[e.Path] = true
		folded[key] = true

		if e.Type == "tree" && e.Mode == "040000" {
			continue
		}
		if e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755") {
			continue
		}
		if e.Type == "commit" && e.Mode == "160000" {
			continue
		}
		return fixedErr()
	}
	return nil
}
func safePath(s string) bool {
	if s == "" || strings.ContainsAny(s, "\\\x00\r\n:") || strings.HasPrefix(s, "/") || path.Clean(s) != s {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		upper := strings.ToUpper(part)
		if part == "." || part == ".." || strings.EqualFold(part, ".git") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, "<>\"|?*") || reservedWindows(upper) {
			return false
		}
	}
	return true
}
func reservedWindows(s string) bool {
	base := strings.TrimSuffix(s, ".")
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}

// Receipt contains only non-identifying integrity facts.
type Receipt struct {
	RepoID        int64  `json:"repo_id"`
	PR            int    `json:"pr"`
	HeadSHA       string `json:"head_sha"`
	TreeSHA       string `json:"tree_sha"`
	ArchiveSHA256 string `json:"archive_sha256"`
	BlobCount     int    `json:"blob_count"`
}

// Materialize is guest-side only. root must be a fresh, absolute directory; the
// caller must provide an isolated Linux guest with Git and no credentials. This
// function never fetches submodules or runs source-controlled commands/hooks.
func Materialize(ctx context.Context, p Packet, expectedPin Pin, freshGuestRoot string) (Receipt, error) {
	if p.Pin != expectedPin || validatePacket(p) != nil || !filepath.IsAbs(freshGuestRoot) {
		return Receipt{}, fixedErr()
	}
	if err := os.MkdirAll(filepath.Dir(freshGuestRoot), 0700); err != nil {
		return Receipt{}, fixedErr()
	}
	if err := os.Mkdir(freshGuestRoot, 0700); err != nil {
		return Receipt{}, fixedErr()
	}
	files, err := archiveFiles(p.Archive)
	if err != nil {
		return Receipt{}, fixedErr()
	}
	_ = files.root
	allowed := make(map[string]bool)
	for _, e := range p.TreeEntries {
		if e.Type == "blob" && e.Mode != "120000" {
			allowed[e.Path] = true
		}
	}
	if len(allowed) != len(files.data) {
		return Receipt{}, fixedErr()
	}
	for name := range files.data {
		if !allowed[name] {
			return Receipt{}, fixedErr()
		}
	}
	git := func(stdin []byte, args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, "git", args...)
		configureProcess(c)
		c.Dir = freshGuestRoot
		c.Env = cleanGitEnv(os.Environ())
		if stdin != nil {
			c.Stdin = bytes.NewReader(stdin)
		}
		c.Stderr = io.Discard
		c.WaitDelay = 3 * time.Second
		var out bounded
		out.limit = 1 << 20
		c.Stdout = &out
		if e := c.Run(); e != nil {
			return nil, fixedErr()
		}
		return out.Bytes(), nil
	}
	if _, err = git(nil, "init", "--quiet", "--template="); err != nil {
		return Receipt{}, err
	}
	configNames, err := git(nil, "config", "--local", "--name-only", "--list")
	if err != nil {
		return Receipt{}, err
	}
	for _, key := range strings.Split(strings.ToLower(string(configNames)), "\n") {
		if strings.HasPrefix(key, "remote.") || strings.HasPrefix(key, "credential.") || strings.HasPrefix(key, "filter.") || strings.HasPrefix(key, "submodule.") || key == "core.hookspath" {
			return Receipt{}, fixedErr()
		}
	}
	blobs := map[string]TreeEntry{}
	var index bytes.Buffer
	for _, e := range p.TreeEntries {
		if e.Type == "tree" {
			continue
		}
		if e.Mode == "160000" {
			fmt.Fprintf(&index, "%s %s\t%s%c", e.Mode, e.SHA, e.Path, byte(0))
			continue
		}
		if e.Type != "blob" || e.Mode == "120000" {
			return Receipt{}, fixedErr()
		}
		content, ok := files.data[e.Path]
		if !ok || len(content) > maxFileBytes {
			return Receipt{}, fixedErr()
		}
		oid, er := git(content, "hash-object", "--no-filters", "-w", "--stdin")
		if er != nil || strings.TrimSpace(string(oid)) != e.SHA {
			return Receipt{}, fixedErr()
		}
		target := filepath.Join(freshGuestRoot, filepath.FromSlash(e.Path))
		if !within(freshGuestRoot, target) {
			return Receipt{}, fixedErr()
		}
		if er = os.MkdirAll(filepath.Dir(target), 0700); er != nil {
			return Receipt{}, fixedErr()
		}
		f, er := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if er != nil {
			return Receipt{}, fixedErr()
		}
		_, er = f.Write(content)
		ce := f.Close()
		if er != nil || ce != nil {
			return Receipt{}, fixedErr()
		}
		mode := os.FileMode(0644)
		if e.Mode == "100755" {
			mode = 0755
		}
		if er = os.Chmod(target, mode); er != nil {
			return Receipt{}, fixedErr()
		}
		blobs[e.Path] = e
		fmt.Fprintf(&index, "%s %s\t%s%c", e.Mode, e.SHA, e.Path, byte(0))
	}
	if _, err = git(index.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return Receipt{}, err
	}
	tree, err := git(nil, "write-tree")
	if err != nil || strings.TrimSpace(string(tree)) != expectedPin.TreeSHA {
		return Receipt{}, fixedErr()
	}
	commit, err := git(p.CommitPayload, "hash-object", "-t", "commit", "-w", "--stdin")
	if err != nil || strings.TrimSpace(string(commit)) != expectedPin.HeadSHA {
		return Receipt{}, fixedErr()
	}
	if _, err = git(nil, "update-ref", "--no-deref", "HEAD", expectedPin.HeadSHA); err != nil {
		return Receipt{}, err
	}
	head, err := git(nil, "rev-parse", "HEAD^{tree}")
	if err != nil || strings.TrimSpace(string(head)) != expectedPin.TreeSHA {
		return Receipt{}, fixedErr()
	}
	return Receipt{RepoID: RepoID, PR: PR, HeadSHA: expectedPin.HeadSHA, TreeSHA: expectedPin.TreeSHA, ArchiveSHA256: p.ArchiveSHA256, BlobCount: len(blobs)}, nil
}
func cleanGitEnv(env []string) []string {
	global := "NUL"
	if runtime.GOOS != "windows" {
		global = "/dev/null"
	}
	out := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + global, "GIT_TERMINAL_PROMPT=0"}
	for _, v := range env {
		k, _, _ := strings.Cut(v, "=")
		u := strings.ToUpper(k)
		if strings.HasPrefix(u, "GIT_CONFIG_") || u == "GIT_DIR" || u == "GIT_WORK_TREE" || u == "GIT_INDEX_FILE" || u == "GIT_OBJECT_DIRECTORY" || u == "GIT_ALTERNATE_OBJECT_DIRECTORIES" || u == "GIT_ASKPASS" || u == "SSH_ASKPASS" {
			continue
		}
		out = append(out, v)
	}
	return out
}
func within(root, target string) bool {
	r, e := filepath.Rel(root, target)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}

type archiveData struct {
	root string
	data map[string][]byte
}

func archiveFiles(raw []byte) (archiveData, error) {
	if len(raw) == 0 || len(raw) > maxArchive {
		return archiveData{}, fixedErr()
	}
	compressed := bytes.NewReader(raw)
	gz, e := gzip.NewReader(compressed)
	if e != nil {
		return archiveData{}, fixedErr()
	}
	gz.Multistream(false)
	limited := &io.LimitedReader{R: gz, N: maxExpanded + 1}
	tr := tar.NewReader(limited)
	files := map[string][]byte{}
	foldedFiles := map[string]bool{}
	archivePaths := map[string]bool{}
	common := ""
	count := 0
	for {
		h, er := tr.Next()
		if er == io.EOF {
			break
		}
		if er != nil {
			return archiveData{}, fixedErr()
		}
		count++
		if count > maxFiles {
			return archiveData{}, fixedErr()
		}
		name := strings.TrimSuffix(h.Name, "/")
		parts := strings.Split(name, "/")
		if len(parts) < 1 || !safePath(parts[0]) {
			return archiveData{}, fixedErr()
		}
		if common == "" {
			common = parts[0]
		} else if common != parts[0] {
			return archiveData{}, fixedErr()
		}
		rel := ""
		if len(parts) > 1 {
			rel = strings.Join(parts[1:], "/")
			if !safePath(rel) {
				return archiveData{}, fixedErr()
			}
		}
		entryPath := strings.ToLower(rel)
		if archivePaths[entryPath] {
			return archiveData{}, fixedErr()
		}
		archivePaths[entryPath] = true
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if rel == "" || h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA || h.Size < 0 || h.Size > maxFileBytes {
			return archiveData{}, fixedErr()
		}
		if _, exists := files[rel]; exists {
			return archiveData{}, fixedErr()
		}
		folded := strings.ToLower(rel)
		if foldedFiles[folded] {
			return archiveData{}, fixedErr()
		}
		foldedFiles[folded] = true
		b, er := io.ReadAll(io.LimitReader(tr, maxFileBytes+1))
		if er != nil || int64(len(b)) != h.Size || len(b) > maxFileBytes {
			return archiveData{}, fixedErr()
		}
		files[rel] = b
	}
	_, drainErr := io.Copy(io.Discard, gz)
	if e = gz.Close(); e != nil || drainErr != nil || len(files) == 0 || limited.N == 0 || compressed.Len() != 0 {
		return archiveData{}, fixedErr()
	}
	return archiveData{root: common, data: files}, nil
}

// hasDuplicateJSONKeys parses structure recursively without trusting map decode.
func hasDuplicateJSONKeys(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var val func() bool
	val = func() bool {
		t, e := d.Token()
		if e != nil {
			return true
		}
		del, ok := t.(json.Delim)
		if !ok {
			return false
		}
		switch del {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return true
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return true
				}
				seen[s] = true
				if val() {
					return true
				}
			}
			_, e = d.Token()
			return e != nil
		case '[':
			for d.More() {
				if val() {
					return true
				}
			}
			_, e = d.Token()
			return e != nil
		default:
			return true
		}
	}
	if val() {
		return true
	}
	_, e := d.Token()
	return e != io.EOF
}
