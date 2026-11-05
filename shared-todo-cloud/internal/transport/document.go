package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

// RawMessage keeps key case, missing/null and number spellings intact. No
// domain schema projection is allowed at this boundary.
type document struct {
	meta  map[string]json.RawMessage
	items map[int64]json.RawMessage
	order []int64
}

func parseDocument(b []byte) (*document, error) {
	if len(b) > MaxDocumentBytes {
		return nil, errors.New("todo_remote_document_too_large")
	}
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	var root map[string]json.RawMessage
	if json.Unmarshal(b, &root) != nil || root == nil {
		return nil, errors.New("todo_remote_invalid_document")
	}
	var rows []json.RawMessage
	if json.Unmarshal(root["items"], &rows) != nil || rows == nil {
		return nil, errors.New("todo_remote_invalid_items")
	}
	delete(root, "items")
	d := &document{meta: root, items: make(map[int64]json.RawMessage)}
	for _, row := range rows {
		id, err := itemID(row)
		if err != nil {
			return nil, err
		}
		if _, ok := d.items[id]; ok {
			return nil, errors.New("todo_remote_duplicate_id")
		}
		d.items[id] = row
		d.order = append(d.order, id)
	}
	return d, nil
}
func itemID(b json.RawMessage) (int64, error) {
	var row map[string]json.RawMessage
	var id int64
	if json.Unmarshal(b, &row) != nil || row == nil || json.Unmarshal(row["id"], &id) != nil || id <= 0 {
		return 0, errors.New("todo_remote_invalid_item_id")
	}
	return id, nil
}
func compact(b []byte) []byte {
	var out bytes.Buffer
	if json.Compact(&out, b) != nil {
		return b
	}
	return out.Bytes()
}
func rawEqual(a, b []byte) bool { return bytes.Equal(compact(a), compact(b)) }

// Rust's existing Value reader round-trips f64, so an untouched decimal may
// have a shorter spelling in its candidate. Retain the original raw leaf when
// it denotes the same Rust value, including the distinction between ints and
// floats and between positive and negative zero. Actual edits still win.
func preserveValue(original, next json.RawMessage) json.RawMessage {
	if rawEqual(original, next) {
		return original
	}
	a, b := bytes.TrimSpace(original), bytes.TrimSpace(next)
	if len(a) == 0 || len(b) == 0 {
		return next
	}
	if a[0] == '{' && b[0] == '{' {
		var old, new map[string]json.RawMessage
		if json.Unmarshal(a, &old) != nil || json.Unmarshal(b, &new) != nil {
			return next
		}
		for k, v := range new {
			if was, ok := old[k]; ok {
				new[k] = preserveValue(was, v)
			}
		}
		return wire(new)
	}
	if a[0] == '[' && b[0] == '[' {
		var old, new []json.RawMessage
		if json.Unmarshal(a, &old) != nil || json.Unmarshal(b, &new) != nil || len(old) != len(new) {
			return next
		}
		for i := range new {
			new[i] = preserveValue(old[i], new[i])
		}
		return wire(new)
	}
	if a[0] == '"' && b[0] == '"' {
		var old, new string
		if json.Unmarshal(a, &old) == nil && json.Unmarshal(b, &new) == nil && old == new {
			return original
		}
	}
	if strings.ContainsAny(string(a), ".eE") && strings.ContainsAny(string(b), ".eE") {
		old, e1 := strconv.ParseFloat(string(a), 64)
		new, e2 := strconv.ParseFloat(string(b), 64)
		if e1 == nil && e2 == nil && old == new && math.Signbit(old) == math.Signbit(new) {
			return original
		}
	}
	return next
}
func (d *document) preserveUnchanged(next *document) {
	for id, row := range next.items {
		if previous, ok := d.items[id]; ok {
			next.items[id] = preserveValue(previous, row)
		}
	}
	for key, value := range next.meta {
		if previous, ok := d.meta[key]; ok {
			next.meta[key] = preserveValue(previous, value)
		}
	}
}
func (d *document) bytes() ([]byte, error) {
	root := make(map[string]json.RawMessage, len(d.meta)+1)
	for k, v := range d.meta {
		root[k] = v
	}
	rows := make([]json.RawMessage, 0, len(d.items))
	for _, id := range d.order {
		if row, ok := d.items[id]; ok {
			rows = append(rows, row)
		}
	}
	root["items"] = wire(rows)
	b, err := encodeJSON(root)
	if err != nil {
		return nil, errors.New("todo_remote_invalid_document")
	}
	if len(b) > MaxDocumentBytes {
		return nil, errors.New("todo_remote_document_too_large")
	}
	return b, nil
}
func (d *document) apply(change Change) error {
	if len(change.Meta) > 0 {
		var meta map[string]json.RawMessage
		if json.Unmarshal(change.Meta, &meta) != nil || meta == nil || meta["items"] != nil {
			return errors.New("todo_remote_invalid_meta")
		}
		d.meta = meta
	}
	for _, id := range change.DeleteIDs {
		delete(d.items, id)
	}
	// Remove deleted IDs from order before allowing a subsequent reinsert.
	kept := d.order[:0]
	for _, id := range d.order {
		if _, ok := d.items[id]; ok {
			kept = append(kept, id)
		}
	}
	d.order = kept
	for _, row := range change.Upserts {
		id, err := itemID(row)
		if err != nil {
			return err
		}
		if _, ok := d.items[id]; !ok {
			d.order = append(d.order, id)
		}
		d.items[id] = row
	}
	return nil
}
func (d *document) diff(next *document) (Commit, bool) {
	c := Commit{Upserts: []json.RawMessage{}, DeleteIDs: []int64{}}
	for _, id := range next.order {
		if !rawEqual(d.items[id], next.items[id]) {
			c.Upserts = append(c.Upserts, next.items[id])
		}
	}
	for id := range d.items {
		if _, ok := next.items[id]; !ok {
			c.DeleteIDs = append(c.DeleteIDs, id)
		}
	}
	sort.Slice(c.DeleteIDs, func(i, j int) bool { return c.DeleteIDs[i] < c.DeleteIDs[j] })
	if !rawEqual(wire(d.meta), wire(next.meta)) {
		c.Meta = wire(next.meta)
	}
	return c, len(c.Upserts) > 0 || len(c.DeleteIDs) > 0 || len(c.Meta) > 0
}
