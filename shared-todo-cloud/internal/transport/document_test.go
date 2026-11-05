package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExactDocumentCeilingFitsHTTPEnvelopeWithoutExpansion(t *testing.T) {
	const prefix = `{"next_id":2,"items":[{"id":1,"note":"`
	const suffix = `"}]}`
	b := []byte(prefix + strings.Repeat("<", MaxDocumentBytes-len(prefix)-len(suffix)) + suffix)
	if len(b) != MaxDocumentBytes {
		t.Fatal("bad boundary fixture")
	}
	d, err := parseDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := d.bytes()
	if err != nil || len(encoded) != MaxDocumentBytes {
		t.Fatal("JSON encoding expanded the boundary document")
	}
	if _, err = parseDocument(append(b, ' ')); err == nil {
		t.Fatal("excess document accepted")
	}
	// The HTTP envelope limit is distinct from the canonical document limit.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"revision":1,"document":`))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte(`}`))
	}))
	defer server.Close()
	c, err := NewClient(server.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Snapshot
	if err = c.call(context.Background(), "GET", "/api/v1/state", nil, &snapshot); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshot.Document, b) {
		t.Fatal("snapshot was truncated")
	}
}

func TestMetadataReplacementAndDeleteReinsertKeepTypesAndOrder(t *testing.T) {
	d, err := parseDocument([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err = d.apply(Change{Revision: 2, DeleteIDs: []int64{1}, Meta: json.RawMessage(`{"next_id":5,"lastJob":null,"float":1.0,"new":{"unknown":true}}`)}); err != nil {
		t.Fatal(err)
	}
	if err = d.apply(Change{Revision: 3, Upserts: []json.RawMessage{json.RawMessage(`{"id":1,"lastjob":"lower","float":1e-7}`)}}); err != nil {
		t.Fatal(err)
	}
	b, err := d.bytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.order) != 2 || d.order[0] != 2 || d.order[1] != 1 {
		t.Fatal("item order or IDs duplicated")
	}
	if bytes.Contains(b, []byte(`"projects"`)) || !bytes.Contains(b, []byte(`"lastJob":null`)) || !bytes.Contains(b, []byte(`"lastjob":"lower"`)) || !bytes.Contains(b, []byte(`"float":1.0`)) || !bytes.Contains(b, []byte(`"next_id":5`)) {
		t.Fatal("metadata replacement changed types or absence")
	}
}

func TestPreserveValuesOnlyRestoresUnchangedLeaves(t *testing.T) {
	old := json.RawMessage(`{"number":0.10000000000000001,"integer":9007199254740993,"float":1.0,"zero":-0.0,"null":null,"array":[1.2300e-7],"removed":true,"lastJob":"old"}`)
	next := json.RawMessage(`{"number":0.1,"integer":9007199254740992,"float":1,"zero":0.0,"array":[1.23e-7],"lastJob":"new","lastjob":null}`)
	restored := preserveValue(old, next)
	for _, value := range []string{`"number":0.10000000000000001`, `"integer":9007199254740992`, `"float":1`, `"zero":0.0`, `1.2300e-7`, `"lastJob":"new"`, `"lastjob":null`} {
		if !bytes.Contains(restored, []byte(value)) {
			t.Fatalf("invalid preservation: %s", value)
		}
	}
	if bytes.Contains(restored, []byte(`"removed"`)) || bytes.Contains(restored, []byte(`"null"`)) {
		t.Fatal("restored a deliberately absent field")
	}
}
