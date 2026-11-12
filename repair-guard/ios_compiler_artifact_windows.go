package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const iosCompilerRepo = "AutoDevJapan/phone-friction-control-ios-sdk-audit"
const iosCompilerOriginalRoot = "C:/Users/makhmeto/Documents/Codex/2026-11-07/ika-x20/work/4431/hosting-root-lowering-43067567260"
const iosCompilerArchivePath = iosCompilerOriginalRoot + "/artifact-13876552306.zip"
const iosCompilerOutputPath = iosCompilerOriginalRoot + "/artifact"

type iosCompilerAPIPin struct {
	endpoint, query string
	expected        object
}

func iosCompilerPins() []iosCompilerAPIPin {
	base := "repos/" + iosCompilerRepo
	return []iosCompilerAPIPin{
		{base, "{id,full_name,private,owner_id:.owner.id}", object{"id": int64(1413129506), "full_name": iosCompilerRepo, "private": false, "owner_id": int64(246834983)}},
		{base + "/actions/runs/43067567260", "{id,run_attempt,head_sha,head_branch,event,path,workflow_id,status,conclusion,repository_id:.repository.id,head_repository_id:.head_repository.id}", object{"id": int64(43067567260), "run_attempt": 1, "head_sha": iosCompilerSource, "head_branch": "codex/ios-unpresented-hosting-lifetime", "event": "push", "path": ".github/workflows/hosting-root-lowering.yml", "workflow_id": 414956057, "status": "completed", "conclusion": "failure", "repository_id": int64(1413129506), "head_repository_id": int64(1413129506)}},
		{base + "/actions/jobs/130686487618", "{id,run_id,head_sha,status,conclusion,name,upload_success:any(.steps[]?;.conclusion==\"success\" and (.name|test(\"upload.*artifact\";\"i\")))}", object{"id": int64(130686487618), "run_id": int64(43067567260), "head_sha": iosCompilerSource, "status": "completed", "conclusion": "failure", "name": "compiler", "upload_success": true}},
		{base + "/actions/artifacts/13876552306", "{id,name,size_in_bytes,digest,workflow_run:.workflow_run|{id,repository_id,head_repository_id,head_sha}}", object{"id": int64(13876552306), "name": "hosting-root-ownership-lowering", "size_in_bytes": 12392, "digest": "sha256:" + iosCompilerDigest, "workflow_run": object{"id": int64(43067567260), "repository_id": int64(1413129506), "head_repository_id": int64(1413129506), "head_sha": iosCompilerSource}}},
	}
}

// API observations, not a caller JSON file, establish the exact public artifact
// origin. Expiry of the download does not invalidate already retained bytes;
// this operation never redownloads or asserts historical extraction identity.
func iosCompilerProvider(ctx context.Context, command func(context.Context, string, ...string) ([]byte, error)) error {
	for _, pin := range iosCompilerPins() {
		if ctx.Err() != nil {
			return errIOSCompiler
		}
		raw, err := command(ctx, official3894Gate, "api", "--hostname", "github.com", pin.endpoint, "--jq", pin.query)
		if err != nil || len(raw) > 64<<10 || !official3894Match(raw, pin.expected) {
			return errIOSCompiler
		}
	}
	return nil
}

type iosCompilerWindowsAuthority struct{}

func (iosCompilerWindowsAuthority) Provider(ctx context.Context) error {
	return iosCompilerProvider(ctx, iosCompilerCommand)
}
func (iosCompilerWindowsAuthority) PinOriginal(ctx context.Context) (iosCompilerSnapshot, error) {
	if ctx.Err() != nil {
		return nil, errIOSCompiler
	}
	s, err := iosCompilerPinAt(iosCompilerArchivePath, iosCompilerOutputPath)
	if err != nil {
		return s, err
	}
	if len(s.archive.raw) != 12392 {
		return s, errIOSCompiler
	}
	return s, nil
}
func (iosCompilerWindowsAuthority) Seal(raw []byte) ([]byte, error) {
	return iosCompilerDPAPI(raw, true)
}
func (iosCompilerWindowsAuthority) Unseal(raw []byte) ([]byte, error) {
	return iosCompilerDPAPI(raw, false)
}

// Use the platform issuer with a dedicated entropy domain, never an old image
// issuer's signature, caller-supplied key or application-created proof.
func iosCompilerDPAPI(raw []byte, seal bool) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil, errIOSCompiler
	}
	in := ownerIdentityBlob{Size: uint32(len(raw)), Data: &raw[0]}
	entropy := []byte(iosCompilerDomain)
	salt := ownerIdentityBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out ownerIdentityBlob
	operation := "CryptUnprotectData"
	if seal {
		operation = "CryptProtectData"
	}
	ok, _, _ := syscall.NewLazyDLL("crypt32.dll").NewProc(operation).Call(uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&salt)), 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if out.Data != nil {
		defer func() {
			if out.Size <= 64<<10 {
				clear(unsafe.Slice(out.Data, int(out.Size)))
			}
			syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree").Call(uintptr(unsafe.Pointer(out.Data)))
		}()
	}
	if ok == 0 || out.Data == nil || out.Size == 0 || out.Size > 64<<10 {
		return nil, errIOSCompiler
	}
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}

