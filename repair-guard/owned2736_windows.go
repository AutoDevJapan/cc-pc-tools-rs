package main

// One original, self-created Windows CI resource, explicitly assigned to
// ToDo2908 on 2026-10-19. This does not reconstruct the lost private registry.
// Classification reads saved operational receipts and directory metadata only;
// it neither reads this resource's contents nor authorizes collection by itself.
import (
	"path/filepath"
	"syscall"
	"time"
)

const owned2736Root = "C:/Users/makhmeto/AppData/Local/Temp/codex-owned-50515037115a8be86b5a45e35a5d5a63"
const owned2736Tree = "D:/CC/Work/unattended-operations-20261013-wt/todo-2736-pr148-ci"
const owned2736Evidence = owned2736Tree + "/codex-owned-184a58eedf4f923741f160dfe7cfd5b7"
const owned2736Job = "20261019-022634-todo-2736"
const owned2736JobFile = "D:/CC/Tools/codex-jobs/jobs/" + owned2736Job + "/job.json"

// Raw byte hashes independently pinned in this reviewed source. Do not accept
// caller-supplied paths/hashes, a regenerated manifest, or a newly written grant.
type owned2736Receipt struct{ name, sha256 string }

var owned2736Receipts = [...]owned2736Receipt{
	{"windows2-new-owned-022634.stdout.log", "006510082b31ea916801a8f77eebece1a93a5f266229d309df439070c72abff7"},
	{"windows2-plan-022634.json", "732af50423de054727170b447c46d2918cffb55006c0e3c49f68c86d7247fa0f"},
	{"windows2-prepared.json", "63019fa85d9e025564cac0bddbc4a4174aabfea5208485654ba5bd357948a528"},
	{"windows2-source-owner.json", "cc7b3258e19f6159656703163c5e9f0cdba996512a0f67f2bd84dc1998f407f3"},
	{"windows2-source-materialized.json", "7163193bc01108af878a546b350a9da9279bd2a4f5858258b1668434ea8ae623"},
	{"windows2-prepare-process.json", "b8079b912a4674228502a62a75b4a32e660595d7172bd66ddca727ea7a322c8e"},
}

type owned2736Access struct {
	at        time.Time
	protected func(string) bool
	plain     func(string) bool
	origin    func(string, string) bool
	read      func(string, int64) ([]byte, bool)
	identity  func(string) (syscall.ByHandleFileInformation, error)
}

func owned2736PathAllowed(path string) bool {
	return owned2736Allowed(path, owned2736Access{
		at: time.Now(), protected: legacyMatch, plain: conversationPhysical,
		origin: sourceOriginVerified, identity: owned2736Identity,
		read: func(path string, limit int64) ([]byte, bool) {
			return conversationBytes(path, limit, legacyMatch)
		},
	})
}

func owned2736Identity(path string) (syscall.ByHandleFileInformation, error) {
	var info syscall.ByHandleFileInformation
	u, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return info, err
	}
	// FILE_READ_ATTRIBUTES: never open resource file contents to classify it.
	h, err := syscall.CreateFile(u, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil,
		syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return info, err
	}
	defer syscall.CloseHandle(h)
	err = syscall.GetFileInformationByHandle(h, &info)
	return info, err
}

func owned2736Allowed(path string, access owned2736Access) bool {
	return owned2736AllowedWith(path, access, owned2736Receipts[:])
}

func owned2736AllowedWith(path string, access owned2736Access, receipts []owned2736Receipt) bool {
	// Membership and finite time are checked before any metadata, Git or reads.
	// No sibling, unknown namespace, absent root or new resource can qualify.
	if !filepath.IsAbs(path) || !within(owned2736Root, path) ||
		access.at.Before(time.Date(2026, 10, 19, 0, 0, 0, 0, time.FixedZone("JST", 9*3600))) ||
		!access.at.Before(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)) {
		return false
	}
	for _, p := range []string{path, owned2736Root, owned2736Tree, owned2736Evidence, productStore, owned2736JobFile} {
		if access.protected(p) || !access.plain(p) {
			return false
		}
	}
	if !access.origin(owned2736Tree, "autodevjapan/cc-pc-tools-rs") {
		return false
	}
	if len(receipts) == 0 {
		return false
	}
	for _, receipt := range receipts {
		p := filepath.Join(owned2736Evidence, receipt.name)
		if access.protected(p) || !access.plain(p) {
			return false
		}
		body, ok := access.read(p, 64<<10)
		if !ok || hashBytes(body) != receipt.sha256 {
			return false
		}
	}
	// The historical parent job, normal ToDo record and saved worktree must
	// still agree. Current wait/start state is not a replacement ownership grant.
	jobBytes, ok := access.read(owned2736JobFile, 4<<20)
	if !ok {
		return false
	}
	job, ok := conversationJSON(jobBytes)
	owner := obj(job["work_owner"])
	if !ok || job["id"] != owned2736Job || conversationNumber(job["todo_id"]) != 2736 ||
		job["status"] != "succeeded" || !same(str(job["cwd"]), owned2736Tree) ||
		owner["repository"] != "autodevjapan/cc-pc-tools-rs" ||
		!same(str(owner["worktree"]), owned2736Tree) || owner["branch"] != "scheduler/todo-2736-pr148-ci" {
		return false
	}
	dbBytes, ok := access.read(productStore, 64<<20)
	if !ok {
		return false
	}
	db, ok := conversationJSON(dbBytes)
	if !ok {
		return false
	}
	var original object
	rows, _ := db["items"].([]any)
	for _, row := range rows {
		if conversationNumber(obj(row)["id"]) == 2736 {
			if original != nil {
				return false
			}
			original = obj(row)
		}
	}
	if original == nil || original["project"] != "cc-pc-tools-rs" || !hasJob(original, owned2736Job) ||
		!same(str(obj(original["scheduler"])["worktree"]), owned2736Tree) {
		return false
	}
	info, err := access.identity(owned2736Root)
	// The creation receipt records time.Now after mkdir, not NTFS birth time.
	// Its exact bytes are pinned above; the actual NTFS volume/file ID is the
	// physical identity. Do not equate those two different kinds of timestamp.
	if err != nil || info.VolumeSerialNumber != 2229119083 ||
		uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow) != 56857945299087113 ||
		info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 ||
		info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	// Recheck after receipt IO. Classification is read-only, and callers must
	// still use the collector's existing SID/service/process/remote checks.
	return !access.protected(path) && !access.protected(owned2736Root) &&
		access.plain(owned2736Root) && access.plain(path)
}
