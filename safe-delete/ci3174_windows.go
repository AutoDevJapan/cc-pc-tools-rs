//go:build windows

package main

// A finite quarantine qualification for one normally created and collected CI
// root. No deletion plan, client classification, caller grant or link traversal.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const ci3174ID = "4378ff571926b0ddbec4ed99368a6405"
const ci3174Root = `C:\Users\makhmeto\AppData\Local\Temp\codex-owned-` + ci3174ID
const ci3174Evidence = "D:/CC/Work/unattended-operations-20261013-wt/todo-3107-ci/codex-owned-9cb79093b11a8c0baf702c37733deac6"
const ci3174Service = "CC-CIW-civ6-banseiki"
const ci3174ReceiptSHA = "659c0ff190429951230e86cf7ad3572a50b3e89fec65d6ea02c9b96ed2750f65"
const ci3174PostInventory = "D:/CC/Work/unattended-operations-20261013-wt/todo-3174-windows-1/codex-owned-2096045d483cb0e410e780998589af10/postcollection-inventory.private.json"

type ci3174Proof struct {
	path, sha string
	limit     int64
}

var ci3174Proofs = []ci3174Proof{
	{"D:/CC/Tools/safe-delete/receipts-go/" + ci3174ID + ".json", ci3174ReceiptSHA, 8192},
	{ci3174Evidence + "/stage.private.json", "f0ea610c139ba16f0331271ea0f0c6f6362f1a5547ca07dcf97aca2b040024db", 64 << 10},
	{ci3174Evidence + "/result.private.json", "50338b7db2782c07dacf259e11856e9d29a5bc92c8f1426b85e385dd843ea31f", 64 << 10},
	{ci3174Evidence + "/ledger-original.private.json", "e78bc667bdf17c01798bb1b2f9c5f6f6bc50a869e9f39178e26d5746ee061732", 4 << 20},
	{ci3174Evidence + "/inventory3107.private.json", "790a5b4436c0345bcee3da3fbf83f73911e7395d5ceaf269cf6b0bc20dfdce5c", 4 << 20},
	{ci3174Evidence + "/deployment-observed.private.json", "dd2d13dc2c3aeaaaa7e19716314ff7ed6d26acbf264cd3d442e4f8bc33198f00", 4 << 20},
	{"D:/CC/Tools/admin-broker/results/20261020-182537-cf89390d51244b67f84cf2af2e798f63.json", "30661b114c06ba100fafb51538f10460147eb401e9dadb5656b014e29654733f", 4 << 20},
	{ci3174PostInventory, "4860c69b9cedae0d8b1fc148b8e8209edae71f2d3122785c22934b01258f7f30", 4 << 20},
}

type ci3174Entry struct {
	geminiEntry
	Owner string
}
type ci3174Scope struct {
	maxEntries        int
	maxBytes          int64
	captureCollected  bool
	internalLinks     bool
	identity          func(string) (ci3174Entry, error)
	settled           func() error
	baseline          map[string]ci3174Entry
	opaque            map[string]ci3174Entry
	volume            uint32
	rootID, rootBirth uint64
	originalReceipt   func() ([]byte, error)
	// Only an independently bound, fixed producer may set these. Empty retains
	// the original service-owned CI contract for every existing caller.
	preservedGit string
	createdOwner string
}

// OPEN_REPARSE_POINT is mandatory for every opaque entry. No
// FSCTL_GET_REPARSE_POINT, Readlink, content hash or target-name query is used.
func ci3174Identity(path string) (ci3174Entry, error) {
	var out ci3174Entry
	p, e := syscall.UTF16PtrFromString(ci3250WinPath(path))
	if e != nil {
		return out, fail("CI_METADATA_FAILED")
	}
	h, e := syscall.CreateFile(p, 0x80|0x20000, 7, nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if e != nil {
		return out, fail("CI_METADATA_FAILED")
	}
	defer syscall.CloseHandle(h)
	var i syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &i) != nil {
		return out, fail("CI_METADATA_FAILED")
	}
	var owner *syscall.SID
	var sd uintptr
	status, _, _ := syscall.NewLazyDLL("advapi32.dll").NewProc("GetSecurityInfo").Call(uintptr(h), 1, 1,
		uintptr(unsafe.Pointer(&owner)), 0, 0, 0, uintptr(unsafe.Pointer(&sd)))
	if sd != 0 {
		defer syscall.LocalFree(syscall.Handle(sd))
	}
	if status != 0 || owner == nil {
		return out, fail("CI_METADATA_FAILED")
	}
	sid, e := owner.String()
	if e != nil {
		return out, fail("CI_METADATA_FAILED")
	}
	out.geminiEntry, out.Owner = geminiMetadata(i), sid
	return out, nil
}

