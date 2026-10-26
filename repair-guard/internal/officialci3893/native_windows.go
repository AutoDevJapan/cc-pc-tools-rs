package officialci3893

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// Live wiring independently qualifies the exact freshly issued owned parent
// identity before NewNativeFS. Tests use an isolated synthetic owned anchor.
// It does not classify the parent, adopt an existing root, or issue a data grant.
type NativeFS struct {
	parent   string
	identity Identity
}

func NewNativeFS(parent string, expected Identity) (*NativeFS, error) {
	p, err := filepath.Abs(parent)
	if err != nil || filepath.Clean(parent) != p || len(p) < 3 || p[1:3] != `:\` || strings.HasPrefix(p, `\\`) {
		return nil, ErrDenied
	}
	locks := &pins{}
	defer locks.close()
	if err = locks.chain(p); err != nil {
		return nil, err
	}
	id, err := identityOf(locks.last)
	if err != nil || id != expected {
		return nil, ErrDenied
	}
	return &NativeFS{p, id}, nil
}

func (fs *NativeFS) ParentIdentity() Identity { return fs.identity }

// Used only immediately after the normal creation issuer returns a new owned
// anchor. Existing or nonempty anchors are never adopted by live wiring.
func (fs *NativeFS) EmptyParent() error {
	p, err := fs.parentPins()
	if err != nil {
		return err
	}
	defer p.close()
	f, err := os.Open(fs.parent)
	if err != nil {
		return err
	}
	defer f.Close()
	entries, err := f.ReadDir(1)
	if len(entries) != 0 || err != io.EOF {
		return ErrDenied
	}
	return nil
}

type pins struct {
	files []*os.File
	last  *os.File
}

func (p *pins) close() {
	for i := len(p.files) - 1; i >= 0; i-- {
		_ = p.files[i].Close()
	}
}

func identityOf(f *os.File) (Identity, error) {
	var i syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &i); err != nil {
		return Identity{}, err
	}
	return Identity{i.VolumeSerialNumber, uint64(i.FileIndexHigh)<<32 | uint64(i.FileIndexLow),
		uint64(i.CreationTime.HighDateTime)<<32 | uint64(i.CreationTime.LowDateTime)}, nil
}

func (p *pins) open(path string, directory, create bool) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(syscall.GENERIC_READ)
	flags := uint32(0x00200000) // FILE_FLAG_OPEN_REPARSE_POINT: never follow the leaf
	if directory {
		access = 0x80
		flags |= 0x02000000
	} // FILE_READ_ATTRIBUTES / BACKUP_SEMANTICS
	disposition := uint32(syscall.OPEN_EXISTING)
	if create {
		access |= syscall.GENERIC_WRITE
		disposition = syscall.CREATE_NEW
	}
	h, err := syscall.CreateFile(name, access, syscall.FILE_SHARE_READ, nil, disposition, flags, 0)
	if err != nil {
		return nil, err
	}
	return p.adopt(h, path, directory)
}

func (p *pins) adopt(h syscall.Handle, path string, directory bool) (*os.File, error) {
	f := os.NewFile(uintptr(h), path)
	p.files = append(p.files, f)
	p.last = f
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return nil, err
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		(info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || info.NumberOfLinks != 1 {
		return nil, fmt.Errorf("%w: non-plain metadata at %s (attributes=%x links=%d)", ErrDenied, path, info.FileAttributes, info.NumberOfLinks)
	}
	if err := onlyDefaultStream(path); err != nil {
		return nil, fmt.Errorf("%w: stream proof at %s", err, path)
	}
	return f, nil
}

// NtCreateFile returns the handle to the object it created. Mkdir followed by
// Open would allow an intervening replacement to acquire a false creation proof.
// The name is one validated component relative to the pinned parent handle.
func (p *pins) createDirectory(parent *os.File, component, path string) (*os.File, error) {
	if !safeName(component) || strings.Contains(component, "/") {
		return nil, ErrDenied
	}
	units, err := syscall.UTF16FromString(component)
	if err != nil {
		return nil, err
	}
	name := struct {
		Length, Maximum uint16
		Buffer          *uint16
	}{uint16((len(units) - 1) * 2), uint16(len(units) * 2), &units[0]}
	attributes := struct {
		Length            uint32
		Root              syscall.Handle
		Name              unsafe.Pointer
		Attributes        uint32
		Security, Quality unsafe.Pointer
	}{Root: syscall.Handle(parent.Fd()), Name: unsafe.Pointer(&name), Attributes: 0x1040} // CASE_INSENSITIVE | DONT_REPARSE
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var statusBlock struct{ Status, Information uintptr }
	var handle syscall.Handle
	status, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("NtCreateFile").Call(
		uintptr(unsafe.Pointer(&handle)), 0x1000a0, uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&statusBlock)), 0, syscall.FILE_ATTRIBUTE_DIRECTORY,
		syscall.FILE_SHARE_READ, 2, 0x00200021, 0, 0) // FILE_CREATE; DIRECTORY | SYNCHRONOUS_NONALERT | OPEN_REPARSE_POINT
	runtime.KeepAlive(units)
	if status != 0 {
		return nil, fmt.Errorf("%w: directory CreateNew NTSTATUS %x", ErrDenied, uint32(status))
	}
	if statusBlock.Information != 2 {
		_ = syscall.CloseHandle(handle)
		return nil, ErrDenied
	} // FILE_CREATED
	return p.adopt(handle, path, true)
}

func (p *pins) chain(path string) error {
	path = filepath.Clean(path)
	vol := filepath.VolumeName(path)
	if len(vol) != 2 || vol[1] != ':' {
		return ErrDenied
	}
	current := vol + `\`
	if _, err := p.open(current, true, false); err != nil {
		return err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, current), `\`) {
		if part == "" {
			continue
		}
		if !safeName(part) {
			return ErrDenied
		}
		current = filepath.Join(current, part)
		if _, err := p.open(current, true, false); err != nil {
			return err
		}
	}
	return nil
}

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var findFirstStream = kernel32.NewProc("FindFirstStreamW")
var findNextStream = kernel32.NewProc("FindNextStreamW")

// Metadata-only adjunct for the independently created receipt/journal files.
// This is not a file-content or general path authorization API.
func DefaultStreamOnly(path string) error { return onlyDefaultStream(path) }

func onlyDefaultStream(path string) error {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var data struct {
		Size int64
		Name [296]uint16
	}
	h, _, callErr := findFirstStream.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&data)), 0)
	if h == ^uintptr(0) {
		if callErr == syscall.Errno(38) {
			return nil
		} // directories have no default data stream
		return ErrDenied // unsupported stream enumeration cannot prove absence
	}
	defer syscall.FindClose(syscall.Handle(h))
	for {
		if syscall.UTF16ToString(data.Name[:]) != "::$DATA" {
			return ErrDenied
		}
		ok, _, nextErr := findNextStream.Call(h, uintptr(unsafe.Pointer(&data)))
		if ok == 0 {
			if nextErr == syscall.Errno(38) {
				return nil
			}
			return ErrDenied
		}
	}
}

func (fs *NativeFS) parentPins() (*pins, error) {
	p := &pins{}
	if err := p.chain(fs.parent); err != nil {
		p.close()
		return nil, err
	}
	id, err := identityOf(p.last)
	if err != nil || id != fs.identity {
		p.close()
		return nil, ErrDenied
	}
	return p, nil
}

func (fs *NativeFS) rootPins(id string, expected Identity) (*pins, string, error) {
	if !hexN(id, 32) {
		return nil, "", ErrDenied
	}
	p, err := fs.parentPins()
	if err != nil {
		return nil, "", err
	}
	root := filepath.Join(fs.parent, id)
	f, err := p.open(root, true, false)
	if err != nil {
		p.close()
		return nil, "", err
	}
	actual, err := identityOf(f)
	if err != nil || actual != expected {
		p.close()
		return nil, "", ErrDenied
	}
	return p, root, nil
}

func (fs *NativeFS) CreateRoot(id string) (Identity, error) {
	if !hexN(id, 32) {
		return Identity{}, ErrDenied
	}
	p, err := fs.parentPins()
	if err != nil {
		return Identity{}, err
	}
	defer p.close()
	root := filepath.Join(fs.parent, id)
	f, err := p.createDirectory(p.last, id, root)
	if err != nil {
		return Identity{}, err
	}
	return identityOf(f)
}

func (fs *NativeFS) Install(id string, expected Identity, files []Payload) ([]FileRecord, error) {
	p, root, err := fs.rootPins(id, expected)
	if err != nil {
		return nil, err
	}
	defer p.close()
	directories := map[string]*os.File{"": p.last}
	var records []FileRecord
	for _, file := range files {
		directory := strings.HasSuffix(file.Name, "/")
		name := strings.TrimSuffix(file.Name, "/")
		if !safeName(name) || (directory && len(file.Bytes) != 0) {
			return nil, ErrDenied
		}
		parts := strings.Split(name, "/")
		end := len(parts)
		if directory {
			end++
		}
		for i := 1; i < end; i++ {
			name := strings.Join(parts[:i], "/")
			if directories[name] != nil {
				continue
			}
			path := filepath.Join(root, filepath.FromSlash(name))
			parentName := strings.Join(parts[:i-1], "/")
			f, err := p.createDirectory(directories[parentName], parts[i-1], path)
			if err != nil {
				return nil, err
			}
			identity, err := identityOf(f)
			if err != nil {
				return nil, err
			}
			directories[name] = f
			records = append(records, FileRecord{Name: name, Identity: identity, Directory: true})
		}
		if directory {
			continue
		}
		f, err := p.open(filepath.Join(root, filepath.FromSlash(file.Name)), false, true)
		if err != nil {
			return nil, err
		}
		if _, err = f.Write(file.Bytes); err != nil {
			return nil, err
		}
		if err = f.Sync(); err != nil {
			return nil, err
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(f, int64(len(file.Bytes))+1))
		if err != nil || digest(b) != digest(file.Bytes) || len(b) != len(file.Bytes) {
			return nil, ErrDenied
		}
		identity, err := identityOf(f)
		if err != nil {
			return nil, err
		}
		records = append(records, FileRecord{Name: file.Name, Size: int64(len(b)), SHA256: digest(b), Identity: identity})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return records, nil
}

func (fs *NativeFS) WithVerified(r Receipt, use func([]Payload) error) error {
	if r.Parent != fs.identity || use == nil || len(r.Files) == 0 || len(r.Files) > maxEntries {
		return ErrDenied
	}
	p, root, err := fs.rootPins(r.ID, r.Root)
	if err != nil {
		return err
	}
	defer p.close()
	expected := map[string]FileRecord{}
	var directories []string
	directories = append(directories, "")
	var expanded int64
	for _, item := range r.Files {
		if !safeName(item.Name) || item.Size < 0 || item.Size > maxExpanded-expanded {
			return ErrDenied
		}
		key := strings.ToLower(item.Name)
		if _, exists := expected[key]; exists {
			return ErrDenied
		}
		expected[key] = item
		if item.Directory {
			if item.Size != 0 || item.SHA256 != "" {
				return ErrDenied
			}
			directories = append(directories, item.Name)
		} else {
			if !hexN(item.SHA256, 64) {
				return ErrDenied
			}
			expanded += item.Size
		}
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) < len(directories[j]) })
	for _, dir := range directories[1:] {
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent != "." && !expected[strings.ToLower(parent)].Directory {
			return ErrDenied
		}
		f, err := p.open(filepath.Join(root, filepath.FromSlash(dir)), true, false)
		if err != nil {
			return err
		}
		id, err := identityOf(f)
		if err != nil || id != expected[strings.ToLower(dir)].Identity {
			return ErrDenied
		}
	}
	// Enumerate only metadata and names. Unknown entries are rejected before any
	// file contents are opened; the verified manifest is an exact tree, not a prefix.
	count := 0
	for _, dir := range directories {
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return err
		}
		entries, readErr := f.ReadDir(maxEntries + 1)
		closeErr := f.Close()
		if (readErr != nil && readErr != io.EOF) || closeErr != nil {
			return ErrDenied
		}
		for _, entry := range entries {
			count++
			if count > maxEntries {
				return ErrDenied
			}
			name := entry.Name()
			if dir != "" {
				name = dir + "/" + name
			}
			item, ok := expected[strings.ToLower(name)]
			if !ok || name != item.Name || entry.IsDir() != item.Directory {
				return ErrDenied
			}
		}
	}
	if count != len(expected) {
		return ErrDenied
	}
	type openedFile struct {
		record FileRecord
		file   *os.File
	}
	var opened []openedFile
	for _, item := range r.Files {
		if item.Directory {
			continue
		}
		parent := filepath.ToSlash(filepath.Dir(item.Name))
		if parent != "." && !expected[strings.ToLower(parent)].Directory {
			return ErrDenied
		}
		f, err := p.open(filepath.Join(root, filepath.FromSlash(item.Name)), false, false)
		if err != nil {
			return err
		}
		id, err := identityOf(f)
		if err != nil || id != item.Identity {
			return ErrDenied
		}
		opened = append(opened, openedFile{item, f})
	}
	var payloads []Payload
	// Preserve public empty directories too. Nonempty parents are implicit in
	// file names; an empty directory uses a trailing slash and a nil byte slice.
	nonempty := map[string]bool{}
	for _, item := range r.Files {
		if at := strings.LastIndexByte(item.Name, '/'); at >= 0 {
			nonempty[item.Name[:at]] = true
		}
	}
	for _, dir := range directories[1:] {
		if !nonempty[dir] {
			payloads = append(payloads, Payload{dir + "/", nil})
		}
	}
	for _, item := range opened {
		b, err := io.ReadAll(io.LimitReader(item.file, item.record.Size+1))
		if err != nil || int64(len(b)) != item.record.Size || digest(b) != item.record.SHA256 {
			return ErrDenied
		}
		if err = onlyDefaultStream(item.file.Name()); err != nil {
			return err
		}
		payloads = append(payloads, Payload{item.record.Name, b})
	}
	return use(payloads)
}
