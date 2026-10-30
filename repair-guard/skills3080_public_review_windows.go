package main

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// The complete prelaunch specification is this typed contract, not arbitrary
// prose with a caller-supplied public flag. Its public Git inputs are proven
// independently before the raw response can be opened.
type skillsPublicReviewFile struct {
	Path  string `json:"path"`
	Blob  string `json:"blob"`
	SHA   string `json:"sha256"`
	Bytes int64  `json:"bytes"`
}
type skillsPublicReviewBaseFile struct {
	Path   string `json:"path"`
	Blob   string `json:"blob,omitempty"`
	SHA    string `json:"sha256,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Absent bool   `json:"absent,omitempty"`
}
type skillsPublicReviewComparison struct {
	Commit string                       `json:"commit"`
	Files  []skillsPublicReviewBaseFile `json:"files"`
}
type skillsPublicReviewScope struct {
	Domain              string                        `json:"domain"`
	PurposePath         string                        `json:"purpose_path,omitempty"`
	Purpose             string                        `json:"purpose,omitempty"`
	Instructions        string                        `json:"instructions"`
	Todo                int                           `json:"todo_id"`
	Repository          string                        `json:"repository"`
	RepositoryID        uint64                        `json:"repository_id"`
	Commit              string                        `json:"commit"`
	Author              skillsWriter                  `json:"author"`
	AuthorSpecPath      string                        `json:"author_spec_path"`
	ManifestPath        string                        `json:"manifest_path"`
	ManifestSHA         string                        `json:"manifest_sha256"`
	ManifestDeclaration string                        `json:"manifest_declaration"`
	Files               []skillsPublicReviewFile      `json:"files"`
	Comparison          *skillsPublicReviewComparison `json:"comparison,omitempty"`
}

const skillsPublicReviewInstructions = "固定したPUBLIC Gitのfilesとmanifest内の出力だけを読み取り専用でレビューする。別path・私的資料・追加取得・実配置・実行・ファイル保存はしない。出典と個別notice、内容、採用条件、過大な成功主張を確認し、固定commit・読了範囲・Must/Suggestion/Nitを日本語の最終返信へ返す。レビュー読了時は先頭を状態: 完了とし、全読了かつMust0の時だけ SKILLS3080_ACCEPTANCE {\"domain\":\"skills-3080-review-v1\",\"manifest_sha256\":\"manifest_sha256の値\",\"accepted\":true,\"must\":0} を独立行で返す。Mustがあればaccepted:falseと実数を返す。"
const skillsPublicReviewComparisonInstructions = skillsPublicReviewInstructions + "comparisonがある場合は、選択した同じpathの固定baseとheadだけを比較する。宣言したbaseで存在しないpathは追加として扱う。変更と必要な支持の読了範囲を区別し、変更のない範囲を全読したとは報告しない。"
const skillsReadOnlyReviewTaskHeader = "SKILLS3080_READ_ONLY_REVIEW_TASK_V1\n"

const skillsPublicRepairInstructions = "元3686の同じ担当として、宣言したPUBLIC固定Gitのfilesとmanifestに結ぶ用途限定修復だけを実装する。原3080の公開Skillと登録担当の対応、Goの正負例、Luna/maxの正式別モデルレビュー、Must修正、正確headのCI、通常merge、既存用途のWindows更新、同buffer実CLI、親への結果を完遂する。中央の再作成、未知資料の読取、一般guardの解除、元client設定、私的scope、秘密、API鍵、新費用、新しい本人許可の偽装はしない。元の実authorと現在のconsumerを分け、原job/spec/prompt/nativeと厳密な時間窓を保持する。原finalを別pathやcopyで回収しない。未達と停止理由を正確に保持し、最終返信を日本語で返す。"

var skillsPublicValidatorGo = regexp.MustCompile(`^[a-z0-9_]+\.go$`)

func skillsReadOnlyReviewTask(text string) bool {
	return strings.HasPrefix(text, skillsReadOnlyReviewTaskHeader) && strings.TrimSpace(strings.TrimPrefix(text, skillsReadOnlyReviewTaskHeader)) != ""
}

func skillsParsePublicReview(raw []byte) (skillsPublicReviewScope, bool) {
	var c skillsPublicReviewScope
	ok := skillsStrict(raw, &c) && skillsPublicPurposeEncoding(raw, false) && skillsPublicReviewShape(c)
	return c, ok
}

func skillsPublicReviewPath(path string) bool {
	validator := filepath.ToSlash(filepath.Dir(path)) == "tools/skills-3080-catalog" && (filepath.Base(path) == "go.mod" || skillsPublicValidatorGo.MatchString(filepath.Base(path)))
	return skillsRelative(path) && (validator || path == ".gitattributes" || strings.HasPrefix(path, "docs/skills-3080-") || strings.HasPrefix(path, "docs/evidence/skills-3080-")) && !ownerCardPath(path) && !ownerNonSourcePath(path)
}

func skillsPublicReviewShape(c skillsPublicReviewScope) bool {
	return skillsPublicSourceShape(c, false)
}

func skillsParsePublicRepair(raw []byte) (skillsPublicReviewScope, bool) {
	var c skillsPublicReviewScope
	ok := skillsStrict(raw, &c) && skillsPublicPurposeEncoding(raw, true) && skillsPublicSourceShape(c, true)
	return c, ok
}

func skillsPublicPurposeEncoding(raw []byte, repair bool) bool {
	v, ok := conversationJSON(raw)
	if !ok {
		return false
	}
	a, path := v["purpose_path"]
	b, body := v["purpose"]
	if !path && !body {
		return true
	}
	pa, paOK := a.(string)
	pb, pbOK := b.(string)
	return path && body && paOK && pbOK && pa != "" && pb != "" && (repair || skillsReadOnlyReviewTask(pb))
}

func skillsPublicRepairPath(path string) bool {
	return skillsPublicReviewPath(path) || (skillsRelative(path) && filepath.ToSlash(filepath.Dir(path)) == "repair-guard" && (filepath.Base(path) == "go.mod" || skillsPublicValidatorGo.MatchString(filepath.Base(path))) && !ownerCardPath(path) && !ownerNonSourcePath(path))
}

// The original, unedited issuer-continuity assignment explicitly includes this
// already-public software observation. Admit only that declared Git tuple in
// that exact purpose; do not open an official-CI namespace to other repairs or
// to ordinary Skill-content reviewers. Native author/spec/result checks remain
// in the normal original-result receiver after this shape check.
func skillsAssignedIssuerSoftwareEvidence(c skillsPublicReviewScope, f skillsPublicReviewFile) bool {
	return c.Commit == "97b17cd59381342110e1b7998cc42e1dbb067dd2" &&
		c.PurposePath == "docs/skills-3080-runtime-issuer-continuity-spec214717.md" &&
		hashBytes([]byte(c.Purpose)) == "ee7e122fd1c7c96bb135869c5e7dafed6f85f212cc24ac6fb2be8f2f1e8ac222" &&
		f.Path == "docs/evidence/official-ci3387-known-guard-baseline-f30f9e76.json" &&
		f.Blob == "473e6d7475232539273f394292580a33374543d4" && f.Bytes == 688 &&
		f.SHA == "30c63f2b20e35e0b02a99c5c63f387af95913c716680f6df10b965d76c37244e"
}

func skillsPublicSourceShape(c skillsPublicReviewScope, repair bool) bool {
	domain := "skills-3080-public-review-v1"
	pathAllowed := skillsPublicReviewPath
	wantInstructions := skillsPublicReviewInstructions
	if c.Comparison != nil {
		wantInstructions = skillsPublicReviewComparisonInstructions
	}
	if repair {
		domain, wantInstructions, pathAllowed = "skills-3080-public-repair-v1", skillsPublicRepairInstructions, skillsPublicRepairPath
		if c.Comparison != nil {
			return false
		}
	}
	// Delivery only: the complete UTF-8 body must itself be a selected public
	// Git object. Its remote publication is checked with every other input.
	// A review's briefing has a distinct read-only type marker. It cannot
	// acquire a repair purpose or a new reading namespace. Both kinds remain
	// exact selected PUBLIC blobs under the registered author's fixed scope.
	if c.Purpose != "" || c.PurposePath != "" {
		if (!repair && !skillsReadOnlyReviewTask(c.Purpose)) || !skillsPublicReviewPath(c.PurposePath) || len(c.Purpose) == 0 || len(c.Purpose) > 64<<10 {
			return false
		}
		found := false
		for _, f := range c.Files {
			if f.Path == c.PurposePath && f.Bytes == int64(len(c.Purpose)) && f.SHA == hashBytes([]byte(c.Purpose)) && f.Blob == skillsGitBlobID([]byte(c.Purpose)) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if c.Domain != domain || c.Instructions != wantInstructions || c.Todo != 3080 || c.Repository != "AutoDevJapan/cc-pc-tools-rs" || c.RepositoryID != 1406818713 || !skillsGitObjectID(c.Commit) || !skillsWriterValid(c.Author) ||
		!within(currentTodoSpecsRoot, c.AuthorSpecPath) || !strings.HasPrefix(filepath.Base(c.AuthorSpecPath), "3080-") || !currentSubworkSpecName.MatchString(filepath.Base(c.AuthorSpecPath)) ||
		!strings.HasPrefix(c.ManifestPath, "docs/skills-3080-operations/") || !strings.HasSuffix(c.ManifestPath, ".json") || !skillsDigest(c.ManifestSHA) || c.ManifestDeclaration != "SKILLS3080_MANIFEST_SHA256 "+c.ManifestSHA || len(c.Files) < 1 || len(c.Files) > 128 {
		return false
	}
	seen := map[string]bool{}
	manifest := false
	var total int64
	for _, f := range c.Files {
		key := strings.ToLower(f.Path)
		if (!pathAllowed(f.Path) && !(repair && skillsAssignedIssuerSoftwareEvidence(c, f))) || seen[key] || !skillsGitObjectID(f.Blob) || !skillsDigest(f.SHA) || f.Bytes < 1 || f.Bytes > 16<<20 {
			return false
		}
		seen[key] = true
		total += f.Bytes
		if f.Path == c.ManifestPath && f.SHA == c.ManifestSHA {
			manifest = true
		}
	}
	if comparison := c.Comparison; comparison != nil {
		if !skillsGitObjectID(comparison.Commit) || comparison.Commit == c.Commit || len(comparison.Files) != len(c.Files) {
			return false
		}
		before := map[string]bool{}
		for _, f := range comparison.Files {
			// Exact same selected spelling and namespace; no new historical path.
			found := false
			for _, head := range c.Files {
				if head.Path == f.Path {
					found = true
					break
				}
			}
			if !found || before[f.Path] {
				return false
			}
			before[f.Path] = true
			if f.Absent {
				if f.Blob != "" || f.SHA != "" || f.Bytes != 0 {
					return false
				}
			} else {
				if !skillsGitObjectID(f.Blob) || !skillsDigest(f.SHA) || f.Bytes < 1 || f.Bytes > 16<<20 {
					return false
				}
				total += f.Bytes
			}
		}
	}
	return manifest && total <= 32<<20
}

func skillsGitObjectID(s string) bool {
	return civ6Commit.MatchString(s) && s != strings.Repeat("0", 40)
}
func skillsSameWriter(a, b skillsWriter) bool {
	return a.Task == b.Task && a.Job == b.Job && a.SpecSHA == b.SpecSHA && same(a.Workspace, b.Workspace)
}

// The old specifications did not contain a typed scope. These two recoveries
// remain distinct fixed migrations. Never classify a result by parent ID alone.
var skillsPublicReview3850 = skillsResultRequest{
	Task: 3850, Job: "20261025-224324-todo-3850",
	SpecSHA:  "5a995d127dd6f7057f2daa259ebe09dc0a23065862fbe042f0dadef147c84b63",
	FinalSHA: "c1b429d61b02035b0f63843d7370eae658ac3728920ec667047e8a760b1b053e",
}

func skillsPublicReview3850Scope() skillsPublicReviewScope {
	sha := "3dd611655a517ac4734c53cdea296cd0f84070c612f0a88ef376880b262df7b5"
	return skillsPublicReviewScope{
		Domain: "skills-3080-public-review-v1", Instructions: skillsPublicReviewInstructions, Todo: 3080, Repository: "AutoDevJapan/cc-pc-tools-rs", RepositoryID: 1406818713, Commit: "3b15b7a5843c6a41dd030124fc0ce3fa4042e9f3",
		Author:         skillsWriter{Task: 3080, Job: "20261025-074042-todo-3080", SpecSHA: "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435", Workspace: skillsParentRoot},
		AuthorSpecPath: currentTodoSpecsRoot + "/3080-7536b43357044f5ca2f008826da49078.md",
		ManifestPath:   "docs/skills-3080-operations/create-webdesign-next14-074042.json", ManifestSHA: sha, ManifestDeclaration: "SKILLS3080_MANIFEST_SHA256 " + sha,
		Files: []skillsPublicReviewFile{
			{".gitattributes", "e81126a13cebc2c2ba04e9eb3951d12c620b86a7", "e38807d6a1d493b452b01a8ce174ac8f0c9052bd223c01b50041f015c98957d1", 8704},
			{"docs/evidence/skills-3080-transport-unknown074042-test.go.txt", "76ea0723e1e8fb8a71f9b38cf6e7881b797ae455", "c434df121db9f8d8e1ceb1d8cacd0e19f1910d4f1b486f3181635a2584a8cb99", 5561},
			{"docs/evidence/skills-3080-transport-unknown074042.go.txt", "6254b84db09c869176533fcb9512a478f08d5288", "88834e70b502ddc1f244af904d5830a9aeb272d6aac93207402b61384e4b27a9", 24661},
			{"docs/skills-3080-operations/create-webdesign-next14-074042.json", "d57c772e1612970fc4d564c26eb1d63e4524f727", sha, 261552},
			{"docs/skills-3080-progress.json", "c9c731bcb49d64abf501ddb03d049e8075ff959f", "0aadde105ccea9030bbefa4f2854f8b815b7da944a4a2757009dbdbd68761623", 88813},
			{"docs/skills-3080-received-evidence-acceptance.md", "cbc8a5be3b9866162dcd664ee9b5c02a74ee97fe", "56955d9b5c66bd131ff709058187c9a5785c473e83d5002c69b170860a7f2893", 22656},
			{"docs/skills-3080-webdesign-next14-inputs074042.json", "44dbbb15e9b98210009631bc6f3447625bf709ee", "a2ea5376344db1a378d9de602b637e4c87a685f25ca33f25740f6ee07bec85dd", 36796},
		},
	}
}

func skillsPublicReview3850Metadata(job object) bool {
	return job["thread_id"] == "01a1734d-b577-74f1-a98b-59c443859155" && job["launch_key"] == "754b597004ae4c069377c901e313170b" &&
		same(str(job["cwd"]), "D:/CC/Work/unattended-operations-20261013-wt/todo-3850-14-skills") && job["parent_job_id"] == "20261025-074042-todo-3080" &&
		job["started_at"] == "2026-10-25T22:43:53+09:00" && job["finished_at"] == "2026-10-25T22:56:14+09:00" &&
		skillsModel(str(job["model"])) == "gpt-6-luna" && job["effort"] == "max" && job["model_origin"] == "owner_request" && job["effort_origin"] == "owner_request"
}

func skillsPublicReviewScopeFor(r skillsResultRequest, spec []byte) (skillsPublicReviewScope, bool) {
	if r == skillsPublicReview3850 {
		return skillsPublicReview3850Scope(), true
	}
	return skillsParsePublicReview(spec)
}

func skillsPublicReviewCandidate(r skillsResultRequest, reg skillsRegistry, spec []byte) bool {
	c, ok := skillsPublicReviewScopeFor(r, spec)
	row := reg.Tasks[r.Task]
	return ok && skillsDigest(r.FinalSHA) && reg.writer.Task == 3080 && row.Subwork.Parent == 3080 && row.Subwork.ParentJob == c.Author.Job && same(row.Subwork.Owner.Worktree, c.Author.Workspace)
}

// GitHub identity + immutable commit/tree prove publication. Local object bytes
// must match that remote tree's blob ID, their SHA256 and size. No merge check:
// raw read-only review is useful before merge; creation still requires main.
func skillsPublicReviewInputs(c skillsPublicReviewScope, root string, s *skillsFS) bool {
	return skillsPublicReviewInputsWith(c, s, skillsGitHub, func(id string) ([]byte, bool) {
		b, e := permanentGitAt(root, "cat-file", "blob", id)
		return b, e == nil && len(b) <= 16<<20
	})
}

func skillsPublicReviewInputsWith(c skillsPublicReviewScope, s *skillsFS, gh func(...string) ([]byte, bool), blob func(string) ([]byte, bool)) bool {
	return skillsPublicInputsWith(c, s, gh, blob, false)
}

func skillsPublicRepairInputs(c skillsPublicReviewScope, root string, s *skillsFS) bool {
	return skillsPublicInputsWith(c, s, skillsGitHub, func(id string) ([]byte, bool) {
		b, e := permanentGitAt(root, "cat-file", "blob", id)
		return b, e == nil && len(b) <= 16<<20
	}, true)
}

// This software-only assignment deliberately retains the earlier travel
// manifest as a public scope link, not a new creation request. Preserve its
// original writer while proving the exact newly assigned repair author and
// public purpose. This is not an alias for arbitrary older manifests/reviews.
// All remote Git tuples, local bytes and native author/result proofs still run.
func skillsAssignedNativeUpdateManifest(c skillsPublicReviewScope, f skillsPublicReviewFile, m skillsManifest) bool {
	return c.Domain == "skills-3080-public-repair-v1" &&
		c.Commit == "6fdbdeee3933ca5aa124747a13c70fb94414ff27" &&
		c.PurposePath == "docs/skills-3080-native-update609-task175645.md" &&
		hashBytes([]byte(c.Purpose)) == "3f3521cc55b7e8037a9cf53922452e0fe1cfa0b3a50b9e0a6104ace698a5bf4e" &&
		c.ManifestPath == "docs/skills-3080-operations/create-travel3-214717.json" &&
		c.ManifestSHA == "11070b772c86f7ffb459be2f75eaf6d0a436ce9dfdf0d9247eb81c6e433ede51" &&
		f.Path == c.ManifestPath && f.SHA == c.ManifestSHA && f.Blob == "c7db3ae987ac5e4a2dfc15de98f27b2a3cd226fb" && f.Bytes == 55032 &&
		skillsSameWriter(c.Author, skillsWriter{3080, "20261027-175645-todo-3080", "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435", skillsParentRoot}) &&
		skillsSameWriter(m.Writer, skillsWriter{3080, "20261026-214717-todo-3080", "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435", skillsParentRoot})
}

func skillsPublicInputsWith(c skillsPublicReviewScope, s *skillsFS, gh func(...string) ([]byte, bool), blob func(string) ([]byte, bool), repair bool) bool {
	if !skillsPublicSourceShape(c, repair) {
		return false
	}
	base := "repos/AutoDevJapan/cc-pc-tools-rs"
	get := func(path string) (object, bool) {
		b, ok := gh("api", "--hostname", "github.com", path)
		if !ok {
			return nil, false
		}
		return conversationJSON(b)
	}
	repo, ok := get(base)
	if !ok || conversationNumber(repo["id"]) != c.RepositoryID || repo["full_name"] != c.Repository || repo["private"] != false || obj(repo["owner"])["login"] != "AutoDevJapan" {
		return false
	}
	getTree := func(id string) (map[string]object, bool) {
		commit, ok := get(base + "/git/commits/" + id)
		treeID := str(obj(commit["tree"])["sha"])
		if !ok || commit["sha"] != id || !skillsGitObjectID(treeID) {
			return nil, false
		}
		tree, ok := get(base + "/git/trees/" + treeID + "?recursive=1")
		if !ok || tree["sha"] != treeID || tree["truncated"] != false {
			return nil, false
		}
		entries, ok := tree["tree"].([]any)
		if !ok {
			return nil, false
		}
		paths := map[string]object{}
		for _, v := range entries {
			e := obj(v)
			p := str(e["path"])
			if _, dup := paths[p]; dup {
				return nil, false
			}
			paths[p] = e
		}
		return paths, true
	}
	paths, ok := getTree(c.Commit)
	if !ok {
		return false
	}
	var before map[string]object
	if comparison := c.Comparison; comparison != nil {
		// Project only ancestry metadata, never unselected patch bodies.
		b, ok := gh("api", "--hostname", "github.com", base+"/compare/"+comparison.Commit+"..."+c.Commit, "--jq", "{status,base:.base_commit.sha,merge_base:.merge_base_commit.sha}")
		a, valid := conversationJSON(b)
		if !ok || !valid || a["status"] != "ahead" || a["base"] != comparison.Commit || a["merge_base"] != comparison.Commit {
			return false
		}
		before, ok = getTree(comparison.Commit)
		if !ok {
			return false
		}
	}
	check := func(f skillsPublicReviewFile, paths map[string]object, manifest bool) bool {
		if s.blocked(filepath.Join(c.Author.Workspace, filepath.FromSlash(f.Path))) {
			return false
		}
		e := paths[f.Path]
		if e["type"] != "blob" || (e["mode"] != "100644" && e["mode"] != "100755") || e["sha"] != f.Blob || conversationNumber(e["size"]) != uint64(f.Bytes) {
			return false
		}
		for p := filepath.ToSlash(filepath.Dir(f.Path)); p != "."; p = filepath.ToSlash(filepath.Dir(p)) {
			if a := paths[p]; a["type"] != "tree" || a["mode"] != "040000" {
				return false
			}
		}
		b, ok := blob(f.Blob)
		if !ok || int64(len(b)) != f.Bytes || hashBytes(b) != f.SHA || skillsGitBlobID(b) != f.Blob {
			return false
		}
		if manifest && f.Path == c.ManifestPath {
			m, ok := skillsParseManifest(b)
			if !ok || (!m.publicV2 && !skillsSameWriter(m.Writer, c.Author) && !(repair && skillsAssignedNativeUpdateManifest(c, f, m))) {
				return false
			}
		}
		return true
	}
	for _, f := range c.Files {
		if !check(f, paths, true) {
			return false
		}
	}
	if comparison := c.Comparison; comparison != nil {
		for _, f := range comparison.Files {
			if f.Absent {
				if _, exists := before[f.Path]; exists {
					return false
				}
				// Absence under a symlink/blob is not a proven ordinary absent path.
				for p := filepath.ToSlash(filepath.Dir(f.Path)); p != "."; p = filepath.ToSlash(filepath.Dir(p)) {
					if a, exists := before[p]; exists && (a["type"] != "tree" || a["mode"] != "040000") {
						return false
					}
				}
				continue
			}
			if !check(skillsPublicReviewFile{f.Path, f.Blob, f.SHA, f.Bytes}, before, false) {
				return false
			}
		}
	}
	// A fresh runtime may load newer operating instructions than its resumed
	// author. Bind the project part to this same PUBLIC commit, not to whatever
	// text the reviewer calls AGENTS.md. No additional task input is admitted.
	{
		e := paths["AGENTS.md"]
		if e["type"] == "blob" && (e["mode"] == "100644" || e["mode"] == "100755") && conversationNumber(e["size"]) <= 256<<10 {
			b, valid := blob(str(e["sha"]))
			if valid && len(b) > 0 && uint64(len(b)) == conversationNumber(e["size"]) && skillsGitBlobID(b) == e["sha"] {
				if s.bootstrapProjects == nil {
					s.bootstrapProjects = map[string][]byte{}
				}
				s.bootstrapProjects[c.Commit] = b
			}
		}
	}
	return true
}

func skillsGitBlobID(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d%c", len(b), byte(0))
	h.Write(b)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func skillsReviewLaunchPromptAt(s *skillsFS, root string, job object, spec []byte) ([]byte, bool) {
	id, key := str(job["id"]), str(job["launch_key"])
	if !currentSubworkJobName.MatchString(id) || len(key) < 16 || len(key) > 64 || strings.Trim(key, "0123456789abcdef") != "" {
		return nil, false
	}
	prompt, ok := s.read(filepath.Join(root, id, "prompt.md"), 4<<20)
	start := bytes.Index(prompt, []byte("\n依頼:\n"))
	if !ok || start < 0 || !bytes.HasPrefix(prompt[start+len("\n依頼:\n"):], spec) {
		return nil, false
	}
	raw, ok := s.read(filepath.Join(root, ".launch-"+key+".json"), 1<<20)
	var receipt struct {
		ID string `json:"id"`
	}
	if !ok || !skillsStrict(raw, &receipt) || receipt.ID != id {
		return nil, false
	}
	return prompt, true
}

func skillsPublicReviewAuthor(c skillsPublicReviewScope, reg skillsRegistry, row skillsTask, child object, s *skillsFS) bool {
	parent := reg.Tasks[3080]
	if c.Author.Job != row.Subwork.ParentJob || !skillsRegisteredGeneration(parent, c.Author.Job, reg.writer.Job) || !same(c.Author.Workspace, parent.Scheduler.Worktree) {
		return false
	}
	author, ok := skillsReadJob(s, c.Author.Job)
	if !ok || !skillsReviewOriginBound(parent, row, child, author) {
		return false
	}
	spec, ok := s.read(c.AuthorSpecPath, 1<<20)
	if !ok || hashBytes(spec) != c.Author.SpecSHA {
		return false
	}
	_, ok = skillsReviewLaunchPromptAt(s, skillsJobsRoot, author, spec)
	return ok
}

// Keep metadata/final/prompt/native on the real admitted child. The consumer is
// validated separately and never impersonates the historical author.
func skillsPublicReviewNative(s *skillsFS, job object, spec []byte, finalSHA string) ([]object, bool) {
	return skillsReviewNative(s, job, spec, finalSHA)
}

func skillsCodeReviewNative(s *skillsFS, job object, spec []byte, finalSHA string) ([]object, bool) {
	return skillsReviewNative(s, job, spec, finalSHA)
}

func skillsReviewNative(s *skillsFS, job object, spec []byte, finalSHA string) ([]object, bool) {
	if !skillsDigest(finalSHA) {
		return nil, false
	}
	prompt, ok := skillsReviewLaunchPromptAt(s, skillsJobsRoot, job, spec)
	if !ok {
		return nil, false
	}
	_, reviewTyped := skillsParsePublicReview(spec)
	consumer, consumerTyped := skillsParseConsumer(spec)
	bootstrap := ""
	if reviewTyped || consumerTyped {
		prompt, ok = skillsTypedLaunchInputAt(s, skillsJobsRoot, job, spec)
		if !ok {
			return nil, false
		}
		author := consumer.Author
		if reviewTyped {
			scope, _ := skillsParsePublicReview(spec)
			author = scope.Author
		}
		origin, valid := skillsReadJob(s, author.Job)
		if !valid {
			return nil, false
		}
		bootstrap, ok = skillsBootstrapForJob(s, origin)
		if !ok {
			return nil, false
		}
	}
	project, required := skillsTypedRuntimeProject(s, job, spec)
	return skillsCompletedReviewNativeWithBootstrap(s, job, prompt, finalSHA, reviewTyped || consumerTyped, bootstrap, project, required)
}

// Every completed review/consumer result uses one deny-write proof engine.
// Input/source/author validation above remains specific to its registered type;
// live creation and resumed historical results retain their separate contract.
func skillsCompletedReviewNative(s *skillsFS, job object, prompt []byte, finalSHA string, exactInput bool, bootstrap string) ([]object, bool) {
	return skillsCompletedReviewNativeWithBootstrap(s, job, prompt, finalSHA, exactInput, bootstrap, nil, false)
}

func skillsCompletedReviewNativeWithBootstrap(s *skillsFS, job object, prompt []byte, finalSHA string, exactInput bool, bootstrap string, source []byte, required bool) ([]object, bool) {
	native, ok := skillsHistoricalNative(s, job, &skillsNativeResultProof{prompt: prompt, finalSHA: finalSHA, exactInput: exactInput, bootstrapSHA: bootstrap, runtimeProject: source, requireRuntimeSource: required, cwd: str(job["cwd"]), sandbox: str(job["sandbox"]), frozen: true})
	if !ok {
		return nil, false
	}
	for _, n := range native {
		if skillsModel(str(n["model"])) != skillsModel(str(job["model"])) || n["effort"] != job["effort"] {
			return nil, false
		}
	}
	return native, true
}
