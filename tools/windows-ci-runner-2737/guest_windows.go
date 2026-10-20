//go:build windows && amd64

package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	sdk "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const sdkRoot = `C:\ProgramData\AutoDevJapan\windows-ci-sdk-2737`
const agentRoot = `C:\Windows\System32\WindowsCISDK2737`
const runnerRoot = sdkRoot + `\runner`
const sourceRoot = runnerRoot + `\_work\GoDD-MCP-Alpha\GoDD-MCP-Alpha`
const hookRoot = sdkRoot + `\runner-hooks`
const planPath = agentRoot + `\runner-seed.json`
const helperPath = agentRoot + `\windows-ci-runner-agent.exe`
const stockArchive = agentRoot + `\actions-runner-win-x64-2.338.0.zip`

func readBounded(path string, max int) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("fixed public input absent")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if e != nil || len(b) > max {
		return nil, errors.New("fixed input size rejected")
	}
	return b, nil
}

// RunGuestAgent must be invoked only as the replaced SDK service in the fresh
// isolated guest. It cannot install/register a host service or create a VM.
func RunGuestAgent() error {
	exe, e := os.Executable()
	if e != nil || !strings.EqualFold(filepath.Clean(exe), helperPath) {
		return errors.New("guest executable path rejected")
	}
	var seed Seed
	b, e := readBounded(planPath, maxControl)
	if e != nil || decode(b, &seed) != nil || seed.Validate() != nil {
		return errors.New("offline runner seed rejected")
	}
	return sdk.RunGuestServiceBackend(func(public sdk.PublicSeed) (sdk.GuestBackend, error) {
		if public.Instance != seed.Plan.Instance || public.ClientSHA256 != seed.ClientSHA256 {
			return sdk.GuestBackend{}, errors.New("SDK and runner seeds disagree")
		}
		s := &channelServer{run: func(ctx context.Context, c net.Conn) error {
			o := &windowsOps{plan: seed.Plan, hookCalls: make(chan hookCall), closed: make(chan struct{})}
			return guestSession(ctx, c, seed, o)
		}}
		return sdk.GuestBackend{Handler: s, Stop: s.stop}, nil
	})
}

type windowsOps struct {
	plan       Plan
	entries    []sourcepacket.TreeEntry
	env        []string
	hookCalls  chan hookCall
	listener   net.Listener
	capability string
	closed     chan struct{}
	wg         sync.WaitGroup
	closeOnce  sync.Once
}

func (o *windowsOps) prepare(ctx context.Context, p sourcepacket.Packet) (sourcepacket.Receipt, error) {
	// No inherited owner environment. Git operations in sourcepacket use this
	// process's scrubbed environment as well as their own no-auth Git settings.
	var e error
	o.env, e = fixedEnvironment(o.plan)
	if e != nil {
		return sourcepacket.Receipt{}, e
	}
	// This process is the single-use service, no other work runs in it.
	os.Clearenv()
	for _, entry := range o.env {
		k, v, _ := strings.Cut(entry, "=")
		os.Setenv(k, v)
	}
	b, e := readBounded(stockArchive, 512<<20)
	if e != nil {
		return sourcepacket.Receipt{}, e
	}
	e = unpackRunner(b, runnerRoot)
	clear(b)
	if e != nil {
		return sourcepacket.Receipt{}, e
	}
	o.entries = append([]sourcepacket.TreeEntry(nil), p.TreeEntries...)
	proof, e := sourcepacket.Materialize(ctx, p, o.plan.Source, sourceRoot)
	if e != nil {
		return proof, e
	}
	if e = o.startHooks(); e != nil {
		return sourcepacket.Receipt{}, e
	}
	return proof, nil
}
func fixedEnvironment(p Plan) ([]string, error) {
	if p.Validate() != nil {
		return nil, errors.New("environment plan rejected")
	}
	sccache, e := fixedSccache()
	if e != nil {
		return nil, e
	}
	env := []string{
		`SystemRoot=C:\Windows`, `WINDIR=C:\Windows`, `SystemDrive=C:`, `ComSpec=C:\Windows\System32\cmd.exe`,
		`ProgramFiles=C:\Program Files`, `ProgramFiles(x86)=C:\Program Files (x86)`, `ProgramData=C:\ProgramData`,
		`PATHEXT=.COM;.EXE;.BAT;.CMD;.PS1`,
		`PATH=` + fixedServicePath(sccache),
		`USERPROFILE=` + sdkRoot + `\service-profile`, `HOME=` + sdkRoot + `\service-profile`,
		`APPDATA=` + sdkRoot + `\service-profile\AppData\Roaming`, `LOCALAPPDATA=` + sdkRoot + `\service-profile\AppData\Local`,
		`TEMP=` + sdkRoot + `\staging`, `TMP=` + sdkRoot + `\staging`,
		`CARGO_HOME=` + sdkRoot + `\cargo-home`, `RUSTUP_HOME=` + sdkRoot + `\rustup-home`,
		`CARGO_TARGET_DIR=` + sdkRoot + `\target`, `CARGO_BUILD_JOBS=` + strconv.Itoa(p.Jobs), `RUST_TEST_THREADS=` + strconv.Itoa(p.Threads),
		`SCCACHE_DIR=` + sdkRoot + `\sccache-cache`, `SCCACHE_CACHE_SIZE=30G`, `RUSTC_WRAPPER=` + sccache,
		`GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=NUL`, `GIT_TERMINAL_PROMPT=0`,
		`RUNNER_LOGSIZE=1`, `WORKER_LOGSIZE=1`,
	}
	return env, nil
}

