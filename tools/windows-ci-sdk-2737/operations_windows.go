//go:build windows && amd64

package sdkprep

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const guestSDKRoot = `C:\ProgramData\AutoDevJapan\windows-ci-sdk-2737`

type windowsOperations struct {
	root     string
	client   *http.Client
	opMu     sync.Mutex
	versions map[PackageID]string
	proofs   map[PackageID]string
}

func NewWindowsOperations() (Operations, error) {
	root := filepath.Clean(guestSDKRoot)
	if err := initializeGuestRoot(root); err != nil {
		return nil, errors.New("SDK root initialization failed")
	}
	client := &http.Client{
		Timeout: 60 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || req.URL.Scheme != "https" || !allowedHost(req.URL.Hostname()) {
				return errors.New("SDK download redirect rejected")
			}
			return nil
		},
	}
	return &windowsOperations{root: root, client: client}, nil
}

func (o *windowsOperations) Status(instance string) Result {
	return Result{Instance: instance, Stage: "service-ready", OK: true}
}

func (o *windowsOperations) Prepare(instance string, id PackageID) Result {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	result := Result{Instance: instance, Package: id, Stage: "accepted"}
	appendPrivateRunLog("sdk-"+string(id), []byte("prepare accepted"), nil)
	var err error
	switch id {
	case GitForWindows:
		err = o.prepareGit()
	case PowerShell7:
		err = o.preparePowerShell()
	case RustToolchain:
		err = o.prepareRust()
	case MSVCBuildTools, WindowsSDK:
		err = o.prepareVisualStudio()
	case Sccache:
		err = o.prepareSccache()
	default:
		err = errors.New("unknown SDK package")
	}
	if err != nil {
		result.Stage, result.Code = "prepare-failed", safeFailureCode(err)
		result.Detail = err.Error()
		appendPrivateRunLog("sdk-"+string(id)+"-detail", []byte(err.Error()), err)
		appendPrivateRunLog("sdk-"+string(id), []byte(result.Stage+" "+result.Code), err)
		return result
	}
	result.Stage, result.OK = "prepared", true
	appendPrivateRunLog("sdk-"+string(id), []byte(result.Stage), nil)
	return result
}

func (o *windowsOperations) Verify(instance string, id PackageID) Result {
	o.opMu.Lock()
	defer o.opMu.Unlock()
	result := Result{Instance: instance, Package: id, Stage: "version-check"}
	appendPrivateRunLog("sdk-"+string(id), []byte("version check accepted"), nil)
	if err := o.verifyPackage(id); err != nil {
		result.Stage, result.Code = "version-check-failed", safeFailureCode(err)
		appendPrivateRunLog("sdk-"+string(id), []byte(result.Stage+" "+result.Code), err)
		return result
	}
	result.Stage, result.OK = "version-confirmed", true
	result.Version, result.VersionProofSHA256 = o.versions[id], o.proofs[id]
	appendPrivateRunLog("sdk-"+string(id), []byte(result.Stage), nil)
	return result
}

func (o *windowsOperations) prepareGit() error {
	a := artifact(GitForWindows)
	installer, err := o.fetch(a)
	if err != nil {
		return err
	}
	args := []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-", "/DIR=" + filepath.Join(o.root, "git"), "/LOG=" + filepath.Join(o.root, "private", "git-installer.log")}
	if err := runFixed(installer, args, o.environment()); err != nil {
		return fmt.Errorf("Git installer failed: %w", err)
	}
	return o.verifyPackage(GitForWindows)
}

func (o *windowsOperations) preparePowerShell() error {
	a := artifact(PowerShell7)
	msi, err := o.fetch(a)
	if err != nil {
		return err
	}
	installDir := filepath.Join(o.root, "powershell")
	args := []string{"/i", msi, "/qn", "/norestart", "INSTALLFOLDER=" + installDir, "ADD_PATH=0", "ENABLE_PSREMOTING=0"}
	if err := runFixed(filepath.Join(os.Getenv("SystemRoot"), "System32", "msiexec.exe"), args, o.environment()); err != nil {
		return errors.New("PowerShell installer failed")
	}
	return o.verifyPackage(PowerShell7)
}

