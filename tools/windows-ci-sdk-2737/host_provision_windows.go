//go:build windows && amd64

package sdkprep

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	safeDeleteExecutable  = "D:/CC/Tools/safe-delete/safe-delete.exe"
	clientGuardExecutable = "D:/CC/Tools/client-records/scrub391.exe"
	adminBrokerScript     = "D:/CC/Tools/admin-broker/run-as-admin.ps1"
	officialServerISOURL  = "https://aka.ms/WinServ2025iso-enus"
)

type OwnedRoot struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}
type VMReceipt struct {
	Instance      string `json:"instance"`
	VMID          string `json:"vm_id"`
	Name          string `json:"name"`
	Root          string `json:"root"`
	VHD           string `json:"vhd"`
	ISO           string `json:"windows_iso"`
	SeedISO       string `json:"seed_iso"`
	COMPipe       string `json:"pipe"`
	Generation    int    `json:"generation"`
	CPUs          int    `json:"cpu_count"`
	StartupMemory int64  `json:"startup_memory_bytes"`
	DynamicMemory bool   `json:"dynamic_memory"`
	NICs          int    `json:"network_adapters"`
	GPUs          int    `json:"gpu_adapters"`
	State         string `json:"state"`
}
type vmProvisionConfig struct {
	Root     string `json:"root"`
	Instance string `json:"instance"`
	ISO      string `json:"iso"`
	SeedISO  string `json:"seedISO"`
}
type RecoveryReceipt struct {
	Instance       string `json:"instance"`
	VMID           string `json:"vm_id"`
	VMState        string `json:"vm_state"`
	MemoryAssigned int64  `json:"memory_assigned"`
	VMRemoved      bool   `json:"vm_removed"`
	VHD            string `json:"vhd"`
	VHDDetached    bool   `json:"vhd_detached"`
	VHDSize        int64  `json:"vhd_size"`
	VHDSHA256      string `json:"vhd_sha256"`
}

type evaluationISOValidationReceipt struct {
	Source      string `json:"source"`
	ISOSize     int64  `json:"iso_size"`
	ISOSHA256   string `json:"iso_sha256"`
	SetupStatus string `json:"setup_signature"`
	SetupSigner string `json:"setup_signer"`
	Edition     string `json:"edition"`
	ValidatedAt string `json:"validated_at"`
}

func CreateOwnedRoot(ctx context.Context, worktree string) (OwnedRoot, error) {
	base, err := filepath.Abs(worktree)
	if err != nil {
		return OwnedRoot{}, errors.New("worktree path unavailable")
	}
	info, err := os.Lstat(base)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return OwnedRoot{}, errors.New("worktree root is not a real directory")
	}
	if err := verifyUnprotectedPath(base); err != nil {
		return OwnedRoot{}, err
	}
	cmd := exec.CommandContext(ctx, safeDeleteExecutable, "create", "--project", "cc-pc-tools-rs", "--content", "windows-ci-sdk-2737-public-sdk-template", "--root", base)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, io.Discard
	if err := cmd.Run(); err != nil {
		return OwnedRoot{}, errors.New("safe-delete owned root creation failed")
	}
	var full struct {
		ID    string `json:"id"`
		Path  string `json:"path"`
		State string `json:"state"`
	}
	if json.Unmarshal(stdout.Bytes(), &full) != nil || full.State != "owned" {
		return OwnedRoot{}, errors.New("safe-delete ownership state rejected")
	}
	root := OwnedRoot{ID: full.ID, Path: full.Path}
	if err := VerifyOwnedRoot(root, base); err != nil {
		return OwnedRoot{}, err
	}
	return root, nil
}

func VerifyOwnedRoot(root OwnedRoot, worktree string) error {
	if len(root.ID) != 32 {
		return errors.New("owned root ID rejected")
	}
	for _, r := range root.ID {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return errors.New("owned root ID rejected")
		}
	}
	base, err := filepath.Abs(worktree)
	if err != nil {
		return errors.New("worktree path unavailable")
	}
	path, err := filepath.Abs(root.Path)
	if err != nil {
		return errors.New("owned root path unavailable")
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return errors.New("owned root escaped worktree")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("owned root is not a real directory")
	}
	return verifyUnprotectedPath(path)
}

