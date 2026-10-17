package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type pr148StepPolicy struct {
	Dir string
	Env []string
}
type pr148ExecutionPolicy struct {
	TimeoutMinutes int
	Steps          [5]pr148StepPolicy
}

func pr148OriginalExecution(ctx context.Context, p pr148LinuxPlan, read api) (pr148CargoPolicy, pr148ExecutionPolicy, error) {
	var empty pr148CargoPolicy
	var exec pr148ExecutionPolicy
	if !p.valid() {
		return empty, exec, errors.New("explicit PR148 source pin required")
	}
	r, e := verify(ctx, candidate{1392152644, 148, p.Head}, read)
	if e != nil {
		return empty, exec, e
	}
	if r.FullName != pr148Repo || r.Owner.Login != "AutoDevJapan" {
		return empty, exec, errors.New("fixed repository must be verified before workflow read")
	}
	data, e := pr148Contents(ctx, read, pr148Workflows[0].Path, pr148Head)
	if e != nil {
		return empty, exec, e
	}
	return pr148WorkflowExecution(data)
}

func (p pr148ExecutionPolicy) timeout() time.Duration {
	if p.TimeoutMinutes < 1 || p.TimeoutMinutes > 360 {
		return 0
	}
	return time.Duration(p.TimeoutMinutes) * time.Minute
}
func pr148MergeEnv(parent []string, n *yaml.Node) ([]string, error) {
	values := map[string]string{}
	for _, s := range parent {
		k, v, _ := strings.Cut(s, "=")
		values[k] = v
	}
	if n != nil {
		if n.Kind != yaml.MappingNode {
			return nil, errors.New("fixed workflow env must be a literal map")
		}
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i].Value, n.Content[i+1]
			if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(k) || v.Kind != yaml.ScalarNode || strings.Contains(v.Value, "${{") || strings.ContainsAny(v.Value, "\x00\r\n") {
				return nil, errors.New("fixed workflow env cannot be reproduced literally")
			}
			if strings.HasPrefix(k, "GITHUB_") || strings.HasPrefix(k, "RUNNER_") {
				return nil, errors.New("workflow overrides immutable Actions context")
			}
			for _, protected := range []string{"HOME", "PATH", "CARGO_HOME", "RUSTUP_HOME", "CARGO_TARGET_DIR", "RUSTC", "RUSTDOC", "CARGO", "RUSTUP_TOOLCHAIN", "RUSTC_WRAPPER", "RUSTC_WORKSPACE_WRAPPER", "BASH_ENV", "ENV", "LD_PRELOAD", "LD_LIBRARY_PATH", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_NO_REPLACE_OBJECTS", "TMPDIR", "TEMP", "TMP"} {
				if k == protected {
					return nil, fmt.Errorf("fixed workflow overrides isolated tool setting: %s", k)
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
func pr148DefaultDir(n *yaml.Node) string {
	n = pr148Field(pr148Field(n, "defaults"), "run")
	d := pr148Field(n, "working-directory")
	if d == nil {
		return ""
	}
	return d.Value
}
func pr148ResolveDir(s string) (string, error) {
	if s == "" || s == "." {
		return pr148Workspace, nil
	}
	if path.IsAbs(s) || path.Clean(s) != s || s == ".." || strings.HasPrefix(s, "../") || strings.ContainsAny(s, "\\\r\n\x00") || strings.Contains(s, "${{") {
		return "", errors.New("fixed working-directory cannot be reproduced")
	}
	return pr148Workspace + "/" + s, nil
}

// The only source of these settings is the hash-pinned original YAML. Requests
// cannot supply environment values or a working directory to the host.
func pr148WorkflowExecution(data []byte) (pr148CargoPolicy, pr148ExecutionPolicy, error) {
	cargo, e := pr148Policy(data)
	var out pr148ExecutionPolicy
	if e != nil {
		return cargo, out, e
	}
	root, e := pr148YAML(data)
	if e != nil {
		return cargo, out, e
	}
	job := pr148Field(pr148Field(root, "jobs"), "test")
	timeout := pr148Field(job, "timeout-minutes")
	// GitHub's job default is 360 minutes. The original rust.yml omits this
	// key; preserve that limit without adding/changing a product workflow.
	out.TimeoutMinutes = 360
	if timeout != nil {
		if timeout.Kind != yaml.ScalarNode {
			return cargo, out, errors.New("original job timeout rejected")
		}
		out.TimeoutMinutes, e = strconv.Atoi(timeout.Value)
	}
	if e != nil || out.timeout() == 0 {
		return cargo, out, errors.New("original job timeout rejected")
	}
	env, e := pr148MergeEnv(nil, pr148Field(root, "env"))
	if e != nil {
		return cargo, out, e
	}
	env, e = pr148MergeEnv(env, pr148Field(job, "env"))
	if e != nil {
		return cargo, out, e
	}
	defaultDir := pr148DefaultDir(root)
	if s := pr148DefaultDir(job); s != "" {
		defaultDir = s
	}
	seen := [5]bool{}
	for _, step := range pr148Field(job, "steps").Content {
		run := pr148Field(step, "run")
		if run == nil {
			continue
		}
		stepEnv, e := pr148MergeEnv(env, pr148Field(step, "env"))
		if e != nil {
			return cargo, out, e
		}
		dir := defaultDir
		if d := pr148Field(step, "working-directory"); d != nil {
			dir = d.Value
		}
		dir, e = pr148ResolveDir(dir)
		if e != nil {
			return cargo, out, e
		}
		for _, line := range strings.Split(run.Value, "\n") {
			line = strings.Join(strings.Fields(line), " ")
			index := -1
			for i, s := range cargo {
				if line == "cargo "+s {
					index = i
				}
			}
			if line == "rustup "+strings.Join(pr148RustupArgs, " ") {
				index = 4
			}
			if index < 0 {
				continue
			}
			for _, shellLine := range strings.Split(run.Value, "\n") {
				shellLine = strings.TrimSpace(shellLine)
				for _, prefix := range []string{"export ", "source ", ". ", "cd ", "Set-Location ", "$env:"} {
					if strings.HasPrefix(shellLine, prefix) {
						return cargo, out, errors.New("original command changes shell environment/directory; execution context unsupported")
					}
				}
			}
			value := pr148StepPolicy{Dir: dir, Env: stepEnv}
			if seen[index] && !reflect.DeepEqual(out.Steps[index], value) {
				return cargo, out, errors.New("ambiguous original command execution contexts")
			}
			out.Steps[index] = value
			seen[index] = true
		}
	}
	for _, ok := range seen {
		if !ok {
			return cargo, out, errors.New("original Rustup/Cargo execution context missing")
		}
	}
	return cargo, out, nil
}

func pr148StepEnvironment(p pr148ExecutionPolicy, index, jobs, tests int) ([]string, string, error) {
	if p.timeout() == 0 || index < 0 || index > 4 {
		return nil, "", errors.New("fixed execution policy required")
	}
	s := p.Steps[index]
	if s.Dir != pr148Workspace && !strings.HasPrefix(s.Dir, pr148Workspace+"/") {
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
	// syscall.Exec does not deduplicate envp. libc and Rust/Cargo otherwise
	// disagree about first/last duplicate keys, losing original serial bounds.
	values := map[string]string{}
	for _, entry := range append(pr148ToolEnv(jobs, tests), fixed...) {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, "", errors.New("invalid fixed process environment")
		}
		values[key] = value
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
