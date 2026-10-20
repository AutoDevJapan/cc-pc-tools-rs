// One-off Windows metadata inventory for this task's owned temporary directory.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"
)

const workspace = `D:\CC\Work\unattended-operations-20261013-wt\todo-3197-3103-source`
const owned = workspace + `\codex-owned-8daae585cac606ae8a496ca2af6b9169`
const destination = `D:\削除希望\20261021-todo-3197-post3186-red-8daae585\data`
const evidence = workspace + `\docs\todo-3197-post3186-red`
const expectedVolume = uint32(1215965535)
const expectedRootID = uint64(16044073679004940)

type entry struct {
	Path       string `json:"path"`
	ID         string `json:"file_id"`
	Volume     uint32 `json:"volume"`
	Attributes uint32 `json:"attributes"`
	Links      uint32 `json:"links"`
	Directory  bool   `json:"directory"`
	Bytes      uint64 `json:"bytes"`
	Created    uint64 `json:"creation_time"`
	Written    uint64 `json:"write_time"`
}
type inventory struct {
	At           string  `json:"at"`
	Root         string  `json:"root"`
	Entries      []entry `json:"entries"`
	ChildCount   int     `json:"child_count"`
	FileCount    int     `json:"file_count"`
	Bytes        uint64  `json:"bytes"`
	LinksChecked bool    `json:"no_reparse_or_external_hardlinks"`
}

func fail(err error) {
	if err != nil {
		panic(err)
	}
}
func info(path string) syscall.ByHandleFileInformation {
	p, err := syscall.UTF16PtrFromString(path)
	fail(err)
	h, err := syscall.CreateFile(p, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, 0x02000000|0x00200000, 0)
	fail(err)
	defer syscall.CloseHandle(h)
	var result syscall.ByHandleFileInformation
	fail(syscall.GetFileInformationByHandle(h, &result))
	if result.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		panic("reparse point: " + path)
	}
	return result
}
func fileID(i syscall.ByHandleFileInformation) uint64 {
	return uint64(i.FileIndexHigh)<<32 | uint64(i.FileIndexLow)
}
func fileTime(t syscall.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
func walk(root string) inventory {
	for p := root; ; p = filepath.Dir(p) {
		_ = info(p)
		if filepath.Dir(p) == p {
			break
		}
	}
	r := info(root)
	if r.VolumeSerialNumber != expectedVolume || fileID(r) != expectedRootID {
		panic("owned root identity mismatch")
	}
	result := inventory{At: time.Now().Format(time.RFC3339Nano), Root: root}
	counts := map[string]uint32{}
	links := map[string]uint32{}
	fail(filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && !strings.HasPrefix(strings.ToLower(path), strings.ToLower(root)+`\`) {
			return fmt.Errorf("outside owned root")
		}
		i := info(path)
		if i.VolumeSerialNumber != expectedVolume {
			return fmt.Errorf("volume changed")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		size := uint64(i.FileSizeHigh)<<32 | uint64(i.FileSizeLow)
		dir := i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
		if dir {
			size = 0
		} else {
			result.FileCount++
			result.Bytes += size
			key := fmt.Sprint(fileID(i))
			counts[key]++
			links[key] = i.NumberOfLinks
		}
		result.Entries = append(result.Entries, entry{rel, fmt.Sprint(fileID(i)), i.VolumeSerialNumber, i.FileAttributes, i.NumberOfLinks, dir, size, fileTime(i.CreationTime), fileTime(i.LastWriteTime)})
		return nil
	}))
	for key, count := range counts {
		if count != links[key] {
			panic(fmt.Sprintf("external hardlink: id=%s internal=%d links=%d", key, count, links[key]))
		}
	}
	result.ChildCount = len(result.Entries) - 1
	result.LinksChecked = true
	sort.Slice(result.Entries, func(a, b int) bool { return result.Entries[a].Path < result.Entries[b].Path })
	return result
}
func save(path string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	fail(err)
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	fail(err)
	_, err = f.Write(data)
	fail(err)
	fail(f.Close())
}
func main() {
	if len(os.Args) != 2 {
		panic("before or after required")
	}
	switch os.Args[1] {
	case "before":
		if filepath.Clean(owned) == filepath.Clean(workspace) || !strings.HasPrefix(owned, workspace+`\`) || !strings.EqualFold(filepath.VolumeName(owned), filepath.VolumeName(destination)) {
			panic("invalid boundary")
		}
		if _, err := os.Lstat(destination); !os.IsNotExist(err) {
			panic("destination exists or cannot verify absence")
		}
		r := walk(owned)
		if r.ChildCount != 5025 || r.Bytes != 1460365649 {
			panic("plan count or size changed")
		}
		save(evidence+`\cleanup-inventory-before.json`, r)
		fmt.Printf("before: %d children, %d files, %d bytes, all IDs recorded, no links to other data\n", r.ChildCount, r.FileCount, r.Bytes)
	case "verify-before":
		data, err := os.ReadFile(evidence + `\cleanup-inventory-before.json`)
		fail(err)
		var before inventory
		fail(json.Unmarshal(data, &before))
		now := walk(owned)
		if !reflect.DeepEqual(before.Entries, now.Entries) || before.Bytes != now.Bytes || before.ChildCount != now.ChildCount {
			panic("source changed since preflight")
		}
		if _, err := os.Lstat(destination); !os.IsNotExist(err) {
			panic("destination exists or absence unverified")
		}
		save(evidence+`\cleanup-pre-move-verification.json`, map[string]any{"at": time.Now().Format(time.RFC3339Nano), "all_file_ids_and_metadata_unchanged": true, "destination_absent": true, "source": owned, "destination": destination})
		fmt.Println("pre-move: all file IDs and metadata unchanged; destination absent")
	case "after":
		if _, err := os.Lstat(owned); !os.IsNotExist(err) {
			panic("original source remains or absence unverified")
		}
		data, err := os.ReadFile(evidence + `\cleanup-inventory-before.json`)
		fail(err)
		var before inventory
		fail(json.Unmarshal(data, &before))
		after := walk(destination)
		if !reflect.DeepEqual(before.Entries, after.Entries) || before.Bytes != after.Bytes || before.FileCount != after.FileCount || before.ChildCount != after.ChildCount {
			panic("moved metadata/IDs differ")
		}
		save(evidence+`\cleanup-inventory-after.json`, after)
		save(evidence+`\cleanup-move-verification.json`, map[string]any{"at": time.Now().Format(time.RFC3339Nano), "source": owned, "destination": destination, "source_absent": true, "all_file_ids_and_metadata_identical": true, "child_count": after.ChildCount, "file_count": after.FileCount, "bytes": after.Bytes, "no_reparse_or_external_hardlinks": true, "root_file_id": fmt.Sprint(expectedRootID), "volume": expectedVolume})
		fmt.Printf("after: all %d entries including root match; source absent\n", len(after.Entries))
	default:
		panic("unknown stage")
	}
}
