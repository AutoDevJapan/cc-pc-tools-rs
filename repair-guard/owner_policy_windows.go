package main

// The resource classification is separate from permission to run a task.
// Source qualification reads metadata, may request finite identity refresh,
// and never changes an owner stop or the original protection registry.
import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

type ownerPathVerdict struct {
	AccessAllowed bool
	Status        string
	Categories    []string
}

func ownerUnverified() ownerPathVerdict {
	return ownerPathVerdict{Status: "unverified", Categories: []string{}}
}

func ownerProtected(category string) ownerPathVerdict {
	return ownerPathVerdict{Status: "protected", Categories: []string{category}}
}

type ownerLegacyState int

const (
	ownerLegacyAbsent ownerLegacyState = iota
	ownerLegacyClear
	ownerLegacyClient
	ownerLegacyUnavailable
)

// Do not return registry text, even on an error. Absence is not recovery or a
// classification of unknown resources. Keep the old match semantics intact.
func ownerLegacyProtection(path string) ownerLegacyState {
	f, err := os.Open("D:/CC/Tools/client-projects.txt")
	if os.IsNotExist(err) {
		return ownerLegacyAbsent
	}
	if err != nil {
		return ownerLegacyUnavailable
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return ownerLegacyUnavailable
	}
	return ownerLegacyBytes(path, data)
}

