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
	Product  string `json:"product,omitempty"`
	Workflow string `json:"workflow,omitempty"`
	// Only the finite Press PR event may select its actual two-parent merge
	// commit. HeadSHA remains the physical execution commit, never an alias for
	// the PR head; these two extra pins bind the original candidate and main ref.
	PullHeadSHA    string `json:"pull_head_sha,omitempty"`
	MergeBaseSHA   string `json:"merge_base_sha,omitempty"`
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
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Head           struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
		Repo repo   `json:"repo"`
	} `json:"head"`
	Base struct {
		SHA  string `json:"sha"`
		Repo repo   `json:"repo"`
	} `json:"base"`
}
type commitResponse struct {
	SHA     string `json:"sha"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
	Author, Committer struct{ Name, Email, Date string }
	Message           string `json:"message"`
	Tree              struct {
		SHA string `json:"sha"`
	} `json:"tree"`
	Verification struct {
		Payload   string `json:"payload"`
		Signature string `json:"signature"`
	} `json:"verification"`
}

func validPin(p Pin) bool {
	_, ok := p.Profile()
	return ok && hex40.MatchString(p.HeadSHA) && hex40.MatchString(p.TreeSHA) && hex64.MatchString(p.WorkflowSHA256)
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
func Fetch(ctx context.Context, pin Pin) (result Packet, retErr error) {
	stage := "pin"
	defer func() {
		if retErr != nil {
			retErr = fmt.Errorf("fixed source fetch rejected at %s", stage)
		}
	}()
	if !validPin(pin) {
		return Packet{}, fixedErr()
	}
	profile, _ := pin.Profile()
	verify := func() (repo, error) {
		r, detail, err := verifySourceIdentity(ctx, pin, api)
		if err != nil {
			stage += "_" + detail
		}
		return r, err
	}
	stage = "identity"
	r, err := verify()
	if err != nil {
		return Packet{}, err
	}

	var c commitResponse
	stage = "commit"
	if err := api(ctx, "repos/"+r.FullName+"/git/commits/"+pin.HeadSHA, &c); err != nil {
		stage = "commit_api_" + sourceAPICode(err)
		return Packet{}, fixedErr()
	}
	if c.SHA != pin.HeadSHA || c.Tree.SHA != pin.TreeSHA {
		stage = "commit_identity"
		return Packet{}, fixedErr()
	}
	payload, err := originalPayload(c, pin.HeadSHA)
	if err != nil {
		stage = "commit_payload"
		return Packet{}, fixedErr()
	}

	var t struct {
		Truncated bool        `json:"truncated"`
		Tree      []TreeEntry `json:"tree"`
	}
	stage = "tree"
	if api(ctx, "repos/"+r.FullName+"/git/trees/"+pin.TreeSHA+"?recursive=1", &t) != nil || t.Truncated || len(t.Tree) > maxFiles {
		return Packet{}, fixedErr()
	}
	stage = "workflow"
	workflow, err := readAPI(ctx, "repos/"+r.FullName+"/contents/"+profile.Workflow+"?ref="+pin.HeadSHA)
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
		if entry.Path == profile.Workflow && entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755") && gitID("blob", wf) == entry.SHA {
			workflowFound = true
		}
	}
	if !workflowFound {
		return Packet{}, fixedErr()
	}
	stage = "archive"
	archive, err := runGH(ctx, "repos/"+r.FullName+"/tarball/"+pin.HeadSHA)
	if err != nil || len(archive) > maxArchive {
		return Packet{}, fixedErr()
	}
	stage = "identity_after"
	if _, err = verify(); err != nil {
		return Packet{}, err
	}
	p := Packet{Pin: pin, CommitPayload: payload, Archive: archive, ArchiveSHA256: digest(archive), TreeEntries: t.Tree}
	stage = "packet"
	if validatePacket(p) != nil {
		return Packet{}, fixedErr()
	}
	return p, nil
}

// Return only a fixed diagnostic vocabulary. API errors and identity values
// never become error text, and a rejection still prevents all later reads.
func verifySourceIdentity(ctx context.Context, pin Pin, call func(context.Context, string, any) error) (repo, string, error) {
	profile, ok := pin.Profile()
	if !ok {
		return repo{}, "profile", fixedErr()
	}
	var r repo
	if err := call(ctx, fmt.Sprintf("repositories/%d", profile.RepoID), &r); err != nil {
		return repo{}, "repository_api_" + sourceAPICode(err), fixedErr()
	}
	if r.ID != profile.RepoID || r.Owner.Login != "AutoDevJapan" || r.FullName != profile.Repository || !r.Private || r.Fork || !r.Permissions.Admin {
		return repo{}, "repository_identity", fixedErr()
	}
	var p pull
	if err := call(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, profile.PR), &p); err != nil {
		return repo{}, "pull_api_" + sourceAPICode(err), fixedErr()
	}
	if p.State != "open" || p.Head.SHA != pin.CandidateSHA() || p.Head.Ref != profile.HeadRef || p.Head.Repo.ID != profile.RepoID || p.Base.Repo.ID != profile.RepoID || p.Head.Repo.FullName != r.FullName || p.Base.Repo.FullName != r.FullName || p.Head.Repo.Fork || (profile.BaseSHA != "" && (!p.Draft || p.Base.SHA != profile.BaseSHA || p.Base.Repo.Fork)) || (pin.PullHeadSHA != "" && p.MergeCommitSHA != pin.HeadSHA) {
		return repo{}, "pull_identity", fixedErr()
	}
	var ref identityRef
	if err := call(ctx, "repos/"+r.FullName+"/git/ref/heads/"+url.PathEscape(profile.HeadRef), &ref); err != nil {
		return repo{}, "ref_api_" + sourceAPICode(err), fixedErr()
	}
	if ref.Object.SHA != pin.CandidateSHA() {
		return repo{}, "ref_identity", fixedErr()
	}
	if pin.PullHeadSHA != "" {
		for _, check := range []struct{ ref, sha, stage string }{
			{"heads/main", pin.MergeBaseSHA, "merge_base"},
			{"pull/175/merge", pin.HeadSHA, "merge_ref"},
		} {
			var actual identityRef
			if err := call(ctx, "repos/"+r.FullName+"/git/ref/"+check.ref, &actual); err != nil {
				return repo{}, check.stage + "_api_" + sourceAPICode(err), fixedErr()
			}
			if actual.Object.SHA != check.sha {
				return repo{}, check.stage + "_identity", fixedErr()
			}
		}
	}
	return r, "", nil
}

type identityRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

// GitHub returns null verification.payload for unsigned commits. Reconstruct
// only candidates whose real Git object ID equals the explicitly pinned SHA.
// No substitute commit or synthetic HEAD can pass this check.
func originalPayload(c commitResponse, expected string) ([]byte, error) {
	if c.Verification.Signature == "" && gitID("commit", []byte(c.Verification.Payload)) == expected {
		return []byte(c.Verification.Payload), nil
	}
	// Signed API payloads preserve the original timezone and exact message
	// bytes. Reinsert only their detached signature, accepting a candidate only
	// if its real Git object ID equals the explicitly pinned execution commit.
	// REST's normalized author dates cannot reconstruct arbitrary original zones.
	if c.Verification.Signature != "" && len(c.Verification.Payload) > 0 {
		header, body, ok := strings.Cut(c.Verification.Payload, "\n\n")
		if ok {
			for _, signature := range []string{c.Verification.Signature, strings.TrimSuffix(c.Verification.Signature, "\n"), strings.TrimRight(c.Verification.Signature, "\n")} {
				gpg := "gpgsig " + strings.ReplaceAll(signature, "\n", "\n ")
				candidate := []byte(header + "\n" + gpg + "\n\n" + body)
				if gitID("commit", candidate) == expected {
					return candidate, nil
				}
			}
		}
	}
	a, e := time.Parse(time.RFC3339, c.Author.Date)
	if e != nil {
		return nil, fixedErr()
	}
	b, e := time.Parse(time.RFC3339, c.Committer.Date)
	if e != nil {
		return nil, fixedErr()
	}
	prefix := "tree " + c.Tree.SHA + "\n"
	for _, p := range c.Parents {
		prefix += "parent " + p.SHA + "\n"
	}
	for _, za := range []string{"+0000", "+0900"} {
		for _, zb := range []string{"+0000", "+0900"} {
			for _, tail := range []string{"", "\n"} {
				body := prefix + fmt.Sprintf("author %s <%s> %d %s\ncommitter %s <%s> %d %s\n", c.Author.Name, c.Author.Email, a.Unix(), za, c.Committer.Name, c.Committer.Email, b.Unix(), zb)
				if c.Verification.Signature != "" {
					body += "gpgsig " + strings.ReplaceAll(strings.TrimSuffix(c.Verification.Signature, "\n"), "\n", "\n ") + "\n"
				}
				body += "\n" + c.Message + tail
				if gitID("commit", []byte(body)) == expected {
					return []byte(body), nil
				}
			}
		}
	}
	return nil, fixedErr()
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
	var diagnostic bounded
	diagnostic.limit = 64 << 10
	c.Stderr = &diagnostic
	err := c.Run()
	if out.overflow || diagnostic.overflow {
		return nil, &sourceAPIError{"capture"}
	}
	if err != nil {
		code := "start"
		var exit *exec.ExitError
		switch {
		case ctx.Err() != nil:
			code = "context"
		case errors.Is(err, exec.ErrWaitDelay):
			code = "drain"
		case errors.As(err, &exit):
			code = sourceExitCode(string(diagnostic.Bytes()))
		}
		return nil, &sourceAPIError{code}
	}
	return out.Bytes(), nil
}

type bounded struct {
	data     bytes.Buffer
	limit    int
	overflow bool
}

func (b *bounded) Len() int      { return b.data.Len() }
func (b *bounded) Bytes() []byte { return b.data.Bytes() }

func (b *bounded) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		b.overflow = true
		return 0, errors.New("limit")
	}
	return b.data.Write(p)
}
func api(ctx context.Context, endpoint string, v any) error {
	b, e := readAPI(ctx, endpoint)
	if e != nil {
		return e
	}
	if json.Unmarshal(b, v) != nil {
		return &sourceAPIError{"json"}
	}
	return nil
}

type sourceAPIError struct{ code string }

func (*sourceAPIError) Error() string { return "source API rejected" }

func sourceAPICode(err error) string {
	var e *sourceAPIError
	if errors.As(err, &e) && e != nil {
		switch e.code {
		case "start", "context", "drain", "exit", "json", "capture", "denied", "transport":
			return e.code
		}
	}
	return "operation"
}

// Only complete stderr from a nonzero exit is inspected in RAM. Never return
// an arbitrary substring, status body, endpoint, or authentication value.
func sourceExitCode(stderr string) string {
	s := strings.ToLower(stderr)
	for _, marker := range []string{"denied", "rejected", "forbidden", "policy", "http 401", "http 403", "authentication", "gh auth login"} {
		if strings.Contains(s, marker) {
			return "denied"
		}
	}
	for _, marker := range []string{"tls handshake timeout", "i/o timeout", "connection reset by peer", "temporary failure in name resolution", "no such host", "unexpected eof", "http 502", "http 503", "http 504"} {
		if strings.Contains(s, marker) {
			return "transport"
		}
	}
	return "exit"
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
	if !validPin(p.Pin) || len(p.Archive) == 0 || len(p.Archive) > maxArchive || digest(p.Archive) != p.ArchiveSHA256 || len(p.CommitPayload) == 0 || gitID("commit", p.CommitPayload) != p.Pin.HeadSHA || commitTreeID(p.CommitPayload) != p.Pin.TreeSHA || !mergeParentsMatch(p.Pin, p.CommitPayload) || len(p.TreeEntries) > maxFiles {
		return fixedErr()
	}
	profile, _ := p.Pin.Profile()
	if err := validateEntries(p.TreeEntries); err != nil {
		return err
	}
	files, err := archiveFiles(p.Archive)
	if err != nil {
		return err
	}
	workflow, ok := files.data[profile.Workflow]
	if !ok || digest(workflow) != p.Pin.WorkflowSHA256 {
		return fixedErr()
	}
	workflowFound := false
	regular := make(map[string]string)
	for _, entry := range p.TreeEntries {
		if entry.Type == "blob" {
			regular[entry.Path] = entry.SHA
		}
		if entry.Path == profile.Workflow {
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
	RepoID                int64  `json:"repo_id"`
	PR                    int    `json:"pr"`
	HeadSHA               string `json:"head_sha"`
	TreeSHA               string `json:"tree_sha"`
	ArchiveSHA256         string `json:"archive_sha256"`
	BlobCount             int    `json:"blob_count"`
	PhysicalBlobsVerified bool   `json:"physical_blobs_verified"`
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
	proof, err := VerifyCheckout(ctx, expectedPin, p.TreeEntries, freshGuestRoot)
	if err != nil {
		return Receipt{}, err
	}
	proof.ArchiveSHA256 = p.ArchiveSHA256
	return proof, nil
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
		if strings.HasPrefix(u, "GIT_") || u == "SSH_ASKPASS" {
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
		// git archive carries a global PAX commit comment before its root.
		// This metadata is not a filesystem member or the common root.
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
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
	_, drainErr := io.Copy(io.Discard, limited)
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
