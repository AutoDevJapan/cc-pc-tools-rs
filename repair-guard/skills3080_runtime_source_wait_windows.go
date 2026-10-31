package main

import "time"

const skillsSourceAdmissionBudget = 180 * time.Second
const skillsSourceAdmissionAttempt = 90 * time.Second
const skillsSourceAdmissionPause = 5 * time.Second
const skillsSourceAdmissionAttempts = 3

type skillsSourceAdmissionOps struct {
	now   func() time.Time
	check func() string
	run   func(time.Duration) skillsSourceCommandResult
	wait  func(time.Duration)
}

func skillsSourceAwaitAdmission(deadline time.Time, task int, requestSHA, nonce string, ops skillsSourceAdmissionOps) object {
	fail := func(stage string) object {
		return object{"qualified": false, "reason": "source", "failure_stage": stage, "data_access_granted": false}
	}
	var last object
	for attempt := 1; attempt <= skillsSourceAdmissionAttempts; attempt++ {
		if !ops.now().Before(deadline) {
			if last == nil {
				return fail("source_wait_deadline")
			}
			obj(last["normal_dispatch"])["admission_wait_exhausted"] = true
			return last
		}
		if stage := ops.check(); stage != "" {
			return fail(stage)
		}
		remaining := deadline.Sub(ops.now())
		if remaining <= 0 {
			if last == nil {
				return fail("source_wait_deadline")
			}
			obj(last["normal_dispatch"])["admission_wait_exhausted"] = true
			return last
		}
		limit := skillsSourceAdmissionAttempt
		if remaining < limit {
			limit = remaining
		}
		result := ops.run(limit)
		last = skillsRuntimeSourceDispatch(result, task, requestSHA, nonce)
		d := obj(last["normal_dispatch"])
		d["admission_attempts"] = attempt
		if !result.ok || last["qualified"] == true || !skillsSourceAdmissionRetryable(d) {
			return last
		}
		if attempt == skillsSourceAdmissionAttempts {
			d["admission_wait_exhausted"] = true
			return last
		}
		remaining = deadline.Sub(ops.now())
		if remaining <= 0 {
			d["admission_wait_exhausted"] = true
			return last
		}
		pause := skillsSourceAdmissionPause
		if remaining < pause {
			pause = remaining
		}
		ops.wait(pause)
	}
	return last
}

func skillsSourceAdmissionRetryable(d object) bool {
	if d["outcome"] != "not_launched" {
		return false
	}
	switch d["admission_reason"] {
	case "capacity", "priority_wait", "busy":
		return true
	}
	return false
}
