package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Software-virtualized builder guest.
//
// The audited product build unpacks a base image whose files belong to non-root
// users and then runs package tools that change owners and drop privileges.
// Kaniko can only do that with CAP_CHOWN, CAP_SETUID and friends. The outer
// container keeps every capability dropped and no-new-privileges, so Kaniko
// itself cannot run there: it failed while unpacking the base filesystem.
//
// Instead the outer container runs QEMU in pure CPU emulation (TCG). The guest
// is a small Linux kernel whose root user holds the kernel's own capabilities,
// which exist only inside the emulated machine. Nothing is added to the outer
// container: no capability, device, KVM, privileged mode, bind mount, host
// socket, or relaxed seccomp. The guest sees a disposable disk file and a
// disposable output file inside the outer writable layer, a NAT'd network
// through the outer container's own network, and the sealed build context.
//
// All guest bytes (kernel, BusyBox, the Kaniko executor and its CA bundle) come
// from digest-pinned images and are re-hashed in RAM before use.
const (
	releaseGuestKernelImage   = "linuxkit/kernel@sha256:3eae204fb152652742f4679da795a3f2848ad70c83bf1db31af0a0039e40ae67"
	releaseGuestUserlandImage = "ghcr.io/osscontainertools/kaniko@sha256:7ab44d0131c3051b6dea128067e258c7afb5c4815082a93b9cf6c624f2140bf0"
)

type guestFilePin struct {
	image, path, sha256 string
	size                int64
}

// The Kaniko executor is byte-identical to the one in the previously audited
// ghcr.io/osscontainertools/kaniko@sha256:2fa20b0a... image; its SHA-256 below is
// what the guest relies on.
var (
	guestKernelPin   = guestFilePin{releaseGuestKernelImage, "/kernel", "89f7d4f31f6ef77d0f8d45810de9e19e3f8dededf3dd6ebf89bb540d26d8c0fd", 11535360}
	guestBusyboxPin  = guestFilePin{releaseGuestUserlandImage, "/busybox/busybox", "899f11dd0dbf8ac0042c67cb7791f6d10c1bf1efd1c5f82dc65047b9729fb583", 1239304}
	guestExecutorPin = guestFilePin{releaseGuestUserlandImage, "/kaniko/executor", "7e14e3351435495fb6c9eb18c16a418332f45a87e568fc64a362a1f2898a957b", 36999330}
	guestCertsPin    = guestFilePin{releaseGuestUserlandImage, "/kaniko/ssl/certs/ca-certificates.crt", "714d457d580922dbf1d0be8bd35ba236a842b50b0072ae791582a19adef772a5", 224449}
	guestImages      = []string{releaseGuestKernelImage, releaseGuestUserlandImage}
)

func guestPinList() []guestFilePin {
	return []guestFilePin{guestKernelPin, guestBusyboxPin, guestExecutorPin, guestCertsPin}
}

// guestFiles are the verified bytes of the emulated machine. Never serialized.
type guestFiles struct {
	kernel, busybox, executor, certs []byte
}

func (guestFiles) Format(s fmt.State, _ rune) { io.WriteString(s, "<guest-files>") }

func (g guestFiles) verify() bool {
	for _, c := range []struct {
		b   []byte
		pin guestFilePin
	}{{g.kernel, guestKernelPin}, {g.busybox, guestBusyboxPin}, {g.executor, guestExecutorPin}, {g.certs, guestCertsPin}} {
		if int64(len(c.b)) != c.pin.size || digest(c.b) != c.pin.sha256 {
			return false
		}
	}
	return true
}

// guestProvider obtains the pinned guest files and removes whatever it had to
// introduce into the Docker daemon. The default implementation talks to Docker;
// tests of the surrounding lifecycle substitute fixed bytes.
type guestProvider interface {
	acquire(ctx context.Context, b *builderRun) (guestFiles, string)
	release(ctx context.Context, b *builderRun) bool
}

