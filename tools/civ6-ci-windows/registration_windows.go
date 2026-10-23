//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// Only the admitted host controller calls this, after audit and queue checks.
// One POST with the existing OAuth store; no retry, argv credential or output.
func registrationCredential(p Plan) ([]byte, error) {
	ctx, cancel := prestartContext(p, 30*time.Second)
	defer cancel()
	return requestRegistrationCredential(ctx, cancel, func(ctx context.Context) *exec.Cmd {
		c := exec.CommandContext(ctx, `D:\CC\Tools\codex-jobs\gh_gate.exe`, "api", "--method", "POST", "repos/AutoDevJapan/civ6-banseiki/actions/runners/registration-token", "--jq", ".token")
		hidden(c)
		c.Env = controllerGateEnv()
		return c
	})
}

func requestRegistrationCredential(ctx context.Context, cancel context.CancelFunc, command func(context.Context) *exec.Cmd) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, errors.New("registration credential deadline")
	}
	c := command(ctx)
	c.Stderr = io.Discard
	b, overflow, err := captureGitHubRead(c, 4096, cancel)
	defer clear(b)
	if err != nil || overflow || ctx.Err() != nil {
		return nil, errors.New("registration credential unavailable")
	}
	t := bytes.TrimSpace(b)
	if len(t) < 16 || len(t) > 512 || bytes.ContainsAny(t, " \r\n\t\x00") {
		return nil, errors.New("registration credential shape rejected")
	}
	return append([]byte(nil), t...), nil
}
