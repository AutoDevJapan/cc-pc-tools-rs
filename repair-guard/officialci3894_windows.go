package main

// This constructor has no path/hash/owner input and never changes classification.
// It binds one newly issued public-software receipt to the original CI purpose.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	oc "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/internal/officialci3893"
)

const official3894Gate = "D:/CC/Tools/codex-jobs/gh_gate.exe"
const official3894Store = "D:/CC/Tools/client-records/official-ci3387.creation-v1"
const official3894Domain = "official-ci3387-ordinary-implementation-3894-v1"
const official3894Safe = "D:/CC/Tools/safe-delete/safe-delete.exe"
const official3894Temp = "C:/Users/makhmeto/AppData/Local/Temp"
const official3894OriginalCreator = "20261026-064014-todo-3894"

// Independent accepted runtime snapshots, not hashes learned from an unknown
// target. The original SDK producer and its historical receipt stay unchanged.
// docs/evidence/official-ci3387-known-guard-baseline-f30f9e76.json
const official3894PreviousGuardSHA = "02c8cad70e44f2bc4cd4e5a70dfdaf581a7ee6896eb63449fe1606eb28f5b5be"
const official3894PreviousGuardHead = "f30f9e763e25884fa48d1cce0cac22da1cebfb69"

// The reviewed PR567 build and its normal deployment are recorded on PR567.
// This is the known software created by that repair, not an observed unknown
// hash. The original SDK producer, receipt and registration remain unchanged.
const official3894RepairGuardSHA = "434832534d43cbe954aa1c2e573879556bb840818e830a489b744e6e2744740f"
const official3894RepairGuardHead = "521c4c4dddd7abbb0135d13bffb380ec984374b8"

// The normally merged and deployed PR582 diagnostic software, independently
// projected in docs/evidence/official-ci3387-known-guard-baseline-7cea2e8f.json.
const official3894DiagnosticGuardSHA = "c0439d9ccab874ac68e8f054a895cb7f5e50165d3adc5b7ae3e4fe8f92ce3081"
const official3894DiagnosticGuardHead = "7cea2e8f12631aeb5584dab5bc8535b7cd984f8c"

// The existing publisher's normal PR585 distribution superseded PR582 before
// this update. Its source/review and normal delivery provenance are retained in
// docs/evidence/official-ci3387-known-guard-baseline-81700412.json.
const official3894ConcurrentGuardSHA = "94c08be91776df277a6a3442e71f291103345641558657e1872e36520ad924e0"
const official3894ConcurrentGuardHead = "817004120a458b3a37830478512da0aa8040c9d0"

// The shared PR586 updater's normal distribution was independently recorded
// before this successor. Preserve the same software fact in both consumers;
// original SDK producer/receipt, qualification and data access remain separate.
// docs/evidence/skills-3080-guard-before-scope589-ad5.json
const official3894SharedGuardSHA = "ad5d37c7b216264d2e99007a2e6f3468e7529803454d88f1954cc74a7beb4783"
const official3894SharedGuardHead = "0d97c1952ff6f8b47a3d40939941639b35309408"

// PR589's accepted distribution was independently read back before the
// masked-source successor. This is the same software fact as the Skills table;
// it changes no SDK receipt, producer, expiry or data-access qualification.
// docs/evidence/skills-3080-guard-before-masked-source-9f914.json
const official3894MaskedPredecessorSHA = "9f914c724231886438515c5ed3831a13eca3d8fbec09e4d3d6edb1d0113c6dd8"
const official3894MaskedPredecessorHead = "4e117a2d813e4a989d1c3085d2734963d9c198a3"

// The existing publisher completed PR596 before this successor. Both existing
// updater comparisons retain the same independently published software pair.
// docs/evidence/official-ci3387-known-guard-baseline-444eae87.json
const official3894MaskedDistributionSHA = "c80d60ab470e52ef9fbe53c1e039d1fcdc31429e291c3f156a953b9b6059d02f"
const official3894MaskedDistributionHead = "444eae8714ee8b6199c9ed95208ac40272aeb8ce"

// PR594's accepted normal distribution was independently read back before the
// native-log successor. Preserve only this software pair in both updaters.
// docs/evidence/skills-3080-guard-before-native609-16386.json
const official3894NativePredecessorSHA = "16386d55e418ce271cfed923edede1e7472bb93aa691b15409bcdb99753b9899"
const official3894NativePredecessorHead = "61de98373f8cb481302394be11d7d75cfef08a52"

// PR610's independently observed accepted software, retained by both existing
// updaters before generating its notice-representation successor. No data grant.
// docs/evidence/skills-3080-guard-before-notice-representation175645.json
const official3894NoticePredecessorSHA = "9be9a066053183a099e78133571d394ecbb2845372e3cf603e292c74dac1a8d3"
const official3894NoticePredecessorHead = "deb430b8b089fde2391dbd243c01db48de48d493"

// Independent completed PR615 distribution, observed before its successor.
const official3894AliasPredecessorSHA = "50c817a829ecb33c30e4ee2b129dee3716f822fc4627dd01df6f24cb98a7925b"
const official3894AliasPredecessorHead = "34cd8877587596539e8b4a8847468fc74fca8e57"

// The same independent PR618 software observation used by the Skills updater.
const official3894CanonicalAssignmentPredecessorSHA = "f8b2ba6e66e60b06bbbc46c2c0f3d6eedc0208d40444c30e37b70afe29db4fc7"
const official3894CanonicalAssignmentPredecessorHead = "a71330f543969b4585f01b7359a84a2fa528c9f2"

// Independent completed PR624 distribution, observed before the path repair.
const official3894CreationPathPredecessorSHA = "eab69e4fc8e52f2ad6858ef4e8498e8cc6030ac8ce7f788251a1aae476dc5d55"
const official3894CreationPathPredecessorHead = "034520d84d52f76dcd7dc7ef371910d6acd8ae9f"

// Prior normal PR627 deployment, independently observed before this candidate.
// docs/evidence/skills-3080-guard-before-runtime-bootstrap.json
const official3894BootstrapPredecessorSHA = "7fb45b8a411ca2e1dd64a034b94c51ce5ca51324ad7c0c4b778eecb9f4538d36"
const official3894BootstrapPredecessorHead = "b78007695a7c5d73eb1f11118757e93918aa3234"

