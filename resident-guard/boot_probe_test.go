package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuthenticationStatusOnlyPublishesCategories(t *testing.T) {
	for _, test := range []struct {
		cli            string
		exit           int
		body, category string
	}{
		{"codex", 0, "Logged in using ChatGPT", "subscription_status_readable"},
		{"codex", 1, "Not logged in", "not_logged_in"},
		{"codex", 0, "Logged in using API key - SECRET-FIXTURE-VALUE", "api_key_route_detected"},
		{"codex", 1, "Logged in using ChatGPT", "unconfirmed"},
		{"codex", 1, "C:/private/path SECRET-FIXTURE-VALUE", "unconfirmed"},
		{"claude", 0, `{"loggedIn":true,"authMethod":"claude.ai","email":"SECRET-FIXTURE-VALUE","orgName":"private"}`, "subscription_status_readable"},
		{"claude", 0, `{"loggedIn":true,"authMethod":"oauth"}`, "subscription_status_readable"},
		{"claude", 0, `{"loggedIn":true,"authMethod":"api_key","token":"SECRET-FIXTURE-VALUE"}`, "api_key_route_detected"},
		{"claude", 1, `{"loggedIn":false}`, "not_logged_in"},
		{"claude", 0, `{"loggedIn":true,"authMethod":"unknown"}`, "unconfirmed"},
		{"claude", 0, `{}`, "unconfirmed"},
		{"claude", 0, `not-json SECRET-FIXTURE-VALUE`, "unconfirmed"},
	} {
		out := classifyCLIAuth(test.cli, test.exit, []byte(test.body))
		data, _ := json.Marshal(out)
		if out.Category != test.category || strings.Contains(string(data), "SECRET-FIXTURE-VALUE") || strings.Contains(string(data), "private") || strings.Contains(string(data), "email") {
			t.Fatalf("wrong state or raw credential/profile output: %s", data)
		}
	}
}
