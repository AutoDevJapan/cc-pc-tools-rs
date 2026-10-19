//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/binary"
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

// No caller-supplied root, receipt or link allowlist. These are the original
// receipts from the three collected credential-free SDK cases of ToDo2997.
var sdkReceipts = map[string]string{
	"7155bdd37afc8e75e4dbf39509a5f18c": "bee7d4f9f190b6202a4f8e04101dd3ed1ce4c7ca1dd78d7bd79ad87b0b8bda1b",
	"c0d0c333fca8d97bab2fb8219b1ee1a8": "78ca2f18e2c916232178aafab4a540baa9b4161b6e8bd70b71f5f668d67813b3",
	"5eac22dc8041f9b3a471dbb392f86b99": "e4d877155397dca4233fc061cbc347e535dd908b77d08aa5da4cffea796e15e8",
}
var sdkAliases = strings.Fields("cargo-clippy.exe cargo-fmt.exe cargo-miri.exe cargo.exe clippy-driver.exe rls.exe rust-analyzer.exe rust-gdb.exe rust-gdbgui.exe rust-lldb.exe rustc.exe rustdoc.exe rustfmt.exe")

const sdkBin = `runtime\sdk\bootstrap\bin`

type sdkScope struct{ source string }

const sdkCache = `runtime\home\AppData\Local\Microsoft\Windows\INetCache`

