package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var jobsRoot = "D:/CC/Tools/codex-jobs/jobs"

const jobsExe = "D:/CC/Tools/codex-jobs/codex_jobs.exe"
const todoExe = "D:/CC/Tools/todo/todo.exe"

var root = "D:/CC/Tools/x-bookmarks"

type State struct {
	Date string `json:"date"`
	Job  string `json:"job"`
	Todo string `json:"todo"`
	Mode string `json:"mode,omitempty"`
}

type ManualRun struct {
	Date      string `json:"date"`
	Todo      int    `json:"todo"`
	Mode      string `json:"mode"`
	Completed int    `json:"completed_count"`
	Target    int    `json:"target_count"`
	Entries   []struct {
		Done         bool `json:"done_registered"`
		Unbookmarked bool `json:"unbookmarked"`
	} `json:"entries"`
}

func manualRun(s State) (ManualRun, error) {
	var r ManualRun
	b, err := os.ReadFile(filepath.Join(root, "runs", s.Date+".json"))
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	completed := 0
	for _, entry := range r.Entries {
		if entry.Done && entry.Unbookmarked {
			completed++
		}
	}
	if r.Mode != "dedicated-manual" || r.Date != s.Date || strconv.Itoa(r.Todo) != s.Todo || r.Completed < 1 || r.Completed != completed || r.Target < r.Completed {
		return r, fmt.Errorf("manual run is not backed by completed item receipts")
	}
	return r, nil
}

type Job struct {
	Status string `json:"status"`
}
type Runner func(string, ...string) (string, error)

