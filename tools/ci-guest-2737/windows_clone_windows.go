package main

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	wr "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737"
	sdk "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const windowsSDKDisk = `D:\CC\Work\unattended-operations-20261013-wt\todo-2783-godd-mcp-ci-windows-guest-sdk\codex-owned-56e63ad56bbdd616f61707bf87799948\guest.vhdx`
const windowsSDKHash = "e19d76f807c012ebeff2d5d161df3540d851a447aceb9ccbf7471eb877d2d816"
const windowsSDKBytes int64 = 21277704192

// Pin exactly the bytes inspected: no write/delete sharing while checking or
// copying. This is a tool artifact, never a substitute for sourcepacket proof.
func openPinnedArtifact(path string) (*os.File, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(h), path)
	var info syscall.ByHandleFileInformation
	if e = syscall.GetFileInformationByHandle(h, &info); e != nil || info.FileAttributes&(0x400|0x10) != 0 || info.NumberOfLinks != 1 {
		f.Close()
		return nil, errors.New("artifact is not one regular physical file")
	}
	return f, nil
}
func verifyWindowsHelper(path string, executor map[string]any) error {
	f, e := openPinnedArtifact(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return windowsHelperBuild(f, executor)
}
func windowsHelperBuild(f *os.File, executor map[string]any) error {
	b, e := buildinfo.Read(f)
	if e != nil || b.Main.Path != "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737" || b.Path != "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737/cmd/windows-ci-runner-agent" {
		return errors.New("fixed Windows helper module rejected")
	}
	revision, clean, arch, osName := "", false, "", ""
	for _, s := range b.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			clean = s.Value == "false"
		case "GOARCH":
			arch = s.Value
		case "GOOS":
			osName = s.Value
		}
	}
	if !clean || revision != executor["executor_code_sha"] || arch != "amd64" || osName != "windows" {
		return errors.New("Windows helper must be a clean same-revision Windows build")
	}
	return nil
}
func copyPinnedFile(ctx context.Context, in *os.File, destination string, limit int64) (int64, string, error) {
	if _, e := in.Seek(0, io.SeekStart); e != nil {
		return 0, "", e
	}
	out, e := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return 0, "", e
	}
	n, e := io.Copy(out, io.LimitReader(&contextReader{ctx: ctx, r: in}, limit+1))
	e = errors.Join(e, out.Sync(), out.Close())
	if e != nil || n < 1 || n > limit {
		return n, "", errors.New("artifact copy incomplete")
	}
	saved, e := openPinnedArtifact(destination)
	if e != nil {
		return n, "", e
	}
	defer saved.Close()
	sha, e := hashReader(saved)
	return n, sha, e
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}

type windowsCloneLease struct{ StageStarted bool }

func prepareWindowsClone(ctx context.Context, root, helper string, bootstrapSeed, runnerSeed []byte, lease *windowsCloneLease) error {
	if lease == nil {
		return errors.New("Windows clone lease absent")
	}
	executor, e := linkedExecutorIdentity()
	if e != nil {
		return e
	}
	f, e := openPinnedArtifact(helper)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = windowsHelperBuild(f, executor); e != nil {
		return e
	}
	_, helperHash, e := copyPinnedFile(ctx, f, filepath.Join(root, "windows-ci-runner-agent.exe"), 64<<20)
	if e != nil {
		return e
	}
	if e = writeNew(filepath.Join(root, "seed.json"), bootstrapSeed); e != nil {
		return e
	}
	if e = writeNew(filepath.Join(root, "runner-seed.json"), runnerSeed); e != nil {
		return e
	}
	// Read/copy the immutable received asset. Hash the actual clone before any
	// offline edits; never mount the retained original or choose another image.
	disk, e := openPinnedArtifact(windowsSDKDisk)
	if e != nil {
		return e
	}
	n, sha, e := copyPinnedFile(ctx, disk, filepath.Join(root, "guest.vhdx"), windowsSDKBytes)
	_ = disk.Close()
	if e != nil || n != windowsSDKBytes || sha != windowsSDKHash {
		return errors.New("Windows SDK clone digest rejected")
	}
	if e = record(root, "windows-clone-source.private.json", map[string]any{"bytes": n, "sha256": sha, "helper_sha256": helperHash, "original_modified": false}); e != nil {
		return e
	}
	if e = downloadWindowsRunner(ctx, root); e != nil {
		return e
	}
	// Persist BEFORE the broker can mount the clone. A failed/lost detach reply
	// remains a recovery obligation even when New-VM has never been called.
	if e = record(root, "windows-clone-stage-intent.private.json", map[string]any{"clone": filepath.Join(root, "guest.vhdx"), "stage_started": true}); e != nil {
		return e
	}
	lease.StageStarted = true
	return admin(ctx, root, "windows-clone-stage", windowsOfflineStageScript(root))
}

