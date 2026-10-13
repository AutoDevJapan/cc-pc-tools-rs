package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var jst = time.FixedZone("Asia/Tokyo", 9*3600)

func clock(at time.Time, now time.Time, stale time.Duration) Clock {
	if at.IsZero() {
		return Clock{State: "unknown"}
	}
	text := at.UTC().Format(time.RFC3339Nano)
	c := Clock{At: &text, State: "fresh"}
	if at.After(now) {
		c.State = "future"
		return c
	}
	age := now.Sub(at).Seconds()
	c.AgeSeconds = &age
	if now.Sub(at) > stale {
		c.State = "stale"
	}
	return c
}

func timeClock(s string, now time.Time, stale time.Duration) Clock {
	if s == "" {
		return Clock{State: "unknown"}
	}
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		at, err = time.ParseInLocation("2006-01-02 15:04", s, jst)
	}
	if err != nil {
		return Clock{State: "invalid"}
	}
	return clock(at, now, stale)
}

func epochClock(raw json.RawMessage, now time.Time, stale time.Duration) Clock {
	if len(raw) == 0 || string(raw) == "null" {
		return Clock{State: "unknown"}
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return Clock{State: "invalid"}
	}
	f, err := n.Float64()
	if err != nil || f < 0 || f > 253402300799 {
		return Clock{State: "invalid"}
	}
	seconds := int64(f)
	return clock(time.Unix(seconds, int64((f-float64(seconds))*1e9)), now, stale)
}

func valueClock(raw json.RawMessage, now time.Time, stale time.Duration) Clock {
	var text string
	if json.Unmarshal(raw, &text) == nil && string(raw) != "null" {
		return timeClock(text, now, stale)
	}
	return epochClock(raw, now, stale)
}

func rawString(raw json.RawMessage) string {
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}

func newSource(now time.Time) Source {
	return Source{Status: "ok", AcquiredAt: now.UTC().Format(time.RFC3339Nano), Modified: Clock{State: "unknown"}}
}

// Read the same bounded bytes that are validated and decoded. Do not invoke any
// source CLI: status/watch commands can repair job state or send notifications.
func readProjection(path string, max int64, dst any, now time.Time, stale time.Duration) Source {
	s := newSource(now)
	info, err := os.Lstat(path)
	if err != nil {
		s.Status = ioCode(err)
		return s
	}
	if !info.Mode().IsRegular() {
		s.Status = "not_regular"
		return s
	}
	f, err := os.Open(path)
	if err != nil {
		s.Status = ioCode(err)
		return s
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !os.SameFile(info, before) {
		s.Status = "changed_during_read"
		return s
	}
	s.Modified = clock(before.ModTime(), now, stale)
	if before.Size() > max {
		s.Status = "too_large"
		return s
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	s.BytesRead = int64(len(data))
	if err != nil {
		s.Status = ioCode(err)
		return s
	}
	if int64(len(data)) > max {
		s.Status = "too_large"
		return s
	}
	after, err := f.Stat()
	if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		s.Status = "changed_during_read"
		return s
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' || json.Unmarshal(data, dst) != nil {
		s.Status = "invalid_json"
		return s
	}
	s.Complete = true
	return s
}

func ioCode(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if errors.Is(err, os.ErrPermission) {
		return "access_denied"
	}
	return "read_failed"
}

func validJobID(id string) bool {
	if id == "" || len(id) > 240 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func jobKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "job-" + hex.EncodeToString(sum[:])
}

type loadedJob struct {
	meta   jobMeta
	source Source
}

func loadJobs(ctx context.Context, opts Options, now time.Time) ([]loadedJob, Source) {
	total := newSource(now)
	dir, err := os.Open(opts.JobsDir)
	if err != nil {
		total.Status = ioCode(err)
		return nil, total
	}
	defer dir.Close()
	var jobs []loadedJob
	entries, directories := 0, 0
	for {
		if ctx.Err() != nil {
			total.Status = "deadline"
			break
		}
		batch, readErr := dir.ReadDir(128)
		for _, entry := range batch {
			entries++
			if entries > opts.MaxJobs*4 {
				total.Status = "entry_limit"
				return jobs, total
			}
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				continue
			}
			directories++
			if directories > opts.MaxJobs {
				total.Status = "directory_limit"
				return jobs, total
			}
			if ctx.Err() != nil {
				total.Status = "deadline"
				return jobs, total
			}
			id := entry.Name()
			// Never follow names with separators or alternate data streams.
			if !validJobID(id) {
				total.FailedReads++
				continue
			}
			max := opts.JobBytes
			remaining := opts.JobsBytes - total.BytesRead
			if remaining <= 0 {
				total.Status = "byte_limit"
				return jobs, total
			}
			if max > remaining {
				max = remaining
			}
			var j jobMeta
			s := readProjection(filepath.Join(opts.JobsDir, id, "job.json"), max, &j, now, opts.StaleAfter)
			if s.Status == "ok" && (j.ID != id || j.Status == "") {
				s.Status, s.Complete = "invalid_record", false
			}
			if s.Status != "ok" {
				// Retain an unknown row; never convert an unreadable record to a
				// successful absence or leak a partial decode into the snapshot.
				j = jobMeta{ID: id}
				total.FailedReads++
			} else {
				total.RecordsRead++
				s.RecordsRead = 1
			}
			total.BytesRead += s.BytesRead
			jobs = append(jobs, loadedJob{j, s})
		}
		if readErr == io.EOF {
			total.Complete = total.FailedReads == 0
			if total.FailedReads > 0 {
				total.Status = "partial"
			}
			break
		}
		if readErr != nil {
			total.Status = ioCode(readErr)
			break
		}
	}
	return jobs, total
}

func knownStatus(raw string, task bool) string {
	allowed := "|queued|starting|running|succeeded|failed|cancelled|lost|"
	if task {
		allowed = "|todo|doing|done|block|wait|retired|archived|cancelled|"
	}
	if raw != "" && !strings.ContainsAny(raw, "|\r\n\x1b") && strings.Contains(allowed, "|"+raw+"|") {
		return raw
	}
	return "unknown"
}
