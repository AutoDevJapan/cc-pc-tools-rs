package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func managementScope(domain, since, until string) map[string]any {
	return map[string]any{"domain": domain, "todo_id": 3387, "guild_id": monthlyTestGuild, "owner_id": monthlyTestOwner, "since": since, "until": until}
}

func TestManagementWindowsUseExistingInventory(t *testing.T) {
	for _, period := range []struct{ name, since, until string }{
		{"month", "2026-09-23T00:00:00+09:00", "2026-10-23T07:18:00+09:00"},
		{"delta", monthlyUntil, "2026-10-23T07:18:00+09:00"},
	} {
		t.Run(period.name, func(t *testing.T) {
			f := newMonthlyFixture(t)
			s := managementScope("discord-monthly-history-inventory-scope-v1", period.since, period.until)
			s["mode"] = "channels"
			args, path := monthlyTestArgs(t, s)
			var out bytes.Buffer
			if code := monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit); code != 0 {
				t.Fatalf("authorized new period unavailable: %d %s", code, out.String())
			}
			p, _ := monthlyTestReceipt(t, path)
			if p["todo_id"] != float64(3387) || p["since"] != period.since || p["until"] != period.until || p["source"] != "discord-api-v10" || len(p["channels"].([]any)) != 2 {
				t.Fatal("new receipt lost its independent scope or coverage")
			}
		})
	}
}

func TestManagementReadAndCursorStayBoundToTheirWindow(t *testing.T) {
	f := newMonthlyFixture(t)
	s := managementScope("discord-monthly-history-read-scope-v1", monthlyUntil, "2026-10-23T07:18:00+09:00")
	s["channel_ids"] = []string{"100", "400"}
	m := monthlyMessage("800", "100", monthlyTestOwner, "synthetic new instruction", false)
	m["timestamp"] = "2026-10-22T21:00:00Z"
	f.responses["/channels/100/messages"] = []any{m}
	args, path := monthlyTestArgs(t, s)
	var out bytes.Buffer
	if code := monthlyReadCLIWith(args, &out, f.load, monthlyPermit); code != 0 {
		t.Fatalf("authorized delta unavailable: %d %s", code, out.String())
	}
	p, original := monthlyTestReceipt(t, path)
	if p["todo_id"] != float64(3387) || len(p["messages"].([]any)) != 1 || p["next"] == nil {
		t.Fatal("new read lost identity, message, or continuation")
	}
	// Re-reading the same source uses GETs and a fresh private output only.
	replayArgs, replayPath := monthlyTestArgs(t, s)
	out.Reset()
	if monthlyReadCLIWith(replayArgs, &out, f.load, monthlyPermit) != 0 {
		t.Fatal("same source could not be inspected again", out.String())
	}
	_, replay := monthlyTestReceipt(t, replayPath)
	if !bytes.Equal(original, replay) {
		t.Fatal("same source/version and acquisition time changed its receipt")
	}
	// Neither another period nor the original audit may adopt this cursor.
	for _, replacement := range []map[string]any{
		managementScope("discord-monthly-history-read-scope-v1", monthlyUntil, "2026-10-23T07:19:00+09:00"),
		monthlyTestScope("discord-monthly-history-read-scope-v1"),
	} {
		replacement["channel_ids"] = []string{"100", "400"}
		changed, output := monthlyTestArgs(t, replacement)
		changed = append(changed, "--cursor", path)
		loads := f.loads
		out.Reset()
		if monthlyReadCLIWith(changed, &out, f.load, monthlyPermit) == 0 || f.loads != loads {
			t.Fatal("changed scope adopted another period's cursor")
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("rejected cursor published a receipt")
		}
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("previous successful receipt changed")
	}
	if strings.Contains(out.String(), "synthetic new instruction") {
		t.Fatal("diagnostics exposed selected content")
	}
}

func TestManagementInvalidWindowsStopBeforeCredentials(t *testing.T) {
	for _, period := range []struct{ name, since, until string }{
		{"before_owner_lower_bound", "2026-09-22T23:59:59+09:00", "2026-10-23T07:18:00+09:00"},
		{"more_than_31_days", "2026-09-23T00:00:00+09:00", "2026-10-24T00:00:01+09:00"},
		{"empty", monthlyUntil, monthlyUntil},
		{"reversed", "2026-10-23T07:18:00+09:00", monthlyUntil},
		{"future", monthlyUntil, "2999-01-01T00:00:00Z"},
		{"malformed_since", "invalid", "2026-10-23T07:18:00+09:00"},
		{"malformed_until", monthlyUntil, "invalid"},
	} {
		t.Run(period.name, func(t *testing.T) {
			f := newMonthlyFixture(t)
			s := managementScope("discord-monthly-history-inventory-scope-v1", period.since, period.until)
			s["mode"] = "channels"
			args, output := monthlyTestArgs(t, s)
			var out bytes.Buffer
			if monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit) == 0 || f.loads != 0 || len(f.paths) != 0 {
				t.Fatal("invalid management window loaded credentials or made a request")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("invalid management window published a receipt")
			}
		})
	}
}

func TestManagementAuthorityDoesNotBroadenOriginalAudit(t *testing.T) {
	for _, field := range []string{"todo_id", "guild_id", "owner_id", "since", "until", "domain"} {
		f := newMonthlyFixture(t)
		s := monthlyTestScope("discord-monthly-history-inventory-scope-v1")
		s["mode"] = "channels"
		if field == "since" {
			s[field] = "2026-09-23T00:00:00+09:00"
		} else if field == "until" {
			s[field] = "2026-10-23T07:18:00+09:00"
		} else if field == "todo_id" {
			s[field] = 3388
		} else {
			s[field] = "different"
		}
		args, output := monthlyTestArgs(t, s)
		var out bytes.Buffer
		if monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit) == 0 || f.loads != 0 {
			t.Fatalf("original audit authority broadened through %s", field)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid audit receipt")
		}
	}
}

func TestManagementWindowDurationAndEquivalentOffset(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	s := monthlyAuditScope{Domain: "discord-monthly-history-inventory-scope-v1", Todo: 3387, Guild: monthlyTestGuild, Owner: monthlyTestOwner, Since: "2026-09-22T15:00:00Z", Until: "2026-10-23T15:00:00Z"}
	if !s.validAt(s.Domain, now) {
		t.Fatal("exactly 31 days at the owner's lower bound must be readable")
	}
	s.Until = "2026-10-23T15:00:00.000000001Z"
	if s.validAt(s.Domain, now) {
		t.Fatal("over 31 days accepted even though neither bound is in the future")
	}
	s.Until = "2026-10-23T14:59:59Z"
	s.Since = "2026-09-22T14:59:59Z"
	if s.validAt(s.Domain, now) {
		t.Fatal("equivalent UTC offset bypassed the owner's lower bound")
	}
}
