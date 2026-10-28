package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha1" // Git object identity; the independent SHA-256 is also required.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const skillsCorpusRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3080-skills-inventory/.ci-work/todo3080"
const skillsCatalogSHA = "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c"

type skillsSourceAnchor struct {
	IndexSHA, TreeSHA, CommitSHA string
	FlatTree                     bool
}

type skillsSavedSource struct {
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	Path    string `json:"path"`
	Role    string `json:"role"`
	Blob    string `json:"git_blob"`
	SHA     string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Content string `json:"content_path"`
}

// The acquired index's historical classification is immutable. This one
// package's individual Apache notice was acquired as support_text; recognize
// its actual notice role only at the complete fixed object identity. The
// original catalog/index/tree/commit pins and retained object bytes still run.
func skillsSavedNotice(row skillsSavedSource) bool {
	if row.Role == "license_or_notice" {
		return true
	}
	return row == (skillsSavedSource{
		Repo: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d",
		Path: "skills/skywyze/rlm-controller/LICENCE.md", Role: "support_text",
		Bytes: 10752, SHA: "120701352845f72eda11433beb40df387026e41c5a9c6b9c9cef0c6c624f6004",
		Blob:    "0093542cc130c66e50a69f86839a5989484a6db3",
		Content: "objects/120701352845f72eda11433beb40df387026e41c5a9c6b9c9cef0c6c624f6004.gz",
	})
}

// Checksum and select from the same bounded stream. No selected metadata is
// trusted until EOF and the independent whole-file digest have both matched.
func skillsSourceLines(input io.Reader, expected string, selectRow func([]byte) bool) bool {
	if !skillsDigest(expected) {
		return false
	}
	h := sha256.New()
	scan := bufio.NewScanner(io.TeeReader(input, h))
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	for scan.Scan() {
		if !selectRow(scan.Bytes()) {
			return false
		}
	}
	return scan.Err() == nil && hex.EncodeToString(h.Sum(nil)) == expected
}

func skillsSourceKey(repo, commit, path string) string { return repo + "\x00" + commit + "\x00" + path }

func skillsSourcesBound(m skillsManifest, fs *skillsFS) bool {
	return skillsSourcesBoundAt(m, fs, skillsCorpusRoot, skillsCatalogSHA, skillsSourceAnchors)
}

