package main

import (
	"encoding/json"
	"errors"
	"strings"
	"syscall"
	"testing"
)

func TestCI3574RootOnlyChecksEveryOriginalProofAndPhysicalIdentity(t *testing.T) {
	p := ci3574Owned{id: strings.Repeat("1", 32), fileID: 42}
	path := ci3574Root(p)
	data, _ := json.Marshal(object{"state": "owned", "project": "local-ai-console-3574", "path": path})
	proofs := []ci3574GuardProof{{"D:/fixture/result.json", hashBytes([]byte("result")), 100}, {"D:/fixture/end.json", hashBytes([]byte("end")), 100}, {"D:/fixture/" + p.id + ".json", hashBytes(data), 8192}}
	for _, kind := range []string{"pass", "first", "second", "record", "missing", "read_error", "denied", "linked", "volume", "file_id", "links", "not_dir", "reparse", "identity_error", "different_root", "incomplete"} {
		t.Run(kind, func(t *testing.T) {
			q := append([]ci3574GuardProof(nil), proofs...)
			input := path
			a := ci3574GuardAccess{
				denied: func(string) bool { return kind == "denied" }, plain: func(string, bool) bool { return kind != "linked" },
				read: func(name string, _ int64) ([]byte, error) {
					if kind == "read_error" {
						return nil, errors.New("fixture")
					}
					switch name {
					case q[0].path:
						if kind == "first" {
							return []byte("changed"), nil
						}
						return []byte("result"), nil
					case q[1].path:
						if kind == "second" {
							return []byte("changed"), nil
						}
						return []byte("end"), nil
					default:
						if kind == "record" {
							return []byte("changed"), nil
						}
						return data, nil
					}
				},
				identity: func(string) (syscall.ByHandleFileInformation, error) {
					i := syscall.ByHandleFileInformation{VolumeSerialNumber: 2229119083, FileIndexLow: 42, NumberOfLinks: 1, FileAttributes: syscall.FILE_ATTRIBUTE_DIRECTORY}
					switch kind {
					case "volume":
						i.VolumeSerialNumber++
					case "file_id":
						i.FileIndexLow++
					case "links":
						i.NumberOfLinks++
					case "not_dir":
						i.FileAttributes = 0
					case "reparse":
						i.FileAttributes |= syscall.FILE_ATTRIBUTE_REPARSE_POINT
					case "identity_error":
						return i, errors.New("fixture")
					}
					return i, nil
				},
			}
			if kind == "missing" {
				q[2].sha = strings.Repeat("a", 64)
			}
			if kind == "different_root" {
				input += "/child"
			}
			if kind == "incomplete" {
				q = q[:2]
			}
			if got := ci3574GuardQualified(input, p, q, a); got != (kind == "pass") {
				t.Fatalf("qualified=%v", got)
			}
		})
	}
}

func TestCI3574UnknownOrChildPathNeverReadsOriginalReceipts(t *testing.T) {
	access := ci3574GuardAccess{source: func() bool { t.Fatal("unrelated source read"); return false }, denied: func(string) bool { t.Fatal("unrelated protection read"); return true }}
	for _, path := range []string{"C:/unknown", ci3574Root(ci3574OwnedRoots[0]) + "/runner", ci3574Root(ci3574OwnedRoots[0]) + "-other"} {
		if ci3574RootAllowedWith(path, access) {
			t.Fatal("non-root path admitted")
		}
	}
	for _, denied := range []bool{true, false} {
		called := false
		a := ci3574GuardAccess{denied: func(string) bool { return denied }, plain: func(string, bool) bool { return true }, source: func() bool { called = true; return false }}
		if ci3574RootAllowedWith(ci3574Root(ci3574OwnedRoots[0]), a) {
			t.Fatal("source failure admitted")
		}
		if called == denied {
			t.Fatal("source evaluated before original protection result")
		}
	}
}
