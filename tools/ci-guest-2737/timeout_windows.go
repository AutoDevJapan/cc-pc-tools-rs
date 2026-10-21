package main

import (
	sourcepacket "cc/ci-source-2737"
	"time"
)

func hostJobTimeout(args []string) time.Duration {
	file := ""
	if len(args) == 5 && args[1] == "windows-job" {
		file = args[4]
	}
	if len(args) == 6 && args[1] == "linux-job" {
		file = args[5]
	}
	if file != "" {
		if q, e := readJobRequest(file); e == nil && q.Source.Product == sourcepacket.PressProduct {
			if j, ok := jobPolicy(q.Source, q.Role); ok {
				return time.Duration(j.TimeoutMinutes+60) * time.Minute
			}
		}
	}
	return 55 * time.Minute
}
