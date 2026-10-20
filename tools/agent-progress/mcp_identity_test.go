package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestMCPBindingStatusRequiresTwoDistinctRepositoriesAndNeverExposesValues(t *testing.T) {
	old := []string{mcpContractRepository, mcpContractOwner, mcpAlphaRepository, mcpAlphaOwner}
	defer func() {
		mcpContractRepository, mcpContractOwner, mcpAlphaRepository, mcpAlphaOwner = old[0], old[1], old[2], old[3]
	}()
	mcpContractRepository, mcpContractOwner, mcpAlphaRepository, mcpAlphaOwner = "61001", "71001", "61002", "71002"
	if !mcpBindingsConfigured() {
		t.Fatal("complete private inputs were not recognized")
	}
	f := fixtureFor(t, `{"items":[{"id":1,"project":"tools","status":"doing","product_identity":{"repository_id":61001,"owner_id":71001}}]}`)
	s := snap(f)
	data, _ := json.Marshal(s)
	if !s.ProductBindingsConfigured || bytes.Contains(data, []byte("61001")) || bytes.Contains(data, []byte("71001")) {
		t.Fatal("private input status is missing or values escaped")
	}
	f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "null-priority.json")
	writeFixture(t, f.opts.PriorityFile, []byte(`{"product_identities":null}`))
	if s = snap(f); s.Tasks[0].ProductIdentity == nil || s.Tasks[0].ProductIdentity.State != "unknown" {
		t.Fatal("explicit null bindings were rescued by build inputs")
	}
	mcpAlphaRepository = "61001"
	if mcpBindingsConfigured() {
		t.Fatal("duplicate repositories were reported configured")
	}
}

func TestMCPVerifiedJobReclassifiesCodeOnlyTask(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":3200,"project":"tools","status":"doing"}]}`)
	f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
	writeFixture(t, f.opts.PriorityFile, []byte(`{"product_identities":{"godd-mcp-contract":{"repository_id":61001,"owner_id":71001},"godd-mcp-alpha":{"repository_id":61002,"owner_id":71002}}}`))
	f.job(t, "business", `{"id":"business","todo_id":3200,"project":"tools","status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}`)
	s := snap(f)
	if len(s.Tasks) != 1 || s.Tasks[0].Priority != 60 || s.Tasks[0].Department != "GoDD事業部" || s.Tasks[0].ProductIdentity == nil || s.Tasks[0].ProductIdentity.ProductID != "godd-mcp-alpha" || s.Jobs[0].Priority != 60 {
		t.Fatal("verified business target was absorbed into the code repository")
	}
}

func TestMCPProductsHaveDistinctDepartmentsAndPriority(t *testing.T) {
	for _, tc := range []struct {
		project    string
		rank       int
		department string
	}{
		{"GoDD MCP", 10, "受託部"}, {"GoDD-MCP-Alpha", 60, "GoDD事業部"}, {"GoDD　MCP", 10, "受託部"},
	} {
		got := itemLabel(todoItem{Project: tc.project}, priorityPolicy{})
		if got.rank != tc.rank || got.department != tc.department {
			t.Fatal("MCP product classification differs")
		}
	}
}

func TestMCPBusinessIdentityOverridesCodeRepo(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"tools","status":"doing","product_identity":{"repository_id":61001,"owner_id":71001}}]}`)
	f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
	writeFixture(t, f.opts.PriorityFile, []byte(`{"product_identities":{"godd-mcp-contract":{"repository_id":61001,"owner_id":71001},"godd-mcp-alpha":{"repository_id":61002,"owner_id":71002}}}`))
	s := snap(f)
	if len(s.Tasks) != 1 || s.Tasks[0].Priority != 10 || s.Tasks[0].Department != "受託部" {
		t.Fatal("business identity was replaced by the code repository")
	}
}

func TestMCPMissingOwnerRemainsUnknown(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"GoDD-MCP-Alpha","status":"doing","product_identity":{"repository_id":61001}}]}`)
	s := snap(f)
	if len(s.Tasks) != 1 || s.Tasks[0].Priority != 100 {
		t.Fatal("unverified explicit identity was rescued by a name")
	}
}