func (o *windowsOperations) prepareRust() error {
	if err := o.verifyPackage(GitForWindows); err != nil {
		return errors.New("pinned Git for Windows must be prepared before Rust")
	}
	installPrefix := filepath.Join(o.root, "rust-1.97.1")
	bash := filepath.Join(o.root, "git", "bin", "bash.exe")
	for _, a := range PinnedArtifacts() {
		if a.ID != RustToolchain {
			continue
		}
		archivePath, err := o.fetch(a)
		if err != nil {
			return err
		}
		stage := filepath.Join(o.root, "staging", filepath.Base(archivePath))
		if err := extractTarGz(archivePath, stage, 1<<30); err != nil {
			return errors.New("Rust component extraction failed")
		}
		installer, err := findFile(stage, "install.sh")
		if err != nil {
			return errors.New("Rust component installer is missing")
		}
		prefix := msysPath(installPrefix)
		if err := runFixed(bash, []string{"--noprofile", "--norc", msysPath(installer), "--prefix=" + prefix, "--disable-ldconfig"}, o.environment()); err != nil {
			return errors.New("Rust component installation failed")
		}
	}
	return o.verifyPackage(RustToolchain)
}

func (o *windowsOperations) prepareVisualStudio() error {
	installer := filepath.Join(o.root, "downloads", "vs_BuildTools.exe")
	if _, err := os.Stat(installer); errors.Is(err, os.ErrNotExist) {
		if err := o.downloadMicrosoftBootstrapper(installer); err != nil {
			return err
		}
	}
	if err := verifyMicrosoftSigner(installer); err != nil {
		return errors.New("Visual Studio bootstrapper signature check failed")
	}
	installDir := filepath.Join(o.root, "vs-build-tools")
	args := []string{
		"--quiet", "--wait", "--norestart", "--nocache", "--installPath", installDir,
		"--productId", "Microsoft.VisualStudio.Product.BuildTools",
		"--add", "Microsoft.VisualStudio.Workload.VCTools",
		"--add", "Microsoft.VisualStudio.Component.VC.14.50.18.0.x86.x64",
		"--add", "Microsoft.VisualStudio.Component.Windows11SDK.26100",
		"--includeRecommended",
	}
	if err := runFixed(installer, args, o.environment()); err != nil {
		return errors.New("Visual Studio Build Tools installation failed")
	}
	if err := o.verifyPackage(MSVCBuildTools); err != nil {
		return err
	}
	return o.verifyPackage(WindowsSDK)
}

func (o *windowsOperations) prepareSccache() error {
	a := artifact(Sccache)
	archive, err := o.fetch(a)
	if err != nil {
		return err
	}
	if err := extractZip(archive, filepath.Join(o.root, "sccache"), 256<<20); err != nil {
		return errors.New("sccache extraction failed")
	}
	return o.verifyPackage(Sccache)
}

func (o *windowsOperations) fetch(a Artifact) (string, error) {
	if err := ValidateArtifact(a); err != nil {
		return "", err
	}
	name, err := artifactFilename(a)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(o.root, "downloads", name)
	if info, err := os.Stat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("cached artifact is not a regular file")
		}
		f, err := os.Open(destination)
		if err != nil {
			return "", errors.New("cached artifact open failed")
		}
		_, verifyErr := VerifyArtifact(f, a)
		_ = f.Close()
		if verifyErr != nil {
			return "", errors.New("cached artifact hash mismatch")
		}
		return destination, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("cached artifact inspection failed")
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, a.URL, nil)
	if err != nil {
		return "", errors.New("SDK download request construction failed")
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return "", errors.New("SDK download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request == nil || resp.Request.URL.Scheme != "https" || !allowedHost(resp.Request.URL.Hostname()) {
		return "", errors.New("SDK download origin was rejected")
	}
	tmp := destination + ".partial"
	defer os.Remove(tmp)
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", errors.New("SDK download staging file creation failed")
	}
	h := sha256.New()
	limited := io.LimitReader(resp.Body, 2<<30)
	n, copyErr := io.Copy(io.MultiWriter(file, h), limited)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n == 2<<30 || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return "", errors.New("SDK download hash or size check failed")
	}
	if err := os.Rename(tmp, destination); err != nil {
		return "", errors.New("SDK artifact finalize failed")
	}
	return destination, nil
}

