package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

// Only metadata absence is inspected for authentication files; never read them.
func checkUnstartedFiles(root, runner string) error {
	for _, path := range []string{
		filepath.Join(root, "runtime", "started.json"),
		filepath.Join(root, "runtime", "service.json"),
		filepath.Join(root, "runtime", "worker.json"),
		filepath.Join(root, "runtime", "hook-start.json"),
		filepath.Join(root, "runtime", "verify-source.json"),
		filepath.Join(root, "runtime", "hook-completed.json"),
		filepath.Join(runner, ".runner"), filepath.Join(runner, ".credentials"), filepath.Join(runner, ".credentials_rsaparams"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return errors.New("worker or authentication artifact present or uncertain")
		}
	}
	path := filepath.Join(root, "launcher-result.json")
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != 2 {
		return errors.New("initial launcher report unavailable")
	}
	b, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(b, []byte("{}")) {
		return errors.New("launcher report is not initial")
	}
	return nil
}

func checkUnstartedJob(p Plan, j apiJob, steps int) error {
	neverAssigned := j.Status == "queued" || (j.Status == "completed" && j.Conclusion == "cancelled")
	if checkJob(p, j, false) != nil || !neverAssigned || j.Runner != "" || steps != 0 {
		return errors.New("official target job has begun or differs")
	}
	return nil
}