type iosCompilerPinnedFile struct {
	file *os.File
	info syscall.ByHandleFileInformation
	raw  []byte
}
type iosCompilerWindowsSnapshot struct {
	fs      skillsFS
	archive iosCompilerPinnedFile
	root    string
	files   map[string]iosCompilerPinnedFile
}

func iosCompilerPhysicalID(i syscall.ByHandleFileInformation) string {
	return strconv.FormatUint(uint64(i.VolumeSerialNumber), 10) + ":" + strconv.FormatUint(uint64(i.FileIndexHigh)<<32|uint64(i.FileIndexLow), 10) + ":" + strconv.FormatUint(uint64(i.CreationTime.HighDateTime)<<32|uint64(i.CreationTime.LowDateTime), 10)
}

func iosCompilerSameFileInfo(a, b syscall.ByHandleFileInformation) bool {
	// Reading may legitimately update access time. Creation, write time,
	// identity, size, link count and attributes are still pinned exactly.
	a.LastAccessTime = syscall.Filetime{}
	b.LastAccessTime = syscall.Filetime{}
	return a == b
}

func iosCompilerPinAt(archive, root string) (*iosCompilerWindowsSnapshot, error) {
	s := &iosCompilerWindowsSnapshot{root: root, files: map[string]iosCompilerPinnedFile{}, fs: skillsFS{blocked: func(p string) bool { return legacyMatch(p) || ownerCardPath(p) }}}
	// This private constructor is called only after fixed API qualification.
	// It deliberately does not turn the mixed parent into a generic clear path.
	if !filepath.IsAbs(archive) || !filepath.IsAbs(root) || !same(filepath.Dir(archive), filepath.Dir(root)) {
		return nil, errIOSCompiler
	}
	if _, ok := s.fs.directory(root); !ok {
		return s, errIOSCompiler
	}
	p, err := s.open(archive, 16<<20)
	if err != nil {
		return s, err
	}
	s.archive = p
	return s, nil
}

func (s *iosCompilerWindowsSnapshot) open(path string, limit int64) (iosCompilerPinnedFile, error) {
	if !filepath.IsAbs(path) || s.fs.blocked(path) || ownerWindowsAlias(path) {
		return iosCompilerPinnedFile{}, errIOSCompiler
	}
	parent, ok := s.fs.directory(filepath.Dir(path))
	if !ok {
		return iosCompilerPinnedFile{}, errIOSCompiler
	}
	h, ok := skillsOpenAt(parent, filepath.Base(path), false, false)
	if !ok {
		return iosCompilerPinnedFile{}, errIOSCompiler
	}
	f := os.NewFile(uintptr(h), path)
	s.fs.files = append(s.fs.files, f)
	actual, pathErr := ownerHandlePath(h)
	stat, statErr := f.Stat()
	if pathErr != nil || statErr != nil || !same(actual, path) || !stat.Mode().IsRegular() || stat.Size() < 0 || stat.Size() > limit {
		return iosCompilerPinnedFile{}, errIOSCompiler
	}
	var before, after syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &before) != nil {
		return iosCompilerPinnedFile{}, errIOSCompiler
	}
	raw, err := iosCompilerReadFixed(f, stat.Size(), limit)
	if err != nil || int64(len(raw)) > limit || syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &after) != nil || !iosCompilerSameFileInfo(before, after) {
		clear(raw)
		return iosCompilerPinnedFile{}, errIOSCompiler
	}
	return iosCompilerPinnedFile{f, before, raw}, nil
}
func (s *iosCompilerWindowsSnapshot) Archive() ([]byte, string) {
	return s.archive.raw, iosCompilerPhysicalID(s.archive.info)
}
func (s *iosCompilerWindowsSnapshot) File(name string) ([]byte, string, error) {
	allowed := false
	for _, n := range iosCompilerNames {
		allowed = allowed || n == name
	}
	if !allowed {
		return nil, "", errIOSCompiler
	}
	if p, ok := s.files[name]; ok {
		return p.raw, iosCompilerPhysicalID(p.info), nil
	}
	p, err := s.open(filepath.Join(s.root, name), 8<<20)
	if err != nil {
		return nil, "", err
	}
	s.files[name] = p
	return p.raw, iosCompilerPhysicalID(p.info), nil
}
func (s *iosCompilerWindowsSnapshot) Recheck() error {
	all := []iosCompilerPinnedFile{s.archive}
	for _, f := range s.files {
		all = append(all, f)
	}
	for _, f := range all {
		var now syscall.ByHandleFileInformation
		if f.file == nil || syscall.GetFileInformationByHandle(syscall.Handle(f.file.Fd()), &now) != nil || !iosCompilerSameFileInfo(now, f.info) {
			return errIOSCompiler
		}
	}
	return nil
}
func (s *iosCompilerWindowsSnapshot) Close() error {
	clear(s.archive.raw)
	for _, f := range s.files {
		clear(f.raw)
	}
	var failed bool
	for _, f := range s.fs.files {
		if f.Close() != nil {
			failed = true
		}
	}
	for _, h := range s.fs.dirs {
		if syscall.CloseHandle(h) != nil {
			failed = true
		}
	}
	s.fs.files = nil
	s.fs.dirs = nil
	if failed {
		return errIOSCompiler
	}
	return nil
}

