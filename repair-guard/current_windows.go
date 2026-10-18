package main

// Current, explicitly requested succession is separate from recovery of the
// original registry and original task IDs. Each entry admits one new actor.
import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const currentAORoot = "C:/Users/makhmeto/Desktop/dev/ADJ/GoDD-AO"
const currentAORepository = "autodevjapan/godd-ao"
const currentAOSpec = "D:/CC/Tools/todo/specs/2839-ccac08e67b3b4a4488f3f0241c59b32c.md"
const currentAOSpecSHA = "b44e6c70614089c50908901e7d60a902f9e2f0793c813f9a0c2595f03714fd33"
const currentHandoff = "D:/CC/Work/worker-resume-20261018-2103/hand-off.private.md"
const currentGitHubCLI = "C:/Program Files/GitHub CLI/gh.exe"
const currentTodoSpecsRoot = "D:/CC/Tools/todo/specs"

var currentSubworkSpecName = regexp.MustCompile(`^([1-9][0-9]*)-([0-9a-f]{32})\.md$`)
var currentSubworkJobName = regexp.MustCompile(`^([0-9]{8}-[0-9]{6}-todo-)([1-9][0-9]*)$`)
var currentSubworkWorkspaceName = regexp.MustCompile(`^todo-([1-9][0-9]*)-[A-Za-z0-9_-]+$`)

type currentProduct struct {
	project, root, owner, repository, spec, specSHA string
	repositoryID                                    int64
	task                                            int
}

// Keep this a finite declaration. Adding a product requires a separately
// verified registration, owner/repository identity, immutable spec, and path.
var currentProducts = []currentProduct{
	{project: "GoDD-AO", root: currentAORoot, owner: "AutoDevJapan", repository: currentAORepository, repositoryID: 1296537193, task: 2839, spec: currentAOSpec, specSHA: currentAOSpecSHA},
	{project: "GoDD-13a13e1", root: "C:/Users/makhmeto/Desktop/dev/ADJ/GoDD-13a13e1", owner: "AutoDevJapan", repository: "autodevjapan/godd-13a13e1", repositoryID: 1283948432, task: 2850, spec: "D:/CC/Tools/todo/specs/2850-cdf16525f6ab44b1a3f5e6d1f9337359.md", specSHA: "207951d442f732e489326ef92819c3ee2674bdb38b64605d3043e50e198a0d95"},
}

func currentProductQualification(product currentProduct) bool {
	parts := strings.Split(product.repository, "/")
	if product.task <= 0 || product.repositoryID <= 0 || len(parts) != 2 || !strings.EqualFold(parts[0], product.owner) {
		return false
	}
	db, err := readObject(productStore, 64<<20)
	if err != nil {
		return false
	}
	return currentQualification(db, product.project, product.task, product.root, product.spec, product.specSHA, legacyMatch)
}

func currentSuccessionReceipt(db object, product currentProduct) object {
	return currentSuccessionReceiptWith(db, product, time.Now(), legacyMatch, currentPlain, sourceOriginVerified, currentGitHubIdentityVerified)
}

func currentSuccessionReceiptWith(db object, product currentProduct, now time.Time, protected func(string) bool, plain func(string, bool) bool, origin func(string, string) bool, identity func(currentProduct) bool) object {
	failure := object{"verified": false}
	parts := strings.Split(product.repository, "/")
	if !finiteProductScope(now) || product.task <= 0 || product.repositoryID <= 0 || product.project == "" || product.root == "" || product.spec == "" || len(parts) != 2 || !strings.EqualFold(parts[0], product.owner) {
		return failure
	}
	if !currentQualification(db, product.project, product.task, product.root, product.spec, product.specSHA, protected) || !plain(product.root, true) || !origin(product.root, product.repository) || !identity(product) {
		return failure
	}
	registeredHash, ok := currentPathHash(str(obj(db["projects"])[product.project]))
	if !ok {
		return failure
	}
	row := itemAt(db, product.task)
	if row == nil {
		return failure
	}
	specPathHash, ok := currentPathHash(str(row["spec"]))
	if !ok {
		return failure
	}
	return object{
		"domain":                      "current-product-succession-v1",
		"verified":                    true,
		"origin_todo_id":              2834,
		"todo_id":                     product.task,
		"project":                     product.project,
		"registered_path_sha256":      registeredHash,
		"spec_path_sha256":            specPathHash,
		"spec_sha256":                 product.specSHA,
		"original_registry_recovered": false,
	}
}

func currentGitHubIdentityVerified(product currentProduct) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, currentGitHubCLI, currentGitHubIdentityArgs(product)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	// User profile/OAuth and proxy configuration are trusted; token overrides
	// and host selection are filtered before launching the CLI.
	cmd.Env = currentGitHubCLIEnvironment(os.Environ())
	output, err := cmd.Output()
	return err == nil && currentGitHubIdentityMatches(output, product)
}