func fixedServicePath(sccache string) string {
	// Stock config.cmd/run.cmd require Windows PowerShell, while workflow pwsh
	// uses the separately pinned PowerShell 7. Keep both explicit guest paths.
	return sdkRoot + `\rust-1.97.1\bin;` + filepath.Dir(sccache) + `;` + sdkRoot + `\git\cmd;` + sdkRoot + `\git\bin;` + sdkRoot + `\powershell\7;C:\Windows\System32\WindowsPowerShell\v1.0;C:\Windows\System32;C:\Windows`
}
func fixedSccache() (string, error) {
	root := sdkRoot + `\sccache`
	var found string
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("SDK reparse rejected")
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "sccache.exe") {
			if found != "" {
				return errors.New("SDK sccache ambiguous")
			}
			found = path
		}
		return nil
	})
	if e != nil || found == "" {
		return "", errors.New("SDK sccache missing or ambiguous")
	}
	return found, nil
}
func (o *windowsOps) configure(ctx context.Context, token []byte) error {
	for _, b := range token {
		if b < 33 || b > 126 {
			return errors.New("token encoding rejected")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// Stock CommandSettings consumes and removes this secret environment input.
	// Neither the process command line nor our log files contain the token.
	env := append(append([]string(nil), o.env...), "ACTIONS_RUNNER_INPUT_TOKEN="+string(token))
	line := `"C:\Windows\System32\cmd.exe" /d /s /c ""` + runnerRoot + `\config.cmd" --unattended --ephemeral --no-default-labels --disableupdate --url https://github.com/` + Repository + ` --name ` + o.plan.Name + ` --labels ` + o.plan.Label + ` --work _work"`
	r := withDiagnosticBound(ctx, func(ctx context.Context) processResult {
		return runTree(ctx, `C:\Windows\System32\cmd.exe`, line, runnerRoot, env, "")
	})
	for i := range env {
		env[i] = ""
	}
	if r.err != nil || r.exit != 0 || !r.drained {
		return errors.New("stock configuration exit or drain failed")
	}
	return nil
}
func (o *windowsOps) listen(ctx context.Context, id int64) <-chan processResult {
	done := make(chan processResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 23*time.Minute)
		defer cancel()
		env := append(append([]string(nil), o.env...), "CC2737_HOOK_ENDPOINT="+o.listener.Addr().String(), "CC2737_HOOK_CAPABILITY="+o.capability,
			"ACTIONS_RUNNER_HOOK_JOB_STARTED="+hookRoot+`\start.ps1`, "ACTIONS_RUNNER_HOOK_JOB_COMPLETED="+hookRoot+`\complete.ps1`)
		// The original workflow's thirteen Cargo commands/profile/serial flags
		// execute in the stock runner. This is SDK setup, never a workflow rewrite.
		line := `"C:\Windows\System32\cmd.exe" /d /s /c "call "` + sdkRoot + `\vs-build-tools\Common7\Tools\VsDevCmd.bat" -arch=x64 -host_arch=x64 -vcvars_ver=14.50 -winsdk=10.0.26100.0 && call "` + runnerRoot + `\run.cmd""`
		done <- withDiagnosticBound(ctx, func(ctx context.Context) processResult {
			return runTree(ctx, `C:\Windows\System32\cmd.exe`, line, runnerRoot, env, sdkRoot+`\private\runner-listener.log`)
		})
	}()
	return done
}
func (o *windowsOps) proof(ctx context.Context) (sourcepacket.Receipt, error) {
	return sourcepacket.VerifyCheckout(ctx, o.plan.Source, o.entries, sourceRoot)
}
func (o *windowsOps) hooks() <-chan hookCall { return o.hookCalls }
func (o *windowsOps) close() error {
	o.closeOnce.Do(func() {
		close(o.closed)
		if o.listener != nil {
			o.listener.Close()
		}
	})
	o.wg.Wait()
	return nil
}

type localHook struct {
	Capability string
	Stage      Stage
	Context    HookContext
}
type localAck struct{ Accepted bool }

func (o *windowsOps) startHooks() error {
	var secret [32]byte
	if _, e := rand.Read(secret[:]); e != nil {
		return errors.New("hook capability generation failed")
	}
	o.capability = hex.EncodeToString(secret[:])
	clear(secret[:])
	if os.Mkdir(hookRoot, 0700) != nil {
		return errors.New("hook root not fresh")
	}
	for _, stage := range []Stage{Start, Complete} {
		// Fixed script glue required by stock runner; no request text is inserted.
		body := "& '" + helperPath + "' hook " + string(stage) + "\r\nexit $LASTEXITCODE\r\n"
		if e := os.WriteFile(filepath.Join(hookRoot, string(stage)+".ps1"), []byte(body), 0600); e != nil {
			return errors.New("hook staging failed")
		}
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return errors.New("local hook listener failed")
	}
	o.listener = l
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			o.handleHook(c)
		}
	}()
	return nil
}
func (o *windowsOps) handleHook(c net.Conn) {
	defer c.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-o.closed:
			c.Close()
		case <-done:
		}
	}()
	_ = c.SetDeadline(time.Now().Add(90 * time.Second))
	var m localHook
	if receive(c, &m) != nil || subtle.ConstantTimeCompare([]byte(m.Capability), []byte(o.capability)) != 1 || m.Context.Validate(o.plan) != nil {
		return
	}
	call := hookCall{m.Stage, m.Context, make(chan error, 1)}
	select {
	case o.hookCalls <- call:
	case <-o.closed:
		return
	case <-time.After(90 * time.Second):
		return
	}
	select {
	case e := <-call.reply:
		_ = send(c, localAck{e == nil})
	case <-o.closed:
	case <-time.After(90 * time.Second):
	}
}

