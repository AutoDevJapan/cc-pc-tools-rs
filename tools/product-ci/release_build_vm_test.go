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

type fakeSourceContainer struct{ name, image string }

type guestDockerFake struct {
	t          *testing.T
	mu         sync.Mutex
	images     map[string]bool
	digests    map[string][]string
	containers map[string]fakeSourceContainer
	labels     map[string]map[string]string
	files      map[string][]byte // image + "|" + path
	calls      []string
	rmFails    bool
	pullFails  bool
	seq        int
	// loseCreates makes the next creates land only later (see materialize).
	loseCreates int
	pending     []struct {
		id string
		c  fakeSourceContainer
		l  map[string]string
	}
	onCreate func()
}

func newGuestDockerFake(t *testing.T) *guestDockerFake {
	useStubGuestPins(t)
	f := &guestDockerFake{t: t, images: map[string]bool{}, digests: map[string][]string{}, containers: map[string]fakeSourceContainer{}, labels: map[string]map[string]string{}, files: map[string][]byte{}}
	f.files[guestKernelPin.image+"|"+guestKernelPin.path] = stubGuestKernel
	f.files[guestBusyboxPin.image+"|"+guestBusyboxPin.path] = stubGuestBusybox
	f.files[guestExecutorPin.image+"|"+guestExecutorPin.path] = stubGuestExecutor
	f.files[guestCertsPin.image+"|"+guestCertsPin.path] = stubGuestCerts
	return f
}