// One image the guest files were taken from, recorded durably before it can be
// pulled so that recovery removes exactly what this run introduced.
type guestImageRecord struct {
	Ref                              string
	Preexisting, Introduced, Removed bool
}

type dockerGuestProvider struct{}

func (b *builderRun) guestImage(ref string) *guestImageRecord {
	for i := range b.rec.GuestImages {
		if b.rec.GuestImages[i].Ref == ref {
			return &b.rec.GuestImages[i]
		}
	}
	return nil
}

// acquire takes the pinned files out of the pinned images without creating a
// single container: the files are read from the stream of "docker image save".
// The only Docker resources this run can introduce are therefore the images
// themselves, and those are recorded durably before they are pulled.
func (dockerGuestProvider) acquire(ctx context.Context, b *builderRun) (guestFiles, string) {
	got := map[string][]byte{}
	for _, ref := range guestImages {
		present, err := b.imageRefPresent(ctx, ref)
		if err != nil {
			return guestFiles{}, b.classify(ctx, "guest-source-unavailable")
		}
		if b.guestImage(ref) == nil {
			b.rec.GuestImages = append(b.rec.GuestImages, guestImageRecord{Ref: ref, Preexisting: present, Introduced: !present})
			if b.persist() != nil {
				return guestFiles{}, "state-unavailable"
			}
		}
		if !present {
			stderr := boundedBuffer{limit: 64 << 10}
			if b.d.run(ctx, nil, io.Discard, &stderr, "pull", ref) != nil {
				return guestFiles{}, b.classify(ctx, "guest-source-unavailable")
			}
		}
		out, _, err := dockerText(ctx, b.d, 1<<20, "image", "inspect", "--format", "{{json .RepoDigests}}", ref)
		var refs []string
		if err != nil || json.Unmarshal([]byte(out), &refs) != nil {
			return guestFiles{}, b.classify(ctx, "guest-source-unavailable")
		}
		match := false
		for _, r := range refs {
			match = match || r == ref
		}
		if !match {
			return guestFiles{}, "guest-source-mismatch"
		}
		var pins []guestFilePin
		for _, pin := range guestPinList() {
			if pin.image == ref {
				pins = append(pins, pin)
			}
		}
		files, class := b.extractFromImage(ctx, ref, pins)
		if class != "" {
			return guestFiles{}, b.classify(ctx, class)
		}
		for _, pin := range pins {
			data := files[pin.path]
			if int64(len(data)) != pin.size || digest(data) != pin.sha256 {
				return guestFiles{}, "guest-source-mismatch"
			}
			got[pin.path] = data
		}
	}
	files := guestFiles{kernel: got[guestKernelPin.path], busybox: got[guestBusyboxPin.path], executor: got[guestExecutorPin.path], certs: got[guestCertsPin.path]}
	if !files.verify() {
		return guestFiles{}, "guest-source-mismatch"
	}
	return files, ""
}

const (
	guestSaveManifestLimit = 1 << 20
	guestSaveMaxLayers     = 128
	guestLayerExpandLimit  = 1 << 30
)

// saveImage runs "docker image save" and hands its tar stream to visit. It
// returns only after the Docker client has finished, so nothing is left running.
func (b *builderRun) saveImage(ctx context.Context, ref string, visit func(*tar.Reader) error) string {
	saveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		stderr := boundedBuffer{limit: 64 << 10}
		err := b.d.run(saveCtx, nil, pw, &stderr, "image", "save", ref)
		pw.CloseWithError(err)
		done <- err
	}()
	verr := visit(tar.NewReader(pr))
	if verr != nil {
		cancel()
	}
	_, _ = io.Copy(io.Discard, pr)
	_ = pr.Close()
	err := <-done
	if verr != nil || err != nil {
		return "guest-source-unavailable"
	}
	return ""
}

type guestLayerHit struct {
	data    []byte
	link    string
	deleted bool
}

