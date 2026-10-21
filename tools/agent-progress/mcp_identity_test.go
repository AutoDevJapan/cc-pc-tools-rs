package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestMCPRelatedReviewCannotReplaceTheCurrentPrimary(t *testing.T) {
	for _, reference := range []string{"related_todo_ids", "jobs"} {
		for _, check := range []string{"task", "dependency", "execution_relation"} {
			t.Run(reference+"/"+check, func(t *testing.T) {
				parent := map[string]any{"id": 1, "project": "tools", "status": "doing", "wait_on": []int{7}}
				if reference == "jobs" {
					parent["jobs"] = []string{"review"}
				}
				db, _ := json.Marshal(map[string]any{"items": []any{parent,
					map[string]any{"id": 7, "project": "tools", "status": "doing"},
					map[string]any{"id": 9, "project": "tools", "status": "doing"}}})
				f := fixtureFor(t, string(db))
				f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
				writeFixture(t, f.opts.PriorityFile, []byte(`{"product_identities":{"godd-mcp-contract":{"repository_id":61001,"owner_id":71001},"godd-mcp-alpha":{"repository_id":61002,"owner_id":71002}}}`))
				f.job(t, "requester", `{"id":"requester","todo_id":1,"status":"running","created_at":"2026-10-20T00:00:00Z","product_identity":{"repository_id":61001,"owner_id":71001}}`)
				review := map[string]any{"id": "review", "todo_id": 9, "status": "queued", "created_at": "2026-10-20T01:00:00Z"}
				if reference == "related_todo_ids" {
					review["related_todo_ids"] = []int{1}
				}
				encoded, _ := json.Marshal(review)
				f.job(t, "review", string(encoded))
				f.job(t, "prerequisite", `{"id":"prerequisite","todo_id":7,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}`)
				got := snap(f)
				if check == "task" || check == "execution_relation" {
					for _, task := range got.Tasks {
						if task.ID == 1 {
							if check == "execution_relation" {
								if len(task.JobKeys) != 1 || task.JobKeys[0] != jobKey("requester") {
									t.Fatalf("secondary review claimed current execution: %v", task.JobKeys)
								}
								return
							}
							if task.Priority != 10 || task.Department != "受託部" || task.ProductIdentity == nil || task.ProductIdentity.ProductID != "godd-mcp-contract" {
								t.Fatalf("current primary was replaced: %+v", task)
							}
							return
						}
					}
				} else {
					for _, job := range got.Jobs {
						if job.rawID == "prerequisite" {
							if job.Priority != 10 {
								t.Fatalf("prerequisite priority=%d want=10", job.Priority)
							}
							return
						}
					}
				}
				t.Fatal("required row absent")
			})
		}
	}
}

func TestMCPLiveRequesterProductPriorityFlowsThroughDependencies(t *testing.T) {
	for _, alpha := range []bool{false, true} {
		for _, edge := range []string{"wait_on", "work_wait_on"} {
			for _, chain := range []bool{false, true} {
				parent := map[string]any{"id": 1, "project": "tools", "status": "doing"}
				target := 7
				if chain {
					target = 5
				}
				parent[edge] = []int{target}
				db, _ := json.Marshal(map[string]any{"items": []any{parent,
					map[string]any{"id": 5, "project": "tools", "status": "doing", "work_wait_on": []int{7}},
					map[string]any{"id": 7, "project": "tools", "status": "doing"}}})
				f := fixtureFor(t, string(db))
				f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
				writeFixture(t, f.opts.PriorityFile, []byte(`{"product_identities":{"godd-mcp-contract":{"repository_id":61001,"owner_id":71001},"godd-mcp-alpha":{"repository_id":61002,"owner_id":71002}}}`))
				repository, owner, want := 61001, 71001, 10
				if alpha {
					repository, owner, want = 61002, 71002, 60
				}
				f.job(t, "requester", fmt.Sprintf(`{"id":"requester","todo_id":1,"status":"queued","product_identity":{"repository_id":%d,"owner_id":%d}}`, repository, owner))
				f.job(t, "middle", `{"id":"middle","todo_id":5,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}`)
				f.job(t, "prerequisite", `{"id":"prerequisite","todo_id":7,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}`)
				s := snap(f)
				foundTask := false
				for _, task := range s.Tasks {
					if task.ID == 7 {
						foundTask = true
						if task.Priority != want || task.Department != "GoDD事業部" {
							t.Fatalf("alpha=%v/edge=%s/chain=%v: task priority=%d want=%d", alpha, edge, chain, task.Priority, want)
						}
					}
				}
				if !foundTask {
					t.Fatal("prerequisite task absent")
				}
				found := false
				for _, job := range s.Jobs {
					if job.rawID == "prerequisite" {
						found = true
						if job.Priority != want {
							t.Fatalf("job priority=%d want=%d", job.Priority, want)
						}
					}
				}
				if !found {
					t.Fatal("prerequisite job absent")
				}
			}
		}
	}
}

