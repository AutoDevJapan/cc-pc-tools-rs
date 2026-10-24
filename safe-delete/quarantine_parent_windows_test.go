//go:build windows

package main

import "testing"

func TestOwnedQuarantineDefaultStaysOnSourceDrive(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`C:\scratch\codex-owned-test`, defaultQuarantinePath},
		{`D:\work\codex-owned-test`, `D:\削除希望`},
		{`d:\work\codex-owned-test`, `d:\削除希望`},
	} {
		if got := ownedQuarantineParent(tc.source, defaultQuarantinePath); got != tc.want {
			t.Fatalf("source %q: got %q, want %q", tc.source, got, tc.want)
		}
	}
}

func TestOwnedQuarantineKeepsExplicitAndUnsupportedDestinations(t *testing.T) {
	const custom = `C:\isolated-fixture\quarantine`
	for _, source := range []string{`C:\scratch`, `D:\scratch`, `\\server\share\scratch`} {
		if got := ownedQuarantineParent(source, custom); got != custom {
			t.Fatalf("explicit fixture destination changed: %q", got)
		}
	}
	for _, source := range []string{`relative\scratch`, `\\server\share\scratch`} {
		if got := ownedQuarantineParent(source, defaultQuarantinePath); got != defaultQuarantinePath {
			t.Fatalf("unsupported volume acquired a production destination: %q", got)
		}
	}
}
