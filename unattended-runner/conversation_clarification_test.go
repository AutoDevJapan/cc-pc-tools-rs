package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestClarificationCLIRequiresSilentExistingThread(t *testing.T) {
	base := append(conversationLogArgs(), "--thread=600", "--clarification-file=binding.json")
	if opts, err := parseConversationCLI(base); err != nil || opts.ClarificationFile != "binding.json" {
		t.Fatal("explicit explanation binding lost", err)
	}
	for _, extra := range [][]string{{"--notify"}, {"--attach=image.png"}, {"--clarification-file=another.json"}} {
		args := append(append([]string{}, base...), extra...)
		if _, err := parseConversationCLI(args); err == nil {
			t.Fatal("unsafe explanation flags accepted")
		}
	}
	if _, err := parseConversationCLI(append(conversationLogArgs(), "--clarification-file=binding.json")); err == nil {
		t.Fatal("implicit destination accepted")
	}
	if _, err := parseConversationCLI(append(conversationLogArgs(), "--check-only")); err == nil {
		t.Fatal("unscoped check accepted")
	}
}

func TestClarificationResumesExactLogWithoutChangingOriginalReservation(t *testing.T) {
	f := conversationTest(t, false)
	body := filepath.Join(f.r.Config.Home, "body.txt")
	manifest := filepath.Join(f.r.Config.Home, "binding.json")
	if os.WriteFile(body, []byte("元の質問の背景説明です。"), 0600) != nil || os.WriteFile(manifest, []byte(`{"fixture":"binding"}`), 0600) != nil {
		t.Fatal("fixture write")
	}
	f.r.Run = func(context.Context, string, ...string) (string, error) {
		return "", errors.New("original thread unsupported")
	}
	if _, err := f.r.conversationLog(context.Background(), "original-log", "説明", body, "610", false, nil); err == nil {
		t.Fatal("fixture must retain an unsent reservation")
	}
	before, _ := f.r.loadConversations()
	original := before.Logs["original-log"]
	calls := 0
	f.r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		checking := false
		for _, arg := range args {
			checking = checking || arg == "--check-only"
		}
		if argValue(args, "--request-id") != "conversation-log-"+bytesHash([]byte("original-log"))[:32] {
			return "", errors.New("binding rejects a different request")
		}
		if !checking {
			calls++
		}
		if args[0] != "recovery-log" || argValue(args, "--thread") != "610" || !checking && argValue(args, "--body-file") != original.BodyFile {
			t.Error("original sender input changed")
		}
		proof, err := os.ReadFile(argValue(args, "--clarification-file"))
		if err != nil || string(proof) != `{"fixture":"binding"}` {
			t.Error("fixed association evidence lost")
		}
		for _, arg := range args {
			if arg == "--check-only" {
				return firstCheckReport(args), nil
			}
		}
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"620","reused":true}`, nil
	}
	indexBefore, _ := os.ReadFile(f.r.conversationPath())
	if _, err := f.r.conversationCheckClarification(context.Background(), "original-log", "説明", body, "610", manifest); err != nil {
		t.Fatal(err)
	}
	indexAfter, _ := os.ReadFile(f.r.conversationPath())
	if string(indexBefore) != string(indexAfter) {
		t.Fatal("check changed original reservation")
	}
	if _, err := os.Stat(filepath.Join(f.r.Config.Home, "conversation-clarifications", "original-log.json")); !os.IsNotExist(err) {
		t.Fatal("check pinned send evidence")
	}
	calls = 0
	for range 2 {
		if _, err := f.r.conversationClarification(context.Background(), "original-log", "説明", body, "610", manifest); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := f.r.loadConversations()
	if after.Logs["original-log"].InputSHA != original.InputSHA || after.Logs["original-log"].BodyFile != original.BodyFile || after.Logs["original-log"].Thread != original.Thread {
		t.Fatal("original reservation changed")
	}
	for _, change := range []struct{ id, thread string }{{"copy", "610"}, {"original-log", "611"}} {
		if _, err := f.r.conversationClarification(context.Background(), change.id, "説明", body, change.thread, manifest); err == nil {
			t.Fatal("different original identity accepted")
		}
	}
	if calls != 2 {
		t.Fatal("rejected input reached sender")
	}
	if os.WriteFile(manifest, []byte(`{"fixture":"changed"}`), 0600) != nil {
		t.Fatal("fixture mutation")
	}
	// A failed candidate can be corrected on the same original request. The
	// sender's verified binding remains the sole authority to reject rebinding.
	var correctedPath string
	f.r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		correctedPath = argValue(args, "--clarification-file")
		return "", errors.New("sender rejects a different verified binding")
	}
	if _, err := f.r.conversationClarification(context.Background(), "original-log", "説明", body, "610", manifest); err == nil || correctedPath == "" {
		t.Fatal("new proof did not reach authoritative sender validation")
	}
	if correctedPath != manifest {
		t.Fatal("invalid binding must be inspected before creating a snapshot")
	}
}
