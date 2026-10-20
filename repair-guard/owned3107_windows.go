package main

// One already created PR149 resource. This is a reviewed finite provenance
// check, not a caller-supplied grant or reconstruction of the private registry.
import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const owned3107ID = "4378ff571926b0ddbec4ed99368a6405"
const owned3107Root = "C:/Users/makhmeto/AppData/Local/Temp/codex-owned-" + owned3107ID
const owned3107SourceFloor = "37d5359af2dcbbd047dea1f25dad75aa3d0a8f3a"
const owned3107Head = "2bd3a366f8974ba54df83927102aa5bdad3983e5"
const owned3107Tree = "a4e37a064e0f2ea316d0f15d9aaf710abf9b65f6"
const owned3107Context = "1a54d8fd8803551f23d68786fe499a43cb19bf4b"
const owned3107Key = "39423559387/1/118630300541"
const owned3107QueueSHA = "21a90da377ed8a5d18099dae84ff371b35ed015db7263bd91de443c3a8db0e32"
const owned3107HelperSHA = "f46d3147702b5cba0beb1bf5d6390577408bf67468114ecf63256475edf88702"
const owned3107WorkflowSHA = "d73511d79813e3b3b994dc95b3915d2cd21d3d2426fc27eb76d5820769d6c67a"
const owned3107Evidence = "D:/CC/Work/unattended-operations-20261013-wt/todo-3107-ci/codex-owned-9cb79093b11a8c0baf702c37733deac6"

type owned3107Proof struct {
	path, sha string
	limit     int64
}

// Copies preserve the original bytes independently of later normal queue and
// deployment updates. Neither this guard nor a runtime writer updates these pins.
var owned3107Proofs = []owned3107Proof{
	{"D:/CC/Tools/safe-delete/receipts-go/" + owned3107ID + ".json", "659c0ff190429951230e86cf7ad3572a50b3e89fec65d6ea02c9b96ed2750f65", 8192},
	{owned3107Evidence + "/stage.private.json", "f0ea610c139ba16f0331271ea0f0c6f6362f1a5547ca07dcf97aca2b040024db", 64 << 10},
	{owned3107Evidence + "/result.private.json", "50338b7db2782c07dacf259e11856e9d29a5bc92c8f1426b85e385dd843ea31f", 64 << 10},
	{owned3107Evidence + "/ledger-original.private.json", "e78bc667bdf17c01798bb1b2f9c5f6f6bc50a869e9f39178e26d5746ee061732", 4 << 20},
	{owned3107Evidence + "/inventory3107.private.json", "790a5b4436c0345bcee3da3fbf83f73911e7395d5ceaf269cf6b0bc20dfdce5c", 4 << 20},
	{owned3107Evidence + "/deployment-observed.private.json", "dd2d13dc2c3aeaaaa7e19716314ff7ed6d26acbf264cd3d442e4f8bc33198f00", 4 << 20},
}

type owned3107Access struct {
	root                string
	expected, parent    civ6Object
	proofs              []owned3107Proof
	at                  time.Time
	now                 func() time.Time
	protected, plain    func(string) bool
	identity            func(string) (audio2906Metadata, error)
	read                func(string, int64) ([]byte, bool)
	qualified, official func() bool
	runtime             func(civ6Deployment) bool
}

func owned3107PathAllowed(path string) bool {
	return owned3107Allowed(path, owned3107Access{
		root: owned3107Root, at: time.Now(), now: time.Now, proofs: owned3107Proofs, protected: legacyMatch, plain: conversationPhysical,
		expected:  civ6Object{Volume: 2229119083, ID: 45317471266384470, Created: 134369735429421378, Owner: civ6Owner},
		parent:    civ6Object{Volume: 2229119083, ID: 562949953422078, Created: 134072793159133344, Owner: civ6Owner},
		identity:  func(p string) (audio2906Metadata, error) { return resourceIdentity(p, true) },
		read:      func(p string, n int64) ([]byte, bool) { return conversationBytes(p, n, legacyMatch) },
		qualified: func() bool { head, ok := civ6SourceHead(); return ok && civ6Ancestor(owned3107SourceFloor, head) },
		runtime:   owned3107RuntimeMatches, official: owned3107Official,
	})
}

func owned3107RuntimeMatches(r civ6Deployment) bool {
	if !civ6Runtime(filepath.Join(civ6Home, "civ6-ci-queue.exe")) {
		return false
	}
	for _, v := range []struct {
		path      string
		record    civ6Object
		directory bool
	}{
		{civ6Home, r.Root, true}, {filepath.Dir(civ6Home), r.Parent, true},
		{filepath.Join(civ6Home, "civ6-ci-queue.exe"), r.Objects["civ6-ci-queue.exe"], false},
	} {
		m, e := resourceIdentity(v.path, true)
		if e != nil || !civ6Matches(v.record, m, v.directory) {
			return false
		}
	}
	return true
}

func owned3107NoDeny(path string, a owned3107Access) bool {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		if a.protected(p) {
			return false
		}
		if filepath.Dir(p) == p {
			return true
		}
	}
}

