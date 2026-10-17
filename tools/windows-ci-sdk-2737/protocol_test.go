package sdkprep

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestDecodeRequestAllowlist(t *testing.T) {
	valid := `{"instance":"vm-123","operation":"prepare-sdk","package":"rust-1.97.1"}`
	if _, err := DecodeRequest(strings.NewReader(valid)); err != nil {
		t.Fatalf("valid fixed request rejected: %v", err)
	}
	for _, input := range []string{
		`{"instance":"vm-123","operation":"exec","command":"whoami"}`,
		`{"instance":"vm-123","operation":"prepare-sdk","package":"anything"}`,
		`{"instance":"vm-123","operation":"status","package":"rust-1.97.1"}`,
		valid + `{}`,
	} {
		if _, err := DecodeRequest(strings.NewReader(input)); err == nil {
			t.Errorf("unsafe or malformed request accepted: %s", input)
		}
	}
}

func TestArtifactDigestAndOrigin(t *testing.T) {
	data := "public pinned payload"
	digest := sha256.Sum256([]byte(data))
	a := Artifact{ID: RustToolchain, Version: "1.97.1", URL: "https://static.rust-lang.org/dist/rust-1.97.1-x86_64-pc-windows-msvc.tar.xz", SHA256: hex.EncodeToString(digest[:])}
	if _, err := VerifyArtifact(strings.NewReader(data), a); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
	if _, err := VerifyArtifact(strings.NewReader("tampered"), a); err == nil {
		t.Fatal("tampered artifact accepted")
	}
	for _, rawURL := range []string{"http://static.rust-lang.org/a", "https://evil.invalid/a", "https://user@github.com/a", "https://github.com:443/a"} {
		a.URL = rawURL
		if err := ValidateArtifact(a); err == nil {
			t.Errorf("unsafe origin accepted: %s", rawURL)
		}
	}
	a.Version = "1.98.0"
	a.URL = "https://static.rust-lang.org/dist/rust-1.97.1-x86_64-pc-windows-msvc.tar.xz"
	if err := ValidateArtifact(a); err == nil {
		t.Fatal("unpinned SDK version accepted")
	}
}

func TestPinnedPackagesAreClosedAndCopied(t *testing.T) {
	pins := PinnedPackages()
	if len(pins) != 6 {
		t.Fatalf("got %d SDK pins, want 6", len(pins))
	}
	pins[0].Version = "tampered"
	if got, ok := pinnedVersion(GitForWindows); !ok || got != "2.56.0.2" {
		t.Fatalf("returned slice modified package pin: %q, %t", got, ok)
	}
	for _, artifact := range PinnedArtifacts() {
		if err := ValidateArtifact(artifact); err != nil {
			t.Errorf("catalog artifact %s %q rejected: %v", artifact.ID, artifact.URL, err)
		}
	}
}

func TestRustManifestPinRejectsChangedMetadata(t *testing.T) {
	if err := VerifyRustChannelManifest([]byte("not the pinned channel manifest")); err == nil {
		t.Fatal("unverified Rust channel manifest accepted")
	}
	if err := VerifyRustChannelManifest(make([]byte, 8<<20+1)); err == nil {
		t.Fatal("oversized Rust channel manifest accepted")
	}
}

func TestSingleJSON(t *testing.T) {
	if !IsSingleJSON([]byte(`{"ok":true}`)) {
		t.Fatal("single JSON rejected")
	}
	if IsSingleJSON([]byte(`{"ok":true} {}`)) {
		t.Fatal("trailing JSON accepted")
	}
}
