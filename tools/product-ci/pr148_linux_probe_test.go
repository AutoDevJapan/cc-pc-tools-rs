package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Official PowerShell/PowerShell release v7.6.7, public release asset digest.
const pr148PowerShellSHA = "6896f9e3a6b0a354bf470835a96da76d2f801cd50dcf047ad7c4563bf93bf431"

// Explicit CPU-only SDK inspection in a new namespace. No source, registration,
// Cargo build, GPU, token, host mount or resident bridge is involved.
func TestPR148LinuxSDK(t *testing.T) {
	dir := os.Getenv("CC_PR148_PROBE_DIR")
	if dir == "" {
		t.Skip("explicit safe-delete-owned receipt directory required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	name := fmt.Sprintf("todo2736-linux-sdk-probe-%d", time.Now().UnixNano())
	rec := struct {
		Name, ContainerID, Image, LogSHA, Failure                              string
		PowerShellSHA                                                          string
		LogBytes                                                               int
		Isolation, ProbeSuccess, TreeGone, Removed                             bool
		ProductSourceSupplied, RegistrationAttempted, AllRequiredChecksSuccess bool
	}{Name: name, Image: codeQLSDK}
	path := filepath.Join(dir, name+".private.json")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.NewEncoder(f).Encode(rec); e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		b, _ := json.MarshalIndent(rec, "", "  ")
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Error(e)
		}
	}()
	// The established helper's cc.todo=2648 label identifies this SDK family;
	// the exact cc.instance/name and the receipt identify ownership by ToDo2736.
	b, createErr := cmd(ctx, docker, codeQLCreateArgs(name)...).Output()
	inspected, e := cmd(ctx, docker, "inspect", name).Output()
	var entries []struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
	}
	if e != nil || json.Unmarshal(inspected, &entries) != nil || len(entries) != 1 || entries[0].Name != "/"+name || entries[0].Config.Image != codeQLSDK || entries[0].Config.Labels["cc.instance"] != name || entries[0].Config.Labels["cc.todo"] != "2648" {
		rec.Failure = "create identity uncertain; preserve recorded instance"
		t.Fatal(rec.Failure)
	}
	rec.ContainerID = entries[0].ID
	defer func() {
		var ce error
		rec.TreeGone, rec.Removed, ce = disposeOwned(rec.ContainerID)
		if ce != nil {
			rec.Failure = "owned namespace cleanup incomplete"
			t.Error(ce)
		}
	}()
	if createErr != nil || strings.TrimSpace(string(b)) != rec.ContainerID {
		t.Fatal("create completion uncertain")
	}
	if e = codeQLIsolation(inspected); e != nil {
		t.Fatal(e)
	}
	rec.Isolation = true
	if e = cmd(ctx, docker, "start", rec.ContainerID).Run(); e != nil {
		t.Fatal(e)
	}
	if file := os.Getenv("CC_PR148_PWSH_TAR"); file != "" {
		data, re := os.ReadFile(file)
		if re != nil || digest(data) != pr148PowerShellSHA {
			t.Fatal("official PowerShell asset digest rejected")
		}
		rec.PowerShellSHA = digest(data)
		transfer := cmd(ctx, docker, "exec", "-i", rec.ContainerID, "sh", "-c", "mkdir /work/powershell && tar -xz -C /work/powershell && chmod 755 /work/powershell/pwsh")
		transfer.Stdin = bytes.NewReader(data)
		if re = transfer.Run(); re != nil {
			t.Fatal("public SDK transfer failed")
		}
	}
	// Only fixed SDK tools are probed; every test command remains unexecuted.
	script := `set -u
PATH=/work/powershell:$PATH
export PATH
failed=0
for tool in /opt/actions-runner/config.sh /opt/actions-runner/bin/Runner.Listener /opt/rust/bin/rustup /opt/rust/bin/cargo; do
  if test -x "$tool"; then printf '%s=available\n' "$tool"; else printf '%s=missing\n' "$tool"; failed=1; fi
done
if command -v pwsh >/dev/null; then echo pwsh=available; else echo pwsh=missing; failed=1; fi
if test -e /var/run/docker.sock || test -e /opt/actions-runner/.credentials || test -e /opt/actions-runner/.runner; then echo forbidden_state=present; failed=1; else echo forbidden_state=absent; fi
printf 'uid='; id -u
printf 'kernel='; uname -s
if test -x /opt/rust/bin/rustup; then /opt/rust/bin/rustup --version; fi
if test -x /opt/rust/bin/cargo; then /opt/rust/bin/cargo --version; fi
if command -v pwsh >/dev/null; then pwsh -NoLogo -NoProfile -Command '$PSVersionTable.PSVersion.ToString()'; fi
printf 'immutable_root='; if test -w /opt/rustup; then echo writable; else echo readonly; fi
printf 'tmpfs_work='; test -w /work && echo writable
test "$(id -u)" = 1001 || failed=1
exit "$failed"
`
	probe := cmd(ctx, docker, "exec", rec.ContainerID, "sh", "-c", script)
	log, e := probe.CombinedOutput()
	rec.LogSHA = digest(log)
	rec.LogBytes = len(log)
	if we := os.WriteFile(filepath.Join(dir, name+".private.log"), log, 0600); we != nil {
		t.Fatal(we)
	}
	if e != nil {
		rec.Failure = "SDK prerequisite probe failed; inspect private fixed-tool output"
		t.Fatal(rec.Failure)
	}
	rec.ProbeSuccess = true
}