func owned3107ReadProofs(a owned3107Access) ([]object, bool) {
	if len(a.proofs) != 6 {
		return nil, false
	}
	out := make([]object, 0, 6)
	for _, p := range a.proofs {
		if within(a.root, p.path) || !owned3107NoDeny(p.path, a) || !a.plain(p.path) {
			return nil, false
		}
		before, e := a.identity(p.path)
		if e != nil || before.Links != 1 || before.Attributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
			return nil, false
		}
		b, ok := a.read(p.path, p.limit)
		if !ok || hashBytes(b) != p.sha {
			return nil, false
		}
		parsed, ok := conversationJSON(b)
		if !ok {
			return nil, false
		}
		after, e := a.identity(p.path)
		if e != nil || before != after || !owned3107NoDeny(p.path, a) || !a.plain(p.path) {
			return nil, false
		}
		out = append(out, parsed)
	}
	return out, true
}

func owned3107PlanMatches(p object) bool {
	return conversationNumber(p["version"]) == 1 && conversationNumber(p["repo_id"]) == 1392152644 &&
		p["head"] == owned3107Head && p["tree"] == owned3107Tree && p["event"] == "pull_request" &&
		p["branch"] == "scheduler/actions-cost-pr-concurrency-20261019" && conversationNumber(p["pr"]) == 149 &&
		p["context_sha"] == owned3107Context && p["workflow_sha"] == owned3107Context &&
		p["workflow_ref"] == "AutoDevJapan/civ6-banseiki/.github/workflows/rust.yml@refs/pull/149/merge" &&
		p["workflow"] == ".github/workflows/rust.yml" && p["workflow_digest"] == owned3107WorkflowSHA &&
		conversationNumber(p["run_id"]) == 39423559387 && conversationNumber(p["attempt"]) == 1 &&
		conversationNumber(p["job_id"]) == 118630300541 && p["job"] == "test" && p["job_name"] == "test (windows-latest)"
}

func owned3107Binding(a owned3107Access, p []object) (object, civ6Deployment, bool) {
	var deployment civ6Deployment
	c, s, pub, ledger, inventory := p[0], p[1], p[2], p[3], p[4]
	created, e := time.Parse(time.RFC3339Nano, str(c["created_at"]))
	// The producer records time.Now AFTER mkdir. Keep NTFS birth and receipt
	// time separate; require the original recorded ID and a finite time relation.
	if e != nil || conversationNumber(c["version"]) != 2 || c["id"] != owned3107ID ||
		!same(str(c["path"]), a.root) || !same(str(c["root"]), filepath.Dir(a.root)) ||
		conversationNumber(c["volume"]) != uint64(a.expected.Volume) || conversationNumber(c["file_id"]) != a.expected.ID ||
		civ6Filetime(created) < a.expected.Created || civ6Filetime(created) > a.expected.Created+10000000 ||
		c["project"] != "civ6-ci-queue" || c["content"] != "job-39423559387-1-118630300541" || c["state"] != "owned" ||
		s["source_head"] != owned3107SourceFloor || pub["Head"] != owned3107Head || pub["Tree"] != owned3107Tree || pub["BranchVerified"] != true ||
		conversationNumber(ledger["version"]) != 1 || inventory["data_read"] != false || inventory["attribute_walk_complete"] != true {
		return nil, deployment, false
	}
	pins := map[string]string{}
	files, _ := s["files"].([]any)
	for _, f := range files {
		v := obj(f)
		pins[str(v["name"])] = str(v["sha256"])
	}
	if pins["civ6-ci-queue.exe"] != owned3107QueueSHA || pins["civ6-ci-windows.exe"] != owned3107HelperSHA {
		return nil, deployment, false
	}
	entry := obj(obj(ledger["entries"])[owned3107Key])
	cleanup := obj(entry["cleanup"])
	if entry["key"] != owned3107Key || entry["os"] != "Windows" || entry["phase"] != "initial" || !owned3107PlanMatches(obj(entry["plan"])) ||
		!same(str(cleanup["root"]), a.root) || cleanup["instance"] != owned3107ID || cleanup["safe_delete_id"] != owned3107ID {
		return nil, deployment, false
	}
	b, e := json.Marshal(p[5])
	if e != nil || json.Unmarshal(b, &deployment) != nil || deployment.Domain != "civ6-ci-2962-deployment-v1" ||
		deployment.ContractSHA256 != hashBytes(civ6Contract) || deployment.SourceHead != owned3107SourceFloor ||
		deployment.Objects["civ6-ci-queue.exe"].SHA256 != owned3107QueueSHA || deployment.Objects["civ6-ci-windows.exe"].SHA256 != owned3107HelperSHA {
		return nil, deployment, false
	}
	return obj(inventory["objects"]), deployment, true
}

