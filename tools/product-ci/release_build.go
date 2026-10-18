package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Audited release build input.
//
// This file turns one already-sealed, already-verified release snapshot into
// the only Docker build context the bounded builder may receive. It has no CLI,
// reads no guest checkout, accepts no repository/ref/image/URL/host path, and
// follows no newer head. Names and source bytes stay in RAM; none of the types
// below has an exported field that can reach JSON. It grants no execution
// right: the caller must separately hold the job, source, role, queue and
// budget admission for the single builder phase.

// Exactly one builder tool is supported: the maintained upstream Kaniko release
// v1.28.6 pinned by digest. The archived Google repository is not used.
const releaseBuilderTool = "ghcr.io/osscontainertools/kaniko@sha256:2fa20b0a4b40c7db72b479d57e55c2cf79f2178533b2d3bcbd129d6bd3c087ed"

const (
	releaseBuildContextGzipLimit = 64 << 20
	releaseBuildContextFileLimit = 128 << 20
	releaseBuildFixedDockerfile  = "Dockerfile"
)

// Names are compared by SHA-256 so the audited paths never appear in source or
// receipts. A size of zero means the blob/SHA-256 pair alone binds the bytes.
type releaseBuildFilePin struct {
	pathSHA256, blob, sha256 string
	size                     int
}

type releaseBuildPins struct {
	tree, snapshotSHA256 string
	compose, dockerfile  releaseBuildFilePin
	contextDirSHA256     string
}

// Facts established from the fixed merged snapshot (tree 007b.../input fa0096...):
// the Compose model has exactly one build service, with only context and
// dockerfile keys, and both resolve to the paths below.
var auditedReleaseBuildPins = releaseBuildPins{
	tree:           mergedTree,
	snapshotSHA256: "fa0096ecd620e7453d3671bf2f4982fd2be9b8b43e09bf76f758f8303c847b6b",
	compose: releaseBuildFilePin{
		pathSHA256: "a37d4abc31af69a0ea3dcc8349af3360e7331c659efd3e1845baf2116cfc44f4",
		blob:       "638b8b2e0a0ab60f7ceb42469e0c350192d21cb5",
		sha256:     "d5a064ad6dcc62d0654d5777adfec5dbbdbd4068b129a2c1cfd3591a0a66f797",
	},
	dockerfile: releaseBuildFilePin{
		pathSHA256: "2b953517a283d081ae392ee63728340d262f0e769bcd28edb8700be22e8b2571",
		blob:       "514d16a9e9040ecaccbb52a683eb8f465966643e",
		sha256:     "4b9ea56d35aa825eaded97671ec593a2c396f7ae75613f36da364a5a51cef964",
		size:       639,
	},
	contextDirSHA256: "10e08a419e850eba1ebba18fdd28eb7ec1b7e8baa9bcc3b973e2b8891ec726be",
}

// sealedReleaseBuildInput is produced only by prepareReleaseBuild. The gzip tar
// is the single stdin payload; its digest is checked again before every use.
type sealedReleaseBuildInput struct {
	audited                   bool // Set only by prepareAuditedReleaseBuild, for the fixed pins.
	prepared                  bool
	tree, snapshotSHA256      string
	dockerfile                string
	context                   []byte
	contextSHA256             string
	contextFiles, contextDirs int
	contextFileBytes          int64
}

func (sealedReleaseBuildInput) Format(s fmt.State, _ rune) {
	io.WriteString(s, "<sealed-release-build-input>")
}

// verifiedContext returns a private copy only if it still matches the digest
// recorded when the snapshot was sealed.
func (in sealedReleaseBuildInput) verifiedContext() ([]byte, error) {
	b := bytes.Clone(in.context)
	if !in.prepared || len(b) == 0 || len(b) > releaseBuildContextGzipLimit || digest(b) != in.contextSHA256 {
		return nil, errors.New("sealed release build context changed")
	}
	return b, nil
}

// prepareAuditedReleaseBuild is the only product entry point. There is no way to
// supply another tree, archive digest, Compose file, Dockerfile or context.
func prepareAuditedReleaseBuild(snapshot sealedReleaseContext) (sealedReleaseBuildInput, error) {
	in, err := prepareReleaseBuild(snapshot, auditedReleaseBuildPins)
	if err != nil {
		return sealedReleaseBuildInput{}, err
	}
	in.audited = true
	return in, nil
}

