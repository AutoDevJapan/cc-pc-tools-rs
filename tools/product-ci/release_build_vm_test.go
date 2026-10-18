package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var (
	stubGuestKernel   = []byte("stub-kernel-bytes")
	stubGuestBusybox  = []byte("stub-busybox-bytes")
	stubGuestExecutor = []byte("stub-executor-bytes")
	stubGuestCerts    = []byte("stub-certificate-bytes")
)

func stubGuestFiles() guestFiles {
	return guestFiles{kernel: stubGuestKernel, busybox: stubGuestBusybox, executor: stubGuestExecutor, certs: stubGuestCerts}
}

// useStubGuestPins makes the four guest pins describe the stub bytes for one
// test, so the real verification code runs on small inputs.
func useStubGuestPins(t *testing.T) {
	t.Helper()
	oldK, oldB, oldE, oldC := guestKernelPin, guestBusyboxPin, guestExecutorPin, guestCertsPin
	t.Cleanup(func() { guestKernelPin, guestBusyboxPin, guestExecutorPin, guestCertsPin = oldK, oldB, oldE, oldC })
	set := func(pin guestFilePin, b []byte) guestFilePin {
		pin.sha256, pin.size = digest(b), int64(len(b))
		return pin
	}
	guestKernelPin, guestBusyboxPin, guestExecutorPin, guestCertsPin = set(oldK, stubGuestKernel), set(oldB, stubGuestBusybox), set(oldE, stubGuestExecutor), set(oldC, stubGuestCerts)
}

// stubGuest stands in for the Docker-based provider in lifecycle tests.
type stubGuest struct {
	mu                 sync.Mutex
	acquired, releases int
	failClass          string
	releaseFails       bool
}

func newStubGuest(t *testing.T) *stubGuest {
	useStubGuestPins(t)
	return &stubGuest{}
}

func (s *stubGuest) acquire(ctx context.Context, b *builderRun) (guestFiles, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquired++
	if s.failClass != "" {
		return guestFiles{}, s.failClass
	}
	return stubGuestFiles(), ""
}

func (s *stubGuest) release(ctx context.Context, b *builderRun) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases++
	return !s.releaseFails
}

func readPayload(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil || h.Typeflag != tar.TypeReg {
			t.Fatalf("payload is not a flat tar of files: %v", err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = b
	}
	return out
}

type cpioEntry struct {
	mode uint32
	data []byte
}

func readInitrdFull(t *testing.T, gzipped []byte) map[string]cpioEntry {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gzipped))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]cpioEntry{}
	pos := 0
	align := func(n int) int { return (n + 3) &^ 3 }
	for {
		if pos+110 > len(raw) || string(raw[pos:pos+6]) != "070701" {
			t.Fatal("bad newc header")
		}
		field := func(i int) int {
			v, err := strconv.ParseUint(string(raw[pos+6+8*i:pos+14+8*i]), 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			return int(v)
		}
		mode, size, nameLen := field(1), field(6), field(11)
		name := string(raw[pos+110 : pos+110+nameLen-1])
		pos = align(pos + 110 + nameLen)
		if name == "TRAILER!!!" {
			if pos != len(raw) {
				t.Fatal("data after the trailer")
			}
			return out
		}
		if _, dup := out[name]; dup {
			t.Fatalf("duplicate archive entry %q", name)
		}
		out[name] = cpioEntry{mode: uint32(mode), data: bytes.Clone(raw[pos : pos+size])}
		pos = align(pos + size)
	}
}

func readInitrd(t *testing.T, gzipped []byte) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for n, e := range readInitrdFull(t, gzipped) {
		out[n] = e.data
	}
	return out
}

