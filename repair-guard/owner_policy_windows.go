package main

// The resource classification is separate from permission to run a task.
// This file reads metadata only and never changes an owner stop or registry.
import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
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
	for _, component := range strings.Split(strings.ReplaceAll(path, `\`, "/"), "/") {
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
		switch name {
		case "creditcard", "creditcards", "creditcardinfo", "creditcardinformation", "ownercreditcard", "ownercreditcards", "mycreditcard", "mycreditcards", "クレジットカード", "クレジットカード情報", "カード情報", "本人カード情報", "俺のクレジットカード":
			return true
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
		source: ownerRegisteredSource, physical: physical,
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
	}
	for _, candidate := range paths {
		if ownerCardPath(candidate) {
			return ownerProtected("owner_card_information")
		}
		if access.legacy(candidate) == ownerLegacyClient {
			return ownerProtected("client_project")
		}
	}
	for _, candidate := range paths {
		if ownerWindowsAlias(candidate) || ownerNonSourcePath(candidate) || access.legacy(candidate) == ownerLegacyUnavailable {
			return ownerUnverified()
		}
	}
	if access.repair(path) {
		return ownerPathVerdict{AccessAllowed: true, Status: "clear", Categories: []string{}}
	}
	return access.source(path)
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
	git      func(string, ...string) (string, bool)
	identity func(string) (int64, string, string, bool)
	plain    func(string, bool) bool
	denied   func(string) bool
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

func ownerGitHubIdentity(repository string) (int64, string, string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, currentGitHubCLI, "api", "--hostname", "github.com", "repos/"+repository, "--jq", "[.id, .full_name, .owner.login] | @tsv")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
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
	b, ok := conversationBytes(productStore, 64<<20, legacyMatch)
	if !ok {
		return ownerUnverified()
	}
	db, ok := conversationJSON(b)
	if !ok {
		return ownerUnverified()
	}
	result := ownerRegisteredSourceWith(path, db, ownerSourceAccess{git: currentGit, identity: ownerGitHubIdentity, plain: currentPlain, denied: legacyMatch})
	after, ok := conversationBytes(productStore, 64<<20, legacyMatch)
	if !ok || hashBytes(b) != hashBytes(after) {
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
	for _, entry := range projects {
		root, ok := entry.(string)
		if !ok || !filepath.IsAbs(root) || (!within(root, path) && !within(root+"-wt", path)) {
			continue
		}
		if !access.plain(root, true) || access.denied(root) || ownerNonSourcePath(root) || ownerCardPath(root) {
			continue
		}
		registeredTop, ok := access.git(root, "rev-parse", "--show-toplevel")
		if !ok || !same(registeredTop, root) {
			continue
		}
		probe := path
		if !info.IsDir() {
			probe = filepath.Dir(path)
		}
		top, ok := access.git(probe, "rev-parse", "--show-toplevel")
		if !ok || !access.plain(top, true) || (!same(top, root) && !within(root+"-wt", top)) || !within(top, path) || access.denied(top) {
			continue
		}
		common, ok := access.git(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
		other, okOther := access.git(top, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if !ok || !okOther || !same(common, other) || !access.plain(common, true) {
			continue
		}
		origin, ok := access.git(root, "remote", "get-url", "origin")
		otherOrigin, okOther := access.git(top, "remote", "get-url", "origin")
		repository, valid := ownerOriginRepository(origin)
		if !ok || !okOther || !valid || origin != otherOrigin {
			continue
		}
		id, actualRepository, owner, verified := access.identity(repository)
		if !verified || id <= 0 || !strings.EqualFold(actualRepository, repository) || !ownerSelfGitHubOwner(owner) || !strings.EqualFold(strings.Split(repository, "/")[0], owner) {
			continue
		}
		// The owner's fixed product identity policy identifies this repository
		// as contracted work even though its remote has one of the three owners.
		if id == 1156778979 {
			return ownerProtected("client_project")
		}
		if !same(path, top) {
			rel, err := filepath.Rel(top, path)
			tracked, ok := access.git(top, "--literal-pathspecs", "ls-files", "--error-unmatch", "--", filepath.ToSlash(rel))
			if err != nil || !ok || tracked == "" {
				continue
			}
		}
		if access.denied(path) || !access.plain(path, info.IsDir()) {
			return ownerUnverified()
		}
		return ownerPathVerdict{AccessAllowed: true, Status: "clear", Categories: []string{}}
	}
	return ownerUnverified()
}