// Normal PR657 deployment and independent same-buffer readback, observed
// before the public-manifest/consumer successor is generated.
const official3894PublicManifestPredecessorSHA = "c7bd633b2eaeb8f55ba33775817398508b79fad0dcef308e216e0f3c40881ad0"
const official3894PublicManifestPredecessorHead = "4ebfedab3794ff12e0f0be09243be3fd3544107a"
const official3894RuntimeOriginPredecessorSHA = "f3166064c8670868077f1b201959cf5059169b3be9ccc6b73465a0e238650c09"
const official3894RuntimeOriginPredecessorHead = "ae8cf2424ebbc19de2e6eb2561c1bd48f37f9f7e"

// Independently observed normal PR662 distribution, before its source-start
// diagnostic successor is built. This is a software identity, not a data grant.
// docs/evidence/skills-3080-guard-before-source-dispatch.json
const official3894SourceDispatchPredecessorSHA = "910eb3b0f97c527861dfa6db577770bdc207e174b723204588208125d3c15fc6"
const official3894SourceDispatchPredecessorHead = "66438ec4b80fb3c77f2825d382657f914991fd7a"

// Normal PR668 distribution, independently observed before the admission-wait
// successor is generated. docs/evidence/skills-3080-guard-before-source-wait.json
const official3894SourceWaitPredecessorSHA = "4ba236f463cfd6e657399c50d2a690750cf278f49ddc18113872ad32b2edd776"
const official3894SourceWaitPredecessorHead = "046b21597bc3c0510bc407557a4a2c515b15f6ab"

type official3894Source struct {
	Head, Registration, CreatorJob, Workspace, Branch, SpecSHA string
}

type official3894Owned struct {
	Version      int    `json:"version"`
	ID           string `json:"id"`
	Root         string `json:"root"`
	Path         string `json:"path"`
	Volume       uint32 `json:"volume"`
	FileID       uint64 `json:"file_id"`
	Birth        uint64 `json:"birth"`
	Created      string `json:"created_at"`
	Project      string `json:"project"`
	Content      string `json:"content"`
	ProducerJob  string `json:"producer_job"`
	ProducerTodo int    `json:"producer_todo"`
	ProducerKind string `json:"producer_kind"`
	Ancestors    []struct {
		Path   string `json:"path"`
		Volume uint32 `json:"volume"`
		FileID uint64 `json:"file_id"`
		Birth  uint64 `json:"birth"`
	} `json:"ancestors"`
	Integrity string `json:"integrity"`
	State     string `json:"state"`
}

func official3894Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func official3894Hex(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return len(s) == n && e == nil && hex.EncodeToString(b) == s
}

func official3894Flag(args []string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		for _, f := range []string{"prepare-official-ci3387", "verify-official-ci3387", "consume-official-ci3387", "requalify-consume-official-ci3387", "verify-official-ci3387-update"} {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

func official3894Parse(args []string) (string, string, bool) {
	if len(args) == 1 && args[0] == "--verify-official-ci3387-update" {
		return "update", "", true
	}
	if len(args) == 1 && args[0] == "--prepare-official-ci3387" {
		return "prepare", "", true
	}
	if len(args) == 2 && official3894Hex(args[1], 32) {
		switch args[0] {
		case "--verify-official-ci3387":
			return "verify", args[1], true
		case "--consume-official-ci3387":
			return "consume", args[1], true
		case "--requalify-consume-official-ci3387":
			return "requalify-consume", args[1], true
		}
	}
	return "", "", false
}

func official3894Failure(reason string) object {
	return object{"purpose_verified": false, "reason": reason, "access_allowed": false, "original_registry_recovered": false, "launch_performed": false}
}

type official3894Error string

func (e official3894Error) Error() string { return string(e) }

func runOfficial3894CLI(args []string, out io.Writer, operation func(context.Context, string, string, func(object) error) (object, error)) int {
	op, id, ok := official3894Parse(args)
	if !ok {
		_ = json.NewEncoder(out).Encode(official3894Failure("arguments"))
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	handed := false
	handoff := func(result object) error {
		if !official3894Consumes(op) || handed {
			return oc.ErrDenied
		}
		handed = true
		return json.NewEncoder(official3894ExactWriter{out}).Encode(result)
	}
	r, e := operation(ctx, op, id, handoff)
	// A failed/partial public handoff must never get a second JSON document.
	if handed {
		if e != nil {
			return 1
		}
		return 0
	}
	if official3894Consumes(op) && e == nil {
		r, e = official3894Failure("consumer_handoff_required"), oc.ErrDenied
	}
	if e != nil {
		// Preserve bounded, non-secret ownership/state metadata, never public file
		// bodies from a callback whose durable terminal save did not succeed.
		if r == nil {
			r = official3894Failure("purpose_or_io")
		}
		var boundary official3894Error
		if errors.As(e, &boundary) {
			r["reason"] = string(boundary)
		} else if reason := oc.ValidationReason(e); reason != "" {
			r["reason"] = "sdk_" + reason
		} else if errors.Is(e, context.DeadlineExceeded) {
			r["reason"] = "deadline"
		} else if errors.Is(e, context.Canceled) {
			r["reason"] = "cancelled"
		}
		r["purpose_verified"] = false
		delete(r, "files")
		_ = json.NewEncoder(out).Encode(r)
		return 1
	}
	if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	return 0
}

type official3894ExactWriter struct{ io.Writer }

func (w official3894ExactWriter) Write(p []byte) (int, error) {
	n, e := w.Writer.Write(p)
	if e == nil && n != len(p) {
		e = io.ErrShortWrite
	}
	return n, e
}

type official3894Consumer interface {
	Consume(context.Context, string, func([]oc.Payload) error) error
}

func official3894Consumes(op string) bool { return op == "consume" || op == "requalify-consume" }

func official3894Consume(ctx context.Context, engine official3894Consumer, id string, summary func() object, handoff func(object) error) error {
	return official3894ConsumeWith(ctx, engine.Consume, id, summary, handoff)
}

func official3894ConsumeWith(ctx context.Context, consume func(context.Context, string, func([]oc.Payload) error) error, id string, summary func() object, handoff func(object) error) error {
	return consume(ctx, id, func(p []oc.Payload) error {
		r := summary()
		r["purpose_verified"] = true
		delete(r, "reason")
		r["files"] = p
		// The core still holds the verified physical objects and its durable
		// state is consuming. Only a successful actual write permits consumed.
		return handoff(r)
	})
}

// Explicit environment projection: do not read all environment variables and
// never let GH_TOKEN/GITHUB_TOKEN select an API-key credential in this path.
func official3894Env() []string {
	return official3894EnvWith(os.LookupEnv)
}
func official3894EnvWith(lookup func(string) (string, bool)) []string {
	var out []string
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "COMSPEC", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "LOCALAPPDATA", "APPDATA", "CC_JOB_ID"} {
		if value, ok := lookup(key); ok {
			out = append(out, key+"="+value)
		}
	}
	// TEMP is part of the target boundary, not a caller-selected root override.
	out = append(out, "TEMP="+official3894Temp, "TMP="+official3894Temp)
	return out
}

func official3894Command(ctx context.Context, file string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, file, args...)
	cmd.Env = official3894Env()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	// Every endpoint is a bounded metadata projection, and safe-delete create
	// emits only its one new receipt. A child cannot force unbounded capture.
	var b official3894Buffer
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	e := cmd.Run()
	if e != nil || b.overflow {
		return b.Bytes(), oc.ErrDenied
	}
	return b.Bytes(), nil
}

type official3894Buffer struct {
	bytes.Buffer
	overflow bool
}

func (b *official3894Buffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		b.overflow = true
		return 0, oc.ErrDenied
	}
	return b.Buffer.Write(p)
}

