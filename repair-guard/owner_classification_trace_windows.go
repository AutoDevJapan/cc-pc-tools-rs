package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func classificationDiagnosticFlagPresent(args []string) bool {
	for _, arg := range args {
		if arg == "--classification-diagnostic" || arg == "-classification-diagnostic" || strings.HasPrefix(arg, "--classification-diagnostic=") || strings.HasPrefix(arg, "-classification-diagnostic=") {
			return true
		}
	}
	return false
}

func runClassificationDiagnosticCLI(args []string, stdout, stderr io.Writer, classify func(string) (ownerPathVerdict, *ownerClassificationTrace)) int {
	p, err := parseGuardArgs(args)
	if err != nil || !p.pathSet || !filepath.IsAbs(p.path) || p.productSet || p.successionSet || p.refreshSet || p.refreshRequestSet || p.runtimeUpdateSet || p.diagnosticSet || p.remainingArgs {
		_, _ = io.WriteString(stderr, "invalid classification diagnostic arguments\n")
		return 2
	}
	var trace *ownerClassificationTrace
	var output bytes.Buffer
	exit := runGuardCLIWithPath([]string{"--guard-path", p.path}, &output, stderr, nil, func(path string) ownerPathVerdict { v, t := classify(path); trace = t; return v })
	var result map[string]any
	if exit != 0 || json.Unmarshal(output.Bytes(), &result) != nil {
		return 1
	}
	if p.classificationDiagnostic {
		if trace != nil {
			trace.PolicyStage = trace.Stage
			if basis, ok := result["classification_basis"].(string); ok {
				trace.Stage = basis
			}
		}
		result["classification_diagnostic"] = trace
	}
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 1
	}
	return 0
}

// Only fixed phase names, counters and qualified timestamp metadata leave this
// observer. It grants no access, performs no additional fetch and never keeps
// registration contents, repository names, credentials or error messages.
type ownerClassificationTrace struct {
	started            time.Time
	Stage              string `json:"stage"`
	PolicyStage        string `json:"policy_stage,omitempty"`
	DurationMS         int64  `json:"duration_ms"`
	PhysicalMS         int64  `json:"physical_ms"`
	PhysicalCalls      int    `json:"physical_calls"`
	PhysicalState      string `json:"physical_state,omitempty"`
	RegistrationMS     int64  `json:"registration_ms"`
	RegistrationReads  int    `json:"registration_reads"`
	GitMS              int64  `json:"git_ms"`
	GitCalls           int    `json:"git_calls"`
	IdentityMS         int64  `json:"identity_ms"`
	IdentityCalls      int    `json:"identity_calls"`
	IdentityContext    string `json:"identity_context,omitempty"`
	RulesMS            int64  `json:"rules_ms"`
	MembershipMS       int64  `json:"membership_ms"`
	MembershipCalls    int    `json:"membership_calls"`
	PlainMS            int64  `json:"plain_ms"`
	PlainCalls         int    `json:"plain_calls"`
	FinalSnapshotMS    int64  `json:"final_snapshot_ms"`
	Checked            *int64 `json:"identity_checked_unix,omitempty"`
	Expires            *int64 `json:"identity_expires_unix,omitempty"`
	Observed           *int64 `json:"identity_observed_unix,omitempty"`
	IdentityValid      *bool  `json:"identity_valid,omitempty"`
	MultipleIdentities bool   `json:"multiple_identities"`
	identityRepository string
	DeadlineStage      string `json:"deadline_stage,omitempty"`
}

type ownerClassificationTraceKey struct{}

func ownerClassificationPhase(ctx context.Context, stage string) func() {
	switch stage {
	case "registration", "git", "identity", "rules", "final_snapshot", "membership", "plain":
	default:
		return func() {}
	}
	t, _ := ctx.Value(ownerClassificationTraceKey{}).(*ownerClassificationTrace)
	if t == nil {
		return func() {}
	}
	start := time.Now()
	identityBefore := false
	switch stage {
	case "registration":
		t.RegistrationReads++
	case "git":
		t.GitCalls++
	case "identity":
		t.IdentityCalls++
		identityBefore = ctx.Err() != nil
	case "membership":
		t.MembershipCalls++
	case "plain":
		t.PlainCalls++
	}
	return func() {
		elapsed := time.Since(start).Milliseconds()
		switch stage {
		case "registration":
			t.RegistrationMS += elapsed
		case "git":
			t.GitMS += elapsed
		case "identity":
			t.IdentityMS += elapsed
			state := "not_observed"
			if identityBefore {
				state = "before"
			} else if ctx.Err() != nil {
				state = "during"
			}
			if t.IdentityContext == "" {
				t.IdentityContext = state
			} else if t.IdentityContext != state {
				t.IdentityContext = "multiple"
			}
		case "rules":
			t.RulesMS += elapsed
		case "membership":
			t.MembershipMS += elapsed
		case "plain":
			t.PlainMS += elapsed
		case "final_snapshot":
			t.FinalSnapshotMS += elapsed
		}
		if ctx.Err() != nil && t.DeadlineStage == "" {
			t.DeadlineStage = stage
		}
	}
}

func ownerPolicyClassifyTraced(path string, access ownerPolicyAccess, trace *ownerClassificationTrace) ownerPathVerdict {
	physical := access.physical
	access.physical = func(input string) (string, error) {
		started := time.Now()
		target, err := physical(input)
		trace.PhysicalCalls++
		trace.PhysicalMS += time.Since(started).Milliseconds()
		switch {
		case err == nil && same(input, target):
			trace.PhysicalState = "same"
		case err == nil:
			trace.PhysicalState = "mismatch"
		case os.IsNotExist(err):
			trace.PhysicalState = "not_found"
		default:
			trace.PhysicalState = "unavailable"
		}
		return target, err
	}
	return ownerPolicyClassify(path, access)
}

func ownerClassificationIdentity(ctx context.Context, record ownerIdentityRecord, repository string, now time.Time) {
	t, _ := ctx.Value(ownerClassificationTraceKey{}).(*ownerClassificationTrace)
	if t == nil || !ownerIdentityValid(record, repository, time.Unix(record.Checked, 0)) || record.Checked > now.Unix() {
		return
	}
	if t.identityRepository != "" && t.identityRepository != repository {
		t.MultipleIdentities = true
		t.Checked = nil
		t.Expires = nil
		t.Observed = nil
		t.IdentityValid = nil
		return
	}
	if t.MultipleIdentities {
		return
	}
	t.identityRepository = repository
	checked, expires, observed, valid := record.Checked, record.Checked+300, now.Unix(), ownerIdentityValid(record, repository, now)
	t.Checked, t.Expires, t.Observed, t.IdentityValid = &checked, &expires, &observed, &valid
}

func classifyOwnerPathTraced(path string) (ownerPathVerdict, *ownerClassificationTrace) {
	t := &ownerClassificationTrace{started: time.Now()}
	v := ownerPolicyClassifyTraced(path, ownerPolicyAccess{legacy: ownerLegacyProtection, repair: repairPathAllowed, source: ownerRegisteredSource, physical: ownerRulePhysicalPath, organization: func(path string) ownerPathVerdict { return ownerOrganizationPathObserved(path, t) }}, t)
	t.DurationMS = time.Since(t.started).Milliseconds()
	t.Stage = v.Basis
	return v, t
}
