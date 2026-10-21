package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentResultCLIRecordsInitializationFailureWithoutChildOrCredentials(t *testing.T) {
	r, _ := fixture(t)
	t.Setenv("GODD_DISCORD_BOT_TOKEN", "")
	r.Config.Since = r.State.Since
	r.Config.NotifyConfig = filepath.Join(r.Config.Home, "destination.json")
	config := filepath.Join(r.Config.Home, "config.json")
	if publish(r.Config.NotifyConfig, r.State.Destination) != nil || publish(config, r.Config) != nil || r.save() != nil {
		t.Fatal("fixture")
	}
	e := resultEvent()
	e.Source = "launcher"
	e.Child = ""
	e.Parent = ""
	e.State = "start_failed"
	e.Reason = "initialization_rejected"
	e.PrivateRef = filepath.Join(r.Config.Home, "private-reference.json")
	input := filepath.Join(r.Config.Home, "input.json")
	if publish(input, e) != nil {
		t.Fatal("fixture")
	}
	var out bytes.Buffer
	code := agentResultCLI([]string{"--config", config, "record", "--input", input}, &out)
	var p map[string]any
	if code != 0 || json.Unmarshal(out.Bytes(), &p) != nil || p["delivery"] != "pending" || p["work_completed"] != false || p["parent_received_at"] == "" {
		t.Fatalf("CLI success was confused with work/delivery success: %d %s", code, out.String())
	}
	if strings.Contains(out.String(), e.PrivateRef) {
		t.Fatal("private reference printed")
	}
	out.Reset()
	if agentResultCLI([]string{"--config", config, "status", "--id", p["record_id"].(string)}, &out) != 0 {
		t.Fatal("durable parent receipt cannot be retrieved")
	}
	if !strings.Contains(out.String(), `"delivery":"pending"`) {
		t.Fatal("stored parent result lost")
	}
}
