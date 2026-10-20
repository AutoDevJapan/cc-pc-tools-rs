//go:build windows

// Finite evidence runner for ToDo 3209. Each selected Cargo command runs once.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const source = "565ec9bf6d0145721884978eb73a05b03cf3b623"
const owned = "C:/Users/makhmeto/AppData/Local/Temp/codex-owned-fd907bbfcd9076980359c0a5467c51cb"
const gate = "D:/CC/Tools/codex-jobs/cargo_gate.exe"

type memoryStatus struct {
	Length, Load uint32
	Total, Available, TotalPage, AvailablePage, TotalVirtual, AvailableVirtual, Extended uint64
}

func resources() map[string]any {
	m := memoryStatus{}
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&m)))
	return map[string]any{"at": time.Now().Format(time.RFC3339Nano), "logical_cpus": runtime.NumCPU(),
		"memory_sample_ok": r != 0, "physical_bytes": m.Total, "available_physical_bytes": m.Available,
		"memory_load_percent": m.Load, "allocated": []string{"cargo"}, "requested_build_jobs": 10,
		"requested_test_threads": 10, "effective_parallelism": "shared cargo gate may lower both caps; see stderr",
		"target_dir": filepath.Join(owned, "target"), "safe_delete_owner_id": "fd907bbfcd9076980359c0a5467c51cb",
		"rustc_wrapper": "D:/CC/Tools/sccache/sccache.exe", "sccache_dir": "D:/CC/BuildCache/sccache",
		"sccache_cache_size": "30G", "sccache_server_port": "42259", "gpu": false, "civ6": false}
}

func hidden(program string, args ...string) *exec.Cmd {
	c := exec.Command(program, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

func git(args ...string) string {
	b, err := hidden("git", args...).Output()
	if err != nil { panic(fmt.Sprintf("git %v: %v", args, err)) }
	return strings.TrimSpace(string(b))
}

func save(path string, value any, exclusive bool) {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil { panic(err) }
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive { flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL }
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil { panic(err) }
	if _, err = f.Write(append(b, '\n')); err != nil { panic(err) }
	if err = f.Sync(); err != nil { panic(err) }
	if err = f.Close(); err != nil { panic(err) }
}

func main() {
	if len(os.Args) != 2 { panic("one evidence directory argument required") }
	evidence, err := filepath.Abs(os.Args[1])
	if err != nil { panic(err) }
	cwd, err := os.Getwd()
	if err != nil { panic(err) }
	if os.Getenv("CC_JOB_ID") != "20261021-072955-todo-3209" { panic("unexpected job identity") }
	if git("rev-parse", "HEAD") != source { panic("unexpected initial HEAD; do not rerun") }
	if git("diff", "--name-only", source) != "" { panic("tracked source modified") }
	if err = os.Mkdir(filepath.Join(owned, "target"), 0700); err != nil { panic(err) }
	for key, value := range map[string]string{
		"CARGO_TARGET_DIR": filepath.Join(owned, "target"), "CARGO_BUILD_JOBS": "10",
		"NEXTEST_TEST_THREADS": "10", "RUST_TEST_THREADS": "10",
		"RUSTC_WRAPPER": "D:/CC/Tools/sccache/sccache.exe", "SCCACHE_DIR": "D:/CC/BuildCache/sccache",
		"SCCACHE_CACHE_SIZE": "30G", "SCCACHE_SERVER_PORT": "42259", "CARGO_TERM_COLOR": "never",
	} { if err = os.Setenv(key, value); err != nil { panic(err) } }
	commands := [][]string{
		{"nextest", "run", "--locked", "-p", "cc-scheduler", "--test", "parity", "-E", "test(retired_native_conversation_) | test(preparation2164_reuses_owned_terminal_session_only_without_other_worker)", "--no-fail-fast", "--status-level", "all"},
		{"nextest", "run", "--locked", "-p", "cc-ask", "--test", "resume", "-E", "test(completed_capacity_history_does_not_exempt_an_ordinary_discord_resume) | test(every_discord_resume_launcher_applies_policy_before_passing_a_native_session)", "--no-fail-fast", "--status-level", "all"},
		{"nextest", "run", "--locked", "-p", "cc-job-policy", "--test", "resume_policy", "-E", "test(an_eligible_same_model_capacity_attempt_keeps_its_context_and_durable_budget) | test(individual_owner_selection_keeps_the_model_effort_and_compatible_context)", "--no-fail-fast", "--status-level", "all"},
	}
	for i, args := range commands {
		name := fmt.Sprintf("run-%02d", i+1)
		meta := map[string]any{"run": i+1, "attempt": 1, "source": source, "source_tree": git("rev-parse", source+"^{tree}"),
			"execution_head": git("rev-parse", "HEAD"), "branch": git("branch", "--show-current"), "cwd": cwd,
			"job": os.Getenv("CC_JOB_ID"), "executable": gate, "args": args, "requested_command_prefix": "cargo",
			"start": time.Now().Format(time.RFC3339Nano), "resources_before": resources(), "completed": false}
		save(filepath.Join(evidence, name+".started.json"), meta, true)
		out, e := os.OpenFile(filepath.Join(evidence, name+".stdout.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil { panic(e) }
		errout, e := os.OpenFile(filepath.Join(evidence, name+".stderr.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil { panic(e) }
		cmd := hidden(gate, args...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = cwd, out, errout
		fmt.Printf("%s starting %s\n", time.Now().Format(time.RFC3339Nano), name)
		err = cmd.Start()
		if err != nil {
			meta["launch_error"] = err.Error()
		} else {
			meta["pid"] = cmd.Process.Pid
			save(filepath.Join(evidence, name+".running.json"), meta, true)
			err = cmd.Wait()
			meta["exit_code"] = cmd.ProcessState.ExitCode()
			meta["completed"] = true
		}
		if err != nil { meta["process_error"] = err.Error() }
		for _, f := range []*os.File{out, errout} {
			if e = f.Sync(); e != nil { panic(e) }
			if e = f.Close(); e != nil { panic(e) }
		}
		meta["end"], meta["resources_after"] = time.Now().Format(time.RFC3339Nano), resources()
		meta["tracked_source_changes_after"] = git("diff", "--name-only", source)
		save(filepath.Join(evidence, name+".result.json"), meta, true)
		fmt.Printf("%s finished %s exit=%v completed=%v\n", time.Now().Format(time.RFC3339Nano), name, meta["exit_code"], meta["completed"])
	}
}
