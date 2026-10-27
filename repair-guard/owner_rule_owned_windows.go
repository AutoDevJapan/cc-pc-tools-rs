package main

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"io"
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

func ownerRuleOwnedTemporary(ctx context.Context, path string, projects object, access ownerSourceAccess, config ownerRuleConfig) ownerPathVerdict {
	return ownerRuleOwnedVerdictAt(ctx, path, "D:/CC/Tools/safe-delete/receipts-go", "D:/CC/Tools/codex-jobs/jobs", projects, access, config)
}

func ownerRuleOwnedAt(ctx context.Context, path, store, jobs string, projects object, access ownerSourceAccess, config ownerRuleConfig) bool {
	return ownerRuleOwnedVerdictAt(ctx, path, store, jobs, projects, access, config).AccessAllowed
}
func ownerRuleOwnedVerdictAt(ctx context.Context, path, store, jobs string, projects object, access ownerSourceAccess, config ownerRuleConfig) ownerPathVerdict {
	root := path
	for depth := 0; depth < 64 && ctx.Err() == nil; depth++ {
		if match := ownerOwnedName.FindStringSubmatch(filepath.Base(root)); match != nil {
			if !currentPlain(root, true) {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			record := filepath.Join(store, match[1]+".json")
			if !currentPlain(record, false) {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			b, err := readBounded(record, 8192)
			if err != nil {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			if _, ok := conversationJSON(b); !ok {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			var r ownerOwnedReceipt
			if json.Unmarshal(b, &r) != nil || r.Version != 3 || r.ID != match[1] || r.State != "owned" ||
				!same(r.Path, root) || !same(filepath.Join(r.Root, "codex-owned-"+r.ID), root) ||
				r.ProducerKind != "scheduler" || r.ProducerTodo == 0 || !ownerOwnedJob.MatchString(r.ProducerJob) ||
				!strings.HasSuffix(r.ProducerJob, "-todo-"+strconv.FormatUint(r.ProducerTodo, 10)) {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			identity, err := json.Marshal(r.ownerOwnedIdentity)
			if err != nil || hashBytes(identity) != r.Integrity || len(r.Ancestors) == 0 {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			if access.membership(r.Project, "") == "client_project" {
				return ownerRuleProtected("client_record_producer")
			}
			if !ownerOwnedNative(root, r.Volume, r.FileID, r.Birth) {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			ancestor := r.Root
			for _, a := range r.Ancestors {
				if !same(a.Path, ancestor) || !ownerOwnedNative(a.Path, a.Volume, a.ID, a.Birth) {
					return ownerRuleUnknown("owned_resource_metadata_unavailable")
				}
				ancestor = filepath.Dir(ancestor)
			}
			if !same(ancestor, filepath.VolumeName(root)+string(filepath.Separator)) || !same(r.Ancestors[len(r.Ancestors)-1].Path, ancestor) {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			jobPath := filepath.Join(jobs, r.ProducerJob, "job.json")
			if !currentPlain(jobPath, false) {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			job, ok := ownerReadMetadataFile(ctx, jobPath, func(input io.Reader) (ownerRecordMetadata, bool) {
				d := jsontext.NewDecoder(ownerDeadlineReader{ctx, input})
				m, ok := ownerReadRecordObject(ctx, d, "record")
				_, err := d.ReadToken()
				return m, ok && err == io.EOF
			})
			if !ok || job.id != r.ProducerJob || job.todo != strconv.FormatUint(r.ProducerTodo, 10) || !filepath.IsAbs(job.cwd) || !same(job.cwd, job.worktree) {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			if v := ownerRuleRecordOrigin(ctx, job, projects, access, config); !v.AccessAllowed {
				return v
			}
			repository, valid := ownerOriginRepository("https://github.com/" + job.repository)
			if !valid {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			origin, ok := access.git(job.cwd, "remote", "get-url", "origin")
			actual, self := ownerOriginRepository(origin)
			if !ok || !self || !strings.EqualFold(repository, actual) {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			if !ownerRuleWith(ctx, job.cwd, projects, access, config, nil).AccessAllowed {
				return ownerRuleUnknown("owned_resource_metadata_unavailable")
			}
			// Classification does not retire the producer or authorize deletion.
			if ctx.Err() == nil && ownerOwnedNative(root, r.Volume, r.FileID, r.Birth) {
				return ownerRuleClear("self_owned_temporary_resource")
			}
			return ownerRuleUnknown("owned_resource_metadata_unavailable")
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return ownerRuleProtected("owner_rule_unattributed_location")
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
