package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func message(id, tid, author, body string, bot bool) obj {
	return obj{"id": id, "channel_id": tid, "content": body, "author": obj{"id": author, "bot": bot}, "timestamp": "2026-10-11T00:00:00Z"}
}

func TestPrivateTextNeverAppearsInMetadata(t *testing.T) {
	const tid = "1554792868974039150"
	const bot = "1554269962229649478"
	const owner = "730822776147148835"
	q := message("1554792868974039151", tid, bot, "秘密の架空案件\na 保持\nb 削除\n判断を教えてください", true)
	reply := message("1554792868974039152", tid, owner, "a 秘密の架空案件", false)
	reply["message_reference"] = obj{"channel_id": tid, "message_id": q["id"]}
	spoof := message("1554792868974039153", tid, "730822776147148836", "a", false)
	stage, meta := stageHistory(tid, bot, owner, []obj{q, reply, spoof}, true)
	b, _ := json.Marshal(meta)
	if strings.Contains(string(b), "秘密") || strings.Contains(string(b), "保持") {
		t.Fatal("private body leaked")
	}
	if meta["owner_replies_identity_verified"] != 1 || meta["reply_explicit_generation_reference"] != 1 || meta["verified_decision_scopes"] != 0 {
		t.Fatal(meta)
	}
	if len(array(stage["messages"])) != 2 || object(array(stage["owner_replies"])[0])["applied"] != false {
		t.Fatal("spoof retained or reply treated as applied")
	}
}

func TestAmbiguousSequenceDoesNotVerifyScope(t *testing.T) {
	const tid = "1554792868974039150"
	const bot = "1554269962229649478"
	const owner = "730822776147148835"
	first := message("1554792868974039151", tid, owner, "a", false)
	q := message("1554792868974039152", tid, bot, "a 保存\nb 削除\n判断を教えてください", true)
	r := message("1554792868974039153", tid, owner, "a", false)
	r["message_reference"] = obj{"channel_id": "1554792868974039159", "message_id": q["id"]}
	s, m := stageHistory(tid, bot, owner, []obj{first, q, r}, true)
	if m["reply_missing_question"] != 1 || m["reply_explicit_generation_reference"] != 0 {
		t.Fatal(m)
	}
	for _, x := range array(s["owner_replies"]) {
		v := object(x)
		if v["decision_scope_verified"] != false || v["delivery_verified"] != false {
			t.Fatal(v)
		}
	}
	second := object(array(s["owner_replies"])[1])
	if second["association"] != "explicit_reference_unverified" || second["question_message"] != "" || second["original_reference_message_id"] != q["id"] {
		t.Fatal(second)
	}
}

func TestPartialHistorySortedBeforeAssociating(t *testing.T) {
	const tid = "1554792868974039150"
	const bot = "1554269962229649478"
	const owner = "730822776147148835"
	q := message("1554792868974039151", tid, bot, "a 保存\nb 削除\n判断を教えてください", true)
	r := message("1554792868974039152", tid, owner, "a", false)
	s, m := stageHistory(tid, bot, owner, []obj{r, q}, false)
	reply := object(array(s["owner_replies"])[0])
	if reply["question_message"] != q["id"] || m["history_complete"] != false {
		t.Fatal(reply, m)
	}
}

func TestPrivateOutputRefusedInsideGit(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".git"), 0700)
	if privateDir(filepath.Join(root, "private")) == nil {
		t.Fatal("accepted output in repo")
	}
	if _, e := os.Stat(filepath.Join(root, "private")); !os.IsNotExist(e) {
		t.Fatal("created refused output")
	}
}

func TestWideNumericOrder(t *testing.T) {
	if !less("9999999999999999999", "10000000000000000000") || less("10000000000000000000", "9999999999999999999") {
		t.Fatal("numeric order overflow")
	}
}