// extractFromImage reads the pinned paths out of one saved image. The archive
// lists its layers in manifest.json, which may come after the layer blobs, so
// the stream is read twice: first the small manifest, then the layers in turn.
// A later layer overrides an earlier one, a whiteout deletes, a hard link is
// resolved inside its own layer, and anything else (symlink, missing path,
// unsupported compression) is refused.
func (b *builderRun) extractFromImage(ctx context.Context, ref string, pins []guestFilePin) (map[string][]byte, string) {
	var layers []string
	if class := b.saveImage(ctx, ref, func(tr *tar.Reader) error {
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if h.Name != "manifest.json" || h.Typeflag != tar.TypeReg {
				continue
			}
			if h.Size <= 0 || h.Size > guestSaveManifestLimit {
				return errors.New("manifest size")
			}
			raw, err := io.ReadAll(io.LimitReader(tr, h.Size))
			var m []struct{ Layers []string }
			if err != nil || json.Unmarshal(raw, &m) != nil || len(m) != 1 {
				return errors.New("manifest")
			}
			layers = m[0].Layers
		}
		if len(layers) == 0 || len(layers) > guestSaveMaxLayers {
			return errors.New("layers")
		}
		return nil
	}); class != "" {
		return nil, class
	}
	index := map[string]int{}
	for i, l := range layers {
		if _, dup := index[l]; dup {
			return nil, "guest-source-unavailable"
		}
		index[l] = i
	}
	found := make([]map[string]guestLayerHit, len(layers))
	if class := b.saveImage(ctx, ref, func(tr *tar.Reader) error {
		seen := 0
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			i, ok := index[h.Name]
			if !ok || h.Typeflag != tar.TypeReg {
				continue
			}
			hits, err := scanGuestLayer(ctx, tr, pins)
			if err != nil {
				return err
			}
			found[i] = hits
			seen++
		}
		if seen != len(layers) {
			return errors.New("layers missing")
		}
		return nil
	}); class != "" {
		return nil, class
	}
	out := map[string][]byte{}
	for _, pin := range pins {
		name := strings.TrimPrefix(pin.path, "/")
		for i := len(found) - 1; i >= 0; i-- {
			hit, ok := found[i][name]
			if !ok {
				continue
			}
			if hit.deleted || hit.data == nil {
				return nil, "guest-source-mismatch"
			}
			out[pin.path] = hit.data
			break
		}
		if out[pin.path] == nil {
			return nil, "guest-source-mismatch"
		}
	}
	return out, ""
}

func guestLayerName(n string) string {
	return strings.TrimPrefix(strings.TrimPrefix(path.Clean("/"+n), "/"), "./")
}

// scanGuestLayer walks one layer (plain or gzip tar) for the pinned paths.
func scanGuestLayer(ctx context.Context, r io.Reader, pins []guestFilePin) (map[string]guestLayerHit, error) {
	br := bufio.NewReader(ctxReader{ctx, r})
	var plain io.Reader = br
	magic, _ := br.Peek(4)
	switch {
	case len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		plain = gz
	case len(magic) == 4 && magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd:
		return nil, errors.New("zstd layers are not supported")
	}
	want := map[string]guestFilePin{}
	sizes := map[int64]bool{}
	for _, pin := range pins {
		want[strings.TrimPrefix(pin.path, "/")] = pin
		sizes[pin.size] = true
	}
	hits := map[string]guestLayerHit{}
	regular := map[string][]byte{} // Only files whose size equals a pinned size.
	tr := tar.NewReader(io.LimitReader(plain, guestLayerExpandLimit))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := guestLayerName(h.Name)
		base, dir := path.Base(name), path.Dir(name)
		if strings.HasPrefix(base, ".wh.") {
			victim := dir
			if base != ".wh..wh..opq" {
				victim = path.Join(dir, strings.TrimPrefix(base, ".wh."))
			}
			for wp := range want {
				if wp == victim || victim == "." || strings.HasPrefix(wp, victim+"/") {
					hits[wp] = guestLayerHit{deleted: true}
				}
			}
			continue
		}
		pin, isWanted := want[name]
		switch h.Typeflag {
		case tar.TypeReg:
			if !sizes[h.Size] {
				if isWanted {
					hits[name] = guestLayerHit{deleted: true} // A file of another size: not the pin.
				}
				continue
			}
			data := make([]byte, h.Size)
			if _, err := io.ReadFull(tr, data); err != nil {
				return nil, err
			}
			regular[name] = data
			if isWanted && h.Size == pin.size {
				hits[name] = guestLayerHit{data: data}
			}
		case tar.TypeLink:
			if isWanted {
				hits[name] = guestLayerHit{link: guestLayerName(h.Linkname)}
			}
		case tar.TypeSymlink:
			if isWanted {
				return nil, errors.New("a pinned path is a symlink")
			}
		}
	}
	for name, hit := range hits {
		if hit.link == "" {
			continue
		}
		data, ok := regular[hit.link]
		if !ok {
			return nil, errors.New("a pinned path is a link to an unknown file")
		}
		hits[name] = guestLayerHit{data: data}
	}
	return hits, nil
}

