package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	oc "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard/internal/officialci3893"
)

func TestOfficialCI3894MachineRefusal(t *testing.T) {
	var out, errout bytes.Buffer
	code := runGuardCLI([]string{"--prepare-official-ci3387", "--guard-path", "synthetic"}, &out, &errout, nil)
	var result struct {
		Purpose bool `json:"purpose_verified"`
		Access  bool `json:"access_allowed"`
	}
	if code != 2 || json.Unmarshal(out.Bytes(), &result) != nil || result.Purpose || result.Access || errout.Len() != 0 {
		t.Fatalf("mixed call must yield one machine refusal without accessing input: code=%d out=%s stderr=%s", code, out.String(), errout.String())
	}
}

func TestOfficialCI3894CLI(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, args := range [][]string{
		{"--prepare-official-ci3387", "--root", "synthetic"},
		{"--prepare-official-ci3387", "--prepare-official-ci3387"},
		{"--prepare-official-ci3387=true"}, {"-prepare-official-ci3387"},
		{"--verify-official-ci3387"}, {"--verify-official-ci3387", strings.ToUpper(id)},
		{"--verify-official-ci3387", id, "--guard-path", "synthetic"},
		{"--consume-official-ci3387", id, "--url", "https://example.invalid"},
		{"--consume-official-ci3387", id, "--unknown"},
		{"--guard-path", "synthetic", "--prepare-official-ci3387"},
		{"--verify-omb-producer-3255", "--prepare-official-ci3387"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			called := false
			code := runOfficial3894CLI(args, &out, func(context.Context, string, string) (object, error) { called = true; return nil, nil })
			var result object
			if code != 2 || called || json.Unmarshal(out.Bytes(), &result) != nil || result["purpose_verified"] != false || result["access_allowed"] != false || result["original_registry_recovered"] != false || result["launch_performed"] != false {
				t.Fatalf("bad refusal: %d %v %s", code, called, out.String())
			}
		})
	}
	for _, args := range [][]string{{"--prepare-official-ci3387"}, {"--verify-official-ci3387", id}, {"--consume-official-ci3387", id}} {
		var out bytes.Buffer
		calls := 0
		code := runOfficial3894CLI(args, &out, func(ctx context.Context, op, receipt string) (object, error) {
			calls++
			if _, ok := ctx.Deadline(); !ok {
				t.Error("missing finite deadline")
			}
			if op != "prepare" && receipt != id {
				t.Error("receipt changed")
			}
			r := official3894Failure("")
			r["purpose_verified"] = true
			return r, nil
		})
		if code != 0 || calls != 1 {
			t.Fatalf("valid call not dispatched once: %v", args)
		}
	}
}

func TestOfficialCI3894MetadataPins(t *testing.T) {
	for _, p := range official3894Pins() {
		t.Run(p.endpoint, func(t *testing.T) {
			b, _ := json.Marshal(p.expected)
			if !official3894Match(b, p.expected) {
				t.Fatal("independent metadata rejected")
			}
			for key := range p.expected {
				var changed object
				_ = json.Unmarshal(b, &changed)
				changed[key] = "synthetic-other-purpose"
				bad, _ := json.Marshal(changed)
				if official3894Match(bad, p.expected) {
					t.Fatalf("changed %s accepted", key)
				}
				delete(changed, key)
				bad, _ = json.Marshal(changed)
				if official3894Match(bad, p.expected) {
					t.Fatalf("missing %s accepted", key)
				}
			}
			if official3894Match(append(b[:len(b)-1], []byte(`,"id":1,"id":2}`)...), p.expected) {
				t.Fatal("duplicate JSON admitted")
			}
		})
	}
}

