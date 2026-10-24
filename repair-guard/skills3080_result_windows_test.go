package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSkills3080RegistryProjection(t *testing.T) {
	raw := fmt.Sprintf(`{"projects":{"other":"never return","cc-pc-tools-rs":%q},"items":[{"id":3080,"project":"cc-pc-tools-rs","note":"not projected"},{"id":123,"project":"unrelated","secret":"not projected"}]}`, roots[3])
	r, ok := skillsRegistryProjection(strings.NewReader(raw), []int{3080})
	if !ok || len(r.Tasks) != 1 || r.Tasks[3080].ID != 3080 {
		t.Fatal("projection failed")
	}
	b, _ := json.Marshal(r)
	for _, secret := range []string{"unrelated", "secret", "not projected", "other"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("unrelated operational data returned")
		}
	}
	for _, mutated := range []string{strings.Replace(raw, `"projects":`, `"projects":{},"projects":`, 1), strings.Replace(raw, `"id":123`, `"id":3080`, 1), raw + " {}", `{"projects":{},"items":[]}`} {
		if _, ok := skillsRegistryProjection(strings.NewReader(mutated), []int{3080}); ok {
			t.Fatal("ambiguous registry admitted")
		}
	}
}
func TestSkills3080ResultExactBinding(t *testing.T) {
	spec := []byte("Exact public research; parent=" + skillsParentRoot)
	r := skillsResultRequest{Task: 3674, Job: "20261024-231416-todo-3674", SpecSHA: hashBytes(spec)}
	row := skillsTask{ID: r.Task, Project: "cc-pc-tools-rs", Spec: "D:/CC/Tools/todo/specs/3674-a.md", Jobs: []string{r.Job}}
	row.Scheduler.Worktree = "D:/CC/Work/test-public-research"
	reg := skillsRegistry{Tasks: map[int]skillsTask{3080: {ID: 3080, Project: "cc-pc-tools-rs"}, 3674: row}}
	parse := func(raw string) object {
		m, ok := conversationJSON([]byte(raw))
		if !ok {
			t.Fatal("fixture JSON")
		}
		return m
	}
	job := parse(fmt.Sprintf(`{"id":%q,"todo_id":3674,"status":"succeeded","exit_code":0,"cwd":%q}`, r.Job, row.Scheduler.Worktree))
	originals := map[int]string{3674: r.SpecSHA}
	if !skillsResultBindingWith(r, reg, job, spec, false, originals) {
		t.Fatal("positive registered public result rejected")
	}
	for _, field := range []string{"id", "todo_id", "status", "exit_code", "cwd"} {
		copy := objClone(job)
		copy[field] = "wrong"
		if skillsResultBindingWith(r, reg, copy, spec, false, originals) {
			t.Fatalf("wrong %s admitted", field)
		}
	}
	changed := r
	changed.SpecSHA = strings.Repeat("9", 64)
	if skillsResultBindingWith(changed, reg, job, spec, false, originals) {
		t.Fatal("wrong spec admitted")
	}
	if skillsResultBindingWith(r, reg, job, nil, false, originals) {
		t.Fatal("unknown input zeroed")
	}
	row.Jobs = append(row.Jobs, "20261025-010000-todo-3674")
	reg.Tasks[3674] = row
	if skillsResultBindingWith(r, reg, job, spec, false, originals) {
		t.Fatal("stale ownership generation admitted")
	}
}
func objClone(m object) object {
	copy := object{}
	for k, v := range m {
		copy[k] = v
	}
	return copy
}

