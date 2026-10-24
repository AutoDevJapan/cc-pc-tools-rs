//go:build windows

// safe-delete plans visible PowerShell cleanup and quarantines owned scratch.
// It never deletes data itself or changes Codex execution policy.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unsafe"
)

type failure struct {
	Code    string         `json:"code"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *failure) Error() string { return e.Code }
func fail(code string) error     { return &failure{Code: code} }

type identityRecord struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Root    string `json:"root"`
	Path    string `json:"path"`
	Volume  uint32 `json:"volume"`
	FileID  uint64 `json:"file_id"`
	Created string `json:"created_at"`
	Project string `json:"project"`
	Content string `json:"content"`
}
type receipt struct {
	identityRecord
	Integrity   string `json:"integrity"`
	State       string `json:"state"`
	Destination string `json:"destination,omitempty"`
}
type config struct {
	Store, Quarantine, Temp string
	MaxEntries              int
	MaxBytes                int64
	WriteReason             func(string, []byte) error
	FindShell               func() (string, error)
}
type measured struct {
	Entries    int                               `json:"entries"`
	Bytes      int64                             `json:"bytes"`
	Attributes bool                              `json:"protected_attributes"`
	links      map[fileIdentityKey]hardLinkCount `json:"-"`
}
type fileIdentityKey struct {
	volume uint32
	fileID uint64
}
type hardLinkCount struct {
	inside uint32
	total  uint32
}

var reasons = map[string]string{
	"policy":     "Codex の実行ポリシーが、削除コマンドを実行前に拒否した。",
	"windows":    "Windows が削除を拒否した。ロックと確認できない場合は、アクセス権や許可も確認する。",
	"locked":     "Windows の共有違反などで、使用中のファイルの削除が拒否された。",
	"attributes": "隠し属性または読み取り専用属性があるため、強制削除を使わず退避した。",
	"size":       "削除計画の容量上限を超えたため、削除せず退避した。",
	"hardlink":   "補助コマンド自身が外部ハードリンクを検出して削除計画を拒否した。完成動画と実体を共有する固定の一時名を、同じドライブへのフォルダー移動で保持した。OSや自動承認の拒否ではない。",
}

func defaultConfig() config {
	exe, _ := os.Executable()
	return config{Store: filepath.Join(filepath.Dir(exe), "receipts-go"), Quarantine: `C:\Users\makhmeto\Downloads\削除希望`,
		Temp: os.TempDir(), MaxEntries: 10000, MaxBytes: 512 * 1024 * 1024}
}
func label(s string) bool {
	n := 0
	for _, r := range s {
		n++
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return n > 0 && n <= 40
}
func same(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
func protected(s string) bool {
	switch strings.ToLower(s) {
	case ".git", ".codex", ".agents", ".ssh":
		return true
	}
	return false
}
func checked(value string) (string, error) {
	p, err := filepath.Abs(value)
	if err != nil {
		return "", fail("INVALID_PATH")
	}
	for node := p; ; node = filepath.Dir(node) {
		if protected(filepath.Base(node)) {
			return "", fail("PROTECTED_PATH")
		}
		u, e := syscall.UTF16PtrFromString(node)
		if e != nil {
			return "", fail("INVALID_PATH")
		}
		a, e := syscall.GetFileAttributes(u)
		if e == nil {
			if a&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				return "", fail("LINK_OR_REPARSE_POINT")
			}
			if !same(node, p) && a&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
				return "", fail("ANCESTOR_NOT_DIRECTORY")
			}
		} else if !errors.Is(e, syscall.ERROR_FILE_NOT_FOUND) && !errors.Is(e, syscall.ERROR_PATH_NOT_FOUND) {
			return "", fail("PATH_METADATA_FAILED")
		}
		parent := filepath.Dir(node)
		if same(parent, node) {
			break
		}
	}
	return p, nil
}
func fileIdentity(path string) (syscall.ByHandleFileInformation, error) {
	u, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.ByHandleFileInformation{}, fail("INVALID_PATH")
	}
	// FILE_READ_ATTRIBUTES only: never open file contents to establish identity.
	h, err := syscall.CreateFile(u, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil,
		syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return syscall.ByHandleFileInformation{}, fail("IDENTITY_READ_FAILED")
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil {
		return info, fail("IDENTITY_READ_FAILED")
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return info, fail("LINK_OR_REPARSE_POINT")
	}
	return info, nil
}
func fileID(i syscall.ByHandleFileInformation) uint64 {
	return uint64(i.FileIndexHigh)<<32 | uint64(i.FileIndexLow)
}
func runHidden(timeout time.Duration, exe string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Output()
}
func (c config) root(value string) (string, error) {
	p, err := checked(value)
	if err != nil {
		return "", err
	}
	i, err := os.Stat(p)
	if err != nil || !i.IsDir() {
		return "", fail("ROOT_NOT_DIRECTORY")
	}
	tmp, err := checked(c.Temp)
	if err != nil {
		return "", err
	}
	if same(p, tmp) || strings.HasPrefix(strings.ToLower(p), strings.ToLower(tmp+string(os.PathSeparator))) {
		return p, nil
	}
	output, err := runHidden(10*time.Second, "git", "-C", p, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fail("ROOT_MUST_BE_TEMP_OR_WORKTREE_TOP")
	}
	top, err := checked(strings.TrimSpace(string(output)))
	if err != nil {
		return "", fail("ROOT_MUST_BE_TEMP_OR_WORKTREE_TOP")
	}
	// Git expands Windows short (8.3) path components. Compare the checked
	// directories' actual identities, rather than rejecting spelling aliases.
	requested, requestErr := fileIdentity(p)
	gitTop, topErr := fileIdentity(top)
	if requestErr != nil || topErr != nil || requested.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 ||
		gitTop.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 || requested.VolumeSerialNumber != gitTop.VolumeSerialNumber || fileID(requested) != fileID(gitTop) {
		return "", fail("ROOT_MUST_BE_TEMP_OR_WORKTREE_TOP")
	}
	return p, nil
}
func (c config) recordPath(id string) (string, error) {
	if len(id) != 32 {
		return "", fail("INVALID_RECEIPT_ID")
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return "", fail("INVALID_RECEIPT_ID")
		}
	}
	return checked(filepath.Join(c.Store, id+".json"))
}
func digest(r receipt) string {
	b, _ := json.Marshal(r.identityRecord)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (c config) save(r receipt, newRecord bool) error {
	p, err := c.recordPath(r.ID)
	if err != nil {
		return err
	}
	if os.MkdirAll(c.Store, 0700) != nil {
		return fail("RECEIPT_WRITE_FAILED")
	}
	if newRecord {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return fail("RECEIPT_ALREADY_EXISTS")
		}
		r.Integrity = digest(r)
	}
	// Commit complete JSON atomically. A failed write leaves the previous receipt intact.
	f, err := os.OpenFile(p+".writing", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail("RECEIPT_WRITE_FAILED")
	}
	err = json.NewEncoder(f).Encode(r)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return fail("RECEIPT_WRITE_FAILED")
	}
	if os.Rename(p+".writing", p) != nil {
		return fail("RECEIPT_COMMIT_FAILED")
	}
	return nil
}
func (c config) create(root, project, content string) (receipt, error) {
	var r receipt
	p, err := c.root(root)
	if err != nil {
		return r, err
	}
	if !label(project) || !label(content) {
		return r, fail("INVALID_LABEL")
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return r, fail("ID_GENERATION_FAILED")
	}
	id := hex.EncodeToString(random[:])
	target := filepath.Join(p, "codex-owned-"+id)
	if err = os.Mkdir(target, 0700); err != nil {
		return r, fail("FOLDER_CREATE_FAILED")
	}
	r = receipt{identityRecord: identityRecord{Version: 2, ID: id, Root: p, Path: target, Created: time.Now().Format(time.RFC3339Nano), Project: project, Content: content}, State: "owned"}
	i, err := fileIdentity(target)
	if err == nil {
		r.Volume = i.VolumeSerialNumber
		r.FileID = fileID(i)
		r.Integrity = digest(r)
		err = c.save(r, true)
	}
	if err != nil {
		return r, &failure{Code: "OWNERSHIP_RECORD_WRITE_FAILED", Details: map[string]any{"id": id, "path": target, "root": p, "ownership_record_complete": false}}
	}
	return r, nil
}
func (c config) load(id string, requireTarget bool) (receipt, error) {
	var r receipt
	p, err := c.recordPath(id)
	if err != nil {
		return r, err
	}
	i, err := fileIdentity(p)
	if err != nil {
		return r, err
	}
	if i.NumberOfLinks != 1 || i.FileSizeHigh != 0 || i.FileSizeLow > 8192 || i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return r, fail("INVALID_RECEIPT_FILE")
	}
	f, err := os.Open(p)
	if err != nil {
		return r, fail("RECEIPT_READ_FAILED")
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 8193))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil {
		return r, fail("INVALID_RECEIPT_FILE")
	}
	if r.Version != 2 || r.ID != id || r.State != "owned" {
		return r, fail("RECEIPT_NOT_OWNED")
	}
	if digest(r) != r.Integrity {
		return r, fail("RECEIPT_INTEGRITY_MISMATCH")
	}
	if !label(r.Project) || !label(r.Content) {
		return r, fail("INVALID_LABEL")
	}
	root, err := c.root(r.Root)
	if err != nil {
		return r, err
	}
	target, err := checked(r.Path)
	if err != nil {
		return r, err
	}
	if !same(target, filepath.Join(root, "codex-owned-"+id)) {
		return r, fail("RECEIPT_TARGET_MISMATCH")
	}
	if requireTarget {
		i, err = fileIdentity(target)
		if err != nil {
			return r, err
		}
		if i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 || i.VolumeSerialNumber != r.Volume || fileID(i) != r.FileID {
			return r, fail("TARGET_IDENTITY_CHANGED")
		}
	}
	return r, nil
}
func (c config) measure(path string) (measured, error) {
	return c.measureWithShared(path, nil)
}
func (c config) measureWithShared(path string, shared *geminiScope) (measured, error) {
	result := measured{links: make(map[fileIdentityKey]hardLinkCount)}
	pending := []string{path}
	for len(pending) != 0 {
		p := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if protected(filepath.Base(p)) {
			return result, fail("PROTECTED_PATH")
		}
		info, err := fileIdentity(p)
		if err != nil {
			return result, err
		}
		result.Attributes = result.Attributes || info.FileAttributes&3 != 0
		if info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
			key := fileIdentityKey{volume: info.VolumeSerialNumber, fileID: fileID(info)}
			count := result.links[key]
			if count.total != 0 && count.total != info.NumberOfLinks {
				return result, fail("HARD_LINK")
			}
			count.total = info.NumberOfLinks
			count.inside++
			result.links[key] = count
			if info.NumberOfLinks > 1 && shared != nil && !shared.accepts(p, path, info) {
				return result, fail("HARD_LINK")
			}
			result.Bytes += int64(uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow))
			continue
		}
		directory, err := os.Open(p)
		if err != nil {
			return result, fail("CONTENT_METADATA_FAILED")
		}
		for {
			// Read bounded chunks rather than loading a whole large directory at once.
			children, readErr := directory.ReadDir(64)
			for _, child := range children {
				result.Entries++
				if result.Entries > c.MaxEntries {
					directory.Close()
					return result, fail("ENTRY_LIMIT_EXCEEDED")
				}
				pending = append(pending, filepath.Join(p, child.Name()))
			}
			if readErr != nil {
				directory.Close()
				if readErr != io.EOF {
					return result, fail("CONTENT_METADATA_FAILED")
				}
				break
			}
		}
	}
	// A hard link is contained only when every directory entry for its
	// volume/file identity was found inside this bounded, reparse-free walk.
	// Any external name, concurrent count change, or incomplete walk fails closed.
	for _, count := range result.links {
		if count.inside != count.total {
			if shared == nil || count.inside != 1 || count.total != 2 {
				return result, fail("HARD_LINK")
			}
		}
	}
	return result, nil
}
func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func (c config) plan(id string) (map[string]any, error) {
	r, err := c.load(id, true)
	if err != nil {
		return nil, err
	}
	m, err := c.measure(r.Path)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"id": id, "path": r.Path, "entries": m.Entries, "bytes": m.Bytes, "protected_attributes": m.Attributes}
	if m.Attributes || m.Bytes > c.MaxBytes {
		result["action"] = "quarantine"
		result["reason"] = "size"
		if m.Attributes {
			result["reason"] = "attributes"
		}
	} else {
		result["action"] = "submit_to_exec_tool"
		result["powershell_command"] = "Remove-Item -LiteralPath " + quote(r.Path) + " -Recurse -ErrorAction Stop"
	}
	return result, nil
}
func exists(path string) bool { _, err := os.Lstat(path); return !os.IsNotExist(err) }
func (c config) finish(id string) (map[string]any, error) {
	r, err := c.load(id, false)
	if err != nil {
		return nil, err
	}
	if exists(r.Path) {
		return nil, fail("TARGET_STILL_EXISTS")
	}
	r.State = "source_absent"
	if err = c.save(r, false); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "state": r.State}, nil
}
func driveSpace() []map[string]any {
	dll := syscall.NewLazyDLL("kernel32.dll")
	mask, _, _ := dll.NewProc("GetLogicalDrives").Call()
	proc := dll.NewProc("GetDiskFreeSpaceExW")
	var result []map[string]any
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		drive := fmt.Sprintf("%c:\\", 'A'+i)
		p, _ := syscall.UTF16PtrFromString(drive)
		var free, total, totalFree uint64
		ok, _, _ := proc.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
		d := map[string]any{"drive": drive, "available": ok != 0}
		if ok != 0 {
			d["free_bytes"] = free
			d["total_bytes"] = total
		}
		result = append(result, d)
	}
	return result
}
func (c config) quarantine(id, reason string) (result map[string]any, err error) {
	if _, ok := ci3574ResourceByID(id); ok && reason == "windows" {
		scope, e := c.ci3574Recovery(id)
		if e != nil {
			return nil, e
		}
		return c.quarantinePreserved(id, reason, nil, scope)
	}
	if id == console3451ID && reason == "windows" {
		scope, e := c.console3451Recovery(id)
		if e != nil {
			return nil, e
		}
		return c.quarantinePreserved(id, reason, nil, scope)
	}
	if id == ci3174ID && reason == "windows" {
		scope, unlock, e := c.ci3174Recovery(id)
		if e != nil {
			return nil, e
		}
		defer unlock()
		return c.quarantinePreserved(id, reason, nil, scope)
	}
	if c.ci3250Candidate(id) {
		r, e := c.load(id, true)
		if e != nil {
			return nil, e
		}
		if ci3250PreservedReason(r.Root, reason) {
			scope, unlock, e := c.ci3250Recovery(id)
			if e != nil {
				var refused *failure
				if errors.As(e, &refused) && refused.Code == "CI_RETIREMENT_UNQUALIFIED" {
					return c.quarantineScoped(id, reason, nil)
				}
				return nil, e
			}
			defer unlock()
			r, e := c.load(id, true)
			if e != nil {
				return nil, e
			}
			c.Quarantine = ci3250QuarantineParent(r.Root, c.Quarantine)
			return c.quarantinePreserved(id, reason, nil, scope)
		}
	}
	var scope *geminiScope
	if reason == "hardlink" {
		scope, err = c.geminiRecovery(id)
		if err != nil {
			return nil, err
		}
	}
	return c.quarantineScoped(id, reason, scope)
}
func (c config) quarantineScoped(id, reason string, scope *geminiScope) (result map[string]any, err error) {
	return c.quarantinePreserved(id, reason, scope, nil)
}
func (c config) quarantinePreserved(id, reason string, scope *geminiScope, ci *ci3174Scope) (result map[string]any, err error) {
	text, ok := reasons[reason]
	if !ok {
		return nil, fail("INVALID_REASON")
	}
	if ci != nil {
		text = "終了と所有を確認した検査資源を、全項目の属性を照合して原物のまま退避する。本文とリンク先は未読。Windowsや実行前ポリシーが拒否したと推定するものではない。"
	}
	r, err := c.load(id, true)
	if err != nil {
		return nil, err
	}
	var ciBefore map[string]ci3174Entry
	var m measured
	if ci != nil {
		ciBefore, m, err = ci.snapshot(r.Path)
	} else {
		m, err = c.measureWithShared(r.Path, scope)
	}
	if err != nil {
		return nil, err
	}
	var before map[string]geminiEntry
	if scope != nil {
		before, err = scope.snapshot(r.Path)
		if err != nil {
			return nil, err
		}
	}
	dest := filepath.Join(c.Quarantine, time.Now().Format("20060102")+"-"+r.Project+"-"+r.Content+"-"+id[:8])
	phase := "preflight"
	reasonWritten, moveConfirmed, recordConfirmed := false, false, false
	whyPath := filepath.Join(dest, "理由.txt")
	// Every failure after source validation reports both locations and capacity.
	defer func() {
		if err != nil {
			var e *failure
			code := "QUARANTINE_FAILED"
			if errors.As(err, &e) {
				code = e.Code
			}
			if reasonWritten {
				status := "退避状態: 未完了。元の場所と退避先の両方を確認すること。"
				if moveConfirmed {
					status = "データの移動は確認済み。記録処理が未完了。"
				}
				if recordConfirmed {
					status = "データの移動と所有記録の更新は確認済み。理由の更新が未完了。"
				}
				_ = appendReason(whyPath, status+" 段階: "+phase+" 理由: "+code+"\n")
			}
			err = &failure{Code: code, Details: map[string]any{"source": r.Path, "destination": dest, "bytes": m.Bytes, "phase": phase, "drive_space": driveSpace()}}
		}
	}()
	if _, err = checked(dest); err != nil {
		return nil, err
	}
	if !same(filepath.VolumeName(r.Path), filepath.VolumeName(dest)) {
		return nil, fail("QUARANTINE_CROSS_DRIVE")
	}
	find := c.FindShell
	if find == nil {
		find = func() (string, error) { return exec.LookPath("pwsh.exe") }
	}
	shell, e := find()
	if e != nil {
		return nil, fail("POWERSHELL_NOT_FOUND")
	}
	if os.MkdirAll(c.Quarantine, 0700) != nil {
		return nil, fail("QUARANTINE_PARENT_CREATE_FAILED")
	}
	if os.Mkdir(dest, 0700) != nil {
		return nil, fail("QUARANTINE_DESTINATION_CREATE_FAILED")
	}
	phase = "reason"
	why := fmt.Sprintf("退避状態: 未完了（理由を保存したが、移動前）。\n元の場所: %s\n大きさ: %d byte (%.3f MiB)\n削除できなかった理由: %s\n作成記録: %s\n記録時刻: %s\n退避方法: PowerShell の Move-Item。強制削除・別の削除 API は使用しない。\n", r.Path, m.Bytes, float64(m.Bytes)/1048576, text, id, time.Now().Format(time.RFC3339Nano))
	write := c.WriteReason
	if write == nil {
		write = func(p string, b []byte) error { return os.WriteFile(p, b, 0600) }
	}
	if write(whyPath, []byte(why)) != nil {
		return nil, fail("QUARANTINE_REASON_WRITE_FAILED")
	}
	reasonWritten = true
	if ci != nil {
		phase = "preservation_record"
		if ci.originalReceipt == nil {
			return nil, fail("CI_PROOF_INVALID")
		}
		original, readErr := ci.originalReceipt()
		if readErr != nil {
			return nil, readErr
		}
		if os.WriteFile(filepath.Join(dest, "original-receipt.json"), original, 0600) != nil {
			return nil, fail("PRESERVATION_RECORD_WRITE_FAILED")
		}
		b, encodeErr := json.MarshalIndent(ciBefore, "", "  ")
		if encodeErr != nil || os.WriteFile(filepath.Join(dest, "preservation.json"), b, 0600) != nil {
			return nil, fail("PRESERVATION_RECORD_WRITE_FAILED")
		}
	}
	if scope != nil {
		phase = "preservation_record"
		b, encodeErr := json.MarshalIndent(before, "", "  ")
		if encodeErr != nil || os.WriteFile(filepath.Join(dest, "preservation.json"), b, 0600) != nil {
			return nil, fail("PRESERVATION_RECORD_WRITE_FAILED")
		}
	}
	phase = "move"
	if ci != nil {
		if err = ci.settled(); err != nil {
			return nil, err
		}
		if err = ci.verify(r.Path, ciBefore, r); err != nil {
			return nil, err
		}
	}
	if scope != nil {
		if err = scope.verify(r.Path, before, r); err != nil {
			return nil, err
		}
	}
	command := "try { Move-Item -LiteralPath " + quote(r.Path) + " -Destination " + quote(filepath.Join(dest, "files")) + " -ErrorAction Stop } catch { exit 1 }"
	if _, e = runHidden(60*time.Second, shell, "-NoProfile", "-NonInteractive", "-Command", command); e != nil {
		return nil, fail("QUARANTINE_MOVE_FAILED")
	}
	if exists(r.Path) || !exists(filepath.Join(dest, "files")) {
		return nil, fail("QUARANTINE_MOVE_FAILED")
	}
	if scope != nil {
		phase = "verify_preservation"
		if err = scope.verify(filepath.Join(dest, "files"), before, r); err != nil {
			return nil, err
		}
	}
	if ci != nil {
		phase = "verify_preservation"
		if err = ci.verify(filepath.Join(dest, "files"), ciBefore, r); err != nil {
			return nil, err
		}
	}
	moveConfirmed = true
	phase = "record"
	r.State = "quarantined"
	r.Destination = dest
	if err = c.save(r, false); err != nil {
		return nil, err
	}
	recordConfirmed = true
	phase = "reason_complete"
	if appendReason(whyPath, "退避状態: 完了。元の場所の不在・退避先の存在・所有記録の更新を確認した。\n") != nil {
		return nil, fail("QUARANTINE_REASON_UPDATE_FAILED")
	}
	return map[string]any{"id": id, "state": r.State, "destination": dest, "bytes": m.Bytes, "entries": m.Entries}, nil
}

func appendReason(path, text string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(text)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func main() {
	if len(os.Args) < 2 || os.Args[1] == "--help" {
		fmt.Println("safe-delete create --project NAME [--content NAME] [--root PATH]\nsafe-delete plan|finish --id ID\nsafe-delete quarantine --id ID --reason policy|windows|locked|attributes|size|hardlink\nhardlink is restricted to ToDo2307's fixed receipt and media.\nToDo3174's fixed collected CI receipt uses windows quarantine only, with root client guard and opaque preservation.\nDeletion plans must be submitted separately to the caller's ordinary exec tool.")
		return
	}
	c := defaultConfig()
	fs := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", c.Temp, "")
	project := fs.String("project", "", "")
	content := fs.String("content", "temp", "")
	id := fs.String("id", "", "")
	reason := fs.String("reason", "", "")
	var result any
	var err error
	if fs.Parse(os.Args[2:]) != nil || fs.NArg() != 0 {
		err = fail("INVALID_ARGUMENTS")
	} else {
		switch os.Args[1] {
		case "inspect-console":
			// A read-only candidate can verify the original producer record before
			// distribution. No caller-supplied store or mutation is accepted.
			c.Store = "D:/CC/Tools/safe-delete/receipts-go"
			var scope *ci3174Scope
			scope, err = c.console3451Recovery(*id)
			if err == nil {
				var measured measured
				_, measured, err = scope.snapshot(console3451Root)
				result = map[string]any{"id": *id, "state": "qualified_for_preservation", "bytes": measured.Bytes, "entries": measured.Entries, "changed": false}
			}
		case "create":
			result, err = c.create(*root, *project, *content)
		case "plan":
			result, err = c.plan(*id)
		case "finish":
			result, err = c.finish(*id)
		case "quarantine":
			result, err = c.quarantine(*id, *reason)
		default:
			err = fail("INVALID_ACTION")
		}
	}
	if err != nil {
		var e *failure
		if !errors.As(err, &e) {
			e = &failure{Code: "IO_FAILED"}
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": false, "code": e.Code, "details": e.Details})
		os.Exit(2)
	}
	json.NewEncoder(os.Stdout).Encode(result)
}
