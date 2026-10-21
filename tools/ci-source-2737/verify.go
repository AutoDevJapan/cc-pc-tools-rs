package sourcepacket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// VerifyCheckout reads real metadata and blobs through no-follow, anchored
// handles. Git only consumes a private snapshot of those SAME metadata bytes;
// it never reopens an untrusted worktree path or follows its metadata links.
func VerifyCheckout(ctx context.Context, pin Pin, entries []TreeEntry, root string) (receipt Receipt, retErr error) {
	stage := "pin"
	defer func() {
		if retErr != nil {
			if stage == "metadata" && MetadataFailure(retErr) != nil {
				return
			}
			retErr = fmt.Errorf("checkout proof rejected at %s", stage)
		}
	}()
	if !validPin(pin) || !filepath.IsAbs(root) || validateEntries(entries) != nil {
		return Receipt{}, fixedErr()
	}
	stage = "root"
	fs, e := secureRoot(root)
	if e != nil {
		return Receipt{}, e
	}
	defer fs.Close()
	snapshot, e := os.MkdirTemp("", "ci2737-git-proof-")
	if e != nil {
		return Receipt{}, fixedErr()
	}
	defer os.RemoveAll(snapshot)
	git := func(input []byte, args ...string) ([]byte, error) {
		args = append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull}, args...)
		c := exec.CommandContext(ctx, "git", args...)
		configureProcess(c)
		c.Dir = snapshot
		c.Env = append(cleanGitEnv(os.Environ()), "GIT_NO_REPLACE_OBJECTS=1")
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
	// Inspect key NAMES via stdin with includes disabled; never persist a config
	// carrying checkout authentication or executable hooks/filter configuration.
	stage = "config-open"
	config, _, e := fs.read(filepath.FromSlash(".git/config"), 1<<20)
	if e != nil {
		return Receipt{}, e
	}
	stage = "config-names"
	names, e := git(config, "config", "--file", "-", "--no-includes", "--name-only", "--list")
	if e != nil {
		return Receipt{}, e
	}
	for _, name := range strings.Split(strings.ToLower(string(names)), "\n") {
		if strings.HasPrefix(name, "credential.") || strings.HasPrefix(name, "extensions.") || strings.HasSuffix(name, ".extraheader") || strings.HasPrefix(name, "include") || name == "core.sshcommand" || name == "core.hookspath" || name == "core.fsmonitor" || name == "core.worktree" || strings.HasPrefix(name, "filter.") {
			return Receipt{}, fixedErr()
		}
	}
	stage = "metadata"
	if e = copyGitProofMetadata(ctx, fs, snapshot, config); e != nil {
		return Receipt{}, e
	}
	stage = "head"
	for _, check := range []struct{ expr, want string }{{"HEAD", pin.HeadSHA}, {"HEAD^{tree}", pin.TreeSHA}} {
		b, e := git(nil, "rev-parse", check.expr)
		if e != nil || strings.TrimSpace(string(b)) != check.want {
			return Receipt{}, fixedErr()
		}
	}
	stage = "index"
	index, e := git(nil, "write-tree")
	if e != nil || strings.TrimSpace(string(index)) != pin.TreeSHA {
		return Receipt{}, fixedErr()
	}
	stage = "physical-blobs"
	count := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return Receipt{}, fixedErr()
		}
		if entry.Type != "blob" {
			continue
		}
		b, info, e := fs.read(filepath.FromSlash(entry.Path), maxFileBytes)
		if e != nil {
			return Receipt{}, e
		}
		if runtime.GOOS != "windows" && ((entry.Mode == "100755") != (info.Mode()&0111 != 0)) {
			return Receipt{}, fixedErr()
		}
		if gitID("blob", b) != entry.SHA {
			return Receipt{}, fixedErr()
		}
		if entry.Path == workflowPath && digest(b) != pin.WorkflowSHA256 {
			return Receipt{}, fixedErr()
		}
		count++
	}
	stage = "path-revalidation"
	if e = fs.revalidate(); e != nil {
		return Receipt{}, e
	}
	return Receipt{RepoID: RepoID, PR: PR, HeadSHA: pin.HeadSHA, TreeSHA: pin.TreeSHA, BlobCount: count, PhysicalBlobsVerified: true}, nil
}

