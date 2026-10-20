package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const diagnosticBudget int64 = 64 << 20
const diagnosticCount = 8
const cycleOwner = "cc-pc-tools-linux-bridge/todo3026/v1\n"

func diagnosticEnded(path string) (bool, error) {
	for _, name := range []string{"owner", "complete", "interrupted"} {
		info, err := os.Lstat(filepath.Join(path, name))
		if err == nil && !info.Mode().IsRegular() {
			return false, errors.New("diagnostic marker is not a regular file")
		}
		if err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	owner, err := os.ReadFile(filepath.Join(path, "owner"))
	if err != nil || string(owner) != cycleOwner {
		return false, errors.New("diagnostic owner rejected")
	}
	ended := false
	for _, name := range []string{"complete", "interrupted"} {
		marker, err := os.ReadFile(filepath.Join(path, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if string(marker) != cycleOwner || ended {
			return false, errors.New("diagnostic end marker rejected")
		}
		ended = true
	}
	return ended, nil
}

// Only cycles created by this supervisor and marked complete after process-group
// drain are eligible. Unfinished cycles and the original ToDo3024 archive are
// never retention candidates. No caller-supplied path enters the live command.
func retainDiagnostics(root string, budget int64, count int) error {
	if budget < 0 || count < 0 {
		return errors.New("negative diagnostic retention limit")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	type cycle struct {
		path, name string
		bytes      int64
	}
	var completed []cycle
	var total int64
	var unfinishedBytes int64
	unfinishedCount := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "cycle-") || !entry.IsDir() {
			return errors.New("unexpected diagnostic entry; retaining evidence")
		}
		path := filepath.Join(root, entry.Name())
		ended, err := diagnosticEnded(path)
		if err != nil {
			return err
		}
		var size int64
		err = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("diagnostic link rejected")
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return errors.New("diagnostic special file rejected")
			}
			if info.Mode().IsRegular() {
				size += info.Size()
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !ended {
			// Preserve interrupted data, but refuse new sessions when its
			// finite allowance is exhausted. Never grow evidence indefinitely.
			unfinishedCount++
			unfinishedBytes += size
			continue
		}
		completed = append(completed, cycle{path, entry.Name(), size})
		total += size
	}
	if unfinishedCount > diagnosticCount || unfinishedBytes > diagnosticBudget {
		return errors.New("unfinished diagnostic allowance exhausted; retaining evidence")
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].name < completed[j].name })
	for len(completed) > count || total > budget {
		oldest := completed[0]
		if err := os.RemoveAll(oldest.path); err != nil {
			return err
		}
		total -= oldest.bytes
		completed = completed[1:]
	}
	return nil
}