func owned3107TimeAllowed(at time.Time) bool {
	return !at.Before(time.Date(2026, 10, 20, 12, 39, 2, 0, time.UTC)) && at.Before(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
}

func owned3107Allowed(path string, a owned3107Access) bool {
	if !filepath.IsAbs(path) || !within(a.root, path) || !owned3107TimeAllowed(a.at) {
		return false
	}
	// No metadata/read/API work for absent, unknown, or nonphysical paths.
	if !owned3107NoDeny(path, a) || !a.plain(path) {
		return false
	}
	root, e := a.identity(a.root)
	parent, pe := a.identity(filepath.Dir(a.root))
	if e != nil || pe != nil || root.Links != 1 || !civ6Matches(a.expected, root, true) || !civ6Matches(a.parent, parent, true) {
		return false
	}
	proofs, ok := owned3107ReadProofs(a)
	if !ok {
		return false
	}
	objects, deployment, ok := owned3107Binding(a, proofs)
	if !ok {
		return false
	}
	// Match each requested object and its exact observed ancestry. No subtree
	// enumeration, contents, new names, reparse traversal or borrowed hardlinks.
	physicalCheck := func() bool {
		m, e := a.identity(filepath.Dir(a.root))
		if e != nil || !civ6Matches(a.parent, m, true) {
			return false
		}
		for p := filepath.Clean(path); ; p = filepath.Dir(p) {
			rel, e := filepath.Rel(a.root, p)
			if e != nil {
				return false
			}
			recorded, exists := objects[filepath.ToSlash(rel)]
			if !exists {
				return false
			}
			b, e := json.Marshal(recorded)
			var w audio2906Metadata
			if e != nil || json.Unmarshal(b, &w) != nil || w.Attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || w.Created < a.expected.Created {
				return false
			}
			actual, e := a.identity(p)
			directory := w.Attributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
			r := civ6Object{Volume: w.Volume, ID: w.ID, Created: w.Created, Owner: w.Owner, Size: w.Size, Written: w.Written, Changed: w.Changed}
			if e != nil || !civ6Matches(r, actual, directory) || actual.Links != 1 || !a.plain(p) || !owned3107NoDeny(p, a) {
				return false
			}
			if same(p, a.root) {
				return civ6Matches(a.expected, actual, true)
			}
			if filepath.Dir(p) == p {
				return false
			}
		}
	}
	if !physicalCheck() || !a.qualified() || !a.runtime(deployment) || !a.official() {
		return false
	}
	// Pins and attributes can change during external checks. Revalidate locally
	// before returning a read-only classification; callers retain collector locks.
	_, ok = owned3107ReadProofs(a)
	at := a.at
	if a.now != nil {
		at = a.now()
	}
	return ok && physicalCheck() && owned3107TimeAllowed(at)
}

func owned3107API(endpoint string) (object, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, currentGitHubCLI, "api", "--hostname", "github.com", endpoint, "--jq",
		"{id,run_id,run_attempt,head_sha,path,event,head_branch,name,labels,repository_id:.repository.id,head_repository_id:.head_repository.id}")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	cmd.Env = currentGitHubCLIEnvironment(os.Environ())
	b, e := cmd.Output()
	if e != nil || len(b) > 64<<10 {
		return nil, false
	}
	return conversationJSON(b)
}

func owned3107Official() bool {
	if !currentGitHubIdentityVerified(currentProduct{repository: "autodevjapan/civ6-banseiki", owner: "AutoDevJapan", repositoryID: 1392152644}) {
		return false
	}
	run, ok := owned3107API("repos/AutoDevJapan/civ6-banseiki/actions/runs/39423559387/attempts/1")
	if !ok || !owned3107OfficialRunMatches(run) {
		return false
	}
	job, ok := owned3107API("repos/AutoDevJapan/civ6-banseiki/actions/jobs/118630300541")
	return ok && owned3107OfficialJobMatches(job)
}

func owned3107OfficialRunMatches(run object) bool {
	path := str(run["path"])
	// PR149 provenance is pinned in the original ledger/creation evidence.
	// A current pull_requests association is not historical ownership evidence.
	return conversationNumber(run["id"]) == 39423559387 && conversationNumber(run["run_attempt"]) == 1 && run["head_sha"] == owned3107Head &&
		(path == ".github/workflows/rust.yml" || path == ".github/workflows/rust.yml@refs/pull/149/merge") &&
		run["head_branch"] == "scheduler/actions-cost-pr-concurrency-20261019" &&
		run["event"] == "pull_request" && conversationNumber(run["repository_id"]) == 1392152644 && conversationNumber(run["head_repository_id"]) == 1392152644
}

func owned3107OfficialJobMatches(job object) bool {
	if conversationNumber(job["id"]) != 118630300541 || conversationNumber(job["run_id"]) != 39423559387 || conversationNumber(job["run_attempt"]) != 1 ||
		job["head_sha"] != owned3107Head || job["name"] != "test (windows-latest)" {
		return false
	}
	labels, _ := job["labels"].([]any)
	seen := map[string]bool{}
	for _, v := range labels {
		seen[strings.ToLower(str(v))] = true
	}
	return len(labels) == 5 && seen["self-hosted"] && seen["windows"] && seen["x64"] && seen["civ6-ci-windows"] && seen["civ6-ci-run-39423559387-1-test-windows"]
}
