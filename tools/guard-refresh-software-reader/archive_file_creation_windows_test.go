//go:build windows

package main

import (
	"errors"
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
