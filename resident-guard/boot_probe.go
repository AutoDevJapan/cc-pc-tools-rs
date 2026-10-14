package main

import (
	"encoding/json"
	"strings"
	"time"
)

type cliAuthStatus struct {
	CLI      string `json:"cli"`
	Exit     int    `json:"exit"`
	Category string `json:"category"`
}

type sessionReport struct {
	Version              int             `json:"version"`
	CheckedAt            time.Time       `json:"checked_at"`
	FinishedAt           time.Time       `json:"finished_at"`
	ProcessID            int             `json:"process_id"`
	SessionID            *uint32         `json:"session_id"`
	SameExpectedUser     *bool           `json:"same_expected_user"`
	ProfileMatchesToken  *bool           `json:"profile_matches_token"`
	Elevated             *bool           `json:"elevation_marker"`
	AdministratorEnabled *bool           `json:"administrator_enabled"`
	NonInteractive       *bool           `json:"non_interactive"`
	Services             []serviceReport `json:"services"`
	CLIAuth              []cliAuthStatus `json:"cli_auth"`
	AnonymousHTTPS       string          `json:"anonymous_https"`
	CredentialChanges    bool            `json:"credential_changes"`
	ColdBootVerified     bool            `json:"cold_boot_verified"`
	InspectionError      string          `json:"inspection_error,omitempty"`
	TokenRestricted      *bool           `json:"token_restricted"`
	MediumIntegrity      *bool           `json:"medium_integrity"`
}

func observedBool(value bool) *bool { return &value }

func classifyCLIAuth(cli string, exit int, data []byte) cliAuthStatus {
	result := cliAuthStatus{CLI: cli, Exit: exit, Category: "unconfirmed"}
	if cli == "claude" {
		var status struct {
			LoggedIn   *bool  `json:"loggedIn"`
			AuthMethod string `json:"authMethod"`
		}
		if json.Unmarshal(data, &status) != nil || status.LoggedIn == nil {
			return result
		}
		if !*status.LoggedIn {
			result.Category = "not_logged_in"
		} else if exit == 0 {
			switch strings.ToLower(status.AuthMethod) {
			case "oauth", "claude.ai":
				result.Category = "subscription_status_readable"
			case "api_key", "api-key", "apikey":
				result.Category = "api_key_route_detected"
			}
		}
		return result
	}
	if cli == "codex" {
		message := strings.ToLower(string(data))
		switch {
		case strings.Contains(message, "logged in using api key"):
			result.Category = "api_key_route_detected"
		case exit == 0 && strings.Contains(message, "logged in using chatgpt"):
			result.Category = "subscription_status_readable"
		case strings.Contains(message, "not logged in"):
			result.Category = "not_logged_in"
		}
	}
	return result
}