func ci3174ReadProof(p ci3174Proof) error {
	_, e := ci3174ProofBytes(p)
	return e
}
func ci3174ProofBytes(p ci3174Proof) ([]byte, error) {
	if _, e := checked(p.path); e != nil {
		return nil, e
	}
	before, e := fileIdentity(p.path)
	if e != nil || before.NumberOfLinks != 1 || before.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return nil, fail("CI_PROOF_INVALID")
	}
	f, e := os.Open(p.path)
	if e != nil {
		return nil, fail("CI_PROOF_INVALID")
	}
	b, e := io.ReadAll(io.LimitReader(f, p.limit+1))
	f.Close()
	if e != nil || int64(len(b)) > p.limit {
		return nil, fail("CI_PROOF_INVALID")
	}
	h := sha256.Sum256(b)
	after, e := fileIdentity(p.path)
	if e != nil || geminiMetadata(before) != geminiMetadata(after) || hex.EncodeToString(h[:]) != p.sha {
		return nil, fail("CI_PROOF_CHANGED")
	}
	return b, nil
}

func ci3174ReceiptMatches(r receipt) bool {
	return r.ID == ci3174ID && r.Version == 2 && same(r.Path, ci3174Root) && same(r.Root, filepath.Dir(ci3174Root)) &&
		r.Volume == 2229119083 && r.FileID == 45317471266384470 && r.Project == "civ6-ci-queue" &&
		r.Content == "job-39423559387-1-118630300541" && r.Created == "2026-10-20T21:39:02.942647+09:00"
}

func ci3174TimeAllowed() bool {
	now := time.Now().UTC()
	return !now.Before(time.Date(2026, 10, 20, 18, 25, 0, 0, time.UTC)) && now.Before(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
}

func ci3174Birth(i ci3174Entry) uint64 {
	return uint64(i.Created.HighDateTime)<<32 | uint64(i.Created.LowDateTime)
}

func (c config) ci3174Recovery(id string) (*ci3174Scope, func(), error) {
	if id != ci3174ID || !same(c.Store, "D:/CC/Tools/safe-delete/receipts-go") ||
		!same(c.Quarantine, `C:\Users\makhmeto\Downloads\削除希望`) || !ci3174TimeAllowed() {
		return nil, nil, fail("CI_SCOPE_MISMATCH")
	}
	r, e := c.load(id, true)
	if e != nil {
		return nil, nil, e
	}
	if !ci3174ReceiptMatches(r) {
		return nil, nil, fail("CI_SCOPE_MISMATCH")
	}
	if e = clientGuard(r.Path); e != nil {
		return nil, nil, e
	}
	if e = ci3174Official(); e != nil {
		return nil, nil, e
	}
	for _, p := range ci3174Proofs {
		if e = ci3174ReadProof(p); e != nil {
			return nil, nil, e
		}
	}
	parent, e := ci3174Identity(filepath.Dir(r.Path))
	if e != nil || parent.Volume != r.Volume || parent.ID != 562949953422078 || ci3174Birth(parent) != 134072793159133344 {
		return nil, nil, fail("CI_PARENT_CHANGED")
	}
	unlock, e := ci3174Lock()
	if e != nil {
		return nil, nil, e
	}
	b, e := ci3174ProofBytes(ci3174Proofs[len(ci3174Proofs)-1])
	var baseline map[string]ci3174Entry
	if e != nil || json.Unmarshal(b, &baseline) != nil || len(baseline) != 3626 {
		unlock()
		return nil, nil, fail("CI_INVENTORY_INVALID")
	}
	opaque := map[string]ci3174Entry{}
	for name, i := range baseline {
		if i.Attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			opaque[name] = i
		}
	}
	if len(opaque) != 13 {
		unlock()
		return nil, nil, fail("CI_INVENTORY_INVALID")
	}
	if e = ci3174OriginalObjects(baseline); e != nil {
		unlock()
		return nil, nil, e
	}
	s := &ci3174Scope{volume: r.Volume, rootID: r.FileID, rootBirth: 134369735429421378, originalReceipt: func() ([]byte, error) { return ci3174ProofBytes(ci3174Proofs[0]) }, maxEntries: c.MaxEntries, identity: ci3174Identity, baseline: baseline, opaque: opaque, settled: func() error {
		for _, p := range ci3174Proofs {
			if e := ci3174ReadProof(p); e != nil {
				return e
			}
		}
		if e := clientGuard(r.Path); e != nil {
			return e
		}
		return ci3174Settled()
	}}
	if e = s.settled(); e != nil {
		unlock()
		return nil, nil, e
	}
	return s, unlock, nil
}

