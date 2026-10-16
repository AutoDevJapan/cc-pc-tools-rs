package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func auditLinux(ctx context.Context, file string) error {
	s, e := load(file)
	if e != nil {
		return e
	}
	c, e := pick(s.RepoID)
	if e != nil {
		return e
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	b, e := shell(ctx, s, "git write-tree")
	if e != nil || strings.TrimSpace(string(b)) != s.Tree {
		return errors.New("source index tree changed")
	}
	b, e = output(ctx, "exec", "--workdir", "/work/source", s.Container, "git", "ls-files", "--stage", "-z")
	if e != nil {
		return e
	}
	var paths, wants []string
	for _, entry := range strings.Split(string(b), "\x00") {
		if entry == "" {
			continue
		}
		meta, p, ok := strings.Cut(entry, "\t")
		if !ok {
			return errors.New("source index invalid")
		}
		fields := strings.Fields(meta)
		if fields[0] == "160000" {
			continue
		}
		if strings.ContainsAny(p, "\r\n") {
			return errors.New("source path unsupported")
		}
		paths = append(paths, p)
		wants = append(wants, fields[1])
	}
	x := cmd(ctx, docker, "exec", "-i", "--workdir", "/work/source", s.Container, "git", "hash-object", "--no-filters", "--stdin-paths")
	x.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	b, e = x.Output()
	if e != nil {
		return errors.New("final source hashing failed")
	}
	got := strings.Fields(string(b))
	if len(got) != len(wants) {
		return errors.New("source audit count mismatch")
	}
	for i, sha := range got {
		if sha != wants[i] {
			return errors.New("fixed source bytes changed")
		}
	}
	s.SourceUnchanged = true
	return save(s)
}
func disposeLinux(ctx context.Context, file string) error {
	s, e := load(file)
	if e != nil {
		return e
	}
	if !s.SourceUnchanged {
		return errors.New("source audit required before disposal")
	}
	if s.ContainerRemoved {
		return errors.New("container already disposed; no repeat")
	}
	var all []struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
		State struct {
			Running bool
			Pid     int
		}
	}
	inspect := func() error {
		b, err := output(ctx, "inspect", s.Container)
		if err != nil || json.Unmarshal(b, &all) != nil || len(all) != 1 || all[0].ID != s.Container || all[0].Name != "/"+s.Name || all[0].Config.Image != sdk || all[0].Config.Labels["cc.todo"] != "2730" || all[0].Config.Labels["cc.instance"] != s.Name {
			return errors.New("owned container identity mismatch")
		}
		return nil
	}
	if e = inspect(); e != nil {
		return e
	}
	if _, e = output(ctx, "stop", "--time", "10", s.Container); e != nil {
		return errors.New("owned container stop failed")
	}
	if e = inspect(); e != nil {
		return e
	}
	if all[0].State.Running || all[0].State.Pid != 0 {
		return errors.New("container kernel tree absence unverified")
	}
	s.TreeGone = true
	if e = save(s); e != nil {
		return e
	}
	if _, e = output(ctx, "rm", "-v", s.Container); e != nil {
		return errors.New("owned container/volume removal failed")
	}
	s.ContainerRemoved = true
	if e = save(s); e != nil {
		return e
	}
	if e = os.Remove(filepath.Join(os.TempDir(), fmt.Sprintf("todo2648-repo-%d.lock", s.RepoID))); e != nil {
		return errors.New("candidate reservation release failed")
	}
	return writeJSON(filepath.Join(s.Root, fmt.Sprintf("repo%d-cleanup.private.json", s.RepoID)), map[string]any{"repo_id": s.RepoID, "source_sha": s.SHA, "source_tree": s.Tree, "unchanged_source_verified": true, "owned_container": s.Container, "kernel_pid_zero": true, "container_and_anonymous_volumes_removed": true, "original_sdk_image_preserved": true, "at": time.Now().UTC()})
}
