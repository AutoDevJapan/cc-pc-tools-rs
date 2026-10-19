package contract

import "testing"

func TestNativeRunnerCannotMatchAnotherQueuedJob(t *testing.T) {
	p, _ := nativeFixture(t)
	variants := map[string]Plan{}
	q := p
	q.RunID++
	variants["other workflow run"] = q
	q = p
	q.Attempt++
	variants["other attempt"] = q
	q = p
	q.JobName = "test (windows-latest)"
	variants["other matrix OS"] = q
	q = p
	q.Workflow, q.Job, q.JobName = ".github/workflows/native-driver.yml", "native-driver", "native-driver"
	variants["native driver"] = q
	q = p
	q.Workflow, q.Job, q.JobName = ".github/workflows/live-chain.yml", "live-chain", "live-chain"
	variants["live chain"] = q
	for name, other := range variants {
		supplier := map[string]bool{}
		for _, label := range Labels(p) {
			supplier[label] = true
		}
		matches := true
		for _, required := range Labels(other) {
			matches = matches && supplier[required]
		}
		if matches || JobLabel(p) == JobLabel(other) {
			t.Errorf("runner may consume %s", name)
		}
	}
	// Also cover two Windows jobs; an OS mismatch must not make this pass.
	if JobLabel(variants["native driver"]) == JobLabel(variants["live chain"]) {
		t.Fatal("native and live-chain jobs share their selecting label")
	}
}
