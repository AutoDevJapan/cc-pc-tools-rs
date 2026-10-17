package sourcepacket

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// VerifyCheckout observes the actual post-checkout state. It never fetches,
// interprets credentials, executes filters/hooks or treats an index as file proof.
func VerifyCheckout(ctx context.Context, pin Pin, entries []TreeEntry, root string) (Receipt, error) {
	if !validPin(pin) || !filepath.IsAbs(root) || validateEntries(entries) != nil {
		return Receipt{}, fixedErr()
	}
	git := func(input []byte, args ...string) ([]byte, error) {
		args = append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull}, args...)
		c := exec.CommandContext(ctx, "git", args...)
		configureProcess(c)
		c.Dir = root
		c.Env = cleanGitEnv(os.Environ())
		c.Stdin = bytes.NewReader(input)
		c.Stderr = io.Discard
		c.WaitDelay = 3 * time.Second
		b := bounded{limit: 1 << 20}
		c.Stdout = &b
		if c.Run() != nil {
			return nil, fixedErr()
		}
		return b.Bytes(), nil
	}
	names, e := git(nil, "config", "--local", "--name-only", "--list")
	if e != nil {
		return Receipt{}, fixedErr()
	}
	for _, name := range strings.Split(strings.ToLower(string(names)), "\n") {
		if strings.HasPrefix(name, "credential.") || strings.HasSuffix(name, ".extraheader") || strings.HasPrefix(name, "include") || name == "core.sshcommand" || name == "core.hookspath" || name == "core.fsmonitor" || name == "core.worktree" || strings.HasPrefix(name, "filter.") {
			return Receipt{}, fixedErr()
		}
	}
	for _, check := range []struct{ expr, want string }{{"HEAD", pin.HeadSHA}, {"HEAD^{tree}", pin.TreeSHA}} {
		b, e := git(nil, "rev-parse", check.expr)
		if e != nil || strings.TrimSpace(string(b)) != check.want {
			return Receipt{}, fixedErr()
		}
	}
	index, e := git(nil, "write-tree")
	if e != nil || strings.TrimSpace(string(index)) != pin.TreeSHA {
		return Receipt{}, fixedErr()
	}
	count := 0
	for _, entry := range entries {
		if entry.Type != "blob" {
			continue
		}
		file := filepath.Join(root, filepath.FromSlash(entry.Path))
		if !within(root, file) {
			return Receipt{}, fixedErr()
		}
		info, e := os.Lstat(file)
		if e != nil || !info.Mode().IsRegular() {
			return Receipt{}, fixedErr()
		}
		if runtime.GOOS != "windows" && ((entry.Mode == "100755") != (info.Mode()&0111 != 0)) {
			return Receipt{}, fixedErr()
		}
		b, e := git([]byte(file+"\n"), "hash-object", "--no-filters", "--stdin-paths")
		if e != nil || strings.TrimSpace(string(b)) != entry.SHA {
			return Receipt{}, fixedErr()
		}
		count++
	}
	return Receipt{RepoID: RepoID, PR: PR, HeadSHA: pin.HeadSHA, TreeSHA: pin.TreeSHA, BlobCount: count, PhysicalBlobsVerified: true}, nil
}
