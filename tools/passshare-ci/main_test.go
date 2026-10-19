package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func originals(t *testing.T) (object, object) {
	t.Helper()
	read := func(name string) object {
		b, err := os.ReadFile(filepath.Join("testdata", "before", name))
		if err != nil {
			t.Fatal(err)
		}
		v, err := decode(b)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	return read("quality-gate.yml"), read("mobile-build.yml")
}

func TestPreservesIndependentJobsAndAcceptance(t *testing.T) {
	q, m := originals(t)
	before, _ := originals(t)
	originalMobile := before["jobs"].(object)["mobile-smoke"].(object)
	if err := transform(q, m); err != nil {
		t.Fatal(err)
	}
	if err := unchangedJobs(before, q); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m["on"], object{"workflow_call": nil}) {
		t.Fatal("standalone mobile events would duplicate the call")
	}
	jobs := q["jobs"].(object)
	if jobs["mobile-smoke"].(object)["uses"] != "./.github/workflows/mobile-build.yml" {
		t.Fatal("missing reusable call")
	}
	steps := m["jobs"].(object)["smoke"].(object)["steps"].([]any)
	for _, raw := range originalMobile["steps"].([]any) {
		step := raw.(object)
		if _, hasCommand := step["run"]; !hasCommand {
			continue
		}
		var actual object
		for _, s := range steps {
			if s.(object)["name"] == step["name"] {
				actual = s.(object)
			}
		}
		if actual == nil {
			t.Fatalf("mobile step lost: %v", step["name"])
		}
		expected := step["run"].(string)
		if step["name"] == "Verify actual React auth against delayed logout and lifecycle results" {
			expected = strings.TrimPrefix(expected, "rustfmt --edition 2021 --check crates/passshare-core/examples/mobile_auth_acceptance.rs\n")
		}
		if actual["run"] != expected {
			t.Fatalf("mobile command weakened: %v", step["name"])
		}
		for _, key := range []string{"env", "working-directory", "shell"} {
			if !reflect.DeepEqual(step[key], actual[key]) {
				t.Fatalf("mobile conditions changed: %v/%s", step["name"], key)
			}
		}
	}
	// An injected unrelated env/command alteration must be detected.
	jobs["backend"].(object)["env"] = object{"PASSSHARE_MUTATIONS_BIN": "wrong"}
	if err := unchangedJobs(before, q); err == nil {
		t.Fatal("unrelated backend alteration was accepted")
	}
}

func scratch(t *testing.T) string {
	t.Helper()
	parent := os.Getenv("TEST_SCRATCH")
	if parent == "" {
		t.Fatal("TEST_SCRATCH must identify the owned safe-delete root")
	}
	p, err := os.MkdirTemp(parent, "passshare-ci-")
	if err != nil {
		t.Fatal(err)
	}
	return p // The caller reclaims the recorded root using safe-delete.
}

func bashPath(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return `C:\Program Files\Git\bin\bash.exe`
	}
	p, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func TestActualWorkflowChangeSelection(t *testing.T) {
	// Execute the actual emitted workflow step, not an independent model of it.
	q, m := originals(t)
	if err := transform(q, m); err != nil {
		t.Fatal(err)
	}
	step := q["jobs"].(object)["preflight"].(object)["steps"].([]any)[1].(object)
	script := step["run"].(string)
	tests := []struct {
		name, path      string
		desktop, mobile bool
	}{
		{"desktop", "apps/desktop/src/space name.ts", true, false},
		{"mobile", "apps/mobile/src/app.tsx", false, true},
		{"backend", "backend/test/mobile-auth.test.js", true, true},
		{"shared", "shared/test/connect-session.test.ts", true, true},
		{"lock", "pnpm-lock.yaml", true, true},
		{"rustlock", "Cargo.lock", true, true},
		{"toolchain", "rust-toolchain.toml", true, true},
		{"workflow", ".github/workflows/mobile-build.yml", true, true},
		{"docs-consumer", "documents/spec/mobile-ui.md", true, true},
		{"unknown", "future/config", true, true},
		{"frontend-only", "frontend/src/app.tsx", false, false},
		{"extension-only", "packages/extension/test/test.ts", false, false},
		{"deleted", "apps/mobile/deleted.ts", false, true},
		{"rename-across-clients", "apps/desktop/old.ts", true, true},
		{"multi-commit-push", "apps/mobile/a.ts", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := scratch(t)
			git(t, dir, "init", "-q")
			git(t, dir, "config", "user.name", "CI test")
			git(t, dir, "config", "user.email", "ci-test@example.invalid")
			write := func(p string) {
				full := filepath.Join(dir, p)
				if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("fixture\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("initial")
			if tc.name == "deleted" || tc.name == "rename-across-clients" {
				write(tc.path)
			}
			git(t, dir, "add", ".")
			git(t, dir, "commit", "-qm", "base")
			base := git(t, dir, "rev-parse", "HEAD")
			if tc.name == "deleted" {
				git(t, dir, "rm", tc.path)
			} else if tc.name == "rename-across-clients" {
				if err := os.MkdirAll(filepath.Join(dir, "apps/mobile"), 0755); err != nil {
					t.Fatal(err)
				}
				git(t, dir, "mv", tc.path, "apps/mobile/new.ts")
			} else {
				write(tc.path)
			}
			git(t, dir, "add", ".")
			git(t, dir, "commit", "-qm", "candidate")
			if tc.name == "multi-commit-push" {
				write("apps/desktop/b.ts")
				git(t, dir, "add", ".")
				git(t, dir, "commit", "-qm", "second")
			}
			head := git(t, dir, "rev-parse", "HEAD")
			for _, testBase := range []string{base, strings.Repeat("0", 40), strings.Repeat("f", 40)} {
				out := filepath.Join(dir, "scope-output")
				c := exec.Command(bashPath(t), "-c", script)
				c.Dir = dir
				c.Env = append(os.Environ(), "BASE_SHA="+testBase, "GITHUB_SHA="+head, "RUNNER_TEMP="+filepath.ToSlash(dir), "GITHUB_OUTPUT="+filepath.ToSlash(out), "GITHUB_STEP_SUMMARY="+filepath.ToSlash(filepath.Join(dir, "summary")))
				b, err := c.CombinedOutput()
				if testBase == strings.Repeat("f", 40) {
					if err == nil {
						t.Fatal("unknown base must fail instead of skip")
					}
					continue
				}
				if err != nil {
					t.Fatalf("workflow selection: %v %s", err, b)
				}
				data, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				want := "desktop=" + fmtBool(tc.desktop) + "\nmobile=" + fmtBool(tc.mobile) + "\n"
				if testBase == strings.Repeat("0", 40) {
					want += "desktop=true\nmobile=true\n"
				}
				if string(data) != want {
					t.Fatalf("scope: got %q want %q", data, want)
				}
			}
		})
	}
}

func fmtBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
