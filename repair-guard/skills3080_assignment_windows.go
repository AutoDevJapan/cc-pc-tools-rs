package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Only the two current Skills actors gain canonical manual-assignment support.
// Historical authors, reviewer admission and other updaters keep their checks.
func skillsProjectActorAssignment(metadata object, row *skillsTask) bool {
	if !skillsAssignmentNames(metadata, "id", "scheduler", "resume_generation", "worker_receipt") {
		return false
	}
	scheduler := obj(metadata["scheduler"])
	if !skillsAssignmentNames(scheduler, "job", "key") {
		return false
	}
	receipt := obj(metadata["worker_receipt"])
	if !skillsAssignmentNames(receipt, "job", "generation") {
		return false
	}
	key, present := scheduler["key"]
	row.assignmentKeyNull = present && key == nil
	if !row.assignmentKeyNull {
		return true
	}
	generation, valid := skillsAssignmentGeneration(metadata["resume_generation"])
	receiptGeneration, receiptValid := skillsAssignmentGeneration(receipt["generation"])
	job, jobValid := receipt["job"].(string)
	if valid && receiptValid && generation == receiptGeneration && len(receipt) == 2 && jobValid && job != "" && job == row.Scheduler.Job {
		row.assignmentJob, row.assignmentGeneration = job, generation
	}
	return true
}

func skillsAssignmentNames(m object, names ...string) bool {
	for key := range m {
		for _, name := range names {
			if key != name && strings.EqualFold(key, name) {
				return false
			}
		}
	}
	return true
}

func skillsAssignmentGeneration(value any) (uint64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	generation, err := strconv.ParseUint(string(number), 10, 64)
	return generation, err == nil && generation > 0
}

func skillsActorAssignmentBound(row skillsTask, job object) bool {
	if (row.ID != 3080 && row.ID != 3686) || job["id"] != row.Scheduler.Job || !skillsJobTask(job, row.ID) {
		return false
	}
	launch, ok := job["launch_key"].(string)
	if !ok || launch == "" {
		return false
	}
	if row.Scheduler.Key != "" {
		return launch == row.Scheduler.Key
	}
	return row.assignmentKeyNull && row.assignmentGeneration > 0 && row.assignmentJob == job["id"]
}
