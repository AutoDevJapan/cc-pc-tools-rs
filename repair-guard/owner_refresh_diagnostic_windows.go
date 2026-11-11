package main

import (
	"context"
	"strings"
	"time"
)

type ownerRefreshDiagnostic struct {
	Refreshed                 bool   `json:"refreshed"`
	OriginalRegistryRecovered bool   `json:"original_registry_recovered"`
	Stage                     string `json:"stage"`
	IdentityFailure           string `json:"identity_failure,omitempty"`
	Attempted                 bool   `json:"attempted"`
	CLIExit                   *int   `json:"cli_exit,omitempty"`
	Timeout                   bool   `json:"timeout"`
	DurationMS                int64  `json:"duration_ms"`
}
type ownerRefreshObservation struct {
	claimValid, binding, cooling, attempted, fetched, validated, saved, timeout bool
	http                                                                        int
	cliExit                                                                     *int
	started                                                                     time.Time
	identityFailed                                                              bool
	failureStage                                                                string
	cliExitMixed                                                                bool
	lockUnavailable                                                             bool
}

func (o *ownerRefreshObservation) includeIdentity(detail *ownerRefreshObservation, success bool) {
	o.binding = o.binding || detail.binding
	o.attempted = o.attempted || detail.attempted
	o.timeout = o.timeout || detail.timeout
	if detail.cliExit != nil && !o.cliExitMixed {
		if o.cliExit == nil {
			code := *detail.cliExit
			o.cliExit = &code
		} else if *o.cliExit != *detail.cliExit {
			o.cliExit = nil
			o.cliExitMixed = true
		}
	}
	if !success {
		o.identityFailed = true
		stage := ownerRefreshProject(context.Background(), detail, false, time.Now()).Stage
		if o.failureStage == "" {
			o.failureStage = stage
		} else if o.failureStage != stage {
			o.failureStage = "multiple_identity_failures"
		}
	}
}

func ownerRefreshProject(ctx context.Context, o *ownerRefreshObservation, success bool, at time.Time) ownerRefreshDiagnostic {
	r := ownerRefreshDiagnostic{Refreshed: success, Attempted: o.attempted, CLIExit: o.cliExit, DurationMS: at.Sub(o.started).Milliseconds()}
	if r.DurationMS < 0 {
		r.DurationMS = 0
	}
	switch {
	case ctx.Err() != nil || o.timeout:
		r.Stage = "deadline_exceeded"
		r.Timeout = true
	case o.identityFailed:
		if success {
			r.Stage = "partial_identity_failure"
			r.IdentityFailure = o.failureStage
		} else {
			r.Stage = o.failureStage
		}
	case success:
		r.Stage = "complete"
	case !o.claimValid:
		r.Stage = "claim_unavailable"
	case !o.binding:
		r.Stage = "binding_unavailable"
	case o.lockUnavailable:
		r.Stage = "identity_lock_unavailable"
	case o.cooling:
		r.Stage = "cooldown_no_valid_identity"
	case o.attempted && !o.fetched && o.http == 401:
		r.Stage = "identity_http_401"
	case o.attempted && !o.fetched && o.http == 403:
		r.Stage = "identity_http_403"
	case o.attempted && !o.fetched:
		r.Stage = "identity_fetch_failed"
	case o.validated && !o.saved:
		r.Stage = "identity_persistence_failed"
	default:
		r.Stage = "identity_binding_failed"
	}
	return r
}

type ownerRefreshObservationKey struct{}

func refreshDiagnosticFlagPresent(args []string) bool {
	for _, arg := range args {
		if arg == "--diagnostic" || arg == "-diagnostic" || strings.HasPrefix(arg, "--diagnostic=") || strings.HasPrefix(arg, "-diagnostic=") {
			return true
		}
	}
	return false
}

type ownerRefreshErrorBuffer struct{ data []byte }

func (b *ownerRefreshErrorBuffer) Write(p []byte) (int, error) {
	if b.data == nil {
		b.data = make([]byte, 0, 16384)
	}
	if len(b.data) < 16384 {
		n := 16384 - len(b.data)
		if n > len(p) {
			n = len(p)
		}
		b.data = append(b.data, p[:n]...)
	}
	return len(p), nil
}

func (b *ownerRefreshErrorBuffer) erase() {
	clear(b.data)
}

func ownerRefreshRun(path, request string, diagnostic bool) (bool, ownerRefreshDiagnostic) {
	if !diagnostic {
		return ownerRefreshIdentityRequest(path, request), ownerRefreshDiagnostic{}
	}
	o := &ownerRefreshObservation{}
	ok := ownerRefreshIdentityRequestWith(path, request, ownerIdentityHome, time.Now, ownerOrganizationQuery, ownerGitHubIdentityWith, o)
	return ok, ownerRefreshProject(context.Background(), o, ok, time.Now())
}