// copyOut reads exactly one regular file of at most the given size from a
// container and returns only after the Docker client has finished.
func (b *builderRun) copyOut(ctx context.Context, container, path string, size int64) ([]byte, string) {
	cpCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		stderr := boundedBuffer{limit: 64 << 10}
		err := b.d.run(cpCtx, nil, pw, &stderr, "cp", container+":"+path, "-")
		pw.CloseWithError(err)
		done <- err
	}()
	data, class := readSingleFileTar(pr, size)
	if class != "" {
		cancel()
	}
	_, _ = io.Copy(io.Discard, pr)
	_ = pr.Close()
	err := <-done
	if class != "" || err != nil {
		return nil, "guest-source-unavailable"
	}
	return data, ""
}

// release removes every image this run introduced. An image that was already
// present is never touched.
func (dockerGuestProvider) release(ctx context.Context, b *builderRun) bool {
	clean := true
	for i := range b.rec.GuestImages {
		g := &b.rec.GuestImages[i]
		if !g.Introduced {
			continue
		}
		present, err := b.imageRefPresent(ctx, g.Ref)
		if err == nil && present {
			_, _, _ = dockerText(ctx, b.d, 1<<16, "image", "rm", g.Ref)
			present, err = b.imageRefPresent(ctx, g.Ref)
		}
		g.Removed = err == nil && !present
		clean = clean && g.Removed
	}
	return clean
}

// ---- initramfs and payload --------------------------------------------------

// The BusyBox applets the init script and the Kaniko wrapper use.
var guestApplets = []string{"sh", "mount", "umount", "ip", "echo", "mkdir", "tar", "uname", "grep", "wc", "sha256sum", "cut", "sync", "poweroff", "cat", "ls", "id", "stat",
	"env", "cp", "chroot", "printf", "mkfs.ext2", "mknod", "ln", "dd", "sleep", "chmod", "blockdev"}

type cpioWriter struct {
	w   io.Writer
	ino uint32
	err error
}

func (c *cpioWriter) pad(n int) {
	if c.err == nil && n%4 != 0 {
		_, c.err = c.w.Write(make([]byte, 4-n%4))
	}
}

func (c *cpioWriter) entry(name string, mode uint32, data []byte) {
	if c.err != nil {
		return
	}
	c.ino++
	nlink := 1
	if mode&0o170000 == 0o040000 {
		nlink = 2
	}
	hdr := fmt.Sprintf("070701%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X", c.ino, mode, 0, 0, nlink, 0, len(data), 0, 0, 0, 0, len(name)+1, 0)
	if _, c.err = io.WriteString(c.w, hdr+name+"\x00"); c.err != nil {
		return
	}
	c.pad(len(hdr) + len(name) + 1)
	if len(data) > 0 && c.err == nil {
		_, c.err = c.w.Write(data)
	}
	c.pad(len(data))
}

