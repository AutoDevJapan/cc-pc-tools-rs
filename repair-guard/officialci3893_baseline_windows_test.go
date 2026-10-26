package main

import "testing"

func TestOfficialCI3893LegacyPurposeBoundary(t *testing.T) {
	_, err := parseGuardArgs([]string{"--construct-official-ci3893", "synthetic-no-input"})
	if err == nil {
		t.Fatal("the unconnected candidate must not change the general guard CLI")
	}
}

func TestOfficialCI3893UnknownExistingDataStillUnverified(t *testing.T) {
	for _, path := range []string{`D:\synthetic-unknown\sdk`, `D:\synthetic-unknown\credentials.json`, `D:\synthetic-unknown\sdk-other`} {
		v := ownerPolicyClassify(path, ownerPolicyAccess{
			legacy:   func(string) ownerLegacyState { return ownerLegacyAbsent },
			repair:   func(string) bool { return false },
			source:   func(string) ownerPathVerdict { return ownerUnverified() },
			physical: func(p string) (string, error) { return p, nil },
		})
		if v.AccessAllowed || v.Status != "unverified" || len(v.Categories) != 0 {
			t.Fatalf("unknown existing data changed classification: %+v", v)
		}
	}
}
