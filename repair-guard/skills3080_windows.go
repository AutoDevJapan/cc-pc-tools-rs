package main

// Purpose-qualified operations never participate in allowed/classifyOwnerPath.
// The manifest is data, not an owner request or a replacement exclusion list.
import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const skillsParentRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3080-16-skills-3048"
const skillsCentralRoot = "D:/CC/Skills"
const skillsDecisionSHA = "32b8d0d676cb58e82ef256fad0a7003ab62d3047db2ca0c71676f09fdac93fe4"
const skillsScopePath = skillsParentRoot + "/.ci-work/todo3080/qualification-owner-reply235901.private.json"
const skillsScopeSHA = "82a53e380f38f421324b86b4aa088777cd14b9bf0237e64f227d381b39458519"
const skillsSourceReceiptSHA = "414b763df928baadd5e71aff34665163adc724ba05eee74a86245f8cd888e4fa"

type skillsWriter struct {
	Task      int    `json:"task"`
	Job       string `json:"job"`
	SpecSHA   string `json:"spec_sha256"`
	Workspace string `json:"workspace"`
}
type skillsLicense struct {
	Path     string `json:"path"`
	SHA      string `json:"sha256"`
	Scope    string `json:"scope"`
	Accepted bool   `json:"accepted"`
}
type skillsSource struct {
	Repository  string        `json:"repository"`
	Commit      string        `json:"commit"`
	Path        string        `json:"path"`
	SHA         string        `json:"sha256"`
	Acquisition string        `json:"acquisition"`
	License     skillsLicense `json:"license"`
}
type skillsOutput struct {
	Target  string `json:"target"`
	SHA     string `json:"sha256"`
	Content string `json:"content_base64"`
	body    []byte
}
type skillsManifest struct {
	Domain   string         `json:"domain"`
	Todo     int            `json:"todo_id"`
	Decision string         `json:"decision_identity_sha256"`
	Writer   skillsWriter   `json:"writer"`
	Sources  []skillsSource `json:"sources"`
	Files    []skillsOutput `json:"files"`
}
type skillsResultRequest struct {
	Task     int    `json:"task"`
	Job      string `json:"job"`
	SpecSHA  string `json:"spec_sha256"`
	FinalSHA string `json:"final_sha256,omitempty"`
	SpecPath string `json:"spec_path,omitempty"`
}
type skillsRequest struct {
	Domain         string                 `json:"domain"`
	Writer         skillsWriter           `json:"writer"`
	ManifestCommit string                 `json:"manifest_commit,omitempty"`
	ManifestPath   string                 `json:"manifest_path,omitempty"`
	ManifestSHA    string                 `json:"manifest_sha256,omitempty"`
	Review         *skillsResultRequest   `json:"review,omitempty"`
	Result         *skillsResultRequest   `json:"result,omitempty"`
	Runtime        *skillsRuntimeRequest  `json:"runtime,omitempty"`
	Consume        *skillsConsumeRequest  `json:"consume,omitempty"`
	Revision       *skillsRevisionRequest `json:"revision,omitempty"`
}

var skillsName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var skillsPublicRepository = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func skillsDigest(s string) bool { return modelDigestValid(s) && s != strings.Repeat("0", 64) }
func skillsRelative(s string) bool {
	if s == "" || strings.ContainsAny(s, `\:`) || strings.HasPrefix(s, "/") || strings.ContainsAny(s, "\x00\r\n") {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		device := strings.ToUpper(strings.Split(p, ".")[0])
		reserved := device == "CON" || device == "PRN" || device == "AUX" || device == "NUL" || (len(device) == 4 && (strings.HasPrefix(device, "COM") || strings.HasPrefix(device, "LPT")) && device[3] >= '1' && device[3] <= '9')
		if p == "" || p == "." || p == ".." || strings.TrimRight(p, " .") != p || strings.ContainsAny(p, `<>"|?*`) || reserved {
			return false
		}
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(s))) == s
}
func skillsStrict(data []byte, v any) bool {
	if len(data) == 0 || len(data) > 16<<20 || !utf8.Valid(data) {
		return false
	}
	tree, ok := conversationJSON(data)
	if !ok || !skillsCanonicalJSON(tree, v) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(v) == nil
}
func skillsWriterValid(w skillsWriter) bool {
	m := currentSubworkJobName.FindStringSubmatch(w.Job)
	return w.Task == 3080 && len(m) == 3 && m[2] == "3080" && skillsDigest(w.SpecSHA) && same(w.Workspace, skillsParentRoot)
}
func skillsParseManifest(raw []byte) (skillsManifest, bool) {
	var m skillsManifest
	if !skillsStrict(raw, &m) || m.Domain != "skills-3080-create-v1" || m.Todo != 3080 || m.Decision != skillsDecisionSHA || !skillsWriterValid(m.Writer) || len(m.Sources) < 1 || len(m.Sources) > 128 || len(m.Files) < 1 || len(m.Files) > 1024 {
		return m, false
	}
	for _, s := range m.Sources {
		if !skillsPublicRepository.MatchString(s.Repository) || !civ6Commit.MatchString(s.Commit) || s.Commit == strings.Repeat("0", 40) || !skillsRelative(s.Path) || !skillsDigest(s.SHA) || s.Acquisition != "already-acquired" || !skillsRelative(s.License.Path) || !skillsDigest(s.License.SHA) || !s.License.Accepted || strings.TrimSpace(s.License.Scope) == "" || ownerCardPath(s.Path) || ownerNonSourcePath(s.Path) {
			return m, false
		}
	}
	seen := map[string]bool{}
	entries := map[string]bool{}
	names := map[string]bool{}
	for i, f := range m.Files {
		parts := strings.Split(f.Target, "/")
		if len(parts) < 2 || !skillsName.MatchString(parts[0]) || len(parts[0]) > 80 || !skillsRelative(f.Target) || ownerCardPath(f.Target) || ownerNonSourcePath(f.Target) || seen[strings.ToLower(f.Target)] || !skillsDigest(f.SHA) {
			return m, false
		}
		body, e := base64.StdEncoding.Strict().DecodeString(f.Content)
		if e != nil || len(body) < 1 || len(body) > 2<<20 || !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 || hashBytes(body) != f.SHA {
			return m, false
		}
		seen[strings.ToLower(f.Target)] = true
		names[parts[0]] = true
		if len(parts) == 2 && parts[1] == "SKILL.md" {
			entries[parts[0]] = true
		}
		m.Files[i].body = body
	}
	for name := range names {
		if !entries[name] {
			return m, false
		}
	}
	// A file cannot also be a directory of another output, under Win32 casing.
	for path := range seen {
		for p := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path))); p != "."; p = filepath.ToSlash(filepath.Dir(filepath.FromSlash(p))) {
			if seen[p] {
				return m, false
			}
		}
	}
	return m, true
}

