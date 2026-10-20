// One-off Windows measurement harness for the finite ToDo3214 assignment.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const fixed = "684bfba418f1b293b5ed2672038654ede6323fd6"
const gate = "D:/CC/Tools/codex-jobs/cargo_gate.exe"

type fileEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Blob   string `json:"git_blob"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

var evidence, owned, work string

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func writeJSON(name string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(evidence, name), append(b, '\n'), 0644))
}
func command(dir, exe string, args ...string) *exec.Cmd {
	c := exec.Command(exe, args...)
	c.Dir = dir
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}
func capture(dir, exe string, args ...string) []byte {
	c := command(dir, exe, args...)
	var errout bytes.Buffer
	c.Stderr = &errout
	b, err := c.Output()
	if err != nil {
		panic(fmt.Errorf("%s %q: %w: %s", exe, args, err, errout.String()))
	}
	return b
}
func tree() []fileEntry {
	b := capture(work, "git", "ls-tree", "-rz", fixed)
	var rows []fileEntry
	for _, rec := range bytes.Split(b, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		p := bytes.SplitN(rec, []byte{'\t'}, 2)
		if len(p) != 2 {
			panic("invalid ls-tree entry")
		}
		f := strings.Fields(string(p[0]))
		if len(f) != 3 || f[1] != "blob" || (f[0] != "100644" && f[0] != "100755") {
			panic("unsupported tree mode")
		}
		rows = append(rows, fileEntry{Path: string(p[1]), Mode: f[0], Blob: f[2]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	return rows
}
func extract(archive, destination string) {
	must(os.Mkdir(destination, 0755))
	f, err := os.Open(archive)
	must(err)
	defer f.Close()
	r := tar.NewReader(f)
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		must(err)
		rel := filepath.FromSlash(h.Name)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			panic("archive path escape")
		}
		p := filepath.Join(destination, rel)
		switch h.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			continue
		case tar.TypeDir:
			must(os.MkdirAll(p, 0755))
		case tar.TypeReg, tar.TypeRegA:
			must(os.MkdirAll(filepath.Dir(p), 0755))
			out, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			must(err)
			_, copyErr := io.Copy(out, r)
			closeErr := out.Close()
			must(copyErr)
			must(closeErr)
		default:
			panic(fmt.Sprintf("unsupported archive entry %q", h.Name))
		}
	}
}
func compare(source string, rows []fileEntry) ([]fileEntry, []map[string]any) {
	var diffs []map[string]any
	for i := range rows {
		b, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(rows[i].Path)))
		if err != nil {
			diffs = append(diffs, map[string]any{"path": rows[i].Path, "error": err.Error()})
			continue
		}
		h := sha1.New()
		fmt.Fprintf(h, "blob %d%c", len(b), byte(0))
		_, err = h.Write(b)
		must(err)
		actual := hex.EncodeToString(h.Sum(nil))
		rows[i].Size = int64(len(b))
		rows[i].SHA256 = digest(b)
		if actual != rows[i].Blob {
			// Compare against the exact blob only for mismatches, without printing content.
			raw := capture(work, "git", "cat-file", "blob", rows[i].Blob)
			diffs = append(diffs, map[string]any{"path": rows[i].Path, "expected_blob": rows[i].Blob, "actual_blob": actual, "expected_bytes": len(raw), "actual_bytes": len(b), "crlf_to_lf_equals_blob": bytes.Equal(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), raw)})
		}
	}
	actualFiles := 0
	must(filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source link: %s", p)
		}
		if !d.IsDir() {
			actualFiles++
		}
		return nil
	}))
	if actualFiles != len(rows) {
		diffs = append(diffs, map[string]any{"unexpected_file_count": actualFiles, "tree_files": len(rows)})
	}
	return rows, diffs
}
func snapshot(name string) {
	rows, diffs := compare(filepath.Join(owned, "source"), tree())
	writeJSON(name, map[string]any{"commit": fixed, "utc": time.Now().UTC().Format(time.RFC3339Nano), "tree": strings.TrimSpace(string(capture(work, "git", "rev-parse", fixed+"^{tree}"))), "files": rows, "differences": diffs, "identical": len(diffs) == 0})
	if len(diffs) != 0 {
		panic("fixed source differs; inspect " + name)
	}
}
func prepare() {
	if _, err := os.Stat(filepath.Join(owned, "source")); err == nil {
		panic("source already exists")
	}
	blank := filepath.Join(owned, "empty-attributes")
	must(os.WriteFile(blank, nil, 0644))
	archive := filepath.Join(owned, "initial.tar")
	args := []string{"-c", "core.autocrlf=false", "-c", "core.eol=lf", "-c", "core.attributesFile=" + blank, "archive", "--format=tar", "--output=" + archive, fixed}
	capture(work, "git", args...)
	initial := filepath.Join(owned, "initial-source")
	extract(archive, initial)
	rows, diffs := compare(initial, tree())
	writeJSON("archive-initial.json", map[string]any{"args": args, "files": len(rows), "differences": diffs, "utc": time.Now().UTC().Format(time.RFC3339Nano)})
	if len(diffs) == 0 {
		must(os.Rename(initial, filepath.Join(owned, "source")))
	} else {
		// The isolated Git admin/config/attrs affect only this owned archive operation.
		admin := filepath.Join(owned, "archive-git")
		emptyTemplate := filepath.Join(owned, "empty-template")
		must(os.Mkdir(emptyTemplate, 0755))
		capture(owned, "git", "init", "--bare", "--template="+emptyTemplate, admin)
		attributes := "* -text -eol -crlf -filter -ident -working-tree-encoding -export-ignore -export-subst\n"
		must(os.MkdirAll(filepath.Join(admin, "info"), 0755))
		must(os.WriteFile(filepath.Join(admin, "info", "attributes"), []byte(attributes), 0644))
		common := strings.TrimSpace(string(capture(work, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")))
		objects := filepath.Join(common, "objects")
		if strings.ContainsAny(objects, "\r\n") {
			panic("invalid objects path")
		}
		must(os.WriteFile(filepath.Join(admin, "objects", "info", "alternates"), []byte(filepath.ToSlash(objects)+"\n"), 0644))
		rawTar := filepath.Join(owned, "raw.tar")
		rawArgs := []string{"--git-dir=" + admin, "-c", "core.autocrlf=false", "-c", "core.eol=lf", "-c", "core.attributesFile=" + blank, "archive", "--format=tar", "--output=" + rawTar, fixed}
		capture(owned, "git", rawArgs...)
		extract(rawTar, filepath.Join(owned, "source"))
		b, err := os.ReadFile(rawTar)
		must(err)
		writeJSON("archive-raw.json", map[string]any{"args": rawArgs, "isolated_info_attributes": attributes, "archive_sha256": digest(b), "archive_bytes": len(b), "original_config_modified": false})
	}
	snapshot("source-before.json")
	fmt.Println("fixed source prepared and every blob verified")
}
func run(n int) {
	if n != 1 && n != 2 {
		panic("only two authorized commands")
	}
	if n == 2 {
		if _, err := os.Stat(filepath.Join(evidence, "command-1.result.json")); err != nil {
			panic("command 1 must finish first")
		}
	}
	prefix := fmt.Sprintf("command-%d", n)
	started := filepath.Join(evidence, prefix+".started.json")
	f, err := os.OpenFile(started, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	must(err)
	must(f.Close())
	snapshot(prefix + ".source-before.json")
	var args []string
	if n == 1 {
		args = []string{"nextest", "run", "--locked", "-p", "cc-job-policy", "--test", "product_identity", "-E", "test(mcp_live_requester_identity_drives_formal_dependency_priority) | test(mcp_live_requester_dependency_exclusion_controls) | test(mcp_history_only_product_job_does_not_borrow_code_priority)", "--no-fail-fast"}
	} else {
		args = []string{"nextest", "run", "--locked", "-p", "localai-web-rs", "--lib", "-E", "test(mcp_live_requesters_supply_product_priority_before_dependency_propagation) | test(mcp_duplicate_task_ids_preserve_every_private_job_edge) | test(mcp_missing_policy_has_no_runtime_binding_override) | test(mcp_related_private_ids_mask_both_display_lists)", "--no-fail-fast"}
	}
	outPath := filepath.Join(evidence, prefix+".stdout.log")
	errPath := filepath.Join(evidence, prefix+".stderr.log")
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	must(err)
	errout, err := os.OpenFile(errPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	must(err)
	c := command(filepath.Join(owned, "source"), gate, args...)
	c.Stdout = out
	c.Stderr = errout
	c.Env = os.Environ()
	settings := map[string]string{"CARGO_TARGET_DIR": filepath.Join(owned, "target"), "CARGO_BUILD_JOBS": "10", "NEXTEST_TEST_THREADS": "10", "RUSTC_WRAPPER": "D:/CC/Tools/sccache/sccache.exe", "SCCACHE_DIR": "D:/CC/BuildCache/sccache", "SCCACHE_CACHE_SIZE": "30G", "SCCACHE_SERVER_PORT": "42259", "CARGO_TERM_COLOR": "never"}
	for k, v := range settings {
		for i := len(c.Env) - 1; i >= 0; i-- {
			if strings.HasPrefix(strings.ToUpper(c.Env[i]), k+"=") {
				c.Env = append(c.Env[:i], c.Env[i+1:]...)
			}
		}
		c.Env = append(c.Env, k+"="+v)
	}
	start := time.Now()
	meta := map[string]any{"commit": fixed, "executable": gate, "logical_executable": "cargo", "args": args, "cwd": c.Dir, "settings": settings, "started_utc": start.UTC().Format(time.RFC3339Nano), "selected_expected": map[int]int{1: 3, 2: 4}[n], "job": os.Getenv("CC_JOB_ID"), "at_most_once": true, "stdout": "command-" + fmt.Sprint(n) + ".stdout.log", "stderr": "command-" + fmt.Sprint(n) + ".stderr.log"}
	writeJSON(prefix+".started.json", meta)
	startErr := c.Start()
	if startErr != nil {
		meta["launch_error"] = startErr.Error()
		meta["exit"] = -1
	} else {
		meta["pid"] = c.Process.Pid
		writeJSON(prefix+".started.json", meta)
		waitErr := c.Wait()
		meta["exit"] = c.ProcessState.ExitCode()
		if waitErr != nil {
			meta["wait_error"] = waitErr.Error()
		}
	}
	must(out.Close())
	must(errout.Close())
	meta["finished_utc"] = time.Now().UTC().Format(time.RFC3339Nano)
	meta["elapsed_seconds"] = time.Since(start).Seconds()
	b, err := os.ReadFile(outPath)
	must(err)
	meta["stdout_sha256"] = digest(b)
	meta["stdout_bytes"] = len(b)
	b, err = os.ReadFile(errPath)
	must(err)
	meta["stderr_sha256"] = digest(b)
	meta["stderr_bytes"] = len(b)
	writeJSON(prefix+".result.json", meta)
	snapshot(prefix + ".source-after.json")
	fmt.Printf("%s ended: exit=%v; complete raw logs and source verification saved\n", prefix, meta["exit"])
}
func main() {
	if len(os.Args) != 5 {
		panic("usage: measure3214 <prepare|run1|run2|verify> <evidence> <owned> <worktree>")
	}
	evidence = os.Args[2]
	owned = os.Args[3]
	work = os.Args[4]
	switch os.Args[1] {
	case "prepare":
		prepare()
	case "run1":
		run(1)
	case "run2":
		run(2)
	case "verify":
		snapshot("source-final.json")
	default:
		panic("unknown action")
	}
}
