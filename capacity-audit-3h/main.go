package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const runtimeRoot = `D:\CC\Tools\capacity-audit-3h`
const guardExe = `D:\CC\Tools\client-records\scrub391.exe`

type rootSpec struct {
	Category, Path string
	PublicSystem   bool
}

var roots = []rootSpec{
	{"windows", `C:\Windows`, true},
	{"installed_apps", `C:\Program Files`, true},
	{"legacy_installed_apps", `C:\Program Files (x86)`, true},
	{"machine_data", `C:\ProgramData`, false},
	{"owner_data", `C:\Users\makhmeto`, false},
	{"operations", `D:\CC`, false},
	{"deletion_requests", `D:\削除希望`, false},
}

type pendingDir struct {
	Path   string
	Offset int
}
type scanState struct {
	Pending                           []pendingDir `json:"pending"`
	Bytes                             uint64       `json:"logical_bytes"`
	Files, Directories, Links, Errors uint64
	Started                           string `json:"started"`
}
type checkpoint struct {
	Schema   int                   `json:"schema"`
	NextRoot int                   `json:"next_root"`
	Scans    map[string]*scanState `json:"scans"`
	Last     *report               `json:"last,omitempty"`
}
type categoryResult struct {
	Category    string  `json:"category"`
	Status      string  `json:"status"`
	Bytes       *uint64 `json:"logical_bytes"`
	Files       uint64  `json:"files"`
	Directories uint64  `json:"directories"`
	Links       uint64  `json:"reparse_points_skipped"`
	Errors      uint64  `json:"errors"`
	Started     string  `json:"scan_started,omitempty"`
	Completed   string  `json:"scan_completed,omitempty"`
	Increase    *int64  `json:"increase_since_previous_complete_bytes,omitempty"`
}
type report struct {
	Schema           int                `json:"schema"`
	Time             string             `json:"time"`
	Scope            string             `json:"scope"`
	Free             map[string]*uint64 `json:"free_bytes"`
	Used             map[string]*uint64 `json:"used_bytes"`
	Increase         map[string]*int64  `json:"volume_used_change_bytes"`
	Categories       []categoryResult   `json:"categories"`
	RootFiles        map[string]*uint64 `json:"known_root_file_bytes"`
	WholeDriveScan   bool               `json:"whole_drive_scan"`
	Recovered        uint64             `json:"bytes_reclaimed"`
	EFilesTouched    bool               `json:"e_files_touched"`
	EArchiveMarginOK *bool              `json:"e_at_least_500_gib_free"`
	ElapsedMS        int64              `json:"elapsed_ms"`
	Decision         *decisionReceipt   `json:"decision_queue,omitempty"`
	AllDrives        *allDriveReport    `json:"all_drives,omitempty"`
}

func within(root, child string, allowRoot bool) bool {
	if root == "" || child == "" || !filepath.IsAbs(root) || !filepath.IsAbs(child) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(child))
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return allowRoot || rel != "."
}

func safePath(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || isReparse(info) {
			return false
		}
		if filepath.Dir(current) == current {
			return true
		}
	}
}

func safeStateFile(path string) bool {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return safePath(filepath.Dir(path))
	}
	return err == nil && info.Mode().IsRegular() && !isReparse(info) && safePath(filepath.Dir(path))
}

func privateAllowed(ctx context.Context, path string) bool {
	return classifyPath(ctx, path) == "clear"
}

func classifyPath(ctx context.Context, path string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, guardExe, "--guard-path", path)
	cmd.WaitDelay = 2 * time.Second
	hide(cmd)
	// The helper returns a small JSON object; stderr is never exposed.
	cmd.Stderr = io.Discard
	output, err := cmd.Output()
	if err != nil || len(output) > 4096 {
		return "guard_unavailable"
	}
	var result struct {
		Client     *bool `json:"client_resource"`
		Unverified bool  `json:"classification_unverified"`
	}
	if json.Unmarshal(output, &result) != nil || result.Client == nil {
		return "guard_unavailable"
	}
	if result.Unverified {
		return "protected_unclassified"
	}
	if *result.Client {
		return "protected"
	}
	return "clear"
}

