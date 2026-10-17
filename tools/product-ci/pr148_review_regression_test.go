package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pr148FailedWriter struct{}

func (pr148FailedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestPR148AssignmentRetryIsNarrowAndFinite(t *testing.T) {
	calls := 0
	check := func(context.Context) error {
		calls++
		if calls < 3 {
			return errPR148AssignmentPending
		}
		return nil
	}
	if e := pr148AwaitAssignment(context.Background(), check, func(context.Context) error { return nil }); e != nil || calls != 3 {
		t.Fatal("pending assignment was not finitely checked")
	}
	calls = 0
	denied := errors.New("wrong runner")
	check = func(context.Context) error { calls++; return denied }
	if e := pr148AwaitAssignment(context.Background(), check, func(context.Context) error { t.Fatal("policy rejection retried"); return nil }); !errors.Is(e, denied) || calls != 1 {
		t.Fatal("wrong runner rejection weakened")
	}
}
func TestPR148PolicyReadVerifiesRepositoryFirst(t *testing.T) {
	reads := 0
	read := func(_ context.Context, ep string, out any) error {
		reads++
		if ep != "repositories/1392152644" {
			t.Fatalf("unverified repository reached %s", ep)
		}
		b := []byte(`{"id":1,"private":true,"fork":false,"full_name":"AutoDevJapan/other","owner":{"login":"AutoDevJapan"},"permissions":{"admin":true}}`)
		return json.Unmarshal(b, out)
	}
	if _, _, e := pr148OriginalExecution(context.Background(), pr148SyntheticPlan(), read); e == nil || reads != 1 {
		t.Fatal("workflow read allowed before numeric repository verification")
	}
}
func TestPR148OverflowAndFailedDestinationDrain(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, 1024)
	var file bytes.Buffer
	sink := pr148LogSink{file: &file, limit: 10}
	for i := 0; i < 40; i++ {
		if n, e := sink.Write(data); n != len(data) || e != nil {
			t.Fatal("preview stopped pipe consumption")
		}
	}
	if sink.preview.Len() != 10 || sink.bytes != 40960 || sink.SHA() != digest(file.Bytes()) || file.Len() != 40960 {
		t.Fatal("full log digest or preview differs")
	}
	reader := bytes.NewReader(data)
	if pr148ForwardAndDrain(pr148FailedWriter{}, reader) == nil || reader.Len() != 0 {
		t.Fatal("failed destination left source pipe unread")
	}
	fail := pr148LogSink{file: pr148FailedWriter{}, limit: 10}
	if n, e := fail.Write(data); n != len(data) || e != nil || !errors.Is(fail.err, io.ErrClosedPipe) {
		t.Fatal("storage failure not retained while draining")
	}
}
func pr148ExecutionFixtureYAML() []byte {
	s := "on:\n  workflow_dispatch:\nenv:\n  RUSTFLAGS: -D warnings\n  CARGO_INCREMENTAL: '0'\njobs:\n  test:\n    timeout-minutes: 75\n    env:\n      CARGO_TERM_COLOR: always\n    defaults:\n      run:\n        working-directory: .\n    steps:\n"
	for _, a := range pr148CargoArgs {
		s += "      - env:\n          FIXTURE_STEP: fixed\n        run: cargo " + strings.Join(a, " ") + "\n"
	}
	s += "      - run: rustup " + strings.Join(pr148RustupArgs, " ") + "\n"
	return []byte(s)
}
func TestPR148ExecutionPreservesFixedContextAndTimeout(t *testing.T) {
	prior := pr148Workflows[0].OriginalSHA
	defer func() { pr148Workflows[0].OriginalSHA = prior }()
	data := pr148ExecutionFixtureYAML()
	pr148Workflows[0].OriginalSHA = digest(data)
	_, p, e := pr148WorkflowExecution(data)
	if e != nil {
		t.Fatal(e)
	}
	if p.timeout() != 75*time.Minute {
		t.Fatal("job timeout replaced by helper constant")
	}
	env, dir, e := pr148StepEnvironment(p, 1, 2, 3)
	if e != nil || dir != pr148Workspace {
		t.Fatal(e)
	}
	joined := strings.Join(env, "\n")
	for _, s := range []string{"RUSTFLAGS=-D warnings", "CARGO_INCREMENTAL=0", "CARGO_TERM_COLOR=always", "FIXTURE_STEP=fixed", "CARGO_BUILD_JOBS=2"} {
		if !strings.Contains(joined, s) {
			t.Fatalf("lost fixed condition %s", s)
		}
	}
	for _, bad := range []string{"expression", "protected", "timeout", "directory", "git", "github"} {
		b := append([]byte(nil), data...)
		switch bad {
		case "expression":
			b = bytes.Replace(b, []byte("-D warnings"), []byte("${{ secrets.TEST }}"), 1)
		case "protected":
			b = bytes.Replace(b, []byte("RUSTFLAGS:"), []byte("CARGO_HOME:"), 1)
		case "timeout":
			b = bytes.Replace(b, []byte("75"), []byte("${{ matrix.minutes }}"), 1)
		case "directory":
			b = bytes.Replace(b, []byte("working-directory: ."), []byte("working-directory: ../escape"), 1)
		case "git":
			b = bytes.Replace(b, []byte("RUSTFLAGS:"), []byte("GIT_CONFIG_GLOBAL:"), 1)
		case "github":
			b = bytes.Replace(b, []byte("RUSTFLAGS:"), []byte("GITHUB_ACTIONS:"), 1)
		}
		pr148Workflows[0].OriginalSHA = digest(b)
		if _, _, e = pr148WorkflowExecution(b); e == nil {
			t.Fatalf("unsupported context %s accepted", bad)
		}
	}
}

