package main

// Root-only qualification for the normal quarantine of newly created, ended
// Windows acceptance resources. No child, credential or link target is granted.
import (
	"bytes"
	contract "cc/civ6-ci-contract"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func retirement3250API(endpoint, query string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, currentGitHubCLI, "api", "--hostname", "github.com", endpoint, "--jq", query)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	cmd.Env = currentGitHubCLIEnvironment(os.Environ())
	b, err := cmd.Output()
	if len(b) > 64<<10 {
		return nil, os.ErrInvalid
	}
	return b, err
}

const retirement3250Temp = "C:/Users/makhmeto/AppData/Local/Temp"
const retirement3250Store = "D:/CC/Tools/safe-delete/receipts-go"
const retirement3250Floor = "263bb8d763b050da39c80f45560ee4349671524b"

var retirement3250ID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type retirement3250Identity struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Root    string `json:"root"`
	Path    string `json:"path"`
	Volume  uint32 `json:"volume"`
	FileID  uint64 `json:"file_id"`
	Created string `json:"created_at"`
	Project string `json:"project"`
	Content string `json:"content"`
}
type retirement3250Record struct {
	retirement3250Identity
	Integrity   string `json:"integrity"`
	State       string `json:"state"`
	Destination string `json:"destination,omitempty"`
}
type retirement3250Cleanup struct {
	State, Instance, Root string
	ID                    string `json:"safe_delete_id"`
}
type retirement3250Entry struct {
	Key, OS, Phase, State string
	Plan                  contract.Plan
	Cleanup               retirement3250Cleanup
	Queued                struct {
		At        string
		Deferrals int `json:"prestart_deferrals"`
	}
}
type retirement3250Ledger struct {
	Version int
	Budget  struct{ Initial, Normal, Continuation []string }
	Entries map[string]*retirement3250Entry
}

