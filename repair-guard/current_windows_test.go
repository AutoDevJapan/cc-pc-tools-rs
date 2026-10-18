package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func currentFixtureGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture git failed: %v: %s", err, b)
	}
}

func TestCurrentProductAdmitsOnlyNewActorAndSameRepository(t *testing.T) {
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "source")
	container := root + "-wt"
	if os.Mkdir(root, 0700) != nil || os.Mkdir(container, 0700) != nil {
		t.Fatal("fixture mkdir failed")
	}
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/AutoDevJapan/GoDD-AO.git")
	currentFixtureGit(t, root, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "fixture")
	allow := func(string) bool { return false }
	check := func(path string, want bool) {
		t.Helper()
		if currentProductPath(path, root, currentAORepository, "2839", allow) != want {
			t.Fatalf("unexpected admission for fixture path: %s", filepath.Base(path))
		}
	}
	check(root, true)
	workspace := filepath.Join(container, "todo-2839-current")
	check(workspace, true)
	check(filepath.Join(workspace, "not-created"), false)
	check(filepath.Join(container, "todo-1476-original"), false)
	check(filepath.Join(container, "todo-28390-other"), false)
	check(filepath.Join(container, "todo-2839-"), false)
	check(filepath.Join(container, "todo-2839-x:stream"), false)
	check(container, false)
	check(root+"-sibling", false)
	if currentProductPath(root, root, currentAORepository, "2839", func(string) bool { return true }) {
		t.Fatal("existing denial was ignored")
	}
	currentFixtureGit(t, root, "worktree", "add", "-q", "-b", "scheduler/todo-2839-current", workspace)
	check(workspace, true)
	leaf := filepath.Join(workspace, "source.txt")
	if os.WriteFile(leaf, []byte("fixture"), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	check(leaf, true)
	if currentProductPath(leaf, root, currentAORepository, "2839", func(p string) bool { return same(p, leaf) }) {
		t.Fatal("leaf denial was ignored")
	}
	currentFixtureGit(t, workspace, "checkout", "-q", "--detach")
	check(workspace, false)
	currentFixtureGit(t, workspace, "checkout", "-q", "scheduler/todo-2839-current")
	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/other/GoDD-AO.git")
	check(workspace, false)
	check(root, false)
}

func TestCurrentProductRejectsIndependentCloneAndPhysicalEscape(t *testing.T) {
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "source")
	container := root + "-wt"
	workspace := filepath.Join(container, "todo-2839-current")
	for _, p := range []string{root, container, workspace} {
		if os.Mkdir(p, 0700) != nil {
			t.Fatal("fixture mkdir failed")
		}
	}
	for _, p := range []string{root, workspace} {
		currentFixtureGit(t, p, "init", "-q")
		currentFixtureGit(t, p, "remote", "add", "origin", "https://github.com/AutoDevJapan/GoDD-AO.git")
	}
	currentFixtureGit(t, workspace, "checkout", "-q", "-b", "scheduler/todo-2839-current")
	if currentProductPath(workspace, root, currentAORepository, "2839", func(string) bool { return false }) {
		t.Fatal("independent Git common directory was accepted")
	}
	// A junction works without developer-mode symlink privileges on Windows.
	outside := filepath.Join(base, "outside")
	if os.Mkdir(outside, 0700) != nil {
		t.Fatal("fixture mkdir failed")
	}
	alias := filepath.Join(root, "escape")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", alias, outside)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture junction failed: %v: %s", err, b)
	}
	if currentProductPath(alias, root, currentAORepository, "2839", func(string) bool { return false }) {
		t.Fatal("physical escape was accepted")
	}
}

func TestCurrentReceiptRequiresPlainPhysicalFile(t *testing.T) {
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(base, "receipt.txt")
	if currentPlain(p, false) {
		t.Fatal("missing receipt accepted")
	}
	if os.WriteFile(p, []byte("fixture"), 0600) != nil || !currentPlain(p, false) {
		t.Fatal("plain receipt rejected")
	}
	if currentPlain(base, false) || currentPlain(p, true) {
		t.Fatal("wrong entry kind accepted")
	}
}