func (c *cpioWriter) dir(name string, perm uint32)            { c.entry(name, 0o040000|perm, nil) }
func (c *cpioWriter) file(name string, perm uint32, d []byte) { c.entry(name, 0o100000|perm, d) }
func (c *cpioWriter) symlink(name, target string)             { c.entry(name, 0o120777, []byte(target)) }

var guestNoncePattern = regexp.MustCompile("^[0-9a-f]{32}$")

// buildInitrd returns the gzip'd cpio archive the guest boots from. Every input
// is validated again here: file names and numbers end up in a shell script.
func buildInitrd(g guestFiles, spec releaseBuilderSpec, nonce string) ([]byte, error) {
	ctxData, err := spec.verifiedContext()
	if err != nil || !g.verify() || !releaseDockerfileName.MatchString(spec.dockerfile) || !guestNoncePattern.MatchString(nonce) {
		return nil, errors.New("guest input rejected")
	}
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	w := &cpioWriter{w: gz}
	for _, d := range []string{"kaniko", "kaniko/bin", "kaniko/ssl", "kaniko/ssl/certs", "proc", "sys", "dev", "tmp", "work", "etc"} {
		w.dir(d, 0o755)
	}
	w.file("init", 0o755, []byte(guestInitScript))
	w.file("kaniko/bin/busybox", 0o755, g.busybox)
	for _, a := range guestApplets {
		w.symlink("kaniko/bin/"+a, "busybox")
	}
	w.file("kaniko/executor", 0o755, g.executor)
	w.file("kaniko/ssl/certs/ca-certificates.crt", 0o644, g.certs)
	w.file("kaniko/ctx.tgz", 0o600, ctxData)
	w.file("kaniko/dockerfile", 0o644, []byte(spec.dockerfile+"\n"))
	w.file("kaniko/nonce", 0o644, []byte(nonce+"\n"))
	w.file("kaniko/outmax", 0o644, []byte(strconv.FormatInt(spec.profile.artifactMax, 10)+"\n"))
	w.entry("TRAILER!!!", 0, nil)
	if w.err != nil || gz.Close() != nil {
		return nil, errors.New("guest archive failed")
	}
	return buf.Bytes(), nil
}

// buildGuestPayload is the only stdin the outer container receives: the kernel
// and the initial RAM filesystem as a tar stream.
func buildGuestPayload(g guestFiles, spec releaseBuilderSpec, nonce string) ([]byte, error) {
	initrd, err := buildInitrd(g, spec, nonce)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range []struct {
		name string
		data []byte
	}{{"kernel", g.kernel}, {"initrd.gz", initrd}} {
		if tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o600, Size: int64(len(f.data)), Typeflag: tar.TypeReg}) != nil {
			return nil, errors.New("guest payload failed")
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, errors.New("guest payload failed")
		}
	}
	if tw.Close() != nil {
		return nil, errors.New("guest payload failed")
	}
	return buf.Bytes(), nil
}