func releaseBuildRejected(reason string) error {
	return errors.New("release build input rejected: " + reason) // Fixed vocabulary; never a name or byte.
}

func uniquePathHash(names []string, want string) (string, bool) {
	found := ""
	count := 0
	for _, n := range names {
		if digest([]byte(n)) == want {
			found = n
			count++
		}
	}
	return found, count == 1
}

func verifyReleaseBuildFile(c sealedReleaseContext, name string, pin releaseBuildFilePin) bool {
	f, ok := c.files[name]
	if !ok || f.mode != "100644" || f.blob != pin.blob || f.sha256 != pin.sha256 || gitBlobHash(f.data) != pin.blob || digest(f.data) != pin.sha256 {
		return false
	}
	return pin.size == 0 || len(f.data) == pin.size
}

var releaseDockerfileName = regexp.MustCompile("^Dockerfile[A-Za-z0-9._-]{0,48}$")

// Internal primitive shared with synthetic fixtures, like sealGitContext. The
// single fixed wrapper above is the only caller that passes the audited pins.
func prepareReleaseBuild(c sealedReleaseContext, pins releaseBuildPins) (sealedReleaseBuildInput, error) {
	if c.tree != pins.tree || c.archiveSHA256 != pins.snapshotSHA256 || len(c.files) == 0 {
		return sealedReleaseBuildInput{}, releaseBuildRejected("snapshot identity")
	}
	// Rebuild the canonical archive from the live RAM files. A changed byte,
	// mode or directory therefore cannot hide behind the recorded digest.
	again, err := c.makeArchive()
	if err != nil || digest(again) != pins.snapshotSHA256 || digest(c.archive) != pins.snapshotSHA256 {
		return sealedReleaseBuildInput{}, releaseBuildRejected("snapshot bytes")
	}
	fileNames := make([]string, 0, len(c.files))
	for n := range c.files {
		fileNames = append(fileNames, n)
	}
	dirNames := make([]string, 0, len(c.directories))
	for n := range c.directories {
		dirNames = append(dirNames, n)
	}
	composePath, ok := uniquePathHash(fileNames, pins.compose.pathSHA256)
	if !ok || !verifyReleaseBuildFile(c, composePath, pins.compose) {
		return sealedReleaseBuildInput{}, releaseBuildRejected("compose file")
	}
	dockerPath, ok := uniquePathHash(fileNames, pins.dockerfile.pathSHA256)
	if !ok || !verifyReleaseBuildFile(c, dockerPath, pins.dockerfile) {
		return sealedReleaseBuildInput{}, releaseBuildRejected("dockerfile")
	}
	contextDir, ok := uniquePathHash(dirNames, pins.contextDirSHA256)
	if !ok || contextDir == "" {
		return sealedReleaseBuildInput{}, releaseBuildRejected("context directory")
	}
	if !strings.HasPrefix(dockerPath, contextDir+"/") {
		return sealedReleaseBuildInput{}, releaseBuildRejected("dockerfile outside context")
	}
	rel := strings.TrimPrefix(dockerPath, contextDir+"/")
	if !releaseDockerfileName.MatchString(rel) {
		return sealedReleaseBuildInput{}, releaseBuildRejected("dockerfile location")
	}
	if err := checkComposeBuild(c.files[composePath].data, composePath, contextDir, dockerPath); err != nil {
		return sealedReleaseBuildInput{}, err
	}
	archive, files, dirs, rawBytes, err := makeBuildContext(c, contextDir)
	if err != nil {
		return sealedReleaseBuildInput{}, err
	}
	in := sealedReleaseBuildInput{prepared: true, tree: c.tree, snapshotSHA256: c.archiveSHA256, dockerfile: rel, context: archive, contextSHA256: digest(archive), contextFiles: files, contextDirs: dirs, contextFileBytes: rawBytes}
	if _, err := in.verifiedContext(); err != nil {
		return sealedReleaseBuildInput{}, err
	}
	return in, nil
}

