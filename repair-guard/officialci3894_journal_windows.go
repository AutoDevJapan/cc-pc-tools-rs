package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"
	"unsafe"

	oc "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/internal/officialci3893"
)

// Only one purpose has this journal. The CLI never accepts its location, raw
// receipt bytes, signing inputs, or a phase. Authenticated frames are append-only
// through the original exclusive handle; a torn tail is retained and refused.
type official3894Ticket struct {
	Domain          string
	Source          official3894Source
	Started         time.Time
	CreatorPhase    string
	IssuerOutput    []byte
	IssuerIdentity  oc.Identity
	Owned           []byte
	Receipt         *oc.Receipt
	Requalification *oc.Requalification `json:",omitempty"`
}
type official3894Frame struct {
	Domain   string
	Sequence int
	Previous string
	Journal  oc.Identity
	Ticket   official3894Ticket
}
type official3894Journal struct {
	path     string
	fs       skillsFS
	file     *os.File
	identity oc.Identity
	sequence int
	previous string
	ticket   official3894Ticket
	poisoned bool
	closed   bool
}

var _ oc.Journal = (*official3894Journal)(nil)
var _ oc.RequalificationJournal = (*official3894Journal)(nil)

func official3894Identity(f *os.File) (oc.Identity, error) {
	var i syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &i) != nil || i.FileAttributes&0x400 != 0 || i.NumberOfLinks != 1 || oc.DefaultStreamOnly(f.Name()) != nil {
		return oc.Identity{}, oc.ErrDenied
	}
	return oc.Identity{Volume: i.VolumeSerialNumber, FileID: uint64(i.FileIndexHigh)<<32 | uint64(i.FileIndexLow), Birth: uint64(i.CreationTime.HighDateTime)<<32 | uint64(i.CreationTime.LowDateTime)}, nil
}

func official3894Seal(input []byte, protect bool) ([]byte, bool) {
	if len(input) == 0 || len(input) > 16<<20 {
		return nil, false
	}
	in := ownerIdentityBlob{Size: uint32(len(input)), Data: &input[0]}
	var out ownerIdentityBlob
	entropy := []byte(official3894Domain)
	salt := ownerIdentityBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	name := "CryptUnprotectData"
	if protect {
		name = "CryptProtectData"
	}
	ok, _, _ := syscall.NewLazyDLL("crypt32.dll").NewProc(name).Call(uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&salt)), 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if out.Data != nil {
		defer syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree").Call(uintptr(unsafe.Pointer(out.Data)))
	}
	if ok == 0 || out.Data == nil || out.Size == 0 || out.Size > 16<<20 {
		return nil, false
	}
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), true
}

