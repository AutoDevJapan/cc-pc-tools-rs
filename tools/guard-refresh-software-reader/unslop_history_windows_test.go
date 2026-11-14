//go:build windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUnslopHistoryBatchPreservesIDsAndRejectsIncompleteResults(t *testing.T) {
	for _, mode := range []string{"normal", "out_of_order", "missing", "duplicate", "wrong_id", "error", "bad_json"} {
		t.Run(mode, func(t *testing.T) {
			raw := `{"id":1,"result":{}}` + "\n" + `{"id":2,"result":{"thread":{"id":"owned"}}}` + "\n"
			switch mode {
			case "out_of_order":
				raw = `{"id":2,"result":{"thread":{"id":"owned"}}}` + "\n" + `{"id":1,"result":{}}` + "\n"
			case "missing":
				raw = `{"id":1,"result":{}}`
			case "duplicate":
				raw += `{"id":2,"result":{}}`
			case "wrong_id":
				raw = strings.ReplaceAll(raw, `"id":2`, `"id":3`)
			case "error":
				raw = `{"id":1,"result":{}}` + "\n" + `{"id":2,"error":{"code":1}}`
			case "bad_json":
				raw = "invalid"
			}
			result, e := parseUnslopHistoryBatch([]byte(raw), 1)
			valid := mode == "normal" || mode == "out_of_order"
			if (e == nil) != valid {
				t.Fatal("RPC correspondence", mode, e)
			}
			if valid && (len(result) != 1 || !json.Valid(result[0])) {
				t.Fatal("missing response")
			}
		})
	}
}

func TestUnslopNormalHistoryMetadataTransport(t *testing.T) {
	id := os.Getenv("UNSLOP_HISTORY_TEST_THREAD")
	if id == "" {
		t.Skip("explicit owned-thread metadata integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	answers, e := runUnslopHistoryBatch(ctx, []unslopHistoryRequest{{"thread/read", map[string]any{"threadId": id, "includeTurns": false}}})
	if e != nil {
		t.Fatal("normal history metadata connection failed", e)
	}
	defer func() {
		for _, raw := range answers {
			clear(raw)
		}
	}()
	var response struct {
		Thread struct {
			ID    string            `json:"id"`
			Turns []json.RawMessage `json:"turns"`
		} `json:"thread"`
	}
	if len(answers) != 1 || json.Unmarshal(answers[0], &response) != nil || response.Thread.ID != id || len(response.Thread.Turns) != 0 {
		t.Fatal("metadata response mismatch")
	}
	t.Log("normal_history_transport=verified metadata_only=true protected_packet_body_read=0")
}
func TestUnslopHistoryInputDoesNotPermitGeneration(t *testing.T) {
	for _, method := range []string{"thread/read", "thread/turns/list", "thread/items/read", "turn/start", "thread/resume", "thread/start"} {
		raw, e := unslopHistoryInput([]unslopHistoryRequest{{method, map[string]any{"threadId": "owned"}}})
		allow := method == "thread/read" || method == "thread/turns/list" || method == "thread/items/read"
		if (e == nil) != allow {
			t.Fatal(method)
		}
		if allow && !strings.Contains(string(raw), `"method":"initialize"`) {
			t.Fatal("no handshake")
		}
	}
}

func TestUnslopHistoryProtocolWaitsForMatchingResponseBeforeNextStep(t *testing.T) {
	plan, e := buildUnslopHistoryProtocol([]unslopHistoryRequest{{"thread/read", map[string]any{"threadId": "owned", "includeTurns": false}}})
	if e != nil || len(plan.Steps) != 2 || plan.Steps[0].ResponseID != 1 || plan.Steps[1].ResponseID != 2 {
		t.Fatal("protocol shape")
	}
	if strings.Contains(string(plan.Steps[0].Input), "thread/read") || !strings.Contains(string(plan.Steps[1].Input), "initialized") {
		t.Fatal("requests precede initialization response")
	}
	for _, mode := range []string{"normal", "unexpected_id", "server_request", "error", "null"} {
		t.Run(mode, func(t *testing.T) {
			raw := []byte(`{"id":1,"result":{}}`)
			switch mode {
			case "unexpected_id":
				raw = []byte(`{"id":2,"result":{}}`)
			case "server_request":
				raw = []byte(`{"id":1,"method":"item/requestApproval","params":{}}`)
			case "error":
				raw = []byte(`{"id":1,"error":{"code":-1}}`)
			case "null":
				raw = []byte(`{"id":1,"result":null}`)
			}
			_, e := acceptUnslopHistoryProtocolFrame(raw, 1)
			if (e == nil) != (mode == "normal") {
				t.Fatal("protocol accepted invalid transition")
			}
		})
	}
}

func TestUnslopHistoryResponseRejectsAmbiguousOrUnsupportedMembers(t *testing.T) {
	for _,wire:=range []string{
		"{\"id\":9,\"id\":1,\"result\":{}}",
		"{\"ID\":1,\"result\":{}}",
		"{\"id\":1,\"result\":{},\"result\":{}}",
		"{\"id\":1,\"result\":true}",
		"{\"id\":1,\"result\":[]}",
		"{\"id\":1,\"result\":{},\"method\":\"\"}",
		"{\"id\":1,\"result\":{},\"unknown\":\"private\"}",
		"{\"id\":1,\"result\":{},\"jsonrpc\":\"1.0\"}",
		"{\"id\":1,\"result\":{}}{\"id\":2,\"result\":{}}",
	} {
		if result,err:=acceptUnslopHistoryProtocolFrame([]byte(wire),1);err==nil||result!=nil {clear(result);t.Fatal("ambiguous frame became a matching response")}
	}
	result,err:=acceptUnslopHistoryProtocolFrame([]byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{},\"error\":null}"),1)
	defer clear(result)
	if err!=nil||string(result)!="{}" {t.Fatal("supported response envelope was rejected",err)}
}

func TestUnslopNormalHistorySelectorsRemainUnconnected(t *testing.T) {
	for _,threads:=range [][2]string{{"",""},{"synthetic-owned",""},{"synthetic-owned","synthetic-owned"}} {
		result:=normalUnslopHistoryInspection(context.Background(),threads)
		if result.Reason!="normal_history_binding_unavailable"||result.ThreadsMatched!=0||result.CompletedTurns!=0 {t.Fatal("unissued selectors reached history runtime",result)}
	}
}
