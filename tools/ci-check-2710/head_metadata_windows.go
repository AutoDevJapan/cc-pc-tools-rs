package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func installWindowsHead(ctx context.Context, winFile, linuxFile string) error {
	v, e := readWin(winFile)
	if e != nil {
		return e
	}
	s, e := load(linuxFile)
	if e != nil {
		return e
	}
	c, e := pick(v.RepoID)
	if e != nil {
		return e
	}
	if s.RepoID != v.RepoID || s.SHA != c.SHA || v.SHA != c.SHA || s.Tree != v.Tree {
		return errors.New("head metadata scope mismatch")
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	b, e := output(ctx, "exec", "--workdir", "/work/source", s.Container, "git", "cat-file", "commit", c.SHA)
	if e != nil || hex.EncodeToString(gitObject("commit", b)) != c.SHA {
		return errors.New("original GitHub commit object unavailable")
	}
	source := filepath.Join(v.Root, "source")
	run := func(args []string, input []byte) ([]byte, error) {
		x := cmd(ctx, `C:\Program Files\Git\cmd\git.exe`, append([]string{"-C", source, "-c", "safe.directory=" + source}, args...)...)
		x.Env = []string{`SystemRoot=C:\Windows`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=NUL`}
		x.Stdin = bytes.NewReader(input)
		return x.Output()
	}
	oid, e := run([]string{"hash-object", "-t", "commit", "-w", "--stdin"}, b)
	if e != nil || strings.TrimSpace(string(oid)) != c.SHA {
		return errors.New("Windows original commit transfer failed")
	}
	if e = os.WriteFile(filepath.Join(source, ".git", "shallow"), []byte(c.SHA+"\n"), 0600); e != nil {
		return e
	}
	if _, e = run([]string{"update-ref", "--no-deref", "HEAD", c.SHA}, nil); e != nil {
		return errors.New("Windows detached fixed HEAD failed")
	}
	return writeJSON(filepath.Join(v.Root, "head-metadata.private.json"), map[string]any{"repo_id": v.RepoID, "original_commit": c.SHA, "object_sha_verified": true, "source_tree": v.Tree, "shallow": true, "full_history": false})
}

// Reproduce only an original GitHub commit whose bytes hash to the fixed ID.
// This never manufactures a replacement commit or claims full repository history.
func installHead(ctx context.Context, file string) error {
	s, e := load(file)
	if e != nil {
		return e
	}
	c, e := pick(s.RepoID)
	if e != nil {
		return e
	}
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	var commit struct {
		Tree              struct{ SHA string }
		Parents           []struct{ SHA string }
		Author, Committer struct{ Name, Email, Date string }
		Message           string
		Verification      struct{ Payload, Signature string }
	}
	if e = get(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil {
		return e
	}
	if commit.Tree.SHA != s.Tree {
		return errors.New("commit tree binding changed")
	}
	var payload []byte
	if commit.Verification.Signature == "" && commit.Verification.Payload != "" {
		payload = []byte(commit.Verification.Payload)
	}
	if hex.EncodeToString(gitObject("commit", payload)) != c.SHA {
		a, e := time.Parse(time.RFC3339, commit.Author.Date)
		if e != nil {
			return errors.New("author date unsupported")
		}
		b, e := time.Parse(time.RFC3339, commit.Committer.Date)
		if e != nil {
			return errors.New("committer date unsupported")
		}
		base := "tree " + s.Tree + "\n"
		for _, p := range commit.Parents {
			base += "parent " + p.SHA + "\n"
		}
		found := false
		for _, tzA := range []string{"+0000", "+0900"} {
			for _, tzB := range []string{"+0000", "+0900"} {
				for _, tail := range []string{"", "\n"} {
					p := base + fmt.Sprintf("author %s <%s> %d %s\ncommitter %s <%s> %d %s\n", commit.Author.Name, commit.Author.Email, a.Unix(), tzA, commit.Committer.Name, commit.Committer.Email, b.Unix(), tzB)
					if commit.Verification.Signature != "" {
						sig := strings.TrimSuffix(commit.Verification.Signature, "\n")
						p += "gpgsig " + strings.ReplaceAll(sig, "\n", "\n ") + "\n"
					}
					p += "\n" + commit.Message + tail
					if hex.EncodeToString(gitObject("commit", []byte(p))) == c.SHA {
						payload = []byte(p)
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return errors.New("original commit byte reconstruction unavailable; no replacement written")
		}
	}
	x := cmd(ctx, docker, "exec", "-i", "--workdir", "/work/source", s.Container, "git", "hash-object", "-t", "commit", "-w", "--stdin")
	x.Stdin = bytes.NewReader(payload)
	b, e := x.Output()
	if e != nil || strings.TrimSpace(string(b)) != c.SHA {
		return errors.New("original commit object transfer failed")
	}
	// Explicitly shallow, with the original fixed commit as detached HEAD.
	script := "printf '%s\\n' '" + c.SHA + "' > .git/shallow && git update-ref --no-deref HEAD '" + c.SHA + "' && git rev-parse HEAD"
	b, e = shell(ctx, s, script)
	if e != nil || strings.TrimSpace(string(b)) != c.SHA {
		return errors.New("fixed original HEAD verification failed")
	}
	return writeJSON(filepath.Join(s.Root, fmt.Sprintf("repo%d-head-metadata.private.json", s.RepoID)), map[string]any{"repo_id": s.RepoID, "original_commit": c.SHA, "original_object_sha_verified": true, "shallow": true, "full_history": false, "source_tree": s.Tree, "at": time.Now().UTC()})
}
