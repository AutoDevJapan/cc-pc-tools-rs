//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestOriginalEventSnapshotAllowsItsExistingAppenderAndRejectsAliases(t *testing.T) {
	l := fixture(t)
	p := filepath.Join(filepath.Dir(l.Target), "events.jsonl")
	if e := os.WriteFile(p, []byte("original complete event\n"), 0600); e != nil {
		t.Fatal(e)
	}
	writer, e := openPin(p, syscall.GENERIC_WRITE, 3, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer writer.close()
	b, e := readProofEvents(p)
	if e != nil || string(b) != "original complete event\n" {
		t.Fatalf("original append stream rejected: %v", e)
	}
	writer.close()
	if e = os.Link(p, p+".external"); e != nil {
		t.Fatal(e)
	}
	if _, e = readProofEvents(p); e == nil {
		t.Fatal("linked event stream accepted")
	}
}

func TestSoftwareRenameRetriesOnlyFiniteSharingViolations(t *testing.T) {
	for _, kind := range []string{"transient-sharing", "persistent-sharing", "collision", "permission"} {
		t.Run(kind, func(t *testing.T) {
			calls, pauses := 0, 0
			e := retrySharing(func() error {
				calls++
				switch kind {
				case "transient-sharing":
					if calls < 3 {
						return syscall.Errno(32)
					}
					return nil
				case "persistent-sharing":
					return syscall.Errno(32)
				case "collision":
					return syscall.Errno(183)
				default:
					return syscall.Errno(5)
				}
			}, func() { pauses++ })
			wantCalls := map[string]int{"transient-sharing": 3, "persistent-sharing": 4, "collision": 1, "permission": 1}[kind]
			if calls != wantCalls || pauses != wantCalls-1 || (e == nil) != (kind == "transient-sharing") {
				t.Fatalf("wrong finite retries %d/%d %v", calls, pauses, e)
			}
			if kind == "permission" && !errors.Is(e, syscall.Errno(5)) {
				t.Fatal("permission failure was hidden")
			}
		})
	}
}

func TestNativeRefreshKeepsBothPreviousGenerations(t *testing.T) {
	l := fixture(t)
	for _, suffix := range []string{".before-todo3757", ".stage-other-owner"} {
		if e := os.WriteFile(l.Target+suffix, []byte("prior-generation"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	p, e := prepareAt(l, "todo3757-native-proof")
	if e != nil {
		t.Fatal(e)
	}
	defer p.close()
	if e = p.install(hooks{}); e != nil {
		t.Fatal(e)
	}
	content(t, l.Target, "new verified bytes")
	content(t, l.Target+".before-todo3757-native-proof", "old")
	content(t, l.Target+".before-todo3757", "prior-generation")
	content(t, l.Target+".stage-other-owner", "prior-generation")
}

func TestNativeRefreshOriginalEventPinRejectsChangedOrPartialEvidence(t *testing.T) {
	for _, kind := range []string{"original", "missing", "duplicate", "changed-line", "changed-output", "failed", "null-exit", "partial-install", "wrong-hash", "duplicate-kind", "wrong-head"} {
		t.Run(kind, func(t *testing.T) {
			results := []map[string]any{}
			for _, l := range leaves {
				results = append(results, map[string]any{"kind": l.Kind, "old_sha256": l.Old, "new_sha256": l.New, "updated": true, "verified": true})
			}
			head := sourceHead
			if kind == "partial-install" {
				results[1]["verified"] = false
			}
			if kind == "wrong-hash" {
				results[0]["new_sha256"] = strings.Repeat("0", 64)
			}
			if kind == "duplicate-kind" {
				results[1] = results[0]
			}
			if kind == "wrong-head" {
				head = nativeSourceHead
			}
			out, _ := json.Marshal(map[string]any{"domain": "owned-cleanup-software-3757", "source_head": head, "qualified": true, "results": results})
			output := "request=fixture\nexit=0\n--- stdout ---\n" + string(out) + "\n--- stderr ---\n"
			var code any = 0
			status := "completed"
			if kind == "failed" {
				code, status = 1, "failed"
			}
			if kind == "null-exit" {
				code = nil
			}
			line, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"id": "item_fixture", "type": "command_execution", "status": status, "exit_code": code, "aggregated_output": output}})
			pin := installProofPin{sha(line), sha([]byte(output)), "item_fixture"}
			events := append(append([]byte{}, line...), '\n')
			switch kind {
			case "missing":
				events = nil
			case "duplicate":
				events = append(events, events...)
			case "changed-line":
				events = append([]byte(" "), events...)
			case "changed-output":
				pin.OutputSHA = strings.Repeat("0", 64)
			}
			e := nativePriorInstall(events, pin)
			if (e == nil) != (kind == "original") {
				t.Fatalf("wrong original install qualification: %s %v", kind, e)
			}
		})
	}
}
