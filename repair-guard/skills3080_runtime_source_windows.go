package main

import (
	"regexp"
	"strings"
)

// A controller-issued correlation value, never a source digest or a grant.
var skillsRuntimeNoncePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func skillsRuntimeNonce(s string) bool {
	return skillsRuntimeNoncePattern.MatchString(s) && s != strings.Repeat("0", 32)
}

func skillsRuntimeNonceEncoding(raw []byte, repair bool) bool {
	v, ok := conversationJSON(raw)
	if !ok {
		return false
	}
	x, present := v["runtime_source_nonce"]
	if !present {
		return true
	}
	s, text := x.(string)
	return !repair && text && skillsRuntimeNonce(s)
}

type skillsRuntimeSourceRequest struct {
	Task        int    `json:"task"`
	PreviousJob string `json:"previous_job"`
	SpecSHA     string `json:"spec_sha256"`
	Nonce       string `json:"nonce,omitempty"`
}
