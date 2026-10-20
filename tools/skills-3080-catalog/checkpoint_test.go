package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishedQueueHasVerifiedCompletionAndNoOverwrite(t *testing.T) {
	d := t.TempDir()
	input := []byte("{\"inventory_id\":\"one\",\"inventory_unit\":\"site_index_entry\"}\n")
	p := filepath.Join(d, "catalog")
	os.WriteFile(p, input, 0600)
	out := filepath.Join(d, "out")
	if e := writeCatalog(p, out); e != nil {
		t.Fatal(e)
	}
	var s Summary
	b, e := os.ReadFile(filepath.Join(out, "summary.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &s); e != nil {
		t.Fatal(e)
	}
	q, e := os.ReadFile(filepath.Join(out, "review-queue.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(q)
	in := sha256.Sum256(input)
	if s.QueueRows != 1 || s.QueueSHA != hex.EncodeToString(h[:]) || s.CatalogSHA != hex.EncodeToString(in[:]) || s.WithoutURL != 1 {
		t.Fatal("unverified completion receipt")
	}
	if e = writeCatalog(p, out); e == nil {
		t.Fatal("overwrote checkpoint")
	}
	after, _ := os.ReadFile(filepath.Join(out, "summary.json"))
	if string(b) != string(after) {
		t.Fatal("existing completion changed")
	}
}
func TestBadInputRetainsPartialQueueWithoutCompletion(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "catalog")
	os.WriteFile(p, []byte("{bad JSON"), 0600)
	out := filepath.Join(d, "out")
	if e := writeCatalog(p, out); e == nil {
		t.Fatal("bad JSON succeeded")
	}
	if _, e := os.Stat(filepath.Join(out, "summary.json")); !os.IsNotExist(e) {
		t.Fatal("failed read published completion")
	}
	if _, e := os.Stat(filepath.Join(out, "review-queue.jsonl")); !os.IsNotExist(e) {
		t.Fatal("failed read published final queue")
	}
	if _, e := os.Stat(filepath.Join(out, "review-queue.jsonl.partial")); e != nil {
		t.Fatal("lost partial evidence")
	}
}
