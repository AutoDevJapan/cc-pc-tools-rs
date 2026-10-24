//go:build windows

package main

import "testing"

func TestOwnedQuarantineDefaultStaysOnSourceDrive(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`C:\scratch\codex-owned-test`, defaultQuarantinePath},
		{`D:\work\codex-owned-test`, `D:\削除希望`},
		{`d:\work\codex-owned-test`, `d:\削除希望`},
	} {
		if got := ownedQuarantineParentWithType(tc.source, defaultQuarantinePath, func(string) uint32 { return 3 }); got != tc.want {
			t.Fatalf("source %q: got %q, want %q", tc.source, got, tc.want)
		}
	}
}

func TestOwnedQuarantineIgnoresNonlocalDrive(t *testing.T) {
	for _, kind := range []uint32{0, 1, 4, 5} {
		got := ownedQuarantineParentWithType(`X:\work\codex-owned-test`, defaultQuarantinePath, func(root string) uint32 {
			if root != `X:\` {
				t.Fatalf("drive probe requires a root with trailing separator: %q", root)
			}
			return kind
		})
		if got != defaultQuarantinePath {
			t.Fatalf("drive kind %d redirected to %q", kind, got)
		}
	}
}

func TestOwnedQuarantineAcceptsWritableLocalDriveKinds(t *testing.T) {
	for _, kind := range []uint32{2, 3, 6} {
		got := ownedQuarantineParentWithType(`D:\work\codex-owned-test`, defaultQuarantinePath, func(root string) uint32 {
			if root != `D:\` {
				t.Fatalf("unexpected drive root: %q", root)
			}
			return kind
		})
		if got != `D:\削除希望` {
			t.Fatalf("local drive kind %d: %q", kind, got)
		}
	}
}

func TestOwnedQuarantineDoesNotProbeUnchangedConfiguration(t *testing.T) {
	for _, tc := range []struct{ source, configured string }{
		{`D:\work`, `C:\fixture\quarantine`},
		{`C:\work`, defaultQuarantinePath},
		{`\\server\share\work`, defaultQuarantinePath},
	} {
		got := ownedQuarantineParentWithType(tc.source, tc.configured, func(string) uint32 { t.Fatal("unchanged configuration should not query a drive"); return 0 })
		if got != tc.configured {
			t.Fatalf("unchanged destination: %q", got)
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
