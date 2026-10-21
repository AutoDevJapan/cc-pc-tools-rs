//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Git's installation must be outside both SDK's private ancestor and the
// worker-writable runner tree. SYSTEM's source auditor keeps its SDK Git PATH.
func workerGitPath(service string) string {
	paths := []string{workerGitRoot + `\cmd`, workerGitRoot + `\bin`}
	for _, path := range strings.Split(service, ";") {
		if !strings.EqualFold(path, sdkRoot+`\git\cmd`) && !strings.EqualFold(path, sdkRoot+`\git\bin`) {
			paths = append(paths, path)
		}
	}
	return strings.Join(paths, ";")
}

func workerGitACLArgs() []string {
	return []string{workerGitRoot, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*" + runnerUserSID + ":(OI)(CI)RX"}
}

func prepareWorkerGit(ctx context.Context, env []string) error {
	exe, e := os.Executable()
	if e != nil || !fixedGuestService(exe, IsLocalSystem()) {
		return errors.New("Git staging requires the fixed guest SYSTEM agent")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return copyWorkerGitTree(ctx, sdkRoot+`\git`, workerGitRoot, func() error {
		const exe = `C:\Windows\System32\icacls.exe`
		words := []string{syscall.EscapeArg(exe)}
		for _, arg := range workerGitACLArgs() {
			words = append(words, syscall.EscapeArg(arg))
		}
		r := runTree(ctx, exe, strings.Join(words, " "), sdkRoot, env, "")
		if r.err != nil || r.exit != 0 || !r.drained {
			return errors.New("read-only worker Git ACL setup failed")
		}
		return nil
	})
}

// Directory handles deny deletion/rename; OPEN_REPARSE_POINT inspects the actual
// entry. Absolute drive paths use extended syntax even below MAX_PATH. The live
// caller has compiled paths, a hash-received SDK and no runner process yet.
func pinGitCopyEntry(path string, directory bool) (*os.File, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || len(filepath.VolumeName(path)) != 2 || strings.Contains(path, "/") {
		return nil, errors.New("Git copy requires an absolute drive path")
	}
	p, e := syscall.UTF16PtrFromString(`\\?\` + path)
	if e != nil {
		return nil, e
	}
	flags, share := uint32(0x00200000), uint32(syscall.FILE_SHARE_READ)
	if directory {
		flags |= 0x02000000
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ, share, nil, syscall.OPEN_EXISTING, flags, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(h), path)
	var info syscall.ByHandleFileInformation
	if e = syscall.GetFileInformationByHandle(h, &info); e != nil || info.FileAttributes&0x400 != 0 || (info.FileAttributes&0x10 != 0) != directory {
		f.Close()
		return nil, errors.New("Git copy physical entry rejected")
	}
	return f, nil
}

func pinGitCopyChain(path string) ([]*os.File, error) {
	var paths []string
	for path = filepath.Clean(path); ; path = filepath.Dir(path) {
		paths = append(paths, path)
		if filepath.Dir(path) == path {
			break
		}
	}
	var files []*os.File
	// Pin volume to leaf, so a previously checked ancestor cannot be swapped
	// while the next child is opened. READ-only sharing also blocks retargeting.
	for i := len(paths) - 1; i >= 0; i-- {
		f, e := pinGitCopyEntry(paths[i], true)
		if e != nil {
			closeGitCopyPins(files)
			return nil, e
		}
		files = append(files, f)
	}
	return files, nil
}

func closeGitCopyPins(files []*os.File) {
	for _, f := range files {
		f.Close()
	}
}

// Private seam for synthetic public fixtures, never a request-selected copy.
// Sources may have Git's intentional hardlinks: read-only, no-write/no-delete
// handles pin their bytes. Every destination is a newly created regular file.
func copyWorkerGitTree(ctx context.Context, source, destination string, seal func() error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	for _, pair := range [][2]string{{source, destination}, {destination, source}} {
		rel, e := filepath.Rel(pair[0], pair[1])
		if e != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return errors.New("worker Git copy roots overlap")
		}
	}
	sourcePins, e := pinGitCopyChain(source)
	if e != nil {
		return e
	}
	defer closeGitCopyPins(sourcePins)
	destinationPins, e := pinGitCopyChain(filepath.Dir(destination))
	if e != nil {
		return e
	}
	defer closeGitCopyPins(destinationPins)
	if e = os.Mkdir(destination, 0700); e != nil {
		return errors.New("worker Git destination not fresh")
	}
	root, e := pinGitCopyEntry(destination, true)
	if e != nil {
		return e
	}
	defer root.Close()
	if e = seal(); e != nil {
		return e
	}
	remaining, entries := int64(2<<30), 40000
	var walk func(string, string, int) error
	walk = func(from, to string, depth int) error {
		if ctx.Err() != nil || depth > 64 {
			return errors.New("worker Git copy interrupted or too deep")
		}
		children, e := os.ReadDir(from)
		if e != nil {
			return e
		}
		for _, child := range children {
			entries--
			if entries < 0 || !filepath.IsLocal(child.Name()) || strings.Contains(child.Name(), ":") {
				return errors.New("worker Git copy entry limit or name rejected")
			}
			src, dst := filepath.Join(from, child.Name()), filepath.Join(to, child.Name())
			if child.Type()&os.ModeSymlink != 0 {
				return errors.New("worker Git link rejected")
			}
			f, e := pinGitCopyEntry(src, child.IsDir())
			if e != nil {
				return e
			}
			if child.IsDir() {
				e = os.Mkdir(dst, 0700)
				var d *os.File
				if e == nil {
					d, e = pinGitCopyEntry(dst, true)
				}
				if e == nil {
					e = walk(src, dst, depth+1)
				}
				if d != nil {
					d.Close()
				}
			} else {
				info, err := f.Stat()
				if err != nil || !info.Mode().IsRegular() || info.Size() > remaining {
					e = errors.New("worker Git copy file or byte limit rejected")
				} else {
					var out *os.File
					out, e = os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
					if e == nil {
						var n int64
						n, e = io.Copy(out, io.LimitReader(&gitCopyReader{ctx, f}, remaining+1))
						e = errors.Join(e, out.Sync(), out.Close())
						if n != info.Size() {
							e = errors.Join(e, errors.New("worker Git copy incomplete"))
						}
						remaining -= n
					}
				}
			}
			f.Close()
			if e != nil {
				return e
			}
		}
		return nil
	}
	return walk(source, destination, 0)
}

type gitCopyReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *gitCopyReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}
