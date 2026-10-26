//go:build windows

// One finite software update, never a data cleanup or a protection override.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const sourceRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3757-3628"
const evidenceRoot = sourceRoot + "/codex-owned-8bfa50250f964b4f84235c754c7cf821"
const sourceHead = "89f2411f752bfd3d548a4b5a25da1fc9062bd505"
const oldSafe = "7e1737b6bcd7f1b015998e8594527dbab3f211b68cbe8ad3b82f6a46bca39514"
const oldCleanup = "6975fc67257bb45012f96dc42091e26fab9bf19a065e00740ab0476e307b2f9b"
const cleanupHandoff = "D:/CC/Tools/todo/specs/2488-e4aec9aa1c54483d9c28b0b55fa56317.md"
const cleanupHandoffSHA = "8fc610dafd546168cc6120c130542c69890d4c820c198e13ef2c3ddfdac6a74f"

type leaf struct{ Kind, Source, Target, Old, New string }

var leaves = []leaf{
	{"safe-delete", evidenceRoot + "/safe-delete-89f2411.exe", "D:/CC/Tools/safe-delete/safe-delete.exe", oldSafe, "286a22b24412cc19138c083e09a93f4626de9058f329fad256e3f377b1e3b73b"},
	{"cleanup-8h", evidenceRoot + "/cleanup-8h-89f2411.exe", "D:/CC/Tools/cleanup-8h/cleanup-8h.exe", oldCleanup, "7fbc49c6c8005920a139f9231ae8af7fb0babccd014ecc974fc892ee43fa67e8"},
}

var kernel = syscall.NewLazyDLL("kernel32.dll")
var advapi = syscall.NewLazyDLL("advapi32.dll")

