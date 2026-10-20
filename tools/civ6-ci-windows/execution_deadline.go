package main

import "time"

// The queue's absolute bound includes time spent in prepare and waiting for
// Cargo. The gate child reads the same immutable plan after acquiring its slot.
func executionDeadline(p Plan, start time.Time) time.Time {
	d := start.Add(time.Duration(p.Seconds+60) * time.Second)
	if p.ExecuteBefore != 0 {
		outer := time.Unix(0, p.ExecuteBefore)
		if outer.Before(d) {
			return outer
		}
	}
	return d
}
