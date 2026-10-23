package observer

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunCLI defaults to a side-effect-free description. OS observation requires an
// explicit finite mode and a scratch state path; there is no activation mode.
func RunCLI(args []string, out io.Writer) int {
	return runCLI(args, out, func(ctx context.Context, path string) Result {
		w := &WindowsSource{}
		e := Engine{Store: &FileStore{Path: path}, Clock: SystemClock{}, Processes: w, Events: w, Directives: UnknownDirectives{}}
		return e.Tick(ctx)
	})
}

func runCLI(args []string, out io.Writer, observe func(context.Context, string) Result) int {
	mode := "explain"
	if len(args) > 0 {
		mode = args[0]
		args = args[1:]
	}
	if mode == "explain" && len(args) == 0 {
		_ = json.NewEncoder(out).Encode(struct {
			Mode        string `json:"mode"`
			Description string `json:"description"`
			LiveEnabled bool   `json:"live_activation_enabled"`
			LiveCalls   int    `json:"live_activation_calls"`
		}{"explain", "Finite Windows read-only observation. Explicit observe-once --state <scratch.json> saves a baseline and metadata. Native intent and stop source remain unknown; no OS activation exists.", false, 0})
		return 0
	}
	invalid := func() int {
		_ = json.NewEncoder(out).Encode(Result{Stage: "arguments", Decision: "unknown", Reason: "explicit_observe_once_and_scratch_state_required"})
		return 2
	}
	if mode != "observe-once" {
		return invalid()
	}
	flags := flag.NewFlagSet("observe-once", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("state", "", "scratch JSON state file")
	if flags.Parse(args) != nil || flags.NArg() != 0 || !scratchPath(*path) {
		return invalid()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := observe(ctx, *path)
	if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	if r.Decision == "persist_failed" || r.Decision == "busy" {
		return 1
	}
	return 0
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func scratchPath(path string) bool {
	if path == "" || !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".json") {
		return false
	}
	path = filepath.Clean(path)
	allowed := inside(filepath.Clean(os.TempDir()), path)
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err = os.Lstat(filepath.Join(dir, ".git")); err == nil {
			allowed = allowed || inside(filepath.Join(dir, ".ci-work"), path)
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if !allowed {
		return false
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() {
		return false
	}
	for _, candidate := range []string{path, path + ".lock"} {
		for p := candidate; ; p = filepath.Dir(p) {
			info, err := os.Lstat(p)
			if err != nil && !os.IsNotExist(err) {
				return false
			}
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				return false
			}
			if p == candidate && err == nil && !info.Mode().IsRegular() {
				return false
			}
			if filepath.Dir(p) == p {
				break
			}
		}
	}
	return true
}
