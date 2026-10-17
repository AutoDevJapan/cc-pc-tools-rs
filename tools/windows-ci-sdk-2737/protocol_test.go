package sdkprep

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
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

func TestCOMBootstrapEchoesInstanceNonceAndPinsGuest(t *testing.T) {
	host, guest := net.Pipe()
	defer host.Close()
	defer guest.Close()
	serverHash := strings.Repeat("a", 64)
	peerDone := make(chan error, 1)
	go func() {
		line, err := readBoundedLine(bufio.NewReader(guest), 4096)
		if err != nil {
			peerDone <- err
			return
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		var request comRequest
		if err := dec.Decode(&request); err != nil {
			peerDone <- err
			return
		}
		if request.Kind != "sdk-hello" || request.Instance != "nonce-2783" || !validDigest(request.Nonce) {
			peerDone <- io.ErrUnexpectedEOF
			return
		}
		response, err := json.Marshal(comReply{Kind: "sdk-ready", Instance: request.Instance, Nonce: request.Nonce, IPv4: "192.168.30.4", ServerSHA256: serverHash})
		if err != nil {
			peerDone <- err
			return
		}
		_, err = guest.Write(append(response, '\n'))
		peerDone <- err
	}()
	result, err := SendCOMBootstrap(host, "nonce-2783")
	if err != nil {
		t.Fatal(err)
	}
	if result.IPv4.String() != "192.168.30.4" || result.ServerSHA256 != serverHash {
		t.Fatalf("unexpected COM result: %+v", result)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
}

func TestPublicSeedContainsOnlyBoundInstanceAndCertificateHash(t *testing.T) {
	client, err := NewHostIdentity("nonce-2783")
	if err != nil {
		t.Fatal(err)
	}
	defer clearIdentity(&client)
	data, err := BuildPublicSeed("nonce-2783", client)
	if err != nil {
		t.Fatal(err)
	}
	var seed PublicSeed
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&seed); err != nil {
		t.Fatal(err)
	}
	if seed.Instance != "nonce-2783" || seed.ClientSHA256 != CertificateHash(client.Leaf) || strings.Contains(string(data), "private") {
		t.Fatalf("public seed fields are wrong: %s", data)
	}
	if _, err := BuildPublicSeed("other-instance", client); err == nil {
		t.Fatal("cross-instance identity accepted")
	}
}

func TestCOMPipeNameIsInstanceScoped(t *testing.T) {
	name, err := COMPipeName("fresh-nonce-2783")
	if err != nil {
		t.Fatal(err)
	}
	if name != `\\.\pipe\windows-ci-sdk-2737-fresh-nonce-2783` {
		t.Fatalf("unexpected pipe name: %q", name)
	}
	if _, err := COMPipeName(`..\other`); err == nil {
		t.Fatal("invalid instance accepted")
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
