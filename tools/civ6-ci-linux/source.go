package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

// fetchWorkflow returns the exact workflow bytes of the commit the job runs
// (the PR merge context for pull requests) and requires the Plan's digest.
func fetchWorkflow(ctx context.Context, p Plan) ([]byte, error) {
	var v struct{ Type, Encoding, Content, SHA string }
	if e := githubRead(ctx, "repos/"+repoFull+"/contents/"+p.Workflow+"?ref="+p.WorkflowSHA, &v); e != nil {
		return nil, e
	}
	b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
	if e != nil || v.Type != "file" || v.Encoding != "base64" || len(b) > 1<<20 || gitObjectID("blob", b) != v.SHA {
		return nil, errors.New("workflow blob bytes rejected")
	}
	if digest(b) != p.WorkflowDigest {
		return nil, errors.New("workflow bytes differ from the Plan digest")
	}
	return b, nil
}

// Write only bytes that reproduce the real GitHub commit ID. No substitute
// commit, author or timezone is manufactured.
func originalCommitBytes(c originalCommit, expected string) ([]byte, error) {
	if c.Verification.Signature == "" && gitObjectID("commit", []byte(c.Verification.Payload)) == expected {
		return []byte(c.Verification.Payload), nil
	}
	a, e := time.Parse(time.RFC3339, c.Author.Date)
	if e != nil {
		return nil, errors.New("commit author timestamp unavailable")
	}
	b, e := time.Parse(time.RFC3339, c.Committer.Date)
	if e != nil {
		return nil, errors.New("commit committer timestamp unavailable")
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
	return nil, errors.New("commit bytes cannot be reproduced; no replacement written")
}

// tarballOverride lets fixtures supply a credential-free archive.
var tarballOverride func(ctx context.Context, sha string) ([]byte, error)

func fetchTarball(ctx context.Context, sha string) ([]byte, error) {
	if !isHex(sha, 40) {
		return nil, errors.New("exact commit required")
	}
	if tarballOverride != nil {
		return tarballOverride(ctx, sha)
	}
	b, e := boundedOutput(cmd(ctx, ghGate, "api", "repos/"+repoFull+"/tarball/"+sha), 128<<20)
	if e != nil {
		return nil, errors.New("fixed source archive unavailable")
	}
	return b, nil
}

// Convert GitHub's archive into an opaque-root archive before Docker stdin.
// Nothing from the repository is unpacked on the host.
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
		h.Uid, h.Gid = 1001, 1001
		h.Uname, h.Gname = "", ""
		h.PAXRecords, h.Xattrs = nil, nil
		h.Mode &= 0777 // never transfer setuid/setgid/sticky bits
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

// materializeSource writes the Plan's EXACT head commit into the namespace
// (stdin only, no host bind). Every blob ID, the index tree and the commit
// object are proven equal to GitHub's before any registration token exists.
// The PR merge context is separate: the checkout ref is always the actual head.
func materializeSource(ctx context.Context, p Plan, n *namespace) (string, error) {
	var commit originalCommit
	if e := githubRead(ctx, "repos/"+repoFull+"/git/commits/"+p.Head, &commit); e != nil {
		return "", e
	}
	if commit.Tree.SHA != p.Tree {
		return "", errors.New("materialized source tree changed")
	}
	payload, e := originalCommitBytes(commit, p.Head)
	if e != nil {
		return "", e
	}
	var tree struct {
		Truncated bool
		Entries   []struct{ Path, Mode, SHA, Type string } `json:"tree"`
	}
	if e = githubRead(ctx, "repos/"+repoFull+"/git/trees/"+p.Tree+"?recursive=1", &tree); e != nil {
		return "", e
	}
	if tree.Truncated {
		return "", errors.New("complete Git tree unavailable")
	}
	raw, e := fetchTarball(ctx, p.Head)
	if e != nil {
		return "", e
	}
	archive, e := opaqueArchive(raw)
	if e != nil {
		return "", e
	}
	if e = cmd(ctx, dockerExe, "exec", n.ID, "mkdir", "-p", workspace, "/runner/empty-template").Run(); e != nil {
		return "", errors.New("owned checkout directory creation failed")
	}
	transfer := cmd(ctx, dockerExe, "exec", "-i", n.ID, "tar", "-xf", "-", "-C", workspace)
	transfer.Stdin = bytes.NewReader(archive)
	if e = transfer.Run(); e != nil {
		return "", errors.New("fixed source archive transfer failed")
	}
	git := func(input []byte, args ...string) ([]byte, error) {
		x := cmd(ctx, dockerExe, append([]string{"exec", "-i", "-e", "GIT_CONFIG_NOSYSTEM=1", "-e", "GIT_CONFIG_GLOBAL=/dev/null", "--workdir", workspace, n.ID, "git", "-c", "core.autocrlf=false", "-c", "core.filemode=true"}, args...)...)
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
		if entry.Path == "" || path.Clean(entry.Path) != entry.Path || strings.HasPrefix(entry.Path, "/") || strings.HasPrefix(entry.Path, ".git/") || entry.Path == ".git" || strings.ContainsAny(entry.Path, "\\\r\n\x00") || !isHex(entry.SHA, 40) {
			return "", errors.New("tracked source entry rejected")
		}
		if entry.Mode == "160000" && entry.Type == "commit" {
			// A submodule is neither fetched nor executed.
		} else if entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755") {
			paths = append(paths, workspace+"/"+entry.Path)
			expectedBlobs = append(expectedBlobs, entry.SHA)
		} else {
			return "", errors.New("unsupported tracked source mode")
		}
		fmt.Fprintf(&index, "%s %s\t%s%c", entry.Mode, entry.SHA, entry.Path, byte(0))
	}
	if len(paths) == 0 {
		return "", errors.New("source contains no blobs")
	}
	// Hash the actual archive bytes without filters (a fresh git add would drop
	// ignored tracked files and apply .gitattributes), then restore the API's
	// exact index entries and modes.
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
		return "", errors.New("tracked source index restoration failed")
	}
	b, e := git(nil, "write-tree")
	if e != nil || strings.TrimSpace(string(b)) != p.Tree {
		return "", errors.New("actual source index tree differs from GitHub")
	}
	b, e = git(payload, "hash-object", "-t", "commit", "-w", "--stdin")
	if e != nil || strings.TrimSpace(string(b)) != p.Head {
		return "", errors.New("commit object transfer failed")
	}
	if e = (&namespace{ID: n.ID}).execStdinIn(ctx, workspace, []byte(p.Head+"\n"), "cat > .git/shallow"); e != nil {
		return "", errors.New("shallow boundary transfer failed")
	}
	if _, e = git(nil, "update-ref", "--no-deref", "HEAD", p.Head); e != nil {
		return "", errors.New("fixed detached HEAD unavailable")
	}
	b, e = git(nil, "rev-parse", "HEAD^{tree}")
	if e != nil || strings.TrimSpace(string(b)) != p.Tree {
		return "", errors.New("materialized HEAD/tree mismatch")
	}
	return digest(raw), nil
}

func (n *namespace) execStdinIn(ctx context.Context, dir string, data []byte, script string) error {
	c := cmd(ctx, dockerExe, "exec", "-i", "--workdir", dir, n.ID, "sh", "-c", script)
	c.Stdin = bytes.NewReader(data)
	return c.Run()
}
