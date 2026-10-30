package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSkills3080ConsumerLiveAndCompletedSelectTheSameProvenProject(t *testing.T) {
	c, _, _, job := skillsConsumerFixture()
	spec, _ := json.Marshal(c)
	job["started_at"] = "2026-10-30T23:00:00+09:00"
	job["first_started_at"] = job["started_at"]
	job["resume_session"] = nil
	s := skillsFS{bootstrapProjects: map[string][]byte{c.ManifestCommit: []byte("independently published project")}}
	for _, status := range []string{"running", "succeeded"} {
		job["status"] = status
		project, required := skillsTypedRuntimeProject(&s, job, spec)
		if !required || !bytes.Equal(project, s.bootstrapProjects[c.ManifestCommit]) {
			t.Fatalf("consumer status %s lost runtime binding", status)
		}
	}
	delete(s.bootstrapProjects, c.ManifestCommit)
	project, required := skillsTypedRuntimeProject(&s, job, spec)
	if len(project) != 0 || !required {
		t.Fatal("missing public source downgraded a fresh consumer")
	}
	job["resume_session"] = "same-old-session"
	if _, required := skillsTypedRuntimeProject(&s, job, spec); required {
		t.Fatal("historical resumed contract rewritten as a fresh launch")
	}
}