func TestOfficialCI3894EnvironmentProjection(t *testing.T) {
	seen := map[string]bool{}
	env := official3894EnvWith(func(key string) (string, bool) {
		seen[key] = true
		if strings.Contains(strings.ToUpper(key), "TOKEN") || strings.Contains(strings.ToUpper(key), "KEY") || key == "TEMP" || key == "TMP" {
			t.Fatalf("read caller credential or target override %s", key)
		}
		return "synthetic", true
	})
	if len(seen) == 0 || !strings.Contains(strings.Join(env, "\n"), "TEMP="+official3894Temp) || !strings.Contains(strings.Join(env, "\n"), "TMP="+official3894Temp) {
		t.Fatal("fixed target environment missing")
	}
}

func TestOfficialCI3894FreshIssuerReceipt(t *testing.T) {
	now := time.Now().Add(-time.Second).UTC()
	source := official3894Source{CreatorJob: "synthetic-3894"}
	id := strings.Repeat("a", 32)
	base := official3894Owned{Version: 3, ID: id, Integrity: strings.Repeat("b", 64), State: "owned", Project: "todo-3894", Content: "official-ci3387-sdk", ProducerKind: "scheduler", ProducerTodo: 3894, ProducerJob: source.CreatorJob, Root: filepath.Clean(official3894Temp), Path: filepath.Join(filepath.Clean(official3894Temp), "codex-owned-"+id), Volume: 1, FileID: 2, Birth: 3, Created: time.Now().UTC().Format(time.RFC3339Nano)}
	base.Ancestors = append(base.Ancestors, struct {
		Path   string `json:"path"`
		Volume uint32 `json:"volume"`
		FileID uint64 `json:"file_id"`
		Birth  uint64 `json:"birth"`
	}{Path: base.Root, Volume: 1, FileID: 1, Birth: 1})
	raw, _ := json.Marshal(base)
	if _, e := official3894OwnedCheck(raw, source, now); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*official3894Owned){
		"old":         func(r *official3894Owned) { r.Created = now.Add(-time.Second).Format(time.RFC3339Nano) },
		"future":      func(r *official3894Owned) { r.Created = now.Add(time.Hour).Format(time.RFC3339Nano) },
		"other-actor": func(r *official3894Owned) { r.ProducerJob = "another" },
		"other-todo":  func(r *official3894Owned) { r.ProducerTodo = 3888 },
		"other-root": func(r *official3894Owned) {
			r.Root = filepath.Dir(r.Root)
			r.Path = filepath.Join(r.Root, "codex-owned-"+r.ID)
		},
		"other-id":       func(r *official3894Owned) { r.ID = strings.Repeat("c", 32) },
		"not-owned":      func(r *official3894Owned) { r.State = "source_absent" },
		"no-physical-id": func(r *official3894Owned) { r.FileID = 0 },
		"no-ancestors":   func(r *official3894Owned) { r.Ancestors = nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			mutate(&r)
			b, _ := json.Marshal(r)
			if _, e := official3894OwnedCheck(b, source, now); e == nil {
				t.Fatal("unissued/unbound root adopted")
			}
		})
	}
}

func official3894JournalFixture(t *testing.T) (*official3894Journal, oc.Receipt) {
	t.Helper()
	// This is an already newly created synthetic test directory, not an SDK
	// adoption. Windows CI may spell RUNNER_TEMP through a junction/8.3 alias.
	root, e := ownerPhysicalPath(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "creation")
	s, e := official3894OpenStore(path, true)
	if e != nil {
		t.Fatalf("open new synthetic journal: %v", e)
	}
	ticket := official3894Ticket{Domain: official3894Domain, Started: time.Now().UTC(), Source: official3894Source{Head: strings.Repeat("a", 40), Registration: strings.Repeat("b", 64), CreatorJob: "synthetic-3894"}}
	if e = s.start(ticket); e != nil {
		t.Fatalf("start synthetic journal: %v", e)
	}
	ticket.Owned = []byte(`{"synthetic":"issuer-output"}`)
	if e = s.append(ticket); e != nil {
		t.Fatal(e)
	}
	r := oc.Receipt{Domain: oc.Domain, ID: strings.Repeat("c", 32), State: oc.Pending, Created: time.Now().UTC()}
	if e = s.Reserve(r); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.close)
	return s, r
}

