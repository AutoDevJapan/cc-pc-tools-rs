package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const sdk = "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011"

type state struct {
	RepoID                                       int64
	PR                                           int
	SHA, Tree, ArchiveSHA, Name, Container, Root string
	Retrieved, TreeVerified, BeforeRun, AfterRun bool
}

func save(s state) error {
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(s.Root, fmt.Sprintf("repo%d.private.json", s.RepoID)), b, 0600)
}
func load(file string) (s state, e error) {
	b, e := os.ReadFile(file)
	if e == nil {
		e = json.Unmarshal(b, &s)
	}
	return
}
func pick(id int64) (candidate, error) {
	for _, c := range candidates[1:] {
		if c.ID == id {
			return c, nil
		}
	}
	return candidate{}, errors.New("candidate rejected")
}
func output(ctx context.Context, args ...string) ([]byte, error) {
	return boundedOutput(cmd(ctx, docker, args...), 128<<20)
}
func shell(ctx context.Context, s state, script string) ([]byte, error) {
	return output(ctx, "exec", "--workdir", "/work/source", s.Container, "/bin/sh", "-c", script)
}
func prepare(ctx context.Context, id int64, root string) error {
	c, e := pick(id)
	if e != nil {
		return e
	}
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	s := state{RepoID: id, PR: c.PR, SHA: c.SHA, Root: root, Name: fmt.Sprintf("todo2710-repo%d-%d", id, time.Now().UnixNano())}
	if _, e = os.Stat(filepath.Join(root, fmt.Sprintf("repo%d.private.json", id))); !os.IsNotExist(e) {
		return errors.New("receipt exists")
	}
	// A crash leaves this reservation behind rather than permitting a duplicate.
	lock, e := os.OpenFile(filepath.Join(os.TempDir(), fmt.Sprintf("todo2648-repo-%d.lock", id)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("candidate occupied")
	}
	fmt.Fprintln(lock, s.Name)
	lock.Close()
	if e = save(s); e != nil {
		return e
	}
	var commit struct{ Tree struct{ SHA string } }
	if e = get(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil {
		return e
	}
	s.Tree = commit.Tree.SHA
	// Only this new container receives the archive; it is never extracted on host.
	raw, e := boundedOutput(cmd(ctx, gh, "api", "repos/"+r.FullName+"/tarball/"+c.SHA), 128<<20)
	if e != nil {
		return errors.New("download failed")
	}
	s.ArchiveSHA = digest(raw)
	archive, e := opaqueArchive(raw)
	if e != nil {
		return e
	}
	s.Retrieved = true
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	if e = save(s); e != nil {
		return e
	}
	b, e := output(ctx, "create", "--name", s.Name, "--label", "cc.todo=2710", "--label", "cc.instance="+s.Name, "--init", "--user", "1001:1001", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--read-only", "--network", "bridge", "--cpus", "8", "--memory", "16g", "--pids-limit", "1024", "--mount", "type=volume,destination=/work", "--tmpfs", "/tmp:rw,nosuid,noexec,size=1g,uid=1001,gid=1001", "--workdir", "/work", sdk, "sleep", "infinity")
	if e != nil {
		return errors.New("create uncertain; retain intent and lock")
	}
	s.Container = strings.TrimSpace(string(b))
	if len(s.Container) != 64 {
		return errors.New("container ID rejected")
	}
	if e = save(s); e != nil {
		return e
	}
	if _, e = output(ctx, "start", s.Container); e != nil {
		return errors.New("start failed")
	}
	// The image owns /work as UID1001; the new anonymous volume preserves it.
	if _, e = output(ctx, "exec", s.Container, "sh", "-c", "mkdir -p /work/source /work/evidence /work/cargo /work/target && test ! -e /var/run/docker.sock"); e != nil {
		return errors.New("scratch setup failed")
	}
	x := cmd(ctx, docker, "exec", "-i", s.Container, "tar", "xf", "-", "-C", "/work/source")
	x.Stdin = bytes.NewReader(archive)
	if e = x.Run(); e != nil {
		return errors.New("source transfer failed")
	}
	b, e = shell(ctx, s, "git init -q && git config core.autocrlf false && git add -f . && git write-tree")
	if e != nil || strings.TrimSpace(string(b)) != s.Tree {
		return errors.New("tree mismatch")
	}
	s.TreeVerified = true
	// Preserve selected original instructions and workflow bytes privately.
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Name == "AGENTS.md" || h.Name == "CLAUDE.md" || strings.HasPrefix(h.Name, ".github/workflows/") {
			data, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			name := strings.ReplaceAll(h.Name, "/", "_")
			if err = os.WriteFile(filepath.Join(root, fmt.Sprintf("repo%d-%s.private.txt", id, name)), data, 0600); err != nil {
				return err
			}
		}
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	return save(s)
}
func transfer(ctx context.Context, s state, local, remote string) error {
	b, e := os.ReadFile(local)
	if e != nil {
		return e
	}
	if strings.HasSuffix(local, ".sh") {
		b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	}
	x := cmd(ctx, docker, "exec", "-i", s.Container, "sh", "-c", "cat > "+remote+" && chmod 700 "+remote)
	x.Stdin = bytes.NewReader(b)
	if e = x.Run(); e != nil {
		return errors.New("helper transfer failed")
	}
	return nil
}
func gateChild() int {
	s, e := load(os.Getenv("CI2710_STATE"))
	if e != nil {
		return 120
	}
	if os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" {
		return 121
	}
	jobs, e := strconv.Atoi(os.Getenv("CARGO_BUILD_JOBS"))
	if e != nil || jobs < 1 || jobs > 10 {
		return 122
	}
	tests, e := strconv.Atoi(os.Getenv("RUST_TEST_THREADS"))
	if e != nil || tests < 1 || tests > 10 {
		return 123
	}
	x := cmd(context.Background(), docker, "exec", "-i", s.Container, "/work/supervisor", "/work/plan.sh", strconv.Itoa(jobs), strconv.Itoa(tests))
	x.Stdin = os.Stdin
	x.Stdout = os.Stdout
	x.Stderr = os.Stderr
	if e = x.Run(); e != nil {
		return 1
	}
	return 0
}
func run(ctx context.Context, file, supervisor, script string) error {
	s, e := load(file)
	if e != nil {
		return e
	}
	c, e := pick(s.RepoID)
	if e != nil {
		return e
	}
	if !s.TreeVerified {
		return errors.New("tree unverified")
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	s.BeforeRun = true
	if e = save(s); e != nil {
		return e
	}
	if e = transfer(ctx, s, supervisor, "/work/supervisor"); e != nil {
		return e
	}
	if e = transfer(ctx, s, script, "/work/plan.sh"); e != nil {
		return e
	}
	exe, _ := os.Executable()
	x := cmd(context.Background(), `D:\CC\Tools\codex-jobs\cargo_gate.exe`, "test", "--target-dir", filepath.Join(s.Root, fmt.Sprintf("target-repo%d", s.RepoID)))
	x.Env = nil
	for _, k := range []string{"SystemRoot", "WINDIR", "SystemDrive", "TEMP", "TMP", "PATH", "USERPROFILE", "LOCALAPPDATA", "CC_JOB_ID"} {
		if v := os.Getenv(k); v != "" {
			x.Env = append(x.Env, k+"="+v)
		}
	}
	x.Env = append(x.Env, "CODEX_JOBS_REAL_CARGO="+exe, "CI2710_STATE="+file, "CI2710_CHILD=1", "CODEX_JOBS_CARGO_LOCK_DIR=D:/CC/Tools/codex-jobs")
	log, e := os.OpenFile(filepath.Join(s.Root, fmt.Sprintf("repo%d-gate-%d.log", s.RepoID, time.Now().UnixNano())), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer log.Close()
	in, e := x.StdinPipe()
	if e != nil {
		return e
	}
	x.Stdout = log
	x.Stderr = log
	// Keep EOF cancellation connected throughout the gate and Docker supervisor.
	if e = x.Start(); e != nil {
		return errors.New("gate start failed")
	}
	done := make(chan error, 1)
	go func() { done <- x.Wait() }()
	select {
	case e = <-done:
	case <-ctx.Done():
		in.Close()
		e = <-done
	}
	in.Close()
	if _, err := verify(context.Background(), c, get); err != nil {
		return errors.Join(errors.New("post identity failed"), e)
	}
	s.AfterRun = true
	if err := save(s); err != nil {
		return err
	}
	return e
}
func main() {
	if os.Getenv("CI2710_CHILD") == "1" {
		os.Exit(gateChild())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	var e error
	a := os.Args[1:]
	switch {
	case len(a) == 2 && a[0] == "sdk-sccache":
		var asset struct{ Digest string }
		e = get(ctx, "repos/mozilla/sccache/releases/assets/563130201", &asset)
		if e == nil && asset.Digest != "sha256:45f1447fbe231e3037bde351ef70677dd212216c8d62ae7ca409fecc4d6acc89" {
			e = errors.New("SDK digest changed")
		}
		if e == nil {
			var raw []byte
			raw, e = boundedOutput(cmd(ctx, gh, "api", "repos/mozilla/sccache/releases/assets/563130201", "-H", "Accept: application/octet-stream"), 64<<20)
			if e == nil && "sha256:"+digest(raw) != asset.Digest {
				e = errors.New("SDK bytes mismatch")
			}
			if e == nil {
				var z *gzip.Reader
				z, e = gzip.NewReader(bytes.NewReader(raw))
				if e == nil {
					tr := tar.NewReader(z)
					found := false
					for {
						h, err := tr.Next()
						if err == io.EOF {
							break
						}
						if err != nil {
							e = err
							break
						}
						if h.Typeflag == tar.TypeReg && strings.HasSuffix(h.Name, "/sccache") {
							var b []byte
							b, e = io.ReadAll(tr)
							if e == nil {
								e = os.WriteFile(filepath.Join(a[1], "sccache"), b, 0700)
							}
							found = true
							break
						}
					}
					z.Close()
					if !found && e == nil {
						e = errors.New("SDK executable absent")
					}
				}
			}
		}
	case len(a) == 1 && a[0] == "inspect":
		for _, c := range candidates[1:] {
			_, err := verify(ctx, c, get)
			fmt.Printf("repo%d fixed_head_verified=%t\n", c.ID, err == nil)
			e = errors.Join(e, err)
		}
	case len(a) == 3 && a[0] == "prepare":
		id, _ := strconv.ParseInt(a[1], 10, 64)
		e = prepare(ctx, id, a[2])
	case len(a) == 4 && a[0] == "run":
		e = run(ctx, a[1], a[2], a[3])
	case len(a) == 4 && a[0] == "upload":
		var s state
		s,e=load(a[1]);if e==nil{e=transfer(ctx,s,a[2],a[3])}
	case len(a) == 4 && a[0] == "cancel-smoke":
		short, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		e = run(short, a[1], a[2], a[3])
	default:
		e = errors.New("usage inspect | prepare ID owned-root | run state supervisor plan")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