func TestSkills3080NativeProjection(t *testing.T) {
	id := "01234567-1234-1234-1234-0123456789ab"
	cwd := "D:/CC/Work/public"
	start, _ := time.Parse(time.RFC3339, "2026-10-24T23:00:00Z")
	raw := fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"cwd\":%q,\"instructions\":\"never project\"}}\n", id, cwd) + `{"type":"response_item","payload":{"secret":"never project"}}` + "\n" + `{"type":"turn_context","timestamp":"2026-10-24T23:01:00Z","payload":{"model":"claude-sonnet-5-5","effort":"max","service_tier":"default","instructions":"never project"}}` + "\n"
	out, ok := skillsNativeProjection(strings.NewReader(raw), id, cwd, start, start.Add(time.Hour))
	if !ok || len(out) != 1 || out[0]["effort"] != "max" {
		t.Fatal("native projection failed")
	}
	b, _ := json.Marshal(out)
	if bytes.Contains(b, []byte("instructions")) || bytes.Contains(b, []byte("never project")) {
		t.Fatal("non-native-scope content returned")
	}
	for _, mutated := range []string{strings.Replace(raw, id, "00000000-0000-0000-0000-000000000000", 1), strings.Replace(raw, "23:01:00", "22:01:00", 1), strings.Replace(raw, `"model":"claude-sonnet-5-5"`, `"model":"claude-sonnet-5-5","model":"other"`, 1), raw + "not JSON"} {
		if _, ok := skillsNativeProjection(strings.NewReader(mutated), id, cwd, start, start.Add(time.Hour)); ok {
			t.Fatal("unbound native observation admitted")
		}
	}
	future := raw + `{"type":"turn_context","timestamp":"2026-10-25T00:01:00Z","payload":{"model":"gpt-6-astra","effort":"low","service_tier":"default"}}` + "\n"
	bounded, ok := skillsNativeProjection(strings.NewReader(future), id, cwd, start, start.Add(time.Hour))
	if !ok || len(bounded) != 1 || bounded[0]["model"] != "claude-sonnet-5-5" {
		t.Fatal("a later unrelated attempt leaked into this result")
	}
	wrongCWD := strings.Replace(raw, `"model":"claude-sonnet-5-5"`, `"cwd":"D:/unrelated","model":"claude-sonnet-5-5"`, 1)
	if _, ok := skillsNativeProjection(strings.NewReader(wrongCWD), id, cwd, start, start.Add(time.Hour)); ok {
		t.Fatal("unrelated turn working location admitted")
	}
}
func TestSkills3080ReviewAcceptance(t *testing.T) {
	sha := strings.Repeat("a", 64)
	accepted := skillsReceived{Spec: []byte("SKILLS3080_MANIFEST_SHA256 " + sha), Metadata: object{"model": "anthropic/claude-sonnet-5-5", "effort": "max"}, Native: []object{{"model": "claude-sonnet-5-5", "effort": "max"}}, Body: []byte(`状態: 完了` + "\nSKILLS3080_ACCEPTANCE " + `{"domain":"skills-3080-review-v1","manifest_sha256":"` + sha + `","accepted":true,"must":0}`)}
	if !skillsReviewAccepted(accepted, sha, "gpt-6.1-sol") {
		t.Fatal("accepted exact review rejected")
	}
	for _, mutate := range []func(*skillsReceived){
		func(r *skillsReceived) { r.Spec = nil }, func(r *skillsReceived) { r.Native = nil }, func(r *skillsReceived) { r.Native = []object{{"model": "gpt-6.1-sol", "effort": "max"}} }, func(r *skillsReceived) { r.Body = bytes.Replace(r.Body, []byte(`"must":0`), []byte(`"must":1`), 1) }, func(r *skillsReceived) {
			r.Body = append(r.Body, append([]byte("\n"), bytes.Split(r.Body, []byte("\n"))[1]...)...)
		},
	} {
		copy := accepted
		mutate(&copy)
		if skillsReviewAccepted(copy, sha, "gpt-6.1-sol") {
			t.Fatal("unaccepted review admitted")
		}
	}
	if skillsReviewAccepted(accepted, sha, "claude-sonnet-5-5") {
		t.Fatal("same-model review admitted")
	}
}
func TestSkills3080CLIIsolation(t *testing.T) {
	for _, args := range [][]string{{"--qualify-skills3080"}, {"--qualify-skills3080", "relative"}, {"--qualify-skills3080", "D:/request", "--guard-path", "D:/unknown"}, {"--create-skills3080", "D:/request", "--create-skills3080", "D:/request"}, {"qualify-skills3080", "D:/request"}} {
		var out bytes.Buffer
		calls := 0
		exit := runSkillsCLI(args, &out, func(string, string) object { calls++; return object{"qualified": true} })
		if exit == 0 || calls != 0 {
			t.Fatal("mixed or ambiguous CLI admitted")
		}
	}
	var out bytes.Buffer
	exit := runSkillsCLI([]string{"--qualify-skills3080", "D:/request"}, &out, func(string, string) object {
		return object{"qualified": false, "reason": "C:/private/name", "secret": "hidden"}
	})
	if exit != 1 || strings.Contains(out.String(), "private") || strings.Contains(out.String(), "hidden") {
		t.Fatal("refusal leaked input")
	}
	// A purpose flag is dispatched separately; a normal unknown path keeps the
	// exact original verdict and gains no Skills-root classifier exception.
	var ordinary bytes.Buffer
	runGuardCLIWithPath([]string{"--guard-path", "D:/unknown"}, &ordinary, &out, func(int) object { return nil }, func(string) ownerPathVerdict { return ownerUnverified() })
	m, ok := conversationJSON(ordinary.Bytes())
	if !ok || m["access_allowed"] != false || m["classification_unverified"] != true {
		t.Fatal("normal classifier changed")
	}
}

