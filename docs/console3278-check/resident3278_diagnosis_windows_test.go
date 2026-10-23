package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A fixed-purpose, read-only diagnosis of the same compiled leaf. Contents
// are read only by the unchanged engine; no unknown digest is returned.
// This is not a qualification or a baseline-creation path.
func TestActual3278Diagnosis(t *testing.T) {
	if len(model3081UpdateLeaves) != 1 {
		t.Fatal("fixed diagnosis must have exactly one leaf")
	}
	leaf := model3081UpdateLeaves[0]
	if leaf.kind != "resident" || leaf.target != "D:/CC/Tools/resident-guard/resident-guard.exe" {
		t.Fatal("fixed diagnosis scope changed")
	}
	if !runtimeLeaf(leaf.target, filepath.Dir(leaf.target), legacyMatch, true) {
		t.Fatal("target physical/legacy check failed before content access")
	}
	t.Log("target physical/legacy check passed")
	s := modelPinSession{protected: legacyMatch, source: permanentRuntimeSourceVerified, head: modelSourceHead}
	defer s.close()
	if !modelEvidenceIndependent(leaf.baselines[0].evidence, leaf.target, leaf.candidate.path) || !s.evidence(leaf.baselines[0].evidence) {
		t.Fatal("original baseline source/receipt verification failed")
	}
	t.Log("original source/HEAD/origin/tracked bytes and both original receipt pins verified")
	c := leaf.candidate
	if !modelCandidateEvidenceIndependent(c, leaf) || !s.sourceRoot(c.root, c.head) || !s.evidence(c.evidence) || !s.match(c.path, []string{c.sha256}) || !s.recheck() {
		t.Fatal("candidate source/independent receipt/physical bytes verification failed")
	}
	t.Log("candidate source/HEAD/origin/tracked bytes, independent original build receipt and actual candidate bytes verified")
	ptr, err := syscall.UTF16PtrFromString(leaf.target)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(ptr, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000|0x08000000, 0)
	if err != nil {
		t.Fatalf("target read-only pinned open failed: %v", err)
	}
	f := os.NewFile(uintptr(h), leaf.target)
	defer f.Close()
	var before, after syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &before); err != nil || !modelFileInfoValid(before) {
		t.Fatalf("target native identity/type/link count failed: %v", err)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 64<<20 {
		t.Fatalf("target physical size/type failed: %v", err)
	}
	actual, ok := modelHandlePath(h)
	if !ok || !same(actual, leaf.target) {
		t.Fatal("target handle path changed")
	}
	match := s.match(leaf.target, []string{leaf.baselines[0].sha256, c.sha256})
	if err := syscall.GetFileInformationByHandle(h, &after); err != nil {
		t.Fatal(err)
	}
	before.LastAccessTime = after.LastAccessTime
	if before != after || !modelFileInfoValid(after) || !s.recheck() {
		t.Fatal("native identity or pinned original/candidate proof changed during diagnosis")
	}
	t.Logf("target read-only open/type/size/one-link/handle-path/stable-identity checks passed; matches fixed original baseline or reviewed candidate=%v; no unknown digest exposed", match)
}