func TestMCPImplicitReviewPrimaryCannotOccupyAnotherProduct(t *testing.T) {
	for _, choice := range []string{"scheduler", "eligible", "fallback"} {
		t.Run(choice, func(t *testing.T) {
			requestID := int64(1)
			parentStatus := "wait"
			reviewStatus := "doing"
			if choice == "scheduler" {
				parentStatus = "doing"
			}
			if choice == "fallback" {
				requestID, reviewStatus = 11, "wait"
			}
			parent := map[string]any{"id": requestID, "project": "tools", "status": parentStatus, "wait_on": []int{7}}
			reviewTask := map[string]any{"id": 9, "project": "tools", "status": reviewStatus}
			if choice == "scheduler" {
				reviewTask["scheduler"] = map[string]any{"job": "review"}
			}
			db, _ := json.Marshal(map[string]any{"items": []any{parent, reviewTask,
				map[string]any{"id": 7, "project": "tools", "status": "doing"}}})
			f := fixtureFor(t, string(db))
			f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
			writeFixture(t, f.opts.PriorityFile, []byte(`{"product_identities":{"godd-mcp-contract":{"repository_id":61001,"owner_id":71001},"godd-mcp-alpha":{"repository_id":61002,"owner_id":71002}}}`))
			f.job(t, "requester", fmt.Sprintf(`{"id":"requester","todo_id":%d,"status":"running","created_at":"2026-10-20T00:00:00Z","product_identity":{"repository_id":61001,"owner_id":71001}}`, requestID))
			f.job(t, "review", fmt.Sprintf(`{"id":"review","related_todo_ids":[%d,9],"status":"queued","created_at":"2026-10-20T01:00:00Z"}`, requestID))
			f.job(t, "prerequisite", `{"id":"prerequisite","todo_id":7,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}`)
			got, foundTask, foundJob := snap(f), false, false
			for _, task := range got.Tasks {
				if task.ID == requestID {
					foundTask = true
					if task.Priority != 10 || task.ProductIdentity == nil || task.ProductIdentity.ProductID != "godd-mcp-contract" || len(task.JobKeys) != 1 || task.JobKeys[0] != jobKey("requester") {
						t.Fatalf("other primary replaced the requester: %+v", task)
					}
				}
			}
			for _, job := range got.Jobs {
				if job.rawID == "prerequisite" {
					foundJob = true
					if job.Priority != 10 {
						t.Fatalf("prerequisite priority=%d want=10", job.Priority)
					}
				}
			}
			if !foundTask || !foundJob {
				t.Fatal("required task or job absent")
			}
		})
	}
}

func TestMCPHistoryOnlyProductJobKeepsItsProductPriority(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":7,"project":"tools","status":"wait","jobs":["alpha"]}]}`)
	f.job(t, "alpha", `{"id":"alpha","related_todo_ids":[7],"status":"running","project":"GoDD MCP α"}`)
	s := snap(f)
	if len(s.Jobs) != 1 || s.Jobs[0].Priority != 60 {
		t.Fatalf("history-only priority differs: %+v", s.Jobs)
	}
}

func TestMCPLiveRequesterDependencyExclusionControls(t *testing.T) {
	for _, control := range []string{"parent_done", "parent_block", "wait_ready", "child_done", "no_product_job", "terminal_requester"} {
		t.Run(control, func(t *testing.T) {
			parent := map[string]any{"id": 1, "project": "tools", "status": "doing", "wait_on": []int{7}}
			child := map[string]any{"id": 7, "project": "tools", "status": "doing"}
			switch control {
			case "parent_done":
				parent["status"] = "done"
			case "parent_block":
				parent["status"] = "block"
			case "wait_ready":
				parent["wait_ready"] = map[string]any{"condition": "synthetic"}
			case "child_done":
				child["status"] = "done"
			}
			db, _ := json.Marshal(map[string]any{"items": []any{parent, child}})
			f := fixtureFor(t, string(db))
			f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "priority.json")
			writeFixture(t, f.opts.PriorityFile, []byte(`{"product_identities":{"godd-mcp-contract":{"repository_id":61001,"owner_id":71001},"godd-mcp-alpha":{"repository_id":61002,"owner_id":71002}}}`))
			requester := `{"id":"requester","todo_id":1,"status":"queued","product_identity":{"repository_id":61001,"owner_id":71001}}`
			want := 60
			if control == "no_product_job" {
				requester = `{"id":"requester","todo_id":1,"status":"queued"}`
				want = 30
			}
			if control == "terminal_requester" {
				requester = `{"id":"requester","todo_id":1,"status":"failed","product_identity":{"repository_id":61001,"owner_id":71001}}`
				want = 30
			}
			f.job(t, "requester", requester)
			f.job(t, "prerequisite", `{"id":"prerequisite","todo_id":7,"status":"running","product_identity":{"repository_id":61002,"owner_id":71002}}`)
			found := false
			for _, job := range snap(f).Jobs {
				if job.rawID == "prerequisite" {
					found = true
					if job.Priority != want {
						t.Fatalf("priority=%d want=%d", job.Priority, want)
					}
				}
			}
			if !found {
				t.Fatal("prerequisite absent")
			}
		})
	}
}

func TestMCPMissingPolicyReportsNoBindingOverride(t *testing.T) {
	f := fixtureFor(t, `{"items":[]}`)
	f.opts.PriorityFile = filepath.Join(filepath.Dir(f.opts.TodoFile), "missing-policy.json")
	value := snap(f).ProductBindingsOverridden
	if value == nil || *value {
		t.Fatal("missing policy must report no override")
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