type official3894Pin struct {
	endpoint, query string
	expected        object
}

func official3894Pins() []official3894Pin {
	b := oc.FixedBinding("", "", "", "")
	a := oc.OfficialAsset()
	return []official3894Pin{
		{"repos/AutoDevJapan/cc-pc-tools-rs", "{id,full_name,private,owner:.owner.login}", object{"id": int64(1406818713), "full_name": "AutoDevJapan/cc-pc-tools-rs", "private": false, "owner": "AutoDevJapan"}},
		{"repos/AutoDevJapan/local-ai-console", "{id,full_name,private,owner:.owner.login}", object{"id": b.RepoID, "full_name": "AutoDevJapan/local-ai-console", "private": true, "owner": b.Owner}},
		{"repos/AutoDevJapan/local-ai-console/pulls/17", "{number,state,merged,head:.head.sha,repo_id:.head.repo.id}", object{"number": 17, "state": "open", "merged": false, "head": b.Head, "repo_id": b.RepoID}},
		{"repos/AutoDevJapan/local-ai-console/git/commits/" + b.Head, "{sha,tree:.tree.sha}", object{"sha": b.Head, "tree": b.Tree}},
		{"repos/AutoDevJapan/local-ai-console/actions/runs/40228177555", "{id,run_attempt,head_sha,status,conclusion,repository_id:.repository.id,workflow_id,event,path}", object{"id": b.RunID, "run_attempt": b.Attempt, "head_sha": b.Head, "status": "queued", "conclusion": nil, "repository_id": b.RepoID, "workflow_id": 392978784, "event": "pull_request", "path": ".github/workflows/windows.yml"}},
		{"repos/AutoDevJapan/local-ai-console/actions/workflows/392978784/runs?event=pull_request&per_page=100", "[.workflow_runs[]|select(any(.pull_requests[]?;.number==17))][0]|{id,head_sha,run_attempt}", object{"id": b.RunID, "head_sha": b.Head, "run_attempt": b.Attempt}},
		{"repos/AutoDevJapan/local-ai-console/actions/jobs/121273999986", "{id,run_id,head_sha,status,conclusion,labels}", object{"id": b.JobID, "run_id": b.RunID, "head_sha": b.Head, "status": "queued", "conclusion": nil, "labels": []string{"self-hosted", "Windows", "X64", "todo3339"}}},
		{"repos/actions/runner", "{full_name,private,owner:.owner.login}", object{"full_name": "actions/runner", "private": false, "owner": "actions"}},
		{"repos/actions/runner/releases/tags/v2.338.0", "{tag_name,draft,prerelease,assets:[.assets[]|select(.id==615677454)|{id,name,size,digest,state,browser_download_url}]}", object{"tag_name": a.Tag, "draft": false, "prerelease": false, "assets": []object{{"id": a.ID, "name": a.Name, "size": a.Size, "digest": "sha256:" + a.SHA256, "state": "uploaded", "browser_download_url": a.URL}}}},
	}
}

func official3894Match(raw []byte, want object) bool {
	got, ok := conversationJSON(raw)
	if !ok {
		return false
	}
	b, e := json.Marshal(got)
	w, e2 := json.Marshal(want)
	return e == nil && e2 == nil && bytes.Equal(b, w)
}

func official3894GitHub(ctx context.Context) error {
	return official3894GitHubWith(ctx, official3894Command)
}

func official3894GitHubWith(ctx context.Context, command func(context.Context, string, ...string) ([]byte, error)) error {
	queueTimedOut := false
	for i, p := range official3894Pins() {
		b, e := command(ctx, official3894Gate, "api", "--hostname", "github.com", p.endpoint, "--jq", p.query)
		if e != nil {
			return official3894Error("github_metadata_pin_" + strconv.Itoa(i+1))
		}
		// Keep the receipt's original run/attempt/job binding. A normal retry
		// may be prepared only when this exact original job never executed and
		// GitHub independently reports its 24-hour unassigned-queue timeout.
		if i == 4 && official3387CancelledQueueReply(b, p.expected) {
			queueTimedOut = true
			continue
		}
		if i == 6 && queueTimedOut {
			if !official3387CancelledQueueReply(b, p.expected) {
				return official3894Error("github_metadata_pin_7")
			}
			if err := official3387UnassignedTimeoutProof(ctx, command); err != nil {
				return err
			}
			continue
		}
		if !official3894Match(b, p.expected) {
			return official3894Error("github_metadata_pin_" + strconv.Itoa(i+1))
		}
	}
	return nil
}

