package main

// Classify only the roots of three original, completed Console CI resources.
// This is metadata qualification for the existing preservation/quarantine path;
// it grants neither recursive reads nor deletion of their Git data.
import (
	"encoding/json"
	"path/filepath"
	"syscall"
)

const ci3574Evidence = "C:/Users/makhmeto/Documents/Codex/2026-10-24/http-localhost-10012-view-kanban-layout/work/ci-3574"

type ci3574Owned struct {
	id, receiptSHA string
	fileID         uint64
}

var ci3574OwnedRoots = []ci3574Owned{
	{"af44d1559483132d209b05b02fbc1878", "d83f8aac4d77d321e51dbe4a7b86778e8dbfaa2dae6d10a1c15a8c5c0627a641", 7881299348213894},
	{"8e934105e613620abf90e7cecd787162", "417c09febc030d9e548fdef5fb087f3f917b17bc523af615d5810d84723e7d47", 58828270133661538},
	{"38c2d816dc21ce06fe3f42c8dccfae71", "b907662c8c210ee927805ee99ff0127e9f61d470d745966afc1806a30d0813ac", 52354345669394033},
}

func ci3574Root(p ci3574Owned) string {
	return "C:/Users/makhmeto/AppData/Local/Temp/codex-owned-" + p.id
}

type ci3574GuardAccess struct {
	source   func() bool
	denied   func(string) bool
	plain    func(string, bool) bool
	read     func(string, int64) ([]byte, error)
	identity func(string) (syscall.ByHandleFileInformation, error)
}

type ci3574GuardProof struct {
	path, sha string
	limit     int64
}

func ci3574RootAllowed(path string) bool {
	return ci3574RootAllowedWith(path, ci3574GuardAccess{
		source: func() bool {
			db, err := readObject(productStore, 64<<20)
			p, _ := requested3650Product(3623)
			return err == nil && requestedResumeQualified(db, p, requested3650Access(false))
		}, denied: legacyMatch, plain: currentPlain, read: readBounded, identity: owned2736Identity,
	})
}

func ci3574RootAllowedWith(path string, a ci3574GuardAccess) bool {
	var candidate *ci3574Owned
	for _, p := range ci3574OwnedRoots {
		if same(path, ci3574Root(p)) {
			copy := p
			candidate = &copy
			break
		}
	}
	if candidate == nil || a.denied(path) || !a.plain(path, true) || !a.source() {
		return false
	}
	p := *candidate
	proofs := []ci3574GuardProof{
		{ci3574Evidence + "/result.json", "f219e3bfd3468431ee3acba887936ad4cd89e55a6c6d7a79e23af9bffb837246", 128 << 10},
		{ci3574Evidence + "/final-cleanup-results.json", "5b9e2a5f33b8a120f06b702ff3fb1e4a7b347a94aaafa2f956c8974cd3a2b108", 64 << 10},
		{"D:/CC/Tools/safe-delete/receipts-go/" + p.id + ".json", p.receiptSHA, 8192},
	}
	return ci3574GuardQualified(path, p, proofs, a)
}

func ci3574GuardQualified(path string, p ci3574Owned, proofs []ci3574GuardProof, a ci3574GuardAccess) bool {
	if len(proofs) != 3 || !same(path, ci3574Root(p)) {
		return false
	}
	for _, proof := range proofs {
		if a.denied(proof.path) || !a.plain(proof.path, false) {
			return false
		}
		b, err := a.read(proof.path, proof.limit)
		if err != nil || hashBytes(b) != proof.sha {
			return false
		}
		if filepath.Base(proof.path) == p.id+".json" {
			var record object
			if json.Unmarshal(b, &record) != nil || record["state"] != "owned" || record["project"] != "local-ai-console-3574" || !same(str(record["path"]), path) {
				return false
			}
		}
	}
	root, err := a.identity(path)
	return err == nil && root.VolumeSerialNumber == 2229119083 &&
		uint64(root.FileIndexHigh)<<32|uint64(root.FileIndexLow) == p.fileID && root.NumberOfLinks == 1 &&
		root.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 && root.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0
}