func copyGitProofMetadata(ctx context.Context, fs *secureFiles, snapshot string, checkedConfig []byte) error {
	var total int64
	count := 0
	fail := func(code, kind string, size int64) error {
		return &metadataProofError{MetadataDiagnostic{Code: code, Kind: kind, Entries: count, FileBytes: size, TotalBytes: total}}
	}
	var walk func(string) error
	walk = func(rel string) error {
		if ctx.Err() != nil {
			return fail("context", "directory", 0)
		}
		if os.MkdirAll(filepath.Join(snapshot, rel), 0700) != nil {
			return fail("write", "directory", 0)
		}
		d, e := fs.directory(rel)
		if e != nil {
			return fail("directory", "directory", 0)
		}
		items, e := d.ReadDir(-1)
		if e != nil {
			return fail("enumerate", "directory", 0)
		}
		for _, item := range items {
			count++
			if count > maxFiles {
				return fail("entry_limit", "directory", 0)
			}
			name := filepath.Join(rel, item.Name())
			slash := filepath.ToSlash(name)
			if !secureRelative(name) || item.Type()&os.ModeSymlink != 0 {
				return fail("path", metadataKind(slash), 0)
			}
			if slash == ".git/config.worktree" {
				// config-names has already rejected EVERY extensions.* key in
				// checkedConfig. Git's sparse-checkout disable can nevertheless
				// leave this exact, inactive all-false residue after checkout
				// removes extensions.worktreeConfig. Prove it through the SAME
				// anchored/no-link read; never load it into Git's snapshot.
				b, _, e := fs.read(name, int64(len(disabledSparseWorktreeConfig)))
				if e != nil || !bytes.Equal(b, []byte(disabledSparseWorktreeConfig)) {
					return fail("policy", "other", 0)
				}
				total += int64(len(b))
				if total > 512<<20 {
					return fail("total_limit", "other", int64(len(b)))
				}
				continue
			}
			if slash == ".git/commondir" || slash == ".git/gitdir" || slash == ".git/config.worktree" || slash == ".git/objects/info/alternates" || slash == ".git/objects/info/http-alternates" || slash == ".git/info/grafts" || slash == ".git/refs/replace" || slash == ".git/worktrees" {
				return fail("policy", metadataKind(slash), 0)
			}
			if item.IsDir() {
				if e = walk(name); e != nil {
					return e
				}
				continue
			}
			// No reflog, hooks, credential file or other unrelated metadata is read.
			selected := slash == ".git/HEAD" || slash == ".git/index" || slash == ".git/config" || slash == ".git/packed-refs" || slash == ".git/shallow" || strings.HasPrefix(slash, ".git/refs/") || strings.HasPrefix(slash, ".git/objects/")
			if !selected {
				continue
			}
			b := checkedConfig
			if slash != ".git/config" {
				var e error
				b, _, e = fs.read(name, 128<<20)
				if e != nil {
					var readErr *secureReadError
					if errors.As(e, &readErr) {
						return fail(readErr.code, metadataKind(slash), readErr.size)
					}
					return fail("read", metadataKind(slash), 0)
				}
			}
			total += int64(len(b))
			if total > 512<<20 {
				return fail("total_limit", metadataKind(slash), int64(len(b)))
			}
			target := filepath.Join(snapshot, name)
			if !within(snapshot, target) {
				return fail("path", metadataKind(slash), int64(len(b)))
			}
			if os.MkdirAll(filepath.Dir(target), 0700) != nil || os.WriteFile(target, b, 0600) != nil {
				return fail("write", metadataKind(slash), int64(len(b)))
			}
		}
		return nil
	}
	return walk(".git")
}

// Native Git reproduction and the retained CI disk both produce these 83
// bytes (SHA256 443a5f645c23c3d0c0aa09f634b2ad111d46ef61946b598a2fb311678ab47454).
// This is not a general secondary-config parser or an authorization manifest.
const disabledSparseWorktreeConfig = "[core]\n\tsparseCheckout = false\n\tsparseCheckoutCone = false\n[index]\n\tsparse = false\n"
