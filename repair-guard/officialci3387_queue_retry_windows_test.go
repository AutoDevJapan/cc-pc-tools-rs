package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const queueRetryJobEndpoint = "repos/AutoDevJapan/local-ai-console/actions/jobs/121273999986"
const queueRetryJobQuery = "{id,run_id,head_sha,status,conclusion,labels,runner_id,steps}"
const queueRetryAnnotationsEndpoint = "repos/AutoDevJapan/local-ai-console/check-runs/121273999986/annotations"
const queueRetryAnnotationsQuery = "{annotations:[.[]|{level:.annotation_level,title,message}]}"
const queueRetryTimeoutMessage = "The job has exceeded the maximum execution time while awaiting a runner for 24h0m0s"

func queueRetryCopy(input object) object {
	copy := object{}
	for key, value := range input {
		copy[key] = value
	}
	return copy
}

type queueRetryFixture struct {
	pins        []official3894Pin
	proof       object
	annotations object
	calls       []string
	failAt      string
}

func newQueueRetryFixture(cancelled bool) *queueRetryFixture {
	f := &queueRetryFixture{pins: official3894Pins()}
	for i := range f.pins {
		f.pins[i].expected = queueRetryCopy(f.pins[i].expected)
	}
	if cancelled {
		for _, index := range []int{4, 6} {
			f.pins[index].expected["status"] = "completed"
			f.pins[index].expected["conclusion"] = "cancelled"
		}
	}
	f.proof = queueRetryCopy(f.pins[6].expected)
	f.proof["runner_id"] = 0
	f.proof["steps"] = []object{}
	f.annotations = object{"annotations": []object{{"level": "failure", "title": "", "message": queueRetryTimeoutMessage}}}
	return f
}

func (f *queueRetryFixture) command(t *testing.T) func(context.Context, string, ...string) ([]byte, error) {
	t.Helper()
	return func(_ context.Context, file string, args ...string) ([]byte, error) {
		if file != official3894Gate || len(args) != 6 || args[0] != "api" || args[1] != "--hostname" || args[2] != "github.com" || args[4] != "--jq" {
			t.Fatal("retry proof escaped the fixed normal GitHub command")
		}
		endpoint, query := args[3], args[5]
		f.calls = append(f.calls, endpoint+"|"+query)
		if f.failAt == endpoint+"|"+query {
			return nil, errors.New("synthetic-private-command-error")
		}
		if endpoint == queueRetryJobEndpoint && query == queueRetryJobQuery {
			return json.Marshal(f.proof)
		}
		if endpoint == queueRetryAnnotationsEndpoint && query == queueRetryAnnotationsQuery {
			return json.Marshal(f.annotations)
		}
		for _, pin := range f.pins {
			if endpoint == pin.endpoint && query == pin.query {
				return json.Marshal(pin.expected)
			}
		}
		t.Fatalf("unregistered retry metadata query: %s", strings.Join(args, " "))
		return nil, nil
	}
}

func TestOfficialCI3387UnassignedQueueTimeoutPreservesOriginalPurpose(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		f := newQueueRetryFixture(cancelled)
		if err := official3894GitHubWith(context.Background(), f.command(t)); err != nil {
			t.Fatalf("same original CI purpose cannot prepare its normal retry: cancelled=%v err=%v", cancelled, err)
		}
		want := 9
		if cancelled {
			want = 11
		}
		if len(f.calls) != want {
			t.Fatalf("retry validation changed normal request count: got=%d want=%d", len(f.calls), want)
		}
	}
}

func TestOfficialCI3387QueueTimeoutRejectsUnrelatedOrExecutedCI(t *testing.T) {
	for _, change := range []string{
		"another-head", "another-run", "another-attempt", "latest-workflow-changed", "another-job", "another-label",
		"run-cancelled-job-queued", "run-queued-job-cancelled", "runner-used", "step-used", "steps-null", "runner-null",
		"explicit-cancel", "missing-annotation", "extra-annotation", "unknown-run-field", "unknown-job-field", "proof-command", "annotation-command",
	} {
		t.Run(change, func(t *testing.T) {
			f := newQueueRetryFixture(true)
			switch change {
			case "another-head":
				f.pins[4].expected["head_sha"] = strings.Repeat("a", 40)
			case "another-run":
				f.pins[4].expected["id"] = 40228177556
			case "another-attempt":
				f.pins[4].expected["run_attempt"] = 2
			case "latest-workflow-changed":
				f.pins[5].expected["id"] = 40228177556
			case "another-job":
				f.pins[6].expected["id"] = 121273999987
			case "another-label":
				f.pins[6].expected["labels"] = []string{"self-hosted", "Windows", "X64", "different"}
			case "run-cancelled-job-queued":
				f.pins[6].expected["status"], f.pins[6].expected["conclusion"] = "queued", nil
			case "run-queued-job-cancelled":
				f.pins[4].expected["status"], f.pins[4].expected["conclusion"] = "queued", nil
			case "runner-used":
				f.proof["runner_id"] = 1
			case "step-used":
				f.proof["steps"] = []object{{"number": 1, "status": "completed", "conclusion": "cancelled"}}
			case "steps-null":
				f.proof["steps"] = nil
			case "runner-null":
				f.proof["runner_id"] = nil
			case "explicit-cancel":
				f.annotations["annotations"] = []object{{"level": "failure", "title": "", "message": "The run was cancelled by a user"}}
			case "missing-annotation":
				f.annotations["annotations"] = []object{}
			case "extra-annotation":
				f.annotations["annotations"] = []object{{"level": "failure", "title": "", "message": queueRetryTimeoutMessage}, {"level": "failure", "title": "", "message": "different"}}
			case "unknown-run-field":
				f.pins[4].expected["unknown"] = true
			case "unknown-job-field":
				f.pins[6].expected["unknown"] = true
			case "proof-command":
				f.failAt = queueRetryJobEndpoint + "|" + queueRetryJobQuery
			case "annotation-command":
				f.failAt = queueRetryAnnotationsEndpoint + "|" + queueRetryAnnotationsQuery
			}
			err := official3894GitHubWith(context.Background(), f.command(t))
			if err == nil || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatalf("unverified retry state was accepted or exposed: change=%s err=%v", change, err)
			}
		})
	}
}
