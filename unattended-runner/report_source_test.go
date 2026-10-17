package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

func directSourceForTest(at time.Time, targets ...uint64) OwnerSource {
	text := "PRIVATE_DIRECT_OWNER_RECEIPT"
	sum := sha256.Sum256([]byte(text))
	exists := false
	return OwnerSource{Kind: directOwnerChat, ReceivedAt: at.UTC().Format(time.RFC3339Nano), Provenance: "codex_current_user_message", ReceivedText: text, SHA256: hex.EncodeToString(sum[:]), Targets: targets, Scope: reportDeliveryOnly, DiscordURLExists: &exists}
}
func configureStateRequestForTest(f *progressFixture, todo uint64) {
	f.rows[0]["id"] = todo
	f.rows[0]["status"] = "wait"
	f.rows[0]["wait_on"] = []uint64{2407}
	f.saveRows()
	s := f.source
	f.r.State.Requests[s.ID] = &Request{Thread: s.Channel, Guild: "100", RecordVersion: 2, Message: s.ID, Author: "300", Created: s.Timestamp, Source: "discord-api-v10", Text: safeText(s.Content, f.r.Token), Key: requestKey(s.Channel, s.ID), Todo: todo, Phase: "registered", Reported: "accepted"}
	f.r.Config.ProductReports = []ProductReportConfig{{Todo: todo, Project: "fixture-project", Source: OwnerSource{Kind: stateRequestSource, Message: s.ID}}}
}

func TestDirectOwnerChatReportHasNoFabricatedDiscordOwnerPost(t *testing.T) {
	f := progressTestFixture(t)
	f.rows[0]["id"] = uint64(2626)
	f.saveRows()
	f.r.Config.ProductReports = []ProductReportConfig{{Todo: 2626, Project: "fixture-project", Source: directSourceForTest(f.now, 2626)}}
	f.mu.Lock()
	f.source.Author.ID = "999"
	f.mu.Unlock() // unrelated Discord content is not reused.
	var h Health
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 1 || h.ProductReports != 1 {
		t.Fatal("explicit direct chat could not configure a known finite target", h)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.messages {
		if !strings.Contains(m.Content, "Discordの本人投稿URLはありません") || strings.Contains(m.Content, "PRIVATE_DIRECT") || strings.Contains(m.Content, "/501/") {
			t.Fatal("direct chat was copied or falsely attributed to a Discord post")
		}
	}
	for _, link := range f.r.State.ProgressLinks {
		if link.SourceKind != directOwnerChat || link.SourceThread != "" || link.SourceMessage != "" || link.SourceIdentity == "" {
			t.Fatal("direct source identity was lost or fabricated")
		}
	}
}

func TestDirectChatCannotExpandScopeTargetsOrProvenance(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ProductReportConfig)
	}{
		{"unknown target", func(c *ProductReportConfig) { c.Todo = 999; c.Source.Targets = []uint64{999} }},
		{"request source reused", func(c *ProductReportConfig) { c.Todo = 2705; c.Source.Targets = []uint64{2705} }},
		{"different finite target", func(c *ProductReportConfig) { c.Source.Targets = []uint64{2639} }},
		{"extra target", func(c *ProductReportConfig) { c.Source.Targets = append(c.Source.Targets, 999) }},
		{"execution scope", func(c *ProductReportConfig) { c.Source.Scope = "execute-and-spend" }},
		{"wrong provenance", func(c *ProductReportConfig) { c.Source.Provenance = "discord-api-v10" }},
		{"invented message", func(c *ProductReportConfig) { c.Source.Message = "1560534965228404786" }},
		{"invented thread", func(c *ProductReportConfig) { c.Source.Thread = "501" }},
		{"invented URL", func(c *ProductReportConfig) { yes := true; c.Source.DiscordURLExists = &yes }},
		{"different body", func(c *ProductReportConfig) { c.Source.ReceivedText = "different" }},
		{"old authorization", func(c *ProductReportConfig) { c.Source.ReceivedAt = "2026-10-12T00:00:00Z" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := progressTestFixture(t)
			f.rows[0]["id"] = uint64(2626)
			f.saveRows()
			c := ProductReportConfig{Todo: 2626, Project: "fixture-project", Source: directSourceForTest(f.now, 2626)}
			tt.change(&c)
			f.r.Config.ProductReports = []ProductReportConfig{c}
			var h Health
			f.r.reportProducts(context.Background(), &h)
			if f.sends != 0 || len(f.r.State.ProgressLinks) != 0 {
				t.Fatal("direct chat expanded a target, question approval or execution permission")
			}
		})
	}
}

