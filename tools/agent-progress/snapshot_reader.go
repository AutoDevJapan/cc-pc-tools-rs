package main

import (
	"os"
	"time"
)

const snapshotReadBudget = 150 * time.Millisecond

// Consumer contract: retry only transient Windows open errors. On failure the
// caller retains its previous validated generation and marks it stale/failed.
// A failed read never returns bytes that could replace that previous generation.
func readPublishedSnapshot(path string) ([]byte, error) {
	return retrySnapshotRead(func() ([]byte, error) { return os.ReadFile(path) }, time.Now, time.Sleep)
}

func retrySnapshotRead(read func() ([]byte, error), now func() time.Time, sleep func(time.Duration)) ([]byte, error) {
	deadline := now().Add(snapshotReadBudget)
	for {
		data, err := read()
		if err == nil {
			return data, nil
		}
		if !transientSnapshotRead(err) || !now().Before(deadline) {
			return nil, err
		}
		delay := 5 * time.Millisecond
		if remaining := deadline.Sub(now()); remaining < delay {
			delay = remaining
		}
		if delay <= 0 {
			return nil, err
		}
		sleep(delay)
	}
}