func TestOfficialCI3894NativeJournal(t *testing.T) {
	s, r := official3894JournalFixture(t)
	next := r
	next.State = oc.Complete
	if e := s.Replace(r, next); e != nil {
		t.Fatal(e)
	}
	path := s.path
	s.close()
	s.file = nil
	resumed, e := official3894OpenStore(path, false)
	if e != nil {
		t.Fatal(e)
	}
	defer resumed.close()
	loaded, e := resumed.Load(r.ID)
	if e != nil || loaded.State != oc.Complete {
		t.Fatal("restart did not retain exact complete state", e)
	}
	if _, e = official3894OpenStore(path, true); e == nil {
		t.Fatal("existing creation reused")
	}
	if _, e = resumed.Load(strings.Repeat("d", 32)); e == nil {
		t.Fatal("sibling receipt admitted")
	}
	stale := loaded
	stale.Created = stale.Created.Add(time.Second)
	if e = resumed.Replace(stale, next); e == nil {
		t.Fatal("partial old-record match accepted")
	}
	if _, e = official3894OpenStore(path, false); e == nil {
		t.Fatal("concurrent writer admitted")
	}
	consuming := loaded
	consuming.State = oc.Consuming
	if e = resumed.Replace(loaded, consuming); e != nil {
		t.Fatal(e)
	}
	consumed := consuming
	consumed.State = oc.Consumed
	if e = resumed.Replace(consuming, consumed); e != nil {
		t.Fatal(e)
	}
	if e = resumed.Replace(loaded, consuming); e == nil {
		t.Fatal("consumed replay accepted")
	}
}

func TestOfficialCI3894JournalTamperCopyAndPartial(t *testing.T) {
	for _, kind := range []string{"sealed-tamper", "partial-tail", "new-object-copy", "old-prefix-replay"} {
		t.Run(kind, func(t *testing.T) {
			s, r := official3894JournalFixture(t)
			complete := r
			complete.State = oc.Complete
			if e := s.Replace(r, complete); e != nil {
				t.Fatal(e)
			}
			if _, e := s.file.Seek(0, io.SeekStart); e != nil {
				t.Fatal(e)
			}
			original, e := io.ReadAll(s.file)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "old-prefix-replay" {
				consuming := complete
				consuming.State = oc.Consuming
				if e = s.Replace(complete, consuming); e != nil {
					t.Fatal(e)
				}
			}
			path := s.path
			s.close()
			s.file = nil
			switch kind {
			case "sealed-tamper":
				original[len(original)/2] ^= 1
				e = os.WriteFile(path, original, 0600)
			case "partial-tail":
				e = os.WriteFile(path, append(original, 1, 2), 0600)
			case "new-object-copy":
				e = os.Rename(path, path+"-retained")
				if e == nil {
					e = os.WriteFile(path, original, 0600)
				}
			case "old-prefix-replay":
				e = os.WriteFile(path, original, 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			opened, e := official3894OpenStore(path, false)
			if e == nil {
				defer opened.close()
				if _, e = opened.Load(r.ID); e == nil {
					t.Fatal("changed receipt state accepted")
				}
			}
		})
	}
}

func TestOfficialCI3894RepeatedClosePreservesReusedWindowsHandles(t *testing.T) {
	s, _ := official3894JournalFixture(t)
	s.close()
	create := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateEventW")
	set := syscall.NewLazyDLL("kernel32.dll").NewProc("SetEvent")
	var events []syscall.Handle
	defer func() {
		for _, h := range events {
			_ = syscall.CloseHandle(h)
		}
	}()
	for i := 0; i < 32; i++ {
		h, _, e := create.Call(0, 1, 0, 0)
		if h == 0 {
			t.Fatal(e)
		}
		events = append(events, syscall.Handle(h))
	}
	// These events can reuse the native directory handle numbers released by
	// the first close. A second close must not close any of the new objects.
	s.close()
	for _, h := range events {
		if ok, _, e := set.Call(uintptr(h)); ok == 0 {
			t.Fatal("second close destroyed a different object's handle", e)
		}
	}
}
