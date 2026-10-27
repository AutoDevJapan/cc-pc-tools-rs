// Package primaryflow prepares read-only primary-file research. Its plans do
// not establish licence permission, package context, adoption or a full read.
package primaryflow

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const Corpus = "D:/CC/Work/unattended-operations-20261013-wt/todo-3080-skills-inventory/.ci-work/todo3080"
const Parent = "D:/CC/Work/unattended-operations-20261013-wt/todo-3080-16-skills-3048/.ci-work/todo3080"
const CatalogSHA = "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c"
const QueueSHA = "9694c7a9caa07d5f9f1392eef532f1f016c54f45be2c4387a20e9c343e788e1d"
const Commit = "920714f17d81e0a95c7a9bc51faf0a7921cd982d"

type Member struct {
	ID      string `json:"inventory_id"`
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	Path    string `json:"path"`
	SHA     string `json:"sha256"`
	Blob    string `json:"git_blob"`
	Bytes   int64  `json:"bytes"`
	Content string `json:"content_path"`
}
type Group struct {
	Ordinal int      `json:"ordinal"`
	SHA     string   `json:"body_sha256"`
	Bytes   int64    `json:"body_bytes"`
	Members []Member `json:"members"`
}
type Header struct {
	Domain           string `json:"domain"`
	ParentJob        string `json:"parent_origin_job"`
	Catalog          string `json:"catalog_sha256"`
	Queue            string `json:"queue_sha256"`
	Cohort           int    `json:"cohort"`
	TotalGroups      int    `json:"groups"`
	TotalOccurrences int    `json:"occurrences"`
	Scope            string `json:"scope"`
}
type Plan struct {
	Header Header  `json:"header"`
	Groups []Group `json:"groups"`
}
type Pin struct {
	Path  string `json:"path"`
	SHA   string `json:"sha256"`
	Bytes int64  `json:"bytes"`
}

var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var blobID = regexp.MustCompile(`^[0-9a-f]{40}$`)
var primaryPath = regexp.MustCompile(`^skills/[^/\\:]+/(?:[^/\\:]+/)+(?i:SKILL\.md)$`)