func TestCurrentQualificationBindsNewTaskProjectAndExactSpecification(t *testing.T) {
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(base, "request.md")
	body := []byte("current owner request, not recovered original scope")
	if os.WriteFile(spec, body, 0600) != nil {
		t.Fatal("fixture write failed")
	}
	row := object{"id": float64(2839), "project": "GoDD-AO", "spec": spec}
	db := object{"projects": object{"GoDD-AO": base}, "items": []any{row}}
	allow := func(string) bool { return false }
	if !currentQualification(db, "GoDD-AO", 2839, base, spec, hashBytes(body), allow) {
		t.Fatal("current fixture rejected")
	}
	if currentQualification(db, "GoDD-AO", 2839, base, spec, hashBytes(body), func(string) bool { return true }) {
		t.Fatal("specification denial ignored")
	}
	for _, task := range []int{1476, 2179, 2834, 2840} {
		if currentQualification(db, "GoDD-AO", task, base, spec, hashBytes(body), allow) {
			t.Fatal("another original/current task admitted")
		}
	}
	row["project"] = "other"
	if currentQualification(db, "GoDD-AO", 2839, base, spec, hashBytes(body), allow) {
		t.Fatal("changed project admitted")
	}
	row["project"] = "GoDD-AO"
	db["projects"] = object{"GoDD-AO": base + "-other"}
	if currentQualification(db, "GoDD-AO", 2839, base, spec, hashBytes(body), allow) {
		t.Fatal("changed registration root admitted")
	}
	db["projects"] = object{"GoDD-AO": base}
	if os.WriteFile(spec, append(body, '!'), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	if currentQualification(db, "GoDD-AO", 2839, base, spec, hashBytes(body), allow) {
		t.Fatal("changed specification admitted")
	}
	db["items"] = []any{row, row}
	if currentQualification(db, "GoDD-AO", 2839, base, spec, hashBytes(append(body, '!')), allow) {
		t.Fatal("ambiguous current task admitted")
	}
}

func TestDeclaredSuccessorBindsExactTaskAndRepository(t *testing.T) {
	if len(currentProducts) != 2 {
		t.Fatal("unexpected number of declared current products")
	}
	ao := currentProducts[0]
	if ao.project != "GoDD-AO" || ao.task != 2839 || ao.owner != "AutoDevJapan" || ao.repository != "autodevjapan/godd-ao" || ao.repositoryID != 1296537193 || ao.root != "C:/Users/makhmeto/Desktop/dev/ADJ/GoDD-AO" || ao.spec != "D:/CC/Tools/todo/specs/2839-ccac08e67b3b4a4488f3f0241c59b32c.md" || ao.specSHA != "b44e6c70614089c50908901e7d60a902f9e2f0793c813f9a0c2595f03714fd33" {
		t.Fatal("existing AO declaration changed")
	}
	product := currentProducts[1]
	if product.project != "GoDD-13a13e1" || product.task != 2850 || product.owner != "AutoDevJapan" || product.repository != "autodevjapan/godd-13a13e1" || product.repositoryID != 1283948432 || product.root != "C:/Users/makhmeto/Desktop/dev/ADJ/GoDD-13a13e1" || product.spec != "D:/CC/Tools/todo/specs/2850-cdf16525f6ab44b1a3f5e6d1f9337359.md" || product.specSHA != "207951d442f732e489326ef92819c3ee2674bdb38b64605d3043e50e198a0d95" {
		t.Fatal("successor declaration changed")
	}
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(base, "request.md")
	body := []byte("fixed current inheritance spec")
	if os.WriteFile(spec, body, 0600) != nil {
		t.Fatal("fixture spec write failed")
	}
	testRoot := filepath.Join(base, "product")
	db := object{
		"projects": object{product.project: testRoot},
		"items":    []any{object{"id": float64(product.task), "project": product.project, "spec": spec}},
	}
	allow := func(string) bool { return false }
	if !currentQualification(db, product.project, product.task, testRoot, spec, hashBytes(body), allow) {
		t.Fatal("exact current successor rejected")
	}
	for _, altered := range []struct {
		task int
		root string
		spec string
		hash string
	}{
		{product.task + 1, testRoot, spec, hashBytes(body)},
		{product.task, testRoot + "-other", spec, hashBytes(body)},
		{product.task, testRoot, spec, hashBytes(append(body, '!'))},
	} {
		if currentQualification(db, product.project, altered.task, altered.root, altered.spec, altered.hash, allow) {
			t.Fatal("altered successor binding accepted")
		}
	}

	container := testRoot + "-wt"
	if os.Mkdir(testRoot, 0700) != nil || os.Mkdir(container, 0700) != nil {
		t.Fatal("fixture container mkdir failed")
	}
	currentFixtureGit(t, testRoot, "init", "-q")
	currentFixtureGit(t, testRoot, "remote", "add", "origin", "https://github.com/AutoDevJapan/GoDD-13a13e1.git")
	currentFixtureGit(t, testRoot, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "fixture")
	workspace := filepath.Join(container, "todo-2850-issue569")
	if !currentProductPath(workspace, testRoot, product.repository, "2850", allow) {
		t.Fatal("exact planned successor worktree rejected")
	}
	if currentProductPath(filepath.Join(container, "todo-2851-issue569"), testRoot, product.repository, "2850", allow) {
		t.Fatal("different task worktree accepted")
	}
	if currentProductPath(filepath.Join(container, "todo-2850-issue569"), testRoot, "other/repository", "2850", allow) {
		t.Fatal("different repository accepted")
	}
	currentFixtureGit(t, testRoot, "worktree", "add", "-q", "-b", "scheduler/todo-2850-issue569", workspace)
	if !currentProductPath(workspace, testRoot, product.repository, "2850", allow) {
		t.Fatal("exact existing successor worktree rejected")
	}
	currentFixtureGit(t, workspace, "checkout", "-q", "-b", "scheduler/todo-2851-unrelated")
	if currentProductPath(workspace, testRoot, product.repository, "2850", allow) {
		t.Fatal("different task branch accepted")
	}
	currentFixtureGit(t, workspace, "checkout", "-q", "scheduler/todo-2850-issue569")
	currentFixtureGit(t, workspace, "remote", "set-url", "origin", "https://github.com/other/GoDD-13a13e1.git")
	if currentProductPath(workspace, testRoot, product.repository, "2850", allow) {
		t.Fatal("different origin accepted")
	}
	currentFixtureGit(t, workspace, "remote", "set-url", "origin", "https://github.com/AutoDevJapan/GoDD-13a13e1.git")
	if currentProductPath(workspace, testRoot, product.repository, "2850", func(p string) bool { return same(p, container) }) {
		t.Fatal("protected worktree container accepted")
	}
	independent := filepath.Join(container, "todo-2850-independent")
	if os.Mkdir(independent, 0700) != nil {
		t.Fatal("independent worktree fixture mkdir failed")
	}
	currentFixtureGit(t, independent, "init", "-q")
	currentFixtureGit(t, independent, "remote", "add", "origin", "https://github.com/AutoDevJapan/GoDD-13a13e1.git")
	currentFixtureGit(t, independent, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "fixture")
	currentFixtureGit(t, independent, "checkout", "-q", "-b", "scheduler/todo-2850-independent")
	if currentProductPath(independent, testRoot, product.repository, "2850", allow) {
		t.Fatal("independent Git common directory accepted")
	}
	junctionRoot := filepath.Join(base, "junction-product")
	outside := filepath.Join(base, "outside")
	if os.Mkdir(junctionRoot, 0700) != nil || os.Mkdir(outside, 0700) != nil {
		t.Fatal("junction fixture mkdir failed")
	}
	currentFixtureGit(t, junctionRoot, "init", "-q")
	currentFixtureGit(t, junctionRoot, "remote", "add", "origin", "https://github.com/AutoDevJapan/GoDD-13a13e1.git")
	cmd := exec.Command("cmd", "/c", "mklink", "/J", junctionRoot+"-wt", outside)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture container junction failed: %v: %s", err, output)
	}
	if currentProductPath(filepath.Join(junctionRoot+"-wt", "todo-2850-junction"), junctionRoot, product.repository, "2850", allow) {
		t.Fatal("junction worktree container accepted")
	}
}

