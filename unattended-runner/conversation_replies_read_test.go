package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConversationRepliesInspectionKeepsResidentStateAndCache(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	reply := f.reply(t, 8, "private current owner reply")
	outputDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(outputDir, "inspection.private.json")
	lockPath := filepath.Join(f.r.Config.Home, "runner.lock")
	if err := os.WriteFile(lockPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	before := conversationFixtureSnapshot(t, f.r.Config.Home)
	commands := len(f.commands)
	release, err := acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			release()
		}
	}()
	var out bytes.Buffer
	code := conversationCLIWithGuard([]string{"replies", "--request-id", q.RequestID, "--output", output}, &out,
		func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil },
		func(context.Context, string) bool { return true })
	if code != 0 {
		t.Fatalf("read-only inspection unavailable: exit%d %s", code, out.String())
	}
	data, err := os.ReadFile(output)
	var proof conversationProof
	if err != nil || json.Unmarshal(data, &proof) != nil || !proof.ReadOnly || proof.Source != "discord-api-v10" || len(proof.Replies) != 1 || proof.Replies[0].Message != reply.Message || proof.Replies[0].Body != reply.Body {
		t.Fatal("current owner proof was not retained privately")
	}
	release()
	held = false
	if !reflect.DeepEqual(before, conversationFixtureSnapshot(t, f.r.Config.Home)) {
		t.Fatal("inspection changed live state, index or cache")
	}
	if len(f.commands) != commands+1 || f.commands[commands] != "recovery-replies" {
		t.Fatal("inspection performed a mutation or delivery")
	}
	if strings.Contains(out.String(), reply.Body) || strings.Contains(out.String(), "fixture-token") || !strings.Contains(out.String(), "inspection_only") {
		t.Fatal("stdout disclosed text or claimed application")
	}
}

func TestConversationRepliesReadOnlyLoaderDoesNotTakeResidentLock(t *testing.T) {
	f := conversationConfigFixture(t)
	conversationWriteJSON(t, filepath.Join(f.config.Home, "conversation-questions.json"), conversationIndex{Domain: conversationDomain, Since: f.config.Since, Destination: f.destination, Questions: map[string]ConversationQuestion{}})
	before := conversationFixtureSnapshot(t, f.root)
	release, err := acquire(filepath.Join(f.config.Home, "runner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	backend, unlock, loadErr := loadConversationRepliesRunner(f.configPath)
	_, _, defaultErr := loadConversationCLIRunner(f.configPath)
	if unlock != nil {
		unlock()
	}
	release()
	if loadErr != nil || backend.(*Runner).Token != "fixture-token" || !errors.Is(defaultErr, errConversationBusy) {
		t.Fatal("inspection loader failed or cached/mutating loader lost its lock")
	}
	if !reflect.DeepEqual(before, conversationFixtureSnapshot(t, f.root)) {
		t.Fatal("read-only loader changed saved files")
	}
}

func TestConversationRepliesOutputRejectedBeforeLoading(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "existing.private.json")
	if err := os.WriteFile(existing, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"positive", "relative", "suffix", "guard", "existing", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			output := filepath.Join(dir, mode+".private.json")
			if mode == "relative" {
				output = "relative.private.json"
			}
			if mode == "suffix" {
				output = filepath.Join(dir, "public.json")
			}
			if mode == "existing" {
				output = existing
			}
			args := []string{"replies", "--request-id", "fixture", "--output", output}
			if mode == "duplicate" {
				args = append(args, "--output", output)
			}
			loads, guards := 0, 0
			var out bytes.Buffer
			code := conversationCLIWithGuard(args, &out, func(string) (conversationCLIBackend, func(), error) {
				loads++
				return nil, nil, errors.New("private loader details")
			}, func(context.Context, string) bool { guards++; return mode != "guard" })
			wantLoads := 0
			if mode == "positive" {
				wantLoads = 1
			}
			if code != 1 || loads != wantLoads || strings.Contains(out.String(), output) || strings.Contains(out.String(), "private loader") {
				t.Fatal("output rejection loaded credentials or disclosed details")
			}
			if mode == "guard" && guards != 1 {
				t.Fatal("guard rejection was not exercised")
			}
		})
	}
	data, _ := os.ReadFile(existing)
	if string(data) != "original" {
		t.Fatal("existing receipt overwritten")
	}
}

type conversationInspectionTransport func(*http.Request) (*http.Response, error)

func (f conversationInspectionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestConversationRepliesInspectionPreservesFreshEditedVersion(t *testing.T) {
	f := conversationTest(t, false)
	q := f.ask(t)
	reply := f.reply(t, 8, "same body after edit")
	edited := f.r.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	m := msg(mustHistoryTime(reply.Created), q.Thread, reply.Author, reply.Body)
	m.ID, m.Edited = reply.Message, edited
	b, _ := json.Marshal(m)
	var value map[string]any
	_ = json.Unmarshal(b, &value)
	value["type"] = 19
	b, _ = json.Marshal(value)
	f.r.Client.Transport = conversationInspectionTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b))}, nil
	})
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	output := filepath.Join(dir, "edit.private.json")
	result, err := f.r.conversationRepliesSnapshot(context.Background(), q.RequestID, output)
	if err != nil || result["owner_messages"] != 1 {
		t.Fatal("ordinary current version not verified")
	}
	data, _ := os.ReadFile(output)
	var receipt struct {
		Inspection struct {
			Versions []struct {
				Edited string `json:"edited_timestamp"`
				Kind   int    `json:"message_type"`
			} `json:"owner_reply_versions"`
		} `json:"inspection"`
	}
	if json.Unmarshal(data, &receipt) != nil || len(receipt.Inspection.Versions) != 1 || receipt.Inspection.Versions[0].Edited != edited || receipt.Inspection.Versions[0].Kind != 19 {
		t.Fatal("fresh edit/type metadata lost")
	}
}

func TestConversationRepliesInspectionRejectsChangedSourceAndSecrets(t *testing.T) {
	for _, mode := range []string{"question_changed", "live_body_changed", "foreign_author", "secret", "too_many"} {
		t.Run(mode, func(t *testing.T) {
			f := conversationTest(t, false)
			q := f.ask(t)
			reply := f.reply(t, 8, "current body")
			if mode == "secret" {
				f.replies[0].Body = "authorization: fixture-private-value"
			}
			if mode == "foreign_author" {
				f.replies[0].Author = "999"
			}
			if mode == "too_many" {
				for i := 1; i <= 100; i++ {
					f.reply(t, 8+i, "additional body")
				}
				f.r.Now = func() time.Time { return mustHistoryTime(reply.Created).Add(time.Hour) }
			}
			original := f.r.Run
			var mutationErr error
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				raw, err := original(ctx, path, args...)
				if len(args) > 0 && args[0] == "recovery-replies" {
					if mode == "question_changed" {
						index, e := f.r.loadConversations()
						if e != nil {
							mutationErr = e
						} else {
							current := index.Questions[q.RequestID]
							current.Title = "changed after API inspection"
							index.Questions[q.RequestID] = current
							mutationErr = publish(f.r.conversationPath(), index)
						}
					}
					if mode == "live_body_changed" {
						f.replies[0].Body = "later edit"
					}
				}
				return raw, err
			}
			dir, _ := filepath.EvalSymlinks(t.TempDir())
			output := filepath.Join(dir, "declined.private.json")
			_, err := f.r.conversationRepliesSnapshot(context.Background(), q.RequestID, output)
			if err == nil || mutationErr != nil {
				t.Fatal("changed or unsafe source accepted, or fixture mutation failed")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("rejected inspection published a receipt")
			}
		})
	}
}