func iosCompilerFlag(args []string) bool {
	for _, a := range args {
		if a == "--verify-ios-compiler-artifact" || strings.HasPrefix(a, "--verify-ios-compiler-artifact=") || a == "--consume-ios-compiler-artifact" || strings.HasPrefix(a, "--consume-ios-compiler-artifact=") {
			return true
		}
	}
	return false
}

func runIOSCompilerCLI(args []string, out io.Writer, authority iosCompilerAuthority) int {
	return runIOSCompilerCLIWithDigest(args, out, authority, iosCompilerDigest)
}
func runIOSCompilerCLIWithDigest(args []string, out io.Writer, authority iosCompilerAuthority, digest string) int {
	if len(args) != 1 || (args[0] != "--verify-ios-compiler-artifact" && args[0] != "--consume-ios-compiler-artifact") {
		_ = json.NewEncoder(out).Encode(iosCompilerResult{Domain: iosCompilerDomain, Stage: "invalid_arguments", CleanupComplete: true})
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	started := false
	writeFailed := false
	var consume func(iosCompilerManifest, map[string][]byte) error
	if args[0] == "--consume-ios-compiler-artifact" {
		consume = func(m iosCompilerManifest, files map[string][]byte) error {
			started = true
			err := iosCompilerWriteFiles(out, m, files)
			writeFailed = err != nil
			return err
		}
	}
	r := iosCompilerRead(ctx, authority, digest, consume)
	if writeFailed {
		return 1
	}
	if started {
		if iosCompilerWrite(out, []byte(`,"metadata":`)) != nil {
			return 1
		}
		// This encoder sees only fixed metadata/digests, never file contents.
		if json.NewEncoder(out).Encode(r) != nil || iosCompilerWrite(out, []byte("}\n")) != nil {
			return 1
		}
	} else if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	if !r.Qualified || !r.CleanupComplete || (consume != nil && !r.Consumed) {
		return 1
	}
	return 0
}

func iosCompilerWrite(out io.Writer, p []byte) error {
	n, err := out.Write(p)
	if err != nil || n != len(p) {
		return errIOSCompiler
	}
	return nil
}

// The caller captures stdout programmatically and requires exit zero. We
// stream the original verified buffers while pinned; no encoding/json pool
// receives file contents. The only owned encoded scratch is erased on all exits.
func iosCompilerWriteFiles(out io.Writer, m iosCompilerManifest, files map[string][]byte) error {
	if len(files) != len(iosCompilerNames) {
		return errIOSCompiler
	}
	if iosCompilerWrite(out, []byte(`{"manifest":`)) != nil {
		return errIOSCompiler
	}
	if err := json.NewEncoder(out).Encode(m); err != nil {
		return errIOSCompiler
	}
	if iosCompilerWrite(out, []byte(`,"files":{`)) != nil {
		return errIOSCompiler
	}
	scratch := make([]byte, 4096)
	defer clear(scratch)
	for i, name := range iosCompilerNames {
		data, ok := files[name]
		if !ok {
			return errIOSCompiler
		}
		if i != 0 && iosCompilerWrite(out, []byte{','}) != nil {
			return errIOSCompiler
		}
		if iosCompilerWrite(out, []byte(`"`+name+`":"`)) != nil {
			return errIOSCompiler
		}
		if err := iosCompilerWriteBase64(out, data, scratch); err != nil {
			return err
		}
		if iosCompilerWrite(out, []byte{'"'}) != nil {
			return errIOSCompiler
		}
	}
	return iosCompilerWrite(out, []byte{'}'})
}
func iosCompilerWriteBase64(out io.Writer, data, scratch []byte) error {
	defer clear(scratch)
	if len(scratch) < 4096 {
		return errIOSCompiler
	}
	for len(data) > 0 {
		n := min(len(data), 3072)
		encoded := base64.StdEncoding.EncodedLen(n)
		base64.StdEncoding.Encode(scratch[:encoded], data[:n])
		if iosCompilerWrite(out, scratch[:encoded]) != nil {
			return errIOSCompiler
		}
		clear(scratch[:encoded])
		data = data[n:]
	}
	return nil
}
