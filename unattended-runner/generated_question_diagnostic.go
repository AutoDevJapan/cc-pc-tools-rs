package main

import "errors"

// Codes describe the first failed check, never the contents of the rejected
// input. In particular a guard failure says nothing about a later schema,
// content review, original assignment, native tool response or image bytes.
type generatedQuestionFailure struct {
	stage, code string
}

func (e *generatedQuestionFailure) Error() string { return e.code }
func (e *generatedQuestionFailure) Unwrap() error { return errConversationMedia }

func generatedFailure(stage, code string) error {
	return &generatedQuestionFailure{stage: stage, code: code}
}

type generatedQuestionDiagnostic struct {
	Domain     string `json:"domain"`
	Validated  bool   `json:"validated"`
	Registered bool   `json:"registered"`
	Stage      string `json:"stage"`
	Code       string `json:"code"`
}

func generatedDiagnostic(err error) generatedQuestionDiagnostic {
	r := generatedQuestionDiagnostic{Domain: "owner-generated-question-diagnostic-v1", Validated: err == nil, Stage: "complete", Code: "evidence_validated"}
	if err != nil {
		r.Stage, r.Code = "validation", "evidence_not_qualified"
		var failure *generatedQuestionFailure
		if errors.As(err, &failure) {
			r.Stage, r.Code = failure.stage, failure.code
		}
	}
	return r
}
