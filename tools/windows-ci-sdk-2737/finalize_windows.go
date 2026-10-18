//go:build windows && amd64

package sdkprep

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FinalizeVerifiedGuestCLI only recovers this task's single fully measured SDK
// guest. It never installs, starts/restarts a VM, or accepts a path/command.
func FinalizeVerifiedGuestCLI(worktree string) (TaskEvidence, error) {
	if err := verifyTaskWorktree(worktree); err != nil {
		return TaskEvidence{}, err
	}
	entries, err := os.ReadDir(worktree)
	if err != nil {
		return TaskEvidence{}, err
	}
	var root OwnedRoot
	var prep PreparationReceipt
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "codex-owned-") {
			continue
		}
		candidate := OwnedRoot{ID: strings.TrimPrefix(entry.Name(), "codex-owned-"), Path: filepath.Join(worktree, entry.Name())}
		data, err := os.ReadFile(filepath.Join(candidate.Path, "sdk-operations.private.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return TaskEvidence{}, err
		}
		var saved struct {
			Receipt PreparationReceipt `json:"receipt"`
			Failure string             `json:"failure"`
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if len(data) > 64<<10 || dec.Decode(&saved) != nil || dec.Decode(new(json.RawMessage)) != io.EOF {
			return TaskEvidence{}, errors.New("SDK preparation journal rejected")
		}
		if saved.Failure != "<nil>" || validateSDKResults(saved.Receipt.Results) != nil {
			continue
		}
		if _, err := os.Lstat(filepath.Join(candidate.Path, "windows-ci-sdk-evidence.private.json")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return TaskEvidence{}, err
		}
		if err := VerifyOwnedRoot(candidate, worktree); err != nil {
			return TaskEvidence{}, err
		}
		if root.ID != "" {
			return TaskEvidence{}, errors.New("multiple measured SDK guests require separate recovery")
		}
		root, prep = candidate, saved.Receipt
	}
	if root.ID == "" {
		return TaskEvidence{}, errors.New("no fully verified SDK guest to finalize")
	}
	vm, err := readVMReceipt(root.Path, prep.Instance)
	if err != nil {
		return TaskEvidence{}, err
	}
	iso, isoSize, isoHash, ready, err := loadPreparedEvaluationISO(root, worktree)
	if err != nil || !ready {
		return TaskEvidence{}, errors.New("SDK guest public ISO evidence missing")
	}
	helpers, err := filepath.Glob(filepath.Join(root.Path, "windows-ci-sdk-agent-*.exe"))
	if err != nil || len(helpers) != 1 {
		return TaskEvidence{}, errors.New("SDK guest helper source receipt ambiguous")
	}
	helper := helpers[0]
	revision := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(helper), "windows-ci-sdk-agent-"), ".exe")
	if len(revision) != 40 {
		return TaskEvidence{}, errors.New("SDK helper source revision missing")
	}
	helperMeasure, err := hashAndSize(helper)
	if err != nil {
		return TaskEvidence{}, err
	}
	helperSize, helperHash, err := splitSizeHash(helperMeasure)
	if err != nil {
		return TaskEvidence{}, err
	}
	seedMeasure, err := hashAndSize(vm.SeedISO)
	if err != nil {
		return TaskEvidence{}, err
	}
	seedSize, seedHash, err := splitSizeHash(seedMeasure)
	if err != nil {
		return TaskEvidence{}, err
	}
	recovery, err := ShutdownRemoveOwnedVM(root, worktree, prep.Instance)
	if err != nil {
		return TaskEvidence{}, err
	}
	if recovery.ShutdownMode != "guest-integration-shutdown" {
		return TaskEvidence{}, errors.New("SDK template requires a measured guest OS shutdown")
	}
	imageMeasure, err := hashAndSize(vm.VHD)
	if err != nil {
		return TaskEvidence{}, err
	}
	imageSize, imageHash, err := splitSizeHash(imageMeasure)
	if err != nil || imageSize != recovery.VHDSize || !strings.EqualFold(imageHash, recovery.VHDSHA256) {
		return TaskEvidence{}, errors.New("recovered SDK image hash disagrees with original recovery")
	}
	evidence := TaskEvidence{Schema: "todo-2783-windows-sdk-guest-v1", SourceCommit: revision, OwnedRootID: root.ID, OwnedRootPath: root.Path, Instance: prep.Instance, ISOPath: iso, ISOSize: isoSize, ISOSHA256: isoHash, HelperPath: helper, HelperSize: helperSize, HelperSHA256: helperHash, SeedISOPath: vm.SeedISO, SeedISOSize: seedSize, SeedISOSHA256: seedHash, GuestIPv4: prep.GuestIPv4, ServerCertSHA256: prep.ServerSHA256, VersionChecks: prep.Results, VMRecovery: recovery, TemplatePath: vm.VHD, TemplateSize: recovery.VHDSize, TemplateSHA256: recovery.VHDSHA256, ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := writeEvidence(root.Path, evidence); err != nil {
		return TaskEvidence{}, err
	}
	return evidence, nil
}