func TestHistoryUsesGETAndAdvancingCursor(t *testing.T) {
	const tid = "1554792868974039150"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || !strings.HasPrefix(r.Header.Get("User-Agent"), "DiscordBot (") {
			t.Error("bad method or UA")
		}
		if calls == 1 {
			if r.URL.Query().Get("before") != "" {
				t.Error("initial cursor")
			}
			page := []obj{}
			for i := 0; i < 100; i++ {
				page = append(page, message(fmt.Sprint(1554792868974039200+i), tid, "730822776147148835", "private", false))
			}
			json.NewEncoder(w).Encode(page)
		} else {
			if r.URL.Query().Get("before") != "1554792868974039200" {
				t.Error("cursor")
			}
			fmt.Fprint(w, "[]")
		}
	}))
	defer server.Close()
	a := &api{client: server.Client(), base: server.URL, token: "fake"}
	rows, complete, e := history(context.Background(), a, tid, 3)
	if e != nil || !complete || len(rows) != 100 || calls != 2 {
		t.Fatalf("%v %v %d %d", e, complete, len(rows), calls)
	}
}

func TestTruncatedAndWrongChannelAreNotComplete(t *testing.T) {
	const tid = "1554792868974039150"
	for _, wrong := range []bool{false, true} {
		t.Run(fmt.Sprint(wrong), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page := []obj{}
				channel := tid
				if wrong {
					channel = "1554792868974039151"
				}
				for i := 0; i < 100; i++ {
					page = append(page, message(fmt.Sprint(1554792868974039200+i), channel, "730822776147148835", "private", false))
				}
				json.NewEncoder(w).Encode(page)
			}))
			defer server.Close()
			a := &api{client: server.Client(), base: server.URL}
			_, complete, e := history(context.Background(), a, tid, 1)
			if complete || wrong && e == nil || !wrong && e != nil {
				t.Fatalf("complete=%v err=%v", complete, e)
			}
		})
	}
}

func TestNoOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "evidence.json")
	if e := writeNew(p, obj{"first": true}); e != nil {
		t.Fatal(e)
	}
	if writeNew(p, obj{"second": true}) == nil {
		t.Fatal("overwrote evidence")
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "first") {
		t.Fatal("original gone")
	}
}

func TestUntrustedSeedRejectedBeforeNetwork(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cfg.json")
	seed := filepath.Join(dir, "seed.json")
	writeNew(cfg, obj{"guild_id": "1552944707464597524", "owner_user_id": "730822776147148835", "task_channel_id": "1554276307997692004", "application_id": "1554269962229649478"})
	writeNew(seed, obj{"kind": "original-record-seeds", "threads": []any{}})
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("untrusted network call") }))
	defer server.Close()
	a := &api{client: server.Client(), base: server.URL}
	_, e := collect(context.Background(), a, cfg, seed, filepath.Join(dir, "new"), []string{"1554792868974039150"}, 1)
	if e == nil || calls != 0 {
		t.Fatal("untrusted seed accepted")
	}
}

func TestErrorBodyIsNotReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, "private error secret")
	}))
	defer server.Close()
	a := &api{client: server.Client(), base: server.URL}
	_, e := a.get(context.Background(), "/test")
	if e == nil || e.Error() != "http_status_403" {
		t.Fatal(e)
	}
}

func TestReindexRejectsTamperedPrivateEvidence(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.Mkdir(source, 0700)
	const tid = "1554792868974039150"
	stage := obj{"source": "authenticated Discord API GET", "thread_id": tid}
	writeNew(filepath.Join(source, tid+".private.json"), stage)
	report := obj{"kind": "original-record-report", "guild_id": "1552944707464597524", "bot_id": "1554269962229649478", "owner_id": "730822776147148835", "threads": []any{obj{"thread_id": tid, "stage_sha256": "incorrect"}}}
	writeNew(filepath.Join(source, "report.json"), report)
	_, e := reindex(source, filepath.Join(root, "output"))
	if e == nil || e.Error() != "acquisition_hash_or_identity_mismatch" {
		t.Fatal(e)
	}
}