func scan(ctx context.Context, spec rootSpec, state *scanState, maxEntries int) categoryResult {
	result := categoryResult{Category: spec.Category, Status: "partial", Started: state.Started}
	visited := 0
	for len(state.Pending) > 0 {
		if ctx.Err() != nil || visited >= maxEntries {
			break
		}
		last := len(state.Pending) - 1
		current := state.Pending[last]
		if !within(spec.Path, current.Path, true) || current.Offset < 0 || !safePath(current.Path) {
			state.Errors++
			state.Pending = state.Pending[:last]
			continue
		}
		entries, err := os.ReadDir(current.Path)
		if err != nil {
			state.Errors++
			state.Pending = state.Pending[:last]
			continue
		}
		if current.Offset > len(entries) {
			state.Errors++
			current.Offset = 0
		}
		state.Pending = state.Pending[:last]
		for i := current.Offset; i < len(entries); i++ {
			if ctx.Err() != nil || visited >= maxEntries {
				state.Pending = append(state.Pending, pendingDir{current.Path, i})
				break
			}
			visited++
			path := filepath.Join(current.Path, entries[i].Name())
			info, err := os.Lstat(path)
			if err != nil {
				state.Errors++
				continue
			}
			if isReparse(info) {
				state.Links++
				continue
			}
			if info.IsDir() {
				state.Directories++
				state.Pending = append(state.Pending, pendingDir{path, 0})
			} else if info.Mode().IsRegular() {
				state.Files++
				if info.Size() > 0 {
					state.Bytes += uint64(info.Size())
				}
			}
		}
	}
	observedBytes := state.Bytes
	result.Bytes = &observedBytes
	result.Files = state.Files
	result.Directories = state.Directories
	result.Links = state.Links
	result.Errors = state.Errors
	if len(state.Pending) == 0 {
		result.Status = "complete_metadata_scan"
		if state.Errors > 0 {
			result.Status = "partial_with_errors"
		}
		result.Completed = time.Now().UTC().Format(time.RFC3339)
	}
	return result
}

func rootOrder(next, count int) []int {
	order := make([]int, 0, count)
	if count <= 0 {
		return order
	}
	next = ((next % count) + count) % count
	for i := 0; i < count; i++ {
		order = append(order, (next+i)%count)
	}
	return order
}

func readCheckpoint(path string) (checkpoint, error) {
	value := checkpoint{Schema: 1, Scans: make(map[string]*scanState)}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
	if err != nil || len(data) > 8*1024*1024 {
		return value, errors.New("checkpoint size")
	}
	var loaded checkpoint
	if json.Unmarshal(data, &loaded) != nil || loaded.Schema != 1 || loaded.Scans == nil {
		return checkpoint{}, errors.New("checkpoint format")
	}
	return loaded, nil
}