// The alternative root/anchors are a unit-test seam, never request fields or a
// new CLI. Production always supplies the original fixed acquired collection.
func skillsSourcesBoundAt(m skillsManifest, fs *skillsFS, corpus, catalogSHA string, anchors map[string]skillsSourceAnchor) bool {
	fs.failureStage = "source_claims"
	if !skillsSourceClaimsPreserved(m) || len(m.Sources) == 0 {
		return false
	}
	bindings, bindingsOK := skillsMaskedBindings(m)
	if !bindingsOK {
		return false
	}
	wanted := map[string]skillsSource{}
	for _, source := range m.Sources {
		key := skillsSourceKey(source.Repository, source.Commit, source.Path)
		if prior, exists := wanted[key]; exists && prior != source {
			return false
		}
		wanted[key] = source
	}
	fs.failureStage = "source_catalog_open"
	catalog, ok := fs.open(filepath.Join(corpus, "phase-a-catalog.jsonl"), 256<<20)
	if !ok {
		return false
	}
	found := map[string]skillsSavedSource{}
	fs.failureStage = "source_catalog_integrity"
	ok = skillsSourceLines(catalog, catalogSHA, func(raw []byte) bool {
		var row struct {
			skillsSavedSource
			Read bool   `json:"machine_body_read"`
			Unit string `json:"inventory_unit"`
		}
		if json.Unmarshal(raw, &row) != nil {
			return false
		}
		key := skillsSourceKey(row.Repo, row.Commit, row.Path)
		source, selected := wanted[key]
		if !selected {
			return true
		}
		if !row.Read || row.Unit == "unread_additional_text_support_shape" || row.SHA != source.SHA || row.Bytes < 1 || row.Bytes > 2<<20 {
			return false
		}
		if previous, exists := found[key]; exists && previous != row.skillsSavedSource {
			return false
		}
		found[key] = row.skillsSavedSource
		return true
	})
	if !ok {
		return false
	}
	byRepo := map[string][]skillsSource{}
	for _, source := range wanted {
		byRepo[source.Repository] = append(byRepo[source.Repository], source)
	}
	for repo, sources := range byRepo {
		fs.failureStage = "source_anchor"
		anchor, exists := anchors[repo]
		if !exists || !skillsDigest(anchor.IndexSHA) || !skillsDigest(anchor.TreeSHA) || !skillsDigest(anchor.CommitSHA) {
			return false
		}
		root := filepath.Join(corpus, "github", strings.ReplaceAll(repo, "/", "__"))
		fs.failureStage = "source_commit"
		commit, ok := fs.read(filepath.Join(root, "commit.json"), 1<<20)
		value, parsed := conversationJSON(commit)
		treeSHA := str(obj(obj(value["commit"])["tree"])["sha"])
		if !ok || hashBytes(commit) != anchor.CommitSHA || !parsed || value["sha"] != skillsPublicSources[repo] || !civ6Commit.MatchString(treeSHA) {
			return false
		}
		paths := map[string]bool{}
		missing := []skillsSource{}
		for _, source := range sources {
			paths[source.Path], paths[source.License.Path] = true, true
			if _, candidate := found[skillsSourceKey(repo, source.Commit, source.Path)]; !candidate {
				missing = append(missing, source)
			}
		}
		fs.failureStage = "source_index_open"
		index, ok := fs.open(filepath.Join(root, "content-index.jsonl"), 128<<20)
		if !ok {
			return false
		}
		selected := map[string]skillsSavedSource{}
		fs.failureStage = "source_index_integrity"
		ok = skillsSourceLines(index, anchor.IndexSHA, func(raw []byte) bool {
			var row skillsSavedSource
			if json.Unmarshal(raw, &row) != nil {
				return false
			}
			applicable := false
			for _, support := range missing {
				applicable = applicable || (skillsSavedNotice(row) && skillsSupportNotice(row.Path, support.Path))
			}
			if !paths[row.Path] && !applicable {
				return true
			}
			if _, duplicate := selected[row.Path]; duplicate || row.Repo != repo || row.Commit != skillsPublicSources[repo] || !skillsDigest(row.SHA) || !civ6Commit.MatchString(row.Blob) || row.Bytes < 1 || row.Bytes > 2<<20 || row.Content != "objects/"+row.SHA+".gz" {
				return false
			}
			selected[row.Path] = row
			return true
		})
		if !ok {
			return false
		}
		fs.failureStage = "source_selected_paths"
		for p := range paths {
			if _, exists := selected[p]; !exists {
				return false
			}
		}
		// First validate actual catalog candidates. A support row cannot make
		// itself a primary merely by claiming acquisition or a favorable role.
		primaries := map[string]skillsSource{}
		fs.failureStage = "source_notice_or_catalog"
		for _, source := range sources {
			original, notice := selected[source.Path], selected[source.License.Path]
			if original.SHA != source.SHA || notice.SHA != source.License.SHA || !skillsSavedNotice(notice) {
				return false
			}
			if cat, candidate := found[skillsSourceKey(repo, source.Commit, source.Path)]; candidate {
				if original.Blob != cat.Blob || original.Bytes != cat.Bytes || cat.Content != "github/"+strings.ReplaceAll(repo, "/", "__")+"/"+original.Content {
					return false
				}
				if original.Role == "skill_entry" && path.Base(source.Path) == "SKILL.md" {
					primaries[source.Path] = source
				}
			}
		}
		packages := map[string]string{}
		fs.failureStage = "source_support_package"
		for _, support := range missing {
			if selected[support.Path].Role != "support_text" || !strings.HasSuffix(support.Path, ".md") {
				return false
			}
			primary := ""
			for p, candidate := range primaries {
				if skillsSupportUnder(path.Dir(p), support.Path) && p != support.Path &&
					candidate.License.Path == support.License.Path && candidate.License.SHA == support.License.SHA && len(p) > len(primary) {
					primary = p
				}
			}
			if primary == "" {
				return false
			}
			packages[support.Path] = primary
		}
		fs.failureStage = "source_tree"
		if !skillsSourceTree(root, anchor, treeSHA, selected, packages, fs) {
			return false
		}
		// The complete tree may contain additional applicable root/package
		// notices. They too must be acquired and retained byte-for-byte.
		for _, retained := range selected {
			fs.failureStage = "source_object"
			if !skillsSourceObject(root, retained, m.Files, fs, bindings) {
				return false
			}
		}
	}
	fs.failureStage = ""
	return true
}

func skillsSupportUnder(dir, file string) bool { return dir == "." || strings.HasPrefix(file, dir+"/") }

