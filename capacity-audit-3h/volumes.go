package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This is an inventory, never deletion authority. Device IDs are hashed before
// persistence; labels, mount directories, serials and file names are not logged.
type volume struct {
	ID                 string  `json:"id"`
	Drive              string  `json:"drive,omitempty"`
	Total              *uint64 `json:"total_bytes"`
	Free               *uint64 `json:"free_bytes"`
	TotalChange        *int64  `json:"total_change_bytes"`
	FreeChange         *int64  `json:"free_change_bytes"`
	Health             string  `json:"health"`
	Status             string  `json:"status"`
	Scan               string  `json:"scan_status"`
	IdentityUnverified bool    `json:"identity_unverified,omitempty"`
}

type partition struct {
	Disk   uint32  `json:"disk"`
	Number uint32  `json:"number"`
	Type   string  `json:"type"`
	Bytes  *uint64 `json:"bytes"`
	Drive  string  `json:"drive,omitempty"`
	Keep   bool    `json:"retain_os_partition"`
}

type disk struct {
	Number uint32  `json:"number"`
	Bytes  *uint64 `json:"bytes"`
	Health string  `json:"health"`
	Status string  `json:"status"`
}

type volumeInventory struct {
	Schema     int         `json:"schema"`
	Time       string      `json:"time"`
	Status     string      `json:"status"`
	Volumes    []volume    `json:"volumes"`
	Partitions []partition `json:"partitions"`
	Disks      []disk      `json:"disks"`
	Errors     []string    `json:"errors"`
}

type rawVolume struct {
	ID                 string  `json:"id"`
	Drive              string  `json:"drive"`
	Total              *uint64 `json:"total"`
	Free               *uint64 `json:"free"`
	Health             string  `json:"health"`
	IdentityUnverified bool    `json:"identity_unverified,omitempty"`
}

type rawInventory struct {
	Volumes    []rawVolume `json:"volumes"`
	Logical    []rawVolume `json:"logical"`
	Network    []rawVolume `json:"network"`
	Partitions []partition `json:"partitions"`
	Disks      []disk      `json:"disks"`
	Errors     []string    `json:"errors"`
}

func driveLetter(value string) string {
	if len(value) == 1 && value[0] >= 'A' && value[0] <= 'Z' {
		return value
	}
	return ""
}

func inventoryID(value string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(value)))
	return hex.EncodeToString(sum[:])
}

func health(value string) string {
	switch value {
	case "Healthy", "Warning", "Unhealthy", "Unknown":
		return strings.ToLower(value)
	default:
		return "unknown"
	}
}

