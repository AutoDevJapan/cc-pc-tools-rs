package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

type official3894MetadataField struct {
	children map[string]official3894MetadataField
	presence bool
}

// Only these authority fields are decoded. Notes, titles, history, question
// bodies and unrelated operational fields are discarded as JSON, never retained
// as an object and never passed to encoding/json's case-insensitive struct path.
func official3894TaskMetadata(raw []byte) ([]byte, bool) {
	subwork := map[string]official3894MetadataField{
		"parent_todo_id": {}, "origin_job_id": {}, "kind": {},
		"owner": {children: map[string]official3894MetadataField{"worktree": {}}},
	}
	admission := map[string]official3894MetadataField{
		"key": {}, "spec": {}, "worktree": {}, "subwork": {children: subwork},
	}
	scheduler := map[string]official3894MetadataField{
		"job": {}, "last_job": {}, "worktree": {}, "branch": {}, "key": {}, "subwork_parent_job": {},
		"subwork_admission": {children: admission}, "pending_cancel": {presence: true}, "owner_stop_job": {presence: true},
	}
	fields := map[string]official3894MetadataField{
		"id": {}, "project": {}, "status": {}, "spec": {}, "jobs": {},
		"owner_stopped": {}, "owner_postponement_recorded": {}, "stop_kind": {},
		"owner_question_wait": {presence: true}, "decision_wait": {presence: true}, "wait_ready": {presence: true},
		"scheduler": {children: scheduler}, "subwork": {children: subwork},
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	value, ok := official3894MetadataObject(d, fields)
	if !ok || value == nil {
		return nil, false
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, false
	}
	if !skillsCanonicalJSON(value, &skillsTask{}) {
		return nil, false
	}
	filtered, e := json.Marshal(value)
	return filtered, e == nil
}

type official3894Presence struct{ set bool }

func (p *official3894Presence) UnmarshalJSON(raw []byte) error {
	p.set = !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	var discard ownerDiscardJSON
	return discard.UnmarshalJSON(raw)
}

func official3894MetadataObject(d *json.Decoder, fields map[string]official3894MetadataField) (object, bool) {
	first, e := d.Token()
	if e != nil {
		return nil, false
	}
	if first == nil {
		return nil, true
	}
	if first != json.Delim('{') {
		return nil, false
	}
	out, seen := object{}, map[string]bool{}
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok || seen[name] {
			return nil, false
		}
		seen[name] = true
		field, known := fields[name]
		if !known {
			// EqualFold also catches encoding/json aliases such as the Kelvin
			// sign in "key", which simple ASCII lowercasing would miss.
			for canonical := range fields {
				if strings.EqualFold(name, canonical) {
					return nil, false
				}
			}
			var discard ownerDiscardJSON
			if d.Decode(&discard) != nil {
				return nil, false
			}
			continue
		}
		switch {
		case field.presence:
			var presence official3894Presence
			if d.Decode(&presence) != nil {
				return nil, false
			}
			if presence.set {
				out[name] = true
			} else {
				out[name] = nil
			}
		case field.children != nil:
			child, ok := official3894MetadataObject(d, field.children)
			if !ok {
				return nil, false
			}
			out[name] = child
		default:
			var value any
			if d.Decode(&value) != nil {
				return nil, false
			}
			out[name] = value
		}
	}
	last, e := d.Token()
	return out, e == nil && last == json.Delim('}')
}