func skillsSupportNotice(notice, source string) bool {
	base := strings.ToUpper(path.Base(notice))
	for _, prefix := range []string{"LICENSE", "LICENCE", "NOTICE", "COPYING", "COPYRIGHT"} {
		if strings.HasPrefix(base, prefix) && skillsSupportUnder(path.Dir(notice), source) {
			return true
		}
	}
	return false
}

func skillsSourceTree(root string, a skillsSourceAnchor, rootSHA string, selected map[string]skillsSavedSource, packages map[string]string, fs *skillsFS) bool {
	name := "tree-recursive.json"
	if a.FlatTree {
		name = "tree-complete.json"
	}
	f, ok := fs.open(filepath.Join(root, name), 128<<20)
	if !ok {
		return false
	}
	h := sha256.New()
	d := json.NewDecoder(io.TeeReader(f, h))
	seen := map[string]bool{}
	readEntries := func() bool {
		if token, e := d.Token(); e != nil || token != json.Delim('[') {
			return false
		}
		for d.More() {
			var entry struct{ Path, SHA, Type, Mode string }
			if d.Decode(&entry) != nil {
				return false
			}
			for support, primary := range packages {
				// An unselected nested SKILL.md is a different package, even if
				// an ancestor primary was selected in the same repository.
				if path.Base(entry.Path) == "SKILL.md" && entry.Path != primary &&
					skillsSupportUnder(path.Dir(primary), entry.Path) && skillsSupportUnder(path.Dir(entry.Path), support) {
					return false
				}
				if skillsSupportNotice(entry.Path, support) && entry.Type != "tree" {
					row, retained := selected[entry.Path]
					if !retained || !skillsSavedNotice(row) {
						return false
					}
				}
			}
			if row, exists := selected[entry.Path]; exists {
				if seen[entry.Path] || entry.SHA != row.Blob || entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
					return false
				}
				seen[entry.Path] = true
			}
		}
		token, e := d.Token()
		return e == nil && token == json.Delim(']')
	}
	if a.FlatTree {
		if !readEntries() {
			return false
		}
	} else {
		if token, e := d.Token(); e != nil || token != json.Delim('{') {
			return false
		}
		keys := map[string]bool{}
		sha, complete := "", false
		for d.More() {
			token, e := d.Token()
			key, isString := token.(string)
			if e != nil || !isString || keys[key] {
				return false
			}
			keys[key] = true
			switch key {
			case "tree":
				if !readEntries() {
					return false
				}
			case "sha":
				if d.Decode(&sha) != nil {
					return false
				}
			case "truncated":
				var truncated *bool
				if d.Decode(&truncated) != nil || truncated == nil {
					return false
				}
				complete = !*truncated
			default:
				var skip ownerDiscardJSON
				if d.Decode(&skip) != nil {
					return false
				}
			}
		}
		if token, e := d.Token(); e != nil || token != json.Delim('}') || sha != rootSHA || !complete || !keys["tree"] {
			return false
		}
	}
	var extra any
	return d.Decode(&extra) == io.EOF && hex.EncodeToString(h.Sum(nil)) == a.TreeSHA && len(seen) == len(selected)
}

func skillsSourceObject(root string, row skillsSavedSource, outputs []skillsOutput, fs *skillsFS, bindings ...map[string]string) bool {
	compressed, ok := fs.open(filepath.Join(root, filepath.FromSlash(row.Content)), 4<<20)
	if !ok {
		return false
	}
	z, e := gzip.NewReader(compressed)
	if e != nil {
		return false
	}
	raw, e := io.ReadAll(io.LimitReader(z, (2<<20)+1))
	closed := z.Close()
	if e != nil || closed != nil || int64(len(raw)) != row.Bytes || hashBytes(raw) != row.SHA {
		return false
	}
	blob := sha1.New()
	_, _ = io.WriteString(blob, "blob "+strconv.Itoa(len(raw))+"\x00")
	_, _ = blob.Write(raw)
	if hex.EncodeToString(blob.Sum(nil)) != row.Blob {
		return false
	}
	if len(bindings) == 1 {
		if target, ok := bindings[0][skillsSourceKey(row.Repo, row.Commit, row.Path)]; ok {
			return skillsMaskedMetadataMatches(row, outputs, target) && skillsMaskedSourceObjectTarget(row, raw, outputs, target)
		}
	}
	for _, output := range outputs {
		if output.SHA == row.SHA && bytes.Equal(output.body, raw) {
			return true
		}
	}
	return skillsMaskedSourceObject(row, raw, outputs)
}
