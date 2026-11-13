//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

type unslopPacketOrigin struct {
	ID    string
	Cases []unslopCaseOrigin
}

type unslopCaseOrigin struct {
	ID, RequestSHA, ResponseSHA, Producer, ProducerIdentity, Thread string
	Turn, CommandItem                                               string
	Exit                                                            int
	RequestIdentity, ResponseIdentity                               softwareIdentity
}

type unslopCasePlan struct {
	id, request, response, requestSHA, responseSHA string
}

func unslopFixedCases() []unslopCasePlan {
	// Fixed opaque leaf names are resolved only by the admitted native packet
	// reader. Public source does not contain private runtime thread IDs/paths.
	return []unslopCasePlan{
		{"close-subordinate", "register-close-subordinate.request.private.json", "register-close-subordinate.response.private.json", "07f5b07dc12750da6c06620b1e89caf5ee153fd5e3d98c86ae3785984af7fa9c", "9233fa1fcc03e06e32a40495524f748c37df5e96f57e58213ceac992b4a0c1f0"},
		{"formal-superior", "register-formal-superior.request.private.json", "register-formal-superior.response.private.json", "08566aeea1dc7905427995758f0671849f32719c63e6edb177cbc6f1641c72cc", "721f5abfef9b1179978c0849fd885de650557a95fc8660d7ff153bddd996e7ba"},
	}
}

type unslopPacketAccess struct {
	origin unslopOriginProvider
	// The implementation accepts these four opaque leaves only; it must bind
	// them to the original producer's held objects before opening any body.
	open func(string) (updateObject, error)
}

type unslopEvaluationCase struct {
	ID        string          `json:"id"`
	Original  string          `json:"original"`
	Context   json.RawMessage `json:"context"`
	Rewritten string          `json:"rewritten_text"`
}

type unslopPacketResult struct {
	Qualified             bool                   `json:"qualified"`
	Reason                string                 `json:"reason"`
	Cases                 []unslopEvaluationCase `json:"cases,omitempty"`
	IndependentlyAssessed bool                   `json:"independently_assessed"`
	GeneralAccessGranted  bool                   `json:"general_access_granted"`
}

func consumeUnslopPacket(ctx context.Context, access unslopPacketAccess) unslopPacketResult {
	return consumeUnslopCases(ctx, unslopFixedCases(), access)
}

func consumeUnslopCases(ctx context.Context, plans []unslopCasePlan, access unslopPacketAccess) (result unslopPacketResult) {
	result.Reason = "unslop_origin_provider_unavailable"
	if ctx.Err() != nil {
		result.Reason = "unslop_cancelled"
		return result
	}
	if access.origin == nil {
		return result
	}
	proof, err := access.origin.Packet(ctx)
	if err != nil {
		return result
	}
	if proof.ID != "godd-unslop-register-evaluation-v1" || len(proof.Cases) != 2 || len(plans) != 2 {
		result.Reason = "unslop_packet_origin_mismatch"
		return result
	}
	seen := map[string]bool{}
	validID := func(id softwareIdentity) bool { return id.Volume != "" && id.FileID != "" && id.Created != "" }
	for i, p := range plans {
		c := proof.Cases[i]
		if seen[c.ID] || c.ID != p.id || c.RequestSHA != p.requestSHA || c.ResponseSHA != p.responseSHA || c.Thread == "" || c.Turn == "" || c.CommandItem == "" || c.Exit != 0 || c.Producer != unslopInstalledSHA || c.ProducerIdentity == "" || !validID(c.RequestIdentity) || !validID(c.ResponseIdentity) {
			result.Reason = "unslop_packet_origin_mismatch"
			return result
		}
		seen[c.ID] = true
	}
	if ctx.Err() != nil || access.open == nil {
		result.Reason = "unslop_packet_reader_unavailable"
		return result
	}
	var held []updateObject
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			if held[i].Close() != nil {
				result.Qualified = false
				result.Cases = nil
				result.Reason = "unslop_close_failed"
			}
		}
	}()
	var cases []unslopEvaluationCase
	for i, p := range plans {
		var pair [2]updateObject
		for n, path := range []string{p.request, p.response} {
			if ctx.Err() != nil {
				result.Reason = "unslop_cancelled"
				return result
			}
			o, err := access.open(path)
			if err != nil {
				result.Reason = "unslop_packet_read_failed"
				return result
			}
			held = append(held, o)
			pair[n] = o
		}
		c := proof.Cases[i]
		if pair[0].ID() != c.RequestIdentity || pair[1].ID() != c.ResponseIdentity || len(pair[0].Raw()) > 2<<20 || len(pair[1].Raw()) > 2<<20 || digest(pair[0].Raw()) != p.requestSHA || digest(pair[1].Raw()) != p.responseSHA {
			result.Reason = "unslop_packet_content_mismatch"
			return result
		}
		var request struct {
			Text    string          `json:"text"`
			Context json.RawMessage `json:"context,omitempty"`
		}
		d := json.NewDecoder(bytes.NewReader(pair[0].Raw()))
		d.DisallowUnknownFields()
		if d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF || request.Text == "" {
			result.Reason = "unslop_packet_input_invalid"
			return result
		}
		var response struct {
			Status, Backend, Original string
			Rewritten                 string `json:"rewritten_text"`
		}
		if json.Unmarshal(pair[1].Raw(), &response) != nil || response.Status != "rewritten" || response.Backend != "codex_subscription" || response.Original != request.Text || response.Rewritten == "" {
			result.Reason = "unslop_packet_response_invalid"
			return result
		}
		cases = append(cases, unslopEvaluationCase{ID: p.id, Original: request.Text, Context: append(json.RawMessage(nil), request.Context...), Rewritten: response.Rewritten})
	}
	if ctx.Err() != nil {
		result.Reason = "unslop_cancelled"
		return result
	}
	result.Qualified = true
	result.Reason = "verified_same_buffer_evaluation_inputs"
	result.Cases = cases
	return result
}