func official3894OpenStore(path string, fresh bool) (*official3894Journal, error) {
	s := &official3894Journal{path: filepath.Clean(path), fs: skillsFS{blocked: func(p string) bool { return legacyMatch(p) || ownerCardPath(p) }}}
	fail := func() (*official3894Journal, error) { s.close(); return nil, oc.ErrDenied }
	if !filepath.IsAbs(path) || s.fs.blocked(path) || ownerWindowsAlias(path) {
		return fail()
	}
	parent, ok := s.fs.directory(filepath.Dir(s.path))
	if !ok {
		return fail()
	}
	if fresh {
		// Check metadata only. Neither an old partial record nor an IO failure is
		// an absent record that permits another root.
		if _, e := os.Lstat(s.path); !os.IsNotExist(e) {
			return fail()
		}
		if _, e := os.Lstat(s.path + ".consuming"); !os.IsNotExist(e) {
			return fail()
		}
		return s, nil
	}
	h, ok := skillsOpenAt(parent, filepath.Base(s.path), false, false)
	if !ok {
		return fail()
	}
	// The normal read handle pins the original object. This operation opens an
	// exclusive read/write handle instead; no other writer/rename can enter.
	_ = syscall.CloseHandle(h)
	u, e := syscall.UTF16PtrFromString(s.path)
	if e != nil {
		return fail()
	}
	h, e = syscall.CreateFile(u, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
	if e != nil {
		return fail()
	}
	s.file = os.NewFile(uintptr(h), s.path)
	s.identity, e = official3894Identity(s.file)
	if e != nil {
		return fail()
	}
	if e = s.read(); e != nil {
		return fail()
	}
	return s, nil
}
func (s *official3894Journal) close() {
	if s.closed {
		return
	}
	s.closed = true
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	s.fs.close()
	s.fs.files = nil
	s.fs.dirs = nil
}

func (s *official3894Journal) start(t official3894Ticket) error {
	if s.file != nil || t.Domain != official3894Domain || t.Receipt != nil || t.Requalification != nil || len(t.Owned) != 0 {
		return oc.ErrDenied
	}
	parent, ok := s.fs.directory(filepath.Dir(s.path))
	if !ok {
		return oc.ErrDenied
	}
	h, ok := skillsOpenAt(parent, filepath.Base(s.path), false, true)
	if !ok {
		return oc.ErrDenied
	}
	s.file = os.NewFile(uintptr(h), s.path)
	var e error
	s.identity, e = official3894Identity(s.file)
	if e != nil {
		return e
	}
	return s.append(t)
}

func (s *official3894Journal) read() error {
	if s.file == nil || s.poisoned {
		return oc.ErrDenied
	}
	actual, e := official3894Identity(s.file)
	if e != nil || actual != s.identity {
		return oc.ErrDenied
	}
	if _, e = s.file.Seek(0, io.SeekStart); e != nil {
		return e
	}
	raw, e := io.ReadAll(io.LimitReader(s.file, 64<<20+1))
	if e != nil || len(raw) == 0 || len(raw) > 64<<20 {
		return oc.ErrDenied
	}
	sequence := 0
	previous := ""
	var ticket official3894Ticket
	for len(raw) > 0 {
		if len(raw) < 4 || sequence >= 16 {
			return oc.ErrDenied
		}
		n := int(binary.LittleEndian.Uint32(raw[:4]))
		raw = raw[4:]
		if n < 1 || n > 16<<20 || n > len(raw) {
			return oc.ErrDenied
		}
		sealed := raw[:n]
		raw = raw[n:]
		plain, ok := official3894Seal(sealed, false)
		if !ok {
			return oc.ErrDenied
		}
		var f official3894Frame
		// Authenticate and parse the SAME buffer, never reopen between checks.
		if !skillsStrict(plain, &f) || f.Domain != official3894Domain || f.Sequence != sequence || f.Previous != previous || f.Journal != s.identity || f.Ticket.Domain != official3894Domain {
			return oc.ErrDenied
		}
		if sequence > 0 && (f.Ticket.Source != ticket.Source || !f.Ticket.Started.Equal(ticket.Started) || (len(ticket.Owned) > 0 && (!bytes.Equal(f.Ticket.Owned, ticket.Owned) || f.Ticket.IssuerIdentity != ticket.IssuerIdentity))) {
			return oc.ErrDenied
		}
		if !official3894RequalificationTransition(ticket, f.Ticket) {
			return oc.ErrDenied
		}
		ticket = f.Ticket
		sequence++
		previous = official3894Hash(sealed)
	}
	s.sequence = sequence
	s.previous = previous
	s.ticket = ticket
	return nil
}

func (s *official3894Journal) append(t official3894Ticket) error {
	if s.file == nil || s.poisoned || s.sequence >= 16 {
		return oc.ErrDenied
	}
	actual, e := official3894Identity(s.file)
	if e != nil || actual != s.identity {
		return oc.ErrDenied
	}
	if s.sequence > 0 && (t.Domain != s.ticket.Domain || t.Source != s.ticket.Source || !t.Started.Equal(s.ticket.Started) || (len(s.ticket.Owned) > 0 && (!bytes.Equal(t.Owned, s.ticket.Owned) || t.IssuerIdentity != s.ticket.IssuerIdentity))) {
		return oc.ErrDenied
	}
	if !official3894RequalificationTransition(s.ticket, t) {
		return oc.ErrDenied
	}
	plain, e := json.Marshal(official3894Frame{official3894Domain, s.sequence, s.previous, s.identity, t})
	if e != nil {
		return e
	}
	sealed, ok := official3894Seal(plain, true)
	if !ok {
		return oc.ErrDenied
	}
	frame := make([]byte, 4+len(sealed))
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(sealed)))
	copy(frame[4:], sealed)
	if _, e = s.file.Seek(0, io.SeekEnd); e != nil {
		return e
	}
	if n, e := s.file.Write(frame); e != nil || n != len(frame) {
		s.poisoned = true
		return oc.ErrDenied
	}
	if e = s.file.Sync(); e != nil {
		s.poisoned = true
		return e
	}
	s.ticket = t
	s.previous = official3894Hash(sealed)
	s.sequence++
	return nil
}

