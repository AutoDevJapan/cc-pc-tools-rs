package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

func collect(ctx context.Context, opts Options, now time.Time, probe processProbe) Snapshot {
	s := Snapshot{
		SchemaVersion: schemaVersion, CapturedAt: now.UTC().Format(time.RFC3339Nano),
		Status: "ok", Consistency: "per_file_observation", Sources: map[string]Source{},
		Tasks: []Task{}, Jobs: []Job{}, Issues: []string{},
	}
	var store todoStore
	ts := readProjection(opts.TodoFile, opts.TodoBytes, &store, now, opts.StaleAfter)
	items := map[int64]todoItem{}
	if ts.Status == "ok" {
		if store.Items == nil || len(store.Items) > 50000 {
			ts.Status, ts.Complete = "invalid_record", false
		}
		for _, item := range store.Items {
			if item.ID <= 0 || item.Status == "" {
				ts.Status, ts.Complete = "invalid_record", false
				break
			}
			if _, exists := items[item.ID]; exists {
				ts.Status, ts.Complete = "invalid_record", false
				break
			}
			items[item.ID] = item
		}
	}
	if ts.Status != "ok" {
		items = map[int64]todoItem{}
		ts.FailedReads = 1
		s.Issues = append(s.Issues, "todo_"+ts.Status)
	} else {
		ts.RecordsRead = len(items)
	}
	s.Sources["todo"] = ts

	policy := priorityPolicy{}
	ps := newSource(now)
	if opts.PriorityFile == "" {
		ps.Status, ps.Complete = "not_configured", true
	} else {
		var raw map[string]json.RawMessage
		ps = readProjection(opts.PriorityFile, 1<<20, &raw, now, opts.StaleAfter)
		if ps.Status == "missing" {
			ps.Status, ps.Complete = "not_configured", true
		} else if ps.Status == "ok" {
			policy.legacy = raw
			if (raw["items"] != nil && json.Unmarshal(raw["items"], &policy.Items) != nil) ||
				(raw["projects"] != nil && json.Unmarshal(raw["projects"], &policy.Projects) != nil) {
				ps.Status, ps.Complete = "invalid_record", false
				policy = priorityPolicy{}
			}
		}
		if !ps.Complete {
			s.Issues = append(s.Issues, "priority_"+ps.Status)
		}
	}
	s.Sources["priority"] = ps
	loaded, js := loadJobs(ctx, opts, now)
	s.Sources["jobs"] = js
	if !js.Complete {
		s.Issues = append(s.Issues, "jobs_"+js.Status)
	}

	labels := map[int64]label{}
	ranks := map[int64]int{}
	for id, item := range items {
		labels[id] = itemLabel(item, policy)
		ranks[id] = labels[id].rank
	}
	// Mirror explicit unresolved prerequisite promotion from cc-job-policy.
	for changed := true; changed; {
		changed = false
		for id, item := range items {
			if item.Status == "done" || item.Status == "block" || item.WaitReady != nil {
				continue
			}
			for _, dependency := range pending(append(append([]int64{}, item.WaitOn...), item.WorkWaitOn...), items) {
				if rank, exists := ranks[dependency]; exists && ranks[id] < rank {
					ranks[dependency], changed = ranks[id], true
				}
			}
		}
	}
	// Link only canonical numeric relations or saved job references, never text,
	// worktree names, paths, prompts, parent titles, or matching project names.
	references := map[string][]int64{}
	for id, item := range items {
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
	allJobs := map[string]*Job{}
	for _, loaded := range loaded {
		j := loaded.meta
		ids := append(append([]int64{}, j.RelatedTodoIDs...), references[j.ID]...)
		ids = positiveIDs(append(ids, j.TodoID))
		l := publicLabel(j.Project)
		for _, id := range ids {
			if own, ok := labels[id]; ok && (own.rank < l.rank || own.rank == l.rank && l.kind == "generic" && own.kind == "public" && l.rank != 10) {
				l = own
			}
		}
		rank := l.rank
		for _, id := range ids {
			if r, ok := ranks[id]; ok && r < rank {
				rank = r
			}
		}
		worker := presence(j.CodexPID, j.CodexPIDCreated, probe)
		supervisor := presence(j.PID, j.PIDCreated, probe)
		heartbeat := timeClock(j.LastEventAt, now, opts.StaleAfter)
		activity := Clock{State: "unknown"}
		if j.Status == "running" {
			activity = heartbeat
		} else if terminalStatus(j.Status) {
			activity = timeClock(j.FinishedAt, now, opts.StaleAfter)
		}
		created, _ := time.Parse(time.RFC3339Nano, j.CreatedAt)
		row := &Job{
			Key: jobKey(j.ID), TodoIDs: ids, PublicLabel: l.name, LabelKind: l.kind,
			Department: l.department, Priority: rank, RecordedStatus: knownStatus(j.Status, false),
			Execution: execution(j, worker, supervisor), WorkerPresence: worker, SupervisorPresence: supervisor,
			Phase: phase(j), TaskType: safeTaskType(j.TaskType), WaitReasons: jobWaits(j, now, opts.StaleAfter),
			LastActivity: activity, LastHeartbeat: heartbeat, Source: loaded.source,
			rawID: j.ID, parentID: j.ParentJobID, independent: len(j.Subwork) != 0 && string(j.Subwork) != "null", created: created,
		}
		allJobs[j.ID] = row
	}
	// Dependent child jobs inherit a live parent's priority; finite independent
	// subwork consumes its own priority, matching the existing scheduler.
	for changed := true; changed; {
		changed = false
		for _, j := range allJobs {
			p, ok := allJobs[j.parentID]
			if ok && !j.independent && liveStatus(j.RecordedStatus) && liveStatus(p.RecordedStatus) && p.Priority < j.Priority {
				j.Priority, changed = p.Priority, true
			}
		}
	}
	byTask := map[int64][]*Job{}
	for _, j := range allJobs {
		for _, id := range j.TodoIDs {
			byTask[id] = append(byTask[id], j)
		}
	}
	selectedJobs := map[string]bool{}
	for id, item := range items {
		if len(opts.TaskIDs) > 0 && !opts.TaskIDs[id] {
			continue
		}
		if len(opts.TaskIDs) == 0 && !opts.IncludeDone && (item.Status == "done" || item.Status == "retired" || item.Status == "archived" || item.Status == "cancelled") {
			continue
		}
		l := labels[id]
		percent, progressState := measuredProgress(item)
		t := Task{
			ID: id, DisplayName: fmt.Sprintf("%s / ToDo %d", l.name, id), PublicLabel: l.name,
			LabelKind: l.kind, Department: l.department, Priority: ranks[id], RecordedStatus: knownStatus(item.Status, true),
			Percent: percent, ProgressState: progressState, Execution: "not_observed", Phase: "unknown",
			TaskType: safeTaskType(item.TaskType), TaskTypeSource: "unknown", Detail: DetailRef{Kind: "todo", TaskID: id},
			JobKeys: []string{}, WaitReasons: taskWaits(item, items, now, opts.StaleAfter),
			RecordUpdated: valueClock(item.Updated, now, opts.StaleAfter), LastActivity: Clock{State: "unknown"},
		}
		if t.TaskType != "unknown" {
			t.TaskTypeSource = "todo"
		}
		var subwork struct {
			ParentTodoID int64 `json:"parent_todo_id"`
		}
		if json.Unmarshal(item.Subwork, &subwork) == nil && subwork.ParentTodoID > 0 {
			t.ParentTodoID = &subwork.ParentTodoID
		}
		linked := byTask[id]
		sort.Slice(linked, func(i, j int) bool {
			if relevantJob(linked[i]) != relevantJob(linked[j]) {
				return relevantJob(linked[i])
			}
			if relevantJob(linked[i]) && executionRank(linked[i].Execution) != executionRank(linked[j].Execution) {
				return executionRank(linked[i].Execution) < executionRank(linked[j].Execution)
			}
			if !linked[i].created.Equal(linked[j].created) {
				return linked[i].created.After(linked[j].created)
			}
			return linked[i].Key < linked[j].Key
		})
		for n, j := range linked {
			if n > 0 && !relevantJob(j) {
				continue
			}
			selectedJobs[j.rawID] = true
			t.JobKeys = append(t.JobKeys, j.Key)
			if n == 0 {
				t.Execution, t.Phase = j.Execution, j.Phase
				t.LastActivity = j.LastActivity
				if t.TaskType == "unknown" && j.TaskType != "unknown" {
					t.TaskType, t.TaskTypeSource = j.TaskType, "job"
				}
			}
			t.WaitReasons = append(t.WaitReasons, j.WaitReasons...)
		}
		if len(linked) == 0 && !js.Complete {
			t.Execution = "unknown"
		}
		if len(t.WaitReasons) > 0 && t.Phase == "unknown" {
			t.Phase = "waiting"
		}
		s.Tasks = append(s.Tasks, t)
	}
	// Standalone live/unknown jobs survive unknown task identity or ToDo failure.
	for id, j := range allJobs {
		if len(opts.TaskIDs) == 0 && relevantJob(j) {
			selectedJobs[id] = true
		} else if len(opts.TaskIDs) > 0 {
			for _, tid := range j.TodoIDs {
				if opts.TaskIDs[tid] && relevantJob(j) {
					selectedJobs[id] = true
				}
			}
		}
		if selectedJobs[id] {
			s.Jobs = append(s.Jobs, *j)
		}
	}
	sort.Slice(s.Tasks, func(i, j int) bool {
		a, b := s.Tasks[i], s.Tasks[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if executionRank(a.Execution) != executionRank(b.Execution) {
			return executionRank(a.Execution) < executionRank(b.Execution)
		}
		return a.ID < b.ID
	})
	sort.Slice(s.Jobs, func(i, j int) bool {
		a, b := s.Jobs[i], s.Jobs[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if executionRank(a.Execution) != executionRank(b.Execution) {
			return executionRank(a.Execution) < executionRank(b.Execution)
		}
		return a.Key < b.Key
	})
	s.TaskTotal, s.JobTotal = len(s.Tasks), len(s.Jobs)
	if len(s.Tasks) > opts.MaxRows {
		s.Tasks, s.Truncated = s.Tasks[:opts.MaxRows], true
	}
	if len(s.Jobs) > opts.MaxRows {
		s.Jobs, s.Truncated = s.Jobs[:opts.MaxRows], true
	}
	if s.Truncated {
		s.Issues = append(s.Issues, "row_limit")
	}
	if ctx.Err() != nil {
		s.Issues = append(s.Issues, "deadline")
	}
	if len(s.Issues) > 0 {
		s.Status = "degraded"
	}
	if ts.Status != "ok" && !js.Complete && len(s.Jobs) == 0 {
		s.Status = "unavailable"
	}
	s.HeartbeatAt = time.Now().UTC().Format(time.RFC3339Nano)
	return s
}

func relevantJob(j *Job) bool {
	return liveStatus(j.RecordedStatus) || j.RecordedStatus == "unknown" || j.Source.Status != "ok" ||
		j.Execution == "running" || j.Execution == "alive_identity_unknown" || j.Execution == "unknown"
}

func executionRank(s string) int {
	switch s {
	case "running":
		return 0
	case "alive_identity_unknown", "supervising":
		return 1
	case "queued", "starting":
		return 2
	case "unknown":
		return 3
	case "not_observed":
		return 4
	default:
		return 5
	}
}