// guestInitScript runs as PID 1 of the emulated machine. Kaniko replaces the
// filesystem it runs in, so it runs in a chroot on the guest disk with the
// mounts a container runtime would provide; the RAM filesystem and this script
// stay outside it. The outcome is reported only on the second serial port, and
// the chroot's /dev has no node for it.
const guestInitScript = `#!/kaniko/bin/sh
export PATH=/kaniko/bin
Z=0000000000000000000000000000000000000000000000000000000000000000
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
mkdir -p /dev/pts /dev/shm /tmp /work
finish() {
  echo "CC-RESULT rc=$1 size=$2 sha=$3" > /dev/ttyS1
  echo "cc-guest: finished rc=$1"
  sync
  poweroff -f
  while :; do sleep 1; done
}
DF=$(cat /kaniko/dockerfile)
NONCE=$(cat /kaniko/nonce)
OUTMAX=$(cat /kaniko/outmax)
ip link set lo up
ip link set eth0 up
ip addr add 10.0.2.15/24 dev eth0
ip route add default via 10.0.2.2 || finish 121 0 $Z
[ "$(blockdev --getsize64 /dev/vda)" -gt "$(blockdev --getsize64 /dev/vdb)" ] || finish 121 0 $Z
mkfs.ext2 -q -m 0 -N 262144 /dev/vda || finish 121 0 $Z
mount -t ext2 -o noatime /dev/vda /work || finish 122 0 $Z
R=/work/root
mkdir -p $R/kaniko/tmp $R/proc $R/sys $R/dev $R/etc $R/out $R/root /work/out || finish 122 0 $Z
cp -a /kaniko/executor /kaniko/bin /kaniko/ssl $R/kaniko/ || finish 122 0 $Z
echo 'nameserver 10.0.2.3' > $R/kaniko/resolv.conf
echo cc-builder > $R/kaniko/hostname
printf '127.0.0.1 localhost\n127.0.1.1 cc-builder\n' > $R/kaniko/hosts
for f in resolv.conf hostname hosts; do
  : > $R/etc/$f
  mount --bind $R/kaniko/$f $R/etc/$f || finish 122 0 $Z
done
mount --bind /work/out $R/out || finish 122 0 $Z
mount -t proc proc $R/proc || finish 122 0 $Z
mount -t sysfs -o ro sysfs $R/sys || finish 122 0 $Z
mount -t tmpfs -o mode=755,size=2m tmpfs $R/dev || finish 122 0 $Z
mknod -m 666 $R/dev/null c 1 3
mknod -m 666 $R/dev/zero c 1 5
mknod -m 666 $R/dev/full c 1 7
mknod -m 666 $R/dev/random c 1 8
mknod -m 666 $R/dev/urandom c 1 9
mknod -m 666 $R/dev/tty c 5 0
mkdir -p $R/dev/pts $R/dev/shm
mount -t devpts -o newinstance,ptmxmode=0666,mode=0620 devpts $R/dev/pts || finish 122 0 $Z
ln -s pts/ptmx $R/dev/ptmx
mount -t tmpfs -o mode=1777,size=64m tmpfs $R/dev/shm || finish 122 0 $Z
ln -s /proc/self/fd $R/dev/fd
ln -s /proc/self/fd/0 $R/dev/stdin
ln -s /proc/self/fd/1 $R/dev/stdout
ln -s /proc/self/fd/2 $R/dev/stderr
echo "cc-guest: kernel $(uname -r) $(grep CapEff /proc/self/status)"
chroot $R /kaniko/bin/env -i PATH=/kaniko/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root TMPDIR=/kaniko/tmp SSL_CERT_DIR=/kaniko/ssl/certs GOMAXPROCS=2 GOMEMLIMIT=3GiB /kaniko/executor --context=tar://stdin --dockerfile="$DF" --destination="cc-release-build:$NONCE" --no-push --no-push-cache --cache=false --tar-path=/out/output.tar --verbosity=warn --log-format=json --force < /kaniko/ctx.tgz
rc=$?
sync
SZ=0
SHA=$Z
if [ $rc -eq 0 ]; then
  [ -f /work/out/output.tar ] && SZ=$(wc -c < /work/out/output.tar)
  # Numeric diagnostics only. Keep the measured size before an invalid output
  # is cleared from the result channel; exit 124 alone cannot distinguish a
  # missing artifact from one exceeding the closed profile's limit.
  echo "cc-guest: artifact bytes=$SZ limit=$OUTMAX"
  if [ "$SZ" -le 0 ] || [ "$SZ" -gt "$OUTMAX" ]; then
    rc=124
    SZ=0
  else
    SHA=$(sha256sum /work/out/output.tar | cut -d' ' -f1)
    dd if=/work/out/output.tar of=/dev/vdb bs=1M conv=fsync 2>/dev/null || rc=123
    sync
  fi
fi
finish $rc $SZ $SHA
`

