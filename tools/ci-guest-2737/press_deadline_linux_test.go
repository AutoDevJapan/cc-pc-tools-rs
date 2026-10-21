package main

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// Exercise the accepted control-frame path, then deliberately reject the empty
// source frame before touching a filesystem, process, runner or product source.
type pressDeadlineConn struct {
	net.Conn
	bytes.Buffer
	deadline time.Time
}

func (c *pressDeadlineConn) Write(b []byte) (int, error)      { return c.Buffer.Write(b) }
func (c *pressDeadlineConn) Read(b []byte) (int, error)       { return c.Buffer.Read(b) }
func (c *pressDeadlineConn) SetDeadline(d time.Time) error    { c.deadline = d; return nil }
func (c *pressDeadlineConn) SetWriteDeadline(time.Time) error { return nil }

func TestPressLinuxAcceptedPlanExtendsStreamDeadline(t *testing.T) {
	for _, tc := range []struct {
		workflow, role string
		minutes        int
	}{
		{"local-checks.yml", "rust-core", 90},
		{"local-checks.yml", "rust-integration-1", 90},
		{"local-checks.yml", "rust-integration-2", 90},
		{"local-checks.yml", "rust-integration-3", 90},
		{"local-checks.yml", "rust-integration-4", 90},
		{"local-checks.yml", "rust-field-read-signing", 90},
		{"local-checks.yml", "rust-aux", 90},
		{"local-checks.yml", "browser-package", 90},
		{"local-checks.yml", "browser-cold-start", 20},
		{"local-checks.yml", "local-gates", 360},
		{"rust-v2-https.yml", "rust-v2-https-ubuntu-24.04", 90},
	} {
		t.Run(tc.role, func(t *testing.T) {
			pin := sourcepacket.Pin{Product: sourcepacket.PressProduct, Workflow: tc.workflow, HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: strings.Repeat("c", 64)}
			if tc.workflow != "local-checks.yml" {
				pin.PullHeadSHA, pin.MergeBaseSHA = strings.Repeat("d", 40), strings.Repeat("e", 40)
			}
			id := strings.Repeat("1", 32)
			plan := linuxJobPlan{Source: pin, RunID: 7, JobID: 9, Attempt: 2, Jobs: 1, Threads: 1, Role: tc.role, Name: "todo2737-" + id, Label: jobLabel(pin, tc.role, 7, 2)}
			b, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			var input bytes.Buffer
			if writeBlob(&input, b) != nil || writeBlob(&input, []byte("invalid public fixture")) != nil {
				t.Fatal("fixture frames failed")
			}
			conn := &pressDeadlineConn{}
			serveLiveJob(conn, &input, id, pin, nil)
			var result liveJobEvent
			if json.Unmarshal(conn.Buffer.Bytes(), &result) != nil || result.Failure != "job_source_packet_rejected" {
				t.Fatalf("did not enter source-free accepted plan path: %s", conn.Buffer.String())
			}
			want := time.Duration(tc.minutes+30) * time.Minute
			if remaining := time.Until(conn.deadline); remaining < want-time.Second || remaining > want {
				t.Fatalf("stream deadline %s, want %s", remaining, want)
			}
			if guestLifetime(pin) <= want {
				t.Fatal("guest lifetime truncates stream")
			}
		})
	}
}
