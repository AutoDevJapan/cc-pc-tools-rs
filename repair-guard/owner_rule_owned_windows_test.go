package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerRule4088OwnedReceiptNeedsOriginalProducerAndNativeIdentity(t *testing.T) {
	config, access := ownerRuleFixture(t)
	base := filepath.Dir(config.dev)
	id := "0123456789abcdef0123456789abcdef"
	root := filepath.Join(base, "codex-owned-"+id)
	store, jobs := filepath.Join(base, "receipts"), filepath.Join(base, "jobs")
	cwd := filepath.Join(config.dev, "ADJ", "producer")
	jobID := "20261027-000000-todo-42"
	for _, p := range []string{root, store, cwd, filepath.Join(jobs, jobID)} {
		if os.MkdirAll(p, 0700) != nil {
			t.Fatal("fixture mkdir")
		}
	}
	currentFixtureGit(t, cwd, "init", "-q")
	currentFixtureGit(t, cwd, "remote", "add", "origin", "https://github.com/AutoDevJapan/example.git")
	info, err := owned2736Identity(root)
	if err != nil {
		t.Fatal("fixture identity")
	}
	r := ownerOwnedReceipt{ownerOwnedIdentity: ownerOwnedIdentity{
		Version: 3, ID: id, Root: base, Path: root, Volume: info.VolumeSerialNumber,
		FileID: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow), Birth: uint64(info.CreationTime.HighDateTime)<<32 | uint64(info.CreationTime.LowDateTime),
		Created: "2026-10-27T00:00:00+09:00", Project: "fixture", Content: "classification", ProducerJob: jobID, ProducerTodo: 42, ProducerKind: "scheduler",
	}, State: "owned"}
	for p := base; ; p = filepath.Dir(p) {
		i, e := owned2736Identity(p)
		if e != nil {
			t.Fatal("fixture ancestor")
		}
		r.Ancestors = append(r.Ancestors, ownerOwnedAncestor{p, i.VolumeSerialNumber, uint64(i.FileIndexHigh)<<32 | uint64(i.FileIndexLow), uint64(i.CreationTime.HighDateTime)<<32 | uint64(i.CreationTime.LowDateTime)})
		if filepath.Dir(p) == p {
			break
		}
	}
	writeReceipt := func(value ownerOwnedReceipt) {
		b, _ := json.Marshal(value.ownerOwnedIdentity)
		value.Integrity = hashBytes(b)
		b, _ = json.Marshal(value)
		if os.WriteFile(filepath.Join(store, id+".json"), b, 0600) != nil {
			t.Fatal("fixture receipt")
		}
	}
	job := object{"id": jobID, "todo_id": 42, "cwd": cwd, "work_owner": object{"repository": "autodevjapan/example", "worktree": cwd}}
	writeJob := func(value object) {
		b, _ := json.Marshal(value)
		if os.WriteFile(filepath.Join(jobs, jobID, "job.json"), b, 0600) != nil {
			t.Fatal("fixture job")
		}
	}
	writeJob(job)
	writeReceipt(r)
	check := func(projects object) bool {
		return ownerRuleOwnedAt(context.Background(), filepath.Join(root, "new-child"), store, jobs, projects, access, config)
	}
	if !check(object{}) {
		t.Fatal("original self producer did not qualify its owned resource")
	}
	if check(object{"contracted-fixture": cwd}) {
		t.Fatal("client producer admitted")
	}
	bad := r
	bad.FileID++
	writeReceipt(bad)
	if check(object{}) {
		t.Fatal("replaced native identity admitted")
	}
	bad = r
	bad.ProducerTodo++
	writeReceipt(bad)
	if check(object{}) {
		t.Fatal("different producer admitted")
	}
	bad = r
	bad.Ancestors = bad.Ancestors[:len(bad.Ancestors)-1]
	writeReceipt(bad)
	if check(object{}) {
		t.Fatal("incomplete ancestor identity admitted")
	}
	writeReceipt(r)
	job["todo_id"] = 43
	writeJob(job)
	if check(object{}) {
		t.Fatal("unrelated scheduler job admitted")
	}
}
