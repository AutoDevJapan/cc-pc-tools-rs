package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func oldBotQuestionReportFixture(t *testing.T) (*progressFixture, *Request) {
	t.Helper()
	f := progressTestFixture(t)
	f.source.Channel = "601"
	f.source.Content = "a"
	f.threads["601"] = Thread{ID: "601", Type: 11, Guild: "100", Parent: "200", Owner: "400"}
	f.messages["601/"+f.source.ID] = f.source
	configureStateRequestForTest(f, 2705)
	req := f.r.State.Requests[f.source.ID]
	req.RecordVersion, req.Guild = 0, ""
	f.rows[0]["stop_kind"] = "person"
	f.rows[0]["resume_generation"] = 7
	f.saveRows()
	return f, req
}

func TestProgressLinkDiscoveryDoesNotMigrateOrReclassifyOriginalReceipts(t *testing.T) {
	for _, known := range []bool{false, true} {
		f, req := oldBotQuestionReportFixture(t)
		f.r.Config.Project = "fixture-project"
		req.ProgressThread = "801"
		if known {
			f.r.KnownQuestionThreads = map[string]bool{"601": true}
		}
		created := f.now.UTC().Format(time.RFC3339Nano)
		message := Message{ID: "901", Channel: "801", Timestamp: created, Content: "fixed progress", Flags: 4096}
		message.Author.ID, message.Author.Bot = "400", true
		f.messages["801/901"] = message
		f.threads["801"] = Thread{ID: "801", Type: 11, Guild: "100", Parent: "200", Owner: "400"}
		f.ledger.Events = append(f.ledger.Events, newMessageEvent{Event: "verified", Request: "unattended-" + req.Key + "-accepted", Phase: "log:0", Receipt: &newMessageReceipt{Thread: "801", Message: "901", Author: "400", Timestamp: created, Flags: 4096}})
		f.saveLedger()
		before, _ := json.Marshal(req)
		f.r.loadProgressLinks(context.Background(), map[string]bool{"801": true})
		after, _ := json.Marshal(req)
		if string(before) != string(after) || (f.r.ProgressLinks["801"].Todo == 2705) != known {
			t.Fatal("link discovery mutated an original receipt or expanded old bot context")
		}
	}
}

func TestOldBotQuestionCanReportMetadataWithoutChangingIntakeOrExecutionScope(t *testing.T) {
	for _, parent := range []string{"200", requestForum} {
		for _, version := range []uint8{0, 2} {
			f, req := oldBotQuestionReportFixture(t)
			thread := f.threads["601"]
			thread.Parent = parent
			f.threads["601"] = thread
			req.RecordVersion = version
			if version == 2 {
				req.Guild = "100"
			}
			before, _ := json.Marshal(req)
			beforeRows, err := os.ReadFile(f.r.TodoPath)
			if err != nil {
				t.Fatal(err)
			}
			var h Health
			f.r.reportProducts(context.Background(), &h)
			after, _ := json.Marshal(req)
			afterRows, err := os.ReadFile(f.r.TodoPath)
			if err != nil {
				t.Fatal(err)
			}
			if f.sends != 1 || h.ProductReports != 1 || string(before) != string(after) || string(beforeRows) != string(afterRows) {
				t.Fatal("metadata report was blocked or changed intake/task controls", h)
			}
			// Reporting the saved association is not an interpretation of "a".
			if f.r.validateRequest(context.Background(), req) == nil {
				t.Fatal("read-only reporting authorized an unknown old question")
			}
		}
	}
}

func TestReadOnlyReportRejectsChangedSourceAndNeverMigratesFailureState(t *testing.T) {
	for _, kind := range []string{"thread", "guild", "parent", "thread_owner", "message", "channel", "author", "bot", "created", "body", "edited", "reference", "future_schema", "saved_guild"} {
		t.Run(kind, func(t *testing.T) {
			f, req := oldBotQuestionReportFixture(t)
			thread, message := f.threads["601"], f.messages["601/"+f.source.ID]
			switch kind {
			case "thread":
				thread.ID = "999"
			case "guild":
				thread.Guild = "999"
			case "parent":
				thread.Parent = "999"
			case "thread_owner":
				thread.Owner = "999"
			case "message":
				message.ID = "999"
			case "channel":
				message.Channel = "999"
			case "author":
				message.Author.ID = "999"
			case "bot":
				message.Author.Bot = true
			case "created":
				message.Timestamp = f.now.Format("2006-01-02T15:04:05Z07:00")
			case "body":
				message.Content = "b"
			case "edited":
				message.Edited = f.now.Format("2006-01-02T15:04:05Z07:00")
			case "reference":
				req.RecordVersion, req.Guild = 2, "100"
				message.Reference.Message = "999"
			case "future_schema":
				req.RecordVersion = 3
			case "saved_guild":
				req.Guild = "999"
			}
			f.threads["601"], f.messages["601/"+f.source.ID] = thread, message
			before, _ := json.Marshal(req)
			var h Health
			f.r.reportProducts(context.Background(), &h)
			after, _ := json.Marshal(req)
			if f.sends != 0 || string(before) != string(after) {
				t.Fatal("changed provenance sent a report or mutated an intake receipt")
			}
		})
	}
}
