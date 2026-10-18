package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"
)

func TestLocalOwnerDiagnosticsNeverAdmitAndDistinguishContextFromSource(t *testing.T) {
	for _, kind := range []string{"reported", "unknown", "context", "source"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "source" {
				if _, e := os.Lstat(liveWorkspace); !os.IsNotExist(e) {
					t.Skip("source-free fixture required")
				}
			}
			p := linuxJobPlan{Source: fixedSourcePin(), RunID: 17, JobID: 29, Attempt: 1, Role: "verify", Name: "fixed", Inputs: map[string]string{}}
			q := jobHookContext{Stage: "complete", Repository: "AutoDevJapan/GoDD-MCP-Alpha", SHA: p.Source.HeadSHA, WorkflowSHA: p.Source.HeadSHA, Ref: "refs/heads/codex/todo2641-http-response-pseudo-20261015", Event: "workflow_dispatch", RunID: "17", Attempt: "1", Job: "verify", Name: "fixed", Inputs: map[string]string{}}
			want := "source_root"
			switch kind {
			case "reported":
				q.Diagnostic = &hookDiagnostic{1, "complete", "event_unavailable"}
				want = "event_unavailable"
			case "unknown":
				q.Diagnostic = &hookDiagnostic{1, "complete", "/private/SENSITIVE"}
				want = ""
			case "context":
				q.SHA = "wrong"
				want = "context_sha"
			}
			var emitted bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			o := localJobOwner{plan: p, started: true, wire: &liveWire{out: json.NewEncoder(&emitted), ctx: ctx}}
			a, b := net.Pipe()
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(2 * time.Second))
			done := make(chan struct{})
			go func() { o.handle(a); close(done) }()
			if e := json.NewEncoder(b).Encode(q); e != nil {
				t.Fatal(e)
			}
			var reply struct{ Accepted bool }
			if e := json.NewDecoder(b).Decode(&reply); e != nil {
				t.Fatal(e)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("owner did not drain")
			}
			if reply.Accepted || o.completed || !o.started || emitted.Len() != 0 {
				t.Fatal("diagnostic/rejection became an admission or bypassed proof")
			}
			if want == "" {
				if o.diagnostic != nil {
					t.Fatal("unknown code recorded")
				}
			} else if o.diagnostic == nil || o.diagnostic.Code != want || o.diagnostic.Stage != "complete" {
				t.Fatal("wrong fixed rejection classification", o.diagnostic)
			}
		})
	}
}

func TestRustProbeSummaryDoesNotSerializeRawOutput(t *testing.T) {
	out := []byte("rustc 1.97.1 (8bab26f4f 2026-07-14)\ncommit-hash: 8bab26f4f68e0e26f0bb7960be334d5b520ea452\ncommit-date: 2026-07-14\n/private/SENSITIVE\n")
	r := summarizeRustProbe("verbose", 1, out, []byte("SENSITIVE stderr"))
	b, e := json.Marshal(r)
	if e != nil || bytes.Contains(b, []byte("SENSITIVE")) || bytes.Contains(b, []byte("/private")) {
		t.Fatal("raw output serialized")
	}
	if r.Exit != 1 || r.Version == "" || r.CommitDate != "2026-07-14" || r.StdoutSHA256 != hash(out) || r.StderrBytes == 0 {
		t.Fatal("bounded metadata lost")
	}
	if summarizeRustProbe("cachekey", 0, []byte("202607148bab"), nil).CacheKey != "202607148bab" {
		t.Fatal("valid cachekey not recognized")
	}
	if summarizeRustProbe("cachekey", 1, []byte("/private/SENSITIVE"), nil).CacheKey != "" {
		t.Fatal("untrusted cachekey admitted")
	}
}
