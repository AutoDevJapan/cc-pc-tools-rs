package main

// Bind one new, real native owner request to four unchanged original tasks.
// This never changes the lost registry, expired recovery window or task state.
import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type resumeNativeSource struct {
	Path, Session, Cwd, HeaderSHA, MessageSHA, At string
	Line                                          int
}

var resume3650Native = resumeNativeSource{
	Path:    "C:/Users/makhmeto/.codex/sessions/2026/10/24/rollout-2026-10-24T12-54-54-01a16c0c-18a9-7570-ab4e-9522ffa8d6d0.jsonl",
	Session: "01a16c0c-18a9-7570-ab4e-9522ffa8d6d0", Cwd: "C:/Users/makhmeto/Documents/Codex/2026-10-24/http-localhost-10012-view-kanban-layout",
	HeaderSHA:  "399bb5c8196526b817146b6dd375a98dab2fe0f8dedba97113e57a61b87f7ad8",
	MessageSHA: "e0cff4ef85745ffb93ddcc36d1fb9626c4c098967406d8875a672d58c1684465",
	At:         "2026-10-24T11:26:20.672Z", Line: 2672,
}

func resumeNativeMatches(reader io.Reader, proof resumeNativeSource) bool {
	if proof.Line < 2 || proof.Line > 1000000 || !modelDigestValid(proof.HeaderSHA) || !modelDigestValid(proof.MessageSHA) {
		return false
	}
	scanner := bufio.NewScanner(io.LimitReader(reader, 256<<20))
	scanner.Buffer(make([]byte, 64<<10), 36<<20)
	for line := 1; scanner.Scan(); line++ {
		if line != 1 && line != proof.Line {
			continue
		}
		var event object
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return false
		}
		payload := obj(event["payload"])
		if line == 1 {
			if hashBytes(scanner.Bytes()) != proof.HeaderSHA || event["type"] != "session_meta" || payload["id"] != proof.Session || payload["source"] != "vscode" || !same(str(payload["cwd"]), proof.Cwd) {
				return false
			}
		} else {
			return hashBytes(scanner.Bytes()) == proof.MessageSHA && event["type"] == "response_item" && payload["type"] == "message" && payload["role"] == "user" && event["timestamp"] == proof.At
		}
	}
	return false
}