func sdkTargetSame(a, b string) bool {
	// os.Readlink exposes the Win32 extended DOS spelling of junction targets.
	// Only a drive-qualified prefix is normalized; UNC/device targets stay out.
	if strings.HasPrefix(a, `\\?\`) && len(a) > 6 && a[5] == ':' {
		a = a[4:]
	}
	return same(a, b)
}

func sdkFinalPath(path string) (string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := syscall.CreateFile(p, 0x80, 7, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, 32768)
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW").Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n == 0 || n >= uintptr(len(buf)) {
		return "", fail("SDK_FINAL_PATH_FAILED")
	}
	return syscall.UTF16ToString(buf[:n]), nil
}

func init() {
	reasons["sdk-links"] = "所有確認済みの3個のSDK検査データだけを対象に、rustupの相対リンクと全内容を保存して退避する。汎用のリンク保護による道具内の拒否であり、実行審査の拒否ではない。"
}

func (c config) sdkRecovery(id string) (receipt, error) {
	want, ok := sdkReceipts[id]
	if !ok {
		return receipt{}, fail("SDK_RECOVERY_SCOPE_MISMATCH")
	}
	r, err := c.load(id, true)
	if err != nil {
		return r, err
	}
	if r.Integrity != want {
		return r, fail("SDK_RECOVERY_SCOPE_MISMATCH")
	}
	if err = clientGuard(r.Path); err != nil {
		return r, err
	}
	return r, nil
}

// Only known non-secret JSON evidence is read here. SDK/checkout bytes are
// hashed for preservation, never interpreted as credentials or executed.
func sdkJSON(path string, out any) error {
	if _, err := checked(path); err != nil {
		return err
	}
	i, err := fileIdentity(path)
	if err != nil || i.NumberOfLinks != 1 || i.FileSizeHigh != 0 || i.FileSizeLow > 65536 || i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fail("SDK_EVIDENCE_INVALID")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if json.NewDecoder(io.LimitReader(f, 65537)).Decode(out) != nil {
		return fail("SDK_EVIDENCE_INVALID")
	}
	return nil
}

func sdkCollected(root, id string) error {
	var plan struct {
		Instance string `json:"instance"`
		Runtime  bool   `json:"runtime_fixture"`
	}
	var prepared struct {
		Free     bool   `json:"credential_free"`
		Official bool   `json:"official_runner_executed"`
		Service  string `json:"service"`
	}
	var drain struct {
		Drained   bool `json:"job_drained"`
		Signalled bool `json:"retained_handles_signalled"`
	}
	var checkout struct {
		Synthetic  bool `json:"synthetic_only"`
		AuthAbsent bool `json:"auth_absent"`
	}
	for _, item := range []struct {
		name  string
		value any
	}{
		{"plan.json", &plan}, {"prepared.json", &prepared}, {`runtime\drain.json`, &drain}, {`runtime\checkout-verified.json`, &checkout},
	} {
		if err := sdkJSON(filepath.Join(root, item.name), item.value); err != nil {
			return err
		}
	}
	if plan.Instance != id || !plan.Runtime || !prepared.Free || prepared.Official || prepared.Service != "CC-PSW-fixture" || !drain.Drained || !drain.Signalled || !checkout.Synthetic || !checkout.AuthAbsent {
		return fail("SDK_NOT_COLLECTED")
	}
	files, err := filepath.Glob(filepath.Join(root, "collection-*.json"))
	if err != nil || len(files) != 1 {
		return fail("SDK_NOT_COLLECTED")
	}
	var collected struct {
		Service   string `json:"service"`
		Idle      bool   `json:"service_idle_confirmed"`
		Owner     bool   `json:"owner_reset"`
		ACL       bool   `json:"tree_acl_reset"`
		Grants    *int   `json:"additional_ace_count"`
		Processes *int   `json:"queryable_account_process_count"`
		Entries   int    `json:"tree_entries_verified_without_service_grant"`
	}
	if err = sdkJSON(files[0], &collected); err != nil {
		return err
	}
	if collected.Service != "CC-PSW-fixture" || !collected.Idle || !collected.Owner || !collected.ACL || collected.Grants == nil || *collected.Grants != 0 || collected.Processes == nil || *collected.Processes != 0 || collected.Entries < 1 {
		return fail("SDK_NOT_COLLECTED")
	}
	return nil
}

func (c config) quarantineSDK(id string) (map[string]any, error) {
	r, err := c.sdkRecovery(id)
	if err != nil {
		return nil, err
	}
	// Exclude concurrent prepare/execute/collect for the fixed fixture identity.
	// No service stop, registration, profile or ACL change is made here.
	p, _ := syscall.UTF16PtrFromString(`C:\ProgramData\CC\passshare-ci-windows\CC-PSW-fixture.lock`)
	h, err := syscall.CreateFile(p, 0xC0000000, 0, nil, syscall.OPEN_EXISTING, 0x80, 0)
	if err != nil {
		return nil, fail("SDK_IDENTITY_BUSY_OR_LOCK_UNAVAILABLE")
	}
	defer syscall.CloseHandle(h)
	b, err := runHidden(15*time.Second, "pwsh.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; $s=Get-CimInstance Win32_Service -Filter "Name='CC-PSW-fixture'"; if(!$s -or $s.State -ne 'Stopped' -or $s.ProcessId -ne 0){exit 1}`)
	_ = b
	if err != nil {
		return nil, fail("SDK_SERVICE_NOT_IDLE")
	}
	if err = sdkCollected(r.Path, id); err != nil {
		return nil, err
	}
	// Same-drive quarantine is a preservation operation, never a delete plan.
	c.Quarantine = filepath.Join(filepath.VolumeName(r.Path)+`\`, "削除希望")
	if err = clientGuard(c.Quarantine); err != nil {
		return nil, err
	}
	return c.quarantineScoped(id, "sdk-links", &sdkScope{source: r.Path})
}

func sdkReparse(path string) ([]byte, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, 0, 7, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(h)
	buf := make([]byte, 16384)
	var n uint32
	if syscall.DeviceIoControl(h, 0x900a8, nil, 0, &buf[0], uint32(len(buf)), &n, nil) != nil || n < 16 {
		return nil, fail("SDK_REPARSE_READ_FAILED")
	}
	return buf[:n], nil
}

func sdkLink(path string, raw []byte) (string, error) {
	if len(raw) < 20 || binary.LittleEndian.Uint32(raw[:4]) != 0xa000000c || binary.LittleEndian.Uint32(raw[16:20]) != 1 {
		return "", fail("SDK_LINK_NOT_RELATIVE_SYMLINK")
	}
	target, err := os.Readlink(path)
	if err != nil || target != "rustup.exe" {
		return "", fail("SDK_LINK_TARGET_MISMATCH")
	}
	final, err := sdkFinalPath(path)
	if err != nil || !sdkTargetSame(final, filepath.Join(filepath.Dir(path), "rustup.exe")) {
		return "", fail("SDK_LINK_TARGET_MISMATCH")
	}
	if _, err = checked(final); err != nil {
		return "", err
	}
	return target, nil
}

func sdkProtected(rel string) bool {
	// Only these two synthetic Git trees may be preserved. Ordinary plan and
	// quarantine still reject every .git; there is no public generic exception.
	if strings.EqualFold(filepath.Base(rel), ".git") {
		return !same(rel, `runtime\checkout-origin\.git`) && !same(rel, `runtime\runner\_work\PassShare\PassShare\.git`)
	}
	name := strings.ToLower(filepath.Base(rel))
	return protected(name) || name == ".credentials" || name == ".credentials_rsaparams" || name == "id_rsa" || name == "id_ed25519"
}

func (s *sdkScope) snapshot(root string) (map[string]geminiEntry, error) {
	if _, err := checked(root); err != nil {
		return nil, err
	}
	result := map[string]geminiEntry{}
	counts := map[fileIdentityKey]hardLinkCount{}
	aliases := map[string]bool{}
	for _, name := range sdkAliases {
		aliases[strings.ToLower(filepath.Join(sdkBin, name))] = true
	}
	seen := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fail("CONTENT_METADATA_FAILED")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, `..\`) || filepath.IsAbs(rel) {
			return fail("INVALID_PATH")
		}
		if len(result) >= 10001 {
			return fail("ENTRY_LIMIT_EXCEEDED")
		}
		if sdkProtected(rel) {
			return fail("PROTECTED_PATH")
		}
		i, err := rawIdentity(path)
		if err != nil {
			return &failure{Code: "SDK_METADATA_READ_FAILED", Details: map[string]any{"relative": rel}}
		}
		entry := geminiMetadata(i)
		if i.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			raw, e := sdkReparse(path)
			if e != nil {
				return e
			}
			sum := sha256.Sum256(raw)
			entry.Hash = hex.EncodeToString(sum[:])
			if same(rel, filepath.Join(sdkCache, "Content.IE5")) && s.source != "" {
				// Preserve the absolute junction bytes; never enumerate through it.
				// After relocation its original absolute name is intentionally dangling.
				if binary.LittleEndian.Uint32(raw[:4]) != 0xa0000003 || i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
					return fail("SDK_CACHE_JUNCTION_MISMATCH")
				}
				target, e := os.Readlink(path)
				if e != nil || !sdkTargetSame(target, filepath.Join(s.source, sdkCache, "IE")) {
					return fail("SDK_CACHE_JUNCTION_MISMATCH")
				}
				mapped := filepath.Join(root, sdkCache, "IE")
				if _, e = checked(mapped); e != nil {
					return e
				}
				ti, e := fileIdentity(mapped)
				if e != nil || ti.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
					return fail("SDK_CACHE_JUNCTION_MISMATCH")
				}
				if same(root, s.source) {
					final, e := sdkFinalPath(path)
					if e != nil || !sdkTargetSame(final, mapped) {
						return fail("SDK_CACHE_JUNCTION_MISMATCH")
					}
				} else if exists(s.source) {
					return fail("SDK_SOURCE_STILL_PRESENT")
				}
				entry.Target = target
			} else {
				if !aliases[strings.ToLower(rel)] || i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 || i.NumberOfLinks != 1 {
					return fail("LINK_OR_REPARSE_POINT")
				}
				entry.Target, err = sdkLink(path, raw)
				if err != nil {
					return err
				}
				seen++
			}
		} else if i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
			if aliases[strings.ToLower(rel)] {
				return fail("SDK_ALIAS_MISSING")
			}
			key := fileIdentityKey{i.VolumeSerialNumber, fileID(i)}
			count := counts[key]
			if count.total != 0 && count.total != i.NumberOfLinks {
				return fail("HARD_LINK")
			}
			count.total = i.NumberOfLinks
			count.inside++
			counts[key] = count
		}
		result[rel] = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	if seen != len(sdkAliases) {
		return nil, fail("SDK_ALIAS_MISSING")
	}
	for _, count := range counts {
		if count.total != count.inside {
			return nil, fail("HARD_LINK")
		}
	}
	if target, ok := result[filepath.Join(sdkBin, "rustup.exe")]; !ok || target.Target != "" || target.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 || target.Links != 1 {
		return nil, fail("SDK_LINK_TARGET_MISMATCH")
	}
	// Complete the metadata-only boundary and external-hardlink checks before
	// opening any regular file contents for preservation hashes.
	for rel, entry := range result {
		if entry.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 || entry.Target != "" {
			continue
		}
		path := filepath.Join(root, rel)
		i, e := fileIdentity(path)
		if e != nil || geminiMetadata(i) != entry {
			return nil, fail("PRESERVATION_MISMATCH")
		}
		entry.Hash, err = contentHash(path)
		if err != nil {
			return nil, err
		}
		i, e = fileIdentity(path)
		meta := entry
		meta.Hash = ""
		if e != nil || geminiMetadata(i) != meta {
			return nil, fail("PRESERVATION_MISMATCH")
		}
		result[rel] = entry
	}
	return result, nil
}

func (s *sdkScope) measure(root string) (measured, error) {
	entries, err := s.snapshot(root)
	m := measured{Entries: len(entries) - 1}
	if err != nil {
		return m, err
	}
	for _, entry := range entries {
		if entry.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 && entry.Target == "" {
			m.Bytes += int64(uint64(entry.SizeHigh)<<32 | uint64(entry.SizeLow))
		}
	}
	return m, nil
}

func (s *sdkScope) verify(root string, before map[string]geminiEntry, r receipt) error {
	i, err := fileIdentity(root)
	if err != nil || i.VolumeSerialNumber != r.Volume || fileID(i) != r.FileID {
		return fail("TARGET_IDENTITY_CHANGED")
	}
	after, err := s.snapshot(root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return fail("PRESERVATION_MISMATCH")
	}
	return nil
}
