package store

import (
	"encoding/binary"
	"encoding/json"
	"math"

	bolt "go.etcd.io/bbolt"
)

var (
	bItemSizes = []byte("item_sizes_v1")
	kItemSizes = []byte("item_sizes_v1_totals")
)

// Pretty counts the item at depth two, excluding its first-line indentation.
// These are derived indexes only: building them never changes raw ledger bytes,
// revisions, state IDs, leases, history or operation receipts.
type documentSize struct {
	Count, Raw, Compact, Pretty uint64
}

func (s documentSize) bytes() []byte {
	var b [32]byte
	for i, v := range []uint64{s.Count, s.Raw, s.Compact, s.Pretty} {
		binary.BigEndian.PutUint64(b[i*8:], v)
	}
	return b[:]
}

func decodeSize(raw []byte) (documentSize, error) {
	if len(raw) != 32 {
		return documentSize{}, invalid("invalid_document_size_index")
	}
	return documentSize{Count: binary.BigEndian.Uint64(raw), Raw: binary.BigEndian.Uint64(raw[8:]),
		Compact: binary.BigEndian.Uint64(raw[16:]), Pretty: binary.BigEndian.Uint64(raw[24:])}, nil
}

func sumSize(a, b documentSize) (documentSize, error) {
	if a.Count > math.MaxUint64-b.Count || a.Raw > math.MaxUint64-b.Raw || a.Compact > math.MaxUint64-b.Compact || a.Pretty > math.MaxUint64-b.Pretty {
		return documentSize{}, invalid("invalid_document_size_index")
	}
	return documentSize{a.Count + b.Count, a.Raw + b.Raw, a.Compact + b.Compact, a.Pretty + b.Pretty}, nil
}

func subtractSize(a, b documentSize) (documentSize, error) {
	if a.Count < b.Count || a.Raw < b.Raw || a.Compact < b.Compact || a.Pretty < b.Pretty {
		return documentSize{}, invalid("invalid_document_size_index")
	}
	return documentSize{a.Count - b.Count, a.Raw - b.Raw, a.Compact - b.Compact, a.Pretty - b.Pretty}, nil
}

// measureJSON counts compact and one-space pretty bytes directly from the raw
// tokens, retaining number spellings and escapes. It does not allocate another
// complete document or a potentially much larger pretty representation.
func measureJSON(raw []byte, baseDepth uint64) (documentSize, error) {
	if !json.Valid(raw) {
		return documentSize{}, invalid("invalid_json")
	}
	result := documentSize{Count: 1, Raw: uint64(len(raw))}
	var depth uint64
	var previous byte
	quoted, escaped := false, false
	newline := func() { result.Pretty += 1 + depth + baseDepth }
	for i, ch := range raw {
		if quoted {
			result.Compact++
			result.Pretty++
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			previous = ch
			continue
		}
		if jsonSpace(ch) {
			continue
		}
		result.Compact++
		result.Pretty++
		switch ch {
		case '"':
			quoted = true
		case '{', '[':
			depth++
			next := i + 1
			for next < len(raw) && jsonSpace(raw[next]) {
				next++
			}
			if next < len(raw) && !(ch == '{' && raw[next] == '}' || ch == '[' && raw[next] == ']') {
				newline()
			}
		case '}', ']':
			if depth == 0 {
				return documentSize{}, invalid("invalid_json")
			}
			depth--
			if !(ch == '}' && previous == '{' || ch == ']' && previous == '[') {
				newline()
			}
		case ',':
			newline()
		case ':':
			result.Pretty++
		}
		previous = ch
	}
	return result, nil
}

func jsonSpace(ch byte) bool { return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' }

func ensureDocumentSizeIndex(tx *bolt.Tx) error {
	control := tx.Bucket(bControl)
	if tx.Bucket(bItemSizes) != nil {
		_, err := decodeSize(control.Get(kItemSizes))
		return err
	}
	index, err := tx.CreateBucket(bItemSizes)
	if err != nil {
		return err
	}
	var total documentSize
	err = tx.Bucket(bItems).ForEach(func(k, raw []byte) error {
		size, err := measureJSON(raw, 2)
		if err != nil {
			return err
		}
		total, err = sumSize(total, size)
		if err != nil {
			return err
		}
		return index.Put(k, size.bytes())
	})
	if err != nil {
		return err
	}
	return control.Put(kItemSizes, total.bytes())
}

func updateItemSize(tx *bolt.Tx, k, raw []byte) error {
	size, err := measureJSON(raw, 2)
	if err != nil {
		return err
	}
	if size.Raw > MaxDocumentBytes || size.Compact > MaxDocumentBytes || size.Pretty > MaxDocumentBytes {
		return ErrDocumentTooLarge
	}
	index := tx.Bucket(bItemSizes)
	total, err := decodeSize(tx.Bucket(bControl).Get(kItemSizes))
	if err != nil {
		return err
	}
	if old := index.Get(k); old != nil {
		prior, err := decodeSize(old)
		if err != nil {
			return err
		}
		total, err = subtractSize(total, prior)
		if err != nil {
			return err
		}
	} else if tx.Bucket(bItems).Get(k) != nil {
		return invalid("invalid_document_size_index")
	}
	total, err = sumSize(total, size)
	if err != nil {
		return err
	}
	if err := index.Put(k, size.bytes()); err != nil {
		return err
	}
	return tx.Bucket(bControl).Put(kItemSizes, total.bytes())
}

func removeItemSize(tx *bolt.Tx, k []byte) error {
	index := tx.Bucket(bItemSizes)
	size, err := decodeSize(index.Get(k))
	if err != nil {
		return err
	}
	total, err := decodeSize(tx.Bucket(bControl).Get(kItemSizes))
	if err != nil {
		return err
	}
	total, err = subtractSize(total, size)
	if err != nil {
		return err
	}
	if err := index.Delete(k); err != nil {
		return err
	}
	return tx.Bucket(bControl).Put(kItemSizes, total.bytes())
}

func fitsDocument(terms ...uint64) bool {
	remaining := uint64(MaxDocumentBytes)
	for _, term := range terms {
		if term > remaining {
			return false
		}
		remaining -= term
	}
	return true
}

func checkDocumentSize(tx *bolt.Tx, meta []byte) error {
	root, err := object(meta)
	if err != nil {
		return err
	}
	root["items"] = json.RawMessage("[]")
	base, err := measureJSON(encodeObject(root), 0)
	if err != nil {
		return err
	}
	total, err := decodeSize(tx.Bucket(bControl).Get(kItemSizes))
	if err != nil {
		return err
	}
	if total.Count == 0 {
		if !fitsDocument(base.Raw) || !fitsDocument(base.Compact) || !fitsDocument(base.Pretty) {
			return ErrDocumentTooLarge
		}
		return nil
	}
	// Replacing [] adds item bytes and n-1 commas in raw/compact form. In
	// pretty form each depth-two item adds its own indentation and separator;
	// newline indentation inside each item is already included in its index.
	if total.Count > MaxDocumentBytes/4 ||
		!fitsDocument(base.Raw, total.Raw, total.Count-1) ||
		!fitsDocument(base.Compact, total.Compact, total.Count-1) ||
		!fitsDocument(base.Pretty, total.Pretty, total.Count*4, 1) {
		return ErrDocumentTooLarge
	}
	return nil
}