func normalizeInventory(data []byte, previous *volumeInventory, at time.Time) (volumeInventory, error) {
	out := volumeInventory{Schema: 1, Time: at.UTC().Format(time.RFC3339), Status: "complete_device_inventory", Volumes: []volume{}, Partitions: []partition{}, Disks: []disk{}, Errors: []string{}}
	var raw rawInventory
	if len(data) > 1024*1024 || json.Unmarshal(data, &raw) != nil || raw.Volumes == nil || raw.Partitions == nil || raw.Logical == nil || raw.Disks == nil || len(raw.Volumes) > 256 || len(raw.Logical) > 256 || len(raw.Network) > 26 || len(raw.Partitions) > 256 || len(raw.Disks) > 128 {
		return out, errors.New("invalid device inventory")
	}
	for _, code := range raw.Errors {
		switch code {
		case "volumes_unavailable", "partitions_unavailable", "disks_unavailable", "volume_fallback_unavailable", "network_drives_unavailable", "network_identity_unavailable":
			out.Errors = append(out.Errors, code)
		default:
			return out, errors.New("invalid inventory error")
		}
	}
	if len(out.Errors) > 0 {
		out.Status = "partial_device_inventory"
	}
	seen, letters := map[string]bool{}, map[string]bool{}
	appendVolume := func(v rawVolume, logical bool) error {
		letter := driveLetter(v.Drive)
		if v.Drive != "" && letter == "" {
			return errors.New("invalid drive letter")
		}
		if v.ID == "" || len(v.ID) > 1024 {
			return errors.New("invalid volume identity")
		}
		id := inventoryID(v.ID)
		if logical && seen[id] {
			return nil
		}
		if seen[id] || (letter != "" && letters[letter]) {
			return errors.New("duplicate volume identity")
		}
		seen[id] = true
		if letter != "" {
			letters[letter] = true
		}
		row := volume{ID: id, Drive: letter, Health: health(v.Health), Status: "available", Scan: "not_scanned"}
		if v.Total == nil || v.Free == nil || *v.Total == 0 || *v.Free > *v.Total {
			row.Status = "capacity_unavailable"
		} else {
			row.Total, row.Free = v.Total, v.Free
		}
		if v.IdentityUnverified {
			row.Status = "identity_unavailable"
			row.IdentityUnverified = true
			row.Total = nil
			row.Free = nil
			row.Scan = "not_scanned_identity_unavailable"
		}
		if letter == "" {
			row.Scan = "not_scanned_no_drive_letter"
		}
		if previous != nil && row.Status == "available" {
			for _, old := range previous.Volumes {
				// An offline/unavailable sample breaks the consecutive baseline.
				if old.ID == id && old.Status == "available" && old.Total != nil && old.Free != nil {
					dt, df := int64(*row.Total)-int64(*old.Total), int64(*row.Free)-int64(*old.Free)
					row.TotalChange, row.FreeChange = &dt, &df
				}
			}
		}
		out.Volumes = append(out.Volumes, row)
		return nil
	}
	for _, v := range raw.Volumes {
		if err := appendVolume(v, false); err != nil {
			return out, err
		}
	}
	for _, v := range raw.Logical {
		if err := appendVolume(v, true); err != nil {
			return out, err
		}
	}
	for _, v := range raw.Network {
		if err := appendVolume(v, true); err != nil {
			return out, err
		}
	}
	if previous != nil {
		for _, old := range previous.Volumes {
			if old.IdentityUnverified && letters[old.Drive] {
				continue
			}
			if !seen[old.ID] {
				// Discovery errors do not prove physical disconnection.
				status := "disconnected"
				if len(out.Errors) > 0 {
					status = "discovery_unavailable"
				}
				out.Volumes = append(out.Volumes, volume{ID: old.ID, Drive: old.Drive, Health: "unknown", Status: status, Scan: "not_scanned_unavailable"})
			}
		}
	}
	for _, p := range raw.Partitions {
		p.Drive = driveLetter(p.Drive)
		switch p.Type {
		case "System", "Reserved", "Recovery":
			p.Keep = true
		case "Basic", "IFS":
		default:
			p.Type = "Other"
		}
		out.Partitions = append(out.Partitions, p)
	}
	for _, d := range raw.Disks {
		d.Health = health(d.Health)
		// No provider-specific diagnostic text in the anonymous report.
		switch d.Status {
		case "Online", "Offline", "No Media", "Unknown":
		default:
			d.Status = "Unknown"
		}
		out.Disks = append(out.Disks, d)
	}
	sort.Slice(out.Volumes, func(i, j int) bool { return out.Volumes[i].ID < out.Volumes[j].ID })
	return out, nil
}

type driveCursor struct {
	Path  string `json:"path"`
	After string `json:"after"`
}

type driveScan struct {
	Root        string        `json:"root"`
	Pending     []driveCursor `json:"pending"`
	Bytes       uint64        `json:"logical_bytes"`
	Files       uint64        `json:"files"`
	Directories uint64        `json:"directories"`
	Links       uint64        `json:"reparse_points_skipped"`
	Errors      uint64        `json:"read_errors"`
	Blocked     uint64        `json:"protected_or_unclassified"`
	Started     string        `json:"started"`
}

type driveResult struct {
	ID             string  `json:"volume_id"`
	Drive          string  `json:"drive"`
	Status         string  `json:"status"`
	Bytes          *uint64 `json:"observed_logical_bytes"`
	Files          uint64  `json:"files"`
	Directories    uint64  `json:"directories"`
	Links          uint64  `json:"reparse_points_skipped"`
	Errors         uint64  `json:"read_errors"`
	Blocked        uint64  `json:"protected_or_unclassified"`
	Started        string  `json:"scan_started,omitempty"`
	Classification string  `json:"classification"`
}

