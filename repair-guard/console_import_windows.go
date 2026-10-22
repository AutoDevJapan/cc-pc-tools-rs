package main

import (
	"path/filepath"
	"strings"
)

// This exact older Console import is an archive of the original 3264 source,
// not an implementation or replacement of the newer running Console. The
// private repository is AutoDevJapan/local-ai-console, ID1429803672. Its
// current main is owned by 3339 and must not be overwritten with this archive.
const console3264ImportRoot = "C:/Users/makhmeto/AppData/Local/Temp/console-3264-private-import-20261022-0830/repository"
const console3264SourceHead = "9fb62b51c7b4f145efadfbd894224795fbc3157e"

type consoleImportFile struct {
	relative, sourceSHA, candidateSHA, sourceBlob string
}

type consoleImportProfile struct {
	root, head, tree, sourceRoot, sourceHead string
	evidence, sourceInputs                   []consoleEvidence
	files                                    []consoleImportFile
}

var console3264Import = consoleImportProfile{
	root: console3264ImportRoot, head: "82ab222aff65dba36928ea6ea8bd20ea562c94d4",
	tree:       "ddb8bb857badcceb4b105f448da3b241bcd69349",
	sourceRoot: model2903Source, sourceHead: console3264SourceHead,
	evidence: []consoleEvidence{
		{model2903Source, model2903Source + "/.provenance-3264/legacy-3264-to-3081-handoff-final.json", "303907892635bf5170f841f70926ae689ea2581788bac59ca97e9e80d4532b02"},
		{model2903Source, model2903Source + "/.provenance-3264/console-3264-private-import-manifest-20261022.json", "41cc359bed2928699511d61aaefe790e5d3abbf822467f98a0ccca15ebdcd389"},
		{model2903Source, model2903Source + "/.provenance-3264/console-3264-classification-results-20261022.json", "f8a3738234da5c46af04d7b4d72b9e3d51a419b18800d7bd25ec031aa480d8ac"},
		{model2903Source, model2903Source + "/.provenance-3264/console-3264-layout-lock-summary-20261022.json", "32765af89bde9985b6ff9990c499785403395df4892b1c64b4f2113516fcd135"},
		{model2903Source, model2903Source + "/.provenance-3264/console-3264-layout-lock-diff-20261022.json", "499f9a748e4024961865f3f5cc32833a49b0b0f03304bb0fa6e29e1d09dd6a39"},
		{model2903Source, model2903Source + "/.provenance-3264/console-3264-layout-check-result-20261022.json", "2d53ce76ff37b3632922016f8f56e49dc55bfd862be5781ae0cbc3f72ce9b515"},
		{model2903Source, model2903Source + "/.provenance-3264/console-3264-layout-dom-result-20261022.json", "c98453711186a280049ebadd4b8659acf76c79f660b99f8c28ad0bf4053da723"},
	},
	// These original bytes anchor the three generated root settings. The
	// independent layout records above describe resolver3 / the single member,
	// the unchanged registry identities and nine pinned public Git packages,
	// and generated exclusions. They are not supplied by a classification caller.
	sourceInputs: []consoleEvidence{
		{model2903Source, model2903Source + "/Cargo.toml", "85ceffdad72c64902ba086495d785bc6560e7f9f6576f94d534ed5c6f65a4087"},
		{model2903Source, model2903Source + "/Cargo.lock", "437662fd0e4eedf270f5b46f687b47c8d1a94755d059cfcdf8a000b6e81cf1ce"},
		{model2903Source, model2903Source + "/.gitignore", "1efc48e3682925a06562eb95418cba8a85df734ada3f8ea325ebec0c664eeb97"},
	},
	files: console3264ImportFiles,
}

func consoleImport3264Allowed(path string) bool {
	return consoleImportWith(path, console3264Import, legacyMatch, permanentRuntimeSourceVerified, consoleImportGitVerified)
}

func consoleImportGitVerified(profile consoleImportProfile) bool {
	if !civ6Commit.MatchString(profile.head) || !civ6Commit.MatchString(profile.tree) ||
		legacyMatch(profile.root) || !civ6GitRoot(profile.root) {
		return false
	}
	for expression, expected := range map[string]string{"HEAD": profile.head, "HEAD^{tree}": profile.tree} {
		value, err := civ6GitAt(profile.root, "rev-parse", "--verify", expression)
		if err != nil || strings.TrimSpace(string(value)) != expected {
			return false
		}
	}
	parents, err := civ6GitAt(profile.root, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil || strings.TrimSpace(string(parents)) != profile.head {
		return false
	}
	remotes, err := civ6GitAt(profile.root, "remote")
	if err != nil {
		return false
	}
	if names := strings.TrimSpace(string(remotes)); names != "" {
		if names != "origin" {
			return false
		}
		origin, err := civ6GitAt(profile.root, "remote", "get-url", "origin")
		url := strings.ToLower(strings.TrimSpace(string(origin)))
		if err != nil || (url != "https://github.com/autodevjapan/local-ai-console.git" &&
			url != "https://github.com/autodevjapan/local-ai-console" && url != "git@github.com:autodevjapan/local-ai-console.git") {
			return false
		}
	}
	_, err = civ6GitAt(profile.root, "diff", "--quiet", "--no-ext-diff", "--no-textconv", "HEAD", "--")
	return err == nil
}

func consoleImportWith(path string, profile consoleImportProfile, protected, source func(string) bool, lineage func(consoleImportProfile) bool) bool {
	// Match a declared leaf before any evidence or payload read. The parent,
	// Git internals, two held fixtures, aliases and unknown children never match.
	var selected *consoleImportFile
	for i := range profile.files {
		file := &profile.files[i]
		if same(path, filepath.Join(profile.root, file.relative)) {
			selected = file
			break
		}
	}
	if selected == nil || !within(profile.root, path) || same(profile.root, path) ||
		protected(profile.root) || !runtimeLeaf(path, filepath.Dir(path), protected, true) || !lineage(profile) {
		return false
	}
	session := modelPinSession{protected: protected, source: source, head: modelSourceHead}
	defer session.close()
	if !session.sourceRoot(profile.sourceRoot, profile.sourceHead) || len(profile.evidence) == 0 || len(profile.sourceInputs) == 0 {
		return false
	}
	// All independent records belong to the same fixed public source mirror.
	// Keep their handles pinned together, with one final source recheck.
	for _, records := range [][]consoleEvidence{profile.evidence, profile.sourceInputs} {
		for _, record := range records {
			if !same(record.root, profile.sourceRoot) || !within(profile.sourceRoot, record.path) ||
				!modelDigestValid(record.sha256) || same(record.path, path) ||
				!session.match(record.path, []string{record.sha256}) {
				return false
			}
		}
	}
	if selected.sourceSHA != "" {
		original := filepath.Join(profile.sourceRoot, selected.relative)
		if !modelDigestValid(selected.sourceSHA) || !civ6Commit.MatchString(selected.sourceBlob) ||
			!session.match(original, []string{selected.sourceSHA}) {
			return false
		}
		blob, err := civ6GitAt(profile.sourceRoot, "rev-parse", "--verify", "HEAD:"+filepath.ToSlash(selected.relative))
		if err != nil || strings.TrimSpace(string(blob)) != selected.sourceBlob {
			return false
		}
	}
	return modelDigestValid(selected.candidateSHA) && session.match(path, []string{selected.candidateSHA}) &&
		session.recheck() && !protected(profile.root) && lineage(profile)
}
