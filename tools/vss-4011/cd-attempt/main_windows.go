//go:build windows && amd64

// A one-shot, fixed C-to-D positive resize candidate. It never deletes a shadow,
// shrinks storage, stops a service, or calls the reserved migration APIs.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const maxBytes int64 = 50 << 30
const preflightHash = "3C0E26FD7C7B93056AEBB64A7221760BC144218A5E88877DAF8B5BBD9C664E16"

var processDeadline time.Time

type component struct{ table *[37]uintptr }
type guid struct {
	A    uint32
	B, C uint16
	D    [8]byte
}
type call struct {
	Operation string `json:"operation"`
	HRESULT   string `json:"hresult"`
}
type metadata struct {
	Valid                   bool          `json:"valid"`
	ShadowCount             int           `json:"shadow_count"`
	CShadowCount            int           `json:"c_shadow_count"`
	DShadowCount            int           `json:"d_shadow_count"`
	PointCount              int           `json:"restore_point_count"`
	Associations            int           `json:"association_count"`
	AssociationRows         []association `json:"association_rows"`
	AssociationRowsComplete bool          `json:"association_rows_complete"`
	CC                      bool          `json:"c_to_c"`
	CD                      bool          `json:"c_to_d"`
	Max                     int64         `json:"maximum_bytes"`
	Used                    int64         `json:"used_bytes"`
	Allocated               int64         `json:"allocated_bytes"`
	CFree                   uint64        `json:"c_free_bytes"`
	DFree                   uint64        `json:"d_free_bytes"`
	NTFS                    bool          `json:"ntfs"`
	DistinctNVMe            bool          `json:"distinct_nvme"`
	DReady                  bool          `json:"d_online_writable_fixed"`
	Provider                bool          `json:"only_microsoft_provider"`
	Protection              bool          `json:"protection_not_disabled"`
	MinUnset                bool          `json:"minimum_unset"`
}

type association struct {
	Source    string `json:"source"`
	Storage   string `json:"storage"`
	Max       uint64 `json:"maximum_bytes"`
	Used      uint64 `json:"used_bytes"`
	Allocated uint64 `json:"allocated_bytes"`
}

func permit(m metadata) bool {
	return m.Valid && m.ShadowCount == 0 && m.CShadowCount == 0 && m.DShadowCount == 0 && m.PointCount == 0 &&
		m.Associations == 1 && m.CC && !m.CD && m.Max == maxBytes && m.Used == 0 && m.Allocated == 0 &&
		m.CFree >= 200<<30 && m.DFree >= uint64(maxBytes)+(200<<30) && m.NTFS && m.DistinctNVMe && m.DReady && m.Provider && m.Protection && m.MinUnset
}

type outcome struct {
	Started          bool     `json:"started"`
	Exit             int      `json:"exit"`
	Timeout          bool     `json:"timeout"`
	Truncated        bool     `json:"truncated"`
	WaitVerified     bool     `json:"wait_verified"`
	OutHash          string   `json:"stdout_sha256"`
	ErrHash          string   `json:"stderr_sha256"`
	ErrorCodes       []string `json:"numeric_codes"`
	ReasonCodes      []string `json:"reason_codes,omitempty"`
	DiagnosticBase64 string   `json:"sanitized_diagnostic_base64,omitempty"`
	out              []byte
	errOut           []byte
}
type attempt struct {
	Phase               string    `json:"phase"`
	CrossProcessBlocked bool      `json:"cross_process_blocked"`
	Contender           outcome   `json:"contender"`
	ContenderReport     *report   `json:"contender_report,omitempty"`
	BeforeRead          outcome   `json:"before_read"`
	Before              metadata  `json:"before"`
	PositiveResize      *outcome  `json:"positive_resize,omitempty"`
	AfterRead           *outcome  `json:"after_read,omitempty"`
	After               *metadata `json:"after,omitempty"`
	ExclusionAfter      *report   `json:"exclusion_after,omitempty"`
}
type report struct {
	StartedUTC    string   `json:"started_utc"`
	FinishedUTC   string   `json:"finished_utc"`
	Phase         string   `json:"phase"`
	BeforeCleanup string   `json:"phase_before_cleanup"`
	Calls         []call   `json:"calls"`
	Owned         int      `json:"owned_empty_sets_started"`
	Aborted       int      `json:"own_aborts_succeeded"`
	Released      int      `json:"objects_released"`
	Attempt       *attempt `json:"attempt,omitempty"`
}

type limited struct {
	buf       []byte
	truncated bool
}

