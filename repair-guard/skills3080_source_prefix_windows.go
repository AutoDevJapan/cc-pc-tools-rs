package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"strconv"
	"strings"
	"time"
)

// This value describes bytes recorded by the controller at a fresh launch.
// It is not, by itself, a source receipt or permission to read any result.
type skillsInitialSourcePrefix struct {
	Bytes int64  `json:"bytes"`
	SHA   string `json:"sha256"`
}

func skillsFreshRuntimeThread(job object, start, end time.Time) bool {
	id := str(job["thread_id"])
	at, ok := skillsRuntimeThreadBirth(id)
	return str(job["resume_session"]) == "" && ok && !end.Before(start) && !at.Before(start) && !at.After(end)
}

func skillsRuntimeThreadBirth(id string) (time.Time, bool) {
	if !skillsUUID.MatchString(id) || id[14] != '7' || !strings.ContainsRune("89ab", rune(id[19])) {
		return time.Time{}, false
	}
	birth, err := strconv.ParseInt(strings.ReplaceAll(id, "-", "")[:12], 16, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(birth), true
}

// Scanner may read ahead. Feed it one buffered byte at a time so the digest
// ends exactly after the first source turn_context, including its raw newline.
// Buffered input is never decoded or retained as an alternate result body.
type skillsInitialPrefixReader struct {
	reader *bufio.Reader
	hash   hash.Hash
	n      int64
	last   byte
}

func (r *skillsInitialPrefixReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b, err := r.reader.ReadByte()
	if err != nil {
		return 0, err
	}
	p[0] = b
	r.hash.Write(p[:1])
	r.n++
	r.last = b
	return 1, nil
}

func skillsInitialSourcePrefixProjection(input io.Reader, job object, project []byte, start, end time.Time) (skillsInitialSourcePrefix, bool) {
	var empty skillsInitialSourcePrefix
	if !skillsFreshRuntimeThread(job, start, end) || job["sandbox"] != "read-only" {
		return empty, false
	}
	r := &skillsInitialPrefixReader{reader: bufio.NewReaderSize(input, 64<<10), hash: sha256.New()}
	_, modern, valid := skillsRuntimeBootstrapProjection(r, job, project, start, end)
	if !valid || !modern || r.n < 1 || r.n > 4<<20 || r.last != '\n' {
		return empty, false
	}
	return skillsInitialSourcePrefix{Bytes: r.n, SHA: hex.EncodeToString(r.hash.Sum(nil))}, true
}