func vmTestSpec(t *testing.T) releaseBuilderSpec {
	t.Helper()
	c := synthSnapshot(t, synthFiles(synthCompose))
	in, err := prepareReleaseBuild(c, synthPins(c))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := newReleaseBuilderSpec(productBuilderProfile, in)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestGuestInitrdIsTheClosedGuest(t *testing.T) {
	useStubGuestPins(t)
	spec := vmTestSpec(t)
	nonce := strings.Repeat("0a", 16)
	raw, err := buildInitrd(stubGuestFiles(), spec, nonce)
	if err != nil {
		t.Fatal(err)
	}
	entries := readInitrdFull(t, raw)
	want := map[string][]byte{
		"init":                                 []byte(guestInitScript),
		"kaniko/bin/busybox":                   stubGuestBusybox,
		"kaniko/executor":                      stubGuestExecutor,
		"kaniko/ssl/certs/ca-certificates.crt": stubGuestCerts,
		"kaniko/ctx.tgz":                       mustVerified(t, spec),
		"kaniko/dockerfile":                    []byte(spec.dockerfile + "\n"),
		"kaniko/nonce":                         []byte(nonce + "\n"),
		"kaniko/outmax":                        []byte(strconv.FormatInt(spec.profile.artifactMax, 10) + "\n"),
	}
	for name, data := range want {
		e, ok := entries[name]
		if !ok || !bytes.Equal(e.data, data) || e.mode&0o170000 != 0o100000 {
			t.Fatalf("archive entry %q is not the expected regular file", name)
		}
	}
	if entries["init"].mode&0o111 == 0 || entries["kaniko/executor"].mode&0o111 == 0 || entries["kaniko/ctx.tgz"].mode&0o077 != 0 {
		t.Fatal("unexpected file modes")
	}
	// Everything else is a directory or a symlink to the one BusyBox binary.
	applets := map[string]bool{}
	for _, a := range guestApplets {
		applets["kaniko/bin/"+a] = true
	}
	for name, e := range entries {
		if _, ok := want[name]; ok {
			continue
		}
		switch e.mode & 0o170000 {
		case 0o040000:
		case 0o120000:
			if !applets[name] || string(e.data) != "busybox" {
				t.Fatalf("unexpected symlink %q", name)
			}
			delete(applets, name)
		default:
			t.Fatalf("unexpected archive entry %q", name)
		}
	}
	if len(applets) != 0 {
		t.Fatal("an applet link is missing")
	}
}

func TestGuestInitScriptKeepsTheBuildInsideTheGuest(t *testing.T) {
	s := guestInitScript
	// Kaniko runs in a chroot on the guest disk, never in the RAM root that holds
	// this script, and with the audited, closed set of flags.
	for _, must := range []string{"chroot $R /kaniko/bin/env -i ", "--context=tar://stdin", "--no-push ", "--no-push-cache", "--cache=false", "--tar-path=/out/output.tar", "--log-format=json", "--force ", "< /kaniko/ctx.tgz", "mkfs.ext2 -q -m 0 -N 262144 /dev/vda", "dd if=/work/out/output.tar of=/dev/vdb"} {
		if !strings.Contains(s, must) {
			t.Fatalf("init script lacks %q", must)
		}
	}
	// The container-like /dev has a fixed set of nodes. Neither disk nor the
	// result serial port is reachable from the build.
	for _, forbidden := range []string{"mount --bind /dev", "--rbind", "mknod -m 666 $R/dev/vd", "$R/dev/ttyS", "--push ", "--insecure", "--skip-tls"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("init script contains %q", forbidden)
		}
	}
	if strings.Count(s, "> /dev/ttyS1") != 1 {
		t.Fatal("the result must be written in exactly one place")
	}
	// The only inputs are three files the host wrote after validating them.
	if !strings.Contains(s, "DF=$(cat /kaniko/dockerfile)") || strings.Contains(s, "eval ") {
		t.Fatal("unexpected input handling in the init script")
	}
}

func TestGuestInitrdRejectsUnverifiedFilesAndUnsafeNames(t *testing.T) {
	useStubGuestPins(t)
	spec := vmTestSpec(t)
	nonce := strings.Repeat("0a", 16)
	files := stubGuestFiles()
	files.executor = append(bytes.Clone(files.executor), 'x')
	if _, err := buildInitrd(files, spec, nonce); err == nil {
		t.Fatal("unpinned executor accepted")
	}
	for _, bad := range []string{"", "Dockerfile; poweroff", "Dockerfile $(id)", "../Dockerfile", "Docker file", "Dockerfile\nx", "dockerfile"} {
		s := spec
		s.dockerfile = bad
		if _, err := buildInitrd(stubGuestFiles(), s, nonce); err == nil {
			t.Fatalf("dockerfile name %q accepted", bad)
		}
	}
	for _, bad := range []string{"", "xyz", strings.Repeat("A", 32), strings.Repeat("0a", 16) + "0", "$(id)" + strings.Repeat("0", 27)} {
		if _, err := buildInitrd(stubGuestFiles(), spec, bad); err == nil {
			t.Fatalf("nonce %q accepted", bad)
		}
	}
	s := spec
	s.context = append(bytes.Clone(s.context), 0)
	if _, err := buildInitrd(stubGuestFiles(), s, nonce); err == nil {
		t.Fatal("a context that no longer matches its seal was accepted")
	}
}

