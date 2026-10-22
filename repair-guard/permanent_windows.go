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
	{model2903Source, "9fb62b51c7b4f145efadfbd894224795fbc3157e"},
	{model3082Source, "eae2d2bd7e3e16fe3833b252b065c0ee8ee92a4b"},
	{model3250Source, "7bb3858a81d833dc5bd6e2320c2b81f4a55d0969"},
	{model3298Source, model3298Head},
	{model3081Source, "cbddcb32fb8ffc9364862b743c4435739fab1aec"},
	{model3309Source, "e290e27ffdb63cbd35dcb8af559372293a94de87"},
	{model3338GuardSource, "a36a77c42690eef7c0147a6a2eee76b496a48249"},
	{model3344GuardSource, "00e47e1cc5f8bd7d1d3c7af8cce4e3c04f36102a"},
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
