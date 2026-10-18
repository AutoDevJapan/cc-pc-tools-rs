package main

// CC-authored probe for the real-Docker builder fixtures. The base image is a
// public Debian slim image pinned by digest: it is the smallest realistic base
// whose files belong to non-root groups, which is what the original failure was
// about. No product source is involved.
const fixtureBaseImage = "debian@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251"

const fixtureProbeSource = `package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func owner(path string) string {
	var st syscall.Stat_t
	if syscall.Lstat(path, &st) != nil {
		return "missing"
	}
	return fmt.Sprintf("%d:%d:%o", st.Uid, st.Gid, st.Mode&0o7777)
}

func capability(bit uint) string {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "unreadable"
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "CapEff:"); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
			if err != nil {
				return "unreadable"
			}
			if n>>bit&1 == 1 {
				return "1"
			}
			return "0"
		}
	}
	return "unreadable"
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "child" {
		time.Sleep(time.Hour)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "hold" {
		// A child that outlives the build step proves the whole PID namespace is stopped.
		child := exec.Command("/probe", "child")
		if child.Start() != nil {
			os.Exit(5)
		}
		if _, err := os.Stat("/proc/" + strconv.Itoa(child.Process.Pid)); err != nil {
			os.Exit(6)
		}
		// Printed only once the child exists; the harness waits for this line.
		fmt.Println("CC-PROBE-READY")
		time.Sleep(time.Hour)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "drop" {
		// Runs as a non-root user (set up by the parent): it may write what it owns
		// and is refused the root-only file.
		w, r := "denied", "readable"
		if os.WriteFile("/srv/app/by-app", []byte("x"), 0o600) == nil {
			w = "ok"
		}
		if f, err := os.Open("/etc/shadow"); err == nil {
			f.Close()
		} else if errors.Is(err, os.ErrPermission) {
			r = "denied"
		}
		fmt.Printf("%s %s", w, r)
		return
	}
	facts := map[string]string{
		"base.var_mail":      owner("/var/mail"),
		"base.etc_shadow":    owner("/etc/shadow"),
		"base.usr_bin_chage": owner("/usr/bin/chage"),
		"caps.chown":         capability(0),
		"caps.setuid":        capability(7),
		"caps.mknod":         capability(27),
	}
	if os.MkdirAll("/srv/app", 0o755) != nil || os.Chown("/srv/app", 1234, 1234) != nil || os.Chmod("/srv/app", 0o750) != nil {
		os.Exit(2)
	}
	facts["run.srv_app"] = owner("/srv/app")
	if os.WriteFile("/srv/app/data", []byte("d"), 0o600) != nil || os.Chown("/srv/app/data", 1234, 5678) != nil || os.Chmod("/srv/app/data", 0o640|os.ModeSetgid) != nil {
		os.Exit(3)
	}
	facts["run.setgid_file"] = owner("/srv/app/data")
	drop := exec.Command("/probe", "drop")
	drop.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 1234, Gid: 1234}}
	out, err := drop.Output()
	parts := strings.Fields(string(out))
	if err != nil || len(parts) != 2 {
		os.Exit(4)
	}
	facts["run.drop_privs_write"], facts["run.drop_privs_shadow"] = parts[0], parts[1]
	raw, e := json.Marshal(facts)
	if e != nil || os.WriteFile("/proof", raw, 0o600) != nil {
		os.Exit(7)
	}
}
`
