package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"testing"
)

type synthFile struct {
	path, data string
	exec       bool
}

// Builds a complete synthetic Git snapshot (tree objects included) so the same
// verification code that handles the real merge runs on arbitrary fixtures.
func synthSnapshot(t *testing.T, files []synthFile) sealedReleaseContext {
	t.Helper()
	dirs := map[string]bool{}
	for _, f := range files {
		for d := path.Dir(f.path); d != "." && d != ""; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	entries := map[string]releaseTreeEntry{}
	for _, f := range files {
		mode := "100644"
		if f.exec {
			mode = "100755"
		}
		entries[f.path] = releaseTreeEntry{f.path, mode, "blob", gitBlobHash([]byte(f.data))}
	}
	var treeID func(dir string) string
	treeID = func(dir string) string {
		type child struct {
			name, mode, sha string
			isTree          bool
		}
		var kids []child
		for p, e := range entries {
			if e.Type == "blob" && path.Dir(p) == dirOrDot(dir) {
				kids = append(kids, child{path.Base(p), e.Mode, e.SHA, false})
			}
		}
		for d := range dirs {
			if path.Dir(d) == dirOrDot(dir) {
				id := treeID(d)
				entries[d] = releaseTreeEntry{d, "040000", "tree", id}
				kids = append(kids, child{path.Base(d), "40000", id, true})
			}
		}
		key := func(c child) string {
			if c.isTree {
				return c.name + "/"
			}
			return c.name
		}
		sort.Slice(kids, func(i, j int) bool { return key(kids[i]) < key(kids[j]) })
		var raw bytes.Buffer
		for _, k := range kids {
			id, _ := hex.DecodeString(k.sha)
			fmt.Fprintf(&raw, "%s %s%c", k.mode, k.name, 0)
			raw.Write(id)
		}
		return gitObjectID("tree", raw.Bytes())
	}
	root := treeID("")
	var reply releaseTreeReply
	reply.SHA = root
	for _, e := range entries {
		reply.Entries = append(reply.Entries, e)
	}
	var headers []tar.Header
	var bodies [][]byte
	headers, bodies = append(headers, tar.Header{Name: "opaque-root/", Typeflag: tar.TypeDir, Mode: 0755}), append(bodies, nil)
	var names []string
	for p := range entries {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		e := entries[p]
		if e.Type == "tree" {
			headers, bodies = append(headers, tar.Header{Name: "opaque-root/" + p + "/", Typeflag: tar.TypeDir, Mode: 0775}), append(bodies, nil)
			continue
		}
		var data string
		for _, f := range files {
			if f.path == p {
				data = f.data
			}
		}
		mode := int64(0664)
		if e.Mode == "100755" {
			mode = 0775
		}
		headers, bodies = append(headers, tar.Header{Name: "opaque-root/" + p, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(data))}), append(bodies, []byte(data))
	}
	c, err := sealGitContext(reply, contextFixtureArchive(t, headers, bodies), root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func dirOrDot(d string) string {
	if d == "" {
		return "."
	}
	return d
}

const synthCompose = "services:\n  db:\n    image: example/db@sha256:0000\n  app:\n    build:\n      context: ./backend\n      dockerfile: Dockerfile.ci\n"

func synthFiles(compose string) []synthFile {
	return []synthFile{
		{"README.md", "outside the build context\n", false},
		{"docker-compose.ci.yml", compose, false},
		{"backend/Dockerfile.ci", "FROM scratch\nCOPY app /app\n", false},
		{"backend/app/main.txt", "opaque-sensitive-application-source\n", false},
		{"backend/app/run.sh", "#!/bin/sh\nexit 0\n", true},
		{"backend/.dockerignore", "*.tmp\n", false},
		{"elsewhere/other.txt", "also outside\n", false},
	}
}

func pinsFor(c sealedReleaseContext, compose, dockerfile, ctxDir string) releaseBuildPins {
	pin := func(p string) releaseBuildFilePin {
		f := c.files[p]
		return releaseBuildFilePin{pathSHA256: digest([]byte(p)), blob: f.blob, sha256: f.sha256, size: len(f.data)}
	}
	return releaseBuildPins{tree: c.tree, snapshotSHA256: c.archiveSHA256, compose: pin(compose), dockerfile: pin(dockerfile), contextDirSHA256: digest([]byte(ctxDir))}
}

func synthPins(c sealedReleaseContext) releaseBuildPins {
	return pinsFor(c, "docker-compose.ci.yml", "backend/Dockerfile.ci", "backend")
}

func TestPrepareReleaseBuildCopiesOnlyTheVerifiedContextSubtree(t *testing.T) {
	c := synthSnapshot(t, synthFiles(synthCompose))
	in, err := prepareReleaseBuild(c, synthPins(c))
	if err != nil {
		t.Fatal(err)
	}
	if in.dockerfile != "Dockerfile.ci" || in.contextFiles != 4 || in.contextDirs != 1 || in.tree != c.tree || in.snapshotSHA256 != c.archiveSHA256 {
		t.Fatalf("unexpected identity: %d files %d dirs", in.contextFiles, in.contextDirs)
	}
	// Later changes to the snapshot cannot alter the already prepared input.
	f := c.files["backend/app/main.txt"]
	f.data[0] ^= 1
	b, err := in.verifiedContext()
	if err != nil || digest(b) != in.contextSHA256 {
		t.Fatal("prepared input depends on the mutable snapshot")
	}
	b[0] ^= 1
	if _, err := in.verifiedContext(); err != nil {
		t.Fatal("verified copy aliases the sealed input")
	}
	gz, err := gzip.NewReader(bytes.NewReader(in.context))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	seen := map[string]string{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil || h.Uid != 0 || h.Gid != 0 || h.Linkname != "" || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) || strings.HasPrefix(h.Name, "backend") || strings.HasPrefix(h.Name, "opaque-root") {
			t.Fatalf("context header rejected: %v", e)
		}
		body, _ := io.ReadAll(tr)
		seen[h.Name] = fmt.Sprintf("%o:%s", h.Mode, body)
	}
	want := map[string]string{
		"Dockerfile.ci": "644:FROM scratch\nCOPY app /app\n",
		"app/":          "755:",
		"app/main.txt":  "644:opaque-sensitive-application-source\n",
		"app/run.sh":    "755:#!/bin/sh\nexit 0\n",
		".dockerignore": "644:*.tmp\n",
	}
	if len(seen) != len(want) {
		t.Fatalf("context members differ: %d", len(seen))
	}
	for name, v := range want {
		if seen[name] != v {
			t.Fatalf("context member %q differs", name)
		}
	}
	for _, shown := range []string{fmt.Sprintf("%v", in), fmt.Sprintf("%+v", in), fmt.Sprintf("%#v", in)} {
		if strings.Contains(shown, "opaque") || strings.Contains(shown, "Dockerfile") || shown != "<sealed-release-build-input>" {
			t.Fatal("diagnostics expose build input")
		}
	}
	if j, _ := json.Marshal(in); string(j) != "{}" {
		t.Fatal("sealed input is serializable")
	}
}

func TestPrepareReleaseBuildRejectsOtherSources(t *testing.T) {
	base := synthFiles(synthCompose)
	c := synthSnapshot(t, base)
	good := synthPins(c)
	with := func(mut func(*releaseBuildPins)) releaseBuildPins { p := good; mut(&p); return p }
	other := "0123456789abcdef0123456789abcdef01234567"
	for name, pins := range map[string]releaseBuildPins{
		"tree":            with(func(p *releaseBuildPins) { p.tree = other }),
		"snapshot sha":    with(func(p *releaseBuildPins) { p.snapshotSHA256 = digest([]byte("x")) }),
		"compose path":    with(func(p *releaseBuildPins) { p.compose.pathSHA256 = digest([]byte("missing")) }),
		"compose bytes":   with(func(p *releaseBuildPins) { p.compose.sha256 = digest([]byte("x")) }),
		"compose blob":    with(func(p *releaseBuildPins) { p.compose.blob = other }),
		"dockerfile path": with(func(p *releaseBuildPins) { p.dockerfile.pathSHA256 = digest([]byte("missing")) }),
		"dockerfile size": with(func(p *releaseBuildPins) { p.dockerfile.size++ }),
		"dockerfile blob": with(func(p *releaseBuildPins) { p.dockerfile.blob = other }),
		"context dir":     with(func(p *releaseBuildPins) { p.contextDirSHA256 = digest([]byte("elsewhere")) }),
		"context missing": with(func(p *releaseBuildPins) { p.contextDirSHA256 = digest([]byte("absent")) }),
		"root context":    with(func(p *releaseBuildPins) { p.contextDirSHA256 = digest([]byte("")) }),
	} {
		if _, err := prepareReleaseBuild(c, pins); err == nil || !strings.HasPrefix(err.Error(), "release build input rejected: ") {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	// A different Dockerfile or context that does match its own pins still has to
	// agree with the pinned Compose model.
	for name, pins := range map[string]releaseBuildPins{
		"dockerfile elsewhere": pinsFor(c, "docker-compose.ci.yml", "README.md", "backend"),
		"dockerfile outside":   pinsFor(c, "docker-compose.ci.yml", "elsewhere/other.txt", "backend"),
		"sibling context":      pinsFor(c, "docker-compose.ci.yml", "backend/Dockerfile.ci", "elsewhere"),
	} {
		if _, err := prepareReleaseBuild(c, pins); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	// Mutated RAM bytes, modes and membership cannot hide behind recorded digests.
	mutations := map[string]func(*sealedReleaseContext){
		"bytes": func(c *sealedReleaseContext) { c.files["backend/app/main.txt"].data[1] ^= 1 },
		"mode": func(c *sealedReleaseContext) {
			f := c.files["backend/app/run.sh"]
			f.mode = "100644"
			c.files["backend/app/run.sh"] = f
		},
		"deleted": func(c *sealedReleaseContext) { delete(c.files, "backend/.dockerignore") },
		"added": func(c *sealedReleaseContext) {
			c.files["backend/extra"] = sealedReleaseFile{"100644", gitBlobHash(nil), digest(nil), nil}
		},
		"archive": func(c *sealedReleaseContext) { c.archive = append(bytes.Clone(c.archive), 0) },
	}
	for name, mutate := range mutations {
		fresh := synthSnapshot(t, base)
		mutate(&fresh)
		if _, err := prepareReleaseBuild(fresh, synthPins(fresh)); err == nil {
			t.Fatalf("mutated snapshot %s accepted", name)
		}
	}
	if _, err := prepareAuditedReleaseBuild(c); err == nil {
		t.Fatal("synthetic snapshot accepted by the audited wrapper")
	}
	if _, ok := uniquePathHash([]string{"a", "b", "a"}, digest([]byte("a"))); ok {
		t.Fatal("multiple matches accepted")
	}
}

func TestPrepareReleaseBuildRejectsUnexpectedComposeModels(t *testing.T) {
	two := synthCompose + "  worker:\n    build:\n      context: ./backend\n      dockerfile: Dockerfile.ci\n"
	for name, compose := range map[string]string{
		"two builds":    two,
		"no build":      "services:\n  db:\n    image: example/db\n",
		"short form":    "services:\n  app:\n    build: ./backend\n",
		"extra key":     strings.Replace(synthCompose, "      dockerfile: Dockerfile.ci\n", "      dockerfile: Dockerfile.ci\n      args:\n        A: b\n", 1),
		"other context": strings.Replace(synthCompose, "./backend", "./elsewhere", 1),
		"other file":    strings.Replace(synthCompose, "Dockerfile.ci", "Dockerfile.other", 1),
		"absolute":      strings.Replace(synthCompose, "./backend", "/backend", 1),
		"remote":        strings.Replace(synthCompose, "./backend", "https://example.invalid/x.git", 1),
		"alias":         "x: &b\n  context: ./backend\n  dockerfile: Dockerfile.ci\nservices:\n  app:\n    build: *b\n",
		"not yaml":      "services: [",
		"list":          "- a\n",
	} {
		c := synthSnapshot(t, synthFiles(compose))
		if _, err := prepareReleaseBuild(c, synthPins(c)); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	for _, name := range []string{"Dockerfile with space", "Dockerfile/../x", "dockerfile", "Dockerfile;rm", "-Dockerfile"} {
		if releaseDockerfileName.MatchString(name) {
			t.Fatalf("unsafe Dockerfile name %q accepted", name)
		}
	}
}

func TestReleasePhaseBudgetsAreFixedAndAdditive(t *testing.T) {
	if releaseBuilderCPUNano+releaseIdleRunnerCPUNano != releasePhaseCPUNano || releaseBuilderMemory+releaseIdleRunnerMemory+releaseHostTransferBudget != releasePhaseMemory || releaseBuilderPids+releaseIdleRunnerPids != releasePhasePids {
		t.Fatal("builder, idle runner and host transfer do not sum to the phase")
	}
	if productBuilderProfile.nanoCPUs != 1_500_000_000 || productBuilderProfile.memory != 6<<30 || productBuilderProfile.pids != 448 {
		t.Fatal("product builder limits changed")
	}
	if f := fixtureBuilderProfile; f.nanoCPUs > 500_000_000 || f.memory > 512<<20 || f.pids > 128 || f.network != "none" || !f.kernelProof {
		t.Fatal("fixture builder exceeds its limits")
	}
	if productBuilderProfile.artifactMax > 256<<20 || fixtureBuilderProfile.artifactMax > productBuilderProfile.artifactMax {
		t.Fatal("artifact limit raised")
	}
	// Largest simultaneously held buffers: sealed context, its verified stdin
	// copy and the exactly-sized artifact. They fit the 1 GiB transfer budget.
	if 2*releaseBuildContextGzipLimit+releaseBuildArtifactLimit > releaseHostTransferBudget {
		t.Fatal("host RAM copies exceed the transfer budget")
	}
}

func TestBuilderCreateArgumentsAreClosed(t *testing.T) {
	for _, p := range []releaseBuilderProfile{productBuilderProfile, fixtureBuilderProfile} {
		spec := releaseBuilderSpec{profile: p, dockerfile: "Dockerfile.ci"}
		args := spec.createArgs(strings.Repeat("ab", 16), "cc-release-build-"+strings.Repeat("ab", 16))
		joined := " " + strings.Join(args, " ") + " "
		for _, forbidden := range []string{" --privileged ", " --cap-add ", " --volume ", " -v ", " --mount ", " --device ", " --pid ", " --ipc ", " --uts ", " --userns ", " --cgroupns ", " --publish ", " -p ", " --add-host ", " --env-file ", " --volumes-from ", " --gpus ", "--force", "--skip-tls", "--insecure", "--push ", "--destination=ghcr", "docker.sock", "host", "seccomp=", "apparmor=", "label=disable"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("forbidden create option %q", forbidden)
			}
		}
		for _, required := range []string{" --cap-drop ALL ", " --security-opt no-new-privileges ", " --no-push ", " --no-push-cache ", " --cache=false ", " --context=tar://stdin ", " --tar-path=/kaniko/output.tar ", " --user 0:0 ", " --init ", " --interactive ", " " + releaseBuilderTool + " ", " --dockerfile=Dockerfile.ci ", " --memory-swap " + fmt.Sprint(p.memory) + " ", " --pids-limit " + fmt.Sprint(p.pids) + " "} {
			if !strings.Contains(joined, required) {
				t.Fatalf("missing create option %q", required)
			}
		}
		for i, a := range args {
			if a == "--env" && !strings.HasPrefix(args[i+1], "GOMAXPROCS=") && !strings.HasPrefix(args[i+1], "GOMEMLIMIT=") {
				t.Fatal("unexpected container environment")
			}
		}
	}
	if !strings.Contains(strings.Join(productBuilderProfile.tmpfs, " "), "nosuid") || strings.Contains(strings.Join(productBuilderProfile.env, " "), "TOKEN") {
		t.Fatal("unexpected profile")
	}
	if releaseBuilderTool != "ghcr.io/osscontainertools/kaniko@sha256:2fa20b0a4b40c7db72b479d57e55c2cf79f2178533b2d3bcbd129d6bd3c087ed" {
		t.Fatal("builder tool digest changed")
	}
}
