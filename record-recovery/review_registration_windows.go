//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// This adapter uses the already qualified Codex normal API. It never opens a
// native transcript, the saved receipt or the original controller source.
func acquireReviewRegistration() (observed bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	revision, _ := omb3255BuildIdentity()
	location, err := os.Getwd()
	if err != nil || revision == "" || !omb3255SamePath(location, omb3255SourceRoot) {
		return false
	}
	moduleStable := func() bool {
		git := exec.CommandContext(ctx, "git", "-C", omb3255SourceRoot, "rev-parse", revision+":record-recovery", "HEAD:record-recovery")
		hideCommand(git)
		git.WaitDelay = time.Second
		modules, err := git.Output()
		if err != nil || !reviewRegistrationModuleMatch(modules) {
			return false
		}
		// This Go module has no local replacements. Compare its tracked
		// working inputs too; a later unrelated documentation commit is fine.
		git = exec.CommandContext(ctx, "git", "-C", omb3255SourceRoot, "diff", "--quiet", revision, "--", "record-recovery")
		hideCommand(git)
		git.WaitDelay = time.Second
		return git.Run() == nil
	}
	if !moduleStable() {
		return false
	}
	anchor := filepath.Join(omb3255SourceRoot, "record-recovery", "main.go")
	protection := func() bool {
		source, a := omb3255Guard(ctx, anchor)
		binary, b := omb3255Guard(ctx, omb3255Codex)
		return a == nil && b == nil && source.codeClear() && binary.codeClear()
	}
	if !protection() {
		return false
	}
	binary, err := omb3255OpenPinnedRuntime(omb3255Codex)
	if err != nil {
		return false
	}
	defer func() {
		stable := binary.Unchanged()
		closed := binary.Close()
		if !stable || closed != nil {
			observed = false
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(binary, 512<<20+1))
	if err != nil || size <= 0 || size > 512<<20 || hex.EncodeToString(hash.Sum(nil)) != omb3255CodexSHA || !binary.Unchanged() {
		return false
	}
	rpc, err := omb3255StartMetadataRPC(ctx)
	if err != nil {
		return false
	}
	defer func() {
		if rpc.Close() != nil {
			observed = false
		}
	}()
	return inspectReviewRegistration(rpc.Call) && inspectReviewContinuation(func(endpoint string) (obj, error) { return omb3255NormalGET(ctx, endpoint) }) && protection() && moduleStable() && binary.Unchanged()
}