func TestPR148OmittedJobTimeoutPreservesGitHubDefault(t *testing.T) {
	prior := pr148Workflows[0].OriginalSHA
	defer func() { pr148Workflows[0].OriginalSHA = prior }()
	for _, value := range []string{"", "    timeout-minutes: 20\n", "    timeout-minutes: 0\n", "    timeout-minutes: 361\n", "    timeout-minutes: [20]\n"} {
		data := bytes.Replace(pr148ExecutionFixtureYAML(), []byte("    timeout-minutes: 75\n"), []byte(value), 1)
		pr148Workflows[0].OriginalSHA = digest(data)
		_, p, e := pr148WorkflowExecution(data)
		if value == "" {
			if e != nil || p.timeout() != 360*time.Minute {
				t.Fatalf("omitted timeout: %v %v", p.timeout(), e)
			}
		} else if value == "    timeout-minutes: 20\n" {
			if e != nil || p.timeout() != 20*time.Minute {
				t.Fatalf("explicit timeout: %v %v", p.timeout(), e)
			}
		} else if e == nil {
			t.Fatal("invalid timeout accepted", value)
		}
	}
}

func TestPR148WorkflowSerialBoundsSurviveGateCaps(t *testing.T) {
	p := pr148ExecutionPolicy{TimeoutMinutes: 75}
	p.Steps[1] = pr148StepPolicy{Dir: pr148Workspace, Env: []string{"RUST_TEST_THREADS=1", "CARGO_BUILD_JOBS=99"}}
	env, _, e := pr148StepEnvironment(p, 1, 2, 3)
	if e != nil {
		t.Fatal(e)
	}
	actual := map[string]string{}
	for _, s := range env {
		k, v, _ := strings.Cut(s, "=")
		if _, ok := actual[k]; ok {
			t.Fatalf("duplicate raw envp key: %s", k)
		}
		actual[k] = v
	}
	if actual["RUST_TEST_THREADS"] != "1" || actual["CARGO_BUILD_JOBS"] != "2" {
		t.Fatal("original serial bound or machine cap lost")
	}
	if actual["CI"] != "true" || actual["GITHUB_ACTIONS"] != "true" {
		t.Fatal("immutable Actions CI defaults missing")
	}
}
func TestPR148NoDispatchAndHookArgumentConfusion(t *testing.T) {
	w := pr148Workflows[0]
	data := bytes.Replace(pr148Fixture(w), []byte("  workflow_dispatch:\n"), nil, 1)
	w.OriginalSHA = digest(data)
	if _, e := proposePR148(data, w); e == nil {
		t.Fatal("non-dispatchable workflow accepted")
	}
	p := pr148SyntheticPlan()
	key := strings.Repeat("a", 64)
	q := pr148CargoRequest{p, 1392152644, key, pr148RustupArgs}
	body, _ := json.Marshal(q)
	r := httptest.NewRequest("POST", "/pr148/start", bytes.NewReader(body))
	r.Header.Set("X-PR148-Nonce", key)
	wtr := httptest.NewRecorder()
	h := pr148CargoHost{plan: p, key: key}
	h.ServeHTTP(wtr, r)
	if wtr.Code != 403 {
		t.Fatal("hook path accepted Rustup arguments")
	}
}
func TestPR148UnsignedUTCCommitBytes(t *testing.T) {
	var c originalCommit
	c.Tree.SHA = strings.Repeat("2", 40)
	c.Parents = []struct{ SHA string }{{pr148Head}}
	c.Author.Name = "fixture"
	c.Author.Email = "fixture@example.invalid"
	c.Author.Date = "1970-01-01T00:00:01Z"
	c.Committer = c.Author
	c.Message = "fixture"
	raw := []byte("tree " + c.Tree.SHA + "\nparent " + pr148Head + "\nauthor fixture <fixture@example.invalid> 1 +0000\ncommitter fixture <fixture@example.invalid> 1 +0000\n\nfixture\n")
	b, e := originalCommitBytes(c, gitObjectID("commit", raw))
	if e != nil || !bytes.Equal(b, raw) {
		t.Fatal("unsigned parent-created UTC commit cannot be reproduced")
	}
}

