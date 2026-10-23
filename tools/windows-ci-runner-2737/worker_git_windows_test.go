//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func gitCopyFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	source, destination := filepath.Join(root, "sdk-git"), filepath.Join(root, "worker-git")
	if e := os.MkdirAll(filepath.Join(source, "cmd"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(source, "cmd", "git.exe"), []byte("public synthetic tool bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	return source, destination
}

func TestWorkerGitCopiesIndependentBytesAndSealsBeforeWriting(t *testing.T) {
	source, destination := gitCopyFixture(t)
	if e := os.Link(filepath.Join(source, "cmd", "git.exe"), filepath.Join(source, "cmd", "alias.exe")); e != nil {
		t.Fatal(e)
	}
	calls := 0
	e := copyWorkerGitTree(context.Background(), source, destination, func() error {
		calls++
		entries, e := os.ReadDir(destination)
		if e != nil || len(entries) != 0 {
			t.Error("copy preceded destination sealing")
		}
		for _, path := range []string{source, destination, filepath.Dir(source)} {
			if e := os.Rename(path, path+"-replaced"); e == nil {
				t.Error("pinned directory or ancestor could be replaced")
			}
		}
		return nil
	})
	if e != nil || calls != 1 {
		t.Fatal("copy/seal failed", e, calls)
	}
	for _, leaf := range []string{"git.exe", "alias.exe"} {
		path := filepath.Join(destination, "cmd", leaf)
		b, e := os.ReadFile(path)
		if e != nil || string(b) != "public synthetic tool bytes" {
			t.Fatal("copy bytes changed", e)
		}
		in, _ := os.Stat(filepath.Join(source, "cmd", leaf))
		out, _ := os.Stat(path)
		if os.SameFile(in, out) {
			t.Fatal("destination borrowed source hardlink instead of independent bytes")
		}
	}
	if e = copyWorkerGitTree(context.Background(), source, destination, func() error { calls++; return nil }); e == nil || calls != 1 {
		t.Fatal("stale destination reused or sealed")
	}
}

func TestWorkerGitSealFailureCancellationAndOverlapStopCopy(t *testing.T) {
	for _, mode := range []string{"seal", "cancel", "overlap"} {
		t.Run(mode, func(t *testing.T) {
			source, destination := gitCopyFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			if mode == "overlap" {
				destination = filepath.Join(source, "nested")
			}
			calls := 0
			e := copyWorkerGitTree(ctx, source, destination, func() error { calls++; return errors.New("synthetic seal failure") })
			if e == nil {
				t.Fatal("rejected copy succeeded")
			}
			want := 0
			if mode == "seal" {
				want = 1
			}
			if calls != want {
				t.Fatal("rejection reached wrong phase")
			}
			if _, e := os.Lstat(filepath.Join(destination, "cmd")); !os.IsNotExist(e) {
				t.Fatal("rejected copy materialized files")
			}
		})
	}
}

func gitCopyJunction(t *testing.T, link, target string) {
	t.Helper()
	c := exec.Command(`C:\Windows\System32\cmd.exe`, "/d", "/c", "mklink", "/J", link, target)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if out, e := c.CombinedOutput(); e != nil {
		t.Fatalf("mandatory public junction fixture failed: %v %s", e, out)
	}
}

func TestWorkerGitRejectsParentAndChildJunctions(t *testing.T) {
	for _, mode := range []string{"source-parent", "source-child", "destination-parent"} {
		source, destination := gitCopyFixture(t)
		if mode == "source-parent" {
			alias := filepath.Join(filepath.Dir(source), "external-alias")
			gitCopyJunction(t, alias, filepath.Dir(source))
			source = filepath.Join(alias, filepath.Base(source))
		} else if mode == "destination-parent" {
			alias := filepath.Join(filepath.Dir(source), "destination-alias")
			gitCopyJunction(t, alias, t.TempDir())
			destination = filepath.Join(alias, "fresh-git")
		} else {
			external := t.TempDir()
			if e := os.WriteFile(filepath.Join(external, "git.exe"), []byte("public synthetic tool bytes"), 0600); e != nil {
				t.Fatal(e)
			}
			gitCopyJunction(t, filepath.Join(source, "external-alias"), external)
		}
		if e := copyWorkerGitTree(context.Background(), source, destination, func() error { return nil }); e == nil {
			t.Fatal("external identical tool bytes admitted through a junction")
		}
	}
}

func TestWorkerGitCopiesPathsBeyondWin32MaxPath(t *testing.T) {
	source, destination := gitCopyFixture(t)
	path := source
	for len(path) < 280 {
		path = filepath.Join(path, "long-path-component")
	}
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(path, "payload"), []byte("long path fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := copyWorkerGitTree(context.Background(), source, destination, func() error { return nil }); e != nil {
		t.Fatal(e)
	}
	rel, _ := filepath.Rel(source, path)
	if b, e := os.ReadFile(filepath.Join(destination, rel, "payload")); e != nil || string(b) != "long path fixture" {
		t.Fatal("long destination not copied", e)
	}
}

func TestWorkerGitHostCannotInvokeGuestStaging(t *testing.T) {
	if e := prepareWorkerGit(context.Background(), os.Environ()); e == nil {
		t.Fatal("host test process invoked guest staging")
	}
	args := strings.Join(workerGitACLArgs(), " ")
	if !strings.Contains(args, "/inheritance:r") || !strings.Contains(args, "*S-1-5-18:(OI)(CI)F") || !strings.Contains(args, "*S-1-5-32-544:(OI)(CI)F") || !strings.Contains(args, "*S-1-5-19:(OI)(CI)RX") || strings.Contains(args, "*S-1-5-19:(OI)(CI)M") {
		t.Fatal("new Git root lost SYSTEM/admin control or gained worker write access")
	}
}

func TestWorkerGitACLCommandProtectsNewTreeAndDeniesWorkerWrites(t *testing.T) {
	root := t.TempDir()
	token, e := syscall.OpenCurrentProcessToken()
	if e != nil {
		t.Fatal(e)
	}
	defer token.Close()
	user, e := token.GetTokenUser()
	if e != nil {
		t.Fatal(e)
	}
	sid, e := user.User.Sid.String()
	if e != nil {
		t.Fatal(e)
	}
	// The public fixture is owned by this test user, who retains WRITE_DAC.
	// Restore only this fixture's owner access after inspecting the sealed ACL.
	defer func() {
		c := exec.Command(`C:\Windows\System32\icacls.exe`, root, "/grant:r", "*"+sid+":(OI)(CI)F")
		c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
		if out, e := c.CombinedOutput(); e != nil {
			t.Errorf("public fixture owner access restore failed: %v %s", e, out)
		}
	}()
	pin, e := pinGitCopyEntry(root, true)
	if e != nil {
		t.Fatal(e)
	}
	defer pin.Close()
	args := workerGitACLArgs()
	args[0] = root
	words := []string{syscall.EscapeArg(`C:\Windows\System32\icacls.exe`)}
	for _, arg := range args {
		words = append(words, syscall.EscapeArg(arg))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r := runTree(ctx, `C:\Windows\System32\icacls.exe`, strings.Join(words, " "), root, os.Environ(), "")
	if r.err != nil || r.exit != 0 || !r.drained {
		t.Fatal("real Windows rejected sealed Git ACL command", r.err, r.exit)
	}
	script := `$ErrorActionPreference='Stop';$a=Get-Acl -LiteralPath '` + strings.ReplaceAll(root, "'", "''") + `';if(!$a.AreAccessRulesProtected){exit 41};$r=@($a.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier]));if($r.Count-ne3){exit 42};$seen=@{};foreach($x in $r){$id=$x.IdentityReference.Value;$seen[$id]=$true;if($x.IsInherited -or $x.AccessControlType-ne'Allow'){exit 43};if($id-eq'S-1-5-19'){$writes=2-bor4-bor16-bor64-bor256-bor65536-bor262144-bor524288;if(([int]$x.FileSystemRights-band$writes)-ne0 -or ([int]$x.FileSystemRights-band131241)-ne131241){exit 44}}elseif($id-notin@('S-1-5-18','S-1-5-32-544')){exit 45}};if(!$seen['S-1-5-19'] -or !$seen['S-1-5-18'] -or !$seen['S-1-5-32-544']){exit 46}`
	c := exec.CommandContext(ctx, `C:\Program Files\PowerShell\7\pwsh.exe`, "-NoProfile", "-NonInteractive", "-Command", script)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if out, e := c.CombinedOutput(); e != nil {
		t.Fatalf("sealed Git ACL readback failed: %v %s", e, out)
	}
}
