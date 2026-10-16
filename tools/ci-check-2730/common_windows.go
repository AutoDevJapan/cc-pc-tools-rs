package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

const gh = `D:\CC\Tools\codex-jobs\gh_gate.exe`
const docker = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`

type candidate struct {
	ID  int64
	PR  int
	SHA string
}

var candidates = []candidate{
	{1285649124, 1002, "ca3ffe285ca51c6d49e6797fe589566a6bd0a416"},
	{1392152644, 148, "75a4051305b6f47c70ddb59a8d2bf9015666f12c"},
}

type repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Private, Fork bool
	Owner         struct{ Login string }
	Permissions   struct{ Admin bool }
}
type pull struct {
	State string
	Head  struct {
		SHA, Ref string
		Repo     repository
	}
	Base struct{ Repo repository }
}
type api func(context.Context, string, any) error

func cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	if name == docker {
		args = append([]string{"--host", "npipe:////./pipe/dockerDesktopLinuxEngine"}, args...)
	}
	c := exec.CommandContext(ctx, name, args...)
	hidden(c)
	c.WaitDelay = 5 * time.Second
	if name == docker {
		c.Env = localDockerEnv(os.Environ())
	}
	return c
}

func localDockerEnv(env []string) []string {
	var out []string
	for _, s := range env {
		key, _, _ := strings.Cut(s, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "DOCKER_") {
			out = append(out, s)
		}
	}
	return out
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("response size exceeded")
	}
	return b.Buffer.Write(p)
}
func boundedOutput(c *exec.Cmd, limit int) ([]byte, error) {
	b := boundedBuffer{limit: limit}
	c.Stdout = &b
	if e := c.Run(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}

// Command errors deliberately omit argv and stderr: both can identify a client.
func get(ctx context.Context, endpoint string, value any) error {
	b, e := boundedOutput(cmd(ctx, gh, "api", endpoint), 16<<20)
	if e != nil {
		return errors.New("GitHub read failed")
	}
	if json.Unmarshal(b, value) != nil {
		return errors.New("GitHub response invalid")
	}
	return nil
}
func verify(ctx context.Context, c candidate, read api) (repository, error) {
	if c.ID != 1392152644 || c.PR != 148 || c.SHA != fixedSHA {
		return repository{}, errors.New("candidate outside finite check")
	}
	var r repository
	if e := read(ctx, fmt.Sprintf("repositories/%d", c.ID), &r); e != nil {
		return r, e
	}
	if r.ID != c.ID || r.Owner.Login != "AutoDevJapan" || !r.Private || r.Fork || !r.Permissions.Admin ||
		(r.Owner.Login != "AutoDevJapan" && r.Owner.Login != "BM1314" && r.Owner.Login != "mashfromband") ||
		!strings.HasPrefix(r.FullName, r.Owner.Login+"/") || strings.Count(r.FullName, "/") != 1 {
		return repository{}, errors.New("repository identity rejected")
	}
	var p pull
	if e := read(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, c.PR), &p); e != nil {
		return repository{}, e
	}
	if p.State != "open" || p.Head.Ref != "codex/todo2640-workspace-doctests-20261015" || p.Head.SHA != c.SHA || p.Head.Repo.ID != c.ID || p.Base.Repo.ID != c.ID ||
		p.Head.Repo.FullName != r.FullName || p.Base.Repo.FullName != r.FullName || p.Head.Repo.Fork {
		return repository{}, errors.New("fixed same-repository PR head rejected")
	}
	var ref struct{ Object struct{ SHA string } }
	// The PR API and branch API must agree; the fixed revision never follows a ref.
	if e := read(ctx, "repos/"+r.FullName+"/git/ref/heads/"+url.PathEscape(p.Head.Ref), &ref); e != nil {
		return repository{}, e
	}
	if ref.Object.SHA != c.SHA {
		return repository{}, errors.New("branch head changed")
	}
	if e := verifyDelta(ctx, r.FullName, read); e != nil {
		return repository{}, e
	}
	return r, nil
}

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
		// GitHub archives begin with a global PAX metadata record. It is not
		// part of the source root or an extracted filesystem entry.
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
		h.Uid = 1001
		h.Gid = 1001
		h.Uname = ""
		h.Gname = ""
		h.PAXRecords = nil
		h.Xattrs = nil
		h.Mode &= 0777 // Never transfer setuid/setgid/sticky bits.
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
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