func findPreparedOwnedRoot(worktree string) (OwnedRoot, bool, error) {
	entries, err := os.ReadDir(worktree)
	if err != nil {
		return OwnedRoot{}, false, errors.New("worktree entries unavailable")
	}
	var found OwnedRoot
	var foundDigest string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "codex-owned-") {
			continue
		}
		path := filepath.Join(worktree, entry.Name())
		iso := filepath.Join(path, "windows-server-2025-eval.iso")
		partial := filepath.Join(path, "windows-server-2025-eval.iso.partial")
		if _, err := os.Lstat(partial); err == nil {
			return OwnedRoot{}, false, errors.New("incomplete official ISO in owned root; preserve for diagnosis")
		} else if !errors.Is(err, os.ErrNotExist) {
			return OwnedRoot{}, false, errors.New("owned ISO partial status unavailable")
		}
		if _, err := os.Lstat(iso); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return OwnedRoot{}, false, errors.New("owned ISO status unavailable")
		}
		id := strings.TrimPrefix(entry.Name(), "codex-owned-")
		candidate := OwnedRoot{ID: id, Path: path}
		if err := VerifyOwnedRoot(candidate, worktree); err != nil {
			return OwnedRoot{}, false, err
		}
		_, _, digest, prepared, err := loadPreparedEvaluationISO(candidate, worktree)
		if err != nil || !prepared {
			if err != nil {
				return OwnedRoot{}, false, err
			}
			return OwnedRoot{}, false, errors.New("official ISO lacks completed signature validation")
		}
		if found.ID != "" {
			if digest != foundDigest {
				return OwnedRoot{}, false, errors.New("conflicting prepared official ISO digests")
			}
			continue
		}
		found = candidate
		foundDigest = digest
	}
	if found.ID == "" {
		return OwnedRoot{}, false, nil
	}
	return found, true, nil
}

func loadPreparedEvaluationISO(root OwnedRoot, worktree string) (string, int64, string, bool, error) {
	isoPath := filepath.Join(root.Path, "windows-server-2025-eval.iso")
	measurePath := filepath.Join(root.Path, "windows-server-2025-eval.measure.private.txt")
	receiptPath := filepath.Join(root.Path, "windows-server-2025-eval-validation.private.json")
	paths := []string{isoPath, measurePath, receiptPath}
	present := 0
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			present++
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", 0, "", false, errors.New("prepared ISO evidence status unavailable")
		}
	}
	if present == 0 {
		return "", 0, "", false, nil
	}
	if present != len(paths) {
		return "", 0, "", false, errors.New("prepared ISO evidence is incomplete")
	}
	if err := VerifyOwnedRoot(root, worktree); err != nil {
		return "", 0, "", false, err
	}
	measure, err := os.ReadFile(measurePath)
	if err != nil || len(measure) > 256 {
		return "", 0, "", false, errors.New("prepared ISO measurement unavailable")
	}
	info, err := os.Stat(isoPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, "", false, errors.New("prepared ISO file unavailable")
	}
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		return "", 0, "", false, errors.New("prepared ISO validation receipt unavailable")
	}
	size, digest, err := validatePreparedISOEvidence(measure, receipt, info.Size())
	if err != nil {
		return "", 0, "", false, err
	}
	return isoPath, size, digest, true, nil
}

