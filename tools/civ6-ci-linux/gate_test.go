package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

func fixturePolicy() execPolicy {
	p := execPolicy{TimeoutMinutes: 1}
	for i := range p.Present {
		p.Present[i] = true
		p.Steps[i] = stepPolicy{Dir: workspace}
	}
	return p
}

func TestTranslateGate(t *testing.T) {
	p := gatePacket{Policy: fixturePolicy(), Index: cmdTest, Target: "D:\\owned\\cargo-target"}
	for k, v := range map[string]string{"CARGO_BUILD_JOBS": "1", "RUST_TEST_THREADS": "1", "CODEX_JOBS_CARGO_LOCK_HELD": "1", "CODEX_JOBS_CARGO_TARGET_HELD": "fixed-key"} {
		t.Setenv(k, v)
	}
	args := append(append([]string{}, cargoTable[cmdTest]...), "--target-dir", p.Target, "--jobs", "1")
	if jobs, tests, e := translateGate(args, p); e != nil || jobs != 1 || tests != 1 {
		t.Fatalf("gate additions mishandled: %v", e)
	}
	for _, extra := range [][]string{{"--target-dir", "D:\\other"}, {"--config", "evil"}, {"-j", "10"}} {
		if _, _, e := translateGate(append(append([]string{}, args...), extra...), p); e == nil {
			t.Fatalf("caller override accepted: %v", extra)
		}
	}
	for _, key := range []string{"CARGO_BUILD_JOBS", "RUST_TEST_THREADS"} {
		t.Setenv(key, "2")
		if _, _, e := translateGate(args, p); e == nil {
			t.Fatal("namespace cap exceeded")
		}
		t.Setenv(key, "1")
	}
	t.Setenv("CODEX_JOBS_CARGO_LOCK_HELD", "")
	if _, _, e := translateGate(args, p); e == nil {
		t.Fatal("ungated heavy command accepted")
	}
	f := gatePacket{Policy: fixturePolicy(), Index: cmdFmt, Target: p.Target}
	if _, _, e := translateGate(cargoTable[cmdFmt], f); e != nil {
		t.Fatal(e)
	}
	if _, _, e := translateGate(append(append([]string{}, cargoTable[cmdFmt]...), "x"), f); e == nil {
		t.Fatal("changed format argv accepted")
	}
	r := gatePacket{Policy: fixturePolicy(), Index: cmdRustup}
	if _, _, e := translateGate(rustupArgs, r); e != nil {
		t.Fatal(e)
	}
	if _, _, e := translateGate(append(append([]string{}, rustupArgs...), "--default-toolchain", "nightly"), r); e == nil {
		t.Fatal("changed rustup argv accepted")
	}
	absent := gatePacket{Policy: execPolicy{TimeoutMinutes: 1}, Index: cmdTest, Target: p.Target}
	if _, _, e := translateGate(args, absent); e == nil {
		t.Fatal("command absent from the reviewed workflow accepted")
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func post(h *host, path string, q callbackRequest, nonce string, raw ...string) *httptest.ResponseRecorder {
	body := mustJSON(q)
	if len(raw) > 0 {
		body = []byte(raw[0])
	}
	r := httptest.NewRequest("POST", path, bytes.NewReader(body))
	r.Header.Set("X-Civ6-Nonce", nonce)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHostCallbackRejectsIdentityAndReplay(t *testing.T) {
	p := rustPlan()
	calls := 0
	h := &host{plan: p, kind: KindRust, policy: fixturePolicy(), key: rep("a", 64), started: true, recheck: func(context.Context) error { return nil },
		execute: func(context.Context, int) (cargoResult, error) {
			calls++
			return cargoResult{Exit: 9, Drained: true}, nil
		}}
	good := callbackRequest{p, h.key, cargoTable[cmdTest]}
	mutate := map[string]func(*callbackRequest){
		"run":     func(q *callbackRequest) { q.Plan.RunID++ },
		"attempt": func(q *callbackRequest) { q.Plan.Attempt++ },
		"job":     func(q *callbackRequest) { q.Plan.JobID++ },
		"head":    func(q *callbackRequest) { q.Plan.Head = rep("9", 40) },
		"nonce":   func(q *callbackRequest) { q.Nonce = rep("b", 64) },
		"args":    func(q *callbackRequest) { q.Args = append(append([]string{}, q.Args...), "--config", "evil") },
		"empty":   func(q *callbackRequest) { q.Args = nil },
	}
	for name, m := range mutate {
		q := good
		m(&q)
		if w := post(h, "/civ6/cargo", q, h.key); w.Code != 403 {
			t.Fatalf("%s accepted: %d", name, w.Code)
		}
	}
	extra := string(mustJSON(good))
	extra = extra[:len(extra)-1] + ",\"HostCommand\":\"evil\"}"
	if w := post(h, "/civ6/cargo", good, h.key, extra); w.Code != 403 {
		t.Fatal("unknown host field accepted")
	}
	if w := post(h, "/civ6/cargo", good, rep("c", 64)); w.Code != 403 {
		t.Fatal("wrong header nonce accepted")
	}
	if w := post(h, "/civ6/unknown", good, h.key); w.Code != 403 {
		t.Fatal("unknown route accepted")
	}
	if calls != 0 {
		t.Fatalf("rejected requests invoked the gate %d times", calls)
	}
	w := post(h, "/civ6/cargo", good, h.key)
	var result cargoResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Exit != 9 {
		t.Fatalf("fixed request: %d %s", w.Code, w.Body)
	}
	if w = post(h, "/civ6/cargo", good, h.key); w.Code != 409 || calls != 1 {
		t.Fatal("replay accepted")
	}
	rustup := callbackRequest{p, h.key, rustupArgs}
	if w = post(h, "/civ6/rustup", rustup, h.key); w.Code != 200 {
		t.Fatalf("rustup: %d", w.Code)
	}
	if w = post(h, "/civ6/cargo", callbackRequest{p, h.key, rustupArgs}, h.key); w.Code != 403 {
		t.Fatal("rustup argv accepted on the cargo route")
	}
}

func TestHostCodeQLHasNoCargoRoute(t *testing.T) {
	p := codeQLPlan("Analyze (actions)")
	h := &host{plan: p, kind: KindCodeQL, key: rep("a", 64), recheck: func(context.Context) error { return nil },
		execute: func(context.Context, int) (cargoResult, error) {
			t.Fatal("CodeQL reached Cargo")
			return cargoResult{}, nil
		}}
	for _, path := range []string{"/civ6/cargo", "/civ6/rustup"} {
		if w := post(h, path, callbackRequest{p, h.key, cargoTable[cmdTest]}, h.key); w.Code != 403 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	if w := post(h, "/civ6/start", callbackRequest{p, h.key, nil}, h.key); w.Code != 204 {
		t.Fatalf("start: %d", w.Code)
	}
	if w := post(h, "/civ6/start", callbackRequest{p, h.key, nil}, h.key); w.Code != 409 {
		t.Fatal("duplicate start accepted")
	}
	if w := post(h, "/civ6/complete", callbackRequest{p, h.key, nil}, h.key); w.Code != 204 {
		t.Fatalf("complete: %d", w.Code)
	}
	if w := post(h, "/civ6/complete", callbackRequest{p, h.key, nil}, h.key); w.Code != 409 {
		t.Fatal("duplicate complete accepted")
	}
}

func TestHostHooksRequireLiveAssignment(t *testing.T) {
	p := rustPlan()
	bad := true
	h := &host{plan: p, kind: KindRust, policy: fixturePolicy(), key: rep("a", 64), recheck: func(context.Context) error {
		if bad {
			return errors.New("not assigned")
		}
		return nil
	}}
	if w := post(h, "/civ6/complete", callbackRequest{p, h.key, nil}, h.key); w.Code != 403 {
		t.Fatalf("complete before start/assignment: %d", w.Code)
	}
	bad = false
	if w := post(h, "/civ6/complete", callbackRequest{p, h.key, nil}, h.key); w.Code != 409 {
		t.Fatalf("complete before start: %d", w.Code)
	}
	if w := post(h, "/civ6/cargo", callbackRequest{p, h.key, cargoTable[cmdTest]}, h.key); w.Code != 409 {
		t.Fatalf("Cargo before the start hook: %d", w.Code)
	}
	if w := post(h, "/civ6/start", callbackRequest{p, h.key, []string{"x"}}, h.key); w.Code != 403 {
		t.Fatal("hook arguments accepted")
	}
	bad = true
	if w := post(h, "/civ6/start", callbackRequest{p, h.key, nil}, h.key); w.Code != 403 {
		t.Fatal("unassigned start accepted")
	}
}

func TestAwaitAssignmentIsFinite(t *testing.T) {
	n := 0
	e := awaitAssignment(context.Background(), func(context.Context) error { n++; return errors.New("pending") }, func(context.Context) error { return nil })
	if e == nil || n != 5 {
		t.Fatalf("attempts=%d err=%v", n, e)
	}
	n = 0
	e = awaitAssignment(context.Background(), func(context.Context) error {
		n++
		if n < 3 {
			return errors.New("pending")
		}
		return nil
	}, func(context.Context) error { return nil })
	if e != nil || n != 3 {
		t.Fatalf("attempts=%d err=%v", n, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = awaitAssignment(ctx, func(context.Context) error { return nil }, pauseOneSecond); e == nil {
		t.Fatal("cancelled context ignored")
	}
}