func resumeReadNative(proof resumeNativeSource) bool {
	if legacyMatch(proof.Path) || !currentPlain(proof.Path, false) {
		return false
	}
	u, err := syscall.UTF16PtrFromString(proof.Path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(u, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return false
	}
	f := os.NewFile(uintptr(h), proof.Path)
	defer f.Close()
	return resumeNativeMatches(f, proof)
}

const resume3650Root = "D:/CC/Work/unattended-operations-20261013"
const resume3638Root = "D:/CC/Work/unattended-operations-20261013-wt/todo-2903-qualified-release"

const resume1626Archive = "C:/Users/makhmeto/.codex/sessions/2026/10/11/rollout-2026-10-11T01-06-28-01a12690-d332-7140-bc8c-47974cdcd2cb.jsonl.zst"

func requested3650OriginalSessions(pattern string) ([]string, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil || len(paths) != 0 {
		return paths, err
	}
	// The original native record was compressed by the existing archive
	// process. This exact original archive's header/session/cwd was decoded
	// and independently checked before pinning its bytes here. Do not alter
	// the old recovery API or accept a caller-supplied archive/hash.
	expected := filepath.Join("C:/Users/makhmeto/.codex/sessions/2026/10/11", "*-"+gameSession+".jsonl")
	if pattern != expected || legacyMatch(resume1626Archive) || !currentPlain(resume1626Archive, false) {
		return nil, nil
	}
	b, err := readBounded(resume1626Archive, 8<<20)
	if err != nil || hashBytes(b) != "360a6483be9f952d1b57befe5bc574848da3c448de7d0b9689dc8efa167f7487" {
		return nil, nil
	}
	return []string{resume1626Archive}, nil
}

func resume3650SourceQualified(db object) bool {
	// The saved delegation is a separate scope record, never a native user
	// message or Discord reply. Both must agree; neither replaces the other.
	return sourceVerified(resume3650Root) && sourceVerified(resume3638Root) &&
		currentQualification(db, "cc-pc-tools-rs", 3650, resume3650Root,
			"D:/CC/Tools/todo/specs/3650-cf9d4a622dd542aa8fe9df9fb787d1aa.md", "9287059625e106dca78627d0419a25c6bab0da6cf9de7baea7b1a4660a8bff1e", legacyMatch) &&
		currentQualification(db, "local-ai-console", 3638, resume3638Root,
			"D:/CC/Tools/todo/specs/3638-08eebd7fa48a42f99c0f64d47dcc6c17.md", "c2ee36db71afac04d11afa535ba6c30f6551d7220b035f5d6949f73088e5d1f5", legacyMatch) &&
		resumeReadNative(resume3650Native)
}

var requested3650Products = []currentProduct{
	{task: 1626, project: "PhosGaea-Vol1", root: gameRoot, repository: "autodevjapan/phosgaea-vol1", owner: "AutoDevJapan", repositoryID: 1185327122,
		spec: "D:/CC/Tools/todo/specs/1626-0718640658234cfdaf86c652828f717b.md", specSHA: "a600dda0a9dc0f26a3507e081ef5b938cd7de40bad92e500068f3a191b99ecb8"},
	{task: 3277, project: "GitDo", root: "C:/Users/makhmeto/Desktop/dev/ADJ/GitDo", repository: "autodevjapan/gitdo", owner: "AutoDevJapan", repositoryID: 1127406466,
		spec: "D:/CC/Tools/todo/specs/3277-333069b6e17d489495d2e4e9d3b3d6d6.md", specSHA: "7d526ccfc04dae7df0b9a486a52e1feabaec566378cbfc093353b85684b94d2d"},
	{task: 3549, project: "x-bookmarks", root: "D:/CC/Tools/x-bookmarks",
		spec: "D:/CC/Tools/todo/specs/3549-07bd3a0de9e646a9b8d6ef08d8db8bca.md", specSHA: "846b43b64da3468a05aab4f40ecd6c8542effede35a61b195c8e4e6e4c879968"},
	{task: 3623, project: "local-ai-console", root: resume3638Root, repository: "autodevjapan/cc-pc-tools-rs", owner: "AutoDevJapan", repositoryID: 1406818713,
		spec: "D:/CC/Tools/todo/specs/3623-3cf655260f2d430199b4fcab07bb872c.md", specSHA: "f2a95ceba588d7404d3d67aba234927d7fbdd4f57719713fc26ac109150df1ac"},
}

func requested3650Product(task int) (currentProduct, bool) {
	for _, p := range requested3650Products {
		if p.task == task {
			return p, true
		}
	}
	return currentProduct{}, false
}

type requestedResumeAccess struct {
	source   func(object) bool
	plain    func(string, bool) bool
	denied   func(string) bool
	identity func(currentProduct) bool
	origin   func(string, string) bool
	spec     func(object, string, int, string, string, string, func(string) bool) bool
	original func(object) bool
	x        func(object) bool
	now      time.Time
}

func requested3650Access(liveIdentity bool) requestedResumeAccess {
	identity := func(p currentProduct) bool { return p.repositoryID > 0 && p.owner == "AutoDevJapan" }
	if liveIdentity {
		identity = currentGitHubIdentityVerified
	}
	return requestedResumeAccess{source: resume3650SourceQualified, plain: currentPlain, denied: legacyMatch, identity: identity,
		origin: sourceOriginVerified, spec: currentQualification, now: time.Now(),
		original: func(db object) bool {
			return verifyOriginalProductRecords(db, 1626, productIO{readObject, readBounded, exactPhysical, sourceOriginVerified, requested3650OriginalSessions, time.Now()}, object{"verified": false})["verified"] == true
		},
		x: func(db object) bool {
			const originalJob = "20261024-090002-x-bookmarks-2026-10-24"
			row := itemAt(db, 3549)
			job, err := readObject("D:/CC/Tools/codex-jobs/jobs/"+originalJob+"/job.json", 4<<20)
			if err != nil || job["id"] != originalJob || num(job["todo_id"]) != 3549 || job["project"] != "x-bookmarks" || !hasJob(row, originalJob) {
				return false
			}
			path := "D:/CC/Tools/x-bookmarks/x-bookmarks-daily.exe"
			if legacyMatch(path) || !currentPlain(path, false) {
				return false
			}
			b, err := readBounded(path, 8<<20)
			return err == nil && hashBytes(b) == "c2ac5bd3e456d37762b296afd61ca37e1176ce281e64771534ab212e5f90cb3a"
		},
	}
}

func requestedResumeQualified(db object, p currentProduct, a requestedResumeAccess) bool {
	if a.now.Before(time.Date(2026, 10, 24, 11, 26, 20, 672000000, time.UTC)) || !a.source(db) {
		return false
	}
	row := itemAt(db, p.task)
	if row == nil || row["stopped"] == true || row["owner_stopped"] == true || (row["status"] != "todo" && row["status"] != "doing" && row["status"] != "wait") ||
		a.denied(p.root) || !a.plain(p.root, true) {
		return false
	}
	// Establish ownership before opening the saved original instructions.
	if p.task == 3549 {
		if !a.x(db) {
			return false
		}
	} else if !a.origin(p.root, p.repository) || !a.identity(p) {
		return false
	}
	if !a.spec(db, p.project, p.task, p.root, p.spec, p.specSHA, a.denied) {
		return false
	}
	return p.task != 1626 || a.original(db)
}

func requestedResumeReceipt(db object, task int) object {
	failure := object{"verified": false}
	p, ok := requested3650Product(task)
	if !ok || !requestedResumeQualified(db, p, requested3650Access(true)) {
		return failure
	}
	rootSHA, ok := currentPathHash(p.root)
	if !ok {
		return failure
	}
	specSHA, ok := currentPathHash(p.spec)
	if !ok {
		return failure
	}
	return object{"domain": "current-product-succession-v1", "verified": true,
		"origin_todo_id": 3638, "actor_todo_id": 3650, "todo_id": task, "project": p.project,
		"registered_path_sha256": rootSHA, "spec_path_sha256": specSHA, "spec_sha256": p.specSHA,
		"source_kind": "native_user", "source_session": resume3650Native.Session,
		"source_event_sha256": resume3650Native.MessageSHA, "source_at": resume3650Native.At,
		"original_registry_recovered": false}
}

func requestedResumePathAllowed(path string) bool {
	// Select only the exact saved spec, source/workspace or operational working
	// location before any native or private record read. No X data grant.
	var candidate *currentProduct
	for _, p := range requested3650Products {
		workspaceTask, workspace := currentSubworkTaskInWorkspace(path, p.root+"-wt")
		if same(path, p.spec) || same(path, p.root) || (p.task != 3549 && within(p.root, path)) || (workspace && workspaceTask == p.task) {
			copy := p
			candidate = &copy
			break
		}
	}
	if candidate == nil {
		return false
	}
	db, err := readObject(productStore, 64<<20)
	p := *candidate
	if err != nil || !requestedResumeQualified(db, p, requested3650Access(false)) || legacyMatch(path) {
		return false
	}
	if same(path, p.spec) || same(path, p.root) {
		return true
	}
	if p.task != 3549 {
		return currentProductPath(path, p.root, p.repository, strconv.Itoa(p.task), legacyMatch)
	}
	// Only the normal independent non-Git scheduler workspace. Operational
	// files in x-bookmarks itself remain outside this qualification.
	row := itemAt(db, p.task)
	workspace := currentSubworkWorkspacePath(p.root, p.task, str(row["title"]))
	if saved := str(obj(row["scheduler"])["worktree"]); saved != "" && !same(saved, workspace) {
		return false
	}
	if !same(path, workspace) && !within(workspace, path) {
		return false
	}
	if _, e := os.Lstat(workspace); os.IsNotExist(e) {
		return same(path, workspace) && currentPlain(filepath.Dir(p.root), true)
	}
	return currentNoReparse(path) && exactPhysical(path)
}