func validatePreparedISOEvidence(measure, receipt []byte, actualSize int64) (int64, string, error) {
	if len(measure) > 256 {
		return 0, "", errors.New("prepared ISO measurement exceeds bound")
	}
	fields := strings.Fields(string(measure))
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "size=") || !strings.HasPrefix(fields[1], "sha256=") {
		return 0, "", errors.New("prepared ISO measurement malformed")
	}
	size, err := strconv.ParseInt(strings.TrimPrefix(fields[0], "size="), 10, 64)
	if err != nil || size <= 0 || size > 8<<30 || size != actualSize {
		return 0, "", errors.New("prepared ISO size rejected")
	}
	digest := strings.ToLower(strings.TrimPrefix(fields[1], "sha256="))
	if !validDigest(digest) {
		return 0, "", errors.New("prepared ISO digest rejected")
	}
	receipt = bytes.TrimPrefix(receipt, []byte{0xef, 0xbb, 0xbf})
	decoder := json.NewDecoder(bytes.NewReader(receipt))
	decoder.DisallowUnknownFields()
	var validation evaluationISOValidationReceipt
	if decoder.Decode(&validation) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF || validation.Source != "Microsoft Evaluation Center HTTPS" || validation.ISOSize != size || !strings.EqualFold(validation.ISOSHA256, digest) || validation.SetupStatus != "Valid" || validation.SetupSigner != "Microsoft Corporation" || validation.Edition != "Windows Server 2025 Standard Evaluation" {
		return 0, "", errors.New("prepared ISO validation receipt rejected")
	}
	return size, digest, nil
}

func verifyUnprotectedPath(path string) error {
	cmd := exec.Command(clientGuardExecutable, "--guard-path", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("owned root guard check failed")
	}
	var guard struct {
		OK                       bool `json:"ok"`
		ClientResource           bool `json:"client_resource"`
		ClassificationUnverified bool `json:"classification_unverified"`
	}
	if json.Unmarshal(output.Bytes(), &guard) != nil || !guard.OK || guard.ClientResource || guard.ClassificationUnverified {
		return errors.New("work path is protected or unverified")
	}
	return nil
}

