//go:build windows

package main

import "testing"

func TestRetirementDoesNotPromoteAQuarantinedV2ReceiptWithoutOriginalEvidence(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	r.State = "quarantined"
	r.Destination = root
	modify(t, c, r)
	_, err := c.retiredPlan(r.ID)
	code(t, err, "ORIGINAL_CREATE_UNVERIFIED")
}
