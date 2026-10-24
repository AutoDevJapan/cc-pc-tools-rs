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
	if !skillsSourceClaimsPreserved(m) || len(m.Sources) == 0 {
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
	catalog, ok := fs.open(filepath.Join(corpus, "phase-a-catalog.jsonl"), 256<<20)
	if !ok {
		return false
	}
	found := map[string]skillsSavedSource{}
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
	if !ok || len(found) != len(wanted) {
		return false
	}
	byRepo := map[string][]skillsSource{}
	for _, source := range wanted {
		byRepo[source.Repository] = append(byRepo[source.Repository], source)
	}
	for repo, sources := range byRepo {
		anchor, exists := anchors[repo]
		if !exists || !skillsDigest(anchor.IndexSHA) || !skillsDigest(anchor.TreeSHA) || !skillsDigest(anchor.CommitSHA) {
			return false
		}
		root := filepath.Join(corpus, "github", strings.ReplaceAll(repo, "/", "__"))
		commit, ok := fs.read(filepath.Join(root, "commit.json"), 1<<20)
		value, parsed := conversationJSON(commit)
		treeSHA := str(obj(obj(value["commit"])["tree"])["sha"])
		if !ok || hashBytes(commit) != anchor.CommitSHA || !parsed || value["sha"] != skillsPublicSources[repo] || !civ6Commit.MatchString(treeSHA) {
			return false
		}
		paths := map[string]bool{}
		for _, source := range sources {
			paths[source.Path], paths[source.License.Path] = true, true
		}
		index, ok := fs.open(filepath.Join(root, "content-index.jsonl"), 128<<20)
		if !ok {
			return false
		}
		selected := map[string]skillsSavedSource{}
		ok = skillsSourceLines(index, anchor.IndexSHA, func(raw []byte) bool {
			var row skillsSavedSource
			if json.Unmarshal(raw, &row) != nil {
				return false
			}
			if !paths[row.Path] {
				return true
			}
			if _, duplicate := selected[row.Path]; duplicate || row.Repo != repo || row.Commit != skillsPublicSources[repo] || !skillsDigest(row.SHA) || !civ6Commit.MatchString(row.Blob) || row.Bytes < 1 || row.Bytes > 2<<20 || row.Content != "objects/"+row.SHA+".gz" {
				return false
			}
			selected[row.Path] = row
			return true
		})
		if !ok || len(selected) != len(paths) || !skillsSourceTree(root, anchor, treeSHA, selected, fs) {
			return false
		}
		for _, source := range sources {
			original, notice := selected[source.Path], selected[source.License.Path]
			cat := found[skillsSourceKey(repo, source.Commit, source.Path)]
			if original.SHA != source.SHA || original.Blob != cat.Blob || original.Bytes != cat.Bytes ||
				cat.Content != "github/"+strings.ReplaceAll(repo, "/", "__")+"/"+original.Content ||
				notice.SHA != source.License.SHA || notice.Role != "license_or_notice" ||
				!skillsSourceObject(root, original, m.Files, fs) || !skillsSourceObject(root, notice, m.Files, fs) {
				return false
			}
		}
	}
	return true
}

func skillsSourceTree(root string, a skillsSourceAnchor, rootSHA string, selected map[string]skillsSavedSource, fs *skillsFS) bool {
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

func skillsSourceObject(root string, row skillsSavedSource, outputs []skillsOutput, fs *skillsFS) bool {
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
	for _, output := range outputs {
		if output.SHA == row.SHA && bytes.Equal(output.body, raw) {
			return true
		}
	}
	return false
}