func launchKey(day string) string {
	h := sha256.Sum256([]byte("cc-x-bookmarks:" + day))
	return hex.EncodeToString(h[:16])
}
func dayOf(t time.Time) string { return t.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02") }
func readState(path string) (State, error) {
	var s State
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		entries, historyErr := os.ReadDir(filepath.Join(filepath.Dir(path), "runs"))
		if historyErr != nil && !errors.Is(historyErr, os.ErrNotExist) {
			return s, historyErr
		}
		if len(entries) > 0 {
			return s, fmt.Errorf("state is missing while daily run history exists; restore it or explicitly adopt the verified manual run")
		}
		done, doneErr := os.Stat(filepath.Join(filepath.Dir(path), "done.txt"))
		if doneErr != nil && !errors.Is(doneErr, os.ErrNotExist) {
			return s, doneErr
		}
		if done != nil && done.Size() > 0 {
			return s, fmt.Errorf("state is missing while completed-item history exists; do not invent a fresh launch")
		}
		return s, nil
	}
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	return s, e
}
func saveState(path string, s State) error {
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	tmp := path + ".tmp"
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp, path)
}
func recordedStatus(job string) (string, error) {
	if job == "" {
		return "", nil
	}
	if filepath.Base(job) != job {
		return "", fmt.Errorf("invalid job id")
	}
	b, e := os.ReadFile(filepath.Join(jobsRoot, job, "job.json"))
	if e != nil {
		return "", e
	}
	var j Job
	if e = json.Unmarshal(b, &j); e != nil {
		return "", e
	}
	return j.Status, nil
}
func live(job string) (bool, error) {
	status, err := recordedStatus(job)
	return status == "running" || status == "starting" || status == "queued", err
}
func hiddenRun(exe string, args ...string) (string, error) {
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	b, e := c.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("%s: %w: %s", filepath.Base(exe), e, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}
func execute(day string, s State, statePath string, run Runner, check func(string) (bool, error)) (State, string, error) {
	if s.Date == day && s.Mode == "manual" {
		r, err := manualRun(s)
		if err != nil {
			return s, "", err
		}
		return s, fmt.Sprintf("manual batch %d/%d already recorded in ToDo %s; no duplicate worker started", r.Completed, r.Target, s.Todo), nil
	}
	if s.Date == day && s.Job != "" {
		active, err := check(s.Job)
		if err != nil {
			return s, "", err
		}
		if !active {
			status, err := recordedStatus(s.Job)
			if err != nil {
				return s, "", err
			}
			if status != "succeeded" {
				return s, "", fmt.Errorf("today's recorded job is %q; original ToDo %s remains unfinished", status, s.Todo)
			}
		}
		return s, "already launched today", nil
	}
	if s.Job != "" {
		active, e := check(s.Job)
		if e != nil {
			return s, "", e
		}
		if active {
			return s, "previous job still active", fmt.Errorf("previous job still active; retry later")
		}
	}
	if s.Date != day {
		s = State{Date: day}
		if e := saveState(statePath, s); e != nil {
			return s, "", e
		}
	}
	if s.Todo == "" {
		out, e := run(todoExe, "add", "x-bookmarks", day+"のXブックマークを上から10件Skillにする", "--spec", filepath.Join(root, "daily-prompt.md"))
		if e != nil {
			return s, "", e
		}
		fields := strings.Fields(out)
		if len(fields) != 2 || fields[0] != "added" {
			return s, "", fmt.Errorf("unexpected ToDo receipt")
		}
		s.Todo = fields[1]
		if e = saveState(statePath, s); e != nil {
			return s, "", e
		}
	}
	prompt, e := os.ReadFile(filepath.Join(root, "daily-prompt.md"))
	if e != nil {
		return s, "", e
	}
	promptPath := filepath.Join(root, "run-prompt.md")
	if e = os.WriteFile(promptPath, append(prompt, []byte("\n今回の共有ToDo番号: "+s.Todo+"。着手と完了をこの項目に反映する。\n")...), 0600); e != nil {
		return s, "", e
	}
	out, e := run(jobsExe, "run", "--name", "x-bookmarks-"+day, "--task-title", day+"のXブックマークを上から10件Skillにする", "--todo-id", s.Todo, "--project", "x-bookmarks", "--model", "gpt-6.1-sol", "--effort", "high", "--sandbox", "danger-full-access", "--cwd", "D:/CC", "--mcp-profile", "browser", "--task-type", "research", "--launch-key", launchKey(day), "--prompt-file", promptPath)
	if e != nil {
		return s, "", e
	}
	lines := strings.Split(out, "\n")
	id := strings.TrimSpace(lines[len(lines)-1])
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, " \t\r") {
		return s, "", fmt.Errorf("unexpected job receipt")
	}
	if _, e = os.Stat(filepath.Join(jobsRoot, id, "job.json")); e != nil {
		return s, "", fmt.Errorf("job receipt does not exist: %w", e)
	}
	s.Job = id
	if e = saveState(statePath, s); e != nil {
		return s, "", e
	}
	if _, e = run(todoExe, "start", s.Todo, "--job", id); e != nil {
		return s, "launched; ToDo link failed", e
	}
	return s, "launched " + id, nil
}
func lock() (func(), bool, error) {
	dll := syscall.NewLazyDLL("kernel32.dll")
	name, e := syscall.UTF16PtrFromString("Local\\CC-XBookmarks-Daily")
	if e != nil {
		return nil, false, e
	}
	h, _, e := dll.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return nil, false, e
	}
	closeHandle := dll.NewProc("CloseHandle")
	r, _, _ := dll.NewProc("WaitForSingleObject").Call(h, 0)
	if r == 0x102 {
		closeHandle.Call(h)
		return nil, false, nil
	}
	if r != 0 && r != 0x80 {
		closeHandle.Call(h)
		return nil, false, fmt.Errorf("mutex wait failed")
	}
	return func() { dll.NewProc("ReleaseMutex").Call(h); closeHandle.Call(h) }, true, nil
}
func logLine(msg string) {
	p := filepath.Join(root, "launcher.log")
	if st, e := os.Stat(p); e == nil && st.Size() > 1024*1024 {
		_ = os.Remove(p + ".1")
		_ = os.Rename(p, p+".1")
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		defer f.Close()
		fmt.Fprintln(f, time.Now().Format(time.RFC3339), msg)
	}
}
func main() {
	flag.StringVar(&root, "root", root, "runtime home; use a separate owned directory for isolated acceptance tests")
	dry := flag.Bool("dry-run", false, "show the daily plan without launching or writing")
	probe := flag.Bool("probe", false, "verify a hidden child process without starting a job")
	adopt := flag.Int("adopt-manual-todo", 0, "record today's existing manually executed ToDo, backed by the canonical daily item receipts")
	flag.Parse()
	if *probe {
		out, e := hiddenRun(os.Getenv("COMSPEC"), "/d", "/c", "echo", "hidden-child-ok")
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		logLine(out)
		fmt.Println(out)
		return
	}
	day := dayOf(time.Now())
	if *dry {
		s, e := readState(filepath.Join(root, "state.json"))
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		b, _ := json.Marshal(map[string]any{"day": day, "state": s, "launch_key": launchKey(day), "model": "gpt-6.1-sol", "mcp_profile": "browser", "prompt": filepath.Join(root, "daily-prompt.md")})
		fmt.Println(string(b))
		return
	}
	release, ok, e := lock()
	if e != nil {
		logLine(e.Error())
		os.Exit(1)
	}
	if !ok {
		return
	}
	defer release()
	if *adopt != 0 {
		if *adopt < 1 {
			logLine("invalid manual ToDo")
			os.Exit(1)
		}
		s := State{Date: day, Todo: strconv.Itoa(*adopt), Mode: "manual"}
		if _, e = manualRun(s); e == nil {
			e = saveState(filepath.Join(root, "state.json"), s)
		}
		if e != nil {
			logLine(e.Error())
			os.Exit(1)
		}
		logLine("adopted existing manual ToDo " + s.Todo + "; no job was invented or started")
		return
	}
	s, e := readState(filepath.Join(root, "state.json"))
	if e == nil {
		var msg string
		_, msg, e = execute(day, s, filepath.Join(root, "state.json"), hiddenRun, live)
		if msg != "" {
			logLine(msg)
		}
	}
	if e != nil {
		logLine(e.Error())
		os.Exit(1)
	}
}
