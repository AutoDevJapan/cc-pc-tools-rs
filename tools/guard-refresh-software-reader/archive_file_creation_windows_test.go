//go:build windows

package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestArchiveFileWriterOverlapsOnlyTwoAndCollectsBeforeReturn(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var active, maximum, created, collected atomic.Int32
	w := archiveFileWriter{create: func(path string, raw []byte) *archiveFileCompletion {
		n := active.Add(1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		created.Add(1)
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return &archiveFileCompletion{path: path}
	}, collect: func(*archiveFileCompletion) error { collected.Add(1); return nil }}
	done := make(chan error, 1)
	go func() {
		for _, name := range []string{"first", "second", "third"} {
			if err := w.submit(name, []byte("owned")); err != nil {
				done <- err
				return
			}
		}
		done <- w.finish()
	}()
	got := 0
	deadline := time.NewTimer(time.Second)
	for got < 2 {
		select {
		case <-entered:
			got++
		case <-deadline.C:
			close(release)
			<-done
			t.Fatalf("file creation remained serial: started=%d", got)
		}
	}
	deadline.Stop()
	if created.Load() != 2 || active.Load() != 2 {
		close(release)
		<-done
		t.Fatal("worker bound not two")
	}
	close(release)
	if err := <-done; err != nil || maximum.Load() != 2 || collected.Load() != 3 || active.Load() != 0 {
		t.Fatal("not fully collected", err)
	}
}

func TestArchiveLastWriterCancellationCannotPublishChecksum(t *testing.T) {
	s := newArchiveSnapshot(t)
	content := []byte("owned pending payload")
	raw := ownedCargoArchive(t, []*tar.Header{{Name: "owned-1.0.0/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{content})
	defer clear(raw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, beforeDrain := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var active atomic.Int32
	var payload []byte
	var completion *archiveFileCompletion
	released, observed := false, false
	t.Cleanup(func() {
		cancel()
		if !released {
			close(release)
		}
		if !observed {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("controller not collected during cleanup")
			}
		}
	})
	go func() {
		done <- s.extractCargoArchiveWithHooks(ctx, "owned-1.0.0", digest(raw), raw, &cargoExpansionBudget{}, archiveCreationHooks{beforeDrain: func() { close(beforeDrain) }, create: func(path string, b []byte) *archiveFileCompletion {
			active.Add(1)
			defer active.Add(-1)
			payload = b
			close(entered)
			<-release
			fragment := &softwareBuildSnapshot{Root: s.Root, paths: map[string]bool{}}
			err := fragment.create(path, b)
			completion = &archiveFileCompletion{path: path, fragment: fragment, sha: digest(b), size: int64(len(b)), err: err}
			return completion
		}})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	// EOF is observed by the actual controller before this separate signal.
	select {
	case <-beforeDrain:
	case <-time.After(time.Second):
		t.Fatal("controller did not reach EOF drain")
	}
	cancel()
	close(release)
	released = true
	var err error
	select {
	case err = <-done:
		observed = true
	case <-time.After(2 * time.Second):
		t.Fatal("controller did not collect the released writer")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late cancellation became success: %v", err)
	}
	if active.Load() != 0 || completion == nil || completion.fragment == nil || len(completion.fragment.files) != 0 {
		t.Fatal("writer or snapshot handles not collected")
	}
	for _, b := range payload {
		if b != 0 {
			t.Fatal("writer payload not erased before return")
		}
	}
	if _, err := os.Lstat(filepath.Join(s.Root, "vendor", "owned-1.0.0", ".cargo-checksum.json")); !os.IsNotExist(err) {
		t.Fatal("checksum published after cancellation")
	}
}

func TestArchiveEarlierWriterFailureIsNotReplacedByCancellation(t *testing.T) {
	s := newArchiveSnapshot(t)
	content := []byte("owned pending payload")
	raw := ownedCargoArchive(t, []*tar.Header{{Name: "owned-1.0.0/source.rs", Size: int64(len(content)), Mode: 0600, Typeflag: tar.TypeReg}}, [][]byte{content})
	defer clear(raw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	primary := errors.New("owned_first_create_failure")
	err := s.extractCargoArchiveWithCreator(ctx, "owned-1.0.0", digest(raw), raw, &cargoExpansionBudget{}, func(path string, b []byte) *archiveFileCompletion {
		cancel()
		return &archiveFileCompletion{path: path, fragment: &softwareBuildSnapshot{Root: s.Root, paths: map[string]bool{}}, err: primary}
	})
	if !errors.Is(err, primary) {
		t.Fatal("primary writer failure discarded", err)
	}
	if _, err := os.Lstat(filepath.Join(s.Root, "vendor", "owned-1.0.0", ".cargo-checksum.json")); !os.IsNotExist(err) {
		t.Fatal("metadata after earlier failure")
	}
}

func TestArchiveDirectoryCreationRechecksAfterRecursiveParents(t *testing.T) {
	s := newArchiveSnapshot(t)
	for i := 0; len(s.paths) < cargoVendorEntryLimit-1; i++ {
		s.paths[filepath.Join(s.Root, fmt.Sprintf("counted-%d", i))] = true
	}
	path := filepath.Join(s.Root, "first", "second")
	err := s.createVendorDirectories(path)
	if err == nil || len(s.paths) > cargoVendorEntryLimit {
		t.Fatal("recursive directories exceeded namespace entry limit", len(s.paths))
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("directory created past limit")
	}
}

func TestArchiveDirectoryCreationCountsPendingFileReservations(t *testing.T) {
	s := newArchiveSnapshot(t)
	for i := 0; len(s.paths) < cargoVendorEntryLimit-1; i++ {
		s.paths[filepath.Join(s.Root, fmt.Sprintf("counted-%d", i))] = true
	}
	path := filepath.Join(s.Root, "pending-reserved")
	if s.createVendorDirectoriesReserved(path, 1) == nil {
		t.Fatal("pending file was not counted")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("directory consumed reserved slot")
	}
	if err := s.createVendorDirectoriesReserved(path, 0); err != nil || len(s.paths) != cargoVendorEntryLimit {
		t.Fatal("normal last slot rejected", err)
	}
}

func TestArchiveFileWriterRetainsFirstFailureAndErasesEveryOwnedBuffer(t *testing.T) {
	var buffers [][]byte
	collected := 0
	w := archiveFileWriter{create: func(path string, raw []byte) *archiveFileCompletion {
		buffers = append(buffers, raw)
		return &archiveFileCompletion{path: path, err: errors.New("owned_failure")}
	}, collect: func(r *archiveFileCompletion) error { collected++; return r.err }}
	raw := []byte("owned payload")
	_ = w.submit("one", raw)
	if w.finish() == nil || collected != 1 {
		t.Fatal("failed creation discarded")
	}
	for _, b := range buffers {
		for _, v := range b {
			if v != 0 {
				t.Fatal("payload not erased")
			}
		}
	}
}

func TestArchiveFileWriterFailureWaitsForAlreadyStartedWriter(t *testing.T) {
	entered := make(chan string, 2)
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	var active, collected atomic.Int32
	w := archiveFileWriter{create: func(path string, raw []byte) *archiveFileCompletion {
		active.Add(1)
		entered <- path
		if path == "first" {
			<-releaseFirst
			active.Add(-1)
			return &archiveFileCompletion{path: path, err: errors.New("first_failure")}
		}
		<-releaseSecond
		active.Add(-1)
		return &archiveFileCompletion{path: path}
	}, collect: func(r *archiveFileCompletion) error { collected.Add(1); return r.err }}
	_ = w.submit("first", []byte("owned-first"))
	_ = w.submit("second", []byte("owned-second"))
	<-entered
	<-entered
	done := make(chan error, 1)
	go func() { done <- w.finish() }()
	close(releaseFirst)
	select {
	case <-done:
		close(releaseSecond)
		t.Fatal("returned before live writer collected")
	case <-time.After(10 * time.Millisecond):
	}
	close(releaseSecond)
	if err := <-done; err == nil || collected.Load() != 2 || active.Load() != 0 {
		t.Fatal("pending writer abandoned")
	}
}
