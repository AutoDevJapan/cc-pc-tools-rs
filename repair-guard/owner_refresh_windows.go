package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type ownerRefreshClaim struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Catalog    string `json:"catalog"`
	Created    int64  `json:"created"`
}
type ownerRefreshFailure struct {
	Domain     string `json:"domain"`
	Repository string `json:"repository"`
	Catalog    string `json:"catalog"`
	Failed     int64  `json:"failed"`
}

func ownerRefreshFailureName(repository string) string {
	return "failed-" + hashBytes([]byte(strings.ToLower(repository)+"\n"+hashBytes(ownerDepartmentCatalog))) + ".dpapi"
}
func ownerRefreshCooling(home, repository string, now time.Time) bool {
	path := filepath.Join(home, ownerRefreshFailureName(repository))
	if !currentPlain(path, false) {
		return false
	}
	sealed, err := readBounded(path, 8192)
	if err != nil {
		return false
	}
	plain, ok := ownerIdentityDPAPI(sealed, false)
	var f ownerRefreshFailure
	return ok && json.Unmarshal(plain, &f) == nil && f.Domain == "owner-source-refresh-failure-v1" && strings.EqualFold(f.Repository, repository) && f.Catalog == hashBytes(ownerDepartmentCatalog) && f.Failed <= now.Unix() && now.Unix()-f.Failed < 300
}
func ownerReadRefreshClaim(home string) (ownerRefreshClaim, bool) {
	var c ownerRefreshClaim
	path := filepath.Join(home, "refresh.pending")
	if !currentPlain(path, false) {
		return c, false
	}
	b, err := readBounded(path, 4096)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, false
	}
	return c, len(c.ID) == 32 && conversationSHA.MatchString(c.Catalog) && c.Created > 0
}

// Delete the opened claim, never a newer claim rebound to the same pathname.
// The brief metadata handle excludes replacement until disposition and close.
func ownerDropRefreshClaim(home, expected string, now time.Time) bool {
	path := filepath.Join(home, "refresh.pending")
	if !currentPlain(path, false) {
		return false
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|0x10000, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
	if err != nil {
		return false
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !plainEntry(info) {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 {
		return false
	}
	var claim ownerRefreshClaim
	valid := json.Unmarshal(b, &claim) == nil && len(claim.ID) == 32 && claim.Created > 0
	if expected != "" {
		if !valid || claim.ID != expected {
			return false
		}
	} else {
		// Recover interrupted creation only in this program's fixed metadata
		// leaf after the finite stale interval. Time grants no source authority.
		created := info.ModTime()
		if valid {
			created = time.Unix(0, claim.Created)
		}
		if now.Sub(created) < 20*time.Second {
			return false
		}
	}
	deleteFile := byte(1)
	ok, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("SetFileInformationByHandle").Call(uintptr(h), 4, uintptr(unsafe.Pointer(&deleteFile)), 1)
	return ok != 0
}
func ownerClaimRefresh(home, repository string, now time.Time) (ownerRefreshClaim, bool) {
	if ownerRefreshCooling(home, repository, now) {
		return ownerRefreshClaim{}, false
	}
	if os.MkdirAll(home, 0700) != nil || !currentPlain(home, true) {
		return ownerRefreshClaim{}, false
	}
	path := filepath.Join(home, "refresh.pending")
	if _, err := os.Lstat(path); err == nil {
		if !ownerDropRefreshClaim(home, "", now) {
			return ownerRefreshClaim{}, false
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ownerRefreshClaim{}, false
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		_ = f.Close()
		return ownerRefreshClaim{}, false
	}
	c := ownerRefreshClaim{hex.EncodeToString(random), repository, hashBytes(ownerDepartmentCatalog), now.UnixNano()}
	b, _ := json.Marshal(c)
	_, err = f.Write(b)
	closed := f.Close()
	if err != nil || closed != nil {
		return ownerRefreshClaim{}, false
	}
	return c, true
}

// One finite helper. Failed identities have a per-repository/catalogue cooldown;
// ordinary retries cannot create a continuous process or network request loop.
func ownerRequestIdentityRefresh(path, repository string) {
	executable, err := os.Executable()
	if err != nil || filepath.Ext(executable) != ".exe" || filepath.Base(executable) == "repair-guard.test.exe" {
		return
	}
	c, ok := ownerClaimRefresh(ownerIdentityHome, repository, time.Now())
	if !ok {
		return
	}
	cmd := exec.Command(executable, "--refresh-owner-source", path, "--refresh-request", c.ID)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if cmd.Start() != nil {
		_ = ownerWriteSealed(ownerIdentityHome, ownerRefreshFailureName(repository), ownerRefreshFailure{"owner-source-refresh-failure-v1", repository, c.Catalog, time.Now().Unix()})
		_ = ownerDropRefreshClaim(ownerIdentityHome, c.ID, time.Now())
		return
	}
	_ = cmd.Process.Release()
}
func ownerRefreshIdentity(path string) bool { return ownerRefreshIdentityRequest(path, "") }
func ownerRefreshIdentityRequest(path, request string) bool {
	var claim ownerRefreshClaim
	if request != "" {
		var ok bool
		claim, ok = ownerReadRefreshClaim(ownerIdentityHome)
		if !ok || claim.ID != request || claim.Catalog != hashBytes(ownerDepartmentCatalog) {
			return false
		}
		defer func() { _ = ownerDropRefreshClaim(ownerIdentityHome, request, time.Now()) }()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	attempted := claim.Repository
	identityReady := false
	access := ownerSourceAccess{git: func(root string, args ...string) (string, bool) { return ownerSourceGit(ctx, root, args...) }, identity: func(repo string) (int64, string, string, bool) {
		if claim.Repository != "" && !strings.EqualFold(claim.Repository, repo) {
			return 0, "", "", false
		}
		attempted = repo
		if ownerRefreshCooling(ownerIdentityHome, repo, time.Now()) {
			id, name, owner, ok := ownerCachedIdentity(ctx, repo)
			identityReady = identityReady || ok
			return id, name, owner, ok
		}
		id, name, owner, ok := ownerCachedIdentityAt(ctx, repo, ownerIdentityHome, time.Now, ownerGitHubIdentityWith, ownerIdentityAging(repo, ownerIdentityHome, time.Now()))
		identityReady = identityReady || ok
		return id, name, owner, ok
	}, plain: currentPlain, denied: legacyMatch, membership: ownerDepartmentMembership}
	_ = ownerSourceQuery(ctx, path, access)
	// Updating an API identity is distinct from admitting the source path. A
	// concurrent ordinary ToDo write may invalidate the path snapshot without
	// turning the already verified repository identity into a network failure.
	// The next normal guard still rechecks its current registration and stop.
	success := identityReady && ctx.Err() == nil
	if !success && attempted != "" {
		_ = ownerWriteSealed(ownerIdentityHome, ownerRefreshFailureName(attempted), ownerRefreshFailure{"owner-source-refresh-failure-v1", attempted, hashBytes(ownerDepartmentCatalog), time.Now().Unix()})
	}
	return success
}