func (b *limited) Write(p []byte) (int, error) {
	n := len(p)
	left := (64 << 10) - len(b.buf)
	if len(p) > left {
		p = p[:left]
		b.truncated = true
	}
	b.buf = append(b.buf, p...)
	return n, nil
}
func process(exe string, limit time.Duration, args ...string) outcome {
	parent := context.Background()
	if !processDeadline.IsZero() {
		var done context.CancelFunc
		parent, done = context.WithDeadline(parent, processDeadline)
		defer done()
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000, HideWindow: true}
	cmd.WaitDelay = time.Second
	var out, errOut limited
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	r := outcome{Started: cmd.Process != nil, Exit: -1, Timeout: ctx.Err() != nil, Truncated: out.truncated || errOut.truncated, out: out.buf, errOut: errOut.buf}
	if cmd.ProcessState != nil {
		r.Exit = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	r.WaitVerified = cmd.ProcessState != nil && (err == nil || errors.As(err, &exitErr))
	x, y := sha256.Sum256(out.buf), sha256.Sum256(errOut.buf)
	r.OutHash = hex.EncodeToString(x[:])
	r.ErrHash = hex.EncodeToString(y[:])
	r.ErrorCodes = regexp.MustCompile(`0x[0-9a-fA-F]{8}`).FindAllString(string(out.buf)+string(errOut.buf), 12)
	return r
}

// Only fixed vssadmin resize output enters this function. It cannot contain
// file contents; GUID/device references are removed before private retention.
func resizeDiagnostic(p []byte) string {
	s := strings.ToValidUTF8(string(p), "?")
	s = regexp.MustCompile(`(?i)\\\\\?\\Volume\{[^}]*\}\\?`).ReplaceAllString(s, "[volume]")
	s = regexp.MustCompile(`(?i)\{[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\}`).ReplaceAllString(s, "[id]")
	if len(s) > 4096 {
		s = s[:4096]
	}
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func resizeReasons(p []byte) []string {
	s := strings.ToLower(string(p))
	var reasons []string
	for _, v := range []struct {
		tag   string
		terms []string
	}{
		{"association_not_found", []string{"association was not found", "関連付けが見つかりません"}},
		{"association_already_exists", []string{"already exists", "既に存在"}},
		{"maximum_associations", []string{"maximum number", "最大数"}},
		{"snapshot_in_progress", []string{"in progress", "進行中"}},
		{"access_denied", []string{"access is denied", "アクセスが拒否"}},
		{"insufficient_storage", []string{"insufficient", "十分な", "空き領域"}},
	} {
		for _, term := range v.terms {
			if strings.Contains(s, term) {
				reasons = append(reasons, v.tag)
				break
			}
		}
	}
	return reasons
}

func blocked(r report) bool {
	if r.Phase != "start_unavailable_no_retry" || r.Owned != 0 || r.Aborted != 0 || r.Released != 1 {
		return false
	}
	for _, c := range r.Calls {
		if c.Operation == "StartSnapshotSet" && c.HRESULT == "0x80042316" {
			return true
		}
	}
	return false
}
func pinScript(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	b, err := io.ReadAll(io.LimitReader(f, 32<<10))
	if err == nil {
		b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		x := sha256.Sum256(b)
		if fmt.Sprintf("%X", x) != preflightHash {
			err = fmt.Errorf("preflight bytes differ")
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil // deny writes/deletes until both invocations have ended
}
func readMetadata(script string) (metadata, outcome) {
	ps := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	p := process(ps, 20*time.Second, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
	var m metadata
	if p.WaitVerified && !p.Timeout && !p.Truncated && p.Exit == 0 {
		if json.Unmarshal(p.out, &m) != nil {
			m.Valid = false
		}
	}
	return m, p
}
func attemptCD(script string, apply bool) (a attempt) {
	a.Phase = "preflight_source_unverified_no_write"
	f, err := pinScript(script)
	if err != nil {
		return
	}
	defer f.Close()
	self, err := os.Executable()
	if err != nil {
		return
	}
	a.Contender = process(self, 10*time.Second, "--contender")
	var child report
	decoded := json.Unmarshal(a.Contender.out, &child) == nil
	if decoded {
		a.ContenderReport = &child
	}
	if !a.Contender.WaitVerified || a.Contender.Timeout || a.Contender.Truncated || a.Contender.Exit != 1 || !decoded {
		a.Phase = "cross_process_exclusion_unverified_no_write"
		return
	}
	for _, c := range child.Calls {
		if c.Operation == "StartSnapshotSet" && c.HRESULT == "0x80042316" {
			a.CrossProcessBlocked = true
		}
	}
	if !a.CrossProcessBlocked || child.Owned != 0 || child.Aborted != 0 || child.Released != 1 || child.Phase != "start_unavailable_no_retry" {
		a.Phase = "cross_process_exclusion_unverified_no_write"
		return
	}
	a.Before, a.BeforeRead = readMetadata(script)
	if !apply {
		late := report{StartedUTC: time.Now().UTC().Format(time.RFC3339Nano)}
		objectOperation(&late, "", false)
		late.FinishedUTC = time.Now().UTC().Format(time.RFC3339Nano)
		a.ExclusionAfter = &late
		if !blocked(late) {
			a.Phase = "post_read_exclusion_unverified_no_write"
			return
		}
		if !permit(a.Before) {
			a.Phase = "live_preflight_not_met_no_write"
			return
		}
		a.Phase = "locked_preflight_verified_no_write"
		return
	}
	if !permit(a.Before) {
		a.Phase = "live_preflight_not_met_no_write"
		return
	}
	// One fixed, positive 50GiB call while our normal VSS set is still held.
	// There is no delete/shrink, service stop, fallback, or retry in this program.
	cli := filepath.Join(os.Getenv("SystemRoot"), "System32", "vssadmin.exe")
	p := process(cli, 20*time.Second, "resize", "shadowstorage", "/for=C:", "/on=D:", "/maxsize=53687091200")
	p.ReasonCodes = resizeReasons(p.out)
	p.DiagnosticBase64 = resizeDiagnostic(append(append([]byte{}, p.out...), p.errOut...))
	a.PositiveResize = &p
	m, post := readMetadata(script)
	a.After = &m
	a.AfterRead = &post
	late := report{StartedUTC: time.Now().UTC().Format(time.RFC3339Nano)}
	objectOperation(&late, "", false)
	late.FinishedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	a.ExclusionAfter = &late
	a.Phase = "positive_call_outcome_unverified_no_retry"
	if !p.WaitVerified || p.Timeout || p.Truncated || !m.Valid || !blocked(late) {
		return
	}
	if p.Exit != 0 {
		a.Phase = "positive_call_failed_no_retry"
		return
	}
	if m.Associations == 1 && m.CD && !m.CC && m.Max == maxBytes && m.ShadowCount == 0 && m.PointCount == 0 {
		a.Phase = "c_d_association_verified_retention_unverified"
	}
	return
}

func run(script string, apply bool) (r report) {
	// Start before native COM calls. All owned child deadlines end before the
	// outer 90s caller deadline, even if COM initialization was slow.
	if processDeadline.IsZero() {
		processDeadline = time.Now().Add(70 * time.Second)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.StartedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	defer func() { r.FinishedUTC = time.Now().UTC().Format(time.RFC3339Nano) }()
	r.Phase = "entrypoints_unavailable"
	root := filepath.Join(os.Getenv("SystemRoot"), "System32")
	ole, vss := syscall.NewLazyDLL(filepath.Join(root, "ole32.dll")), syscall.NewLazyDLL(filepath.Join(root, "vssapi.dll"))
	initCOM, security, uninit, create := ole.NewProc("CoInitializeEx"), ole.NewProc("CoInitializeSecurity"), ole.NewProc("CoUninitialize"), vss.NewProc("CreateVssBackupComponentsInternal")
	for _, p := range []*syscall.LazyProc{initCOM, security, uninit, create} {
		if p.Find() != nil {
			return
		}
	}
	record := func(op string, h uintptr) { r.Calls = append(r.Calls, call{op, fmt.Sprintf("0x%08X", uint32(h))}) }
	h, _, _ := initCOM.Call(0, 0)
	record("CoInitializeEx", h)
	if int32(uint32(h)) < 0 {
		r.Phase = "com_initialization_failed"
		return
	}
	defer uninit.Call()
	h, _, _ = security.Call(0, ^uintptr(0), 0, 0, 6, 3, 0, 0, 0)
	record("CoInitializeSecurity", h)
	if int32(uint32(h)) < 0 {
		r.Phase = "com_security_failed"
		return
	}
	objectOperation(&r, script, apply)
	return
}

// An additional object on the already initialized COM thread, also used for
// the exclusion check immediately after the post-write observation.
func objectOperation(r *report, script string, apply bool) {
	create := syscall.NewLazyDLL(filepath.Join(os.Getenv("SystemRoot"), "System32", "vssapi.dll")).NewProc("CreateVssBackupComponentsInternal")
	record := func(op string, h uintptr) { r.Calls = append(r.Calls, call{op, fmt.Sprintf("0x%08X", uint32(h))}) }
	var c *component
	h, _, _ := create.Call(uintptr(unsafe.Pointer(&c)))
	record("CreateVssBackupComponents", h)
	if uint32(h) != 0 || c == nil {
		r.Phase = "component_creation_failed"
		return
	}
	defer func() { syscall.SyscallN(c.table[2], uintptr(unsafe.Pointer(c))); r.Released++ }()
	h, _, _ = syscall.SyscallN(c.table[5], uintptr(unsafe.Pointer(c)), 0)
	record("InitializeForBackup", h)
	if uint32(h) != 0 {
		r.Phase = "component_setup_failed"
		return
	}
	h, _, _ = syscall.SyscallN(c.table[35], uintptr(unsafe.Pointer(c)), 0x1d)
	record("SetContext", h)
	if uint32(h) != 0 {
		r.Phase = "component_setup_failed"
		return
	}
	h, _, _ = syscall.SyscallN(c.table[6], uintptr(unsafe.Pointer(c)), 0, 0, 5, 0)
	record("SetBackupState", h)
	if uint32(h) != 0 {
		r.Phase = "component_setup_failed"
		return
	}
	var id guid
	h, _, _ = syscall.SyscallN(c.table[36], uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(&id)))
	runtime.KeepAlive(&id)
	record("StartSnapshotSet", h)
	if uint32(h) != 0 {
		r.Phase = "start_unavailable_no_retry"
		return
	}
	r.Owned = 1
	// This defer precedes Release and runs on the same locked COM thread.
	defer func() {
		r.BeforeCleanup = r.Phase
		h, _, _ := syscall.SyscallN(c.table[15], uintptr(unsafe.Pointer(c)))
		record("AbortBackup", h)
		if uint32(h) == 0 || uint32(h) == 0x00040202 {
			r.Aborted = 1
		} else {
			r.Phase = "own_empty_set_abort_unverified"
		}
	}()
	r.Phase = "empty_set_available"
	if script != "" {
		a := attemptCD(script, apply)
		r.Attempt = &a
		r.Phase = a.Phase
	}
	return
}

func consumeClaim(ledger string) error {
	if !filepath.IsAbs(ledger) || filepath.Base(filepath.Clean(ledger)) != "os-v2" || filepath.Base(filepath.Dir(filepath.Clean(ledger))) != "4011" {
		return fmt.Errorf("fixed ledger required")
	}
	if _, e := os.Stat(filepath.Join(ledger, "positive-cd-result.private.json")); !os.IsNotExist(e) {
		return fmt.Errorf("outcome exists or unknown")
	}
	b, e := os.ReadFile(filepath.Join(ledger, "positive-cd-request.private.json"))
	if e != nil || len(b) > 64<<10 {
		return fmt.Errorf("request unavailable")
	}
	var req struct {
		Todo    int    `json:"todo"`
		Phase   string `json:"phase"`
		ExeSHA  string `json:"exe_sha256"`
		Maximum int64  `json:"maximum_bytes"`
	}
	if json.Unmarshal(b, &req) != nil || req.Todo != 4011 || req.Phase != "positive_cd_call_pending_outcome_unknown" || req.Maximum != maxBytes {
		return fmt.Errorf("request not eligible")
	}
	self, e := os.Executable()
	if e != nil {
		return e
	}
	f, e := os.Open(self)
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return e
	}
	if strings.ToUpper(hex.EncodeToString(h.Sum(nil))) != req.ExeSHA {
		return fmt.Errorf("request binary differs")
	}
	fixed, e := os.OpenFile(filepath.Join(ledger, "positive-cd-native-start.private.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = fmt.Fprintf(fixed, "{\"todo\":4011,\"phase\":\"native_attempt_consumed_no_retry\",\"utc\":%q}\n", time.Now().UTC().Format(time.RFC3339Nano))
	if e == nil {
		e = fixed.Sync()
	}
	ce := fixed.Close()
	if e == nil {
		e = ce
	}
	return e
}
func main() {
	var script string
	apply := false
	if len(os.Args) == 2 && os.Args[1] == "--contender" {
		// It starts no child process and performs no storage operation.
	} else if (len(os.Args) == 4 && os.Args[1] == "--attempt-cd") || (len(os.Args) == 3 && os.Args[1] == "--inspect-cd") {
		script = os.Args[2]
		if script == "" {
			fmt.Fprintln(os.Stderr, "fixed preflight script required")
			os.Exit(2)
		}
		apply = os.Args[1] == "--attempt-cd"
		if apply {
			expected := filepath.Clean(filepath.Join(filepath.Dir(script), "../../../.ci-work/4011/os-v2"))
			if !strings.EqualFold(expected, filepath.Clean(os.Args[3])) {
				fmt.Fprintln(os.Stderr, "fixed worktree ledger required")
				os.Exit(2)
			}
			if e := consumeClaim(os.Args[3]); e != nil {
				fmt.Fprintln(os.Stderr, "fixed request unavailable or consumed; no retry")
				os.Exit(2)
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "usage: cd-attempt --contender | --inspect-cd <fixed-preflight.ps1> | --attempt-cd <fixed-preflight.ps1> <ledger>")
		os.Exit(2)
	}
	r := run(script, apply)
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		os.Exit(1)
	}
	if r.Owned != 1 || r.Aborted != 1 || r.Released != 1 || (r.Phase != "empty_set_available" && r.Phase != "locked_preflight_verified_no_write" && r.Phase != "c_d_association_verified_retention_unverified") {
		os.Exit(1)
	}
}