func saveCheckpoint(path string, value checkpoint) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "capacity-state-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func knownRootFiles() map[string]*uint64 {
	result := make(map[string]*uint64)
	for category, path := range map[string]string{
		"pagefile": `C:\pagefile.sys`, "hibernation": `C:\hiberfil.sys`, "swapfile": `C:\swapfile.sys`,
		"docker_virtual_disk": `C:\Users\makhmeto\AppData\Local\Docker\wsl\disk\docker_data.vhdx`,
	} {
		info, err := os.Lstat(path)
		if err == nil && !isReparse(info) && info.Mode().IsRegular() {
			n := uint64(info.Size())
			result[category] = &n
		} else {
			result[category] = nil
		}
	}
	return result
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "--inventory" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, err := discoverDevices(ctx)
		if err != nil {
			return err
		}
		inv, err := normalizeInventory(data, nil, time.Now())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(inv)
	}
	if len(os.Args) == 2 && os.Args[1] == "--all-drives-status" {
		path := filepath.Join(runtimeRoot, "all-drive-latest-v1.json")
		if !safePath(runtimeRoot) || !safeStateFile(path) {
			return errors.New("all-drive status boundary")
		}
		f, err := os.Open(path)
		if err != nil {
			return errors.New("all-drive status unavailable")
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
		var r allDriveReport
		if err != nil || len(data) > 1024*1024 || json.Unmarshal(data, &r) != nil || r.Inventory.Schema != 1 {
			return errors.New("invalid all-drive status")
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	if len(os.Args) != 1 {
		return errors.New("unsupported arguments")
	}
	if !safePath(runtimeRoot) {
		return errors.New("runtime boundary")
	}
	unlock, err := lock(filepath.Join(runtimeRoot, "run.lock"))
	if errors.Is(err, errLocked) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 165*time.Second)
	defer cancel()
	statePath := filepath.Join(runtimeRoot, "state.json")
	if !safeStateFile(statePath) || !safeStateFile(filepath.Join(runtimeRoot, "runs.jsonl")) {
		return errors.New("linked state or log")
	}
	state, err := readCheckpoint(statePath)
	if err != nil {
		return err
	}
	r := report{Schema: 1, Time: start.UTC().Format(time.RFC3339), Scope: "C/D volume measurements and bounded classified metadata; logical sizes can include hard links", Free: make(map[string]*uint64), Used: make(map[string]*uint64), Increase: make(map[string]*int64), RootFiles: knownRootFiles()}
	allCtx, allCancel := context.WithTimeout(ctx, 45*time.Second)
	all, allErr := collectAllDrives(allCtx, runtimeRoot)
	allCancel()
	if allErr != nil {
		return allErr
	}
	r.AllDrives = &all
	for _, drive := range []string{"C", "D", "E"} {
		free, total, err := space(drive + `:\`)
		if err != nil {
			r.Free[drive] = nil
			r.Used[drive] = nil
			continue
		}
		used := total - free
		r.Free[drive] = &free
		r.Used[drive] = &used
		if state.Last != nil && state.Last.Used[drive] != nil {
			n := int64(used) - int64(*state.Last.Used[drive])
			r.Increase[drive] = &n
		}
	}
	if r.Free["C"] == nil || r.Free["D"] == nil {
		return errors.New("C/D volume measurement failed")
	}
	if r.Free["E"] != nil {
		ok := *r.Free["E"] >= 500*1024*1024*1024
		r.EArchiveMarginOK = &ok
	}
	byCategory := make(map[string]categoryResult)
	for _, index := range rootOrder(state.NextRoot, len(roots)) {
		spec := roots[index]
		if ctx.Err() != nil {
			byCategory[spec.Category] = categoryResult{Category: spec.Category, Status: "not_scanned_deadline"}
			continue
		}
		if !safePath(spec.Path) {
			byCategory[spec.Category] = categoryResult{Category: spec.Category, Status: "missing_linked_or_unavailable"}
			delete(state.Scans, spec.Category)
			continue
		}
		if !spec.PublicSystem && !privateAllowed(ctx, spec.Path) {
			byCategory[spec.Category] = categoryResult{Category: spec.Category, Status: "protected_unclassified"}
			delete(state.Scans, spec.Category)
			continue
		}
		st := state.Scans[spec.Category]
		if st == nil || len(st.Pending) == 0 {
			st = &scanState{Pending: []pendingDir{{spec.Path, 0}}, Started: start.UTC().Format(time.RFC3339)}
			state.Scans[spec.Category] = st
		}
		categoryCtx, categoryCancel := context.WithTimeout(ctx, 25*time.Second)
		result := scan(categoryCtx, spec, st, 150000)
		categoryCancel()
		if result.Status == "complete_metadata_scan" && state.Last != nil {
			for _, old := range state.Last.Categories {
				if old.Category == spec.Category && old.Status == "complete_metadata_scan" && old.Bytes != nil {
					n := int64(*result.Bytes) - int64(*old.Bytes)
					result.Increase = &n
				}
			}
		}
		byCategory[spec.Category] = result
	}
	for _, spec := range roots {
		r.Categories = append(r.Categories, byCategory[spec.Category])
	}
	r.ElapsedMS = time.Since(start).Milliseconds()
	state.NextRoot = (state.NextRoot + 1) % len(roots)
	state.Last = &r
	if err := saveCheckpoint(statePath, state); err != nil {
		return err
	}
	// Persist measurements first; queue failure cannot erase the scan. A retry
	// uses the shared ToDo lock and reuses the same finite decision row.
	r.Decision = queueDecision(r)
	state.Last = &r
	if err := saveCheckpoint(statePath, state); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(runtimeRoot, "runs.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(log).Encode(r)
	if err == nil {
		err = log.Sync()
	}
	closeErr := log.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// A GUI-subsystem scheduled process can have no stdout handle. Persisted
	// and synced receipts are mandatory; the optional console copy is not.
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if r.Decision.Status == "queue_unavailable" {
		return errors.New("decision queue unavailable; measurements retained")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "capacity audit failed; no files were deleted")
		os.Exit(1)
	}
}