func DownloadOfficialEvaluationISO(ctx context.Context, root OwnedRoot, worktree string) (string, int64, string, error) {
	if err := VerifyOwnedRoot(root, worktree); err != nil {
		return "", 0, "", err
	}
	client := &http.Client{Timeout: 2 * time.Hour, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 6 || req.URL.Scheme != "https" || !allowedHost(req.URL.Hostname()) {
			return errors.New("official ISO redirect rejected")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, officialServerISOURL, nil)
	if err != nil {
		return "", 0, "", errors.New("official ISO request invalid")
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, "", fmt.Errorf("official ISO download failed (%s)", classifyNetworkFailure(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request == nil || resp.Request.URL.Scheme != "https" || !allowedHost(resp.Request.URL.Hostname()) {
		return "", 0, "", errors.New("official ISO response origin rejected")
	}
	if resp.ContentLength > 8<<30 {
		return "", 0, "", errors.New("official ISO exceeds size bound")
	}
	destination := filepath.Join(root.Path, "windows-server-2025-eval.iso")
	tmp := destination + ".partial"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, "", errors.New("official ISO staging file creation failed")
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 8<<30))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n == 0 || n >= 8<<30 || (resp.ContentLength >= 0 && n != resp.ContentLength) {
		return "", n, "", errors.New("official ISO download size check failed")
	}
	if err := os.Rename(tmp, destination); err != nil {
		return "", n, "", errors.New("official ISO finalize failed")
	}
	return destination, n, hex.EncodeToString(h.Sum(nil)), nil
}

func classifyNetworkFailure(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "name-resolution"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		var opErr *net.OpError
		if errors.As(urlErr.Err, &opErr) && opErr.Timeout() {
			return "timeout"
		}
		text := strings.ToLower(urlErr.Err.Error())
		for _, item := range []struct{ fragment, category string }{
			{"proxyconnect", "proxy-connect"},
			{"connection refused", "connection-refused"},
			{"i/o timeout", "timeout"},
			{"deadline exceeded", "timeout"},
			{"x509:", "tls-verification"},
			{"certificate", "tls-verification"},
			{"unexpected eof", "truncated-response"},
		} {
			if strings.Contains(text, item.fragment) {
				return item.category
			}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "network-or-protocol"
}

func BuildGuestAgent(root OwnedRoot, worktree, modulePath, revision string) (string, string, error) {
	if err := VerifyOwnedRoot(root, worktree); err != nil {
		return "", "", err
	}
	module, err := filepath.Abs(modulePath)
	if err != nil {
		return "", "", errors.New("SDK module path unavailable")
	}
	expected := filepath.Join(worktree, "tools", "windows-ci-sdk-2737")
	if !strings.EqualFold(filepath.Clean(module), filepath.Clean(expected)) {
		return "", "", errors.New("guest agent source escaped the fixed module")
	}
	if !validInstance(revision) {
		return "", "", errors.New("guest agent build revision rejected")
	}
	output := filepath.Join(root.Path, "windows-ci-sdk-agent-"+revision+".exe")
	goExe, err := exec.LookPath("go")
	if err != nil {
		return "", "", errors.New("Go compiler unavailable")
	}
	cmd := exec.Command(goExe, "build", "-trimpath", "-o", output, "./cmd/windows-ci-sdk-agent")
	cmd.Dir = module
	for _, directory := range []string{"go-temp", "go-cache", "go-path"} {
		if err := os.MkdirAll(filepath.Join(root.Path, directory), 0o700); err != nil {
			return "", "", errors.New("owned Go build directory creation failed")
		}
	}
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "TEMP=" + filepath.Join(root.Path, "go-temp"), "TMP=" + filepath.Join(root.Path, "go-temp"), "GOCACHE=" + filepath.Join(root.Path, "go-cache"), "GOPATH=" + filepath.Join(root.Path, "go-path"), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off"}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		writePrivateBuildFailure(root.Path, revision, stdout.Bytes(), stderr.Bytes())
		return "", "", errors.New("guest helper build failed")
	}
	sizeAndHash, err := hashAndSize(output)
	if err != nil {
		return "", "", err
	}
	return output, sizeAndHash, nil
}

func BuildGuestSeedISO(root OwnedRoot, worktree, instance, agentPath string, client Identity) (string, error) {
	if err := VerifyOwnedRoot(root, worktree); err != nil {
		return "", err
	}
	if err := ValidateSetupAssets(); err != nil {
		return "", err
	}
	if !validInstance(instance) {
		return "", errors.New("guest instance rejected")
	}
	seed, err := BuildPublicSeed(instance, client)
	if err != nil {
		return "", err
	}
	if !pathWithin(root.Path, agentPath) {
		return "", errors.New("guest helper must be stored in the owned root")
	}
	info, err := os.Stat(agentPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 64<<20 {
		return "", errors.New("guest helper binary rejected")
	}
	seedDir := filepath.Join(root.Path, "seed-"+instance)
	if err := os.Mkdir(seedDir, 0o700); err != nil {
		return "", errors.New("seed directory creation failed")
	}
	for _, name := range []string{"Autounattend.xml", "stage.ps1"} {
		data, ok := guestSeedAsset(name)
		if !ok {
			return "", errors.New("fixed seed asset missing")
		}
		if err := writeOwnedFile(filepath.Join(seedDir, name), data); err != nil {
			return "", err
		}
	}
	if err := copyOwnedFile(agentPath, filepath.Join(seedDir, "windows-ci-sdk-agent.exe")); err != nil {
		return "", err
	}
	if err := writeOwnedFile(filepath.Join(seedDir, "seed.json"), seed); err != nil {
		return "", err
	}
	psScript := filepath.Join(root.Path, "create-seed-iso-"+instance+".ps1")
	if err := writeOwnedFile(psScript, seedISOBuilderScript); err != nil {
		return "", err
	}
	iso := filepath.Join(root.Path, "windows-ci-sdk-seed-"+instance+".iso")
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", psScript, "-SourceDir", seedDir, "-Destination", iso)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32") + `;` + filepath.Join(os.Getenv("SystemRoot"), `System32\WindowsPowerShell\v1.0`), "TEMP=" + filepath.Join(root.Path, "go-temp"), "TMP=" + filepath.Join(root.Path, "go-temp")}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		writePrivateBuildFailure(root.Path, "seed-iso", stdout.Bytes(), stderr.Bytes())
		return "", errors.New("seed ISO creation failed")
	}
	if info, err := os.Stat(iso); err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 128<<20 {
		return "", errors.New("seed ISO size rejected")
	}
	return iso, nil
}

