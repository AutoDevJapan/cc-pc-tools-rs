package main

// Synthetic acceptance of the Go-to-sender boundary. No Discord/API call or
// owner decision is represented by the substituted sender's result.
import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func firstCheckReport(args []string) string {
	body, _ := os.ReadFile(argValue(args, "--body-file"))
	proof, _ := os.ReadFile(argValue(args, "--clarification-file"))
	text := strings.TrimLeft(string(body), "\ufeff")
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	value, _ := json.Marshal(map[string]any{"domain": "new-owner-recovery-messages", "request_id": argValue(args, "--request-id"), "check_only": true, "delivery": "not_sent", "source_body_sha256": bytesHash(body), "body_sha256": bytesHash([]byte(text)), "binding_sha256": bytesHash(proof)})
	return string(value)
}

func firstClarificationFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[relative] = "directory"
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[relative] = bytesHash(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func firstClarificationInput(t *testing.T, f *conversationFixture) (string, string) {
	t.Helper()
	body := filepath.Join(f.r.Config.Home, "explanation-source.txt")
	manifest := filepath.Join(f.r.Config.Home, "explanation-proof.json")
	if err := os.WriteFile(body, []byte("費用の内訳を補足します。"), 0600); err != nil {
		t.Fatal(err)
	}
	// The actual sender validates the manifest. This boundary must forward it
	// unchanged, and may save an original only after the sender's acceptance.
	proof := map[string]any{
		"domain": "discord-question-clarification-v1", "purpose": "explain_original_question",
		"request_id":          "conversation-log-" + bytesHash([]byte("fresh-log"))[:32],
		"question_request_id": "conversation-v1-" + bytesHash([]byte(f.input.RequestID))[:32],
		"todo_id":             42, "thread_id": "610", "question_message_id": "610",
		"question_fingerprint": strings.Repeat("a", 64),
		"question_body_sha256": bytesHash([]byte("aかbを選んでください。")),
		"bot_id":               f.r.State.Destination.Bot, "owner_id": f.r.State.Destination.Owner,
		"reply_message_id": "612", "reply_body_sha256": bytesHash([]byte("費用の用途を説明してほしい。")),
		"reply_timestamp": f.r.Now().Add(-time.Second).Format(time.RFC3339Nano), "reply_edited_timestamp": nil,
		"body_sha256": bytesHash([]byte("費用の内訳を補足します。")),
	}
	bytes, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(manifest, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	return body, manifest
}

func TestFirstClarificationCheckDoesNotRequireOrCreateLogReservation(t *testing.T) {
	f := conversationTest(t, false)
	body, proof := firstClarificationInput(t, f)
	before := firstClarificationFiles(t, f.r.Config.Home)
	checks, sends := 0, 0
	f.r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		checking := false
		for _, arg := range args {
			checking = checking || arg == "--check-only"
		}
		if !checking {
			sends++
			return "", errors.New("check must never send")
		}
		checks++
		return firstCheckReport(args), nil
	}
	report, err := f.r.conversationCheckClarification(context.Background(), "fresh-log", "補足", body, "610", proof)
	if err != nil {
		t.Fatalf("fresh GET-only check rejected before sender validation: %v", err)
	}
	if report["delivery"] != "not_sent" || report["check_only"] != true {
		t.Fatal("check was treated as delivery", report)
	}
	if checks == 0 || sends != 0 {
		t.Fatalf("check/send boundary: %d/%d", checks, sends)
	}
	if after := firstClarificationFiles(t, f.r.Config.Home); !reflect.DeepEqual(after, before) {
		t.Fatal("check created or changed an original file", before, after)
	}
}

func TestFirstClarificationSendValidatesBeforePersistingOriginal(t *testing.T) {
	f := conversationTest(t, false)
	body, proof := firstClarificationInput(t, f)
	checks, sends := 0, 0
	premature, bytesChanged := false, false
	f.r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		checking := false
		for _, arg := range args {
			checking = checking || arg == "--check-only"
		}
		if checking {
			if checks == 0 {
				_, statErr := os.Stat(f.r.conversationPath())
				premature = !os.IsNotExist(statErr)
			}
			checks++
			return firstCheckReport(args), nil
		}
		sends++
		if checks == 0 {
			return "", errors.New("sender requires prevalidation")
		}
		sentBody, bodyErr := os.ReadFile(argValue(args, "--body-file"))
		sentProof, proofErr := os.ReadFile(argValue(args, "--clarification-file"))
		originalProof, originalErr := os.ReadFile(proof)
		bytesChanged = bodyErr != nil || proofErr != nil || originalErr != nil || string(sentBody) != "費用の内訳を補足します。" || string(sentProof) != string(originalProof)
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"620","reused":false}`, nil
	}
	report, err := f.r.conversationClarification(context.Background(), "fresh-log", "補足", body, "610", proof)
	if err != nil {
		t.Fatalf("first explanation rejected: %v", err)
	}
	if report["delivery"] != "verified" || checks == 0 || sends != 1 || premature || bytesChanged {
		t.Fatal("first delivery did not retain validated original", report, checks, sends, premature, bytesChanged)
	}
	index, err := f.r.loadConversations()
	if err != nil || index.Logs["fresh-log"].Message != "620" || index.Logs["fresh-log"].Thread != "610" {
		t.Fatal("first delivery receipt missing", err)
	}
}

func TestFirstClarificationRejectionLeavesAllInputsUnchanged(t *testing.T) {
	f := conversationTest(t, false)
	body, proof := firstClarificationInput(t, f)
	before := firstClarificationFiles(t, f.r.Config.Home)
	checks, sends := 0, 0
	f.r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		checking := false
		for _, arg := range args {
			checking = checking || arg == "--check-only"
		}
		if checking {
			checks++
		} else {
			sends++
		}
		return "", errors.New("synthetic current-owner-reply mismatch")
	}
	_, err := f.r.conversationClarification(context.Background(), "fresh-log", "補足", body, "610", proof)
	if err == nil {
		t.Fatal("invalid current reply accepted")
	}
	if checks == 0 || sends != 0 {
		t.Fatalf("rejection did not reach GET-only validation: checks=%d sends=%d error=%v", checks, sends, err)
	}
	if after := firstClarificationFiles(t, f.r.Config.Home); !reflect.DeepEqual(after, before) {
		t.Fatal("rejected explanation left original input or reservation", before, after)
	}
}

func TestFirstClarificationMismatchedReadBytesCannotCreateOriginal(t *testing.T) {
	for _, field := range []string{"source_body_sha256", "binding_sha256", "request_id"} {
		t.Run(field, func(t *testing.T) {
			f := conversationTest(t, false)
			body, proof := firstClarificationInput(t, f)
			before := firstClarificationFiles(t, f.r.Config.Home)
			f.r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
				var checked map[string]any
				if json.Unmarshal([]byte(firstCheckReport(args)), &checked) != nil {
					t.Fatal("fixture report")
				}
				checked[field] = "wrong"
				raw, _ := json.Marshal(checked)
				return string(raw), nil
			}
			if _, err := f.r.conversationClarification(context.Background(), "fresh-log", "補足", body, "610", proof); err == nil {
				t.Fatal("different inspected input accepted")
			}
			if after := firstClarificationFiles(t, f.r.Config.Home); !reflect.DeepEqual(before, after) {
				t.Fatal("mismatched input persisted")
			}
		})
	}
}

func TestFirstClarificationUsesTheAcceptedBytesWhenOriginalFilesChange(t *testing.T) {
	f := conversationTest(t, false)
	body, proof := firstClarificationInput(t, f)
	originalBody, _ := os.ReadFile(body)
	originalProof, _ := os.ReadFile(proof)
	checks, sends := 0, 0
	valid := false
	f.r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		for _, value := range args {
			if value == "--check-only" {
				checks++
				result := firstCheckReport(args)
				if os.WriteFile(body, []byte("changed body"), 0600) != nil || os.WriteFile(proof, []byte("changed proof"), 0600) != nil {
					t.Fatal("fixture mutation")
				}
				return result, nil
			}
		}
		sends++
		sentBody, _ := os.ReadFile(argValue(args, "--body-file"))
		sentProof, _ := os.ReadFile(argValue(args, "--clarification-file"))
		valid = string(sentBody) == string(originalBody) && string(sentProof) == string(originalProof)
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"620"}`, nil
	}
	if _, err := f.r.conversationClarification(context.Background(), "fresh-log", "補足", body, "610", proof); err != nil {
		t.Fatal(err)
	}
	if checks != 1 || sends != 1 || !valid {
		t.Fatal("validated bytes changed", checks, sends, valid)
	}
}

func TestFirstClarificationCLICheckDoesNotCreateRunnerHomeOrLock(t *testing.T) {
	f := conversationConfigFixture(t)
	f.config.Home = filepath.Join(f.root, "absent-home")
	conversationWriteJSON(t, f.configPath, f.config)
	before := firstClarificationFiles(t, f.root)
	var out strings.Builder
	code := conversationCLI([]string{"--config", f.configPath, "log", "--request-id", "fresh-log", "--title", "説明", "--body-file", filepath.Join(f.root, "missing-body"), "--thread", "610", "--clarification-file", filepath.Join(f.root, "missing-proof"), "--check-only"}, &out)
	if code == 0 {
		t.Fatal("missing independent original accepted")
	}
	if after := firstClarificationFiles(t, f.root); !reflect.DeepEqual(before, after) {
		t.Fatal("check created home, lock or original", out.String())
	}
}