func windowsCloneDetachedScript(root string) string {
	return "Import-Module Hyper-V\n$root=" + psQuote(root) + "\n" + `
$disk=Join-Path $root 'guest.vhdx'
$hive='CC2737_'+[IO.Path]::GetFileName($root).Replace('-','_')
if(Test-Path -LiteralPath ("Registry::HKEY_LOCAL_MACHINE\"+$hive)){throw 'CLONE_HIVE_STILL_LOADED'}
if(-not (Test-Path -LiteralPath $disk -PathType Leaf)){throw 'CLONE_DISK_ABSENT'}
$states=@(Get-VHD -Path $disk -ErrorAction Stop)
if($states.Count -ne 1 -or $states[0].Attached -isnot [bool]){throw 'CLONE_ATTACHMENT_STATE_UNAVAILABLE'}
if($states[0].Attached){throw 'CLONE_STILL_ATTACHED'}
`
}

func observeWindowsCloneDetached(ctx context.Context, root string) error {
	// This read-only broker flight is queued after staging/VM cleanup; it never
	// retries a failed unload/dismount or infers detach from a missing receipt.
	return admin(ctx, root, "windows-clone-observe-"+newID(), windowsCloneDetachedScript(root))
}

func reconcileWindowsClone(lease windowsCloneLease, observe func() error) error {
	if !lease.StageStarted {
		return nil
	}
	return observe()
}
func downloadWindowsRunner(ctx context.Context, root string) error {
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, wr.RunnerURL, nil)
	if e != nil {
		return e
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	r, e := client.Do(request)
	if e != nil {
		return errors.New("public Windows runner download unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return errors.New("public Windows runner download rejected")
	}
	f, e := os.OpenFile(filepath.Join(root, "actions-runner-win-x64-2.338.0.zip"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	n, e := io.Copy(f, io.LimitReader(r.Body, 768<<20+1))
	e = errors.Join(e, f.Sync(), f.Close())
	if e != nil || n < 1 || n > 768<<20 {
		return errors.New("public Windows runner download incomplete")
	}
	f, e = openPinnedArtifact(filepath.Join(root, "actions-runner-win-x64-2.338.0.zip"))
	if e != nil {
		return e
	}
	defer f.Close()
	sha, e := hashReader(f)
	if e != nil || sha != wr.RunnerSHA256 {
		return errors.New("public Windows runner digest rejected")
	}
	return record(root, "windows-runner-payload.private.json", map[string]any{"bytes": n, "sha256": sha})
}
func windowsOfflineStageScript(root string) string {
	return "Import-Module Hyper-V\n$root=" + psQuote(root) + "\n" + `
$disk=Join-Path $root 'guest.vhdx'
if((Get-VHD -Path $disk -ErrorAction Stop).Attached){throw 'CLONE_ALREADY_ATTACHED'}
$mounted=$null;$hiveLoaded=$false
try {
 $mounted=Mount-VHD -Path $disk -Passthru -ErrorAction Stop
 $volumes=@($mounted|Get-Disk|Get-Partition|Get-Volume|Where-Object {$_.DriveLetter -and (Test-Path -LiteralPath ($_.DriveLetter+':\Windows\System32\config\SYSTEM'))})
 if($volumes.Count -ne 1){throw 'CLONE_SYSTEM_VOLUME_AMBIGUOUS'}
 $volume=$volumes[0].DriveLetter+':\'
 $helper=Join-Path $volume 'Windows\System32\WindowsCISDK2737'
 $state=Join-Path $volume 'ProgramData\AutoDevJapan\windows-ci-sdk-2737'
 if(-not (Test-Path -LiteralPath $helper -PathType Container)){throw 'CLONE_HELPER_ABSENT'}
 foreach($name in @('runner','runner-hooks','private\runner-listener.log')){if(Test-Path -LiteralPath (Join-Path $state $name)){throw 'CLONE_RUNNER_STATE_ALREADY_EXISTS'}}
 foreach($name in @('windows-ci-runner-agent.exe','runner-seed.json','actions-runner-win-x64-2.338.0.zip')){if(Test-Path -LiteralPath (Join-Path $helper $name)){throw 'CLONE_STAGED_FILE_ALREADY_EXISTS'}}
	foreach($name in @('windows-ci-runner-agent.exe','runner-seed.json','actions-runner-win-x64-2.338.0.zip')){Copy-Item -LiteralPath (Join-Path $root $name) -Destination (Join-Path $helper $name) -ErrorAction Stop}
` + windowsOfflineSeedReplacementScript() + `
 $hive='CC2737_'+[IO.Path]::GetFileName($root).Replace('-','_')
 & "$env:SystemRoot\System32\reg.exe" load "HKLM\$hive" (Join-Path $volume 'Windows\System32\config\SYSTEM') | Out-Null
 if($LASTEXITCODE -ne 0){throw 'CLONE_HIVE_LOAD_FAILED'}
 $hiveLoaded=$true
 $current=(Get-ItemProperty -LiteralPath "Registry::HKEY_LOCAL_MACHINE\$hive\Select" -Name Current -ErrorAction Stop).Current
 if($current -lt 1 -or $current -gt 9){throw 'CLONE_CONTROLSET_REJECTED'}
 $controlSet="Registry::HKEY_LOCAL_MACHINE\$hive\ControlSet$($current.ToString('000'))"
 $service=$controlSet+'\Services\WindowsCISDK2737'
 $value=Get-ItemProperty -LiteralPath $service -ErrorAction Stop
 if($value.Start -ne 2 -or $value.Type -ne 16 -or $value.ObjectName -ne 'LocalSystem'){throw 'CLONE_SERVICE_IDENTITY_REJECTED'}
 Set-ItemProperty -LiteralPath $service -Name ImagePath -Value '"C:\Windows\System32\WindowsCISDK2737\windows-ci-runner-agent.exe"' -ErrorAction Stop
 # Match the official Windows runner image before any guest process starts.
 # This path belongs to the loaded clone hive, never host CurrentControlSet.
 $fileSystem=$controlSet+'\Control\FileSystem'
 New-ItemProperty -LiteralPath $fileSystem -Name LongPathsEnabled -PropertyType DWord -Value 1 -Force -ErrorAction Stop | Out-Null
 $fileSystemKey=Get-Item -LiteralPath $fileSystem -ErrorAction Stop
 try {
  if($fileSystemKey.GetValueKind('LongPathsEnabled') -ne [Microsoft.Win32.RegistryValueKind]::DWord -or $fileSystemKey.GetValue('LongPathsEnabled') -ne 1){throw 'CLONE_LONG_PATHS_UNCONFIRMED'}
 } finally {$fileSystemKey.Close()}
 $value=$null
}finally {
 try {if($hiveLoaded){[GC]::Collect();[GC]::WaitForPendingFinalizers(); & "$env:SystemRoot\System32\reg.exe" unload "HKLM\$hive" | Out-Null; if($LASTEXITCODE -ne 0){throw 'CLONE_HIVE_UNLOAD_FAILED'}}}
 finally {if($null -ne $mounted){Dismount-VHD -Path $disk -ErrorAction Stop}}
}

if((Get-VHD -Path $disk -ErrorAction Stop).Attached){throw 'CLONE_DETACH_UNCONFIRMED'}
`
}
func windowsOfflineSeedReplacementScript() string {
	return `
$seedPath=Join-Path $helper 'seed.json'
$seedItem=Get-Item -LiteralPath $seedPath -ErrorAction Stop
if($seedItem.PSIsContainer -or (($seedItem.Attributes -band [IO.FileAttributes]::ReparsePoint)-ne0)){throw 'CLONE_SEED_NOT_REGULAR'}
$seedAttributes=$seedItem.Attributes
try {
 if($seedItem.IsReadOnly){$seedItem.IsReadOnly=$false}
 Copy-Item -LiteralPath (Join-Path $root 'seed.json') -Destination $seedPath -ErrorAction Stop
} finally {[IO.File]::SetAttributes($seedPath,$seedAttributes)}
`
}
func windowsBootstrap(ctx context.Context, root, instance string) (sdk.BootstrapResult, error) {
	exe, e := os.Executable()
	if e != nil {
		return sdk.BootstrapResult{}, e
	}
	file := filepath.Join(root, "windows-bootstrap.private.json")
	if e = admin(ctx, root, "windows-bootstrap", "& "+psQuote(exe)+" windows-serial "+psQuote(instance)+" "+psQuote(file)+"\nif($LASTEXITCODE -ne 0){throw 'WINDOWS_COM_FAILED'}\n"); e != nil {
		return sdk.BootstrapResult{}, e
	}
	b, e := os.ReadFile(file)
	var result struct {
		Instance string
		Boot     sdk.BootstrapResult
	}
	if e != nil || json.Unmarshal(b, &result) != nil || result.Instance != instance || result.Boot.IPv4.To4() == nil || !result.Boot.IPv4.IsPrivate() || len(result.Boot.ServerSHA256) != 64 {
		return result.Boot, errors.New("Windows bootstrap receipt rejected")
	}
	return result.Boot, nil
}
func readWindowsSerial(ctx context.Context, instance, file string) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	for ctx.Err() == nil {
		pipe, e := sdk.OpenOwnedGuestCOMPipe(ctx, instance)
		if e != nil {
			return e
		}
		boot, e := sdk.SendCOMBootstrap(pipe, instance)
		_ = pipe.Close()
		if e == nil {
			b, e := json.Marshal(struct {
				Instance string
				Boot     sdk.BootstrapResult
			}{instance, boot})
			if e != nil {
				return e
			}
			return writeNew(file, b)
		}
		if !strings.Contains(e.Error(), "COM bootstrap response unavailable") {
			return windowsCOMRejection(e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return ctx.Err()
}

func windowsCOMRejection(e error) error {
	if diagnostic := sdk.COMBootstrapRejectionDiagnostic(e); diagnostic != "" {
		return errors.New("Windows COM bootstrap rejected: " + diagnostic)
	}
	return errors.New("Windows COM bootstrap rejected")
}
