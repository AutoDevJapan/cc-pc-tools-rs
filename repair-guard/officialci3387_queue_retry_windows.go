package main

import "context"

// This admits preparation of the same ordinary CI retry, not an SDK expiry
// extension, another run/attempt/head, another owner, or a runner launch.
// The receipt's immutable binding and every source/actor/stop/physical gate
// remain unchanged. Explicit cancellation or any executed step is rejected.
func official3387CancelledQueueReply(raw []byte, queued object) bool {
	want := object{}
	for key, value := range queued {
		want[key] = value
	}
	want["status"] = "completed"
	want["conclusion"] = "cancelled"
	return official3894Match(raw, want)
}

func official3387UnassignedTimeoutProof(ctx context.Context, command func(context.Context, string, ...string) ([]byte, error)) error {
	job := object{}
	for key, value := range official3894Pins()[6].expected {
		job[key] = value
	}
	job["status"], job["conclusion"] = "completed", "cancelled"
	job["runner_id"], job["steps"] = 0, []object{}
	proofs := []official3894Pin{
		{"repos/AutoDevJapan/local-ai-console/actions/jobs/121273999986", "{id,run_id,head_sha,status,conclusion,labels,runner_id,steps}", job},
		{"repos/AutoDevJapan/local-ai-console/check-runs/121273999986/annotations", "{annotations:[.[]|{level:.annotation_level,title,message}]}", object{"annotations": []object{{
			"level": "failure", "title": "", "message": "The job has exceeded the maximum execution time while awaiting a runner for 24h0m0s",
		}}}},
	}
	for _, p := range proofs {
		raw, err := command(ctx, official3894Gate, "api", "--hostname", "github.com", p.endpoint, "--jq", p.query)
		if err != nil || !official3894Match(raw, p.expected) {
			return official3894Error("github_unassigned_queue_timeout_not_verified")
		}
	}
	return ctx.Err()
}