func TestCurrentSuccessionReceiptIsFiniteAndPathPrivate(t *testing.T) {
	product := currentProducts[1]
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "product")
	spec := filepath.Join(base, "request.md")
	body := []byte("fixed current inheritance spec")
	if os.Mkdir(root, 0700) != nil || os.WriteFile(spec, body, 0600) != nil {
		t.Fatal("fixture setup failed")
	}
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/AutoDevJapan/GoDD-13a13e1.git")
	product.root, product.spec, product.specSHA = root, spec, hashBytes(body)
	db := object{
		"projects": object{product.project: root},
		"items":    []any{object{"id": float64(product.task), "project": product.project, "spec": spec}},
	}
	allow := func(string) bool { return false }
	valid := func(path, repo string) bool { return sourceOriginVerified(path, repo) }
	identity := func(p currentProduct) bool { return p.repositoryID == product.repositoryID && p.owner == product.owner }
	receipt := currentSuccessionReceiptWith(db, product, allow, currentPlain, valid, identity)
	if receipt["verified"] != true || receipt["domain"] != "current-product-succession-v1" || receipt["origin_todo_id"] != 2834 || receipt["todo_id"] != 2850 || receipt["project"] != product.project || receipt["spec_sha256"] != hashBytes(body) || receipt["original_registry_recovered"] != false {
		t.Fatal("valid successor receipt rejected")
	}
	registeredHash, _ := currentPathHash(root)
	specPathHash, _ := currentPathHash(spec)
	if receipt["registered_path_sha256"] != registeredHash || receipt["spec_path_sha256"] != specPathHash {
		t.Fatal("receipt path fingerprints differ")
	}
	encoded, err := json.Marshal(receipt)
	if err != nil || strings.Contains(string(encoded), root) || strings.Contains(string(encoded), spec) {
		t.Fatal("receipt leaked a private path")
	}
	denied := currentSuccessionReceiptWith(db, product, func(string) bool { return true }, currentPlain, valid, identity)
	if denied["verified"] != false || len(denied) != 1 {
		t.Fatal("protected successor scope was not denied generically")
	}
	wrongOrigin := currentSuccessionReceiptWith(db, product, allow, currentPlain, func(string, string) bool { return false }, identity)
	if wrongOrigin["verified"] != false {
		t.Fatal("wrong origin was accepted")
	}
	wrongIdentity := currentSuccessionReceiptWith(db, product, allow, currentPlain, valid, func(currentProduct) bool { return false })
	if wrongIdentity["verified"] != false {
		t.Fatal("wrong GitHub identity was accepted")
	}
	if os.WriteFile(spec, append(body, '!'), 0600) != nil {
		t.Fatal("fixture spec mutation failed")
	}
	changedSpec := currentSuccessionReceiptWith(db, product, allow, currentPlain, valid, identity)
	if changedSpec["verified"] != false {
		t.Fatal("changed spec received a succession receipt")
	}
	if _, ok := currentProductByTask(987654); ok {
		t.Fatal("unknown task received a declaration")
	}
	if _, ok := currentProductByTask(product.task); !ok {
		t.Fatal("declared task lookup failed")
	}
}
