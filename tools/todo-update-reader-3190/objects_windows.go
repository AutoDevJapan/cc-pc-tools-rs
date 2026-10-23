package main

import (
	"bytes"
	"compress/zlib"
	"container/heap"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Bounds of the object reads. The commits and trees read here, and the delta
// bases on the way to them, are far smaller than maxObjectBytes.
const (
	maxObjectBytes = 16 << 20
	maxLooseBytes  = 16 << 20
	maxIndexBytes  = 256 << 20
	maxPackBytes   = 1 << 40
	maxDeltaChain  = 4096
	maxCommits     = 1 << 20
	maxTreeDepth   = 128
	maxTreeEntries = 1 << 20
	maxCachedBytes = 64 << 20
)

// Pack entry types, as Git's pack format numbers them.
const (
	objCommit   = 1
	objTree     = 2
	objBlob     = 3
	objTag      = 4
	objOfsDelta = 6
	objRefDelta = 7
)

var objectKinds = map[int]string{objCommit: "commit", objTree: "tree", objBlob: "blob", objTag: "tag"}

var packIndexName = regexp.MustCompile(`^pack-[0-9a-f]{40}\.idx$`)

// treeModes maps each mode Git writes in a tree to the type and the mode that
// ls-tree prints for the entry.
var treeModes = map[string]treeEntry{
	"100644": {mode: "100644", kind: "blob"},
	"100755": {mode: "100755", kind: "blob"},
	"120000": {mode: "120000", kind: "blob"},
	"160000": {mode: "160000", kind: "commit"},
	"40000":  {mode: "040000", kind: "tree"},
}

// sourceObjects is the fixed source's own object directory and the shallow
// file that the reader has already read itself.
type sourceObjects struct {
	dir     string
	shallow []byte
}

// objectStore reads objects the way Git's loose and pack backends store them,
// from the source's own object directory only, and never runs Git. It reads
// no alternates, commit-graph, multi-pack index or replacement ref, so a file
// added to the shared repository while it reads cannot bring in another
// store. The guard classifies every file before it is opened, each file is
// read from a plain single-link handle that denies writes, and every object
// is checked against its name before it is used. The result therefore rests
// on the objects' names, and the store's files are not rechecked.
type objectStore struct {
	s       *session
	dir     string
	limits  objectLimits
	packs   []*pack
	shallow map[string]bool
	reason  string
	cache   map[packedAt]packedObject
	cached  int
	spent   int64
}

type packedAt struct {
	p   *pack
	off int64
}

type packedObject struct {
	kind int
	data []byte
}

// pack is one pack file and its version 2 index.
type pack struct {
	path     string
	count    int
	fanout   [256]uint32
	names    []byte
	offsets  []byte
	large    []byte
	checksum []byte
	file     *pinnedFile
	failed   bool
}

// entryHeader is the start of a pack entry: its type, the size of its
// inflated data, where that data starts and, for a delta, where its base is.
type entryHeader struct {
	kind   int
	size   int64
	data   int64
	base   int64
	baseID []byte
}

type namedEntry struct {
	name  string
	entry treeEntry
}

type queuedCommit struct {
	id      string
	parents []string
	when    int64
	order   int
}

// commitQueue holds queued commits newest first, then in the order queued.
type commitQueue []queuedCommit

func (q commitQueue) Len() int { return len(q) }

func (q commitQueue) Less(i, j int) bool {
	if q[i].when != q[j].when {
		return q[i].when > q[j].when
	}
	return q[i].order < q[j].order
}

func (q commitQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *commitQueue) Push(x any) { *q = append(*q, x.(queuedCommit)) }

func (q *commitQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

// openObjects lists the store's packs and reads their indexes. The guard
// classifies the pack directory, then every index and pack, before any is
// opened. An index without its pack is skipped, as Git skips it, and no other
// file of the directory is opened.
func (s *session) openObjects(src sourceObjects, limits objectLimits) (*objectStore, string) {
	o := &objectStore{s: s, dir: src.dir, limits: limits, shallow: map[string]bool{}, cache: map[packedAt]packedObject{}}
	if text, ok := strings.CutSuffix(string(src.shallow), "\n"); ok {
		for _, id := range strings.Split(text, "\n") {
			o.shallow[id] = true
		}
	}
	dir := filepath.Join(src.dir, "pack")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return o, ""
	} else if err != nil {
		return nil, "source_git"
	}
	if !guardClear(s.env, dir) {
		return nil, "guard_classification"
	}
	if !s.pinDir(dir) {
		return nil, "source_git"
	}
	if s.env.beforeList != nil {
		s.env.beforeList(dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "source_git"
	}
	present := map[string]bool{}
	for _, e := range entries {
		present[e.Name()] = true
	}
	var paths []string
	for _, e := range entries {
		packName := strings.TrimSuffix(e.Name(), ".idx") + ".pack"
		if packIndexName.MatchString(e.Name()) && present[packName] {
			paths = append(paths, filepath.Join(dir, e.Name()), filepath.Join(dir, packName))
		}
	}
	if !s.classifyPaths(paths) {
		return nil, "guard_classification"
	}
	for i := 0; i < len(paths); i += 2 {
		f, ok := s.openObject(paths[i], maxIndexBytes)
		if !ok {
			return nil, "source_git"
		}
		data, _, ok := s.read(f)
		f.file.Close()
		p := &pack{path: paths[i+1]}
		if !ok || !p.parseIndex(data) {
			return nil, "source_git"
		}
		o.packs = append(o.packs, p)
	}
	return o, ""
}

func (o *objectStore) close() {
	for _, p := range o.packs {
		if p.file != nil {
			p.file.file.Close()
			p.file = nil
		}
	}
}

// refuse keeps the first reason why an object could not be read.
func (o *objectStore) refuse(reason string) {
	if o.reason == "" {
		o.reason = reason
	}
}

// failure is the reason code of the first refusal: source_git unless the
// guard refused a file of the store.
func (o *objectStore) failure() string {
	if o.reason == "" {
		return "source_git"
	}
	return o.reason
}

// parseIndex reads a version 2 pack index: the fanout table, the sorted
// object names, their CRCs and offsets, the large offsets and the checksum of
// the pack. The index's own trailing checksum must match its bytes.
func (p *pack) parseIndex(data []byte) bool {
	const head = 8 + 256*4
	if len(data) < head+40 || !bytes.Equal(data[:8], []byte{0xff, 't', 'O', 'c', 0, 0, 0, 2}) {
		return false
	}
	if sum := sha1.Sum(data[:len(data)-20]); !bytes.Equal(sum[:], data[len(data)-20:]) {
		return false
	}
	for i := range p.fanout {
		p.fanout[i] = binary.BigEndian.Uint32(data[8+4*i:])
		if i > 0 && p.fanout[i] < p.fanout[i-1] {
			return false
		}
	}
	body, count := len(data)-head-40, int(p.fanout[255])
	if count > body/28 || (body-28*count)%8 != 0 {
		return false
	}
	p.count = count
	p.names = data[head : head+20*count]
	p.offsets = data[head+24*count : head+28*count]
	p.large = data[head+28*count : len(data)-40]
	p.checksum = data[len(data)-40 : len(data)-20]
	for i := range count {
		name := p.names[20*i : 20*i+20]
		lo := uint32(0)
		if name[0] > 0 {
			lo = p.fanout[name[0]-1]
		}
		if uint32(i) < lo || uint32(i) >= p.fanout[name[0]] || i > 0 && bytes.Compare(p.names[20*i-20:20*i], name) >= 0 {
			return false
		}
	}
	return true
}

// find returns the index position of the object named id.
func (p *pack) find(id []byte) (int, bool) {
	lo, hi := 0, int(p.fanout[id[0]])
	if id[0] > 0 {
		lo = int(p.fanout[id[0]-1])
	}
	i := lo + sort.Search(hi-lo, func(k int) bool { return bytes.Compare(p.names[20*(lo+k):20*(lo+k)+20], id) >= 0 })
	return i, i < hi && bytes.Equal(p.names[20*i:20*i+20], id)
}

// offset returns where the entry at index position i starts in the pack.
func (p *pack) offset(i int) (int64, bool) {
	v := binary.BigEndian.Uint32(p.offsets[4*i:])
	if v&0x80000000 == 0 {
		return int64(v), true
	}
	j := int(v & 0x7fffffff)
	if j >= len(p.large)/8 {
		return 0, false
	}
	off := binary.BigEndian.Uint64(p.large[8*j:])
	return int64(off), off < 1<<62
}

// readAt reads exactly len(buf) bytes at off from a held file.
func readAt(f *pinnedFile, buf []byte, off int64) bool {
	n, err := f.file.ReadAt(buf, off)
	return n == len(buf) && (err == nil || err == io.EOF)
}

// openPack opens a pack the first time an object is found in its index. The
// pack's header must count the index's objects, and its trailer must be the
// pack checksum the index recorded.
func (o *objectStore) openPack(p *pack) bool {
	if p.file != nil || p.failed {
		return p.file != nil
	}
	p.failed = true
	f, ok := o.s.openObject(p.path, maxPackBytes)
	if !ok {
		return false
	}
	if o.s.env.beforeRead != nil {
		o.s.env.beforeRead(p.path)
	}
	head, tail := make([]byte, 12), make([]byte, 20)
	ok = f.size >= 32 && readAt(f, head, 0) && readAt(f, tail, f.size-20)
	version := binary.BigEndian.Uint32(head[4:8])
	if !ok || !bytes.Equal(head[:4], []byte("PACK")) || (version != 2 && version != 3) ||
		binary.BigEndian.Uint32(head[8:12]) != uint32(p.count) || !bytes.Equal(tail, p.checksum) {
		f.file.Close()
		return false
	}
	p.file, p.failed = f, false
	return true
}

// header reads the entry at off as Git's unpack_object_header and
// get_delta_base do.
func (p *pack) header(off int64) (entryHeader, bool) {
	end := p.file.size - 20
	if off < 12 || off >= end {
		return entryHeader{}, false
	}
	buf := make([]byte, min(32, end-off))
	if !readAt(p.file, buf, off) {
		return entryHeader{}, false
	}
	c := buf[0]
	h := entryHeader{kind: int(c >> 4 & 7), size: int64(c & 15)}
	i := 1
	for shift := 4; c&0x80 != 0; shift += 7 {
		if i == len(buf) || shift > 56 {
			return entryHeader{}, false
		}
		c = buf[i]
		i++
		h.size |= int64(c&0x7f) << shift
	}
	switch h.kind {
	case objCommit, objTree, objBlob, objTag:
	case objOfsDelta:
		if i == len(buf) {
			return entryHeader{}, false
		}
		c = buf[i]
		i++
		rel := int64(c & 0x7f)
		for c&0x80 != 0 {
			if i == len(buf) || rel >= 1<<48 {
				return entryHeader{}, false
			}
			c = buf[i]
			i++
			rel = (rel+1)<<7 | int64(c&0x7f)
		}
		if h.base = off - rel; rel <= 0 || h.base <= 0 {
			return entryHeader{}, false
		}
	case objRefDelta:
		if i+20 > len(buf) {
			return entryHeader{}, false
		}
		h.baseID = buf[i : i+20]
		i += 20
	default:
		return entryHeader{}, false
	}
	h.data = off + int64(i)
	return h, h.size <= maxObjectBytes
}

// inflate decompresses the zlib stream at at. It must hold exactly size bytes
// and end there with a matching checksum.
func (p *pack) inflate(at, size int64) ([]byte, bool) {
	z, err := zlib.NewReader(io.NewSectionReader(p.file.file, at, p.file.size-20-at))
	if err != nil {
		return nil, false
	}
	defer z.Close()
	return inflateExact(z, size)
}

func inflateExact(z io.Reader, size int64) ([]byte, bool) {
	data := make([]byte, size)
	if _, err := io.ReadFull(z, data); err != nil {
		return nil, false
	}
	n, err := z.Read(make([]byte, 1))
	return data, n == 0 && err == io.EOF
}

// entry inflates the entry at off and applies its deltas against bases in the
// same pack, as Git's unpack_entry does for a pack stored on disk.
func (o *objectStore) entry(p *pack, off int64, depth int) (int, []byte, bool) {
	if cached, ok := o.cache[packedAt{p, off}]; ok {
		return cached.kind, cached.data, true
	}
	h, ok := p.header(off)
	if !ok || depth > maxDeltaChain {
		return 0, nil, false
	}
	data, ok := p.inflate(h.data, h.size)
	if !ok {
		return 0, nil, false
	}
	kind := h.kind
	if kind == objOfsDelta || kind == objRefDelta {
		base := h.base
		if kind == objRefDelta {
			i, in := p.find(h.baseID)
			if !in {
				return 0, nil, false
			}
			if base, ok = p.offset(i); !ok {
				return 0, nil, false
			}
		}
		baseKind, baseData, ok := o.entry(p, base, depth+1)
		if !ok {
			return 0, nil, false
		}
		if data, ok = applyDelta(baseData, data); !ok {
			return 0, nil, false
		}
		kind = baseKind
	}
	if o.cached+len(data) <= maxCachedBytes {
		o.cache[packedAt{p, off}] = packedObject{kind, data}
		o.cached += len(data)
	}
	return kind, data, true
}

// applyDelta rebuilds an object from its base and a delta, as Git's
// patch_delta does: the delta starts with the base's size and the result's
// size, then copies ranges of the base or inserts literal bytes.
func applyDelta(base, delta []byte) ([]byte, bool) {
	if len(delta) < 4 {
		return nil, false
	}
	from, delta, ok := deltaSize(delta)
	if !ok || from != uint64(len(base)) {
		return nil, false
	}
	size, delta, ok := deltaSize(delta)
	if !ok || size > maxObjectBytes {
		return nil, false
	}
	out := make([]byte, 0, size)
	for len(delta) > 0 {
		cmd := delta[0]
		delta = delta[1:]
		switch {
		case cmd&0x80 != 0:
			var off, n uint64
			for bit := range 7 {
				if cmd&(1<<bit) == 0 {
					continue
				}
				if len(delta) == 0 {
					return nil, false
				}
				if bit < 4 {
					off |= uint64(delta[0]) << (8 * bit)
				} else {
					n |= uint64(delta[0]) << (8 * (bit - 4))
				}
				delta = delta[1:]
			}
			if n == 0 {
				n = 0x10000
			}
			if off+n > uint64(len(base)) || n > size-uint64(len(out)) {
				return nil, false
			}
			out = append(out, base[off:off+n]...)
		case cmd != 0:
			n := int(cmd)
			if n > len(delta) || uint64(n) > size-uint64(len(out)) {
				return nil, false
			}
			out = append(out, delta[:n]...)
			delta = delta[n:]
		default:
			return nil, false
		}
	}
	return out, uint64(len(out)) == size
}

// deltaSize reads one of a delta's two leading sizes.
func deltaSize(d []byte) (uint64, []byte, bool) {
	var size uint64
	for shift := 0; shift < 64; shift += 7 {
		if len(d) == 0 {
			return 0, nil, false
		}
		c := d[0]
		d = d[1:]
		size |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return size, d, true
		}
	}
	return 0, nil, false
}

// object returns the type and bytes of the object named id, taken from a pack
// or a loose file of the store and checked against id.
func (o *objectStore) object(id string) (string, []byte, bool) {
	raw, err := hex.DecodeString(id)
	if err != nil || !hexDigest(id, 40) {
		o.refuse("source_git")
		return "", nil, false
	}
	for _, p := range o.packs {
		if i, in := p.find(raw); in {
			kind, data, ok := o.packed(p, i)
			return o.checked(id, kind, data, ok)
		}
	}
	kind, data, ok := o.loose(id)
	return o.checked(id, kind, data, ok)
}

// checked accepts an object only if its name is id.
func (o *objectStore) checked(id, kind string, data []byte, ok bool) (string, []byte, bool) {
	if !ok || kind == "" || objectName(kind, data) != id {
		o.refuse("source_git")
		return "", nil, false
	}
	return kind, data, true
}

// packed reads the object at index position i of p.
func (o *objectStore) packed(p *pack, i int) (string, []byte, bool) {
	off, ok := p.offset(i)
	if !ok || !o.openPack(p) {
		return "", nil, false
	}
	kind, data, ok := o.entry(p, off, 0)
	return objectKinds[kind], data, ok
}

// loose reads the loose file of id. A missing file means that the store does
// not hold the object.
func (o *objectStore) loose(id string) (string, []byte, bool) {
	path := filepath.Join(o.dir, id[:2], id[2:])
	if _, err := os.Lstat(path); err != nil {
		return "", nil, false
	}
	if !guardClear(o.s.env, path) {
		o.refuse("guard_classification")
		return "", nil, false
	}
	f, ok := o.s.openObject(path, maxLooseBytes)
	if !ok {
		return "", nil, false
	}
	data, _, ok := o.s.read(f)
	f.file.Close()
	if !ok {
		return "", nil, false
	}
	return inflateLoose(data)
}

// inflateLoose reads a loose object file: one zlib stream of
// "<type> <size>\x00" and exactly size bytes.
func inflateLoose(data []byte) (string, []byte, bool) {
	z, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", nil, false
	}
	defer z.Close()
	var header []byte
	b := make([]byte, 1)
	for {
		if _, err := io.ReadFull(z, b); err != nil || len(header) == 32 {
			return "", nil, false
		}
		if b[0] == 0 {
			break
		}
		header = append(header, b[0])
	}
	kind, text, _ := strings.Cut(string(header), " ")
	size, err := strconv.ParseInt(text, 10, 64)
	if err != nil || size < 0 || size > maxObjectBytes || strconv.FormatInt(size, 10) != text || !knownKind(kind) {
		return "", nil, false
	}
	content, ok := inflateExact(z, size)
	return kind, content, ok
}

