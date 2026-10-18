//go:build windows && amd64

package sdkprep

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TaskEvidence struct {
	Schema           string          `json:"schema"`
	SourceCommit     string          `json:"source_commit"`
	OwnedRootID      string          `json:"owned_root_id"`
	OwnedRootPath    string          `json:"owned_root_path"`
	Instance         string          `json:"instance"`
	ISOPath          string          `json:"iso_path"`
	ISOSize          int64           `json:"iso_size"`
	ISOSHA256        string          `json:"iso_sha256"`
	HelperPath       string          `json:"helper_path"`
	HelperSize       int64           `json:"helper_size"`
	HelperSHA256     string          `json:"helper_sha256"`
	SeedISOPath      string          `json:"seed_iso_path"`
	SeedISOSize      int64           `json:"seed_iso_size"`
	SeedISOSHA256    string          `json:"seed_iso_sha256"`
	GuestIPv4        string          `json:"guest_ipv4"`
	ServerCertSHA256 string          `json:"server_certificate_sha256"`
	VersionChecks    []Result        `json:"version_checks"`
	VMRecovery       RecoveryReceipt `json:"vm_recovery"`
	TemplatePath     string          `json:"template_path"`
	TemplateSize     int64           `json:"template_size"`
	TemplateSHA256   string          `json:"template_sha256"`
	ObservedAt       string          `json:"observed_at"`
}

func RunSDKPreparation(ctx context.Context, root OwnedRoot, worktree, modulePath, sourceCommit string) (TaskEvidence, error) {
	var evidence TaskEvidence
	if err := VerifyOwnedRoot(root, worktree); err != nil {
		return evidence, err
	}
	if len(sourceCommit) != 40 {
		return evidence, errors.New("source commit must be a full SHA-1")
	}
	for _, r := range sourceCommit {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return evidence, errors.New("source commit format rejected")
		}
	}
	instance, err := newInstance()
	if err != nil {
		return evidence, err
	}
	stage := "guest-helper-build"
	failed := true
	defer func() {
		if failed {
			writePrivateStageFailure(root.Path, instance, stage)
		}
	}()
	helperPath, helperCombined, err := BuildGuestAgent(root, worktree, modulePath, sourceCommit)
	if err != nil {
		return evidence, err
	}
	helperSize, helperHash, err := splitSizeHash(helperCombined)
	if err != nil {
		return evidence, errors.New("guest helper build evidence rejected")
	}
	stage = "official-evaluation-iso-acquire-or-reuse"
	isoPath, isoSize, isoHash, prepared, err := loadPreparedEvaluationISO(root, worktree)
	if err != nil {
		return evidence, err
	}
	if !prepared {
		isoPath, isoSize, isoHash, err = DownloadOfficialEvaluationISO(ctx, root, worktree)
		if err != nil {
			return evidence, err
		}
	}
	stage = "public-seed-generation"
	clientIdentity, err := NewHostIdentity(instance)
	if err != nil {
		return evidence, err
	}
	defer clearIdentity(&clientIdentity)
	seedISO, err := BuildGuestSeedISO(root, worktree, instance, helperPath, clientIdentity)
	if err != nil {
		return evidence, err
	}
	seedMeasure, err := hashAndSize(seedISO)
	if err != nil {
		return evidence, errors.New("seed ISO hash measurement failed")
	}
	seedSize, seedHash, err := splitSizeHash(seedMeasure)
	if err != nil {
		return evidence, errors.New("seed ISO hash measurement failed")
	}
	stage = "normal-setup-hyperv-provision"
	_, err = ProvisionNewGuest(root, worktree, isoPath, seedISO, instance)
	if err != nil {
		if _, readErr := readVMReceipt(root.Path, instance); readErr == nil {
			stage = "normal-setup-hyperv-recovery"
			if _, recoveryErr := ShutdownRemoveOwnedVM(root, worktree, instance); recoveryErr != nil {
				stage = "normal-setup-hyperv-recovery-failed"
			}
		}
		return evidence, err
	}
	stage = "guest-sdk-install-and-version-check"
	receipt, err := prepareProvisionedGuestObserved(ctx, instance, clientIdentity, func(result Result) {
		f, err := os.OpenFile(filepath.Join(root.Path, "host-sdk-progress.private.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		_ = json.NewEncoder(f).Encode(struct {
			ObservedAt string `json:"observed_at"`
			Result     Result `json:"result"`
		}{ObservedAt: time.Now().UTC().Format(time.RFC3339), Result: result})
		_ = f.Sync()
	})
	resultRecord, _ := json.Marshal(struct {
		Receipt PreparationReceipt `json:"receipt"`
		Failure string             `json:"failure"`
	}{Receipt: receipt, Failure: fmt.Sprint(err)})
	_ = writeOwnedFile(filepath.Join(root.Path, "sdk-operations.private.json"), resultRecord)
	if err != nil {
		stage = "guest-sdk-failed-vm-recovery"
		_, _ = ShutdownRemoveOwnedVM(root, worktree, instance)
		return evidence, err
	}
	if err := validateSDKResults(receipt.Results); err != nil {
		stage = "guest-sdk-result-validation-vm-recovery"
		_, _ = ShutdownRemoveOwnedVM(root, worktree, instance)
		return evidence, err
	}
	stage = "vm-stop-removal-and-vhd-detach"
	recovery, err := ShutdownRemoveOwnedVM(root, worktree, instance)
	if err != nil {
		return evidence, err
	}
	templatePath := filepath.Join(root.Path, "guest.vhdx")
	measure, err := hashAndSize(templatePath)
	if err != nil {
		return evidence, errors.New("template VHDX measurement failed")
	}
	measuredSize, measuredHash, err := splitSizeHash(measure)
	if err != nil || measuredSize != recovery.VHDSize || !strings.EqualFold(measuredHash, recovery.VHDSHA256) {
		return evidence, errors.New("template VHDX measurement disagrees with recovery receipt")
	}
	evidence = TaskEvidence{
		Schema: "todo-2783-windows-sdk-guest-v1", SourceCommit: sourceCommit, OwnedRootID: root.ID, OwnedRootPath: root.Path,
		Instance: instance, ISOPath: isoPath, ISOSize: isoSize, ISOSHA256: isoHash,
		HelperPath: helperPath, HelperSize: helperSize, HelperSHA256: helperHash,
		SeedISOPath: seedISO, SeedISOSize: seedSize, SeedISOSHA256: seedHash,
		GuestIPv4: receipt.GuestIPv4, ServerCertSHA256: receipt.ServerSHA256, VersionChecks: receipt.Results,
		VMRecovery: recovery, TemplatePath: templatePath, TemplateSize: measuredSize, TemplateSHA256: measuredHash,
		ObservedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeEvidence(root.Path, evidence); err != nil {
		return TaskEvidence{}, err
	}
	failed = false
	return evidence, nil
}

