package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// The full queue remains with its canonical worker. CI verifies public source
// and summary data; it must not claim an absent full queue was byte-checked.
// Publication acceptance on that worker supplies this explicit path and fails
// if the file is missing, stale, truncated or differs from the summary.
func TestPublishedActualQueueBytesMatchRecordedSummary(t *testing.T) {
	path := os.Getenv("SKILLS3080_QUEUE_SNAPSHOT")
	if path == "" {
		t.Skip("Full queue-byte acceptance is performed by the canonical worker with SKILLS3080_QUEUE_SNAPSHOT; CI does not claim this check.")
	}
	var summary ExecutionSummary
	loadPublicReceipt(t, "skills-3080-execution-queue-summary.json", &summary)
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 128<<20 {
		t.Fatal("explicit bounded regular queue required")
	}
	if e = queueBytesMatch(f, summary.QueueSHA, summary.Records); e != nil {
		t.Fatal(e)
	}
	t.Logf("Actual canonical queue: %d rows, %d bytes, SHA256 %s", summary.Records, st.Size(), summary.QueueSHA)
}

func queueBytesMatch(r io.Reader, wantSHA string, wantRows int) error {
	h := sha256.New()
	scan := bufio.NewScanner(io.TeeReader(r, h))
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	rows := 0
	for scan.Scan() {
		rows++
	}
	if e := scan.Err(); e != nil {
		return e
	}
	if rows != wantRows || hex.EncodeToString(h.Sum(nil)) != wantSHA {
		return fmt.Errorf("queue bytes or row count differ from recorded summary")
	}
	return nil
}

func TestQueueBytesAcceptanceRejectsStaleOrIncompleteData(t *testing.T) {
	original := "first\nsecond\n"
	sum := sha256.Sum256([]byte(original))
	sha := hex.EncodeToString(sum[:])
	if e := queueBytesMatch(strings.NewReader(original), sha, 2); e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"same_rows_changed_bytes": "first\nSecond\n", "truncated": "first\n", "missing_last_newline": "first\nsecond", "extra_row": "first\nsecond\nthird\n"} {
		t.Run(name, func(t *testing.T) {
			if queueBytesMatch(strings.NewReader(body), sha, 2) == nil {
				t.Fatal("stale or incomplete queue accepted")
			}
		})
	}
	if queueBytesMatch(strings.NewReader(original), strings.Repeat("0", 64), 2) == nil {
		t.Fatal("well-shaped wrong summary digest accepted")
	}
}
