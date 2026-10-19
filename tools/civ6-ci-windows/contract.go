package main

import (
	"bytes"
	contract "cc/civ6-ci-contract"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const repository = contract.Repository
const repositoryID = contract.RepoID

// fixtureHead and fixtureTree are the immutable original PR149 pins. They are
// only accepted by credential-free service fixtures, never by an official hook.
const fixtureHead = contract.OriginalHead
const fixtureTree = contract.OriginalTree

var hex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Plan contains no credentials. The embedded contract.Plan is the shared,
// parent-owned identity (JSON fields are flattened into one document); the
// remaining fields are Windows-local operational parameters.
type Plan struct {
	contract.Plan
	Instance       string `json:"instance"`
	Root           string `json:"root"`
	RunnerVersion  string `json:"runner_version"`
	RunnerDigest   string `json:"runner_digest"`
	ControllerSID  string `json:"controller_sid"`
	Seconds        int    `json:"seconds"`
	CleanupSeconds int    `json:"cleanup_seconds,omitempty"`
	Fixture        string `json:"fixture,omitempty"`
	RuntimeFixture bool   `json:"runtime_fixture,omitempty"`
}

// name is the unique per-job identity: the official runner registration and the
// one-message registration pipe. It never names the SCM service.
func (p Plan) name() string { return "CC-CIW-" + p.Instance }

// The Windows service and its virtual account are one fixed identity for this
// repository (and one for credential-free fixtures), reused by every job in turn.
// This bounds the OS profile/hive to one per identity instead of one per job.
const sharedService = "CC-CIW-civ6-banseiki"
const fixtureService = "CC-CIW-civ6-fixture"

func (p Plan) svc() string {
	if p.Fixture != "" {
		return fixtureService
	}
	return sharedService
}
func (p Plan) runtime() string { return filepath.Join(p.Root, "runtime") }
func (p Plan) runner() string  { return filepath.Join(p.runtime(), "runner") }

// windowsWorkflows are the only workflow/job pairs this helper can serve.
var windowsWorkflows = map[string]string{
	".github/workflows/rust.yml":          "test",
	".github/workflows/native-driver.yml": "native-driver",
	".github/workflows/live-chain.yml":    "live-chain",
}

// requiredLabels is the exact label set of the one ephemeral listener.
func (p Plan) requiredLabels() []string { return contract.Labels(p.Plan) }

func (p Plan) validate() error {
	if !hex32.MatchString(p.Instance) || !filepath.IsAbs(p.Root) || filepath.Clean(p.Root) != p.Root || filepath.Base(p.Root) != "codex-owned-"+p.Instance {
		return errors.New("new safe-delete root and instance required")
	}
	if p.Seconds < 1 || p.Seconds > 14400 || !regexp.MustCompile(`^S-1-5-21-[0-9]+-[0-9]+-[0-9]+-[0-9]+$`).MatchString(p.ControllerSID) {
		return errors.New("finite deadline and controller SID required")
	}
	if p.CleanupSeconds != 0 && (p.CleanupSeconds < 60 || p.CleanupSeconds > 14400) {
		return errors.New("cleanup deadline must be 60..14400 seconds or zero for 1800")
	}
	if p.Fixture != "" {
		if p.Fixture != "normal" && p.Fixture != "cancel" {
			return errors.New("unknown fixture")
		}
		if p.Version != 1 || p.RepoID != repositoryID || p.Head != fixtureHead || p.Tree != fixtureTree {
			return errors.New("credential-free fixture must retain original source pins")
		}
		return nil
	}
	if p.RuntimeFixture {
		return errors.New("runtime fixture cannot be official execution")
	}
	if e := p.Plan.Validate(); e != nil {
		return e
	}
	job, ok := windowsWorkflows[p.Workflow]
	if !ok || p.Event == "dynamic" || p.Job != job || p.OS() != "Windows" || !slices.Equal(p.requiredLabels(), []string{"self-hosted", "Windows", "X64", contract.WindowsLabel}) {
		return errors.New("only the three original Windows jobs are served")
	}
	if p.Head == fixtureHead || p.Tree == fixtureTree {
		// The original unrouted source is the credential-free fixture pin only.
		return errors.New("original unrouted source cannot run officially")
	}
	if !hex64.MatchString(p.RunnerDigest) || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(p.RunnerVersion) {
		return errors.New("official runner pin required")
	}
	return nil
}

func readPlan(root string) (p Plan, err error) {
	b, err := os.ReadFile(filepath.Join(root, "plan.json"))
	if err != nil {
		return p, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&p); err != nil {
		return p, errors.New("invalid plan")
	}
	if d.Decode(new(any)) != io.EOF {
		return p, errors.New("trailing plan data")
	}
	if err = p.validate(); err != nil {
		return p, err
	}
	if p.Root != root {
		return p, errors.New("root mismatch")
	}
	return p, nil
}
func digest(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }
func saveNew(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	c := f.Close()
	return errors.Join(e, c)
}
func boundedRead(r io.Reader, n int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, n+1))
	if e == nil && int64(len(b)) > n {
		e = errors.New("input exceeds limit")
	}
	return b, e
}

