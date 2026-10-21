package windowsrunner

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const maxControl = 32768
const maxSource = 256 << 20

func writeFrame(w io.Writer, b []byte) error {
	if len(b) == 0 || len(b) > maxSource {
		return errors.New("frame size rejected")
	}
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	if _, e := io.Copy(w, bytes.NewReader(n[:])); e != nil {
		return e
	}
	_, e := io.Copy(w, bytes.NewReader(b))
	return e
}
func readFrame(r io.Reader, max int) ([]byte, error) {
	var n [4]byte
	if _, e := io.ReadFull(r, n[:]); e != nil {
		return nil, e
	}
	size := binary.BigEndian.Uint32(n[:])
	if size == 0 || uint64(size) > uint64(max) {
		return nil, errors.New("frame size rejected")
	}
	b := make([]byte, int(size))
	_, e := io.ReadFull(r, b)
	if e != nil {
		clear(b)
		return nil, e
	}
	return b, nil
}
func send(w io.Writer, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return errors.New("frame encoding failed")
	}
	defer clear(b)
	if len(b) > maxControl {
		return errors.New("control size rejected")
	}
	return writeFrame(w, b)
}
func receive(r io.Reader, v any) error {
	b, e := readFrame(r, maxControl)
	if e != nil {
		return e
	}
	defer clear(b)
	return decode(b, v)
}
func decode(b []byte, v any) error {
	// Reject duplicate keys before decoding into maps or case-insensitive fields.
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		del, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch del {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok {
					return errors.New("JSON key rejected")
				}
				s = strings.ToLower(s)
				if seen[s] {
					return errors.New("duplicate JSON key")
				}
				seen[s] = true
				if e = walk(); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e := walk(); e != nil {
					return e
				}
			}
		default:
			return errors.New("JSON delimiter rejected")
		}
		_, e = d.Token()
		return e
	}
	if e := walk(); e != nil {
		return errors.New("JSON structure rejected")
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("JSON trailing data")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("JSON fields rejected")
	}
	return nil
}
