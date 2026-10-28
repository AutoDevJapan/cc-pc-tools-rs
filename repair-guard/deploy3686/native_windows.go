package deploy3686

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

type unicodeName struct {
	Length, MaximumLength uint16
	Buffer                *uint16
}
type attributes struct {
	Length            uint32
	Root              syscall.Handle
	Name              *unicodeName
	Flags             uint32
	Security, Quality unsafe.Pointer
}
type ioStatus struct{ Status, Information uintptr }

var ntCreate = syscall.NewLazyDLL("ntdll.dll").NewProc("NtCreateFile")
var ntSet = syscall.NewLazyDLL("ntdll.dll").NewProc("NtSetInformationFile")
var getSecurity = syscall.NewLazyDLL("advapi32.dll").NewProc("GetSecurityInfo")
var securityString = syscall.NewLazyDLL("advapi32.dll").NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW")
var localFree = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")

type physicalParent struct {
	handles []syscall.Handle
	last    syscall.Handle
}

func (p *physicalParent) close() {
	for i := len(p.handles) - 1; i >= 0; i-- {
		syscall.CloseHandle(p.handles[i])
	}
}
func component(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\:")
}
func openAt(parent syscall.Handle, name string, directory, rename, create bool) (*os.File, error) {
	if !component(name) {
		return nil, errors.New("invalid_component")
	}
	text, e := syscall.UTF16FromString(name)
	if e != nil || len(text) > 32767 {
		return nil, errors.New("invalid_component")
	}
	u := unicodeName{uint16((len(text) - 1) * 2), uint16(len(text) * 2), &text[0]}
	a := attributes{Root: parent, Name: &u, Flags: 0x40}
	a.Length = uint32(unsafe.Sizeof(a))
	access := uint32(syscall.GENERIC_READ | 0x100000)
	share := uint32(syscall.FILE_SHARE_READ)
	options := uint32(0x00200000 | 0x20 | 0x40)
	if directory {
		access = 0x100000 | 0x80 | 1
		share |= syscall.FILE_SHARE_WRITE
		options = 0x00200000 | 0x20 | 1
	}
	if rename {
		access |= 0x10000
	}
	if create {
		access |= syscall.GENERIC_WRITE
	}
	disposition := uint32(1)
	if create {
		disposition = 2
	}
	var h syscall.Handle
	var status ioStatus
	code, _, _ := ntCreate.Call(uintptr(unsafe.Pointer(&h)), uintptr(access), uintptr(unsafe.Pointer(&a)), uintptr(unsafe.Pointer(&status)), 0, syscall.FILE_ATTRIBUTE_NORMAL, uintptr(share), uintptr(disposition), uintptr(options), 0, 0)
	runtime.KeepAlive(text)
	runtime.KeepAlive(a)
	if code != 0 {
		return nil, errors.New("open_failed")
	}
	f := os.NewFile(uintptr(h), name)
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		f.Close()
		return nil, errors.New("physical_object_required")
	}
	return f, nil
}
func parentAt(path string) (*physicalParent, error) {
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	if !filepath.IsAbs(path) || len(volume) != 2 || volume[1] != ':' {
		return nil, errors.New("physical_local_volume_required")
	}
	root := volume + string(os.PathSeparator)
	ptr, e := syscall.UTF16PtrFromString(root)
	if e != nil {
		return nil, errors.New("physical_parent")
	}
	h, e := syscall.CreateFile(ptr, 0x100000|0x80|1, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x02000000|0x00200000, 0)
	if e != nil {
		return nil, errors.New("physical_parent")
	}
	p := &physicalParent{handles: []syscall.Handle{h}, last: h}
	for _, part := range strings.Split(strings.TrimPrefix(path, root), string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		f, e := openAt(p.last, part, true, false, false)
		if e != nil {
			p.close()
			return nil, e
		}
		var next syscall.Handle
		process, _ := syscall.GetCurrentProcess()
		if syscall.DuplicateHandle(process, syscall.Handle(f.Fd()), process, &next, 0, false, syscall.DUPLICATE_SAME_ACCESS) != nil {
			f.Close()
			p.close()
			return nil, errors.New("physical_parent")
		}
		f.Close()
		p.handles = append(p.handles, next)
		p.last = next
	}
	return p, nil
}
func renameHeld(f *os.File, parent syscall.Handle, name string) error {
	if !component(name) {
		return errors.New("invalid_component")
	}
	u, e := syscall.UTF16FromString(name)
	if e != nil {
		return errors.New("invalid_component")
	}
	type header struct {
		Replace uint32
		Root    syscall.Handle
		Length  uint32
		Name    [1]uint16
	}
	n := int(unsafe.Offsetof(header{}.Name)) + 2*(len(u)-1)
	if n < int(unsafe.Sizeof(header{})) {
		n = int(unsafe.Sizeof(header{}))
	}
	buf := make([]byte, n)
	h := (*header)(unsafe.Pointer(&buf[0]))
	h.Root = parent
	h.Length = uint32(2 * (len(u) - 1))
	copy(unsafe.Slice(&h.Name[0], len(u)-1), u[:len(u)-1])
	var status ioStatus
	code, _, _ := ntSet.Call(f.Fd(), uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 10)
	runtime.KeepAlive(f)
	if code != 0 {
		return errors.New("rename_collision_or_failure")
	}
	return nil
}
func identity(f *os.File) ([3]uint32, error) {
	var info syscall.ByHandleFileInformation
	e := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info)
	return [3]uint32{info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow}, e
}
func sddl(f *os.File) (string, error) {
	var sd uintptr
	code, _, _ := getSecurity.Call(f.Fd(), 1, 7, 0, 0, 0, 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 || sd == 0 {
		return "", errors.New("metadata_read_failed")
	}
	defer localFree.Call(sd)
	var text *uint16
	var length uint32
	ok, _, _ := securityString.Call(sd, 1, 7, uintptr(unsafe.Pointer(&text)), uintptr(unsafe.Pointer(&length)))
	if ok == 0 || text == nil || length == 0 || length > 1<<20 {
		return "", errors.New("metadata_read_failed")
	}
	defer localFree.Call(uintptr(unsafe.Pointer(text)))
	return syscall.UTF16ToString(unsafe.Slice(text, length)), nil
}