func ProvisionNewGuest(root OwnedRoot, worktree, iso, seedISO, instance string) (VMReceipt, error) {
	if err := VerifyOwnedRoot(root, worktree); err != nil {
		return VMReceipt{}, err
	}
	if !validInstance(instance) || !pathWithin(root.Path, iso) || !pathWithin(root.Path, seedISO) {
		return VMReceipt{}, errors.New("guest provisioning inputs rejected")
	}
	if err := ValidateSetupAssets(); err != nil {
		return VMReceipt{}, err
	}
	if err := validateISOFile(iso); err != nil {
		return VMReceipt{}, err
	}
	if err := validateSeedISO(seedISO); err != nil {
		return VMReceipt{}, err
	}
	cfg := vmProvisionConfig{Root: root.Path, Instance: instance, ISO: iso, SeedISO: seedISO}
	configPath := filepath.Join(root.Path, "provision.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		return VMReceipt{}, errors.New("VM config encoding failed")
	}
	if err := writeOwnedFile(configPath, data); err != nil {
		return VMReceipt{}, err
	}
	scriptPath := filepath.Join(root.Path, "provision-vm.ps1")
	adminScript, err := renderAdminPowerShellScript(provisionVMPS, root.Path)
	if err != nil {
		return VMReceipt{}, err
	}
	if err := writeOwnedFile(scriptPath, adminScript); err != nil {
		return VMReceipt{}, err
	}
	if err := runAdminBrokerScript(scriptPath, 1800, root.Path); err != nil {
		return VMReceipt{}, err
	}
	return readProvisionedVMReceipt(root.Path, instance)
}

