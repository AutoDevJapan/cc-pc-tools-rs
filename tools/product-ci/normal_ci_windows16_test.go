package main

import (
	"cc/product-ci/normalci"
	"strings"
	"testing"
)

func TestWindowsSixteenCommandsRequireDeliveredWorkflowDigest(t *testing.T) {
	const pinned = "fa5a017fca45710ebccae6a1ca7dfc70070199d9eacb8d94ddb6797d93320220"
	line := "      - run: cargo test --locked -- --test-threads=1 # 0\r\n"
	for _, tc := range []struct {
		count    int
		sha      string
		accepted bool
	}{
		{13, strings.Repeat("a", 64), true},
		{16, pinned, true},
		{16, strings.Repeat("a", 64), false},
		{13, pinned, false},
		{15, pinned, false},
		{17, pinned, false},
	} {
		data := strings.Replace(string(normalCIFixture()), line, strings.Repeat(line, tc.count-12), 1)
		root, err := normalci.ParseYAML([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		count, err := normalci.CountWindowsCargoForWorkflow(root, tc.sha)
		if count != tc.count || (err == nil) != tc.accepted {
			t.Fatalf("count=%d want=%d accepted=%t err=%v", count, tc.count, tc.accepted, err)
		}
	}
}
