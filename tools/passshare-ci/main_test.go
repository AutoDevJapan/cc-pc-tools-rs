package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("PASSSHARE_DOCKER_STUB") == "1" {
		args := os.Args[1:]
		if len(args) > 3 && args[3] == "config" {
			fmtText := os.Getenv("PASSSHARE_COMPOSE_CONFIG")
			_, _ = os.Stdout.WriteString(fmtText)
			os.Exit(0)
		}
		b, err := json.Marshal(args)
		if err == nil {
			err = os.WriteFile(os.Getenv("PASSSHARE_DOCKER_CAPTURE"), b, 0644)
		}
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAggregateRejectsFailedOrUnexpectedlySkippedChecks(t *testing.T) {
	q, m := originals(t)
	if err := transform(q, m); err != nil {
		t.Fatal(err)
	}
	script := q["jobs"].(object)["gate"].(object)["steps"].([]any)[0].(object)["run"].(string)
	for _, tc := range []struct {
		name, client, goSDK, preflight, desktop string
		pass                                    bool
	}{
		{"all-success", "success", "success", "success", "true", true},
		{"unaffected-desktop", "skipped", "success", "success", "false", true},
		{"unexpected-skip", "skipped", "success", "success", "true", false},
		{"failed-desktop", "failure", "success", "success", "true", false},
		{"failed-go-matrix", "success", "failure", "success", "true", false},
		{"failed-preflight", "skipped", "skipped", "failure", "false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results, err := json.Marshal(object{"desktop-smoke": object{"result": tc.client}, "mobile-smoke": object{"result": "success"}, "connect-go": object{"result": tc.goSDK}, "preflight": object{"result": tc.preflight}})
			if err != nil {
				t.Fatal(err)
			}
			c := exec.Command(bashPath(t), "-c", script)
			c.Env = append(os.Environ(), "RESULTS="+string(results), "DESKTOP="+tc.desktop, "MOBILE=true")
			output, err := c.CombinedOutput()
			if (err == nil) != tc.pass {
				t.Fatalf("aggregate pass=%t expected=%t: %v %s", err == nil, tc.pass, err, output)
			}
		})
	}
}

func TestSerialE2EPreservesTheComposeCommandAndTestList(t *testing.T) {
	original := "cargo build -p op-cli --release && cargo build --manifest-path backend/native/Cargo.toml --release --locked --bin passshare-mutations && pnpm --dir backend install --frozen-lockfile && node --test backend/test/pg-integration.test.js backend/test/cross-client.integration.test.js backend/test/future-added.integration.test.js"
	for _, tc := range []struct {
		script             string
		valid, unsupported bool
	}{
		{original, true, false}, {original + " && node --test extra.js", true, false},
		{"echo $HOME && " + original, false, false}, {original, false, true},
	} {
		original, valid := tc.script, tc.valid
		dir := scratch(t)
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		name := "docker"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0755); err != nil {
			t.Fatal(err)
		}
		command := []string{"sh", "-c", original}
		if tc.unsupported {
			command = []string{"unsupported", original}
		}
		config, err := json.Marshal(object{"services": object{"backend-e2e": object{"command": command}}})
		if err != nil {
			t.Fatal(err)
		}
		capture := filepath.Join(dir, "capture.json")
		c := exec.Command(bashPath(t), "-c", serialE2E)
		c.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "PASSSHARE_DOCKER_STUB=1", "PASSSHARE_COMPOSE_CONFIG="+string(config), "PASSSHARE_DOCKER_CAPTURE="+capture)
		out, err := c.CombinedOutput()
		if !valid {
			if err == nil {
				t.Fatalf("unsupported Compose command accepted: %s", out)
			}
			if _, err := os.Stat(capture); !os.IsNotExist(err) {
				t.Fatal("unsupported config reached the run command")
			}
			continue
		}
		if err != nil {
			t.Fatalf("serial workflow: %v %s", err, out)
		}
		data, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		var args []string
		if err := json.Unmarshal(data, &args); err != nil {
			t.Fatal(err)
		}
		want := []string{"compose", "-f", "docker/docker-compose.test.yml", "run", "--rm", "backend-e2e", "sh", "-c", strings.ReplaceAll(original, "node --test ", "node --test --test-concurrency=1 ")}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("Compose build/acceptance command changed: got %q want %q", args, want)
		}
	}
}

func TestGeneratedYAMLPreservesCommentsAndExactStructure(t *testing.T) {
	q, m := originals(t)
	if err := transform(q, m); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]object{"quality-gate.yml": q, "mobile-build.yml": m} {
		b, err := os.ReadFile(filepath.Join("testdata", "before", name))
		if err != nil {
			t.Fatal(err)
		}
		out, err := encodePreserving(b, value)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := decode(out)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(value, actual) {
			t.Fatalf("comment-preserving emission changed %s semantics", name)
		}
		if !strings.Contains(string(out), "# v7.0.0") {
			t.Fatalf("action version comment lost in %s", name)
		}
		if name == "quality-gate.yml" && !strings.Contains(string(out), "#320") {
			t.Fatal("integration-test rationale lost")
		}
	}
}

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
	gateNeeds := jobs["gate"].(object)["needs"].([]any)
	covered := map[string]bool{}
	for _, n := range gateNeeds {
		covered[n.(string)] = true
	}
	for name := range jobs {
		if name != "gate" && !covered[name] {
			t.Fatalf("aggregate omits job %s", name)
		}
	}
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
	c.Env = append(os.Environ(), "GIT_DIR="+dir+".gitdb", "GIT_WORK_TREE="+dir)
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
		{"cloud-manifest", "frontend/cloud-assets.json", true, true},
		{"cloud-wasm", "frontend/public/cloud-crypto/passshare_cloud_wasm_bg.wasm", true, true},
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
				c.Env = append(c.Env, "GIT_DIR="+filepath.ToSlash(dir)+".gitdb", "GIT_WORK_TREE="+filepath.ToSlash(dir))
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
			// A later main push must still cover a prior cancelled client's changes.
			out := filepath.Join(dir, "push-output")
			c := exec.Command(bashPath(t), "-c", script)
			c.Dir = dir
			c.Env = append(os.Environ(), "EVENT_NAME=push", "BASE_SHA="+head, "GITHUB_SHA="+head,
				"RUNNER_TEMP="+filepath.ToSlash(dir), "GITHUB_OUTPUT="+filepath.ToSlash(out),
				"GITHUB_STEP_SUMMARY="+filepath.ToSlash(filepath.Join(dir, "summary")))
			if output, err := c.CombinedOutput(); err != nil {
				t.Fatalf("push selection: %v %s", err, output)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "desktop=true\nmobile=true\n" {
				t.Fatalf("push omitted cancelled scope: %s", data)
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
