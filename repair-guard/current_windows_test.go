package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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
	if got := strings.Join(currentGitHubIdentityArgs(product), "\x00"); got != "api\x00--hostname\x00github.com\x00repos/autodevjapan/godd-13a13e1\x00--jq\x00[.id, .full_name, .owner.login] | @tsv" {
		t.Fatal("GitHub identity request is not pinned to the declared host and repository")
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
	fixtureProduct := product
	fixtureProduct.root = testRoot
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
	qualifies := func(p currentProduct) bool { return p.project == "GoDD-13a13e1" && p.task == 2850 }
	if !currentProductsScopePathAllowed(workspace, []currentProduct{fixtureProduct}, qualifies, allow) {
		t.Fatal("declared product scope failed to route the exact task")
	}
	if currentProductsScopePathAllowed(workspace, []currentProduct{ao}, qualifies, allow) || currentProductsScopePathAllowed(workspace, []currentProduct{fixtureProduct}, func(currentProduct) bool { return false }, allow) {
		t.Fatal("path routing accepted another product or an unqualified task")
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
	currentFixtureGit(t, junctionRoot, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "fixture")
	plainContainer := junctionRoot + "-wt"
	if os.Mkdir(plainContainer, 0700) != nil {
		t.Fatal("plain junction-control container mkdir failed")
	}
	if !currentProductPath(filepath.Join(plainContainer, "todo-2850-control"), junctionRoot, product.repository, "2850", allow) {
		t.Fatal("plain container control did not admit the planned successor")
	}
	if os.Remove(plainContainer) != nil {
		t.Fatal("plain junction-control container cleanup failed")
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", junctionRoot+"-wt", outside)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture container junction failed: %v: %s", err, output)
	}
	if currentProductPath(filepath.Join(junctionRoot+"-wt", "todo-2850-junction"), junctionRoot, product.repository, "2850", allow) {
		t.Fatal("junction worktree container accepted")
	}
	realRoot := filepath.Join(base, "real-root")
	aliasParent := filepath.Join(base, "alias-parent")
	if os.Mkdir(realRoot, 0700) != nil || os.Mkdir(aliasParent, 0700) != nil {
		t.Fatal("root junction fixture mkdir failed")
	}
	currentFixtureGit(t, realRoot, "init", "-q")
	currentFixtureGit(t, realRoot, "remote", "add", "origin", "https://github.com/AutoDevJapan/GoDD-13a13e1.git")
	currentFixtureGit(t, realRoot, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "fixture")
	sourceFile := filepath.Join(realRoot, "source.txt")
	if os.WriteFile(sourceFile, []byte("fixture"), 0600) != nil {
		t.Fatal("root junction positive-control file setup failed")
	}
	plainRootContainer := realRoot + "-wt"
	if os.Mkdir(plainRootContainer, 0700) != nil {
		t.Fatal("root junction positive-control container setup failed")
	}
	plannedRootChild := filepath.Join(plainRootContainer, "todo-2850-root-junction")
	if !currentProductPath(sourceFile, realRoot, product.repository, "2850", allow) || !currentProductPath(plannedRootChild, realRoot, product.repository, "2850", allow) {
		t.Fatal("normal root positive control did not admit its source and planned worktree")
	}
	if os.Remove(plainRootContainer) != nil {
		t.Fatal("root junction positive-control container cleanup failed")
	}
	rootAlias := filepath.Join(aliasParent, "product")
	cmd = exec.Command("cmd", "/c", "mklink", "/J", rootAlias, realRoot)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture project-root junction failed: %v: %s", err, output)
	}
	if currentProductPath(filepath.Join(rootAlias, "source.txt"), rootAlias, product.repository, "2850", allow) || currentProductPath(filepath.Join(rootAlias+"-wt", "todo-2850-root-junction"), rootAlias, product.repository, "2850", allow) {
		t.Fatal("junction project root accepted")
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
	identityOutput := []byte("1283948432\tautodevjapan/godd-13a13e1\tAutoDevJapan\n")
	identity := func(p currentProduct) bool { return currentGitHubIdentityMatches(identityOutput, p) }
	inScope := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	receipt := currentSuccessionReceiptWith(db, product, inScope, allow, currentPlain, valid, identity)
	if len(receipt) != 9 || receipt["verified"] != true || receipt["domain"] != "current-product-succession-v1" || receipt["origin_todo_id"] != 2834 || receipt["todo_id"] != 2850 || receipt["project"] != product.project || receipt["spec_sha256"] != hashBytes(body) || receipt["original_registry_recovered"] != false {
		t.Fatal("valid successor receipt rejected")
	}
	registeredHash, _ := currentPathHash(root)
	specPathHash, _ := currentPathHash(spec)
	if receipt["registered_path_sha256"] != registeredHash || receipt["spec_path_sha256"] != specPathHash {
		t.Fatal("receipt path fingerprints differ")
	}
	storedProjectPath := root + `\`
	storedSpecPath := strings.ReplaceAll(spec, "/", `\`) + `\`
	obj(db["projects"])[product.project] = storedProjectPath
	itemAt(db, product.task)["spec"] = storedSpecPath
	storedReceipt := currentSuccessionReceiptWith(db, product, inScope, allow, currentPlain, valid, identity)
	storedProjectHash, _ := currentPathHash(storedProjectPath)
	storedSpecHash, _ := currentPathHash(storedSpecPath)
	if storedReceipt["verified"] != true || storedReceipt["registered_path_sha256"] != storedProjectHash || storedReceipt["spec_path_sha256"] != storedSpecHash {
		t.Fatal("receipt fingerprints differ from stored registration values")
	}
	encoded, err := json.Marshal(receipt)
	if err != nil || strings.Contains(string(encoded), root) || strings.Contains(string(encoded), spec) || strings.Contains(string(encoded), strings.ToLower(strings.ReplaceAll(root, `\`, "/"))) || strings.Contains(string(encoded), strings.ToLower(strings.ReplaceAll(spec, `\`, "/"))) {
		t.Fatal("receipt leaked a private path")
	}
	assertGenericFailure := func(label string, got object) {
		t.Helper()
		if got["verified"] != false || len(got) != 1 {
			t.Fatalf("%s did not return generic failure", label)
		}
	}
	assertGenericFailure("protected scope", currentSuccessionReceiptWith(db, product, inScope, func(string) bool { return true }, currentPlain, valid, identity))
	assertGenericFailure("wrong origin", currentSuccessionReceiptWith(db, product, inScope, allow, currentPlain, func(string, string) bool { return false }, identity))
	assertGenericFailure("wrong identity", currentSuccessionReceiptWith(db, product, inScope, allow, currentPlain, valid, func(currentProduct) bool { return false }))
	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/other/GoDD-13a13e1.git")
	assertGenericFailure("mismatched repository origin", currentSuccessionReceiptWith(db, product, inScope, allow, currentPlain, sourceOriginVerified, identity))
	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/AutoDevJapan/GoDD-13a13e1.git")
	for _, bad := range [][]byte{
		{},
		[]byte("\n"),
		[]byte("1283948432\tautodevjapan/godd-13a13e1\t\n"),
		[]byte("1283948433\tautodevjapan/godd-13a13e1\tAutoDevJapan\n"),
		[]byte("1283948432\tother/godd-13a13e1\tAutoDevJapan\n"),
		[]byte("1283948432\tautodevjapan/godd-13a13e1\tOtherOwner\n"),
		[]byte("-1283948432\tautodevjapan/godd-13a13e1\tAutoDevJapan\n"),
		[]byte("not-an-id\tautodevjapan/godd-13a13e1\tAutoDevJapan\n"),
		[]byte("1283948432\tautodevjapan/godd-13a13e1\tAutoDevJapan\nextra\n"),
		[]byte("1283948432\tautodevjapan/godd-13a13e1\tAutoDevJapan\textra\n"),
	} {
		if currentGitHubIdentityMatches(bad, product) {
			t.Fatal("malformed or mismatched GitHub identity accepted")
		}
	}
	assertGenericFailure("out-of-scope date", currentSuccessionReceiptWith(db, product, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), allow, currentPlain, valid, identity))
	assertGenericFailure("before-scope date", currentSuccessionReceiptWith(db, product, time.Date(2026, 10, 12, 23, 59, 59, 0, time.UTC), allow, currentPlain, valid, identity))
	for _, boundary := range []struct {
		name string
		at   time.Time
	}{
		{"scope start", time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)},
		{"just before scope end", time.Date(2026, 10, 20, 23, 59, 59, 999999999, time.UTC)},
	} {
		if currentSuccessionReceiptWith(db, product, boundary.at, allow, currentPlain, valid, identity)["verified"] != true {
			t.Fatalf("valid %s was rejected", boundary.name)
		}
	}
	if os.WriteFile(spec, append(body, '!'), 0600) != nil {
		t.Fatal("fixture spec mutation failed")
	}
	assertGenericFailure("changed spec", currentSuccessionReceiptWith(db, product, inScope, allow, currentPlain, valid, identity))
	if _, ok := currentProductByTask(987654); ok {
		t.Fatal("unknown task received a declaration")
	}
	if _, ok := currentProductByTask(product.task); !ok {
		t.Fatal("declared task lookup failed")
	}
}

func TestCurrentPathHashMatchesFixedReceiptVectors(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"C:/Users/makhmeto/Desktop/dev/ADJ/GoDD-13a13e1/", "3062f24ecddbd7720f761e2c44496591c1fb1ad83c7a07fe880229624e38b4a8"},
		{`C:\Users\Makhmeto\Desktop\dev\ADJ\GoDD-13a13e1\\`, "3062f24ecddbd7720f761e2c44496591c1fb1ad83c7a07fe880229624e38b4a8"},
		{"c:/users/makhmeto/Desktop/DEV/adj/GoDD-13a13e1", "3062f24ecddbd7720f761e2c44496591c1fb1ad83c7a07fe880229624e38b4a8"},
		{"D:/CC/Tools/todo/specs/2850-cdf16525f6ab44b1a3f5e6d1f9337359.md", "ffc9fa43704106b183468369479cf616e9af3304382880d72a28f3378b48b739"},
		{`D:\CC\Tools\todo\specs\2850-cdf16525f6ab44b1a3f5e6d1f9337359.md`, "ffc9fa43704106b183468369479cf616e9af3304382880d72a28f3378b48b739"},
		{"d:/cc/TOOLS/todo/specs/2850-cdf16525f6ab44b1a3f5e6d1f9337359.MD", "ffc9fa43704106b183468369479cf616e9af3304382880d72a28f3378b48b739"},
	} {
		got, ok := currentPathHash(tc.path)
		if !ok || got != tc.want {
			t.Fatalf("path fingerprint did not match fixed vector")
		}
	}
	for _, path := range []string{
		"relative/product",
		"C:/Users/fixture/../other/product",
		"C:/Users//fixture/product",
		`\\?\C:\Users\fixture\product`,
		`\\.\C:\Users\fixture\product`,
		"//?/C:/Users/fixture/product",
		"//./C:/Users/fixture/product",
		"C:/Users/fixture/名前",
	} {
		if _, ok := currentPathHash(path); ok {
			t.Fatal("noncanonical or non-absolute path received a fingerprint")
		}
	}
}

func TestCurrentGitHubCLIEnvironmentUsesStoredOAuthConfiguration(t *testing.T) {
	input := []string{
		"APPDATA=C:/Users/fixture/AppData/Roaming",
		"GH_CONFIG_DIR=C:/Users/fixture/.config/gh",
		"HTTPS_PROXY=http://proxy.invalid:8080",
		"GH_HOST=enterprise.invalid",
		"GH_TOKEN=environment-token",
		"GITHUB_TOKEN=environment-token",
		"GH_ENTERPRISE_TOKEN=environment-token",
		"GITHUB_ENTERPRISE_TOKEN=environment-token",
	}
	got := currentGitHubCLIEnvironment(input)
	want := []string{
		"APPDATA=C:/Users/fixture/AppData/Roaming",
		"GH_CONFIG_DIR=C:/Users/fixture/.config/gh",
		"HTTPS_PROXY=http://proxy.invalid:8080",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatal("GitHub CLI environment retained an auth or host override")
	}
}

func TestCurrentSuccessionCLIArgumentsFailClosed(t *testing.T) {
	for _, value := range []string{"", "abc", "0", "-1", "+2850", "02850"} {
		if _, ok := parseCurrentSuccessionTask(value); ok {
			t.Fatalf("invalid successor task value accepted")
		}
	}
	if task, ok := parseCurrentSuccessionTask("2850"); !ok || task != 2850 {
		t.Fatal("valid successor task value rejected")
	}
	if task, ok := parseCurrentSuccessionTask("999999"); !ok {
		t.Fatal("positive unknown task did not parse")
	} else if _, declared := currentProductByTask(task); declared {
		t.Fatal("unknown positive task received a declaration")
	}
	invalid := []string{"--verify-current-succession=abc", "-verify-current-succession", "--verify-current-succession"}
	for _, args := range invalid {
		if !currentSuccessionFlagPresent([]string{args}) {
			t.Fatal("succession command not detected for generic failure handling")
		}
	}
	parsed, err := parseGuardArgs([]string{"--verify-current-succession", "2850", "--guard-path", "C:/fixture"})
	if err != nil || !parsed.successionSet || !parsed.pathSet {
		t.Fatal("combined CLI modes were not identified for rejection")
	}
}

type closedWriter struct{}

func (closedWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestCurrentSuccessionCLIReturnsGenericJSONAndExitCodes(t *testing.T) {
	success := object{
		"domain": "current-product-succession-v1", "verified": true,
		"origin_todo_id": 2834, "todo_id": 2850, "project": "GoDD-13a13e1",
		"registered_path_sha256": strings.Repeat("a", 64), "spec_path_sha256": strings.Repeat("b", 64),
		"spec_sha256": strings.Repeat("c", 64), "original_registry_recovered": false,
	}
	successJSON, err := json.Marshal(success)
	if err != nil {
		t.Fatal("success fixture encoding failed")
	}
	cases := []struct {
		name string
		args []string
		want string
		code int
	}{
		{"unknown value", []string{"--verify-current-succession", "abc"}, "{\"verified\":false}\n", 1},
		{"zero value", []string{"--verify-current-succession", "0"}, "{\"verified\":false}\n", 1},
		{"negative value", []string{"--verify-current-succession", "-1"}, "{\"verified\":false}\n", 1},
		{"missing value", []string{"--verify-current-succession"}, "{\"verified\":false}\n", 1},
		{"mixed mode", []string{"--verify-current-succession", "2850", "--guard-path", "C:/fixture"}, "{\"verified\":false}\n", 1},
		{"trailing positional", []string{"--verify-current-succession", "2850", "junk"}, "{\"verified\":false}\n", 1},
		{"flag after positional", []string{"junk", "--verify-current-succession", "2850"}, "{\"verified\":false}\n", 1},
		{"unverified receipt", []string{"--verify-current-succession", "999999"}, "{\"verified\":false}\n", 1},
		{"verified receipt", []string{"--verify-current-succession", "2850"}, string(successJSON) + "\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runGuardCLI(tc.args, &stdout, &stderr, func(id int) object {
				if id == 2850 {
					return success
				}
				return object{"verified": false, "private_path": "must not be emitted"}
			})
			if code != tc.code || stdout.String() != tc.want || stderr.Len() != 0 {
				t.Fatalf("unexpected current-succession CLI response: exit=%d", code)
			}
		})
	}
	code := runGuardCLI([]string{"--verify-current-succession", "2850"}, closedWriter{}, &bytes.Buffer{}, func(int) object { return success })
	if code != 1 {
		t.Fatal("stdout write failure was reported as successful")
	}
	var helpStdout, helpStderr bytes.Buffer
	if code = runGuardCLI([]string{"--help"}, &helpStdout, &helpStderr, func(int) object { return success }); code != 0 || helpStdout.Len() != 0 || !strings.Contains(helpStderr.String(), "Usage of scrub391:") {
		t.Fatal("help output location or exit code changed")
	}
}