func retirement3250Binding(path string, r retirement3250Record, l retirement3250Ledger, now time.Time) (*retirement3250Entry, bool) {
	created, err := time.Parse(time.RFC3339Nano, r.Created)
	if err != nil || created.Before(time.Date(2026, 10, 21, 0, 25, 0, 0, time.UTC)) || now.Before(created) || now.Sub(created) > 24*time.Hour ||
		!retirement3250ID.MatchString(r.ID) || r.ID == owned3107ID || !same(path, filepath.Join(retirement3250Temp, "codex-owned-"+r.ID)) ||
		!same(r.Path, path) || !same(r.Root, retirement3250Temp) || r.Version != 2 || r.State != "owned" || r.Destination != "" || r.Project != "civ6-ci-queue" || r.Volume == 0 || r.FileID == 0 ||
		l.Version != 1 || len(l.Budget.Initial) > 3 || len(l.Budget.Normal) > 3 || len(l.Budget.Continuation) != 0 || len(l.Entries) > 6 {
		return nil, false
	}
	var found *retirement3250Entry
	seen := map[string]bool{}
	for _, key := range append(append([]string{}, l.Budget.Initial...), l.Budget.Normal...) {
		if seen[key] {
			return nil, false
		}
		seen[key] = true
		e := l.Entries[key]
		if e == nil || e.Key != key {
			return nil, false
		}
		if e.Cleanup.ID != r.ID {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = e
		p := e.Plan
		expected := fmt.Sprintf("%d/%d/%d", p.RunID, p.Attempt, p.JobID)
		queued, qe := time.Parse(time.RFC3339Nano, e.Queued.At)
		if p.Validate() != nil || p.OS() != "Windows" || key != expected || e.OS != "Windows" ||
			(e.State != "collecting" && e.State != "needs_recovery") || e.Queued.Deferrals < 0 || e.Queued.Deferrals >= 6 || qe != nil || queued.After(created) || created.Sub(queued) > 24*time.Hour ||
			e.Cleanup.Instance != r.ID || !same(e.Cleanup.Root, path) || r.Content != fmt.Sprintf("job-%d-%d-%d", p.RunID, p.Attempt, p.JobID) {
			return nil, false
		}
		initial := false
		for _, k := range l.Budget.Initial {
			initial = initial || k == key
		}
		if initial {
			if e.Phase != "initial" || p.Event != "pull_request" || p.PR != 149 || p.Head != owned3107Head || p.Tree != owned3107Tree || p.Branch != "scheduler/actions-cost-pr-concurrency-20261019" {
				return nil, false
			}
		} else {
			if e.Phase != "normal" || p.Event != "push" || p.PR != 0 || p.Branch != "main" {
				return nil, false
			}
		}
	}
	return found, found != nil
}

// Reads ONLY fixed controller-authored operational records, after validating
// the current source, independently recorded runtime and exact ancestry. The
// normally atomically replaced ledger/logs cannot use stale leaf timestamps;
// require their real owner, birth and physical single-link file identity now.
func retirement3250RuntimeRead(rel string, limit int64) ([]byte, bool) {
	if !civ6Runtime(filepath.Join(civ6Home, "civ6-ci-queue.exe")) {
		return nil, false
	}
	b, ok := conversationBytes(civ6Receipt, 4<<20, legacyMatch)
	var d civ6Deployment
	if !ok || json.Unmarshal(b, &d) != nil || !civ6Ancestor(retirement3250Floor, d.SourceHead) {
		return nil, false
	}
	for _, leaf := range []string{"civ6-ci-queue.exe", "civ6-ci-windows.exe"} {
		p := filepath.Join(civ6Home, leaf)
		if !civ6Runtime(p) {
			return nil, false
		}
		bytes, ok := conversationBytes(p, 64<<20, legacyMatch)
		if !ok || hashBytes(bytes) != d.Objects[leaf].SHA256 {
			return nil, false
		}
	}
	path := filepath.Join(civ6Home, filepath.FromSlash(rel))
	if !within(civ6Home, path) {
		return nil, false
	}
	for p := path; !same(p, civ6Home); p = filepath.Dir(p) {
		if legacyMatch(p) || !conversationPhysical(p) {
			return nil, false
		}
		m, e := resourceIdentity(p, true)
		if e != nil || m.Owner != civ6Owner || m.Created < d.Root.Created || m.Links != 1 {
			return nil, false
		}
	}
	return conversationBytes(path, limit, legacyMatch)
}

func retirement3250PathAllowed(path string) bool { return retirement3250Qualification(path) == "" }
func retirement3250Qualification(path string) string {
	id := strings.TrimPrefix(filepath.Base(path), "codex-owned-")
	if !retirement3250ID.MatchString(id) || id == owned3107ID || !same(path, filepath.Join(retirement3250Temp, "codex-owned-"+id)) || legacyMatch(path) || !conversationPhysical(path) {
		return "qualification-stage-1"
	}
	// Original producer record is external to the opaque resource. Never adopt,
	// edit, register or synthesize it. Decode the producer's exact identity bytes.
	recordPath := filepath.Join(retirement3250Store, id+".json")
	b, ok := conversationBytes(recordPath, 8192, legacyMatch)
	var r retirement3250Record
	_, validJSON := conversationJSON(b)
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	recordMeta, recordErr := resourceIdentity(recordPath, true)
	if !ok || !validJSON || recordErr != nil || recordMeta.Links != 1 || recordMeta.Owner != civ6Owner || decoder.Decode(&r) != nil {
		return "qualification-stage-2"
	}
	identity, _ := json.Marshal(r.retirement3250Identity)
	if hashBytes(identity) != r.Integrity {
		return "qualification-stage-3"
	}
	ledgerBytes, ok := retirement3250RuntimeRead("state/ledger.json", 4<<20)
	var l retirement3250Ledger
	_, validJSON = conversationJSON(ledgerBytes)
	if !ok || !validJSON || json.Unmarshal(ledgerBytes, &l) != nil {
		return "qualification-stage-4"
	}
	e, ok := retirement3250Binding(path, r, l, time.Now())
	if !ok {
		return "qualification-stage-5"
	}
	root, err := resourceIdentity(path, true)
	created, _ := time.Parse(time.RFC3339Nano, r.Created)
	parent, pe := resourceIdentity(retirement3250Temp, true)
	if err != nil || pe != nil || root.Owner != "S-1-5-32-544" || root.Volume != r.Volume || root.ID != r.FileID || root.Links != 1 || root.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 ||
		root.Created > civ6Filetime(created) || civ6Filetime(created)-root.Created > 10000000 || parent.Volume != r.Volume || parent.ID != 562949953422078 || parent.Created != 134072793159133344 {
		return "qualification-stage-6"
	}
	entryRel := "state/entries/" + strings.ReplaceAll(e.Key, "/", "-")
	if e.Queued.Deferrals > 0 {
		entryRel += fmt.Sprintf("/prestart-%d", e.Queued.Deferrals)
	}
	collected := false
	// Only sanitized collect-end events from the qualified queue's original log.
	for _, leaf := range []string{"collect.log", "collect-unstarted.log"} {
		log, readOK := retirement3250RuntimeRead(entryRel+"/"+leaf, 64<<10)
		if !readOK {
			continue
		}
		for _, line := range strings.Split(string(log), "\n") {
			v, valid := conversationJSON([]byte(line))
			if valid && v["event"] == "collect-end" && v["instance"] == id && conversationNumber(v["run_id"]) == uint64(e.Plan.RunID) && conversationNumber(v["attempt"]) == uint64(e.Plan.Attempt) && conversationNumber(v["job_id"]) == uint64(e.Plan.JobID) && v["complete"] == true && v["shared_profile_retained"] == true {
				collected = true
			}
		}
	}
	if !collected || retirement3250Settled(path) != nil || !retirement3250Official(e, id) {
		return "qualification-stage-7"
	}
	// All names are checked against the live private exclusions in memory. Walk
	// metadata only, with OPEN_REPARSE_POINT and no enumeration below a reparse.
	if stage := retirement3250InventoryCheck(path, r.Volume); stage != "" {
		return "inventory-" + stage
	}
	end, er := resourceIdentity(path, true)
	again, ok := conversationBytes(recordPath, 8192, legacyMatch)
	lb, lo := retirement3250RuntimeRead("state/ledger.json", 4<<20)
	finalRecord, fe := resourceIdentity(recordPath, true)
	if !(er == nil && end == root && fe == nil && finalRecord == recordMeta && ok && string(again) == string(b) && lo && string(lb) == string(ledgerBytes) && retirement3250Settled(path) == nil && retirement3250InventoryCheck(path, r.Volume) == "") {
		return "qualification-final-recheck"
	}
	return ""
}

func retirement3250InventoryCheck(root string, volume uint32) string {
	return retirement3250InventoryWith(root, volume, collected3174DenySnapshot(), func(p string) (audio2906Metadata, error) { return resourceIdentity(p, true) })
}
func retirement3250InventoryWith(root string, volume uint32, deny func(string) bool, identity func(string) (audio2906Metadata, error)) string {
	type group struct{ inside, total uint32 }
	links := map[uint64]group{}
	pending := []string{root}
	entries := 0
	for len(pending) > 0 {
		p := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entries++
		if entries > 10001 || deny(p) {
			return "metadata-or-bound"
		}
		m, e := identity(p)
		if e != nil {
			return "identity"
		}
		if m.Volume != volume {
			return "volume"
		}
		if m.Owner != "S-1-5-32-544" {
			return "owner"
		}
		if m.Links < 1 || m.Links > 10001 {
			return "hardlink-bound"
		}
		if m.Attributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 && m.Links != 1 {
			return "hardlinked-directory-or-reparse"
		}
		if m.Attributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) == 0 {
			g := links[m.ID]
			if g.total != 0 && g.total != m.Links {
				return "hardlink-changed"
			}
			g.inside++
			g.total = m.Links
			links[m.ID] = g
		}
		if m.Attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || m.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
			continue
		}
		dir, e := os.Open(p)
		if e != nil {
			return "metadata-or-bound"
		}
		for {
			children, re := dir.ReadDir(64)
			for _, c := range children {
				pending = append(pending, filepath.Join(p, c.Name()))
				if entries+len(pending) > 10001 {
					dir.Close()
					return "metadata-or-bound"
				}
			}
			if re != nil {
				dir.Close()
				if re != io.EOF {
					return "metadata-or-bound"
				}
				break
			}
		}
	}
	for _, g := range links {
		if g.inside != g.total {
			return "external-hardlink"
		}
	}
	if deny(root) {
		return "deny"
	}
	return ""
}

