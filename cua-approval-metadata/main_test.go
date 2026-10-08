package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const appRequest = `{"jsonrpc":"2.0","id":"app:9","method":"elicitation/create","params":{"message":"Allow Codex to use Google Chrome?","requestedSchema":{"type":"object","properties":{}},"_meta":{"codex_approval_kind":"mcp_tool_call","connector_id":"computer-use","connector_name":"Computer Use","persist":["session","always"],"riskLevel":"medium","tool_params":{"app":"Chrome"},"tool_params_display":[{"name":"app","display_name":"App","value":"Google Chrome"}]}}}`

func TestAppRequestPreservesEveryOtherField(t *testing.T) {
	repaired := normalize([]byte(appRequest + "\n"))
	if bytes.Equal(repaired, []byte(appRequest+"\n")) {
		t.Fatal("actual SDK app request wasn't repaired")
	}
	var before, after map[string]any
	_ = json.Unmarshal([]byte(appRequest), &before)
	_ = json.Unmarshal(repaired, &after)
	meta := after["params"].(map[string]any)["_meta"].(map[string]any)
	if meta["tool_name"] != appOperation("Chrome") {
		t.Fatalf("wrong operation: %v", meta["tool_name"])
	}
	delete(meta, "tool_name")
	a, _ := json.Marshal(after)
	b, _ := json.Marshal(before)
	if !bytes.Equal(a, b) {
		t.Fatalf("request changed beyond its missing operation name: %s", a)
	}
}

func TestUnrelatedAndAmbiguousMessagesAreUnchanged(t *testing.T) {
	cases := []string{
		`{"jsonrpc":"2.0","id":"app:9","result":{"action":"decline"}}`,
		`{"jsonrpc":"2.0","id":"app:9","result":{"action":"accept"}}`,
		strings.Replace(appRequest, `"computer-use"`, `"different-server"`, 1),
		strings.Replace(appRequest, `"mcp_tool_call"`, `"different-kind"`, 1),
		strings.Replace(appRequest, `"app":"Chrome"`, `"app":"computer-audio"`, 1),
		strings.Replace(appRequest, `"app":"Chrome"`, `"app":""`, 1),
		strings.Replace(appRequest, `"app":"Chrome"`, `"app":"Chrome","extra":true`, 1),
		strings.Replace(appRequest, `"message":"Allow Codex to use Google Chrome?"`, `"message":"Upload a file?"`, 1),
		strings.Replace(appRequest, `"connector_id":"computer-use"`, `"connector_id":"different","connector_id":"computer-use"`, 1),
		strings.Replace(appRequest, `"riskLevel":"medium"`, `"riskLevel":"medium","tool_name":"existing_operation"`, 1),
		strings.Replace(appRequest, `"riskLevel":"medium"`, `"riskLevel":"medium","tool_name":""`, 1),
		strings.Replace(appRequest, `"requestedSchema"`, `"mode":"url","requestedSchema"`, 1),
		appRequest + " {}",
		`{"method":"elicitation/create","params":null}`,
	}
	for _, c := range cases {
		if got := normalize([]byte(c)); !bytes.Equal(got, []byte(c)) {
			t.Fatalf("unrelated/ambiguous message was modified: %s", c)
		}
	}
}

func TestFramingAcrossChunks(t *testing.T) {
	unrelated := `{"method":"notifications/message","params":{"text":"秘密は記録しない"}}` + "\n"
	input := []byte(unrelated + appRequest + "\n" + unrelated)
	var output bytes.Buffer
	w := &messageWriter{destination: &output}
	for _, b := range input {
		if _, err := w.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	expect := unrelated + string(normalize([]byte(appRequest+"\n"))) + unrelated
	if output.String() != expect || len(w.pending) != 0 {
		t.Fatal("chunked frames were dropped or merged")
	}
}

func TestSubprocessDecisionsPassThrough(t *testing.T) {
	for _, decision := range []string{"decline", "accept", "cancel"} {
		t.Run(decision, func(t *testing.T) {
			t.Setenv("CUA_METADATA_TEST_HELPER", "1")
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			var output, stderr bytes.Buffer
			response := fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":\"app:9\",\"result\":{\"action\":%q}}\n", decision)
			code := run([]string{"--", exe, "-test.run=TestSDKHelper"}, strings.NewReader(response), &output, &stderr)
			if code != 0 {
				t.Fatalf("SDK exit %d: %s", code, stderr.String())
			}
			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			if len(lines) != 2 || !strings.Contains(lines[0], `"tool_name":"`+appOperation("Chrome")+`"`) || lines[1] != strings.TrimSpace(response) {
				t.Fatalf("original SDK decision was not passed through: %s", output.String())
			}
		})
	}
}

func TestOperationCacheKeysDoNotCrossApplications(t *testing.T) {
	if appOperation("Chrome") == appOperation("Notepad") {
		t.Fatal("different apps share a reviewer operation")
	}
	request := strings.Replace(appRequest, `"app":"Chrome"`, `"app":"Notepad"`, 1)
	var message map[string]any
	if err := json.Unmarshal(normalize([]byte(request)), &message); err != nil {
		t.Fatal(err)
	}
	if message["params"].(map[string]any)["_meta"].(map[string]any)["tool_name"] != appOperation("Notepad") {
		t.Fatal("repaired operation doesn't identify the requested app")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("output unavailable") }

func TestOutputFailureSignalsChildTermination(t *testing.T) {
	fatal := make(chan struct{}, 1)
	w := &messageWriter{destination: brokenWriter{}, fatal: fatal}
	if _, err := w.Write([]byte(appRequest + "\n")); err == nil {
		t.Fatal("output failure ignored")
	}
	select {
	case <-fatal:
	default:
		t.Fatal("child termination wasn't requested")
	}
}

func TestLimitAndIncompleteFrame(t *testing.T) {
	fatal := make(chan struct{}, 1)
	w := &messageWriter{destination: &bytes.Buffer{}, fatal: fatal}
	w.pending = make([]byte, maxMessage)
	if _, err := w.Write([]byte{'x'}); err == nil {
		t.Fatal("message limit not enforced")
	}
	select {
	case <-fatal:
	default:
		t.Fatal("oversized stream didn't request child termination")
	}
	var output bytes.Buffer
	w = &messageWriter{destination: &output}
	if _, err := w.Write([]byte(appRequest)); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 || !bytes.Equal(w.pending, []byte(appRequest)) {
		t.Fatal("incomplete frame was repaired or forwarded early")
	}
}

func TestSDKHelper(t *testing.T) {
	if os.Getenv("CUA_METADATA_TEST_HELPER") != "1" {
		return
	}
	fmt.Println(appRequest)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		os.Exit(3)
	}
	fmt.Print(line)
	os.Exit(0)
}
