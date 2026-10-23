package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxMetadataBytes bounds each Git metadata file the reader reads itself.
const maxMetadataBytes = 4 << 20

// readSource ties the source root to its repository from bytes the reader
// read itself, the way Git's files backend would: the worktree link and its
// registration, HEAD and the branch it names, the origin in the repository
// configuration and the main ref. It refuses what Git would read in another
// way: an include or URL rewrite, per-worktree configuration, grafts,
// alternates or another ref storage. It returns the object store, which the
// reader then reads itself, and the commit of the main ref.
func (s *session) readSource(p profile) (sourceObjects, string, string) {
	fail := func(reason string) (sourceObjects, string, string) { return sourceObjects{}, "", reason }

	link, reason := s.required(filepath.Join(p.root, ".git"))
	if reason != "" {
		return fail(reason)
	}
	gitDir, ok := gitdirLink(link)
	if !ok || !strings.EqualFold(filepath.Base(filepath.Dir(gitDir)), "worktrees") {
		return fail("source_git")
	}
	common := filepath.Dir(filepath.Dir(gitDir))
	commondir, reason := s.required(filepath.Join(gitDir, "commondir"))
	if reason != "" {
		return fail(reason)
	}
	backlink, reason := s.required(filepath.Join(gitDir, "gitdir"))
	if reason != "" {
		return fail(reason)
	}
	back, ok := strings.CutSuffix(string(backlink), "\n")
	if string(commondir) != "../..\n" || !ok || !strings.EqualFold(filepath.FromSlash(back), filepath.Join(p.root, ".git")) {
		return fail("source_git")
	}

	// Nothing may change what Git would read for this worktree. The reader never
	// reads an alternate store; a store that borrows objects is refused.
	for _, path := range []string{
		filepath.Join(gitDir, "config.worktree"),
		filepath.Join(common, "info", "grafts"),
		filepath.Join(common, "objects", "info", "alternates"),
	} {
		if !s.absentFile(path) {
			return fail("source_git")
		}
	}

	config, reason := s.required(filepath.Join(common, "config"))
	if reason != "" {
		return fail(reason)
	}
	if entries, ok := parseConfig(config); !ok || !sourceConfig(entries, p.origins) {
		return fail("source_git")
	}

	headFile, reason := s.required(filepath.Join(gitDir, "HEAD"))
	if reason != "" {
		return fail(reason)
	}
	branch, head, ok := headTarget(headFile)
	if !ok {
		return fail("source_git")
	}
	refs := &refReader{s: s, common: common}
	if branch != "" {
		if head, reason = refs.resolve(branch); reason != "" {
			return fail(reason)
		}
	}
	if head != p.head {
		return fail("source_git")
	}
	mainID, reason := refs.resolve(p.mainRef)
	if reason != "" {
		return fail(reason)
	}

	shallow, present, reason := s.metadata(filepath.Join(common, "shallow"), false)
	if reason != "" {
		return fail(reason)
	}
	if present && !shallowBoundary(shallow) {
		return fail("source_git")
	}

	objects := filepath.Join(common, "objects")
	if _, err := os.Lstat(objects); err != nil {
		return fail("source_git")
	}
	if !guardClear(s.env, objects) {
		return fail("guard_classification")
	}
	if !s.pinDir(objects) {
		return fail("source_git")
	}
	return sourceObjects{dir: objects, shallow: shallow}, mainID, ""
}

// metadata reads one Git metadata file. The guard classifies it first; then it
// is read once from a plain single-link handle and closed again, so the
// worktrees sharing the repository can keep updating it. The recheck compares
// its identity and digest. A missing optional file is recorded and must stay
// missing; a missing required file refuses the source.
func (s *session) metadata(path string, required bool) ([]byte, bool, string) {
	if _, err := os.Lstat(path); err != nil {
		if !required && os.IsNotExist(err) {
			s.absent = append(s.absent, path)
			return nil, false, ""
		}
		return nil, false, "source_git"
	}
	if !guardClear(s.env, path) {
		return nil, false, "guard_classification"
	}
	f, ok := s.open(path, maxMetadataBytes)
	if !ok {
		return nil, false, "source_git"
	}
	data, sum, ok := s.read(f)
	if !ok {
		f.file.Close()
		return nil, false, "source_git"
	}
	s.release(f, sum)
	return data, true, ""
}

func (s *session) required(path string) ([]byte, string) {
	data, _, reason := s.metadata(path, true)
	return data, reason
}

// refReader resolves a ref as Git's files backend does: a loose file wins,
// otherwise packed-refs. A missing loose file must stay missing.
type refReader struct {
	s      *session
	common string
	packed map[string]string
	loaded bool
}

