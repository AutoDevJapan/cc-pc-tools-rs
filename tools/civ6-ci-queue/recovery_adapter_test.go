package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRecoveryRequiresFixedHelperCollectionIdentity(t *testing.T) {
	for _, kind := range []string{"valid", "exit", "error", "instance", "run", "job", "attempt", "incomplete", "queryable", "missing-census", "unreadable", "scm", "root"} {
		t.Run(kind, func(t *testing.T) {
			h, key, _ := recoveryHarness(t)
			e := h.ledger().Entries[key]
			h.d.Cfg.Helpers = map[string]HelperConfig{"Windows": {Exe: Pinned{Path: "fixed-helper"}, ControllerSID: "fixed-controller"}}
			h.d.Exec = func(ctx context.Context, p Pinned, args []string, in []byte, onStart func(ProcID)) (StepResult, error) {
				if p.Path != "fixed-helper" || len(args) != 2 || args[0] != "verify-collected" || in != nil || onStart != nil {
					t.Fatal("recovery executed an unexpected effect")
				}
				v := fmt.Sprintf(`{"event":"collection-verified","complete":true,"root_retired":true,"scm_stopped":true,"queryable_account_process_count":0,"unreadable_process_count":3,"instance":%q,"run_id":801,"job_id":8011,"attempt":1}`, e.Cleanup.Instance)
				s := StepResult{Output: []byte(v)}
				switch kind {
				case "exit":
					s.Exit = 1
				case "error":
					return s, errors.New("deadline")
				case "instance":
					s.Output = []byte(strings.ReplaceAll(v, e.Cleanup.Instance, "different-instance"))
				case "run":
					s.Output = []byte(strings.ReplaceAll(v, `"run_id":801`, `"run_id":802`))
				case "job":
					s.Output = []byte(strings.ReplaceAll(v, `"job_id":8011`, `"job_id":8021`))
				case "attempt":
					s.Output = []byte(strings.ReplaceAll(v, `"attempt":1`, `"attempt":2`))
				case "incomplete":
					s.Output = []byte(strings.ReplaceAll(v, `"complete":true`, `"complete":false`))
				case "queryable":
					s.Output = []byte(strings.ReplaceAll(v, `"queryable_account_process_count":0`, `"queryable_account_process_count":1`))
				case "missing-census":
					s.Output = []byte(strings.ReplaceAll(v, `"unreadable_process_count":3,`, ``))
				case "unreadable":
					s.Output = []byte(strings.ReplaceAll(v, `"unreadable_process_count":3`, `"unreadable_process_count":-1`))
				case "scm":
					s.Output = []byte(strings.ReplaceAll(v, `"scm_stopped":true`, `"scm_stopped":false`))
				case "root":
					s.Output = []byte(strings.ReplaceAll(v, `"root_retired":true`, `"root_retired":false`))
				}
				return s, nil
			}
			observation, err := h.d.verifyRetiredWindows(context.Background(), e, t.TempDir())
			if (err == nil) != (kind == "valid") {
				t.Fatal("collection proof accepted incorrectly", err)
			}
			if err == nil && (observation.Queryable != 0 || observation.Unreadable != 3) {
				t.Fatal("census scope lost", observation)
			}
		})
	}
}
