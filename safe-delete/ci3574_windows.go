//go:build windows

package main

// Preserve three original, collected Console CI roots using the existing
// quarantine implementation. The ordinary deletion planner stays unchanged.
import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func ci3574OAuthEnvironment(environ []string) []string {
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST":
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func ci3574GitHub(path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, `C:\Program Files\GitHub CLI\gh.exe`, "api", "--hostname", "github.com", "repos/AutoDevJapan/local-ai-console/"+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Env = ci3574OAuthEnvironment(os.Environ())
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

const ci3574Evidence = "C:/Users/makhmeto/Documents/Codex/2026-10-24/http-localhost-10012-view-kanban-layout/work/ci-3574"
const ci3574Git = "runner/_work/local-ai-console/local-ai-console/.git"

type ci3574Resource struct {
	id, receiptSHA, proof, proofSHA, head, tree string
	run, job                                    int64
}

var ci3574Resources = []ci3574Resource{
	{"af44d1559483132d209b05b02fbc1878", "d83f8aac4d77d321e51dbe4a7b86778e8dbfaa2dae6d10a1c15a8c5c0627a641", "pr13-local-proof.json", "76dadf1ada565300f3ce24f54366a9276b43cc80ceaaaaa9f992178c0b3a87a6", "a055bcffe7665b92a048f685d4f737ef7dd6b8f7", "7b34abdf1d74937e00f8dfae793388b09da9f11e", 40055430227, 120757260110},
	{"8e934105e613620abf90e7cecd787162", "417c09febc030d9e548fdef5fb087f3f917b17bc523af615d5810d84723e7d47", "pr12-local-proof.json", "00bb5ee79b85a6852095c51a045866a88969a915298427e48e0f737f15679cc2", "3ec65bd1ad6975332cd3369ba6a2b3b219b0daf9", "d245045b8ea38b43cb9f923b239909d9c18e65bc", 40031871062, 120837241158},
	{"38c2d816dc21ce06fe3f42c8dccfae71", "b907662c8c210ee927805ee99ff0127e9f61d470d745966afc1806a30d0813ac", "pr15-final-local-proof.json", "18cc7a6f0de6a8fd81ae1dd61c4307fa41d53ef4d56d7c9bd768ca2250a1baab", "10a6c1385a26147285a4ad665e815a31a8280c6b", "5140e9c19f68a5e799f2a6c7657f165594d01f29", 40083106520, 120840518461},
}

func ci3574ResourceByID(id string) (ci3574Resource, bool) {
	for _, p := range ci3574Resources {
		if p.id == id {
			return p, true
		}
	}
	return ci3574Resource{}, false
}

type ci3574Run struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Head       string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Name       string `json:"name"`
	Repository struct {
		ID int64 `json:"id"`
	} `json:"repository"`
}

func ci3574OfficialValid(p ci3574Resource, run, job ci3574Run, runnerIDs []int64) bool {
	if run.ID != p.run || run.Head != p.head || run.Status != "completed" || run.Conclusion != "success" || run.Repository.ID != 1429803672 ||
		job.ID != p.job || job.RunID != p.run || job.Head != p.head || job.Status != "completed" || job.Conclusion != "success" || job.Name != "acceptance" {
		return false
	}
	for _, id := range runnerIDs {
		if id == 40 || id == 41 || id == 42 || id == 43 {
			return false
		}
	}
	return true
}

func ci3574Official(p ci3574Resource) error {
	var run, job ci3574Run
	var runners struct {
		Total *int `json:"total_count"`
		Rows  []struct {
			ID int64 `json:"id"`
		} `json:"runners"`
	}
	for _, query := range []struct {
		path   string
		target any
	}{
		{"actions/runs/" + strconv.FormatInt(p.run, 10), &run},
		{"actions/jobs/" + strconv.FormatInt(p.job, 10), &job},
		{"actions/runners?per_page=100", &runners},
	} {
		b, e := ci3574GitHub(query.path)
		if e != nil || len(b) > 1<<20 || json.Unmarshal(b, query.target) != nil {
			return fail("CONSOLE_CI_SOURCE_UNAVAILABLE")
		}
	}
	if runners.Total == nil || *runners.Total != len(runners.Rows) || len(runners.Rows) > 100 {
		return fail("CONSOLE_CI_RUNNERS_UNVERIFIED")
	}
	ids := []int64{}
	for _, r := range runners.Rows {
		ids = append(ids, r.ID)
	}
	if !ci3574OfficialValid(p, run, job, ids) {
		return fail("CONSOLE_CI_NOT_COLLECTED")
	}
	return nil
}

func (c config) ci3574Recovery(id string) (*ci3174Scope, error) {
	p, ok := ci3574ResourceByID(id)
	if !ok || !same(c.Store, "D:/CC/Tools/safe-delete/receipts-go") || !same(c.Quarantine, `C:\Users\makhmeto\Downloads\削除希望`) {
		return nil, fail("CONSOLE_CI_SCOPE_MISMATCH")
	}
	r, e := c.load(id, true)
	if e != nil {
		return nil, e
	}
	// The normal guard positively verifies current native source, original
	// creation/end receipts and this exact root's physical identity first.
	if e = ci3250Guard(r.Path); e != nil {
		return nil, e
	}
	originalPath, _ := c.recordPath(id)
	original, e := console3451Pinned(originalPath, p.receiptSHA, 8192)
	if e != nil {
		return nil, e
	}
	if r.State != "owned" || r.Project != "local-ai-console-3574" || r.Volume != 2229119083 || !same(r.Root, `C:\Users\makhmeto\AppData\Local\Temp`) || !same(r.Path, filepath.Join(r.Root, "codex-owned-"+id)) {
		return nil, fail("CONSOLE_CI_SCOPE_MISMATCH")
	}
	current, e := ci3174Identity(r.Path)
	if e != nil {
		return nil, e
	}
	token, e := syscall.OpenCurrentProcessToken()
	if e != nil {
		return nil, fail("CONSOLE_OWNER_UNVERIFIED")
	}
	user, e := token.GetTokenUser()
	token.Close()
	if e != nil {
		return nil, fail("CONSOLE_OWNER_UNVERIFIED")
	}
	owner, e := user.User.Sid.String()
	if e != nil || current.Owner != owner {
		return nil, fail("CONSOLE_OWNER_UNVERIFIED")
	}
	s := &ci3174Scope{maxEntries: 100000, maxBytes: 16 << 30, identity: ci3174Identity, volume: r.Volume, rootID: r.FileID, rootBirth: ci3174Birth(current), captureCollected: true, internalLinks: true, preservedGit: ci3574Git, createdOwner: owner}
	before, _, e := s.snapshot(r.Path)
	if e != nil {
		return nil, e
	}
	names := map[string]bool{}
	for name, m := range before {
		if m.Attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return nil, fail("LINK_OR_REPARSE_POINT")
		}
		leaf := strings.ToLower(filepath.Base(name))
		if leaf == ".runner" || strings.HasPrefix(leaf, ".credentials") {
			return nil, fail("CONSOLE_RUNNER_NOT_COLLECTED")
		}
		if strings.HasSuffix(leaf, ".exe") {
			names[leaf] = true
		}
	}
	s.captureCollected, s.baseline = false, before
	s.opaque = map[string]ci3174Entry{}
	s.originalReceipt = func() ([]byte, error) {
		b, e := console3451Pinned(originalPath, p.receiptSHA, 8192)
		if e != nil || string(b) != string(original) {
			return nil, fail("CONSOLE_RECEIPT_CHANGED")
		}
		return b, nil
	}
	s.settled = func() error {
		if _, e := s.originalReceipt(); e != nil {
			return e
		}
		if e := ci3250Guard(r.Path); e != nil {
			return e
		}
		if _, e := console3451Pinned(filepath.Join(ci3574Evidence, p.proof), p.proofSHA, 64<<10); e != nil {
			return e
		}
		if e := ci3574Official(p); e != nil {
			return e
		}
		proof := console3451Acceptance{Head: p.head, Tree: p.tree}
		// Reuse the ordinary saved-Git proof, with the fixed producer's path.
		if e := consoleCIGitSaved(r.Path, ci3574Git, proof); e != nil {
			return e
		}
		if e := console3451Idle(r.Path, names); e != nil {
			return e
		}
		first, _, e := s.snapshot(r.Path)
		if e != nil {
			return e
		}
		time.Sleep(300 * time.Millisecond)
		second, _, e := s.snapshot(r.Path)
		if e != nil || !reflect.DeepEqual(first, second) {
			return fail("CONSOLE_RESOURCE_CHANGING")
		}
		return nil
	}
	if e = s.settled(); e != nil {
		return nil, e
	}
	return s, nil
}