// No caller-controlled command, executable, environment, host callback, or
// download URL is accepted by this helper. The listener keeps the three default
// labels and adds only the dedicated one, matching the reviewed runs-on array.
func configureArgs(p Plan) []string {
	return []string{"configure", "--unattended", "--url", "https://github.com/" + repository, "--name", p.name(), "--labels", contract.WindowsLabel, "--ephemeral", "--disableupdate", "--work", "_work"}
}

// checkEvent compares the actual SDK context to the shared contract and then
// adds this helper's own runner identity and PR/branch environment keys.
func checkEvent(p Plan, env map[string]string, raw []byte) error {
	if p.Fixture != "" || p.validate() != nil {
		return errors.New("official hook requires a real pinned plan")
	}
	if e := contract.CheckEvent(p.Plan, env, raw); e != nil {
		return e
	}
	wantHead, wantBase := "", ""
	if p.Event == "pull_request" {
		wantHead, wantBase = p.Branch, "main"
	}
	if env["RUNNER_NAME"] != p.name() || env["GITHUB_HEAD_REF"] != wantHead || env["GITHUB_BASE_REF"] != wantBase {
		return fmt.Errorf("job context rejected: runner or PR refs")
	}
	return nil
}

var ownedRootPattern = regexp.MustCompile(`^[A-Za-z]:\\.*codex-owned-[0-9a-f]{32}$`)

// imageRoot parses a service image path built by ensureService with Windows
// argument rules: <root>\helper.exe service <root>, each argument quoted when it
// contains a space. Anything else is rejected, never guessed.
func imageRoot(image string) (string, error) {
	var toks []string
	var cur strings.Builder
	quoted, has := false, false
	for _, r := range image {
		switch {
		case r == '"':
			quoted, has = !quoted, true
		case r == ' ' && !quoted:
			if has {
				toks = append(toks, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if quoted {
		return "", errors.New("service image path has an unterminated quote")
	}
	if has {
		toks = append(toks, cur.String())
	}
	if len(toks) != 3 || toks[1] != "service" {
		return "", errors.New("service image path is not <helper> service <root>")
	}
	helper, root := toks[0], toks[2]
	i := strings.LastIndex(helper, `\`)
	if i < 0 || !strings.EqualFold(helper[i+1:], "helper.exe") || !strings.EqualFold(helper[:i], root) || !ownedRootPattern.MatchString(root) {
		return "", errors.New("service image path does not name an owned job root")
	}
	return root, nil
}

// previousRootSettled lets the shared identity move to a new job only when the
// previous job's root no longer exists at its path (deleted or quarantined). A
// collected root is not enough: the next job runs as the same SID, and a file
// ACL cannot be proven clean by the two inherited ACE removals alone.
func previousRootSettled(prev, current string) error {
	if prev == "" || strings.EqualFold(filepath.Clean(prev), filepath.Clean(current)) {
		return nil
	}
	_, e := os.Lstat(prev)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return fmt.Errorf("previous job root cannot be inspected; shared identity not reused: %w", e)
	}
	return errors.New("previous job root still exists; delete it (safe-delete) before the shared service identity moves on")
}

func validSHA(s string) bool { return contract.ValidSHA(s) }

func beneath(root, path string) error {
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return errors.New("path must be strictly inside owned root")
	}
	return nil
}
