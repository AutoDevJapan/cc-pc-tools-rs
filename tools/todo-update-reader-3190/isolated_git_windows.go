package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// isolatedEnvironment gives Git a scratch repository whose only outside data
// is the fixed object store. It drops every inherited GIT_* setting and turns
// off the system and global configuration, so Git reads no configuration
// file and follows no include.
func isolatedEnvironment(parent []string, gitDir, objects string) []string {
	out := make([]string, 0, len(parent)+7)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			out = append(out, entry)
		}
	}
	return append(out, "GIT_DIR="+gitDir, "GIT_OBJECT_DIRECTORY="+objects, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
}

// runGit runs one object read in a new scratch repository that holds only
// HEAD, an empty refs directory and, for a shallow source, its boundary. The
// repository has no configuration file; the objects are the source's own.
func runGit(src gitSource, args ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "todo-update-reader-3190-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.Mkdir(filepath.Join(dir, "refs"), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		return nil, err
	}
	if len(src.shallow) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "shallow"), src.shallow, 0o600); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-replace-objects"}, args...)...)
	hiddenProcess(cmd)
	cmd.Dir = dir
	cmd.Env = isolatedEnvironment(os.Environ(), dir, src.objects)
	out := &limitedBuffer{limit: 32 << 20}
	cmd.Stdout = out
	err = cmd.Run()
	if out.overflow {
		return nil, errors.New("git output limit exceeded")
	}
	return out.Bytes(), err
}