func official3894Birth(pid uint32) (uint64, bool) {
	h, e := syscall.OpenProcess(0x1000|0x100000, false, pid)
	if e != nil {
		return 0, false
	}
	defer syscall.CloseHandle(h)
	var c, x, k, u syscall.Filetime
	var code uint32
	if syscall.GetProcessTimes(h, &c, &x, &k, &u) != nil || syscall.GetExitCodeProcess(h, &code) != nil || code != 259 {
		return 0, false
	}
	return uint64(c.HighDateTime)<<32 | uint64(c.LowDateTime), true
}

func official3894Descendant(job object) bool {
	want, birth := conversationNumber(job["codex_pid"]), conversationNumber(job["codex_pid_created"])
	if want == 0 || want > 0xffffffff || birth == 0 {
		return false
	}
	actual, ok := official3894Birth(uint32(want))
	if !ok || actual != birth {
		return false
	}
	h, e := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	parents := map[uint32]uint32{}
	var p syscall.ProcessEntry32
	p.Size = uint32(unsafe.Sizeof(p))
	for e = syscall.Process32First(h, &p); e == nil; e = syscall.Process32Next(h, &p) {
		parents[p.ProcessID] = p.ParentProcessID
	}
	pid := uint32(os.Getpid())
	seen := map[uint32]bool{}
	for i := 0; i < 64 && pid != 0 && !seen[pid]; i++ {
		if pid == uint32(want) {
			return true
		}
		seen[pid] = true
		pid = parents[pid]
	}
	return false
}

func official3894BuildHead() (string, bool) {
	b, ok := debug.ReadBuildInfo()
	if !ok || b.Path != skillsGuardModule {
		return "", false
	}
	settings := map[string]string{}
	for _, s := range b.Settings {
		settings[s.Key] = s.Value
	}
	head := settings["vcs.revision"]
	return head, official3894Hex(head, 40) && settings["vcs.modified"] == "false" && settings["GOOS"] == "windows" && settings["GOARCH"] == "amd64"
}

func official3894TaskLineage(reg skillsRegistry) bool {
	parent, provider, creator := reg.Tasks[3387], reg.Tasks[3888], reg.Tasks[3894]
	b := oc.FixedBinding("", "", "", "")
	// These are nominated ordinary independent tasks, not typed subwork. A
	// review/check parent field must never supply their implementation authority.
	independent := func(row skillsTask) bool {
		return row.Subwork == (skillsSubwork{}) && row.Scheduler.SubworkParent == "" && row.Scheduler.Admission == nil
	}
	active := func(row skillsTask) bool {
		return row.Status == "todo" || row.Status == "doing" || row.Status == "wait"
	}
	return parent.ID == 3387 && provider.ID == 3888 && creator.ID == 3894 &&
		parent.Project == "cc-pc-tools-rs" && creator.Project == parent.Project && provider.Project == "localai-web-rs" &&
		skillsHasJob(parent, b.ParentJob) && skillsHasJob(provider, b.OriginalProviderJob) &&
		skillsHasJob(creator, official3894OriginalCreator) && independent(provider) && independent(creator) &&
		!skillsHeld(parent) && !skillsHeld(provider) && !skillsHeld(creator) && active(parent) && active(provider) && (active(creator) || creator.Status == "done")
}

// Read job metadata without decoding prompt, notes, last_message or native
// content. The normal source reader holds this original record's file handle.
func official3894ReadJob(s *skillsFS, id string) (object, bool) {
	if !currentSubworkJobName.MatchString(id) {
		return nil, false
	}
	f, ok := s.open(filepath.Join(skillsJobsRoot, id, "job.json"), 4<<20)
	if !ok {
		return nil, false
	}
	return official3894JobProjection(io.LimitReader(f, 4<<20+1), id)
}

func official3894JobProjection(input io.Reader, id string) (object, bool) {
	d := json.NewDecoder(input)
	d.UseNumber()
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return nil, false
	}
	allowed := map[string]bool{}
	for _, k := range []string{"id", "todo_id", "project", "task_type", "parent_job_id", "subwork", "work_owner", "scope_source", "cwd", "status", "launch_key", "codex_pid", "codex_pid_created"} {
		allowed[k] = true
	}
	out, seen := object{}, map[string]bool{}
	for d.More() {
		key, e := d.Token()
		k, ok := key.(string)
		if e != nil || !ok || seen[k] {
			return nil, false
		}
		seen[k] = true
		if allowed[k] {
			var raw json.RawMessage
			if d.Decode(&raw) != nil {
				return nil, false
			}
			// Validate duplicate nested metadata without decoding ignored text.
			wrapped := append([]byte(`{"value":`), raw...)
			wrapped = append(wrapped, '}')
			value, ok := conversationJSON(wrapped)
			if !ok {
				return nil, false
			}
			out[k] = value["value"]
		} else {
			var discard ownerDiscardJSON
			if d.Decode(&discard) != nil {
				return nil, false
			}
		}
	}
	if _, e := d.Token(); e != nil {
		return nil, false
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, false
	}
	return out, out["id"] == id
}