// The pinned Compose bytes must still describe the only build we are about to
// run: one service, a mapping with exactly context and dockerfile, resolving to
// the audited context directory and Dockerfile. Values never leave this function.
func checkComposeBuild(data []byte, composePath, contextDir, dockerPath string) error {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return releaseBuildRejected("compose structure")
	}
	mapValue := func(m *yaml.Node, key string) (*yaml.Node, int) {
		var found *yaml.Node
		count := 0
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
				found = m.Content[i+1]
				count++
			}
		}
		return found, count
	}
	services, n := mapValue(doc.Content[0], "services")
	if n != 1 || services.Kind != yaml.MappingNode {
		return releaseBuildRejected("compose services")
	}
	var builds []*yaml.Node
	for i := 1; i < len(services.Content); i += 2 {
		svc := services.Content[i]
		if svc.Kind != yaml.MappingNode {
			return releaseBuildRejected("compose service")
		}
		b, count := mapValue(svc, "build")
		if count > 1 {
			return releaseBuildRejected("compose duplicate build")
		}
		if count == 1 {
			builds = append(builds, b)
		}
	}
	if len(builds) != 1 {
		return releaseBuildRejected("compose build count")
	}
	b := builds[0]
	if b.Kind != yaml.MappingNode || b.Anchor != "" || len(b.Content) != 4 {
		return releaseBuildRejected("compose build shape")
	}
	ctxNode, c1 := mapValue(b, "context")
	dfNode, c2 := mapValue(b, "dockerfile")
	if c1 != 1 || c2 != 1 || ctxNode.Kind != yaml.ScalarNode || dfNode.Kind != yaml.ScalarNode || ctxNode.Anchor != "" || dfNode.Anchor != "" {
		return releaseBuildRejected("compose build keys")
	}
	if strings.HasPrefix(ctxNode.Value, "/") || strings.Contains(ctxNode.Value, "://") || strings.HasPrefix(dfNode.Value, "/") || strings.Contains(ctxNode.Value, "\\") || strings.Contains(dfNode.Value, "\\") {
		return releaseBuildRejected("compose build location")
	}
	gotContext := path.Join(path.Dir(composePath), ctxNode.Value)
	gotDockerfile := path.Join(gotContext, dfNode.Value)
	if gotContext != contextDir || gotDockerfile != dockerPath {
		return releaseBuildRejected("compose build mismatch")
	}
	return nil
}

// Copy only the context subtree from the verified RAM bytes. Git has no owner,
// so uid/gid are normalized to 0; every byte and the executable bit remain.
func makeBuildContext(c sealedReleaseContext, contextDir string) ([]byte, int, int, int64, error) {
	prefix := contextDir + "/"
	var names []string
	files, dirs, raw := 0, 0, int64(0)
	for n, f := range c.files {
		if strings.HasPrefix(n, prefix) {
			if gitBlobHash(f.data) != f.blob || digest(f.data) != f.sha256 {
				return nil, 0, 0, 0, releaseBuildRejected("context file changed")
			}
			names = append(names, n)
			files++
			raw += int64(len(f.data))
		}
	}
	for n := range c.directories {
		if strings.HasPrefix(n, prefix) {
			names = append(names, n)
			dirs++
		}
	}
	if files == 0 || raw > releaseBuildContextFileLimit {
		return nil, 0, 0, 0, releaseBuildRejected("context size")
	}
	sort.Strings(names)
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression) // Zero header time/name: reproducible for one Go toolchain.
	tw := tar.NewWriter(gz)
	for _, n := range names {
		rel := strings.TrimPrefix(n, prefix)
		if !releaseSourcePath(rel) {
			return nil, 0, 0, 0, releaseBuildRejected("context path")
		}
		if c.directories[n] {
			if tw.WriteHeader(&tar.Header{Name: rel + "/", Mode: 0755, Typeflag: tar.TypeDir, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}) != nil {
				return nil, 0, 0, 0, releaseBuildRejected("context archive")
			}
			continue
		}
		f := c.files[n]
		mode := int64(0644)
		if f.mode == "100755" {
			mode = 0755
		} else if f.mode != "100644" {
			return nil, 0, 0, 0, releaseBuildRejected("context mode")
		}
		if tw.WriteHeader(&tar.Header{Name: rel, Mode: mode, Size: int64(len(f.data)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}) != nil {
			return nil, 0, 0, 0, releaseBuildRejected("context archive")
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, 0, 0, 0, releaseBuildRejected("context archive")
		}
	}
	if tw.Close() != nil || gz.Close() != nil || buf.Len() > releaseBuildContextGzipLimit {
		return nil, 0, 0, 0, releaseBuildRejected("context archive")
	}
	return buf.Bytes(), files, dirs, raw, nil
}
