package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type OwnedRec struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// SafeDelete is the owned-root lifecycle of D:/CC/Tools/safe-delete: a new
// record is created (never an existing folder registered), the plan is
// checked, and finish/quarantine closes it.
type SafeDelete interface {
	Create(ctx context.Context, project, content string) (OwnedRec, error)
	Plan(ctx context.Context, id string) (string, error)
	Finish(ctx context.Context, id string) error
	Quarantine(ctx context.Context, id, reason string) error
}

type fixedSafeDelete struct {
	Exe Pinned
	Run RunFunc
}

func (s fixedSafeDelete) call(ctx context.Context, args ...string) ([]byte, error) {
	if e := s.Exe.Verify(); e != nil {
		return nil, e
	}
	return s.Run(ctx, s.Exe.Path, nil, 1<<20, args...)
}

func (s fixedSafeDelete) Create(ctx context.Context, project, content string) (OwnedRec, error) {
	out, e := s.call(ctx, "create", "--project", project, "--content", content)
	var r struct {
		ID    string `json:"id"`
		Path  string `json:"path"`
		State string `json:"state"`
	}
	if e != nil || json.Unmarshal(out, &r) != nil || r.State != "owned" || !hex32.MatchString(r.ID) || !absClean(r.Path) || filepath.Base(r.Path) != "codex-owned-"+r.ID {
		return OwnedRec{}, errors.New("owned root creation unconfirmed")
	}
	return OwnedRec{ID: r.ID, Path: r.Path}, nil
}

func (s fixedSafeDelete) Plan(ctx context.Context, id string) (string, error) {
	if !hex32.MatchString(id) {
		return "", errors.New("owned id rejected")
	}
	out, e := s.call(ctx, "plan", "--id", id)
	var r struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if e != nil || json.Unmarshal(out, &r) != nil || r.ID != id || !absClean(r.Path) {
		return "", errors.New("deletion plan unavailable")
	}
	return r.Path, nil
}

func (s fixedSafeDelete) Finish(ctx context.Context, id string) error {
	if !hex32.MatchString(id) {
		return errors.New("owned id rejected")
	}
	_, e := s.call(ctx, "finish", "--id", id)
	return e
}

func (s fixedSafeDelete) Quarantine(ctx context.Context, id, reason string) error {
	if !hex32.MatchString(id) || (reason != "policy" && reason != "windows" && reason != "locked") {
		return errors.New("quarantine request rejected")
	}
	_, e := s.call(ctx, "quarantine", "--id", id, "--reason", reason)
	return e
}

// removeOwned removes exactly the planned owned root. It refuses links and
// reparse points anywhere below the root rather than following them, and it
// never uses a forced/recursive shell deletion.
func removeOwned(root, planned string) error {
	if root != planned || !absClean(root) || !strings.HasPrefix(filepath.Base(root), "codex-owned-") {
		return errors.New("deletion target is not the recorded owned root")
	}
	st, e := os.Lstat(root)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil || !st.IsDir() || st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return errors.New("owned root is not a plain directory")
	}
	var bad bool
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			bad = true
			return filepath.SkipDir
		}
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			bad = true
			return filepath.SkipDir
		}
		return nil
	})
	if bad {
		return errors.New("owned root contains a link or unreadable entry")
	}
	return os.RemoveAll(root)
}
