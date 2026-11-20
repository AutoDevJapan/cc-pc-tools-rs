package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

// A refresh failure marker only says that a repository is cooling. The cause
// is kept beside it so that the refresh diagnostic can name it while the 300
// second cooldown applies.
//
// What the diagnostic reports is the fixed vocabulary below and the age in
// seconds: never a path, a repository name, a credential or CLI output. The
// sealed record also holds the repository, the catalogue hash and the failure
// second, like the failure marker beside it. Those three items only tie a
// cause to the marker it belongs to; they are never reported, and the record
// is sealed for the current Windows user. The cause grants nothing and the
// cooldown decision never reads it.

const ownerRefreshCauseDomain = "owner-source-refresh-cause-v1"

const (
	ownerRefreshKindFetchFailed       = "fetch_failed"
	ownerRefreshKindHelperUnfinished  = "helper_unfinished"
	ownerRefreshKindHelperStartFailed = "helper_start_failed"
	ownerRefreshKindUnknown           = "unknown"
)

type ownerRefreshCause struct {
	Domain     string `json:"domain"`
	Repository string `json:"repository"`
	Catalog    string `json:"catalog"`
	Failed     int64  `json:"failed"`
	Kind       string `json:"kind"`
	Stage      string `json:"stage"`
	CLIExit    int    `json:"cli_exit"`
	HTTP       int    `json:"http"`
}

// ownerRefreshCooldown is the sanitized cause of the failure marker that
// currently applies. Kind is "unknown" when no usable cause was stored: none at
// all, for example for a marker written by an earlier guard, or a record that
// is not a valid sealed cause of this marker.
type ownerRefreshCooldown struct {
	Kind       string
	Stage      string
	CLIExit    int
	HTTP       int
	AgeSeconds int64
}

// sameCause compares what a cooldown names. The age only says when, so two
// failures of one cause agree whatever their age.
func (c ownerRefreshCooldown) sameCause(other ownerRefreshCooldown) bool {
	return c.Kind == other.Kind && c.Stage == other.Stage && c.CLIExit == other.CLIExit && c.HTTP == other.HTTP
}

type ownerRefreshCooldownDiagnostic struct {
	Kind       string `json:"kind"`
	Stage      string `json:"stage,omitempty"`
	CLIExit    *int   `json:"cli_exit,omitempty"`
	HTTP       int    `json:"http,omitempty"`
	AgeSeconds int64  `json:"age_seconds"`
}

func ownerRefreshCauseName(repository string) string {
	return "cause-" + hashBytes([]byte(strings.ToLower(repository)+"\n"+hashBytes(ownerDepartmentCatalog))) + ".dpapi"
}

func ownerRefreshCauseKind(kind string) string {
	switch kind {
	case ownerRefreshKindFetchFailed, ownerRefreshKindHelperUnfinished, ownerRefreshKindHelperStartFailed:
		return kind
	}
	return ownerRefreshKindUnknown
}

func ownerRefreshCauseStage(stage string) string {
	switch stage {
	case "identity_fetch_failed", "identity_http_401", "identity_http_403", "identity_persistence_failed", "identity_binding_failed", "deadline_exceeded":
		return stage
	}
	return ""
}

func ownerRefreshCauseExit(exit int) int {
	if exit < 0 || exit > 255 {
		return -1
	}
	return exit
}

func ownerRefreshCauseHTTP(status int) int {
	if status == 401 || status == 403 {
		return status
	}
	return 0
}

func ownerRefreshExit(o *ownerRefreshObservation) int {
	if o == nil || o.cliExit == nil {
		return -1
	}
	return *o.cliExit
}

// ownerRecordRefreshCause stores the fixed cause for the failure marker that is
// written next with the same failure second. Storing it is best effort.
func ownerRecordRefreshCause(home, repository string, failed int64, kind, stage string, cliExit, httpStatus int) {
	_ = ownerWriteSealed(home, ownerRefreshCauseName(repository), ownerRefreshCause{
		Domain:     ownerRefreshCauseDomain,
		Repository: repository,
		Catalog:    hashBytes(ownerDepartmentCatalog),
		Failed:     failed,
		Kind:       ownerRefreshCauseKind(kind),
		Stage:      ownerRefreshCauseStage(stage),
		CLIExit:    ownerRefreshCauseExit(cliExit),
		HTTP:       ownerRefreshCauseHTTP(httpStatus),
	})
}

func ownerRefreshCooldownCause(home, repository string, now time.Time) *ownerRefreshCooldown {
	failed, cooling := ownerRefreshCooldownStart(home, repository, now)
	if !cooling {
		return nil
	}
	cause := &ownerRefreshCooldown{Kind: ownerRefreshKindUnknown, CLIExit: -1, AgeSeconds: now.Unix() - failed}
	path := filepath.Join(home, ownerRefreshCauseName(repository))
	if !currentPlain(path, false) {
		return cause
	}
	sealed, err := readBounded(path, 8192)
	if err != nil {
		return cause
	}
	plain, ok := ownerIdentityDPAPI(sealed, false)
	var stored ownerRefreshCause
	if !ok || json.Unmarshal(plain, &stored) != nil || stored.Domain != ownerRefreshCauseDomain || !strings.EqualFold(stored.Repository, repository) || stored.Catalog != hashBytes(ownerDepartmentCatalog) || stored.Failed != failed {
		return cause
	}
	cause.Kind = ownerRefreshCauseKind(stored.Kind)
	cause.Stage = ownerRefreshCauseStage(stored.Stage)
	cause.CLIExit = ownerRefreshCauseExit(stored.CLIExit)
	cause.HTTP = ownerRefreshCauseHTTP(stored.HTTP)
	return cause
}

func ownerRefreshCooldownProject(cause *ownerRefreshCooldown) *ownerRefreshCooldownDiagnostic {
	if cause == nil {
		return nil
	}
	projected := &ownerRefreshCooldownDiagnostic{Kind: cause.Kind, Stage: cause.Stage, HTTP: cause.HTTP, AgeSeconds: cause.AgeSeconds}
	if cause.CLIExit >= 0 {
		exit := cause.CLIExit
		projected.CLIExit = &exit
	}
	return projected
}