func validPrimaryPath(path string) bool {
	if !primaryPath.MatchString(path) {
		return false
	}
	for _, p := range strings.Split(path, "/") {
		if p == "." || p == ".." || strings.IndexFunc(p, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return false
		}
	}
	return true
}
func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Decode never converts historical unknown values into observed read states.
func Decode(b []byte, target any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if e := d.Decode(target); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func frozen(path, want string) ([]byte, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if Hash(b) != want {
		return nil, errors.New("fixed metadata SHA differs")
	}
	return b, nil
}
func HashStrings(v any, out map[string]bool) {
	switch x := v.(type) {
	case string:
		if digest.MatchString(x) {
			out[x] = true
		}
	case []any:
		for _, v := range x {
			HashStrings(v, out)
		}
	case map[string]any:
		for _, v := range x {
			HashStrings(v, out)
		}
	}
}

// Select is pure: it reads no source bodies and grants no permission through
// equal hashes. A protected hash is deferred for its existing evidence, not
// labelled read, licensed, excluded or completed.
func Select(rows []Member, reserved, protected map[string]bool) ([2]Plan, []Member, error) {
	var plans [2]Plan
	deferred := []Member{}
	byBody := map[string][]Member{}
	seen := map[string]bool{}
	sizes := map[string]int64{}
	blobs := map[string]string{}
	for _, r := range rows {
		if r.ID == "" || seen[r.ID] {
			return plans, nil, errors.New("empty or duplicate occurrence")
		}
		seen[r.ID] = true
		if r.Repo != "6mile/skills" || r.Commit != Commit || !validPrimaryPath(r.Path) || !digest.MatchString(r.SHA) || !blobID.MatchString(r.Blob) || r.Bytes <= 0 || r.Content != "github/6mile__skills/objects/"+r.SHA+".gz" {
			return plans, nil, errors.New("outside fixed saved primary descriptor")
		}
		if n, ok := sizes[r.SHA]; ok && n != r.Bytes {
			return plans, nil, errors.New("same digest has conflicting body lengths")
		}
		sizes[r.SHA] = r.Bytes
		if b, ok := blobs[r.SHA]; ok && b != r.Blob {
			return plans, nil, errors.New("same digest has conflicting Git blobs")
		}
		blobs[r.SHA] = r.Blob
		if reserved[r.ID] || protected[r.SHA] {
			deferred = append(deferred, r)
			continue
		}
		key := r.SHA + fmt.Sprintf(":%d", r.Bytes)
		byBody[key] = append(byBody[key], r)
	}
	keys := []string{}
	for k := range byBody {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ms := byBody[key]
		sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
		for _, m := range ms {
			if m.Blob != ms[0].Blob {
				return plans, nil, errors.New("same body descriptor has conflicting Git blob")
			}
		}
		b, _ := hex.DecodeString(ms[0].SHA[:2])
		cohort := int(b[0]) % 2
		g := Group{Ordinal: len(plans[cohort].Groups) + 1, SHA: ms[0].SHA, Bytes: ms[0].Bytes, Members: ms}
		plans[cohort].Groups = append(plans[cohort].Groups, g)
	}
	for i := range plans {
		n := 0
		for _, g := range plans[i].Groups {
			n += len(g.Members)
		}
		plans[i].Header = Header{Domain: "skills3080-primary-meaning-plan-v1", ParentJob: os.Getenv("CC_JOB_ID"), Catalog: CatalogSHA, Queue: QueueSHA, Cohort: i, TotalGroups: len(plans[i].Groups), TotalOccurrences: n, Scope: "Primary visible meaning only; each member's source context, support resources and individual rights remain unverified. No new raw/unique/adoption credit is inferred from metadata equality."}
	}
	return plans, deferred, nil
}

func Prepare() (map[string]any, error) {
	if os.Getenv("CC_JOB_ID") != "20261026-214717-todo-3080" {
		return nil, errors.New("actual original parent required")
	}
	reserved := map[string]bool{}
	protected := map[string]bool{}
	pins := []Pin{}
	for _, p := range [][2]string{{"sixmile-remaining-notice598-roster235901.json", "f9c6b2e44c5e3aa8947f758f864eaa0d5edc396d622359826bca848cd96f9901"}, {"othergit-all373-roster-v2074042.json", "460219790c088b33e6044c6a97c68a882549c608c3de46ea536aa990a2c1adac"}} {
		b, e := frozen(filepath.Join(Parent, p[0]), p[1])
		if e != nil {
			return nil, e
		}
		var r struct {
			Rows []Member `json:"rows"`
		}
		if e = Decode(b, &r); e != nil {
			return nil, e
		}
		for _, m := range r.Rows {
			if m.ID == "" || reserved[m.ID] {
				return nil, errors.New("old reservation overlap")
			}
			reserved[m.ID] = true
		}
		pins = append(pins, Pin{Path: p[0], SHA: p[1], Bytes: int64(len(b))})
	}
	if len(reserved) != 971 {
		return nil, errors.New("complete old reservations required")
	}
	for _, pin := range pins {
		b, e := frozen(filepath.Join(Parent, pin.Path), pin.SHA)
		if e != nil {
			return nil, e
		}
		var v any
		if e = Decode(b, &v); e != nil {
			return nil, e
		}
		HashStrings(v, protected)
	}
	var manifestPath string
	// Resolve from the known worktree, not from supplied metadata paths.
	manifestPath = filepath.Join(filepath.Dir(filepath.Dir(Parent)), "docs/skills-3080-execution-evidence.json")
	mb, e := frozen(manifestPath, "73d277034bc76ceb71eb012ea61e3184a275192827626d6943e0b7dcd5735d9e")
	if e != nil {
		return nil, e
	}
	var manifest struct {
		Artifacts []struct {
			File string `json:"file"`
			SHA  string `json:"sha256"`
		} `json:"assessment_artifacts"`
	}
	if e = Decode(mb, &manifest); e != nil {
		return nil, e
	}
	docs := filepath.Dir(manifestPath)
	for _, a := range manifest.Artifacts {
		if filepath.Base(a.File) != a.File || !strings.HasPrefix(a.File, "skills-3080-") {
			return nil, errors.New("assessment artifact outside public evidence")
		}
		b, e := frozen(filepath.Join(docs, a.File), a.SHA)
		if e != nil {
			return nil, e
		}
		var v any
		if e = Decode(b, &v); e != nil {
			return nil, e
		}
		HashStrings(v, protected)
		pins = append(pins, Pin{Path: "docs/" + a.File, SHA: a.SHA, Bytes: int64(len(b))})
	}
	for _, name := range []string{"research153-support121-independent-proof090101.json", "research168-support108-independent-proof090101.json"} {
		b, e := os.ReadFile(filepath.Join(Parent, name))
		if e != nil {
			return nil, e
		}
		var v any
		if e = Decode(b, &v); e != nil {
			return nil, e
		}
		HashStrings(v, protected)
		pins = append(pins, Pin{Path: name, SHA: Hash(b), Bytes: int64(len(b))})
	}
	for _, pattern := range []string{"research3719-chunk-*.json", "research3719-support-*.json", "other373-chunk-*.json"} {
		names, e := filepath.Glob(filepath.Join(Parent, pattern))
		if e != nil {
			return nil, e
		}
		for _, name := range names {
			b, e := os.ReadFile(name)
			if e != nil {
				return nil, e
			}
			if len(b) > 8<<20 {
				return nil, errors.New("bounded prior structured receipt required")
			}
			var v any
			if e = Decode(b, &v); e != nil {
				return nil, e
			}
			HashStrings(v, protected)
			pins = append(pins, Pin{Path: filepath.Base(name), SHA: Hash(b), Bytes: int64(len(b))})
		}
	}
	cf, e := os.Open(filepath.Join(Corpus, "phase-a-catalog.jsonl"))
	if e != nil {
		return nil, e
	}
	defer cf.Close()
	qf, e := os.Open(filepath.Join(Parent, "execution-queue-received9-support6-214717.jsonl"))
	if e != nil {
		return nil, e
	}
	defer qf.Close()
	ch, qh := sha256.New(), sha256.New()
	cs := bufio.NewScanner(io.TeeReader(cf, ch))
	qs := bufio.NewScanner(io.TeeReader(qf, qh))
	cs.Buffer(make([]byte, 65536), 4<<20)
	qs.Buffer(make([]byte, 65536), 4<<20)
	selected := []Member{}
	unmapped := []Member{}
	n := 0
	for cs.Scan() {
		if !qs.Scan() {
			return nil, errors.New("queue/catalog length differs")
		}
		var r struct {
			Member
			Unit string `json:"inventory_unit"`
			Kind string `json:"kind"`
		}
		var q struct {
			ID    string `json:"inventory_id"`
			Stage string `json:"execution_stage"`
		}
		if e = Decode(cs.Bytes(), &r); e != nil {
			return nil, e
		}
		if e = Decode(qs.Bytes(), &q); e != nil {
			return nil, e
		}
		if r.ID != q.ID {
			return nil, errors.New("queue/catalog identity differs")
		}
		n++
		if q.Stage != "needs_semantic_and_individual_rights_review" || r.Repo != "6mile/skills" || r.Unit != "github_skill_entry" || r.Kind != "skill_entry" || !strings.EqualFold(filepath.Base(r.Path), "SKILL.md") || reserved[r.ID] {
			continue
		}
		if !digest.MatchString(r.SHA) || !blobID.MatchString(r.Blob) || r.Bytes <= 0 || r.Content != "github/6mile__skills/objects/"+r.SHA+".gz" {
			unmapped = append(unmapped, r.Member)
			continue
		}
		selected = append(selected, r.Member)
	}
	if e = errors.Join(cs.Err(), qs.Err()); e != nil {
		return nil, e
	}
	if qs.Scan() || n != 73220 || hex.EncodeToString(ch.Sum(nil)) != CatalogSHA || hex.EncodeToString(qh.Sum(nil)) != QueueSHA {
		return nil, errors.New("whole fixed catalog/queue proof differs")
	}
	plans, deferred, e := Select(selected, map[string]bool{}, protected)
	if e != nil {
		return nil, e
	}
	if e = validateTotals(plans, [2]int{25452, 25578}, [2]int{27412, 27277}, len(deferred), len(unmapped), 50, 18, 54757); e != nil {
		return nil, e
	}
	output := []map[string]any{}
	for i, p := range plans {
		b, e := json.MarshalIndent(p, "", "  ")
		if e != nil {
			return nil, e
		}
		b = append(b, '\n')
		name := fmt.Sprintf("primaryflow-cohort%d-v2-214717.json", i)
		path := filepath.Join(Parent, name)
		if _, e = os.Lstat(path); !os.IsNotExist(e) {
			return nil, errors.New("frozen plan already exists or cannot be checked")
		}
		output = append(output, map[string]any{"path": path, "sha256": Hash(b), "bytes": len(b), "groups": p.Header.TotalGroups, "occurrences": p.Header.TotalOccurrences, "buffer": b})
	}
	// Both names are checked before the first CreateNew. Summary is published
	// last, so a partial write is never a complete planning receipt.
	for _, o := range output {
		f, e := os.OpenFile(o["path"].(string), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		b := o["buffer"].([]byte)
		_, w := f.Write(b)
		s := f.Sync()
		c := f.Close()
		if e = errors.Join(w, s, c); e != nil {
			return nil, e
		}
		delete(o, "buffer")
	}
	result := map[string]any{"domain": "skills3080-primary-meaning-plans-prepared-v2", "catalog_occurrences": n, "old_reserved_occurrences": len(reserved), "metadata_protected_hashes": len(protected), "protected_metadata_pins": pins, "deferred_existing_evidence_occurrences": len(deferred), "deferred_descriptor_unmapped_occurrences": len(unmapped), "unmapped_descriptors": unmapped, "plans": output, "source_bodies_opened": 0, "new_semantic_or_copy_permission_claims": 0, "source_file_current_presence_not_checked": true, "original_v1_preserved_and_never_dispatched": true, "v2_correction": "Retain nested and case-variant SKILL.md primary occurrences, with no source-body read or earlier proof alteration.", "whole16_complete": false}
	b, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		return nil, e
	}
	b = append(b, '\n')
	f, e := os.OpenFile(filepath.Join(Parent, "primaryflow-plans-prepared-v2-214717.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	_, e = f.Write(b)
	return result, e
}

func validateTotals(plans [2]Plan, groups, occurrences [2]int, deferred, unmapped, wantDeferred, wantUnmapped, wantUniverse int) error {
	total := deferred + unmapped
	for i, p := range plans {
		n := 0
		for _, g := range p.Groups {
			n += len(g.Members)
		}
		if len(p.Groups) != groups[i] || n != occurrences[i] || p.Header.TotalGroups != groups[i] || p.Header.TotalOccurrences != occurrences[i] {
			return errors.New("fixed cohort group/occurrence counts differ before output")
		}
		total += n
	}
	if deferred != wantDeferred || unmapped != wantUnmapped || total != wantUniverse {
		return errors.New("fixed unassigned primary universe is not conserved before output")
	}
	return nil
}

func memberPage(g Group, first, count int) (any, error) {
	if first < 1 || count < 1 || count > 32 || first > len(g.Members) {
		return nil, errors.New("finite member metadata page outside group")
	}
	through := first + count - 1
	if through > len(g.Members) {
		through = len(g.Members)
	}
	b, _ := json.Marshal(g.Members)
	return map[string]any{"ordinal": g.Ordinal, "body_sha256": g.SHA, "total_members": len(g.Members), "first": first, "through": through, "next": through + 1, "members": g.Members[first-1 : through], "whole_member_metadata_sha256": Hash(b), "member_source_contexts_and_rights_pending": true, "source_bodies_opened": 0}, nil
}
func Members(path, expected string, ordinal, first, count int) (any, error) {
	p, e := Load(path, expected)
	if e != nil {
		return nil, e
	}
	if ordinal < 1 || ordinal > len(p.Groups) {
		return nil, errors.New("member group ordinal outside plan")
	}
	return memberPage(p.Groups[ordinal-1], first, count)
}

func Load(path, expected string) (Plan, error) {
	var p Plan
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(Parent) || !regexp.MustCompile(`^primaryflow-cohort[01]-v2-214717\.json$`).MatchString(filepath.Base(path)) || !digest.MatchString(expected) {
		return p, errors.New("only frozen task-owned plans allowed")
	}
	fi, e := os.Lstat(path)
	if e != nil {
		return p, e
	}
	if !fi.Mode().IsRegular() || fi.Size() > 64<<20 {
		return p, errors.New("bounded regular plan required")
	}
	b, e := frozen(path, expected)
	if e != nil {
		return p, e
	}
	if e = Decode(b, &p); e != nil {
		return p, e
	}
	if p.Header.Domain != "skills3080-primary-meaning-plan-v1" || p.Header.Catalog != CatalogSHA || p.Header.Queue != QueueSHA || p.Header.ParentJob != "20261026-214717-todo-3080" || p.Header.TotalGroups != len(p.Groups) || p.Header.Cohort < 0 || p.Header.Cohort > 1 {
		return p, errors.New("frozen plan header differs")
	}
	n := 0
	seenID := map[string]bool{}
	seenSHA := map[string]bool{}
	for i, g := range p.Groups {
		if g.Ordinal != i+1 || !digest.MatchString(g.SHA) || g.Bytes <= 0 || len(g.Members) == 0 || seenSHA[g.SHA] {
			return p, errors.New("invalid frozen primary group")
		}
		seenSHA[g.SHA] = true
		for _, m := range g.Members {
			if m.ID == "" || seenID[m.ID] || m.Repo != "6mile/skills" || m.Commit != Commit || m.SHA != g.SHA || m.Bytes != g.Bytes || !blobID.MatchString(m.Blob) || m.Blob != g.Members[0].Blob || m.Content != "github/6mile__skills/objects/"+m.SHA+".gz" || !validPrimaryPath(m.Path) {
				return p, errors.New("primary source descriptor differs")
			}
			seenID[m.ID] = true
			n++
		}
	}
	if n != p.Header.TotalOccurrences {
		return p, errors.New("occurrence conservation differs")
	}
	return p, nil
}

func Slice(path, expected string, first, count int) (any, error) {
	p, e := Load(path, expected)
	if e != nil {
		return nil, e
	}
	if first < 1 || count < 1 || count > 16 || first > len(p.Groups) {
		return nil, errors.New("finite 1..16 group window required; not a total-work cap")
	}
	end := first + count - 1
	if end > len(p.Groups) {
		end = len(p.Groups)
	}
	rows := []any{}
	for _, g := range p.Groups[first-1 : end] {
		rows = append(rows, map[string]any{"ordinal": g.Ordinal, "body_sha256": g.SHA, "body_bytes": g.Bytes, "member_occurrences": len(g.Members), "representative": g.Members[0], "member_metadata_sha256": func() string { b, _ := json.Marshal(g.Members); return Hash(b) }(), "each_member_context_and_rights_pending": true})
	}
	return map[string]any{"header": p.Header, "first": first, "through": end, "next": end + 1, "groups": rows, "source_bodies_opened": 0, "metadata_is_data_not_permission": true}, nil
}