// Explicit fresh tmpfs installation using the ORIGINAL rustup command, without
// any product checkout, token, runner registration or GitHub mutation.
func TestPR148CredentialFreeRustupInstall(t *testing.T) {
	dir, binary := os.Getenv("CC_PR148_FIXTURE_DIR"), os.Getenv("CC_PR148_FIXTURE_ELF")
	if os.Getenv("CC_PR148_RUSTUP_FIXTURE") != "1" || dir == "" || binary == "" {
		t.Skip("explicit credential-free SDK install fixture required")
	}
	caseDir := filepath.Join(dir, "rustup-"+time.Now().Format("150405.000000000"))
	if e := os.Mkdir(caseDir, 0700); e != nil {
		t.Fatal(e)
	}
	ctx, stop := context.WithTimeout(context.Background(), 6*time.Minute)
	defer stop()
	n, e := newPR148Namespace(ctx, caseDir)
	if n != nil && n.ID != "" {
		defer func() {
			if e := n.dispose(); e != nil {
				t.Error(e)
			}
			if e := pr148SaveNew(filepath.Join(caseDir, "namespace-result.private.json"), n); e != nil {
				t.Error(e)
			}
		}()
	}
	if e != nil {
		t.Fatal(e)
	}
	guest, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal(e)
	}
	if e = pr148Transfer(ctx, n, guest); e != nil {
		t.Fatal(e)
	}
	if e = cmd(ctx, docker, "exec", n.ID, "mkdir", "-p", pr148Workspace, "/toolchain/cargo", "/toolchain/rustup").Run(); e != nil {
		t.Fatal(e)
	}
	if e = pr148PrepareToolchain(ctx, n); e != nil {
		t.Fatal(e)
	}
	execution := pr148ExecutionPolicy{TimeoutMinutes: 5}
	for i := range execution.Steps {
		execution.Steps[i].Dir = pr148Workspace
		execution.Steps[i].Env = []string{"RUST_TEST_THREADS=1"}
	}
	p := pr148SyntheticPlan()
	key := strings.Repeat("c", 64)
	packet := pr148GuestPacket{Plan: p, Policy: pr148SyntheticPolicy(), Execution: execution, Repo: pr148Repo, Name: n.Name, Nonce: key}
	data, _ := json.Marshal(packet)
	x := cmd(ctx, docker, "exec", "-i", n.ID, "sh", "-c", "cat > /runner/pr148-plan.json")
	x.Stdin = bytes.NewReader(data)
	if e = x.Run(); e != nil {
		t.Fatal(e)
	}
	h := &pr148CargoHost{plan: p, policy: packet.Policy, execution: execution, key: key, ns: n, dir: caseDir, target: filepath.Join(caseDir, "cargo-target")}
	r, e := h.run(ctx, 4)
	if e != nil || r.Exit != 0 || !r.Drained {
		t.Fatalf("original rustup install: %v exit=%d %s", e, r.Exit, r.Log)
	}
	b, e := cmd(ctx, docker, "exec", n.ID, "/toolchain/rustup/toolchains/1.95.0-x86_64-unknown-linux-gnu/bin/rustc", "--version").CombinedOutput()
	if e != nil || !bytes.HasPrefix(b, []byte("rustc 1.95.0")) {
		t.Fatalf("new tmpfs toolchain unavailable: %v %s", e, b)
	}
	if e = pr148SaveNew(filepath.Join(caseDir, "rustup-result.private.json"), map[string]any{"exit": r.Exit, "drained": r.Drained, "log_bytes": r.FullLogBytes, "log_sha256": r.FullLogSHA, "version_output_sha256": digest(b), "product_source_supplied": false, "official_checks_success": false}); e != nil {
		t.Fatal(e)
	}
	if e = cmd(ctx, docker, "exec", n.ID, "mkdir", "-p", pr148Workspace+"/tools/src").Run(); e != nil {
		t.Fatal(e)
	}
	put := func(path, content string) {
		c := cmd(ctx, docker, "exec", "-i", n.ID, "sh", "-c", "cat > "+path)
		c.Stdin = strings.NewReader(content)
		if e := c.Run(); e != nil {
			t.Fatal(e)
		}
	}
	put(pr148Workspace+"/tools/Cargo.toml", "[package]\nname=\"todo2736-credential-free-fixture\"\nversion=\"0.0.0\"\nedition=\"2021\"\n[workspace]\n")
	put(pr148Workspace+"/tools/Cargo.lock", "version = 4\n\n[[package]]\nname = \"todo2736-credential-free-fixture\"\nversion = \"0.0.0\"\n")
	put(pr148Workspace+"/tools/src/main.rs", "fn main() {\n    println!(\"credential-free\");\n}\n")
	put(pr148Workspace+"/tools/src/lib.rs", `/// A credential-free fixture value.
///
/// `+"```"+`
/// assert_eq!(todo2736_credential_free_fixture::value(), 42);
/// `+"```"+`
pub fn value() -> usize {
    42
}

#[cfg(test)]
mod tests {
    #[test]
    fn original_serial_environment() {
        assert_eq!(std::env::var("RUST_TEST_THREADS").unwrap(), "1");
        assert_eq!(std::env::var("CI").unwrap(), "true");
        assert_eq!(std::env::var("GITHUB_ACTIONS").unwrap(), "true");
    }
}
`)
	var commands []map[string]any
	for index := 0; index < 4; index++ {
		result, e := h.run(ctx, index)
		if e != nil || result.Exit != 0 || !result.Drained {
			t.Fatalf("Rust1.95 original-command fixture index=%d error=%v exit=%d log=%s", index, e, result.Exit, result.Log)
		}
		commands = append(commands, map[string]any{"index": index, "args": packet.Policy[index], "exit": result.Exit, "drained": result.Drained, "log_bytes": result.FullLogBytes, "log_sha256": result.FullLogSHA})
	}
	if e = pr148SaveNew(filepath.Join(caseDir, "rust195-command-results.private.json"), commands); e != nil {
		t.Fatal(e)
	}
}