// Only the documented, read-only metadata RPCs are used here. History without
// the original producer receipt cannot authorize reading old packet bodies.
type unslopHistoryRPC interface {
	Call(context.Context, string, any) (json.RawMessage, error)
}

type unslopOriginInspection struct {
	Reason                 string `json:"reason"`
	ThreadsMatched         int    `json:"threads_matched"`
	CompletedTurns         int    `json:"completed_turns"`
	OriginalFilesQualified bool   `json:"original_files_qualified"`
	BodyReads              int    `json:"body_reads"`
}

type unslopPurposeRuntime struct {
	capture        unslopCaptureAccess
	packet         unslopPacketAccess
	history        unslopHistoryRPC
	historyThreads [2]string
}

// The common dispatcher owns installation of the normal provider factory.
// Zero dependencies are intentionally a visible non-success diagnostic.
func runUnslopPurpose(ctx context.Context, args []string, out io.Writer, runtime unslopPurposeRuntime) int {
	if len(args) != 1 {
		return 2
	}
	var value any
	qualified := false
	switch args[0] {
	case "--capture-unslop-release":
		r := captureUnslopRelease(ctx, runtime.capture)
		value = r
		qualified = r.Qualified
	case "--verify-unslop-update", "--prepare-unslop-update":
		r := executeUnslopUpdate(ctx, args[0] == "--prepare-unslop-update", unslopUpdateAccess{})
		value = r
	case "--inspect-unslop-evaluation-origin":
		value = inspectUnslopEvaluationOrigin(ctx, runtime.history, runtime.historyThreads)
	case "--read-unslop-evaluation-packet":
		r := consumeUnslopPacket(ctx, runtime.packet)
		value = r
		qualified = r.Qualified
	default:
		return 2
	}
	if json.NewEncoder(out).Encode(value) != nil || !qualified {
		return 1
	}
	return 0
}

func inspectUnslopEvaluationOrigin(ctx context.Context, rpc unslopHistoryRPC, threads [2]string) unslopOriginInspection {
	r := unslopOriginInspection{Reason: "normal_history_provider_unavailable"}
	if ctx.Err() != nil {
		r.Reason = "unslop_cancelled"
		return r
	}
	if rpc == nil {
		return r
	}
	if threads[0] == "" || threads[1] == "" || threads[0] == threads[1] {
		r.Reason = "normal_history_binding_unavailable"
		return r
	}
	for _, thread := range threads {
		if ctx.Err() != nil {
			r.Reason = "unslop_cancelled"
			return r
		}
		raw, e := rpc.Call(ctx, "thread/read", map[string]any{"threadId": thread, "includeTurns": false})
		if e != nil {
			return r
		}
		var page struct {
			Thread struct {
				ID     string            `json:"id"`
				Source string            `json:"source"`
				Turns  []json.RawMessage `json:"turns"`
			} `json:"thread"`
		}
		err := json.Unmarshal(raw, &page)
		clear(raw)
		if err != nil || page.Thread.ID != thread || (page.Thread.Source != "cli" && page.Thread.Source != "vscode") || len(page.Thread.Turns) != 0 {
			r.Reason = "normal_history_thread_mismatch"
			return r
		}
		r.ThreadsMatched++
		cursor := ""
		seen := map[string]bool{}
		complete := 0
		for pages := 0; pages < 8; pages++ {
			p := map[string]any{"threadId": thread, "itemsView": "notLoaded", "limit": 20, "sortDirection": "desc"}
			if cursor != "" {
				p["cursor"] = cursor
			}
			raw, e = rpc.Call(ctx, "thread/turns/list", p)
			if e != nil || ctx.Err() != nil {
				clear(raw)
				return r
			}
			var turns struct {
				Data []struct {
					ID, Status, ItemsView string
					Items                 []json.RawMessage
				} `json:"data"`
				NextCursor *string `json:"nextCursor"`
			}
			err = json.Unmarshal(raw, &turns)
			clear(raw)
			if err != nil || len(turns.Data) > 20 {
				r.Reason = "normal_history_turn_mismatch"
				return r
			}
			for _, turn := range turns.Data {
				if turn.ID == "" || turn.ItemsView != "notLoaded" || len(turn.Items) != 0 {
					r.Reason = "normal_history_turn_mismatch"
					return r
				}
				if turn.Status == "completed" {
					complete++
				}
			}
			if turns.NextCursor == nil || *turns.NextCursor == "" {
				break
			}
			cursor = *turns.NextCursor
			if seen[cursor] || pages == 7 {
				r.Reason = "normal_history_pagination_unqualified"
				return r
			}
			seen[cursor] = true
		}
		if complete == 0 {
			r.Reason = "normal_history_completed_turn_unavailable"
			return r
		}
		r.CompletedTurns += complete
	}
	// A finished turn does not establish which executable created the fixed
	// files, their original handles, or their request/response digests.
	r.Reason = "original_producer_binding_unavailable"
	return r
}