func TestGuestPayloadIsKernelAndInitrdOnly(t *testing.T) {
	useStubGuestPins(t)
	spec := vmTestSpec(t)
	raw, err := buildGuestPayload(stubGuestFiles(), spec, strings.Repeat("0a", 16))
	if err != nil {
		t.Fatal(err)
	}
	p := readPayload(t, raw)
	if len(p) != 2 || !bytes.Equal(p["kernel"], stubGuestKernel) || len(p["initrd.gz"]) == 0 {
		t.Fatal("payload is not exactly the kernel and the initrd")
	}
}

func TestGuestPinsAreExactDigests(t *testing.T) {
	for _, ref := range []string{releaseBuilderTool, releaseGuestKernelImage, releaseGuestUserlandImage} {
		i := strings.Index(ref, "@sha256:")
		if i < 0 || len(ref[i+8:]) != 64 || strings.Trim(ref[i+8:], "0123456789abcdef") != "" {
			t.Fatalf("%q is not pinned by a SHA-256 digest", ref)
		}
	}
	for _, pin := range guestPinList() {
		if len(pin.sha256) != 64 || strings.Trim(pin.sha256, "0123456789abcdef") != "" || pin.size <= 0 || pin.path == "" || (pin.image != releaseGuestKernelImage && pin.image != releaseGuestUserlandImage) {
			t.Fatalf("pin %q is malformed", pin.path)
		}
	}
}

func TestOuterScriptIsFullyFormatted(t *testing.T) {
	for _, p := range []releaseBuilderProfile{productBuilderProfile, fixtureBuilderProfile} {
		s := p.outerScript()
		if strings.Contains(s, "%!") || strings.Contains(s, "%d") || strings.Contains(s, "%[") {
			t.Fatal("outer script has an unformatted placeholder")
		}
		for _, must := range []string{"\"%s\",\"memory.max\":\"%s", "$" + "{rc%% *}", "$" + "{rest%% *}", "exit 125", "exit 126", "sha256sum -c -", "head -c \"$size\""} {
			if !strings.Contains(s, must) {
				t.Fatalf("outer script lacks %q", must)
			}
		}
	}
}

func TestOuterProofMustMatchTheProfile(t *testing.T) {
	p := fixtureBuilderProfile
	good := map[string]string{"cpu.max": "150000 100000", "memory.max": strconv.FormatInt(p.memory, 10), "pids.max": "448", "CapEff": "0000000000000000", "NoNewPrivs": "1", "memory.peak": "123456789", "pids.peak": "40"}
	enc := func(m map[string]string) []byte { b, _ := json.Marshal(m); return b }
	if got, err := checkOuterProof(enc(good), p); err != nil || got["cpu.max"] != "150000 100000" {
		t.Fatalf("valid outer proof rejected: %v", err)
	}
	for key, bad := range map[string]string{"cpu.max": "max 100000", "memory.max": "max", "pids.max": "max", "CapEff": "00000000a80425fb", "NoNewPrivs": "0",
		"memory.peak": "n/a", "pids.peak": "449"} {
		wrong := map[string]string{}
		for k, v := range good {
			wrong[k] = v
		}
		wrong[key] = bad
		if _, err := checkOuterProof(enc(wrong), p); !errors.Is(err, errKernelProof) {
			t.Fatalf("%s=%s accepted", key, bad)
		}
	}
	wrong := map[string]string{"extra": "1"}
	for k, v := range good {
		wrong[k] = v
	}
	if _, err := checkOuterProof(enc(wrong), p); err == nil {
		t.Fatal("unexpected extra fact accepted")
	}
	if _, err := checkOuterProof([]byte("junk"), p); err == nil {
		t.Fatal("junk accepted")
	}
}

// ---- the Docker-based provider ---------------------------------------------

type testEntry struct {
	name     string
	typeflag byte
	data     []byte
	link     string
}