func TestMCPSecondaryLinksCannotClaimOtherProductsExecution(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"GoDD MCP","status":"doing","jobs":["alpha"]},{"id":2641,"project":"cc-pc-tools-rs","status":"doing"}]}`)
	f.opts.ActiveOnly = true
	f.job(t, "alpha", `{"id":"alpha","todo_id":2641,"status":"running","codex_pid":1,"codex_pid_created":10,"last_event_at":"2026-10-13T10:00:00Z","activity":"thinking"}`)
	birth := uint64(10)
	s := collect(context.Background(), f.opts, testNow, func(uint32) processObservation { return processObservation{alive: true, known: true, created: &birth} })
	if len(s.Tasks) != 1 || s.Tasks[0].ID != 2641 || len(s.Jobs) != 1 || len(s.Jobs[0].TodoIDs) != 1 || s.Jobs[0].TodoIDs[0] != 2641 {
		t.Fatal("another product borrowed processing through a legacy edge")
	}
}

func TestMCPNoPrimaryDoesNotMergeTwoProducts(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"GoDD MCP","status":"doing","jobs":["mixed"]},{"id":2,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["mixed"]}]}`)
	f.opts.ActiveOnly = true
	f.job(t, "mixed", `{"id":"mixed","status":"running","codex_pid":1,"codex_pid_created":10,"last_event_at":"2026-10-13T10:00:00Z","activity":"thinking"}`)
	birth := uint64(10)
	s := collect(context.Background(), f.opts, testNow, func(uint32) processObservation { return processObservation{alive: true, known: true, created: &birth} })
	if len(s.Tasks) != 0 || len(s.Jobs) != 1 || len(s.Jobs[0].TodoIDs) != 0 {
		t.Fatal("ambiguous products were assigned to processing")
	}
}

func TestMCPHistoryRetainsCompatibleReferencesWithoutDuplicatingLiveExecution(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["failed-alpha"]},{"id":2,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["failed-alpha"]}]}`)
	f.job(t, "failed-alpha", `{"id":"failed-alpha","todo_id":1,"status":"failed","finished_at":"2026-10-13T10:00:00Z"}`)
	s := snap(f)
	if len(s.Tasks) != 2 || len(s.Tasks[0].JobKeys) != 1 || len(s.Tasks[1].JobKeys) != 1 || s.Tasks[0].JobKeys[0] != s.Tasks[1].JobKeys[0] {
		t.Fatal("compatible failure history was discarded")
	}
}

func TestMCPDependencyUrgencyDoesNotChangeProductDepartment(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"GoDD MCP","status":"wait","wait_on":[2]},{"id":2,"project":"GoDD-MCP-Alpha","status":"doing"}]}`)
	f.job(t, "alpha", `{"id":"alpha","todo_id":2,"status":"queued"}`)
	s := snap(f)
	var child Task
	for _, task := range s.Tasks {
		if task.ID == 2 {
			child = task
		}
	}
	if child.Priority != 10 || child.Department != "GoDD事業部" || len(s.Jobs) != 1 || s.Jobs[0].Priority != 10 || s.Jobs[0].Department != "GoDD事業部" || s.Jobs[0].PublicLabel == "受託案件" {
		t.Fatal("dependency priority replaced business ownership")
	}
}

func TestMCPBusinessJobInheritsOnlyRealDependencyUrgency(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		for _, dependency := range []string{"wait_on", "work_wait_on", "none"} {
			for _, parentRank := range []int{10, 20, 30, 40, 50, 61, 100} {
				parent := map[string]any{"id": 1, "project": "tools", "status": "wait", "jobs": []string{"alpha"}}
				if dependency != "none" {
					parent[dependency] = []int{7}
				}
				db, _ := json.Marshal(map[string]any{"items": []any{parent, map[string]any{"id": 7, "project": "tools", "status": "doing"}}})
				f := fixtureFor(t, string(db))
				f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
				policy, _ := json.Marshal(map[string]any{"items": map[string]int{"1": parentRank}, "product_identities": map[string]any{"godd-mcp-alpha": map[string]int{"repository_id": 61002, "owner_id": 71002}}})
				writeFixture(t, f.opts.PriorityFile, policy)
				job := map[string]any{"id": "alpha", "todo_id": 7, "project": "GoDD MCP α", "status": "queued"}
				if explicit {
					job["product_identity"] = map[string]int{"repository_id": 61002, "owner_id": 71002}
				}
				data, _ := json.Marshal(job)
				f.job(t, "alpha", string(data))
				got := snap(f)
				expected := 60
				if dependency != "none" {
					expected = min(expected, parentRank)
				}
				if len(got.Jobs) != 1 || got.Jobs[0].Priority != expected || got.Jobs[0].Department != "GoDD事業部" {
					t.Fatalf("explicit=%t dependency=%s parent=%d: jobs=%+v, expected priority=%d and GoDD department", explicit, dependency, parentRank, got.Jobs, expected)
				}
				for _, task := range got.Tasks {
					if explicit && task.ID == 7 && task.Priority != expected {
						t.Fatalf("task and job urgency differ: task=%d job=%d", task.Priority, expected)
					}
				}
			}
		}
	}
}

