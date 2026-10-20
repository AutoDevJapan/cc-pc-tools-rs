//go:build windows && amd64

package main

import (
	"encoding/json"
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
	if len(events) != 2 || events[0]["event"] != "execute-begin" || events[1]["event"] != "execute-end" || events[1]["service_started"] != false || events[1]["fence"] != false {
		t.Fatalf("%s", b)
	}
	files, e := os.ReadDir(p.Root)
	if e != nil || len(files) != 0 {
		t.Fatal("expired execution touched the namespace")
	}
}
