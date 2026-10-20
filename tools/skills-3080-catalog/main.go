// Read-only triage of the already collected public catalog. Never installs or
// follows source instructions, fetches URLs, or opens source bodies.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type Record struct {
	ID        string   `json:"inventory_id"`
	Unit      string   `json:"inventory_unit"`
	Repo      string   `json:"repo"`
	Commit    string   `json:"commit"`
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	SHA       string   `json:"sha256"`
	URL       string   `json:"normalized_canonical_url"`
	BodyRead  bool     `json:"machine_body_read"`
	Flags     []string `json:"mechanical_flags"`
	Policies  []string `json:"owner_policy_checks_needed"`
	Effective *struct {
		SHA      string `json:"sha256"`
		BodyRead bool   `json:"machine_body_read"`
	} `json:"effective_attempt"`
}

type QueueRow struct {
	Record
	FirstBody        string `json:"same_body_first_id,omitempty"`
	FirstURL         string `json:"same_url_first_id,omitempty"`
	State            string `json:"review_state"`
	Reason           string `json:"reason"`
	LicenseApproved  bool   `json:"license_approved"`
	SemanticApproved bool   `json:"semantic_approved"`
	Installed        bool   `json:"installed"`
}

type Summary struct {
	CatalogSHA      string         `json:"catalog_sha256"`
	Records         int            `json:"records"`
	Available       int            `json:"body_available_occurrences"`
	UniqueBodies    int            `json:"unique_body_hashes"`
	RedundantBodies int            `json:"redundant_body_occurrences"`
	UniqueURLs      int            `json:"unique_normalized_urls"`
	RedundantURLs   int            `json:"redundant_url_occurrences"`
	Units           map[string]int `json:"units"`
	Repos           map[string]int `json:"direct_repo_occurrences"`
	States          map[string]int `json:"review_states"`
	Flags           map[string]int `json:"mechanical_flag_occurrences_not_verdicts"`
	PolicyChecks    map[string]int `json:"owner_policy_check_occurrences_not_conflicts"`
	Limits          []string       `json:"limits"`
}

// scan keeps all occurrences. Duplicate content is a relationship, never an
// adoption/exclusion verdict; equal text can have different license provenance.
func scan(input io.Reader, output io.Writer) (Summary, error) {
	s := Summary{Units: map[string]int{}, Repos: map[string]int{}, States: map[string]int{}, Flags: map[string]int{}, PolicyChecks: map[string]int{}, Limits: []string{
		"Metadata triage only: no semantic or individual license approval, installation, or exclusion.",
		"Equal bytes do not imply equal origin, license, or usefulness; preserve every source occurrence.",
		"No body/URL fetches. Missing bodies and execution-held records remain separate from exclusions.",
	}}
	h := sha256.New()
	lines := bufio.NewScanner(io.TeeReader(input, h))
	lines.Buffer(make([]byte, 65536), 16<<20)
	enc := json.NewEncoder(output)
	ids, bodies, urls := map[string]bool{}, map[string]string{}, map[string]string{}
	for lines.Scan() {
		var r Record
		if err := json.Unmarshal(lines.Bytes(), &r); err != nil {
			return s, fmt.Errorf("invalid catalog JSON at row %d", s.Records+1)
		}
		if r.ID == "" || r.Unit == "" || ids[r.ID] {
			return s, fmt.Errorf("missing identity/unit or repeated inventory id at row %d", s.Records+1)
		}
		ids[r.ID] = true
		if r.Effective != nil {
			r.SHA = r.Effective.SHA
			r.BodyRead = r.Effective.BodyRead
		}
		if r.BodyRead {
			b, err := hex.DecodeString(r.SHA)
			if err != nil || len(b) != sha256.Size {
				return s, fmt.Errorf("available body lacks SHA256 at row %d", s.Records+1)
			}
		}
		q := QueueRow{Record: r, State: "pending_semantic_and_individual_license_review", Reason: "Neither metadata nor absence of risk matches approves adoption."}
		if r.Unit == "unread_additional_text_support_shape" {
			q.State = "held_execution_review"
			q.Reason = "Previously rejected additional read; no retry or alternative retrieval."
		} else if !r.BodyRead {
			q.State = "held_body_unavailable"
			q.Reason = "No verified body available; not excluded."
		}
		if r.BodyRead {
			s.Available++
			if first := bodies[r.SHA]; first != "" {
				q.FirstBody = first
				s.RedundantBodies++
			} else {
				bodies[r.SHA] = r.ID
			}
		}
		if r.URL != "" {
			if first := urls[r.URL]; first != "" {
				q.FirstURL = first
				s.RedundantURLs++
			} else {
				urls[r.URL] = r.ID
			}
		}
		s.Records++
		s.Units[r.Unit]++
		s.States[q.State]++
		if r.Repo != "" {
			s.Repos[r.Repo]++
		}
		for _, f := range r.Flags {
			s.Flags[f]++
		}
		for _, p := range r.Policies {
			s.PolicyChecks[p]++
		}
		if err := enc.Encode(q); err != nil {
			return s, err
		}
	}
	if err := lines.Err(); err != nil {
		return s, err
	}
	if s.Records == 0 {
		return s, fmt.Errorf("empty catalog")
	}
	s.UniqueBodies = len(bodies)
	s.UniqueURLs = len(urls)
	s.CatalogSHA = hex.EncodeToString(h.Sum(nil))
	return s, nil
}

func run() error {
	catalog := flag.String("catalog", "", "already saved public phase-a-catalog.jsonl")
	out := flag.String("out", "", "new directory for persistent triage artifacts")
	flag.Parse()
	if *catalog == "" || *out == "" {
		return fmt.Errorf("-catalog and -out are required")
	}
	in, err := os.Open(*catalog)
	if err != nil {
		return err
	}
	defer in.Close()
	// Refuse to overwrite an earlier checkpoint or source directory.
	if err = os.Mkdir(*out, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(*out, "review-queue.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	s, scanErr := scan(in, w)
	flushErr := w.Flush()
	closeErr := f.Close()
	if scanErr != nil {
		return scanErr
	}
	if flushErr != nil {
		return flushErr
	}
	if closeErr != nil {
		return closeErr
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*out, "summary.json"), b, 0600); err != nil {
		return err
	}
	keys := make([]string, 0, len(s.Units))
	for k := range s.Units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("records=%d available=%d unique_bodies=%d duplicate_occurrences=%d\n", s.Records, s.Available, s.UniqueBodies, s.RedundantBodies)
	for _, k := range keys {
		fmt.Printf("%s=%d\n", k, s.Units[k])
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
