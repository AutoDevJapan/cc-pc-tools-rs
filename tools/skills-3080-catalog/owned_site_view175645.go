//go:build ignore

// Read one fixed already-acquired site source for the canonical3080 parent.
// No HTTP, installation, source script or upstream-Git assertion is performed.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"cc-pc-tools-rs/skills-3080-catalog/internal/primaryflow"
)

func main() {
	if e := siteView(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func siteView() error {
	if os.Getenv("CC_JOB_ID") != "20261027-175645-todo-3080" || len(os.Args) != 4 {
		return fmt.Errorf("actual parent: site ID, first line and through line required")
	}
	id := os.Args[1]
	if !regexp.MustCompile(`^site:[a-zA-Z0-9_-]+$`).MatchString(id) {
		return fmt.Errorf("finite site inventory ID required")
	}
	first, e := strconv.Atoi(os.Args[2])
	if e != nil {
		return e
	}
	through, e := strconv.Atoi(os.Args[3])
	if e != nil || first < 1 || through < first || through-first+1 > 180 {
		return fmt.Errorf("bounded180-line view; no total source cap")
	}
	root := "D:/CC/Work/unattended-operations-20261013-wt/todo-3080-skills-inventory/.ci-work/todo3080"
	f, e := os.Open(filepath.Join(root, "phase-a-catalog.jsonl"))
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	s := bufio.NewScanner(io.TeeReader(f, h))
	s.Buffer(make([]byte, 65536), 4<<20)
	var found struct {
		ID      string `json:"inventory_id"`
		Unit    string `json:"inventory_unit"`
		URL     string `json:"source_url"`
		Slug    string `json:"slug"`
		Attempt struct {
			SHA    string `json:"sha256"`
			Bytes  int64  `json:"bytes"`
			Path   string `json:"content_path"`
			Method string `json:"canonical_raw"`
			At     string `json:"retrieved_at"`
			Read   bool   `json:"machine_body_read"`
			Git    bool   `json:"commit_verified"`
			Commit string `json:"commit"`
		} `json:"effective_attempt"`
	}
	count, matched := 0, 0
	for s.Scan() {
		var identity struct {
			ID string `json:"inventory_id"`
		}
		if e = json.Unmarshal(s.Bytes(), &identity); e != nil {
			return e
		}
		count++
		if identity.ID == id {
			if e = primaryflow.Decode(s.Bytes(), &found); e != nil {
				return e
			}
			matched++
		}
	}
	if e = s.Err(); e != nil {
		return e
	}
	if count != 73220 || hex.EncodeToString(h.Sum(nil)) != "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c" || matched != 1 || found.Unit != "site_index_entry" || !found.Attempt.Read || found.Attempt.Bytes < 1 || found.Attempt.Bytes > primaryflow.MaxBodyBytes || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(found.Attempt.SHA) || found.Attempt.Path != "site/objects/"+found.Attempt.SHA+".gz" {
		return fmt.Errorf("complete fixed catalog or exact saved source identity differs")
	}
	path := filepath.Join(root, filepath.FromSlash(found.Attempt.Path))
	if e = primaryflow.Guard(path); e != nil {
		return fmt.Errorf("normal exact source guard unavailable/held; no open or alternate retrieval")
	}
	meta, e := os.Lstat(path)
	if e != nil || !meta.Mode().IsRegular() || meta.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("regular saved original required")
	}
	src, e := os.Open(path)
	if e != nil {
		return fmt.Errorf("saved source open failed")
	}
	defer src.Close()
	opened, e := src.Stat()
	if e != nil || !os.SameFile(meta, opened) {
		return fmt.Errorf("saved original changed during open")
	}
	z, e := gzip.NewReader(src)
	if e != nil {
		return fmt.Errorf("saved gzip invalid")
	}
	defer z.Close()
	raw, e := io.ReadAll(io.LimitReader(z, found.Attempt.Bytes+1))
	if e != nil || int64(len(raw)) != found.Attempt.Bytes || primaryflow.Hash(raw) != found.Attempt.SHA {
		return fmt.Errorf("same-buffer source SHA/bytes differ")
	}
	// The site has no verified upstream Git object. The local blob computation
	// is only an adapter for the existing byte-preserving masking function and
	// is explicitly labelled as derived, never independent upstream provenance.
	bh := sha1.New()
	fmt.Fprintf(bh, "blob %d%c", len(raw), 0)
	bh.Write(raw)
	derived := hex.EncodeToString(bh.Sum(nil))
	group := primaryflow.Group{Ordinal: 1, SHA: found.Attempt.SHA, Bytes: found.Attempt.Bytes, Members: []primaryflow.Member{{Blob: derived}}}
	safe, e := primaryflow.CheckAndMask(raw, group)
	if e != nil {
		return e
	}
	text := safe.Visible
	terminal := bytes.HasSuffix([]byte(text), []byte("\n"))
	lines := bytes.Split([]byte(text), []byte("\n"))
	if terminal {
		lines = lines[:len(lines)-1]
	}
	if first > len(lines) {
		return fmt.Errorf("requested first line past actual source")
	}
	if through > len(lines) {
		through = len(lines)
	}
	view := bytes.Join(lines[first-1:through], []byte("\n"))
	if through < len(lines) || terminal {
		view = append(view, '\n')
	}
	withheld := []int{}
	for _, n := range safe.Withheld {
		if n >= first && n <= through {
			withheld = append(withheld, n)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"domain": "skills3080-fixed-saved-site-visible-view-v1", "actual_reader_job": os.Getenv("CC_JOB_ID"), "inventory_id": id, "source_url": found.URL, "fixed_saved_source_SHA256": found.Attempt.SHA, "fixed_saved_source_bytes": found.Attempt.Bytes, "original_retrieved_at": found.Attempt.At, "upstream_commit_verified": found.Attempt.Git, "upstream_commit": found.Attempt.Commit, "derived_local_blob_not_upstream_proof": derived, "total_physical_lines": safe.Lines, "first": first, "through": through, "mask_version": safe.MaskVersion, "withheld_physical_lines": withheld, "visible_lines": through - first + 1 - len(withheld), "range_sha256": primaryflow.Hash(view), "whole_masked_view_sha256": safe.MaskedSHA, "masked_range": string(view), "unseen_meanings_remain_unknown": true, "new_raw_credit": 0, "source_scripts_or_HTTP_or_permission_or_adoption_or_central_operations": 0, "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "whole16_complete": false})
}