func ownerLegacyBytes(path string, data []byte) ownerLegacyState {
	if len(data) > 1024*1024 || !utf8.Valid(data) || strings.TrimSpace(string(data)) == "" {
		return ownerLegacyUnavailable
	}
	normalized := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		entry := strings.ToLower(strings.TrimSpace(line))
		if entry != "" && !strings.HasPrefix(entry, "#") && strings.Contains(normalized, strings.ReplaceAll(entry, `\`, "/")) {
			return ownerLegacyClient
		}
	}
	return ownerLegacyClear
}

func ownerCardPath(path string) bool {
	codeLeaf := false
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".rs", ".ts", ".tsx", ".js", ".jsx", ".html", ".css":
		codeLeaf = true
	}
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		// A directory verdict qualifies the working location, not every child.
		// Repository names below its marker are software metadata, but a marker
		// must not conceal an outside storage location that needs classification.
		if root, ok := ownerRepositoryRoot(path); ok {
			path = filepath.Dir(root)
			codeLeaf = false
		}
	}
	components := strings.Split(strings.ReplaceAll(path, `\`, "/"), "/")
	for index, component := range components {
		if index == len(components)-1 && codeLeaf {
			continue
		}
		var normalizedOK bool
		component, normalizedOK = ownerUnicodeKC(component)
		if !normalizedOK {
			return true // Unknown spelling is not a confirmed protection category.
		}
		component, _, _ = strings.Cut(component, ":")
		component = strings.TrimRight(component, " .")
		name := strings.TrimSuffix(strings.ToLower(component), strings.ToLower(filepath.Ext(component)))
		for {
			switch ext := filepath.Ext(name); ext {
			case ".json", ".csv", ".tsv", ".txt", ".private", ".bak", ".backup":
				name = strings.TrimSuffix(name, ext)
			default:
				goto normalized
			}
		}
	normalized:
		name = strings.NewReplacer("-", "", "_", "", " ", "").Replace(name)
		for _, marker := range []string{"creditcard", "クレジットカード", "クレカ", "カード情報", "カード番号"} {
			if strings.Contains(name, marker) {
				return true
			}
		}
	}
	return false
}

// These paths are not an extra protection category. Their contents cannot be
// qualified as ordinary source by repository ownership and tracking alone.
func ownerNonSourcePath(path string) bool {
	for _, part := range strings.Split(strings.ToLower(strings.ReplaceAll(path, `\`, "/")), "/") {
		if part == ".env" || strings.HasPrefix(part, ".env.") {
			return true
		}
		switch part {
		case ".git", ".ssh", ".aws", ".azure", ".netrc", ".npmrc", ".pypirc", "id_rsa", "id_ed25519", "credentials.json", "auth.json", "tokens.json", "secrets.json", "client-projects.txt":
			return true
		}
	}
	return false
}

type ownerPolicyAccess struct {
	legacy   func(string) ownerLegacyState
	repair   func(string) bool
	source   func(string) ownerPathVerdict
	physical func(string) (string, error)
}

func classifyOwnerPath(path string) ownerPathVerdict {
	return ownerPolicyClassify(path, ownerPolicyAccess{
		legacy: ownerLegacyProtection, repair: repairPathAllowed,
		source: ownerRegisteredSource, physical: ownerPhysicalPath,
	})
}

func ownerPolicyClassify(path string, access ownerPolicyAccess) ownerPathVerdict {
	path, err := portable(path)
	if err != nil || !filepath.IsAbs(path) {
		return ownerUnverified()
	}
	path = filepath.Clean(path)
	// Check lexical and resolved names before admitting any source/receipt.
	paths := []string{path}
	if target, err := access.physical(path); err == nil && !same(path, target) {
		paths = append(paths, target)
	} else if err != nil && !os.IsNotExist(err) {
		return ownerUnverified()
	}
	categories := []string{}
	card, client := false, false
	for _, candidate := range paths {
		card = card || ownerCardPath(candidate)
		client = client || access.legacy(candidate) == ownerLegacyClient
	}
	if client {
		categories = append(categories, "client_project")
	}
	if len(categories) > 0 {
		return ownerPathVerdict{Status: "protected", Categories: categories}
	}
	// A filename is not evidence that real owner card information exists.
	// Keep suspected data unknown and unread, without manufacturing a category
	// or treating official test data as confirmed real card information.
	if card {
		return ownerUnverified()
	}
	for _, candidate := range paths {
		if ownerWindowsAlias(candidate) || access.legacy(candidate) == ownerLegacyUnavailable {
			return ownerUnverified()
		}
	}
	if access.repair(path) {
		if len(paths) > 1 && !access.repair(paths[1]) {
			return ownerUnverified()
		}
		return ownerPathVerdict{AccessAllowed: true, Status: "clear", Categories: []string{}}
	}
	return access.source(path)
}

func repairOnlyAllowed(path string) bool {
	return ownerPolicyClassify(path, ownerPolicyAccess{legacy: ownerLegacyProtection, repair: repairPathAllowed, source: func(string) ownerPathVerdict { return ownerUnverified() }, physical: ownerPhysicalPath}).AccessAllowed
}

func ownerWindowsAlias(path string) bool {
	// ADS and Win32 trailing-dot/space aliases do not identify an ordinary
	// tracked source path. The card check above still recognizes card aliases.
	relative := strings.TrimPrefix(path, filepath.VolumeName(path))
	for _, part := range strings.Split(strings.ReplaceAll(relative, `\`, "/"), "/") {
		if strings.Contains(part, ":") || strings.TrimRight(part, " .") != part {
			return true
		}
	}
	return false
}

type ownerSourceAccess struct {
	git        func(string, ...string) (string, bool)
	identity   func(string) (int64, string, string, bool)
	plain      func(string, bool) bool
	denied     func(string) bool
	membership func(string, string) string
	stable     func() bool
}

// The owner's normal department catalogue supplies positive membership. Hashes
// avoid copying contracted product names into the guard's public source.
//
//go:generate go run ./internal/cataloggen
//go:embed owner_catalog.json
var ownerDepartmentCatalog []byte

func ownerProductHash(value string) string {
	value = strings.TrimPrefix(strings.ToLower(value), "resume-")
	return hashBytes([]byte(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)))
}

func ownerDepartmentMembership(project, repository string) string {
	return ownerMembershipWith(project, repository, ownerDepartmentCatalog)
}

func ownerMembershipWith(project, repository string, catalog []byte) string {
	var data struct {
		Domain string `json:"domain"`
		Groups []struct {
			Kind    string   `json:"kind"`
			Aliases []string `json:"aliases"`
		} `json:"groups"`
	}
	if json.Unmarshal(catalog, &data) != nil || data.Domain != "owner-department-classification-v1" {
		return ""
	}
	_, name, ok := strings.Cut(repository, "/")
	if !ok {
		name = repository
	}
	p, r := ownerProductHash(project), ownerProductHash(name)
	matched := ""
	for _, group := range data.Groups {
		projectMatch, repositoryMatch := false, false
		for _, alias := range group.Aliases {
			projectMatch = projectMatch || alias == p
			repositoryMatch = repositoryMatch || alias == r
		}
		if group.Kind == "client_project" && (projectMatch || repositoryMatch) {
			return "client_project"
		}
		if group.Kind == "self" && projectMatch && (repository == "" || repositoryMatch) {
			matched = "self"
		}
	}
	return matched
}

// Only these actual GitHub owners have the owner's self-development policy.
// This contains no ToDo, individual path, branch, recovery window or file list.
func ownerSelfGitHubOwner(owner string) bool {
	return strings.EqualFold(owner, "AutoDevJapan") || strings.EqualFold(owner, "BM1314") || strings.EqualFold(owner, "mashfromband")
}

var ownerRepositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func ownerOriginRepository(origin string) (string, bool) {
	var repository string
	switch {
	case strings.HasPrefix(origin, "https://github.com/"):
		repository = strings.TrimPrefix(origin, "https://github.com/")
	case strings.HasPrefix(origin, "git@github.com:"):
		repository = strings.TrimPrefix(origin, "git@github.com:")
	default:
		return "", false
	}
	repository = strings.TrimSuffix(repository, ".git")
	parts := strings.Split(repository, "/")
	return repository, ownerRepositoryName.MatchString(repository) && len(parts) == 2 && ownerSelfGitHubOwner(parts[0]) && parts[1] != "." && parts[1] != ".."
}

func ownerGitHubIdentityWith(ctx context.Context, repository string) (int64, string, string, bool) {
	cmd := exec.CommandContext(ctx, currentGitHubCLI, "api", "--hostname", "github.com", "repos/"+repository, "--jq", "[.id, .full_name, .owner.login] | @tsv")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 20 * time.Millisecond
	cmd.Env = currentGitHubCLIEnvironment(os.Environ())
	b, err := cmd.Output()
	if err != nil {
		return 0, "", "", false
	}
	fields := strings.Split(strings.TrimSpace(string(b)), "\t")
	if len(fields) != 3 {
		return 0, "", "", false
	}
	id, err := strconv.ParseInt(fields[0], 10, 64)
	return id, fields[1], fields[2], err == nil && id > 0
}

func ownerRegisteredSource(path string) ownerPathVerdict {
	// This is the existing normal internal metadata reader. Never print project
	// names/paths, initialize a missing store, or use registration as permission.
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	refreshRepo := ""
	access := ownerSourceAccess{git: func(root string, args ...string) (string, bool) { return ownerSourceGit(ctx, root, args...) }, identity: func(repo string) (int64, string, string, bool) {
		id, name, owner, ok := ownerCachedIdentity(ctx, repo)
		if !ok || ownerIdentityAging(repo, ownerIdentityHome, time.Now()) {
			refreshRepo = repo
		}
		return id, name, owner, ok
	}, plain: currentPlain, denied: legacyMatch, membership: ownerDepartmentMembership}
	result := ownerSourceQuery(ctx, path, access)
	if refreshRepo != "" && ctx.Err() == nil {
		ownerRequestIdentityRefresh(path, refreshRepo)
	}
	return result
}

func ownerSourceQuery(ctx context.Context, path string, access ownerSourceAccess) ownerPathVerdict {
	var snapshot *ownerProjectSnapshot
	access.stable = func() bool { return snapshot != nil && snapshot.current() }
	return ownerRegisteredSourceAt(ctx, path, func(ctx context.Context) (object, bool) {
		var ok bool
		snapshot, ok = ownerReadProjectSnapshot(ctx, productStore, legacyMatch)
		if !ok {
			return nil, false
		}
		return snapshot.projects, true
	}, access)
}

func ownerRegisteredSourceAt(ctx context.Context, path string, read func(context.Context) (object, bool), access ownerSourceAccess) ownerPathVerdict {
	if !ownerRepositoryCandidate(path) {
		return ownerUnverified()
	}
	projects, ok := read(ctx)
	if !ok {
		return ownerUnverified()
	}
	result := ownerRegisteredSourceWith(path, object{"projects": projects}, access)
	if access.stable == nil || !access.stable() || ctx.Err() != nil {
		return ownerUnverified()
	}
	return result
}

func ownerRegisteredSourceWith(path string, db object, access ownerSourceAccess) ownerPathVerdict {
	info, err := os.Lstat(path)
	if err != nil || !access.plain(path, info.IsDir()) || access.denied(path) || ownerNonSourcePath(path) || ownerCardPath(path) {
		return ownerUnverified()
	}
	projects, ok := db["projects"].(map[string]any)
	if !ok {
		return ownerUnverified()
	}
	// Examine every covering registration before allowing any self group. Map
	// iteration order must not override a contracted department declaration.
	if access.membership != nil {
		for project, entry := range projects {
			root, ok := entry.(string)
			if ok && filepath.IsAbs(root) && (within(root, path) || within(root+"-wt", path)) && access.plain(root, true) && access.membership(project, "") == "client_project" {
				return ownerProtected("client_project")
			}
		}
	}
	for project, entry := range projects {
		root, ok := entry.(string)
		if !ok || !filepath.IsAbs(root) || (!within(root, path) && !within(root+"-wt", path)) {
			continue
		}
		if !access.plain(root, true) || access.denied(root) || ownerNonSourcePath(root) || ownerCardPath(root) {
			continue
		}
		if access.membership == nil {
			continue
		}
		membership := access.membership(project, "")
		if membership == "client_project" {
			return ownerProtected("client_project")
		}
		if membership != "self" {
			continue
		}
		registered, ok := access.git(root, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
		parts := strings.Split(strings.ReplaceAll(registered, "\r\n", "\n"), "\n")
		if !ok || len(parts) != 2 || !same(parts[0], root) {
			continue
		}
		common := parts[1]
		top, ok := ownerRepositoryRoot(path)
		if !ok || !access.plain(top, true) || (!same(top, root) && !within(root+"-wt", top)) || !within(top, path) || access.denied(top) {
			continue
		}
		if !access.plain(common, true) {
			continue
		}
		if !same(root, top) {
			other, ok := access.git(top, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
			otherParts := strings.Split(strings.ReplaceAll(other, "\r\n", "\n"), "\n")
			if !ok || len(otherParts) != 2 || !same(otherParts[0], top) || !same(common, otherParts[1]) {
				continue
			}
		}
		origin, ok := access.git(root, "remote", "get-url", "origin")
		repository, valid := ownerOriginRepository(origin)
		if !ok || !valid {
			continue
		}
		membership = access.membership(project, repository)
		if membership == "client_project" {
			return ownerProtected("client_project")
		}
		if membership != "self" {
			continue
		}
		if !same(path, top) {
			rel, err := filepath.Rel(top, path)
			tracked, ok := access.git(top, "--literal-pathspecs", "ls-files", "--error-unmatch", "--", filepath.ToSlash(rel))
			if err != nil || !ok || tracked == "" {
				continue
			}
		}
		id, actualRepository, owner, verified := access.identity(repository)
		if !verified || id <= 0 || !strings.EqualFold(actualRepository, repository) || !ownerSelfGitHubOwner(owner) || !strings.EqualFold(strings.Split(repository, "/")[0], owner) {
			continue
		}
		if id == 1156778979 {
			return ownerProtected("client_project")
		}
		if access.denied(path) || !access.plain(path, info.IsDir()) {
			return ownerUnverified()
		}
		return ownerPathVerdict{AccessAllowed: true, Status: "clear", Categories: []string{}}
	}
	return ownerUnverified()
}