func retirement3250Official(e *retirement3250Entry, id string) bool {
	p := e.Plan
	if !currentGitHubIdentityVerified(currentProduct{repository: "autodevjapan/civ6-banseiki", owner: "AutoDevJapan", repositoryID: 1392152644}) {
		return false
	}
	run, ok := owned3107API(fmt.Sprintf("repos/AutoDevJapan/civ6-banseiki/actions/runs/%d/attempts/%d", p.RunID, p.Attempt))
	if !ok || conversationNumber(run["id"]) != uint64(p.RunID) || conversationNumber(run["run_attempt"]) != uint64(p.Attempt) || run["head_sha"] != p.Head || run["event"] != p.Event || run["head_branch"] != p.Branch || !contract.MatchRunPath(p, str(run["path"])) || conversationNumber(run["repository_id"]) != 1392152644 || conversationNumber(run["head_repository_id"]) != 1392152644 {
		return false
	}
	job, ok := owned3107API(fmt.Sprintf("repos/AutoDevJapan/civ6-banseiki/actions/jobs/%d", p.JobID))
	if !ok || conversationNumber(job["id"]) != uint64(p.JobID) || conversationNumber(job["run_id"]) != uint64(p.RunID) || conversationNumber(job["run_attempt"]) != uint64(p.Attempt) || job["head_sha"] != p.Head || job["name"] != p.JobName {
		return false
	}
	if e.Phase == "normal" {
		b, err := retirement3250API("repos/AutoDevJapan/civ6-banseiki/pulls/149", "{merged,merge_commit_sha}")
		var pr struct {
			Merged bool
			Head   string `json:"merge_commit_sha"`
		}
		if err != nil || json.Unmarshal(b, &pr) != nil || !pr.Merged || pr.Head != p.Head {
			return false
		}
	}
	b, err := retirement3250API("repos/AutoDevJapan/civ6-banseiki/actions/runners?per_page=100", "{total_count,runners:[.runners[]|{name}]}")
	var runners struct {
		Total   int `json:"total_count"`
		Runners []struct{ Name string }
	}
	if err != nil || json.Unmarshal(b, &runners) != nil || runners.Total != len(runners.Runners) || runners.Total > 100 {
		return false
	}
	for _, r := range runners.Runners {
		if r.Name == "CC-CIW-"+id {
			return false
		}
	}
	return true
}
func retirement3250Settled(root string) error {
	// Read only a public collection marker, never any credential/profile value.
	var key syscall.Handle
	p, _ := syscall.UTF16PtrFromString(`SOFTWARE\CC\civ6-ci-windows\` + "CC-CIW-civ6-banseiki")
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, p, 0, 0x20019, &key) != nil {
		return errors.New("CI_COLLECTION_UNVERIFIED")
	}
	defer syscall.RegCloseKey(key)
	name, _ := syscall.UTF16PtrFromString("CollectedRoot")
	var buf [2048]uint16
	var typ uint32
	n := uint32(len(buf) * 2)
	if syscall.RegQueryValueEx(key, name, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil || typ != 1 || n < 4 || n > uint32(len(buf)*2) || n%2 != 0 || buf[n/2-1] != 0 {
		return errors.New("CI_COLLECTION_UNVERIFIED")
	}
	for _, v := range buf[:n/2-1] {
		if v == 0 {
			return errors.New("CI_COLLECTION_UNVERIFIED")
		}
	}
	if !same(syscall.UTF16ToString(buf[:n/2]), root) {
		return errors.New("CI_COLLECTION_UNVERIFIED")
	}
	a := syscall.NewLazyDLL("advapi32.dll")
	manager, _, _ := a.NewProc("OpenSCManagerW").Call(0, 0, 1)
	if manager == 0 {
		return errors.New("CI_SERVICE_UNVERIFIED")
	}
	defer a.NewProc("CloseServiceHandle").Call(manager)
	svcName, _ := syscall.UTF16PtrFromString("CC-CIW-civ6-banseiki")
	svc, _, _ := a.NewProc("OpenServiceW").Call(manager, uintptr(unsafe.Pointer(svcName)), 5)
	if svc == 0 {
		return errors.New("CI_SERVICE_UNVERIFIED")
	}
	defer a.NewProc("CloseServiceHandle").Call(svc)
	var status [9]uint32
	var needed uint32
	ok, _, _ := a.NewProc("QueryServiceStatusEx").Call(svc, 0, uintptr(unsafe.Pointer(&status[0])), unsafe.Sizeof(status), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 || status[1] != 1 || status[7] != 0 {
		return errors.New("CI_SERVICE_NOT_STOPPED")
	}
	// The exact service image must still belong to this collected root.
	var cfg [8192]byte
	ok, _, _ = a.NewProc("QueryServiceConfigW").Call(svc, uintptr(unsafe.Pointer(&cfg[0])), uintptr(len(cfg)), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return errors.New("CI_SERVICE_UNVERIFIED")
	}
	type serviceConfig struct {
		ServiceType, StartType, ErrorControl uint32
		Binary                               *uint16
	}
	binary := (*serviceConfig)(unsafe.Pointer(&cfg[0])).Binary
	if binary == nil {
		return errors.New("CI_SERVICE_UNVERIFIED")
	}
	start, end, addr := uintptr(unsafe.Pointer(&cfg[0])), uintptr(unsafe.Pointer(&cfg[0]))+uintptr(len(cfg)), uintptr(unsafe.Pointer(binary))
	if addr < start || addr >= end || addr%2 != 0 {
		return errors.New("CI_SERVICE_UNVERIFIED")
	}
	chars := unsafe.Slice(binary, int((end-addr)/2))
	terminated := false
	for _, c := range chars {
		if c == 0 {
			terminated = true
			break
		}
	}
	if !terminated {
		return errors.New("CI_SERVICE_UNVERIFIED")
	}
	image := syscall.UTF16ToString(chars)
	if image != syscall.EscapeArg(root+`\helper.exe`)+" service "+syscall.EscapeArg(root) {
		return errors.New("CI_SERVICE_CHANGED")
	}
	return nil
}