func ci3174OriginalObjects(baseline map[string]ci3174Entry) error {
	b, e := ci3174ProofBytes(ci3174Proofs[4])
	var old struct {
		Objects map[string]struct {
			Volume  uint32
			ID      uint64
			Created uint64
		}
	}
	if e != nil || json.Unmarshal(b, &old) != nil || len(old.Objects) != 3598 {
		return fail("CI_ORIGINAL_INVENTORY_INVALID")
	}
	for name, want := range old.Objects {
		actual, ok := baseline[name]
		if !ok || actual.Volume != want.Volume || actual.ID != want.ID || ci3174Birth(actual) != want.Created {
			return fail("CI_ORIGINAL_OBJECT_CHANGED")
		}
	}
	return nil
}

func ci3174Official() error {
	for _, v := range []struct {
		endpoint string
		job      bool
	}{
		{"repos/AutoDevJapan/civ6-banseiki/actions/runs/39423559387/attempts/1", false},
		{"repos/AutoDevJapan/civ6-banseiki/actions/jobs/118630300541", true},
	} {
		b, e := runHidden(10*time.Second, "gh", "api", "--hostname", "github.com", v.endpoint, "--jq", "{id,run_id,run_attempt,head_sha,event,head_branch,path,name,repository_id:.repository.id,head_repository_id:.head_repository.id}")
		var o struct {
			ID       uint64 `json:"id"`
			RunID    uint64 `json:"run_id"`
			Attempt  uint64 `json:"run_attempt"`
			Repo     uint64 `json:"repository_id"`
			HeadRepo uint64 `json:"head_repository_id"`
			Head     string `json:"head_sha"`
			Event    string `json:"event"`
			Branch   string `json:"head_branch"`
			Path     string `json:"path"`
			Name     string `json:"name"`
		}
		if e != nil || len(b) > 64<<10 || json.Unmarshal(b, &o) != nil {
			return fail("CI_OFFICIAL_IDENTITY_UNVERIFIED")
		}
		if o.Attempt != 1 || o.Head != "2bd3a366f8974ba54df83927102aa5bdad3983e5" {
			return fail("CI_OFFICIAL_IDENTITY_UNVERIFIED")
		}
		if v.job {
			if o.ID != 118630300541 || o.RunID != 39423559387 || o.Name != "test (windows-latest)" {
				return fail("CI_OFFICIAL_IDENTITY_UNVERIFIED")
			}
		} else if o.ID != 39423559387 || o.Repo != 1392152644 || o.HeadRepo != 1392152644 || o.Event != "pull_request" || o.Branch != "scheduler/actions-cost-pr-concurrency-20261019" || (o.Path != ".github/workflows/rust.yml" && o.Path != ".github/workflows/rust.yml@refs/pull/149/merge") {
			return fail("CI_OFFICIAL_IDENTITY_UNVERIFIED")
		}
	}
	return nil
}