func PrepareProvisionedGuest(ctx context.Context, instance string, client Identity) (PreparationReceipt, error) {
	bootCtx, cancel := context.WithTimeout(ctx, 25*time.Minute)
	defer cancel()
	for bootCtx.Err() == nil {
		pipe, err := OpenOwnedGuestCOMPipe(bootCtx, instance)
		if err != nil {
			return PreparationReceipt{}, err
		}
		bootstrap, err := SendCOMBootstrap(pipe, instance)
		_ = pipe.Close()
		if err != nil {
			if err.Error() != "COM bootstrap response unavailable" {
				return PreparationReceipt{}, err
			}
			select {
			case <-bootCtx.Done():
				return PreparationReceipt{}, errors.New("guest Setup readiness deadline exceeded")
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		results, err := PrepareSDKSet(bootstrap.IPv4, bootstrap.ServerSHA256, instance, client)
		return PreparationReceipt{Instance: instance, GuestIPv4: bootstrap.IPv4.String(), ServerSHA256: bootstrap.ServerSHA256, Results: results}, err
	}
	return PreparationReceipt{}, errors.New("guest Setup readiness deadline exceeded")
}

func ShutdownRemoveOwnedVM(root OwnedRoot, worktree, instance string) (RecoveryReceipt, error) {
	if err := VerifyOwnedRoot(root, worktree); err != nil {
		return RecoveryReceipt{}, err
	}
	vm, err := readVMReceipt(root.Path, instance)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	script := filepath.Join(root.Path, "remove-vm-"+instance+".ps1")
	adminScript, err := renderAdminPowerShellScript(removeVMKeepVHDPS, root.Path)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	if err := writeOwnedFile(script, adminScript); err != nil {
		return RecoveryReceipt{}, err
	}
	if err := runAdminBrokerScript(script, 600, root.Path); err != nil {
		return RecoveryReceipt{}, err
	}
	data, err := os.ReadFile(filepath.Join(root.Path, "vm-recovery.private.json"))
	if err != nil {
		return RecoveryReceipt{}, errors.New("template recovery receipt unavailable")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var receipt RecoveryReceipt
	if dec.Decode(&receipt) != nil || dec.Decode(new(json.RawMessage)) != io.EOF || receipt.Instance != instance || receipt.VMID != vm.VMID || receipt.VMState != "Off" || receipt.MemoryAssigned != 0 || !receipt.VMRemoved || filepath.Clean(receipt.VHD) != filepath.Clean(vm.VHD) || !receipt.VHDDetached || receipt.VHDSize <= 0 || !validDigest(receipt.VHDSHA256) {
		return RecoveryReceipt{}, errors.New("template recovery receipt rejected")
	}
	return receipt, nil
}

func validateISOFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 8<<30 {
		return errors.New("Windows evaluation ISO rejected")
	}
	return nil
}

func validateSeedISO(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 128<<20 {
		return errors.New("guest seed ISO rejected")
	}
	return nil
}

func runAdminBrokerScript(scriptPath string, timeoutSeconds int, evidenceRoot string) error {
	if timeoutSeconds < 1 || timeoutSeconds > 1800 {
		return errors.New("admin broker timeout rejected")
	}
	if !pathWithin(evidenceRoot, scriptPath) {
		return errors.New("admin script path escaped owned root")
	}
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", adminBrokerScript, "-Script", scriptPath, "-TimeoutSec", fmt.Sprint(timeoutSeconds))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32") + `;` + filepath.Join(os.Getenv("SystemRoot"), `System32\WindowsPowerShell\v1.0`), "TEMP=" + filepath.Join(os.Getenv("SystemRoot"), "Temp"), "TMP=" + filepath.Join(os.Getenv("SystemRoot"), "Temp")}
	cmd.Env = append(cmd.Env, "PATHEXT=.COM;.EXE;.BAT;.CMD;.PS1", "WINDIR="+os.Getenv("SystemRoot"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		writePrivateBuildFailure(evidenceRoot, "admin-broker", stdout.Bytes(), stderr.Bytes())
		return errors.New("CC-AdminBroker Hyper-V operation failed")
	}
	return nil
}

// RunSDKPreparationCLI is the fixed, no-argument host entrypoint used only from
// this task's scheduler worktree. It creates a fresh safe-delete-owned root.
func RunSDKPreparationCLI(ctx context.Context, worktree string) (TaskEvidence, error) {
	base, err := filepath.Abs(worktree)
	if err != nil {
		return TaskEvidence{}, errors.New("task worktree unavailable")
	}
	if err := verifyTaskWorktree(base); err != nil {
		return TaskEvidence{}, err
	}
	preparedRoot, reusable, err := findPreparedOwnedRoot(base)
	if err != nil {
		return TaskEvidence{}, err
	}
	root, err := CreateOwnedRoot(ctx, base)
	if err != nil {
		return TaskEvidence{}, err
	}
	if reusable {
		for _, name := range []string{"windows-server-2025-eval.iso", "windows-server-2025-eval.measure.private.txt", "windows-server-2025-eval-validation.private.json"} {
			if err := copyOwnedFile(filepath.Join(preparedRoot.Path, name), filepath.Join(root.Path, name)); err != nil {
				return TaskEvidence{}, err
			}
		}
		measurement, err := hashAndSize(filepath.Join(root.Path, "windows-server-2025-eval.iso"))
		if err != nil {
			return TaskEvidence{}, err
		}
		_, _, expected, _, err := loadPreparedEvaluationISO(preparedRoot, base)
		if err != nil || !strings.HasSuffix(strings.ToLower(measurement), expected) {
			return TaskEvidence{}, errors.New("copied public ISO integrity mismatch")
		}
	}
	module := filepath.Join(base, "tools", "windows-ci-sdk-2737")
	return RunSDKPreparation(ctx, root, base, module, currentSourceCommit(ctx, base))
}

// ValidatePreparedEvaluationISOCLI verifies only the previously staged public
// image and its private validation receipt; it never creates a VM.
func ValidatePreparedEvaluationISOCLI(worktree string) (int64, string, error) {
	base, err := filepath.Abs(worktree)
	if err != nil {
		return 0, "", errors.New("task worktree unavailable")
	}
	if err := verifyTaskWorktree(base); err != nil {
		return 0, "", err
	}
	root, found, err := findPreparedOwnedRoot(base)
	if err != nil {
		return 0, "", err
	}
	if !found {
		return 0, "", errors.New("prepared official evaluation ISO not found")
	}
	_, size, digest, prepared, err := loadPreparedEvaluationISO(root, base)
	if err != nil {
		return 0, "", err
	}
	if !prepared {
		return 0, "", errors.New("prepared official evaluation ISO evidence incomplete")
	}
	return size, digest, nil
}

func verifyTaskWorktree(worktree string) error {
	if filepath.Base(worktree) != "todo-2783-godd-mcp-ci-windows-guest-sdk" || !strings.HasPrefix(filepath.Base(filepath.Dir(worktree)), "unattended-operations-20261013-wt") {
		return errors.New("current directory is not the assigned task worktree")
	}
	if err := verifyUnprotectedPath(worktree); err != nil {
		return err
	}
	branchCmd := exec.Command("git", "-C", worktree, "branch", "--show-current")
	branchCmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	var branch bytes.Buffer
	branchCmd.Stdout, branchCmd.Stderr = &branch, io.Discard
	if err := branchCmd.Run(); err != nil || !strings.HasPrefix(strings.TrimSpace(branch.String()), "scheduler/todo-2783-") {
		return errors.New("current branch is not the assigned scheduler branch")
	}
	return nil
}

func currentSourceCommit(ctx context.Context, worktree string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", worktree, "rev-parse", "HEAD")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	if cmd.Run() != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

func runHiddenPowerShell(scriptPath string, timeoutSeconds int, evidenceRoot string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds+15)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32"), "TEMP=" + filepath.Join(os.Getenv("SystemRoot"), "Temp"), "TMP=" + filepath.Join(os.Getenv("SystemRoot"), "Temp")}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		writePrivateBuildFailure(evidenceRoot, "seed-iso", stdout.Bytes(), stderr.Bytes())
		return errors.New("seed ISO generation failed")
	}
	return nil
}