func currentGitHubCLIEnvironment(environ []string) []string {
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if ok && (strings.EqualFold(name, "GH_TOKEN") || strings.EqualFold(name, "GITHUB_TOKEN") || strings.EqualFold(name, "GH_ENTERPRISE_TOKEN") || strings.EqualFold(name, "GITHUB_ENTERPRISE_TOKEN") || strings.EqualFold(name, "GH_HOST")) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func currentGitHubIdentityArgs(product currentProduct) []string {
	return []string{"api", "--hostname", "github.com", "repos/" + product.repository, "--jq", "[.id, .full_name, .owner.login] | @tsv"}
}

func currentGitHubIdentityMatches(output []byte, product currentProduct) bool {
	fields := strings.Split(strings.TrimSpace(string(output)), "\t")
	if len(fields) != 3 {
		return false
	}
	id, err := strconv.ParseInt(fields[0], 10, 64)
	return err == nil && id == product.repositoryID && strings.EqualFold(fields[1], product.repository) && strings.EqualFold(fields[2], product.owner)
}

func currentProductByTask(task int) (currentProduct, bool) {
	for _, product := range currentProducts {
		if product.task == task {
			return product, true
		}
	}
	return currentProduct{}, false
}

func currentPathHash(path string) (string, bool) {
	if !filepath.IsAbs(path) {
		return "", false
	}
	slashPath := strings.ReplaceAll(path, `\`, "/")
	lowerSlashPath := strings.ToLower(slashPath)
	if strings.HasPrefix(lowerSlashPath, "//?/") || strings.HasPrefix(lowerSlashPath, "//./") {
		return "", false
	}
	canonical := strings.TrimRight(slashPath, "/")
	cleaned := strings.TrimRight(strings.ReplaceAll(filepath.Clean(path), `\`, "/"), "/")
	if !strings.EqualFold(cleaned, canonical) {
		return "", false
	}
	for _, char := range canonical {
		if char > 0x7f {
			return "", false
		}
	}
	return hashBytes([]byte(strings.ToLower(canonical))), true
}

func currentProductsScopePathAllowed(path string, products []currentProduct, qualifies func(currentProduct) bool, protected func(string) bool) bool {
	for _, product := range products {
		if !within(product.root, path) && !within(product.root+"-wt", path) {
			continue
		}
		if qualifies(product) && currentProductPath(path, product.root, product.repository, strconv.Itoa(product.task), protected) {
			return true
		}
	}
	return false
}

type currentProductPathVerifier func(path, root, repository, task string, protected func(string) bool) bool

// currentSubworkTaskInWorkspace extracts only a direct scheduler workspace
// directory name. The full root, repository, branch and common-dir checks are
// still performed after the saved subwork relation has been verified.
func currentSubworkTaskInWorkspace(path, container string) (int, bool) {
	rel, err := filepath.Rel(filepath.Clean(container), filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return 0, false
	}
	child := strings.Split(rel, string(os.PathSeparator))[0]
	match := currentSubworkWorkspaceName.FindStringSubmatch(child)
	if len(match) != 2 {
		return 0, false
	}
	task, err := strconv.Atoi(match[1])
	return task, err == nil && task > 0
}

// currentSubworkScopePathAllowedWith admits a child worktree only when the
// caller can prove that the child is a direct, currently qualified subwork of
// one of the finite current product declarations.
func currentSubworkScopePathAllowedWith(path string, products []currentProduct, verify func(int) (currentProduct, bool), protected func(string) bool, pathAllowed currentProductPathVerifier) bool {
	for _, parent := range products {
		container := filepath.Clean(parent.root + "-wt")
		if !within(container, path) {
			continue
		}
		task, ok := currentSubworkTaskInWorkspace(path, container)
		if !ok {
			return false
		}
		qualifiedParent, ok := verify(task)
		if !ok || qualifiedParent.task != parent.task || qualifiedParent.repository != parent.repository || !same(qualifiedParent.root, parent.root) {
			return false
		}
		return pathAllowed(path, parent.root, parent.repository, strconv.Itoa(task), protected)
	}
	return false
}

// currentSubworkSpecPath limits child work to the immutable spec copy created
// by todo.exe for that exact new ID. It never trusts a caller-supplied manifest.
func currentSubworkSpecPath(path string, task int, specsRoot string, protected func(string) bool, plain func(string, bool) bool) bool {
	if task <= 0 || !filepath.IsAbs(path) || !same(filepath.Dir(filepath.Clean(path)), specsRoot) || protected(path) || !plain(path, false) {
		return false
	}
	name := filepath.Base(path)
	match := currentSubworkSpecName.FindStringSubmatch(name)
	return len(match) == 3 && match[1] == strconv.Itoa(task)
}

// currentSubworkReceiptWith verifies one direct child of a fixed current
// product. It accepts only the ordinary research/check/review relation saved
// by todo.exe, pins the parent job to the parent's recorded job list, and
// requires the child's registered project to resolve to the parent's exact
// repository root and common Git directory. The returned receipt names the
// child itself so Rust can compare its own item and spec bytes.
func currentSubworkReceiptWith(db object, task int, products []currentProduct, specsRoot string, now time.Time, protected func(string) bool, plain func(string, bool) bool, origin func(string, string) bool, identity func(currentProduct) bool, common func(string) (string, bool), pathAllowed currentProductPathVerifier) (object, currentProduct, bool) {
	failure := object{"verified": false}
	child := itemAt(db, task)
	if child == nil {
		return failure, currentProduct{}, false
	}
	dispatchable, dispatchableOK := child["dispatchable"].(bool)
	if task <= 2407 || num(child["id"]) != task || !dispatchableOK || !dispatchable {
		return failure, currentProduct{}, false
	}
	if status := str(child["status"]); status != "todo" && status != "doing" && status != "wait" {
		return failure, currentProduct{}, false
	}
	relation := obj(child["subwork"])
	parentTask := num(relation["parent_todo_id"])
	var parent currentProduct
	parentFound := false
	for _, declared := range products {
		if declared.task == parentTask {
			parent, parentFound = declared, true
			break
		}
	}
	if !parentFound || task <= parent.task || !finiteProductScope(now) {
		return failure, currentProduct{}, false
	}
	parentRow := itemAt(db, parent.task)
	originJob := str(relation["origin_job_id"])
	kind := str(relation["kind"])
	if parentRow == nil || (str(parentRow["status"]) != "doing" && str(parentRow["status"]) != "wait") || parentRow["subwork"] != nil ||
		!hasJob(parentRow, originJob) || !currentSubworkJobName.MatchString(originJob) ||
		(kind != "research" && kind != "check" && kind != "review") {
		return failure, currentProduct{}, false
	}
	jobMatch := currentSubworkJobName.FindStringSubmatch(originJob)
	if len(jobMatch) != 3 || jobMatch[2] != strconv.Itoa(parent.task) {
		return failure, currentProduct{}, false
	}
	if !currentQualification(db, parent.project, parent.task, parent.root, parent.spec, parent.specSHA, protected) ||
		!plain(parent.root, true) || !origin(parent.root, parent.repository) || !identity(parent) {
		return failure, currentProduct{}, false
	}
	childProject := str(child["project"])
	registeredRoot := str(obj(db["projects"])[childProject])
	if childProject != parent.project || !same(registeredRoot, parent.root) {
		return failure, currentProduct{}, false
	}
	spec := str(child["spec"])
	if !currentSubworkSpecPath(spec, task, specsRoot, protected, plain) {
		return failure, currentProduct{}, false
	}
	body, err := readBounded(spec, 1<<20)
	if err != nil {
		return failure, currentProduct{}, false
	}
	owner := obj(relation["owner"])
	ownerWorktree := str(owner["worktree"])
	parentWorktree := str(obj(parentRow["scheduler"])["worktree"])
	if !strings.EqualFold(str(owner["repository"]), parent.repository) || ownerWorktree == "" ||
		parentWorktree == "" || !same(ownerWorktree, parentWorktree) ||
		!pathAllowed(ownerWorktree, parent.root, parent.repository, strconv.Itoa(parent.task), protected) {
		return failure, currentProduct{}, false
	}
	commonRoot, ok := common(parent.root)
	if !ok || commonRoot == "" || !same(str(owner["git_common"]), commonRoot) {
		return failure, currentProduct{}, false
	}
	branch, ok := currentGit(ownerWorktree, "branch", "--show-current")
	if !ok || branch == "" || branch != str(owner["branch"]) {
		return failure, currentProduct{}, false
	}
	if childWorktree := str(obj(child["scheduler"])["worktree"]); childWorktree != "" &&
		!pathAllowed(childWorktree, parent.root, parent.repository, strconv.Itoa(task), protected) {
		return failure, currentProduct{}, false
	}
	childProduct := parent
	childProduct.project = childProject
	childProduct.task = task
	childProduct.spec = spec
	childProduct.specSHA = hashBytes(body)
	// The parent receipt already verified the exact owner/repository ID. The
	// child inherits that identity only; it cannot select another repository.
	childIdentity := func(candidate currentProduct) bool {
		return candidate.repositoryID == parent.repositoryID && strings.EqualFold(candidate.owner, parent.owner) && strings.EqualFold(candidate.repository, parent.repository)
	}
	receipt := currentSuccessionReceiptWith(db, childProduct, now, protected, plain, origin, childIdentity)
	if receipt["verified"] != true {
		return failure, currentProduct{}, false
	}
	return receipt, parent, true
}

func currentSubworkReceipt(db object, task int) (object, currentProduct, bool) {
	return currentSubworkReceiptWith(db, task, currentProducts, currentTodoSpecsRoot, time.Now(), legacyMatch, currentPlain, sourceOriginVerified, currentGitHubIdentityVerified, currentGitCommonDir, currentProductPath)
}

func currentGitCommonDir(root string) (string, bool) {
	common, ok := currentGit(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	return common, ok && common != ""
}

func currentSubworkScopePathAllowed(path string) bool {
	db, err := readObject(productStore, 64<<20)
	if err != nil {
		return false
	}
	return currentSubworkScopePathAllowedWith(path, currentProducts, func(task int) (currentProduct, bool) {
		_, parent, ok := currentSubworkReceipt(db, task)
		return parent, ok
	}, legacyMatch, currentProductPath)
}

func currentQualification(db object, project string, task int, root, spec, expectedHash string, protected func(string) bool) bool {
	row := itemAt(db, task)
	if row == nil || row["project"] != project || !same(str(obj(db["projects"])[project]), root) || !same(str(row["spec"]), spec) {
		return false
	}
	p, err := physical(spec)
	if err != nil || !same(p, spec) || protected(p) || !currentPlain(spec, false) {
		return false
	}
	b, err := readBounded(p, 1<<20)
	return err == nil && hashBytes(b) == expectedHash
}

func currentGit(root string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err == nil
}

func currentPlain(path string, directory bool) bool {
	info, err := os.Lstat(path)
	if err != nil || !plainEntry(info) || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return false
	}
	p, err := physical(path)
	return err == nil && same(path, p) && currentNoReparse(path)
}

// Recent Go versions do not resolve every Windows junction in EvalSymlinks.
// Inspect each actual ancestor instead of inferring absence of reparse points
// from an unchanged result. This does not traverse directory contents.
func currentNoReparse(path string) bool {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || !plainEntry(info) {
			return false
		}
		if filepath.Dir(p) == p {
			return true
		}
	}
}

func currentProductPath(path, root, repository, task string, protected func(string) bool) bool {
	// Test path membership before Git or private metadata reads. Neither sibling
	// worktrees nor an old actor's original ID are covered by this scope.
	container := filepath.Clean(root + "-wt")
	rel, err := filepath.Rel(container, path)
	child := ""
	if err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		child = strings.Split(rel, string(os.PathSeparator))[0]
	}
	inRoot := within(root, path)
	if !inRoot && !regexp.MustCompile(`^todo-`+regexp.QuoteMeta(task)+`-[A-Za-z0-9_-]+$`).MatchString(child) {
		return false
	}
	if protected(path) || protected(root) || !currentPlain(root, true) || !sourceOriginVerified(root, repository) {
		return false
	}
	if inRoot {
		target, e := physical(path)
		return e == nil && within(root, target) && !protected(target) && currentNoReparse(path)
	}
	// The existing container must not be a junction. A planned child is
	// admitted only at its root, so ordinary scheduler creation can proceed.
	if protected(container) || !currentPlain(container, true) {
		return false
	}
	workspace := filepath.Join(container, child)
	if _, e := os.Lstat(workspace); os.IsNotExist(e) {
		return same(path, workspace)
	}
	if protected(workspace) || !currentPlain(workspace, true) || !sourceOriginVerified(workspace, repository) {
		return false
	}
	branch, ok := currentGit(workspace, "branch", "--show-current")
	if !ok || !strings.HasPrefix(branch, "scheduler/todo-"+task+"-") {
		return false
	}
	common, ok := currentGit(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	other, ok2 := currentGit(workspace, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if !ok || !ok2 || !same(common, other) {
		return false
	}
	target, e := physical(path)
	return e == nil && within(workspace, target) && !protected(target) && currentNoReparse(path)
}

func currentScopePathAllowed(path string) bool {
	if same(path, currentHandoff) {
		return !legacyMatch(path) && currentPlain(path, false) && sourceVerified(base)
	}
	// Only an exact current product/actor declaration qualifies. Existing
	// denials remain authoritative; no lost original row is restored.
	return currentProductsScopePathAllowed(path, currentProducts, currentProductQualification, legacyMatch) || currentSubworkScopePathAllowed(path)
}