// The existing collector/controller lock excludes reuse while moving the root.
// Open an existing lock only; never create a replacement or change its ACL.
func ci3174Lock() (func(), error) {
	path := `C:\ProgramData\CC\civ6-ci-windows\` + ci3174Service + ".lock"
	if _, e := checked(path); e != nil {
		return nil, e
	}
	p, _ := syscall.UTF16PtrFromString(ci3250WinPath(path))
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 7, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return nil, fail("CI_COLLECTION_LOCK_FAILED")
	}
	var ov syscall.Overlapped
	k := syscall.NewLazyDLL("kernel32.dll")
	ok, _, _ := k.NewProc("LockFileEx").Call(uintptr(h), 3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if ok == 0 {
		syscall.CloseHandle(h)
		return nil, fail("CI_COLLECTION_LOCK_FAILED")
	}
	return func() {
		k.NewProc("UnlockFileEx").Call(uintptr(h), 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
		syscall.CloseHandle(h)
	}, nil
}

func ci3174Settled() error {
	if !ci3174TimeAllowed() {
		return fail("CI_QUALIFICATION_EXPIRED")
	}
	// Read only a public collection marker, never any credential/profile value.
	var key syscall.Handle
	p, _ := syscall.UTF16PtrFromString(`SOFTWARE\CC\civ6-ci-windows\` + ci3174Service)
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, p, 0, 0x20019, &key) != nil {
		return fail("CI_COLLECTION_UNVERIFIED")
	}
	defer syscall.RegCloseKey(key)
	name, _ := syscall.UTF16PtrFromString("CollectedRoot")
	var buf [2048]uint16
	var typ uint32
	n := uint32(len(buf) * 2)
	if syscall.RegQueryValueEx(key, name, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil || typ != 1 || n < 4 || n > uint32(len(buf)*2) || n%2 != 0 || buf[n/2-1] != 0 {
		return fail("CI_COLLECTION_UNVERIFIED")
	}
	for _, v := range buf[:n/2-1] {
		if v == 0 {
			return fail("CI_COLLECTION_UNVERIFIED")
		}
	}
	if !same(syscall.UTF16ToString(buf[:n/2]), ci3174Root) {
		return fail("CI_COLLECTION_UNVERIFIED")
	}
	a := syscall.NewLazyDLL("advapi32.dll")
	manager, _, _ := a.NewProc("OpenSCManagerW").Call(0, 0, 1)
	if manager == 0 {
		return fail("CI_SERVICE_UNVERIFIED")
	}
	defer a.NewProc("CloseServiceHandle").Call(manager)
	svcName, _ := syscall.UTF16PtrFromString(ci3174Service)
	svc, _, _ := a.NewProc("OpenServiceW").Call(manager, uintptr(unsafe.Pointer(svcName)), 5)
	if svc == 0 {
		return fail("CI_SERVICE_UNVERIFIED")
	}
	defer a.NewProc("CloseServiceHandle").Call(svc)
	var status [9]uint32
	var needed uint32
	ok, _, _ := a.NewProc("QueryServiceStatusEx").Call(svc, 0, uintptr(unsafe.Pointer(&status[0])), unsafe.Sizeof(status), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 || status[1] != 1 || status[7] != 0 {
		return fail("CI_SERVICE_NOT_STOPPED")
	}
	// The exact service image must still belong to this collected root.
	var cfg [8192]byte
	ok, _, _ = a.NewProc("QueryServiceConfigW").Call(svc, uintptr(unsafe.Pointer(&cfg[0])), uintptr(len(cfg)), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return fail("CI_SERVICE_UNVERIFIED")
	}
	type serviceConfig struct {
		ServiceType, StartType, ErrorControl uint32
		Binary                               *uint16
	}
	binary := (*serviceConfig)(unsafe.Pointer(&cfg[0])).Binary
	if binary == nil {
		return fail("CI_SERVICE_UNVERIFIED")
	}
	start, end, addr := uintptr(unsafe.Pointer(&cfg[0])), uintptr(unsafe.Pointer(&cfg[0]))+uintptr(len(cfg)), uintptr(unsafe.Pointer(binary))
	if addr < start || addr >= end || addr%2 != 0 {
		return fail("CI_SERVICE_UNVERIFIED")
	}
	chars := unsafe.Slice(binary, int((end-addr)/2))
	terminated := false
	for _, c := range chars {
		if c == 0 {
			terminated = true
			break
		}
	}
	if !terminated {
		return fail("CI_SERVICE_UNVERIFIED")
	}
	image := syscall.UTF16ToString(chars)
	if image != syscall.EscapeArg(ci3174Root+`\helper.exe`)+" service "+syscall.EscapeArg(ci3174Root) {
		return fail("CI_SERVICE_CHANGED")
	}
	return nil
}

func (s *ci3174Scope) snapshot(root string) (map[string]ci3174Entry, measured, error) {
	result := map[string]ci3174Entry{}
	m := measured{}
	links := map[fileIdentityKey]hardLinkCount{}
	if _, e := checked(root); e != nil {
		return nil, m, e
	}
	pending := []string{root}
	for len(pending) > 0 {
		p := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		rel, e := filepath.Rel(root, p)
		if e != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, m, fail("CI_INVENTORY_INVALID")
		}
		want, recorded := s.baseline[filepath.ToSlash(rel)]
		// This completed CI root contains its original Git administrative file.
		// Its pinned metadata is preserved without opening its body. No other
		// protected name, new entry or caller-supplied Git tree is admitted.
		gitPath := ci3250Git
		if s.preservedGit != "" {
			gitPath = s.preservedGit
		}
		gitAllowed := strings.EqualFold(filepath.Base(p), ".git") && (recorded || (s.captureCollected && filepath.ToSlash(rel) == gitPath))
		if s.preservedGit != "" {
			gitAllowed = strings.EqualFold(filepath.Base(p), ".git") && filepath.ToSlash(rel) == gitPath && (recorded || s.captureCollected)
		}
		if protected(filepath.Base(p)) && !gitAllowed {
			return nil, m, fail("PROTECTED_PATH")
		}
		if s.baseline != nil && !recorded {
			return nil, m, fail("CI_INVENTORY_CHANGED")
		}
		i, e := s.identity(p)
		if e != nil {
			return nil, m, e
		}
		if recorded && want != i {
			return nil, m, fail("CI_INVENTORY_CHANGED")
		}
		owner := "S-1-5-32-544"
		if s.createdOwner != "" {
			owner = s.createdOwner
		}
		if i.Owner != owner || i.Volume != s.volume || i.Links < 1 || (!s.internalLinks && i.Links != 1) || i.Links > uint32(s.maxEntries+1) || (i.Attributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 && i.Links != 1) {
			return nil, m, fail("CI_OBJECT_CHANGED")
		}
		if rel == "." && (i.ID != s.rootID || ci3174Birth(i) != s.rootBirth || i.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0) {
			return nil, m, fail("CI_ROOT_CHANGED")
		}
		result[filepath.ToSlash(rel)] = i
		m.Attributes = m.Attributes || i.Attributes&3 != 0
		if i.Attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			if want, ok := s.opaque[filepath.ToSlash(rel)]; !s.captureCollected && (!ok || want != i) {
				return nil, m, fail("LINK_OR_REPARSE_POINT")
			}
			continue // opaque object: never open, enumerate or resolve its target
		}
		if i.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
			k := fileIdentityKey{volume: i.Volume, fileID: i.ID}
			g := links[k]
			if g.total != 0 && g.total != i.Links {
				return nil, m, fail("CI_HARDLINK_CHANGED")
			}
			g.inside++
			g.total = i.Links
			links[k] = g
			m.Bytes += int64(uint64(i.SizeHigh)<<32 | uint64(i.SizeLow))
			if m.Bytes < 0 || (s.maxBytes > 0 && m.Bytes > s.maxBytes) {
				return nil, m, fail("CI_SIZE_LIMIT_EXCEEDED")
			}
			continue
		}
		dir, e := os.Open(p)
		if e != nil {
			return nil, m, fail("CONTENT_METADATA_FAILED")
		}
		for {
			children, re := dir.ReadDir(64)
			for _, child := range children {
				m.Entries++
				if m.Entries > s.maxEntries {
					dir.Close()
					return nil, m, fail("ENTRY_LIMIT_EXCEEDED")
				}
				pending = append(pending, filepath.Join(p, child.Name()))
			}
			if re != nil {
				dir.Close()
				if re != io.EOF {
					return nil, m, fail("CONTENT_METADATA_FAILED")
				}
				break
			}
		}
	}
	for _, g := range links {
		if g.inside != g.total {
			return nil, m, fail("CI_EXTERNAL_HARDLINK")
		}
	}
	for name := range s.opaque {
		if _, ok := result[name]; !ok {
			return nil, m, fail("CI_OPAQUE_MISSING")
		}
	}
	if s.baseline != nil && !reflect.DeepEqual(s.baseline, result) {
		return nil, m, fail("CI_INVENTORY_CHANGED")
	}
	return result, m, nil
}

func (s *ci3174Scope) verify(root string, before map[string]ci3174Entry, r receipt) error {
	after, _, e := s.snapshot(root)
	if e != nil {
		return e
	}
	if after["."].ID != r.FileID || after["."].Volume != r.Volume || !reflect.DeepEqual(before, after) {
		return fail("PRESERVATION_MISMATCH")
	}
	return nil
}
