package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRecoveryRequiresFixedHelperCollectionIdentity(t *testing.T) {
	for _, kind := range []string{"valid", "exit", "error", "instance", "run", "job", "attempt", "incomplete"} {
		t.Run(kind, func(t *testing.T) {
			h, key, _ := recoveryHarness(t)
			e := h.ledger().Entries[key]
			h.d.Cfg.Helpers = map[string]HelperConfig{"Windows": {Exe: Pinned{Path: "fixed-helper"}, ControllerSID: "fixed-controller"}}
			h.d.Exec = func(ctx context.Context, p Pinned, args []string, in []byte, onStart func(ProcID)) (StepResult, error) {
				if p.Path != "fixed-helper" || len(args) != 2 || args[0] != "verify-collected" || in != nil || onStart != nil {
					t.Fatal("recovery executed an unexpected effect")
				}
				v := fmt.Sprintf(`{"event":"collection-verified","complete":true,"root_retired":true,"shared_service_idle":true,"instance":%q,"run_id":801,"job_id":8011,"attempt":1}`, e.Cleanup.Instance)
				s := StepResult{Output: []byte(v)}
				switch kind {
				case "exit":
					s.Exit = 1
				case "error":
					return s, errors.New("deadline")
				case "instance":
					s.Output = []byte(`{"event":"collection-verified","complete":true}`)
				case "run":
					s.Output = []byte(strings.ReplaceAll(v, `"run_id":801`, `"run_id":802`))
				case "job":
					s.Output = []byte(strings.ReplaceAll(v, `"job_id":8011`, `"job_id":8021`))
				case "attempt":
					s.Output = []byte(strings.ReplaceAll(v, `"attempt":1`, `"attempt":2`))
				case "incomplete":
					s.Output = []byte(strings.ReplaceAll(v, `"complete":true`, `"complete":false`))
				}
				return s, nil
			}
			err := h.d.verifyRetiredWindows(context.Background(), e, t.TempDir())
			if (err == nil) != (kind == "valid") {
				t.Fatal("collection proof accepted incorrectly", err)
			}
		})
	}
}
