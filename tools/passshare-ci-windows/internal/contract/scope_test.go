package contract

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func scopeScript(t *testing.T) string {
	t.Helper()
	n, e := parse(DesktopWorkflow)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range field(field(field(n, "jobs"), "admission"), "steps").Content {
		if id := field(s, "id"); id != nil && id.Value == "scope" {
			return field(s, "run").Value
		}
	}
	t.Fatal("scope step absent")
	return ""
}
func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", args...)
	hideTest(c)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, "absent-config"), "GIT_TERMINAL_PROMPT=0")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func TestRealGitScope(t *testing.T) {
	bash := "bash"
	if runtime.GOOS == "windows" {
		bash = `C:\Program Files\Git\bin\bash.exe`
	}
	if _, e := os.Stat(bash); runtime.GOOS == "windows" && e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, path, event, base, want string
		bad, rename                   bool
	}{
		{name: "root lock", path: "Cargo.lock", want: "true"},
		{name: "toolchain", path: "rust-toolchain.toml", want: "true"},
		{name: "shared", path: "shared/ui/a.ts", want: "true"},
		{name: "crate", path: "crates/core/a.rs", want: "true"},
		{name: "package", path: "packages/connect-js/a.ts", want: "true"},
		{name: "workflow", path: ".github/workflows/desktop-build.yml", want: "true"},
		{name: "runtime document", path: "documents/sync.json", want: "true"},
		{name: "script", path: "scripts/build.mjs", want: "true"},
		{name: "unknown", path: "new-input", want: "true"},
		{name: "frontend only", path: "frontend/a.ts", want: "false"},
		{name: "move from desktop", path: "frontend/a.ts", rename: true, want: "true"},
		{name: "first push", path: "frontend/a.ts", base: strings.Repeat("0", 40), want: "true"},
		{name: "bad comparison", path: "frontend/a.ts", base: strings.Repeat("c", 40), bad: true},
		{name: "missing comparison", path: "frontend/a.ts", base: "bad", bad: true},
		{name: "PR stays hosted", path: "Cargo.lock", event: "pull_request", want: "false"},
		{name: "PR bad comparison fails", path: "Cargo.lock", event: "pull_request", base: "bad", bad: true},
		{name: "manual candidate", path: "frontend/a.ts", event: "workflow_dispatch", want: "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gitFixture(t, dir, "init", "--quiet")
			gitFixture(t, dir, "config", "user.name", "CI fixture")
			gitFixture(t, dir, "config", "user.email", "fixture@example.invalid")
			original := filepath.Join(dir, "apps", "desktop", "a")
			if e := os.MkdirAll(filepath.Dir(original), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(original, []byte("before"), 0600); e != nil {
				t.Fatal(e)
			}
			gitFixture(t, dir, "add", ".")
			gitFixture(t, dir, "commit", "--quiet", "-m", "base")
			base := gitFixture(t, dir, "rev-parse", "HEAD")
			dest := filepath.Join(dir, filepath.FromSlash(tc.path))
			if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
				t.Fatal(e)
			}
			if tc.rename {
				if e := os.Rename(original, dest); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.WriteFile(dest, []byte("after"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			gitFixture(t, dir, "add", "-A")
			gitFixture(t, dir, "commit", "--quiet", "-m", "change")
			head := gitFixture(t, dir, "rev-parse", "HEAD")
			if tc.base != "" {
				base = tc.base
			}
			event := tc.event
			if event == "" {
				event = "push"
			}
			output := filepath.Join(dir, "output")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			c := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-c", scopeScript(t))
			hideTest(c)
			c.Dir = dir
			c.Env = append(os.Environ(), "EVENT="+event, "BASE_SHA="+base, "GITHUB_SHA="+head, "GITHUB_OUTPUT="+filepath.ToSlash(output), "RUNNER_TEMP="+filepath.ToSlash(dir))
			raw, e := c.CombinedOutput()
			if tc.bad {
				if e == nil {
					t.Fatal("comparison failure skipped successfully")
				}
				return
			}
			if e != nil {
				t.Fatalf("scope: %v %s", e, raw)
			}
			b, e := os.ReadFile(output)
			if e != nil {
				t.Fatal(e)
			}
			if strings.TrimSpace(string(b)) != "build="+tc.want {
				t.Fatalf("scope output %s", b)
			}
		})
	}
}