func TestSkills3080SourcesAndCodeAcceptance(t *testing.T) {
	m := skillsTestManifest()
	m.Sources[0].Repository = "6mile/skills"
	m.Sources[0].Commit = skillsPublicSources["6mile/skills"]
	source, license := []byte("fixed public source"), []byte("accepted individual license")
	m.Sources[0].SHA = hashBytes(source)
	m.Sources[0].License.SHA = hashBytes(license)
	m.Files = append(m.Files, skillsOutput{Target: "sample/references/upstream.txt", SHA: hashBytes(source)}, skillsOutput{Target: "sample/references/LICENSE.txt", SHA: hashBytes(license)})
	if !skillsSourcesBound(m) {
		t.Fatal("source preservation positive rejected")
	}
	m.Sources[0].Repository = "other/department"
	if skillsSourcesBound(m) {
		t.Fatal("different source scope admitted")
	}
	m.Sources[0].Repository = "6mile/skills"
	m.Files = m.Files[:len(m.Files)-1]
	if skillsSourcesBound(m) {
		t.Fatal("missing preserved license accepted")
	}
	head := strings.Repeat("b", 40)
	review := skillsReceived{Spec: []byte("SKILLS3080_IMPLEMENTATION_REVIEW " + head), Metadata: object{"model": "anthropic/claude-sonnet-5-5", "effort": "max"}, Native: []object{{"model": "claude-sonnet-5-5", "effort": "max"}}, Body: []byte("SKILLS3080_CODE_ACCEPTANCE " + `{"domain":"skills-3080-code-review-v1","head":"` + head + `","accepted":true,"must":0}`)}
	if !skillsCodeReviewAccepted(review, head) {
		t.Fatal("code acceptance positive rejected")
	}
	if skillsCodeReviewAccepted(review, strings.Repeat("c", 40)) {
		t.Fatal("wrong source head accepted")
	}
	review.Body = bytes.Replace(review.Body, []byte(`"must":0`), []byte(`"must":2`), 1)
	if skillsCodeReviewAccepted(review, head) {
		t.Fatal("Must accepted for deployment")
	}
}

func TestSkills3080RuntimeInputRefusesUnacceptedCandidate(t *testing.T) {
	r := skillsRequest{Writer: skillsWriter{Task: 3686}}
	fs := skillsFS{blocked: func(string) bool { t.Fatal("unqualified runtime input reached a data read"); return false }}
	if skillsRuntimeQualification(r, skillsRegistry{}, &fs)["qualified"] != false {
		t.Fatal("empty software proof qualified")
	}
	if _, ok := skillsBuildRevision([]byte("not a reviewed Windows executable")); ok {
		t.Fatal("unreviewed candidate qualified")
	}
	if skillsScopeBytes([]byte(`{}`)) {
		t.Fatal("empty replacement scope accepted")
	}
}

func TestSkills3080RetainsOwnerStopsAndBoundedIdentifiers(t *testing.T) {
	if skillsHeld(skillsTask{}) {
		t.Fatal("empty optional stop fields treated as owner stop")
	}
	for _, mutate := range []func(*skillsTask){
		func(r *skillsTask) { r.OwnerStopped = true }, func(r *skillsTask) { r.Postponed = true }, func(r *skillsTask) { r.StopKind = "owner" }, func(r *skillsTask) { r.OwnerQuestion = object{"waiting": true} }, func(r *skillsTask) { r.DecisionWait = object{"pending": true} }, func(r *skillsTask) { r.WaitReady = object{"condition": "ready"} }, func(r *skillsTask) { r.Scheduler.PendingCancel = object{"job": "original"} }, func(r *skillsTask) { r.Scheduler.OwnerStop = "original" },
	} {
		var row skillsTask
		mutate(&row)
		if !skillsHeld(row) {
			t.Fatal("explicit stop lost")
		}
	}
	for _, model := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol", "anthropic/claude-sonnet-5-5", "p12ab3f/gpt-6-luna"} {
		if !skillsModelIdentifier(model) {
			t.Fatal("known identifier rejected")
		}
	}
	for _, model := range []string{"", "secret-value", "private-account/claude-sonnet-5-5", "anything\n/gpt-6.1-sol", "openai/other/gpt-6.1-sol"} {
		if skillsModelIdentifier(model) {
			t.Fatal("unbounded metadata admitted")
		}
	}
}

func TestSkills3080AlternativeOAuthReviewKeepsActualEffort(t *testing.T) {
	head := strings.Repeat("c", 40)
	r := skillsReceived{Spec: []byte("SKILLS3080_IMPLEMENTATION_REVIEW " + head), Metadata: object{"model": "google-antigravity/gemini-3.1-pro", "effort": "high"}, Native: []object{{"model": "google-antigravity/gemini-3.1-pro", "effort": "high"}}, Body: []byte("SKILLS3080_CODE_ACCEPTANCE " + `{"domain":"skills-3080-code-review-v1","head":"` + head + `","accepted":true,"must":0}`)}
	if !skillsModelIdentifier(str(r.Metadata["model"])) || !skillsCodeReviewAccepted(r, head) {
		t.Fatal("existing OAuth model's highest actual effort rejected")
	}
	r.Metadata["effort"] = "max"
	if skillsCodeReviewAccepted(r, head) {
		t.Fatal("high misreported as max")
	}
	r.Metadata["effort"] = "high"
	r.Native = []object{{"model": "google-antigravity/gemini-3.1-pro", "effort": "low"}}
	if skillsCodeReviewAccepted(r, head) {
		t.Fatal("lower effort accepted")
	}
	if skillsReviewEffort("claude-sonnet-5-5") != "max" || skillsReviewEffort("gpt-6-astra") != "max" {
		t.Fatal("owner max requirement changed")
	}
	if !skillsModelIdentifier("google-antigravity/gemini-3.8-flash") || skillsReviewEffort("gemini-3.8-flash") != "high" {
		t.Fatal("observed alternate catalogue model lost its highest actual setting")
	}
}