func (s *official3894Journal) Reserve(r oc.Receipt) error {
	if s.read() != nil || s.ticket.Receipt != nil || len(s.ticket.Owned) == 0 || r.State != oc.Pending {
		return oc.ErrDenied
	}
	t := s.ticket
	t.Receipt = &r
	return s.append(t)
}
func (s *official3894Journal) Load(id string) (oc.Receipt, error) {
	if s.read() != nil || s.ticket.Receipt == nil || s.ticket.Receipt.ID != id {
		return oc.Receipt{}, oc.ErrDenied
	}
	r := *s.ticket.Receipt
	if r.State == oc.Complete {
		if _, e := os.Lstat(s.path + ".consuming"); !os.IsNotExist(e) {
			return oc.Receipt{}, oc.ErrDenied
		}
	}
	return r, nil
}
func (s *official3894Journal) Replace(old, next oc.Receipt) error {
	if s.read() != nil || s.ticket.Receipt == nil || !reflect.DeepEqual(*s.ticket.Receipt, old) || old.ID != next.ID {
		return oc.ErrDenied
	}
	if old.State == oc.Complete && next.State == oc.Consuming {
		if e := s.markConsuming(old.ID); e != nil {
			return e
		}
	}
	t := s.ticket
	t.Receipt = &next
	return s.append(t)
}

func official3894RequalificationTransition(before, next official3894Ticket) bool {
	if before.Requalification != nil {
		return before.Receipt != nil && before.Receipt.State == oc.Consuming &&
			next.Receipt != nil && (next.Receipt.State == oc.Consumed || next.Receipt.State == oc.Failed) &&
			reflect.DeepEqual(before.Requalification, next.Requalification) && next.Requalification.ValidFor(*next.Receipt)
	}
	if next.Requalification == nil {
		return true
	}
	return before.Receipt != nil && next.Receipt != nil && before.Receipt.State == oc.Complete &&
		next.Receipt.State == oc.Consuming && next.Requalification.ValidFor(*before.Receipt) && next.Requalification.ValidFor(*next.Receipt)
}

func (s *official3894Journal) markConsuming(id string) error {
	// Independent CreateNew replay marker survives a torn terminal write or
	// restoration of an older authenticated journal prefix. Never adopt one.
	parent, ok := s.fs.directory(filepath.Dir(s.path))
	if !ok {
		return oc.ErrDenied
	}
	h, ok := skillsOpenAt(parent, filepath.Base(s.path)+".consuming", false, true)
	if !ok {
		return oc.ErrDenied
	}
	f := os.NewFile(uintptr(h), s.path+".consuming")
	b, e := json.Marshal(struct {
		Journal oc.Identity
		Receipt string
	}{s.identity, id})
	if e == nil {
		var n int
		n, e = f.Write(b)
		if e == nil && n != len(b) {
			e = io.ErrShortWrite
		}
	}
	if e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e != nil || closed != nil {
		return oc.ErrDenied
	}
	return nil
}

func (s *official3894Journal) RequalifyForConsumption(old oc.Receipt, q oc.Requalification) error {
	if s.read() != nil || s.ticket.Receipt == nil || !reflect.DeepEqual(*s.ticket.Receipt, old) ||
		old.State != oc.Complete || s.ticket.Requalification != nil || !q.ValidFor(old) || s.sequence+2 > 16 {
		return oc.ErrDenied
	}
	if e := s.markConsuming(old.ID); e != nil {
		return e
	}
	next := old
	next.State = oc.Consuming
	t := s.ticket
	t.Receipt, t.Requalification = &next, &q
	return s.append(t)
}
