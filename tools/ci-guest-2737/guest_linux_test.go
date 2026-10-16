package main

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "fixture-parent":
			os.Exit(fixtureParent())
		case "fixture-child":
			fixtureChild()
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestLinuxRealProcessFixtureNormalAndCancel(t *testing.T) {
	g := guest{results: map[string]fixtureResult{}}
	for _, cancel := range []bool{false, true} {
		id := strings.Repeat("a", 32)
		kind := "normal"
		if cancel {
			id = strings.Repeat("b", 32)
			kind = "cancel"
		}
		host, server := net.Pipe()
		_ = host.SetDeadline(time.Now().Add(20 * time.Second))
		done := make(chan struct{})
		go func() { g.serve(server); close(done) }()
		if e := json.NewEncoder(host).Encode(request{kind, id}); e != nil {
			t.Fatal(e)
		}
		var observed fixtureResult
		if e := json.NewDecoder(host).Decode(&observed); e != nil {
			t.Fatal(e)
		}
		if !observed.Observed || observed.Child == observed.Parent || observed.Child <= 1 || observed.Group != observed.Parent {
			t.Fatal("fixture child not independently observed")
		}
		if !cancel {
			if e := json.NewEncoder(host).Encode(request{"continue", id}); e != nil {
				t.Fatal(e)
			}
		} else {
			_ = host.Close()
		}
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = host.Close()
			t.Fatal("fixture cleanup deadline")
		}
		_ = host.Close()
		g.mu.Lock()
		r := g.results[id]
		g.mu.Unlock()
		if !r.Done || !r.Gone || !groupGone(r.Group) || r.Cancelled != cancel || r.Failure != "" {
			t.Fatalf("owned process group cleanup not proven: %+v", r)
		}
	}
}
