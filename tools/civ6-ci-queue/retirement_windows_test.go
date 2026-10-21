//go:build windows

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestQuarantinedReceiptUsesActualOriginalDirectoryIdentity(t *testing.T) {
	base := t.TempDir()
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	key := "801/1/8011"
	root := filepath.Join(base, "codex-owned-"+id)
	dest := filepath.Join(base, "quarantine")
	for _, p := range []string{root, dest, filepath.Join(base, "receipts-go")} {
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	w, _ := syscall.UTF16PtrFromString(root)
	h, e := syscall.CreateFile(w, 0x80, 7, nil, 3, 0x02000000|0x00200000, 0)
	if e != nil {
		t.Fatal(e)
	}
	var info syscall.ByHandleFileInformation
	e = syscall.GetFileInformationByHandle(h, &info)
	syscall.CloseHandle(h)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(root, filepath.Join(dest, "files")); e != nil {
		t.Fatal(e)
	}
	cl := CleanupRec{Root: root, Instance: id, SafeDeleteID: id}
	r := retirementRecord{retirementIdentity: retirementIdentity{Version: 2, ID: id, Root: base, Path: root, Volume: info.VolumeSerialNumber, FileID: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow), Project: "civ6-ci-queue", Content: "job-" + safeKey(key)}, State: "quarantined", Destination: dest}
	write := func() {
		b, _ := json.Marshal(r.retirementIdentity)
		r.Integrity = digestHexBytes(b)
		b, _ = json.Marshal(r)
		if e := os.WriteFile(filepath.Join(base, "receipts-go", id+".json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	exe := filepath.Join(base, "fixed-safe-delete.exe")
	if e = os.WriteFile(exe, []byte("fake fixed executable; never executed"), 0600); e != nil {
		t.Fatal(e)
	}
	pin := Pinned{Path: exe, SHA256: digestHexBytes([]byte("fake fixed executable; never executed"))}
	write()
	if got, e := retiredReceipt(pin, cl, key); e != nil || got.State != "quarantined" {
		t.Fatal("actual moved directory rejected", got, e)
	}
	r.FileID++
	write()
	if _, e = retiredReceipt(pin, cl, key); e == nil {
		t.Fatal("wrong file ID accepted")
	}
	r.FileID--
	r.Content = "job-other"
	write()
	if _, e = retiredReceipt(pin, cl, key); e == nil {
		t.Fatal("wrong job receipt accepted")
	}
	r.Content = "job-" + safeKey(key)
	write()
	link := filepath.Join(base, "junction")
	c := exec.Command(`C:\Windows\System32\cmd.exe`, "/d", "/c", "mklink", "/J", link, dest)
	hide(c)
	if e = c.Run(); e != nil {
		t.Fatal("create fixture junction", e)
	}
	if retiredIdentityMatches(filepath.Join(link, "files"), r.Volume, r.FileID) {
		t.Fatal("reparse ancestor accepted")
	}
	if e = os.Remove(link); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(filepath.Join(dest, "files"), filepath.Join(dest, "retained")); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dest, "files"), []byte("ordinary file"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = retiredReceipt(pin, cl, key); e == nil {
		t.Fatal("ordinary file accepted as retired directory")
	}
}
