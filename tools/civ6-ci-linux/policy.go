package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Indices of the only commands the host may ever run for the Rust Linux job.
const (
	cmdFmt = iota
	cmdTest
	cmdDoc
	cmdBuild
	cmdRustup
)

var cargoTable = [4][]string{
	{"fmt", "--manifest-path", "tools/Cargo.toml", "--all", "--", "--check"},
	{"test", "--locked", "--manifest-path", "tools/Cargo.toml", "--workspace", "--all-targets"},
	{"test", "--locked", "--manifest-path", "tools/Cargo.toml", "--workspace", "--doc"},
	{"build", "--locked", "--release", "--manifest-path", "tools/Cargo.toml", "--workspace", "--bins"},
}

var rustupArgs = []string{"toolchain", "install", "1.95.0", "--profile", "minimal", "--component", "rustfmt"}

type stepPolicy struct {
	Dir string
	Env []string
}

// execPolicy is derived only from the workflow bytes whose digest the parent's
// Plan pins. Neither a request nor the environment can add a command, directory
// or environment value. The doctest step is optional because only some
// reviewed workflow revisions contain it.
type execPolicy struct {
	TimeoutMinutes int
	Present        [5]bool
	Steps          [5]stepPolicy
}

func (e execPolicy) timeout() time.Duration {
	if e.TimeoutMinutes < 1 || e.TimeoutMinutes > 360 {
		return 0
	}
	return time.Duration(e.TimeoutMinutes) * time.Minute
}

func (e execPolicy) args(i int) []string {
	if i == cmdRustup {
		return rustupArgs
	}
	return cargoTable[i]
}

// index maps an exact argv to its command index; -1 when not an allowed command.
func (e execPolicy) index(args []string, rustup bool) int {
	if rustup {
		if reflect.DeepEqual(args, rustupArgs) && e.Present[cmdRustup] {
			return cmdRustup
		}
		return -1
	}
	for i, want := range cargoTable {
		if e.Present[i] && reflect.DeepEqual(args, want) {
			return i
		}
	}
	return -1
}

func parseYAML(data []byte) (*yaml.Node, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("workflow size rejected")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	var root, extra yaml.Node
	if d.Decode(&root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode || d.Decode(&extra) != io.EOF {
		return nil, errors.New("workflow document rejected")
	}
	var walk func(*yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return errors.New("workflow alias rejected")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
					return errors.New("workflow mapping rejected")
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if e := walk(c); e != nil {
				return e
			}
		}
		return nil
	}
	if e := walk(&root); e != nil {
		return nil, e
	}
	return root.Content[0], nil
}

func field(n *yaml.Node, k string) *yaml.Node {
	if n != nil && n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == k {
				return n.Content[i+1]
			}
		}
	}
	return nil
}

var envKey = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")

func mergeEnv(parent []string, n *yaml.Node) ([]string, error) {
	values := map[string]string{}
	for _, s := range parent {
		k, v, _ := strings.Cut(s, "=")
		values[k] = v
	}
	if n != nil {
		if n.Kind != yaml.MappingNode {
			return nil, errors.New("workflow env must be a literal map")
		}
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i].Value, n.Content[i+1]
			if !envKey.MatchString(k) || v.Kind != yaml.ScalarNode || strings.Contains(v.Value, "$"+"{{") || strings.ContainsAny(v.Value, "\x00\r\n") {
				return nil, errors.New("workflow env cannot be reproduced literally")
			}
			if strings.HasPrefix(k, "GITHUB_") || strings.HasPrefix(k, "RUNNER_") {
				return nil, errors.New("workflow overrides immutable Actions context")
			}
			for _, protected := range []string{"HOME", "PATH", "CARGO_HOME", "RUSTUP_HOME", "CARGO_TARGET_DIR", "RUSTC", "RUSTDOC", "CARGO", "RUSTUP_TOOLCHAIN", "RUSTC_WRAPPER", "RUSTC_WORKSPACE_WRAPPER", "BASH_ENV", "ENV", "LD_PRELOAD", "LD_LIBRARY_PATH", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_NO_REPLACE_OBJECTS", "TMPDIR", "TEMP", "TMP"} {
				if k == protected {
					return nil, fmt.Errorf("workflow overrides isolated tool setting: %s", k)
				}
			}
			values[k] = v.Value
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		out = append(out, k+"="+values[k])
	}
	return out, nil
}

func defaultDir(n *yaml.Node) string {
	if d := field(field(field(n, "defaults"), "run"), "working-directory"); d != nil {
		return d.Value
	}
	return ""
}

func resolveDir(s string) (string, error) {
	if s == "" || s == "." {
		return workspace, nil
	}
	if path.IsAbs(s) || path.Clean(s) != s || s == ".." || strings.HasPrefix(s, "../") || strings.ContainsAny(s, "\\\r\n\x00") || strings.Contains(s, "$"+"{{") {
		return "", errors.New("workflow working-directory cannot be reproduced")
	}
	return workspace + "/" + s, nil
}

