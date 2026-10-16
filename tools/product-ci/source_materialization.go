package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

type originalCommit struct {
	Tree              struct{ SHA string }
	Parents           []struct{ SHA string }
	Author, Committer struct{ Name, Email, Date string }
	Message           string
	Verification      struct{ Payload, Signature string }
}

func gitObjectID(kind string, b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// Write only bytes that reproduce the real fixed GitHub commit ID. No
// substitute commit, author, timezone or full-history claim is manufactured.
func originalCommitBytes(c originalCommit, expected string) ([]byte, error) {
	if c.Verification.Signature == "" && gitObjectID("commit", []byte(c.Verification.Payload)) == expected {
		return []byte(c.Verification.Payload), nil
	}
	a, e := time.Parse(time.RFC3339, c.Author.Date)
	if e != nil {
		return nil, errors.New("original author timestamp unavailable")
	}
	b, e := time.Parse(time.RFC3339, c.Committer.Date)
	if e != nil {
		return nil, errors.New("original committer timestamp unavailable")
	}
	prefix := "tree " + c.Tree.SHA + "\n"
	for _, p := range c.Parents {
		prefix += "parent " + p.SHA + "\n"
	}
	for _, tzA := range []string{"+0000", "+0900"} {
		for _, tzB := range []string{"+0000", "+0900"} {
			for _, tail := range []string{"", "\n"} {
				body := prefix + fmt.Sprintf("author %s <%s> %d %s\ncommitter %s <%s> %d %s\n", c.Author.Name, c.Author.Email, a.Unix(), tzA, c.Committer.Name, c.Committer.Email, b.Unix(), tzB)
				if c.Verification.Signature != "" {
					body += "gpgsig " + strings.ReplaceAll(strings.TrimSuffix(c.Verification.Signature, "\n"), "\n", "\n ") + "\n"
				}
				body += "\n" + c.Message + tail
				if gitObjectID("commit", []byte(body)) == expected {
					return []byte(body), nil
				}
			}
		}
	}
	return nil, errors.New("original commit bytes cannot be reproduced; no replacement written")
}

// A policy/aggregation job may not have an actions/checkout step. Materialize
// the same fixed source before registration so every job still has real Git
// HEAD/tree evidence. Product source crosses stdin, never a host bind/profile.
func materializeFixedSource(ctx context.Context, c candidate, p codeQLPlan, container string) (string, error) {
	r, e := verify(ctx, c, get)
	if e != nil {
		return "", e
	}
	if p.SHA != c.SHA || p.RepoID != c.ID || p.Repo != r.FullName {
		return "", errors.New("materialized source scope mismatch")
	}
	var commit originalCommit
	if e = get(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil {
		return "", e
	}
	if commit.Tree.SHA != p.Tree {
		return "", errors.New("materialized source tree changed")
	}
	payload, e := originalCommitBytes(commit, c.SHA)
	if e != nil {
		return "", e
	}
	var tree struct {
		Truncated bool
		Entries   []struct{ Path, Mode, SHA, Type string } `json:"tree"`
	}
	if e = get(ctx, "repos/"+r.FullName+"/git/trees/"+p.Tree+"?recursive=1", &tree); e != nil {
		return "", e
	}
	if tree.Truncated {
		return "", errors.New("complete Git tree unavailable")
	}
	raw, e := boundedOutput(cmd(ctx, gh, "api", "repos/"+r.FullName+"/tarball/"+c.SHA), 128<<20)
	if e != nil {
		return "", errors.New("fixed source archive unavailable")
	}
	archive, e := opaqueArchive(raw)
	if e != nil {
		return "", e
	}
	_, name, _ := strings.Cut(r.FullName, "/")
	workspace := "/work/" + name + "/" + name
	if e = cmd(ctx, docker, "exec", container, "mkdir", "-p", workspace, "/runner/empty-template").Run(); e != nil {
		return "", errors.New("owned checkout directory creation failed")
	}
	transfer := cmd(ctx, docker, "exec", "-i", container, "tar", "-xf", "-", "-C", workspace)
	transfer.Stdin = bytes.NewReader(archive)
	if e = transfer.Run(); e != nil {
		return "", errors.New("fixed source archive transfer failed")
	}
	git := func(input []byte, args ...string) ([]byte, error) {
		x := cmd(ctx, docker, append([]string{"exec", "-i", "-e", "GIT_CONFIG_NOSYSTEM=1", "-e", "GIT_CONFIG_GLOBAL=/dev/null", "--workdir", workspace, container, "git", "-c", "core.autocrlf=false", "-c", "core.filemode=true"}, args...)...)
		x.Stdin = bytes.NewReader(input)
		return x.Output()
	}
	if _, e = git(nil, "init", "--quiet", "--template=/runner/empty-template"); e != nil {
		return "", errors.New("owned Git initialization failed")
	}
	var paths, expectedBlobs []string
	var index bytes.Buffer
	for _, entry := range tree.Entries {
		if entry.Type == "tree" {
			continue
		}
		if entry.Path == "" || path.Clean(entry.Path) != entry.Path || strings.HasPrefix(entry.Path, "/") || strings.HasPrefix(entry.Path, ".git/") || entry.Path == ".git" || strings.ContainsAny(entry.Path, "\\\r\n\x00") || len(entry.SHA) != 40 || strings.Trim(entry.SHA, "0123456789abcdef") != "" {
			return "", errors.New("original tracked source entry rejected")
		}
		if entry.Mode == "160000" && entry.Type == "commit" {
			// Do not fetch or execute the referenced repository.
		} else if entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755") {
			paths = append(paths, workspace+"/"+entry.Path)
			expectedBlobs = append(expectedBlobs, entry.SHA)
		} else {
			return "", errors.New("unsupported tracked source mode")
		}
		fmt.Fprintf(&index, "%s %s\t%s%c", entry.Mode, entry.SHA, entry.Path, byte(0))
	}
	// A fresh `git add` loses already-tracked ignored files and can apply
	// .gitattributes filters. Hash actual archive bytes without filters instead,
	// require every original blob ID, then restore exactly the API's index modes.
	if len(paths) == 0 {
		return "", errors.New("original source contains no blobs")
	}
	hashed, e := git([]byte(strings.Join(paths, "\n")+"\n"), "hash-object", "--no-filters", "-w", "--stdin-paths")
	if e != nil {
		return "", errors.New("actual source blob hashing failed")
	}
	got := strings.Fields(string(hashed))
	if len(got) != len(expectedBlobs) {
		return "", errors.New("actual source blob count differs")
	}
	for i, sha := range got {
		if sha != expectedBlobs[i] {
			return "", errors.New("actual source blob bytes differ from GitHub")
		}
	}
	if _, e = git(index.Bytes(), "update-index", "-z", "--index-info"); e != nil {
		return "", errors.New("original tracked source index restoration failed")
	}
	b, e := git(nil, "write-tree")
	if e != nil || strings.TrimSpace(string(b)) != p.Tree {
		return "", errors.New("actual source index tree differs from GitHub")
	}
	b, e = git(payload, "hash-object", "-t", "commit", "-w", "--stdin")
	if e != nil || strings.TrimSpace(string(b)) != c.SHA {
		return "", errors.New("original commit object transfer failed")
	}
	x := cmd(ctx, docker, "exec", "-i", "--workdir", workspace, container, "sh", "-c", "cat > .git/shallow")
	x.Stdin = strings.NewReader(c.SHA + "\n")
	if e = x.Run(); e != nil {
		return "", errors.New("shallow boundary transfer failed")
	}
	if _, e = git(nil, "update-ref", "--no-deref", "HEAD", c.SHA); e != nil {
		return "", errors.New("fixed detached HEAD unavailable")
	}
	b, e = git(nil, "rev-parse", "HEAD^{tree}")
	if e != nil || strings.TrimSpace(string(b)) != p.Tree {
		return "", errors.New("materialized original HEAD/tree mismatch")
	}
	if _, e = verify(ctx, c, get); e != nil {
		return "", e
	}
	return digest(raw), nil
}
