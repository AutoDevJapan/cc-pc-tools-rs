//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestUnslopPurposeRoutesDurableCaptureAndSaveFailureJSON(t *testing.T) {
	for _,mode:=range []string{"normal","late-provider-close","save-failure","nil-controller"} {
		t.Run(mode,func(t *testing.T){
			store:=newDurableTestStore(t)
			controller,session,pending,_:=newDurableTestCapture(t,store,mode)
			if mode=="save-failure" {durableTestFailPhase(store,"final")}
			runtime:=unslopPurposeRuntime{durableCapture:controller}
			if mode=="nil-controller" {runtime.durableCapture=nil}
			var out bytes.Buffer
			code:=runUnslopPurpose(context.Background(),[]string{"--capture-unslop-release"},&out,runtime)
			var value map[string]any
			if json.Unmarshal(out.Bytes(),&value)!=nil {t.Fatal("capture result was not returned as JSON",out.String())}
			want:=1;if mode=="normal" {want=0}
			if code!=want {t.Fatal("durable route exit contract",mode,code,out.String())}
			if mode=="nil-controller" {if value["reason"]!="missing_producer_provenance"||session.builds!=0||pending.commits!=0 {t.Fatal("nil durable controller activated capture")};return}
			if value["generation"]==""||value["receipt_published"]!=true||value["result_saved"]!=(mode!="save-failure")||value["qualified"]!=(mode=="normal") {t.Fatal("durable state did not reach CLI consumer",mode,out.String())}
			if mode=="late-provider-close"&&value["toolchain_cleanup_failed"]!=true {t.Fatal("CLI erased late provider cleanup")}
			if mode!="normal" {if _,ok:=value["receipt"];ok {t.Fatal("failure JSON returned a usable receipt")}}
			if session.builds!=1||pending.commits!=1||strings.Contains(out.String(),"private") {t.Fatal("route repeated capture or exposed error detail")}
		})
	}
}

func TestUnslopPacketProjectsOnlyBoundInputsAndGeneratedText(t *testing.T) {
	for _, fault := range []string{"normal", "sensitive_context", "missing_provider", "wrong_origin", "duplicate_case", "failed_generation", "missing_thread", "wrong_input_sha", "changed_response", "same_hash_new_identity", "reader_failure", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			plans := unslopFixedCases()
			data := map[string]*fakeUpdateObject{}
			origin := unslopPacketOrigin{ID: "godd-unslop-register-evaluation-v1"}
			for i := range plans {
				p := &plans[i]
				request := []byte(`{"text":"保存しました。","context":{"channel":"chat"}}`)
				if fault == "sensitive_context" {
					request = []byte(`{"text":"保存しました。","context":{"channel":"chat","recipient":{"relation":"superior","closeness":"normal","address":"sensitive-identity"},"company_style":"sensitive-house-style","access_token":"sensitive-token","tone":{"warmth":3,"emoji":"none"}}}`)
				}
				response := []byte(`{"status":"rewritten","backend":"codex_subscription","original":"保存しました。","rewritten_text":"保存しました。","semantic_equality":"not_established","private_internal":"synthetic-secret"}`)
				p.requestSHA, p.responseSHA = digest(request), digest(response)
				qid, rid := softwareIdentity{"v", p.id + "q", "c"}, softwareIdentity{"v", p.id + "r", "c"}
				data[p.request] = &fakeUpdateObject{raw: request, id: qid}
				data[p.response] = &fakeUpdateObject{raw: response, id: rid}
				origin.Cases = append(origin.Cases, unslopCaseOrigin{ID: p.id, Thread: "synthetic-thread-" + p.id, Turn: "synthetic-turn", CommandItem: "synthetic-command", RequestSHA: p.requestSHA, ResponseSHA: p.responseSHA, Producer: unslopInstalledSHA, ProducerIdentity: "verified-original-generation", RequestIdentity: qid, ResponseIdentity: rid})
			}
			switch fault {
			case "wrong_origin":
				origin.ID = "caller-origin"
			case "duplicate_case":
				origin.Cases[1] = origin.Cases[0]
			case "failed_generation":
				origin.Cases[0].Exit = 1
			case "missing_thread":
				origin.Cases[0].Thread = ""
			case "wrong_input_sha":
				origin.Cases[0].RequestSHA = strings.Repeat("0", 64)
			case "changed_response":
				data[plans[1].response].raw = []byte("changed")
			case "same_hash_new_identity":
				data[plans[1].response].id.FileID = "replacement"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "cancelled" {
				cancel()
			}
			reads := 0
			var opened []*fakeUpdateObject
			access := unslopPacketAccess{origin: unslopTestProvider{packet: origin}, open: func(path string) (updateObject, error) {
				reads++
				if fault == "reader_failure" && path == plans[1].response {
					return nil, errUnslopOrigin
				}
				o, ok := data[path]
				if !ok {
					t.Fatal("outside packet")
				}
				opened = append(opened, o)
				return o, nil
			}}
			if fault == "missing_provider" {
				access.origin = nil
			}
			result := consumeUnslopCases(ctx, plans, access)
			if fault == "normal" || fault == "sensitive_context" {
				if !result.Qualified || len(result.Cases) != 2 || reads != 4 {
					t.Fatal(result)
				}
				b, _ := json.Marshal(result)
				if strings.Contains(string(b), "synthetic-secret") || strings.Contains(string(b), "private_internal") || strings.Contains(string(b), "sensitive-") {
					t.Fatal("runtime content forwarded")
				}
				if fault == "sensitive_context" && !strings.Contains(string(b), `"relation":"superior"`) {
					t.Fatal("required style context omitted")
				}
			} else if result.Qualified || len(result.Cases) != 0 {
				t.Fatal("partial or failed packet leaked", result)
			}
			if fault == "missing_provider" || fault == "wrong_origin" || fault == "duplicate_case" || fault == "failed_generation" || fault == "missing_thread" || fault == "wrong_input_sha" || fault == "cancelled" {
				if reads != 0 {
					t.Fatal("invalid origin opened body")
				}
			}
			for _, o := range opened {
				if !o.closed {
					t.Fatal("held object leaked")
				}
			}
			if result.GeneralAccessGranted || result.IndependentlyAssessed {
				t.Fatal("reader invented authorization or assessment")
			}
		})
	}
}

