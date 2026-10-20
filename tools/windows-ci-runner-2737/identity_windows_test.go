//go:build windows && amd64

package windowsrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunnerRejectsMissingAndDifferentUserBeforeProcessCreation(t *testing.T) {
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
	if sid == runnerUserSID {
		t.Fatal("this denial fixture requires a different user")
	}
	for _, candidate := range []syscall.Token{0, token} {
		root := t.TempDir()
		sentinel := filepath.Join(root, "unexpected.txt")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		line := `"C:\Windows\System32\cmd.exe" /d /s /c "echo public-fixture>"` + sentinel + `""`
		r := runRunnerTree(ctx, `C:\Windows\System32\cmd.exe`, line, root, os.Environ(), "", candidate)
		cancel()
		if r.err == nil {
			t.Fatal("wrong identity was accepted")
		}
		if _, e := os.Lstat(sentinel); !os.IsNotExist(e) {
			t.Fatal("rejected runner still executed its command")
		}
	}
}

func TestRunnerRootTraverseGrantIsAcceptedByWindowsAndDoesNotInherit(t *testing.T) {
	root := t.TempDir()
	args := append([]string(nil), runnerAccessCommands()[0]...)
	if args[0] != sdkRoot {
		t.Fatal("first ACL command changed")
	}
	// Redirect only the target into this public fixture; execute the exact
	// generated rights/flags using the real Windows ACL tool and process helper.
	args[0] = root
	const exe = `C:\Windows\System32\icacls.exe`
	words := []string{syscall.EscapeArg(exe)}
	for _, arg := range args {
		words = append(words, syscall.EscapeArg(arg))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := runTree(ctx, exe, strings.Join(words, " "), root, os.Environ(), filepath.Join(root, "public-acl.log"))
	if r.err != nil || r.exit != 0 || !r.drained {
		t.Fatalf("Windows rejected generated traverse grant: exit=%d err=%v", r.exit, r.err)
	}
	script := `$ErrorActionPreference='Stop';$acl=Get-Acl -LiteralPath '` + strings.ReplaceAll(root, "'", "''") + `';$rules=@($acl.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier])|Where-Object {$_.IdentityReference.Value -eq 'S-1-5-19'});if($rules.Count -ne 1){exit 41};$r=$rules[0];$bits=[int]$r.FileSystemRights;if(($bits -band 32)-ne32 -or ($bits -band (-bnot 1048608))-ne0 -or $r.InheritanceFlags-ne0 -or $r.PropagationFlags-ne0 -or $r.AccessControlType-ne[Security.AccessControl.AccessControlType]::Allow){exit 42}`
	c := exec.CommandContext(ctx, `C:\Program Files\PowerShell\7\pwsh.exe`, "-NoProfile", "-NonInteractive", "-Command", script)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if out, e := c.CombinedOutput(); e != nil {
		t.Fatalf("traverse grant was broader/inherited: %v\n%s", e, out)
	}
}

func TestRunnerAccessHasNoInheritedSDKRootOrPrivateGrant(t *testing.T) {
	commands := runnerAccessCommands()
	seen := map[string]bool{}
	for _, args := range commands {
		if len(args) < 3 || seen[args[0]] {
			t.Fatal("ACL command absent/ambiguous")
		}
		seen[args[0]] = true
		joined := strings.Join(args, " ")
		if strings.Contains(joined, ":(OI)(CI)F") || strings.Contains(joined, "Everyone") || strings.Contains(joined, "S-1-5-32-545") {
			t.Fatal("runner gained broad rights")
		}
		if args[0] == sdkRoot && (args[1] != "/grant:r" || args[2] != "*S-1-5-19:(X)" || len(args) != 3) {
			t.Fatal("SDK root grant inherited")
		}
		if args[0] == sdkRoot+`\private` || args[0] == sdkRoot+`\downloads` || args[0] == agentRoot {
			t.Fatal("service private/bootstrap exposed")
		}
		if args[0] == sourceRoot && (strings.Join(args[1:], " ") != "/setowner *S-1-5-19 /T") {
			t.Fatal("checkout owner not its executor")
		}
	}
	for _, path := range []string{sdkRoot, helperPath, hookRoot, runnerRoot, sourceRoot, sdkRoot + `\rust-1.97.1`, sdkRoot + `\vs-build-tools`, sdkRoot + `\staging`, sdkRoot + `\target`} {
		if !seen[path] {
			t.Fatal("required fixed runtime path absent")
		}
	}
}

func TestRunnerTokenClosesOnceAfterSession(t *testing.T) {
	token, e := syscall.OpenCurrentProcessToken()
	if e != nil {
		t.Fatal(e)
	}
	o := &windowsOps{runnerToken: token, closed: make(chan struct{})}
	if e := o.close(); e != nil {
		t.Fatal(e)
	}
	if e := o.close(); e != nil {
		t.Fatal("second close changed the result")
	}
	if _, e := token.GetTokenUser(); e == nil {
		t.Fatal("session token leaked")
	}
	if o.runnerToken != 0 {
		t.Fatal("closed token retained")
	}
}
