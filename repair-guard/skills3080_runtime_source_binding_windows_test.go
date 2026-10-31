package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSkills3080SourceStartRequiresActualNormalLaunch(t *testing.T) {
	for _, c := range []struct {
		name, raw string
		valid     bool
	}{
		{"string", `{"id":6291,"launched":"20300101-000001-todo-6291"}`, true},
		{"tuple", `{"launched":{"item":6291,"job":"20300101-000001-todo-6291"}}`, true},
		{"busy", `{"busy":true,"private":"must not be forwarded"}`, false},
		{"empty", `{}`, false},
		{"malformed", `not json`, false},
		{"wrong_task", `{"launched":{"item":6292,"job":"20300101-000001-todo-6292"}}`, false},
		{"wrong_job", `{"launched":{"item":6291,"job":"20300101-000001-todo-6292"}}`, false},
		{"conflict", `{"id":6292,"launched":{"item":6291,"job":"20300101-000001-todo-6291"}}`, false},
		{"duplicate", `{"id":6291,"id":6291,"launched":"20300101-000001-todo-6291"}`, false},
		{"alias", `{"id":6291,"Launched":"20300101-000001-todo-6291"}`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var output bytes.Buffer
			code := runSkillsCLI([]string{"--start-skills3080-runtime-source", "D:/synthetic/request.json"}, &output, func(string, string) object {
				return skillsRuntimeSourceDispatchReceipt([]byte(c.raw), 6291, hashBytes([]byte("request")), "11111111111111111111111111111111")
			})
			var result object
			if json.Unmarshal(output.Bytes(), &result) != nil || (code == 0) != c.valid || (result["qualified"] == true) != c.valid {
				t.Fatal("no launch was reported as successful")
			}
			if bytes.Contains(output.Bytes(), []byte("private")) || (!c.valid && result["failure_stage"] != "source_normal_dispatch") {
				t.Fatal("normal admission failure lost its fixed stage or exposed unselected data")
			}
		})
	}
}