func (o *windowsOperations) downloadMicrosoftBootstrapper(destination string) error {
	const url = "https://aka.ms/vs/stable/vs_buildtools.exe"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return errors.New("Microsoft bootstrapper request construction failed")
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("Microsoft bootstrapper download failed (%s)", classifyNetworkFailure(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request == nil || resp.Request.URL.Scheme != "https" || !allowedHost(resp.Request.URL.Hostname()) {
		return errors.New("Microsoft bootstrapper origin rejected")
	}
	tmp := destination + ".partial"
	defer os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("Microsoft bootstrapper staging file creation failed")
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, 100<<20))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n == 100<<20 {
		return errors.New("Microsoft bootstrapper download incomplete")
	}
	if err := verifyMicrosoftSigner(tmp); err != nil {
		return errors.New("Microsoft bootstrapper signature rejected")
	}
	if err := os.Rename(tmp, destination); err != nil {
		return errors.New("Microsoft bootstrapper finalize failed")
	}
	return nil
}

func (o *windowsOperations) verifyPackage(id PackageID) error {
	var commands [][]string
	var expected []string
	type probe struct {
		Tool         string `json:"tool"`
		Version      string `json:"version"`
		OutputSHA256 string `json:"output_sha256"`
	}
	var probes []probe
	var versions []string
	switch id {
	case GitForWindows:
		commands = [][]string{{filepath.Join(o.root, "git", "cmd", "git.exe"), "--version"}}
		expected = []string{"2.56.0.windows.2"}
	case PowerShell7:
		commands = [][]string{{filepath.Join(o.root, "powershell", "7", "pwsh.exe"), "--version"}}
		expected = []string{"7.6.6"}
	case RustToolchain:
		bin := filepath.Join(o.root, "rust-1.97.1", "bin")
		commands = [][]string{{filepath.Join(bin, "rustc.exe"), "--version"}, {filepath.Join(bin, "cargo.exe"), "--version"}, {filepath.Join(bin, "rustfmt.exe"), "--version"}, {filepath.Join(bin, "clippy-driver.exe"), "--version"}}
		expected = []string{"1.97.1", "1.97.", "1.9.0", "0.1.97"}
	case Sccache:
		exe, err := findFile(filepath.Join(o.root, "sccache"), "sccache.exe")
		if err != nil {
			return errors.New("sccache executable is missing")
		}
		commands = [][]string{{exe, "--version"}}
		expected = []string{"0.18.0"}
	case MSVCBuildTools:
		cl, err := findMSVCCompiler(filepath.Join(o.root, "vs-build-tools"))
		if err != nil {
			return err
		}
		args, err := o.vsDevEnvironment(`(set VCToolsVersion & set WindowsSDKVersion & set INCLUDE & set LIB & "` + cl + `" /Bv & exit /b 0)`)
		if err != nil {
			return err
		}
		commands = [][]string{append([]string{filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")}, args...)}
		expected = []string{"19.50."}
	case WindowsSDK:
		kit := filepath.Join(os.Getenv("ProgramFiles(x86)"), "Windows Kits", "10")
		rc := filepath.Join(kit, "bin", "10.0.26100.0", "x64", "rc.exe")
		header := filepath.Join(kit, "Include", "10.0.26100.0", "um", "Windows.h")
		library := filepath.Join(kit, "Lib", "10.0.26100.0", "um", "x64", "kernel32.lib")
		metadata := `if(-not (Test-Path -LiteralPath '` + header + `') -or -not (Test-Path -LiteralPath '` + library + `')){exit 74}; (Get-Item -LiteralPath '` + rc + `').VersionInfo.ProductVersion`
		commands = [][]string{{rc, "/?"}, {filepath.Join(o.root, "powershell", "7", "pwsh.exe"), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", metadata}}
		expected = []string{"10.0.10011.", "10.0.26100."}
	default:
		return errors.New("unknown SDK package")
	}
	for i, command := range commands {
		output, err := runCapture(command[0], command[1:], o.environment())
		if err != nil {
			return errors.New("SDK version probe failed")
		}
		if i >= len(expected) || !bytes.Contains(output, []byte(expected[i])) {
			return errors.New("SDK version does not match the pin")
		}
		if id == MSVCBuildTools && (!bytes.Contains(output, []byte("VCToolsVersion=14.50.")) || !bytes.Contains(output, []byte("WindowsSDKVersion=10.0.26100.0")) || !bytes.Contains(output, []byte("INCLUDE=")) || !bytes.Contains(output, []byte("LIB="))) {
			return errors.New("MSVC and Windows SDK environment is incomplete")
		}
		version := measuredVersion(output, expected[i])
		if version == "" {
			return errors.New("SDK measured version unavailable")
		}
		digest := sha256.Sum256(output)
		probes = append(probes, probe{Tool: filepath.Base(command[0]), Version: version, OutputSHA256: hex.EncodeToString(digest[:])})
		versions = append(versions, version)
	}
	proof, err := json.Marshal(probes)
	if err != nil {
		return errors.New("SDK version proof encoding failed")
	}
	if err := os.WriteFile(filepath.Join(o.root, "private", "versions-"+string(id)+".json"), proof, 0o600); err != nil {
		return errors.New("SDK version proof save failed")
	}
	digest := sha256.Sum256(proof)
	if o.versions == nil {
		o.versions = make(map[PackageID]string)
		o.proofs = make(map[PackageID]string)
	}
	o.versions[id], o.proofs[id] = strings.Join(versions, "; "), hex.EncodeToString(digest[:])
	if id == WindowsSDK {
		o.versions[id] = versions[len(versions)-1]
	}
	return nil
}

func measuredVersion(output []byte, prefix string) string {
	pattern := regexp.MustCompile(`[0-9]+(?:\.[0-9]+)+(?:\.windows\.[0-9]+)?`)
	for _, match := range pattern.FindAllString(string(output), -1) {
		if strings.HasPrefix(match, prefix) {
			return match
		}
	}
	return ""
}

func (o *windowsOperations) environment() []string {
	env := []string{
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"PATH=" + os.Getenv("SystemRoot") + `\System32;` + filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0") + ";" + filepath.Join(o.root, "git", "cmd") + ";" + filepath.Join(o.root, "git", "usr", "bin") + ";" + filepath.Join(o.root, "git", "bin") + ";" + filepath.Join(o.root, "powershell", "7"),
		"SystemDrive=" + filepath.VolumeName(os.Getenv("SystemRoot")), "ComSpec=" + filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"),
		"USERPROFILE=" + filepath.Join(o.root, "service-profile"), "HOME=" + filepath.Join(o.root, "service-profile"), "APPDATA=" + filepath.Join(o.root, "service-profile", "AppData", "Roaming"), "LOCALAPPDATA=" + filepath.Join(o.root, "service-profile", "AppData", "Local"),
		"TEMP=" + filepath.Join(o.root, "staging"), "TMP=" + filepath.Join(o.root, "staging"),
		"PATHEXT=.COM;.EXE;.BAT;.CMD;.PS1", "WINDIR=" + os.Getenv("SystemRoot"),
		"ProgramFiles=" + os.Getenv("ProgramFiles"), "ProgramFiles(x86)=" + os.Getenv("ProgramFiles(x86)"), "ProgramData=" + os.Getenv("ProgramData"),
		"CARGO_HOME=" + filepath.Join(o.root, "cargo-home"),
		"RUSTUP_HOME=" + filepath.Join(o.root, "rustup-home"),
		"CARGO_TARGET_DIR=" + filepath.Join(o.root, "target"),
		"SCCACHE_DIR=" + filepath.Join(o.root, "sccache-cache"),
		"SCCACHE_CACHE_SIZE=30G",
	}
	return env
}

func (o *windowsOperations) vsDevEnvironment(command string) ([]string, error) {
	vsDev := filepath.Join(o.root, "vs-build-tools", "Common7", "Tools", "VsDevCmd.bat")
	joined := `call "` + vsDev + `" -arch=x64 -host_arch=x64 -vcvars_ver=14.50 -winsdk=10.0.26100.0 && ` + command
	return []string{"/d", "/s", "/c", joined}, nil
}

func runFixed(executable string, args, env []string) error {
	_, err := runCapture(executable, args, env)
	var exit *fixedProcessExit
	if errors.As(err, &exit) && exit.Code == 3010 && (strings.EqualFold(filepath.Base(executable), "msiexec.exe") || strings.EqualFold(filepath.Base(executable), "vs_BuildTools.exe")) {
		appendPrivateRunLog("sdk-installer-reboot-requested", []byte(filepath.Base(executable)), nil)
		return nil
	}
	return err
}

type fixedProcessExit struct{ Code int }

func (e *fixedProcessExit) Error() string {
	return fmt.Sprintf("fixed SDK process failed (exit=%d)", e.Code)
}

func runCapture(executable string, args, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	configureFixedCommandLine(cmd)
	cmd.WaitDelay = 5 * time.Second
	cmd.Env, cmd.Stdin = env, nil
	var output bytes.Buffer
	writer := &boundedBuffer{buffer: &output, max: 64 << 10}
	cmd.Stdout, cmd.Stderr = writer, writer
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		err = nil
	}
	appendPrivateRunLog(filepath.Base(executable), output.Bytes(), err)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, &fixedProcessExit{Code: exit.ExitCode()}
		}
		var errno syscall.Errno
		if errors.As(err, &errno) {
			return nil, fmt.Errorf("fixed SDK process failed (win32=%d)", errno)
		}
		return nil, errors.New("fixed SDK process start failed")
	}
	return output.Bytes(), nil
}

// cmd.exe does not use CommandLineToArgvW quoting. Preserve the fixed batch
// command's literal quotes instead of applying Go's backslash-quote escaping.
func configureFixedCommandLine(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") && len(cmd.Args) == 5 && cmd.Args[1] == "/d" && cmd.Args[2] == "/s" && cmd.Args[3] == "/c" {
		cmd.SysProcAttr.CmdLine = syscall.EscapeArg(cmd.Path) + ` /d /s /c "` + cmd.Args[4] + `"`
	}
}

func initializeGuestRoot(root string) error {
	if filepath.Clean(root) != filepath.Clean(guestSDKRoot) {
		return errors.New("SDK root is not the fixed guest directory")
	}
	for _, directory := range []string{"downloads", "private", "staging", "cargo-home", "rustup-home", "target", "sccache-cache", "service-profile/AppData/Roaming", "service-profile/AppData/Local"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			return err
		}
	}
	icacls := filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe")
	cmd := exec.Command(icacls, root, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)RX")
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32")}, nil, nil, nil
	if err := cmd.Run(); err != nil {
		return errors.New("guest SDK root ACL setup failed")
	}
	private := exec.Command(icacls, filepath.Join(root, "private"), "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F")
	private.Stdin, private.Stdout, private.Stderr = nil, nil, nil
	if err := private.Run(); err != nil {
		return errors.New("guest raw log ACL setup failed")
	}
	return nil
}

