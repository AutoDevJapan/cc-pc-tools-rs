// Product CI is independent of the resident CC runner and its admission policy.
// Repository names exist only in RAM. Public receipts use opaque numeric IDs.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const gh = `D:\CC\Tools\codex-jobs\gh_gate.exe`
const docker = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`

type candidate struct {
	ID  int64
	PR  int
	SHA string
}

var candidates = []candidate{
	{1285649124, 1002, "ca3ffe285ca51c6d49e6797fe589566a6bd0a416"},
	{1392152644, 148, "74f023c0ee4b460f05f87dc3db5ceaf75e999a8a"},
	{1154066087, 3613, "c53dc14f1b49f3d7d16a604f29b120de3ce8163d"},
	{1386702183, 175, "a39a2b1833b3c24125a7b48d5a036376d94c662a"},
}

type repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Private, Fork bool
	Owner         struct{ Login string }
	Permissions   struct{ Admin bool }
}
type pull struct {
	State string
	Head  struct {
		SHA, Ref string
		Repo     repository
	}
	Base struct{ Repo repository }
}
type api func(context.Context, string, any) error

func cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	if name == docker {
		args = append([]string{"--host", "npipe:////./pipe/dockerDesktopLinuxEngine"}, args...)
	}
	c := exec.CommandContext(ctx, name, args...)
	hidden(c)
	c.WaitDelay = 5 * time.Second
	if name == docker {
		c.Env = localDockerEnv(os.Environ())
	}
	return c
}

func localDockerEnv(env []string) []string {
	var out []string
	for _, s := range env {
		key, _, _ := strings.Cut(s, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "DOCKER_") {
			out = append(out, s)
		}
	}
	return out
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("response size exceeded")
	}
	return b.Buffer.Write(p)
}
func boundedOutput(c *exec.Cmd, limit int) ([]byte, error) {
	b := boundedBuffer{limit: limit}
	c.Stdout = &b
	if e := c.Run(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}

// Command errors deliberately omit argv and stderr: both can identify a client.
func get(ctx context.Context, endpoint string, value any) error {
	b, e := boundedOutput(cmd(ctx, gh, "api", endpoint), 16<<20)
	if e != nil {
		return errors.New("GitHub read failed")
	}
	if json.Unmarshal(b, value) != nil {
		return errors.New("GitHub response invalid")
	}
	return nil
}
func verify(ctx context.Context, c candidate, read api) (repository, error) {
	var r repository
	if e := read(ctx, fmt.Sprintf("repositories/%d", c.ID), &r); e != nil {
		return r, e
	}
	if r.ID != c.ID || !r.Private || r.Fork || !r.Permissions.Admin ||
		(r.Owner.Login != "AutoDevJapan" && r.Owner.Login != "BM1314" && r.Owner.Login != "mashfromband") ||
		!strings.HasPrefix(r.FullName, r.Owner.Login+"/") || strings.Count(r.FullName, "/") != 1 {
		return repository{}, errors.New("repository identity rejected")
	}
	var p pull
	if e := read(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, c.PR), &p); e != nil {
		return repository{}, e
	}
	if p.State != "open" || p.Head.SHA != c.SHA || p.Head.Repo.ID != c.ID || p.Base.Repo.ID != c.ID ||
		p.Head.Repo.FullName != r.FullName || p.Base.Repo.FullName != r.FullName || p.Head.Repo.Fork {
		return repository{}, errors.New("fixed same-repository PR head rejected")
	}
	var ref struct{ Object struct{ SHA string } }
	// The PR API and branch API must agree; the fixed revision never follows a ref.
	if e := read(ctx, "repos/"+r.FullName+"/git/ref/heads/"+url.PathEscape(p.Head.Ref), &ref); e != nil {
		return repository{}, e
	}
	if ref.Object.SHA != c.SHA {
		return repository{}, errors.New("branch head changed")
	}
	return r, nil
}

type receipt struct {
	RepoID                   int64     `json:"repo_id"`
	PR                       int       `json:"pr"`
	SourceSHA                string    `json:"source_sha"`
	SourceTree               string    `json:"source_tree,omitempty"`
	SourceTreeVerified       bool      `json:"source_tree_verified"`
	Stage                    string    `json:"stage"`
	At                       time.Time `json:"at"`
	ArchiveSHA               string    `json:"archive_sha256,omitempty"`
	LogSHA                   string    `json:"log_sha256,omitempty"`
	LogBytes                 int64     `json:"log_bytes,omitempty"`
	Exit                     *int      `json:"exit,omitempty"`
	ContainerID              string    `json:"container_id,omitempty"`
	TreeGone                 bool      `json:"tree_gone"`
	ContainerRemoved         bool      `json:"container_removed"`
	IsolationVerified        bool      `json:"isolation_verified"`
	Image                    string    `json:"image,omitempty"`
	AllRequiredChecksSuccess bool      `json:"all_required_checks_success"`
	DependencySHA            string    `json:"dependency_sha512,omitempty"`
	SelectedFailedScripts    int       `json:"selected_failed_scripts,omitempty"`
	LockReleased             bool      `json:"lock_released"`
	Failure                  string    `json:"failure,omitempty"`
	Scope                    string    `json:"scope"`
	ContainerName            string    `json:"container_name,omitempty"`
	CreationUncertain        bool      `json:"creation_uncertain"`
}

func writeReceipt(dir string, r receipt) error {
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, "receipt-*.private.tmp")
	if e != nil {
		return e
	}
	temp := f.Name()
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, filepath.Join(dir, fmt.Sprintf("repo-%d-result.private.json", r.RepoID)))
}

// Convert GitHub's fixed archive into an opaque-root archive before Docker stdin.
// No file from the repository is unpacked on the owner's host.
func opaqueArchive(b []byte) ([]byte, error) {
	gz, e := gzip.NewReader(bytes.NewReader(b))
	if e != nil {
		return nil, errors.New("source archive invalid")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	var total int64
	root := ""
	files := 0
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("source archive invalid")
		}
		// GitHub archives begin with a global PAX metadata record. It is not
		// part of the source root or an extracted filesystem entry.
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if strings.ContainsAny(h.Name, "\\\x00") || strings.HasPrefix(h.Name, "/") {
			return nil, errors.New("source path rejected")
		}
		parts := strings.SplitN(h.Name, "/", 2)
		if root == "" {
			root = parts[0]
		}
		if root == "" || parts[0] != root {
			return nil, errors.New("source roots rejected")
		}
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		name := strings.TrimSuffix(parts[1], "/")
		if name == "" || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return nil, errors.New("source path rejected")
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return nil, errors.New("source links or special files rejected")
		}
		if h.Size < 0 || h.Size > 64<<20 {
			return nil, errors.New("source size rejected")
		}
		total += h.Size
		files++
		if total > 512<<20 || files > 100000 {
			return nil, errors.New("source size rejected")
		}
		h.Name = name
		h.Uid = 1001
		h.Gid = 1001
		h.Uname = ""
		h.Gname = ""
		h.PAXRecords = nil
		h.Xattrs = nil
		h.Mode &= 0777 // Never transfer setuid/setgid/sticky bits.
		if e = tw.WriteHeader(h); e != nil {
			return nil, e
		}
		if _, e = io.CopyN(tw, tr, h.Size); e != nil {
			return nil, errors.New("source archive truncated")
		}
	}
	if files == 0 {
		return nil, errors.New("empty source archive")
	}
	if e = tw.Close(); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// The first independent acceptance step. This does not replace workflow checks,
// CodeQL, Windows/Lua, services or native tests, and never writes success statuses.
func nodeContracts(ctx context.Context, c candidate, dir, image, previousLog string) (ret error) {
	if c.ID != 1285649124 {
		return errors.New("this acceptance step is only approved for the first candidate")
	}
	if e := nodeImage(image); e != nil {
		return e
	}
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	rec := receipt{RepoID: c.ID, PR: c.PR, SourceSHA: c.SHA, Stage: "identity-verified", At: time.Now().UTC()}
	rec.Image = image
	rec.Scope = "all-scripts"
	reservation, e := os.OpenFile(filepath.Join(dir, fmt.Sprintf("repo-%d-result.private.json", c.ID)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("receipt destination already used or unavailable")
	}
	if e = reservation.Close(); e != nil {
		return errors.New("receipt reservation failed")
	}
	defer func() {
		if ret != nil {
			rec.Failure = ret.Error()
		}
		if e := writeReceipt(dir, rec); e != nil {
			ret = errors.Join(ret, errors.New("final receipt preservation failed"))
		}
	}()
	var selected []string
	if previousLog != "" {
		rec.Scope = "failed-subset"
		b, e := previousFailure(previousLog, c)
		if e != nil {
			return errors.New("previous failure evidence unavailable")
		}
		selected, e = failedScripts(b)
		if e != nil {
			return e
		}
		rec.SelectedFailedScripts = len(selected)
	}
	var commit struct{ Tree struct{ SHA string } }
	if e = get(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil {
		return e
	}
	rec.SourceTree = commit.Tree.SHA
	raw, e := boundedOutput(cmd(ctx, gh, "api", "repos/"+r.FullName+"/tarball/"+c.SHA), 128<<20)
	if e != nil {
		return errors.New("fixed source download failed")
	}
	rec.ArchiveSHA = digest(raw)
	archive, e := opaqueArchive(raw)
	if e != nil {
		return e
	}
	if e = verifyLockedYaml(archive); e != nil {
		return e
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	// Exclusive per-candidate lock. A crashed run is retained for explicit recovery,
	// never treated as permission to launch another container.
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("todo2648-repo-%d.lock", c.ID))
	lock, e := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("candidate already occupied or lock unavailable")
	}
	defer func() {
		if e := lock.Close(); e != nil {
			ret = errors.Join(ret, errors.New("candidate lock close failed"))
			return
		}
		if (!rec.CreationUncertain && rec.ContainerID == "") || rec.ContainerRemoved {
			if e := os.Remove(lockPath); e != nil {
				ret = errors.Join(ret, errors.New("candidate lock release failed"))
			} else {
				rec.LockReleased = true
			}
		}
	}()
	if e = writeReceipt(dir, rec); e != nil {
		return e
	}
	name := fmt.Sprintf("todo2648-repo-%d-node-%d", c.ID, time.Now().UnixNano())
	rec.ContainerName = name
	rec.CreationUncertain = true
	defer func() {
		if rec.ContainerID == "" {
			return
		}
		var e error
		rec.TreeGone, rec.ContainerRemoved, e = disposeOwned(rec.ContainerID)
		ret = errors.Join(ret, e)
	}()
	id, e := createIsolated(ctx, name, image)
	rec.ContainerID = id
	if e != nil {
		return e
	}
	rec.CreationUncertain = false
	rec.Stage = "container-created"
	if e = cmd(ctx, docker, "start", rec.ContainerID).Run(); e != nil {
		return errors.New("container start failed")
	}
	b, e := cmd(ctx, docker, "inspect", rec.ContainerID).Output()
	if e != nil {
		return errors.New("isolation inspection unavailable")
	}
	if e = checkIsolation(b, image); e != nil {
		return e
	}
	rec.IsolationVerified = true
	// The trusted test command cannot start Docker, read a host profile or use GPU.
	extract := cmd(ctx, docker, "exec", "-i", rec.ContainerID, "tar", "--no-same-owner", "-xf", "-", "-C", "/work")
	extract.Stdin = bytes.NewReader(archive)
	if e = extract.Run(); e != nil {
		return errors.New("isolated source transfer failed")
	}
	// Some script contracts enumerate tracked files with git ls-files. Restore
	// an index from the archive, and require git write-tree to match GitHub's
	// exact tree before testing. Do not create a replacement source commit.
	gitArgs := []string{"exec", "--workdir", "/work", "-e", "GIT_CONFIG_NOSYSTEM=1", "-e", "GIT_CONFIG_GLOBAL=/dev/null", rec.ContainerID}
	for _, args := range [][]string{{"git", "init", "--quiet"}, {"git", "config", "core.autocrlf", "false"}, {"git", "add", "--force", "--all", "--", "."}} {
		if e = cmd(ctx, docker, append(append([]string{}, gitArgs...), args...)...).Run(); e != nil {
			return errors.New("source index reconstruction failed")
		}
	}
	tree, e := cmd(ctx, docker, append(append([]string{}, gitArgs...), "git", "write-tree")...).Output()
	if e != nil || strings.TrimSpace(string(tree)) != rec.SourceTree {
		return errors.New("extracted source tree differs from fixed GitHub tree")
	}
	rec.SourceTreeVerified = true
	// The original locked script-test dependency is fetched on the host, checked
	// against the candidate lockfile's SRI, and transferred without credentials.
	dependency, e := yamlDependency(ctx)
	if e != nil {
		return e
	}
	rec.DependencySHA = yamlIntegrity
	if e = cmd(ctx, docker, "exec", rec.ContainerID, "mkdir", "-p", "/work/node_modules/yaml").Run(); e != nil {
		return errors.New("test dependency directory unavailable")
	}
	dep := cmd(ctx, docker, "exec", "-i", rec.ContainerID, "tar", "--no-same-owner", "-xf", "-", "-C", "/work/node_modules/yaml")
	dep.Stdin = bytes.NewReader(dependency)
	if e = dep.Run(); e != nil {
		return errors.New("verified test dependency transfer failed")
	}
	logPath := filepath.Join(dir, fmt.Sprintf("repo-%d-node-contracts.private.log", c.ID))
	log, e := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("exclusive test log unavailable")
	}
	testArgs := []string{"exec", "--workdir", "/work", rec.ContainerID}
	if previousLog == "" {
		testArgs = append(testArgs, "sh", "-c", "node --test scripts/*.test.mjs")
	} else {
		testArgs = append(testArgs, "node", "--test")
		testArgs = append(testArgs, selected...)
	}
	test := cmd(ctx, docker, testArgs...)
	test.Stdout = log
	test.Stderr = log
	e = test.Run()
	closeErr := log.Close()
	code := 0
	if e != nil {
		code = 1
		var ee *exec.ExitError
		if errors.As(e, &ee) {
			code = ee.ExitCode()
		}
	}
	rec.Exit = &code
	rec.Stage = "node-contracts-finished"
	var readErr error
	b, readErr = os.ReadFile(logPath)
	rec.LogSHA = digest(b)
	rec.LogBytes = int64(len(b))
	if closeErr != nil || readErr != nil {
		return errors.New("test log preservation failed")
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	if code != 0 {
		return errors.New("node contract tests failed; consult private original log")
	}
	return nil
}

func nodeImage(image string) error {
	if !strings.HasPrefix(image, "node@sha256:") || len(strings.TrimPrefix(image, "node@sha256:")) != 64 {
		return errors.New("immutable official Node image required")
	}
	if _, e := hex.DecodeString(strings.TrimPrefix(image, "node@sha256:")); e != nil {
		return errors.New("image digest invalid")
	}
	return nil
}
func previousFailure(file string, c candidate) ([]byte, error) {
	if filepath.Base(file) != fmt.Sprintf("repo-%d-node-contracts.private.log", c.ID) {
		return nil, errors.New("previous log identity rejected")
	}
	meta, e := os.ReadFile(filepath.Join(filepath.Dir(file), fmt.Sprintf("repo-%d-result.private.json", c.ID)))
	if e != nil {
		return nil, errors.New("previous receipt unavailable")
	}
	var previous receipt
	if json.Unmarshal(meta, &previous) != nil || previous.RepoID != c.ID || previous.PR != c.PR || previous.SourceSHA != c.SHA || previous.Stage != "node-contracts-finished" || previous.Exit == nil || *previous.Exit == 0 {
		return nil, errors.New("previous failure identity rejected")
	}
	b, e := os.ReadFile(file)
	if e != nil || digest(b) != previous.LogSHA || int64(len(b)) != previous.LogBytes {
		return nil, errors.New("previous failure log digest rejected")
	}
	return b, nil
}
func verifyLockedYaml(archive []byte) error {
	tr := tar.NewReader(bytes.NewReader(archive))
	havePackage, haveLock := false, false
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return errors.New("dependency source archive invalid")
		}
		if h.Name != "package.json" && h.Name != "pnpm-lock.yaml" {
			continue
		}
		b, e := io.ReadAll(io.LimitReader(tr, 16<<20))
		if e != nil {
			return errors.New("dependency source unavailable")
		}
		if h.Name == "package.json" {
			var p struct {
				DevDependencies map[string]string `json:"devDependencies"`
			}
			havePackage = json.Unmarshal(b, &p) == nil && p.DevDependencies["yaml"] == "2.9.0"
		} else {
			haveLock = regexp.MustCompile(`(?m)^  yaml@2\.9\.0:\r?\n    resolution: \{integrity: ` + regexp.QuoteMeta(yamlIntegrity) + `\}`).Match(b)
		}
	}
	if !havePackage || !haveLock {
		return errors.New("fixed source YAML lock binding rejected")
	}
	return nil
}

const yamlIntegrity = "sha512-2AvhNX3mb8zd6Zy7INTtSpl1F15HW6Wnqj0srWlkKLcpYl/gMIMJiyuGq2KeI2YFxUPjdlB+3Lc10seMLtL4cA=="

func yamlDependency(ctx context.Context) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", "https://registry.npmjs.org/yaml/-/yaml-2.9.0.tgz", nil)
	if e != nil {
		return nil, e
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return errors.New("dependency redirect rejected") }}
	r, e := client.Do(req)
	if e != nil {
		return nil, errors.New("test dependency download failed")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, errors.New("test dependency download failed")
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 4<<20+1))
	if e != nil || len(b) > 4<<20 {
		return nil, errors.New("test dependency size rejected")
	}
	h := sha512.Sum512(b)
	if "sha512-"+base64.StdEncoding.EncodeToString(h[:]) != yamlIntegrity {
		return nil, errors.New("test dependency integrity rejected")
	}
	return opaqueArchive(b)
}
func failedScripts(b []byte) ([]string, error) {
	seen := map[string]bool{}
	var paths []string
	patterns := []string{`(?m)^not ok [0-9]+ - (scripts/[A-Za-z0-9._/-]+\.test\.mjs)\r?$`, `(?m)^\s+location: ['"]?/work/(scripts/[A-Za-z0-9._/-]+\.test\.mjs):[0-9]+:[0-9]+['"]?\r?$`}
	for _, pattern := range patterns {
		for _, m := range regexp.MustCompile(pattern).FindAllSubmatch(b, -1) {
			p := string(m[1])
			if path.Clean(p) != p || strings.Contains(p, "..") {
				return nil, errors.New("failure source path rejected")
			}
			if !seen[p] {
				paths = append(paths, p)
				seen[p] = true
			}
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("no failed script evidence")
	}
	return paths, nil
}

type isolation struct {
	Config struct {
		User, Image string
		Labels      map[string]string
	}
	HostConfig struct {
		Init                                *bool
		Privileged, ReadonlyRootfs          bool
		NetworkMode, PidMode                string
		Binds, CapAdd, CapDrop, SecurityOpt []string
		Devices, DeviceRequests             []json.RawMessage
		Tmpfs                               map[string]string
		NanoCPUs, Memory                    int64
		PidsLimit                           *int64
	}
	Mounts []struct{ Type, Destination string }
}

func checkIsolation(b []byte, image string) error {
	var all []isolation
	if json.Unmarshal(b, &all) != nil || len(all) != 1 {
		return errors.New("isolation metadata invalid")
	}
	i := all[0]
	h := i.HostConfig
	if i.Config.User != "1001:1001" || i.Config.Image != image || i.Config.Labels["cc.todo"] != "2648" || h.Init == nil || !*h.Init || h.Privileged || !h.ReadonlyRootfs || h.NetworkMode != "none" || h.PidMode != "" || len(h.Binds) > 0 || len(h.CapAdd) > 0 || len(h.Devices) > 0 || len(h.DeviceRequests) > 0 || h.NanoCPUs != 2000000000 || h.Memory != 2<<30 || h.PidsLimit == nil || *h.PidsLimit != 256 {
		return errors.New("container isolation rejected")
	}
	if len(h.CapDrop) != 1 || !strings.EqualFold(h.CapDrop[0], "ALL") {
		return errors.New("container capabilities rejected")
	}
	if len(h.SecurityOpt) != 1 || (h.SecurityOpt[0] != "no-new-privileges" && h.SecurityOpt[0] != "no-new-privileges=true") {
		return errors.New("container security options rejected")
	}
	if len(h.Tmpfs) != 2 || !strings.Contains(h.Tmpfs["/work"], "uid=1001") || !strings.Contains(h.Tmpfs["/tmp"], "noexec") {
		return errors.New("container scratch rejected")
	}
	for _, m := range i.Mounts {
		if m.Type != "tmpfs" || (m.Destination != "/work" && m.Destination != "/tmp") {
			return errors.New("host mount rejected")
		}
	}
	return nil
}

func main() {
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(stopCtx, 30*time.Minute)
	defer cancel()
	var e error
	if len(os.Args) == 2 && os.Args[1] == "inspect" {
		for _, c := range candidates {
			_, err := verify(ctx, c, get)
			stage := "fixed-head-verified"
			if err != nil {
				stage = err.Error()
				e = errors.Join(e, err)
			}
			_ = json.NewEncoder(os.Stdout).Encode(receipt{RepoID: c.ID, PR: c.PR, SourceSHA: c.SHA, Stage: stage, At: time.Now().UTC()})
		}
	} else if len(os.Args) == 4 && os.Args[1] == "lifecycle-smoke" {
		e = lifecycleSmoke(ctx, os.Args[2], os.Args[3])
	} else if (len(os.Args) == 5 && os.Args[1] == "node-contracts") || (len(os.Args) == 6 && os.Args[1] == "node-retry") {
		id, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			e = errors.New("invalid opaque ID")
		} else {
			found := false
			for _, c := range candidates {
				if c.ID == id {
					found = true
					previous := ""
					if len(os.Args) == 6 {
						previous = os.Args[5]
					}
					e = nodeContracts(ctx, c, os.Args[3], os.Args[4], previous)
					break
				}
			}
			if !found {
				e = errors.New("unknown candidate")
			}
		}
	} else {
		e = errors.New("usage: product-ci inspect | node-contracts <id> <new-owned-dir> <node@sha256:digest> | node-retry <id> <new-owned-dir> <image> <previous-log> | lifecycle-smoke <owned-dir> <image>")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
