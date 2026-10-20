//go:build windows && amd64

package windowsrunner

import (
	"context"
	"os"
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
		if args[0] == sdkRoot && (args[1] != "/grant:r" || args[2] != "*S-1-5-19:X" || len(args) != 3) {
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
