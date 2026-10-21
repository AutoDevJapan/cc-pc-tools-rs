package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// One bounded metadata helper, not a resident or a polling loop. Unknown stays
// denied during refresh. Existing callers' next normal pass sees the fresh ID.
func ownerRequestIdentityRefresh(path string) {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	if filepath.Ext(executable) != ".exe" || filepath.Base(executable) == "repair-guard.test.exe" {
		return
	}
	if os.MkdirAll(ownerIdentityHome, 0700) != nil || !currentPlain(ownerIdentityHome, true) {
		return
	}
	marker := filepath.Join(ownerIdentityHome, "refresh.pending")
	if info, err := os.Stat(marker); err == nil {
		age := time.Since(info.ModTime())
		if age < 20*time.Second {
			return
		}
		if !currentPlain(marker, false) {
			return
		}
		_ = os.Remove(marker)
	}
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	_ = f.Close()
	cmd := exec.Command(executable, "--refresh-owner-source", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if cmd.Start() != nil {
		_ = os.Remove(marker)
		return
	}
	_ = cmd.Process.Release()
}

func ownerRefreshIdentity(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	defer os.Remove(filepath.Join(ownerIdentityHome, "refresh.pending"))
	access := ownerSourceAccess{git: func(root string, args ...string) (string, bool) { return ownerSourceGit(ctx, root, args...) }, identity: func(repo string) (int64, string, string, bool) {
		return ownerCachedIdentityAt(ctx, repo, ownerIdentityHome, time.Now, ownerGitHubIdentityWith)
	}, plain: currentPlain, denied: legacyMatch, membership: ownerDepartmentMembership}
	result := ownerRegisteredSourceAt(ctx, path, func(ctx context.Context) (object, bool) { return ownerReadProjects(ctx, productStore) }, access)
	return result.AccessAllowed && ctx.Err() == nil
}
