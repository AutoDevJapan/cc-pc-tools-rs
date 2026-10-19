package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Deps holds every effect the controller can perform. Production wires the
// pinned fixed executables; tests substitute fakes without changing the logic.
type Deps struct {
	Cfg     Config
	CfgPath string
	Store   *Store
	Now     func() time.Time
	Reader  func() *Reader
	C       Contract
	Helpers map[string]Helper
	Exec    ExecFunc
	Admin   AdminFunc
	SD      SafeDelete
	Token   TokenSource
	Alive   func(ProcID) bool
	Spawn   func(key string) (ProcID, bool, error)
	Self    func() ProcID
	LockOS  func(osName string) (func(), error)
}

func (d *Deps) home(parts ...string) string {
	return filepath.Join(append([]string{d.Cfg.Home}, parts...)...)
}

func safeKey(key string) string { return strings.ReplaceAll(key, "/", "-") }

func (d *Deps) mutate(key string, fn func(l *Ledger, e *Entry)) error {
	return d.Store.Update(func(l *Ledger) error {
		e := l.Entries[key]
		if e == nil {
			return errors.New("ledger entry missing")
		}
		fn(l, e)
		return nil
	})
}

// NewDeps wires the production effects from the deployed configuration.
func NewDeps(cfgPath string) (*Deps, error) {
	cfg, e := LoadConfig(cfgPath)
	if e != nil {
		return nil, e
	}
	d := &Deps{Cfg: cfg, CfgPath: cfgPath, Now: time.Now, C: activeContract, Exec: runPinned, Alive: procAlive}
	if cfg.AdminBroker.Path != "" {
		d.Admin = realAdmin(cfg.AdminBroker)
	}
	d.Store = &Store{Path: filepath.Join(cfg.Home, "ledger.json"), Now: time.Now}
	d.Reader = func() *Reader {
		return &Reader{GH: cfg.GH, Run: realRun, CacheDir: filepath.Join(cfg.Home, "cache"), MaxCalls: 40}
	}
	d.SD = fixedSafeDelete{Exe: cfg.SafeDelete, Run: realRun}
	d.Token = ghToken{GH: cfg.GH, Run: realRun}
	d.Helpers = map[string]Helper{}
	for name, h := range cfg.Helpers {
		if name == "Windows" {
			d.Helpers[name] = windowsHelper{cfg: h}
		} else {
			d.Helpers[name] = linuxHelper{cfg: h}
		}
	}
	d.Self = func() ProcID {
		id, _ := procIdentity(uint32(os.Getpid()))
		return id
	}
	d.LockOS = func(osName string) (func(), error) {
		if osName != "Linux" && osName != "Windows" {
			return nil, errors.New("unknown OS lock")
		}
		if e := os.MkdirAll(d.home("locks"), 0700); e != nil {
			return nil, e
		}
		return tryLock(d.home("locks", "os-"+osName+".lock"))
	}
	d.Spawn = func(key string) (ProcID, bool, error) {
		exe, e := os.Executable()
		if e != nil {
			return ProcID{}, false, e
		}
		c := exec.Command(exe, "worker", "--config", cfgPath, "--entry", key)
		c.Env = scrubbedEnv()
		breakaway, e := startDetached(c)
		if e != nil {
			return ProcID{}, false, errors.New("supervisor could not be started")
		}
		id, _ := procIdentity(uint32(c.Process.Pid))
		_ = c.Process.Release()
		return id, breakaway, nil
	}
	return d, nil
}

// Result is written by the supervisor before its final ledger update, so a
// crash between the two can be reconciled read-only from the file.
type Result struct {
	Key      string      `json:"key"`
	State    string      `json:"state"`
	Reason   string      `json:"reason,omitempty"`
	ExitCode *int        `json:"exit_code,omitempty"`
	Official OfficialRec `json:"official"`
	Cleanup  CleanupRec  `json:"cleanup"`
	At       string      `json:"at"`
}

func (d *Deps) resultPath(key string) string {
	return d.home("entries", safeKey(key), "result.json")
}

func (d *Deps) readResult(key string) (Result, bool) {
	var r Result
	f, e := os.Open(d.resultPath(key))
	if e != nil {
		return r, false
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 256<<10))
	if e != nil || json.Unmarshal(b, &r) != nil || r.Key != key {
		return Result{}, false
	}
	return r, terminalState(r.State) || r.State == StateNeedsRecovery
}

func writeAtomic(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".r-*.tmp")
	if e != nil {
		return e
	}
	name := f.Name()
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	if c := f.Close(); e == nil {
		e = c
	}
	if e == nil {
		e = replaceFile(name, path)
	}
	if e != nil {
		_ = os.Remove(name)
	}
	return e
}
