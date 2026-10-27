package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// Exact safe-delete v3 identity projection, including its field order for the
// producer's checksum. The checksum detects damage, not hostile-user forgery.
// Native identity and the independent scheduler job are checked separately.
type ownerOwnedAncestor struct {
	Path   string `json:"path"`
	Volume uint32 `json:"volume"`
	ID     uint64 `json:"file_id"`
	Birth  uint64 `json:"birth"`
}
type ownerOwnedIdentity struct {
	Version      int                  `json:"version"`
	ID           string               `json:"id"`
	Root         string               `json:"root"`
	Path         string               `json:"path"`
	Volume       uint32               `json:"volume"`
	FileID       uint64               `json:"file_id"`
	Birth        uint64               `json:"birth,omitempty"`
	Created      string               `json:"created_at"`
	Project      string               `json:"project"`
	Content      string               `json:"content"`
	ProducerJob  string               `json:"producer_job,omitempty"`
	ProducerTodo uint64               `json:"producer_todo,omitempty"`
	ProducerKind string               `json:"producer_kind,omitempty"`
	Ancestors    []ownerOwnedAncestor `json:"ancestors,omitempty"`
}
type ownerOwnedReceipt struct {
	ownerOwnedIdentity
	Integrity string `json:"integrity"`
	State     string `json:"state"`
}

var ownerOwnedName = regexp.MustCompile(`^codex-owned-([a-f0-9]{32})$`)
var ownerOwnedJob = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)

func ownerRuleOwnedTemporary(ctx context.Context, path string, projects object, access ownerSourceAccess, config ownerRuleConfig) bool {
	return ownerRuleOwnedAt(ctx, path, "D:/CC/Tools/safe-delete/receipts-go", "D:/CC/Tools/codex-jobs/jobs", projects, access, config)
}

func ownerRuleOwnedAt(ctx context.Context, path, store, jobs string, projects object, access ownerSourceAccess, config ownerRuleConfig) bool {
	root := path
	for depth := 0; depth < 64 && ctx.Err() == nil; depth++ {
		if match := ownerOwnedName.FindStringSubmatch(filepath.Base(root)); match != nil {
			if !currentPlain(root, true) {
				return false
			}
			record := filepath.Join(store, match[1]+".json")
			if !currentPlain(record, false) {
				return false
			}
			b, err := readBounded(record, 8192)
			if err != nil {
				return false
			}
			if _, ok := conversationJSON(b); !ok {
				return false
			}
			var r ownerOwnedReceipt
			if json.Unmarshal(b, &r) != nil || r.Version != 3 || r.ID != match[1] || r.State != "owned" ||
				!same(r.Path, root) || !same(filepath.Join(r.Root, "codex-owned-"+r.ID), root) ||
				r.ProducerKind != "scheduler" || r.ProducerTodo == 0 || !ownerOwnedJob.MatchString(r.ProducerJob) ||
				!strings.HasSuffix(r.ProducerJob, "-todo-"+strconv.FormatUint(r.ProducerTodo, 10)) {
				return false
			}
			identity, err := json.Marshal(r.ownerOwnedIdentity)
			if err != nil || hashBytes(identity) != r.Integrity || len(r.Ancestors) == 0 {
				return false
			}
			if access.membership(r.Project, "") == "client_project" {
				return false
			}
			if !ownerOwnedNative(root, r.Volume, r.FileID, r.Birth) {
				return false
			}
			ancestor := r.Root
			for _, a := range r.Ancestors {
				if !same(a.Path, ancestor) || !ownerOwnedNative(a.Path, a.Volume, a.ID, a.Birth) {
					return false
				}
				ancestor = filepath.Dir(ancestor)
			}
			if !same(ancestor, filepath.VolumeName(root)+string(filepath.Separator)) || !same(r.Ancestors[len(r.Ancestors)-1].Path, ancestor) {
				return false
			}
			jobPath := filepath.Join(jobs, r.ProducerJob, "job.json")
			if !currentPlain(jobPath, false) {
				return false
			}
			jobBytes, err := readBounded(jobPath, 4<<20)
			if err != nil {
				return false
			}
			if _, ok := conversationJSON(jobBytes); !ok {
				return false
			}
			// Decode only producer metadata; prompts, outputs and credentials are
			// neither interpreted as authority nor returned to the caller.
			var job struct {
				ID    string `json:"id"`
				Todo  uint64 `json:"todo_id"`
				CWD   string `json:"cwd"`
				Owner struct {
					Repository string `json:"repository"`
					Worktree   string `json:"worktree"`
				} `json:"work_owner"`
			}
			if json.Unmarshal(jobBytes, &job) != nil || job.ID != r.ProducerJob || job.Todo != r.ProducerTodo ||
				!filepath.IsAbs(job.CWD) || !same(job.CWD, job.Owner.Worktree) {
				return false
			}
			repository, valid := ownerOriginRepository("https://github.com/" + job.Owner.Repository)
			if !valid {
				return false
			}
			origin, ok := access.git(job.CWD, "remote", "get-url", "origin")
			actual, self := ownerOriginRepository(origin)
			if !ok || !self || !strings.EqualFold(repository, actual) {
				return false
			}
			if !ownerRuleWith(ctx, job.CWD, projects, access, config, nil).AccessAllowed {
				return false
			}
			// Classification does not retire the producer or authorize deletion.
			return ctx.Err() == nil && ownerOwnedNative(root, r.Volume, r.FileID, r.Birth)
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return false
}

func ownerOwnedNative(path string, volume uint32, id, birth uint64) bool {
	if !currentPlain(path, true) {
		return false
	}
	i, err := owned2736Identity(path)
	return err == nil && i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 &&
		i.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 && i.VolumeSerialNumber == volume &&
		uint64(i.FileIndexHigh)<<32|uint64(i.FileIndexLow) == id &&
		uint64(i.CreationTime.HighDateTime)<<32|uint64(i.CreationTime.LowDateTime) == birth
}