func TestStateRequest2705ReportsLongWaitAndHeartbeatWithoutInventingProgress(t *testing.T) {
	f := progressTestFixture(t)
	configureStateRequestForTest(f, 2705)
	var h Health
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 1 || f.r.State.ProductReports[2705] == nil || !f.r.isConfiguredRequestReport(2705) {
		t.Fatal("verified intake request was excluded by the finite product list", h)
	}
	first := f.r.State.ProductReports[2705].Signature
	f.r.NoticeAttempted = false
	f.now = f.now.Add(time.Hour - time.Second)
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 1 {
		t.Fatal("unchanged long wait was reported too soon")
	}
	f.now = f.now.Add(time.Second)
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 2 || f.r.State.ProductReports[2705].Signature != first {
		t.Fatal("long-running intake report was silenced or fabricated changed state")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	last := f.messages["801/902"]
	if !strings.Contains(last.Content, "依頼の進捗") || !strings.Contains(last.Content, "前回と同じ状態") || !strings.Contains(last.Content, "/501/"+f.source.ID) {
		t.Fatal("request heartbeat lost its exact original URL or unchanged-state label")
	}
}

func TestStateRequestSourceCannotAuthorizeAnotherTodoOrProject(t *testing.T) {
	for _, kind := range []string{"todo", "project", "owner", "key", "source", "message", "bot"} {
		t.Run(kind, func(t *testing.T) {
			f := progressTestFixture(t)
			configureStateRequestForTest(f, 2705)
			req := f.r.State.Requests[f.source.ID]
			switch kind {
			case "todo":
				f.r.Config.ProductReports[0].Todo = 2626
			case "project":
				f.r.Config.ProductReports[0].Project = "different"
			case "owner":
				req.Author = "999"
			case "key":
				req.Key = "wrong"
			case "source":
				req.Source = "unknown"
			case "message":
				f.r.Config.ProductReports[0].Source.Message = "999"
			case "bot":
				f.mu.Lock()
				f.source.Author.Bot = true
				f.mu.Unlock()
			}
			var h Health
			f.r.reportProducts(context.Background(), &h)
			if f.sends != 0 {
				t.Fatal("a different request, owner or project gained reporting scope")
			}
		})
	}
}

func TestInitialTwoConfiguredReports2705And2626RespectOneSendPerTick(t *testing.T) {
	f := progressTestFixture(t)
	configureStateRequestForTest(f, 2705)
	f.r.Config.ProductReports = append(f.r.Config.ProductReports, ProductReportConfig{Todo: 2626, Project: "cc-pc-tools-rs", Source: directSourceForTest(f.now, 2626)})
	f.rows = append(f.rows, map[string]any{"id": uint64(2626), "project": "cc-pc-tools-rs", "status": "doing", "jobs": []any{"fixture-job"}})
	f.saveRows()
	f.r.tick(context.Background())
	if f.sends != 1 {
		t.Fatal("initial tick duplicated normal and configured request reports")
	}
	f.r.tick(context.Background())
	if f.sends != 2 || f.r.State.ProductReports[2705] == nil || f.r.State.ProductReports[2626] == nil || len(f.r.State.ProgressLinks) != 2 {
		t.Fatal("two exact targets did not progress in separate ticks")
	}
	if f.r.Config.Project != "cc-pc-tools-rs" {
		t.Fatal("reporting expanded execution project")
	}
}

func TestDirectPhosReportStillRequiresOriginalFiniteQualification(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			f := progressTestFixture(t)
			db, at := productCandidateFixture(t)
			proof := candidateRow(db)["product_resume_qualification"].(map[string]any)["receipt"].(productRetryProof)
			f.now = at
			f.proof = proof
			f.rows[0]["id"] = uint64(1626)
			if valid {
				f.rows[0]["product_resume_qualification"] = map[string]any{"receipt": proof}
			}
			f.saveRows()
			f.r.Config.ProductReports = []ProductReportConfig{{Todo: 1626, Project: "fixture-project", Source: directSourceForTest(f.now, 1626)}}
			var h Health
			f.r.reportProducts(context.Background(), &h)
			if (f.sends == 1) != valid {
				t.Fatal("direct chat bypassed original Phos qualification")
			}
		})
	}
}