func skillsFlag(args []string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		for _, f := range []string{"qualify-skills3080", "create-skills3080", "create-skills3080-forward", "consume-skills3080", "consume-skills3080-admitted", "receive-skills3080-result", "verify-skills3080-update", "qualify-skills3080-revision", "prepare-skills3080-revision", "apply-skills3080-revision", "consume-skills3080-revision"} {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}
func runSkillsCLI(args []string, out io.Writer, run func(string, string) object) int {
	result := object{"qualified": false, "reason": "arguments"}
	if len(args) == 1 && args[0] == "--consume-skills3080-admitted" {
		result = run("consume-skills3080-admitted", "")
	}
	if len(args) == 2 && filepath.IsAbs(args[1]) {
		op := strings.TrimPrefix(strings.TrimPrefix(args[0], "-"), "-")
		if args[0] == "-"+op || args[0] == "--"+op {
			switch op {
			case "qualify-skills3080", "create-skills3080", "create-skills3080-forward", "consume-skills3080", "receive-skills3080-result", "verify-skills3080-update", "qualify-skills3080-revision", "prepare-skills3080-revision", "apply-skills3080-revision", "consume-skills3080-revision":
				result = run(op, args[1])
			}
		}
	}
	// Never leak callback error text, names, paths or arbitrary metadata on refusal.
	if result["qualified"] != true {
		created, partial := result["created_files"], result["partial_operation_possible"]
		stage, _ := result["failure_stage"].(string)
		reason := str(result["reason"])
		if !skillsReason.MatchString(reason) {
			reason = "unqualified"
		}
		result = object{"qualified": false, "reason": reason}
		if skillsFixedFailureStage(stage) {
			result["failure_stage"] = stage
		}
		if n, ok := created.(int); ok && n >= 0 && n <= 1024 && partial == true {
			result["created_files"] = n
			result["partial_operation_possible"] = true
		}
	}
	if json.NewEncoder(out).Encode(result) != nil || result["qualified"] != true {
		return 1
	}
	return 0
}

var skillsReason = regexp.MustCompile(`^(arguments|request|scope|writer|manifest|review|source|existing_target|physical_parent|protection|result_binding|result_unavailable|native_context|creation_evidence|current_identity|current_bytes|create_failed|unqualified)$`)

func skillsFixedFailureStage(stage string) bool {
	switch stage {
	case "creation_request", "installed_producer", "creation_native", "creation_header", "created_manifest", "manifest_shape",
		"source_provenance", "source_claims", "source_catalog_open", "source_catalog_integrity", "source_anchor", "source_commit",
		"source_index_open", "source_index_integrity", "source_selected_paths", "source_notice_or_catalog", "source_support_package",
		"source_tree", "source_object", "creation_binding", "current_snapshot", "writer_recheck":
		return true
	}
	return false
}

func skillsScopeBytes(raw []byte) bool {
	if hashBytes(raw) != skillsScopeSHA {
		return false
	}
	m, ok := conversationJSON(raw)
	if !ok || m["domain"] != "new-owner-recovery-messages" || m["source"] != "discord-api-v10" || m["owner_id"] != "730822776147148835" || m["thread_id"] != "1563573286858784859" || conversationCanonical(m["decision_identity"]) != skillsDecisionSHA {
		return false
	}
	replies, ok := m["owner_replies"].([]any)
	if !ok || len(replies) != 1 {
		return false
	}
	r := obj(replies[0])
	inspection := obj(m["inspection"])
	versions, _ := inspection["owner_reply_versions"].([]any)
	return r["author_id"] == m["owner_id"] && r["message_id"] == "1563573380836491305" && r["body"] == "<@1554269962229649478> a" && r["timestamp"] == "2026-10-24T15:22:23.780000+00:00" && len(versions) == 1 && obj(versions[0])["source_receipt_sha256"] == skillsSourceReceiptSHA
}