// The owner's finite request nominates these exact original independent jobs.
// Confirm each original job's actual task and source ownership; hashing the
// resulting metadata binds their relation on reuse without publishing requests.
func official3894OriginalLineage(reg skillsRegistry, read func(string) (object, bool)) (string, bool) {
	b := oc.FixedBinding("", "", "", "")
	proof := []object{}
	common := ""
	for _, origin := range []struct {
		task                  int
		job, repository, kind string
	}{
		{3387, b.ParentJob, "autodevjapan/cc-pc-tools-rs", "research"},
		{3888, b.OriginalProviderJob, "autodevjapan/local-ai-console", "implementation"},
		{3894, official3894OriginalCreator, "autodevjapan/cc-pc-tools-rs", "implementation"},
	} {
		row := reg.Tasks[origin.task]
		job, ok := read(origin.job)
		owner := obj(job["work_owner"])
		if !ok || job["id"] != origin.job || !skillsJobTask(job, origin.task) || job["project"] != row.Project || job["task_type"] != origin.kind || job["scope_source"] != "launcher" ||
			!official3894IndependentJob(job) || len(owner) != 4 || !strings.EqualFold(str(owner["repository"]), origin.repository) || str(owner["git_common"]) == "" ||
			!same(str(job["cwd"]), row.Scheduler.Worktree) || !same(str(owner["worktree"]), row.Scheduler.Worktree) || owner["branch"] != row.Scheduler.Branch {
			return "", false
		}
		if origin.task == 3387 {
			common = str(owner["git_common"])
		}
		if origin.task == 3894 && !same(common, str(owner["git_common"])) {
			return "", false
		}
		proof = append(proof, object{"id": origin.job, "todo_id": origin.task, "project": row.Project, "task_type": origin.kind, "work_owner": owner})
	}
	raw, e := json.Marshal(proof)
	return official3894Hash(raw), e == nil
}

func official3894IndependentJob(job object) bool {
	parent := job["parent_job_id"]
	return (parent == nil || parent == "") && job["subwork"] == nil
}

// Only the normal metadata reader projects these three existing tasks. It
// neither creates a registry nor returns notes, original prompts or credentials.
func official3894SourceCheck(ctx context.Context, saved *official3894Source, consume bool) (official3894Source, error) {
	source, _, err := official3894SourceCheckWithActor(ctx, saved, consume)
	return source, err
}

type official3894VerifiedActor string

func official3894SourceCheckWithActor(ctx context.Context, saved *official3894Source, consume bool) (official3894Source, official3894VerifiedActor, error) {
	// Capture once. The actor persisted by Observe is exactly the caller passed
	// through the normal source/assignment/process checks, never a later read.
	caller := os.Getenv("CC_JOB_ID")
	source, err := official3894SourceCheckCaller(ctx, saved, consume, caller)
	if err != nil {
		return source, "", err
	}
	return source, official3894VerifiedActor(caller), nil
}

func official3894SourceCheckCaller(ctx context.Context, saved *official3894Source, consume bool, caller string) (official3894Source, error) {
	var result official3894Source
	m := currentSubworkJobName.FindStringSubmatch(caller)
	if len(m) != 3 || (m[2] != "3894" && m[2] != "3888") || (consume && m[2] != "3888") {
		return result, official3894Error("registered_actor_required")
	}
	reg, ok := official3894Registry(ctx)
	if !ok {
		return result, official3894Error("normal_task_metadata_unavailable")
	}
	creator := reg.Tasks[3894]
	b := oc.FixedBinding("", "", "", "")
	if !official3894TaskLineage(reg) {
		return result, official3894Error("original_task_lineage_or_stop")
	}
	actorID, _ := strconv.Atoi(m[2])
	row := reg.Tasks[actorID]
	if (row.Status != "doing" && row.Status != "todo") || row.Scheduler.Job != caller || !skillsLatest(row, caller) || !skillsHasJob(row, caller) {
		return result, official3894Error("current_assignment_required")
	}
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	origins, ok := official3894OriginalLineage(reg, func(id string) (object, bool) { return official3894ReadJob(&s, id) })
	if !ok {
		return result, official3894Error("original_registered_job_lineage_required")
	}
	job, ok := official3894ReadJob(&s, caller)
	if !ok || !skillsJobTask(job, actorID) || job["task_type"] != "implementation" || !official3894IndependentJob(job) || job["status"] != "running" || !official3894Descendant(job) || !same(str(job["cwd"]), row.Scheduler.Worktree) || job["launch_key"] != row.Scheduler.Key {
		return result, official3894Error("native_actor_identity_required")
	}
	if saved == nil && actorID != 3894 {
		return result, official3894Error("original_creator_required")
	}
	creatorJob := caller
	if saved != nil {
		creatorJob = saved.CreatorJob
	}
	if !skillsHasJob(creator, creatorJob) || !strings.HasPrefix(creator.Scheduler.Branch, "scheduler/todo-3894-") || !within(roots[3]+"-wt", creator.Scheduler.Worktree) {
		return result, official3894Error("original_creator_assignment_required")
	}
	if actorID == 3888 {
		verdict, expired := ownerRegisteredSourceStatus(row.Scheduler.Worktree)
		if !verdict.AccessAllowed {
			if expired {
				return result, official3894Error("provider_registered_source_deadline")
			}
			return result, official3894Error("provider_registered_source_required")
		}
		if !sourceOriginVerified(row.Scheduler.Worktree, "autodevjapan/local-ai-console") {
			return result, official3894Error("provider_source_origin_required")
		}
		if !official3894ProviderSource(row.Scheduler.Worktree, row.Scheduler.Branch) {
			return result, official3894Error("provider_source_binding_required")
		}
	}
	if !within(currentTodoSpecsRoot, creator.Spec) || !strings.HasPrefix(filepath.Base(creator.Spec), "3894-") || !currentSubworkSpecName.MatchString(filepath.Base(creator.Spec)) {
		return result, official3894Error("original_creator_spec_scope_required")
	}
	spec, ok := s.read(creator.Spec, 1<<20)
	if !ok {
		return result, official3894Error("original_creator_spec_unavailable")
	}
	specSHA := official3894Hash(spec)
	// Source registration qualifies only the actual source. The SDK anchor is
	// never sent to this function or made into a Git source.
	verdict, expired := ownerRegisteredSourceStatus(creator.Scheduler.Worktree)
	if !verdict.AccessAllowed {
		if expired {
			return result, official3894Error("creator_registered_source_deadline")
		}
		return result, official3894Error("normal_registered_source_required")
	}
	if !sourceVerified(creator.Scheduler.Worktree) {
		return result, official3894Error("creator_source_origin_required")
	}
	head, ok := official3894BuildHead()
	if !ok {
		return result, official3894Error("clean_windows_source_build_required")
	}
	head, e := official3894SourceRevision(head, saved,
		func(revision string) bool { return official3894InputsUnchanged(creator.Scheduler.Worktree, revision) },
		func(revision string) official3894MergeOutcome {
			return official3894MergeLookup(ctx, revision, official3894Command)
		})
	if e != nil {
		return result, e
	}
	// Only immutable registration fields enter the binding. Normal progress,
	// waiting and new provider jobs cannot silently replace the original purpose.
	proof, _ := json.Marshal(struct{ Root, Spec, SpecSHA, Workspace, Branch, ParentJob, ProviderJob, OriginalJobs string }{reg.Root, creator.Spec, specSHA, creator.Scheduler.Worktree, creator.Scheduler.Branch, b.ParentJob, b.OriginalProviderJob, origins})
	result = official3894Source{head, official3894Hash(proof), creatorJob, creator.Scheduler.Worktree, creator.Scheduler.Branch, specSHA}
	if saved != nil && result != *saved {
		return result, official3894Error("original_source_registration_changed")
	}
	return result, ctx.Err()
}

