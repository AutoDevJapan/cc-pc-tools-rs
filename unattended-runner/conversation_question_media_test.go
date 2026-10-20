package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConversationCLIAskAttachments(t *testing.T) {
	args := append(conversationAskArgs(), "--attach=first.png", "--attach", "second.png")
	opts, err := parseConversationCLI(args)
	if err != nil || !reflect.DeepEqual(opts.Attachments, []string{"first.png", "second.png"}) {
		t.Fatalf("question attachments must reach the normal ask entry point: options=%+v error=%v", opts, err)
	}
	fake := &conversationCLIFake{}
	var out bytes.Buffer
	if code := conversationCLIWithLoader(args, &out, func(string) (conversationCLIBackend, func(), error) {
		return fake, func() {}, nil
	}); code != 0 {
		t.Fatalf("question attachments must dispatch: code=%d output=%s", code, &out)
	}
	if !reflect.DeepEqual(fake.ask.Attachments, opts.Attachments) {
		t.Fatal("question attachments were dropped at dispatch")
	}
}

func TestConversationQuestionAttachmentPersistsAcrossLostResult(t *testing.T) {
	f := conversationTest(t, true)
	image := filepath.Join(f.r.Config.Home, "question.png")
	data := []byte("\x89PNG\r\n\x1a\nquestion-image")
	if err := os.WriteFile(image, data, 0600); err != nil {
		t.Fatal(err)
	}
	f.input.Attachments = []string{image}
	base := f.r.Run
	var sent []string
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "recovery-ask" {
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "--attach" {
					sent = append(sent, args[i+1])
				}
			}
			return "", errors.New("lost result")
		}
		return base(ctx, path, args...)
	}
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil {
		t.Fatal("lost result reported as delivered")
	}
	if !reflect.DeepEqual(sent, []string{image}) {
		t.Fatal("question image did not reach the sender")
	}
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	q := index.Questions[f.input.RequestID]
	if !reflect.DeepEqual(q.Attachments, []string{image}) || !reflect.DeepEqual(q.AttachmentHashes, []string{bytesHash(data)}) || q.Phase != "reserved" {
		t.Fatal("question image provenance not retained for retry")
	}
	sent = nil
	f.r.processConversations(context.Background(), &Health{})
	if !reflect.DeepEqual(sent, []string{image}) {
		t.Fatal("automatic retry dropped the question image")
	}
	if err = os.WriteFile(image, []byte("\x89PNG\r\n\x1a\nchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	sent = nil
	if _, err = f.r.conversationAsk(context.Background(), f.input); err == nil || len(sent) != 0 {
		t.Fatal("changed retry image reached the sender")
	}
}

func TestConversationQuestionImageReadbackMustMatchBeforeWait(t *testing.T) {
	for _, verified := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_image", true: "verified_image"}[verified], func(t *testing.T) {
			f := conversationTest(t, true)
			image := filepath.Join(f.r.Config.Home, "question.png")
			data := []byte("\x89PNG\r\n\x1a\nquestion-image")
			if err := os.WriteFile(image, data, 0600); err != nil {
				t.Fatal(err)
			}
			f.input.Attachments = []string{image}
			base := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				raw, err := base(ctx, path, args...)
				if err == nil && len(args) != 0 && args[0] == "recovery-ask" && verified {
					var result map[string]any
					if json.Unmarshal([]byte(raw), &result) != nil {
						t.Fatal("invalid fixture report")
					}
					result["image_sha256"] = []string{bytesHash(data)}
					b, _ := json.Marshal(result)
					raw = string(b)
				}
				return raw, err
			}
			q, err := f.r.conversationAsk(context.Background(), f.input)
			row, rowErr := f.r.todo(42)
			if rowErr != nil {
				t.Fatal(rowErr)
			}
			if verified {
				if err != nil || q.Phase != "waiting" || row.Status != "block" {
					t.Fatalf("verified question did not enter its canonical wait: %v", err)
				}
			} else if err == nil || row.Status != "doing" || f.posts != 1 {
				t.Fatal("missing image was reported as a delivered owner question")
			}
		})
	}
}