// RunHook is invoked only by stock synchronous started/completed scripts. It
// reads only required GitHub context and dispatch inputs, never credentials.
func RunHook(stage Stage) error {
	if stage != Start && stage != Complete {
		return errors.New("hook stage rejected")
	}
	endpoint, capability := os.Getenv("CC2737_HOOK_ENDPOINT"), os.Getenv("CC2737_HOOK_CAPABILITY")
	host, port, e := net.SplitHostPort(endpoint)
	if e != nil || host != "127.0.0.1" || !validHex(capability, 64) {
		return errors.New("hook channel rejected")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return errors.New("hook port rejected")
	}
	run, e := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	if e != nil {
		return errors.New("hook run rejected")
	}
	attempt, e := strconv.Atoi(os.Getenv("GITHUB_RUN_ATTEMPT"))
	if e != nil {
		return errors.New("hook attempt rejected")
	}
	// Event is stock-runner generated; only the inputs member is decoded. The
	// rest of its payload is discarded without being logged or sent to the host.
	b, e := readBounded(os.Getenv("GITHUB_EVENT_PATH"), 1<<20)
	if e != nil {
		return e
	}
	defer clear(b)
	var event struct {
		Inputs map[string]string `json:"inputs"`
	}
	if json.Unmarshal(b, &event) != nil || len(event.Inputs) != 0 {
		return errors.New("hook dispatch inputs rejected")
	}
	context := HookContext{Repository: os.Getenv("GITHUB_REPOSITORY"), SHA: os.Getenv("GITHUB_SHA"), Ref: os.Getenv("GITHUB_REF"), Event: os.Getenv("GITHUB_EVENT_NAME"), Role: os.Getenv("GITHUB_JOB"), Name: os.Getenv("RUNNER_NAME"), WorkflowSHA: os.Getenv("GITHUB_WORKFLOW_SHA"), RunID: run, Attempt: attempt, Inputs: event.Inputs}
	c, e := net.DialTimeout("tcp4", endpoint, 5*time.Second)
	if e != nil {
		return errors.New("hook agent unavailable")
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(90 * time.Second))
	if send(c, localHook{capability, stage, context}) != nil {
		return errors.New("hook transfer failed")
	}
	var ack localAck
	if receive(c, &ack) != nil || !ack.Accepted {
		return errors.New("hook positive acknowledgement absent")
	}
	return nil
}

// Ensure LocalSystem is used only after the parent's dedicated-guest boot.
// This check is an execution precondition, not a claim of host Cargo admission.
func IsLocalSystem() bool {
	var token syscall.Token
	process, e := syscall.GetCurrentProcess()
	if e != nil {
		return false
	}
	if syscall.OpenProcessToken(process, syscall.TOKEN_QUERY, &token) != nil {
		return false
	}
	defer token.Close()
	user, e := token.GetTokenUser()
	if e != nil {
		return false
	}
	sid, e := user.User.Sid.String()
	return e == nil && sid == "S-1-5-18"
}