func official3894Registry(ctx context.Context) (skillsRegistry, bool) {
	s := skillsFS{blocked: skillsBlocked}
	defer s.close()
	// The ordinary store is already close to the older reader's 64 MiB limit.
	// Stream its existing metadata projection; retain only these three rows,
	// never notes, private prompts, unrelated project names or a replacement DB.
	f, ok := s.open(productStore, 512<<20)
	if !ok {
		return skillsRegistry{}, false
	}
	r, ok := official3894RegistryProjection(ownerDeadlineReader{ctx: ctx, input: io.LimitReader(f, 512<<20+1)})
	return r, ok && ctx.Err() == nil
}

func official3894RegistryProjection(input io.Reader) (skillsRegistry, bool) {
	return skillsRegistryProjectionWith(input, []int{3387, 3888, 3894}, official3894TaskMetadata)
}

func official3894Download(ctx context.Context) ([]byte, error) {
	a := oc.OfficialAsset()
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if e != nil {
		return nil, e
	}
	request.Header.Set("User-Agent", "cc-pc-tools-rs-fixed-official-software")
	client := http.Client{Timeout: 90 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || r.URL.Scheme != "https" || r.URL.User != nil || (r.URL.Host != "github.com" && r.URL.Host != "release-assets.githubusercontent.com" && r.URL.Host != "objects.githubusercontent.com") {
			return oc.ErrDenied
		}
		return nil
	}}
	response, e := client.Do(request)
	if e != nil {
		return nil, oc.ErrDenied
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || (response.ContentLength >= 0 && response.ContentLength != a.Size) {
		return nil, oc.ErrDenied
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, a.Size+1))
	if e != nil || int64(len(b)) != a.Size || official3894Hash(b) != a.SHA256 {
		return nil, oc.ErrDenied
	}
	return b, nil
}

type official3894Archive []byte

func (a official3894Archive) Open(context.Context, oc.Asset) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a)), nil
}

func official3894OwnedCheck(raw []byte, source official3894Source, since time.Time) (official3894Owned, error) {
	var r official3894Owned
	if !skillsStrict(raw, &r) || r.Version != 3 || !official3894Hex(r.ID, 32) || !official3894Hex(r.Integrity, 64) || r.State != "owned" || r.Project != "todo-3894" || r.Content != "official-ci3387-sdk" || r.ProducerKind != "scheduler" || r.ProducerTodo != 3894 || r.ProducerJob != source.CreatorJob ||
		!filepath.IsAbs(r.Root) || !same(r.Path, filepath.Join(r.Root, "codex-owned-"+r.ID)) || !same(r.Root, official3894Temp) || r.Volume == 0 || r.FileID == 0 || r.Birth == 0 || len(r.Ancestors) == 0 {
		return r, oc.ErrDenied
	}
	created, e := time.Parse(time.RFC3339Nano, r.Created)
	if e != nil || created.Before(since) || created.After(time.Now()) {
		return r, oc.ErrDenied
	}
	return r, nil
}

type official3894Authority struct {
	source         official3894Source
	owned          official3894Owned
	raw            []byte
	issuerIdentity oc.Identity
	started        time.Time
	fs             *oc.NativeFS
	consume        bool
}

func (a official3894Authority) Observe(ctx context.Context) (oc.Evidence, error) {
	var out oc.Evidence
	observed := time.Now()
	if _, e := official3894OwnedCheck(a.raw, a.source, a.started); e != nil {
		return out, official3894Error("original_owned_receipt_metadata_invalid")
	}
	if a.fs.ParentIdentity() != (oc.Identity{Volume: a.owned.Volume, FileID: a.owned.FileID, Birth: a.owned.Birth}) {
		return out, official3894Error("sdk_anchor_identity_changed")
	}
	_, caller, e := official3894SourceCheckWithActor(ctx, &a.source, a.consume)
	if e != nil {
		return out, e
	}
	if e := official3894GitHub(ctx); e != nil {
		return out, e
	}
	// Authenticate the saved direct issuer output before this read; compare the
	// original issuer's current record, never a caller-provided receipt checksum.
	s := skillsFS{blocked: func(p string) bool { return legacyMatch(p) || ownerCardPath(p) }}
	defer s.close()
	path := filepath.Join("D:/CC/Tools/safe-delete/receipts-go", a.owned.ID+".json")
	b, ok := s.read(path, 8192)
	if !ok {
		return out, official3894Error("original_issuer_receipt_unavailable")
	}
	if !bytes.Equal(b, a.raw) {
		return out, official3894Error("original_issuer_receipt_bytes_changed")
	}
	identity, e := official3894Identity(s.files[len(s.files)-1])
	if e != nil {
		return out, official3894Error("original_issuer_identity_unavailable")
	}
	if identity != a.issuerIdentity || identity.FileID == 0 {
		return out, official3894Error("original_issuer_identity_changed")
	}
	binding := oc.FixedBinding(a.source.Head, a.source.Registration, official3894Hash(a.raw), a.source.CreatorJob)
	out = oc.Evidence{Parent: a.fs.ParentIdentity(), FreshOwnedAnchor: true, Binding: binding, Asset: oc.OfficialAsset(), Observed: observed, SourceVerified: true, JobVerified: true, Active: true, OfficialPublicAsset: true, ActorJob: string(caller)}
	return out, nil
}