func (r *refReader) resolve(name string) (string, string) {
	if !refName(name) {
		return "", "source_git"
	}
	data, present, reason := r.s.metadata(filepath.Join(r.common, filepath.FromSlash(name)), false)
	if reason != "" {
		return "", reason
	}
	if present {
		id, ok := strings.CutSuffix(string(data), "\n")
		if !ok || !hexDigest(id, 40) {
			return "", "source_git"
		}
		return id, ""
	}
	if !r.loaded {
		data, present, reason := r.s.metadata(filepath.Join(r.common, "packed-refs"), false)
		if reason != "" {
			return "", reason
		}
		r.loaded = true
		if present {
			refs, ok := packedRefs(data)
			if !ok {
				return "", "source_git"
			}
			r.packed = refs
		}
	}
	id, ok := r.packed[name]
	if !ok {
		return "", "source_git"
	}
	return id, ""
}

// gitdirLink reads a worktree's .git file: "gitdir: " and the absolute path
// of its registration, as Git writes it.
func gitdirLink(data []byte) (string, bool) {
	line, ok := strings.CutSuffix(string(data), "\n")
	value, found := strings.CutPrefix(line, "gitdir: ")
	if !ok || !found {
		return "", false
	}
	dir := filepath.FromSlash(value)
	if len(dir) < 4 || !filepath.IsAbs(dir) || strings.HasPrefix(dir, `\\`) || filepath.Clean(dir) != dir ||
		strings.ContainsFunc(dir[2:], func(r rune) bool { return r < ' ' || strings.ContainsRune(`:*?"<>|`, r) }) {
		return "", false
	}
	return dir, true
}

// headTarget reads a worktree HEAD: a branch under refs/heads or, detached,
// a commit.
func headTarget(data []byte) (branch, commit string, ok bool) {
	line, ok := strings.CutSuffix(string(data), "\n")
	if !ok {
		return "", "", false
	}
	if name, found := strings.CutPrefix(line, "ref: "); found {
		if !strings.HasPrefix(name, "refs/heads/") || !refName(name) {
			return "", "", false
		}
		return name, "", true
	}
	if !hexDigest(line, 40) {
		return "", "", false
	}
	return "", line, true
}

// refName accepts a ref that the files backend stores as a plain path: parts
// of letters, digits, '-', '_' and '.', none starting with a dot or ending in
// a dot or ".lock", and no "..".
func refName(name string) bool {
	if !strings.HasPrefix(name, "refs/") || !validRelPath(name) || strings.Contains(name, "..") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// packedRefs reads packed-refs as Git does (refs/packed-backend.c): an
// optional first line "# pack-refs with: " and its traits, "<id> <name>"
// records and "^<id>" lines that peel the record before them. Git trusts the
// "sorted" trait and finds a name by binary search, so a file that declares it
// must have its names in strictly ascending byte order; without the trait Git
// sorts the records itself. The reader also refuses an unknown trait, a
// repeated name and any other line.
func packedRefs(data []byte) (map[string]string, bool) {
	text, ok := strings.CutSuffix(string(data), "\n")
	if !ok {
		return nil, false
	}
	lines := strings.Split(text, "\n")
	sorted := false
	if strings.HasPrefix(lines[0], "#") {
		traits, found := strings.CutPrefix(lines[0], "# pack-refs with: ")
		if !found {
			return nil, false
		}
		for _, trait := range strings.Split(traits, " ") {
			switch trait {
			case "", "peeled", "fully-peeled":
			case "sorted":
				sorted = true
			default:
				return nil, false
			}
		}
		lines = lines[1:]
	}
	refs := map[string]string{}
	previous := ""
	peelable := false
	for _, line := range lines {
		if id, found := strings.CutPrefix(line, "^"); found {
			if !peelable || !hexDigest(id, 40) {
				return nil, false
			}
			peelable = false
			continue
		}
		id, name, found := strings.Cut(line, " ")
		if !found || !hexDigest(id, 40) || !strings.HasPrefix(name, "refs/") ||
			strings.ContainsFunc(name, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
			return nil, false
		}
		if _, dup := refs[name]; dup {
			return nil, false
		}
		if sorted && previous != "" && name <= previous {
			return nil, false
		}
		refs[name] = id
		previous = name
		peelable = true
	}
	return refs, true
}

// shallowBoundary accepts the shallow file Git writes: one commit per line.
func shallowBoundary(data []byte) bool {
	text, ok := strings.CutSuffix(string(data), "\n")
	if !ok {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		if !hexDigest(line, 40) {
			return false
		}
	}
	return true
}

// configEntry is one line of a Git configuration file: a section header
// (empty key) or a variable. Section and key names are lower case, the
// subsection keeps its case, and the value is the one Git reads.
type configEntry struct {
	section, subsection, key, value string
	hasValue                        bool
}

// parseConfig reads Git's configuration syntax. It accepts a subset of what
// Git accepts and reads every accepted line as Git does. It refuses a
// continuation line, text after a section header, the old [section.sub] form,
// an empty subsection, a variable outside a section, an unknown escape, an
// open quote and any control character other than a tab; refusing only
// narrows what qualifies.
func parseConfig(data []byte) ([]configEntry, bool) {
	text := strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n")
	var entries []configEntry
	section, subsection, inSection := "", "", false
	for _, line := range strings.Split(text, "\n") {
		if strings.ContainsFunc(line, func(r rune) bool { return r < ' ' && r != '\t' || r == 0x7f }) {
			return nil, false
		}
		line = strings.TrimLeft(line, " \t")
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
		case line[0] == '[':
			name, sub, rest, ok := sectionHeader(line[1:])
			if !ok || !blankOrComment(rest) {
				return nil, false
			}
			section, subsection, inSection = name, sub, true
			entries = append(entries, configEntry{section: section, subsection: subsection})
		default:
			key, value, hasValue, ok := configVariable(line)
			if !ok || !inSection {
				return nil, false
			}
			entries = append(entries, configEntry{section, subsection, key, value, hasValue})
		}
	}
	return entries, true
}

func keyChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
}

func blankOrComment(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	return rest == "" || rest[0] == '#' || rest[0] == ';'
}

// sectionHeader parses what follows "[": name] or name "subsection"]. In the
// subsection only the escapes \" and \\ are accepted.
func sectionHeader(s string) (name, sub, rest string, ok bool) {
	i := 0
	for i < len(s) && keyChar(s[i]) {
		i++
	}
	if i == 0 || i == len(s) {
		return "", "", "", false
	}
	name = strings.ToLower(s[:i])
	if s[i] == ']' {
		return name, "", s[i+1:], true
	}
	if s[i] != ' ' && s[i] != '\t' {
		return "", "", "", false
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i == len(s) || s[i] != '"' {
		return "", "", "", false
	}
	var b strings.Builder
	for i++; i < len(s); i++ {
		switch s[i] {
		case '"':
			if b.Len() == 0 || i+1 == len(s) || s[i+1] != ']' {
				return "", "", "", false
			}
			return name, b.String(), s[i+2:], true
		case '\\':
			if i++; i == len(s) || s[i] != '"' && s[i] != '\\' {
				return "", "", "", false
			}
		}
		b.WriteByte(s[i])
	}
	return "", "", "", false
}

// configVariable parses "name", "name = value" or "name=value".
func configVariable(line string) (key, value string, hasValue, ok bool) {
	c := line[0]
	if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return "", "", false, false
	}
	i := 0
	for i < len(line) && keyChar(line[i]) {
		i++
	}
	key = strings.ToLower(line[:i])
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i == len(line) {
		return key, "", false, true
	}
	if line[i] != '=' {
		return "", "", false, false
	}
	value, ok = configValue(line[i+1:])
	return key, value, true, ok
}

// configValue reads a value as Git does: leading blanks are skipped, inner
// blanks kept, trailing blanks and a comment outside quotes dropped, quotes
// removed and the escapes \" \\ \n \t \b decoded.
func configValue(s string) (string, bool) {
	var b strings.Builder
	quote := false
	trim := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c == ' ' || c == '\t') && !quote {
			if b.Len() > 0 {
				if trim < 0 {
					trim = b.Len()
				}
				b.WriteByte(c)
			}
			continue
		}
		if !quote && (c == '#' || c == ';') {
			break
		}
		trim = -1
		switch c {
		case '"':
			quote = !quote
			continue
		case '\\':
			if i++; i == len(s) {
				return "", false
			}
			switch s[i] {
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'n':
				c = '\n'
			case '\\', '"':
				c = s[i]
			default:
				return "", false
			}
		}
		b.WriteByte(c)
	}
	if quote {
		return "", false
	}
	value := b.String()
	if trim >= 0 {
		value = value[:trim]
	}
	return value, true
}

// sourceConfig checks what the configuration means for this repository: the
// origin has exactly one URL and it is a fixed origin; the core keys are those
// of a normal non-bare repository; the only extension is worktreeConfig; and
// no include, conditional include or URL rewrite changes what Git reads.
func sourceConfig(entries []configEntry, origins []string) bool {
	urls := 0
	for _, e := range entries {
		switch e.section {
		case "include", "includeif", "url":
			return false
		case "core":
			if e.subsection != "" || !coreSetting(e) {
				return false
			}
		case "extensions":
			if e.subsection != "" || e.key != "" && (e.key != "worktreeconfig" || !configBool(e)) {
				return false
			}
		case "remote":
			if e.subsection == "origin" && e.key == "url" {
				if !e.hasValue || !slices.Contains(origins, strings.ToLower(e.value)) {
					return false
				}
				urls++
			}
		}
	}
	return urls == 1
}

func coreSetting(e configEntry) bool {
	switch e.key {
	case "", "filemode", "logallrefupdates", "ignorecase", "symlinks", "autocrlf", "longpaths":
		return true
	case "repositoryformatversion":
		return e.hasValue && (e.value == "0" || e.value == "1")
	case "bare":
		return e.hasValue && slices.Contains([]string{"false", "no", "off", "0"}, strings.ToLower(e.value))
	}
	return false
}

func configBool(e configEntry) bool {
	return !e.hasValue || slices.Contains([]string{"true", "false", "yes", "no", "on", "off", "1", "0"}, strings.ToLower(e.value))
}