func appendPrivateRunLog(executable string, output []byte, runErr error) {
	root := filepath.Join(guestSDKRoot, "private")
	if os.MkdirAll(root, 0o700) != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(root, "operations.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	status := "success"
	if runErr != nil {
		status = "failed"
	}
	_, _ = fmt.Fprintf(file, "[%s] %s %s output-bytes=%d\n", time.Now().UTC().Format(time.RFC3339), filepath.Base(executable), status, len(output))
	if len(output) != 0 {
		_, _ = file.Write(output)
		if output[len(output)-1] != '\n' {
			_, _ = file.Write([]byte{'\n'})
		}
	}
	if runErr != nil {
		_, _ = fmt.Fprintf(file, "process-error=%v\n", runErr)
	}
	_ = file.Sync()
}

type boundedBuffer struct {
	buffer *bytes.Buffer
	max    int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.max - b.buffer.Len()
	if remaining > 0 {
		if remaining > n {
			remaining = n
		}
		_, _ = b.buffer.Write(p[:remaining])
	}
	return n, nil
}

func artifact(id PackageID) Artifact {
	for _, a := range PinnedArtifacts() {
		if a.ID == id {
			return a
		}
	}
	return Artifact{}
}

func artifactFilename(a Artifact) (string, error) {
	name := path.Base(strings.Split(a.URL, "?")[0])
	if name == "." || name == "/" || strings.ContainsAny(name, "\\/:\x00") {
		return "", errors.New("invalid pinned artifact filename")
	}
	return name, nil
}

func extractTarGz(archivePath, destination string, maxBytes int64) error {
	if _, err := os.Stat(destination); err == nil {
		return errors.New("Rust staging destination already exists")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !filepath.IsLocal(filepath.FromSlash(h.Name)) || strings.Contains(h.Name, "\\") {
			return errors.New("archive path rejected")
		}
		target := filepath.Join(destination, filepath.FromSlash(h.Name))
		if !within(destination, target) {
			return errors.New("archive path escaped staging root")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += h.Size
			if h.Size < 0 || total > maxBytes {
				return errors.New("archive exceeds size limit")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(out, tr, h.Size)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return errors.New("archive entry type rejected")
		}
	}
	return nil
}

func extractZip(archivePath, destination string, maxBytes int64) error {
	if _, err := os.Stat(destination); err == nil {
		return errors.New("sccache staging destination already exists")
	}
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	var total uint64
	for _, f := range r.File {
		if !filepath.IsLocal(filepath.FromSlash(f.Name)) || strings.Contains(f.Name, "\\") {
			return errors.New("archive path rejected")
		}
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 {
			return errors.New("archive symlink rejected")
		}
		target := filepath.Join(destination, filepath.FromSlash(f.Name))
		if !within(destination, target) {
			return errors.New("archive path escaped staging root")
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() {
			return errors.New("archive entry type rejected")
		}
		total += f.UncompressedSize64
		if total > uint64(maxBytes) {
			return errors.New("archive exceeds size limit")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		in, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.CopyN(out, in, int64(f.UncompressedSize64))
		closeIn := in.Close()
		closeOut := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeIn != nil {
			return closeIn
		}
		if closeOut != nil {
			return closeOut
		}
	}
	return nil
}

func within(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func msysPath(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if len(clean) >= 2 && clean[1] == ':' {
		return "/" + strings.ToLower(clean[:1]) + clean[2:]
	}
	return clean
}

func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("staging link rejected")
		}
		if !d.IsDir() && d.Name() == name {
			if found != "" {
				return errors.New("duplicate fixed installer path")
			}
			found = p
		}
		return nil
	})
	if err != nil || found == "" {
		return "", errors.New("fixed installer file not found")
	}
	return found, nil
}

func findMSVCCompiler(vsRoot string) (string, error) {
	toolsetRoot := filepath.Join(vsRoot, "VC", "Tools", "MSVC")
	entries, err := os.ReadDir(toolsetRoot)
	if err != nil {
		return "", errors.New("pinned MSVC toolset directory unavailable")
	}
	var match string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "14.50.") {
			if match != "" {
				return "", errors.New("multiple pinned MSVC toolsets found")
			}
			match = entry.Name()
		}
	}
	if match == "" {
		return "", errors.New("pinned MSVC toolset not found")
	}
	compiler := filepath.Join(toolsetRoot, match, "bin", "Hostx64", "x64", "cl.exe")
	if !within(vsRoot, compiler) {
		return "", errors.New("MSVC compiler path escaped installation root")
	}
	if info, err := os.Stat(compiler); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("MSVC compiler file unavailable")
	}
	return compiler, nil
}

func safeFailureCode(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "hash"):
		return "artifact-integrity"
	case strings.Contains(text, "signature"):
		return "publisher-signature"
	case strings.Contains(text, "version"):
		return "version-probe"
	case strings.Contains(text, "download"):
		return "public-download"
	default:
		return "fixed-operation-failed"
	}
}
