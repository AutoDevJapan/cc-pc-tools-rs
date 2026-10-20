// One-off manifest verifier for an explicitly owned same-volume quarantine.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"
)

type entry struct {
	Path       string           `json:"path"`
	Directory  bool             `json:"directory"`
	Volume     uint32           `json:"volume"`
	ID         uint64           `json:"file_id"`
	Size       uint64           `json:"size"`
	Attributes uint32           `json:"attributes"`
	Created    syscall.Filetime `json:"created"`
	Written    syscall.Filetime `json:"written"`
	Links      uint32           `json:"links"`
	SHA256     string           `json:"sha256,omitempty"`
}
type manifest struct {
	Root             string  `json:"root"`
	UTC              string  `json:"utc"`
	Ancestors        []entry `json:"ancestors"`
	Entries          []entry `json:"entries"`
	Bytes            uint64  `json:"bytes"`
	AllLinksInternal bool    `json:"all_links_internal"`
}

func checked(err error) {
	if err != nil {
		panic(err)
	}
}
func info(p string) entry {
	ptr, err := syscall.UTF16PtrFromString(p)
	checked(err)
	h, err := syscall.CreateFile(ptr, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	checked(err)
	defer syscall.CloseHandle(h)
	var data syscall.ByHandleFileInformation
	checked(syscall.GetFileInformationByHandle(h, &data))
	if data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		panic("reparse point: " + p)
	}
	return entry{Path: p, Directory: data.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0, Volume: data.VolumeSerialNumber, ID: uint64(data.FileIndexHigh)<<32 | uint64(data.FileIndexLow), Size: uint64(data.FileSizeHigh)<<32 | uint64(data.FileSizeLow), Attributes: data.FileAttributes, Created: data.CreationTime, Written: data.LastWriteTime, Links: data.NumberOfLinks}
}
func save(p string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	checked(err)
	checked(os.WriteFile(p, append(b, '\n'), 0644))
}
func scan(root, out string) {
	abs, err := filepath.Abs(root)
	checked(err)
	m := manifest{Root: abs, UTC: time.Now().UTC().Format(time.RFC3339Nano), AllLinksInternal: true}
	for p := abs; ; p = filepath.Dir(p) {
		m.Ancestors = append(m.Ancestors, info(p))
		if filepath.Dir(p) == p {
			break
		}
	}
	if m.Ancestors[0].Volume != 1215965535 || m.Ancestors[0].ID != 35184372095523418 {
		panic("root identity does not match owned receipt")
	}
	counts := map[string]uint32{}
	checked(filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		before := info(p)
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return err
		}
		before.Path = filepath.ToSlash(rel)
		if !before.Directory {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, copyErr := io.Copy(h, f)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			after := info(p)
			after.Path = before.Path
			if !reflect.DeepEqual(before, after) {
				return fmt.Errorf("file changed during read: %s", rel)
			}
			before.SHA256 = hex.EncodeToString(h.Sum(nil))
			m.Bytes += before.Size
			counts[fmt.Sprintf("%d/%d", before.Volume, before.ID)]++
		}
		m.Entries = append(m.Entries, before)
		return nil
	}))
	for _, e := range m.Entries {
		if !e.Directory && counts[fmt.Sprintf("%d/%d", e.Volume, e.ID)] != e.Links {
			m.AllLinksInternal = false
			panic("external hard link: " + e.Path)
		}
	}
	save(out, m)
	fmt.Printf("entries=%d bytes=%d all_links_internal=true\n", len(m.Entries), m.Bytes)
}
func read(p string) manifest {
	var m manifest
	b, err := os.ReadFile(p)
	checked(err)
	checked(json.Unmarshal(b, &m))
	return m
}
func main() {
	if len(os.Args) != 4 {
		panic("usage: preserve3214 <scan|equal> <root-or-before> <out-or-after>")
	}
	switch os.Args[1] {
	case "scan":
		scan(os.Args[2], os.Args[3])
	case "equal":
		a, b := read(os.Args[2]), read(os.Args[3])
		if a.Bytes != b.Bytes || !a.AllLinksInternal || !b.AllLinksInternal || !reflect.DeepEqual(a.Entries, b.Entries) {
			panic("preservation manifests differ")
		}
		fmt.Printf("identical entries=%d bytes=%d; volume/file IDs, contents, attributes, links and creation/write times preserved\n", len(a.Entries), a.Bytes)
	default:
		panic("unknown operation")
	}
}
