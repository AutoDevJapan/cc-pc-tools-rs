package inputqual

import (
	"os"
	"strings"
	"testing"
)

func TestCaseAliasedJSONKeysDoNotReplaceAuthority(t *testing.T) {
	var scope struct {
		Stopped bool   `json:"stopped"`
		Head    string `json:"head_sha"`
	}
	if err := decode([]byte(`{"stopped":false,"head_sha":"original"}`), &scope, true); err != nil || scope.Stopped || scope.Head != "original" {
		t.Fatal("canonical control failed")
	}
	for _, raw := range []string{`{"stopped":false,"stopped":true}`, `{"stopped":false,"STOPPED":true}`, `{"head_sha":"original","HEAD_SHA":"other"}`} {
		err := decode([]byte(raw), &scope, true)
		if err == nil || !strings.HasPrefix(err.Error(), "JSON key") {
			t.Fatal("duplicate key did not reach JSON key rejection")
		}
	}
}

func TestRoleSubstitutionRejectedAtEvidenceBoundary(t *testing.T) {
	f := newFixture(t)
	if _, err := qualify(f.p.pin, f.p); err != nil {
		t.Fatal("eligible control failed")
	}
	// Update the synthetic prior bindings too, so the request reaches the role
	// verification instead of merely failing an earlier plan hash check.
	if err := os.WriteFile(f.p.review, mustReadFixture(t, f.p.acceptance), 0600); err != nil {
		t.Fatal(err)
	}
	f.rebind()
	_, err := qualify(f.p.pin, f.p)
	if err == nil || !strings.HasSuffix(err.Error(), "source_evidence") {
		t.Fatal("wrong evidence role was not rejected at source evidence boundary")
	}
}
