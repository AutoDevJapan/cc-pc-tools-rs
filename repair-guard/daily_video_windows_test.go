package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type dailyVideoFixture struct {
	io                                                             dailyVideoIO
	db, row, job, actor                                            object
	directory, file, spec, jobPath, actorRoot, actorSpec, actorSHA string
}

func newDailyVideoFixture(t *testing.T, channel string) *dailyVideoFixture {
	t.Helper()
	f := &dailyVideoFixture{}
	root := t.TempDir()
	f.io = dailyVideoIO{root: filepath.Join(root, "videos"), specs: filepath.Join(root, "specs"), jobs: filepath.Join(root, "jobs"), toolRoot: filepath.Join(root, "tool"), at: time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC), read: readBounded, protected: func(string) bool { return false }}
	f.directory = filepath.Join(f.io.root, channel, "2026-10-20")
	f.file = filepath.Join(f.directory, "publication.json")
	f.spec = filepath.Join(f.io.specs, "3000-"+strings.Repeat("a", 32)+".md")
	id := "20261019-060000-production"
	f.jobPath = filepath.Join(f.io.jobs, id, "job.json")
	f.actorRoot = filepath.Join(root, "cc-source")
	f.actorSpec = filepath.Join(f.io.specs, "2906-"+strings.Repeat("b", 32)+".md")
	for _, dir := range []string{f.directory, f.io.specs, filepath.Dir(f.jobPath), f.actorRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{f.file: "public media manifest", f.actorSpec: "Current owner request: resume the existing eight channels.", f.spec: "- チャンネル: key=" + channel + ", channel_id=" + dailyVideoChannels[channel] + "\n- 品質manifest: " + f.file} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	actorBytes, err := os.ReadFile(f.actorSpec)
	if err != nil {
		t.Fatal(err)
	}
	f.actorSHA = hashBytes(actorBytes)
	f.actor = object{"id": float64(2906), "project": "cc-pc-tools-rs", "dispatchable": true, "status": "doing", "spec": f.actorSpec}
	f.row = object{"id": float64(3000), "project": "youtube-daily", "title": "[youtube-daily " + channel + " 2026-10-20] production", "dispatchable": true, "status": "wait", "spec": f.spec, "jobs": []any{id}}
	f.db = object{"projects": object{"youtube-daily": f.io.toolRoot, "cc-pc-tools-rs": f.actorRoot}, "items": []any{f.actor, f.row}}
	f.job = object{"id": id, "cwd": f.directory}
	f.saveJob(t)
	f.io.readObject = func(path string, limit int64) (object, error) {
		if path == productStore {
			return f.db, nil
		}
		return readObject(path, limit)
	}
	f.io.actor = func(db object) bool {
		return dailyVideoActorAllowed(db, f.actorRoot, f.actorSpec, f.actorSHA, func(string) bool { return true }, f.io.protected)
	}
	return f
}
func (f *dailyVideoFixture) saveJob(t *testing.T) {
	t.Helper()
	bytes, err := json.Marshal(f.job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.jobPath, bytes, 0o600); err != nil {
		t.Fatal(err)
	}
}
func TestDailyVideoOwnProductionAndFreshAtomicLeaf(t *testing.T) {
	for channel := range dailyVideoChannels {
		t.Run(channel, func(t *testing.T) {
			f := newDailyVideoFixture(t, channel)
			for _, path := range []string{f.directory, f.file, filepath.Join(f.directory, "publication.new.json")} {
				if !dailyVideoPathAllowedWith(path, f.io) {
					t.Fatalf("verified production rejected: %s", filepath.Base(path))
				}
			}
			f.job["project"], f.job["todo_id"], f.job["registered_cwd"] = "youtube-daily", float64(3000), f.io.toolRoot
			f.saveJob(t)
			if !dailyVideoPathAllowedWith(f.file, f.io) {
				t.Fatal("explicit job identity rejected")
			}
		})
	}
}
func TestDailyVideoRejectsChangedIdentityAndStops(t *testing.T) {
	cases := map[string]func(*dailyVideoFixture){
		"actor spec changed":            func(f *dailyVideoFixture) { f.actorSHA = strings.Repeat("0", 64) },
		"actor stopped":                 func(f *dailyVideoFixture) { f.actor["stopped"] = true },
		"actor blocked":                 func(f *dailyVideoFixture) { f.actor["status"] = "block" },
		"actor dispatch disabled":       func(f *dailyVideoFixture) { f.actor["dispatchable"] = false },
		"actor project":                 func(f *dailyVideoFixture) { f.actor["project"] = "unrelated" },
		"actor registered root":         func(f *dailyVideoFixture) { obj(f.db["projects"])["cc-pc-tools-rs"] = f.io.toolRoot },
		"production project":            func(f *dailyVideoFixture) { f.row["project"] = "unrelated" },
		"production stopped":            func(f *dailyVideoFixture) { f.row["owner_stopped"] = true },
		"production recovery unknown":   func(f *dailyVideoFixture) { f.row["status"] = "recovery_unverified" },
		"production registered root":    func(f *dailyVideoFixture) { obj(f.db["projects"])["youtube-daily"] = f.actorRoot },
		"title date mismatch":           func(f *dailyVideoFixture) { f.row["title"] = "[youtube-daily beats 2026-10-21] production" },
		"spec different ID":             func(f *dailyVideoFixture) { f.row["id"] = float64(3001) },
		"job different project":         func(f *dailyVideoFixture) { f.job["project"] = "unrelated" },
		"job different ID":              func(f *dailyVideoFixture) { f.job["todo_id"] = float64(3001) },
		"job different cwd":             func(f *dailyVideoFixture) { f.job["cwd"] = f.actorRoot },
		"job different registered root": func(f *dailyVideoFixture) { f.job["registered_cwd"] = f.actorRoot },
		"job reference missing":         func(f *dailyVideoFixture) { f.row["jobs"] = []any{"launch-unknown"} },
		"actual deny":                   func(f *dailyVideoFixture) { f.io.protected = func(string) bool { return true } },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newDailyVideoFixture(t, "beats")
			mutate(f)
			f.saveJob(t)
			if dailyVideoPathAllowedWith(f.file, f.io) {
				t.Fatal("changed or stopped production admitted")
			}
		})
	}
}
func TestDailyVideoRejectsUnrelatedAndUnprovenPaths(t *testing.T) {
	f := newDailyVideoFixture(t, "beats")
	for _, path := range []string{f.io.root, filepath.Dir(f.directory), f.actorRoot, filepath.Join(f.io.root, "unknown", "2026-10-20"), filepath.Join(f.io.root, "beats", "2026-10-18"), filepath.Join(f.io.root, "beats", "2026-10-23"), filepath.Join(f.directory, "missing-parent", "new.json")} {
		if dailyVideoPathAllowedWith(path, f.io) {
			t.Fatalf("unproven path admitted: %s", filepath.Base(path))
		}
	}
	if err := os.WriteFile(f.spec, []byte("key=beats, channel_id=wrong\n"+f.file), 0o600); err != nil {
		t.Fatal(err)
	}
	if dailyVideoPathAllowedWith(f.file, f.io) {
		t.Fatal("different channel identity admitted")
	}
	if dailyVideoActorAllowed(f.db, f.actorRoot, f.actorSpec, f.actorSHA, func(string) bool { return false }, f.io.protected) {
		t.Fatal("unverified repair source admitted")
	}
}

func TestDailyVideoRejectsAlternateStreamsAndMalformedOptionalIdentity(t *testing.T) {
	f := newDailyVideoFixture(t, "beats")
	if dailyVideoPathAllowedWith(f.file+":stream", f.io) {
		t.Fatal("alternate data stream admitted as a fresh production file")
	}
	for _, field := range []string{"project", "registered_cwd"} {
		f := newDailyVideoFixture(t, "beats")
		f.job[field] = true
		f.saveJob(t)
		if dailyVideoPathAllowedWith(f.file, f.io) {
			t.Fatalf("malformed optional identity accepted: %s", field)
		}
	}
}
