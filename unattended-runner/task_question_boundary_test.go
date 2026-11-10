package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskQuestionNativeSecondsCreation(t *testing.T) {
	for _, kind := range []string{"same_second", "same_second_zone", "fractional", "old_second", "future_second", "other_task", "stopped"} {
		t.Run(kind, func(t *testing.T) {
			c, f, _ := taskQuestionReleasedFixture(t, false)
			row, _ := c.r.todo(42)
			var record struct {
				At time.Time `json:"applied_at"`
			}
			if json.Unmarshal(row.QuestionAnswers[0], &record) != nil || record.At.Nanosecond() == 0 {
				t.Fatal("microsecond canonical answer missing")
			}
			id := taskQuestionStartSuccessor(t, c, f)
			created := record.At.Add(time.Nanosecond).Format(time.RFC3339)
			if kind == "same_second_zone" {
				created = record.At.Add(time.Nanosecond).In(time.FixedZone("native-local", 9*60*60)).Format(time.RFC3339)
			}
			if kind == "fractional" {
				created = record.At.Add(time.Nanosecond).Format(time.RFC3339Nano)
			}
			if kind == "old_second" {
				created = record.At.Add(-2 * time.Second).Format(time.RFC3339)
			}
			if kind == "future_second" {
				created = record.At.Add(time.Minute).Format(time.RFC3339)
			}
			job := map[string]any{"id": id, "status": "running", "todo_id": 42, "created_at": created}
			if kind == "other_task" {
				job["todo_id"] = 43
			}
			if kind == "stopped" {
				job["owner_stopped"] = true
			}
			mediaJSON(t, filepath.Join(f.r.Jobs, id, "job.json"), job)
			reply := c.reply(t, 9, "full follow-up with native job timestamp precision")
			before, _ := os.ReadFile(c.r.TodoPath)
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			row, _ = c.r.todo(42)
			if kind == "same_second" || kind == "same_second_zone" || kind == "fractional" {
				if len(h.Errors) != 0 || c.r.conversationRequest(reply.Message).Phase != "conversation_reply_delivered" || strings.Count(row.conversationInstruction(), reply.Body) != 1 {
					t.Fatal("native creation precision rejected", h.Errors)
				}
			} else {
				after, _ := os.ReadFile(c.r.TodoPath)
				if len(h.Errors) == 0 || !bytes.Equal(before, after) {
					t.Fatal("invalid native job accepted", h.Errors)
				}
			}
		})
	}
}

func TestTaskQuestionCanonicalReservationCycles(t *testing.T) {
	for _, transition := range []string{"reprepare", "linked_start"} {
		for _, kind := range []string{"followup", "edit", "lost_response"} {
			t.Run(transition+"_"+kind, func(t *testing.T) {
				c, f, first := taskQuestionReleasedFixture(t, kind == "lost_response")
				if transition == "reprepare" {
					taskQuestionSchedulerProgress(t, c, f, "prepared")
					taskQuestionSchedulerProgress(t, c, f, "prepared")
				} else {
					taskQuestionSchedulerProgress(t, c, f, "running")
					if _, err := c.r.Run(context.Background(), conversationTodo, "start", "42", "--job", "regular-scheduler-successor"); err != nil {
						t.Fatal(err)
					}
				}
				message, want := first.Message, first.Body
				if kind == "followup" {
					r := c.reply(t, 9, "full follow-up after regular reservation cycle")
					message, want = r.Message, r.Body
				} else if kind == "edit" {
					want = "full edited reply after regular reservation cycle"
					c.replies[0].Body = want
					c.edited = map[string]string{first.Message: time.Now().UTC().Format(time.RFC3339Nano)}
				}
				h := Health{}
				c.r.processConversations(context.Background(), &h)
				row, _ := c.r.todo(42)
				if len(h.Errors) != 0 || c.r.conversationRequest(message).Phase != "conversation_reply_delivered" || strings.Count(row.Note, want) != 1 || strings.Count(row.conversationInstruction(), want) != 1 {
					t.Fatal("regular writer cycle was not consumed", h.Errors)
				}
				before, _ := os.ReadFile(c.r.TodoPath)
				h = Health{}
				c.r.processConversations(context.Background(), &h)
				after, _ := os.ReadFile(c.r.TodoPath)
				if len(h.Errors) != 0 || !bytes.Equal(before, after) {
					t.Fatal("cycle replay duplicated input", h.Errors)
				}
			})
		}
	}
}
