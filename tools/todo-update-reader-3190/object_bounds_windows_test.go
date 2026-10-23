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

// padded is data as one zlib stream that holds blocks empty stored blocks
// before a final stored block of the data.
func padded(data []byte, blocks int) []byte {
	n := len(data)
	stream := slices.Concat([]byte{0x78, 0x01}, bytes.Repeat([]byte{0, 0, 0, 0xff, 0xff}, blocks),
		[]byte{1, byte(n), byte(n >> 8), ^byte(n), ^byte(n >> 8)}, data)
	return binary.BigEndian.AppendUint32(stream, adler32.Checksum(data))
}

// rawName is the 20-byte name of the object of kind with data.
func rawName(t *testing.T, kind string, data []byte) []byte {
	t.Helper()
	name, err := hex.DecodeString(objectName(kind, data))
	if err != nil {
		t.Fatal(err)
	}
	return name
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

// A delta chain that returns to an entry is refused as soon as the entries'
// headers show the loop, before anything is inflated, however large the
// deltas say they are. Inflating each delta on the way down would hold one per
// step up to the depth limit: tens of gigabytes for 16 MiB deltas. Walking
// the headers on to the depth limit would allocate about a megabyte.
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
			if grown := after.TotalAlloc - before.TotalAlloc; grown > 256<<10 {
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

// Every byte that the store reads or produces counts against the read limit:
// the compressed bytes as they are read, each pack entry's inflated size and
// each delta's result, and each loose file's size and inflated size. Each
// object below is read under the production limit, and refused under a limit
// that only the bytes of its case pass.
func TestReadLimitCountsEveryByte(t *testing.T) {
	const limit = 512 << 10
	small := []byte("a blob that fits under any limit\n")
	large := make([]byte, 1<<20)
	base := make([]byte, 64<<10)
	// A delta from base to large: the two sizes, then 16 copies of all of base.
	delta := slices.Concat([]byte{0x80, 0x80, 0x04, 0x80, 0x80, 0x40}, bytes.Repeat([]byte{0x80}, 16))
	cases := map[string]struct {
		data  []byte
		write func(t *testing.T, objects string)
	}{
		"compressed bytes of a pack entry": {small, func(t *testing.T, objects string) {
			writeRawPack(t, filepath.Join(objects, "pack"), []rawEntry{
				{rawName(t, "blob", small), slices.Concat(entryHead(objBlob, len(small)), padded(small, 1<<18))},
			})
		}},
		"inflated size of a pack entry": {large, func(t *testing.T, objects string) {
			writeRawPack(t, filepath.Join(objects, "pack"), []rawEntry{
				{rawName(t, "blob", large), slices.Concat(entryHead(objBlob, len(large)), deflated(t, large))},
			})
		}},
		"size that a delta rebuilds": {large, func(t *testing.T, objects string) {
			whole := slices.Concat(entryHead(objBlob, len(base)), deflated(t, base))
			writeRawPack(t, filepath.Join(objects, "pack"), []rawEntry{
				{rawName(t, "blob", base), whole},
				{rawName(t, "blob", large), slices.Concat(entryHead(objOfsDelta, len(delta)), ofsBase(len(whole)), deflated(t, delta))},
			})
		}},
		"bytes of a loose file": {small, func(t *testing.T, objects string) {
			id := objectName("blob", small)
			content := slices.Concat([]byte("blob "+strconv.Itoa(len(small))+"\x00"), small)
			writeFile(t, filepath.Join(objects, id[:2], id[2:]), string(padded(content, 1<<18)))
		}},
		"inflated size of a loose object": {large, func(t *testing.T, objects string) {
			writeLooseObject(t, objects, "blob", string(large))
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			objects := filepath.Join(testLongPath(t, t.TempDir()), "objects")
			tc.write(t, objects)
			id := objectName("blob", tc.data)
			if kind, got, ok := openRawStore(t, objects, productionProfile.limits).object(id); !ok || kind != "blob" || !bytes.Equal(got, tc.data) {
				t.Fatalf("under the production limit, object = %q of %d bytes, %v", kind, len(got), ok)
			}
			limits := productionProfile.limits
			limits.readBytes = limit
			store := openRawStore(t, objects, limits)
			if _, _, ok := store.object(id); ok || store.failure() != "source_git" {
				t.Fatalf("under a limit of %d bytes, object = %v, reason %q", limit, ok, store.failure())
			}
		})
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
	// twoPacks leaves the fixture's objects in one pack and a commit that a tag
	// names in a second one.
	twoPacks := func(f *fixture) {
		repack(f)
		f.gitIn(f.repo, "update-ref", "refs/tags/second-pack", commit(f, "-m", "a commit in a second pack"))
		f.gitIn(f.repo, "repack", "-d", "-q")
	}
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
				twoPacks(f)
				return int64(len(packFiles(f, "pack-*.idx")))
			},
		},
		"pack directory entries": {
			func(l *objectLimits, n int64) { l.packEntries = int(n) },
			func(f *fixture) int64 { repack(f); return int64(len(packFiles(f, "*"))) },
		},
		// Each of the two indexes fits under the bound; only their total does not.
		"index bytes": {
			func(l *objectLimits, n int64) { l.indexBytes = n },
			func(f *fixture) int64 {
				twoPacks(f)
				if n := len(packFiles(f, "pack-*.idx")); n != 2 {
					f.t.Fatalf("%d indexes", n)
				}
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