func official3894Summary(t official3894Ticket) object {
	r := official3894Failure("purpose_or_io")
	if t.CreatorPhase != "" {
		r["creation_state"] = t.CreatorPhase
	}
	if len(t.IssuerOutput) > 0 {
		r["original_creator_output_base64"] = t.IssuerOutput
		r["original_creator_output_sha256"] = official3894Hash(t.IssuerOutput)
	}
	if len(t.Owned) > 0 {
		var owned official3894Owned
		if skillsStrict(t.Owned, &owned) {
			r["owned_id"] = owned.ID
			r["original_creation_receipt_base64"] = t.Owned
			r["original_creation_receipt_sha256"] = official3894Hash(t.Owned)
			r["original_creation_receipt_identity"] = t.IssuerIdentity
		}
	}
	if t.Receipt != nil {
		r["receipt_id"] = t.Receipt.ID
		r["state"] = t.Receipt.State
		r["source_head"] = t.Source.Head
		r["source_registration_sha256"] = t.Source.Registration
		r["archive_bytes"] = t.Receipt.Asset.Size
		r["archive_sha256"] = t.Receipt.Asset.SHA256
		r["root_identity"] = t.Receipt.Root
		r["parent_identity"] = t.Receipt.Parent
		var owned official3894Owned
		if skillsStrict(t.Owned, &owned) {
			r["sdk_path"] = filepath.Join(owned.Path, t.Receipt.ID)
		}
		r["manifest_entries"] = len(t.Receipt.Files)
		if t.Requalification != nil {
			r["expired_at_requalification"] = true
			r["receipt_deadline_extended"] = false
			r["original_receipt_created_at"] = t.Receipt.Created
			r["original_receipt_expires_at"] = t.Receipt.Expires
			r["requalification_accepted_at"] = t.Requalification.AcceptedAt
			r["requalification_actor_job"] = t.Requalification.Evidence.ActorJob
			r["requalification_original_receipt_sha256"] = t.Requalification.OriginalReceiptSHA256
			r["requalification_evidence_sha256"] = t.Requalification.EvidenceSHA256()
		}
	}
	return r
}

func official3894Operation(ctx context.Context, op, id string, handoff func(object) error) (object, error) {
	if op == "update" {
		return official3894Update(ctx)
	}
	// These are purpose-specific native metadata records. No normal data path
	// classification is modified by opening this fixed journal.
	store, e := official3894OpenStore(official3894Store, op == "prepare")
	if e != nil {
		return official3894Failure("creation_store_busy_invalid_or_already_reserved"), e
	}
	defer store.close()
	if op == "prepare" {
		source, e := official3894SourceCheck(ctx, nil, false)
		if e != nil {
			return nil, e
		}
		if e = official3894GitHub(ctx); e != nil {
			return nil, e
		}
		issuer, e := official3894PinIssuer()
		if e != nil {
			return nil, e
		}
		defer issuer.close()
		archive, e := official3894Download(ctx)
		if e != nil {
			return nil, e
		}
		start := time.Now().UTC()
		if e = store.start(official3894Ticket{Domain: official3894Domain, Source: source, Started: start, CreatorPhase: "create_reserved"}); e != nil {
			return nil, e
		}
		// The constructor itself invokes the normal issuer. No existing ID/root
		// is accepted, and the ticket reservation prevents a second SDK attempt.
		raw, e := official3894Command(ctx, official3894Safe, "create", "--project", "todo-3894", "--content", "official-ci3387-sdk", "--root", official3894Temp)
		attempt := store.ticket
		attempt.IssuerOutput = append([]byte(nil), raw...)
		attempt.CreatorPhase = "issuer_returned"
		if e != nil {
			attempt.CreatorPhase = "issuer_failed_or_interrupted"
		}
		if saved := store.append(attempt); saved != nil {
			return official3894Summary(attempt), saved
		}
		if e != nil {
			return official3894Summary(store.ticket), e
		}
		owned, e := official3894OwnedCheck(raw, source, start)
		if e != nil {
			return official3894Summary(store.ticket), e
		}
		issuerReceipt := skillsFS{blocked: func(p string) bool { return legacyMatch(p) || ownerCardPath(p) }}
		original, ok := issuerReceipt.read(filepath.Join("D:/CC/Tools/safe-delete/receipts-go", owned.ID+".json"), 8192)
		var issuerIdentity oc.Identity
		if ok && bytes.Equal(original, raw) {
			issuerIdentity, e = official3894Identity(issuerReceipt.files[len(issuerReceipt.files)-1])
		} else {
			e = oc.ErrDenied
		}
		issuerReceipt.close()
		if e != nil {
			return official3894Summary(store.ticket), e
		}
		next := store.ticket
		next.Owned = append([]byte(nil), raw...)
		next.IssuerIdentity = issuerIdentity
		next.CreatorPhase = "ownership_recorded"
		if e = store.append(next); e != nil {
			return official3894Summary(next), e
		}
		fs, e := oc.NewNativeFS(filepath.Clean(owned.Path), oc.Identity{Volume: owned.Volume, FileID: owned.FileID, Birth: owned.Birth})
		if e != nil {
			return official3894Summary(next), e
		}
		if e = fs.EmptyParent(); e != nil {
			return official3894Summary(next), e
		}
		a := official3894Authority{source: source, owned: owned, raw: raw, issuerIdentity: issuerIdentity, started: start, fs: fs}
		engine, e := oc.New(oc.FixedBinding(source.Head, source.Registration, official3894Hash(raw), source.CreatorJob), a, official3894Archive(archive), store, fs)
		if e != nil {
			return official3894Summary(store.ticket), e
		}
		r, e := engine.Construct(ctx)
		result := official3894Summary(store.ticket)
		if e != nil {
			return result, e
		}
		if e = engine.Resume(ctx, r.ID); e != nil {
			return result, e
		}
		result["purpose_verified"] = true
		delete(result, "reason")
		return result, nil
	}
	t := store.ticket
	if t.Receipt == nil || t.Receipt.ID != id || t.Domain != official3894Domain {
		return nil, official3894Error("original_creation_receipt_required")
	}
	var owned official3894Owned
	if !skillsStrict(t.Owned, &owned) {
		return nil, official3894Error("saved_owned_receipt_invalid")
	}
	fs, e := oc.NewNativeFS(filepath.Clean(owned.Path), t.Receipt.Parent)
	if e != nil {
		return official3894Summary(t), official3894Error("sdk_parent_identity_or_open_failed")
	}
	a := official3894Authority{source: t.Source, owned: owned, raw: t.Owned, issuerIdentity: t.IssuerIdentity, started: t.Started, fs: fs, consume: official3894Consumes(op)}
	engine, e := oc.New(oc.FixedBinding(t.Source.Head, t.Source.Registration, official3894Hash(t.Owned), t.Source.CreatorJob), a, official3894Archive(nil), store, fs)
	if e != nil {
		return official3894Summary(t), official3894Error("sdk_engine_binding_invalid")
	}
	if op == "verify" {
		e = engine.Resume(ctx, id)
	} else if op == "consume" {
		e = official3894Consume(ctx, engine, id, func() object { return official3894Summary(store.ticket) }, handoff)
	} else if op == "requalify-consume" {
		e = official3894ConsumeWith(ctx, engine.RequalifyConsume, id, func() object { return official3894Summary(store.ticket) }, handoff)
	} else {
		e = oc.ErrDenied
	}
	result := official3894Summary(store.ticket)
	if e != nil {
		return result, e
	}
	result["purpose_verified"] = true
	delete(result, "reason")
	return result, nil
}

