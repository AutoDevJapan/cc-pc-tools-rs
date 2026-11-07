package httpapi

import (
	"encoding/json"
	"errors"
	"sort"

	"autodevjapan/shared-todo-cloud/internal/store"
)

func (s *Server) departmentNames(assignments map[int64]allocation) []string {
	seen := map[string]bool{}
	var names []string
	for _, name := range s.Config.DepartmentOrder {
		if name != "" && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	var remaining []string
	for _, row := range assignments {
		if row.Department != "" && !seen[row.Department] {
			remaining = append(remaining, row.Department)
			seen[row.Department] = true
		}
	}
	sort.Strings(remaining)
	return append(names, remaining...)
}

type allocation struct {
	TaskID          int64  `json:"taskId"`
	Department      string `json:"department"`
	DisplayName     string `json:"displayName"`
	State           string `json:"state"`
	AgentID         string `json:"agentId"`
	LogicalAssigned bool   `json:"logicalAssigned"`
}

func parseAllocations(b json.RawMessage) (map[int64]allocation, error) {
	var envelope struct {
		Assignments []allocation `json:"assignments"`
	}
	if json.Unmarshal(b, &envelope) != nil || len(envelope.Assignments) == 0 {
		return nil, errors.New("invalid assignments")
	}
	result := make(map[int64]allocation, len(envelope.Assignments))
	for _, row := range envelope.Assignments {
		if row.TaskID <= 0 {
			return nil, errors.New("invalid task id")
		}
		if _, exists := result[row.TaskID]; exists {
			return nil, errors.New("duplicate task id")
		}
		result[row.TaskID] = row
	}
	return result, nil
}
func (s *Server) allocations() map[int64]allocation {
	r, err := s.Store.Registry("task-allocations")
	if err != nil {
		return nil
	}
	m, _ := parseAllocations(r.Document)
	return m
}
func allocationLabel(row allocation) string {
	state := map[string]string{"assigned": "配属済", "queued": "実行待ち", "registered": "登録済", "running": "実行中", "reused": "既存担当を継続", "historical": "完了履歴", "unverified": "担当を照合中"}[row.State]
	if state == "" {
		state = "担当を照合中"
	}
	if row.State == "unverified" && row.LogicalAssigned {
		state = "配属済・実行状況を照合中"
	}
	if row.DisplayName == "" {
		return state
	}
	return row.DisplayName + "（" + state + "）"
}
func (s *Server) departmentList(opts store.ListOptions, department string, assignments map[int64]allocation) (store.SummaryPage, error) {
	if department == "" {
		return s.Store.ListSummaries(opts)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	result := store.SummaryPage{NextAfter: opts.After, Items: []store.Summary{}}
	opts.Limit = 1000
	for {
		rows, err := s.Store.ListSummaries(opts)
		if err != nil {
			return result, err
		}
		if result.Revision != 0 && result.Revision != rows.Revision {
			return result, store.ErrConflict
		}
		result.Revision = rows.Revision
		for _, row := range rows.Items {
			if assignments[row.ID].Department != department {
				continue
			}
			if len(result.Items) == limit {
				result.HasMore = true
				return result, nil
			}
			result.Items = append(result.Items, row)
			result.NextAfter = row.ID
		}
		if !rows.HasMore {
			return result, nil
		}
		opts.After = rows.NextAfter
	}
}