// workflowExecution reads the Rust Linux job. A line starting with cargo or
// rustup that is not exactly one of the fixed commands is rejected; it is never
// replaced by a "similar" command.
func workflowExecution(data []byte) (execPolicy, error) {
	var out execPolicy
	root, e := parseYAML(data)
	if e != nil {
		return out, e
	}
	job := field(field(root, "jobs"), "test")
	steps := field(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return out, errors.New("Rust steps missing")
	}
	// GitHub's default job limit is 360 minutes when the key is omitted.
	out.TimeoutMinutes = 360
	if t := field(job, "timeout-minutes"); t != nil {
		if t.Kind != yaml.ScalarNode {
			return out, errors.New("job timeout rejected")
		}
		if out.TimeoutMinutes, e = strconv.Atoi(t.Value); e != nil {
			return out, errors.New("job timeout rejected")
		}
	}
	if out.timeout() == 0 {
		return out, errors.New("job timeout rejected")
	}
	env, e := mergeEnv(nil, field(root, "env"))
	if e == nil {
		env, e = mergeEnv(env, field(job, "env"))
	}
	if e != nil {
		return out, e
	}
	dir0 := defaultDir(root)
	if s := defaultDir(job); s != "" {
		dir0 = s
	}
	for _, step := range steps.Content {
		run := field(step, "run")
		if run == nil {
			continue
		}
		stepEnv, e := mergeEnv(env, field(step, "env"))
		if e != nil {
			return out, e
		}
		dir := dir0
		if d := field(step, "working-directory"); d != nil {
			dir = d.Value
		}
		for _, raw := range strings.Split(run.Value, "\n") {
			line := strings.Join(strings.Fields(raw), " ")
			f := strings.Fields(line)
			if len(f) == 0 || (f[0] != "cargo" && f[0] != "rustup") {
				continue
			}
			index := -1
			for i, want := range cargoTable {
				if line == "cargo "+strings.Join(want, " ") {
					index = i
				}
			}
			if line == "rustup "+strings.Join(rustupArgs, " ") {
				index = cmdRustup
			}
			if index < 0 {
				return out, errors.New("unrecognized cargo/rustup command; no substitute is run")
			}
			for _, sl := range strings.Split(run.Value, "\n") {
				sl = strings.TrimSpace(sl)
				for _, prefix := range []string{"export ", "source ", ". ", "cd ", "Set-Location ", "$env:"} {
					if strings.HasPrefix(sl, prefix) {
						return out, errors.New("command changes shell environment/directory; context unsupported")
					}
				}
			}
			resolved, e := resolveDir(dir)
			if e != nil {
				return out, e
			}
			value := stepPolicy{Dir: resolved, Env: stepEnv}
			if out.Present[index] && !reflect.DeepEqual(out.Steps[index], value) {
				return out, errors.New("ambiguous execution contexts for one command")
			}
			out.Steps[index] = value
			out.Present[index] = true
		}
	}
	for _, i := range []int{cmdFmt, cmdTest, cmdBuild, cmdRustup} {
		if !out.Present[i] {
			return out, errors.New("required Rustup/Cargo command missing from the workflow")
		}
	}
	return out, nil
}

// toolEnv is the fixed base environment of every Cargo/Rustup process.
func toolEnv(jobs, tests int) []string {
	return []string{"CI=true", "GITHUB_ACTIONS=true", "TMPDIR=/work/tool-tmp", "HOME=/home/runner",
		"PATH=/toolchain/rustup/toolchains/1.95.0-x86_64-unknown-linux-gnu/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin",
		"CARGO_HOME=/toolchain/cargo", "RUSTUP_HOME=/toolchain/rustup", "CARGO_TARGET_DIR=" + workspace + "/tools/target",
		"CARGO_BUILD_JOBS=" + strconv.Itoa(jobs), "RUST_TEST_THREADS=" + strconv.Itoa(tests), "NEXTEST_TEST_THREADS=" + strconv.Itoa(tests),
		"CARGO=/runner/bin/cargo", "RUSTUP_TOOLCHAIN=1.95.0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1"}
}

// stepEnvironment returns the complete deduplicated environment and directory
// of one command. Literal workflow parallel bounds are clamped to the granted
// caps; lld/linker threads are bounded without changing features or tests.
func stepEnvironment(p execPolicy, index, jobs, tests int) ([]string, string, error) {
	if p.timeout() == 0 || index < 0 || index > cmdRustup || !p.Present[index] {
		return nil, "", errors.New("fixed execution policy required")
	}
	s := p.Steps[index]
	if s.Dir != workspace && !strings.HasPrefix(s.Dir, workspace+"/") {
		return nil, "", errors.New("fixed working directory rejected")
	}
	fixed := append([]string{}, s.Env...)
	for i, entry := range fixed {
		key, value, _ := strings.Cut(entry, "=")
		limit := 0
		switch key {
		case "CARGO_BUILD_JOBS":
			limit = jobs
		case "RUST_TEST_THREADS", "NEXTEST_TEST_THREADS":
			limit = tests
		}
		if limit > 0 {
			n, e := strconv.Atoi(value)
			if e != nil || n < 1 {
				return nil, "", errors.New("literal workflow parallel bound rejected")
			}
			if n > limit {
				n = limit
			}
			fixed[i] = key + "=" + strconv.Itoa(n)
		}
	}
	// syscall.Exec does not deduplicate envp, and libc and Rust disagree about
	// first/last duplicates, which would lose the original serial bounds.
	values := map[string]string{}
	for _, entry := range append(toolEnv(jobs, tests), fixed...) {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, "", errors.New("invalid fixed process environment")
		}
		values[key] = value
	}
	if index >= cmdTest && index <= cmdBuild {
		values["RUSTFLAGS"] = strings.TrimSpace(values["RUSTFLAGS"] + " -C link-arg=-Wl,--threads=1")
		if index == cmdDoc {
			values["RUSTDOCFLAGS"] = strings.TrimSpace(values["RUSTDOCFLAGS"] + " -C link-arg=-Wl,--threads=1")
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var env []string
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env, s.Dir, nil
}