type testLayer struct {
	entries []testEntry
	gzip    bool
	raw     []byte // Replaces the whole layer blob (for unsupported formats).
}

func file(name string, data []byte) testEntry {
	return testEntry{name: name, typeflag: tar.TypeReg, data: data}
}

// savedImage builds what "docker image save" prints: layer blobs, an OCI index
// and a manifest.json, with the manifest before or after the blobs.
func savedImage(t *testing.T, layers []testLayer, manifestFirst bool, dupLayer bool) []byte {
	t.Helper()
	type blob struct {
		name string
		data []byte
	}
	var blobs []blob
	var names []string
	for _, l := range layers {
		data := l.raw
		if data == nil {
			var lt bytes.Buffer
			w := tar.NewWriter(&lt)
			for _, e := range l.entries {
				h := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: e.typeflag, Size: int64(len(e.data)), Linkname: e.link}
				if e.typeflag != tar.TypeReg {
					h.Size = 0
				}
				if w.WriteHeader(h) != nil {
					t.Fatal("layer header")
				}
				if e.typeflag == tar.TypeReg {
					w.Write(e.data)
				}
			}
			w.Close()
			data = lt.Bytes()
			if l.gzip {
				var g bytes.Buffer
				z := gzip.NewWriter(&g)
				z.Write(data)
				z.Close()
				data = g.Bytes()
			}
		}
		n := "blobs/sha256/" + digest(data)
		blobs = append(blobs, blob{n, data})
		names = append(names, n)
	}
	if dupLayer && len(names) > 0 {
		names = append(names, names[0])
	}
	manifest, _ := json.Marshal([]map[string]any{{"Config": "blobs/sha256/" + digest([]byte("cfg")), "Layers": names}})
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	add := func(n string, b []byte) {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o444, Size: int64(len(b)), Typeflag: tar.TypeReg})
		tw.Write(b)
	}
	if manifestFirst {
		add("manifest.json", manifest)
	}
	add("oci-layout", []byte("{}"))
	for _, b := range blobs {
		add(b.name, b.data)
	}
	add("index.json", []byte("{}"))
	if !manifestFirst {
		add("manifest.json", manifest)
	}
	tw.Close()
	return out.Bytes()
}

func stubKernelImage(t *testing.T) []byte {
	return savedImage(t, []testLayer{{entries: []testEntry{file("kernel", stubGuestKernel)}, gzip: true}}, false, false)
}

func stubUserlandImage(t *testing.T, mutate func(*[]testLayer)) []byte {
	layers := []testLayer{
		{entries: []testEntry{file("busybox/[", stubGuestBusybox), {name: "busybox/busybox", typeflag: tar.TypeLink, link: "busybox/["}}, gzip: true},
		{entries: []testEntry{file("kaniko/executor", stubGuestExecutor), file("kaniko/ssl/certs/ca-certificates.crt", stubGuestCerts)}},
	}
	if mutate != nil {
		mutate(&layers)
	}
	return savedImage(t, layers, false, false)
}

type guestDockerFake struct {
	t         *testing.T
	mu        sync.Mutex
	images    map[string]bool
	digests   map[string][]string
	archives  map[string][]byte
	calls     []string
	pullFails bool
	rmFails   bool
}

func newGuestDockerFake(t *testing.T) *guestDockerFake {
	useStubGuestPins(t)
	return &guestDockerFake{t: t, images: map[string]bool{}, digests: map[string][]string{}, archives: map[string][]byte{
		releaseGuestKernelImage:   stubKernelImage(t),
		releaseGuestUserlandImage: stubUserlandImage(t, nil),
	}}
}

func (f *guestDockerFake) run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := args[0]
	if name == "image" {
		name += " " + args[1]
	}
	f.calls = append(f.calls, name)
	last := args[len(args)-1]
	switch name {
	case "image inspect":
		if !f.images[last] {
			io.WriteString(stderr, "Error: No such image: x\n")
			return errors.New("exit status 1")
		}
		if strings.Contains(strings.Join(args, " "), "RepoDigests") {
			d := f.digests[last]
			if d == nil {
				d = []string{last}
			}
			raw, _ := json.Marshal(d)
			stdout.Write(raw)
		} else {
			fmt.Fprintf(stdout, "sha256:%064d\n", 5)
		}
	case "pull":
		if f.pullFails {
			return errors.New("exit status 1")
		}
		f.images[last] = true
	case "image rm":
		if f.rmFails {
			return errors.New("exit status 1")
		}
		delete(f.images, last)
	case "image save":
		if !f.images[last] || f.archives[last] == nil {
			return errors.New("exit status 1")
		}
		_, err := stdout.Write(f.archives[last])
		return err
	default:
		f.t.Fatalf("unexpected docker command %q (no container is ever created)", name)
	}
	return nil
}

