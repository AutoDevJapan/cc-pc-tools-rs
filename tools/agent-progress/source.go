package main

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
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
		at, err = time.Parse("2006-01-02T15:04:05.999999999-0700", s)
	}
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

// Canonical IDs begin with YYYYMMDD-HHMMSS. Keep only the newest MaxJobs
// names in memory, but enumerate the entire directory (bounded by the deadline)
// instead of stopping on its oldest entries. Pinned references are read first
// and are exempt from the recent-history count, not the byte/deadline bounds.
type recentJobNames []string

func (h recentJobNames) Len() int           { return len(h) }
func (h recentJobNames) Less(i, j int) bool { return h[i] < h[j] }
func (h recentJobNames) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *recentJobNames) Push(v any)        { *h = append(*h, v.(string)) }
func (h *recentJobNames) Pop() any {
	last := len(*h) - 1
	v := (*h)[last]
	*h = (*h)[:last]
	return v
}

func taskReferences(items map[int64]todoItem, unfinishedOnly bool) map[string][]int64 {
	references := map[string][]int64{}
	for id, item := range items {
		if unfinishedOnly && (item.Status == "done" || item.Status == "retired" || item.Status == "archived" || item.Status == "cancelled") {
			continue
		}
		var refs []json.RawMessage
		_ = json.Unmarshal(item.Jobs, &refs)
		for _, raw := range refs {
			var jid string
			if json.Unmarshal(raw, &jid) == nil && validJobID(jid) {
				references[jid] = append(references[jid], id)
			}
		}
		if validJobID(item.Scheduler.Job) {
			references[item.Scheduler.Job] = append(references[item.Scheduler.Job], id)
		}
	}
	return references
}

func loadJobs(ctx context.Context, opts Options, now time.Time, pinned map[string][]int64) ([]loadedJob, Source) {
	total := newSource(now)
	dir, err := os.Open(opts.JobsDir)
	if err != nil {
		total.Status = ioCode(err)
		return nil, total
	}
	defer dir.Close()
	var jobs []loadedJob
	read := map[string]bool{}
	load := func(id string) bool {
		if read[id] {
			return true
		}
		if ctx.Err() != nil {
			total.Status = "deadline"
			return false
		}
		max := opts.JobBytes
		remaining := opts.JobsBytes - total.BytesRead
		if remaining <= 0 {
			total.Status = "byte_limit"
			return false
		}
		if max > remaining {
			max = remaining
		}
		var j jobMeta
		s := newSource(now)
		// Direct references must obey the same no-directory-symlink rule as
		// discovered entries; checking only job.json would follow its parent.
		info, statErr := os.Lstat(filepath.Join(opts.JobsDir, id))
		if statErr != nil {
			s.Status = ioCode(statErr)
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			s.Status = "not_regular"
		} else {
			s = readProjection(filepath.Join(opts.JobsDir, id, "job.json"), max, &j, now, opts.StaleAfter)
		}
		if s.Status == "ok" && (j.ID != id || j.Status == "") {
			s.Status, s.Complete = "invalid_record", false
		}
		if s.Status != "ok" {
			j = jobMeta{ID: id}
			total.FailedReads++
		} else {
			total.RecordsRead++
			s.RecordsRead = 1
		}
		total.BytesRead += s.BytesRead
		jobs = append(jobs, loadedJob{j, s})
		read[id] = true
		return true
	}
	ids := make([]string, 0, len(pinned))
	for id := range pinned {
		if validJobID(id) {
			ids = append(ids, id)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	for _, id := range ids {
		if !load(id) {
			return jobs, total
		}
	}
	newest := &recentJobNames{}
	directories, pinnedDirectories := 0, 0
	scanComplete := false
	for {
		if ctx.Err() != nil {
			total.Status = "deadline"
			break
		}
		batch, readErr := dir.ReadDir(128)
		for _, entry := range batch {
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				continue
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
			directories++
			if read[id] {
				pinnedDirectories++
			}
			if newest.Len() < opts.MaxJobs {
				heap.Push(newest, id)
			} else if id > (*newest)[0] {
				(*newest)[0] = id
				heap.Fix(newest, 0)
			}
		}
		if readErr == io.EOF {
			scanComplete = true
			break
		}
		if readErr != nil {
			total.Status = ioCode(readErr)
			break
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(*newest)))
	retainedDirectories := newest.Len() + pinnedDirectories
	for _, id := range *newest {
		if read[id] {
			retainedDirectories--
		}
	}
	for _, id := range *newest {
		if !load(id) {
			return jobs, total
		}
	}
	if scanComplete {
		if directories > retainedDirectories {
			total.Status = "directory_limit"
		} else if total.FailedReads > 0 {
			total.Status = "partial"
		} else {
			total.Complete = true
		}
	}
	return jobs, total
}

func knownStatus(raw string, task bool) string {
	allowed := "|queued|starting|running|succeeded|failed|cancelled|lost|"
	if task {
		allowed = "|todo|doing|done|block|wait|recovery_unverified|retired|archived|cancelled|"
	}
	if raw != "" && !strings.ContainsAny(raw, "|\r\n\x1b") && strings.Contains(allowed, "|"+raw+"|") {
		return raw
	}
	return "unknown"
}