func official3894InstalledBaseline(sha, head string) bool {
	return (sha == official3894PreviousGuardSHA && head == official3894PreviousGuardHead) ||
		(sha == official3894RepairGuardSHA && head == official3894RepairGuardHead) ||
		(sha == official3894DiagnosticGuardSHA && head == official3894DiagnosticGuardHead) ||
		(sha == official3894ConcurrentGuardSHA && head == official3894ConcurrentGuardHead) ||
		(sha == official3894SharedGuardSHA && head == official3894SharedGuardHead) ||
		(sha == official3894MaskedPredecessorSHA && head == official3894MaskedPredecessorHead) ||
		(sha == official3894MaskedDistributionSHA && head == official3894MaskedDistributionHead) ||
		(sha == official3894NativePredecessorSHA && head == official3894NativePredecessorHead) ||
		(sha == official3894NoticePredecessorSHA && head == official3894NoticePredecessorHead) ||
		(sha == official3894AliasPredecessorSHA && head == official3894AliasPredecessorHead) ||
		(sha == official3894CanonicalAssignmentPredecessorSHA && head == official3894CanonicalAssignmentPredecessorHead) ||
		(sha == official3894CreationPathPredecessorSHA && head == official3894CreationPathPredecessorHead) ||
		(sha == official3894BootstrapPredecessorSHA && head == official3894BootstrapPredecessorHead) ||
		(sha == official3894PublicManifestPredecessorSHA && head == official3894PublicManifestPredecessorHead) ||
		(sha == official3894RuntimeOriginPredecessorSHA && head == official3894RuntimeOriginPredecessorHead) ||
		(sha == official3894SourceDispatchPredecessorSHA && head == official3894SourceDispatchPredecessorHead) ||
		(sha == official3894SourceWaitPredecessorSHA && head == official3894SourceWaitPredecessorHead)
}

// Qualifies only comparison of the known previously published guard software.
// It performs no update, SDK creation, data read grant or general classification.
// Review/CI acceptance and the existing AdminBroker CAS remain separate gates.
func official3894Update(ctx context.Context) (object, error) {
	var source official3894Source
	var e error
	if m := currentSubworkJobName.FindStringSubmatch(os.Getenv("CC_JOB_ID")); len(m) == 3 && m[2] == "3387" {
		source, e = official3894ParentUpdateSource(ctx)
	} else {
		source, e = official3894SourceCheck(ctx, nil, false)
	}
	if e != nil {
		return nil, e
	}
	s := skillsFS{blocked: func(path string) bool {
		return !same(path, skillsRuntimeTarget) || legacyMatch(path) || ownerCardPath(path)
	}}
	defer s.close()
	raw, ok := s.read(skillsRuntimeTarget, 64<<20)
	if !ok {
		return nil, official3894Error("known_guard_software_unavailable")
	}
	head, ok := skillsBuildRevision(raw)
	currentSHA := official3894Hash(raw)
	if !ok || !official3894InstalledBaseline(currentSHA, head) {
		// Never return or adopt an unknown observed hash as the new baseline.
		return nil, official3894Error("known_guard_software_mismatch")
	}
	identity, e := official3894Identity(s.files[len(s.files)-1])
	if e != nil {
		return nil, e
	}
	return object{"purpose_verified": true, "access_allowed": false, "original_registry_recovered": false, "launch_performed": false,
		"domain": "official-ci3387-software-comparison-v1", "todo_id": 3894, "current_sha256": currentSHA,
		"current_source_head": head, "current_identity": identity, "source_head": source.Head, "source_registration_sha256": source.Registration,
		"sdk_created": false, "update_performed": false, "general_access_granted": false}, nil
}

// Pins are from the original finite publisher in tools/owned-cleanup-deploy,
// source 89f2411, not hashes supplied by a caller or learned from unknown data.
// The original software handle remains held through CreateProcess and receipt
// capture so its path cannot be replaced after this comparison.
func official3894PinIssuer() (*skillsFS, error) {
	s := &skillsFS{blocked: func(p string) bool { return !same(p, official3894Safe) || legacyMatch(p) || ownerCardPath(p) }}
	b, ok := s.read(official3894Safe, 64<<20)
	if ok {
		switch official3894Hash(b) {
		case "286a22b24412cc19138c083e09a93f4626de9058f329fad256e3f377b1e3b73b", "7e1737b6bcd7f1b015998e8594527dbab3f211b68cbe8ad3b82f6a46bca39514":
			return s, nil
		}
	}
	s.close()
	return nil, official3894Error("normal_creation_issuer_unverified")
}