func (f *guestDockerFake) providerRun(t *testing.T) (*builderRun, guestFiles, string) {
	t.Helper()
	b := &builderRun{parent: context.Background(), d: f, dir: t.TempDir(), rec: releaseBuildReceipt{Nonce: strings.Repeat("0a", 16)}}
	files, class := dockerGuestProvider{}.acquire(context.Background(), b)
	return b, files, class
}

func TestDockerGuestProviderIntroducesAndRemovesWhatItPulled(t *testing.T) {
	f := newGuestDockerFake(t)
	b, files, class := f.providerRun(t)
	if class != "" || !files.verify() {
		t.Fatalf("acquire failed: %s", class)
	}
	if len(b.rec.GuestImages) != 2 || !b.rec.GuestImages[0].Introduced || !b.rec.GuestImages[1].Introduced {
		t.Fatal("introduced images were not recorded")
	}
	if !f.images[releaseGuestKernelImage] || !f.images[releaseGuestUserlandImage] {
		t.Fatal("images missing before release")
	}
	// The record was durable before anything could be pulled.
	raw, err := os.ReadFile(filepath.Join(b.dir, releaseBuildStateFile))
	var saved releaseBuildReceipt
	if err != nil || json.Unmarshal(raw, &saved) != nil || len(saved.GuestImages) != 2 {
		t.Fatal("guest images are not in the saved state")
	}
	if !(dockerGuestProvider{}).release(context.Background(), b) || f.images[releaseGuestKernelImage] || f.images[releaseGuestUserlandImage] || !b.rec.GuestImages[0].Removed || !b.rec.GuestImages[1].Removed {
		t.Fatal("introduced images were not removed")
	}
	// Every image is read twice (manifest, then layers); no container is involved.
	saves := 0
	for _, c := range f.calls {
		if c == "image save" {
			saves++
		}
	}
	if saves != 4 {
		t.Fatalf("unexpected number of saves: %d", saves)
	}
}

func TestDockerGuestProviderNeverRemovesAPreexistingImage(t *testing.T) {
	f := newGuestDockerFake(t)
	f.images[releaseGuestKernelImage], f.images[releaseGuestUserlandImage] = true, true
	b, _, class := f.providerRun(t)
	if class != "" {
		t.Fatal(class)
	}
	if !(dockerGuestProvider{}).release(context.Background(), b) || !f.images[releaseGuestKernelImage] || !f.images[releaseGuestUserlandImage] {
		t.Fatal("a preexisting image was touched")
	}
	for _, c := range f.calls {
		if c == "pull" || c == "image rm" {
			t.Fatalf("unexpected %s for images that were already present", c)
		}
	}
}

func TestDockerGuestProviderRejectsWrongBytesAndWrongDigests(t *testing.T) {
	// A file whose bytes are not the pinned bytes is never accepted.
	f := newGuestDockerFake(t)
	f.archives[releaseGuestUserlandImage] = stubUserlandImage(t, func(l *[]testLayer) {
		(*l)[1].entries[0] = file("kaniko/executor", append(bytes.Clone(stubGuestExecutor), 'x'))
	})
	b, files, class := f.providerRun(t)
	if class == "" || files.verify() {
		t.Fatal("tampered executor accepted")
	}
	if !(dockerGuestProvider{}).release(context.Background(), b) || len(f.images) != 0 {
		t.Fatal("a rejected source was not cleaned up")
	}
	// The same bytes under an image whose digest is not the pinned one.
	f = newGuestDockerFake(t)
	f.digests[releaseGuestKernelImage] = []string{"linuxkit/kernel@sha256:" + strings.Repeat("0", 64)}
	b, _, class = f.providerRun(t)
	if class != "guest-source-mismatch" {
		t.Fatalf("wrong image digest accepted: %s", class)
	}
	(dockerGuestProvider{}).release(context.Background(), b)
	if len(f.images) != 0 {
		t.Fatal("leftovers after a digest mismatch")
	}
}