// outerScript is the whole behavior of the outer container. Its only input is
// the payload tar on stdin plus the numbers of the closed profile.
func (p releaseBuilderProfile) outerScript() string {
	return fmt.Sprintf(outerScriptTemplate, p.guestDiskBytes, p.artifactMax+(4<<20), p.guestMemoryMiB, p.guestCPUs)
}

const outerScriptTemplate = `set -u
W=/w
mkdir -p "$W/in" /kaniko || exit 120
tar -x -f - -C "$W/in" || exit 120
truncate -s %d "$W/disk.img" || exit 120
truncate -s %d "$W/out.img" || exit 120
: > "$W/result.txt" || exit 120
qemu-system-x86_64 -name cc-guest -sandbox on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny \
  -accel tcg,thread=multi,tb-size=256 -machine q35 -cpu max -smp %[4]d -m %[3]d \
  -display none -monitor none -nodefaults -no-reboot \
  -chardev stdio,id=con,signal=off -serial chardev:con \
  -chardev file,id=res,path="$W/result.txt" -serial chardev:res \
  -kernel "$W/in/kernel" -initrd "$W/in/initrd.gz" -append "console=ttyS0 panic=-1 loglevel=3 quiet rdinit=/init" \
  -drive if=none,id=d0,file="$W/disk.img",format=raw,cache=unsafe -device virtio-blk-pci,drive=d0 \
  -drive if=none,id=d1,file="$W/out.img",format=raw,cache=unsafe -device virtio-blk-pci,drive=d1 \
  -netdev user,id=n0 -device virtio-net-pci,netdev=n0,romfile= \
  -object rng-random,id=rng0,filename=/dev/urandom -device virtio-rng-pci,rng=rng0 &
q=$!
trap 'kill -TERM "$q" 2>/dev/null' TERM INT
wait "$q"; qrc=$?
while kill -0 "$q" 2>/dev/null; do wait "$q"; qrc=$?; done
cg=/sys/fs/cgroup
rd() { if [ -r "$cg/$1" ]; then tr -d '\n' < "$cg/$1"; else printf 'n/a'; fi; }
cap=$(sed -n 's/^CapEff:[[:space:]]*//p' /proc/self/status)
nnp=$(sed -n 's/^NoNewPrivs:[[:space:]]*//p' /proc/self/status)
printf '{"cpu.max":"%%s","memory.max":"%%s","pids.max":"%%s","memory.peak":"%%s","pids.peak":"%%s","CapEff":"%%s","NoNewPrivs":"%%s"}\n' \
  "$(rd cpu.max)" "$(rd memory.max)" "$(rd pids.max)" "$(rd memory.peak)" "$(rd pids.peak)" "$cap" "$nnp" > /kaniko/outer-proof.json
# A result line is only believed from an emulator that itself exited cleanly: a
# crash after the guest wrote its result must not turn into a success.
[ "$qrc" -eq 0 ] || exit 127
line=$(tail -n 1 "$W/result.txt" | tr -d '\r')
case $line in
  "CC-RESULT rc="*" size="*" sha="*) ;;
  *) exit 125 ;;
esac
rc=${line#CC-RESULT rc=}; rc=${rc%%%% *}
rest=${line#*size=}; size=${rest%%%% *}
sha=${rest#*sha=}
case "$rc$size" in *[!0-9]*|'') exit 125 ;; esac
if [ "$rc" -ne 0 ]; then
  [ "$rc" -le 255 ] || rc=125
  exit "$rc"
fi
case $sha in *[!0-9a-f]*) exit 125 ;; esac
[ "${#sha}" -eq 64 ] || exit 125
[ "$size" -gt 0 ] || exit 126
head -c "$size" "$W/out.img" > /kaniko/output.tar || exit 126
echo "$sha  /kaniko/output.tar" | sha256sum -c - >/dev/null 2>&1 || exit 126
exit 0
`
