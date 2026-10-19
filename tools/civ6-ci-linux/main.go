package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const usage = "usage: civ6-ci-linux run <plan-json> <new-safe-delete-owned-root> <Linux-guest-binary> <official-pwsh-tar>"

func main() { os.Exit(entry()) }

func entry() int {
	if os.Getenv("CC_CIL_GATE_CHILD") == "1" {
		return gatedChild(os.Args[1:])
	}
	switch strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") {
	case "cargo":
		return guestCargo(false)
	case "rustup":
		return guestCargo(true)
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	var e error
	switch os.Args[1] {
	case "run":
		if len(os.Args) != 6 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		e = runJob(ctx, os.Args[2], os.Args[3], os.Args[4], os.Args[5])
	case "guest-runner":
		e = guestRunner()
	case "guest-hook-start":
		e = guestHook(false)
	case "guest-hook-complete":
		e = guestHook(true)
	case "guest-execute":
		return supervise(os.Args[2:])
	case "guest-exec-child":
		e = guestExecChild(os.Args[2:])
	case "guest-check-group":
		id, err := strconv.Atoi(os.Args[len(os.Args)-1])
		if len(os.Args) != 3 || err != nil || id <= 1 || !groupAbsent(id) {
			return 1
		}
		return 0
	case "guest-listener-probe":
		// Offline: the restricted official listener must be able to start.
		if err := cmd(context.Background(), "cp", "-a", "/opt/actions-runner/.", "/runner/").Run(); err != nil {
			return 1
		}
		e = execRestricted("/runner/bin/Runner.Listener", []string{"--version"}, []string{"HOME=/home/runner", "TMPDIR=/tmp", "PATH=/usr/bin:/bin"}, true)
	case "guest-restriction-fixture":
		e = execRestricted("/bin/sh", []string{"-c", restrictionScript}, []string{"PATH=/usr/bin:/bin", "HOME=/home/runner"}, true)
	case "guest-fixture-callback":
		e = fixtureCallback()
	default:
		e = errors.New("unknown helper mode")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	return 0
}

const restrictionScript = "set -eu\n" +
	"if /opt/rust/bin/cargo --version >/dev/null 2>&1; then exit 81; fi\n" +
	"if cp /opt/rust/bin/cargo /work/copied-cargo 2>/dev/null; then exit 82; fi\n" +
	"if cat /toolchain/protected >/dev/null 2>&1; then exit 83; fi\n" +
	"if cat /proc/1/root/opt/rust/bin/cargo >/dev/null 2>&1; then exit 84; fi\n" +
	"if (printf 'tamper' > /runner/civ6-ci-linux) 2>/dev/null; then exit 85; fi\n" +
	"if (printf 'tamper' > /runner/plan.json) 2>/dev/null; then exit 86; fi\n" +
	"if truncate -s 0 /runner/civ6-ci-linux 2>/dev/null; then exit 87; fi\n" +
	"mkdir -p /work/refer-from /work/refer-to\n" +
	"printf refer > /work/refer-from/source\n" +
	"mv /work/refer-from/source /work/refer-to/destination\n" +
	"ln /work/refer-to/destination /work/refer-from/hardlink\n" +
	"test \"$(cat /work/refer-from/hardlink)\" = refer\n" +
	"printf 'landlock-direct-copy-proc-toolchain-denied\\n'\n"

// fixtureCallback is the credential-free guest side of the lifecycle fixture:
// it asks the host for the fixed test command exactly as the cargo wrapper does.
func fixtureCallback() error {
	p, e := readGuestPacket()
	if e != nil {
		return e
	}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stop()
	r, e := requestHost(ctx, p.Callback, "cargo", p.Nonce, p.Plan, cargoTable[cmdTest])
	if e != nil {
		return e
	}
	_, _ = os.Stdout.Write(r.Log)
	if r.LogTruncated && r.Exit == 0 {
		return errors.New("output preview truncated")
	}
	if r.Exit != 0 {
		return errors.New("fixture exit " + strconv.Itoa(r.Exit))
	}
	return nil
}
