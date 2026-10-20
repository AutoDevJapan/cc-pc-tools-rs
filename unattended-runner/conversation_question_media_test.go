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
	image := conversationQuestionImagePath(t, f)
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
	if _, err = f.r.conversationAsk(context.Background(), f.input); err == nil || !reflect.DeepEqual(sent, []string{image}) {
		t.Fatal("saved upload inspection did not reach the sender")
	}
}

func TestConversationQuestionImageReadbackMustMatchBeforeWait(t *testing.T) {
	for _, verified := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_image", true: "verified_image"}[verified], func(t *testing.T) {
			f := conversationTest(t, true)
			image := conversationQuestionImagePath(t, f)
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

func conversationQuestionImagePath(t *testing.T, f *conversationFixture) string {
	t.Helper()
	// Windows service TEMP may contain 8.3 aliases. Synthetic input must obey
	// the same physical spelling as real bridge output; keep production strict.
	home, err := filepath.EvalSymlinks(f.r.Config.Home)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, "question.png")
}

func TestConversationLostImageResultRetriesWithoutOriginalSource(t *testing.T) {
	f := conversationTest(t, false)
	image := conversationQuestionImagePath(t, f)
	data := []byte("\x89PNG\r\n\x1a\noriginal-image")
	if err := os.WriteFile(image, data, 0600); err != nil {
		t.Fatal(err)
	}
	f.input.Attachments = []string{image}
	base := f.r.Run
	calls := 0
	var expected []string
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "recovery-ask" {
			calls++
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "--expected-image-sha256" {
					expected = append(expected, args[i+1])
				}
			}
			return "", errors.New("result lost")
		}
		return base(ctx, path, args...)
	}
	_, _ = f.r.conversationAsk(context.Background(), f.input)
	if err := os.Remove(image); err != nil {
		t.Fatal(err)
	}
	_, _ = f.r.conversationAsk(context.Background(), f.input)
	if calls != 2 || !reflect.DeepEqual(expected, []string{bytesHash(data), bytesHash(data)}) {
		t.Fatal("saved upload could not be inspected without the original source")
	}
}

func TestConversationPostedQuestionRecordsWaitWithoutOriginalSource(t *testing.T) {
	f := conversationTest(t, true)
	image := conversationQuestionImagePath(t, f)
	data := []byte("\x89PNG\r\n\x1a\noriginal-image")
	if err := os.WriteFile(image, data, 0600); err != nil {
		t.Fatal(err)
	}
	f.input.Attachments = []string{image}
	f.waitFailure = true
	base := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		raw, err := base(ctx, path, args...)
		if err == nil && len(args) > 0 && args[0] == "recovery-ask" {
			var value map[string]any
			_ = json.Unmarshal([]byte(raw), &value)
			value["image_sha256"] = []string{bytesHash(data)}
			b, _ := json.Marshal(value)
			return string(b), nil
		}
		return raw, err
	}
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil {
		t.Fatal("expected wait recording failure")
	}
	if err := os.Remove(image); err != nil {
		t.Fatal(err)
	}
	q, err := f.r.conversationAsk(context.Background(), f.input)
	if err != nil || q.Phase != "waiting" || f.posts != 1 {
		t.Fatalf("posted question could not recover its wait: %v", err)
	}
}

func TestConversationChangedQuestionImagesReturnExistingQuestionWithoutNewWait(t *testing.T) {
	f := conversationTest(t, true)
	image := conversationQuestionImagePath(t, f)
	data := []byte("\x89PNG\r\n\x1a\nfirst-image")
	if err := os.WriteFile(image, data, 0600); err != nil {
		t.Fatal(err)
	}
	f.input.Attachments = []string{image}
	base := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		raw, err := base(ctx, path, args...)
		if err == nil && len(args) > 0 && args[0] == "recovery-ask" {
			var value map[string]any
			_ = json.Unmarshal([]byte(raw), &value)
			value["image_sha256"] = []string{bytesHash(data)}
			b, _ := json.Marshal(value)
			return string(b), nil
		}
		return raw, err
	}
	original := f.ask(t)
	next := f.input
	next.RequestID = "different-images"
	second := filepath.Join(filepath.Dir(image), "second.png")
	if err := os.WriteFile(second, []byte("\x89PNG\r\n\x1a\nsecond-image"), 0600); err != nil {
		t.Fatal(err)
	}
	next.Attachments = []string{second}
	q, err := f.r.conversationAsk(context.Background(), next)
	if err != nil || q.Phase != "duplicate" || q.Thread != original.Thread || q.Wait || q.ExistingURL == "" || f.posts != 1 {
		t.Fatalf("did not return the existing image question: %v %+v", err, q)
	}
	index, err := f.r.loadConversations()
	if err != nil || len(index.Questions) != 1 {
		t.Fatal("duplicate created a permanent retry")
	}
	row, err := f.r.todo(42)
	if err != nil || row.Status != "block" || !conversationWaitRecorded(row, original) {
		t.Fatal("original UI wait changed")
	}
}
