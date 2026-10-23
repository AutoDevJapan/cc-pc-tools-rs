package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// These are normal public Git fetches at exact commits, with byte-identical
// copies of the independently pinned receipts. No old receipt is rewritten to
// claim that a build took place in the mirror. No caller can add a source.
const permanent3344Root = "D:/CC/RuntimeSources/todo-3344"

type permanentSource struct{ root, head string }

var permanent3344Sources = []permanentSource{
	{"D:/CC/Work/unattended-operations-20261013", "cb47db4d325b026499fb20295158b1de8c66f427"},
	{"D:/CC/Work/unattended-operations-20261013-wt/todo-3278-console", "9a80b18358dd58174f1536536d12eb28d79cb806"},
}

func permanentRuntimeSourceVerified(root string) bool {
	return permanentSourceVerifiedWith(root, permanent3344Sources)
}

func permanentSourceVerifiedWith(root string, sources []permanentSource) bool {
	for _, source := range sources {
		if !same(root, source.root) {
			continue
		}
		resolved, err := physical(root)
		if err != nil || !same(resolved, root) || legacyMatch(root) ||
			!civ6Commit.MatchString(source.head) || !consoleSourceVerified(root) {
			return false
		}
		head, err := permanentGitAt(root, "rev-parse", "HEAD")
		if err != nil || strings.TrimSpace(string(head)) != source.head {
			return false
		}
		// Check tracked content with the declared Windows newline normalization.
		// Per-asset SHA pins still require the exact original source bytes.
		// Untracked, separately pinned original receipts are allowed.
		_, err = permanentGitAt(root, "-c", "core.autocrlf=true", "diff", "--quiet", "--no-ext-diff", "--no-textconv", "HEAD", "--")
		return err == nil
	}
	return false
}

func permanentGitAt(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	argv := append([]string{"-C", root, "--no-replace-objects"}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	// Partial clones may never fetch an object as a side effect of classification.
	cmd.Env = append(civ6GitEnvironment(os.Environ()), "GIT_NO_LAZY_FETCH=1")
	return cmd.Output()
}