type allDriveState struct {
	Schema    int                   `json:"schema"`
	Next      int                   `json:"next"`
	Inventory *volumeInventory      `json:"inventory"`
	Scans     map[string]*driveScan `json:"scans"`
}

type allDriveReport struct {
	Inventory         volumeInventory `json:"inventory"`
	Scans             []driveResult   `json:"scans"`
	WholeDriveScan    bool            `json:"whole_drive_scan"`
	DeletionAuthority bool            `json:"deletion_authority"`
}

func readAllDriveState(path string) (allDriveState, error) {
	st := allDriveState{Schema: 1, Scans: map[string]*driveScan{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, errors.New("all-drive state unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
	if err != nil {
		return st, errors.New("all-drive state unreadable")
	}
	st = allDriveState{}
	if len(data) > 8*1024*1024 || json.Unmarshal(data, &st) != nil || st.Schema != 1 || st.Scans == nil {
		return allDriveState{}, errors.New("invalid all-drive state")
	}
	return st, nil
}

func saveAllDriveJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > 8*1024*1024 {
		return errors.New("all-drive output exceeds checkpoint limit")
	}
	f, err := os.CreateTemp(filepath.Dir(path), "all-drive-stage-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Every directory and file is guarded before using metadata. Public-system
// scans remain separate legacy categories; they do not authorize another root.
func scanDrive(ctx context.Context, root string, st *driveScan, max int, guard func(context.Context, string) string) driveResult {
	visited := 0
	guardUnavailable := false
	// Keep a small invocation-local LRU so a wide parent is not re-read and
	// sorted once per child after switching to a depth-first persistent cursor.
	listings := map[string][]os.DirEntry{}
	listingOrder := []string{}
	list := func(path string) ([]os.DirEntry, error) {
		entries, ok := listings[path]
		if !ok {
			var err error
			entries, err = os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			listings[path] = entries
		}
		for i, key := range listingOrder {
			if key == path {
				listingOrder = append(listingOrder[:i], listingOrder[i+1:]...)
				break
			}
		}
		listingOrder = append(listingOrder, path)
		if len(listingOrder) > 16 {
			delete(listings, listingOrder[0])
			listingOrder = listingOrder[1:]
		}
		return entries, nil
	}
	for len(st.Pending) > 0 && visited < max && ctx.Err() == nil {
		i := len(st.Pending) - 1
		cur := st.Pending[i]
		if !within(root, cur.Path, true) || !safePath(cur.Path) {
			st.Errors++
			st.Pending = st.Pending[:i]
			continue
		}
		classification := guard(ctx, cur.Path)
		if classification == "guard_unavailable" {
			guardUnavailable = ctx.Err() == nil
			break
		}
		if classification != "clear" {
			st.Blocked++
			st.Pending = st.Pending[:i]
			continue
		}
		entries, err := list(cur.Path)
		if err != nil {
			st.Errors++
			st.Pending = st.Pending[:i]
			continue
		}
		st.Pending = st.Pending[:i]
		for _, entry := range entries {
			if entry.Name() <= cur.After {
				continue
			}
			if visited >= max || ctx.Err() != nil {
				st.Pending = append(st.Pending, cur)
				break
			}
			visited++
			after := cur.After
			cur.After = entry.Name()
			path := filepath.Join(cur.Path, entry.Name())
			classification = guard(ctx, path)
			if classification == "guard_unavailable" {
				// Revisit this same file after a transient guard failure.
				st.Pending = append(st.Pending, driveCursor{Path: cur.Path, After: after})
				guardUnavailable = ctx.Err() == nil
				break
			}
			if classification != "clear" {
				st.Blocked++
				continue
			}
			info, err := os.Lstat(path)
			if err != nil {
				st.Errors++
				continue
			}
			if isReparse(info) {
				st.Links++
				continue
			}
			if info.IsDir() {
				st.Directories++
				// Descend immediately instead of accumulating a whole wide
				// directory in the persistent pending stack.
				st.Pending = append(st.Pending, cur)
				st.Pending = append(st.Pending, driveCursor{Path: path})
				break
			}
			if info.Mode().IsRegular() {
				st.Files++
				if info.Size() > 0 {
					st.Bytes += uint64(info.Size())
				}
			}
		}
		if guardUnavailable {
			break
		}
	}
	r := driveResult{Status: "partial", Files: st.Files, Directories: st.Directories, Links: st.Links, Errors: st.Errors, Blocked: st.Blocked, Started: st.Started, Classification: "not_assessed"}
	if st.Files > 0 || st.Directories > 0 || st.Links > 0 {
		n := st.Bytes
		r.Bytes = &n
	}
	if len(st.Pending) == 0 {
		r.Status = "complete_metadata_scan"
		if st.Errors > 0 {
			r.Status = "partial_with_errors"
		}
		if st.Blocked > 0 {
			r.Status = "partial_protected"
			r.Classification = "protected_unclassified"
		}
		if st.Errors == 0 && st.Blocked == 0 && r.Bytes == nil {
			n := uint64(0)
			r.Bytes = &n
		}
	}
	if guardUnavailable {
		r.Status = "partial_guard_unavailable"
	}
	return r
}

func scanAtMount(st *driveScan, root string) *driveScan {
	if st == nil || len(st.Pending) == 0 || st.Root == "" {
		return &driveScan{Root: root, Pending: []driveCursor{{Path: root}}, Started: time.Now().UTC().Format(time.RFC3339)}
	}
	if st.Root != root {
		for i, cur := range st.Pending {
			if !within(st.Root, cur.Path, true) {
				return &driveScan{Root: root, Pending: []driveCursor{{Path: root}}, Started: time.Now().UTC().Format(time.RFC3339)}
			}
			rel, _ := filepath.Rel(st.Root, cur.Path)
			st.Pending[i].Path = filepath.Join(root, rel)
		}
		st.Root = root
	}
	return st
}

// Reserve the first turn for the rotating cursor, then prioritize growth.
// One growing drive must not starve every other drive on every invocation.
func driveOrder(next int, volumes []volume) []int {
	order := rootOrder(next, len(volumes))
	if len(order) > 1 {
		sort.SliceStable(order[1:], func(i, j int) bool {
			a, b := volumes[order[i+1]].FreeChange, volumes[order[j+1]].FreeChange
			return a != nil && *a < 0 && (b == nil || *b >= 0 || *a < *b)
		})
	}
	return order
}

func collectAllDrives(ctx context.Context, home string) (allDriveReport, error) {
	var out allDriveReport
	statePath := filepath.Join(home, "all-drive-state-v1.json")
	latestPath := filepath.Join(home, "all-drive-latest-v1.json")
	if !safeStateFile(statePath) || !safeStateFile(latestPath) {
		return out, errors.New("linked all-drive state")
	}
	st, err := readAllDriveState(statePath)
	if err != nil {
		return out, err
	}
	data, err := discoverDevices(ctx)
	if err != nil {
		return out, err
	}
	inv, err := normalizeInventory(data, st.Inventory, time.Now())
	if err != nil {
		return out, err
	}
	out.Inventory = inv
	out.Scans = []driveResult{}
	for _, index := range driveOrder(st.Next, inv.Volumes) {
		v := &out.Inventory.Volumes[index]
		if v.Drive == "" || v.Status != "available" {
			continue
		}
		if ctx.Err() != nil {
			v.Scan = "not_scanned_deadline"
			continue
		}
		root := v.Drive + `:\`
		scan := scanAtMount(st.Scans[v.ID], root)
		st.Scans[v.ID] = scan
		partCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		r := scanDrive(partCtx, root, scan, 20000, classifyPath)
		cancel()
		r.ID, r.Drive = v.ID, v.Drive
		v.Scan = r.Status
		out.Scans = append(out.Scans, r)
	}
	st.Next++
	st.Inventory = &out.Inventory
	if err = saveAllDriveJSON(statePath, st); err != nil {
		return out, err
	}
	if err = saveAllDriveJSON(latestPath, out); err != nil {
		return out, err
	}
	return out, nil
}
