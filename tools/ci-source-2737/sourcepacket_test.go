package sourcepacket

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, input []byte, args ...string) []byte {
	t.Helper()
	c := exec.Command("git", args...)
	configureProcess(c)
	c.Dir = dir
	c.Env = cleanGitEnv(os.Environ())
	c.Stdin = bytes.NewReader(input)
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git fixture command failed (%s): %v", args[0], err)
	}
	return b
}

func fixture(t *testing.T) (Packet, string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "repo")
	if err := os.Mkdir(src, 0700); err != nil {
		t.Fatal(err)
	}
	run(t, src, nil, "init", "--quiet", "--template=")
	run(t, src, nil, "config", "user.name", "Fixture")
	run(t, src, nil, "config", "user.email", "fixture@example.invalid")
	files := map[string]string{".gitignore": "tracked.dat\n", ".gitattributes": "*.dat filter=mutate\n", "tracked.dat": "tracked raw bytes\n", workflowPath: "name: fixture\n"}
	for name, body := range files {
		target := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run(t, src, nil, "config", "filter.mutate.clean", "tr 'a-z' 'A-Z'")
	run(t, src, nil, "add", "-f", ".")
	run(t, src, nil, "update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("c", 40)+",vendor/dependency")
	run(t, src, nil, "commit", "--quiet", "-m", "synthetic fixture")
	head := strings.TrimSpace(string(run(t, src, nil, "rev-parse", "HEAD")))
	tree := strings.TrimSpace(string(run(t, src, nil, "rev-parse", "HEAD^{tree}")))
	commit := run(t, src, nil, "cat-file", "commit", head)
	rawTree := run(t, src, nil, "ls-tree", "-r", "-t", "-z", head)
	var entries []TreeEntry
	for _, line := range bytes.Split(rawTree, []byte{0}) {
		if len(line) == 0 {
			continue
		}
		meta, name, ok := bytes.Cut(line, []byte{'\t'})
		if !ok {
			t.Fatal("bad fixture tree")
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 3 {
			t.Fatal("bad fixture entry")
		}
		entries = append(entries, TreeEntry{Path: string(name), Mode: fields[0], Type: fields[1], SHA: fields[2]})
	}
	var ar bytes.Buffer
	gz := gzip.NewWriter(&ar)
	tw := tar.NewWriter(gz)
	for name := range files {
		for _, entry := range entries {
			if entry.Path == name && entry.Type == "blob" {
				body := run(t, src, nil, "cat-file", "blob", entry.SHA)
				if err := tw.WriteHeader(&tar.Header{Name: "repo-prefix/" + name, Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(body); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	wf := []byte("name: fixture\n")
	wh := sha256.Sum256(wf)
	p := Packet{Pin: Pin{HeadSHA: head, TreeSHA: tree, WorkflowSHA256: hex.EncodeToString(wh[:])}, CommitPayload: commit, Archive: ar.Bytes(), ArchiveSHA256: digest(ar.Bytes()), TreeEntries: entries}
	return p, root
}

func TestEncodeDecodeStrictPinAndDuplicates(t *testing.T) {
	p, _ := fixture(t)
	encoded, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(encoded, p.Pin)
	if err != nil || got.Pin != p.Pin {
		t.Fatalf("roundtrip rejected: %v", err)
	}
	other := p.Pin
	other.HeadSHA = strings.Repeat("a", 40)
	if _, err = Decode(encoded, other); err == nil {
		t.Fatal("different expected pin accepted")
	}
	if _, err = Decode(bytes.Replace(encoded, []byte(`"pin":`), []byte(`"pin":{},"pin":`), 1), p.Pin); err == nil {
		t.Fatal("duplicate key accepted")
	}
	if _, err = Decode(append(encoded, []byte(` {}`)...), p.Pin); err == nil {
		t.Fatal("multiple documents accepted")
	}
	if _, err = Decode(bytes.Replace(encoded, []byte(`"pin":`), []byte(`"unexpected":1,"pin":`), 1), p.Pin); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestMaterializeReproducesRealGitObjectsAndRawIgnoredBlob(t *testing.T) {
	p, root := fixture(t)
	dest := filepath.Join(root, "guest", "source")
	receipt, err := Materialize(context.Background(), p, p.Pin, dest)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.HeadSHA != p.Pin.HeadSHA || receipt.TreeSHA != p.Pin.TreeSHA || receipt.BlobCount != 4 {
		t.Fatalf("wrong receipt: %+v", receipt)
	}
	if got := strings.TrimSpace(string(run(t, dest, nil, "rev-parse", "HEAD^{tree}"))); got != p.Pin.TreeSHA {
		t.Fatalf("tree mismatch %s", got)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "tracked.dat")); err != nil || string(got) != "TRACKED RAW BYTES\n" {
		t.Fatalf("archive bytes changed: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "vendor", "dependency")); !os.IsNotExist(err) {
		t.Fatalf("submodule was materialized: %v", err)
	}
	for _, args := range [][]string{{"config", "--local", "--name-only", "--list"}} {
		out := string(run(t, dest, nil, args...))
		for _, bad := range []string{"remote.", "credential.", "filter.", "core.hookspath", "submodule."} {
			if strings.Contains(strings.ToLower(out), bad) {
				t.Fatalf("unexpected git config key %s", bad)
			}
		}
	}
	if _, err = Materialize(context.Background(), p, p.Pin, dest); err == nil {
		t.Fatal("nonfresh root accepted")
	}
}

func TestMaterializeRejectsAlteredArchiveBlobCommitAndTree(t *testing.T) {
	base, root := fixture(t)
	dest := filepath.Join(root, "guest")
	changed := base
	changed.Archive = append([]byte(nil), base.Archive...)
	changed.Archive[len(changed.Archive)-1] ^= 1
	changed.ArchiveSHA256 = digest(changed.Archive)
	if _, err := Materialize(context.Background(), changed, base.Pin, filepath.Join(dest, "archive")); err == nil {
		t.Fatal("altered archive accepted")
	}
	changed = base
	changed.TreeEntries = append([]TreeEntry(nil), base.TreeEntries...)
	changed.TreeEntries[0].SHA = strings.Repeat("a", 40)
	if _, err := Materialize(context.Background(), changed, base.Pin, filepath.Join(dest, "blob")); err == nil {
		t.Fatal("altered blob identity accepted")
	}
	changed = base
	changed.CommitPayload = append([]byte(nil), base.CommitPayload...)
	changed.CommitPayload = append(changed.CommitPayload, 'x')
	if _, err := Materialize(context.Background(), changed, base.Pin, filepath.Join(dest, "commit")); err == nil {
		t.Fatal("altered commit accepted")
	}
	changed = base
	changed.Pin.TreeSHA = strings.Repeat("b", 40)
	if _, err := Materialize(context.Background(), changed, base.Pin, filepath.Join(dest, "tree")); err == nil {
		t.Fatal("altered tree pin accepted")
	}
}

type tarFixtureEntry struct {
	name, body string
	kind       byte
}

func makeTar(t *testing.T, names ...tarFixtureEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	g := gzip.NewWriter(&b)
	w := tar.NewWriter(g)
	for _, item := range names {
		kind := item.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		if err := w.WriteHeader(&tar.Header{Name: item.name, Linkname: "target", Mode: 0600, Size: int64(len(item.body)), Typeflag: kind}); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, item.body)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestArchivePathTypeDuplicateAndWindowsBoundaries(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", `prefix/a\\b`, "prefix/C:/evil", "prefix/CON.txt", "prefix/.git/config"} {
		if _, err := archiveFiles(makeTar(t, tarFixtureEntry{name: name, body: "x"})); err == nil {
			t.Errorf("unsafe archive path accepted: %q", name)
		}
	}
	if _, err := archiveFiles(makeTar(t, tarFixtureEntry{name: "prefix/duplicate", body: "x"}, tarFixtureEntry{name: "prefix/duplicate", body: "y"})); err == nil {
		t.Fatal("duplicate archive entry accepted")
	}
	if _, err := archiveFiles(makeTar(t, tarFixtureEntry{name: "prefix/link", kind: tar.TypeSymlink})); err == nil {
		t.Fatal("archive symlink accepted")
	}
	if safePath("a/NUL.txt") || safePath("a/name.") || safePath("a/name ") {
		t.Fatal("Windows-special path accepted")
	}
}
