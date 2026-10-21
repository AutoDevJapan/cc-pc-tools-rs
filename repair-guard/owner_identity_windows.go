package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const ownerIdentityHome = "D:/CC/Tools/client-records/source-identities-v1"

type ownerIdentityRecord struct {
	Domain     string `json:"domain"`
	Catalog    string `json:"catalog"`
	Repository string `json:"repository"`
	Owner      string `json:"owner"`
	ID         int64  `json:"id"`
	Checked    int64  `json:"checked"`
}

func ownerIdentityValid(record ownerIdentityRecord, repository string, now time.Time) bool {
	return record.Domain == "owner-source-identity-v1" && record.Catalog == hashBytes(ownerDepartmentCatalog) && strings.EqualFold(record.Repository, repository) && record.ID > 0 && ownerSelfGitHubOwner(record.Owner) && strings.EqualFold(strings.Split(repository, "/")[0], record.Owner) && record.Checked <= now.Unix() && now.Unix()-record.Checked < 300
}

func ownerCachedIdentity(ctx context.Context, repository string) (int64, string, string, bool) {
	// Ordinary classification never waits on the network. A missing identity
	// requests one finite metadata refresh after local qualification succeeds.
	return ownerCachedIdentityAt(ctx, repository, ownerIdentityHome, time.Now, func(context.Context, string) (int64, string, string, bool) { return 0, "", "", false })
}

func ownerCachedIdentityAt(ctx context.Context, repository, home string, now func() time.Time, fetch func(context.Context, string) (int64, string, string, bool)) (int64, string, string, bool) {
	if ctx.Err() != nil {
		return 0, "", "", false
	}
	if _, valid := ownerOriginRepository("https://github.com/" + repository); !valid {
		return 0, "", "", false
	}
	path := filepath.Join(home, hashBytes([]byte(strings.ToLower(repository)))+".dpapi")
	if currentPlain(path, false) {
		sealed, err := readBounded(path, 1<<20)
		if err == nil {
			if data, ok := ownerIdentityDPAPI(sealed, false); ok {
				var r ownerIdentityRecord
				if json.Unmarshal(data, &r) == nil && ownerIdentityValid(r, repository, now()) {
					return r.ID, r.Repository, r.Owner, true
				}
			}
		}
	}
	if ctx.Err() != nil {
		return 0, "", "", false
	}
	id, actual, owner, ok := fetch(ctx, repository)
	if !ok || !strings.EqualFold(actual, repository) || !ownerSelfGitHubOwner(owner) {
		return 0, "", "", false
	}
	r := ownerIdentityRecord{"owner-source-identity-v1", hashBytes(ownerDepartmentCatalog), actual, owner, id, now().Unix()}
	if !ownerIdentityValid(r, repository, now()) {
		return 0, "", "", false
	}
	data, err := json.Marshal(r)
	if err == nil {
		if sealed, ok := ownerIdentityDPAPI(data, true); ok && ctx.Err() == nil {
			if os.MkdirAll(home, 0700) == nil && currentPlain(home, true) {
				// A race may lose a cache update, never create authority. The next
				// query then uses the bounded API path again. Only encrypted metadata
				// from this normal API response is stored; no tokens are copied.
				f, err := os.CreateTemp(home, "identity-*.pending")
				if err == nil {
					name := f.Name()
					_, writeErr := f.Write(sealed)
					closeErr := f.Close()
					if writeErr == nil && closeErr == nil {
						_ = os.Rename(name, path)
					}
					_ = os.Remove(name)
				}
			}
		}
	}
	return id, actual, owner, ctx.Err() == nil
}

type ownerIdentityBlob struct {
	Size uint32
	Data *byte
}

func ownerIdentityDPAPI(input []byte, protect bool) ([]byte, bool) {
	if len(input) == 0 || len(input) > 1<<20 {
		return nil, false
	}
	in := ownerIdentityBlob{uint32(len(input)), &input[0]}
	out := ownerIdentityBlob{}
	entropy := []byte("owner-source-identity-v1")
	salt := ownerIdentityBlob{uint32(len(entropy)), &entropy[0]}
	name := "CryptUnprotectData"
	if protect {
		name = "CryptProtectData"
	}
	ok, _, _ := syscall.NewLazyDLL("crypt32.dll").NewProc(name).Call(uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&salt)), 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if out.Data != nil {
		defer syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree").Call(uintptr(unsafe.Pointer(out.Data)))
	}
	if ok == 0 || out.Data == nil || out.Size == 0 || out.Size > 1<<20 {
		return nil, false
	}
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), true
}