func TestUnslopPurposeReportsMissingNativeProviderToConsumer(t *testing.T) {
	for flag, want := range map[string]string{"--capture-unslop-release": "missing_producer_provenance", "--verify-unslop-update": "missing_compiled_input_provenance", "--prepare-unslop-update": "missing_compiled_input_provenance", "--inspect-unslop-evaluation-origin": "normal_history_provider_unavailable", "--read-unslop-evaluation-packet": "unslop_origin_provider_unavailable"} {
		t.Run(flag, func(t *testing.T) {
			var out bytes.Buffer
			code := runUnslopPurpose(context.Background(), []string{flag}, &out, unslopPurposeRuntime{})
			var got map[string]any
			if code != 1 || json.Unmarshal(out.Bytes(), &got) != nil || got["reason"] != want || got["qualified"] == true {
				t.Fatal(code, out.String())
			}
			if strings.Contains(out.String(), "C:/") || strings.Contains(out.String(), "private.json") {
				t.Fatal("diagnostic disclosed internal path")
			}
		})
	}
	for _, args := range [][]string{nil, {"--capture-unslop-release", "--path", "untrusted"}, {"--unknown"}} {
		var out bytes.Buffer
		if runUnslopPurpose(context.Background(), args, &out, unslopPurposeRuntime{}) != 2 || out.Len() != 0 {
			t.Fatal("caller proof arguments accepted")
		}
	}
}

type syntheticUnslopHistory struct {
	fault string
	calls []string
}

func (s *syntheticUnslopHistory) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	s.calls = append(s.calls, method)
	p := params.(map[string]any)
	id := p["threadId"].(string)
	if s.fault == "api_error" {
		return nil, errUnslopOrigin
	}
	if method == "thread/read" {
		if s.fault == "wrong_thread" {
			id = "other-thread"
		}
		return json.Marshal(map[string]any{"thread": map[string]any{"id": id, "source": "cli", "turns": []any{}}})
	}
	if method != "thread/turns/list" {
		panic("history inspector requested body")
	}
	status := "completed"
	if s.fault == "failed_turn" {
		status = "failed"
	}
	page := map[string]any{"data": []any{map[string]any{"id": "synthetic-generation", "status": status, "itemsView": "notLoaded", "items": []any{}}}, "nextCursor": nil}
	if s.fault == "cyclic_cursor" {
		page["nextCursor"] = "same-cursor"
	}
	if s.fault == "loaded_items" {
		page["data"] = []any{map[string]any{"id": "synthetic-generation", "status": "completed", "itemsView": "full", "items": []any{map[string]any{"secret": "synthetic-body"}}}}
	}
	return json.Marshal(page)
}

func TestUnslopHistoryMetadataDoesNotInventPacketAuthority(t *testing.T) {
	for _, fault := range []string{"normal", "missing_provider", "wrong_thread", "failed_turn", "api_error", "cyclic_cursor", "loaded_items", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			s := &syntheticUnslopHistory{fault: fault}
			var rpc unslopHistoryRPC = s
			if fault == "missing_provider" {
				rpc = nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "cancelled" {
				cancel()
			}
			r := inspectUnslopEvaluationOrigin(ctx, rpc, [2]string{"synthetic-subordinate", "synthetic-superior"})
			if r.OriginalFilesQualified || r.BodyReads != 0 {
				t.Fatal("metadata elevated to body authority", r)
			}
			if fault == "normal" && (r.Reason != "original_producer_binding_unavailable" || r.ThreadsMatched != 2 || r.CompletedTurns != 2) {
				t.Fatal(r)
			}
			if fault != "normal" && r.Reason == "original_producer_binding_unavailable" {
				t.Fatal("invalid metadata accepted", r)
			}
			for _, call := range s.calls {
				if call != "thread/read" && call != "thread/turns/list" {
					t.Fatal("body RPC reached")
				}
			}
		})
	}
}

func TestUnslopContextDoesNotTurnNullIntoToneZero(t *testing.T) {
	for _, name := range []string{"formality", "warmth", "directness"} {
		t.Run(name, func(t *testing.T) {
			if _, _, valid := unslopEvaluationContext(json.RawMessage(`{"tone":{"` + name + `":null}}`)); valid {
				t.Fatal("null interpreted as explicit tone")
			}
			raw, omitted, valid := unslopEvaluationContext(json.RawMessage(`{"tone":{"` + name + `":0}}`))
			if !valid || omitted || !strings.Contains(string(raw), `":0`) {
				t.Fatal("explicit zero rejected")
			}
		})
	}
}
