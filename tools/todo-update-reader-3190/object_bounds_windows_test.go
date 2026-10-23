package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"hash/adler32"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// entryHead encodes a pack entry's type and inflated size as Git does.
func entryHead(kind, size int) []byte {
	head := []byte{byte(kind<<4) | byte(size&15)}
	for size >>= 4; size > 0; size >>= 7 {
		head[len(head)-1] |= 0x80
		head = append(head, byte(size&0x7f))
	}
	return head
}

// ofsBase encodes how far back an offset delta's base starts, as Git does.
func ofsBase(distance int) []byte {
	out := []byte{byte(distance & 0x7f)}
	for distance >>= 7; distance > 0; distance >>= 7 {
		distance--
		out = append([]byte{0x80 | byte(distance&0x7f)}, out...)
	}
	return out
}

// deflated is data as one zlib stream.
func deflated(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zlib.NewWriter(&buf)
	if _, err := z.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// rawEntry is one entry of a pack that writeRawPack writes: the name that the
// index gives it, and its bytes from its type and size to the end of its zlib
// stream.
type rawEntry struct {
	name []byte
	data []byte
}

// writeRawPack writes a version 2 pack that holds the entries in their order
// from offset 12, and its version 2 index, into dir under Git's names.
func writeRawPack(t *testing.T, dir string, entries []rawEntry) {
	t.Helper()
	pack := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32([]byte("PACK"), 2), uint32(len(entries)))
	offsets := make([]uint32, len(entries))
	for i, e := range entries {
		offsets[i] = uint32(len(pack))
		pack = append(pack, e.data...)
	}
	packSum := sha1.Sum(pack)
	pack = append(pack, packSum[:]...)

	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return bytes.Compare(entries[a].name, entries[b].name) })
	idx := []byte{0xff, 't', 'O', 'c', 0, 0, 0, 2}
	for first := range 256 {
		n := 0
		for _, e := range entries {
			if int(e.name[0]) <= first {
				n++
			}
		}
		idx = binary.BigEndian.AppendUint32(idx, uint32(n))
	}
	for _, i := range order {
		idx = append(idx, entries[i].name...)
	}
	for _, i := range order {
		idx = binary.BigEndian.AppendUint32(idx, crc32.ChecksumIEEE(entries[i].data))
	}
	for _, i := range order {
		idx = binary.BigEndian.AppendUint32(idx, offsets[i])
	}
	idx = append(idx, packSum[:]...)
	idxSum := sha1.Sum(idx)
	idx = append(idx, idxSum[:]...)

	name := filepath.Join(dir, "pack-"+hex.EncodeToString(packSum[:]))
	writeFile(t, name+".pack", string(pack))
	writeFile(t, name+".idx", string(idx))
}

// openRawStore opens the object store at objects under limits, as the reader
// opens the source's store.
func openRawStore(t *testing.T, objects string, limits objectLimits) *objectStore {
	t.Helper()
	s := newSession(environment{guard: clearGuard})
	t.Cleanup(s.close)
	store, reason := s.openObjects(sourceObjects{dir: objects}, limits)
	if reason != "" {
		t.Fatalf("openObjects refused the store: %s", reason)
	}
	t.Cleanup(store.close)
	return store
}

