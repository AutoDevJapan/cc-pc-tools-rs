package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// This configures the official ephemeral runner OFFLINE. There is deliberately
// no listener/run.sh path here, and no job or Cargo can be accepted.
func configureOffline(conn net.Conn, input io.Reader, id string) (result offlineResult) {
	result.ID = id
	raw, e := readBlob(input, 16384)
	if e != nil {
		result.Failure = "offline_frame_rejected"
		return
	}
	var q offlineConfiguration
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF || !strings.HasPrefix(q.Name, "todo2737-") || !validInstance(strings.TrimPrefix(q.Name, "todo2737-")) || len(q.Token) < 1 || len(q.Token) > 4096 {
		result.Failure = "offline_configuration_rejected"
		return
	}
	for n := range raw {
		raw[n] = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	go func() { var b [1]byte; _, _ = input.Read(b[:]); cancel() }()
	if e = os.MkdirAll("/home/ci2737/work", 0755); e != nil {
		result.Failure = "offline_workspace_failed"
		return
	}
	if e = os.Chown("/home/ci2737/work", 1001, 1001); e != nil {
		result.Failure = "offline_workspace_owner_failed"
		return
	}
	c := exec.CommandContext(ctx, "/opt/ci2737-runner/config.sh", "--unattended", "--url", "https://github.com/AutoDevJapan/GoDD-MCP-Alpha", "--name", q.Name, "--labels", offlineLabel, "--no-default-labels", "--work", "/home/ci2737/work", "--ephemeral", "--disableupdate")
	c.Dir = "/opt/ci2737-runner"
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: 1001, Gid: 1001}}
	c.Env = []string{"HOME=/home/ci2737", "PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "ACTIONS_RUNNER_INPUT_TOKEN=" + q.Token}
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	c.WaitDelay = 5 * time.Second
	c.Cancel = func() error {
		if c.Process != nil {
			return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	e = c.Run()
	q.Token = ""
	c.Env = nil
	if e != nil {
		result.Failure = "official_offline_configuration_failed"
		return
	}
	result.Configured = true
	return
}
