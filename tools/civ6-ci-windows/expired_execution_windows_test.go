//go:build windows && amd64

package main

import (
	contract "cc/civ6-ci-contract"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExpiredGateChildEndsBeforeAuditOrSCM(t *testing.T) {
	p := fixturePlan()
	p.Root = t.TempDir()
	p.ExecuteBefore = time.Now().Add(-time.Second).UnixNano()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old; w.Close() }()
	err := executeAdmitted(p)
	os.Stdout = old
	w.Close()
	b, e := io.ReadAll(r)
	if e != nil {
		t.Fatal(e)
	}
	if err == nil || !strings.Contains(err.Error(), "execution budget") {
		t.Fatalf("expiry was not checked first: %v", err)
	}
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) != nil {
			t.Fatal("invalid event")
		}
		events = append(events, row)
	}
	if len(events) != 2 || events[0]["event"] != "execute-begin" || events[1]["event"] != "execute-end" || events[1]["service_started"] != false || events[1]["fence"] != false || events[1]["retryable_prestart"] != true {
		t.Fatalf("%s", b)
	}
	files, e := os.ReadDir(p.Root)
	if e != nil || len(files) != 0 {
		t.Fatal("expired execution touched the namespace")
	}
}

func TestPrestartRetryClassificationIsCauseBounded(t *testing.T) {
	for _, err := range []error{errPrestartBudget, context.DeadlineExceeded, contract.ErrRegistrationQueueBusy, fmt.Errorf("audit: %w", errGitHubReadTransport)} {
		if !retryablePrestart(err) {
			t.Fatal("temporary cause refused")
		}
	}
	for _, err := range []error{nil, errors.New("registration credential unavailable"), errors.New("registration credential shape rejected"), errors.New("audit rejected"), &apiReadError{errors.New("HTTP 403")}, context.Canceled} {
		if retryablePrestart(err) {
			t.Fatal("deterministic failure retried")
		}
	}
}
