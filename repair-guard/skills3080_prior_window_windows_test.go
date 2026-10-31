package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSkills3080PriorRuntimeWindowCannotChooseAnAmbiguousOwner(t *testing.T) {
	at := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	p := skillsNativeResultProof{runtimePriorWindows: []skillsRuntimePriorWindow{{Start: at.Add(-time.Minute), End: at}, {Start: at, End: at.Add(time.Minute)}}}
	if p.priorRuntimeWindowAt(at) != nil || p.priorRuntimeWindow(at) {
		t.Fatal("shared boundary chose an arbitrary job")
	}
	if p.observePriorRuntimeInput([]byte(`{}`), at, false) || p.observePriorRuntimeContext([]byte(`{}`), at) || p.observePriorRuntimeGenerated([]byte(`{}`), at) {
		t.Fatal("ambiguous record accepted")
	}
}

func TestSkills3080PriorRuntimeContextRejectsAliasesAndContradictoryEffort(t *testing.T) {
	for _, kind := range []string{"valid", "turn_alias", "model_alias", "conflicting_effort", "missing_turn"} {
		t.Run(kind, func(t *testing.T) {
			at := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
			job := object{"cwd": "D:/CC/Work/synthetic", "model": "gpt-6-luna", "effort": "max"}
			p := skillsNativeResultProof{runtimePriorWindows: []skillsRuntimePriorWindow{{Start: at, End: at.Add(time.Minute), Job: job}}}
			v := object{"cwd": job["cwd"], "model": job["model"], "effort": "max", "turn_id": "22222222-2222-7222-8222-222222222222"}
			if kind == "turn_alias" {
				v["Turn_ID"] = v["turn_id"]
				delete(v, "turn_id")
			}
			if kind == "model_alias" {
				v["Model"] = v["model"]
				delete(v, "model")
			}
			if kind == "conflicting_effort" {
				v["reasoning_effort"] = "low"
			}
			if kind == "missing_turn" {
				delete(v, "turn_id")
			}
			b, _ := json.Marshal(object{"payload": v})
			if ok := p.observePriorRuntimeContext(b, at); ok != (kind == "valid") {
				t.Fatalf("context %s accepted=%v", kind, ok)
			}
		})
	}
}
