package main

import (
	"encoding/json"
	"errors"
	"strings"
)

const executionVisibleJournal = "skills-3080-visible-primary-assessments.json"
const executionVisibleDomain = "skills3080-visible-primary-assessment-journal-v1"

type executionVisibleMember struct {
	ID     string `json:"inventory_id"`
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Path   string `json:"path"`
	SHA    string `json:"sha256"`
	Blob   string `json:"git_blob"`
	Bytes  int64  `json:"bytes"`
}

// Keep each same-body occurrence pending while checking its own source tuple
// against the full pinned catalog. A representative never licenses its members.
func executionVisibleMembers(raw []byte, catalogSHA string) (map[string]executionVisibleMember, error) {
	fail := func() (map[string]executionVisibleMember, error) {
		return nil, errors.New("visible primary member identity or catalog boundary differs")
	}
	value, err := uniqueEvidenceJSON(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return fail()
	}
	var journal struct {
		Domain  string `json:"domain"`
		Catalog string `json:"catalog_sha256"`
		Groups  int    `json:"representative_body_groups"`
		Count   int    `json:"bound_member_metadata"`
		Entries []struct {
			ID      string                   `json:"inventory_id"`
			Repo    string                   `json:"source_repo"`
			Commit  string                   `json:"source_commit"`
			Path    string                   `json:"path"`
			SHA     string                   `json:"source_sha256"`
			Blob    string                   `json:"git_blob"`
			Bytes   int64                    `json:"bytes"`
			Members []executionVisibleMember `json:"member_metadata"`
		} `json:"entries"`
	}
	if json.Unmarshal(raw, &journal) != nil || journal.Domain != executionVisibleDomain || journal.Catalog != catalogSHA || journal.Groups < 1 || journal.Groups != len(journal.Entries) || journal.Count < journal.Groups {
		return fail()
	}
	rows, ok := object["entries"].([]any)
	if !ok || len(rows) != len(journal.Entries) {
		return fail()
	}
	result := map[string]executionVisibleMember{}
	for i, entry := range journal.Entries {
		row, ok := rows[i].(map[string]any)
		if !ok {
			return fail()
		}
		members, ok := row["member_metadata"].([]any)
		if !ok || len(members) == 0 || len(members) != len(entry.Members) {
			return fail()
		}
		representative := false
		for j, member := range entry.Members {
			fields, ok := members[j].(map[string]any)
			_, duplicate := result[member.ID]
			if !ok || len(fields) != 7 || duplicate || !strings.HasPrefix(member.ID, "github:") || !evidenceHex(strings.TrimPrefix(member.ID, "github:"), 64) || !executionRepositoryName.MatchString(member.Repo) || !evidenceHex(member.Commit, 40) || !evidencePath(member.Path) || !evidenceHex(member.SHA, 64) || !evidenceHex(member.Blob, 40) || member.Bytes <= 0 || member.SHA != entry.SHA || member.Blob != entry.Blob || member.Bytes != entry.Bytes {
				return fail()
			}
			if member.ID == entry.ID {
				if member.Repo != entry.Repo || member.Commit != entry.Commit || member.Path != entry.Path {
					return fail()
				}
				representative = true
			}
			result[member.ID] = member
		}
		if !representative {
			return fail()
		}
	}
	if len(result) != journal.Count {
		return fail()
	}
	return result, nil
}
