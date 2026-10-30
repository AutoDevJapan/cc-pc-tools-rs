package main

import "time"

type skillsRuntimePriorWindow struct{ Start, End time.Time }
type skillsRuntimeOrigin struct {
	Start   time.Time
	Windows []skillsRuntimePriorWindow
}

func skillsRuntimeSourceOrigin(s *skillsFS, job object, start time.Time) (skillsRuntimeOrigin, bool) {
	id := int(conversationNumber(job["todo_id"]))
	reg, ok := skillsRegistryRead([]int{id})
	if !ok {
		return skillsRuntimeOrigin{}, false
	}
	return skillsRuntimeSourceOriginWith(reg.Tasks[id], job, start, func(id string) (object, bool) {
		return skillsReadJob(s, id)
	})
}

// The task/final window remains the current job's. This distinct source origin
// must come from the normally registered continuation chain, never a caller's
// claimed first-start time or bootstrap digest.
func skillsRuntimeSourceStartWith(row skillsTask, job object, start time.Time, read func(string) (object, bool)) (time.Time, bool) {
	origin, ok := skillsRuntimeSourceOriginWith(row, job, start, read)
	return origin.Start, ok
}
func skillsRuntimeSourceOriginWith(row skillsTask, job object, start time.Time, read func(string) (object, bool)) (skillsRuntimeOrigin, bool) {
	id, thread := str(job["id"]), str(job["thread_id"])
	currentStart, startErr := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	if row.ID < 1 || !skillsJobTask(job, row.ID) || !skillsResultID(skillsResultRequest{Task: row.ID, Job: id}) || !skillsUUID.MatchString(thread) || startErr != nil || !currentStart.Equal(start) {
		return skillsRuntimeOrigin{}, false
	}
	current := -1
	seen := map[string]bool{}
	for i, j := range row.Jobs {
		if seen[j] {
			return skillsRuntimeOrigin{}, false
		}
		seen[j] = true
		if j == id {
			current = i
		}
	}
	if current < 0 {
		return skillsRuntimeOrigin{}, false
	}
	resume := str(job["resume_session"])
	if resume == "" {
		return skillsRuntimeOrigin{Start: start}, true
	}
	if resume != thread {
		return skillsRuntimeOrigin{}, false
	}
	source, nextStart, matched := start, start, false
	initialResume := resume
	windows := []skillsRuntimePriorWindow{}
	for i := current - 1; i >= 0; i-- {
		prior, ok := read(row.Jobs[i])
		if !ok || prior["id"] != row.Jobs[i] || !skillsJobTask(prior, row.ID) {
			return skillsRuntimeOrigin{}, false
		}
		if str(prior["thread_id"]) != thread {
			continue
		}
		if initialResume != thread {
			return skillsRuntimeOrigin{}, false
		}
		if !same(str(prior["cwd"]), str(job["cwd"])) || prior["sandbox"] != job["sandbox"] || skillsModel(str(prior["model"])) == "" || skillsModel(str(prior["model"])) != skillsModel(str(job["model"])) || prior["effort"] != job["effort"] {
			return skillsRuntimeOrigin{}, false
		}
		switch str(prior["status"]) {
		case "succeeded", "failed", "cancelled", "canceled", "lost":
		default:
			return skillsRuntimeOrigin{}, false
		}
		began, e := time.Parse(time.RFC3339Nano, str(prior["started_at"]))
		ended, e2 := time.Parse(time.RFC3339Nano, str(prior["finished_at"]))
		if e != nil || e2 != nil || began.After(ended) || !began.Before(nextStart) || ended.After(nextStart) {
			return skillsRuntimeOrigin{}, false
		}
		initialResume = str(prior["resume_session"])
		if initialResume != "" && initialResume != thread {
			return skillsRuntimeOrigin{}, false
		}
		windows = append(windows, skillsRuntimePriorWindow{began, ended})
		source, nextStart, matched = began, began, true
	}
	if !matched || initialResume != "" {
		return skillsRuntimeOrigin{}, false
	}
	return skillsRuntimeOrigin{source, windows}, true
}