func TestMCPContractRankBeatsItsLessUrgentPrerequisiteRequester(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"tools","status":"wait","wait_on":[7]},{"id":7,"project":"tools","status":"doing"}]}`)
	f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
	writeFixture(t, f.opts.PriorityFile, []byte(`{"items":{"1":20},"product_identities":{"godd-mcp-contract":{"repository_id":61001,"owner_id":71001}}}`))
	f.job(t, "contract", `{"id":"contract","todo_id":7,"status":"queued","product_identity":{"repository_id":61001,"owner_id":71001}}`)
	s := snap(f)
	if len(s.Jobs) != 1 || s.Jobs[0].Priority != 10 || s.Jobs[0].Department != "受託部" {
		t.Fatal("less urgent dependency requester downgraded the contract job")
	}
	for _, task := range s.Tasks {
		if task.ID == 7 && (task.Priority != 10 || task.Department != "受託部") {
			t.Fatal("less urgent dependency requester downgraded the contract task")
		}
	}
}

func TestMCPRuntimeBindingOverridePresenceIsReportedWithoutValues(t *testing.T) {
	for _, policy := range []string{`{}`, `{"product_identities":{}}`, `{"product_identities":null}`} {
		f := fixtureFor(t, `{"items":[]}`)
		f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
		writeFixture(t, f.opts.PriorityFile, []byte(policy))
		data, err := json.Marshal(snap(f))
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		overridden, known := value["product_bindings_overridden"].(bool)
		if !known || overridden != (policy != `{}`) {
			t.Fatalf("runtime override presence was not reported: %s", data)
		}
	}
}

func TestMCPNoPrimaryPrefersAnUnblockedCurrentTaskOverWaitingHistory(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"GoDD-MCP-Alpha","status":"wait","jobs":["worker"]},{"id":2,"project":"GoDD-MCP-Alpha","status":"doing","jobs":["worker"]}]}`)
	f.opts.ActiveOnly = true
	f.job(t, "worker", `{"id":"worker","status":"running","codex_pid":1,"codex_pid_created":10,"last_event_at":"2026-10-13T10:00:00Z","activity":"thinking"}`)
	birth := uint64(10)
	s := collect(context.Background(), f.opts, testNow, func(uint32) processObservation { return processObservation{alive: true, known: true, created: &birth} })
	if len(s.Tasks) != 1 || s.Tasks[0].ID != 2 || len(s.Jobs) != 1 || len(s.Jobs[0].TodoIDs) != 1 || s.Jobs[0].TodoIDs[0] != 2 {
		t.Fatal("waiting history displaced the observed task")
	}
}

func TestMCPJobParentUrgencyKeepsItsBusinessDepartment(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"project":"GoDD MCP","status":"doing"},{"id":2,"project":"GoDD-MCP-Alpha","status":"doing"}]}`)
	f.job(t, "parent", `{"id":"parent","todo_id":1,"status":"running"}`)
	f.job(t, "child", `{"id":"child","todo_id":2,"status":"queued","parent_job_id":"parent"}`)
	found := false
	for _, job := range snap(f).Jobs {
		if job.Key == jobKey("child") {
			found = true
			if job.Priority != 10 || job.Department != "GoDD事業部" {
				t.Fatal("job-parent urgency replaced business ownership")
			}
		}
	}
	if !found {
		t.Fatal("child job was not observed")
	}
}