// A delta chain that returns to an entry is refused once the entries' headers
// show the loop, before anything is inflated, however large the deltas say
// they are. Inflating each delta on the way down would hold one per step until
// the depth limit: tens of gigabytes for 16 MiB deltas.
func TestDeltaCyclesInflateNothing(t *testing.T) {
	const size = 16 << 10
	delta := deflated(t, make([]byte, size))
	a, b := sha1.Sum([]byte("a")), sha1.Sum([]byte("b"))
	basedOnB := slices.Concat(entryHead(objRefDelta, size), b[:], delta)
	cases := map[string][]rawEntry{
		"delta based on itself": {{name: a[:], data: slices.Concat(entryHead(objRefDelta, size), a[:], delta)}},
		"two deltas based on each other": {
			{name: a[:], data: basedOnB},
			{name: b[:], data: slices.Concat(entryHead(objOfsDelta, size), ofsBase(len(basedOnB)), delta)},
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			objects := filepath.Join(testLongPath(t, t.TempDir()), "objects")
			writeRawPack(t, filepath.Join(objects, "pack"), entries)
			store := openRawStore(t, objects, productionProfile.limits)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, _, ok := store.object(hex.EncodeToString(a[:]))
			runtime.ReadMemStats(&after)
			if ok || store.failure() != "source_git" {
				t.Fatalf("object = %v, reason %q; want the cycle refused", ok, store.failure())
			}
			if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
				t.Fatalf("refusing the cycle allocated %d bytes", grown)
			}
			if store.spent != 0 {
				t.Fatalf("refusing the cycle counted %d bytes as read", store.spent)
			}
		})
	}
}

// A chain of more deltas than the depth limit is refused before any of its
// entries is inflated, and a chain that meets the limit is read.
func TestDeltaDepthLimit(t *testing.T) {
	dir, git := historyRepo(t)
	git("repack", "-a", "-d", "-f", "-q", "--depth=50", "--window=50")
	objects := filepath.Join(dir, ".git", "objects")
	indexes, err := filepath.Glob(filepath.Join(objects, "pack", "pack-*.idx"))
	if err != nil || len(indexes) != 1 {
		t.Fatalf("pack indexes %v: %v", indexes, err)
	}
	// git verify-pack -v prints "<name> <type> <size> <size in pack> <offset>
	// <depth> <base>" for each delta.
	deepest, depth := "", 0
	for _, line := range strings.Split(git("verify-pack", "-v", indexes[0]), "\n") {
		if fields := strings.Fields(line); len(fields) == 7 {
			if d, err := strconv.Atoi(fields[5]); err == nil && d > depth {
				deepest, depth = fields[0], d
			}
		}
	}
	if depth < 2 {
		t.Fatalf("the deepest delta chain holds %d deltas", depth)
	}
	for _, limit := range []int{depth, depth - 1} {
		limits := productionProfile.limits
		limits.deltaDepth = limit
		store := openRawStore(t, objects, limits)
		_, _, ok := store.object(deepest)
		if ok != (limit == depth) {
			t.Fatalf("with %d deltas allowed, reading a chain of %d deltas = %v", limit, depth, ok)
		}
		if !ok && store.spent != 0 {
			t.Fatalf("refusing the chain counted %d bytes as read", store.spent)
		}
	}
}

// The compressed bytes read count against the read limit, so a stream padded
// with empty blocks is refused under a limit that its inflated size meets.
func TestReadLimitCountsCompressedBytes(t *testing.T) {
	data := []byte("a blob whose stream is padded with empty blocks\n")
	id := objectName("blob", data)
	name, err := hex.DecodeString(id)
	if err != nil {
		t.Fatal(err)
	}
	// A zlib header, 2^18 empty stored blocks, a final stored block of the
	// data and the data's Adler-32 checksum.
	n := len(data)
	stream := slices.Concat([]byte{0x78, 0x01}, bytes.Repeat([]byte{0, 0, 0, 0xff, 0xff}, 1<<18),
		[]byte{1, byte(n), byte(n >> 8), ^byte(n), ^byte(n >> 8)}, data)
	stream = binary.BigEndian.AppendUint32(stream, adler32.Checksum(data))
	objects := filepath.Join(testLongPath(t, t.TempDir()), "objects")
	writeRawPack(t, filepath.Join(objects, "pack"), []rawEntry{{name: name, data: slices.Concat(entryHead(objBlob, n), stream)}})

	store := openRawStore(t, objects, productionProfile.limits)
	if kind, got, ok := store.object(id); !ok || kind != "blob" || !bytes.Equal(got, data) {
		t.Fatalf("object = %q %q %v", kind, got, ok)
	}
	if store.spent < int64(len(stream)) {
		t.Fatalf("reading a stream of %d bytes counted %d bytes", len(stream), store.spent)
	}
	limits := productionProfile.limits
	limits.readBytes = int64(len(stream)) / 2
	if _, _, ok := openRawStore(t, objects, limits).object(id); ok {
		t.Fatalf("a stream of %d bytes was read under a limit of %d bytes", len(stream), limits.readBytes)
	}
}

