//go:build windows

// One-off ToDo3242 cleanup receipt. It does not run product tests or move files.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const original = `D:\CC\Work\unattended-operations-20261013-wt\todo-3242-3103\codex-owned-bcd13dfcd2f6a3494a968c067cd0c48f`
const destination = `D:\削除希望\20261021-todo-3242-post3226-fixed-red-bcd13dfc\files`
const expectedID = uint64(10414574145053331)
const expectedVolume = uint32(1215965535)

type record struct {
	Path     string `json:"path"`
	Dir      bool   `json:"directory"`
	Volume   uint32 `json:"volume"`
	ID       string `json:"file_id"`
	Links    uint32 `json:"links"`
	Bytes    uint64 `json:"bytes"`
	Attrs    uint32 `json:"attributes"`
	Created  uint64 `json:"created"`
	Modified uint64 `json:"modified"`
	SHA256   string `json:"sha256,omitempty"`
}

func ticks(t syscall.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }

func openInfo(path string) (*os.File, syscall.ByHandleFileInformation, error) {
	var info syscall.ByHandleFileInformation
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil { return nil, info, err }
	handle, err := syscall.CreateFile(wide, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|0x00200000, 0)
	if err != nil { return nil, info, err }
	file := os.NewFile(uintptr(handle), path)
	if err = syscall.GetFileInformationByHandle(handle, &info); err != nil { file.Close(); return nil, info, err }
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 { file.Close(); return nil, info, fmt.Errorf("reparse point rejected") }
	return file, info, nil
}

func identity(info syscall.ByHandleFileInformation, rel string) record {
	id := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	return record{Path: rel, Dir: info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0,
		Volume: info.VolumeSerialNumber, ID: fmt.Sprintf("%016x", id), Links: info.NumberOfLinks,
		Bytes: uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow), Attrs: info.FileAttributes,
		Created: ticks(info.CreationTime), Modified: ticks(info.LastWriteTime)}
}

func inspect(root, output string) error {
	root, err := filepath.Abs(root)
	if err != nil { return err }
	if !strings.EqualFold(root, original) && !strings.EqualFold(root, destination) { return fmt.Errorf("outside fixed owned roots") }
	for path := root; ; path = filepath.Dir(path) {
		file, _, err := openInfo(path)
		if err != nil { return err }
		file.Close()
		if filepath.Dir(path) == path { break }
	}
	rootFile, rootInfo, err := openInfo(root)
	if err != nil { return err }
	rootFile.Close()
	if uint64(rootInfo.FileIndexHigh)<<32|uint64(rootInfo.FileIndexLow) != expectedID || rootInfo.VolumeSerialNumber != expectedVolume { return fmt.Errorf("owned root identity changed") }
	out, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil { return err }
	defer out.Close()
	encoder := json.NewEncoder(out)
	treeHash := sha256.New()
	encoder = json.NewEncoder(io.MultiWriter(out, treeHash))
	type links struct { inside, total uint32 }
	linkCounts := map[string]links{}
	var count, files, size, shared uint64
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil { return walkErr }
		switch strings.ToLower(entry.Name()) { case ".git", ".codex", ".agents", ".ssh": return fmt.Errorf("protected metadata rejected") }
		file, info, err := openInfo(path)
		if err != nil { return err }
		defer file.Close()
		rel, err := filepath.Rel(root, path)
		if err != nil { return err }
		r := identity(info, filepath.ToSlash(rel))
		if !r.Dir {
			digest := sha256.New()
			if _, err = io.Copy(digest, file); err != nil { return err }
			r.SHA256 = hex.EncodeToString(digest.Sum(nil))
			var again syscall.ByHandleFileInformation
			if err = syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &again); err != nil { return err }
			if r != func() record { a := identity(again, r.Path); a.SHA256 = r.SHA256; return a }() { return fmt.Errorf("file changed during snapshot") }
			key := fmt.Sprintf("%d:%s", r.Volume, r.ID)
			links := linkCounts[key]
			if links.total != 0 && links.total != r.Links { return fmt.Errorf("link count changed") }
			links.total, links.inside = r.Links, links.inside+1
			linkCounts[key] = links
			files++
			size += r.Bytes
		}
		count++
		return encoder.Encode(r)
	})
	if err != nil { return err }
	for _, links := range linkCounts {
		if links.inside != links.total { return fmt.Errorf("external hardlink or incomplete enumeration") }
		if links.total > 1 { shared++ }
	}
	if err = out.Sync(); err != nil { return err }
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"root":root,"root_file_id":fmt.Sprintf("%016x",expectedID),"volume":expectedVolume,
		"entries_including_root":count,"files":files,"bytes":size,"internal_shared_file_identities":shared,"all_links_contained":true,"no_reparse_points":true,
		"manifest_sha256":hex.EncodeToString(treeHash.Sum(nil))})
}

func main() {
	if len(os.Args) != 3 { fmt.Fprintln(os.Stderr, "expected fixed-root and new-manifest-file"); os.Exit(2) }
	if err := inspect(os.Args[1], os.Args[2]); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}