func errCode(s string) error { return errors.New(s) }
func same(a, b string) bool  { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
func utf(s string) *uint16   { p, _ := syscall.UTF16PtrFromString(s); return p }

type pin struct {
	f    *os.File
	info syscall.ByHandleFileInformation
}

func (p *pin) close() { p.f.Close() }
func (p *pin) id() string {
	return fmt.Sprintf("%d:%d:%d:%d:%d", p.info.VolumeSerialNumber, p.info.FileIndexHigh, p.info.FileIndexLow, p.info.CreationTime.HighDateTime, p.info.CreationTime.LowDateTime)
}
func (p *pin) hash() (string, error) {
	if _, e := p.f.Seek(0, 0); e != nil {
		return "", e
	}
	h := sha256.New()
	if _, e := io.Copy(h, io.LimitReader(p.f, 64<<20+1)); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (p *pin) at(path string) bool {
	name := make([]uint16, 32768)
	n, _, _ := kernel.NewProc("GetFinalPathNameByHandleW").Call(p.f.Fd(), uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)), 0)
	return n > 0 && n < uintptr(len(name)) && same(strings.TrimPrefix(syscall.UTF16ToString(name[:n]), `\\?\`), path)
}

func (p *pin) namedAs(path string) bool {
	return p.verifyName(path) == nil
}

func (p *pin) verifyName(path string) error {
	// Reopen the public name while our original pin still excludes writers
	// and removers. A cached name associated with a renamed handle is not a
	// namespace lookup; compare the freshly named object's complete identity.
	named, e := openPin(path, 0x80, 7, 3)
	if e != nil {
		return fmt.Errorf("NAME_PIN:%w", e)
	}
	defer named.close()
	var current syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(syscall.Handle(p.f.Fd()), &current) != nil {
		return errCode("HELD_METADATA_READ_FAILED")
	}
	physical := func(a, b syscall.ByHandleFileInformation) bool {
		return a.VolumeSerialNumber == b.VolumeSerialNumber && a.FileIndexHigh == b.FileIndexHigh && a.FileIndexLow == b.FileIndexLow
	}
	// The OS can change creation time during rename of this same held file.
	// The original pin prevents ID reuse; require its volume/file ID to stay
	// fixed. Creation time is diagnostic metadata, not additional evidence of
	// sameness for our already-open object; different handles may cache it.
	if !physical(current, p.info) || !physical(named.info, current) {
		return errCode("PUBLIC_VOLUME_OR_FILE_ID_CHANGED")
	}
	return nil
}
func openPin(path string, access, share, disposition uint32) (*pin, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errCode("INVALID_PATH")
	}
	h, e := syscall.CreateFile(utf(path), access, share, nil, disposition, syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if e != nil {
		return nil, errCode("OPEN_OR_BUSY")
	}
	f := os.NewFile(uintptr(h), path)
	var i syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &i) != nil || i.FileAttributes&0x400 != 0 || i.NumberOfLinks != 1 {
		f.Close()
		return nil, errCode("LINK_OR_IDENTITY")
	}
	name := make([]uint16, 32768)
	n, _, _ := kernel.NewProc("GetFinalPathNameByHandleW").Call(uintptr(h), uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)), 0)
	if n == 0 || n >= uintptr(len(name)) || !same(strings.TrimPrefix(syscall.UTF16ToString(name[:n]), `\\?\`), path) {
		f.Close()
		return nil, errCode("PHYSICAL_PATH_CHANGED")
	}
	return &pin{f, i}, nil
}
func parents(path string) ([]*pin, error) {
	var nodes []string
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		nodes = append(nodes, p)
		if same(p, filepath.Dir(p)) {
			break
		}
	}
	var result []*pin
	for i := len(nodes) - 1; i >= 0; i-- {
		p, e := openPin(nodes[i], 0x80, 3, 3)
		if e != nil || p.info.FileAttributes&0x10 == 0 {
			if p != nil {
				p.close()
			}
			closePins(result)
			return nil, errCode("ANCESTOR_UNVERIFIED")
		}
		result = append(result, p)
	}
	return result, nil
}
func closePins(xs []*pin) {
	for i := len(xs) - 1; i >= 0; i-- {
		xs[i].close()
	}
}
func missing(path string) bool { _, e := os.Lstat(path); return os.IsNotExist(e) }
func readPinned(path string, limit int64) ([]byte, error) {
	ps, e := parents(path)
	if e != nil {
		return nil, e
	}
	defer closePins(ps)
	p, e := openPin(path, syscall.GENERIC_READ, 1, 3)
	if e != nil {
		return nil, e
	}
	defer p.close()
	if p.info.FileAttributes&0x10 != 0 {
		return nil, errCode("INVALID_PROOF")
	}
	b, e := io.ReadAll(io.LimitReader(p.f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errCode("PROOF_LIMIT")
	}
	return b, nil
}
func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// Original successful install event, obtained from the original actor, not
// synthesized from the currently installed bytes or a caller's manifest.
func originalProofs() error {
	b, e := readPinned("D:/CC/Tools/codex-jobs/jobs/20261025-032641-todo-3650/events.jsonl", 64<<20)
	if e != nil {
		return e
	}
	s := bufio.NewScanner(strings.NewReader(string(b)))
	s.Buffer(make([]byte, 65536), 8<<20)
	found := 0
	for s.Scan() {
		if sha(s.Bytes()) != "2606a28bf168562ea20c2d7167220b8252642a8a2cf85dcffc3b0abc888a66de" {
			continue
		}
		var v struct {
			Type string `json:"type"`
			Item struct {
				ID, Status string
				Exit       int    `json:"exit_code"`
				Output     string `json:"aggregated_output"`
			} `json:"item"`
		}
		if json.Unmarshal(s.Bytes(), &v) != nil || v.Type != "item.completed" || v.Item.ID != "item_174" || v.Item.Status != "completed" || v.Item.Exit != 0 {
			return errCode("ORIGINAL_INSTALL_INVALID")
		}
		var o struct {
			Head      string `json:"source_head"`
			Installed string `json:"installed_sha256"`
			Target    string `json:"target"`
		}
		if json.Unmarshal([]byte(v.Item.Output), &o) != nil || o.Installed != oldSafe || o.Head != "608188c225cba2d102200879b02a9ef7cdad9b5b" || !same(o.Target, leaves[0].Target) {
			return errCode("ORIGINAL_INSTALL_INVALID")
		}
		found++
	}
	if s.Err() != nil || found != 1 {
		return errCode("ORIGINAL_INSTALL_MISSING")
	}
	b, e = readPinned("D:/CC/Work/unattended-operations-20261013/cleanup-8h/deploy-2491/manifest.json", 8192)
	if e != nil || sha(b) != "18d8b5cb4075049df06b1033be92124dbae740cfe7d03e88fc0d559324f09ab4" {
		return errCode("ORIGINAL_CLEANUP_MANIFEST_CHANGED")
	}
	var m struct {
		Schema, Todo int
		Cleanup      string `json:"cleanup_sha256"`
	}
	if json.Unmarshal(b, &m) != nil || m.Schema != 1 || m.Todo != 2491 || !strings.EqualFold(m.Cleanup, "d72a2bd62e3f5c491234e22b17a7533a3d9b25d7944691f96fc4d79e895b8d24") {
		return errCode("ORIGINAL_CLEANUP_MANIFEST_INVALID")
	}
	// The earlier 2491 manifest is deliberately not the installed baseline.
	// Original parent 2488 saved the subsequent deployment hash on October 13;
	// verify its archived original bytes and reference before opening current.
	b, e = readPinned(cleanupHandoff, 1<<20)
	if e != nil || sha(b) != cleanupHandoffSHA || !strings.Contains(strings.ToLower(string(b)), "cleanup"+oldCleanup) || !strings.Contains(string(b), "578cbc6c6126690761680bc1a307c1118726e9e5") {
		return errCode("ORIGINAL_CLEANUP_HANDOFF_CHANGED")
	}
	b, e = readPinned("D:/CC/Tools/todo/todo.json", 96<<20)
	if e != nil || cleanupReference(b) != nil {
		return errCode("ORIGINAL_CLEANUP_REFERENCE_MISSING")
	}
	return verifySource(sourceHead, leaves[0].Source)
}

func verifySource(head, artifact string) error {
	for _, args := range [][]string{{"remote", "get-url", "origin"}, {"merge-base", "--is-ancestor", head, "origin/main"}, {"diff", "--quiet", head, "HEAD", "--", "safe-delete", "cleanup-8h", "Cargo.toml", "Cargo.lock"}, {"diff", "--quiet", "HEAD", "--", "safe-delete", "cleanup-8h", "Cargo.toml", "Cargo.lock"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		c := exec.CommandContext(ctx, "git", append([]string{"-C", sourceRoot}, args...)...)
		c.WaitDelay = 2 * time.Second
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		out, e := c.Output()
		cancel()
		if e != nil {
			return errCode("SOURCE_CHANGED")
		}
		if args[0] == "remote" && strings.TrimSpace(string(out)) != "https://github.com/AutoDevJapan/cc-pc-tools-rs.git" {
			return errCode("SOURCE_OWNER_CHANGED")
		}
	}
	i, e := buildinfo.ReadFile(artifact)
	if e != nil {
		return errCode("BUILD_INFO_MISSING")
	}
	revision, modified := "", ""
	for _, s := range i.Settings {
		if s.Key == "vcs.revision" {
			revision = s.Value
		}
		if s.Key == "vcs.modified" {
			modified = s.Value
		}
	}
	if revision != head || modified != "false" {
		return errCode("BUILD_SOURCE_CHANGED")
	}
	return nil
}

// Decode only the original task; legacy types in unrelated tasks are not this
// software update's authority. A historical source is not a new data receipt.
func cleanupReference(b []byte) error {
	var table struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(b, &table) != nil {
		return errCode("INVALID_TASK_TABLE")
	}
	found := 0
	selected := 0
	for _, raw := range table.Items {
		var key struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(raw, &key) != nil || key.ID != 2488 {
			continue
		}
		selected++
		var task struct {
			Project string                           `json:"project"`
			History []struct{ Spec, Updated string } `json:"spec_history"`
		}
		if json.Unmarshal(raw, &task) != nil || task.Project != "cc-pc-tools-rs" {
			return errCode("INVALID_ORIGINAL_TASK")
		}
		for _, h := range task.History {
			if same(h.Spec, cleanupHandoff) && h.Updated == "2026-10-13 16:46" {
				found++
			}
		}
	}
	if selected != 1 || found != 1 {
		return errCode("ORIGINAL_CLEANUP_REFERENCE_MISSING")
	}
	return nil
}

// Rename the actual pinned object with ReplaceIfExists=false. A competing
// destination is retained; path-based replacement never overwrites its bytes.
func renameNoReplace(p *pin, destination string) error {
	buf, e := nativeRenameBuffer(destination)
	if e != nil {
		return e
	}
	e = retrySharing(func() error {
		ok, _, callErr := kernel.NewProc("SetFileInformationByHandle").Call(p.f.Fd(), 3, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if ok != 0 {
			return nil
		}
		return callErr
	}, func() { time.Sleep(25 * time.Millisecond) })
	runtime.KeepAlive(buf)
	if e != nil {
		return fmt.Errorf("RENAME_COLLISION_OR_BUSY:%w", e)
	}
	return nil
}

func nativeRenameBuffer(destination string) ([]byte, error) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return nil, errCode("UNSUPPORTED_PLATFORM")
	}
	words, e := syscall.UTF16FromString(filepath.Clean(destination))
	if e != nil {
		return nil, e
	}
	// FileNameLength excludes NUL, while the Win32 DOS-path conversion also
	// consumes a terminated wide string. Never rely on zeroed heap padding.
	buf := make([]byte, 20+len(words)*2)
	*(*uint32)(unsafe.Pointer(&buf[16])) = uint32((len(words) - 1) * 2)
	for i, w := range words {
		buf[20+i*2] = byte(w)
		buf[21+i*2] = byte(w >> 8)
	}
	return buf, nil
}

// Windows may briefly hold a reader while scanning a just-written file. Keep
// the same pinned object and no-replace destination. Only sharing violation
// gets four finite attempts; collision/permission errors retain immediately.
func retrySharing(operation func() error, pause func()) error {
	for attempt := 0; ; attempt++ {
		e := operation()
		if !errors.Is(e, syscall.Errno(32)) || attempt == 3 {
			return e
		}
		pause()
	}
}
func copySecurity(from, to *pin) error {
	var owner, group, dacl, sd uintptr
	code, _, _ := advapi.NewProc("GetSecurityInfo").Call(from.f.Fd(), 1, 7, uintptr(unsafe.Pointer(&owner)), uintptr(unsafe.Pointer(&group)), uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 {
		return errCode("SECURITY_READ_FAILED")
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	var control uint16
	var revision uint32
	ok, _, _ := advapi.NewProc("GetSecurityDescriptorControl").Call(sd, uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	if ok == 0 {
		return errCode("SECURITY_READ_FAILED")
	}
	flags := uintptr(7 | 0x20000000)
	if control&0x1000 != 0 {
		flags = 7 | 0x80000000
	}
	code, _, _ = advapi.NewProc("SetSecurityInfo").Call(to.f.Fd(), 1, flags, owner, group, dacl, 0)
	if code != 0 {
		return errCode("SECURITY_COPY_FAILED")
	}
	return nil
}

type prepared struct {
	leaf     leaf
	old, new *pin
	parents  []*pin
	state    installState
	suffix   string
}

type installState struct {
	StageCreated     bool   `json:"stage_created"`
	OldRetired       bool   `json:"old_retired"`
	Published        bool   `json:"published"`
	Verified         bool   `json:"verified"`
	RestoreAttempted bool   `json:"restore_attempted"`
	Restored         bool   `json:"restored"`
	PublicName       string `json:"public_name_state"`
}

func namespaceState(path string) string {
	_, e := os.Lstat(path)
	if e == nil {
		return "present"
	}
	if os.IsNotExist(e) {
		return "absent"
	}
	return "unverified"
}

func prepare(l leaf) (*prepared, error) {
	return prepareAt(l, "todo3757")
}
func prepareAt(l leaf, suffix string) (*prepared, error) {
	if suffix != "todo3757" && suffix != "todo3757-native-proof" && suffix != "todo3942" && suffix != "todo3942-scope" && suffix != "todo3942-observe" && suffix != "todo3942-legacy" {
		return nil, errCode("INVALID_UPDATE_GENERATION")
	}
	ps, e := parents(l.Target)
	if e != nil {
		return nil, e
	}
	// This handle owns DELETE, while other handles may only read. A competing
	// rename/delete cannot move the checked object during our own rename.
	old, e := openPin(l.Target, syscall.GENERIC_READ|0x20000|0x10000, 1, 3)
	if e != nil {
		closePins(ps)
		return nil, e
	}
	newParents, e := parents(l.Source)
	if e != nil {
		old.close()
		closePins(ps)
		return nil, e
	}
	ps = append(ps, newParents...)
	n, e := openPin(l.Source, syscall.GENERIC_READ, 1, 3)
	if e != nil {
		old.close()
		closePins(ps)
		return nil, e
	}
	p := &prepared{leaf: l, old: old, new: n, parents: ps, suffix: suffix}
	a, e := old.hash()
	b, be := n.hash()
	if e != nil || be != nil || a != l.Old || b != l.New || old.info.FileAttributes&0x10 != 0 || n.info.FileAttributes&0x10 != 0 || old.info.FileSizeHigh != 0 || n.info.FileSizeHigh != 0 {
		p.close()
		return nil, errCode("PINNED_BYTES_DIFFER")
	}
	if !missing(p.backupPath()) || !missing(p.stagePath()) {
		p.close()
		return nil, errCode("PREVIOUS_STAGE_RETAINED")
	}
	return p, nil
}
func (p *prepared) close()             { p.old.close(); p.new.close(); closePins(p.parents) }
func (p *prepared) backupPath() string { return p.leaf.Target + ".before-" + p.suffix }
func (p *prepared) stagePath() string  { return p.leaf.Target + ".stage-" + p.suffix }
func (p *prepared) result() map[string]any {
	return map[string]any{"kind": p.leaf.Kind, "updated": p.state.Published, "verified": p.state.Verified, "steps": p.state, "old_sha256": p.leaf.Old, "new_sha256": p.leaf.New, "original_file_id": p.old.id()}
}

type hooks struct {
	beforeRetire, beforeRename, beforePublish func()
	afterPublish                              func(*pin)
	rename                                    func(*pin, string) error
}

func (p *prepared) install(test hooks) error {
	defer func() { p.state.PublicName = namespaceState(p.leaf.Target) }()
	rename := renameNoReplace
	if test.rename != nil {
		rename = test.rename
	}
	stagePath := p.stagePath()
	backup := p.backupPath()
	stage, e := openPin(stagePath, syscall.GENERIC_READ|syscall.GENERIC_WRITE|0x10000|0x40000|0x80000, 1, 1)
	if e != nil {
		return e
	}
	defer stage.close()
	p.state.StageCreated = true
	if _, e = p.new.f.Seek(0, 0); e != nil {
		return e
	}
	if _, e = io.Copy(stage.f, p.new.f); e != nil {
		return errCode("STAGE_WRITE_FAILED")
	}
	if e = stage.f.Sync(); e != nil {
		return e
	}
	h, e := stage.hash()
	if e != nil || h != p.leaf.New {
		return errCode("STAGE_CHANGED")
	}
	if e = copySecurity(p.old, stage); e != nil {
		return e
	}
	if test.beforeRetire != nil {
		test.beforeRetire()
	}
	current, e := openPin(p.leaf.Target, 0x80, 5, 3)
	if e != nil {
		return e
	}
	sameID := current.id() == p.old.id()
	current.close()
	if !sameID {
		return errCode("CONCURRENT_TARGET_CHANGED")
	}
	if !p.old.at(p.leaf.Target) {
		return errCode("PINNED_PUBLIC_NAME_CHANGED")
	}
	if test.beforeRename != nil {
		test.beforeRename()
	}
	if e = rename(p.old, backup); e != nil {
		return e
	}
	p.state.OldRetired = true
	if test.beforePublish != nil {
		test.beforePublish()
	}
	if e = rename(stage, p.leaf.Target); e != nil {
		publishErr := e
		// Restore only this original object into an absent public name, never
		// overwrite a concurrent writer or erase evidence after a partial step.
		if missing(p.leaf.Target) {
			p.state.RestoreAttempted = true
			if restoreErr := rename(p.old, p.leaf.Target); restoreErr != nil {
				return fmt.Errorf("PUBLISH_FAILED_RESTORE_FAILED:%w (publish:%v)", restoreErr, publishErr)
			}
			p.state.Restored = true
			if readbackErr := p.old.verifyName(p.leaf.Target); readbackErr != nil {
				return fmt.Errorf("PUBLISH_FAILED_RESTORE_UNVERIFIED:%w (publish:%v)", readbackErr, publishErr)
			}
			return fmt.Errorf("PUBLISH_FAILED_RESTORED:%w", publishErr)
		}
		return fmt.Errorf("PUBLISH_FAILED_TARGET_PRESENT_OR_UNVERIFIED:%w", publishErr)
	}
	p.state.Published = true
	if test.afterPublish != nil {
		test.afterPublish(stage)
	}
	h, e = stage.hash()
	if e != nil {
		return errCode("INSTALLED_READBACK_FAILED:READ_ERROR")
	}
	if h != p.leaf.New {
		return errCode("INSTALLED_READBACK_FAILED:BYTES_CHANGED")
	}
	if e = stage.verifyName(p.leaf.Target); e != nil {
		return fmt.Errorf("INSTALLED_READBACK_FAILED:PUBLIC_IDENTITY:%w", e)
	}
	old, e := p.old.hash()
	if e != nil {
		return errCode("BACKUP_CHANGED:READ_ERROR")
	}
	if old != p.leaf.Old {
		return errCode("BACKUP_CHANGED:BYTES_CHANGED")
	}
	if e = p.old.verifyName(backup); e != nil {
		return fmt.Errorf("BACKUP_CHANGED:NAME:%w", e)
	}
	p.state.Verified = true
	return nil
}
func run(apply bool) ([]map[string]any, error) {
	if e := originalProofs(); e != nil {
		return nil, e
	}
	var ps []*prepared
	defer func() {
		for _, p := range ps {
			p.close()
		}
	}()
	for _, l := range leaves {
		p, e := prepare(l)
		if e != nil {
			return nil, fmt.Errorf("%s:%s", l.Kind, e)
		}
		ps = append(ps, p)
	}
	var rows []map[string]any
	if !apply {
		for _, p := range ps {
			rows = append(rows, map[string]any{"kind": p.leaf.Kind, "qualified": true, "changed": false})
		}
		return rows, nil
	}
	lock, e := openPin("D:/CC/Tools/cleanup-8h/run.lock", syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, 3)
	if e != nil {
		return nil, errCode("HOURLY_CLEANER_LOCK_MISSING_OR_BUSY")
	}
	defer lock.close()
	// The consumer is installed before the producer starts writing v3 records.
	for _, index := range []int{1, 0} {
		p := ps[index]
		e := p.install(hooks{})
		row := p.result()
		if e != nil {
			row["retained_reason"] = e.Error()
			rows = append(rows, row)
			return rows, e
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--completed-build-check" || os.Args[1] == "--completed-build-apply") {
		completedBuildMain(os.Args[1] == "--completed-build-apply")
		return
	}
	if len(os.Args) != 2 || (os.Args[1] != "--check" && os.Args[1] != "--apply" && os.Args[1] != "--native-proof-check" && os.Args[1] != "--native-proof-apply") {
		fmt.Println(`{"qualified":false,"reason":"INVALID_ARGUMENTS"}`)
		os.Exit(2)
	}
	var rows []map[string]any
	var e error
	head := sourceHead
	if strings.HasPrefix(os.Args[1], "--native-proof-") {
		head = nativeSourceHead
		rows, e = refreshNativeProof(os.Args[1] == "--native-proof-apply")
	} else {
		rows, e = run(os.Args[1] == "--apply")
	}
	r := map[string]any{"domain": "owned-cleanup-software-3757", "source_head": head, "qualified": e == nil, "results": rows, "data_access_granted": false, "general_guard_changed": false, "at": time.Now().UTC().Format(time.RFC3339Nano)}
	if e != nil {
		r["reason"] = e.Error()
	}
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		os.Exit(1)
	}
	if e != nil {
		os.Exit(1)
	}
}