func newInstance() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errors.New("fresh guest instance generation failed")
	}
	return "todo2783-" + hex.EncodeToString(nonce[:]), nil
}

func validateSDKResults(results []Result) error {
	wantPrepare := map[PackageID]bool{GitForWindows: false, PowerShell7: false, RustToolchain: false, MSVCBuildTools: false, Sccache: false}
	wantVerify := map[PackageID]bool{GitForWindows: false, PowerShell7: false, RustToolchain: false, MSVCBuildTools: false, WindowsSDK: false, Sccache: false}
	for _, result := range results {
		if !result.OK {
			return errors.New("guest reported an unsuccessful SDK step")
		}
		switch result.Operation {
		case OpPrepare:
			if _, ok := wantPrepare[result.Package]; !ok || result.Stage != "prepared" {
				return errors.New("unexpected SDK prepare receipt")
			}
			wantPrepare[result.Package] = true
		case OpVersions:
			if _, ok := wantVerify[result.Package]; !ok || result.Stage != "version-confirmed" || result.Version == "" || len(result.Version) > 256 || strings.ContainsAny(result.Version, "\r\n\x00") || !validDigest(result.VersionProofSHA256) {
				return errors.New("unexpected SDK version receipt")
			}
			wantVerify[result.Package] = true
		default:
			return errors.New("unexpected guest operation in SDK evidence")
		}
	}
	for _, ok := range wantPrepare {
		if !ok {
			return errors.New("SDK preparation receipt missing")
		}
	}
	for _, ok := range wantVerify {
		if !ok {
			return errors.New("SDK version receipt missing")
		}
	}
	if len(results) != len(wantPrepare)+len(wantVerify) {
		return errors.New("duplicate SDK operation receipt")
	}
	return nil
}

func splitSizeHash(value string) (int64, string, error) {
	separator := strings.IndexByte(value, ':')
	if separator <= 0 || len(value)-separator-1 != 64 {
		return 0, "", errors.New("size/hash encoding invalid")
	}
	var size int64
	if _, err := fmt.Sscan(value[:separator], &size); err != nil || size <= 0 {
		return 0, "", errors.New("artifact size invalid")
	}
	hash := value[separator+1:]
	if !validDigest(hash) {
		return 0, "", errors.New("artifact digest invalid")
	}
	return size, hash, nil
}

func writeEvidence(root string, evidence TaskEvidence) error {
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return errors.New("SDK evidence encoding failed")
	}
	return writeOwnedFile(filepath.Join(root, "windows-ci-sdk-evidence.private.json"), data)
}

func writePrivateStageFailure(root, instance, stage string) {
	data, err := json.Marshal(map[string]string{"instance": instance, "stage": stage, "result": "failed-or-incomplete"})
	if err == nil {
		_ = writeOwnedFile(filepath.Join(root, "windows-ci-sdk-failure-"+instance+".private.json"), data)
	}
}
