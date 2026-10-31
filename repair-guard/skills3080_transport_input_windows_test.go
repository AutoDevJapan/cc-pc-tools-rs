package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func skillsTransportRequestFixture() (object, []byte) {
	raw, _, _, _, _ := skillsSourcePrefixFixture()
	input := []object{}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		var r object
		if json.Unmarshal(line, &r) == nil && r["type"] == "response_item" {
			input = append(input, obj(r["payload"]))
		}
	}
	prompt := []byte("normal registered task\n日本語")
	input = append(input, object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": string(prompt)}}})
	return object{"model": "gpt-6-luna", "reasoning": object{"effort": "max", "summary": "auto", "context": "all_turns"}, "input": input, "tool_choice": "auto", "store": false, "stream": true}, prompt
}

func TestSkills3080TransportInputRequiresCompleteInitialContext(t *testing.T) {
	for _, kind := range []string{"http_or_logical", "full_ws", "wrong_model", "other_provider_model", "wrong_effort", "wrong_task", "unknown_field", "header", "previous_response", "warmup", "unknown_type", "assistant", "tool_result", "third_user", "late_developer", "wrong_role", "missing_tools", "primitive_tool", "reordered_tools", "attachment", "aliased_block", "null_text", "unknown_item_field", "wrong_id_type", "unknown_reasoning", "unknown_context", "second_instruction_channel", "second_tool_channel"} {
		t.Run(kind, func(t *testing.T) {
			r, prompt := skillsTransportRequestFixture()
			input := r["input"].([]object)
			switch kind {
			case "full_ws":
				r["type"], r["generate"] = "response.create", true
			case "wrong_model":
				r["model"] = "gpt-6.1-sol"
			case "other_provider_model":
				r["model"] = "other-provider/gpt-6-luna"
			case "wrong_effort":
				obj(r["reasoning"])["effort"] = "low"
			case "wrong_task":
				prompt = []byte("different registered input")
			case "unknown_field":
				r["extra_instructions"] = "unregistered"
			case "header":
				r["headers"] = object{"synthetic": "not a request body"}
			case "previous_response":
				r["previous_response_id"] = "response_synthetic"
			case "warmup":
				r["type"], r["generate"] = "response.create", false
			case "unknown_type":
				r["type"] = "other"
			case "assistant", "tool_result":
				input = append(input[:len(input)-1], object{"type": "message", "role": "assistant", "content": []object{{"type": "output_text", "text": "not initial source"}}}, input[len(input)-1])
				if kind == "tool_result" {
					input[len(input)-2] = object{"type": "function_call_output", "call_id": "call_synthetic", "output": "not an input source"}
				}
			case "third_user":
				input = append(input, input[len(input)-1])
			case "late_developer":
				input = append(input, input[1])
			case "wrong_role":
				input[1]["role"] = "system"
			case "missing_tools":
				input = input[1:]
			case "primitive_tool":
				input[0]["tools"] = []any{"not a tool schema object"}
			case "reordered_tools":
				input[0], input[1] = input[1], input[0]
			case "attachment":
				input[len(input)-1]["content"] = []object{{"type": "input_image", "image_url": "synthetic"}}
			case "aliased_block":
				input[len(input)-1]["content"] = []object{{"type": "input_text", "text": string(prompt), "Text": "different"}}
			case "null_text":
				input[len(input)-1]["content"] = []object{{"type": "input_text", "text": nil}}
			case "unknown_item_field":
				input[1]["other"] = "unregistered"
			case "wrong_id_type":
				input[1]["id"] = object{"unexpected": true}
			case "unknown_reasoning":
				obj(r["reasoning"])["other"] = true
			case "unknown_context":
				obj(r["reasoning"])["context"] = "unregistered_mode"
			case "second_instruction_channel":
				r["instructions"] = "unregistered instruction channel"
			case "second_tool_channel":
				r["tools"] = []object{{"type": "function", "name": "unregistered"}}
			}
			r["input"] = input
			b, _ := json.Marshal(r)
			_, ok := skillsTransportInputProjection(b, prompt, "gpt-6-luna", "max")
			if ok != (kind == "http_or_logical" || kind == "full_ws") {
				t.Fatalf("initial input %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080TransportProjectionPreservesContentMetadataAndOrder(t *testing.T) {
	base, prompt := skillsTransportRequestFixture()
	raw, _ := json.Marshal(base)
	original, ok := skillsTransportInputProjection(raw, prompt, "gpt-6-luna", "max")
	if !ok {
		t.Fatal("normal initial source")
	}
	for _, kind := range []string{"generated_id", "developer_text", "tool_schema", "block_order", "message_order", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			r, _ := skillsTransportRequestFixture()
			items := r["input"].([]object)
			switch kind {
			case "generated_id":
				items[1]["id"] = "msg_regenerated_transport_id"
			case "developer_text":
				obj(items[1]["content"].([]any)[0])["text"] = "different launch instructions"
			case "tool_schema":
				obj(items[0]["tools"].([]any)[0])["description"] = "different schema description"
			case "block_order":
				blocks := items[1]["content"].([]any)
				blocks[0], blocks[1] = blocks[1], blocks[0]
			case "message_order":
				// Two developer messages are valid input, but their order must
				// still differ from the controller's recorded source.
				extra := object{"type": "message", "role": "developer", "content": []object{{"type": "input_text", "text": "another runtime fragment"}}}
				items = append(items[:2], append([]object{extra}, items[2:]...)...)
			case "metadata":
				items[1]["internal_chat_message_metadata_passthrough"] = object{"turn_id": strings.Repeat("2", 36)}
			}
			r["input"] = items
			b, _ := json.Marshal(r)
			view, ok := skillsTransportInputProjection(b, prompt, "gpt-6-luna", "max")
			if !ok {
				t.Fatal("source shape unexpectedly rejected")
			}
			a, _ := json.Marshal(original.Items)
			z, _ := json.Marshal(view.Items)
			if bytes.Equal(a, z) != (kind == "generated_id") {
				t.Fatalf("content or order was discarded for %s", kind)
			}
		})
	}
}

func TestSkills3080TransportBindsKnownRegisteredModelFamily(t *testing.T) {
	for _, c := range []struct {
		registered, actual string
		valid              bool
	}{
		{"gpt-6-luna", "gpt-6-luna", true},
		{"openai/gpt-6-luna", "gpt-6-luna", true},
		{"main/gpt-6.1-sol", "gpt-6.1-sol", true},
		{"pabcdef/gpt-6-astra", "gpt-6-astra", true},
		{"gpt-6-luna", "other-provider/gpt-6-luna", false},
		{"gpt-6-luna", "openai/gpt-6-luna", false},
		{"anthropic/gpt-6-luna", "gpt-6-luna", false},
		{"other-provider/gpt-6-luna", "gpt-6-luna", false},
		{"openai/nested/gpt-6-luna", "gpt-6-luna", false},
		{"pABCDEF/gpt-6-luna", "gpt-6-luna", false},
		{"gpt-6-luna", "gpt-6.1-sol", false},
	} {
		if skillsTransportModelMatches(c.actual, c.registered) != c.valid {
			t.Fatalf("registered %q transport %q", c.registered, c.actual)
		}
	}
}
