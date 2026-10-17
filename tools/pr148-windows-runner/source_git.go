package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func sourceGit(source string, args ...string) ([]byte, error) {
	exe := "/usr/bin/git"
	if runtime.GOOS == "windows" {
		exe = `C:\Program Files\Git\cmd\git.exe`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, exe, append([]string{"--no-optional-locks", "-c", "safe.directory=" + source, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=NUL", "-C", source}, args...)...)
	hideSourceCommand(c)
	c.WaitDelay = time.Second
	c.Env = []string{`SystemRoot=C:\Windows`, `WINDIR=C:\Windows`, `PATH=C:\Windows\System32`, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1"}
	var out bytes.Buffer
	c.Stdout = &sourceOutput{Buffer: &out}
	if e := c.Run(); e != nil {
		return nil, errors.New("source Git operation failed")
	}
	return out.Bytes(), nil
}

type sourceOutput struct{ *bytes.Buffer }

func (w *sourceOutput) Write(b []byte) (int, error) {
	if w.Len()+len(b) > 8<<20 {
		return 0, errors.New("source Git output bound")
	}
	return w.Buffer.Write(b)
}

// Check the actual bytes independently of diff, filters, fsmonitor and index
// assume-unchanged/skip-worktree bits. The complete index must match the tree.
func verifyTrackedBytes(source, tree string) error {
	if e := sourceMetadata(source); e != nil {
		return e
	}
	b, e := sourceGit(source, "write-tree")
	if e != nil || strings.TrimSpace(string(b)) != tree {
		return errors.New("actual source index tree mismatch")
	}
	b, e = sourceGit(source, "ls-files", "--stage", "-z")
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, row := range bytes.Split(bytes.TrimSuffix(b, []byte{0}), []byte{0}) {
		meta, path, ok := strings.Cut(string(row), "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[2] != "0" || (fields[0] != "100644" && fields[0] != "100755") || !validSHA(fields[1]) || !sourcePathOK(path) || seen[strings.ToLower(path)] {
			return errors.New("source index entry rejected")
		}
		seen[strings.ToLower(path)] = true
		full := filepath.Join(source, filepath.FromSlash(path))
		for cur := full; cur != source; cur = filepath.Dir(cur) {
			fi, e := os.Lstat(cur)
			if e != nil || sourceReparse(fi) {
				return errors.New("source path unavailable or reparse")
			}
		}
		fi, e := os.Lstat(full)
		if e != nil || !fi.Mode().IsRegular() || fi.Size() > 16<<20 {
			return errors.New("source tracked type/size rejected")
		}
		raw, e := os.ReadFile(full)
		if e != nil || objectID("blob", raw) != fields[1] {
			return errors.New("actual tracked bytes mismatch")
		}
	}
	return nil
}

func sourceMetadata(source string) error {
	fi, e := os.Lstat(source)
	if e != nil || !fi.IsDir() || sourceReparse(fi) {
		return errors.New("real private source directory required")
	}
	git := filepath.Join(source, ".git")
	fi, e = os.Lstat(git)
	if e != nil || !fi.IsDir() || sourceReparse(fi) {
		return errors.New("real private Git directory required")
	}
	for _, path := range []string{"objects/info/alternates", "objects/info/http-alternates", "info/grafts", "refs/replace", "commondir", "worktrees"} {
		if _, e := os.Lstat(filepath.Join(git, filepath.FromSlash(path))); !os.IsNotExist(e) {
			return errors.New("Git alternate/replacement storage rejected")
		}
	}
	if e = filepath.Walk(git, func(path string, fi os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if sourceReparse(fi) {
			return errors.New("Git metadata reparse rejected")
		}
		return nil
	}); e != nil {
		return e
	}
	// checkout's sparse-checkout disable leaves this inert file after removing
	// extensions.worktreeConfig. Accept only its literal disabled core values;
	// activated worktree config is separately refused by the config-key guard.
	path := filepath.Join(git, "config.worktree")
	if fi, err := os.Lstat(path); err == nil {
		if !fi.Mode().IsRegular() || fi.Size() > 4096 {
			return errors.New("worktree config type/size rejected")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !inertWorktreeConfig(raw) {
			return errors.New("non-inert worktree config rejected")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func inertWorktreeConfig(raw []byte) bool {
	section := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if line == "[core]" {
			section = "core"
			continue
		}
		if line == "[index]" {
			section = "index"
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(value) != "false" {
			return false
		}
		key = strings.TrimSpace(key)
		if !((section == "core" && (key == "sparsecheckout" || key == "sparsecheckoutcone")) || (section == "index" && key == "sparse")) {
			return false
		}
	}
	return true
}