func readProvisionedVMReceipt(root, instance string) (VMReceipt, error) {
	receipt, err := readVMReceipt(root, instance)
	if err != nil {
		return VMReceipt{}, err
	}
	if receipt.State != "Running" || receipt.Generation != 1 || receipt.CPUs != 2 || receipt.StartupMemory != 4<<30 || receipt.DynamicMemory || receipt.NICs != 1 || receipt.GPUs != 0 {
		return VMReceipt{}, errors.New("Hyper-V guest resource configuration rejected")
	}
	return receipt, nil
}

func readVMReceipt(root, instance string) (VMReceipt, error) {
	data, err := os.ReadFile(filepath.Join(root, "vm-created.private.json"))
	if err != nil {
		return VMReceipt{}, errors.New("VM creation receipt unavailable")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var receipt VMReceipt
	if dec.Decode(&receipt) != nil || dec.Decode(new(json.RawMessage)) != io.EOF || receipt.Instance != instance || !validVMGUID(receipt.VMID) || receipt.Name != "cc-sdk-2737-"+instance || !strings.EqualFold(filepath.Clean(receipt.Root), filepath.Clean(root)) || filepath.Clean(receipt.VHD) != filepath.Join(filepath.Clean(root), "guest.vhdx") || !pathWithin(root, receipt.ISO) || !pathWithin(root, receipt.SeedISO) || receipt.Generation != 1 || (receipt.State != "Off" && receipt.State != "Running") || receipt.COMPipe != mustCOMPipe(instance) {
		return VMReceipt{}, errors.New("Hyper-V guest receipt failed configuration validation")
	}
	return receipt, nil
}

func validVMGUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func hashAndSize(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errors.New("owned artifact unavailable")
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil || n == 0 {
		return "", errors.New("owned artifact hash failed")
	}
	return fmt.Sprintf("%d:%s", n, hex.EncodeToString(h.Sum(nil))), nil
}

func writeOwnedFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("owned file creation failed")
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("owned file write failed")
	}
	return nil
}

func copyOwnedFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return errors.New("owned source file unavailable")
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("owned copy destination creation failed")
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("owned file copy failed")
	}
	return nil
}

func writePrivateBuildFailure(root, stage string, stdout, stderr []byte) {
	name := "failure-" + stage + "-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".private.json"
	record := map[string]string{"stage": stage, "stdout": string(stdout[:min(len(stdout), 8192)]), "stderr": string(stderr[:min(len(stderr), 8192)])}
	data, err := json.Marshal(record)
	if err == nil {
		_ = writeOwnedFile(filepath.Join(root, name), data)
	}
}

func pathWithin(root, path string) bool {
	base, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func validateOwnedRoot(root OwnedRoot, worktree string) error { return VerifyOwnedRoot(root, worktree) }
