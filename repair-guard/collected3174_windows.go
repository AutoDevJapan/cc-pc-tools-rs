package main

// Root-only classification after the normal collector's owner/ACL restoration.
// No child (including opaque SDK reparses) gains a new classification here.
import (
	"encoding/json"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var collected3174Proofs = []owned3107Proof{
	{"D:/CC/Tools/admin-broker/results/20261020-182537-cf89390d51244b67f84cf2af2e798f63.json", "30661b114c06ba100fafb51538f10460147eb401e9dadb5656b014e29654733f", 4 << 20},
	{"D:/CC/Work/unattended-operations-20261013-wt/todo-3174-windows-1/codex-owned-2096045d483cb0e410e780998589af10/postcollection-inventory.private.json", "4860c69b9cedae0d8b1fc148b8e8209edae71f2d3122785c22934b01258f7f30", 4 << 20},
}

func collected3174At(at time.Time) bool {
	return owned3107TimeAllowed(at) && !at.Before(time.Date(2026, 10, 20, 18, 25, 47, 0, time.UTC))
}
func collected3174Allowed(path string, a owned3107Access, extra []owned3107Proof, marker func() bool) bool {
	if !filepath.IsAbs(path) || !same(path, a.root) || len(extra) != 2 || !collected3174At(a.at) || !owned3107NoDeny(path, a) || !a.plain(path) {
		return false
	}
	physical := func() bool {
		root, e := a.identity(a.root)
		parent, pe := a.identity(filepath.Dir(a.root))
		return e == nil && pe == nil && root.Links == 1 && parent.Links == 1 && civ6Matches(a.expected, root, true) && civ6Matches(a.parent, parent, true) && a.plain(a.root) && owned3107NoDeny(a.root, a)
	}
	if !physical() {
		return false
	}
	original, ok := owned3107ReadProofs(a)
	if !ok {
		return false
	}
	_, deployment, ok := owned3107Binding(a, original)
	if !ok {
		return false
	}
	postAccess := a
	postAccess.proofs = extra
	proof, ok := owned3107ReadProofFiles(postAccess)
	if !ok {
		return false
	}
	// Decode the exact hashed bytes' parsed root metadata; never enumerate root.
	b, e := json.Marshal(proof[1]["."])
	var observed struct {
		Volume                               uint32
		ID                                   uint64
		Attributes, Links, SizeHigh, SizeLow uint32
		Owner                                string
		Created, Written                     syscall.Filetime
	}
	if e != nil || json.Unmarshal(b, &observed) != nil {
		return false
	}
	postRoot := func() bool {
		actual, e := a.identity(a.root)
		birth := int64(uint64(observed.Created.HighDateTime)<<32 | uint64(observed.Created.LowDateTime))
		written := int64(uint64(observed.Written.HighDateTime)<<32 | uint64(observed.Written.LowDateTime))
		size := uint64(observed.SizeHigh)<<32 | uint64(observed.SizeLow)
		return e == nil && actual.Volume == observed.Volume && actual.ID == observed.ID && actual.Attributes == observed.Attributes && actual.Links == observed.Links && actual.Owner == observed.Owner && actual.Created == birth && actual.Written == written && actual.Size == size
	}
	if !postRoot() || !a.qualified() || !a.runtime(deployment) || !a.official() || !marker() {
		return false
	}
	if _, ok = owned3107ReadProofs(a); !ok {
		return false
	}
	if _, ok = owned3107ReadProofFiles(postAccess); !ok {
		return false
	}
	now := a.at
	if a.now != nil {
		now = a.now()
	}
	return collected3174At(now) && physical() && postRoot() && marker()
}

func collected3174Marker() bool {
	var key syscall.Handle
	path, _ := syscall.UTF16PtrFromString(`SOFTWARE\CC\civ6-ci-windows\CC-CIW-civ6-banseiki`)
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, 0x20019, &key) != nil {
		return false
	}
	defer syscall.RegCloseKey(key)
	name, _ := syscall.UTF16PtrFromString("CollectedRoot")
	var buf [2048]uint16
	var typ uint32
	n := uint32(len(buf) * 2)
	if syscall.RegQueryValueEx(key, name, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil || typ != 1 || n < 4 || n > uint32(len(buf)*2) || n%2 != 0 || buf[n/2-1] != 0 {
		return false
	}
	for _, c := range buf[:n/2-1] {
		if c == 0 {
			return false
		}
	}
	return same(syscall.UTF16ToString(buf[:n/2]), owned3107Root)
}
