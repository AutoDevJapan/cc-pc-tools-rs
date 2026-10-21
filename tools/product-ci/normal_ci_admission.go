package main

import (
	"cc/product-ci/normalci"
	"context"
)

type normalCIJobPin = normalci.Pin
type normalCIJobBinding = normalci.Binding
type normalCIOfficialJob = normalci.OfficialJob

func normalCIJobPlan(ctx context.Context, p normalCIJobPin, read api) (normalCIJobBinding, error) {
	return normalci.Plan(ctx, p, read)
}
func readNormalJobs(ctx context.Context, endpoint string, read api) ([]normalCIOfficialJob, error) {
	return normalci.ReadJobs(ctx, endpoint, read)
}
func normalCIRevision(s string) bool { return normalci.Revision(s) }
func verifyNormalRunnerClaim(b normalCIJobBinding, j normalCIOfficialJob, id int64, name string) error {
	return normalci.VerifyRunnerClaim(b, j, id, name)
}
