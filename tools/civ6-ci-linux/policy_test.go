package main

import (
	"os"
	"strings"
	"testing"
)

func fixtureWorkflow(t *testing.T) string {
	t.Helper()
	b, e := os.ReadFile("testdata/rust-linux.yml")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

func TestWorkflowExecutionFromReviewedBytes(t *testing.T) {
	p, e := workflowExecution([]byte(fixtureWorkflow(t)))
	if e != nil {
		t.Fatal(e)
	}
	if p.TimeoutMinutes != 360 || p.timeout() == 0 {
		t.Fatalf("GitHub's default job limit not preserved: %d", p.TimeoutMinutes)
	}
	for i, want := range []bool{true, true, false, true, true} {
		if p.Present[i] != want {
			t.Fatalf("command %d present=%v", i, p.Present[i])
		}
		if want && p.Steps[i].Dir != workspace {
			t.Fatalf("command %d dir=%s", i, p.Steps[i].Dir)
		}
	}
	// An optional doctest step is recognised only with its exact argv.
	withDoc := strings.Replace(fixtureWorkflow(t), "      - name: Build all tools\n", "      - name: Doc tests\n        run: cargo test --locked --manifest-path tools/Cargo.toml --workspace --doc\n      - name: Build all tools\n", 1)
	q, e := workflowExecution([]byte(withDoc))
	if e != nil || !q.Present[cmdDoc] {
		t.Fatalf("doctest step: %v", e)
	}
	if q.index(cargoTable[cmdDoc], false) != cmdDoc || q.index(cargoTable[cmdDoc], true) != -1 || p.index(cargoTable[cmdDoc], false) != -1 {
		t.Fatal("command index does not follow the reviewed workflow")
	}
	if p.index(rustupArgs, true) != cmdRustup || p.index(rustupArgs, false) != -1 || p.index(append(append([]string{}, cargoTable[cmdTest]...), "--config", "x"), false) != -1 {
		t.Fatal("rustup/extra argv mishandled")
	}
}

func TestWorkflowExecutionRejectsSubstitutes(t *testing.T) {
	base := fixtureWorkflow(t)
	fmtLine := "run: cargo fmt --manifest-path tools/Cargo.toml --all -- --check"
	expr := "$" + "{{ secrets.X }}"
	cases := map[string]string{
		"changed argv":     strings.Replace(base, fmtLine, "run: cargo fmt --manifest-path tools/Cargo.toml --all", 1),
		"chained command":  strings.Replace(base, fmtLine, fmtLine+" && curl http://example.invalid", 1),
		"extra cargo line": strings.Replace(base, "      - name: Build all tools\n", "      - run: cargo install evil\n      - name: Build all tools\n", 1),
		"protected env":    strings.Replace(base, "jobs:\n", "env:\n  PATH: /tmp\njobs:\n", 1),
		"github env":       strings.Replace(base, "jobs:\n", "env:\n  GITHUB_SHA: x\njobs:\n", 1),
		"expression env":   strings.Replace(base, "jobs:\n", "env:\n  X: "+expr+"\njobs:\n", 1),
		"shell directory":  strings.Replace(base, fmtLine, "run: |\n          cd /\n          cargo fmt --manifest-path tools/Cargo.toml --all -- --check", 1),
		"no rustup":        strings.Replace(base, "run: rustup toolchain install 1.95.0 --profile minimal --component rustfmt", "run: echo none", 1),
		"bad timeout":      strings.Replace(base, "    steps:\n", "    timeout-minutes: 0\n    steps:\n", 1),
		"alias":            strings.Replace(base, "jobs:\n", "x: &a 1\ny: *a\njobs:\n", 1),
		"two documents":    base + "---\na: b\n",
		"escaping dir":     strings.Replace(base, fmtLine, "working-directory: ../x\n        "+fmtLine, 1),
	}
	for name, wf := range cases {
		if wf == base {
			t.Fatalf("%s: fixture edit did not apply", name)
		}
		if _, e := workflowExecution([]byte(wf)); e == nil {
			t.Errorf("accepted %s", name)
		}
	}
}

func TestStepEnvironmentBoundsAndFlags(t *testing.T) {
	p := execPolicy{TimeoutMinutes: 1}
	for i := range p.Steps {
		p.Present[i] = true
		p.Steps[i] = stepPolicy{Dir: workspace, Env: []string{"RUSTFLAGS=-D warnings", "RUSTDOCFLAGS=-D warnings", "CARGO_BUILD_JOBS=8"}}
		env, dir, e := stepEnvironment(p, i, 1, 1)
		if e != nil || dir != workspace {
			t.Fatal(e)
		}
		values := map[string]string{}
		for _, s := range env {
			k, v, _ := strings.Cut(s, "=")
			if _, dup := values[k]; dup {
				t.Fatalf("duplicate key %s", k)
			}
			values[k] = v
		}
		wantRust, wantDoc := "-D warnings", "-D warnings"
		if i >= cmdTest && i <= cmdBuild {
			wantRust += " -C link-arg=-Wl,--threads=1"
		}
		if i == cmdDoc {
			wantDoc += " -C link-arg=-Wl,--threads=1"
		}
		if values["RUSTFLAGS"] != wantRust || values["RUSTDOCFLAGS"] != wantDoc || values["CARGO_BUILD_JOBS"] != "1" {
			t.Fatalf("command %d lost flags or exceeded the cap: %v", i, values)
		}
	}
	p.Steps[0].Dir = "/etc"
	if _, _, e := stepEnvironment(p, 0, 1, 1); e == nil {
		t.Fatal("directory outside the workspace accepted")
	}
	p.Present[cmdDoc] = false
	if _, _, e := stepEnvironment(p, cmdDoc, 1, 1); e == nil {
		t.Fatal("absent command accepted")
	}
}
