package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

type receivedLiteralSupport struct {
	Row     int
	Support ReceivedSupport
}

func validateRichSupportReuse(s ReceivedSupport, actor string, row int, prior map[string]receivedLiteralSupport) bool {
	var r struct {
		Path   string `json:"path"`
		Reader string `json:"reader_job"`
		Row    int    `json:"reader_row"`
		SHA    string `json:"source_sha256"`
		Blob   string `json:"git_blob"`
		Bytes  int    `json:"bytes"`
		Method string `json:"method"`
	}
	if json.Unmarshal(s.Representative, &r) != nil || r.Reader != actor || r.Row <= 0 || r.Row >= row || r.Method != "earlier_same_reader_raw_full" {
		return false
	}
	p, ok := prior[actor+"|"+r.Path]
	if !ok || p.Row != r.Row || p.Support.SHA != r.SHA || p.Support.Blob != r.Blob || p.Support.Bytes == nil || *p.Support.Bytes != r.Bytes || !evidenceTrue(p.Support.Raw) || !evidenceTrue(p.Support.Workflow) {
		return false
	}
	return s.SHA == r.SHA && s.Blob == r.Blob && s.Bytes != nil && *s.Bytes == r.Bytes
}

// countRichReceivedPrimary describes evidence, including evidence that a body
// has not been read. A reporter can have read its notice or other resources
// without becoming the reader of that body.
func countRichReceivedPrimary(e ReceivedEntry, actor string, prior map[int]ReceivedEntry, counts map[string]int) error {
	bad := func() error {
		return errors.New("original primary state, actual reader or earlier literal source differs")
	}
	if len(e.OriginalRead) != 6 || !strings.HasPrefix(e.OriginalActor, actor+"/") || !evidenceFalse(e.Copy) || e.PrimaryPartial == nil || e.PrimaryUnread == nil || e.Workflow == nil || e.Raw == nil {
		return bad()
	}
	var original struct {
		Read   []json.RawMessage `json:"read"`
		Source []json.RawMessage `json:"source"`
		Reuse  *struct {
			NewLiteral     *bool `json:"new_literal_primary_read"`
			Inherited      *bool `json:"mask_count_is_inherited"`
			Representative struct {
				Row    int               `json:"row"`
				Reader string            `json:"reader_job"`
				Source []json.RawMessage `json:"source"`
				Read   []json.RawMessage `json:"read"`
			} `json:"representative"`
		} `json:"primary_reuse"`
	}
	if json.Unmarshal(e.Original, &original) != nil || len(original.Read) != 6 || len(original.Source) != 5 {
		return bad()
	}
	var sourceID, sourcePath, sourceSHA, sourceBlob string
	var sourceBytes int
	for _, item := range []struct {
		raw   json.RawMessage
		value any
	}{{original.Source[0], &sourceID}, {original.Source[1], &sourcePath}, {original.Source[2], &sourceSHA}, {original.Source[3], &sourceBlob}, {original.Source[4], &sourceBytes}} {
		if json.Unmarshal(item.raw, item.value) != nil {
			return bad()
		}
	}
	if sourceID != e.ID || sourcePath != e.Path || sourceSHA != e.SHA || sourceBlob != e.Blob || sourceBytes != e.Bytes {
		return bad()
	}
	for i := range original.Read {
		a, ae := uniqueEvidenceJSON(original.Read[i])
		b, be := uniqueEvidenceJSON(e.OriginalRead[i])
		if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
			return bad()
		}
	}
	var state, last int
	var mask, reader *int
	var raw, workflow bool
	for _, item := range []struct {
		raw   json.RawMessage
		value any
	}{{e.OriginalRead[0], &state}, {e.OriginalRead[1], &mask}, {e.OriginalRead[2], &reader}, {e.OriginalRead[3], &raw}, {e.OriginalRead[4], &workflow}, {e.OriginalRead[5], &last}} {
		if json.Unmarshal(item.raw, item.value) != nil {
			return bad()
		}
	}
	if raw != *e.Raw || workflow != *e.Workflow || !reflect.DeepEqual(mask, e.Mask) {
		return bad()
	}
	if state == 1 {
		if e.Reader != "" || reader != nil || mask != nil || raw || workflow || last != 0 || !*e.PrimaryUnread || *e.PrimaryPartial || e.Method != "saved_unread_specific_hold" || original.Reuse != nil {
			return bad()
		}
		counts["primary_saved_unread"]++
		return nil
	}
	if e.Reader != actor || reader == nil || *reader != 0 || mask == nil || *mask < 0 || last <= 0 || *e.PrimaryUnread {
		return bad()
	}
	switch state {
	case 2:
		if !raw || !workflow || *mask != 0 || *e.PrimaryPartial || e.Method != "direct_raw_full" || original.Reuse != nil {
			return bad()
		}
		counts["primary_raw"]++
		counts["primary_workflow"]++
	case 3:
		if raw || !workflow || *mask <= 0 || *e.PrimaryPartial || e.Method != "direct_masked_full" || original.Reuse != nil {
			return bad()
		}
		counts["primary_masked"]++
		counts["primary_direct_masked"]++
		counts["primary_workflow"]++
	case 4:
		r := original.Reuse
		if raw || !workflow || *e.PrimaryPartial || e.Method != "exact_prior_literal_masked_or_raw_bytes_reuse" || r == nil || !evidenceFalse(r.NewLiteral) || !evidenceTrue(r.Inherited) {
			return bad()
		}
		p, ok := prior[r.Representative.Row]
		// This registered format contains an exact earlier masked literal primary,
		// not a new raw reading or an unbounded chain of inferred predecessors.
		if !ok || r.Representative.Row >= e.Row || r.Representative.Reader != actor || p.Reader != actor || p.SHA != e.SHA || p.Blob != e.Blob || p.Bytes != e.Bytes || !evidenceTrue(p.Workflow) || !evidenceFalse(p.Raw) || p.Method != "direct_masked_full" || *mask <= 0 || !reflect.DeepEqual(p.Mask, e.Mask) || len(p.OriginalRead) != 6 || len(r.Representative.Source) != 5 || len(r.Representative.Read) != 6 {
			return bad()
		}
		var repID, repPath, repSHA, repBlob string
		var repBytes int
		for _, item := range []struct {
			raw   json.RawMessage
			value any
		}{{r.Representative.Source[0], &repID}, {r.Representative.Source[1], &repPath}, {r.Representative.Source[2], &repSHA}, {r.Representative.Source[3], &repBlob}, {r.Representative.Source[4], &repBytes}} {
			if json.Unmarshal(item.raw, item.value) != nil {
				return bad()
			}
		}
		if repID != p.ID || repPath != p.Path || repSHA != p.SHA || repBlob != p.Blob || repBytes != p.Bytes {
			return bad()
		}
		for i := range p.OriginalRead {
			a, ae := uniqueEvidenceJSON(p.OriginalRead[i])
			b, be := uniqueEvidenceJSON(r.Representative.Read[i])
			if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
				return bad()
			}
		}
		counts["primary_masked"]++
		counts["primary_exact_reuse"]++
		counts["primary_workflow"]++
	case 6:
		if raw || workflow || !*e.PrimaryPartial || e.Method != "direct_partial" || original.Reuse != nil {
			return bad()
		}
		counts["primary_partial"]++
	default:
		return bad()
	}
	return nil
}