func (f *guestDockerFake) run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := args[0]
	if name == "image" || name == "container" {
		name += " " + args[1]
	}
	f.calls = append(f.calls, name)
	last := args[len(args)-1]
	switch name {
	case "image inspect":
		ref := last
		if !f.images[ref] {
			io.WriteString(stderr, "Error: No such image: x\n")
			return errors.New("exit status 1")
		}
		if strings.Contains(strings.Join(args, " "), "RepoDigests") {
			d := f.digests[ref]
			if d == nil {
				d = []string{ref}
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
		delete(f.images, last)
	case "create":
		if f.onCreate != nil {
			f.onCreate()
		}
		cname := argValues(args, "--name")[0]
		image := args[len(args)-2]
		if strings.Contains(strings.Join(args, " "), "--pull never") && !f.images[image] {
			io.WriteString(stderr, "Error response from daemon: No such image: x\n")
			return errors.New("exit status 125")
		}
		for _, c := range f.containers {
			if c.name == cname {
				io.WriteString(stderr, "docker: Error response from daemon: Conflict. The container name is already in use.\n")
				return errors.New("exit status 125")
			}
		}
		f.seq++
		id := fmt.Sprintf("%064x", f.seq)
		labels := map[string]string{}
		for _, l := range argValues(args, "--label") {
			k, v, _ := strings.Cut(l, "=")
			labels[k] = v
		}
		if f.loseCreates > 0 {
			f.loseCreates--
			f.pending = append(f.pending, struct {
				id string
				c  fakeSourceContainer
				l  map[string]string
			}{id, fakeSourceContainer{cname, image}, labels})
			return errors.New("response lost")
		}
		f.containers[id] = fakeSourceContainer{cname, image}
		f.labels[id] = labels
		fmt.Fprintln(stdout, id)
	case "cp":
		src, path, _ := strings.Cut(args[1], ":")
		var c fakeSourceContainer
		for _, v := range f.containers {
			if v.name == src {
				c = v
			}
		}
		data, ok := f.files[c.image+"|"+path]
		if !ok {
			return errors.New("exit status 1")
		}
		tw := tar.NewWriter(stdout)
		tw.WriteHeader(&tar.Header{Name: "f", Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		tw.Write(data)
		tw.Close()
	case "container ls":
		want := strings.TrimPrefix(argValues(args, "--filter")[0], "label=")
		k, v, hasValue := strings.Cut(want, "=")
		for id := range f.containers {
			if got, ok := f.labels[id][k]; ok && (!hasValue || got == v) {
				fmt.Fprintln(stdout, id)
			}
		}
	case "container inspect":
		c, ok := f.containers[last]
		if !ok {
			return errors.New("exit status 1")
		}
		raw, _ := json.Marshal(map[string]any{"Id": last, "Name": "/" + c.name, "State": map[string]any{"Status": "created"}, "Config": map[string]any{"Image": c.image, "Labels": f.labels[last]}})
		stdout.Write(raw)
	case "container rm":
		if f.rmFails {
			return errors.New("exit status 1")
		}
		delete(f.containers, last)
	default:
		f.t.Fatalf("unexpected docker command %q", name)
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
	if len(f.containers) != 0 {
		t.Fatal("source containers were left behind")
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
	f.files[guestExecutorPin.image+"|"+guestExecutorPin.path] = append(bytes.Clone(stubGuestExecutor), 'x')
	b, files, class := f.providerRun(t)
	if class == "" || files.verify() {
		t.Fatal("tampered executor accepted")
	}
	if !(dockerGuestProvider{}).release(context.Background(), b) || len(f.containers) != 0 || len(f.images) != 0 {
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
	if len(f.containers) != 0 || len(f.images) != 0 {
		t.Fatal("leftovers after a digest mismatch")
	}
}

func TestDockerGuestProviderReportsAFailedRemoval(t *testing.T) {
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
	// A leftover handle that cannot be removed keeps the phase from being released.
	leftover := strings.Repeat("f", 64)
	f.containers[leftover] = fakeSourceContainer{guestSourceContainerName(b.rec.Nonce, 0), releaseGuestKernelImage}
	f.labels[leftover] = map[string]string{releaseGuestSourceLabel: b.rec.Nonce}
	f.rmFails = true
	if (dockerGuestProvider{}).release(context.Background(), b) {
		t.Fatal("a failed removal was reported as clean")
	}
	// A container that merely carries the label but not our name is never removed.
	f.rmFails = false
	f.containers[leftover] = fakeSourceContainer{"somebody-else", releaseGuestKernelImage}
	if (dockerGuestProvider{}).release(context.Background(), b) || len(f.containers) != 1 {
		t.Fatal("a foreign container was removed or the release was reported clean")
	}
}

// materialize lands the creates whose answers were lost, as a slow daemon would;
// a request whose name is already taken fails with a conflict.
func (f *guestDockerFake) materialize() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.pending {
		taken := false
		for _, c := range f.containers {
			taken = taken || c.name == p.c.name
		}
		if !taken {
			f.containers[p.id], f.labels[p.id] = p.c, p.l
		}
	}
	f.pending = nil
}

func TestDockerGuestProviderClosesAnUncertainSourceCreate(t *testing.T) {
	f := newGuestDockerFake(t)
	f.images[releaseGuestKernelImage], f.images[releaseGuestUserlandImage] = true, true
	f.loseCreates = 1
	b, _, class := f.providerRun(t)
	if class != "guest-source-unavailable" || b.rec.GuestSourceAttempted != 1 || b.rec.GuestSourceConfirmed != 0 {
		t.Fatalf("a lost create answer was not recorded as unconfirmed: %s %d/%d", class, b.rec.GuestSourceAttempted, b.rec.GuestSourceConfirmed)
	}
	// Nothing exists yet, so a plain listing would find nothing to remove. The
	// release takes the name itself first, then removes what holds it.
	creates := 0
	for _, c := range f.calls {
		if c == "create" {
			creates++
		}
	}
	if !(dockerGuestProvider{}).release(context.Background(), b) || b.rec.GuestSourceConfirmed != b.rec.GuestSourceAttempted {
		t.Fatal("an unconfirmed source create was not resolved")
	}
	after := 0
	for _, c := range f.calls {
		if c == "create" {
			after++
		}
	}
	if after != creates+1 || len(f.containers) != 0 {
		t.Fatal("the name was not taken and released again")
	}
	// The late request found the name taken while the hold existed.
	f.materialize()
	// A hold that fails for any reason other than a proof (conflict / missing
	// image) keeps the release refused so the caller keeps the phase.
	g := newGuestDockerFake(t)
	g.images[releaseGuestKernelImage], g.images[releaseGuestUserlandImage] = true, true
	g.loseCreates = 1
	b2, _, _ := g.providerRun(t)
	g.onCreate = func() { g.t.Helper() }
	broken := &failingCreateDocker{guestDockerFake: g}
	b2.d = broken
	if (dockerGuestProvider{}).release(context.Background(), b2) {
		t.Fatal("an unresolved source create was released")
	}
}

type failingCreateDocker struct{ *guestDockerFake }

func (d *failingCreateDocker) run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	if args[0] == "create" {
		io.WriteString(stderr, "Cannot connect to the Docker daemon")
		return errors.New("exit status 1")
	}
	return d.guestDockerFake.run(ctx, stdin, stdout, stderr, args...)
}

func TestSourceCreateAttemptIsDurableBeforeTheDaemonIsAsked(t *testing.T) {
	f := newGuestDockerFake(t)
	b := &builderRun{parent: context.Background(), d: f, dir: t.TempDir(), rec: releaseBuildReceipt{Nonce: strings.Repeat("0a", 16)}}
	checked := 0
	f.onCreate = func() {
		raw, err := os.ReadFile(filepath.Join(b.dir, releaseBuildStateFile))
		var saved releaseBuildReceipt
		if err != nil || json.Unmarshal(raw, &saved) != nil || saved.GuestSourceAttempted != saved.GuestSourceConfirmed+1 {
			t.Error("the source create was not durable before it was sent")
		}
		checked++
	}
	if _, class := (dockerGuestProvider{}).acquire(context.Background(), b); class != "" || checked != 2 {
		t.Fatalf("acquire: %q, %d checks", class, checked)
	}
}

func TestInertGuestSourceLeftoverIsSweptAndOthersKeepThePhaseBusy(t *testing.T) {
	f := newGuestDockerFake(t)
	b := &builderRun{parent: context.Background(), d: f, dir: t.TempDir(), rec: releaseBuildReceipt{Nonce: strings.Repeat("0b", 16)}}
	other := strings.Repeat("c", 32)
	leftover := strings.Repeat("1", 64)
	f.containers[leftover] = fakeSourceContainer{guestSourceContainerName(other, 1), releaseGuestUserlandImage}
	f.labels[leftover] = map[string]string{releaseGuestSourceLabel: other}
	if err := b.sweepInertBuilders(context.Background()); err != nil || len(f.containers) != 0 {
		t.Fatalf("an inert late source handle was not swept: %v", err)
	}
	// A container that only carries the label, or has another image or a
	// different name, is never removed and the phase stays busy.
	foreign := strings.Repeat("2", 64)
	for _, c := range []fakeSourceContainer{{"somebody-else", releaseGuestUserlandImage}, {guestSourceContainerName(other, 1), "other-image"}} {
		f.containers[foreign] = c
		f.labels[foreign] = map[string]string{releaseGuestSourceLabel: other}
		if err := b.sweepInertBuilders(context.Background()); err != nil || len(f.containers) != 1 {
			t.Fatalf("foreign container touched: %v", err)
		}
		if busy, err := b.anyBuilder(context.Background()); err != nil || !busy {
			t.Fatal("a source handle that is not recognized must keep the phase busy")
		}
		delete(f.containers, foreign)
	}
	if busy, err := b.anyBuilder(context.Background()); err != nil || busy {
		t.Fatal("the phase is not free")
	}
}