func knownKind(kind string) bool {
	for _, k := range objectKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// commit reads a commit's tree, its parents (none for a shallow boundary, as
// Git's shallow file defines it) and its committer time.
func (o *objectStore) commit(id string) (string, []string, int64, bool) {
	kind, data, ok := o.object(id)
	if !ok {
		return "", nil, 0, false
	}
	tree, parents, when, parsed := parseCommit(data)
	if kind != "commit" || !parsed {
		o.refuse("source_git")
		return "", nil, 0, false
	}
	if o.shallow[id] {
		parents = nil
	}
	return tree, parents, when, true
}

// parseCommit reads the header Git requires of a commit: the tree line first,
// then the parent lines. The committer line's time only orders the walk.
func parseCommit(data []byte) (string, []string, int64, bool) {
	header, _, _ := strings.Cut(string(data), "\n\n")
	lines := strings.Split(header, "\n")
	tree, ok := strings.CutPrefix(lines[0], "tree ")
	if !ok || !hexDigest(tree, 40) {
		return "", nil, 0, false
	}
	var parents []string
	for lines = lines[1:]; len(lines) > 0 && strings.HasPrefix(lines[0], "parent "); lines = lines[1:] {
		id := strings.TrimPrefix(lines[0], "parent ")
		if !hexDigest(id, 40) {
			return "", nil, 0, false
		}
		parents = append(parents, id)
	}
	var when int64
	for _, line := range lines {
		if who, ok := strings.CutPrefix(line, "committer "); ok {
			if i := strings.LastIndexByte(who, '>'); i >= 0 {
				if fields := strings.Fields(who[i+1:]); len(fields) > 0 {
					when, _ = strconv.ParseInt(fields[0], 10, 64)
				}
			}
			break
		}
	}
	return tree, parents, when, true
}

// reaches reports whether target is tip or one of its ancestors. Like Git's
// merge-base walk it reads each commit when it is queued and takes the newest
// first; a shallow boundary has no parents. A commit it cannot read refuses
// the source.
func (o *objectStore) reaches(tip, target string) bool {
	q := &commitQueue{}
	seen := map[string]bool{}
	queue := func(id string) bool {
		_, parents, when, ok := o.commit(id)
		if ok {
			seen[id] = true
			heap.Push(q, queuedCommit{id: id, parents: parents, when: when, order: len(seen)})
		}
		return ok
	}
	if !queue(tip) {
		return false
	}
	for q.Len() > 0 && len(seen) <= maxCommits {
		c := heap.Pop(q).(queuedCommit)
		if c.id == target {
			return true
		}
		for _, parent := range c.parents {
			if !seen[parent] && !queue(parent) {
				return false
			}
		}
	}
	return false
}

// listTree lists every entry below the tree id as `git ls-tree -r --full-tree`
// does: subtrees are entered and not listed; blobs, links and submodule
// commits are listed under their path.
func (o *objectStore) listTree(id string) (map[string]treeEntry, bool) {
	out := map[string]treeEntry{}
	var walk func(id, prefix string, depth int) bool
	walk = func(id, prefix string, depth int) bool {
		kind, data, ok := o.object(id)
		if !ok {
			return false
		}
		if kind != "tree" || depth > maxTreeDepth {
			o.refuse("source_git")
			return false
		}
		entries, ok := parseTreeObject(data)
		if !ok {
			o.refuse("source_git")
			return false
		}
		for _, e := range entries {
			path := prefix + e.name
			if e.entry.kind == "tree" {
				if !walk(e.entry.object, path+"/", depth+1) {
					return false
				}
				continue
			}
			if _, dup := out[path]; dup || len(out) == maxTreeEntries {
				o.refuse("source_git")
				return false
			}
			out[path] = e.entry
		}
		return true
	}
	if !walk(id, "", 0) {
		return nil, false
	}
	return out, len(out) > 0
}

// parseTreeObject reads a tree's entries: "<mode> <name>\x00" and the 20-byte
// object name. It accepts only the modes Git writes, and names that are not
// empty, "." or ".." and contain no "/".
func parseTreeObject(data []byte) ([]namedEntry, bool) {
	var entries []namedEntry
	for len(data) > 0 {
		sp := bytes.IndexByte(data, ' ')
		if sp < 0 {
			return nil, false
		}
		entry, known := treeModes[string(data[:sp])]
		data = data[sp+1:]
		nul := bytes.IndexByte(data, 0)
		if !known || nul < 0 || len(data) < nul+21 {
			return nil, false
		}
		name := string(data[:nul])
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
			return nil, false
		}
		entry.object = hex.EncodeToString(data[nul+1 : nul+21])
		entries = append(entries, namedEntry{name, entry})
		data = data[nul+21:]
	}
	return entries, true
}