func TestDockerGuestProviderReportsAFailedPullAndAFailedRemoval(t *testing.T) {
	f := newGuestDockerFake(t)
	f.pullFails = true
	if _, _, class := f.providerRun(t); class != "guest-source-unavailable" {
		t.Fatalf("pull failure: %s", class)
	}
	f = newGuestDockerFake(t)
	b, _, class := f.providerRun(t)
	if class != "" {
		t.Fatal(class)
	}
	f.rmFails = true
	if (dockerGuestProvider{}).release(context.Background(), b) {
		t.Fatal("a failed image removal was reported as clean")
	}
}

// What a layered image may legitimately do to the pinned paths, and what must
// be refused.
func TestGuestFilesAreResolvedLikeAContainerFilesystem(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*[]testLayer)
		want   string // "" = accepted
	}{
		{"hard link in either direction", func(l *[]testLayer) {
			(*l)[0].entries = []testEntry{file("busybox/busybox", stubGuestBusybox), {name: "busybox/[", typeflag: tar.TypeLink, link: "busybox/busybox"}}
		}, ""},
		{"later layer replaces the executor", func(l *[]testLayer) {
			*l = append([]testLayer{{entries: []testEntry{file("kaniko/executor", []byte("older"))}}}, *l...)
		}, ""},
		{"later layer replaces it with other bytes", func(l *[]testLayer) {
			*l = append(*l, testLayer{entries: []testEntry{file("kaniko/executor", []byte("newer"))}})
		}, "guest-source-mismatch"},
		{"whiteout deletes the executor", func(l *[]testLayer) {
			*l = append(*l, testLayer{entries: []testEntry{file("kaniko/.wh.executor", nil)}})
		}, "guest-source-mismatch"},
		{"opaque whiteout hides the directory", func(l *[]testLayer) {
			*l = append(*l, testLayer{entries: []testEntry{file("kaniko/.wh..wh..opq", nil)}})
		}, "guest-source-mismatch"},
		{"a pinned path is a symlink", func(l *[]testLayer) {
			*l = append(*l, testLayer{entries: []testEntry{{name: "kaniko/executor", typeflag: tar.TypeSymlink, link: "/bin/x"}}})
		}, "guest-source-unavailable"},
		{"zstd layer", func(l *[]testLayer) {
			*l = append(*l, testLayer{raw: []byte{0x28, 0xb5, 0x2f, 0xfd, 1, 2, 3, 4}})
		}, "guest-source-unavailable"},
		{"missing path", func(l *[]testLayer) {
			(*l)[1].entries = (*l)[1].entries[1:]
		}, "guest-source-mismatch"},
		{"link to a file of another layer", func(l *[]testLayer) {
			(*l)[0].entries = []testEntry{{name: "busybox/busybox", typeflag: tar.TypeLink, link: "busybox/["}}
			*l = append(*l, testLayer{entries: []testEntry{file("busybox/[", stubGuestBusybox)}})
		}, "guest-source-unavailable"},
	}
	for _, c := range cases {
		f := newGuestDockerFake(t)
		f.archives[releaseGuestUserlandImage] = stubUserlandImage(t, c.mutate)
		_, files, class := f.providerRun(t)
		if class != c.want || (c.want == "" && !files.verify()) {
			t.Fatalf("%s: class %q, want %q", c.name, class, c.want)
		}
	}
	// The manifest may be stored before the layers.
	f := newGuestDockerFake(t)
	f.archives[releaseGuestKernelImage] = savedImage(t, []testLayer{{entries: []testEntry{file("kernel", stubGuestKernel)}}}, true, false)
	if _, files, class := f.providerRun(t); class != "" || !files.verify() {
		t.Fatalf("manifest first: %s", class)
	}
	// The same layer listed twice is refused.
	f = newGuestDockerFake(t)
	f.archives[releaseGuestKernelImage] = savedImage(t, []testLayer{{entries: []testEntry{file("kernel", stubGuestKernel)}}}, false, true)
	if _, _, class := f.providerRun(t); class != "guest-source-unavailable" {
		t.Fatalf("duplicate layer: %s", class)
	}
}