// A source that needs more bytes than the read limit is refused, whether its
// objects are loose or packed.
func TestReadLimit(t *testing.T) {
	for name, prepare := range map[string]func(f *fixture){
		"loose objects":  func(*fixture) {},
		"packed objects": func(f *fixture) { f.gitIn(f.repo, "repack", "-a", "-d", "-q") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			prepare(f)
			f.p.limits.readBytes = 64
			expectReason(t, f, "source_git")
		})
	}
}

// Each bound on the object store accepts a source that needs exactly the
// bound, and refuses it under a bound one lower.
func TestObjectLimits(t *testing.T) {
	commit := func(f *fixture, args ...string) string {
		return f.gitOut(append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit-tree", f.p.tree}, args...)...)
	}
	repack := func(f *fixture) { f.gitIn(f.repo, "repack", "-a", "-d", "-q") }
	packFiles := func(f *fixture, pattern string) []string {
		matches, err := filepath.Glob(filepath.Join(f.common, "objects", "pack", pattern))
		if err != nil || len(matches) == 0 {
			f.t.Fatalf("pack files %v: %v", matches, err)
		}
		return matches
	}
	listed := func(f *fixture, args ...string) []string {
		return strings.Split(f.gitOut(append([]string{"ls-tree", "-r", "--full-tree"}, args...)...), "\n")
	}
	cases := map[string]struct {
		set  func(l *objectLimits, n int64)
		need func(f *fixture) int64 // shapes the store and returns what it needs
	}{
		"index and pack pairs": {
			func(l *objectLimits, n int64) { l.packs = int(n) },
			func(f *fixture) int64 {
				repack(f)
				f.gitIn(f.repo, "update-ref", "refs/tags/second-pack", commit(f, "-m", "a commit in a second pack"))
				f.gitIn(f.repo, "repack", "-d", "-q")
				return int64(len(packFiles(f, "pack-*.idx")))
			},
		},
		"pack directory entries": {
			func(l *objectLimits, n int64) { l.packEntries = int(n) },
			func(f *fixture) int64 { repack(f); return int64(len(packFiles(f, "*"))) },
		},
		"index bytes": {
			func(l *objectLimits, n int64) { l.indexBytes = n },
			func(f *fixture) int64 {
				repack(f)
				var total int64
				for _, idx := range packFiles(f, "pack-*.idx") {
					info, err := os.Stat(idx)
					if err != nil {
						f.t.Fatal(err)
					}
					total += info.Size()
				}
				return total
			},
		},
		"commits walked": {
			func(l *objectLimits, n int64) { l.commits = int(n) },
			func(f *fixture) int64 {
				ahead := commit(f, "-p", f.p.head, "-m", "one ahead")
				f.gitIn(f.repo, "update-ref", "refs/remotes/origin/main", commit(f, "-p", ahead, "-m", "two ahead"))
				return 3
			},
		},
		"tree entries": {
			func(l *objectLimits, n int64) { l.treeEntries = int(n) },
			func(f *fixture) int64 { return int64(len(listed(f, "-t", "HEAD"))) },
		},
		"path bytes": {
			func(l *objectLimits, n int64) { l.pathBytes = n },
			func(f *fixture) int64 {
				var total int64
				for _, path := range listed(f, "--name-only", "HEAD") {
					total += int64(len(path))
				}
				return total
			},
		},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			need := b.need(f)
			b.set(&f.p.limits, need)
			if got, reason := f.verify(); !got.Qualified || reason != "" {
				t.Fatalf("with the bound at the %d needed, verify = qualified %v reason %q", need, got.Qualified, reason)
			}
			b.set(&f.p.limits, need-1)
			expectReason(t, f, "source_git")
		})
	}
}
